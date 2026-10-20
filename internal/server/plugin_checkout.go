package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// seedCheckoutPlugins initializes a fresh source checkout once. Installed
// plugins and their registry are workspace state and must never be refreshed
// from the pristine build/test bundle during an upgrade.
func (a *App) seedCheckoutPlugins() error {
	if _, err := os.Lstat(filepath.Join(a.pluginsPath, "registry.json")); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	bundle := filepath.Join(a.workPath, "bundled-plugins")
	raw, err := os.ReadFile(filepath.Join(bundle, "registry.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var registry pluginRegistry
	if err := json.Unmarshal(raw, &registry); err != nil {
		return err
	}
	if err := os.MkdirAll(a.pluginsPath, 0755); err != nil {
		return err
	}
	for _, p := range registry.Plugins {
		if !pluginIDPattern.MatchString(p.ID) {
			return fmt.Errorf("invalid checkout plugin id: %s", p.ID)
		}
		destination := filepath.Join(a.pluginsPath, p.ID)
		if _, err := os.Lstat(destination); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		stage, err := os.MkdirTemp(a.pluginsPath, ".seed-")
		if err != nil {
			return err
		}
		err = os.CopyFS(stage, os.DirFS(filepath.Join(bundle, p.ID)))
		if err == nil {
			err = os.Rename(stage, destination)
		}
		_ = os.RemoveAll(stage)
		if err != nil {
			return err
		}
	}
	// Exclusive creation preserves a registry another startup may have written.
	file, err := os.OpenFile(filepath.Join(a.pluginsPath, "registry.json"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if errors.Is(err, os.ErrExist) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = file.Write(raw)
	closeErr := file.Close()
	if err != nil {
		return err
	}
	return closeErr
}
