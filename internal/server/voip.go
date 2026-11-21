package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"aide/internal/voip"
	"github.com/gorilla/websocket"
)

type voipSettings struct {
	Messages  bool        `json:"messages"`
	Assistant bool        `json:"assistant"`
	Target    voip.Target `json:"target"`
}

type voipRuntime struct {
	mu   sync.Mutex
	host *voip.Socket
	call *voipCall
}

type voipCall struct {
	id     string
	events chan voip.Event
	lost   chan struct{}
}

func (a *App) readVoIPSettings() (settings voipSettings, err error) {
	raw, err := os.ReadFile(filepath.Join(a.dataPath, "config", "voip.json"))
	if errors.Is(err, os.ErrNotExist) {
		settings.Target.Port = 5060
		return settings, nil
	}
	if err == nil {
		err = json.Unmarshal(raw, &settings)
	}
	return settings, err
}

func (a *App) voipSettingsAPI(w http.ResponseWriter, r *http.Request) {
	if lanAuthorized(r) {
		if r.Method == http.MethodGet {
			jsonOut(w, 200, struct {
				voipSettings
				Local bool `json:"local"`
			}{})
		} else {
			fail(w, 403, errors.New("请在本机设置话机"))
		}
		return
	}
	if a.readLockState().Locked {
		fail(w, 423, errors.New("请先解锁工作台"))
		return
	}
	a.voip.mu.Lock()
	defer a.voip.mu.Unlock()
	var settings voipSettings
	var err error
	if r.Method == http.MethodPut {
		r.Body = http.MaxBytesReader(w, r.Body, 2048)
		if err = decode(w, r, &settings); err != nil {
			fail(w, 400, err)
			return
		}
		if settings.Target.IP != "" || settings.Messages || settings.Assistant {
			if err = settings.Target.Normalize(); err != nil {
				fail(w, 400, err)
				return
			}
		}
		err = atomicJSON(filepath.Join(a.dataPath, "config", "voip.json"), settings)
	} else {
		settings, err = a.readVoIPSettings()
	}
	if err != nil {
		fail(w, 500, errors.New("无法读取或保存 VoIP 设置"))
		return
	}
	jsonOut(w, 200, struct {
		voipSettings
		Local bool `json:"local"`
		Ready bool `json:"ready"`
	}{settings, true, a.voip.host != nil})
}

func (a *App) voipHostSocket(w http.ResponseWriter, r *http.Request) {
	if lanAuthorized(r) || r.Header.Get("Origin") != "" {
		fail(w, 403, errors.New("此接口仅供本机语音组件使用"))
		return
	}
	u := websocket.Upgrader{}
	conn, err := u.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	host := &voip.Socket{Conn: conn}
	defer host.Close()
	// Replacing an old helper terminates its associated call immediately.
	a.voip.mu.Lock()
	if a.voip.host != nil {
		_ = a.voip.host.Close()
	}
	if call := a.voip.call; call != nil {
		close(call.lost)
		a.voip.call = nil
	}
	a.voip.host = host
	a.voip.mu.Unlock()
	defer func() {
		a.voip.mu.Lock()
		defer a.voip.mu.Unlock()
		if a.voip.host == host {
			a.voip.host = nil
			if call := a.voip.call; call != nil {
				close(call.lost)
				a.voip.call = nil
			}
		}
	}()
	for e := range host.ReadUntilClosed() {
		a.voip.mu.Lock()
		call := a.voip.call
		if a.voip.host == host && call != nil && call.id == e.Call {
			switch e.Op {
			case "audio":
				select {
				case call.events <- e:
				default:
				}
			case "ringing", "answered", "ended":
				select {
				case call.events <- e:
				default:
					close(call.lost)
					a.voip.call = nil
				}
			}
		}
		a.voip.mu.Unlock()
	}
}

