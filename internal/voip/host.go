package voip

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// RunHost reconnects to the local Aide installation. The helper has no HTTP
// listener of its own. SIP and RTP sockets exist only for the requested call.
func RunHost(ctx context.Context, authority, data string) {
	for ctx.Err() == nil {
		conn := connectHost(ctx, authority, data)
		if conn != nil {
			runHostSocket(ctx, &Socket{Conn: conn})
			conn.Close()
		}
		select {
		case <-ctx.Done():
		case <-time.After(2 * time.Second):
		}
	}
}

func connectHost(ctx context.Context, authority, data string) *websocket.Conn {
	cert, err := os.ReadFile(filepath.Join(data, "certs", "cert.pem"))
	if err != nil {
		return nil
	}
	token, err := os.ReadFile(filepath.Join(data, "auth", "access-token"))
	if err != nil {
		return nil
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(cert) {
		return nil
	}
	dialer := websocket.Dialer{HandshakeTimeout: 4 * time.Second, TLSClientConfig: &tls.Config{
		RootCAs: roots, ServerName: "localhost", MinVersion: tls.VersionTLS12,
	}}
	u := url.URL{Scheme: "wss", Host: authority, Path: "/api/voip/host"}
	conn, resp, err := dialer.DialContext(ctx, u.String(), http.Header{"Authorization": {"Bearer " + strings.TrimSpace(string(token))}})
	if err != nil && resp != nil {
		resp.Body.Close()
	}
	return conn
}

func runHostSocket(ctx context.Context, socket *Socket) {
	events := socket.ReadUntilClosed()
	var call string
	var stop context.CancelFunc
	var input chan []byte
	finished := make(chan struct{}, 1)
	defer func() {
		if stop != nil {
			stop()
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case <-finished:
			stop()
			call, stop, input = "", nil, nil
		case e, ok := <-events:
			if !ok {
				return
			}
			switch {
			case e.Op == "dial" && e.Target != nil && e.Call != "":
				if stop != nil {
					_ = socket.Send(Event{Op: "ended", Call: e.Call, Error: "上一通话机通话正在结束，请稍后重试"})
					continue
				}
				callCtx, cancel := context.WithCancel(ctx)
				call, stop, input = e.Call, cancel, make(chan []byte, 10)
				go func(id string, target Target, audio <-chan []byte) {
					err := ServePhone(callCtx, target, audio, func(event Event) error {
						event.Call = id
						return socket.Send(event)
					})
					event := Event{Op: "ended", Call: id}
					if err != nil {
						event.Error = err.Error()
					}
					_ = socket.Send(event)
					finished <- struct{}{}
				}(call, *e.Target, input)
			case e.Call == call && stop != nil && e.Op == "hangup":
				stop()
			case e.Call == call && input != nil && e.Op == "audio" && len(e.PCM) == PCMFrameBytes:
				select {
				case input <- e.PCM:
				default: // Keep live audio current when its transport falls behind.
					select {
					case <-input:
					default:
					}
					input <- e.PCM
				}
			}
		}
	}
}
