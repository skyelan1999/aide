package server

import (
	"archive/zip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
)

var releaseTagPattern = regexp.MustCompile(`^v([0-9]+\.[0-9]+\.[0-9]+\.[0-9]+)-RC([0-9]+)$`)
var displayVersionPattern = regexp.MustCompile(`^v?([0-9]+\.[0-9]+\.[0-9]+\.[0-9]+)[ -]RC([0-9]+)$`)
var updatePackageIDPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)
var updateImageIDPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

const updatePackageMaxBytes = 2 << 30

type updateSlot struct {
	Version     string `json:"version,omitempty"`
	Tag         string `json:"tag,omitempty"`
	ImageRef    string `json:"imageRef,omitempty"`
	ImageID     string `json:"imageId,omitempty"`
	Platform    string `json:"platform,omitempty"`
	PackageID   string `json:"packageId,omitempty"`
	ImageSHA256 string `json:"imageSha256,omitempty"`
}

type updateOperation struct {
	ID        string `json:"id"`
	Target    string `json:"target"`
	Status    string `json:"status"`
	CreatedAt string `json:"createdAt"`
}

type updateSlotsState struct {
	Version     int                   `json:"version"`
	ActiveSlot  string                `json:"activeSlot"`
	Slots       map[string]updateSlot `json:"slots"`
	Pending     *updateOperation      `json:"pending,omitempty"`
	LastMessage string                `json:"lastMessage,omitempty"`
	UpdatedAt   string                `json:"updatedAt,omitempty"`
}

type updatePackageManifest struct {
	Format       string `json:"format"`
	Version      int    `json:"version"`
	ReleaseTag   string `json:"releaseTag"`
	Platform     string `json:"platform"`
	ImageArchive string `json:"imageArchive"`
	ImageID      string `json:"imageId"`
}

type githubRelease struct {
	TagName     string `json:"tag_name"`
	Name        string `json:"name"`
	HTMLURL     string `json:"html_url"`
	Body        string `json:"body"`
	PublishedAt string `json:"published_at"`
	Draft       bool   `json:"draft"`
	Assets      []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

type updateAsset struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

func (a *App) updateRuntimeMode() string {
	if a.buildRuntimeMode == "release-image" {
		return "release-image"
	}
	return "source"
}

func (a *App) requireReleaseUpdateRuntime(w http.ResponseWriter) bool {
	if a.updateRuntimeMode() == "release-image" {
		return true
	}
	fail(w, http.StatusConflict, errors.New("当前由源代码启动；源码更新无需应用内升级。A/B 槽与宿主升级代理仅适用于 Release 镜像"))
	return false
}

func (a *App) checkUpdates(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	base := a.updateAPIBase
	if base == "" {
		base = "https://api.github.com/repos/skyelan1999/aide/releases?per_page=20"
	}
	client := a.updateHTTPClient
	if client == nil {
		client = &http.Client{Timeout: 8 * time.Second}
	}
	releases, err := fetchReleases(ctx, client, base)
	if err != nil {
		fail(w, http.StatusBadGateway, fmt.Errorf("检查更新失败：%w", err))
		return
	}
	var latest *githubRelease
	for i := range releases {
		if !releases[i].Draft && releaseTagPattern.MatchString(releases[i].TagName) {
			latest = &releases[i]
			break
		}
	}
	if latest == nil {
		fail(w, http.StatusBadGateway, errors.New("官方 Release 列表中没有可识别的版本"))
		return
	}
	if !isGitHubURL(latest.HTMLURL) {
		fail(w, http.StatusBadGateway, errors.New("Release 页面地址无效"))
		return
	}
	current := a.version
	_, currentKnown := parseReleaseVersion(current)
	latestVersion := strings.TrimPrefix(latest.TagName, "v")
	assets := make([]updateAsset, 0, 3)
	for _, asset := range latest.Assets {
		if (!isLauncherAsset(asset.Name, latest.TagName) && !isUpdateBundleAsset(asset.Name, latest.TagName)) || !isGitHubURL(asset.BrowserDownloadURL) {
			continue
		}
		assets = append(assets, updateAsset{Name: asset.Name, URL: asset.BrowserDownloadURL})
	}
	notes := latest.Body
	if len(notes) > 24000 {
		notes = notes[:24000]
	}
	jsonOut(w, http.StatusOK, map[string]any{
		"currentVersion":  current,
		"runtimeMode":     a.updateRuntimeMode(),
		"latestVersion":   latestVersion,
		"updateAvailable": !currentKnown || versionLess(current, latestVersion),
		"currentAhead":    currentKnown && versionLess(latestVersion, current),
		"releaseURL":      latest.HTMLURL,
		"releaseNotes":    notes,
		"publishedAt":     latest.PublishedAt,
		"assets":          assets,
	})
}

func fetchReleases(ctx context.Context, client *http.Client, endpoint string) ([]githubRelease, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" && u.Scheme != "http" || u.Host == "" {
		return nil, errors.New("更新源地址无效")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "aide-update-checker")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("更新服务返回 HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20+1))
	if err != nil {
		return nil, err
	}
	if len(body) > 1<<20 {
		return nil, errors.New("更新服务响应过大")
	}
	var releases []githubRelease
	if err := json.Unmarshal(body, &releases); err != nil {
		return nil, errors.New("更新服务响应格式无效")
	}
	return releases, nil
}

func isGitHubURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && strings.EqualFold(u.Hostname(), "github.com") && u.User == nil
}

