package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const marketRepository = "skyelan1999/aide-pkg-shop"
const marketUpdatePrefix = "https://github.com/" + marketRepository + "/tree/main/plugins/"

type marketInstallReceipt struct {
	SHA256 string
	ID     string
}

const marketRaw = "https://raw.githubusercontent.com/" + marketRepository + "/"

//go:embed plugin-market/catalog.json
var shippedMarket []byte

var marketSHA = regexp.MustCompile(`^[a-f0-9]{64}$`)
var marketRevision = regexp.MustCompile(`^[a-f0-9]{40}$`)
var marketVersion = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+){1,3}(?:[ -][A-Za-z0-9.-]+)?$`)

type marketCommerce struct {
	Status      string            `json:"status"`
	Model       string            `json:"model"`
	PriceLabel  string            `json:"priceLabel"`
	PurchaseURL string            `json:"purchaseURL,omitempty"`
	PublicKeys  map[string]string `json:"publicKeys,omitempty"`
}
type marketPlugin struct {
	SourceCompatibility *marketSourceCompatibility `json:"sourceCompatibility,omitempty"`
	Routing             *pluginRouteMetadata       `json:"routing,omitempty"`
	Distribution        string                     `json:"distribution,omitempty"`
	Commerce            *marketCommerce            `json:"commerce,omitempty"`
	Dependencies        []string                   `json:"dependencies"`
	ID                  string                     `json:"id"`
	Kind                string                     `json:"kind"`
	Name                string                     `json:"name"`
	Description         string                     `json:"description"`
	Version             string                     `json:"version"`
	Author              string                     `json:"author"`
	Category            string                     `json:"category"`
	Icon                string                     `json:"icon"`
	Package             string                     `json:"package"`
	SHA256              string                     `json:"sha256"`
	Bytes               int64                      `json:"bytes"`
	MinAideVersion      string                     `json:"minAideVersion"`
	Platforms           []string                   `json:"platforms"`
	Features            []string                   `json:"features"`
	Permissions         string                     `json:"permissions"`
	TrustNotice         string                     `json:"trustNotice"`
	Source              string                     `json:"source"`
	License             string                     `json:"license"`
	Readme              string                     `json:"readme"`
	Screenshots         []string                   `json:"screenshots"`
	Changelog           string                     `json:"changelog"`
}
type marketCatalog struct {
	SchemaVersion   int            `json:"schemaVersion"`
	Title           string         `json:"title"`
	SourceCommit    string         `json:"sourceCommit"`
	Plugins         []marketPlugin `json:"plugins"`
	PrivateListings []marketPlugin `json:"privateListings,omitempty"`
}

func (c marketCatalog) entries() []marketPlugin {
	return append(append([]marketPlugin{}, c.Plugins...), c.PrivateListings...)
}

type marketSnapshot struct {
	Checked      int64         `json:"checked,omitempty"`
	RefreshError string        `json:"refreshError,omitempty"`
	Revision     string        `json:"revision"`
	Fetched      int64         `json:"fetched"`
	Catalog      marketCatalog `json:"catalog"`
}

func validateMarket(s marketSnapshot) error {
	if !marketRevision.MatchString(s.Revision) || s.Catalog.SchemaVersion != 1 || len(s.Catalog.entries()) > 200 || len(s.Catalog.entries()) == 0 {
		return errors.New("官方市场目录版本无效")
	}
	seen := map[string]bool{}
	for _, p := range s.Catalog.entries() {
		if c := p.SourceCompatibility; c != nil {
			if !marketVersion.MatchString(c.MinAideVersion) || len(c.Features) == 0 || len(c.Features) > 16 {
				return errors.New("源码兼容条件无效")
			}
			seenFeatures := map[string]bool{}
			for _, f := range c.Features {
				if len(f) == 0 || len(f) > 80 || seenFeatures[f] || strings.ContainsAny(f, " \t\r\n\x00") {
					return errors.New("源码兼容能力无效")
				}
				seenFeatures[f] = true
			}
		}
		if err := validatePluginRouting(p.Routing); err != nil {
			return err
		}
		if !pluginIDPattern.MatchString(p.ID) || seen[p.ID] || p.Kind != "plugin" || len(p.Name) == 0 || len(p.Name) > 160 || len(p.Description) > 4096 || !marketVersion.MatchString(p.Version) || !marketVersion.MatchString(p.MinAideVersion) || len(p.Readme) > 32768 || len(p.Permissions) > 4096 || len(p.TrustNotice) > 4096 || len(p.Changelog) > 8192 || len(p.Dependencies) > 16 || len(p.Features) > 16 || len(p.Platforms) > 3 || len(p.Screenshots) > 6 {
			return errors.New("官方市场插件元数据无效")
		}
		if err := validateMarketDistribution(p); err != nil {
			return err
		}
		if p.Distribution != "private" && (path.Clean(p.Package) != p.Package || !strings.HasPrefix(p.Package, "packages/"+p.ID+"/") || !strings.HasSuffix(p.Package, ".zip") || strings.ContainsAny(p.Package, "\\:?#%") || strings.Contains(p.Package, "..")) {
			return errors.New("官方市场包路径无效")
		}
		if len(p.Source) > 2048 || !strings.HasPrefix(p.Source, "https://github.com/skyelan1999/") {
			return errors.New("官方市场源码地址无效")
		}
		for _, image := range p.Screenshots {
			if !marketScreenshotAllowed(p, image) {
				return errors.New("官方市场截图地址无效")
			}
		}
		for _, dependency := range p.Dependencies {
			if len(dependency) > 512 {
				return errors.New("插件依赖说明过长")
			}
		}
		seen[p.ID] = true
	}
	return nil
}

// Screenshot metadata may live in the shop or the listing's own official
// repository. Plugin-owned screenshots must be commit-pinned; accepting an
// arbitrary raw.githubusercontent.com path would expand the catalog boundary.
func marketScreenshotAllowed(p marketPlugin, raw string) bool {
	if len(raw) > 1024 || strings.ContainsAny(raw, "%\\\r\n\x00") {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != "raw.githubusercontent.com" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if len(parts) < 4 || parts[0] != "skyelan1999" {
		return false
	}
	for _, v := range parts {
		if v == "" || v == "." || v == ".." {
			return false
		}
	}
	if parts[1] == "aide-pkg-shop" {
		return true
	}
	source, err := url.Parse(p.Source)
	if err != nil || source.Scheme != "https" || source.Host != "github.com" || source.User != nil || source.RawQuery != "" || source.Fragment != "" {
		return false
	}
	sourceParts := strings.Split(strings.Trim(source.Path, "/"), "/")
	if len(sourceParts) != 2 && len(sourceParts) != 4 {
		return false
	}
	if sourceParts[0] != parts[0] || sourceParts[1] != parts[1] || !marketRevision.MatchString(parts[2]) {
		return false
	}
	if len(sourceParts) == 4 {
		return sourceParts[2] == "tree" && sourceParts[3] == parts[2]
	}
	return true
}

func (a *App) marketRoot() (*os.Root, error) {
	root, err := os.OpenRoot(a.dataPath)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	if err = root.MkdirAll("cache/plugin-market", 0700); err != nil {
		return nil, err
	}
	return root.OpenRoot("cache/plugin-market")
}
func (a *App) marketClient() *http.Client {
	base := a.pluginUpdateClient
	if base == nil {
		base = newPluginUpdateClient()
	}
	c := *base
	prior := base.CheckRedirect
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.Hostname() != "raw.githubusercontent.com" && req.URL.Hostname() != "api.github.com" {
			return errors.New("市场下载不能离开官方目录主机")
		}
		if prior != nil {
			return prior(req, via)
		}
		if len(via) > 4 {
			return errors.New("重定向次数过多")
		}
		return nil
	}
	return &c
}
func (a *App) loadMarket(ctx context.Context, refresh bool) (marketSnapshot, bool, error) {
	a.marketMu.Lock()
	defer a.marketMu.Unlock()
	var cached marketSnapshot
	root, err := a.marketRoot()
	if err != nil {
		return cached, false, err
	}
	defer root.Close()
	raw, err := skillReadCache(root, "catalog.json", 2<<20)
	if err == nil {
		if json.Unmarshal(raw, &cached) != nil || validateMarket(cached) != nil {
			cached = marketSnapshot{}
		}
	}
	if cached.Revision == "" {
		_ = json.Unmarshal(shippedMarket, &cached)
		if validateMarket(cached) != nil {
			cached = marketSnapshot{}
		}
	}
	age := time.Now().Unix() - cached.Fetched
	if !refresh && cached.RefreshError == "" && cached.Fetched > 0 && age >= 0 && age < 600 {
		return cached, false, nil
	}
	// Browsing must not wait for GitHub: render a validated snapshot first.
	if !refresh && cached.Revision != "" {
		return cached, true, nil
	}
	client := a.marketClient()
	ref, err := pluginFetch(ctx, client, "https://api.github.com/repos/"+marketRepository+"/git/ref/heads/main", 1<<20)
	var head struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	var fresh marketSnapshot
	if err == nil && json.Unmarshal(ref, &head) == nil && marketRevision.MatchString(head.Object.SHA) {
		fresh.Revision = head.Object.SHA
		raw, err = pluginFetch(ctx, client, marketRaw+fresh.Revision+"/catalog.json", 2<<20)
		if err == nil {
			err = json.Unmarshal(raw, &fresh.Catalog)
		}
		if err == nil {
			err = validateMarket(fresh)
		}
	} else if err == nil {
		err = errors.New("官方市场快照无效")
	}
	if err != nil {
		if cached.Revision != "" {
			// Keep a validated catalog available, but do not hide why it could
			// not be refreshed or pretend that an unsuccessful attempt was a sync.
			cached.Checked = time.Now().Unix()
			cached.RefreshError = err.Error()
			if feedback, encodeErr := json.Marshal(cached); encodeErr == nil {
				_ = skillCacheWrite(root, "catalog.json", feedback)
			}
			return cached, true, nil
		}
		return cached, false, err
	}
	fresh.Fetched = time.Now().Unix()
	fresh.Checked = fresh.Fetched
	raw, _ = json.Marshal(fresh)
	if err = skillCacheWrite(root, "catalog.json", raw); err != nil {
		return cached, false, err
	}
	return fresh, false, nil
}

// Compare numeric release components, then prerelease tokens (including legacy " RC2").
func marketCompare(a, b string) (int, bool) {
	parse := func(v string) ([]int, []string, bool) {
		v = strings.ReplaceAll(strings.TrimSpace(strings.TrimPrefix(v, "v")), " ", "-")
		pieces := strings.SplitN(v, "-", 2)
		core := strings.Split(pieces[0], ".")
		if len(core) < 2 || len(core) > 4 {
			return nil, nil, false
		}
		out := make([]int, 4)
		for i, s := range core {
			n, e := strconv.Atoi(s)
			if e != nil || n < 0 {
				return nil, nil, false
			}
			out[i] = n
		}
		var pre []string
		if len(pieces) > 1 {
			pre = regexp.MustCompile(`[a-z]+|[0-9]+`).FindAllString(strings.ToLower(pieces[1]), -1)
			if len(pre) == 0 {
				return nil, nil, false
			}
		}
		return out, pre, true
	}
	x, xp, ok := parse(a)
	y, yp, valid := parse(b)
	if !ok || !valid {
		return 0, false
	}
	for i := range x {
		if x[i] < y[i] {
			return -1, true
		}
		if x[i] > y[i] {
			return 1, true
		}
	}
	if len(xp) == 0 && len(yp) > 0 {
		return 1, true
	}
	if len(yp) == 0 && len(xp) > 0 {
		return -1, true
	}
	for i := 0; i < len(xp) && i < len(yp); i++ {
		if xp[i] == yp[i] {
			continue
		}
		xn, xe := strconv.Atoi(xp[i])
		yn, ye := strconv.Atoi(yp[i])
		if xe == nil && ye == nil {
			if xn < yn {
				return -1, true
			}
			return 1, true
		}
		if xe == nil {
			return -1, true
		}
		if ye == nil {
			return 1, true
		}
		if xp[i] < yp[i] {
			return -1, true
		}
		return 1, true
	}
	if len(xp) < len(yp) {
		return -1, true
	}
	if len(xp) > len(yp) {
		return 1, true
	}
	return 0, true
}
func marketVersionOlder(a, b string) bool { n, ok := marketCompare(a, b); return ok && n < 0 }
func marketCompatible(p marketPlugin, version string) (bool, string) {
	for _, f := range p.Features {
		if f != "provider-account-quota-v1" && f != "plugin-tool-images-v1" && f != "skill-library-v1" && f != "plugin-authoring-v1" && f != "private-plugin-v1" && f != "terminal-workbench-v1" && f != "terminal-ssh-workspace-v1" && f != "terminal-communications-v1" && f != "terminal-relay-v1" {
			return false, "当前 Aide 不支持此插件所需能力"
		}
	}
	if version != "dev" {
		compare, valid := marketCompare(version, p.MinAideVersion)
		if !valid || compare < 0 {
			return false, "需要 Aide " + p.MinAideVersion + " 或更新版本"
		}
	}
	return true, ""
}
func (a *App) marketCatalogAPI(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	snapshot, stale, err := a.loadMarket(ctx, r.URL.Query().Get("refresh") == "1")
	if err != nil {
		fail(w, 502, err)
		return
	}
	a.mu.Lock()
	installed := map[string]PluginManifest{}
	for _, p := range a.pluginRegistry.Plugins {
		installed[p.ID] = p
	}
	version := a.buildVersion
	ws, revision := a.wsID(), a.wsRevision
	a.mu.Unlock()
	entries := []map[string]any{}
	for _, p := range snapshot.Catalog.entries() {
		compatible, reason := marketCompatibleForRuntime(p, version, a.buildRuntimeMode)
		b, _ := json.Marshal(p)
		item := map[string]any{}
		_ = json.Unmarshal(b, &item)
		item["official"] = true
		item["compatible"] = compatible
		if baseOK, _ := marketCompatible(p, version); compatible && !baseOK {
			item["compatibilityMode"] = "source-capabilities"
		}
		item["incompatibility"] = reason
		if p.Distribution != "private" {
			item["downloadURL"] = "/api/plugin-market/" + p.ID + "/download?revision=" + snapshot.Revision
		}
		if local, ok := installed[p.ID]; ok {
			item["installedVersion"] = local.Version
			item["enabled"] = local.Enabled
			item["updateAvailable"] = marketVersionOlder(local.Version, p.Version)
			item["newerInstalled"] = marketVersionOlder(p.Version, local.Version)
			item["contentDiffers"] = local.Version == p.Version && local.PackageSHA256 != "" && local.PackageSHA256 != p.SHA256
		}
		entries = append(entries, item)
	}
	retryAfter := int64(0)
	if snapshot.RefreshError != "" {
		retryAfter = max(int64(0), 60-(time.Now().Unix()-snapshot.Checked))
	}
	jsonOut(w, 200, map[string]any{"plugins": entries, "revision": snapshot.Revision, "sourceCommit": snapshot.Catalog.SourceCommit, "stale": stale, "fetched": snapshot.Fetched, "checked": snapshot.Checked, "refreshError": snapshot.RefreshError, "retryAfter": retryAfter, "repository": "https://github.com/" + marketRepository, "workspace": ws, "workspaceRevision": revision})
}
func (a *App) marketPackage(ctx context.Context, s marketSnapshot, p marketPlugin) ([]byte, error) {
	if p.Distribution == "private" {
		return nil, errors.New("付费包不通过公开目录下载，请使用授权安装")
	}
	a.marketMu.Lock()
	defer a.marketMu.Unlock()
	root, err := a.marketRoot()
	if err != nil {
		return nil, err
	}
	defer root.Close()
	name := p.SHA256 + ".zip"
	matches := func(b []byte) bool {
		sum := sha256.Sum256(b)
		return int64(len(b)) == p.Bytes && hex.EncodeToString(sum[:]) == p.SHA256
	}
	if b, e := skillReadCache(root, name, maxPluginBundle); e == nil && matches(b) {
		if err := validateMarketPackage(b, p); err != nil {
			return nil, err
		}
		return b, nil
	}
	// Bound reusable public package cache without deleting any existing files.
	directory, e := root.Open(".")
	if e != nil {
		return nil, e
	}
	infos, e := directory.Readdir(-1)
	directory.Close()
	if e != nil {
		return nil, e
	}
	count := 0
	var used int64
	for _, i := range infos {
		if strings.HasSuffix(i.Name(), ".zip") {
			count++
			used += i.Size()
		}
	}
	if count >= 64 || used+p.Bytes > 256<<20 {
		return nil, errors.New("插件下载缓存已满，请先清理不再需要的下载缓存")
	}
	b, err := pluginFetch(ctx, a.marketClient(), marketRaw+s.Revision+"/"+p.Package, maxPluginBundle)
	if err != nil {
		return nil, err
	}
	if !matches(b) {
		return nil, errors.New("官方插件包摘要或大小不匹配，未安装")
	}
	if err := validateMarketPackage(b, p); err != nil {
		return nil, err
	}
	if err = skillCacheWrite(root, name, b); err != nil {
		return nil, err
	}
	return b, nil
}

func validateMarketPackage(b []byte, p marketPlugin) error {
	manifest, _, err := parsePluginBundle(b)
	if err != nil {
		return err
	}
	if manifest.ID != p.ID || manifest.Version != p.Version {
		return errors.New("插件包 ID 或版本与目录不符")
	}
	return nil
}
func (a *App) marketSelected(ctx context.Context, id, revision string) (marketSnapshot, marketPlugin, error) {
	s, _, err := a.loadMarket(ctx, false)
	if err != nil {
		return s, marketPlugin{}, err
	}
	if s.Revision != revision {
		return s, marketPlugin{}, errors.New("市场目录已更新，请刷新并重新确认")
	}
	for _, p := range s.Catalog.entries() {
		if p.ID == id {
			return s, p, nil
		}
	}
	return s, marketPlugin{}, errors.New("官方目录中没有此插件")
}
func (a *App) marketDownload(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	s, p, err := a.marketSelected(ctx, r.PathValue("id"), r.URL.Query().Get("revision"))
	if err != nil {
		fail(w, 409, err)
		return
	}
	if p.Distribution == "private" {
		fail(w, 403, errors.New("付费包不提供公开下载，请在详情中授权安装"))
		return
	}
	raw, err := a.marketPackage(ctx, s, p)
	if err != nil {
		fail(w, 502, err)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="aide-pkg-%s-v%s.zip"`, p.ID, strings.ReplaceAll(p.Version, " ", "-")))
	w.Header().Set("X-Content-SHA256", p.SHA256)
	w.Write(raw)
}
func (a *App) marketInstall(w http.ResponseWriter, r *http.Request) {
	if !a.marketInstallMu.TryLock() {
		fail(w, 409, errors.New("已有市场安装正在处理"))
		return
	}
	defer a.marketInstallMu.Unlock()
	var in struct {
		Revision          string              `json:"revision"`
		Version           string              `json:"version"`
		SHA256            string              `json:"sha256"`
		Workspace         string              `json:"workspace"`
		WorkspaceRevision uint64              `json:"workspaceRevision"`
		License           signedPluginLicense `json:"license"`
	}
	var privateBundle []byte
	var inputErr error
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		privateBundle, inputErr = readPrivateInstall(w, r, &in)
	} else {
		inputErr = decode(w, r, &in)
	}
	if err := inputErr; err != nil {
		fail(w, 400, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	a.mu.Lock()
	ws, rev, scope := a.wsID(), a.wsRevision, a.pluginsPath
	base := a.managementBaseLocked("plugins", r.PathValue("id"))
	lockGeneration := a.loadLockStateLocked().Gen
	a.mu.Unlock()
	guard := func() error {
		if err := managementGuardLocked(a, r); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		lock := a.loadLockStateLocked()
		if lock.Locked || lock.Gen != lockGeneration {
			return errors.New("工作台已锁定或锁定状态变化，请重新确认")
		}
		if sandboxMode(a.settings.SandboxMode) == "read-only" {
			return errors.New("只读沙箱不能安装插件")
		}
		if in.Workspace != ws || in.WorkspaceRevision != rev || a.wsID() != ws || a.wsRevision != rev || a.pluginsPath != scope {
			return errors.New("工作空间已变化，请重新确认")
		}
		if a.managementBaseLocked("plugins", r.PathValue("id")) != base {
			return errors.New("插件状态已变化，请刷新后重试")
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

	s, p, err := a.marketSelected(ctx, r.PathValue("id"), in.Revision)
	if err != nil {
		fail(w, 409, err)
		return
	}
	if p.SHA256 != in.SHA256 || p.Version != in.Version {
		fail(w, 409, errors.New("插件版本或摘要已变化，请重新确认"))
		return
	}
	if ok, reason := marketCompatibleForRuntime(p, a.buildVersion, a.buildRuntimeMode); !ok {
		fail(w, 409, errors.New(reason))
		return
	}
	var raw []byte
	if p.Distribution == "private" {
		if p.Commerce == nil || p.Commerce.Status != "available" {
			fail(w, 403, errors.New("插件尚未开放授权安装"))
			return
		}
		raw = privateBundle
		a.marketMu.Lock()
		device, identityErr := a.pluginLicenseDevice()
		a.marketMu.Unlock()
		if identityErr != nil {
			fail(w, 500, identityErr)
			return
		}
		err = verifyPrivateBundle(p, in.License, device, raw)
	} else if len(privateBundle) != 0 {
		err = errors.New("公开插件不接受私有包上传")
	} else {
		raw, err = a.marketPackage(ctx, s, p)
	}
	if err != nil {
		code := 502
		if p.Distribution == "private" {
			code = 403
		}
		fail(w, code, err)
		return
	}
	if p.Distribution == "private" {
		sum := sha256.Sum256(raw)
		p.SHA256 = hex.EncodeToString(sum[:])
		p.Bytes = int64(len(raw))
	}

	a.mu.Lock()
	err = guard()
	_, installed := func() (PluginManifest, bool) {
		for _, x := range a.pluginRegistry.Plugins {
			if x.ID == p.ID {
				return x, true
			}
		}
		return PluginManifest{}, false
	}()
	a.mu.Unlock()
	if err != nil {
		fail(w, 409, err)
		return
	}
	ctx = context.WithValue(ctx, managementGuardKey{}, guard)
	ctx = context.WithValue(ctx, marketInstallReceipt{}, marketInstallReceipt{SHA256: p.SHA256, ID: p.ID})
	result := httptest.NewRecorder()
	if installed {
		ctx = context.WithValue(ctx, managementBundleKey{}, raw)
		body, _ := json.Marshal(map[string]string{"sha256": p.SHA256})
		req, _ := http.NewRequestWithContext(ctx, "POST", "/api/plugins/"+p.ID+"/upgrade", bytes.NewReader(body))
		req.SetPathValue("id", p.ID)
		a.upgradePlugin(result, req)
	} else {
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		part, e := form.CreateFormFile("bundle", "plugin.zip")
		if e != nil {
			fail(w, 500, e)
			return
		}
		_, _ = part.Write(raw)
		_ = form.Close()
		req, _ := http.NewRequestWithContext(ctx, "POST", "/api/plugins/bundle", &body)
		req.Header.Set("Content-Type", form.FormDataContentType())
		a.uploadPluginBundle(result, req)
	}
	for k, values := range result.Header() {
		for _, v := range values {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(result.Code)
	_, _ = w.Write(result.Body.Bytes())
}
