package server

// Workspace raster evidence travels through the existing ephemeral visual
// channel. Tool text contains provenance, never image bytes or a write baseline.
import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
	"os"
	"path"
	"strings"
)

const imageViewMaxBytes = 16 << 20
const imageViewMaxPixels = 80_000_000
const imageViewMaxDimension = 32768
const imageViewMaxDecodedBytes = 320 << 20

// Decode one source raster at a time across chats; waiting is cancellable.
var imageViewDecodeSlot = make(chan struct{}, 1)

var imageViewTool = map[string]any{"type": "function", "function": map[string]any{
	"name":        "view_image",
	"description": "View a PNG/JPEG in this task's current local workspace as visual evidence. Use list_files to find it. Optional source-pixel crop shows drawing details; maxDimension bounds the returned raster, default1600/max2048. Source remains unchanged. Only the returned crop is covered; this is not OCR, a disk-write baseline, or independent acceptance. Reference sources and SSH are not supported by this tool.",
	"parameters": map[string]any{"type": "object", "properties": map[string]any{
		"path":         map[string]any{"type": "string"},
		"sha256":       map[string]any{"type": "string", "description": "Optional expected original-file SHA-256; mismatch rejects the read."},
		"maxDimension": map[string]any{"type": "integer", "minimum": 128, "maximum": 2048},
		"crop": map[string]any{"type": "object", "properties": map[string]any{
			"x": map[string]any{"type": "integer", "minimum": 0}, "y": map[string]any{"type": "integer", "minimum": 0},
			"width": map[string]any{"type": "integer", "minimum": 1}, "height": map[string]any{"type": "integer", "minimum": 1},
		}, "required": []string{"x", "y", "width", "height"}, "additionalProperties": false},
	}, "required": []string{"path"}, "additionalProperties": false},
}}

func imageViewNumber(args map[string]any, key string, fallback, lo, hi int) (int, error) {
	v, exists := args[key]
	if !exists {
		return fallback, nil
	}
	n, ok := v.(float64)
	if !ok || math.IsNaN(n) || math.IsInf(n, 0) || n != math.Trunc(n) || n < float64(lo) || n > float64(hi) {
		return 0, fmt.Errorf("invalid %s", key)
	}
	return int(n), nil
}

func (a *App) imageViewTaskCurrent(task *Task) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if task == nil || task.WorkspaceID == "" || task.WorkspaceID != a.wsID() || task.WorkspaceRev != a.wsRevision {
		return false
	}
	for _, session := range a.sessions {
		if session.Deleted {
			continue
		}
		for _, run := range session.Runs {
			if run == task {
				return true
			}
		}
	}
	return false
}

func imageViewRaster(raw []byte, args map[string]any) ([]byte, map[string]any, error) {
	config, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || (format != "png" && format != "jpeg") {
		return nil, nil, errors.New("only valid PNG/JPEG raster files are supported")
	}
	pixels := int64(config.Width) * int64(config.Height)
	bytesPerPixel := int64(4)
	if format == "png" && len(raw) > 24 && raw[24] == 16 {
		bytesPerPixel = 8 // PNG16 may decode to RGBA64/NRGBA64.
	}
	if config.Width < 1 || config.Height < 1 || config.Width > imageViewMaxDimension || config.Height > imageViewMaxDimension || pixels > imageViewMaxPixels || pixels*bytesPerPixel > imageViewMaxDecodedBytes {
		return nil, nil, errors.New("source raster exceeds pixel, dimension or decoded-memory budget")
	}
	maxSide, err := imageViewNumber(args, "maxDimension", 1600, 128, 2048)
	if err != nil {
		return nil, nil, err
	}
	x, y, w, h := 0, 0, config.Width, config.Height
	if value, exists := args["crop"]; exists {
		crop, ok := value.(map[string]any)
		if !ok || len(crop) != 4 {
			return nil, nil, errors.New("crop requires x,y,width,height in original pixels")
		}
		for _, key := range []string{"x", "y", "width", "height"} {
			if _, exists := crop[key]; !exists {
				return nil, nil, errors.New("crop requires x,y,width,height")
			}
		}
		for _, field := range []struct {
			key string
			out *int
			min int
		}{{"x", &x, 0}, {"y", &y, 0}, {"width", &w, 1}, {"height", &h, 1}} {
			*field.out, err = imageViewNumber(crop, field.key, 0, field.min, imageViewMaxDimension)
			if err != nil {
				return nil, nil, err
			}
		}
		if x+w > config.Width || y+h > config.Height {
			return nil, nil, errors.New("crop lies outside the source raster")
		}
	}
	source, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, nil, errors.New("corrupt source raster")
	}
	dw, dh := w, h
	if w > maxSide || h > maxSide {
		if w >= h {
			dw, dh = maxSide, max(1, h*maxSide/w)
		} else {
			dh, dw = maxSide, max(1, w*maxSide/h)
		}
	}
	// Area average covers every source pixel when reducing fine drawing lines.
	// No source mutation, filesystem cache, external codec, or network access.
	dest := image.NewNRGBA(image.Rect(0, 0, dw, dh))
	bounds := source.Bounds()
	for j := 0; j < dh; j++ {
		for i := 0; i < dw; i++ {
			x0, x1 := i*w/dw, (i+1)*w/dw
			y0, y1 := j*h/dh, (j+1)*h/dh
			var r, g, b, alpha, n uint64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					rr, gg, bb, aa := source.At(bounds.Min.X+x+sx, bounds.Min.Y+y+sy).RGBA()
					r += uint64(rr)
					g += uint64(gg)
					b += uint64(bb)
					alpha += uint64(aa)
					n++
				}
			}
			if alpha == 0 {
				dest.SetNRGBA(i, j, color.NRGBA{})
				continue
			}
			dest.SetNRGBA(i, j, color.NRGBA{uint8(r * 255 / alpha), uint8(g * 255 / alpha), uint8(b * 255 / alpha), uint8(alpha / n / 257)})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, dest); err != nil {
		return nil, nil, err
	}
	if encoded.Len() > 2<<20 || int64(dw)*int64(dh) > 4_000_000 {
		return nil, nil, errors.New("rendered evidence exceeds budget; request a smaller maxDimension or crop")
	}
	metadata := map[string]any{"sourceWidth": config.Width, "sourceHeight": config.Height,
		"crop": map[string]int{"x": x, "y": y, "width": w, "height": h}, "renderedWidth": dw, "renderedHeight": dh,
		"renderedSHA256": hash(encoded.Bytes()), "coverage": "only the returned crop; source pixels unchanged"}
	return encoded.Bytes(), metadata, nil
}

