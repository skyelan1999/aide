package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"sort"
	"strings"
	"time"
)

const (
	mcpRequestTimeout = 30 * time.Second
	mcpLineLimit      = 1 << 20
	mcpResultLimit    = 64 << 10
	mcpMaxTools       = 64
)

// validateMCPConfig accepts only a program plus discrete arguments. In
// particular, a value such as "npx -y package" is not treated as a shell
// command: the UI and API must send command="npx", args=["-y", "package"].
func validateMCPConfig(cfg *SourceConfig) error {
	cfg.Command = strings.TrimSpace(cfg.Command)
	if cfg.Command == "" {
		return errors.New("MCP 来源需要启动程序")
	}
	if strings.ContainsAny(cfg.Command, "\x00\r\n") {
		return errors.New("MCP 启动程序不能包含换行或空字符")
	}
	cfg.Transport = strings.TrimSpace(cfg.Transport)
	if cfg.Transport == "" {
		cfg.Transport = "stdio"
	}
	if cfg.Transport != "stdio" {
		return errors.New("当前仅支持 MCP stdio 传输")
	}
	if len(cfg.Args) > 64 {
		return errors.New("MCP 参数最多 64 个")
	}
	for i, arg := range cfg.Args {
		if len(arg) > 2048 || strings.ContainsAny(arg, "\x00\r\n") {
			return fmt.Errorf("MCP 参数 %d 包含不支持的字符或过长", i+1)
		}
	}
	return nil
}

type mcpRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type mcpRPCResponse struct {
	ID     json.RawMessage `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *mcpRPCError    `json:"error"`
}

type mcpStdio struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Scanner
	stderr *limitedBuffer
	cancel context.CancelFunc
	nextID int
}

type limitedBuffer struct {
	buf bytes.Buffer
	n   int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.n += len(p)
	remaining := 8 << 10
	if b.buf.Len() < remaining {
		part := p
		if len(part) > remaining-b.buf.Len() {
			part = part[:remaining-b.buf.Len()]
		}
		_, _ = b.buf.Write(part)
	}
	return len(p), nil
}

func (b *limitedBuffer) String() string {
	msg := strings.TrimSpace(b.buf.String())
	if b.n > b.buf.Len() {
		msg += "…"
	}
	return msg
}

func openMCPStdio(ctx context.Context, cfg SourceConfig) (*mcpStdio, error) {
	if err := validateMCPConfig(&cfg); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, mcpRequestTimeout)
	cmd := exec.CommandContext(ctx, cfg.Command, cfg.Args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stderr := &limitedBuffer{}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("启动 MCP 程序失败: %w", err)
	}
	scanner := bufio.NewScanner(stdoutPipe)
	scanner.Buffer(make([]byte, 32<<10), mcpLineLimit)
	s := &mcpStdio{cmd: cmd, stdin: stdin, stdout: scanner, stderr: stderr, cancel: cancel, nextID: 1}
	if err := s.initialize(); err != nil {
		s.close()
		return nil, err
	}
	return s, nil
}

func (s *mcpStdio) close() {
	if s == nil || s.cmd == nil {
		return
	}
	if s.cancel != nil {
		s.cancel()
	}
	_ = s.stdin.Close()
	if s.cmd.Process != nil && s.cmd.ProcessState == nil {
		_ = s.cmd.Process.Kill()
	}
	_ = s.cmd.Wait()
}

func (s *mcpStdio) initialize() error {
	var ignored json.RawMessage
	if err := s.request("initialize", map[string]any{
		"protocolVersion": "2025-03-26",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]string{"name": "aide", "version": buildVersion},
	}, &ignored); err != nil {
		return fmt.Errorf("MCP 初始化失败: %w", err)
	}
	return s.notify("notifications/initialized", map[string]any{})
}

func (s *mcpStdio) notify(method string, params any) error {
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
	_, err := s.stdin.Write(append(b, '\n'))
	return err
}

func (s *mcpStdio) request(method string, params any, result any) error {
	id := s.nextID
	s.nextID++
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	if _, err := s.stdin.Write(append(b, '\n')); err != nil {
		return fmt.Errorf("MCP 请求写入失败: %w", err)
	}
	for s.stdout.Scan() {
		line := s.stdout.Bytes()
		var response mcpRPCResponse
		if err := json.Unmarshal(line, &response); err != nil || len(response.ID) == 0 {
			// Stdio MCP servers must reserve stdout for protocol frames, but ignore
			// an accidental log line rather than expose it to an AI task.
			continue
		}
		var responseID int
		if json.Unmarshal(response.ID, &responseID) != nil || responseID != id {
			continue
		}
		if response.Error != nil {
			return fmt.Errorf("MCP 返回错误 %d: %s", response.Error.Code, response.Error.Message)
		}
		if len(response.Result) == 0 {
			return errors.New("MCP 返回空结果")
		}
		if err := json.Unmarshal(response.Result, result); err != nil {
			return fmt.Errorf("MCP 响应格式无效: %w", err)
		}
		return nil
	}
	if err := s.stdout.Err(); err != nil {
		return fmt.Errorf("MCP 响应读取失败: %w", err)
	}
	if detail := s.stderr.String(); detail != "" {
		return fmt.Errorf("MCP 程序在响应前结束: %s", detail)
	}
	return errors.New("MCP 程序在响应前结束")
}

type mcpWireTool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Annotations struct {
		ReadOnlyHint bool `json:"readOnlyHint"`
	} `json:"annotations"`
}

func (s *mcpStdio) listTools() ([]MCPTool, error) {
	var response struct {
		Tools []mcpWireTool `json:"tools"`
	}
	if err := s.request("tools/list", map[string]any{}, &response); err != nil {
		return nil, err
	}
	tools := make([]MCPTool, 0, len(response.Tools))
	for _, tool := range response.Tools {
		name := strings.TrimSpace(tool.Name)
		if name == "" || len(name) > 128 || strings.ContainsAny(name, "\x00\r\n") {
			continue
		}
		description := strings.TrimSpace(tool.Description)
		if len(description) > 400 {
			description = description[:400] + "…"
		}
		tools = append(tools, MCPTool{Name: name, Description: description, ReadOnly: tool.Annotations.ReadOnlyHint})
		if len(tools) == mcpMaxTools {
			break
		}
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	return tools, nil
}

func (a *App) probeMCPSource(ctx context.Context, src Source) ([]MCPTool, error) {
	s, err := openMCPStdio(ctx, src.Config)
	if err != nil {
		return nil, err
	}
	defer s.close()
	return s.listTools()
}

func (a *App) callMCPReadOnlyTool(ctx context.Context, src Source, name string, arguments map[string]any) (string, error) {
	s, err := openMCPStdio(ctx, src.Config)
	if err != nil {
		return "", err
	}
	defer s.close()
	tools, err := s.listTools()
	if err != nil {
		return "", err
	}
	allowed := false
	for _, tool := range tools {
		if tool.Name == name && tool.ReadOnly {
			allowed = true
			break
		}
	}
	if !allowed {
		return "", errors.New("MCP 工具不存在或未声明为只读，引用来源不会调用写入工具")
	}
	var response struct {
		Content any  `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := s.request("tools/call", map[string]any{"name": name, "arguments": arguments}, &response); err != nil {
		return "", err
	}
	b, _ := json.Marshal(response.Content)
	if len(b) > mcpResultLimit {
		b = append(b[:mcpResultLimit], []byte("…（MCP 返回已截断）")...)
	}
	if response.IsError {
		return "", fmt.Errorf("MCP 工具返回错误: %s", string(b))
	}
	return string(b), nil
}

