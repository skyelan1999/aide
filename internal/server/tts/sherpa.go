package tts

// sherpa-onnx 本地离线 TTS Provider（#44）。
//
// 设计原则：完全离线、零网络、可空气隙（air-gap）部署。
//   - 二进制 sherpa-onnx-offline-tts 由 Dockerfile 打进运行镜像（shared/CPU 预编译，无 cgo），
//     Go 仅经 os/exec 拉起子进程，不链接任何 C 库。
//   - 模型外置在 /data/tts/（不进镜像控体积）。联网环境用 scripts/tts-setup 下载+SHA256 校验；
//     保密/空气隙环境由管理员手动把模型目录放入 /data/tts/ 并校验。
//   - 无模型（或二进制缺失）→ ErrNotConfigured，上层 buildChain 自动跳过，降级 edge → 浏览器 Web Speech。
//   - 输出 16/22kHz WAV；前端经 blob URL 直接 <audio> 播放，无需转码（离线无 ffmpeg）。
//
// 磁盘约定（scripts/tts-setup 生成；手动放入时照此布局）：
//
//	/data/tts/
//	  <voice-name>/                 一个音色 = 一个目录
//	    model.onnx                  主模型（tts-setup 会把真实 *.onnx 软链/拷贝成 model.onnx）
//	    lexicon.txt                 词典（vits-zh-hf 类需要；piper 类可缺省）
//	    tokens.txt                  音素表（必备）
//	    number.fst / date.fst / phone.fst   规则 FST（可选，自动收集逗号拼接）
//	    espeak-ng-data/             piper 类模型的音素数据目录（存在则作为 --vits-data-dir）
//	    meta.json                   可选：{"name":"华严(女)","gender":"female","nSpeakers":1,"piper":false}

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// sherpaSynthTimeout 单次合成总超时（含模型冷加载 + 推理）。本地 CPU 首调用要加载 ~100MB 模型，给足。
var sherpaSynthTimeout = 30 * time.Second

// ErrSherpaUnavailable 二进制存在但模型加载/推理失败（非“未配置”）。上层切下一引擎。
var ErrSherpaUnavailable = fmt.Errorf("sherpa-onnx 合成失败")

// sherpaModelMeta 单个模型目录的可选自述（meta.json）。
type sherpaModelMeta struct {
	Name       string `json:"name"`       // 展示名，如 "华严（女）"
	Gender     string `json:"gender"`     // female|male
	NSpeakers  int    `json:"nSpeakers"`  // 多 speaker 模型的说话人数；0/1=单音色
	Piper      bool   `json:"piper"`      // 是否 piper 类（用 espeak-ng-data 而非 lexicon）
	SidDefault int    `json:"sidDefault"` // 多 speaker 模型默认 sid
}

// sherpaVoice 一个已安装的本地音色（供 /api/config 回显 + 前端展示）。
type sherpaVoice struct {
	Dir    string `json:"dir"`    // 目录名（作为 voice id 传给合成）
	Name   string `json:"name"`   // 展示名
	Gender string `json:"gender"` // female|male
}

// sherpa 是 sherpa-onnx Provider。无状态：每次 Synth 新起一个 CLI 子进程（模型在进程内加载）。
type sherpa struct {
	bin    string
	models string
}

// newSherpa 构造本地离线 Provider。bin/models 空时用默认路径（可用环境变量覆盖，便于测试）。
func newSherpa(bin, modelsDir string) *sherpa {
	if bin == "" {
		bin = "/usr/local/bin/sherpa-onnx-offline-tts"
	}
	if modelsDir == "" {
		modelsDir = "/data/tts"
	}
	if v := strings.TrimSpace(os.Getenv("SHERPA_BIN")); v != "" {
		bin = v
	}
	if v := strings.TrimSpace(os.Getenv("SHERPA_TTS_DIR")); v != "" {
		modelsDir = v
	}
	return &sherpa{bin: bin, models: modelsDir}
}

func (s *sherpa) Name() string   { return "sherpa" }
func (s *sherpa) Format() string { return "wav" }

