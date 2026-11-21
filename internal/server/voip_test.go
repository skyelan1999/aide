package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"aide/internal/messaging"
	"aide/internal/voip"
	"github.com/gorilla/websocket"
)

func TestVoIPRoutesRequireConsentAndReleaseOnLock(t *testing.T) {
	a, b := testApp(t), testApp(t)
	peer := connectVoIPMessagePeers(t, a, b)
	requireStatus(t, request(a, "GET", "/api/voip/settings", nil), 200)
	requireStatus(t, request(a, "PUT", "/api/voip/settings", voipSettings{Assistant: true, Target: voip.Target{IP: "8.8.8.8"}}), 400)
	settings := voipSettings{Messages: true, Assistant: true, Target: voip.Target{IP: "192.168.1.50"}}
	requireStatus(t, request(a, "PUT", "/api/voip/settings", settings), 200)

	server := httptest.NewServer(a.Handler())
	defer server.Close()
	base := "ws" + strings.TrimPrefix(server.URL, "http")
	dial := func(path string) (*websocket.Conn, *http.Response, error) {
		u := websocket.Dialer{Subprotocols: []string{"aide-voip", "aide-auth." + a.token}}
		return u.Dial(base+path, http.Header{"Origin": {server.URL}})
	}
	reject := func(path string) {
		t.Helper()
		conn, resp, err := dial(path)
		if conn != nil {
			conn.Close()
		}
		if resp != nil {
			defer resp.Body.Close()
		}
		if err == nil || resp == nil || resp.StatusCode != 403 {
			t.Fatalf("expected VoIP to deny %s, got %v %v", path, resp, err)
		}
	}
	read := func(conn *websocket.Conn, op string) voip.Event {
		t.Helper()
		_ = conn.SetReadDeadline(time.Now().Add(4 * time.Second))
		var event voip.Event
		if err := conn.ReadJSON(&event); err != nil || event.Op != op {
			t.Fatalf("expected %s: %#v %v", op, event, err)
		}
		return event
	}
	send := func(conn *websocket.Conn, event voip.Event) {
		t.Helper()
		if err := conn.WriteJSON(event); err != nil {
			t.Fatal(err)
		}
	}
	id := assistantSessionID(t, a)
	assistantPath := "/api/voip/socket?mode=assistant&session=" + id
	reject(assistantPath)
	unlockAssistantSessionForTest(t, a)

	m := a.messages.service
	inv, err := m.Request(context.Background(), peer, messaging.Voice, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	query := url.Values{"mode": {"messages"}, "owner": {strings.Repeat("1", 32)}, "peer": {peer}, "call": {inv.Resource}, "invitation": {inv.ID}}
	messagePath := "/api/voip/socket?" + query.Encode()
	reject(messagePath)
	if err := b.messages.service.Decide(inv.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := m.RefreshInvitation(context.Background(), inv.ID); err != nil {
		t.Fatal(err)
	}
	reject(messagePath) // Consent alone cannot dial from an unleased window.
	lease := map[string]string{"owner": query.Get("owner"), "peer": peer, "call": inv.Resource, "invitation": inv.ID}
	requireStatus(t, request(a, "POST", "/api/messages/voice-lease", lease), 200)

	host, _, err := websocket.DefaultDialer.Dial(base+"/api/voip/host", http.Header{"Authorization": {"Bearer " + a.token}})
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	for _, path := range []string{assistantPath, messagePath} {
		conn, _, err := dial(path)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		start := read(host, "dial")
		if start.Target == nil || start.Target.IP != settings.Target.IP || start.Target.Port != 5060 {
			t.Fatal("incorrect configured phone destination")
		}
		read(conn, "dialing")
		for _, event := range []voip.Event{{Op: "ringing", Code: 180}, {Op: "answered", Code: 200}, {Op: "audio", PCM: make([]byte, voip.PCMFrameBytes)}} {
			event.Call = start.Call
			send(host, event)
			clientEvent := read(conn, event.Op)
			if clientEvent.Call != "" || clientEvent.Code != event.Code || len(clientEvent.PCM) != len(event.PCM) {
				t.Fatalf("browser event: %#v", clientEvent)
			}
		}
		send(conn, voip.Event{Op: "audio", PCM: make([]byte, voip.PCMFrameBytes)})
		if event := read(host, "audio"); event.Call != start.Call || len(event.PCM) != voip.PCMFrameBytes {
			t.Fatal("browser microphone data not routed to call")
		}
		if path == assistantPath {
			send(host, voip.Event{Op: "ended", Call: start.Call})
		} else {
			if _, err := a.writeLockState(true); err != nil {
				t.Fatal(err)
			}
		}
		read(conn, "ended")
		if event := read(host, "hangup"); event.Call != start.Call {
			t.Fatal("wrong dialog closed")
		}
	}
	reject(messagePath)
	reject(assistantPath)
}

func connectVoIPMessagePeers(t *testing.T, a, b *App) string {
	t.Helper()
	for i, app := range []*App{a, b} {
		name := []string{"SIP browser", "WebRTC browser"}[i]
		app.mu.Lock()
		app.settings.UserName = name
		app.mu.Unlock()
		config := messageCenterConfig{Enabled: true, Name: name, Network: structNetworkConfigForTest()}
		requireStatus(t, request(app, "PUT", "/api/messages/config", map[string]any{"config": config}), 200)
	}
	left, right := a.messages.service, b.messages.service
	lcard, err := left.Card()
	if err != nil {
		t.Fatal(err)
	}
	rcard, err := right.Card()
	if err != nil {
		t.Fatal(err)
	}
	if err := left.Remember(rcard); err != nil {
		t.Fatal(err)
	}
	if err := right.Remember(lcard); err != nil {
		t.Fatal(err)
	}
	invite, err := left.Request(context.Background(), rcard.Device.Fingerprint, messaging.Contact, "contact", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := right.Decide(invite.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := left.RefreshInvitation(context.Background(), invite.ID); err != nil {
		t.Fatal(err)
	}
	return rcard.Device.Fingerprint
}

func TestVoIPSettingsDoNotExposeHostThroughLAN(t *testing.T) {
	a := testApp(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/voip/settings", nil)
	r = r.WithContext(context.WithValue(r.Context(), lanIdentityKey{}, &lanSession{}))
	a.voipSettingsAPI(w, r)
	requireStatus(t, w, 200)
	var data struct {
		Local bool
		Ready bool
		voipSettings
	}
	if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil || data.Local || data.Ready || data.Target.IP != "" {
		t.Fatalf("unexpected remote settings: %s (%v)", w.Body, err)
	}
}
