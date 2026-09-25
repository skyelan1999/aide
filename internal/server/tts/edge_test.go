package tts

// #42 edge-tts 超时分层 / 重试 / 健康探测单测。
// 非联网测试用 channelConn 模拟慢响应（建连即返回、握手不答、握手后不送音频），
// 不依赖真实网络；联网 MD5 区分度测试由 RUN_EDGE_LIVE=1 触发。

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"os"
	"sync"
	"testing"
	"time"
)

// ── 可控超时的内存 conn ──────────────────────────────────────────────────────

type timeoutErr struct{}

func (timeoutErr) Error() string { return "i/o timeout" }
func (timeoutErr) Timeout() bool { return true }
func (timeoutErr) Temporary() bool { return true }

// chanConn 是一对内存 conn：SetDeadline 到期后 Read 返回 timeoutErr（模拟真实 TCP 读超时）。
type chanConn struct {
	out      chan<- []byte
	in       <-chan []byte
	done     chan struct{}
	once     sync.Once
	mu       sync.Mutex
	deadline time.Time
}

func chanPair() (net.Conn, net.Conn) {
	c2s := make(chan []byte, 16)
	s2c := make(chan []byte, 16)
	c := &chanConn{out: c2s, in: s2c, done: make(chan struct{})}
	s := &chanConn{out: s2c, in: c2s, done: make(chan struct{})}
	return c, s
}

func (c *chanConn) Read(b []byte) (int, error) {
	c.mu.Lock()
	dl := c.deadline
	c.mu.Unlock()
	var tch <-chan time.Time
	if !dl.IsZero() {
		d := time.Until(dl)
		if d <= 0 {
			return 0, timeoutErr{}
		}
		tch = time.After(d)
	}
	select {
	case data, ok := <-c.in:
		if !ok {
			return 0, io.EOF
		}
		n := copy(b, data)
		return n, nil
	case <-tch:
		return 0, timeoutErr{}
	case <-c.done:
		return 0, io.ErrClosedPipe
	}
}

func (c *chanConn) Write(b []byte) (int, error) {
	cp := make([]byte, len(b))
	copy(cp, b)
	select {
	case c.out <- cp:
		return len(cp), nil
	case <-c.done:
		return 0, io.ErrClosedPipe
	}
}

func (c *chanConn) Close() error { c.once.Do(func() { close(c.done) }); return nil }

func (c *chanConn) LocalAddr() net.Addr  { return nil }
func (c *chanConn) RemoteAddr() net.Addr { return nil }

func (c *chanConn) SetDeadline(t time.Time) error {
	c.mu.Lock()
	c.deadline = t
	c.mu.Unlock()
	return nil
}
func (c *chanConn) SetReadDeadline(t time.Time) error  { return c.SetDeadline(t) }
func (c *chanConn) SetWriteDeadline(t time.Time) error { return c.SetDeadline(t) }

// ── 测试辅助 ──────────────────────────────────────────────────────────────

func shortTimeouts(t *testing.T) {
	t.Helper()
	od, oh, of := dialTimeout, handshakeTimeout, firstByteTimeout
	dialTimeout, handshakeTimeout, firstByteTimeout = 60*time.Millisecond, 60*time.Millisecond, 60*time.Millisecond
	t.Cleanup(func() { dialTimeout, handshakeTimeout, firstByteTimeout = od, oh, of })
}

func saveEdgeDial(t *testing.T) func() {
	t.Helper()
	orig := edgeDial
	return func() { edgeDial = orig }
}

// handshake101 是 edgeHandshake 期望的合法升级响应。
const handshake101 = "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n"

// ── 超时分类 ──────────────────────────────────────────────────────────────

func TestDialTimeoutClassification(t *testing.T) {
	shortTimeouts(t)
	restore := saveEdgeDial(t)
	defer restore()
	edgeDial = func(ctx context.Context, _, _, _ string) (net.Conn, error) {
		<-ctx.Done() // 建连一直挂起，直到 dial ctx 超时
		return nil, ctx.Err()
	}
	p := newEdge(Config{})
	_, err := p.Synth(context.Background(), "你好", SynthOpts{Voice: "zh-CN-XiaoxiaoNeural"})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("want ErrUnavailable, got %v", err)
	}
	if !errors.Is(err, ErrEdgeDialTimeout) {
		t.Fatalf("want wrapped ErrEdgeDialTimeout, got %v", err)
	}
}

func TestHandshakeTimeoutClassification(t *testing.T) {
	shortTimeouts(t)
	restore := saveEdgeDial(t)
	defer restore()
	edgeDial = func(_ context.Context, _, _, _ string) (net.Conn, error) {
		cli, srv := chanPair()
		go io.Copy(io.Discard, srv) // 读掉客户端握手请求，但永不返回 101
		return cli, nil
	}
	p := newEdge(Config{})
	_, err := p.Synth(context.Background(), "你好", SynthOpts{Voice: "zh-CN-XiaoxiaoNeural"})
	if !errors.Is(err, ErrEdgeHandshakeTimeout) {
		t.Fatalf("want ErrEdgeHandshakeTimeout, got %v", err)
	}
}

