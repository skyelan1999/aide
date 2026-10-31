package server

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type billingCredential struct {
	AccessKeyID     string `json:"accessKeyId"`
	AccessKeySecret string `json:"accessKeySecret"`
}

func billingVaultID(provider string) string { return "provider-billing:" + provider }
func billingKeyVaultID(provider, key string) string {
	if key == "" {
		return billingVaultID(provider)
	}
	return "provider-billing-key:" + billingScope(provider, key, "")
}
func (a *App) providerBillingCredentials(w http.ResponseWriter, r *http.Request) {
	pid := r.URL.Query().Get("providerId")
	kid := r.URL.Query().Get("keyId")
	var in struct {
		ProviderID      string `json:"providerId"`
		KeyID           string `json:"keyId"`
		AccessKeyID     string `json:"accessKeyId"`
		AccessKeySecret string `json:"accessKeySecret"`
		Clear           bool   `json:"clear"`
	}
	if r.Method == http.MethodPut {
		if e := decode(w, r, &in); e != nil {
			fail(w, 400, e)
			return
		}
		pid = in.ProviderID
		kid = in.KeyID
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	p, kid, e := a.billingTargetLocked(pid, kid, "")
	if e != nil {
		fail(w, 400, e)
		return
	}
	c, _ := a.providerSupportLocked(p.BaseURL)
	if c == nil || c.Balance == nil || c.Balance.Auth != "aliyun-rpc" {
		fail(w, 400, errors.New("active provider balance plugin does not require billing credentials"))
		return
	}
	id := billingKeyVaultID(p.ID, kid)
	if r.Method == http.MethodPut {
		if e := managementGuardLocked(a, r); e != nil {
			fail(w, 403, e)
			return
		}
		if a.vault == nil || !a.vault.Unlocked() {
			fail(w, 423, errVaultLocked)
			return
		}
		if in.Clear {
			a.vault.Delete(id)
		} else {
			if strings.TrimSpace(in.AccessKeyID) == "" || strings.TrimSpace(in.AccessKeySecret) == "" || len(in.AccessKeyID) > 256 || len(in.AccessKeySecret) > 512 {
				fail(w, 400, errors.New("billing AccessKey ID and secret are required"))
				return
			}
			raw, _ := json.Marshal(billingCredential{in.AccessKeyID, in.AccessKeySecret})
			if e := a.vault.Put(id, "provider-billing", "Provider billing · "+p.ID, raw, ""); e != nil {
				fail(w, 500, e)
				return
			}
		}
		if e := a.vault.Save(); e != nil {
			fail(w, 500, e)
			return
		}
	}
	jsonOut(w, 200, map[string]any{"providerId": p.ID, "keyId": kid, "configured": a.vault != nil && a.vault.Has(id)})
}
func rpcEscape(v string) string { return strings.ReplaceAll(url.QueryEscape(v), "+", "%20") }
func signBalanceRPC(ak, secret string, at time.Time, nonce string) url.Values {
	q := url.Values{"Action": {"QueryAccountBalance"}, "Version": {"2017-12-14"}, "Format": {"JSON"}, "AccessKeyId": {ak}, "SignatureMethod": {"HMAC-SHA1"}, "SignatureVersion": {"1.0"}, "SignatureNonce": {nonce}, "Timestamp": {at.UTC().Format("2006-01-02T15:04:05Z")}}
	canonical := strings.ReplaceAll(q.Encode(), "+", "%20")
	toSign := "GET&%2F&" + rpcEscape(canonical)
	mac := hmac.New(sha1.New, []byte(secret+"&"))
	mac.Write([]byte(toSign))
	q.Set("Signature", base64.StdEncoding.EncodeToString(mac.Sum(nil)))
	return q
}
func (a *App) listBalance(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	p, kid, e := a.billingTargetLocked(r.URL.Query().Get("providerId"), r.URL.Query().Get("keyId"), "")
	if e != nil {
		a.mu.Unlock()
		fail(w, 400, e)
		return
	}
	c, owner := a.providerSupportLocked(p.BaseURL)
	if c == nil || c.Balance == nil {
		out := map[string]any{"supported": false, "balance_infos": []any{}}
		if c != nil {
			out["pricingSupported"] = len(c.Models) > 0
			out["supporter"] = c.Name
		}
		a.mu.Unlock()
		jsonOut(w, 200, out)
		return
	}
	b := *c.Balance
	declared, revision, scopePath, workspaceRevision := b, c.Revision, a.pluginsPath, a.wsRevision
	if b.Path != "" {
		u, err := url.Parse(p.BaseURL)
		if err != nil || validateSupportQuotaPath(&b) != nil {
			a.mu.Unlock()
			fail(w, 400, errors.New("invalid plugin quota target"))
			return
		}
		u.Path, u.RawPath = b.Path, ""
		b.Endpoint = u.String()
	}
	name := c.Name
	pluginID := owner.ID
	var key string
	var creds billingCredential
	if b.Auth == "model-bearer" {
		endpointURL, endpointErr := url.Parse(b.Endpoint)
		providerURL, providerErr := url.Parse(p.BaseURL)
		if endpointErr != nil || providerErr != nil || !strings.EqualFold(endpointURL.Hostname(), providerURL.Hostname()) || (b.Path != "" && (endpointURL.Scheme != providerURL.Scheme || endpointURL.Host != providerURL.Host)) {
			a.mu.Unlock()
			fail(w, 400, errors.New("balance credentials cannot cross provider hosts"))
			return
		}
		key, e = a.vaultKeyByIDLocked(modelProviderVaultID(p.ID, kid))
		if e == nil && key == "" {
			e = errors.New("model API key not configured")
		}
	} else {
		var raw string
		raw, e = a.vaultKeyByIDLocked(billingKeyVaultID(p.ID, kid))
		if e == nil && raw == "" {
			a.mu.Unlock()
			jsonOut(w, 200, map[string]any{"supported": true, "billingCredentialsRequired": true, "providerId": p.ID, "keyId": kid, "balance_infos": []any{}})
			return
		}
		if e == nil {
			e = json.Unmarshal([]byte(raw), &creds)
		}
	}
	a.mu.Unlock()
	if e != nil {
		fail(w, 423, e)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	endpoint := b.Endpoint
	if b.Auth == "aliyun-rpc" {
		var random [16]byte
		if _, e = rand.Read(random[:]); e != nil {
			fail(w, 500, errors.New("nonce generation failed"))
			return
		}
		endpoint += "?" + signBalanceRPC(creds.AccessKeyID, creds.AccessKeySecret, time.Now(), hex.EncodeToString(random[:])).Encode()
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if e != nil {
		fail(w, 500, errors.New("invalid balance request"))
		return
	}
	if b.Auth == "model-bearer" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, e := client.Do(req)
	if e != nil {
		fail(w, 502, errors.New("balance service connection failed"))
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		if resp.StatusCode == 404 && b.Format == "quota-windows" {
			a.mu.Lock()
			currentCatalog, current := a.providerSupportLocked(p.BaseURL)
			active := current != nil && current.ID == pluginID && currentCatalog.Revision == revision && currentCatalog.Balance != nil && *currentCatalog.Balance == declared && a.pluginsPath == scopePath && a.wsRevision == workspaceRevision
			a.mu.Unlock()
			if !active {
				fail(w, 409, errors.New("provider balance extension changed"))
				return
			}
			jsonOut(w, 200, map[string]any{"supported": true, "kind": "quota", "upgradeRequired": true, "providerId": p.ID, "keyId": kid, "supporter": name, "quota_infos": []any{}})
			return
		}
		fail(w, 502, fmt.Errorf("balance service returned HTTP %d", resp.StatusCode))
		return
	}
	raw, e := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if e != nil || len(raw) > 1<<20 {
		fail(w, 502, errors.New("invalid balance response size"))
		return
	}
	out, e := normalizeBalance(raw, b.Format)
	if e != nil {
		fail(w, 502, e)
		return
	}
	// Do not apply an in-flight result after disabling/uninstalling its provider extension.
	a.mu.Lock()
	currentCatalog, current := a.providerSupportLocked(p.BaseURL)
	active := current != nil && current.ID == pluginID && currentCatalog.Revision == revision && currentCatalog.Balance != nil && *currentCatalog.Balance == declared && a.pluginsPath == scopePath && a.wsRevision == workspaceRevision
	a.mu.Unlock()
	if !active {
		fail(w, 409, errors.New("provider balance extension changed"))
		return
	}
	out["supported"] = true
	out["supporter"] = name
	out["providerId"] = p.ID
	out["keyId"] = kid
	out["balanceScope"] = "account"
	out["balanceAuth"] = b.Auth
	jsonOut(w, 200, out)
}
func normalizeBalance(raw []byte, format string) (map[string]any, error) {
	if format == "quota-windows" {
		return normalizeQuotaWindows(raw)
	}
	if format == "balance-infos" {
		var src struct {
			Available bool `json:"is_available"`
			Infos     []struct {
				Currency string `json:"currency"`
				Total    string `json:"total_balance"`
				Granted  string `json:"granted_balance"`
				Topped   string `json:"topped_up_balance"`
			} `json:"balance_infos"`
		}
		if json.Unmarshal(raw, &src) != nil || len(src.Infos) > 8 {
			return nil, errors.New("invalid balance response")
		}
		infos := []any{}
		for _, v := range src.Infos {
			if v.Currency != "CNY" && v.Currency != "USD" {
				return nil, errors.New("unsupported balance currency")
			}
			for _, n := range []string{v.Total, v.Granted, v.Topped} {
				if value, e := strconv.ParseFloat(n, 64); e != nil || math.IsNaN(value) || math.IsInf(value, 0) {
					return nil, errors.New("invalid balance amount")
				}
			}
			infos = append(infos, map[string]any{"currency": v.Currency, "total_balance": v.Total, "granted_balance": v.Granted, "topped_up_balance": v.Topped})
		}
		return map[string]any{"is_available": src.Available, "balance_infos": infos}, nil
	}
	if format == "aliyun-account-balance" {
		var src struct {
			Success bool `json:"Success"`
			Data    struct {
				Available string `json:"AvailableAmount"`
				Cash      string `json:"AvailableCashAmount"`
				Currency  string `json:"Currency"`
			} `json:"Data"`
		}
		if json.Unmarshal(raw, &src) != nil || !src.Success || src.Data.Currency != "CNY" {
			return nil, errors.New("billing account response unavailable or region currency mismatch")
		}
		for _, n := range []string{src.Data.Available, src.Data.Cash} {
			if value, e := strconv.ParseFloat(strings.ReplaceAll(n, ",", ""), 64); e != nil || math.IsNaN(value) || math.IsInf(value, 0) {
				return nil, errors.New("invalid balance amount")
			}
		}
		return map[string]any{"balance_infos": []any{map[string]any{"currency": src.Data.Currency, "total_balance": src.Data.Available, "available_cash_balance": src.Data.Cash}}}, nil
	}
	return nil, errors.New("unsupported balance format")
}
