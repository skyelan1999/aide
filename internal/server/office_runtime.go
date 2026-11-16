package server

// The editor is a bundled local child, not a separately configured service.
// Only this process may access its listener and the private WOPI callback.
import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const officeEnginePrefix = "/office-engine"
const officeEngineCookie = "aide_office_engine"

type officeRuntime struct {
	mu        sync.Mutex
	config    *collaboraConfiguration
	command   *exec.Cmd
	done      chan struct{}
	callback  *http.Server
	directory string
}

func officeEngineExecutable() string {
	paths := []string{"/opt/aide/office-engine/run"}
	if executable, err := os.Executable(); err == nil {
		paths = append(paths, filepath.Join(filepath.Dir(executable), "office-engine", "run"))
	}
	for _, p := range paths {
		if info, err := os.Stat(p); err == nil && info.Mode().IsRegular() && info.Mode()&0111 != 0 {
			return p
		}
	}
	return ""
}

// Keep Aide/provider credentials and proxy overrides out of the editor child.
func officeEngineEnvironment(directory string) []string {
	return []string{"PATH=/usr/local/bin:/usr/bin:/bin", "HOME=" + directory, "TMPDIR=" + directory, "LANG=C.UTF-8", "LC_ALL=C.UTF-8"}
}

func (a *App) officeViewerEnabled() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, p := range a.pluginRegistry.Plugins {
		if p.ID == "office" {
			return p.Enabled
		}
	}
	return true // Viewing/editing is built in; the plugin supplies AI tools.
}

func (a *App) officeCallbackHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/wopi/files/{id}", a.collaboraWOPI)
	mux.HandleFunc("/wopi/files/{id}/contents", a.collaboraWOPI)
	return mux
}

func (a *App) stopOfficeEngineLocked() {
	o := &a.officeRuntime
	if o.command != nil {
		_ = killPluginProcess(o.command)
		if o.done != nil {
			<-o.done
		}
	}
	if o.callback != nil {
		_ = o.callback.Close()
	}
	if o.directory != "" {
		_ = os.RemoveAll(o.directory)
	}
	o.config, o.command, o.done, o.callback, o.directory = nil, nil, nil, nil, ""
}

func (a *App) stopOfficeEngine() {
	a.officeRuntime.mu.Lock()
	defer a.officeRuntime.mu.Unlock()
	a.stopOfficeEngineLocked()
}

func (a *App) ensureOfficeEngine(ctx context.Context) error {
	if !a.officeViewerEnabled() {
		return errors.New("Office 插件已停用")
	}
	o := &a.officeRuntime
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.config != nil {
		if o.done == nil {
			return nil
		} // In-process fixture; never configured by users.
		select {
		case <-o.done:
			a.stopOfficeEngineLocked()
		default:
			return nil
		}
	}
	executable := officeEngineExecutable()
	if executable == "" {
		return errors.New("此 Aide 安装包缺少内置 Office 引擎，请更新完整安装包")
	}
	if a.bgCtx.Err() != nil {
		return errors.New("Aide 正在停止")
	}
	directory, err := os.MkdirTemp("", "aide-office-")
	if err != nil {
		return err
	}
	o.directory = directory
	ok := false
	defer func() {
		if !ok {
			a.stopOfficeEngineLocked()
		}
	}()
	callbackListener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return err
	}
	o.callback = &http.Server{Handler: a.officeCallbackHandler(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	callbackServer := o.callback
	go func() { _ = callbackServer.Serve(callbackListener) }()
	// coolwsd cannot inherit a listener. Reserve an ephemeral port immediately
	// before spawn and require successful process readiness before using it.
	reservation, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return err
	}
	port := reservation.Addr().(*net.TCPAddr).Port
	_ = reservation.Close()
	internal := "http://127.0.0.1:" + strconv.Itoa(port)
	callback := "http://" + callbackListener.Addr().String()
	args := []string{"--port=" + strconv.Itoa(port),
		"--o:net.proto=IPv4", "--o:net.listen=loopback", "--o:net.proxy_prefix=false",
		"--o:net.service_root=" + officeEnginePrefix, "--o:ssl.enable=false", "--o:ssl.termination=true",
		"--o:storage.wopi.alias_groups[@mode]=groups",
		"--o:storage.wopi.alias_groups.group[0].host=" + callback,
		"--o:storage.wopi.alias_groups.group[0].host[@allow]=true",
		"--o:net.lok_allow.host=127\\.0\\.0\\.1", "--o:remote_font_config.url=",
		"--o:welcome.enable=false", "--o:fetch_update_check=0", "--o:allow_update_popup=false",
		"--o:logging.level=error", "--o:logging.level_startup=error", "--o:logging.file[@enable]=false",
		"--o:admin_console.enable=false", "--o:ai.enabled=false", "--o:ai.allow_user_settings=false",
		"--o:languagetool.enabled=false", "--o:remote_config.remote_url=", "--o:remote_links.enable=false",
		"--o:net.post_allow[@allow]=false"}
	for i := 1; i < 16; i++ {
		args = append(args, "--o:net.lok_allow.host["+strconv.Itoa(i)+"]=127\\.0\\.0\\.1")
	}
	cmd := exec.CommandContext(a.bgCtx, executable, args...)
	cmd.Dir = directory
	cmd.Env = officeEngineEnvironment(directory)
	configurePluginProcess(cmd, true)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动内置 Office 引擎: %w", err)
	}
	o.command = cmd
	o.done = make(chan struct{})
	done := o.done
	go func() { _ = cmd.Wait(); close(done) }()
	readyCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	client := &http.Client{Timeout: time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	for {
		req, _ := http.NewRequestWithContext(readyCtx, "GET", internal+officeEnginePrefix+"/hosting/discovery", nil)
		resp, e := client.Do(req)
		if e == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == 200 {
				break
			}
		}
		select {
		case <-readyCtx.Done():
			return errors.New("内置 Office 引擎尚未就绪，请重试")
		case <-a.bgCtx.Done():
			return errors.New("Aide 正在停止")
		case <-done:
			return errors.New("内置 Office 引擎启动失败，请更新完整安装包")
		case <-time.After(150 * time.Millisecond):
		}
	}
	public, _ := url.Parse(officeEnginePrefix)
	inside, _ := url.Parse(internal + officeEnginePrefix)
	cb, _ := url.Parse(callback)
	o.config = &collaboraConfiguration{Public: public, Internal: inside, Callback: cb, Fingerprint: collaboraRandom()}
	ok = true
	return nil
}

