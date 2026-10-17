package server

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const officialPluginPrefix = "https://github.com/skyelan1999/aide/tree/main/plugins/"

var officialPluginIDs = strings.Fields("archive-tools browser-control computer-control comm-serial comm-ssh comm-tcp comm-udp current-time environment-guide feedback goal lunar-calendar markdown-history office plan plugin-authoring propeller-aero skill sqlite subagent terminal todo workflow")

func pluginUpdateSource(p PluginManifest) string {
	if p.UpdateURL != "" {
		return p.UpdateURL
	}
	for _, id := range officialPluginIDs {
		if id == p.ID {
			return officialPluginPrefix + id
		}
	}
	return ""
}
func (a *App) pluginPackageDigestLocked(id string) string {
	for _, p := range a.pluginRegistry.Plugins {
		if p.ID == id {
			return p.PackageSHA256
		}
	}
	return ""
}
func pluginPublicIP(ip net.IP) bool {
	if ip == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	if v := ip.To4(); v != nil {
		return v[0] != 0 && !(v[0] == 100 && v[1] >= 64 && v[1] <= 127) && !(v[0] == 198 && (v[1] == 18 || v[1] == 19)) && !(v[0] == 192 && v[1] == 0 && v[2] <= 2) && !(v[0] == 198 && v[1] == 51 && v[2] == 100) && !(v[0] == 203 && v[1] == 0 && v[2] == 113)
	}
	return !(ip[0] == 0x20 && ip[1] == 0x01 && ip[2] == 0x0d && ip[3] == 0xb8)
}

// Local TUN proxies commonly resolve GitHub to RFC2544 Fake-IP addresses.
// Permit that range only for fixed first-party GitHub names; TLS still checks
// the original hostname. Literal benchmark/private URLs remain forbidden.
func pluginResolvedIPAllowed(host string, ip net.IP) bool {
	if pluginPublicIP(ip) {
		return true
	}
	v := ip.To4()
	if v == nil || v[0] != 198 || (v[1] != 18 && v[1] != 19) {
		return false
	}
	switch strings.ToLower(host) {
	case "api.github.com", "github.com", "raw.githubusercontent.com", "objects.githubusercontent.com", "release-assets.githubusercontent.com", "codeload.github.com":
		return true
	}
	return false
}
func pluginUpdateURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 2048 || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
		return nil, errors.New("升级地址必须是不含登录凭据的 HTTPS 地址")
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil && !pluginPublicIP(ip) {
		return nil, errors.New("升级地址不能指向本机或私有网络")
	}
	return u, nil
}
func newPluginUpdateClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, errors.New("升级服务器地址解析失败")
		}
		if len(ips) == 0 {
			return nil, errors.New("升级服务器无可用地址")
		}
		for _, ip := range ips {
			if !pluginResolvedIPAllowed(host, ip.IP) {
				return nil, errors.New("升级服务器不能指向本机或私有网络")
			}
		}
		var last error
		for _, ip := range ips {
			var conn net.Conn
			conn, last = (&net.Dialer{Timeout: 15 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
			if last == nil {
				return conn, nil
			}
		}
		return nil, last
	}
	return &http.Client{Transport: transport, Timeout: 45 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("升级下载重定向次数过多")
		}
		_, err := pluginUpdateURL(req.URL.String())
		return err
	}}
}
func pluginFetch(ctx context.Context, client *http.Client, raw string, limit int64) ([]byte, error) {
	if _, err := pluginUpdateURL(raw); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "GET", raw, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "aide-plugin-upgrade")
	req.Header.Set("Accept", "application/vnd.github+json, application/zip, application/octet-stream")
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("插件下载失败，请检查网络和升级地址")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("插件下载返回 HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > limit {
		return nil, errors.New("插件下载超过大小限制")
	}
	rawBytes, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, errors.New("插件下载中断")
	}
	if int64(len(rawBytes)) > limit {
		return nil, errors.New("插件下载超过大小限制")
	}
	return rawBytes, nil
}

