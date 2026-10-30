package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"strings"
)

// Called with a.mu held. A proposal records the configured binding, never a
// model-supplied absolute destination or an empty-path /context fallback.
func (a *App) proposalSourceLocked(sourceID string) (Source, string, error) {
	src, ok := a.findSource(sourceID)
	if sourceID == systemDocsSource && strings.TrimSpace(a.wsConfig.Docs.Path) == "" {
		return Source{}, "", errors.New("请先在工作空间中配置自动系统文档目录")
	}
	if !ok || !src.RW || !sourceAIVisible(src, a.wsID()) {
		return Source{}, "", errors.New("引用来源对当前工作区不可见或不可写")
	}
	if src.Type != "local" && src.Type != "skill" && src.Type != "workspace-sftp" && src.Type != "sftp" && src.Type != "relay" {
		return Source{}, "", errors.New("该类型引用不支持文件写入")
	}
	if sourceID == systemDocsSource {
		src.Config.Path = a.wsConfig.Docs.Path
	}
	if strings.TrimSpace(src.Config.Path) == "" {
		return Source{}, "", errors.New("引用目录未配置，不能使用默认回退目录写入")
	}
	if src.Type == "workspace-sftp" {
		src.Config.Path = a.workspaceRemoteConfiguredDir(src.Config.Path)
	}
	binding := hash([]byte(a.wsID() + "\x00" + markdownSourceIdentity(src)))
	if src.Type == "relay" {
		digest, err := a.relayCredentialDigestLocked(src.ID)
		if err != nil {
			return Source{}, "", err
		}
		binding = hash([]byte(binding + digest))
	}
	return src, binding, nil
}

func sourceVersionKey(source, p string) string { return source + "\x00" + path.Clean(p) }
func sourceProposalPath(src Source, p string) string {
	target := path.Join(src.Config.Path, p)
	if src.Type == "sftp" {
		target = src.Config.Username + "@" + src.Config.Host + ":" + target
	}
	return target
}

func hasDocumentChanges(files []Change) bool {
	for _, f := range files {
		if f.Source != "" {
			return true
		}
	}
	return false
}
func proposalDisplayPath(f Change) string {
	if f.Source == systemDocsSource {
		return "自动系统文档 / " + f.Path + "（" + f.TargetPath + "）"
	}
	if f.Source != "" {
		return "引用（" + f.Source + "） / " + f.Path + "（" + f.TargetPath + "）"
	}
	return f.Path
}

type proposalTarget struct {
	relayCredential string
	root            *os.Root
	owned           bool
	remote          string
	generation      uint64
	identity        string
	source          Source
	socket          string
	noSymlinks      bool
}

