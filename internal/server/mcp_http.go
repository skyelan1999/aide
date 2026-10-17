package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// MCP HTTP sessions are short-lived, like our stdio sessions: discovery and
// calls always recheck the server, then close the authenticated session.
type mcpHTTP struct {
	ctx           context.Context
	cancel        context.CancelFunc
	client        *http.Client
	transport     *http.Transport
	endpoint      string
	token         string
	session       string
	version       string
	dockerGateway bool
	nextID        int
	beforePost    func() error
}

func validateMCPHTTPURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Fragment != "" || u.RawQuery != "" || (u.Scheme != "http" && u.Scheme != "https") {
		return errors.New("MCP HTTP 地址须为完整 http/https 端点；凭据使用独立令牌字段，不支持查询参数或片段")
	}
	if u.Scheme == "http" {
		host := strings.ToLower(u.Hostname())
		ip := net.ParseIP(host)
		if host != "localhost" && host != "host.docker.internal" && (ip == nil || !ip.IsLoopback()) {
			return errors.New("MCP 明文 HTTP 仅允许本机地址；远端服务请使用 HTTPS")
		}
	}
	return nil
}

func validateDockerMCPGatewayURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.Hostname() != "host.docker.internal" {
		return errors.New("Docker 本机网关模式仅适用于 http://host.docker.internal 端点")
	}
	return validateMCPHTTPURL(raw)
}

func openMCPHTTP(ctx context.Context, cfg SourceConfig, token string) (*mcpHTTP, error) {
	return openMCPHTTPGuarded(ctx, cfg, token, nil)
}

func openMCPHTTPGuarded(ctx context.Context, cfg SourceConfig, token string, guard func() error) (*mcpHTTP, error) {
	if err := validateMCPHTTPURL(cfg.URL); err != nil {
		return nil, err
	}
	if strings.ContainsAny(token, "\r\n\x00") {
		return nil, errors.New("MCP Bearer 令牌含无效字符")
	}
	if cfg.DockerGateway {
		if err := validateDockerMCPGatewayURL(cfg.URL); err != nil {
			return nil, err
		}
		if strings.TrimSpace(token) == "" {
			return nil, errors.New("Docker 本机网关需要独立 Bearer 令牌")
		}
	}
	ctx, cancel := context.WithTimeout(ctx, mcpRequestTimeout)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// Never forward local gateway credentials to an environment HTTP proxy.
	u, _ := url.Parse(cfg.URL)
	ip := net.ParseIP(u.Hostname())
	if u.Hostname() == "localhost" || u.Hostname() == "host.docker.internal" || (ip != nil && ip.IsLoopback()) {
		transport.Proxy = nil
	}
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	s := &mcpHTTP{ctx: ctx, cancel: cancel, client: client, transport: transport, endpoint: cfg.URL, token: strings.TrimSpace(token), version: "2025-03-26", nextID: 1, dockerGateway: cfg.DockerGateway, beforePost: guard}
	var initialized struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if err := s.request("initialize", map[string]any{"protocolVersion": s.version, "capabilities": map[string]any{}, "clientInfo": map[string]string{"name": "aide", "version": buildVersion}}, &initialized); err != nil {
		s.close()
		return nil, fmt.Errorf("MCP HTTP 初始化失败: %w", err)
	}
	switch initialized.ProtocolVersion {
	case "2025-03-26", "2024-11-05", "2025-06-18":
		s.version = initialized.ProtocolVersion
	default:
		s.close()
		return nil, errors.New("MCP HTTP 服务返回不支持的协议版本")
	}
	if err := s.notify("notifications/initialized", map[string]any{}); err != nil {
		s.close()
		return nil, err
	}
	return s, nil
}

func (s *mcpHTTP) headers(request *http.Request) {
	if s.dockerGateway {
		// Docker Desktop forwards this private alias to host loopback. Retain the
		// authenticated gateway's localhost virtual host without opening a LAN bind.
		request.Host = "localhost"
		if port := request.URL.Port(); port != "" {
			request.Host = net.JoinHostPort("localhost", port)
		}
	}
	request.Header.Set("Accept", "application/json, text/event-stream")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("MCP-Protocol-Version", s.version)
	if s.token != "" {
		request.Header.Set("Authorization", "Bearer "+s.token)
	}
	if s.session != "" {
		request.Header.Set("Mcp-Session-Id", s.session)
	}
}

