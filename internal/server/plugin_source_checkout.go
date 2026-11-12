package server

// A per-invocation checkout lets file-based tools work with authorized remote
// sources without receiving SSH credentials or an unconfined remote pathname.
import (
	"context"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

const pluginCheckoutLimit int64 = 64 << 20

type pluginSourceCheckout struct {
	lockGen                       int64
	area                          knowledgeArea
	path, local, hash             string
	existed, write, saved, sqlite bool
}

var pluginCheckoutSlot = regexp.MustCompile(`^[a-z][a-z0-9-]{0,24}$`)

func (a *App) pluginCheckoutCurrent(ar knowledgeArea) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, current := range a.pluginSourceAreasLocked() {
		if pluginSourceID(current) == pluginSourceID(ar) && current.origin == ar.origin {
			return true
		}
	}
	return false
}

func checkoutSource(ar knowledgeArea) string {
	if ar.source != "" {
		return ar.source
	}
	return ar.root
}

func checkoutRead(fs *transferFS, p string, sqlite bool) ([]byte, bool, error) {
	for _, suffix := range []string{"-wal", "-journal"} {
		if !sqlite {
			break
		}
		info, err := fs.Stat(p + suffix)
		if err == nil && (!info.Mode().IsRegular() || info.Size() > 0) {
			return nil, false, errors.New("数据库仍有 WAL/日志；请停止远端写入并完成 checkpoint 后重试")
		}
		if err != nil && !os.IsNotExist(err) {
			return nil, false, err
		}
	}
	parts := strings.Split(p, "/")
	for i := range parts {
		info, err := fs.Stat(strings.Join(parts[:i+1], "/"))
		if os.IsNotExist(err) && i == len(parts)-1 {
			return nil, false, nil
		}
		if err != nil {
			return nil, false, err
		}
		if info.Mode()&os.ModeSymlink != 0 || i < len(parts)-1 && !info.IsDir() || i == len(parts)-1 && !info.Mode().IsRegular() {
			return nil, false, errors.New("checkout 路径必须是普通文件，且不含符号链接")
		}
	}
	file, err := fs.Open(p, os.O_RDONLY)
	if err != nil {
		return nil, false, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > pluginCheckoutLimit {
		return nil, false, errors.New("checkout 文件无效或超过 64 MiB")
	}
	b, err := io.ReadAll(io.LimitReader(file, pluginCheckoutLimit+1))
	if err != nil || int64(len(b)) > pluginCheckoutLimit || int64(len(b)) != info.Size() {
		return nil, false, errors.New("checkout 读取失败、超过预算或读取期间发生变化")
	}
	return b, true, nil
}

func (a *App) pluginCheckoutRequest(ctx context.Context, areas []knowledgeArea, temp string, total *int64, entries map[string]*pluginSourceCheckout, in pluginSourceRequest) (any, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	lock := a.readLockState()
	if lock.Locked {
		return nil, errors.New("工作台已锁定，来源调用已取消")
	}
	if !pluginCheckoutSlot.MatchString(in.Slot) {
		return nil, errors.New("checkout slot 无效")
	}
	if in.Operation == "commit" {
		entry := entries[in.Slot]
		if entry == nil || !entry.write || entry.saved || in.Source != "" || in.Path != "" || in.Write || in.AllowCreate || in.SQLite {
			return nil, errors.New("commit 必须使用本次已获写权限且尚未提交的 checkout slot")
		}
		a.filesMu.Lock()
		defer a.filesMu.Unlock()
		mutationCtx, finish, e := a.beginMessageMutation(ctx, entry.lockGen)
		if e != nil {
			return nil, e
		}
		defer finish()
		ctx = mutationCtx
		if !a.pluginCheckoutEntryCurrent(entry) {
			return nil, errors.New("工作区、来源权限或 AI 可见性已变化，未写回")
		}
		local, err := os.OpenRoot(temp)
		if err != nil {
			return nil, err
		}
		defer local.Close()
		staged, exists, err := checkoutRead(&transferFS{root: local}, entry.local, entry.sqlite)
		if err != nil || !exists {
			return nil, errors.New("checkout 输出不是完整普通文件，未写回")
		}
		fs, err := a.streamTransferFS(ctx, checkoutSource(entry.area), true)
		if err != nil {
			return nil, err
		}
		defer fs.Close()
		current, existed, err := checkoutRead(fs, entry.path, entry.sqlite)
		if err != nil {
			return nil, err
		}
		if existed != entry.existed || hash(current) != entry.hash || !a.pluginCheckoutEntryCurrent(entry) {
			return nil, errors.New("来源文件或配置已变化，checkout 冲突，未写回")
		}
		if hash(staged) != entry.hash || !entry.existed {
			guard := func() error {
				if e := ctx.Err(); e != nil {
					return e
				}
				if !a.pluginCheckoutEntryCurrent(entry) {
					return errors.New("来源或锁屏状态已变化，未发布")
				}
				latest, found, e := checkoutRead(fs, entry.path, entry.sqlite)
				if e != nil {
					return e
				}
				if found != entry.existed || hash(latest) != entry.hash {
					return errors.New("来源在暂存写入期间变化，未发布")
				}
				return nil
			}
			if err := fs.writeAtomicChecked(entry.path, staged, false, guard); err != nil {
				return nil, err
			}
		}
		entry.saved = true
		if !a.pluginCheckoutEntryCurrent(entry) {
			return nil, errors.New("写回期间来源配置变化；保存状态需重新读取核对")
		}
		return map[string]any{"saved": true, "path": entry.path, "source": pluginSourceID(entry.area), "hash": hash(staged)}, nil
	}
	if in.Operation != "checkout" || entries[in.Slot] != nil || len(entries) >= 4 || in.Path == "." || path.Clean(in.Path) != in.Path || !knowledgeSafePath(in.Path) {
		return nil, errors.New("checkout 路径或重复 slot 无效")
	}
	for _, part := range strings.Split(in.Path, "/") {
		for _, protected := range pluginSourceProtected {
			if strings.EqualFold(part, protected) {
				return nil, errors.New("checkout 不访问宿主受保护路径")
			}
		}
	}
	if in.Source == "" {
		in.Source = "workspace"
	}
	var area *knowledgeArea
	for i := range areas {
		if pluginSourceID(areas[i]) == in.Source {
			area = &areas[i]
			break
		}
	}
	if area == nil || !a.pluginCheckoutCurrent(*area) || in.AllowCreate && !in.Write {
		return nil, errors.New("来源不可用、AI 不可见或未获写权限")
	}
	fs, err := a.streamTransferFS(ctx, checkoutSource(*area), in.Write)
	if err != nil {
		return nil, err
	}
	defer fs.Close()
	b, existed, err := checkoutRead(fs, in.Path, in.SQLite)
	if err != nil {
		return nil, err
	}
	if !existed && !in.AllowCreate {
		return nil, os.ErrNotExist
	}
	afterLock := a.readLockState()
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	if afterLock.Locked || afterLock.Gen != lock.Gen {
		return nil, errors.New("附件暂存期间锁屏状态变化，结果丢弃")
	}
	if *total+int64(len(b)) > pluginCheckoutLimit || !a.pluginCheckoutCurrent(*area) {
		return nil, errors.New("checkout 超过调用预算或来源配置已变化")
	}
	local := path.Join("checkout", in.Slot, path.Base(in.Path))
	if err := os.MkdirAll(filepath.Dir(filepath.Join(temp, local)), 0700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(temp, local), b, 0600); err != nil {
		return nil, err
	}
	*total += int64(len(b))
	entries[in.Slot] = &pluginSourceCheckout{area: *area, path: in.Path, local: local, hash: hash(b), existed: existed, write: in.Write, sqlite: in.SQLite, lockGen: lock.Gen}
	return map[string]any{"workspace": temp, "path": local, "source": pluginSourceLabel(*area), "hash": hash(b), "exists": existed, "writable": in.Write, "maxBytes": pluginCheckoutLimit}, nil
}

func (a *App) pluginCheckoutEntryCurrent(entry *pluginSourceCheckout) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	lock := a.loadLockStateLocked()
	if lock.Locked || lock.Gen != entry.lockGen {
		return false
	}
	for _, current := range a.pluginSourceAreasLocked() {
		if pluginSourceID(current) == pluginSourceID(entry.area) && current.origin == entry.area.origin {
			return true
		}
	}
	return false
}
