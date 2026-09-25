package server

// 由声音推断性格（#36）。
//
// 流程：样本（加密音频 + 可选浏览器 STT 转写）→ 轻量声学特征（时长/语速/停顿粗估）
// → 交 LLM 推断沟通风格与性格画像（大五倾向、正式度、热情/冷静、果断/谨慎、幽默、情绪稳定）
// → 输出结构化画像 + "小秘性格提示词草稿" → 用户确认/编辑后采纳，写入小秘性格（#34 蓝本性格）。
//
// 合规：画像标注"AI 推断仅供参考"；用户可一键恢复默认。声学特征只在内存计算，不落盘。

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// VoicePersonalityProfile 性格画像（LLM 推断结果，结构化）。
type VoicePersonalityProfile struct {
	Openness         int    `json:"openness"`         // 开放性 0-100
	Conscientiousness int   `json:"conscientiousness"` // 尽责性 0-100
	Extraversion     int    `json:"extraversion"`     // 外向性 0-100
	Agreeableness    int    `json:"agreeableness"`    // 宜人性 0-100
	Neuroticism      int    `json:"neuroticism"`      // 情绪稳定性(反向) 0-100
	Formality        string `json:"formality"`        // formal | neutral | casual
	Warmth           string `json:"warmth"`           // warm | neutral | cool
	Decisiveness     string `json:"decisiveness"`     // decisive | balanced | cautious
	Humor            string `json:"humor"`            // playful | mild | serious
	EmotionalStable  string `json:"emotionalStable"`  // calm | neutral | expressive
	Summary          string `json:"summary"`          // 一句话画像
}

// voiceAcousticFeatures 轻量声学特征（无 DSP 依赖；WAV 头解析时长 + 转写字数比）。
type voiceAcousticFeatures struct {
	DurationSec  float64 `json:"durationSec"`
	CharsPerSec  float64 `json:"charsPerSec"`  // 语速（中文字符/秒）
	SampleCount  int     `json:"sampleCount"`
	HasTranscript bool   `json:"hasTranscript"`
}

