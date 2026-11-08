package server

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

type computerSnapshotReference struct {
	ID                      string
	Target                  computerControlTarget
	Window                  map[string]any
	WindowID                float64
	PixelWidth, PixelHeight float64
	At                      time.Time
	ImagePoints             map[string]any
}
type computerActionReferenceKey struct{}

func normalizeComputerKey(raw string) (string, error) {
	parts := strings.Split(strings.ToUpper(strings.TrimSpace(raw)), "+")
	if len(parts) > 5 {
		return "", errors.New("组合键最多四个修饰键和一个主键")
	}
	aliases := map[string]string{"COMMAND": "CMD", "META": "CMD", "SUPER": "CMD", "CONTROL": "CTRL", "OPTION": "ALT", "RETURN": "ENTER", "ESC": "ESCAPE", "PGUP": "PAGEUP", "PGDN": "PAGEDOWN"}
	mods := map[string]bool{}
	for _, p := range parts[:len(parts)-1] {
		if a := aliases[p]; a != "" {
			p = a
		}
		if (p != "CMD" && p != "CTRL" && p != "ALT" && p != "SHIFT") || mods[p] {
			return "", errors.New("修饰键无效或重复")
		}
		mods[p] = true
	}
	key := parts[len(parts)-1]
	if a := aliases[key]; a != "" {
		key = a
	}
	valid := len(key) == 1 && strings.Contains("ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-=[];',./`\\", key)
	for _, k := range []string{"ENTER", "TAB", "ESCAPE", "BACKSPACE", "DELETE", "UP", "DOWN", "LEFT", "RIGHT", "SPACE", "HOME", "END", "PAGEUP", "PAGEDOWN", "F1", "F2", "F3", "F4", "F5", "F6", "F7", "F8", "F9", "F10", "F11", "F12", "F13", "F14", "F15", "F16", "F17", "F18", "F19", "F20"} {
		valid = valid || key == k
	}
	if !valid {
		return "", errors.New("不支持的主键；使用 CMD+SHIFT+Z、ALT+LEFT 等格式")
	}
	ordered := []string{}
	for _, m := range []string{"CMD", "CTRL", "ALT", "SHIFT"} {
		if mods[m] {
			ordered = append(ordered, m)
		}
	}
	return strings.Join(append(ordered, key), "+"), nil
}

func computerPointerTool(name string) bool {
	return name == "computer_click" || name == "computer_drag" || name == "computer_scroll"
}

func computerActionArguments(tool string, args map[string]any) (map[string]any, error) {
	out := map[string]any{}
	for k, v := range args {
		out[k] = v
	}
	if tool == "computer_key" {
		key, _ := args["key"].(string)
		normalized, err := normalizeComputerKey(key)
		out["key"] = normalized
		return out, err
	}
	if !computerPointerTool(tool) {
		return out, nil
	}
	names := []string{"x", "y"}
	if tool == "computer_drag" {
		names = append(names, "toX", "toY")
	}
	for _, k := range names {
		v, ok := uiNumber(args[k])
		if !ok || v != math.Trunc(v) || math.Abs(v) > 1_000_000 {
			return nil, fmt.Errorf("%s 必须是有效整数坐标", k)
		}
		out[k] = int(v)
	}
	if tool == "computer_drag" {
		d := float64(900)
		if args["durationMs"] != nil {
			var ok bool
			d, ok = uiNumber(args["durationMs"])
			if !ok {
				return nil, errors.New("durationMs 无效")
			}
		}
		if d < 100 || d > 2500 || d != math.Trunc(d) {
			return nil, errors.New("拖拽时长须为100至2500毫秒")
		}
		out["durationMs"] = int(d)
	}
	if tool == "computer_scroll" {
		nonzero := false
		for _, k := range []string{"deltaX", "deltaY"} {
			v := float64(0)
			if args[k] != nil {
				var ok bool
				v, ok = uiNumber(args[k])
				if !ok {
					return nil, errors.New("滚动量无效")
				}
			}
			if math.Abs(v) > 2000 || v != math.Trunc(v) {
				return nil, errors.New("每轴滚动量须为-2000至2000整数像素")
			}
			out[k] = int(v)
			nonzero = nonzero || v != 0
		}
		if !nonzero {
			return nil, errors.New("至少一个滚动轴必须非零")
		}
	}
	return out, nil
}

func (a *App) rememberComputerSnapshot(task *Task, target computerControlTarget, raw any) {
	m, _ := raw.(map[string]any)
	source, _ := m["source"].(map[string]any)
	id, _ := source["screenshotId"].(string)
	window, _ := source["window"].(map[string]any)
	wid, ok := uiNumber(source["windowId"])
	pw, p := uiNumber(source["pixelWidth"])
	ph, q := uiNumber(source["pixelHeight"])
	if id == "" || window == nil || !ok || !p || !q || pw <= 0 || ph <= 0 {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if task.computerSnapshots == nil || len(task.computerSnapshots) >= 8 {
		task.computerSnapshots = map[string]computerSnapshotReference{}
	}
	task.computerSnapshots[target.Rule] = computerSnapshotReference{ID: id, Target: target, Window: window, WindowID: wid, PixelWidth: pw, PixelHeight: ph, At: time.Now()}
}

func (a *App) resolveComputerCoordinates(ctx context.Context, task *Task, tool string, args map[string]any, target computerControlTarget) (context.Context, map[string]any, error) {
	out, err := computerActionArguments(tool, args)
	if err != nil {
		return ctx, nil, err
	}
	if !computerPointerTool(tool) {
		return ctx, out, nil
	}
	space, _ := out["coordinateSpace"].(string)
	if space == "" || space == "desktop" {
		return ctx, out, nil
	}
	if space != "image" {
		return ctx, nil, errors.New("coordinateSpace 须为 desktop 或 image")
	}
	a.mu.Lock()
	ref, ok := task.computerSnapshots[target.Rule]
	a.mu.Unlock()
	if !ok || out["screenshotId"] != ref.ID || ref.Target != target || time.Since(ref.At) > 2*time.Minute {
		return ctx, nil, errors.New("截图引用缺失、过期或不属于本会话目标；请重新 computer_snapshot")
	}
	x, a1 := uiNumber(ref.Window["x"])
	y, b := uiNumber(ref.Window["y"])
	w, c := uiNumber(ref.Window["width"])
	h, d := uiNumber(ref.Window["height"])
	if !a1 || !b || !c || !d || w <= 0 || h <= 0 {
		return ctx, nil, errors.New("截图缺少有效窗口映射")
	}
	for _, pair := range [][2]string{{"x", "y"}, {"toX", "toY"}} {
		if pair[0] == "toX" && tool != "computer_drag" {
			continue
		}
		px, _ := uiNumber(out[pair[0]])
		py, _ := uiNumber(out[pair[1]])
		if px < 0 || py < 0 || px >= ref.PixelWidth || py >= ref.PixelHeight {
			return ctx, nil, errors.New("图像坐标超出引用截图")
		}
		out[pair[0]] = int(math.Floor(x + px*w/ref.PixelWidth))
		out[pair[1]] = int(math.Floor(y + py*h/ref.PixelHeight))
	}
	ref.ImagePoints = map[string]any{"x": args["x"], "y": args["y"]}
	if tool == "computer_drag" {
		ref.ImagePoints["toX"] = args["toX"]
		ref.ImagePoints["toY"] = args["toY"]
	}
	out["coordinateSpace"] = "desktop"
	return context.WithValue(ctx, computerActionReferenceKey{}, ref), out, nil
}
