package server

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 本文件全部在 t.TempDir() 隔离容器内运行（testApp 已用临时数据卷），
// 绝不触碰用户真实 /data。每个用例灌入假会话/记忆/凭据，再按 scope 重置并断言。

// writeUserFile 在 /workspace（workPath）下写一个"用户代码"文件，用于断言永不删除。
func writeUserFile(t *testing.T, a *App, name, body string) string {
	t.Helper()
	p := filepath.Join(a.workPath, name)
	if err := os.WriteFile(p, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	return p
}

func userFileExists(p string) bool {
	b, err := os.ReadFile(p)
	return err == nil && strings.Contains(string(b), "USERKEEP")
}

// setPasswordDirect 直接写入账户密码哈希（绕过改密码流程的重加密，仅用于让密码门生效）。
func setPasswordDirect(t *testing.T, a *App, pw string) {
	t.Helper()
	a.mu.Lock()
	a.settings.UserPasswordHash = mustHashPassword(pw)
	if a.settings.UserPasswordHash == "" {
		t.Fatal("mustHashPassword 返回空")
	}
	if err := atomicJSON(SettingsPath(a.dataPath), a.settings); err != nil {
		t.Fatal(err)
	}
	a.mu.Unlock()
}

// postReset 发起一次出厂重置请求（使用当前 a.token）。
func postReset(a *App, body map[string]any) *httptest.ResponseRecorder {
	return request(a, "POST", "/api/factory-reset", body)
}

func resetBody(scope map[string]any, confirm, password string) map[string]any {
	return map[string]any{"scope": scope, "confirmText": confirm, "password": password}
}

// TestResetSettingsOnly 仅重置设置：设置回默认，而会话/记忆/凭据/用户文件全部保留。
func TestFactoryReset_SettingsOnly(t *testing.T) {
	a := testApp(t)
	// 改一个设置项（工具轮次 60→30），重置后应回到 60。
	a.mu.Lock()
	a.settings.ToolMaxRounds = 30
	a.settings.APIKey = "keep-api-key" // 凭据类应保留
	_ = atomicJSON(SettingsPath(a.dataPath), a.settings)
	a.mu.Unlock()

	// 造一个会话、一个核心记忆文件、一个用户文件。
	w := request(a, "POST", "/api/sessions", map[string]string{"title": "keep-me"})
	var s Session
	_ = json.Unmarshal(w.Body.Bytes(), &s)
	if err := os.MkdirAll(MemoryCoreDir(a.dataPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(MemoryCoreDir(a.dataPath), "core-1.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	uf := writeUserFile(t, a, "keepme.go", "package main // USERKEEP")

	requireStatus(t, postReset(a, resetBody(map[string]any{"settings": true}, "重置", "")), 200)

	a.mu.Lock()
	if a.settings.ToolMaxRounds != 60 {
		t.Fatalf("设置未回默认: ToolMaxRounds=%d", a.settings.ToolMaxRounds)
	}
	if a.settings.APIKey != "keep-api-key" {
		t.Fatal("设置-only 不应清除 API Key")
	}
	a.mu.Unlock()

	// 会话保留、记忆保留、用户文件保留。
	if _, ok := a.sessions[s.ID]; !ok {
		t.Fatal("设置-only 不应删除会话")
	}
	if _, err := os.Stat(filepath.Join(MemoryCoreDir(a.dataPath), "core-1.json")); err != nil {
		t.Fatal("设置-only 不应删除核心记忆")
	}
	if !userFileExists(uf) {
		t.Fatal("用户文件被误删")
	}
}

// TestResetSessionsAndMemory 清空会话与记忆，设置/凭据/用户文件按选项保留。
func TestFactoryReset_SessionsAndMemory(t *testing.T) {
	a := testApp(t)
	// 造两个普通会话 + 核心记忆 + 小秘历史文件。
	request(a, "POST", "/api/sessions", map[string]string{"title": "s1"})
	request(a, "POST", "/api/sessions", map[string]string{"title": "s2"})
	if err := os.MkdirAll(MemoryCoreDir(a.dataPath), 0700); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(MemoryCoreDir(a.dataPath), "core-x.json"), []byte("{}"), 0600)
	_ = os.WriteFile(VoiceHistoryPath(a.dataPath), []byte("ENVELOPE"), 0600)
	uf := writeUserFile(t, a, "app.py", "# USERKEEP")
	a.mu.Lock()
	a.settings.APIKey = "keep-key"
	_ = atomicJSON(SettingsPath(a.dataPath), a.settings)
	a.mu.Unlock()

	requireStatus(t, postReset(a, resetBody(map[string]any{"sessionsAndMemory": true}, "重置", "")), 200)

	// 会话文件应全部清空，仅重建恰好一个小秘系统会话（小秘会话文件同样落在 active 桶，按内容 Kind 判定）。
	totalN := 0
	var onlyKind string
	for _, dir := range sessionBucketDirs(a.dataPath) {
		matches, _ := filepath.Glob(filepath.Join(dir, "session-*.json"))
		totalN += len(matches)
		for _, m := range matches {
			if b, e := os.ReadFile(m); e == nil {
				var ss Session
				if json.Unmarshal(b, &ss) == nil {
					onlyKind = ss.Kind
				}
			}
		}
	}
	if totalN != 1 {
		t.Fatalf("重置后应恰好剩 1 个会话（小秘系统会话）, got %d", totalN)
	}
	if onlyKind != assistantSessionKind {
		t.Fatalf("重建会话应为 kind=assistant, got %q", onlyKind)
	}
	if _, err := os.Stat(filepath.Join(MemoryCoreDir(a.dataPath), "core-x.json")); !os.IsNotExist(err) {
		t.Fatal("核心记忆未清空")
	}
	if _, err := os.Stat(VoiceHistoryPath(a.dataPath)); !os.IsNotExist(err) {
		t.Fatal("小秘历史未清空")
	}
	a.mu.Lock()
	if a.settings.APIKey != "keep-key" {
		t.Fatal("未勾凭据时不应清 API Key")
	}
	a.mu.Unlock()
	if !userFileExists(uf) {
		t.Fatal("用户文件被误删")
	}
}

// TestResetCredentials 清除凭据、需重设密码、access-token 轮换；错误密码被拒。
func TestFactoryReset_Credentials(t *testing.T) {
	a := testApp(t)
	setPasswordDirect(t, a, "pw123")
	a.mu.Lock()
	a.settings.APIKey = "will-wipe"
	a.settings.DebugTokenHash = "deadbeef"
	_ = atomicJSON(SettingsPath(a.dataPath), a.settings)
	oldToken := a.token
	a.mu.Unlock()

	// 错误密码 → 401。
	requireStatus(t, postReset(a, resetBody(map[string]any{"credentialsAndKeys": true}, "重置", "wrong")), 401)

	// 正确密码 → 200。
	w := postReset(a, resetBody(map[string]any{"credentialsAndKeys": true}, "重置", "pw123"))
	requireStatus(t, w, 200)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if out["needRelogin"] != true {
		t.Fatal("凭据重置应要求重新登录")
	}

	a.mu.Lock()
	if a.settings.UserPasswordHash != "" {
		t.Fatal("密码哈希未清除")
	}
	if a.settings.APIKey != "" {
		t.Fatal("API Key 未清除")
	}
	if a.settings.DebugTokenHash != "" {
		t.Fatal("调试令牌未清除")
	}
	if a.token == oldToken {
		t.Fatal("access-token 未轮换")
	}
	a.mu.Unlock()
	if _, err := os.Stat(KdfSaltPath(a.dataPath)); !os.IsNotExist(err) {
		t.Fatal("KDF salt 未删除")
	}
	if _, err := os.Stat(WebAuthnCredsPath(a.dataPath)); !os.IsNotExist(err) {
		t.Fatal("WebAuthn 凭证未删除")
	}
}

// TestResetAll 全量重置：/data 回到初始、生成新 token。
func TestFactoryReset_All(t *testing.T) {
	a := testApp(t)
	setPasswordDirect(t, a, "pw123")
	request(a, "POST", "/api/sessions", map[string]string{"title": "gone"})
	a.mu.Lock()
	oldToken := a.token
	a.mu.Unlock()

	scope := map[string]any{"settings": true, "sessionsAndMemory": true, "credentialsAndKeys": true, "workspaceConfig": true}
	w := postReset(a, resetBody(scope, "重置", "pw123"))
	requireStatus(t, w, 200)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if out["backupPath"] == "" {
		t.Fatal("应返回备份路径")
	}
	a.mu.Lock()
	if a.token == oldToken || a.settings.UserPasswordHash != "" {
		t.Fatal("全量重置未轮换 token / 未清密码")
	}
	a.mu.Unlock()
	// healthz 仍健康。
	requireStatus(t, request(a, "GET", "/healthz", nil), 200)
}

// TestBackupGenerated 重置前自动生成备份，且信封可解析、可作为恢复点。
func TestFactoryReset_BackupGenerated(t *testing.T) {
	a := testApp(t)
	a.mu.Lock()
	a.settings.APIKey = "back-me-up"
	_ = atomicJSON(SettingsPath(a.dataPath), a.settings)
	a.mu.Unlock()

	w := postReset(a, resetBody(map[string]any{"settings": true}, "重置", ""))
	requireStatus(t, w, 200)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	backupPath, _ := out["backupPath"].(string)
	if backupPath == "" {
		t.Fatal("无备份路径")
	}
	b, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("备份文件不存在: %v", err)
	}
	var bk configBackup
	if err := json.Unmarshal(b, &bk); err != nil {
		t.Fatalf("备份信封不可解析: %v", err)
	}
	if bk.Format != configBackupFormat || !bk.IncludeSecrets {
		t.Fatalf("备份格式/含密钥标记异常: %+v", bk)
	}
	// 备份里应保留被重置前的 API Key（后悔药）。
	var snap Settings
	_ = json.Unmarshal(bk.Settings, &snap)
	if snap.APIKey != "back-me-up" {
		t.Fatal("备份未保留重置前的 API Key")
	}
}

// TestUserFilesPreserved 每种重置下 /workspace 用户文件均未被删除。
func TestFactoryReset_UserFilesPreserved(t *testing.T) {
	for _, scope := range []map[string]any{
		{"settings": true},
		{"sessionsAndMemory": true},
		{"workspaceConfig": true},
	} {
		a := testApp(t)
		uf := writeUserFile(t, a, "usercode.go", "package main // USERKEEP")
		requireStatus(t, postReset(a, resetBody(scope, "重置", "")), 200)
		if !userFileExists(uf) {
			t.Fatalf("scope=%v 删除了用户文件", scope)
		}
	}
}

// TestUnauthorizedRejected 未携带访问令牌调用被拒（401）。
func TestFactoryReset_UnauthorizedRejected(t *testing.T) {
	a := testApp(t)
	r := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/factory-reset", strings.NewReader(`{"scope":{"settings":true},"confirmText":"重置"}`))
	a.Handler().ServeHTTP(r, req)
	if r.Code != 401 {
		t.Fatalf("未授权应 401, got %d", r.Code)
	}
	// preview 同样受保护。
	r2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", "/api/factory-reset/preview", nil)
	a.Handler().ServeHTTP(r2, req2)
	if r2.Code != 401 {
		t.Fatalf("未授权 preview 应 401, got %d", r2.Code)
	}
}

// TestAuditWritten 每次重置都有审计记录。
func TestFactoryReset_AuditWritten(t *testing.T) {
	a := testApp(t)
	requireStatus(t, postReset(a, resetBody(map[string]any{"settings": true}, "重置", "")), 200)
	b, err := os.ReadFile(factoryResetAuditPath(a.dataPath))
	if err != nil {
		t.Fatalf("审计文件不存在: %v", err)
	}
	if !strings.Contains(string(b), `"settings":true`) || !strings.Contains(string(b), `"result":"ok"`) {
		t.Fatalf("审计内容缺失: %s", b)
	}
}

// TestPasswordRequired 涉及小秘历史/凭据时，已设密码却未提供/错误密码 → 拒绝。
func TestFactoryReset_PasswordRequired(t *testing.T) {
	a := testApp(t)
	setPasswordDirect(t, a, "pw123")
	// 已设密码，重置会话与记忆却不给密码 → 401。
	requireStatus(t, postReset(a, resetBody(map[string]any{"sessionsAndMemory": true}, "重置", "")), 401)
	// 只重置设置（不碰凭据/小秘历史）→ 不需要密码，200。
	requireStatus(t, postReset(a, resetBody(map[string]any{"settings": true}, "重置", "")), 200)
}

// TestPreviewReportsImpact preview 返回各范围影响计数，供弹窗展示。
func TestFactoryReset_Preview(t *testing.T) {
	a := testApp(t)
	request(a, "POST", "/api/sessions", map[string]string{"title": "p1"})
	w := request(a, "GET", "/api/factory-reset/preview", nil)
	requireStatus(t, w, 200)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	sm, _ := out["sessionsAndMemory"].(map[string]any)
	if sm["sessionFiles"].(float64) < 1 {
		t.Fatalf("preview 未统计会话数: %v", out)
	}
}
