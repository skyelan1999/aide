package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// 锁屏集群的权威持久化状态（lock-cluster.js 后端权威优先方案）。
//
// 背景：lock-cluster.js 仅靠 BroadcastChannel 做跨 tab leader election。主 tab 刷新时旧
// master 消失，新 tab 在加入窗口期内见不到任何心跳，旧逻辑以“默认锁定”当选——即便此前
// 从未锁过也会锁屏。该硬默认无法区分“从未锁”与“锁了、master 重启”。
//
// 方案：后端持有权威锁定状态（config/lock-state.json）。master 执行锁定/解锁/空闲升锁/
// 切换时写入；新 tab 选举、joining veil 超时、看门狗重选前先读本状态，以后端为准；仅当
// 后端不可达/无法判断才回退前端保守默认。
//
// 安全：仅持有布尔与代际/时间戳，不含凭据、会话或任何敏感内容；目录 0700、文件 0600。

const lockStateFileName = "lock-state.json"

// LockStateFile 权威锁屏状态（持久化 JSON）。
type LockStateFile struct {
	Locked    bool  `json:"locked"`
	Gen       int64 `json:"gen"`
	UpdatedAt int64 `json:"updatedAt"` // unix 秒
}

// lockStatePath 返回 config/lock-state.json 绝对路径。
// 归属 config 目录（明文、非机密的运行态标志），与 settings.json 同层。
func lockStatePath(data string) string {
	return filepath.Join(ConfigDir(data), lockStateFileName)
}

// readLockState 读权威状态。文件缺失/损坏视为“未锁、gen=0”初始态：
// 全新部署与首次启动默认不锁，避免刷新即误锁。
func (a *App) readLockState() LockStateFile {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.loadLockStateLocked()
}

func (a *App) loadLockStateLocked() LockStateFile {
	b, err := os.ReadFile(lockStatePath(a.dataPath))
	if err != nil {
		return LockStateFile{}
	}
	var st LockStateFile
	if json.Unmarshal(b, &st) != nil {
		return LockStateFile{}
	}
	return st
}

// writeLockState 由（自封的）master 写入权威状态。
// 每次写单调递增 gen（last-write-wins；gen 用于观察/测试，并让前端可察觉状态切换）。
// atomicJSON 的临时文件以 0600 创建后 rename，落盘即 0600。
func (a *App) writeLockState(locked bool) (LockStateFile, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	cur := a.loadLockStateLocked()
	st := LockStateFile{Locked: locked, Gen: cur.Gen + 1, UpdatedAt: time.Now().Unix()}
	if err := atomicJSON(lockStatePath(a.dataPath), st); err != nil {
		return cur, err
	}
	return st, nil
}

// GET /api/lock-state：读取权威锁定状态。
// 走外层统一的普通 access-token 鉴权；仅回显 locked/gen/updatedAt，不泄露任何敏感内容。
func (a *App) lockStateGet(w http.ResponseWriter, r *http.Request) {
	st := a.readLockState()
	jsonOut(w, 200, map[string]any{"locked": st.Locked, "gen": st.Gen, "updatedAt": st.UpdatedAt})
}

// PUT /api/lock-state：master 更新权威状态。
// body: {"locked": bool}。仅 master 在执行锁定/解锁/空闲升锁/切换成功后调用。
func (a *App) lockStatePut(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Locked bool `json:"locked"`
	}
	if err := decode(w, r, &in); err != nil {
		fail(w, 400, err)
		return
	}
	st, err := a.writeLockState(in.Locked)
	if err != nil {
		fail(w, 500, err)
		return
	}
	jsonOut(w, 200, map[string]any{"locked": st.Locked, "gen": st.Gen, "updatedAt": st.UpdatedAt})
}
