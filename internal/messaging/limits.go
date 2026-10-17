package messaging

import (
	"net"
	"net/http"
	"sync"
	"time"
)

func (s *Service) knownPeer(peer string) bool {
	if s.consent.CanChat(peer) {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, g := range s.state.Groups {
		if _, ok := g.Members[peer]; ok && s.localGroupAllowed(g) {
			return true
		}
	}
	return false
}
func (s *Service) rateLimitedHandler() http.Handler {
	handler := s.Handler()
	var mu sync.Mutex
	windows := map[string]relayWindow{}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		peer, err := AuthenticatedPeer(r)
		if err != nil {
			http.Error(w, "authentication required", 403)
			return
		}
		now := time.Now()
		known := s.knownPeer(peer.Fingerprint)
		limit := 60
		if known {
			limit = 1200
		}
		host, _, _ := net.SplitHostPort(r.RemoteAddr)
		// Relay tunnels have the relay socket as RemoteAddr. Network limits are
		// secondary abuse bounds; they never determine identity or contact trust.
		mu.Lock()
		for key, window := range windows {
			if now.Sub(window.at) > time.Minute {
				delete(windows, key)
			}
		}
		denied := len(windows) > 4096
		for key, bound := range map[string]int{"peer:" + peer.Fingerprint: limit, "network:" + host: 10000} {
			window := windows[key]
			if now.Sub(window.at) > time.Minute {
				window = relayWindow{at: now}
			}
			if window.count >= bound {
				denied = true
			}
			window.count++
			windows[key] = window
		}
		mu.Unlock()
		if denied {
			w.Header().Set("Connection", "close")
			http.Error(w, "request limit", 429)
			return
		}
		handler.ServeHTTP(w, r)
	})
}
