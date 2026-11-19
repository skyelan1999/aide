package server

import (
	"encoding/json"
	"errors"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	miMoMeteredHost = "api.xiaomimimo.com"
	moonshotHost    = "api.moonshot.cn"
	miMoMetered     = "xiaomi-mimo"
	miMoPlan        = "xiaomi-mimo-token-plan"
	moonshotBalance = "moonshot"
)

func platformBalanceHost(adapter string) string {
	switch adapter {
	case miMoMetered:
		return miMoMeteredHost
	case moonshotBalance:
		return moonshotHost
	default:
		return ""
	}
}

// Account adapters are optional catalog metadata. The host owns their fixed
// endpoints and response formats. Existing hosts can still read the prices.
func validatePlatformAccount(c *supportCatalog) error {
	host := platformBalanceHost(c.AccountBalance)
	if host == "" || c.Balance != nil || len(c.Providers) != 1 {
		return errors.New("invalid provider account adapter")
	}
	provider := c.Providers[0]
	if provider.Host != host || provider.Scheme != "" || provider.Port != "" ||
		provider.WorkspaceRegion != "" || len(provider.Paths) != 1 || provider.Paths[0] != "/v1" {
		return errors.New("provider account adapter requires its official HTTPS origin")
	}
	return nil
}

func platformSupportForBase(c *supportCatalog, base string) *supportCatalog {
	if supportMatches(c, base) {
		switch c.AccountBalance {
		case miMoMetered:
			c.Balance = &supportBalance{Endpoint: "https://" + miMoMeteredHost + "/v1/platform/balance", Auth: "model-bearer", Format: miMoMetered}
		case moonshotBalance:
			c.Balance = &supportBalance{Endpoint: "https://" + moonshotHost + "/v1/users/me/balance", Auth: "model-bearer", Format: moonshotBalance}
		}
		return c
	}
	if c.AccountBalance != miMoMetered {
		return nil
	}
	u, err := url.Parse(base)
	if err != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" ||
		(u.Port() != "" && u.Port() != "443") || strings.TrimRight(u.Path, "/") != "/v1" {
		return nil
	}
	host := strings.ToLower(u.Hostname())
	switch host {
	case "token-plan-cn.xiaomimimo.com", "token-plan-sgp.xiaomimimo.com", "token-plan-ams.xiaomimimo.com":
		// A subscription reports credits. It does not inherit metered prices.
		c.Models = nil
		c.Currency = ""
		c.Balance = &supportBalance{Endpoint: "https://" + host + "/v1/platform/token_plan", Auth: "model-bearer", Format: miMoPlan}
		return c
	default:
		return nil
	}
}

func parsePlatformAmount(value string) (float64, error) {
	amount, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(amount) || math.IsInf(amount, 0) || math.Abs(amount) > 1e12 {
		return 0, errors.New("invalid provider account amount")
	}
	return amount, nil
}

func normalizeMiMoBalance(raw []byte) (map[string]any, error) {
	var input struct {
		Object    string `json:"object"`
		Available *bool  `json:"is_available"`
		Infos     []struct {
			Currency string `json:"currency"`
			Cash     string `json:"cash_balance"`
			Gift     string `json:"gift_balance"`
			Frozen   string `json:"frozen_balance"`
			Total    string `json:"total_available"`
		} `json:"balance_infos"`
	}
	if json.Unmarshal(raw, &input) != nil || input.Object != "balance" || input.Available == nil || len(input.Infos) != 1 {
		return nil, errors.New("invalid MiMo balance response")
	}
	row := input.Infos[0]
	if !validSupportCurrency(row.Currency) {
		return nil, errors.New("unsupported balance currency")
	}
	total, totalErr := parsePlatformAmount(row.Total)
	frozen, frozenErr := parsePlatformAmount(row.Frozen)
	_, cashErr := parsePlatformAmount(row.Cash)
	_, giftErr := parsePlatformAmount(row.Gift)
	if totalErr != nil || frozenErr != nil || cashErr != nil || giftErr != nil || frozen < 0 {
		return nil, errors.New("invalid MiMo balance amount")
	}
	available := math.Round((total-frozen)*100) / 100
	return map[string]any{"is_available": *input.Available, "balance_infos": []any{map[string]any{
		"currency": row.Currency, "total_balance": strconv.FormatFloat(available, 'f', 2, 64),
		"available_cash_balance": row.Cash, "granted_balance": row.Gift,
	}}}, nil
}

func normalizeMoonshotBalance(raw []byte) (map[string]any, error) {
	var input struct {
		Code   *int `json:"code"`
		Status bool `json:"status"`
		Data   struct {
			Available *float64 `json:"available_balance"`
			Voucher   *float64 `json:"voucher_balance"`
			Cash      *float64 `json:"cash_balance"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &input) != nil || input.Code == nil || *input.Code != 0 || !input.Status {
		return nil, errors.New("invalid Kimi balance response")
	}
	for _, value := range []*float64{input.Data.Available, input.Data.Voucher, input.Data.Cash} {
		if value == nil || math.IsNaN(*value) || math.IsInf(*value, 0) || math.Abs(*value) > 1e12 {
			return nil, errors.New("invalid Kimi balance amount")
		}
	}
	return map[string]any{"is_available": *input.Data.Available > 0, "balance_infos": []any{map[string]any{
		"currency": "CNY", "total_balance": strconv.FormatFloat(*input.Data.Available, 'f', -1, 64),
		"available_cash_balance": strconv.FormatFloat(*input.Data.Cash, 'f', -1, 64),
		"granted_balance":        strconv.FormatFloat(*input.Data.Voucher, 'f', -1, 64),
	}}}, nil
}

func normalizeMiMoPlan(raw []byte) (map[string]any, error) {
	var input struct {
		Object string `json:"object"`
		Period struct {
			End string `json:"end_at"`
		} `json:"period"`
		Quota struct {
			Total     *int64 `json:"total_credits"`
			Remaining *int64 `json:"remaining_credits"`
		} `json:"quota"`
	}
	if json.Unmarshal(raw, &input) != nil || input.Object != "token_plan" ||
		input.Quota.Total == nil || input.Quota.Remaining == nil ||
		*input.Quota.Total < 0 || *input.Quota.Remaining < 0 || *input.Quota.Remaining > *input.Quota.Total {
		return nil, errors.New("invalid MiMo Token Plan response")
	}
	remaining, total := *input.Quota.Remaining, *input.Quota.Total
	percent := 0.0
	if total > 0 {
		percent = 100 * float64(remaining) / float64(total)
	}
	row := map[string]any{"id": "mimo-token-plan/primary", "label": "MiMo Token Plan", "window": "primary",
		"remainingPercent": percent, "remainingCredits": remaining, "totalCredits": total}
	if end, err := time.Parse("2006-01-02 15:04:05", input.Period.End); err == nil && end.Unix() > 0 {
		row["resetsAt"] = end.Unix()
	}
	return map[string]any{"kind": "quota", "available": true, "quota_infos": []any{row}, "balance_infos": []any{}}, nil
}