func (a *App) serveOfficeEngine(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, officeEnginePrefix)
	// Check nested escaping before forwarding the original encoded WOPI path.
	checked := rest
	for i := 0; i < 3; i++ {
		next, e := url.PathUnescape(checked)
		if e != nil {
			http.NotFound(w, r)
			return
		}
		if next == checked {
			break
		}
		checked = next
	}
	denied := strings.Contains(strings.ToLower(checked), "/admin") || strings.Contains(checked, "..")
	for _, endpoint := range []string{"/cool/convert-to", "/cool/extract-document-structure", "/cool/transform-document-structure", "/cool/render-search-result"} {
		denied = denied || strings.HasPrefix(strings.ToLower(checked), endpoint)
	}
	if (r.Method != "GET" && r.Method != "HEAD" && r.Method != "POST") ||
		!(strings.HasPrefix(rest, "/browser/") || strings.HasPrefix(rest, "/cool/")) ||
		denied {
		http.NotFound(w, r)
		return
	}
	cookie, err := r.Cookie(officeEngineCookie)
	if err != nil {
		fail(w, 401, errors.New("请从 Aide 打开 Office 文档"))
		return
	}
	id, secret, found := strings.Cut(cookie.Value, ".")
	if !found {
		fail(w, 401, errors.New("Office 会话无效"))
		return
	}
	a.collabora.mu.Lock()
	s := a.collabora.sessions[id]
	valid := s != nil && s.ProxyTokenHash != "" && s.ProxyTokenHash == hash([]byte(secret)) && a.collaboraSessionCurrent(s)
	a.collabora.mu.Unlock()
	if !valid {
		fail(w, 401, errors.New("Office 会话已失效，请重新打开"))
		return
	}
	cfg, err := a.collaboraConfig()
	if err != nil {
		fail(w, 503, err)
		return
	}
	target := *cfg.Internal
	target.Path, target.RawPath = "", ""
	proxy := httputil.NewSingleHostReverseProxy(&target)
	baseDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		baseDirector(req)
		req.Host = r.Host
		req.Header.Del("Authorization")
		req.Header.Del("Cookie")
		req.Header.Set("X-Forwarded-Proto", "https")
	}
	proxy.Transport = &http.Transport{Proxy: nil, ResponseHeaderTimeout: 30 * time.Second}
	defer proxy.Transport.(*http.Transport).CloseIdleConnections()
	proxy.ModifyResponse = func(resp *http.Response) error {
		resp.Header.Del("Set-Cookie")
		return nil
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, _ error) {
		fail(w, 502, errors.New("本地 Office 引擎连接中断，请重新打开文档"))
	}
	// The bundled editor supplies its own CSP for its scripts, workers and fonts.
	w.Header().Del("Content-Security-Policy")
	proxy.ServeHTTP(w, r)
}