// Official updates pin all files to one immutable commit, not independently
// moving main URLs. Only the selected plugin directory is downloaded.
func officialPluginBundle(ctx context.Context, client *http.Client, id string) ([]byte, error) {
	raw, err := pluginFetch(ctx, client, "https://api.github.com/repos/skyelan1999/aide/git/ref/heads/main", 1<<20)
	if err != nil {
		return nil, err
	}
	var ref struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if json.Unmarshal(raw, &ref) != nil || len(ref.Object.SHA) != 40 {
		return nil, errors.New("官方插件版本信息无效")
	}
	if _, err = hex.DecodeString(ref.Object.SHA); err != nil {
		return nil, errors.New("官方插件版本信息无效")
	}
	type entry struct {
		Name string `json:"name"`
		Path string `json:"path"`
		Type string `json:"type"`
		SHA  string `json:"sha"`
		Size int64  `json:"size"`
	}
	files := map[string][]byte{}
	var expanded int64
	var visit func(string) error
	visit = func(dir string) error {
		if len(files) >= maxPluginFiles {
			return errors.New("官方插件文件数超过限制")
		}
		raw, err := pluginFetch(ctx, client, "https://api.github.com/repos/skyelan1999/aide/contents/plugins/"+id+dir+"?ref="+ref.Object.SHA, 1<<20)
		if err != nil {
			return err
		}
		var entries []entry
		if json.Unmarshal(raw, &entries) != nil {
			return errors.New("官方插件目录信息无效")
		}
		for _, e := range entries {
			expected := "plugins/" + id + dir + "/" + e.Name
			if e.Path != expected || e.Name == "" || e.Name == "." || e.Name == ".." || strings.ContainsAny(e.Name, "/\\") || strings.HasPrefix(e.Name, ".") {
				return errors.New("官方插件路径无效")
			}
			if e.Type == "dir" {
				if strings.Count(dir, "/") >= 8 {
					return errors.New("官方插件目录过深")
				}
				if err := visit(dir + "/" + e.Name); err != nil {
					return err
				}
				continue
			}
			if e.Type != "file" || e.Size < 0 || e.Size > 16<<20 || expanded+e.Size > maxPluginExpanded || len(files) >= maxPluginFiles {
				return errors.New("官方插件文件类型或大小不符合限制")
			}
			contents, err := pluginFetch(ctx, client, "https://raw.githubusercontent.com/skyelan1999/aide/"+ref.Object.SHA+"/"+expected, 16<<20)
			if err != nil {
				return err
			}
			if int64(len(contents)) != e.Size {
				return errors.New("官方插件文件长度不匹配")
			}
			digest := sha1.Sum(append([]byte(fmt.Sprintf("blob %d\x00", len(contents))), contents...))
			if hex.EncodeToString(digest[:]) != e.SHA {
				return errors.New("官方插件文件摘要不匹配")
			}
			files[strings.TrimPrefix(dir+"/"+e.Name, "/")] = contents
			expanded += int64(len(contents))
		}
		return nil
	}
	if err = visit(""); err != nil {
		return nil, err
	}
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		f, err := writer.Create(name)
		if err != nil {
			return nil, err
		}
		if _, err = f.Write(files[name]); err != nil {
			return nil, err
		}
	}
	if err = writer.Close(); err != nil {
		return nil, err
	}
	if buffer.Len() > maxPluginBundle {
		return nil, errors.New("插件 ZIP 包超过 24 MiB")
	}
	return buffer.Bytes(), nil
}

