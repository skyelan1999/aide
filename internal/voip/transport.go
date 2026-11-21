package voip

import (
	"errors"
	"net/netip"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const PCMFrameBytes = 320 // 20 ms of little-endian mono PCM16 at 8 kHz.

type Target struct {
	IP   string `json:"ip"`
	Port uint16 `json:"port"`
	User string `json:"user"`
}

var sipUser = regexp.MustCompile(`^[A-Za-z0-9_.+-]{0,64}$`)

func (t *Target) Normalize() error {
	ip, err := netip.ParseAddr(strings.TrimSpace(t.IP))
	if err != nil || !ip.Is4() || !(ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast()) {
		return errors.New("请输入局域网 IPv4 地址")
	}
	t.IP = ip.String()
	t.User = strings.TrimSpace(t.User)
	if !sipUser.MatchString(t.User) {
		return errors.New("SIP 用户只能使用字母、数字和 . _ + -")
	}
	if t.Port == 0 {
		t.Port = 5060
	}
	return nil
}

type Event struct {
	Op     string  `json:"op"`
	Call   string  `json:"call,omitempty"`
	Target *Target `json:"target,omitempty"`
	Code   int     `json:"code,omitempty"`
	PCM    []byte  `json:"pcm,omitempty"`
	Error  string  `json:"error,omitempty"`
}

// Socket gives each end a single reader and serializes bounded writes.
type Socket struct {
	*websocket.Conn
	mu sync.Mutex
}

func (s *Socket) Send(event Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.SetWriteDeadline(time.Now().Add(2 * time.Second)); err != nil {
		return err
	}
	return s.WriteJSON(event)
}

func (s *Socket) ReadUntilClosed() <-chan Event {
	out := make(chan Event)
	s.SetReadLimit(3072)
	_ = s.SetReadDeadline(time.Now().Add(15 * time.Second))
	s.SetPongHandler(func(string) error { return s.SetReadDeadline(time.Now().Add(15 * time.Second)) })
	done := make(chan struct{})
	go func() {
		defer close(out)
		defer close(done)
		for {
			var event Event
			if s.ReadJSON(&event) != nil {
				return
			}
			// A blocked consumer must not prevent detecting disconnects.
			select {
			case out <- event:
			case <-time.After(2 * time.Second):
				return
			}
		}
	}()
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if s.WriteControl(websocket.PingMessage, nil, time.Now().Add(2*time.Second)) != nil {
					_ = s.Close()
					return
				}
			}
		}
	}()
	return out
}
