package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// 最小 App 构造：read/write/handler 仅依赖 dataPath + a.mu，避免拉起重型 New()。
func newLockStateTestApp(t *testing.T) *App {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(ConfigDir(dir), 0700); err != nil {
		t.Fatal(err)
	}
	return &App{dataPath: dir}
}

// 文件缺失 = 全新部署：默认未锁、gen=0（刷新不误锁的根基）。
func TestLockStateMissingDefaultsUnlocked(t *testing.T) {
	a := newLockStateTestApp(t)
	st := a.readLockState()
	if st.Locked || st.Gen != 0 || st.UpdatedAt != 0 {
		t.Fatalf("缺失文件应默认未锁 gen=0，got %+v", st)
	}
}

// 写后读回环；连续写代际单调递增。
func TestLockStateWriteReadRoundtripAndGen(t *testing.T) {
	a := newLockStateTestApp(t)

	s1, err := a.writeLockState(true)
	if err != nil || !s1.Locked || s1.Gen != 1 {
		t.Fatalf("第一次写应 locked gen=1，got %+v err=%v", s1, err)
	}
	if s1.UpdatedAt == 0 {
		t.Fatal("UpdatedAt 应被填充")
	}

	s2, err := a.writeLockState(false)
	if err != nil || s2.Locked || s2.Gen != 2 {
		t.Fatalf("第二次写应 unlocked gen=2，got %+v err=%v", s2, err)
	}

	got := a.readLockState()
	if got.Locked || got.Gen != 2 {
		t.Fatalf("读回应为 unlocked gen=2，got %+v", got)
	}
}

// 落盘权限 0600（不泄露给同机其他用户）。
func TestLockStateFileMode0600(t *testing.T) {
	a := newLockStateTestApp(t)
	if _, err := a.writeLockState(true); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(lockStatePath(a.dataPath))
	if err != nil {
		t.Fatal(err)
	}
	if m := fi.Mode().Perm(); m != 0o600 {
		t.Fatalf("lock-state.json 权限应为 0600，got %o", m)
	}
}

// 损坏文件按初始未锁处理，不 panic。
func TestLockStateCorruptedFallsBackUnlocked(t *testing.T) {
	a := newLockStateTestApp(t)
	if err := os.WriteFile(lockStatePath(a.dataPath), []byte("{not json"), 0600); err != nil {
		t.Fatal(err)
	}
	st := a.readLockState()
	if st.Locked || st.Gen != 0 {
		t.Fatalf("损坏文件应回退未锁 gen=0，got %+v", st)
	}
}

// GET / PUT 处理端到端。
func TestLockStateHandlers(t *testing.T) {
	a := newLockStateTestApp(t)

	// GET 初始未锁
	rr := httptest.NewRecorder()
	a.lockStateGet(rr, httptest.NewRequest(http.MethodGet, "/api/lock-state", nil))
	if rr.Code != 200 {
		t.Fatalf("GET 初始应 200，got %d", rr.Code)
	}
	var out map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["locked"] != false {
		t.Fatalf("GET 初始应 locked=false，got %v", out)
	}

	// PUT 锁定
	body, _ := json.Marshal(map[string]any{"locked": true})
	rr = httptest.NewRecorder()
	a.lockStatePut(rr, httptest.NewRequest(http.MethodPut, "/api/lock-state", bytes.NewReader(body)))
	if rr.Code != 200 {
		t.Fatalf("PUT 应 200，got %d body=%s", rr.Code, rr.Body.String())
	}

	// GET 反映已锁
	rr = httptest.NewRecorder()
	a.lockStateGet(rr, httptest.NewRequest(http.MethodGet, "/api/lock-state", nil))
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["locked"] != true {
		t.Fatalf("PUT 后 GET 应 locked=true，got %v", out)
	}

	// 路径落在 config/lock-state.json
	if got := lockStatePath(a.dataPath); filepath.Base(got) != "lock-state.json" || filepath.Base(filepath.Dir(got)) != "config" {
		t.Fatalf("路径应在 config/lock-state.json，got %s", got)
	}
}
