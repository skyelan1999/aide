package messaging

// The launcher-owned host helper handles LAN sockets, never device keys or TLS.
// Only public discovery cards and bounded lookup jobs cross this private mailbox.
import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"time"
)

type bridgeLease struct {
	Instance  string   `json:"instance"`
	Expires   int64    `json:"expires"`
	Addresses []string `json:"addresses"`
}
type bridgeAnnouncement struct {
	Instance string `json:"instance"`
	Owner    string `json:"owner"`
	Expires  int64  `json:"expires"`
	Card     Card   `json:"card"`
}
type bridgeQuery struct {
	Instance string `json:"instance"`
	Code     string `json:"code"`
	Expires  int64  `json:"expires"`
}
type bridgeReply struct {
	Instance string `json:"instance"`
	Cards    []Card `json:"cards"`
}

func bridgeRead(root *os.Root, name string, value any) error {
	f, err := root.Open(name)
	if err != nil {
		return err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 256<<10+1))
	if err != nil {
		return err
	}
	if len(b) > 256<<10 {
		return errors.New("LAN bridge record too large")
	}
	return json.Unmarshal(b, value)
}
func bridgeWrite(root *os.Root, name string, value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(b) > 256<<10 {
		return errors.New("LAN bridge record too large")
	}
	temp := ".write-" + randomID()
	f, err := root.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer root.Remove(temp)
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return root.Rename(temp, name)
}
func bridgeOpen(dir string) (*os.Root, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	return os.OpenRoot(dir)
}
func readBridgeLease(root *os.Root) (bridgeLease, error) {
	var lease bridgeLease
	if err := bridgeRead(root, "host.json", &lease); err != nil {
		return lease, err
	}
	now := time.Now().Unix()
	if !validID(lease.Instance) || lease.Expires <= now || lease.Expires > now+15 || len(lease.Addresses) == 0 || len(lease.Addresses) > 12 {
		return lease, errors.New("LAN bridge unavailable or expired")
	}
	for _, a := range lease.Addresses {
		if _, err := ParseAddress(a); err != nil {
			return lease, err
		}
	}
	return lease, nil
}
func (n *Network) publishBridge() bool {
	if n.config.BridgeDir == "" || !n.config.LAN {
		return false
	}
	root, err := bridgeOpen(n.config.BridgeDir)
	if err != nil {
		return false
	}
	defer root.Close()
	lease, err := readBridgeLease(root)
	if err != nil {
		return false
	}
	addresses := lease.Addresses
	if len(n.config.Advertise) > 0 {
		addresses = n.config.Advertise
	}
	if err = n.service.SetAddresses(addresses); err != nil {
		return false
	}
	card, err := n.service.Card()
	if err != nil {
		return false
	}
	return bridgeWrite(root, "announce.json", bridgeAnnouncement{Instance: lease.Instance, Owner: n.bridgeOwner, Expires: time.Now().Add(6 * time.Second).Unix(), Card: card}) == nil
}
func (n *Network) clearBridge() {
	if n.config.BridgeDir == "" {
		return
	}
	root, err := os.OpenRoot(n.config.BridgeDir)
	if err != nil {
		return
	}
	defer root.Close()
	var ann bridgeAnnouncement
	if bridgeRead(root, "announce.json", &ann) == nil && ann.Owner == n.bridgeOwner {
		_ = root.Remove("announce.json")
	}
}
func bridgeDiscover(ctx context.Context, dir, code string) ([]Card, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	lease, err := readBridgeLease(root)
	if err != nil {
		return nil, err
	}
	id := randomID()
	req := "query-" + id + ".json"
	reply := "reply-" + id + ".json"
	defer root.Remove(req)
	defer root.Remove(reply)
	ctx, cancel := context.WithTimeout(ctx, 1800*time.Millisecond)
	defer cancel()
	if err = bridgeWrite(root, req, bridgeQuery{Instance: lease.Instance, Code: code, Expires: time.Now().Add(3 * time.Second).Unix()}); err != nil {
		return nil, err
	}
	ticker := time.NewTicker(40 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			var r bridgeReply
			if bridgeRead(root, reply, &r) != nil {
				continue
			}
			if r.Instance != lease.Instance || len(r.Cards) > 16 {
				return nil, errors.New("invalid LAN bridge reply")
			}
			out := []Card{}
			for _, c := range r.Cards {
				if c.Device.Code == code && c.Validate(time.Now()) == nil {
					out = append(out, c)
				}
			}
			return out, nil
		}
	}
}
func bridgeJobID(name, prefix string) string {
	if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".json") {
		return ""
	}
	id := strings.TrimSuffix(strings.TrimPrefix(name, prefix), ".json")
	if !validID(id) {
		return ""
	}
	return id
}

func LANBridgeReady(dir string) bool {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return false
	}
	defer root.Close()
	_, err = readBridgeLease(root)
	return err == nil
}
