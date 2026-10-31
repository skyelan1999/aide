package server

// Browser balances are opt-in snapshots, declared by a data-only plugin. The
// native helper reads only the dedicated, owner-opened source window. No model
// key, browser cookie, arbitrary DOM or executable selector crosses this API.
import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

type pageBalanceRecord struct {
	ProviderID   string  `json:"providerId"`
	KeyID        string  `json:"keyId"`
	WorkspaceID  string  `json:"workspaceId"`
	Stamp        string  `json:"stamp"`
	KeyHash      string  `json:"keyHash"`
	Nonce        string  `json:"nonce"`
	AccountLabel string  `json:"accountLabel"`
	Identity     string  `json:"identity"`
	Amount       float64 `json:"amount"`
	Checked      int64   `json:"checked"`
	Attempt      int64   `json:"attempt"`
	Enabled      bool    `json:"enabled"`
	Confirmed    bool    `json:"confirmed"`
	PendingID    string  `json:"pendingId,omitempty"`
	State        string  `json:"state"`
	Sequence     uint64  `json:"sequence"`
	Busy         bool    `json:"-"`
}

func validatePageBalance(b *supportBalance) error {
	u, e := url.Parse(b.PageURL)
	clean := func(s string, max int) bool {
		return s != "" && len(s) <= max && !strings.ContainsFunc(s, unicode.IsControl)
	}
	if e != nil || b.Auth != "browser-page" || b.Format != "page-balance" || b.Endpoint != "" || b.Path != "" || b.TotalPath != "" ||
		u.Scheme != "https" || u.User != nil || u.Port() != "" || u.RawQuery != "" || u.Fragment != "" || u.Hostname() == "" ||
		!strings.Contains(u.Hostname(), ".") || net.ParseIP(u.Hostname()) != nil || strings.HasSuffix(u.Hostname(), ".local") ||
		len(b.PageURL) > 2048 || strings.ContainsAny(u.Path, "\\") || strings.Contains(u.Path, "..") ||
		!clean(b.Label, 120) || !clean(b.AccountMarker, 80) || b.RefreshSeconds < 300 || b.RefreshSeconds > 3600 {
		return errors.New("invalid browser page balance declaration")
	}
	identity, err := url.Parse(b.IdentityLinkPrefix)
	if err != nil || identity.Scheme != u.Scheme || identity.Host != u.Host || identity.User != nil || identity.RawQuery != "" || identity.Fragment != "" || identity.Path == "" || len(b.IdentityLinkPrefix) > 1900 || strings.Contains(identity.Path, "..") || strings.Contains(identity.Path, "\\") {
		return errors.New("invalid browser account identity declaration")
	}
	return nil
}
func validPageIdentity(value, prefix string) bool {
	if !strings.HasPrefix(value, prefix) {
		return false
	}
	suffix := strings.TrimPrefix(value, prefix)
	return len(suffix) >= 8 && len(suffix) <= 120 && !strings.ContainsFunc(suffix, func(r rune) bool {
		return !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-')
	})
}
func pageHash(value string) string {
	h := sha256.Sum256([]byte(value))
	return hex.EncodeToString(h[:])
}
func pageNonce() string {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		return ""
	}
	return hex.EncodeToString(b[:])
}
func (a *App) pageBalanceFile() string {
	return filepath.Join(a.dataPath, "config", "provider-page-balances.json")
}
func (a *App) loadPageBalancesLocked() {
	if a.pageBalances != nil {
		return
	}
	a.pageBalances = map[string]*pageBalanceRecord{}
	info, err := os.Lstat(a.pageBalanceFile())
	if err != nil || !info.Mode().IsRegular() || info.Size() > 256<<10 {
		return
	}
	raw, e := os.ReadFile(a.pageBalanceFile())
	if e != nil || len(raw) > 256<<10 {
		return
	}
	var records map[string]*pageBalanceRecord
	if json.Unmarshal(raw, &records) != nil || len(records) > 128 {
		return
	}
	for key, r := range records {
		if r != nil && len(key) == 64 && len(r.Nonce) == 32 && len(r.AccountLabel) <= 160 && len(r.Stamp) == 64 && len(r.KeyHash) == 64 && math.Abs(r.Amount) <= 1e9 {
			a.pageBalances[key] = r
		}
	}
}
func (a *App) savePageBalancesLocked() error {
	if err := os.MkdirAll(filepath.Dir(a.pageBalanceFile()), 0700); err != nil {
		return err
	}
	return atomicJSON(a.pageBalanceFile(), a.pageBalances)
}
func (a *App) pageTargetLocked(p ModelProvider, kid string, c *supportCatalog, owner *PluginManifest) (string, string, string, error) {
	key, e := a.vaultKeyByIDLocked(modelProviderVaultID(p.ID, kid))
	if e != nil {
		return "", "", "", e
	}
	if key == "" {
		return "", "", "", errors.New("model API key not configured")
	}
	raw, _ := json.Marshal([]any{owner.ID, owner.Version, owner.PackageSHA256, c.Revision, c.Currency, c.Balance})
	return pageHash(a.wsID() + "\x00" + p.ID + "\x00" + kid + "\x00" + p.BaseURL), pageHash(string(raw)), pageHash(key), nil
}
func (a *App) pageBalanceResultLocked(p ModelProvider, kid string, c *supportCatalog, owner *PluginManifest) (map[string]any, error) {
	scope, stamp, keyHash, e := a.pageTargetLocked(p, kid, c, owner)
	if e != nil {
		return nil, e
	}
	a.loadPageBalancesLocked()
	out := map[string]any{"supported": true, "providerId": p.ID, "keyId": kid, "supporter": c.Name, "balanceAuth": "browser-page", "balance_infos": []any{}}
	info := map[string]any{"url": c.Balance.PageURL, "state": "setup", "enabled": false, "refreshSeconds": c.Balance.RefreshSeconds}
	if r := a.pageBalances[scope]; r != nil && r.Stamp == stamp && r.KeyHash == keyHash {
		state := r.State
		if r.Confirmed && r.Enabled && state == "ready" && time.Now().Unix()-r.Checked > int64(c.Balance.RefreshSeconds*2+60) {
			state = "stale"
		}
		info["state"], info["enabled"], info["accountLabel"], info["checkedAt"], info["lastAttempt"] = state, r.Enabled, r.AccountLabel, r.Checked, r.Attempt
		info["accountIdentity"] = r.Identity
		if r.Busy {
			info["syncing"] = true
		}
		if !r.Confirmed && r.PendingID != "" && time.Now().Unix()-r.Checked <= 120 {
			info["pendingId"] = r.PendingID
			info["previewAmount"] = r.Amount
			info["currency"] = c.Currency
		}
		if state == "ready" && r.Confirmed && r.Enabled {
			out["balance_infos"] = []any{map[string]any{"currency": c.Currency, "total_balance": r.Amount}}
		}
	}
	out["pageBalance"] = info
	return out, nil
}