// Available 二进制存在且至少发现一个模型目录（含 model.onnx）。
func (s *sherpa) Available() bool {
	_, err := s.detect()
	return err == nil
}

// sherpaDetectResult 探测结果。
type sherpaDetectResult struct {
	binOK  bool
	voices []sherpaVoice
}

// detect 探测二进制与已安装模型。无模型/无二进制返回 ErrNotConfigured。
func (s *sherpa) detect() (*sherpaDetectResult, error) {
	binOK := true
	if _, err := os.Stat(s.bin); err != nil {
		binOK = false
	}
	voices, _ := s.discoverVoices()
	if !binOK || len(voices) == 0 {
		return &sherpaDetectResult{binOK: binOK, voices: voices}, ErrNotConfigured
	}
	return &sherpaDetectResult{binOK: binOK, voices: voices}, nil
}

// discoverVoices 扫描 models 目录下含 model.onnx 的子目录，返回音色列表（按目录名排序，稳定）。
func (s *sherpa) discoverVoices() ([]sherpaVoice, error) {
	entries, err := os.ReadDir(s.models)
	if err != nil {
		return nil, err
	}
	var out []sherpaVoice
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(s.models, e.Name())
		onnx := sherpaFindOnnx(dir)
		if onnx == "" {
			continue
		}
		v := sherpaVoice{Dir: e.Name(), Name: e.Name(), Gender: "female"}
		if meta, err := loadSherpaMeta(dir); err == nil {
			if meta.Name != "" {
				v.Name = meta.Name
			}
			if meta.Gender != "" {
				v.Gender = meta.Gender
			}
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Dir < out[j].Dir })
	return out, nil
}

// sherpaFindOnnx 在模型目录里找主 onnx：优先 model.onnx，否则取最大的非 int8 *.onnx。
func sherpaFindOnnx(dir string) string {
	if fi, err := os.Stat(filepath.Join(dir, "model.onnx")); err == nil && !fi.IsDir() {
		return filepath.Join(dir, "model.onnx")
	}
	entries, _ := os.ReadDir(dir)
	var best string
	var bestSize int64
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".onnx") {
			continue
		}
		if strings.Contains(e.Name(), ".int8.onnx") {
			continue // 优先非量化模型
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		if fi.Size() > bestSize {
			bestSize = fi.Size()
			best = filepath.Join(dir, e.Name())
		}
	}
	return best
}

func loadSherpaMeta(dir string) (sherpaModelMeta, error) {
	var m sherpaModelMeta
	b, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		return m, err
	}
	err = json.Unmarshal(b, &m)
	return m, err
}