// Copy existing local data and dependencies into the candidate. Never follow
// links outside the plugin, special files, or an unbounded local tree.
func copyPluginForUpgrade(source, dest string) error {
	var count int
	var size int64
	rooted, err := os.OpenRoot(source)
	if err != nil {
		return err
	}
	defer rooted.Close()
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		count++
		if count > 50000 {
			return errors.New("插件本地文件过多，未执行替换")
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, rel)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		if info.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			resolved := filepath.Clean(filepath.Join(filepath.Dir(path), link))
			if filepath.IsAbs(link) || !pluginResourceContains(source, resolved) {
				return errors.New("插件含指向外部的链接，未执行替换")
			}
			return os.Symlink(link, target)
		}
		if !info.Mode().IsRegular() {
			return errors.New("插件含特殊文件，未执行替换")
		}
		size += info.Size()
		if size > 1<<30 {
			return errors.New("插件本地数据超过 1 GiB，未执行替换")
		}
		input, err := rooted.OpenFile(rel, os.O_RDONLY|syscall.O_NONBLOCK, 0)
		if err != nil {
			return err
		}
		current, err := input.Stat()
		if err != nil || !current.Mode().IsRegular() {
			input.Close()
			return errors.New("插件文件在复制时已变化")
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
		if err != nil {
			input.Close()
			return err
		}
		_, err = io.CopyN(out, input, info.Size())
		_ = input.Close()
		closeErr := out.Close()
		if err != nil {
			return err
		}
		return closeErr
	})
}
func writePluginCandidate(root string, files map[string][]byte) error {
	for rel, contents := range files {
		if rel == "manifest.json" {
			continue
		}
		target := filepath.Join(root, filepath.FromSlash(rel))
		parent := filepath.Dir(target)
		for p := parent; p != root; p = filepath.Dir(p) {
			if info, err := os.Lstat(p); err == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
				return errors.New("新版文件与本地链接或文件冲突")
			}
		}
		if err := os.MkdirAll(parent, 0755); err != nil {
			return err
		}
		temp, err := os.CreateTemp(parent, ".upgrade-file-")
		if err != nil {
			return err
		}
		name := temp.Name()
		_, err = temp.Write(contents)
		if err == nil {
			err = temp.Sync()
		}
		ce := temp.Close()
		if err == nil {
			err = ce
		}
		if err == nil {
			err = os.Chmod(name, 0644)
		}
		if err == nil {
			err = os.Rename(name, target)
		}
		_ = os.Remove(name)
		if err != nil {
			return err
		}
	}
	return nil
}

type pluginUpgradeJournal struct {
	ID     string         `json:"id"`
	Backup string         `json:"backup"`
	Stage  string         `json:"stage"`
	Digest string         `json:"digest"`
	Old    PluginManifest `json:"old"`
}