func (t proposalTarget) close() {
	if t.owned && t.root != nil {
		t.root.Close()
	}
}
func (a *App) proposalTargetLocked(f Change) (proposalTarget, error) {
	if err := safePath(f.Path); err != nil {
		return proposalTarget{}, err
	}
	if f.Source == "" {
		if f.Binding != "" || f.TargetPath != "" {
			return proposalTarget{}, errors.New("工作目录提案不能指定文档绑定")
		}
		if a.workspaceMode() == "ssh" {
			if f.NoSymlinks {
				return proposalTarget{}, errors.New("文件绑定提案仅支持本地工作区")
			}
			return proposalTarget{remote: a.wsConfig.Workspace.Path, socket: sshControlSocket, generation: a.sshSessionGeneration(sshControlSocket), identity: "workspace:" + a.wsID()}, nil
		}
		return proposalTarget{root: a.workspace, identity: "workspace:" + a.wsID(), noSymlinks: f.NoSymlinks}, nil
	}
	src, binding, err := a.proposalSourceLocked(f.Source)
	if err != nil {
		return proposalTarget{}, err
	}
	if binding != f.Binding || sourceProposalPath(src, f.Path) != f.TargetPath {
		return proposalTarget{}, errors.New("自动系统文档绑定已变化，请重新生成提案")
	}
	if src.Type == "relay" {
		digest, err := a.relayCredentialDigestLocked(src.ID)
		return proposalTarget{source: src, identity: markdownSourceIdentity(src), relayCredential: digest}, err
	}
	if src.Type == "sftp" {
		return proposalTarget{source: src, socket: sourceSocket(src.ID), generation: a.sshSessionGeneration(sourceSocket(src.ID)), identity: markdownSourceIdentity(src)}, nil
	}
	if src.Type == "workspace-sftp" {
		return proposalTarget{remote: src.Config.Path, socket: sshControlSocket, generation: a.sshSessionGeneration(sshControlSocket), identity: markdownSourceIdentity(src)}, nil
	}
	root, err := a.localSourceRoot(src)
	return proposalTarget{root: root, owned: true, identity: markdownSourceIdentity(src)}, err
}
func (a *App) readProposalTarget(t proposalTarget, p string) ([]byte, error) {
	if t.source.Type == "relay" {
		return a.relayReadSource(relayExpectedContext(context.Background(), t.relayCredential), t.source, p, sourceRelayMaxFile)
	}
	if t.root != nil {
		if t.noSymlinks {
			return readBoundRawBytes(t.root, p)
		}
		return readRawBytes(t.root, p)
	}
	if t.source.ID != "" {
		return a.sftpReadSourceExpected(t.source, p, &t.generation)
	}
	return a.sftpReadAtGeneration(pathJoinRemote(t.remote, p), t.generation)
}
func (a *App) writeProposalTarget(t proposalTarget, f Change) error {
	return a.withMarkdownHistory(t.identity, f.Path, []byte(f.Content), func(p string) ([]byte, error) { return a.readProposalTarget(t, p) }, func() error {
		if t.root != nil {
			if t.noSymlinks {
				return putBoundText(t.root, f.Path, []byte(f.Content))
			}
			return putText(t.root, f.Path, []byte(f.Content))
		}
		if t.source.Type == "relay" {
			return a.relayWriteSourceContext(relayExpectedContext(context.Background(), t.relayCredential), t.source, f.Path, []byte(f.Content), false)
		}
		if t.source.ID != "" {
			return a.sftpWriteSourceExpected(t.source, f.Path, []byte(f.Content), &t.generation)
		}
		return a.sftpWriteAtGeneration(pathJoinRemote(t.remote, f.Path), []byte(f.Content), t.generation)
	})
}
func proposalVersion(b []byte, err error, f Change) error {
	if err == nil {
		if hash(b) != f.BaseHash {
			return errors.New("文件已改变，请重新生成提案")
		}
		return nil
	}
	if f.BaseHash == "" && (errors.Is(err, os.ErrNotExist) || isSFTPNotExistErr(err)) {
		return nil
	}
	return fmt.Errorf("无法确认文件版本: %w", err)
}

func (a *App) recordDocumentProposal(task *Task, versions map[string]Change, source, p, content string) (string, error) {
	if err := safePath(p); err != nil {
		return "", err
	}
	p = path.Clean(p)
	if p == "." {
		return "", errors.New("无效路径")
	}
	if len(content) > maxFile {
		return "", errors.New("文件内容超过提案限额")
	}
	a.filesMu.Lock()
	defer a.filesMu.Unlock()
	a.mu.Lock()
	defer a.mu.Unlock()
	if task.WorkspaceID != "" && task.WorkspaceID != a.wsID() {
		return "", errors.New("工作区已切换，请重新生成提案")
	}
	src, binding, err := a.proposalSourceLocked(source)
	if err != nil {
		return "", err
	}
	f := Change{Path: p, Source: source, Binding: binding, TargetPath: sourceProposalPath(src, p), Content: content}
	if v, ok := versions[sourceVersionKey(source, p)]; ok {
		f.BaseHash = v.BaseHash
		f.Before = v.Before
	}
	target, err := a.proposalTargetLocked(f)
	if err != nil {
		return "", err
	}
	defer target.close()
	revision := a.wsRevision
	a.mu.Unlock()
	b, readErr := a.readProposalTarget(target, p)
	a.mu.Lock()
	_, currentBinding, bindingErr := a.proposalSourceLocked(source)
	if revision != a.wsRevision || bindingErr != nil || currentBinding != binding {
		return "", errors.New("工作空间配置已变化，请重新生成提案")
	}
	if err = proposalVersion(b, readErr, f); err != nil {
		return "", fmt.Errorf("%s：请先附加现有文档后生成修改，或检查原文件版本: %w", p, err)
	}
	count, total, index := 0, len(content), -1
	for i, old := range task.Files {
		if old.Source == source && old.Path == p {
			index = i
			continue
		}
		count++
		total += len(old.Content)
	}
	if count >= 10 || total > maxProposalTotal {
		return "", errors.New("文件提案数量或总量超过限额")
	}
	if index >= 0 {
		task.Files[index] = f
	} else {
		task.Files = append(task.Files, f)
	}
	return "已生成文件修改提案：" + proposalDisplayPath(f) + "（审批后才写入；请使用相同 source 的 read_file 读取）", nil
}

