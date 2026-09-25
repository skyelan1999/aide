package tts

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// edge-tts 直连微软 Read-Aloud WebSocket（与 Edge 浏览器大声朗读同一通道），免费、无需 key。
// 协议是反向工程自官方前端：握手后依次发 speech.config 与 ssml，服务端回二进制 MP3 帧。
// 为保持离线优先、不引入 gorilla/websocket 依赖，这里用标准库实现最小 RFC6455 客户端。

const (
	edgeDefaultEndpoint = "wss://speech.platform.bing.com/consumer/speech/synthesize/readaloud/edge/v1"
	edgeClientToken     = "6A5AA1D4EAFF4E9FB37E23D68491D6F4" // 公开可信客户端 token（Edge 扩展内置）
	edgeUserAgent       = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/143.0.0.0 Safari/537.36 Edg/143.0.0.0"
	edgeOrigin          = "chrome-extension://jdiccldimpdaibmpdkjnbmckianbfold"
	edgeGECVersion      = "1-143.0.3650.75"
	// firstByteTimeout 首包（第一个音频字节）超时：超过即认为 edge 不可用，供前端降级 Web Speech。
	firstByteTimeout = 1500 * time.Millisecond
)

// edge 是 edge-tts Provider。无状态：每次 Synth 新建一条 WSS 连接。
type edge struct {
	cfg Config
}

func newEdge(cfg Config) *edge { return &edge{cfg: cfg} }

func (e *edge) Name() string    { return "edge" }
func (e *edge) Format() string   { return "mp3" }
func (e *edge) Available() bool  { return true } // 联网可用性在 Synth 时探测

// Synth 建立一条 edge-tts 会话并把 MP3 字节流作为 ReadCloser 返回。
// 读取端 Close 会中断连接。
func (e *edge) Synth(ctx context.Context, text string, opts SynthOpts) (io.ReadCloser, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, errors.New("合成文本为空")
	}
	endpoint := strings.TrimSpace(e.cfg.Endpoint)
	if endpoint == "" {
		endpoint = edgeDefaultEndpoint
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "wss" && u.Scheme != "https" {
		return nil, fmt.Errorf("非法 TTS 端点：%s", endpoint)
	}
	if u.Scheme == "https" {
		u.Scheme = "wss"
	}
	q := u.Query()
	q.Set("TrustedClientToken", edgeClientToken)
	q.Set("ConnectionId", randHex(16))
	q.Set("Sec-MS-GEC", secMsgGEC())
	q.Set("Sec-MS-GEC-Version", edgeGECVersion)
	u.RawQuery = q.Encode()

	dialer := &tls.Dialer{Config: &tls.Config{ServerName: u.Hostname()}}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(u.Hostname(), portOf(u)))
	if err != nil {
		return nil, fmt.Errorf("edge-tts 连接失败：%w", err)
	}
	ws, err := edgeHandshake(conn, u, "muid="+strings.ToUpper(randHex(16))+";")
	if err != nil {
		conn.Close()
		return nil, err
	}

	voice := ResolveVoice(opts.Voice, opts.Gender)
	if voice == "" {
		voice = ResolveVoice(e.cfg.Voice, e.cfg.Gender)
	}
	ssml := buildSSML(text, voice, opts)

	// 1) speech.config
	configBody := `{"context":{"synthesis":{"audio":{"metadataoptions":{"sentenceBoundaryEnabled":"false","wordBoundaryEnabled":"false","sessionEndWaitTimeout":"300ms"},"outputFormat":"audio-24khz-48kbitrate-mono-mp3"}}}}`
	if err := ws.writeText("Path:speech.config\r\nContent-Type:application/json; charset=utf-8\r\n\r\n" + configBody); err != nil {
		ws.Close()
		return nil, err
	}
	// 2) ssml
	ssmlMsg := fmt.Sprintf("X-RequestId:%s\r\nContent-Type:application/ssml+xml\r\nX-Timestamp:%s GMT\r\nPath:ssml\r\n\r\n%s",
		randHex(16), time.Now().Format("02 Jan 2006 15:04:05"), ssml)
	if err := ws.writeText(ssmlMsg); err != nil {
		ws.Close()
		return nil, err
	}

	pr, pw := io.Pipe()
	go func() {
		err := e.pump(ctx, ws, pw)
		pw.CloseWithError(err)
	}()
	return pr, nil
}

