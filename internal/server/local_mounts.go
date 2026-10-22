package server

import (
	"aide/internal/localmounts"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (a *App) initLocalMounts() {
	a.localMountRoots = map[string]*os.Root{}
	a.localMountConfig = localmounts.Empty()
	file := os.Getenv("AIDE_LOCAL_MOUNTS")
	if file == "" {
		return
	}
	c, err := localmounts.Read(file)
	if err != nil {
		return
	}
	if os.Getenv("AIDE_LOCAL_MOUNTS_REVISION") != localmounts.Revision(c) {
		return
	}
	for i, e := range c.Entries {
		if e.Error != "" {
			continue
		}
		r, err := os.OpenRoot(localmounts.Target(e))
		if err != nil {
			c.Entries[i].Error = "directory unavailable"
			continue
		}
		a.localMountRoots[e.ID] = r
	}
	a.localMountConfig = c
}

// localMountPath retains the opened root and relative path. Callers must use
// its Root methods for filesystem operations rather than reopen the joined path.
func (a *App) localMountPath(p string) (*os.Root, string, bool, bool) {
	for _, e := range a.localMountConfig.Entries {
		r := a.localMountRoots[e.ID]
		if r == nil {
			continue
		}
		for _, prefix := range []string{e.Path, localmounts.Target(e), r.Name()} {
			if rel, ok := localmounts.Relative(prefix, p); ok {
				return r, filepath.FromSlash(rel), e.ReadOnly, true
			}
		}
	}
	return nil, "", false, false
}
func (a *App) mountHostPath(p string) (string, bool, bool, error) {
	r, rel, ro, matched := a.localMountPath(p)
	if !matched {
		return "", false, false, nil
	}
	// A missing leaf is valid for a new cache directory. Validate the longest
	// existing prefix through Root.Stat, which rejects links escaping this root.
	for probe := rel; ; probe = filepath.Dir(probe) {
		if _, err := r.Stat(probe); err == nil {
			break
		} else if !os.IsNotExist(err) {
			return "", ro, true, err
		}
		if info, err := r.Lstat(probe); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", ro, true, errors.New("目录符号链接不可用")
		}
		if probe == "." {
			return "", ro, true, errors.New("挂载目录不可用")
		}
	}
	return filepath.Join(r.Name(), rel), ro, true, nil
}
func (a *App) openHostRoot(p string) (*os.Root, error) {
	if r, rel, _, matched := a.localMountPath(p); matched {
		return r.OpenRoot(rel)
	}
	return os.OpenRoot(p)
}
func (a *App) mkdirHostPath(p string, mode os.FileMode) error {
	if r, rel, ro, matched := a.localMountPath(p); matched {
		if ro {
			return errors.New("只读目录不能作为需要写入的缓存")
		}
		return r.MkdirAll(rel, mode)
	}
	return os.MkdirAll(p, mode)
}
func (a *App) localMountDir() string { return filepath.Join(a.dataPath, "local-mounts") }
func readMountObject(p string) map[string]any {
	b, err := os.ReadFile(p)
	if err != nil || len(b) > 128<<10 {
		return nil
	}
	var out map[string]any
	if json.Unmarshal(b, &out) != nil {
		return nil
	}
	return out
}
func (a *App) localMountSettings(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	desired, err := localmounts.Read(filepath.Join(a.localMountDir(), "desired.json"))
	if err != nil {
		fail(w, 500, errors.New("本地目录配置损坏，请保留文件并检查"))
		return
	}
	if r.Method == "PUT" {
		var in struct {
			Config   localmounts.Config `json:"config"`
			Revision string             `json:"revision"`
		}
		if err = decode(w, r, &in); err != nil {
			fail(w, 400, err)
			return
		}
		if in.Revision != localmounts.Revision(desired) {
			fail(w, 409, errors.New("目录配置已变化，请刷新后重试"))
			return
		}
		for i := range in.Config.Entries {
			e := &in.Config.Entries[i]
			e.Error = ""
			e.Path, err = localmounts.Normalize(e.Path)
			if err != nil {
				fail(w, 400, err)
				return
			}
		}
		if err = localmounts.Validate(in.Config); err != nil {
			fail(w, 400, err)
			return
		}
		if err = localmounts.Write(filepath.Join(a.localMountDir(), "desired.json"), in.Config); err != nil {
			fail(w, 500, err)
			return
		}
		desired = in.Config
	}
	host := readMountObject(filepath.Join(a.localMountDir(), "host.json"))
	ready := false
	if n, ok := host["updated"].(float64); ok {
		age := time.Now().Unix() - int64(n)
		ready = age >= 0 && age < 12
	}
	jsonOut(w, 200, map[string]any{"config": desired, "revision": localmounts.Revision(desired), "active": a.localMountConfig, "pending": localmounts.Revision(desired) != localmounts.Revision(a.localMountConfig), "hostReady": ready, "host": host, "response": readMountObject(filepath.Join(a.localMountDir(), "response.json"))})
}
func (a *App) localMountAction(w http.ResponseWriter, r *http.Request) {
	// Not registered as an AI/plugin capability. Native apply additionally needs
	// confirmation at the host desktop, and never exposes a Docker socket.
	a.mu.Lock()
	defer a.mu.Unlock()
	var in struct {
		Action   string `json:"action"`
		Revision string `json:"revision"`
	}
	if err := decode(w, r, &in); err != nil {
		fail(w, 400, err)
		return
	}
	if in.Action != "choose" && in.Action != "apply" {
		fail(w, 400, errors.New("未知目录操作"))
		return
	}
	host := readMountObject(filepath.Join(a.localMountDir(), "host.json"))
	n, _ := host["updated"].(float64)
	age := time.Now().Unix() - int64(n)
	if age < 0 || age >= 12 {
		fail(w, 409, errors.New("本机目录服务未连接，请用新版启动器重新启动 Aide"))
		return
	}
	pending := readMountObject(filepath.Join(a.localMountDir(), "request.json"))
	reply := readMountObject(filepath.Join(a.localMountDir(), "response.json"))
	if created, ok := pending["created"].(float64); ok && time.Now().Unix()-int64(created) < 180 && (reply["id"] != pending["id"] || reply["state"] == "running") {
		fail(w, 409, errors.New("已有目录操作等待完成，请先处理本机窗口"))
		return
	}
	c, err := localmounts.Read(filepath.Join(a.localMountDir(), "desired.json"))
	if err != nil {
		fail(w, 500, err)
		return
	}
	if in.Action == "apply" && in.Revision != localmounts.Revision(c) {
		fail(w, 409, errors.New("目录配置已变化，请重新确认"))
		return
	}
	id := newID()
	err = localmounts.Write(filepath.Join(a.localMountDir(), "request.json"), map[string]any{"id": id, "action": in.Action, "revision": in.Revision, "created": time.Now().Unix()})
	if err != nil {
		fail(w, 500, err)
		return
	}
	jsonOut(w, 202, map[string]string{"id": id})
}
func (a *App) mountRoot(which string) (*os.Root, error) {
	id := strings.TrimPrefix(which, "mount:")
	if r := a.localMountRoots[id]; r != nil {
		return r, nil
	}
	return nil, errors.New("目录尚未挂载或已不可用，请到设置检查")
}