// wavDurationSec 从 WAV 字节解析时长（秒）。非 WAV 返回 0。
// WAV 头：RIFF(4)+size(4)+WAVE(4)，随后找 "fmt "(16B) 与 "data" 块。
func wavDurationSec(b []byte) float64 {
	if len(b) < 44 || string(b[0:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		return 0
	}
	dataSize := uint32(0)
	sampleRate := uint32(0)
	bitsPerSample := uint16(0)
	channels := uint16(0)
	for i := 12; i+8 <= len(b); {
		chunkID := string(b[i : i+4])
		chunkSize := binary.LittleEndian.Uint32(b[i+4 : i+8])
		switch chunkID {
		case "fmt ":
			if i+8+16 > len(b) {
				return 0
			}
			channels = binary.LittleEndian.Uint16(b[i+8 : i+10])
			sampleRate = binary.LittleEndian.Uint32(b[i+12 : i+16])
			bitsPerSample = binary.LittleEndian.Uint16(b[i+22 : i+24])
		case "data":
			dataSize = chunkSize
		}
		i += 8 + int(chunkSize)
		if chunkSize%2 == 1 {
			i++
		}
	}
	if sampleRate == 0 || channels == 0 || bitsPerSample == 0 || dataSize == 0 {
		return 0
	}
	bytesPerSec := uint32(channels) * sampleRate * uint32(bitsPerSample) / 8
	if bytesPerSec == 0 {
		return 0
	}
	return float64(dataSize) / float64(bytesPerSec)
}

// computeAcousticFeatures 汇总样本的声学特征（时长优先取 WAV 解析，回退元数据 durationSec）。
func (a *App) computeAcousticFeatures(samples []VoiceSampleMeta, decrypt func(id string) ([]byte, error)) (voiceAcousticFeatures, error) {
	feats := voiceAcousticFeatures{SampleCount: len(samples)}
	totalDur := 0.0
	totalChars := 0
	for _, s := range samples {
		dur := float64(s.DurationSec)
		audio, err := decrypt(s.ID)
		if err == nil && len(audio) > 44 {
			if d := wavDurationSec(audio); d > 0 {
				dur = d
			}
		}
		totalDur += dur
		if strings.TrimSpace(s.Transcript) != "" {
			feats.HasTranscript = true
			totalChars += len([]rune(strings.TrimSpace(s.Transcript)))
		}
	}
	feats.DurationSec = totalDur
	if totalDur > 0 {
		feats.CharsPerSec = float64(totalChars) / totalDur
	}
	return feats, nil
}

// voicePersonalityInfer 由样本推断性格画像 + 提示词草稿。
// 请求体：{sampleIds:[...]}。需小秘已解锁；样本需含 transcript 才更准（否则只给保守画像）。
func (a *App) voicePersonalityInfer(w http.ResponseWriter, r *http.Request) {
	var in struct {
		SampleIDs []string `json:"sampleIds"`
	}
	if err := decode(w, r, &in); err != nil {
		return
	}
	if len(in.SampleIDs) == 0 {
		fail(w, 400, errors.New("请至少选择一个声音样本"))
		return
	}
	key, err := a.voiceSampleKey()
	if err != nil {
		fail(w, 403, err)
		return
	}

	profile := personaXiaomi
	idx := a.loadVoiceSampleIndex(profile)
	byID := map[string]VoiceSampleMeta{}
	for _, it := range idx.Items {
		byID[it.ID] = it
	}
	var samples []VoiceSampleMeta
	for _, sid := range in.SampleIDs {
		if m, ok := byID[sid]; ok {
			samples = append(samples, m)
		}
	}
	if len(samples) == 0 {
		fail(w, 404, errors.New("所选样本不存在"))
		return
	}

	decrypt := func(id string) ([]byte, error) {
		return decryptSampleFile(key, sampleEncPath(a, profile, id))
	}
	feats, err := a.computeAcousticFeatures(samples, decrypt)
	if err != nil {
		fail(w, 500, err)
		return
	}

	// 汇总转写文本（取前 800 字，避免上下文爆炸）
	var transcript strings.Builder
	for _, s := range samples {
		if s.Transcript != "" {
			transcript.WriteString(s.Transcript)
			transcript.WriteString("\n")
		}
	}
	transcriptStr := strings.TrimSpace(transcript.String())
	if len([]rune(transcriptStr)) > 800 {
		transcriptStr = string([]rune(transcriptStr)[:800])
	}

	a.mu.Lock()
	cfg := a.settings
	cur := a.personalityLocked(personaXiaomi)
	a.mu.Unlock()

	profileResult, promptDraft, err := a.inferPersonalityLLM(r.Context(), cfg, cur.Prompt, feats, transcriptStr)
	if err != nil {
		// LLM 不可用时返回保守占位（不阻断流程，用户可手动编辑）
		profileResult = VoicePersonalityProfile{
			Formality: "neutral", Warmth: "warm", Decisiveness: "balanced",
			Humor: "mild", EmotionalStable: "calm",
			Summary: "（LLM 不可用，未能从声音推断；请手动调整性格提示词）",
		}
		promptDraft = cur.Prompt
	}

	jsonOut(w, 200, map[string]any{
		"profile":    profileResult,
		"features":   feats,
		"promptDraft": promptDraft,
		"disclaimer": "AI 推断仅供参考，采纳前请人工确认/编辑",
	})
}

// inferPersonalityLLM 调 LLM 由声学特征 + 转写推断性格，返回结构化画像与提示词草稿。
func (a *App) inferPersonalityLLM(ctx context.Context, cfg Settings, curPrompt string, feats voiceAcousticFeatures, transcript string) (VoicePersonalityProfile, string, error) {
	sys := `你是一名语音行为分析与人格画像专家。根据用户声音样本的声学特征和转写文本，
推断其沟通风格与性格倾向，并据此为"私人生活秘书小秘"写一版性格提示词草稿。
只输出 JSON，不要解释、不要 markdown 围栏。JSON schema：
{"profile":{"openness":0-100,"conscientiousness":0-100,"extraversion":0-100,
"agreeableness":0-100,"neuroticism":0-100,"formality":"formal|neutral|casual",
"warmth":"warm|neutral|cool","decisiveness":"decisive|balanced|cautious",
"humor":"playful|mild|serious","emotionalStable":"calm|neutral|expressive",
"summary":"一句话画像"},
"promptDraft":"小秘性格提示词正文（保留'小秘'身份，150-300字，温暖口语、体贴简洁）"}`

	req := fmt.Sprintf(`声学特征：
- 总时长 %.1fs，语速 %.1f 字/秒，样本数 %d，含转写：%v

转写片段：
"""
%s
"""

当前小秘性格提示词（保留身份锚点，在其基础上微调语气与侧重）：
"""
%s
"""

请输出上述 JSON。`,
		feats.DurationSec, feats.CharsPerSec, feats.SampleCount, feats.HasTranscript,
		orDash(transcript), curPrompt)

	out, _, _, err := complete(ctx, cfg, []Message{
		{Role: "system", Content: sys},
		{Role: "user", Content: req},
	}, ProfileParams{Temperature: fp(0.3), MaxTokens: 1024, ResponseFormat: "json_object"}, nil, nil)
	if err != nil {
		return VoicePersonalityProfile{}, "", err
	}
	out = strings.TrimSpace(out)
	out = strings.TrimPrefix(out, "```")
	out = strings.TrimSuffix(out, "```")
	out = strings.TrimSpace(out)
	var parsed struct {
		Profile     VoicePersonalityProfile `json:"profile"`
		PromptDraft string                  `json:"promptDraft"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		return VoicePersonalityProfile{}, "", fmt.Errorf("LLM 返回非 JSON：%w", err)
	}
	if strings.TrimSpace(parsed.PromptDraft) == "" {
		parsed.PromptDraft = curPrompt
	}
	return parsed.Profile, parsed.PromptDraft, nil
}

// voicePersonalityAdopt 采纳推断出的性格提示词草稿（用户可编辑后提交），写入小秘性格。
// 复用 #34 演化体系：保留旧版入回滚栈、校验身份锚点、更新时间戳。
func (a *App) voicePersonalityAdopt(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Prompt string `json:"prompt"`
	}
	if err := decode(w, r, &in); err != nil {
		return
	}
	prompt := strings.TrimSpace(in.Prompt)
	if prompt == "" {
		fail(w, 400, errors.New("prompt 不能为空"))
		return
	}
	id := personaXiaomi
	if !a.personalityCoreOK(id, prompt) {
		fail(w, 400, errors.New("性格提示词必须保留「小秘」身份锚点"))
		return
	}
	a.mu.Lock()
	cur := a.personalityLocked(id)
	// 旧版入回滚栈
	st := a.personalityStateLocked(id)
	st.History = appendHistory(st.History, strings.TrimSpace(cur.Prompt))
	if a.settings.Personalities == nil {
		a.settings.Personalities = map[string]Personality{}
	}
	cur.Prompt = prompt
	cur.Enabled = true
	cur.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	a.settings.Personalities[id] = cur
	a.personalityState.Entries[id] = st
	a.persistPersonalitiesLocked()
	a.persistPersonalityStateLocked()
	a.mu.Unlock()
	jsonOut(w, 200, map[string]any{"adopted": true, "updatedAt": cur.UpdatedAt, "evolutions": cur.Evolutions})
}

// sampleEncPath 样本加密文件路径（内部用）。
func sampleEncPath(a *App, profile, id string) string {
	return a.voiceSamplesDir(profile) + "/" + id + ".enc"
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "（无转写）"
	}
	return s
}
