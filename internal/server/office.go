package server

// ── Office 查看辅助（#63）───────────────────────────────────────────────────
//
// 1. 旧版 .doc（二进制）检测：在线查看器只支持 .docx，.doc 给出明确提示。
// 2. LibreOffice/soffice 探测：存在即报告"可转换"，本任务不实际安装/调用。
// 3. aide 工具执行器：os/exec 调 scripts/office/ 下的 python 脚本。
//    脚本查找顺序：$AIDE_OFFICE_SCRIPTS → /workspace/scripts/office → /opt/aide/office-scripts。
//    （dev 容器把仓库挂在 /workspace；发布镜像由 Dockerfile 拷贝到 /opt/aide/office-scripts）

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"strings"
	"sync"
	"syscall"
	"time"
)

// officeScriptTimeout 单次 office 转换脚本执行上限（python3 + soffice 转换损坏文档可能死锁）。
var officeScriptTimeout = 60 * time.Second

// officeMaxStdout / officeMaxStderr 限制脚本输出，防止脚本狂写致内存膨胀。
const (
	officeMaxStdout = 1 << 20 // 1 MiB
	officeMaxStderr = 64 << 10
)

// legacyDocExt 旧版二进制 Word 扩展名（.docx 不在此列）。
func isLegacyDoc(p string) bool {
	return strings.ToLower(path.Ext(p)) == ".doc"
}

var libreofficeProbe struct {
	once      sync.Once
	available bool
}

// libreofficeAvailable 探测容器内是否有 soffice/libreoffice（结果进程内缓存）。
func libreofficeAvailable() bool {
	libreofficeProbe.once.Do(func() {
		for _, bin := range []string{"soffice", "libreoffice"} {
			if _, err := exec.LookPath(bin); err == nil {
				libreofficeProbe.available = true
				return
			}
		}
	})
	return libreofficeProbe.available
}

// legacyDocError 构造 .doc 拒绝错误（查看器端点统一文案）。
func legacyDocError(p string) error {
	msg := fmt.Sprintf("旧版 .doc 二进制格式（%s），暂不支持在线查看/批注。请在 Word/WPS 中另存为 .docx 后打开。", p)
	if libreofficeAvailable() {
		msg += "（检测到 LibreOffice，后续可加服务端转换通道）"
	}
	return errors.New(msg)
}

// officeScriptPath 定位 scripts/office 下的脚本文件。
func officeScriptPath(name string) (string, error) {
	candidates := []string{}
	if env := strings.TrimSpace(os.Getenv("AIDE_OFFICE_SCRIPTS")); env != "" {
		candidates = append(candidates, env)
	}
	candidates = append(candidates, "/workspace/scripts/office", "/opt/aide/office-scripts")
	for _, dir := range candidates {
		cand := path.Join(dir, name)
		if st, err := os.Stat(cand); err == nil && !st.IsDir() {
			return cand, nil
		}
	}
	return "", fmt.Errorf("office 脚本未找到: %s（设置 AIDE_OFFICE_SCRIPTS 指向 scripts/office 目录）", name)
}

// limitedBytesWriter 有界 bytes.Buffer：超过 limit 后丢弃后续字节，并在尾部标注截断。
type limitedBytesWriter struct {
	limit     int
	buf       bytes.Buffer
	truncated bool
}

func (w *limitedBytesWriter) Write(p []byte) (int, error) {
	if w.truncated {
		return len(p), nil
	}
	remaining := w.limit - w.buf.Len()
	if remaining <= 0 {
		w.truncated = true
		w.buf.WriteString("…[truncated]")
		return len(p), nil
	}
	if len(p) <= remaining {
		return w.buf.Write(p)
	}
	w.buf.Write(p[:remaining])
	w.truncated = true
	w.buf.WriteString("…[truncated]")
	return len(p), nil
}

func (w *limitedBytesWriter) String() string { return w.buf.String() }

// runOfficeScript 执行 python3 <script> <args...>；workdir 为工作区根（生产即 /workspace）。
// 返回 stdout；失败时把 stderr 带进错误信息，便于 aide 排障。
// 带 officeScriptTimeout 超时；超时后 kill 整个进程组（python 可能 fork soffice），避免子进程泄漏。
func runOfficeScript(workdir, script string, args ...string) (string, error) {
	bin, err := exec.LookPath("python3")
	if err != nil {
		return "", errors.New("容器内无 python3")
	}
	ctx, cancel := context.WithTimeout(context.Background(), officeScriptTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, append([]string{script}, args...)...)
	cmd.Dir = workdir
	// 独立进程组；超时/取消时 kill 整个组（含 soffice 子进程）。
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = 2 * time.Second

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return "", err
	}
	if err := cmd.Start(); err != nil {
		return "", errors.New("office 脚本启动失败: " + err.Error())
	}
	stdoutBuf := &limitedBytesWriter{limit: officeMaxStdout}
	stderrBuf := &limitedBytesWriter{limit: officeMaxStderr}
	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		_, _ = io.Copy(stderrBuf, stderrPipe)
	}()
	_, _ = io.Copy(stdoutBuf, stdoutPipe)
	waitErr := cmd.Wait()
	<-stderrDone

	if ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("office 脚本超时（%s），已终止进程组", officeScriptTimeout)
	}
	if waitErr != nil {
		stderrText := strings.TrimSpace(stderrBuf.String())
		if stderrText == "" {
			stderrText = waitErr.Error()
		}
		return "", errors.New("office 脚本失败: " + stderrText)
	}
	return stdoutBuf.String(), nil
}

// docxTool 是 aide 工具调用 .docx 脚本的统一入口：路径/扩展/存在性校验 + 执行。
// 只支持本地工作区（脚本跑在容器内，SSH 远端工作区无文件可读）。
func (a *App) docxTool(wsRoot *os.Root, mode, p, script string, extra ...string) string {
	if mode == "ssh" {
		return "Office 工具仅支持本地工作区（SSH 工作区暂不支持）"
	}
	if p == "" {
		return "缺少 path 参数"
	}
	if err := safePath(p); err != nil {
		return "路径无效: " + err.Error()
	}
	if !strings.HasSuffix(strings.ToLower(p), ".docx") {
		return "仅支持 .docx 文件（旧版 .doc 请先另存为 .docx）"
	}
	if st, err := wsRoot.Stat(p); err != nil || st.IsDir() {
		return "文件不存在: " + p
	}
	sp, err := officeScriptPath(script)
	if err != nil {
		return err.Error()
	}
	out, err := runOfficeScript(wsRoot.Name(), sp, append([]string{p}, extra...)...)
	if err != nil {
		return err.Error()
	}
	return strings.TrimSpace(out)
}