func (a *App) recoverPluginUpgrades() error {
	names, err := filepath.Glob(filepath.Join(a.pluginsPath, ".upgrade-journal-*.json"))
	if err != nil {
		return err
	}
	for _, name := range names {
		info, err := os.Lstat(name)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() > 128<<10 {
			return errors.New("插件升级恢复记录类型或大小无效")
		}
		raw, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		var journal pluginUpgradeJournal
		if json.Unmarshal(raw, &journal) != nil || !pluginIDPattern.MatchString(journal.ID) || journal.Old.ID != journal.ID || filepath.Base(journal.Backup) != journal.Backup || !strings.HasPrefix(journal.Backup, ".upgrade-backup-") || filepath.Base(journal.Stage) != journal.Stage || !strings.HasPrefix(journal.Stage, ".upgrade-stage-") {
			return errors.New("插件升级恢复记录无效")
		}
		committed := false
		index := -1
		for i, p := range a.pluginRegistry.Plugins {
			if p.ID == journal.ID {
				index = i
				committed = p.PackageSHA256 == journal.Digest && journal.Digest != ""
				break
			}
		}
		destination := filepath.Join(a.pluginsPath, journal.ID)
		backup := filepath.Join(a.pluginsPath, journal.Backup)
		if committed {
			if info, e := os.Lstat(destination); e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return errors.New("已提交插件升级目录缺失，需要恢复备份")
			}
		} else if info, e := os.Lstat(backup); e == nil {
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return errors.New("插件升级备份目录无效")
			}
			failed := filepath.Join(a.pluginsPath, ".upgrade-failed-"+journal.ID+"-"+newID()[:8])
			if _, e := os.Lstat(destination); e == nil {
				if e = os.Rename(destination, failed); e != nil {
					return e
				}
			} else if !errors.Is(e, os.ErrNotExist) {
				return e
			}
			if err = os.Rename(backup, destination); err != nil {
				return err
			}
			if index >= 0 {
				a.pluginRegistry.Plugins[index] = journal.Old
			} else {
				a.pluginRegistry.Plugins = append(a.pluginRegistry.Plugins, journal.Old)
			}
			if err = a.savePluginRegistry(); err != nil {
				return err
			}
		} else if !errors.Is(e, os.ErrNotExist) {
			return e
		}
		if err = os.RemoveAll(filepath.Join(a.pluginsPath, journal.Stage)); err != nil {
			return err
		}
		if err = os.Remove(name); err != nil {
			return err
		}
	}
	return nil
}
func (a *App) probePluginUpgrade(ctx context.Context, root, id string, entries []PluginManifest) ([]byte, error) {
	list := []map[string]any{}
	for _, p := range entries {
		if p.ID != id {
			if !p.Enabled {
				continue
			}
			if err := os.Symlink(filepath.Join(a.pluginsPath, p.ID), filepath.Join(root, p.ID)); err != nil {
				return nil, err
			}
		}
		list = append(list, map[string]any{"id": p.ID, "name": p.Name, "main": p.Main, "settings": p.Settings})
	}
	encoded, _ := json.Marshal(map[string]any{"plugins": list, "reservedTools": builtinToolNames()})
	out := filepath.Join(root, "surface.json")
	cctx, cancel := context.WithTimeout(ctx, pluginRunTimeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, "node", "-e", pluginHostJS, "run", root, string(encoded), out)
	configurePluginProcess(cmd, true)
	cmd.Env = []string{"PATH=/usr/local/bin:/usr/bin:/bin", "HOME=/home/aide"}
	log := &limitedBytesWriter{limit: 64 << 10}
	cmd.Stdout = log
	cmd.Stderr = log
	err := cmd.Run()
	_ = killPluginProcess(cmd)
	if err != nil {
		return nil, errors.New("新版插件加载失败或超时，原插件未替换")
	}
	raw, err := os.ReadFile(out)
	if err != nil || len(raw) > 2<<20 {
		return nil, errors.New("新版插件未返回有效能力清单")
	}
	var surface struct {
		Plugins []struct {
			ID    string `json:"id"`
			Error string `json:"error"`
		} `json:"plugins"`
	}
	if json.Unmarshal(raw, &surface) != nil {
		return nil, errors.New("新版插件能力清单无效")
	}
	found := false
	for _, p := range surface.Plugins {
		if p.ID == id {
			found = true
			if p.Error != "" {
				return nil, fmt.Errorf("新版插件加载失败：%s", p.Error)
			}
		}
	}
	if !found {
		return nil, errors.New("新版插件未被加载")
	}
	return raw, nil
}

