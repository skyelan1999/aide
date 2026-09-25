package server

import (
	"errors"
	"path/filepath"
	"strings"
)

// ── 记忆访问的单向可见策略（#35）──────────────────────────────────────────────
//
// 边界划分（与 paths.go 的数据目录分层一致，#31）：
//
//	aide 记忆区 = <data>/memory/core/   （aide 可读写；小秘只读）
//	小秘私有区  = <data>/assistant/      （小秘可读写；aide 一律禁止）
//	    ├─ voice-memory.json              小秘长期记忆
//	    └─ voice-history.json             小秘对话历史信封
//
// 这是【显式策略层】：判断依据是路径前缀与目录归属，而不是文件名巧合。
// 即便日后文件改名/移动，只要落在正确的目录归属里，策略依然成立。
// aide 的 read_memory/write_memory 不接收路径参数（路径固定指向 memory/core/），
// 本模块同时在工具入口做一次“断言式”越权检查，作为纵深防御。

// 调用方身份。
const (
	callerAide      = "aide"      // aide 主工作台 agent（含其工具与沙箱命令）
	callerAssistant = "assistant" // 小秘（语音秘书 agent）
)

// 访问操作。
const (
	opRead  = "read"
	opWrite = "write"
)

// withinDataBase 判断 target 是否位于 base 之内（base 自身也算在内部）。
// 返回相对 base 的 slash 化相对路径；越界/逃逸/跨盘返回 ok=false。
// 纯路径比较，不访问磁盘、不跟随符号链接。
func withinDataBase(base, target string) (rel string, ok bool) {
	if base == "" || target == "" {
		return "", false
	}
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return "", false
	}
	if rel == "." {
		return "", true
	}
	// Rel 在跨盘符/异常挂载场景可能给出 ".." 前缀却不报错，这里显式拦截逃逸。
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

// isAssistantMemoryPath 判断绝对路径是否落在小秘私有区整棵子树（<data>/assistant/）。
func isAssistantMemoryPath(dataPath, path string) bool {
	_, ok := withinDataBase(AssistantDir(dataPath), path)
	return ok
}

// isAideMemoryPath 判断绝对路径是否落在 aide 自己的记忆区整棵子树（<data>/memory/core/）。
func isAideMemoryPath(dataPath, path string) bool {
	_, ok := withinDataBase(MemoryCoreDir(dataPath), path)
	return ok
}

// canAccessMemory 记忆访问的唯一权威判定。
//
// 策略矩阵：
//
//	aide 记忆区 (memory/core) ：aide 读√ 写√ ; 小秘 读√ 写×（只读，不污染）
//	小秘私有区  (assistant/)   ：aide 读× 写× ; 小秘 读√ 写√
//
// 其他 /data 路径（config/sessions/secrets/...）不属于本策略管辖，由各自模块的
// 既有访问控制负责；记忆工具只应落在上述两区之一。
func canAccessMemory(dataPath, caller, path, op string) (bool, string) {
	switch {
	case isAideMemoryPath(dataPath, path):
		if caller == callerAide {
			return true, ""
		}
		// 小秘对 aide 记忆只读，不可写。
		if op == opRead {
			return true, ""
		}
		return false, "小秘对 aide 记忆为只读，禁止写入"
	case isAssistantMemoryPath(dataPath, path):
		if caller == callerAssistant {
			return true, ""
		}
		return false, "小秘私有记忆/历史区对 aide 不可见"
	default:
		return true, ""
	}
}

// errMemoryDenied 工具入口统一返回的越界错误。
func errMemoryDenied(reason string) error {
	return errors.New("记忆访问被拒绝：" + reason)
}

// shellTouchesAssistantZone 判断一条 aide 沙箱 shell 命令是否试图触碰小秘私有区。
//
// 背景：read_file/write_file/list_files 已被 safePath() 限制在工作区内（拒绝绝对路径与 ..），
// 但 run_shell 是同容器 bash（danger 模式下不拦截），能直接读到容器内 /data。
// 这里对“小秘私有区”做沙箱层补强：命中容器内路径前缀 /data/assistant/，
// 或小秘记忆/历史文件名即拦截。前缀命中是主判据，文件名命中是兜底（平铺期遗留位置）。
func shellTouchesAssistantZone(command string) (string, bool) {
	low := strings.ToLower(command)
	// 1) 路径前缀（权威判据）：整棵 assistant/ 子树。
	if strings.Contains(low, "/data/assistant") {
		return "小秘私有区（/data/assistant/）对 aide 沙箱命令不可见", true
	}
	// 2) 文件名兜底：平铺期/别名直接点名这两个文件。
	for _, name := range []string{VoiceMemoryFileName, VoiceHistoryFileName} {
		if strings.Contains(low, name) {
			return "小秘私有记忆/历史文件（" + name + "）对 aide 沙箱命令不可见", true
		}
	}
	return "", false
}