// pump 读取 WSS 帧，把 MP3 数据写入 pipe；turn.end 后正常关闭。
func (e *edge) pump(ctx context.Context, ws *wsConn, pw *io.PipeWriter) error {
	defer ws.Close()
	// 首包超时：在收到第一个音频字节前用 deadline 兜底；收到后取消。
	_ = ws.setReadDeadline(time.Now().Add(firstByteTimeout))
	gotAudio := false
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		opcode, payload, err := ws.readMessage()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		switch opcode {
		case opText:
			if strings.Contains(string(payload), "Path:turn.end") {
				return nil
			}
			// turn.start / response 等元数据帧：忽略内容
		case opBinary:
			audio := extractAudio(payload)
			if len(audio) == 0 {
				continue
			}
			if !gotAudio {
				gotAudio = true
				_ = ws.setReadDeadline(time.Time{}) // 首包到达，取消超时
			}
			if _, err := pw.Write(audio); err != nil {
				return err
			}
		}
	}
}

// extractAudio 从 edge-tts 二进制帧取 MP3 负载：
// 前 2 字节大端 uint16 = 头部文本（如 "Path:audio\r\n..."）长度；其后为 MP3 字节。
func extractAudio(frame []byte) []byte {
	if len(frame) < 2 {
		return nil
	}
	headerLen := int(binary.BigEndian.Uint16(frame[:2]))
	off := 2 + headerLen
	if off > len(frame) {
		off = 2 // 防御：头部长度异常时按无头部处理
	}
	return frame[off:]
}

// buildSSML 组装 edge-tts SSML；把语速/表现力映射为 prosody/express-as。
func buildSSML(text, voice string, o SynthOpts) string {
	rate := o.Rate
	if rate <= 0 {
		rate = 1.0
	}
	// 1.0 → +0%；0.8 → -20%；1.3 → +30%
	ratePct := int(mathRound((rate - 1.0) * 100))
	rateAttr := fmt.Sprintf("%+d%%", ratePct)

	inner := xmlEscape(text)
	body := fmt.Sprintf("<prosody rate='%s'>%s</prosody>", rateAttr, inner)

	style := strings.TrimSpace(o.Style)
	degree := o.Expressiveness
	if style == "" && degree > 0 {
		style = "chat" // 有表现力要求但未指定风格时默认 chat
	}
	if style != "" {
		if degree <= 0 {
			degree = 0.8 // 选了风格给个基础强度
		}
		if degree > 1 {
			degree = 1
		}
		// styledegree 建议范围 0.5–2.0，把 0..1 线性映射到 0.6..1.8
		sd := 0.6 + degree*1.2
		body = fmt.Sprintf("<mstts:express-as style='%s' styledegree='%.2f'>%s</mstts:express-as>", xmlEscape(style), sd, body)
	}

	return fmt.Sprintf("<speak version='1.0' xmlns='http://www.w3.org/2001/10/synthesis' xmlns:mstts='https://www.w3.org/2001/mstts' xml:lang='zh-CN'><voice name='%s'>%s</voice></speak>",
		xmlEscape(voice), body)
}

func xmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")
	return r.Replace(s)
}

func mathRound(f float64) float64 {
	if f < 0 {
		return float64(int(f - 0.5))
	}
	return float64(int(f + 0.5))
}

