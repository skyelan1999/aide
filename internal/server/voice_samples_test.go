package server

// #36 声音样本加密存储 / 删除 / 克隆创建 端到端单测。
// 用 testApp + 直接注入 a.personaKey 解锁，验证：
//   - 未解锁上传 → 403
//   - 未勾选 consented → 400
//   - 上传后音频 AES-256-GCM 加密落盘（.enc 非明文）
//   - 列表只回元数据、不含音频
//   - 删除后 .enc 无残留
//   - 克隆创建：mock 克隆服务返回 voiceID 并写入设置

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// unlockPersona 模拟小秘解锁：把账户密码注入 a.personaKey（deriveKey 用）。
func (a *App) unlockPersona(t *testing.T, pw string) {
	t.Helper()
	a.mu.Lock()
	a.personaKey = pw
	a.mu.Unlock()
}

// rawUpload 向 /api/voice-sample/upload 发原始音频字节。
func rawUpload(a *App, audio []byte, query string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/api/voice-sample/upload?"+query, bytes.NewReader(audio))
	r.Header.Set("Authorization", "Bearer "+a.token)
	r.Header.Set("Content-Type", "audio/wav")
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	return w
}

func TestVoiceSampleRequiresUnlock(t *testing.T) {
	a := testApp(t)
	// 不 unlock
	w := rawUpload(a, []byte("RIFF....fakewav"), "consented=1")
	requireStatus(t, w, 403)
}

func TestVoiceSampleRequiresConsent(t *testing.T) {
	a := testApp(t)
	a.unlockPersona(t, "test-pw")
	w := rawUpload(a, []byte("RIFF....fakewav"), "") // 未勾选 consented
	requireStatus(t, w, 400)
	if !strings.Contains(w.Body.String(), "GDPR") && !strings.Contains(w.Body.String(), "同意") {
		t.Errorf("应提示 GDPR 同意，got %s", w.Body.String())
	}
}

func TestVoiceSampleEncryptedStoreAndDelete(t *testing.T) {
	a := testApp(t)
	a.unlockPersona(t, "test-pw")
	plainAudio := []byte("RIFF\x24\x08\x00\x00WAVEfake-audio-bytes-for-encryption-check-1234567890")

	w := rawUpload(a, plainAudio, "consented=1&durationSec=3")
	requireStatus(t, w, 200)
	var created struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	if created.ID == "" {
		t.Fatal("应返回样本 id")
	}

	// 落盘文件必须是密文：不得含明文音频片段
	encPath := filepath.Join(a.voiceSamplesDir(personaXiaomi), created.ID+".enc")
	raw, err := os.ReadFile(encPath)
	if err != nil {
		t.Fatalf("样本 .enc 应存在：%v", err)
	}
	if bytes.Contains(raw, plainAudio[20:40]) {
		t.Fatal("样本 .enc 含明文音频片段，未加密")
	}
	if len(raw) < len(plainAudio)+12 { // GCM nonce(12)+tag(16)
		t.Fatalf("密文长度异常：%d", len(raw))
	}

	// 列表只回元数据，不回音频
	lw := request(a, "GET", "/api/voice-samples", nil)
	requireStatus(t, lw, 200)
	var list struct {
		Samples []VoiceSampleMeta `json:"samples"`
	}
	_ = json.Unmarshal(lw.Body.Bytes(), &list)
	if len(list.Samples) != 1 || list.Samples[0].ID != created.ID {
		t.Fatalf("列表应含 1 条样本：%s", lw.Body.String())
	}
	if list.Samples[0].Bytes != len(plainAudio) {
		t.Errorf("元数据字节数不符：%d", list.Samples[0].Bytes)
	}

	// 删除后 .enc 无残留
	dw := request(a, "DELETE", "/api/voice-sample/"+created.ID, nil)
	requireStatus(t, dw, 200)
	if _, err := os.Stat(encPath); !os.IsNotExist(err) {
		t.Fatalf("删除后 .enc 应不存在，stat err=%v", err)
	}
	lw2 := request(a, "GET", "/api/voice-samples", nil)
	var list2 struct {
		Samples []VoiceSampleMeta `json:"samples"`
	}
	_ = json.Unmarshal(lw2.Body.Bytes(), &list2)
	if len(list2.Samples) != 0 {
		t.Fatalf("删除后列表应为空：%s", lw2.Body.String())
	}
}

func TestVoiceCloneCreateBindsVoiceID(t *testing.T) {
	// mock 克隆服务：POST /voices 返回 {"voice_id":"cloned-001","status":"ready"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/voices" {
			t.Errorf("克隆创建路径应为 /voices，got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"voice_id":"cloned-001","status":"ready"}`))
	}))
	defer srv.Close()

	a := testApp(t)
	a.unlockPersona(t, "test-pw")
	// 配克隆服务
	a.mu.Lock()
	a.settings.CloneTTSBaseURL = srv.URL
	a.settings.CloneTTSBackend = "openai"
	a.mu.Unlock()

	// 先上传一个样本
	uw := rawUpload(a, []byte("RIFFfake"), "consented=1&durationSec=2")
	requireStatus(t, uw, 200)
	var up struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(uw.Body.Bytes(), &up)

	// 创建音色
	cw := request(a, "POST", "/api/voice-clone/create", map[string]any{"sampleIds": []string{up.ID}})
	requireStatus(t, cw, 200)
	var created struct {
		VoiceID string `json:"voiceID"`
		Bound   bool   `json:"bound"`
	}
	_ = json.Unmarshal(cw.Body.Bytes(), &created)
	if created.VoiceID != "cloned-001" || !created.Bound {
		t.Fatalf("应绑定 cloned-001：%s", cw.Body.String())
	}
	a.mu.Lock()
	got := a.settings.CloneVoiceID
	a.mu.Unlock()
	if got != "cloned-001" {
		t.Fatalf("settings.CloneVoiceID 应为 cloned-001，got %q", got)
	}
}

func TestVoiceCloneCreateNoBaseURL(t *testing.T) {
	a := testApp(t)
	a.unlockPersona(t, "test-pw")
	// 未配 CloneTTSBaseURL
	cw := request(a, "POST", "/api/voice-clone/create", map[string]any{"sampleIds": []string{"x"}})
	requireStatus(t, cw, 400)
}

func TestVoiceSamplesWipe(t *testing.T) {
	a := testApp(t)
	a.unlockPersona(t, "test-pw")
	for i := 0; i < 2; i++ {
		w := rawUpload(a, []byte("RIFFfake"), "consented=1")
		requireStatus(t, w, 200)
	}
	ww := request(a, "POST", "/api/voice-samples/wipe", nil)
	requireStatus(t, ww, 200)
	leftover, _ := os.ReadDir(a.voiceSamplesDir(personaXiaomi))
	for _, e := range leftover {
		if strings.HasSuffix(e.Name(), ".enc") {
			t.Fatalf("wipe 后仍有残留：%s", e.Name())
		}
	}
}
