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
	// edgeMaxRetries 连接/握手/首包超时后用新 ConnectionId 自动重试次数（仍失败才交上层降级）。
	edgeMaxRetries = 1
)

// 超时分层（#42）：区分连接 / 握手 / 首包三类，避免首包稍慢即误判 edge 不可用而降级浏览器机械音。
// 用 var 便于测试注入短超时。生产值：
//   dialTimeout      TCP/TLS 建连 5s
//   handshakeTimeout WSS 升级握手 5s（含本地 Sec-MS-GEC 生成，不含音频）
//   firstByteTimeout 第一个音频字节 5s（WSS 握手 + GEC + 首次连接常超 1.5s，放宽到 5s）
var (
	dialTimeout      = 5 * time.Second
	handshakeTimeout = 5 * time.Second
	firstByteTimeout = 5000 * time.Millisecond
)

// 超时分类错误：供上层区分"瞬时抖动可重试"与"认证失败/配置错误不重试"。
var (
	ErrEdgeDialTimeout      = errors.New("edge-tts 连接超时")
	ErrEdgeHandshakeTimeout = errors.New("edge-tts 握手超时")
	ErrEdgeFirstByteTimeout = errors.New("edge-tts 首包超时")
)

// ErrEdgeClosed 服务端在返回音频前主动关闭 WSS（带 close code+reason）。
// 与超时不同：这通常是微软风控/拒绝（如 1008 policy violation、1011 server error），
// 重试无意义，应直接标记 edge 不可用并切下一引擎/降级浏览器。
type ErrEdgeClosed struct {
	Code   int    // WebSocket close code（RFC6455 §7.4）
	Reason string // 服务端给出的关闭原因文本
}

func (e *ErrEdgeClosed) Error() string {
	return fmt.Sprintf("edge-tts 服务端关闭连接(code=%d, %s)", e.Code, e.Reason)
}

// isEdgeClosed 判断是否为服务端主动断连（含裸 EOF，即对端未发 close 帧直接断）。
func isEdgeClosed(err error) bool {
	var c *ErrEdgeClosed
	if errors.As(err, &c) {
		return true
	}
	return errors.Is(err, io.EOF)
}

// edgeDial 是建连钩子：生产用标准库 TLS Dialer；测试可替换为 net.Pipe 制造慢响应。
var edgeDial = func(ctx context.Context, network, addr, serverName string) (net.Conn, error) {
	d := &tls.Dialer{Config: &tls.Config{ServerName: serverName}}
	return d.DialContext(ctx, network, addr)
}

// isDeadlineErr 判断是否超时类错误（net.Error.Timeout 或 ctx 超时）。
func isDeadlineErr(err error) bool {
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}
	return errors.Is(err, context.DeadlineExceeded)
}

// isEdgeTimeout 判断是否属于可重试的超时类错误（连接/握手/首包）。
func isEdgeTimeout(err error) bool {
	return errors.Is(err, ErrEdgeDialTimeout) ||
		errors.Is(err, ErrEdgeHandshakeTimeout) ||
		errors.Is(err, ErrEdgeFirstByteTimeout)
}

// edge 是 edge-tts Provider。无状态：每次 Synth 新建一条 WSS 连接。
type edge struct {
	cfg Config
}

func newEdge(cfg Config) *edge { return &edge{cfg: cfg} }

func (e *edge) Name() string    { return "edge" }
func (e *edge) Format() string   { return "mp3" }
func (e *edge) Available() bool  { return true } // 联网可用性在 Synth 时探测

