package messaging

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

func (s *Service) peerHTTPClient(peer string, card Card) (*http.Client, error) {
	relayDial := s.dial
	dial := func(ctx context.Context, network, _ string) (net.Conn, error) {
		var last error = errors.New("peer has no reachable direct address or relay")
		for _, address := range card.Addresses {
			u, err := ParseAddress(address)
			if err != nil {
				continue
			}
			host := u.Host
			if u.Port() == "" {
				host = net.JoinHostPort(u.Hostname(), "443")
			}
			attempt, cancel := context.WithTimeout(ctx, 3*time.Second)
			conn, err := (&net.Dialer{}).DialContext(attempt, "tcp", host)
			cancel()
			if err == nil {
				return conn, nil
			}
			last = err
		}
		if relayDial != nil {
			return relayDial(ctx, peer, card)
		}
		return nil, last
	}
	return PeerClient(s.cert, card, dial)
}
func (s *Service) UploadChunk(ctx context.Context, id string, index int, data []byte) error {
	inv, ok := s.consent.Snapshot().Invitations[id]
	if !ok || inv.Direction != "outgoing" || !s.consent.Authorized(inv.Peer, id, File, inv.Resource) {
		return ErrDenied
	}
	s.mu.Lock()
	f, ok := s.state.Transfers[id]
	s.mu.Unlock()
	if !ok || f.Incoming || f.Cancelled || index < 0 || index >= len(f.Manifest.Hashes) {
		return ErrDenied
	}
	expected := ChunkSize
	if index == len(f.Manifest.Hashes)-1 {
		expected = int(f.Manifest.Size - int64(index)*ChunkSize)
	}
	digest := sha256.Sum256(data)
	if len(data) != expected || hex.EncodeToString(digest[:]) != f.Manifest.Hashes[index] {
		return errors.New("source file changed or chunk invalid")
	}
	card, err := s.peer(f.Peer)
	if err != nil {
		return err
	}
	client, err := s.client(f.Peer, card)
	if err != nil {
		return err
	}
	endpoint := "https://" + peerName
	if len(card.Addresses) > 0 {
		endpoint = card.Addresses[0]
	}
	request, err := http.NewRequestWithContext(ctx, "PUT", fmt.Sprintf("%s/v1/files/%s/chunks/%d", endpoint, id, index), bytes.NewReader(data))
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != 204 {
		return errors.New("recipient rejected chunk")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f = s.state.Transfers[id]
	if f.Cancelled {
		return ErrDenied
	}
	f.Received[index] = true
	s.state.Transfers[id] = f
	return s.persistLocked()
}
func (s *Service) Download(ctx context.Context, id string, writer io.Writer) error {
	s.mu.Lock()
	f, ok := s.state.Transfers[id]
	s.mu.Unlock()
	inv := s.consent.Snapshot().Invitations[id]
	if !ok || !f.Incoming || f.Cancelled || len(f.Received) != len(f.Manifest.Hashes) || !s.consent.Authorized(f.Peer, id, File, inv.Resource) {
		return ErrDenied
	}
	for index, expected := range f.Manifest.Hashes {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !s.consent.Authorized(f.Peer, id, File, inv.Resource) {
			return ErrDenied
		}
		chunk, err := s.store.Get(fmt.Sprintf("chunk-%s-%d", id, index), ChunkSize)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(chunk)
		if hex.EncodeToString(digest[:]) != expected {
			return errors.New("stored chunk integrity failed")
		}
		if _, err = writer.Write(chunk); err != nil {
			return err
		}
	}
	return nil
}
func (s *Service) Cancel(ctx context.Context, id string) error {
	inv, ok := s.consent.Snapshot().Invitations[id]
	if !ok {
		return ErrDenied
	}
	if err := s.consent.Revoke(id); err != nil {
		return err
	}
	s.mu.Lock()
	cleanup := s.cleanupTransferLocked(id)
	err := s.persistLocked()
	s.mu.Unlock()
	if cleanup != nil {
		return cleanup
	}
	if err != nil {
		return err
	}
	s.afterRevoke(ctx, inv)
	// Local revocation is immediate even if the remote endpoint is offline.
	if reply, err := s.push(ctx, inv.Peer, Wire{Type: "revoke", ID: id}); err == nil && reply.Status == "revoked" {
		_ = s.consent.AcknowledgeRevocation(id)
	}
	return nil
}

func (s *Service) cleanupTransferLocked(id string) error {
	f, exists := s.state.Transfers[id]
	if !exists {
		return nil
	}
	f.Cancelled = true
	f.Received = copyProgress(f.Received)
	var failed error
	for index := range f.Received {
		if err := s.store.Delete(fmt.Sprintf("chunk-%s-%d", id, index)); err != nil {
			failed = errors.Join(failed, err)
		} else {
			delete(f.Received, index)
		}
	}
	if failed != nil {
		f.CleanupError = failed.Error()
	} else {
		f.CleanupError = ""
	}
	f.Manifest.Hashes = []string{}
	s.state.Transfers[id] = f
	return failed
}
func copyProgress(source map[int]bool) map[int]bool {
	out := map[int]bool{}
	for k, v := range source {
		out[k] = v
	}
	return out
}
