package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/jpeg"
	"strings"
	"testing"
)

func TestTerminalDesktopImagesAreBoundedScopedAndVisionGated(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 20, 10))
	img.Set(1, 1, color.White)
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, nil); err != nil {
		t.Fatal(err)
	}
	raw := map[string]any{"mediaType": "image/jpeg", "imageBase64": base64.StdEncoding.EncodeToString(b.Bytes())}
	var images []MessageImage
	ctx := context.WithValue(context.Background(), terminalDesktopImagesKey{}, &images)
	if collectTerminalDesktopImage(ctx, "other", "terminal_desktop_capture", nil, raw) {
		t.Fatal("foreign owner admitted")
	}
	if collectTerminalDesktopImage(ctx, "terminal", "terminal_session_desktop", map[string]any{"action": "text"}, raw) {
		t.Fatal("input result admitted as image")
	}
	if collectTerminalDesktopImage(ctx, "terminal", "terminal_desktop_capture", nil, map[string]any{"mediaType": "image/jpeg", "imageBase64": "bad"}) {
		t.Fatal("invalid bytes admitted")
	}
	if !collectTerminalDesktopImage(ctx, "terminal", "terminal_desktop_capture", nil, raw) || len(images) != 1 {
		t.Fatal("valid JPEG missing")
	}
	input := []Message{{Role: "tool", ToolCallID: "capture", Content: "saved"}}
	visual := appendTerminalDesktopImages(input, Settings{Model: "gpt-4o"}, images)
	if len(visual) != 2 || len(visual[1].Images) != 1 || !strings.Contains(visual[1].Content, "不可信") {
		t.Fatal("vision evidence not attached safely")
	}
	textOnly := appendTerminalDesktopImages(input, Settings{Model: "text-only-model"}, images)
	if len(textOnly[1].Images) != 0 || !strings.Contains(textOnly[1].Content, "不支持") {
		t.Fatal("non-vision model received image")
	}
	second := appendTerminalDesktopImages(visual, Settings{Model: "gpt-4o"}, images)
	if len(second[1].Images) != 0 || len(second[2].Images) != 1 {
		t.Fatal("old remote frames accumulated in model context")
	}

	for i := 0; i < 8; i++ {
		collectTerminalDesktopImage(ctx, "terminal", "terminal_desktop_capture", nil, raw)
	}
	if len(images) != 4 {
		t.Fatal("image accumulation unbounded")
	}
}
