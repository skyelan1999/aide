package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

var terminalStateMu sync.Mutex

func terminalStateHash(identity any) string {
	raw, _ := json.Marshal(identity)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func terminalPrivateDirectory(dir string) error {
	if err := os.Mkdir(dir, 0700); err != nil && !os.IsExist(err) {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("终端私有状态目录无效")
	}
	return os.Chmod(dir, 0700)
}

// Persist the locator independently of installation/container paths. Reuse old
// directories in place, including relay secrets and logs; never merge projects
// or move files out from under a running daemon.
func terminalStateDirectory(dataPath string, config WorkspaceConfig, local string) (string, error) {
	terminalStateMu.Lock()
	defer terminalStateMu.Unlock()
	w := config.Workspace
	identity := []any{"terminal-state-v2", w.Mode, filepath.Clean(w.Path)}
	if w.Mode == "ssh" {
		// The remote account and endpoint remain separate security boundaries.
		identity = append(identity, w.Host, w.Port, w.Username)
	}
	base := filepath.Join(dataPath, "terminal-private")
	if err := terminalPrivateDirectory(base); err != nil {
		return "", err
	}
	key := terminalStateHash(identity)
	locator := filepath.Join(base, key+".root.json")
	if info, err := os.Lstat(locator); err == nil {
		if !info.Mode().IsRegular() || info.Size() > 128 {
			return "", errors.New("终端存储索引无效")
		}
		raw, err := os.ReadFile(locator)
		if err != nil {
			return "", err
		}
		if json.Unmarshal(raw, &key) != nil || len(key) != 64 {
			return "", errors.New("终端存储索引无效")
		}
		decoded, err := hex.DecodeString(key)
		if err != nil || len(decoded) != 32 {
			return "", errors.New("终端存储索引无效")
		}
	} else if !os.IsNotExist(err) {
		return "", err
	} else {
		scope := w.Mode + "|" + w.Path + "|" + w.Host
		aliases := []string{local}
		if w.Mode == "local" && filepath.IsAbs(w.Path) {
			// Native and Docker mounts of the same configured host directory.
			aliases = append(aliases, w.Path, filepath.Join("/local", w.Path))
		}
		for _, alias := range aliases {
			legacy := terminalStateHash([]any{scope, w.Mode, w.Port, w.Username, alias})
			info, err := os.Lstat(filepath.Join(base, legacy))
			if os.IsNotExist(err) {
				continue
			}
			if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return "", errors.New("终端私有状态目录无效")
			}
			key = legacy
			break
		}
		if err := atomicJSON(locator, key); err != nil {
			return "", err
		}
	}
	dir := filepath.Join(base, key)
	if err := terminalPrivateDirectory(dir); err != nil {
		return "", err
	}
	return dir, nil
}
