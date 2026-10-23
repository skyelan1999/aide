package server

import (
	"net/url"
	"strings"
)

func isBailianEndpoint(base string) bool {
	u, err := url.Parse(base)
	if err != nil {
		return false
	}
	h := strings.ToLower(u.Hostname())
	return h == "dashscope.aliyuncs.com" || h == "dashscope-intl.aliyuncs.com" || h == "dashscope-us.aliyuncs.com" || strings.HasSuffix(h, ".maas.aliyuncs.com")
}

func isQwenRealtime(model string) bool {
	m := strings.ToLower(model)
	return (strings.HasPrefix(m, "qwen3.8-omni-") || strings.HasPrefix(m, "qwen3.5-omni-") || strings.HasPrefix(m, "qwen3-omni-")) && strings.Contains(m, "realtime")
}

func qwenModernVision(model string) bool {
	m := strings.ToLower(model)
	for _, prefix := range []string{"qwen3.8-max", "qwen3.8-flash", "qwen3.7-plus", "qwen3.7-flash", "qwen3.6-plus", "qwen3.6-flash", "qwen3.5-plus", "qwen3.5-flash", "qwen3.8-omni", "qwen3.5-omni", "qwen3-omni"} {
		if m == prefix || strings.HasPrefix(m, prefix+"-") {
			return true
		}
	}
	return false
}

// Apply after routing adaptation, so an offline router profile cannot restore
// another vendor's reasoning syntax. Third-party/self-hosted endpoints retain
// their existing contract rather than inheriting DashScope-specific fields.
func adaptQwenChatBody(cfg Settings, body map[string]any) {
	if !isBailianEndpoint(cfg.BaseURL) {
		return
	}
	m := strings.ToLower(cfg.Model)
	if !strings.HasPrefix(m, "qwen") {
		return
	}
	delete(body, "thinking")
	delete(body, "reasoning_effort")
	if strings.Contains(m, "omni") {
		body["modalities"] = []string{"text"}
	}
	if strings.HasPrefix(m, "qwen3.8-omni-flash") {
		body["preserve_thinking"] = false // Aide history stores visible answers, not hidden reasoning.
		switch cfg.ReasoningEffort {
		case "off":
			body["reasoning_effort"] = "none"
		case "low", "medium", "high":
			body["reasoning_effort"] = cfg.ReasoningEffort
		}
		return
	}
	hybrid := strings.HasPrefix(m, "qwen3.") || strings.HasPrefix(m, "qwen3-vl-") || strings.HasPrefix(m, "qwen3-omni-") || m == "qwen-plus" || strings.HasPrefix(m, "qwen-plus-") || m == "qwen-flash" || strings.HasPrefix(m, "qwen-flash-")
	if strings.Contains(m, "coder") || strings.Contains(m, "realtime") {
		hybrid = false
	}
	if !hybrid {
		return
	}
	// These exact published models cannot disable thinking.
	onlyThinking := m == "qwen3.8-2.4t-a95b" || m == "qwen3.7-max-preview" || m == "qwen3.7-max-2026-05-17"
	if onlyThinking {
		body["enable_thinking"] = true
		return
	}
	switch cfg.ReasoningEffort {
	case "off":
		body["enable_thinking"] = false
	case "low", "medium", "high":
		body["enable_thinking"] = true
	}
}
