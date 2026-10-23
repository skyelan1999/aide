package server

import (
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/url"
	"os"
	"strings"
	"time"
)

// Provider support is data only. Plugin JavaScript never receives credentials.
type PluginProviderSupport struct {
	Protocol int    `json:"protocol"`
	Catalog  string `json:"catalog"`
}
type supportProvider struct {
	Host            string   `json:"host,omitempty"`
	WorkspaceRegion string   `json:"workspaceRegion,omitempty"`
	Paths           []string `json:"paths"`
}
type supportTier struct {
	MaxInput         int      `json:"maxInput"`
	PriceIn          float64  `json:"priceIn"`
	PriceInCacheHit  *float64 `json:"priceInCacheHit,omitempty"`
	PriceOut         float64  `json:"priceOut"`
	PriceOutThinking *float64 `json:"priceOutThinking,omitempty"`
}
type supportModel struct {
	Names []string      `json:"names"`
	Tiers []supportTier `json:"tiers"`
}
type supportPeak struct {
	Timezone    string   `json:"timezone"`
	Multiplier  float64  `json:"multiplier"`
	Hours       [][2]int `json:"hours"`
	HolidayYear int      `json:"holidayYear"`
	Holidays    []string `json:"holidays"`
}
type supportBalance struct {
	Endpoint  string `json:"endpoint"`
	Auth      string `json:"auth"`
	TotalPath string `json:"totalPath"`
	Format    string `json:"format"`
}
type supportCatalog struct {
	Protocol  int               `json:"protocol"`
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Revision  string            `json:"revision"`
	Currency  string            `json:"currency"`
	Sources   []string          `json:"sources"`
	Providers []supportProvider `json:"providers"`
	Models    []supportModel    `json:"models"`
	Peak      *supportPeak      `json:"peak,omitempty"`
	Balance   *supportBalance   `json:"balance,omitempty"`
}
type supportQuote struct {
	Tier      supportTier
	Peak      bool
	Estimated bool
	Source    string
	Name      string
	Revision  string
}

func validSupportPrice(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 && v <= 1e9
}
func validateSupportCatalog(raw []byte) (*supportCatalog, error) {
	var c supportCatalog
	if len(raw) == 0 || len(raw) > 128<<10 || json.Unmarshal(raw, &c) != nil || c.Protocol != 1 || !pluginIDPattern.MatchString(c.ID) || c.Name == "" || len(c.Name) > 128 || c.Revision == "" || len(c.Revision) > 80 || c.Currency != "CNY" || len(c.Providers) == 0 || len(c.Providers) > 16 || len(c.Models) > 128 || len(c.Sources) > 16 {
		return nil, errors.New("invalid provider support catalog")
	}
	for _, p := range c.Providers {
		if (p.Host == "" && p.WorkspaceRegion != "cn-beijing") || (p.Host != "" && p.WorkspaceRegion != "") || strings.ContainsAny(p.Host, "/:@ \\?#*") || len(p.Paths) == 0 || len(p.Paths) > 16 {
			return nil, errors.New("invalid provider match")
		}
		for _, path := range p.Paths {
			if path != "" && (!strings.HasPrefix(path, "/") || strings.ContainsAny(path, "?#\\") || strings.Contains(path, "..")) {
				return nil, errors.New("invalid provider path")
			}
		}
	}
	names := map[string]bool{}
	for _, m := range c.Models {
		if len(m.Names) == 0 || len(m.Names) > 16 || len(m.Tiers) == 0 || len(m.Tiers) > 32 {
			return nil, errors.New("invalid pricing model")
		}
		for _, n := range m.Names {
			n = strings.ToLower(n)
			if n == "" || len(n) > 128 || names[n] {
				return nil, errors.New("duplicate or invalid pricing model")
			}
			names[n] = true
		}
		prev := 0
		for i, t := range m.Tiers {
			if t.MaxInput < 0 || (t.MaxInput == 0 && i != len(m.Tiers)-1) || (t.MaxInput > 0 && t.MaxInput <= prev) || !validSupportPrice(t.PriceIn) || !validSupportPrice(t.PriceOut) || (t.PriceOutThinking != nil && !validSupportPrice(*t.PriceOutThinking)) || (t.PriceInCacheHit != nil && !validSupportPrice(*t.PriceInCacheHit)) {
				return nil, errors.New("invalid pricing tier")
			}
			prev = t.MaxInput
		}
	}
	if p := c.Peak; p != nil {
		if _, err := time.LoadLocation(p.Timezone); err != nil || p.Multiplier < 1 || p.Multiplier > 100 || len(p.Hours) > 8 || len(p.Holidays) > 366 {
			return nil, errors.New("invalid pricing calendar")
		}
		for _, h := range p.Hours {
			if h[0] < 0 || h[1] > 24 || h[0] >= h[1] {
				return nil, errors.New("invalid peak hours")
			}
		}
		for _, d := range p.Holidays {
			if _, err := time.Parse("2006-01-02", d); err != nil {
				return nil, err
			}
		}
	}
	if b := c.Balance; b != nil {
		u, e := url.Parse(b.Endpoint)
		if e != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || u.RawQuery != "" || u.Fragment != "" || (b.Auth != "model-bearer" && b.Auth != "aliyun-rpc") {
			return nil, errors.New("invalid balance endpoint")
		}
		if b.Auth == "aliyun-rpc" && (b.Endpoint != "https://business.aliyuncs.com/" || b.Format != "aliyun-account-balance") {
			return nil, errors.New("invalid billing RPC endpoint")
		}
		if b.Auth == "model-bearer" {
			match := false
			for _, p := range c.Providers {
				match = match || strings.EqualFold(p.Host, u.Hostname())
			}
			if !match || b.Format != "balance-infos" {
				return nil, errors.New("balance credentials must remain on provider host")
			}
		}
	}
	return &c, nil
}
func supportMatches(c *supportCatalog, base string) bool {
	u, e := url.Parse(base)
	if e != nil || u.Scheme != "https" || u.User != nil || (u.Port() != "" && u.Port() != "443") || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	for _, p := range c.Providers {
		matched := strings.EqualFold(u.Hostname(), p.Host)
		if p.WorkspaceRegion == "cn-beijing" {
			host := strings.ToLower(u.Hostname())
			suffix := ".cn-beijing.maas.aliyuncs.com"
			label := strings.TrimSuffix(host, suffix)
			matched = strings.HasSuffix(host, suffix) && label != "" && !strings.ContainsAny(label, ".:")
		}
		if !matched {
			continue
		}
		for _, path := range p.Paths {
			if strings.TrimRight(u.Path, "/") == strings.TrimRight(path, "/") {
				return true
			}
		}
	}
	return false
}

