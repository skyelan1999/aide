package server

import (
	"testing"
)

func TestQwenMediaSelectionGate(t *testing.T) {
	audio := []MessageImage{{MediaType: "audio/wav", DataURL: "data:audio/wav;base64,AA==", Format: "wav"}}
	for _, tc := range []struct {
		model string
		ok    bool
	}{{"qwen3.8-omni-flash", true}, {"qwen3.5-omni-plus", true}, {"qwen3.8-flash", false}, {"qwen3.8-omni-flash-realtime", false}} {
		err := visionGateForConfig(Settings{BaseURL: "https://example.cn-beijing.maas.aliyuncs.com/compatible-mode/v1", Model: tc.model}, audio)
		if (err == nil) != tc.ok {
			t.Fatalf("%s: %v", tc.model, err)
		}
	}
	if isQwenRealtime("qwen-audio-3.1-realtime-plus") || isQwenRealtime("qwen3.8-livetranslate-flash-realtime") {
		t.Fatal("specialized speech protocol must not use Omni bridge")
	}
}
func TestQwenWorkspaceTariffIsolation(t *testing.T) {
	c := &supportCatalog{Providers: []supportProvider{{WorkspaceRegion: "cn-beijing", Paths: []string{"/compatible-mode/v1"}}}}
	for _, tc := range []struct {
		url string
		ok  bool
	}{{"https://ws-test.cn-beijing.maas.aliyuncs.com/compatible-mode/v1", true}, {"https://ws-test.ap-southeast-1.maas.aliyuncs.com/compatible-mode/v1", false}, {"https://ws-test.cn-beijing.maas.aliyuncs.com.evil.test/compatible-mode/v1", false}, {"https://a.b.cn-beijing.maas.aliyuncs.com/compatible-mode/v1", false}, {"http://ws-test.cn-beijing.maas.aliyuncs.com/compatible-mode/v1", false}} {
		if supportMatches(c, tc.url) != tc.ok {
			t.Fatal(tc.url)
		}
	}
}
func TestQwenGeneratedAssetOrigins(t *testing.T) {
	for _, host := range []string{"127.0.0.1", "localhost", "evil.aliyuncs.com", "x.oss-accelerate.aliyuncs.com.evil.test"} {
		if qwenImageAssetHost.MatchString(host) {
			t.Fatal(host)
		}
	}
	if !qwenImageAssetHost.MatchString("dashscope-example.oss-accelerate.aliyuncs.com") {
		t.Fatal("official image origin")
	}
}
