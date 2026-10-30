package server

import (
	"net/url"
	"strings"
)

// Match the endpoint, not an editable display name or an OpenAI-compatible protocol.
func isOpenAIEndpoint(base string) bool {
	u, err := url.Parse(strings.TrimSpace(base))
	return err == nil && strings.EqualFold(u.Hostname(), "api.openai.com")
}

func isOpenAIReasoningModel(model string) bool {
	m := strings.ToLower(strings.TrimSpace(model))
	for _, family := range []string{"o1", "o3", "o4", "gpt-5", "gpt-6"} {
		if m == family || strings.HasPrefix(m, family+"-") || strings.HasPrefix(m, family+".") {
			return true
		}
	}
	return false
}

func openAIRequiresReasoning(model string) bool {
	m := strings.ToLower(strings.TrimSpace(model))
	for _, family := range []string{"gpt-6.1-sol", "gpt-6-astra"} {
		if m == family || strings.HasPrefix(m, family+"-") {
			return true
		}
	}
	return false
}

// These are application choices (off maps to the provider's none value).
// Only documented model families expose the additional xhigh/max choices.
func modelReasoningEfforts(cfg Settings) []string {
	if modelCapabilityStatus(cfg, "reasoning") == "unsupported" {
		return []string{"auto"}
	}
	if isOpenAIEndpoint(cfg.BaseURL) {
		if openAIRequiresReasoning(cfg.Model) {
			return []string{"auto", "low", "medium", "high", "xhigh", "max"}
		}
		m := strings.ToLower(cfg.Model)
		for _, family := range []string{"gpt-6-sol", "gpt-6-luna"} {
			if m == family || strings.HasPrefix(m, family+"-") {
				return []string{"auto", "off", "low", "medium", "high", "xhigh", "max"}
			}
		}
		if !isOpenAIReasoningModel(cfg.Model) && modelCapabilityStatus(cfg, "reasoning") != "supported" {
			return []string{"auto"}
		}
	}
	return []string{"auto", "off", "low", "medium", "high"}
}

func adaptedReasoningEffort(cfg Settings, effort string) string {
	for _, option := range modelReasoningEfforts(cfg) {
		if option == effort {
			return effort
		}
	}
	return "auto"
}