// Caller holds a.mu. Invalid, disabled, failed or ambiguous extensions do not apply.
func (a *App) providerSupportLocked(base string) (*supportCatalog, *PluginManifest) {
	var selected *supportCatalog
	var owner *PluginManifest
	for i := range a.pluginRegistry.Plugins {
		p := &a.pluginRegistry.Plugins[i]
		s := p.ProviderSupport
		if !p.Enabled || a.pluginError(p.ID) != "" || s == nil || s.Protocol != 1 || !pluginIDPattern.MatchString(p.ID) || !routerCatalogName.MatchString(s.Catalog) {
			continue
		}
		root, e := os.OpenRoot(a.pluginsPath)
		if e != nil {
			continue
		}
		pr, e := root.OpenRoot(p.ID)
		root.Close()
		if e != nil {
			continue
		}
		f, e := pr.Open(s.Catalog)
		pr.Close()
		if e != nil {
			continue
		}
		raw, e := io.ReadAll(io.LimitReader(f, (128<<10)+1))
		f.Close()
		if e != nil {
			continue
		}
		c, e := validateSupportCatalog(raw)
		if e != nil || !supportMatches(c, base) {
			continue
		}
		if owner != nil {
			return nil, nil
		}
		selected, owner = c, p
	}
	return selected, owner
}
func (a *App) providerQuote(base, model string, input int, at time.Time) *supportQuote {
	a.mu.Lock()
	c, p := a.providerSupportLocked(base)
	sourceID := ""
	if p != nil {
		sourceID = p.ID
	}
	a.mu.Unlock()
	if c == nil {
		return nil
	}
	for _, m := range c.Models {
		matched := false
		for _, n := range m.Names {
			matched = matched || strings.EqualFold(n, model)
		}
		if !matched {
			continue
		}
		for _, t := range m.Tiers {
			if t.MaxInput != 0 && input > t.MaxInput {
				continue
			}
			q := &supportQuote{Tier: t, Source: sourceID, Name: c.Name, Revision: c.Revision}
			if peak := c.Peak; peak != nil {
				loc, _ := time.LoadLocation(peak.Timezone)
				local := at.In(loc)
				q.Estimated = local.Year() != peak.HolidayYear
				holiday := false
				for _, d := range peak.Holidays {
					holiday = holiday || local.Format("2006-01-02") == d
				}
				if local.Weekday() != time.Saturday && local.Weekday() != time.Sunday && !holiday {
					for _, h := range peak.Hours {
						q.Peak = q.Peak || (local.Hour() >= h[0] && local.Hour() < h[1])
					}
				}
				if q.Peak {
					q.Tier.PriceIn *= peak.Multiplier
					q.Tier.PriceOut *= peak.Multiplier
					if t.PriceOutThinking != nil {
						v := *t.PriceOutThinking * peak.Multiplier
						q.Tier.PriceOutThinking = &v
					}
					if t.PriceInCacheHit != nil {
						hit := *t.PriceInCacheHit * peak.Multiplier
						q.Tier.PriceInCacheHit = &hit
					}
				}
			}
			return q
		}
	}
	return nil
}
func validatePluginProviderSupport(s *PluginProviderSupport, files map[string][]byte) error {
	if s == nil {
		return nil
	}
	if s.Protocol != 1 || !routerCatalogName.MatchString(s.Catalog) {
		return errors.New("invalid provider support declaration")
	}
	_, e := validateSupportCatalog(files[s.Catalog])
	return e
}

