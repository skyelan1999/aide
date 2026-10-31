package server

import (
	"encoding/json"
	"errors"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Local origin matching is opt-in plugin metadata, never inferred from a name.
func validateLocalSupportProvider(p supportProvider) error {
	if p.Scheme == "" && p.Port == "" {
		return nil
	}
	n, err := strconv.Atoi(p.Port)
	if p.Scheme != "http" || p.WorkspaceRegion != "" || err != nil || n < 1 || n > 65535 || strconv.Itoa(n) != p.Port {
		return errors.New("invalid local provider origin")
	}
	switch strings.ToLower(p.Host) {
	case "localhost", "127.0.0.1", "host.docker.internal":
		return nil
	}
	return errors.New("unencrypted provider must be an explicit local origin")
}

func supportProviderOriginMatches(p supportProvider, u *url.URL) bool {
	if p.Scheme == "" {
		return u.Scheme == "https" && (u.Port() == "" || u.Port() == "443")
	}
	return validateLocalSupportProvider(p) == nil && u.Scheme == p.Scheme && u.Port() == p.Port
}

var quotaPath = regexp.MustCompile(`^/[a-zA-Z0-9_/-]+$`)

func validateSupportQuotaPath(b *supportBalance) error {
	if b.Endpoint != "" || b.Auth != "model-bearer" || b.Format != "quota-windows" || len(b.Path) > 512 || !quotaPath.MatchString(b.Path) || strings.Contains(b.Path, "//") {
		return errors.New("invalid plugin quota path")
	}
	return nil
}

func normalizeQuotaWindows(raw []byte) (map[string]any, error) {
	type window struct {
		Used     *float64 `json:"usedPercent"`
		Duration *float64 `json:"windowDurationMins"`
		Reset    *int64   `json:"resetsAt"`
	}
	var input struct {
		Available bool   `json:"available"`
		Checked   string `json:"checkedAt"`
		Buckets   []struct {
			ID        string  `json:"id"`
			Label     string  `json:"label"`
			Primary   *window `json:"primary"`
			Secondary *window `json:"secondary"`
		} `json:"buckets"`
	}
	if json.Unmarshal(raw, &input) != nil || len(input.Buckets) > 32 {
		return nil, errors.New("invalid plugin quota response")
	}
	rows := []map[string]any{}
	clean := func(s string) string {
		s = strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return -1
			}
			return r
		}, s)
		r := []rune(s)
		if len(r) > 120 {
			r = r[:120]
		}
		return string(r)
	}
	if input.Available {
		for _, b := range input.Buckets {
			id, label := clean(b.ID), clean(b.Label)
			if id == "" {
				continue
			}
			if label == "" {
				label = id
			}
			for _, item := range []struct {
				name string
				w    *window
			}{{"primary", b.Primary}, {"secondary", b.Secondary}} {
				w := item.w
				if w == nil || w.Used == nil || math.IsNaN(*w.Used) || math.IsInf(*w.Used, 0) || *w.Used < 0 {
					continue
				}
				row := map[string]any{"id": id + "/" + item.name, "label": label, "window": item.name, "remainingPercent": math.Max(0, math.Min(100, 100-*w.Used))}
				if w.Duration != nil && *w.Duration > 0 && *w.Duration <= 525600 && !math.IsNaN(*w.Duration) && !math.IsInf(*w.Duration, 0) {
					row["windowDurationMins"] = *w.Duration
				}
				if w.Reset != nil && *w.Reset > 0 && *w.Reset < 253402300800 {
					row["resetsAt"] = *w.Reset
				}
				rows = append(rows, row)
			}
		}
	}
	out := map[string]any{"kind": "quota", "available": len(rows) > 0, "quota_infos": rows, "balance_infos": []any{}}
	if at, err := time.Parse(time.RFC3339Nano, input.Checked); err == nil {
		out["checkedAt"] = at.UTC().Format(time.RFC3339Nano)
	}
	return out, nil
}