func (a *App) providerPageBalance(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ProviderID string `json:"providerId"`
		KeyID      string `json:"keyId"`
		Action     string `json:"action"`
		PendingID  string `json:"pendingId"`
	}
	if e := decode(w, r, &in); e != nil {
		fail(w, 400, e)
		return
	}
	a.mu.Lock()
	if e := managementGuardLocked(a, r); e != nil {
		a.mu.Unlock()
		fail(w, 403, e)
		return
	}
	p, kid, e := a.billingTargetLocked(in.ProviderID, in.KeyID, "")
	if e != nil {
		a.mu.Unlock()
		fail(w, 400, e)
		return
	}
	c, owner := a.providerSupportLocked(p.BaseURL)
	if c == nil || c.Balance == nil || c.Balance.Auth != "browser-page" {
		a.mu.Unlock()
		fail(w, 400, errors.New("browser balance plugin unavailable"))
		return
	}
	scope, stamp, keyHash, e := a.pageTargetLocked(p, kid, c, owner)
	if e != nil {
		a.mu.Unlock()
		fail(w, 423, e)
		return
	}
	a.loadPageBalancesLocked()
	record := a.pageBalances[scope]
	if in.Action == "prepare" {
		if record == nil || record.Stamp != stamp || record.KeyHash != keyHash {
			if record == nil && len(a.pageBalances) >= 128 {
				a.mu.Unlock()
				fail(w, 409, errors.New("too many linked browser sources"))
				return
			}
			record = &pageBalanceRecord{ProviderID: p.ID, KeyID: kid, WorkspaceID: a.wsID(), Stamp: stamp, KeyHash: keyHash, Nonce: pageNonce(), State: "setup"}
			a.pageBalances[scope] = record
		}
		if len(record.Nonce) != 32 {
			a.mu.Unlock()
			fail(w, 500, errors.New("source window identity unavailable"))
			return
		}
		record.Sequence++
		record.Busy = false
		if e = a.savePageBalancesLocked(); e != nil {
			a.mu.Unlock()
			fail(w, 500, e)
			return
		}
		openURL := c.Balance.PageURL + "#aide-balance-" + record.Nonce
		a.mu.Unlock()
		jsonOut(w, 200, map[string]any{"openURL": openURL})
		return
	}
	if record == nil || record.Stamp != stamp || record.KeyHash != keyHash {
		a.mu.Unlock()
		fail(w, 409, errors.New("prepare source window first"))
		return
	}
	switch in.Action {
	case "confirm":
		if record.PendingID == "" || record.PendingID != in.PendingID || time.Now().Unix()-record.Checked > 120 || record.Busy {
			a.mu.Unlock()
			fail(w, 409, errors.New("balance preview expired; read source again"))
			return
		}
		record.Confirmed = true
		record.Enabled = true
		record.State = "ready"
		record.PendingID = ""
		record.Sequence++
	case "disable":
		record.Enabled = false
		record.Confirmed = false
		record.State = "disabled"
		record.PendingID = ""
		record.Busy = false
		record.Sequence++
	case "read":
		if record.Busy {
			a.mu.Unlock()
			fail(w, 409, errors.New("balance sync already running"))
			return
		}
		a.mu.Unlock()
		a.readPageBalance(r.Context(), p, kid, false)
		a.mu.Lock()
		p, kid, e = a.billingTargetLocked(in.ProviderID, in.KeyID, "")
		if e == nil {
			c, owner = a.providerSupportLocked(p.BaseURL)
		}
		if e != nil || c == nil || c.Balance == nil || c.Balance.Auth != "browser-page" {
			a.mu.Unlock()
			fail(w, 409, errors.New("balance source changed"))
			return
		}
		out, err := a.pageBalanceResultLocked(p, kid, c, owner)
		a.mu.Unlock()
		if err != nil {
			fail(w, 423, err)
			return
		}
		jsonOut(w, 200, out)
		return
	default:
		a.mu.Unlock()
		fail(w, 400, errors.New("unsupported browser balance action"))
		return
	}
	e = a.savePageBalancesLocked()
	out, err := a.pageBalanceResultLocked(p, kid, c, owner)
	a.mu.Unlock()
	if e != nil {
		fail(w, 500, e)
		return
	}
	if err != nil {
		fail(w, 423, err)
		return
	}
	jsonOut(w, 200, out)
}

