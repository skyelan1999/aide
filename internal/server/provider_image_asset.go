package server

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Only the provider-owned object-storage origins used by DashScope may be
// fetched. No authorization header or redirects are forwarded to image hosts.
var qwenImageAssetHost = regexp.MustCompile(`^[a-z0-9-]+\.oss-(accelerate|cn-[a-z0-9-]+|ap-[a-z0-9-]+|us-[a-z0-9-]+)\.aliyuncs\.com$`)

func (a *App) saveQwenImageAsset(ctx context.Context, markdown string, tasks ...*Task) (string, error) {
	start := strings.Index(markdown, "![生成图片](<")
	if start < 0 {
		return markdown, fmt.Errorf("图片链接不存在")
	}
	rest := markdown[start+len("![生成图片](<"):]
	end := strings.Index(rest, ">)")
	if end < 0 {
		return markdown, fmt.Errorf("图片链接不存在")
	}
	link := rest[:end]
	u, err := url.Parse(link)
	if err != nil || u.Scheme != "https" || u.User != nil || (u.Port() != "" && u.Port() != "443") || !qwenImageAssetHost.MatchString(strings.ToLower(u.Hostname())) {
		return markdown, fmt.Errorf("图片存储地址不支持自动保存")
	}
	if a.workspaceMode() == "ssh" {
		return markdown, fmt.Errorf("自动保存暂不支持远程工作区")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return markdown, err
	}
	client := &http.Client{Timeout: 90 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return markdown, fmt.Errorf("下载图片失败")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return markdown, fmt.Errorf("下载图片返回 HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (20<<20)+1))
	if err != nil || len(data) > 20<<20 {
		return markdown, fmt.Errorf("图片超过保存大小限制")
	}
	ext := ""
	switch http.DetectContentType(data) {
	case "image/png":
		ext = ".png"
	case "image/jpeg":
		ext = ".jpg"
	case "image/webp":
		ext = ".webp"
	default:
		return markdown, fmt.Errorf("返回内容不是支持的图片")
	}
	filename := "generated/images/qwen-" + newID() + ext
	a.filesMu.Lock()
	defer a.filesMu.Unlock()
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(tasks) > 0 && (tasks[0].WorkspaceID != a.wsID() || tasks[0].WorkspaceRev != a.wsRevision) {
		return markdown, fmt.Errorf("生成期间工作区已变化；原图片记录保留，未写入新工作区")
	}
	if err := putText(a.workspace, filename, data); err != nil {
		return markdown, fmt.Errorf("保存图片失败")
	}
	return "![生成图片](" + filename + ")\n\n[下载图片](" + filename + ") · 已保存到工作区。", nil
}
