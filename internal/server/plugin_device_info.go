package server

import (
	"aide/internal/messaging"
	"errors"
	"os"
)

// Public display metadata only. Reading it must never opt into messaging,
// create an identity, or expose the seed, fingerprint or contact database.
func (a *App) pluginDeviceInfo() (map[string]any, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.vault == nil || !a.vault.Unlocked() {
		return nil, errors.New("请先解锁保险库后读取设备码")
	}
	seed, err := a.vault.Get("message:device")
	if errors.Is(err, os.ErrNotExist) {
		return map[string]any{"available": false, "code": ""}, nil
	}
	if err != nil {
		return nil, errors.New("暂时无法读取 Aide 设备码")
	}
	defer zeroBytes(seed)
	identity, err := messaging.FromSeed(seed)
	if err != nil {
		return nil, errors.New("Aide 设备标识无效")
	}
	return map[string]any{"available": true, "code": identity.Device().Code}, nil
}