// Synth 建立一条 edge-tts 会话并把 MP3 字节流作为 ReadCloser 返回。
// 读取端 Close 会中断连接。连接/握手/首包超时自动用新 ConnectionId 重试 edgeMaxRetries 次；
// 非超时错误（如 403 认证失败）不重试，直接返回。
func (e *edge) Synth(ctx context.Context, text string, opts SynthOpts) (io.ReadCloser, error) {
	var lastErr error
	for attempt := 0; attempt <= edgeMaxRetries; attempt++ {
		first, rest, err := e.synthOnce(ctx, text, opts)
		if err == nil {
			return &prependReader{first: first, rest: rest}, nil
		}
		lastErr = err
		// 仅超时类错误重试；服务端断连(风控)、403 等不重试，立即返回。
		if !isEdgeTimeout(err) || attempt == edgeMaxRetries {
			break
		}
	}
	// 超时(重试耗尽)或服务端主动断连 → 包装为 ErrUnavailable，切下一引擎/降级。
	if isEdgeTimeout(lastErr) || isEdgeClosed(lastErr) {
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, lastErr)
	}
	return nil, lastErr
}

// synthOnce 完成一次完整尝试：建连→握手→发 speech.config/ssml→阻塞读到第一个音频字节。
// 成功时返回第一个音频片段 first 与后续字节流 rest；首包未在 firstByteTimeout 内到达返回 ErrEdgeFirstByteTimeout。
func (e *edge) synthOnce(ctx context.Context, text string, opts SynthOpts) (first []byte, rest io.ReadCloser, err error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, nil, errors.New("合成文本为空")
	}
	endpoint := strings.TrimSpace(e.cfg.Endpoint)
	if endpoint == "" {
		endpoint = edgeDefaultEndpoint
	}
	u, err := url.Parse(endpoint)
	if err != nil || (u.Scheme != "wss" && u.Scheme != "https") {
		return nil, nil, fmt.Errorf("非法 TTS 端点：%s", endpoint)
	}
	if u.Scheme == "https" {
		u.Scheme = "wss"
	}
	q := u.Query()
	q.Set("TrustedClientToken", edgeClientToken)
	q.Set("ConnectionId", randHex(16)) // 每次尝试用新 ConnectionId
	q.Set("Sec-MS-GEC", secMsgGEC())
	q.Set("Sec-MS-GEC-Version", edgeGECVersion)
	u.RawQuery = q.Encode()

	// 1) TCP/TLS 建连（带独立超时）
	dialCtx, dialCancel := context.WithTimeout(ctx, dialTimeout)
	conn, derr := edgeDial(dialCtx, "tcp", net.JoinHostPort(u.Hostname(), portOf(u)), u.Hostname())
	dialCancel()
	if derr != nil {
		if isDeadlineErr(derr) || errors.Is(derr, context.DeadlineExceeded) {
			return nil, nil, ErrEdgeDialTimeout
		}
		return nil, nil, fmt.Errorf("edge-tts 连接失败：%w", derr)
	}

	// 2) WSS 握手（带独立超时，覆盖写请求+读 101 响应）
	_ = conn.SetDeadline(time.Now().Add(handshakeTimeout))
	ws, herr := edgeHandshake(conn, u, "muid="+strings.ToUpper(randHex(16))+";")
	_ = conn.SetDeadline(time.Time{})
	if herr != nil {
		conn.Close()
		if isDeadlineErr(herr) {
			return nil, nil, ErrEdgeHandshakeTimeout
		}
		return nil, nil, herr
	}

	voice := ResolveVoice(opts.Voice, opts.Gender)
	if voice == "" {
		voice = ResolveVoice(e.cfg.Voice, e.cfg.Gender)
	}
	ssml := buildSSML(text, voice, opts)

	// 3) speech.config
	configBody := `{"context":{"synthesis":{"audio":{"metadataoptions":{"sentenceBoundaryEnabled":"false","wordBoundaryEnabled":"false","sessionEndWaitTimeout":"300ms"},"outputFormat":"audio-24khz-48kbitrate-mono-mp3"}}}}`
	if err := ws.writeText("Path:speech.config\r\nContent-Type:application/json; charset=utf-8\r\n\r\n" + configBody); err != nil {
		ws.Close()
		return nil, nil, err
	}
	// 4) ssml
	ssmlMsg := fmt.Sprintf("X-RequestId:%s\r\nContent-Type:application/ssml+xml\r\nX-Timestamp:%s GMT\r\nPath:ssml\r\n\r\n%s",
		randHex(16), time.Now().Format("02 Jan 2006 15:04:05"), ssml)
	if err := ws.writeText(ssmlMsg); err != nil {
		ws.Close()
		return nil, nil, err
	}

	// 5) 同步读到第一个音频字节（首包超时在此判定）
	_ = ws.setReadDeadline(time.Now().Add(firstByteTimeout))
	for {
		if err := ctx.Err(); err != nil {
			ws.Close()
			return nil, nil, err
		}
		opcode, payload, rerr := ws.readMessage()
		if rerr != nil {
			ws.Close()
			var closed *ErrEdgeClosed
			if errors.As(rerr, &closed) {
				return nil, nil, closed // 服务端风控/拒绝，带 code+reason
			}
			if errors.Is(rerr, io.EOF) {
				return nil, nil, &ErrEdgeClosed{Code: 0, Reason: "对端裸关闭(无 close 帧)"}
			}
			if isDeadlineErr(rerr) {
				return nil, nil, ErrEdgeFirstByteTimeout
			}
			return nil, nil, rerr
		}
		switch opcode {
		case opText:
			if strings.Contains(string(payload), "Path:turn.end") {
				ws.Close()
				return nil, nil, fmt.Errorf("edge-tts 未返回音频")
			}
			// turn.start / response 等元数据帧：继续等音频
		case opBinary:
			audio := extractAudio(payload)
			if len(audio) == 0 {
				continue
			}
			// 首包到达：取消读超时，后续帧交给 goroutine 流式写入 pipe。
			_ = ws.setReadDeadline(time.Time{})
			pr, pw := io.Pipe()
			go func() {
				err := e.pumpRest(ctx, ws, pw)
				pw.CloseWithError(err)
			}()
			return audio, pr, nil
		}
	}
}

