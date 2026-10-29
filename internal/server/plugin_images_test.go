package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func previewRaster(t *testing.T) string {
	t.Helper()
	im := image.NewRGBA(image.Rect(0, 0, 12, 8))
	im.Set(4, 3, color.RGBA{20, 80, 200, 255})
	var b bytes.Buffer
	if e := png.Encode(&b, im); e != nil {
		t.Fatal(e)
	}
	return base64.StdEncoding.EncodeToString(b.Bytes())
}
func TestPluginImagesValidateAndDoNotLeakIntoText(t *testing.T) {
	s := &pluginImageSink{}
	ctx := context.WithValue(context.Background(), pluginImageKey{}, s)
	encoded := previewRaster(t)
	raw := map[string]any{"protocol": "plugin-tool-images-v1", "source": map[string]any{"sha256": "source-hash"}, "images": []any{map[string]any{"mediaType": "image/png", "dataBase64": encoded}}}
	if e := capturePluginImages(ctx, raw); e != nil {
		t.Fatal(e)
	}
	text, _ := normalizePluginResult(raw)
	if strings.Contains(text, encoded) || !strings.Contains(text, "source-hash") {
		t.Fatal(text)
	}
	msgs := appendPluginImages([]Message{{Role: "tool", Content: text}}, s)
	wire, _ := json.Marshal(outgoingMessages(msgs))
	if !bytes.Contains(wire, []byte("image_url")) || !bytes.Contains(wire, []byte(encoded)) {
		t.Fatal("actual image missing")
	}
	for _, bad := range []any{[]any{map[string]any{"mediaType": "image/svg+xml", "dataBase64": encoded}}, []any{map[string]any{"mediaType": "image/jpeg", "dataBase64": encoded}}, []any{map[string]any{"mediaType": "image/png", "dataBase64": encoded, "sha256": "bad"}}, "invalid"} {
		if capturePluginImages(ctx, map[string]any{"protocol": "plugin-tool-images-v1", "images": bad}) == nil {
			t.Fatal("accepted invalid image")
		}
	}
	old := appendPluginImages(msgs, &pluginImageSink{images: []MessageImage{{MediaType: "image/png", DataURL: "data:image/png;base64," + encoded}}})
	if len(old[len(old)-2].Images) != 0 {
		t.Fatal("old automatic image retained")
	}
	b, _ := json.Marshal(old)
	if bytes.Contains(b, []byte(encoded)) {
		t.Fatal("image bytes persisted in messages")
	}
}
func TestPluginImagesProviderGate(t *testing.T) {
	messages := []Message{{Role: "user", Images: []MessageImage{{MediaType: "image/png", DataURL: "data:image/png;base64," + previewRaster(t)}}}}
	cfg := Settings{Model: "text-only", BaseURL: "http://127.0.0.1:1", Models: []ModelRef{{ID: "text-only", Capabilities: map[string]string{"vision": "unsupported"}}}}
	if _, _, _, e := complete(context.Background(), cfg, messages, ProfileParams{}, nil, nil); e == nil {
		t.Fatal("no vision gate")
	}
	if _, _, _, _, e := completeStream(context.Background(), cfg, messages, ProfileParams{}, nil, nil, nil, nil); e == nil {
		t.Fatal("no streaming vision gate")
	}
}
func TestPluginImagesActualToolLoopWire(t *testing.T) {
	a := testApp(t)
	encoded := previewRaster(t)
	planInstall(t, a, "visual-fixture", `module.exports={apply(ctx){ctx.tool({name:'fixture_render',handler:()=>({protocol:'plugin-tool-images-v1',source:{sha256:'synthetic-page'},images:[{mediaType:'image/png',dataBase64:'`+encoded+`'}]})});}};`)
	var mu sync.Mutex
	seen := false
	calls := 0
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		defer mu.Unlock()
		msg := map[string]any{"role": "assistant", "content": "fixture title"}
		if _, ok := body["tools"]; ok {
			calls++
			if calls == 1 {
				msg["content"] = ""
				msg["tool_calls"] = []any{map[string]any{"id": "image-call", "type": "function", "function": map[string]any{"name": "fixture_render", "arguments": "{}"}}}
			} else {
				data, _ := json.Marshal(body["messages"])
				seen = bytes.Contains(data, []byte("image_url")) && bytes.Contains(data, []byte(encoded))
				msg["content"] = "Controlled wire check complete; no semantic vision acceptance claimed."
			}
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":`)
		_ = json.NewEncoder(w).Encode(msg)
		fmt.Fprint(w, `,"finish_reason":"stop"}]}`)
	}))
	defer provider.Close()
	a.settings = Settings{BaseURL: provider.URL, Model: "fixture-vision", Models: []ModelRef{{ID: "fixture-vision", Vision: true}}}
	s := createSession(t, a)
	requireStatus(t, request(a, "POST", "/api/sessions/"+s.ID+"/runs", map[string]any{"mode": "chat", "prompt": "Use fixture_render then inspect the returned raster."}), 202)
	waitTaskDone(t, a, s.ID)
	mu.Lock()
	defer mu.Unlock()
	if !seen {
		t.Fatal("provider did not receive actual raster in next request")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	task := a.sessions[s.ID].Runs[0]
	if task.Status != "completed" {
		t.Fatal(task.Error)
	}
	for _, u := range task.ToolUses {
		if strings.Contains(u.Result, encoded) {
			t.Fatal("base64 leaked into tool ledger")
		}
	}
}

