package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

type terminalMasterPins struct {
	mu     sync.Mutex
	dir    string
	info   os.FileInfo
	closed bool
}

func terminalMasterPinDir(dataPath string) string {
	owner := sha256.Sum256([]byte(dataPath))
	return filepath.Join(os.TempDir(), "aide-terminal-masters-"+hex.EncodeToString(owner[:8]))
}

func terminalPinName(s string) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == 16 && s == hex.EncodeToString(b)
}

// Only a confirmed refused listener is obsolete. Timeouts, permission errors,
// and live old masters are preserved even after their original path is replaced.
// all is used only for this App's private directory after its daemon has stopped.
func reapTerminalMasterPins(parent context.Context, dir string, expected os.FileInfo, all bool) {
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	st, err := os.Lstat(dir)
	if err != nil || expected == nil || !os.SameFile(st, expected) || !st.IsDir() || st.Mode().Perm() != 0700 {
		return
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return
	}
	defer root.Close()
	f, err := root.Open(".")
	if err != nil {
		return
	}
	defer f.Close()
	for {
		entries, readErr := f.ReadDir(128)
		for _, entry := range entries {
			if ctx.Err() != nil {
				return
			}
			name := entry.Name()
			if !terminalPinName(name) {
				continue
			}
			before, err := root.Lstat(name)
			if err != nil || before.Mode()&os.ModeSocket == 0 {
				continue
			}
			if !all {
				conn, err := (&net.Dialer{Timeout: 100 * time.Millisecond}).DialContext(ctx, "unix", filepath.Join(dir, name))
				if err == nil {
					conn.Close()
					continue
				}
				if !errors.Is(err, syscall.ECONNREFUSED) {
					continue
				}
			}
			after, err := root.Lstat(name)
			if err == nil && os.SameFile(before, after) {
				_ = root.Remove(name)
			}
		}
		if readErr == io.EOF {
			return
		}
		if readErr != nil {
			return
		}
	}
}

func (a *App) pinTerminalMaster(socket string) (string, error) {
	p := &a.terminalPins
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return "", errors.New("终端后台已停止")
	}
	if p.dir == "" {
		// Reclaim only confirmed-dead links from the legacy deterministic folder.
		legacy := terminalMasterPinDir(a.dataPath)
		if info, err := os.Lstat(legacy); err == nil && info.IsDir() && info.Mode().Perm() == 0700 {
			reapTerminalMasterPins(context.Background(), legacy, info, false)
			_ = os.Remove(legacy) // succeeds only when empty
		}
		dir, err := os.MkdirTemp("", "aide-terminal-masters-")
		if err != nil {
			return "", err
		}
		info, err := os.Lstat(dir)
		if err != nil {
			_ = os.Remove(dir)
			return "", err
		}
		p.dir, p.info = dir, info
		a.background(func() {
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-a.bgCtx.Done():
					return
				case <-ticker.C:
					p.mu.Lock()
					if !p.closed {
						reapTerminalMasterPins(a.bgCtx, p.dir, p.info, false)
					}
					p.mu.Unlock()
				}
			}
		})
	}
	if st, err := os.Lstat(p.dir); err != nil || !os.SameFile(st, p.info) {
		return "", errors.New("SSH 连接固定目录已变化")
	}
	return pinTerminalSSHMasterInDir(p.dir, socket)
}

func (a *App) closeTerminalMasterPins() {
	p := &a.terminalPins
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	if p.dir != "" {
		reapTerminalMasterPins(context.Background(), p.dir, p.info, true)
		if st, err := os.Lstat(p.dir); err == nil && os.SameFile(st, p.info) {
			_ = os.Remove(p.dir)
		}
	}
}
