package server

import "testing"

func TestColloquialCacheKeyFramesModelAndText(t *testing.T) {
	text := "项目进度正常。"
	brief := colloquialKey("example|brief", text)
	full := colloquialKey("example|full", text)
	if len(brief) != 64 || brief == full || brief != colloquialKey("example|brief", text) {
		t.Fatal("cache keys must be stable SHA-256 digests and distinguish voice modes")
	}
	if colloquialKey("one\x00two", "three") == colloquialKey("one", "two\x00three") {
		t.Fatal("cache keys must distinguish the model and text boundary")
	}
}