func isLauncherAsset(name, tag string) bool {
	if !strings.HasPrefix(name, "aide-"+tag+"-") || !strings.HasSuffix(name, ".zip") {
		return false
	}
	for _, platform := range []string{"macos-arm64", "windows-arm64", "ubuntu-arm64"} {
		if name == "aide-"+tag+"-"+platform+".zip" {
			return true
		}
	}
	return false
}

func isUpdateBundleAsset(name, tag string) bool {
	return name == "aide-"+tag+"-update-linux-arm64.zip" || name == "aide-"+tag+"-update-linux-amd64.zip"
}

type parsedVersion struct {
	parts [5]int
}

func parseReleaseVersion(raw string) (parsedVersion, bool) {
	m := displayVersionPattern.FindStringSubmatch(strings.TrimSpace(raw))
	if len(m) != 3 {
		return parsedVersion{}, false
	}
	fields := strings.Split(m[1], ".")
	var out parsedVersion
	for i, field := range fields {
		v, err := strconv.Atoi(field)
		if err != nil {
			return parsedVersion{}, false
		}
		out.parts[i] = v
	}
	out.parts[4], _ = strconv.Atoi(m[2])
	return out, true
}

func versionLess(current, latest string) bool {
	c, okC := parseReleaseVersion(current)
	l, okL := parseReleaseVersion(latest)
	if !okC || !okL {
		return false
	}
	for i := range c.parts {
		if c.parts[i] != l.parts[i] {
			return c.parts[i] < l.parts[i]
		}
	}
	return false
}

func (a *App) updateStatePath() string {
	return filepath.Join(a.dataPath, "updates", "slots.json")
}

func (a *App) updatePackagesPath() string {
	return filepath.Join(a.dataPath, "updates", "packages")
}

func (a *App) loadUpdateStateLocked() (updateSlotsState, error) {
	path := a.updateStatePath()
	b, err := os.ReadFile(path)
	if err == nil {
		var state updateSlotsState
		if json.Unmarshal(b, &state) != nil || state.Version != 1 || (state.ActiveSlot != "A" && state.ActiveSlot != "B") || state.Slots == nil {
			return updateSlotsState{}, errors.New("软件升级槽状态损坏，请保留 /data/updates/slots.json 后修复")
		}
		return state, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return updateSlotsState{}, err
	}
	version := a.version
	state := updateSlotsState{Version: 1, ActiveSlot: "A", Slots: map[string]updateSlot{
		"A": {Version: version, Tag: versionToReleaseTag(version), ImageRef: "", Platform: runtimePlatform()},
		"B": {},
	}}
	return state, nil
}