func (a *App) voipCallSocket(w http.ResponseWriter, r *http.Request) {
	lock := a.readLockState()
	if lanAuthorized(r) || lock.Locked {
		fail(w, 403, errors.New("请在已解锁的本机使用 VoIP 接管"))
		return
	}
	q := r.URL.Query()
	settings, err := a.readVoIPSettings()
	if err != nil {
		fail(w, 500, errors.New("无法读取 VoIP 设置"))
		return
	}
	lockGen := lock.Gen
	valid := func() bool { return false }
	switch q.Get("mode") {
	case "messages":
		if settings.Messages {
			valid = func() bool {
				a.messageMu.Lock()
				defer a.messageMu.Unlock()
				m := a.messages
				if m == nil || m.service == nil {
					return false
				}
				m.voiceMu.Lock()
				defer m.voiceMu.Unlock()
				if m.voiceOwner != q.Get("owner") || m.voiceCall != q.Get("call") ||
					m.voiceGroup != q.Get("group") || !time.Now().Before(m.voiceExpires) {
					return false
				}
				// Group renewals check a current participant, so a different
				// participant leaving does not terminate the whole handset call.
				return m.voiceGroup != "" ||
					m.service.VoiceSessionAllowed(q.Get("peer"), q.Get("invitation"), q.Get("call"), "")
			}
		}
	case "assistant":
		if settings.Assistant {
			a.mu.Lock()
			workspace := a.wsID()
			a.mu.Unlock()
			valid = func() bool { return a.realtimeCallValid(q.Get("session"), workspace) }
		}
	}
	if !valid() {
		fail(w, 403, errors.New("当前通话未获准使用 VoIP 接管"))
		return
	}
	u := websocket.Upgrader{Subprotocols: []string{"aide-voip"}, CheckOrigin: func(req *http.Request) bool {
		origin, err := url.Parse(req.Header.Get("Origin"))
		return err == nil && origin.Host == req.Host && (origin.Scheme == "http" || origin.Scheme == "https")
	}}
	conn, err := u.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	socket := &voip.Socket{Conn: conn}
	defer socket.Close()
	a.voip.mu.Lock()
	host := a.voip.host
	call := &voipCall{id: newID(), events: make(chan voip.Event, 16), lost: make(chan struct{})}
	if host == nil || a.voip.call != nil {
		a.voip.mu.Unlock()
		message := "本机 VoIP 组件未连接，请用启动器重新启动 Aide"
		if host != nil {
			message = "话机正用于另一通通话"
		}
		_ = socket.Send(voip.Event{Op: "ended", Error: message})
		return
	}
	a.voip.call = call
	a.voip.mu.Unlock()
	defer func() {
		_ = host.Send(voip.Event{Op: "hangup", Call: call.id})
		a.voip.mu.Lock()
		if a.voip.call == call {
			a.voip.call = nil
		}
		a.voip.mu.Unlock()
	}()
	if !valid() || a.readLockState().Gen != lockGen {
		return
	}
	if host.Send(voip.Event{Op: "dial", Call: call.id, Target: &settings.Target}) != nil {
		_ = socket.Send(voip.Event{Op: "ended", Error: "本机 VoIP 组件连接已断开"})
		return
	}
	if socket.Send(voip.Event{Op: "dialing"}) != nil {
		return
	}
	clientEvents := socket.ReadUntilClosed()
	validate := time.NewTicker(time.Second)
	defer validate.Stop()
	timer := time.NewTimer(45 * time.Second)
	defer timer.Stop()
	answered := false
	for {
		select {
		case <-r.Context().Done():
			return
		case <-call.lost:
			_ = socket.Send(voip.Event{Op: "ended", Error: "本机 VoIP 组件连接已断开"})
			return
		case <-validate.C:
			lock := a.readLockState()
			if lock.Locked || lock.Gen != lockGen || !valid() || a.runtimeStopping.Load() {
				_ = socket.Send(voip.Event{Op: "ended", Error: "通话授权已结束"})
				return
			}
		case <-timer.C:
			_ = socket.Send(voip.Event{Op: "ended", Error: "话机接听超时"})
			return
		case e, ok := <-clientEvents:
			if !ok || e.Op == "hangup" {
				return
			}
			if answered && e.Op == "audio" && len(e.PCM) == voip.PCMFrameBytes {
				if host.Send(voip.Event{Op: "audio", Call: call.id, PCM: e.PCM}) != nil {
					return
				}
			}
		case e := <-call.events:
			if e.Op == "answered" {
				answered = true
				timer.Stop()
			}
			if (e.Op != "audio" || answered) &&
				socket.Send(voip.Event{Op: e.Op, Code: e.Code, PCM: e.PCM, Error: e.Error}) != nil {
				return
			}
			if e.Op == "ended" {
				return
			}
		}
	}
}
