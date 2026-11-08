package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
)

func snapshotFixture(t *testing.T, im image.Image, rect map[string]any) map[string]any {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, im); err != nil {
		t.Fatal(err)
	}
	raw, err := computerSnapshotEvidence(map[string]any{"app": "Fixture", "mimeType": "image/png", "imageBase64": base64.StdEncoding.EncodeToString(b.Bytes()), "windowId": 42, "window": rect, "pixelWidth": 99999})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestComputerSnapshotRetainsDesktopCoordinateMapping(t *testing.T) {
	raw := snapshotFixture(t, image.NewGray(image.Rect(0, 0, 120, 80)), map[string]any{"x": -300, "y": 20, "width": 600, "height": 400})
	source := raw["source"].(map[string]any)
	if source["pixelWidth"] != 120 || source["pixelHeight"] != 80 || source["coordinateMapping"] == nil {
		t.Fatal(source)
	}
	if err := capturePluginImages(context.Background(), raw); err != nil {
		t.Fatal(err)
	}
	text, _ := normalizePluginResult(raw)
	if !strings.Contains(text, "coordinateMapping") || !strings.Contains(text, "-300") || strings.Contains(text, "dataBase64") {
		t.Fatalf("mapping lost or pixels leaked: %.300s", text)
	}
}

func TestComputerSnapshotRejectsOldOversizedGeometry(t *testing.T) {
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewGray(image.Rect(0, 0, 2560, 1600))); err != nil {
		t.Fatal(err)
	}
	if _, err := computerSnapshotEvidence(map[string]any{"mimeType": "image/png", "imageBase64": base64.StdEncoding.EncodeToString(b.Bytes())}); err == nil {
		t.Fatal("legacy 4096000-pixel image was accepted")
	}
	raw := snapshotFixture(t, image.NewGray(image.Rect(0, 0, 2500, 1600)), nil)
	if err := capturePluginImages(context.Background(), raw); err != nil {
		t.Fatalf("exact four million pixel image failed: %v", err)
	}
}

func TestSurfaceVisualEvidenceBindsWindowAndClickRegion(t *testing.T) {
	im := image.NewRGBA(image.Rect(0, 0, 300, 200))
	rect := map[string]any{"x": 0, "y": 0, "width": 300, "height": 200}
	input := &surfaceApprovalInput{Tool: "computer_click", Args: map[string]any{"x": 30, "y": 30}, Target: computerControlTarget{Name: "Fixture", Rule: "bundle:fixture", PID: 123}}
	first := surfaceVisualInput(input, snapshotFixture(t, im, rect))
	if first == nil || !reviewUIInput(first) || first.Visual.ImageX != 30 || first.Image == nil {
		t.Fatal("visual target missing")
	}
	im.Set(220, 150, color.White)
	outside := surfaceVisualInput(input, snapshotFixture(t, im, rect))
	if outside == nil || !first.Visual.sameTarget(outside.Visual) || first.Visual.ImageDigest == outside.Visual.ImageDigest {
		t.Fatal("unrelated image change should not invalidate the click region")
	}
	im.Set(30, 30, color.White)
	changed := surfaceVisualInput(input, snapshotFixture(t, im, rect))
	if changed == nil || first.Visual.sameTarget(changed.Visual) {
		t.Fatal("changed target region was not detected")
	}
	shifted := surfaceVisualInput(input, snapshotFixture(t, im, map[string]any{"x": 5, "y": 0, "width": 300, "height": 200}))
	if shifted == nil || first.Visual.sameTarget(shifted.Visual) {
		t.Fatal("window movement was not bound")
	}
	encoded, _ := json.Marshal(first)
	if strings.Contains(string(encoded), "base64") || strings.Contains(string(encoded), first.Image.DataURL) {
		t.Fatal("pixels leaked into audit serialization")
	}
	input.Tool = "computer_type"
	if candidate := surfaceVisualInput(input, snapshotFixture(t, im, rect)); candidate != nil && reviewUIInput(candidate) {
		t.Fatal("image-only typing must remain manual")
	}
}
