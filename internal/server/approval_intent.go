package server

import (
	"path"
	"strings"
)

// approvalExecutionIntent is a deterministic, evidence-linked summary of the
// action awaiting approval. It never replaces the original command or Changes.
type approvalExecutionIntent struct {
	Action                 string                 `json:"action"`
	WorkingDirectory       string                 `json:"workingDirectory,omitempty"`
	SandboxMode            string                 `json:"sandboxMode,omitempty"`
	WorkspaceMode          string                 `json:"workspaceMode,omitempty"`
	ContainerIsolationNote string                 `json:"containerIsolationNote,omitempty"`
	CommandSteps           []string               `json:"commandSteps,omitempty"`
	ParseStatus            string                 `json:"parseStatus,omitempty"`
	Targets                []approvalIntentTarget `json:"targets,omitempty"`
	TargetScopeWarning     string                 `json:"targetScopeWarning,omitempty"`
	Impact                 string                 `json:"impact"`
}

type approvalIntentTarget struct {
	Action string `json:"action"`
	Path   string `json:"path"`
}

// Optional environment arguments preserve callers that do not have current
// sandbox/workspace context; they must not imply facts that were not captured.
func shellApprovalExecutionIntent(cwd string, environment ...string) approvalExecutionIntent {
	sandbox, workspace := "", ""
	if len(environment) >= 2 {
		sandbox, workspace = environment[0], environment[1]
	}
	return approvalExecutionIntent{
		Action:                 "运行待审批的 shell 命令",
		WorkingDirectory:       cwd,
		SandboxMode:            sandbox,
		WorkspaceMode:          workspace,
		ContainerIsolationNote: "外层容器若存在仅是附加隔离；不得据此扩大授权，也不得假定宿主文件系统或网络副作用已被隔离。",
		ParseStatus:            "未解析",
		TargetScopeWarning:     "命令实际涉及的对象需以完整命令及其参数为准；此摘要未静态解析目标。",
		Impact:                 "命令可能修改文件、访问网络或改变外部状态；实际影响与可逆性无法仅由摘要确认。",
	}
}

// shellApprovalExecutionIntentForCommand summarizes only facts proven by the
// restricted literal-shell lexer. Unsupported syntax remains explicitly unknown.
func shellApprovalExecutionIntentForCommand(command, cwd, sandbox, workspace string) approvalExecutionIntent {
	intent := shellApprovalExecutionIntent(cwd, sandbox, workspace)
	commands, _, ok := literalShellCommands(command)
	if !ok {
		intent.ParseStatus = "包含受限解析器不支持的 Shell 语法；目标未知"
		intent.TargetScopeWarning = "未能静态解析完整命令；变量、替换、重定向、循环或其他 Shell 语法可能改变实际目标。必须以原始命令审查，不得按摘要推定安全。"
		return intent
	}
	intent.ParseStatus = "仅按受限字面语法解析"
	intent.CommandSteps = make([]string, 0, len(commands))
	for _, words := range commands {
		name := path.Base(words[0])
		if name == "git" && len(words) > 1 && words[1] == "rm" {
			name = "git rm"
		}
		intent.CommandSteps = append(intent.CommandSteps, name)
	}
	if target, valid := approvalGitRmTarget(command); valid {
		intent.Targets = []approvalIntentTarget{{Action: "从 Git 工作区移除单个明确路径（不删除 .git 元数据）", Path: target}}
		intent.ParseStatus = "已识别单个 git rm 路径；是否属于任务授权仍需审核"
		intent.TargetScopeWarning = "只识别了命令中的字面目标；执行前仍须确认该文件在 Git 索引中、位于授权工作区且确由用户任务要求移除。"
		intent.Impact = "git rm 将从索引移除并删除该目标文件；可否恢复取决于 Git 状态和对象是否仍可用，不能保证可逆。"
		return intent
	}
	intent.TargetScopeWarning = "仅识别了字面命令名称，未可靠解析一般命令的目标路径；选项、工作目录、管道及工具自身语义可能改变实际影响。请核对完整命令。"
	return intent
}