func billingScope(provider, key, model string) string {
	b, _ := json.Marshal([]string{provider, key, model})
	return string(b)
}
func applyBillingIdentity(u *TokenUsage, cfg Settings) {
	if cfg.billingIsolated {
		u.ProviderID = ""
		u.KeyID = ""
		u.KeyLabel = ""
		return
	}
	pid := "default"
	for _, m := range cfg.Models {
		if m.ID == cfg.Model && m.ProviderID != "" {
			pid = m.ProviderID
			break
		}
	}
	for _, p := range cfg.ModelProviders {
		if p.ID == pid && strings.TrimRight(p.BaseURL, "/") == strings.TrimRight(cfg.BaseURL, "/") {
			u.ProviderID = p.ID
			u.KeyID = p.ActiveKeyID
			for _, k := range p.Keys {
				if k.ID == p.ActiveKeyID {
					u.KeyLabel = k.Label
				}
			}
			break
		}
	}
}
func (a *App) billingTargetLocked(pid, kid, model string) (ModelProvider, string, error) {
	if pid == "" {
		p, ok := a.activeModelProviderLocked()
		if !ok {
			return ModelProvider{}, "", errors.New("provider unavailable")
		}
		pid = p.ID
	}
	for _, p := range a.settings.ModelProviders {
		if p.ID != pid {
			continue
		}
		if kid == "" {
			kid = p.ActiveKeyID
		}
		if len(p.Keys) == 0 && kid == "" {
			return p, "", nil
		}
		for _, k := range p.Keys {
			if k.ID == kid {
				return p, kid, nil
			}
		}
	}
	return ModelProvider{}, "", errors.New("provider key unavailable")
}

func (a *App) resolvedPricing(base, model, pid, kid string, defaultOnly bool) map[string]any {
	quote := a.providerQuote(base, model, 0, time.Now())
	a.mu.Lock()
	cat, _ := a.providerSupportLocked(base)
	standards := []string{}
	seenStandards := map[string]bool{}
	for _, provider := range a.settings.ModelProviders {
		if catalog, owner := a.providerSupportLocked(provider.BaseURL); catalog != nil && owner != nil && !seenStandards[owner.ID] {
			standards = append(standards, catalog.Name)
			seenStandards[owner.ID] = true
		}
	}
	balanceAuth := ""
	if cat != nil && cat.Balance != nil {
		balanceAuth = cat.Balance.Auth
	}
	a.mu.Unlock()
	a.tokenStatsMu.Lock()
	defer a.tokenStatsMu.Unlock()
	entry, configured := a.pricing.Rates[model]
	if !configured {
		entry = a.pricing.Default
	}
	scoped, scopedOK := a.pricing.KeyRates[billingScope(pid, kid, model)]
	if !scopedOK && kid != "" {
		scoped, scopedOK = a.pricing.KeyRates[billingScope(pid, "", model)]
	}
	if scopedOK && quote == nil {
		entry = scoped
		configured = true
	}
	if defaultOnly {
		entry = a.pricing.Default
		configured = true
		quote = nil
	}
	hit, miss := 0.0, 0.0
	peak := false
	supporter := ""
	if quote != nil {
		entry.PriceIn, entry.PriceOut = quote.Tier.PriceIn, quote.Tier.PriceOut
		configured = true
		miss = entry.PriceIn
		peak = quote.Peak
		supporter = quote.Name
		if quote.Tier.PriceInCacheHit != nil {
			hit = *quote.Tier.PriceInCacheHit
		}
	}
	rates := map[string]Pricing{}
	for k, v := range a.pricing.Rates {
		rates[k] = v
	}
	keyRates := map[string]Pricing{}
	scopes := []map[string]any{}
	for k, v := range a.pricing.KeyRates {
		keyRates[k] = v
		var parts []string
		if json.Unmarshal([]byte(k), &parts) == nil && len(parts) == 3 {
			scopes = append(scopes, map[string]any{"providerId": parts[0], "keyId": parts[1], "model": parts[2], "priceIn": v.PriceIn, "priceOut": v.PriceOut})
		}
	}
	return map[string]any{"priceIn": entry.PriceIn, "priceOut": entry.PriceOut, "model": model, "providerId": pid, "keyId": kid, "defaulted": !configured, "rates": rates, "keyRates": keyRates, "scopes": scopes, "default": a.pricing.Default, "official": quote != nil, "supporter": supporter, "balanceAuth": balanceAuth, "standards": standards, "priceInCacheHit": hit, "priceInCacheMiss": miss, "peak": peak}
}
