package server

// #36 由声音推断性格 单测：WAV 时长解析、声学特征、采纳写回小秘性格、锚点校验。

import (
	"encoding/binary"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

// makeWAV 构造一个最小合法 WAV 字节流（PCM16 mono，指定采样率与秒数）。
func makeWAV(sampleRate uint32, seconds int) []byte {
	dataBytes := uint32(2) * sampleRate * uint32(seconds) // mono 16-bit
	total := 12 + 24 + 8 + dataBytes
	b := make([]byte, 0, total)
	b = append(b, "RIFF"...)
	b = binary.LittleEndian.AppendUint32(b, uint32(total-8))
	b = append(b, "WAVE"...)
	// fmt chunk
	b = append(b, "fmt "...)
	b = binary.LittleEndian.AppendUint32(b, 16)
	b = binary.LittleEndian.AppendUint16(b, 1)       // PCM
	b = binary.LittleEndian.AppendUint16(b, 1)       // mono
	b = binary.LittleEndian.AppendUint32(b, sampleRate)
	b = binary.LittleEndian.AppendUint32(b, sampleRate*2) // byte rate
	b = binary.LittleEndian.AppendUint16(b, 2)        // block align
	b = binary.LittleEndian.AppendUint16(b, 16)       // bits
	// data chunk
	b = append(b, "data"...)
	b = binary.LittleEndian.AppendUint32(b, dataBytes)
	b = append(b, make([]byte, dataBytes)...)
	return b
}

func TestWAVDuration(t *testing.T) {
	b := makeWAV(16000, 3)
	got := wavDurationSec(b)
	if got < 2.9 || got > 3.1 {
		t.Fatalf("3s WAV 应解析出 ~3.0s，got %.2f", got)
	}
	// 非 WAV
	if d := wavDurationSec([]byte("not-a-wav-at-all")); d != 0 {
		t.Fatalf("非 WAV 应返回 0，got %.2f", d)
	}
}

func TestVoicePersonalityAdopt(t *testing.T) {
	a := testApp(t)
	a.unlockPersona(t, "test-pw")

	// 采纳合法提示词（含"小秘"锚点）
	w := request(a, "POST", "/api/voice-personality/adopt", map[string]any{
		"prompt": "你是用户的生活秘书小秘。温暖、口语、体贴，回答简短自然。",
	})
	requireStatus(t, w, 200)
	a.mu.Lock()
	p := a.personalityLocked(personaXiaomi)
	a.mu.Unlock()
	if !strings.Contains(p.Prompt, "小秘") {
		t.Fatalf("采纳后提示词应含小秘：%s", p.Prompt)
	}
	if !p.Enabled {
		t.Error("采纳后应 enabled")
	}
}

func TestVoicePersonalityAdoptRejectsMissingAnchor(t *testing.T) {
	a := testApp(t)
	a.unlockPersona(t, "test-pw")
	w := request(a, "POST", "/api/voice-personality/adopt", map[string]any{
		"prompt": "你是一个温暖的助手，喜欢聊天。", // 无"小秘"
	})
	requireStatus(t, w, 400)
	if !strings.Contains(w.Body.String(), "锚点") {
		t.Errorf("应提示身份锚点：%s", w.Body.String())
	}
}

func TestVoicePersonalityAdoptEmptyPrompt(t *testing.T) {
	a := testApp(t)
	a.unlockPersona(t, "test-pw")
	w := request(a, "POST", "/api/voice-personality/adopt", map[string]any{"prompt": "  "})
	requireStatus(t, w, 400)
}

func TestVoicePersonalityInferNoSamples(t *testing.T) {
	a := testApp(t)
	a.unlockPersona(t, "test-pw")
	w := request(a, "POST", "/api/voice-personality/infer", map[string]any{"sampleIds": []string{}})
	requireStatus(t, w, 400)
}

func TestVoicePersonalityInferFallbackWhenNoLLM(t *testing.T) {
	// 不传 BaseURL/Model → complete 报错 → 走 fallback 画像，不阻断
	a := testApp(t)
	a.unlockPersona(t, "test-pw")
	// 上传一个样本
	uw := rawUpload(a, makeWAV(16000, 2), "consented=1&transcript=你好我是测试转写")
	requireStatus(t, uw, 200)
	var up struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(uw.Body.Bytes(), &up)

	w := request(a, "POST", "/api/voice-personality/infer", map[string]any{"sampleIds": []string{up.ID}})
	requireStatus(t, w, 200)
	var out struct {
		Profile     VoicePersonalityProfile `json:"profile"`
		Features    voiceAcousticFeatures   `json:"features"`
		PromptDraft string                  `json:"promptDraft"`
		Disclaimer  string                  `json:"disclaimer"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("响应非 JSON：%v\n%s", err, w.Body.String())
	}
	if out.Features.DurationSec < 1.9 || out.Features.DurationSec > 2.1 {
		t.Errorf("WAV 时长应 ~2.0s，got %.2f", out.Features.DurationSec)
	}
	if !strings.Contains(out.Disclaimer, "参考") {
		t.Errorf("应带 AI 仅供参考提示：%s", out.Disclaimer)
	}
	if out.PromptDraft == "" {
		t.Error("fallback 应返回当前提示词草稿")
	}
}

// 确保未使用 import
var _ = httptest.NewRecorder