func shellApprovalExecutionIntentFor(cwd, sandbox, workspace string) approvalExecutionIntent {
	return shellApprovalExecutionIntent(cwd, sandbox, workspace)
}

func fileApprovalExecutionIntent(files []Change) approvalExecutionIntent {
	intent := approvalExecutionIntent{
		Action:  "应用文件提案",
		Impact:  "批准后将把提案内容写入下列未应用目标；能否撤销取决于目标状态与备份，摘要不保证可逆。",
		Targets: make([]approvalIntentTarget, 0, len(files)),
	}
	for _, file := range files {
		if file.Applied {
			continue
		}
		action := "写入/修改文件"
		if file.BaseHash == "" && file.Before == "" {
			action = "创建新文件"
		}
		intent.Targets = append(intent.Targets, approvalIntentTarget{Action: action, Path: proposalDisplayPath(file)})
	}
	return intent
}

// approvalGitRmTarget permits only one explicit, relative, non-recursive Git
// removal target. User authorization and repository state are checked separately
// by the reviewer and Git itself; this helper is not a reusable grant.
func approvalGitRmTarget(command string) (string, bool) {
	commands, _, ok := literalShellCommands(command)
	if !ok || len(commands) != 1 {
		return "", false
	}
	words := commands[0]
	if len(words) < 3 || path.Base(words[0]) != "git" || words[1] != "rm" {
		return "", false
	}
	var target string
	switch {
	case len(words) == 4 && words[2] == "--":
		target = words[3]
	case len(words) == 3 && !strings.HasPrefix(words[2], "-"):
		target = words[2]
	default:
		return "", false
	}
	if safePath(target) != nil || target == "." || target == "" {
		return "", false
	}
	return target, true
}

func gitMetadataPathArgument(arg string) bool {
	// Git accepts both --git-dir=.git and --git-dir .git forms. Inspect path
	// components, not substrings, so ordinary names such as .gitignore are safe.
	for _, candidate := range strings.Split(arg, "=") {
		candidate = strings.Trim(candidate, `"'`)
		for _, component := range strings.Split(strings.ReplaceAll(candidate, `\\`, "/"), "/") {
			if component == ".git" {
				return true
			}
		}
	}
	return false
}

// Only inspect parsed literal shell commands. Dynamic syntax stays manual when
// it may conceal protected operations; harmless quoted arguments are not treated
// as commands merely because they contain a dangerous word.
func approvalCommandNeedsManual(command string) (string, bool) {
	commands, _, ok := literalShellCommands(command)
	protected := map[string]bool{"rm": true, "rmdir": true, "unlink": true, "sudo": true, "chmod": true, "chown": true}
	if !ok {
		low := strings.ToLower(command)
		for name := range protected {
			if hasShellWord(low, name) {
				return "删除、权限变更或动态命令需要手动确认", true
			}
		}
		if strings.Contains(low, ".git") {
			return "复杂命令可能触及 .git 元数据，需要手动确认", true
		}
		return "", false
	}
	wrappers := map[string]bool{"env": true, "command": true, "nohup": true, "busybox": true}
	for _, words := range commands {
		name := path.Base(words[0])
		if protected[name] {
			return "删除或权限变更需要手动确认", true
		}
		if name == "git" && len(words) > 1 && words[1] == "rm" {
			if _, valid := approvalGitRmTarget(command); !valid {
				return "仅允许审核单个明确路径的 git rm；递归、强制或多目标删除需要手动确认", true
			}
		}
		if wrappers[name] {
			for _, arg := range words[1:] {
				if protected[path.Base(arg)] {
					return "删除或权限变更需要手动确认", true
				}
			}
		}
		for _, arg := range words[1:] {
			if !gitMetadataPathArgument(arg) {
				continue
			}
			switch name {
			case "git", "mv", "cp", "find", "python", "python3", "node", "perl", "ruby", "rm", "rmdir", "unlink", "chmod", "chown", "tee", "touch", "mkdir", "install":
				return "命令参数明确涉及 .git 元数据，需要手动确认", true
			}
		}
	}
	return "", false
}