func (s *mcpHTTP) close() {
	if s == nil {
		return
	}
	if s.session != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		request, err := http.NewRequestWithContext(ctx, http.MethodDelete, s.endpoint, nil)
		if err == nil {
			s.headers(request)
			if response, err := s.client.Do(request); err == nil {
				response.Body.Close()
			}
		}
		cancel()
	}
	s.cancel()
	s.transport.CloseIdleConnections()
}

func (s *mcpHTTP) post(message any, id int, result any) error {
	if s.beforePost != nil {
		if err := s.beforePost(); err != nil {
			return err
		}
	}
	body, err := json.Marshal(message)
	if err != nil {
		return errors.New("MCP HTTP 请求无法编码")
	}
	if len(body) > mcpLineLimit {
		return errors.New("MCP HTTP 请求超过大小限制")
	}
	request, err := http.NewRequestWithContext(s.ctx, http.MethodPost, s.endpoint, bytes.NewReader(body))
	if err != nil {
		return errors.New("MCP HTTP 请求地址无效")
	}
	s.headers(request)
	response, err := s.client.Do(request)
	if err != nil {
		return errors.New("MCP HTTP 连接失败、已取消或超时")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("MCP HTTP 返回 %d；请核对端点、认证或连接", response.StatusCode)
	}
	if session := response.Header.Get("Mcp-Session-Id"); session != "" {
		if len(session) > 1024 || strings.IndexFunc(session, func(r rune) bool { return r < 33 || r > 126 }) >= 0 {
			return errors.New("MCP HTTP 会话标识无效")
		}
		if s.session != "" && s.session != session {
			return errors.New("MCP HTTP 会话标识在请求间发生变化")
		}
		s.session = session
	}
	if id == 0 {
		return nil
	}
	media, _, _ := mime.ParseMediaType(response.Header.Get("Content-Type"))
	limited := &io.LimitedReader{R: response.Body, N: mcpLineLimit + 1}
	if media == "application/json" {
		data, err := io.ReadAll(limited)
		if err != nil {
			return errors.New("MCP HTTP 响应读取失败")
		}
		if len(data) > mcpLineLimit {
			return errors.New("MCP HTTP 响应超过大小限制")
		}
		return s.decodeResponse(data, id, result)
	}
	if media != "text/event-stream" {
		return errors.New("MCP HTTP 响应须为 JSON 或 SSE")
	}
	scanner := bufio.NewScanner(limited)
	scanner.Buffer(make([]byte, 32<<10), mcpLineLimit)
	var data []byte
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" && len(data) > 0 {
			var envelope mcpRPCResponse
			if json.Unmarshal(data, &envelope) == nil && len(envelope.ID) > 0 {
				return s.decodeResponse(data, id, result)
			}
			data = nil
		} else if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " ")...)
			data = append(data, '\n')
		}
	}
	if limited.N <= 0 {
		return errors.New("MCP HTTP 响应超过大小限制")
	}
	if scanner.Err() != nil {
		return errors.New("MCP HTTP SSE 读取失败、已取消或超时")
	}
	return errors.New("MCP HTTP SSE 未返回匹配结果")
}

func (s *mcpHTTP) decodeResponse(data []byte, id int, result any) error {
	var response mcpRPCResponse
	if json.Unmarshal(data, &response) != nil {
		return errors.New("MCP HTTP 返回无效 JSON-RPC")
	}
	var actual int
	if json.Unmarshal(response.ID, &actual) != nil || actual != id {
		return errors.New("MCP HTTP 返回不匹配的请求编号")
	}
	if response.Error != nil {
		message := response.Error.Message
		if s.token != "" {
			message = strings.ReplaceAll(message, s.token, "[redacted]")
		}
		return fmt.Errorf("MCP 返回错误 %d: %s", response.Error.Code, message)
	}
	if len(response.Result) == 0 || json.Unmarshal(response.Result, result) != nil {
		return errors.New("MCP HTTP 返回空或无效结果")
	}
	return nil
}
func (s *mcpHTTP) request(method string, params any, result any) error {
	id := s.nextID
	s.nextID++
	return s.post(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}, id, result)
}
func (s *mcpHTTP) notify(method string, params any) error {
	return s.post(map[string]any{"jsonrpc": "2.0", "method": method, "params": params}, 0, nil)
}
func (s *mcpHTTP) listTools() ([]MCPTool, error) { return listMCPTools(s.request) }