func (a *App) viewWorkspaceImage(ctx context.Context, task *Task, args map[string]any, root *os.Root, mode string) (string, error) {
	if mode == "ssh" || !a.imageViewTaskCurrent(task) || root == nil {
		return "", errors.New("image viewing requires this task's unchanged current local workspace")
	}
	sink, ok := ctx.Value(pluginImageKey{}).(*pluginImageSink)
	if !ok || sink == nil {
		return "", errors.New("image visual channel unavailable in this execution context")
	}
	sink.mu.Lock()
	full := len(sink.images) >= 4 || sink.bytes >= 4<<20
	sink.mu.Unlock()
	if full {
		return "", errors.New("tool round image budget exhausted; continue in the next round")
	}
	select {
	case imageViewDecodeSlot <- struct{}{}:
		defer func() { <-imageViewDecodeSlot }()
	case <-ctx.Done():
		return "", ctx.Err()
	}
	p, _ := args["path"].(string)
	if err := safePath(p); err != nil {
		return "", err
	}
	ext := strings.ToLower(path.Ext(p))
	if ext != ".png" && ext != ".jpg" && ext != ".jpeg" {
		return "", errors.New("select a PNG/JPEG file")
	}
	// Verified nonblocking regular-file open also rejects symlinks/FIFOs and
	// replacement races. Image reads do not need a broader path surface.
	opener := boundFileOpener(root)
	f, err := opener(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() > imageViewMaxBytes {
		return "", errors.New("image must be a regular file at most16MiB")
	}
	raw, err := io.ReadAll(io.LimitReader(f, imageViewMaxBytes+1))
	if err != nil || len(raw) > imageViewMaxBytes {
		return "", errors.New("image read failed or exceeds16MiB")
	}
	digest := hash(raw)
	if expected, _ := args["sha256"].(string); expected != "" && expected != digest {
		return "", errors.New("source image SHA-256 changed")
	}
	encoded, metadata, err := imageViewRaster(raw, args)
	if err != nil {
		return "", err
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if !a.imageViewTaskCurrent(task) {
		return "", errors.New("image workspace changed during read")
	}
	if err := capturePluginImages(ctx, map[string]any{"protocol": "plugin-tool-images-v1", "images": []any{map[string]any{
		"mediaType": "image/png", "dataBase64": base64.StdEncoding.EncodeToString(encoded), "sha256": hash(encoded)}}}); err != nil {
		return "", err
	}
	metadata["path"], metadata["sourceSHA256"], metadata["sourceBytes"] = p, digest, len(raw)
	metadata["boundary"] = "visual evidence only; no write baseline, OCR, or independent acceptance"
	text, err := json.Marshal(metadata)
	return string(text), err
}
