package tts

import (
	"context"
	"io"
	"os"
	"testing"
	"time"
)

// TestEdgeLiveDial 连真 edge-tts 服务合成一小段，验证手写 WebSocket 客户端能拿到 MP3。
// 仅在 RUN_EDGE_LIVE=1 时执行（需要联网）。
func TestEdgeLiveDial(t *testing.T) {
	if os.Getenv("RUN_EDGE_LIVE") != "1" {
		t.Skip("set RUN_EDGE_LIVE=1 to hit live edge-tts")
	}
	p := newEdge(Config{Voice: "zh-CN-XiaoxiaoNeural", Gender: "female"})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rc, err := p.Synth(ctx, "你好，我是小秘，这是来自 aide 后端的合成测试。", SynthOpts{Rate: 1.0})
	if err != nil {
		t.Fatalf("Synth: %v", err)
	}
	defer rc.Close()
	b, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(b) < 500 {
		t.Fatalf("audio too short: %d bytes", len(b))
	}
	// MP3 帧同步字 0xFFFB / 0xFFF3 / ID3 头 "ID3"
	if len(b) < 3 || (b[0] != 0xFF && string(b[:3]) != "ID3") {
		t.Fatalf("not mp3: head=% x", b[:8])
	}
	t.Logf("got %d bytes mp3, head=% x", len(b), b[:8])
}