// A provider update must not silently break a previously healthy consumer.
func validatePluginUpgradeDependents(before, after []byte, target string) error {
	type item struct {
		ID    string `json:"id"`
		Error string `json:"error"`
	}
	var oldSurface, newSurface struct {
		Plugins []item `json:"plugins"`
	}
	if json.Unmarshal(after, &newSurface) != nil {
		return errors.New("新版插件能力清单无效")
	}
	_ = json.Unmarshal(before, &oldSurface)
	healthy := map[string]bool{}
	for _, p := range oldSurface.Plugins {
		healthy[p.ID] = p.Error == ""
	}
	for _, p := range newSurface.Plugins {
		if p.ID != target && healthy[p.ID] && p.Error != "" {
			return fmt.Errorf("新版插件影响依赖插件 %s：%s", p.ID, p.Error)
		}
	}
	return nil
}
func pluginVersionDowngrade(oldVersion, newVersion string) bool {
	parse := func(value string) ([]int, string, bool) {
		parts := strings.SplitN(strings.SplitN(strings.TrimPrefix(value, "v"), "+", 2)[0], "-", 2)
		core := strings.Split(parts[0], ".")
		if len(core) < 2 || len(core) > 4 {
			return nil, "", false
		}
		numbers := make([]int, 4)
		for i, part := range core {
			n, err := strconv.Atoi(part)
			if err != nil || n < 0 {
				return nil, "", false
			}
			numbers[i] = n
		}
		suffix := ""
		if len(parts) > 1 {
			suffix = parts[1]
		}
		return numbers, suffix, true
	}
	old, oldPre, ok := parse(oldVersion)
	next, newPre, nextOK := parse(newVersion)
	if !ok || !nextOK {
		return false
	}
	for i := range old {
		if old[i] != next[i] {
			return next[i] < old[i]
		}
	}
	return oldPre == "" && newPre != ""
}
func (a *App) upgradePlugin(w http.ResponseWriter, r *http.Request) {
	if !a.pluginUpgradeMu.TryLock() {
		fail(w, 409, errors.New("已有插件正在升级，请稍后重试"))
		return
	}
	defer a.pluginUpgradeMu.Unlock()
	var in struct {
		URL    string `json:"url"`
		SHA256 string `json:"sha256"`
	}
	if err := decode(w, r, &in); err != nil {
		fail(w, 400, err)
		return
	}
	id := r.PathValue("id")
	a.mu.Lock()
	if err := managementGuardLocked(a, r); err != nil {
		a.mu.Unlock()
		fail(w, 409, err)
		return
	}
	scope, epoch := a.pluginsPath, a.wsRevision
	marketBase, marketGen, marketWS := a.managementBaseLocked("plugins", id), a.loadLockStateLocked().Gen, a.wsID()
	var old PluginManifest
	for _, p := range a.pluginRegistry.Plugins {
		if p.ID == id {
			old = p
			break
		}
	}
	a.mu.Unlock()
	if old.ID == "" {
		fail(w, 404, errors.New("插件不存在"))
		return
	}
	localBundle, hasLocalBundle := r.Context().Value(managementBundleKey{}).([]byte)
	source := strings.TrimSpace(in.URL)
	if source == "" {
		source = pluginUpdateSource(old)
	}
	if source == "" && !hasLocalBundle {
		fail(w, 400, errors.New("请先填写插件的 HTTPS ZIP 升级地址"))
		return
	}
	if !hasLocalBundle {
		if _, err := pluginUpdateURL(source); err != nil {
			fail(w, 400, err)
			return
		}
	}
	// The installed-list upgrade button must retain the same market revocation boundary.
	if source == marketUpdatePrefix+id && !hasLocalBundle {
		parent := r.Context()
		prior, _ := parent.Value(managementGuardKey{}).(func() error)
		guard := func() error {
			if err := parent.Err(); err != nil {
				return err
			}
			if prior != nil {
				if err := prior(); err != nil {
					return err
				}
			}
			lock := a.loadLockStateLocked()
			if lock.Locked || lock.Gen != marketGen || sandboxMode(a.settings.SandboxMode) == "read-only" {
				return errors.New("锁定或只读状态不能更新市场插件")
			}
			if a.wsID() != marketWS || a.wsRevision != epoch || a.pluginsPath != scope || a.managementBaseLocked("plugins", id) != marketBase {
				return errors.New("工作空间或插件已变化，请重新确认")
			}
			return nil
		}
		a.mu.Lock()
		err := guard()
		a.mu.Unlock()
		if err != nil {
			fail(w, 409, err)
			return
		}
		r = r.WithContext(context.WithValue(parent, managementGuardKey{}, guard))
	}
	expected := strings.ToLower(strings.TrimSpace(in.SHA256))
	if expected != "" {
		if b, err := hex.DecodeString(expected); err != nil || len(b) != 32 {
			fail(w, 400, errors.New("SHA-256 必须为 64 位十六进制摘要"))
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	downloadCtx, downloadCancel := context.WithTimeout(ctx, 2*time.Minute)
	defer downloadCancel()
	client := a.pluginUpdateClient
	if client == nil {
		client = newPluginUpdateClient()
	}
	var raw []byte
	var err error
	if hasLocalBundle {
		raw = localBundle
		source = old.UpdateURL
	} else if source == marketUpdatePrefix+id {
		var snapshot marketSnapshot
		snapshot, _, err = a.loadMarket(downloadCtx, true)
		if err == nil {
			found := false
			for _, entry := range snapshot.Catalog.Plugins {
				if entry.ID != id {
					continue
				}
				found = true
				if compatible, reason := marketCompatible(entry, a.buildVersion); !compatible {
					err = errors.New(reason)
					break
				}
				raw, err = a.marketPackage(downloadCtx, snapshot, entry)
				break
			}
			if !found {
				err = errors.New("官方市场未收录此插件")
			}
		}
	} else if source == officialPluginPrefix+id {
		raw, err = officialPluginBundle(downloadCtx, client, id)
	} else {
		raw, err = pluginFetch(downloadCtx, client, source, maxPluginBundle)
	}
	if err != nil {
		fail(w, 502, err)
		return
	}
	digest := sha256.Sum256(raw)
	sum := hex.EncodeToString(digest[:])
	if expected != "" && expected != sum {
		fail(w, 400, errors.New("插件包 SHA-256 校验不通过，原插件未替换"))
		return
	}
	manifest, files, err := parsePluginBundle(raw)
	if err != nil {
		fail(w, 400, err)
		return
	}
	if manifest.ID != id {
		fail(w, 400, errors.New("升级包插件 ID 与当前插件不一致"))
		return
	}
	if len(manifest.Version) > 128 {
		fail(w, 400, errors.New("插件版本号过长"))
		return
	}
	if old.Version != "" && strings.TrimSpace(manifest.Version) == "" {
		fail(w, 400, errors.New("升级包缺少版本号"))
		return
	}
	if pluginVersionDowngrade(old.Version, manifest.Version) {
		fail(w, 400, errors.New("升级包版本低于当前版本，未执行替换"))
		return
	}
	a.pluginLifecycle.Lock()
	defer a.pluginLifecycle.Unlock()
	release, err := a.pluginResources.acquire(ctx, pluginFootprint{exclusive: true})
	if err != nil {
		fail(w, 409, errors.New("等待插件调用结束时已取消或超时"))
		return
	}
	defer release()
	a.mu.Lock()
	index := -1
	for i, p := range a.pluginRegistry.Plugins {
		if p.ID == id {
			index = i
			if !reflect.DeepEqual(old, p) {
				index = -1
			}
			break
		}
	}
	if err := managementGuardLocked(a, r); err != nil {
		a.mu.Unlock()
		fail(w, 409, err)
		return
	}
	changed := a.pluginsPath != scope || a.wsRevision != epoch
	a.mu.Unlock()
	if changed || index < 0 {
		fail(w, 409, errors.New("工作区或插件已变化，请刷新后重试"))
		return
	}
	if old.PackageSHA256 == sum && old.UpdateURL == source {
		jsonOut(w, 200, map[string]any{"id": id, "version": old.Version, "unchanged": true})
		return
	}
	info, err := os.Lstat(filepath.Join(scope, id))
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		fail(w, 400, errors.New("当前插件目录无效，未执行替换"))
		return
	}
	wasRunning := false
	if a.daemons != nil {
		a.daemons.mu.Lock()
		proc := a.daemons.procs[id]
		a.daemons.mu.Unlock()
		if proc != nil {
			status := proc.statusSnapshot()
			wasRunning = status == dsRunning || status == dsBackoff
		}
		if err = a.daemons.Stop(id); err != nil {
			fail(w, 500, err)
			return
		}
	}
	restartOld := func() {
		if wasRunning && a.daemons != nil {
			_ = a.daemons.StartWithSettings(id, filepath.Join(scope, id, old.Main), old.Settings)
		}
	}
	stage, err := os.MkdirTemp(scope, ".upgrade-stage-")
	if err != nil {
		restartOld()
		fail(w, 500, err)
		return
	}
	defer os.RemoveAll(stage)
	candidate := filepath.Join(stage, id)
	if err = copyPluginForUpgrade(filepath.Join(scope, id), candidate); err == nil {
		err = writePluginCandidate(candidate, files)
	}
	if err != nil {
		restartOld()
		fail(w, 400, err)
		return
	}
	manifest.Enabled = old.Enabled
	manifest.Settings = old.Settings
	manifest.InstalledAt = old.InstalledAt
	manifest.UpdateURL = source
	manifest.PackageSHA256 = sum
	manifest.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	manifest.UpgradeBackup = ".upgrade-backup-" + id + "-" + newID()
	if err = atomicJSON(filepath.Join(candidate, "manifest.json"), manifest); err != nil {
		restartOld()
		fail(w, 500, err)
		return
	}
	a.mu.Lock()
	entries := append([]PluginManifest(nil), a.pluginRegistry.Plugins...)
	beforeSurface := append([]byte(nil), a.pluginSurface...)
	a.mu.Unlock()
	entries[index] = manifest
	surface, err := a.probePluginUpgrade(ctx, stage, id, entries)
	if err == nil {
		err = validatePluginUpgradeDependents(beforeSurface, surface, id)
	}
	if err != nil {
		restartOld()
		fail(w, 400, err)
		return
	}
	if ctx.Err() != nil {
		restartOld()
		fail(w, 409, errors.New("升级已取消，原插件未替换"))
		return
	}
	if _, statErr := os.Lstat(filepath.Join(scope, manifest.UpgradeBackup)); !errors.Is(statErr, os.ErrNotExist) {
		restartOld()
		fail(w, 500, errors.New("无法创建独立的插件升级备份"))
		return
	}
	journal := pluginUpgradeJournal{id, manifest.UpgradeBackup, filepath.Base(stage), sum, old}
	journalBytes, _ := json.MarshalIndent(journal, "", "  ")
	if len(journalBytes) > 128<<10 {
		restartOld()
		fail(w, 400, errors.New("插件升级恢复记录过大，原插件未替换"))
		return
	}
	journalPath := filepath.Join(scope, ".upgrade-journal-"+id+".json")
	if err = atomicJSON(journalPath, journal); err != nil {
		restartOld()
		fail(w, 500, err)
		return
	}
	destination := filepath.Join(scope, id)
	backup := filepath.Join(scope, journal.Backup)
	a.mu.Lock()
	if err := managementGuardLocked(a, r); err != nil {
		a.mu.Unlock()
		_ = os.Remove(journalPath)
		restartOld()
		fail(w, 409, err)
		return
	}
	registrySnapshot, _ := json.Marshal(a.pluginRegistry)
	a.mu.Unlock()
	// Once swapping begins finish the transaction even if the client disconnects.
	a.mu.Lock()
	changed = a.pluginsPath != scope || a.wsRevision != epoch
	guardErr := managementGuardLocked(a, r)
	a.mu.Unlock()
	if changed || guardErr != nil {
		_ = os.Remove(journalPath)
		restartOld()
		fail(w, 409, errors.New("工作区已变化，原插件未替换"))
		return
	}
	if err = os.Rename(destination, backup); err == nil {
		err = atomicJSON(filepath.Join(backup, ".aide-upgrade-registry.json"), json.RawMessage(registrySnapshot))
	}
	if err == nil {
		err = os.Rename(candidate, destination)
	}
	if err == nil && manifest.Enabled && manifest.Daemon && (wasRunning || !old.Daemon) && a.daemons != nil {
		err = a.daemons.StartWithSettings(id, filepath.Join(destination, manifest.Main), manifest.Settings)
	}
	if err == nil {
		a.mu.Lock()
		if a.pluginsPath != scope || a.wsRevision != epoch {
			err = errors.New("工作区已变化")
		}
		if err == nil {
			a.pluginRegistry.Plugins[index] = manifest
			err = a.savePluginRegistry()
			if err != nil {
				a.pluginRegistry.Plugins[index] = old
			}
			if err == nil && manifest.Enabled {
				a.pluginSurface = surface
			}
		}
		a.mu.Unlock()
	}
	if err != nil {
		if a.daemons != nil {
			_ = a.daemons.Stop(id)
		}
		a.mu.Lock()
		recoverErr := a.recoverPluginUpgrades()
		a.mu.Unlock()
		restartOld()
		if recoverErr != nil {
			fail(w, 500, fmt.Errorf("升级失败，自动恢复未完成：%v", recoverErr))
			return
		}
		fail(w, 500, errors.New("升级失败，已恢复原插件"))
		return
	}
	// Keep old files and its registry snapshot as a recoverable rollback set.
	_ = os.Remove(journalPath)
	a.runPluginHost(context.Background())
	jsonOut(w, 200, map[string]any{"id": id, "version": manifest.Version, "previousVersion": old.Version, "updatedAt": manifest.UpdatedAt, "backup": journal.Backup, "enabled": manifest.Enabled})
}