func versionToReleaseTag(version string) string {
	version = strings.TrimSpace(version)
	if m := displayVersionPattern.FindStringSubmatch(version); len(m) == 3 {
		return "v" + m[1] + "-RC" + m[2]
	}
	return ""
}

func runtimePlatform() string {
	arch := runtime.GOARCH
	if arch == "arm64" || arch == "amd64" {
		return "linux/" + arch
	}
	return "linux/unknown"
}

func (a *App) saveUpdateStateLocked(state updateSlotsState) error {
	state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := os.MkdirAll(filepath.Dir(a.updateStatePath()), 0700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	tmp := a.updateStatePath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, a.updateStatePath())
}

func (a *App) updateSlots(w http.ResponseWriter, _ *http.Request) {
	a.updatesMu.Lock()
	defer a.updatesMu.Unlock()
	state, err := a.loadUpdateStateLocked()
	if err != nil {
		fail(w, 500, err)
		return
	}
	jsonOut(w, 200, map[string]any{
		"version": state.Version, "activeSlot": state.ActiveSlot, "slots": state.Slots,
		"pending": state.Pending, "lastMessage": state.LastMessage, "updatedAt": state.UpdatedAt,
		"runtimeMode": a.updateRuntimeMode(),
	})
}

func (a *App) uploadUpdatePackage(w http.ResponseWriter, r *http.Request) {
	if !a.requireReleaseUpdateRuntime(w) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, updatePackageMaxBytes+(1<<20))
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		fail(w, 413, errors.New("升级包超过 2 GiB 或 multipart 数据无效"))
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	if err := os.MkdirAll(a.updatePackagesPath(), 0700); err != nil {
		fail(w, 500, err)
		return
	}
	tmp, err := os.CreateTemp(a.updatePackagesPath(), ".upload-*.zip")
	if err != nil {
		fail(w, 500, err)
		return
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if files := r.MultipartForm.File["package"]; len(files) > 0 {
		if len(files) != 1 || !strings.EqualFold(filepath.Ext(files[0].Filename), ".zip") {
			_ = tmp.Close()
			fail(w, 400, errors.New("请选择一个 aide 应用内升级 ZIP 包，不能选择平台启动器 ZIP"))
			return
		}
		file, err := files[0].Open()
		if err != nil {
			_ = tmp.Close()
			fail(w, 400, errors.New("读取升级 ZIP 包失败"))
			return
		}
		n, copyErr := io.Copy(tmp, io.LimitReader(file, updatePackageMaxBytes+1))
		closeFileErr := file.Close()
		closeErr := tmp.Close()
		if copyErr != nil || closeFileErr != nil || closeErr != nil {
			fail(w, 500, errors.New("保存升级包失败"))
			return
		}
		if n > updatePackageMaxBytes {
			fail(w, 413, errors.New("升级包超过 2 GiB"))
			return
		}
	} else if files := r.MultipartForm.File["files"]; len(files) > 0 {
		var paths []string
		if err := json.Unmarshal([]byte(r.FormValue("paths")), &paths); err != nil || len(paths) != len(files) {
			_ = tmp.Close()
			fail(w, 400, errors.New("无法读取所选升级文件夹，请重新选择完整文件夹"))
			return
		}
		if err := tmp.Close(); err != nil {
			fail(w, 500, errors.New("准备升级文件夹失败"))
			return
		}
		if err := zipUpdateFolder(files, paths, tmpPath); err != nil {
			var maxErr *updatePackageSizeError
			if errors.As(err, &maxErr) {
				fail(w, 413, errors.New("升级文件夹超过 2 GiB"))
			} else {
				fail(w, 400, err)
			}
			return
		}
	} else {
		_ = tmp.Close()
		fail(w, 400, errors.New("请选择 aide 应用内升级 ZIP 包，或选择解压后的完整升级文件夹"))
		return
	}
	manifest, imageHash, err := validateUpdatePackage(tmpPath)
	if err != nil {
		fail(w, 400, err)
		return
	}
	if manifest.Platform != runtimePlatform() {
		fail(w, 400, fmt.Errorf("升级包平台 %s 与当前运行镜像 %s 不匹配", manifest.Platform, runtimePlatform()))
		return
	}
	currentVersion := strings.TrimPrefix(manifest.ReleaseTag, "v")
	if !releaseTagPattern.MatchString(manifest.ReleaseTag) {
		fail(w, 400, errors.New("升级包版本标记无效"))
		return
	}
	var idBytes [16]byte
	if _, err := rand.Read(idBytes[:]); err != nil {
		fail(w, 500, errors.New("创建升级包编号失败"))
		return
	}
	packageID := hex.EncodeToString(idBytes[:])
	storedPath := filepath.Join(a.updatePackagesPath(), packageID+".zip")
	a.updatesMu.Lock()
	defer a.updatesMu.Unlock()
	state, err := a.loadUpdateStateLocked()
	if err != nil {
		fail(w, 500, err)
		return
	}
	if state.Pending != nil {
		fail(w, 409, errors.New("当前有升级切换正在进行，请等待结束后再添加软件包"))
		return
	}
	active := state.Slots[state.ActiveSlot]
	if active.Version != "" && !versionLess(active.Version, currentVersion) {
		fail(w, 400, errors.New("升级包版本必须高于当前活动槽版本"))
		return
	}
	if err := os.Rename(tmpPath, storedPath); err != nil {
		fail(w, 500, errors.New("保存升级包失败"))
		return
	}
	target := inactiveSlot(state.ActiveSlot)
	state.Slots[target] = updateSlot{
		Version: currentVersion, Tag: manifest.ReleaseTag,
		ImageRef: slotImageRef(target), ImageID: manifest.ImageID,
		Platform: manifest.Platform, PackageID: packageID, ImageSHA256: imageHash,
	}
	if err := a.saveUpdateStateLocked(state); err != nil {
		_ = os.Remove(storedPath)
		fail(w, 500, err)
		return
	}
	jsonOut(w, 201, map[string]any{"packageId": packageID, "targetSlot": target, "slot": state.Slots[target]})
}

