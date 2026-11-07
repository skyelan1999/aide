package server

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"strings"
)

// browserReadApprovalKey is set only after the user answers Aide's bound
// confirmation. It is never populated from model-provided tool arguments.
type browserReadApprovalKey struct{}

func browserReadTarget(raw string) (string, string, error) {
	if len(raw) == 0 || len(raw) > 4096 {
		return "", "", errors.New("请提供有效的网页 URL")
	}
	u, err := url.Parse(raw)
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Host == "" || u.Opaque != "" {
		return "", "", errors.New("仅允许不含账号信息的 HTTP/HTTPS 网页")
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if host == "" || strings.ContainsAny(host, " \t\r\n/\\") || strings.Contains(host, "*") {
		return "", "", errors.New("网页域名无效")
	}
	return u.String(), host, nil
}

func browserHostMayBeApproved(host string) bool {
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback()
	}
	return true
}

func browserHostAllowed(host string, settings map[string]any) bool {
	var hosts []string
	switch list := settings["allowedHosts"].(type) {
	case []any:
		for _, item := range list {
			if value, ok := item.(string); ok {
				hosts = append(hosts, value)
			}
		}
	case []string:
		hosts = list
	}
	for _, raw := range hosts {
		rule := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(raw), "."))
		if rule == host || (strings.HasPrefix(rule, "*.") && host != rule[2:] && strings.HasSuffix(host, rule[1:])) {
			return true
		}
	}
	return false
}

func browserHostsWith(settings map[string]any, host string) map[string]any {
	copySettings := make(map[string]any, len(settings)+1)
	for key, value := range settings {
		copySettings[key] = value
	}
	hosts := make([]any, 0)
	switch list := settings["allowedHosts"].(type) {
	case []any:
		hosts = append(hosts, list...)
	case []string:
		for _, item := range list {
			hosts = append(hosts, item)
		}
	}
	if !browserHostAllowed(host, settings) {
		hosts = append(hosts, host)
	}
	copySettings["allowedHosts"] = hosts
	return copySettings
}

func (a *App) browserReadApproval(ctx context.Context, task *Task, args map[string]any) (context.Context, string) {
	raw, _ := args["url"].(string)
	urlText, host, err := browserReadTarget(raw)
	if err != nil {
		return ctx, "错误：" + err.Error()
	}
	a.mu.Lock()
	allowed := false
	for _, plugin := range a.pluginRegistry.Plugins {
		if plugin.ID == "browser-control" {
			allowed = browserHostAllowed(host, pluginSettingsOrEmpty(plugin.Settings))
			break
		}
	}
	a.mu.Unlock()
	if allowed {
		return ctx, ""
	}
	if !browserHostMayBeApproved(host) {
		return ctx, "错误：不能为本机或私有地址申请浏览器读取授权"
	}
	question, _ := json.Marshal(map[string]any{
		"type": "confirm", "approvalKind": "browser-site", "url": urlText, "host": host,
		"question": "Aide 将使用网页读取工具访问以下网址并读取页面正文。允许访问域名「" + host + "」吗？\n" + urlText,
	})
	switch a.awaitUserAnswer(ctx, task, question) {
	case "确认", "记住并允许":
		return context.WithValue(ctx, browserReadApprovalKey{}, urlText), ""
	default:
		return ctx, "用户未授权访问网站「" + host + "」；网页未读取。"
	}
}

// Called with a.mu held by the answer endpoint, before the answer is delivered.
func (a *App) rememberBrowserHostLocked(rawURL string) error {
	_, host, err := browserReadTarget(rawURL)
	if err != nil {
		return err
	}
	if !browserHostMayBeApproved(host) {
		return errors.New("不能记住本机或私有地址的网站授权")
	}
	for i := range a.pluginRegistry.Plugins {
		plugin := &a.pluginRegistry.Plugins[i]
		if plugin.ID != "browser-control" || !plugin.Enabled {
			continue
		}
		if browserHostAllowed(host, pluginSettingsOrEmpty(plugin.Settings)) {
			return nil
		}
		previous := plugin.Settings
		plugin.Settings = browserHostsWith(pluginSettingsOrEmpty(previous), host)
		if err := a.savePluginRegistry(); err != nil {
			plugin.Settings = previous
			return err
		}
		return nil
	}
	return errors.New("浏览器插件未启用，未保存网站授权")
}