func (a *App) testMCPSource(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	a.mu.Lock()
	src, ok := a.findSource(id)
	a.mu.Unlock()
	if !ok || !src.Enabled || src.Type != "mcp" {
		fail(w, http.StatusNotFound, errors.New("未找到已启用的 MCP 引用来源"))
		return
	}
	tools, err := a.probeMCPSource(r.Context(), src)
	if err != nil {
		fail(w, http.StatusBadGateway, err)
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for i := range a.sourceRegistry.Sources {
		if a.sourceRegistry.Sources[i].ID == id {
			a.sourceRegistry.Sources[i].Config.MCPTools = tools
			break
		}
	}
	if err := a.saveSources(); err != nil {
		fail(w, 500, err)
		return
	}
	jsonOut(w, 200, map[string]any{"source": id, "tools": tools})
}

func mcpVirtualFiles(src Source, p string) ([]map[string]any, error) {
	if p != "." && p != "tools" {
		return nil, errors.New("未知 MCP 引用路径")
	}
	if p == "." {
		return []map[string]any{{"name": "tools", "path": "tools", "dir": true}}, nil
	}
	items := make([]map[string]any, 0, len(src.Config.MCPTools))
	for i, tool := range src.Config.MCPTools {
		items = append(items, map[string]any{"name": fmt.Sprintf("%02d-%s.md", i+1, mcpSafeName(tool.Name)), "path": fmt.Sprintf("tools/%d.md", i+1), "dir": false})
	}
	return items, nil
}

func mcpVirtualFile(src Source, p string) ([]byte, error) {
	var index int
	if _, err := fmt.Sscanf(p, "tools/%d.md", &index); err != nil || index < 1 || index > len(src.Config.MCPTools) {
		return nil, errors.New("未知 MCP 工具说明")
	}
	tool := src.Config.MCPTools[index-1]
	access := "不可从引用调用（未声明为只读）"
	if tool.ReadOnly {
		access = "可由 AI 通过 mcp_call 读取调用"
	}
	return []byte("# MCP 工具：" + tool.Name + "\n\n" + tool.Description + "\n\n访问：" + access + "\n"), nil
}

func mcpSafeName(name string) string {
	name = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			return r
		}
		return '_'
	}, name)
	if name == "" {
		return "tool"
	}
	return name
}