// pumpRest 首包到达后继续读取 WSS 帧，把 MP3 数据写入 pipe；turn.end 后正常关闭。
func (e *edge) pumpRest(ctx context.Context, ws *wsConn, pw *io.PipeWriter) error {
	defer ws.Close()
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
		case opBinary:
			audio := extractAudio(payload)
			if len(audio) == 0 {
				continue
			}
			if _, err := pw.Write(audio); err != nil {
				return err
			}
		}
	}
}

// prependReader 先吐出 first（首包音频），再透传 rest（后续流），Close 时关闭 rest。
type prependReader struct {
	first []byte
	rest  io.ReadCloser
	off   int
}

func (p *prependReader) Read(b []byte) (int, error) {
	if p.off < len(p.first) {
		n := copy(b, p.first[p.off:])
		p.off += n
		return n, nil
	}
	return p.rest.Read(b)
}

func (p *prependReader) Close() error { return p.rest.Close() }

// ProbeEdge 轻量探测 edge-tts 可用性：合成一句固定短文本。成功返回 nil，失败返回错误（含超时分类）。
// 供 server 层做健康探测/预热；不抛出 ErrUnavailable，由调用方据错误类型决定状态。
func ProbeEdge(ctx context.Context, cfg Config) error {
	p := newEdge(cfg)
	voice := ResolveVoice(cfg.Voice, cfg.Gender)
	rc, err := p.Synth(ctx, "你好", SynthOpts{Voice: voice, Rate: 1.0})
	if err != nil {
		return err
	}
	defer rc.Close()
	_, err = io.Copy(io.Discard, rc)
	return err
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
			// 解析 close 帧：前 2 字节大端 uint16 = close code，其后为 UTF-8 reason。
			code := 1000
			reason := ""
			if len(payload) >= 2 {
				code = int(binary.BigEndian.Uint16(payload[:2]))
				reason = strings.TrimSpace(string(payload[2:]))
			}
			_ = w.writeFrame(opClose, payload) // 回 close 帧
			return 0, nil, &ErrEdgeClosed{Code: code, Reason: reason}
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