// An explicit configured local document binding is eligible for independent
// review; remote targets and original-file replacement remain manual.
func (a *App) newProposalTargetEligibleLocked(f Change) bool {
	t, err := a.proposalTargetLocked(f)
	if err != nil {
		return false
	}
	defer t.close()
	check := checkVersion
	if t.noSymlinks {
		check = checkBoundVersion
	}
	return t.root != nil && check(t.root, f.Path, "") == nil
}
func (a *App) readAppliedProposalLocked(task *Task, f Change) ([]byte, error) {
	f.NoSymlinks = pwChangeNoSymlinks(task, f)
	t, err := a.proposalTargetLocked(f)
	if err != nil {
		return nil, err
	}
	defer t.close()
	if t.root == nil {
		return nil, errors.New("远程文档未进行本地字节读回验证")
	}
	return a.readProposalTarget(t, f.Path)
}

// Enters/returns with a.mu and filesMu held. Pin all local roots and remote
// generations, validate all originals before the first write, and persist each
// successful file independently so a partial failure can be retried safely.
func (a *App) applyDocumentProposalLocked(w http.ResponseWriter, r *http.Request, s *Session, task *Task) {
	files := append([]Change(nil), task.Files...)
	revision, workspace := a.wsRevision, a.wsID()
	targets := make([]proposalTarget, len(files))
	defer func() {
		for _, t := range targets {
			t.close()
		}
	}()
	current := func() error {
		found := false
		for _, run := range s.Runs {
			if run == task {
				found = true
				break
			}
		}
		if !found || a.wsRevision != revision || a.wsID() != workspace || a.sessions[s.ID] != s || s.Deleted || task.Status != "awaiting_approval" || len(task.Files) != len(files) {
			return errors.New("工作空间配置或任务状态已变化")
		}
		if sandboxMode(a.settings.SandboxMode) == "read-only" {
			return errors.New("只读沙箱不能写入")
		}
		for i, t := range targets {
			if !files[i].Applied && t.root == nil && t.identity != "" && a.sshSessionGeneration(t.socket) != t.generation {
				return errors.New("远程连接已变化")
			}
		}
		for i, f := range files {
			if task.Files[i] != f {
				return errors.New("文件提案已变化")
			}
			if f.Source != "" {
				src, binding, err := a.proposalSourceLocked(f.Source)
				if err != nil {
					return err
				}
				if binding != f.Binding || sourceProposalPath(src, f.Path) != f.TargetPath {
					return errors.New("自动系统文档绑定已变化")
				}
			}
		}
		return nil
	}
	for i, f := range files {
		if f.Applied {
			continue
		}
		t, err := a.proposalTargetLocked(f)
		if err == nil && pwChangeNoSymlinks(task, f) {
			t.noSymlinks = true
			if t.root == nil {
				err = errors.New("文件绑定提案仅支持本地工作区")
			}
		}
		if err != nil {
			fail(w, 409, err)
			return
		}
		targets[i] = t
		a.mu.Unlock()
		b, readErr := a.readProposalTarget(t, f.Path)
		a.mu.Lock()
		if err = current(); err == nil {
			err = proposalVersion(b, readErr, f)
		}
		if err != nil {
			fail(w, 409, fmt.Errorf("%s: %w", proposalDisplayPath(f), err))
			return
		}
	}
	for i, f := range files {
		if f.Applied {
			continue
		}
		if err := current(); err != nil {
			fail(w, 409, err)
			return
		}
		a.mu.Unlock()
		writeErr := a.writeProposalTarget(targets[i], f)
		a.mu.Lock()
		if i >= len(task.Files) || task.Files[i] != f {
			fail(w, 409, errors.New("保存期间文件提案已变化，请检查文件状态"))
			return
		}
		var afterWrite *markdownPostWriteError
		if writeErr != nil {
			if errors.As(writeErr, &afterWrite) {
				task.Files[i].Applied = true
				files[i].Applied = true
			}
			task.Error = "部分应用失败: " + writeErr.Error()
			_ = a.save(s)
			fail(w, 500, errors.New(task.Error))
			return
		}
		task.Files[i].Applied = true
		files[i].Applied = true
		validationErr := current()
		saveErr := a.save(s)
		if validationErr != nil {
			fail(w, 409, fmt.Errorf("文件已写入，后续应用已停止: %w", validationErr))
			return
		}
		if saveErr != nil {
			fail(w, 500, saveErr)
			return
		}
	}
	task.Applied = true
	task.Status = "completed"
	task.Error = ""
	s.Checked = false
	if err := a.save(s); err != nil {
		fail(w, 500, err)
		return
	}
	jsonOut(w, 200, task)
}
