package tts

// #44 sherpa-onnx 本地离线 Provider 单测。
// 不依赖真实二进制/模型：用临时空目录 + 不存在的二进制路径，验证
//   - 无模型 → Available()=false、Synth 返回 ErrNotConfigured（上层自动降级）
//   - buildChain 优先级：auto=[sherpa,edge,...]、显式 sherpa=[sherpa]、edge=[edge,...]
// 真实合成需下载模型，记 NOT_RUN（由 scripts/tts-setup + 用户盲听验证）。

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withSherpaEnv 把 sherpa 指向一个空临时目录 + 不存在的二进制，返回清理函数。
func withSherpaEnv(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("SHERPA_TTS_DIR", dir)
	t.Setenv("SHERPA_BIN", filepath.Join(dir, "no-such-sherpa"))
}

func TestSherpaNoModelNotConfigured(t *testing.T) {
	withSherpaEnv(t)
	s := newSherpa("", "")
	if s.Available() {
		t.Fatal("空目录下 sherpa.Available() 应为 false")
	}
	_, err := s.Synth(context.Background(), "你好", SynthOpts{})
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("无模型应返回 ErrNotConfigured，得到 %v", err)
	}
}

func TestSherpaDetectFindsModelDir(t *testing.T) {
	withSherpaEnv(t)
	dir := os.Getenv("SHERPA_TTS_DIR")
	// 放一个假模型目录（含 model.onnx + tokens.txt）
	v := filepath.Join(dir, "fake-voice")
	os.MkdirAll(v, 0o755)
	os.WriteFile(filepath.Join(v, "model.onnx"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(v, "tokens.txt"), []byte("a 0\n"), 0o644)
	os.WriteFile(filepath.Join(v, "meta.json"), []byte(`{"name":"测试女","gender":"female"}`), 0o644)

	binOK, voices := SherpaModelInfo("", "")
	// 二进制不存在 → binOK=false（detect 返回 ErrNotConfigured），但 discoverVoices 能列出目录
	if binOK {
		t.Log("binOK=true（若本机恰有 sherpa 二进制）")
	}
	if len(voices) != 1 || voices[0].Dir != "fake-voice" {
		t.Fatalf("应发现 1 个音色 fake-voice，得到 %+v", voices)
	}
	if voices[0].Name != "测试女" {
		t.Fatalf("meta.json 展示名未读取，得到 %q", voices[0].Name)
	}
	if voices[0].Gender != "female" {
		t.Fatalf("性别未读取，得到 %q", voices[0].Gender)
	}
}

func TestBuildChainPriority(t *testing.T) {
	withSherpaEnv(t)
	// auto：sherpa 第一，edge 第二
	auto := buildChain(Config{Provider: "auto"})
	if len(auto) < 2 || auto[0].Name() != "sherpa" || auto[1].Name() != "edge" {
		t.Fatalf("auto 链应为 [sherpa, edge, ...]，得到 %s/%s", namesOf(auto), namesOf(auto))
	}
	// 显式 sherpa：链里只有 sherpa（不静默降级 edge）
	sh := buildChain(Config{Provider: "sherpa"})
	if len(sh) != 1 || sh[0].Name() != "sherpa" {
		t.Fatalf("显式 sherpa 链应只有 sherpa，得到 %s", namesOf(sh))
	}
	// 显式 edge：不走 sherpa
	ed := buildChain(Config{Provider: "edge"})
	if len(ed) < 1 || ed[0].Name() != "edge" {
		t.Fatalf("edge 链应 edge 开头，得到 %s", namesOf(ed))
	}
	for _, e := range ed {
		if e.Name() == "sherpa" {
			t.Fatal("edge 链不应包含 sherpa")
		}
	}
}

func TestChainSynthExplicitSherpaNoModel(t *testing.T) {
	withSherpaEnv(t)
	// 显式选 sherpa：链里只有 sherpa，无模型 → 直接 ErrNotConfigured（不静默降级 edge）。
	_, engine, err := ChainSynth(context.Background(), "你好", SynthOpts{}, Config{Provider: "sherpa"})
	if err == nil {
		t.Fatal("无模型时显式 sherpa 应失败")
	}
	if !errors.Is(err, ErrNotConfigured) && !strings.Contains(err.Error(), "未配置") {
		t.Fatalf("应返回 ErrNotConfigured，得到 %v", err)
	}
	if engine != "" {
		t.Fatalf("失败时不应报告命中引擎，得到 %q", engine)
	}
}

func TestChainSynthAutoSkipsNoModelSherpa(t *testing.T) {
	withSherpaEnv(t)
	// auto 链：sherpa 无模型返回 ErrNotConfigured 后自动跳过（不 panic、不阻塞）。
	// edge 在本测试环境可能通也可能不通——只验证 sherpa 不阻断链、不返回 sherpa 作为命中引擎。
	_, engine, _ := ChainSynth(context.Background(), "你好", SynthOpts{}, Config{Provider: "auto"})
	if engine == "sherpa" {
		t.Fatal("无模型时不应命中 sherpa 作为实际引擎")
	}
}

func namesOf(engines []TTSProvider) string {
	var b strings.Builder
	for i, e := range engines {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(e.Name())
	}
	return b.String()
}