func TestFirstByteTimeoutClassification(t *testing.T) {
	shortTimeouts(t)
	restore := saveEdgeDial(t)
	defer restore()
	edgeDial = func(_ context.Context, _, _, _ string) (net.Conn, error) {
		cli, srv := chanPair()
		go func() {
			buf := make([]byte, 8192)
			srv.Read(buf) // 消费客户端握手请求
			srv.Write([]byte(handshake101)) // 握手成功
			// 之后永不发送音频帧 → 首包超时
			select {}
		}()
		return cli, nil
	}
	p := newEdge(Config{})
	_, err := p.Synth(context.Background(), "你好", SynthOpts{Voice: "zh-CN-XiaoxiaoNeural"})
	if !errors.Is(err, ErrEdgeFirstByteTimeout) {
		t.Fatalf("want ErrEdgeFirstByteTimeout, got %v", err)
	}
}

// TestRetryOnTimeout 慢首包 → 自动重试 1 次 → 仍失败才返回 ErrUnavailable。
func TestRetryOnTimeout(t *testing.T) {
	shortTimeouts(t)
	restore := saveEdgeDial(t)
	defer restore()
	attempts := 0
	edgeDial = func(_ context.Context, _, _, _ string) (net.Conn, error) {
		attempts++
		cli, srv := chanPair()
		go func() {
			buf := make([]byte, 8192)
			srv.Read(buf)
			srv.Write([]byte(handshake101))
			select {}
		}()
		return cli, nil
	}
	p := newEdge(Config{})
	_, err := p.Synth(context.Background(), "你好", SynthOpts{Voice: "zh-CN-XiaoxiaoNeural"})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("want ErrUnavailable after retries, got %v", err)
	}
	if attempts != edgeMaxRetries+1 {
		t.Fatalf("want %d dial attempts (initial+%d retries), got %d", edgeMaxRetries+1, edgeMaxRetries, attempts)
	}
}

// TestNonTimeoutNoRetry 非超时错误（模拟握手返回非 101）不重试、不包 ErrUnavailable。
func TestNonTimeoutNoRetry(t *testing.T) {
	shortTimeouts(t)
	restore := saveEdgeDial(t)
	defer restore()
	attempts := 0
	edgeDial = func(_ context.Context, _, _, _ string) (net.Conn, error) {
		attempts++
		cli, srv := chanPair()
		go func() {
			buf := make([]byte, 8192)
			srv.Read(buf)
			srv.Write([]byte("HTTP/1.1 403 Forbidden\r\n\r\n")) // 认证失败类错误
		}()
		return cli, nil
	}
	p := newEdge(Config{})
	_, err := p.Synth(context.Background(), "你好", SynthOpts{Voice: "zh-CN-XiaoxiaoNeural"})
	if errors.Is(err, ErrUnavailable) {
		t.Fatalf("non-timeout error must not be wrapped as ErrUnavailable: %v", err)
	}
	if attempts != 1 {
		t.Fatalf("non-timeout must not retry, attempts=%d", attempts)
	}
}

// ── 联网：4 音色同文本音频两两不同（#42 回归"怎么换都一样"）────────────────

func TestVoiceDistinctMD5(t *testing.T) {
	if os.Getenv("RUN_EDGE_LIVE") != "1" {
		t.Skip("set RUN_EDGE_LIVE=1 to hit live edge-tts")
	}
	voices := []string{
		"zh-CN-XiaoxiaoNeural",
		"zh-CN-XiaoyiNeural",
		"zh-CN-YunxiNeural",
		"zh-CN-YunjianNeural",
	}
	sums := map[string]string{}
	for _, v := range voices {
		p := newEdge(Config{Voice: v})
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		rc, err := p.Synth(ctx, "今天天气不错，我们去公园散步吧，记得带上水杯。", SynthOpts{Voice: v, Rate: 1.0})
		if err != nil {
			cancel()
			t.Fatalf("synth %s: %v", v, err)
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		cancel()
		if err != nil {
			t.Fatalf("read %s: %v", v, err)
		}
		if len(b) < 500 {
			t.Fatalf("%s audio too short: %d bytes", v, len(b))
		}
		sum := md5.Sum(b)
		sums[v] = hex.EncodeToString(sum[:])
		time.Sleep(500 * time.Millisecond) // edge-tts 快速重连偶发断流，间隔避免限流
	}
	for i := range voices {
		for j := i + 1; j < len(voices); j++ {
			a, b := voices[i], voices[j]
			if sums[a] == sums[b] {
				t.Errorf("音色音频相同：%s 与 %s md5 都是 %s", a, b, sums[a])
			}
		}
	}
	t.Logf("md5: %v", sums)
}
