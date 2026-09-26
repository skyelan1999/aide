package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 记忆访问单向可见策略（#35）的单元测试。
// 三条核心规则：
//  1. 小秘的记忆/历史对 aide 隔离（aide 一律不可读不可写）
//  2. 小秘对 aide 记忆只读（可读、不可写）
//  3. （流式桥接见 stream_broker_test.go）

func aideMemFile(data string) string  { return filepath.Join(MemoryCoreDir(data), "memory.md") }
func asstMemFile(data string) string  { return VoiceMemoryPath(data) }
func asstHistFile(data string) string { return VoiceHistoryPath(data) }

// TestAideCannotReadAssistantMemory：aide 工具尝试读小秘记忆 → 被策略拒绝。
func TestAideCannotReadAssistantMemory(t *testing.T) {
	data := t.TempDir()
	cases := map[string]string{
		"小秘长期记忆":           asstMemFile(data),
		"小秘对话历史":           asstHistFile(data),
		"assistant目录下任意文件": filepath.Join(AssistantDir(data), "sub", "anything.json"),
	}
	for name, p := range cases {
		if ok, reason := canAccessMemory(data, callerAide, p, opRead); ok {
			t.Errorf("%s: aide 不应可读 %s", name, p)
		} else if !strings.Contains(reason, "小秘") {
			t.Errorf("%s: 拒绝原因应指明小秘私有区，got=%q", name, reason)
		}
	}
}

// TestAideCannotWriteAssistantMemory：aide 尝试写小秘区 → 被拒。
func TestAideCannotWriteAssistantMemory(t *testing.T) {
	data := t.TempDir()
	if ok, _ := canAccessMemory(data, callerAide, asstMemFile(data), opWrite); ok {
		t.Fatal("aide 不应可写小秘记忆")
	}
	if ok, _ := canAccessMemory(data, callerAide, filepath.Join(AssistantDir(data), "x"), opWrite); ok {
		t.Fatal("aide 不应可写 assistant/ 子树")
	}
}

// TestAssistantCanReadAideMemory：小秘 readAideMemory 能读到 aide 写下的记忆。
func TestAssistantCanReadAideMemory(t *testing.T) {
	data := t.TempDir()
	if err := os.MkdirAll(MemoryCoreDir(data), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(aideMemFile(data), []byte("- 用户偏好深色模式\n- 项目用 vendor 依赖"), 0600); err != nil {
		t.Fatal(err)
	}
	va := newVoiceAgent(data)
	got := va.readAideMemory()
	if !strings.Contains(got, "深色模式") || !strings.Contains(got, "vendor") {
		t.Fatalf("readAideMemory 未返回 aide 记忆内容: %q", got)
	}
	// 策略层：小秘读 memory/core 放行
	if ok, _ := canAccessMemory(data, callerAssistant, aideMemFile(data), opRead); !ok {
		t.Fatal("小秘应可读 aide 记忆")
	}
}

// TestAssistantCannotWriteAideMemory：小秘对 aide 记忆只读（无写方法 + 策略拒绝写）。
// “无 writeAideMemory 方法”是编译期保证：本类型不暴露任何写方法；
// 这里再断言策略层对小秘写 memory/core 明确拒绝。
func TestAssistantCannotWriteAideMemory(t *testing.T) {
	data := t.TempDir()
	if ok, reason := canAccessMemory(data, callerAssistant, aideMemFile(data), opWrite); ok {
		t.Fatalf("小秘不应可写 aide 记忆（策略层应拒绝）")
	} else if !strings.Contains(reason, "只读") {
		t.Fatalf("拒绝原因应说明只读，got=%q", reason)
	}
}

// TestIsolationPolicy：双向隔离矩阵 + 路径逃逸不得绕过目录归属判断。
func TestIsolationPolicy(t *testing.T) {
	data := t.TempDir()
	aideCore := aideMemFile(data)
	asst := asstMemFile(data)

	// aide 自己的记忆：读写都放行
	if ok, _ := canAccessMemory(data, callerAide, aideCore, opRead); !ok {
		t.Error("aide 应可读自己的记忆")
	}
	if ok, _ := canAccessMemory(data, callerAide, aideCore, opWrite); !ok {
		t.Error("aide 应可写自己的记忆")
	}
	// 小秘区：小秘读写放行
	if ok, _ := canAccessMemory(data, callerAssistant, asst, opWrite); !ok {
		t.Error("小秘应可写自己的记忆")
	}
	// 路径逃逸（..）不得把 assistant/ 伪装成 aide 区
	escaped := filepath.Join(MemoryCoreDir(data), "..", "..", "assistant", "voice-memory.json")
	if !isAssistantMemoryPath(data, escaped) {
		t.Error("归一化后落在 assistant/ 的路径仍应被判为小秘区（防 .. 逃逸绕过文件名判断）")
	}
	if ok, _ := canAccessMemory(data, callerAide, escaped, opRead); ok {
		t.Error("aide 经 .. 逃逸读小秘记忆应被拒")
	}
	// 文件名巧合不得跨目录：在 aide 区放一个叫 voice-memory.json 的文件，不属小秘区
	decoy := filepath.Join(MemoryCoreDir(data), "voice-memory.json")
	if isAssistantMemoryPath(data, decoy) {
		t.Error("仅凭文件名 voice-memory.json 不应判为小秘区（必须按目录归属）")
	}
}

// TestShellTouchesAssistantZone：run_shell 对小秘区的沙箱层拦截。
func TestShellTouchesAssistantZone(t *testing.T) {
	blocked := []string{
		"cat /data/assistant/voice-memory.json",
		"cat /data/assistant/voice-history.json",
		"cp /data/assistant/x /tmp/",
		"cat voice-memory.json",
		"cat voice-history.json",
	}
	for _, c := range blocked {
		if _, bad := shellTouchesAssistantZone(c); !bad {
			t.Errorf("应拦截命令: %q", c)
		}
	}
	allowed := []string{
		"ls -la",
		"cat memory/core/memory.md", // aide 自己的记忆（文件名不含 voice-*）
		"cat /data/memory/core/memory.md",
		"grep -r assistant internal/", // 工作区内正常操作
	}
	for _, c := range allowed {
		if _, bad := shellTouchesAssistantZone(c); bad {
			t.Errorf("不应拦截命令: %q", c)
		}
	}
}