type updatePackageSizeError struct{}

func (*updatePackageSizeError) Error() string { return "update package exceeds size limit" }

// zipUpdateFolder rebuilds the exact three-entry update archive from a browser
// directory upload. The browser locates and submits only the three package
// files from one shared parent directory; their paths are validated and never
// used as ZIP paths.
func zipUpdateFolder(files []*multipart.FileHeader, paths []string, destination string) error {
	if len(paths) != len(files) || len(files) < 3 || len(files) > 64 {
		return errors.New("升级文件夹必须只包含 manifest.json、SHA256SUMS 和一个 Docker 镜像归档")
	}
	w, err := os.Create(destination)
	if err != nil {
		return errors.New("创建升级文件夹临时包失败")
	}
	zw := zip.NewWriter(w)
	var total int64
	parent := ""
	parentSet := false
	seen := map[string]bool{}
	for i, fh := range files {
		rel := strings.ReplaceAll(paths[i], `\`, "/")
		if rel == "" || strings.HasPrefix(rel, "/") {
			err = errors.New("升级文件夹包含无效文件路径")
			break
		}
		parts := strings.Split(rel, "/")
		for _, part := range parts {
			if part == "" || part == "." || part == ".." {
				err = errors.New("升级文件夹包含无效文件路径")
				break
			}
		}
		if err != nil {
			break
		}
		name := parts[len(parts)-1]
		if name == ".DS_Store" || strings.HasPrefix(name, "._") || slices.Contains(parts, "__MACOSX") {
			continue
		}
		currentParent := strings.Join(parts[:len(parts)-1], "/")
		if !parentSet {
			parent = currentParent
			parentSet = true
		} else if currentParent != parent {
			err = errors.New("请选择同一个完整升级文件夹中的文件")
			break
		}
		if name != "manifest.json" && name != "SHA256SUMS" && !strings.HasSuffix(name, "-image.tar.gz") {
			err = errors.New("升级文件夹必须只包含 manifest.json、SHA256SUMS 和一个 Docker 镜像归档")
			break
		}
		if seen[name] {
			err = errors.New("升级文件夹包含重复文件")
			break
		}
		seen[name] = true
		if total+fh.Size > updatePackageMaxBytes {
			err = &updatePackageSizeError{}
			break
		}
		total += fh.Size
		entry, createErr := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store})
		if createErr != nil {
			err = errors.New("写入升级文件夹失败")
			break
		}
		reader, openErr := fh.Open()
		if openErr != nil {
			err = errors.New("读取升级文件夹失败")
			break
		}
		written, copyErr := io.Copy(entry, io.LimitReader(reader, updatePackageMaxBytes+1))
		closeErr := reader.Close()
		if copyErr != nil || closeErr != nil || written != fh.Size {
			err = errors.New("读取升级文件夹中的文件失败")
			break
		}
	}
	if err == nil && (!seen["manifest.json"] || !seen["SHA256SUMS"] || len(seen) != 3) {
		err = errors.New("升级文件夹必须包含 manifest.json、SHA256SUMS 和一个 Docker 镜像归档")
	}
	zipErr := zw.Close()
	fileErr := w.Close()
	if err != nil {
		return err
	}
	if zipErr != nil || fileErr != nil {
		return errors.New("完成升级文件夹打包失败")
	}
	return nil
}

func validateUpdatePackage(path string) (updatePackageManifest, string, error) {
	var manifest updatePackageManifest
	zr, err := zip.OpenReader(path)
	if err != nil {
		return manifest, "", errors.New("升级包不是有效 ZIP")
	}
	defer zr.Close()
	if len(zr.File) != 3 {
		return manifest, "", errors.New("升级包必须恰好包含 manifest.json、SHA256SUMS 和一个镜像归档")
	}
	entries := map[string]*zip.File{}
	var total uint64
	for _, f := range zr.File {
		if f.Name != "manifest.json" && f.Name != "SHA256SUMS" && !strings.HasSuffix(f.Name, "-image.tar.gz") {
			return manifest, "", errors.New("升级包包含不支持的文件")
		}
		if _, exists := entries[f.Name]; exists || f.UncompressedSize64 > uint64(updatePackageMaxBytes) {
			return manifest, "", errors.New("升级包文件重复或过大")
		}
		entries[f.Name] = f
		total += f.UncompressedSize64
	}
	if total > uint64(updatePackageMaxBytes) {
		return manifest, "", errors.New("升级包解压后超过 2 GiB")
	}
	readEntry := func(name string, limit int64) ([]byte, error) {
		entry := entries[name]
		if entry == nil || entry.UncompressedSize64 > uint64(limit) {
			return nil, errors.New("升级包缺少必需文件或文件过大：" + name)
		}
		rc, err := entry.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		return io.ReadAll(io.LimitReader(rc, limit+1))
	}
	manifestBytes, err := readEntry("manifest.json", 32<<10)
	if err != nil || json.Unmarshal(manifestBytes, &manifest) != nil || manifest.Format != "aide-update-package" || manifest.Version != 1 {
		return manifest, "", errors.New("升级包 manifest 无效")
	}
	if !releaseTagPattern.MatchString(manifest.ReleaseTag) || !updateImageIDPattern.MatchString(manifest.ImageID) {
		return manifest, "", errors.New("升级包版本或 Docker image ID 无效")
	}
	arch := strings.TrimPrefix(manifest.Platform, "linux/")
	if arch != "arm64" && arch != "amd64" || manifest.Platform != "linux/"+arch {
		return manifest, "", errors.New("升级包平台不受支持")
	}
	expectedImageName := "aide-" + manifest.ReleaseTag + "-linux-" + arch + "-image.tar.gz"
	if manifest.ImageArchive != expectedImageName || entries[expectedImageName] == nil {
		return manifest, "", errors.New("升级包镜像文件名与清单不匹配")
	}
	sums, err := readEntry("SHA256SUMS", 1<<20)
	if err != nil {
		return manifest, "", err
	}
	var expectedHash string
	for _, line := range strings.Split(string(sums), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == expectedImageName {
			expectedHash = fields[0]
			break
		}
	}
	if !regexp.MustCompile(`^[a-fA-F0-9]{64}$`).MatchString(expectedHash) {
		return manifest, "", errors.New("SHA256SUMS 中缺少有效镜像校验值")
	}
	img, err := entries[expectedImageName].Open()
	if err != nil {
		return manifest, "", err
	}
	h := sha256.New()
	_, err = io.Copy(h, img)
	closeErr := img.Close()
	if err != nil || closeErr != nil {
		return manifest, "", errors.New("无法读取升级镜像")
	}
	actualHash := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(actualHash, expectedHash) {
		return manifest, "", errors.New("升级镜像 SHA256 不匹配")
	}
	return manifest, actualHash, nil
}

func inactiveSlot(active string) string {
	if active == "A" {
		return "B"
	}
	return "A"
}

func slotImageRef(slot string) string { return "aide:slot-" + strings.ToLower(slot) }

func (a *App) switchUpdateSlot(w http.ResponseWriter, r *http.Request) {
	if !a.requireReleaseUpdateRuntime(w) {
		return
	}
	var req struct {
		Target string `json:"target"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&req); err != nil {
		fail(w, 400, errors.New("切换请求无效"))
		return
	}
	if req.Target != "A" && req.Target != "B" {
		fail(w, 400, errors.New("目标槽必须是 A 或 B"))
		return
	}
	a.updatesMu.Lock()
	defer a.updatesMu.Unlock()
	state, err := a.loadUpdateStateLocked()
	if err != nil {
		fail(w, 500, err)
		return
	}
	if state.Pending != nil {
		fail(w, 409, errors.New("已有槽切换正在处理"))
		return
	}
	if req.Target == state.ActiveSlot {
		fail(w, 400, errors.New("该槽已经处于活动状态"))
		return
	}
	slot := state.Slots[req.Target]
	if slot.ImageID == "" || !updateImageIDPattern.MatchString(slot.ImageID) || slotImageRef(req.Target) != slot.ImageRef {
		fail(w, 400, errors.New("目标槽还没有可用的软件包"))
		return
	}
	opIDBytes := make([]byte, 16)
	if _, err := rand.Read(opIDBytes); err != nil {
		fail(w, 500, errors.New("创建切换操作编号失败"))
		return
	}
	state.Pending = &updateOperation{ID: hex.EncodeToString(opIDBytes), Target: req.Target, Status: "requested", CreatedAt: time.Now().UTC().Format(time.RFC3339)}
	state.LastMessage = "正在等待启动器切换到槽 " + req.Target
	if err := a.saveUpdateStateLocked(state); err != nil {
		fail(w, 500, err)
		return
	}
	jsonOut(w, 202, map[string]any{"status": "queued", "targetSlot": req.Target, "operationId": state.Pending.ID})
}

func (a *App) updateAgentCommand(w http.ResponseWriter, _ *http.Request) {
	if !a.requireReleaseUpdateRuntime(w) {
		return
	}
	a.updatesMu.Lock()
	defer a.updatesMu.Unlock()
	state, err := a.loadUpdateStateLocked()
	if err != nil {
		fail(w, 500, err)
		return
	}
	if state.Pending == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	slot := state.Slots[state.Pending.Target]
	if slot.PackageID != "" && !updatePackageIDPattern.MatchString(slot.PackageID) || !updateImageIDPattern.MatchString(slot.ImageID) || slot.Tag == "" || !releaseTagPattern.MatchString(slot.Tag) {
		fail(w, 500, errors.New("目标槽元数据无效"))
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", state.Pending.ID, state.Pending.Target, slot.PackageID, slot.Tag, slot.ImageID, slot.Platform, slot.ImageSHA256, slotImageRef(state.Pending.Target), slot.Version)
}

func (a *App) downloadUpdatePackage(w http.ResponseWriter, r *http.Request) {
	if !a.requireReleaseUpdateRuntime(w) {
		return
	}
	id := r.PathValue("id")
	if !updatePackageIDPattern.MatchString(id) {
		fail(w, 404, errors.New("升级包不存在"))
		return
	}
	path := filepath.Join(a.updatePackagesPath(), id+".zip")
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		fail(w, 404, errors.New("升级包不存在"))
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	http.ServeFile(w, r, path)
}

func (a *App) updateAgentResult(w http.ResponseWriter, r *http.Request) {
	if !a.requireReleaseUpdateRuntime(w) {
		return
	}
	var req struct {
		OperationID string `json:"operationId"`
		Success     bool   `json:"success"`
		Message     string `json:"message"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&req); err != nil || len(req.Message) > 1000 {
		fail(w, 400, errors.New("升级结果无效"))
		return
	}
	a.updatesMu.Lock()
	defer a.updatesMu.Unlock()
	state, err := a.loadUpdateStateLocked()
	if err != nil {
		fail(w, 500, err)
		return
	}
	if state.Pending == nil || state.Pending.ID != req.OperationID {
		fail(w, 409, errors.New("升级操作已变化或不存在"))
		return
	}
	if req.Success {
		state.ActiveSlot = state.Pending.Target
		state.LastMessage = "已切换到槽 " + state.ActiveSlot
	} else {
		state.LastMessage = "槽切换失败并已回滚：" + req.Message
	}
	state.Pending = nil
	if err := a.saveUpdateStateLocked(state); err != nil {
		fail(w, 500, err)
		return
	}
	jsonOut(w, 200, state)
}

func (a *App) updateAgentSync(w http.ResponseWriter, r *http.Request) {
	if !a.requireReleaseUpdateRuntime(w) {
		return
	}
	var req updateSlotsState
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10)).Decode(&req); err != nil {
		fail(w, 400, errors.New("启动器槽状态无效"))
		return
	}
	if req.ActiveSlot != "A" && req.ActiveSlot != "B" || req.Slots == nil {
		fail(w, 400, errors.New("启动器槽状态缺少活动槽"))
		return
	}
	for _, key := range []string{"A", "B"} {
		slot := req.Slots[key]
		if slot.ImageID != "" && (!updateImageIDPattern.MatchString(slot.ImageID) || slot.ImageRef != slotImageRef(key) || (slot.Platform != "linux/arm64" && slot.Platform != "linux/amd64") || (slot.Tag != "" && !releaseTagPattern.MatchString(slot.Tag)) || (slot.PackageID != "" && !updatePackageIDPattern.MatchString(slot.PackageID))) {
			fail(w, 400, errors.New("启动器槽镜像元数据无效"))
			return
		}
	}
	if req.Slots[req.ActiveSlot].ImageID == "" {
		fail(w, 400, errors.New("启动器活动槽缺少镜像 ID"))
		return
	}
	a.updatesMu.Lock()
	defer a.updatesMu.Unlock()
	state, err := a.loadUpdateStateLocked()
	if err != nil {
		fail(w, 500, err)
		return
	}
	if state.Pending == nil {
		state.ActiveSlot = req.ActiveSlot
		state.LastMessage = "宿主启动器已同步当前活动槽"
	}
	// A launcher reports only its currently running image. Preserve the other
	// slot's package metadata, which was validated and stored by the upload API.
	active := req.Slots[req.ActiveSlot]
	if active.ImageID != "" {
		state.Slots[req.ActiveSlot] = active
	}
	if err := a.saveUpdateStateLocked(state); err != nil {
		fail(w, 500, err)
		return
	}
	jsonOut(w, 200, state)
}