func portOf(u *url.URL) string {
	if u.Port() != "" {
		return u.Port()
	}
	return "443"
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ─── 最小 RFC6455 WebSocket 客户端 ──────────────────────────────────────────

const (
	opContinuation = 0x0
	opText         = 0x1
	opBinary       = 0x2
	opClose        = 0x8
	opPing         = 0x9
	opPong         = 0xA
)

type wsConn struct {
	mu   sync.Mutex
	conn net.Conn
	rd   *bufio.Reader
}

// secMsgGEC 生成微软要求的 Sec-MS-GEC 令牌：基于当前时间（Windows 文件时间，5 分钟对齐）
// 与可信 token 的 SHA256。系统时钟偏差超过 5 分钟会导致 403。
func secMsgGEC() string {
	const winEpoch = 11644473600 // 1601-01-01 与 Unix 纪元的秒差
	t := time.Now().Unix() + winEpoch
	t -= t % 300 // 向下对齐到 5 分钟
	ticks := float64(t) * 1e7
	sum := sha256.Sum256([]byte(fmt.Sprintf("%.0f%s", ticks, edgeClientToken)))
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}

// edgeHandshake 完成 WebSocket 升级握手。cookie 形如 "muid=XXXX;"。
func edgeHandshake(conn net.Conn, u *url.URL, cookie string) (*wsConn, error) {
	key := make([]byte, 16)
	_, _ = rand.Read(key)
	encKey := base64Encode(key)
	path := u.RequestURI()
	req := "GET " + path + " HTTP/1.1\r\n" +
		"Host: " + u.Host + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + encKey + "\r\n" +
		"Sec-WebSocket-Version: 13\r\n" +
		"Origin: " + edgeOrigin + "\r\n" +
		"User-Agent: " + edgeUserAgent + "\r\n" +
		"Accept-Language: en-US,en;q=0.9\r\n" +
		"Pragma: no-cache\r\n" +
		"Cache-Control: no-cache\r\n" +
		"Cookie: " + cookie + "\r\n" +
		"\r\n"
	if _, err := conn.Write([]byte(req)); err != nil {
		return nil, err
	}
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, &http.Request{Method: "GET"})
	if err != nil {
		return nil, fmt.Errorf("edge 握手响应读取失败：%w", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 101 {
		return nil, fmt.Errorf("edge 握手失败：HTTP %d", resp.StatusCode)
	}
	return &wsConn{conn: conn, rd: br}, nil
}

func (w *wsConn) setReadDeadline(t time.Time) error {
	return w.conn.SetReadDeadline(t)
}

// writeText 发一条客户端文本帧（必须掩码）。
func (w *wsConn) writeText(s string) error {
	return w.writeFrame(opText, []byte(s))
}

func (w *wsConn) writeFrame(opcode byte, payload []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	var hdr []byte
	b0 := byte(0x80) | opcode // FIN
	l := len(payload)
	mask := make([]byte, 4)
	_, _ = rand.Read(mask)
	switch {
	case l < 126:
		hdr = []byte{b0, 0x80 | byte(l)}
	case l <= 65535:
		hdr = []byte{b0, 0x80 | 126, byte(l >> 8), byte(l)}
	default:
		var ext [8]byte
		binary.BigEndian.PutUint64(ext[:], uint64(l))
		hdr = append([]byte{b0, 0x80 | 127}, ext[:]...)
	}
	masked := make([]byte, l)
	for i, b := range payload {
		masked[i] = b ^ mask[i%4]
	}
	if _, err := w.conn.Write(hdr); err != nil {
		return err
	}
	if _, err := w.conn.Write(mask); err != nil {
		return err
	}
	_, err := w.conn.Write(masked)
	return err
}

// readMessage 读一条完整消息（自动重组分片、应答 ping、忽略 pong）。
func (w *wsConn) readMessage() (byte, []byte, error) {
	var buf []byte
	var opcode byte
	for {
		b0, err := w.rd.ReadByte()
		if err != nil {
			return 0, nil, err
		}
		b1, err := w.rd.ReadByte()
		if err != nil {
			return 0, nil, err
		}
		fin := b0&0x80 != 0
		op := b0 & 0x0f
		masked := b1&0x80 != 0
		l := int(b1 & 0x7f)
		switch l {
		case 126:
			var ext [2]byte
			if _, err := io.ReadFull(w.rd, ext[:]); err != nil {
				return 0, nil, err
			}
			l = int(binary.BigEndian.Uint16(ext[:]))
		case 127:
			var ext [8]byte
			if _, err := io.ReadFull(w.rd, ext[:]); err != nil {
				return 0, nil, err
			}
			l = int(binary.BigEndian.Uint64(ext[:]))
		}
		var mk [4]byte
		if masked {
			if _, err := io.ReadFull(w.rd, mk[:]); err != nil {
				return 0, nil, err
			}
		}
		payload := make([]byte, l)
		if _, err := io.ReadFull(w.rd, payload); err != nil {
			return 0, nil, err
		}
		if masked {
			for i := range payload {
				payload[i] ^= mk[i%4]
			}
		}
		switch op {
		case opPing:
			_ = w.writeFrame(opPong, payload)
			continue
		case opPong:
			continue
		case opClose:
			return 0, nil, io.EOF
		case opContinuation:
			buf = append(buf, payload...)
		default:
			opcode = op
			buf = payload
		}
		if fin {
			return opcode, buf, nil
		}
	}
}

func (w *wsConn) Close() error { return w.conn.Close() }

func base64Encode(b []byte) string {
	const table = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	var out strings.Builder
	for i := 0; i < len(b); i += 3 {
		v := uint32(b[i]) << 16
		n := 1
		if i+1 < len(b) {
			v |= uint32(b[i+1]) << 8
			n = 2
		}
		if i+2 < len(b) {
			v |= uint32(b[i+2])
			n = 3
		}
		out.WriteByte(table[(v>>18)&0x3F])
		out.WriteByte(table[(v>>12)&0x3F])
		if n == 2 {
			out.WriteByte(table[(v>>6)&0x3F])
			out.WriteByte('=')
		} else if n == 3 {
			out.WriteByte(table[(v>>6)&0x3F])
			out.WriteByte(table[v&0x3F])
		} else {
			out.WriteByte('=')
			out.WriteByte('=')
		}
	}
	return out.String()
}