func TestPluginImagesProtocolAndRoundBudget(t *testing.T) {
	legacy := map[string]any{"images": []any{map[string]any{"name": "ordinary metadata"}}}
	if err := capturePluginImages(context.Background(), legacy); err != nil {
		t.Fatal("legacy metadata changed", err)
	}
	text, _ := normalizePluginResult(legacy)
	if !strings.Contains(text, "ordinary metadata") {
		t.Fatal("legacy metadata stripped")
	}
	s := &pluginImageSink{}
	ctx := context.WithValue(context.Background(), pluginImageKey{}, s)
	part := map[string]any{"mediaType": "image/png", "dataBase64": previewRaster(t)}
	raw := map[string]any{"protocol": "plugin-tool-images-v1", "images": []any{part, part, part, part}}
	if err := capturePluginImages(ctx, raw); err != nil {
		t.Fatal(err)
	}
	if err := capturePluginImages(ctx, map[string]any{"protocol": "plugin-tool-images-v1", "images": []any{part}}); err == nil {
		t.Fatal("round image limit not enforced")
	}
	wire, _ := json.Marshal(outgoingMessages([]Message{{Role: "user", Type: "tool_visual_evidence", Content: "old automatic evidence"}}))
	if !strings.Contains(string(wire), "Image payload absent") {
		t.Fatal("restored checkpoint implies images are present")
	}
}
func TestPluginImagesAutoRouteRequiresVision(t *testing.T) {
	a := testApp(t)
	routerFixture(t, a)
	cfg := routerSettings(t, a, "http://a.invalid", "http://b.invalid")
	file := filepath.Join(a.pluginsPath, "router-fixture", "capabilities.json")
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var catalog routerCatalog
	if json.Unmarshal(b, &catalog) != nil {
		t.Fatal("catalog")
	}
	catalog.Models[1].Vision = true
	b, _ = json.Marshal(catalog)
	if err := os.WriteFile(file, b, 0644); err != nil {
		t.Fatal(err)
	}
	cfg.Models[1].Vision = true
	a.settings.Models[1].Vision = true
	input := appendPluginImages([]Message{{Role: "user", Content: "code implement"}}, &pluginImageSink{images: []MessageImage{{MediaType: "image/png", DataURL: "data:image/png;base64," + previewRaster(t)}}})
	task := &Task{Prompt: "code implement", ModelSelection: &SessionModelSelection{ModelRouter: "auto"}}
	selected := a.routedModel(task, cfg, input, ProfileParams{MaxTokens: 1024}, nil, "chat")
	if selected.Model != "write-model" {
		t.Fatal("router selected a text-only profile for actual images", selected.Model)
	}
}
