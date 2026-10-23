package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type mediaProviderError struct {
	Status   int
	Category string
}

func (e *mediaProviderError) Error() string {
	labels := map[string]string{"quota": "账户额度或配额不足", "rate_limit": "请求频率受限", "auth": "密钥无效或访问权限不足", "activation": "模型未开通", "parameter": "请求参数不兼容", "upstream": "服务商暂时故障"}
	return fmt.Sprintf("媒体 API HTTP %d：%s；未自动重新生成", e.Status, labels[e.Category])
}
func classifyMediaError(status int, body []byte) error {
	var data struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Error   struct {
			Code    string `json:"code"`
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(body, &data)
	text := strings.ToLower(data.Code + " " + data.Message + " " + data.Error.Code + " " + data.Error.Type + " " + data.Error.Message)
	category := "parameter"
	switch {
	case strings.Contains(text, "quota") || strings.Contains(text, "credit_balance") || strings.Contains(text, "arrear") || strings.Contains(text, "insufficient_balance"):
		category = "quota"
	case strings.Contains(text, "not activated") || strings.Contains(text, "notactivated"):
		category = "activation"
	case status == 401 || status == 403:
		category = "auth"
	case status == 429:
		category = "rate_limit"
	case status >= 500:
		category = "upstream"
	}
	return &mediaProviderError{status, category}
}

type mediaHealth struct {
	Category string
	Until    time.Time
}

func (a *App) recordMediaHealth(cfg Settings, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	key := mediaIdentity(cfg)
	if err == nil {
		delete(a.mediaHealth, key)
		return
	}
	var failure *mediaProviderError
	if !errors.As(err, &failure) {
		return
	}
	delay := 30 * time.Second
	switch failure.Category {
	case "quota", "activation", "auth":
		delay = 5 * time.Minute
	case "parameter":
		return
	}
	if a.mediaHealth == nil {
		a.mediaHealth = map[string]mediaHealth{}
	}
	// Expired entries are metadata only, bounded even with frequently rotated keys.
	for k, v := range a.mediaHealth {
		if time.Now().After(v.Until) {
			delete(a.mediaHealth, k)
		}
	}
	if len(a.mediaHealth) < 128 {
		a.mediaHealth[key] = mediaHealth{failure.Category, time.Now().Add(delay)}
	}
}