func (a *App) readPageBalance(ctx context.Context, p ModelProvider, kid string, refresh bool) {
	a.mu.Lock()
	c, owner := a.providerSupportLocked(p.BaseURL)
	if c == nil || c.Balance == nil || c.Balance.Auth != "browser-page" {
		a.mu.Unlock()
		return
	}
	scope, stamp, keyHash, e := a.pageTargetLocked(p, kid, c, owner)
	if e != nil {
		a.mu.Unlock()
		return
	}
	a.loadPageBalancesLocked()
	record := a.pageBalances[scope]
	if record == nil || record.Stamp != stamp || record.KeyHash != keyHash || record.Busy || (refresh && !record.Enabled) {
		a.mu.Unlock()
		return
	}
	record.Busy = true
	record.Attempt = time.Now().Unix()
	seq, nonce, wsRevision, path := record.Sequence, record.Nonce, a.wsRevision, a.pluginsPath
	declaration, currency := *c.Balance, c.Currency
	client := a.pageBalanceClient
	a.mu.Unlock()
	state := "bridge_unavailable"
	var value struct {
		OK              bool    `json:"ok"`
		Available       bool    `json:"available"`
		Amount          float64 `json:"amount"`
		Currency        string  `json:"currency"`
		AccountLabel    string  `json:"accountLabel"`
		AccountIdentity string  `json:"accountIdentity"`
		SourceURL       string  `json:"sourceURL"`
		CheckedAt       string  `json:"checkedAt"`
		State           string  `json:"state"`
	}
	token := os.Getenv("AIDE_COMPUTER_BRIDGE_TOKEN")
	if len(token) >= 32 {
		base := "http://host.docker.internal:17778"
		if _, e := os.Stat("/.dockerenv"); os.IsNotExist(e) {
			base = "http://127.0.0.1:17778"
		}
		body, _ := json.Marshal(map[string]any{"url": declaration.PageURL + "#aide-balance-" + nonce, "label": declaration.Label, "currency": currency, "accountMarker": declaration.AccountMarker, "identityLinkPrefix": declaration.IdentityLinkPrefix, "refresh": refresh})
		callCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
		defer cancel()
		request, _ := http.NewRequestWithContext(callCtx, http.MethodPost, base+"/v1/browser-page-balance", bytes.NewReader(body))
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("Content-Type", "application/json")
		if client == nil {
			transport := http.DefaultTransport.(*http.Transport).Clone()
			transport.Proxy = nil
			defer transport.CloseIdleConnections()
			client = &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		}
		if response, e := client.Do(request); e == nil {
			raw, err := io.ReadAll(io.LimitReader(response.Body, 8193))
			response.Body.Close()
			if response.StatusCode == 200 && err == nil && len(raw) <= 8192 && json.Unmarshal(raw, &value) == nil && value.OK {
				if !value.Available {
					state = "source_unavailable"
					switch value.State {
					case "browser_closed", "source_closed", "ambiguous_source", "refresh_unavailable", "login_or_page_changed":
						state = value.State
					}
				}
				if at, e := time.Parse(time.RFC3339, value.CheckedAt); value.Available && e == nil && math.Abs(time.Since(at).Seconds()) <= 120 && value.SourceURL == declaration.PageURL+"#aide-balance-"+nonce && validPageIdentity(value.AccountIdentity, declaration.IdentityLinkPrefix) && value.Currency == currency && !math.IsNaN(value.Amount) && !math.IsInf(value.Amount, 0) && math.Abs(value.Amount) <= 1e9 && strings.TrimSpace(value.AccountLabel) != "" && len(value.AccountLabel) <= 160 && !strings.ContainsFunc(value.AccountLabel, unicode.IsControl) {
					state = "ready"
				}
			}
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	current := a.pageBalances[scope]
	if current == nil || current.Sequence != seq {
		return
	}
	current.Busy = false
	if a.wsRevision != wsRevision || a.pluginsPath != path {
		return
	}
	currentProvider, currentKid, targetErr := a.billingTargetLocked(p.ID, kid, "")
	if targetErr != nil || currentProvider.BaseURL != p.BaseURL || currentKid != kid {
		return
	}
	catalog, plugin := a.providerSupportLocked(p.BaseURL)
	if catalog == nil || catalog.Balance == nil || plugin == nil {
		return
	}
	_, currentStamp, currentKey, err := a.pageTargetLocked(p, kid, catalog, plugin)
	if err != nil || currentStamp != stamp || currentKey != keyHash {
		return
	}
	current.State = state
	if state == "ready" {
		if !current.Confirmed || current.AccountLabel != value.AccountLabel || current.Identity != value.AccountIdentity {
			current.Confirmed = false
			current.Enabled = false
			current.PendingID = pageNonce()
			current.State = "confirm"
		}
		current.Amount = value.Amount
		current.AccountLabel = value.AccountLabel
		current.Identity = value.AccountIdentity
		current.Checked = time.Now().Unix()
	}
	if err := a.savePageBalancesLocked(); err != nil {
		current.State = "storage_error"
		current.Enabled = false
		current.Confirmed = false
		current.PendingID = ""
	}
}

func (a *App) pageBalanceLoop(ctx context.Context) {
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		a.syncDuePageBalances(ctx)
	}
}
func (a *App) syncDuePageBalances(ctx context.Context) {
	type target struct {
		p   ModelProvider
		kid string
	}
	var targets []target
	a.mu.Lock()
	a.loadPageBalancesLocked()
	for _, record := range a.pageBalances {
		if !record.Enabled || !record.Confirmed || record.Busy || record.WorkspaceID != a.wsID() {
			continue
		}
		p, kid, e := a.billingTargetLocked(record.ProviderID, record.KeyID, "")
		if e != nil {
			continue
		}
		c, owner := a.providerSupportLocked(p.BaseURL)
		if c == nil || c.Balance == nil || c.Balance.Auth != "browser-page" {
			continue
		}
		_, stamp, keyHash, e := a.pageTargetLocked(p, kid, c, owner)
		if e != nil || stamp != record.Stamp || keyHash != record.KeyHash {
			continue
		}
		interval := int64(c.Balance.RefreshSeconds)
		if record.State != "ready" {
			interval = 60
		}
		if time.Now().Unix()-record.Attempt >= interval {
			targets = append(targets, target{p, kid})
		}
	}
	a.mu.Unlock()
	for _, target := range targets {
		if ctx.Err() != nil {
			return
		}
		a.readPageBalance(ctx, target.p, target.kid, true)
	}
}
