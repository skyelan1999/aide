package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
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

func imageViewFixturePNG(t *testing.T, width, height int) []byte {
	t.Helper()
	im := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			im.SetNRGBA(x, y, color.NRGBA{255, 255, 255, 255})
		}
	}
	im.SetNRGBA(0, 0, color.NRGBA{0, 0, 0, 255})
	var raw bytes.Buffer
	if err := png.Encode(&raw, im); err != nil {
		t.Fatal(err)
	}
	return raw.Bytes()
}

func TestImageViewCropAndFineLineReduction(t *testing.T) {
	raw := imageViewFixturePNG(t, 256, 128)
	encoded, metadata, err := imageViewRaster(raw, map[string]any{"maxDimension": float64(128)})
	if err != nil {
		t.Fatal(err)
	}
	im, err := png.Decode(bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, a := im.At(0, 0).RGBA()
	if im.Bounds().Dx() != 128 || im.Bounds().Dy() != 64 || r/257 != 191 || g != r || b != r || a != 65535 {
		t.Fatalf("lost fine source line during reduction: bounds=%v pixel=%v metadata=%v", im.Bounds(), []uint32{r, g, b, a}, metadata)
	}
	encoded, metadata, err = imageViewRaster(raw, map[string]any{"crop": map[string]any{"x": float64(2), "y": float64(1), "width": float64(7), "height": float64(5)}})
	if err != nil {
		t.Fatal(err)
	}
	im, err = png.Decode(bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	r, _, _, _ = im.At(0, 0).RGBA()
	if im.Bounds().Dx() != 7 || im.Bounds().Dy() != 5 || r != 65535 || metadata["crop"].(map[string]int)["x"] != 2 {
		t.Fatal("crop pixels/provenance wrong", metadata)
	}
}

func TestImageViewRejectsInvalidCropAndParameters(t *testing.T) {
	raw := imageViewFixturePNG(t, 12, 8)
	for _, args := range []map[string]any{
		{"maxDimension": float64(2049)}, {"maxDimension": 128.5}, {"crop": "invalid"},
		{"crop": map[string]any{"x": float64(0)}},
		{"crop": map[string]any{"x": float64(-1), "y": float64(0), "width": float64(2), "height": float64(2)}},
		{"crop": map[string]any{"x": float64(11), "y": float64(0), "width": float64(2), "height": float64(2)}},
		{"crop": map[string]any{"x": float64(0), "y": float64(0), "width": float64(0), "height": float64(2)}},
	} {
		if _, _, err := imageViewRaster(raw, args); err == nil {
			t.Fatalf("accepted invalid image view: %v", args)
		}
	}
	if _, _, err := imageViewRaster([]byte("not an image"), nil); err == nil {
		t.Fatal("accepted corrupt image")
	}
}

func TestImageViewActualBoundReadNoWriteBaselineOrPersistentPixels(t *testing.T) {
	a, _, task := proposalReadFixture(t)
	raw := imageViewFixturePNG(t, 12, 8)
	file := filepath.Join(a.workPath, "drawing.png")
	if err := os.WriteFile(file, raw, 0600); err != nil {
		t.Fatal(err)
	}
	sink := &pluginImageSink{}
	ctx := context.WithValue(context.Background(), pluginImageKey{}, sink)
	text := a.executeToolCall(ctx, testToolCall("view_image", `{"path":"drawing.png"}`), task, nil)
	var meta map[string]any
	if err := json.Unmarshal([]byte(text), &meta); err != nil {
		t.Fatal(text, err)
	}
	if meta["sourceSHA256"] != hash(raw) || len(sink.images) != 1 || len(task.readSnapshots) != 0 {
		t.Fatal("missing image or granted write baseline", text)
	}
	after, err := os.ReadFile(file)
	if err != nil || !bytes.Equal(after, raw) {
		t.Fatal("mutated source raster")
	}
	encoded := strings.SplitN(sink.images[0].DataURL, ",", 2)[1]
	if strings.Contains(text, encoded) {
		t.Fatal("pixels leaked into text tool result")
	}
	msgs := appendPluginImages([]Message{{Role: "tool", Content: text}}, sink)
	durable, _ := json.Marshal(msgs)
	wire, _ := json.Marshal(outgoingMessages(msgs))
	if bytes.Contains(durable, []byte(encoded)) || !bytes.Contains(wire, []byte(encoded)) {
		t.Fatal("visual evidence persistence/wire boundary wrong")
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if hash(decoded) != meta["renderedSHA256"] {
		t.Fatal("rendered image digest mismatch")
	}
}

func TestImageViewWorkspacePathHashAndDenyBoundaries(t *testing.T) {
	a, _, task := proposalReadFixture(t)
	raw := imageViewFixturePNG(t, 12, 8)
	if err := os.WriteFile(filepath.Join(a.workPath, "drawing.png"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.png")
	if err := os.WriteFile(outside, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(a.workPath, "escape.png")); err != nil {
		t.Fatal(err)
	}
	for _, args := range []string{`{"path":"../outside.png"}`, `{"path":"escape.png"}`, `{"path":"drawing.png","sha256":"stale"}`, `{"path":"drawing.png","source":"foreign"}`} {
		sink := &pluginImageSink{}
		ctx := context.WithValue(context.Background(), pluginImageKey{}, sink)
		a.executeToolCall(ctx, testToolCall("view_image", args), task, nil)
		if len(sink.images) != 0 {
			t.Fatal("scope/hash escape", args)
		}
	}
	task.WorkspaceRev++
	sink := &pluginImageSink{}
	ctx := context.WithValue(context.Background(), pluginImageKey{}, sink)
	a.executeToolCall(ctx, testToolCall("view_image", `{"path":"drawing.png"}`), task, nil)
	if len(sink.images) != 0 {
		t.Fatal("stale workspace read")
	}
	task.WorkspaceRev = a.wsRevision
	a.settings.DisabledTools = append(a.settings.DisabledTools, "view_image")
	a.executeToolCall(ctx, testToolCall("view_image", `{"path":"drawing.png"}`), task, nil)
	if len(sink.images) != 0 {
		t.Fatal("disabled image tool ran")
	}
}

func TestImageViewEncodedFileBudgetAndUnavailableChannel(t *testing.T) {
	a, _, task := proposalReadFixture(t)
	f, err := os.Create(filepath.Join(a.workPath, "large.png"))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(imageViewMaxBytes + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()
	sink := &pluginImageSink{}
	ctx := context.WithValue(context.Background(), pluginImageKey{}, sink)
	if _, err := a.viewWorkspaceImage(ctx, task, map[string]any{"path": "large.png"}, a.workspace, "local"); err == nil {
		t.Fatal("unbounded encoded raster")
	}
	if _, err := a.viewWorkspaceImage(context.Background(), task, map[string]any{"path": "large.png"}, a.workspace, "local"); err == nil {
		t.Fatal("claimed visual evidence without a channel")
	}
	if _, err := a.viewWorkspaceImage(ctx, task, map[string]any{"path": "large.png"}, a.workspace, "ssh"); err == nil {
		t.Fatal("SSH fell back to local path")
	}
}

func TestImageViewRejectsDecodeBombBeforePixelAllocation(t *testing.T) {
	raw := imageViewFixturePNG(t, 12, 8)
	// A valid IHDR advertises 16k x 16k. DecodeConfig can read this header;
	// the image tool must reject it before decoding the tiny pixel stream.
	binary.BigEndian.PutUint32(raw[16:20], 16000)
	binary.BigEndian.PutUint32(raw[20:24], 16000)
	binary.BigEndian.PutUint32(raw[29:33], crc32.ChecksumIEEE(raw[12:29]))
	_, _, err := imageViewRaster(raw, nil)
	if err == nil || !strings.Contains(err.Error(), "pixel") {
		t.Fatalf("decode budget not enforced before pixels: %v", err)
	}
}

func TestImageViewActualToolLoopSendsPixelsToProvider(t *testing.T) {
	a := testApp(t)
	raw := imageViewFixturePNG(t, 12, 8)
	if err := os.WriteFile(filepath.Join(a.workPath, "drawing.png"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	seen, calls := false, 0
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		msg := map[string]any{"role": "assistant", "content": "fixture title"}
		if _, ok := body["tools"]; ok {
			calls++
			if calls == 1 {
				msg["content"] = ""
				msg["tool_calls"] = []any{map[string]any{"id": "view-call", "type": "function", "function": map[string]any{"name": "view_image", "arguments": `{"path":"drawing.png"}`}}}
			} else {
				for _, value := range body["messages"].([]any) {
					message := value.(map[string]any)
					parts, _ := message["content"].([]any)
					for _, value := range parts {
						part := value.(map[string]any)
						if part["type"] != "image_url" {
							continue
						}
						url := part["image_url"].(map[string]any)["url"].(string)
						data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(url, "data:image/png;base64,"))
						if err != nil {
							t.Error(err)
							continue
						}
						im, err := png.Decode(bytes.NewReader(data))
						if err != nil {
							t.Error(err)
							continue
						}
						rr, gg, bb, aa := im.At(0, 0).RGBA()
						seen = im.Bounds().Dx() == 12 && im.Bounds().Dy() == 8 && rr == 0 && gg == 0 && bb == 0 && aa == 65535
					}
				}
				msg["content"] = "Controlled tool-loop wire check; no semantic image acceptance claimed."
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": msg, "finish_reason": "stop"}}})
	}))
	defer provider.Close()
	a.settings = Settings{BaseURL: provider.URL, Model: "fixture-vision", Models: []ModelRef{{ID: "fixture-vision", Vision: true}}}
	s := createSession(t, a)
	requireStatus(t, request(a, "POST", "/api/sessions/"+s.ID+"/runs", map[string]any{"mode": "chat", "prompt": "View drawing.png with view_image."}), 202)
	waitTaskDone(t, a, s.ID)
	mu.Lock()
	defer mu.Unlock()
	if !seen {
		t.Fatal("actual view_image pixels missing in next provider request")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	task := a.sessions[s.ID].Runs[0]
	if task.Status != "completed" {
		t.Fatal(task.Error)
	}
	if len(task.readSnapshots) != 0 {
		t.Fatal("image view granted a write baseline")
	}
	for _, call := range task.ToolUses {
		if strings.Contains(call.Result, "data:image/") {
			t.Fatal("image bytes leaked into tool history")
		}
	}
}

func TestImageViewCancelledWhileWaitingForDecodeSlot(t *testing.T) {
	a, _, task := proposalReadFixture(t)
	imageViewDecodeSlot <- struct{}{}
	defer func() { <-imageViewDecodeSlot }()
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), pluginImageKey{}, &pluginImageSink{}))
	cancel()
	if _, err := a.viewWorkspaceImage(ctx, task, map[string]any{"path": "drawing.png"}, a.workspace, "local"); err != context.Canceled {
		t.Fatalf("decode wait ignored cancellation: %v", err)
	}
}

// Explicit operator fixture gate: inspect every supplied raster header, then
// decode/crop the large sources that exceed the initial small-image boundary.
// This is not semantic drawing review and never writes into the fixture tree.
func TestImageViewOperatorRasterFixtures(t *testing.T) {
	dir := os.Getenv("AIDE_IMAGE_VIEW_FIXTURES")
	if dir == "" {
		t.Skip("operator raster fixture directory not supplied")
	}
	rows := []map[string]any{}
	large := 0
	err := filepath.Walk(dir, func(file string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		ext := strings.ToLower(filepath.Ext(file))
		if !info.Mode().IsRegular() || ext != ".png" && ext != ".jpg" && ext != ".jpeg" {
			return nil
		}
		if info.Size() > imageViewMaxBytes {
			t.Fatalf("fixture exceeds encoded budget: %s", file)
		}
		raw, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		config, format, err := image.DecodeConfig(bytes.NewReader(raw))
		if err != nil {
			return err
		}
		pixels := int64(config.Width) * int64(config.Height)
		bpp := int64(4)
		if format == "png" && raw[24] == 16 {
			bpp = 8
		}
		if config.Width > imageViewMaxDimension || config.Height > imageViewMaxDimension || pixels > imageViewMaxPixels || pixels*bpp > imageViewMaxDecodedBytes {
			t.Fatalf("fixture outside decode budget: %s", file)
		}
		rel, _ := filepath.Rel(dir, file)
		row := map[string]any{"file": rel, "width": config.Width, "height": config.Height, "sha256": hash(raw), "header": "PASS"}
		if pixels > 25_000_000 {
			large++
			encoded, meta, err := imageViewRaster(raw, map[string]any{"crop": map[string]any{"x": float64(config.Width - 128), "y": float64(config.Height - 128), "width": float64(128), "height": float64(128)}})
			if err != nil {
				return err
			}
			im, err := png.Decode(bytes.NewReader(encoded))
			if err != nil {
				return err
			}
			if im.Bounds().Dx() != 128 || im.Bounds().Dy() != 128 {
				t.Fatal("large fixture crop lost its source pixel frame")
			}
			row["actualLargeRasterCrop"], row["renderedSHA256"] = "PASS", meta["renderedSHA256"]
		}
		after, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		if hash(after) != row["sha256"] {
			t.Fatal("source fixture changed")
		}
		rows = append(rows, row)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("fixture set contains no rasters")
	}
	result := map[string]any{"status": "PASS", "rasterHeaders": len(rows), "actualLargeRasterCrops": large, "scope": "source budgets and actual bottom-right raster crops only; no semantic drawing acceptance", "rows": rows}
	if file := os.Getenv("AIDE_IMAGE_VIEW_FIXTURE_REPORT"); file != "" {
		b, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("PASS %d raster headers, %d actual large-raster crops", len(rows), large)
}
