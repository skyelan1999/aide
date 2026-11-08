package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
)

func computerSnapshotEvidence(result map[string]any) (map[string]any, error) {
	encoded, _ := result["imageBase64"].(string)
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(data) == 0 || len(data) > 2<<20 {
		return nil, errors.New("电脑控制截图无效或超过 2 MiB")
	}
	mediaType, _ := result["mimeType"].(string)
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "png" && format != "jpeg") || mediaType != "image/"+format || config.Width < 1 || config.Height < 1 || config.Width > 4096 || config.Height > 4096 || config.Width*config.Height > 4_000_000 {
		return nil, errors.New("电脑截图格式或像素预算无效")
	}
	sum := sha256.Sum256(data)
	source := map[string]any{"app": result["app"], "pixelWidth": config.Width, "pixelHeight": config.Height}
	if rect, ok := result["window"].(map[string]any); ok {
		x, a := uiNumber(rect["x"])
		y, b := uiNumber(rect["y"])
		w, c := uiNumber(rect["width"])
		h, d := uiNumber(rect["height"])
		if a && b && c && d && w > 0 && h > 0 {
			source["window"] = map[string]any{"x": x, "y": y, "width": w, "height": h}
			source["windowId"] = result["windowId"]
			source["coordinateMapping"] = "desktopX = window.x + imageX * window.width / pixelWidth; desktopY = window.y + imageY * window.height / pixelHeight"
		}
	}
	source["textLocations"] = result["textLocations"]
	identity, _ := json.Marshal(source)
	reference := sha256.Sum256(append(identity, data...))
	source["screenshotId"] = hex.EncodeToString(reference[:])
	metadata, _ := json.Marshal(source)
	return map[string]any{"protocol": "plugin-tool-images-v1", "source": source, "images": []any{map[string]any{"mediaType": mediaType, "dataBase64": encoded, "sha256": hex.EncodeToString(sum[:])}}, "text": fmt.Sprintf("已截取目标应用窗口。优先使用 textLocations 返回的文字与桌面坐标。也可 computer_click/drag/scroll 传 coordinateSpace=image 和本图 screenshotId 直接使用图片像素坐标，由服务端换算。不要将图片像素直接当桌面坐标。窗口元数据：%s", metadata)}, nil
}
