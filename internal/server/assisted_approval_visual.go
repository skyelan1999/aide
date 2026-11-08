package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
)

// Raster evidence stays in memory. The durable audit binds only its digest.
type surfaceVisualEvidence struct {
	WindowID                float64 `json:"windowId"`
	X, Y, Width, Height     float64
	PixelWidth, PixelHeight int
	ImageX, ImageY          int
	ImageToX, ImageToY      int
	ImageDigest             string
	RegionDigest            string
}

func (v *surfaceVisualEvidence) sameTarget(other *surfaceVisualEvidence) bool {
	return v != nil && other != nil && v.WindowID == other.WindowID && v.X == other.X && v.Y == other.Y && v.Width == other.Width && v.Height == other.Height && v.PixelWidth == other.PixelWidth && v.PixelHeight == other.PixelHeight && v.ImageX == other.ImageX && v.ImageY == other.ImageY && v.ImageToX == other.ImageToX && v.ImageToY == other.ImageToY && v.RegionDigest == other.RegionDigest
}

func surfaceVisualInput(input *surfaceApprovalInput, raw any) *surfaceApprovalInput {
	m, ok := raw.(map[string]any)
	if !ok || input == nil || m["protocol"] != "plugin-tool-images-v1" {
		return nil
	}
	source, _ := m["source"].(map[string]any)
	if source["app"] != input.Target.Name {
		return nil
	}
	rect, _ := source["window"].(map[string]any)
	x, a := uiNumber(rect["x"])
	y, b := uiNumber(rect["y"])
	w, c := uiNumber(rect["width"])
	h, d := uiNumber(rect["height"])
	id, e := uiNumber(source["windowId"])
	px, pxOK := uiNumber(input.Args["x"])
	py, pyOK := uiNumber(input.Args["y"])
	if input.Tool == "computer_key" {
		px = x + w/2
		py = y + h/2
		pxOK = true
		pyOK = true
	}
	if !a || !b || !c || !d || !e || id <= 0 || w <= 0 || h <= 0 || !pxOK || !pyOK || px < x || py < y || px >= x+w || py >= y+h {
		return nil
	}
	parts, _ := m["images"].([]any)
	if len(parts) != 1 {
		return nil
	}
	part, _ := parts[0].(map[string]any)
	encoded, _ := part["dataBase64"].(string)
	media, _ := part["mediaType"].(string)
	if media != "image/png" && media != "image/jpeg" {
		return nil
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(data) == 0 || len(data) > 2<<20 {
		return nil
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width < 1 || config.Height < 1 || config.Width > 4096 || config.Height > 4096 || config.Width*config.Height > 4_000_000 || media != "image/"+format {
		return nil
	}
	im, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	imageX := int((px - x) * float64(config.Width) / w)
	imageY := int((py - y) * float64(config.Height) / h)
	points := []image.Point{{X: imageX, Y: imageY}}
	toX, toY := imageX, imageY
	if input.Tool == "computer_drag" {
		tx, ta := uiNumber(input.Args["toX"])
		ty, tb := uiNumber(input.Args["toY"])
		if !ta || !tb || tx < x || ty < y || tx >= x+w || ty >= y+h {
			return nil
		}
		toX = int((tx - x) * float64(config.Width) / w)
		toY = int((ty - y) * float64(config.Height) / h)
		points = append(points, image.Pt(toX, toY))
	}
	hash := sha256.New()
	details := []MessageImage{}
	for _, point := range points {
		region := image.Rect(point.X-128, point.Y-128, point.X+129, point.Y+129).Intersect(im.Bounds())
		if region.Empty() {
			return nil
		}
		for row := region.Min.Y; row < region.Max.Y; row++ {
			for col := region.Min.X; col < region.Max.X; col++ {
				r, g, b, a := im.At(col, row).RGBA()
				_, _ = hash.Write([]byte{byte(r >> 8), byte(g >> 8), byte(b >> 8), byte(a >> 8)})
			}
		}
		crop := image.NewRGBA(image.Rect(0, 0, region.Dx(), region.Dy()))
		draw.Draw(crop, crop.Bounds(), im, region.Min, draw.Src)
		cx, cy := point.X-region.Min.X, point.Y-region.Min.Y
		// Four short ticks identify the exact target without obscuring its text.
		for d := 6; d <= 13; d++ {
			for _, p := range []image.Point{{cx - d, cy}, {cx + d, cy}, {cx, cy - d}, {cx, cy + d}} {
				if p.In(crop.Bounds()) {
					crop.Set(p.X, p.Y, color.RGBA{255, 35, 80, 255})
				}
			}
		}
		var buf bytes.Buffer
		if png.Encode(&buf, crop) != nil {
			return nil
		}
		details = append(details, MessageImage{MediaType: "image/png", DataURL: "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())})
	}

	full := sha256.Sum256(data)
	copy := *input
	copy.Control = nil
	copy.Visual = &surfaceVisualEvidence{WindowID: id, X: x, Y: y, Width: w, Height: h, PixelWidth: config.Width, PixelHeight: config.Height, ImageX: imageX, ImageY: imageY, ImageToX: toX, ImageToY: toY, ImageDigest: hex.EncodeToString(full[:]), RegionDigest: hex.EncodeToString(hash.Sum(nil))}
	copy.DetailImages = details
	copy.Image = &MessageImage{MediaType: media, DataURL: "data:" + media + ";base64," + encoded}
	return &copy
}

func (a *App) computerSurfaceVisual(ctx context.Context, input *surfaceApprovalInput, approvedRule string) *surfaceApprovalInput {
	if !computerPointerTool(input.Tool) && input.Tool != "computer_key" {
		return nil
	}
	raw, err := a.callComputerControlTool(ctx, "computer_snapshot", nil, input.Target, approvedRule)
	if err != nil {
		copy := *input
		copy.Control = nil
		copy.EvidenceError = "截图请求失败：" + err.Error()
		return &copy
	}
	result := surfaceVisualInput(input, raw)
	if result == nil {
		copy := *input
		copy.Control = nil
		copy.EvidenceError = fmt.Sprintf("截图几何或目标范围无效；tool=%s，起点=(%v,%v)，终点=(%v,%v)", input.Tool, input.Args["x"], input.Args["y"], input.Args["toX"], input.Args["toY"])
		return &copy
	}
	return result
}