// Synth 合成一段文本为 WAV。模型未配置 → ErrNotConfigured（上层降级）；
// 二进制在但推理失败 → ErrSherpaUnavailable（上层切下一引擎）。
func (s *sherpa) Synth(ctx context.Context, text string, opts SynthOpts) (io.ReadCloser, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, errors.New("合成文本为空")
	}
	det, err := s.detect()
	if err != nil {
		return nil, err // ErrNotConfigured
	}

	// 选模型目录：显式 voice 命中某音色目录则用之，否则取第一个。
	dirName := ""
	if v := strings.TrimSpace(opts.Voice); v != "" {
		// 允许 "sid:12" 形式直接指定 speaker id（多 speaker 模型）；目录名仍取默认。
		if strings.HasPrefix(v, "sid:") {
			dirName = det.voices[0].Dir
		} else {
			for _, voice := range det.voices {
				if voice.Dir == v || voice.Name == v {
					dirName = voice.Dir
					break
				}
			}
		}
	}
	if dirName == "" {
		dirName = det.voices[0].Dir
	}
	modelDir := filepath.Join(s.models, dirName)
	onnx := sherpaFindOnnx(modelDir)
	if onnx == "" {
		return nil, fmt.Errorf("%w: 模型目录 %s 无 .onnx", ErrNotConfigured, dirName)
	}

	// 组装 CLI 参数。
	args := []string{"--vits-model=" + onnx}
	if tok := sherpaFirstExist(modelDir, "tokens.txt"); tok != "" {
		args = append(args, "--vits-tokens="+tok)
	}
	// piper 类用 espeak-ng-data；vits-zh-hf 类用 lexicon。
	if espeakDir := filepath.Join(modelDir, "espeak-ng-data"); isDir(espeakDir) {
		args = append(args, "--vits-data-dir="+espeakDir)
	} else if lex := sherpaFirstExist(modelDir, "lexicon.txt"); lex != "" {
		args = append(args, "--vits-lexicon="+lex)
	}
	// 规则 FST（可选）：收集目录下所有 *.fst 逗号拼接。
	if fsts := sherpaCollectFsts(modelDir); len(fsts) > 0 {
		args = append(args, "--tts-rule-fsts="+strings.Join(fsts, ","))
	}
	// 语速：sherpa --vits-length-scale 与语速成反比（1.0 正常；越大越慢）。
	lengthScale := 1.0
	if opts.Rate > 0 {
		lengthScale = 1.0 / opts.Rate
	}
	args = append(args, fmt.Sprintf("--vits-length-scale=%.3f", lengthScale))
	// 多 speaker：voice 形如 "sid:N" 时透传。
	if strings.HasPrefix(strings.TrimSpace(opts.Voice), "sid:") {
		var sid int
		if _, e := fmt.Sscanf(strings.TrimSpace(opts.Voice), "sid:%d", &sid); e == nil {
			args = append(args, fmt.Sprintf("--sid=%d", sid))
		}
	}

	// 输出到临时 WAV。
	outTmp, err := os.CreateTemp("", "sherpa-*.wav")
	if err != nil {
		return nil, fmt.Errorf("%w: 建临时文件失败：%w", ErrSherpaUnavailable, err)
	}
	outPath := outTmp.Name()
	_ = outTmp.Close()
	defer os.Remove(outPath)
	args = append(args, "--output-filename="+outPath, text)

	// 带超时执行。
	runCtx, cancel := context.WithTimeout(ctx, sherpaSynthTimeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, s.bin, args...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if runCtx.Err() != nil {
			return nil, fmt.Errorf("%w: 合成超时(%s)", ErrSherpaUnavailable, sherpaSynthTimeout)
		}
		return nil, fmt.Errorf("%w: %v: %s", ErrSherpaUnavailable, err, strings.TrimSpace(stderr.String()))
	}
	wavBytes, err := os.ReadFile(outPath)
	if err != nil {
		return nil, fmt.Errorf("%w: 读取输出失败：%w", ErrSherpaUnavailable, err)
	}
	if len(wavBytes) == 0 {
		return nil, fmt.Errorf("%w: 输出为空", ErrSherpaUnavailable)
	}
	return io.NopCloser(strings.NewReader(string(wavBytes))), nil
}

// ProbeSherpa 轻量探测：二进制+模型是否就绪。不实际合成（避免每次都加载模型冷启动）。
func ProbeSherpa(bin, modelsDir string) error {
	s := newSherpa(bin, modelsDir)
	_, err := s.detect()
	return err
}

// SherpaModelInfo 返回 (二进制是否就位, 已发现的本地音色列表)，供 /api/config 回显。
// 音色发现与二进制就位解耦：即使二进制缺失也列出模型目录（前端可提示"模型已放但二进制未装"）。
// sherpaAvailable = binOK && len(voices)>0。
func SherpaModelInfo(bin, modelsDir string) (binOK bool, voices []sherpaVoice) {
	s := newSherpa(bin, modelsDir)
	if _, err := os.Stat(s.bin); err == nil {
		binOK = true
	}
	voices, _ = s.discoverVoices()
	return binOK, voices
}

func isDir(p string) bool { fi, err := os.Stat(p); return err == nil && fi.IsDir() }

func sherpaFirstExist(dir, name string) string {
	p := filepath.Join(dir, name)
	if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
		return p
	}
	return ""
}

// sherpaCollectFsts 收集目录下所有 *.fst（排序稳定）。
func sherpaCollectFsts(dir string) []string {
	entries, _ := os.ReadDir(dir)
	var fsts []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".fst") {
			fsts = append(fsts, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(fsts)
	return fsts
}
