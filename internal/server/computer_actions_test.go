package server

import (
	"context"
	"image"
	"image/color"
	"testing"
	"time"
)

func TestComputerImageCoordinatesAreTaskAndWindowBound(t *testing.T) {
	a := &App{}
	task := &Task{}
	target := computerControlTarget{Name: "Fixture", Rule: "bundle:fixture", PID: 123}
	raw := snapshotFixture(t, image.NewGray(image.Rect(0, 0, 2560, 1500)), map[string]any{"x": -1470, "y": 33, "width": 1470, "height": 870})
	a.rememberComputerSnapshot(task, target, raw)
	id := raw["source"].(map[string]any)["screenshotId"]
	args := map[string]any{"coordinateSpace": "image", "screenshotId": id, "x": 1280, "y": 750, "toX": 2000, "toY": 1000}
	ctx, out, err := a.resolveComputerCoordinates(context.Background(), task, "computer_drag", args, target)
	if err != nil || out["x"] != -735 || out["y"] != 468 || out["toX"] != -322 || out["toY"] != 613 {
		t.Fatalf("mapped=%v err=%v", out, err)
	}
	if ctx.Value(computerActionReferenceKey{}).(computerSnapshotReference).WindowID != 42 {
		t.Fatal("window identity lost")
	}
	if ctx.Value(computerActionReferenceKey{}).(computerSnapshotReference).ImagePoints["x"] != 1280 || args["x"] != 1280 || out["coordinateSpace"] != "desktop" {
		t.Fatal("original image coordinates were lost or actor parameters were mutated")
	}
	for _, tc := range []struct {
		task   *Task
		target computerControlTarget
	}{{&Task{}, target}, {task, computerControlTarget{Name: "Fixture", Rule: "bundle:fixture", PID: 124}}} {
		if _, _, err := a.resolveComputerCoordinates(context.Background(), tc.task, "computer_drag", args, tc.target); err == nil {
			t.Fatal("foreign task or restarted process accepted")
		}
	}
	for _, bad := range []any{-1, 2560, 1.5} {
		copy := map[string]any{}
		for k, v := range args {
			copy[k] = v
		}
		copy["x"] = bad
		if _, _, err := a.resolveComputerCoordinates(context.Background(), task, "computer_drag", copy, target); err == nil {
			t.Fatalf("bad coordinate accepted: %v", bad)
		}
	}
	ref := task.computerSnapshots[target.Rule]
	ref.At = time.Now().Add(-3 * time.Minute)
	task.computerSnapshots[target.Rule] = ref
	if _, _, err := a.resolveComputerCoordinates(context.Background(), task, "computer_drag", args, target); err == nil {
		t.Fatal("expired screenshot accepted")
	}
}

func TestComputerGestureBudgetsAndKeyGrammar(t *testing.T) {
	for raw, want := range map[string]string{"Option+Shift+Right": "ALT+SHIFT+RIGHT", "command+shift+z": "CMD+SHIFT+Z", "CTRL+ALT+F12": "CTRL+ALT+F12", "PGDN": "PAGEDOWN", "CMD+L": "CMD+L", "F20": "F20"} {
		got, err := normalizeComputerKey(raw)
		if err != nil || got != want {
			t.Errorf("%q: %q %v", raw, got, err)
		}
	}
	for _, raw := range []string{"", "CMD", "CMD+CMD+A", "CTRL++A", "CTRL+A+B", "CMD+not-a-key", "SHIFT+CTRL+ALT+CMD+X+Y"} {
		if _, err := normalizeComputerKey(raw); err == nil {
			t.Errorf("invalid chord %q", raw)
		}
	}
	for _, args := range []map[string]any{{"x": 0, "y": 0, "toX": 10, "toY": 10, "durationMs": 2501}, {"x": 0, "y": 0, "toX": 10, "toY": 10, "durationMs": 99}, {"x": 0, "y": 0, "toX": 10.5, "toY": 10}} {
		if _, err := computerActionArguments("computer_drag", args); err == nil {
			t.Fatal("invalid drag", args)
		}
	}
	for _, args := range []map[string]any{{"x": 0, "y": 0}, {"x": 0, "y": 0, "deltaY": 2001}, {"x": 0, "y": 0, "deltaX": -2001}, {"x": 0, "y": 0, "deltaY": 1.25}} {
		if _, err := computerActionArguments("computer_scroll", args); err == nil {
			t.Fatal("invalid scroll", args)
		}
	}
	if _, err := computerActionArguments("computer_scroll", map[string]any{"x": -10, "y": -10, "deltaX": -500, "deltaY": 700}); err != nil {
		t.Fatal(err)
	}
}

func TestComputerVisualDragBindsBothEndpointsAndKeepsCropsPrivate(t *testing.T) {
	im := image.NewRGBA(image.Rect(0, 0, 600, 200))
	rect := map[string]any{"x": 0, "y": 0, "width": 600, "height": 200}
	input := &surfaceApprovalInput{Tool: "computer_drag", Target: computerControlTarget{Name: "Fixture", Rule: "bundle:fixture", PID: 123}, Args: map[string]any{"x": 30, "y": 30, "toX": 500, "toY": 30}}
	first := surfaceVisualInput(input, snapshotFixture(t, im, rect))
	if first == nil || len(first.DetailImages) != 2 || !reviewUIInput(first) {
		t.Fatal("drag evidence missing")
	}
	im.Set(500, 30, color.White)
	changed := surfaceVisualInput(input, snapshotFixture(t, im, rect))
	if changed == nil || first.Visual.sameTarget(changed.Visual) {
		t.Fatal("drag endpoint change not detected")
	}
	input.Args["toX"] = 600
	if surfaceVisualInput(input, snapshotFixture(t, im, rect)) != nil {
		t.Fatal("out-of-window drag accepted")
	}
}
