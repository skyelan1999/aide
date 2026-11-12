package localmounts

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Volume struct {
	Type     string          `json:"type"`
	Source   string          `json:"source"`
	Target   string          `json:"target"`
	ReadOnly bool            `json:"read_only"`
	Bind     map[string]bool `json:"bind,omitempty"`
}

func bind(source, target string, ro bool) Volume {
	// Compose interpolates dollar expressions even in JSON/YAML string values.
	return Volume{"bind", strings.ReplaceAll(source, "$", "$$"), target, ro, map[string]bool{"create_host_path": false}}
}

// Prepare never creates selected directories. An unplugged drive is omitted,
// with an explicit error, rather than silently mounted as an empty directory.
func Prepare(root string, protected []string) (Config, string, error) {
	return prepare(root, protected, nil, nil)
}

// PrepareRevision freezes one validated desired snapshot and checks the native
// confirmation revision before writing any active metadata. Subsequent saves
// remain pending; no second read can substitute an unconfirmed configuration.
func PrepareRevision(root string, protected []string, expected string) (Config, string, error) {
	return prepare(root, protected, &expected, nil)
}

// Exporter accepts ownership of an opened directory only on success.
type Exporter func(Entry, *os.Root) (string, map[string]any, error)

func PrepareWithBridge(root string, protected []string, expected *string, export Exporter) (Config, string, error) {
	return prepare(root, protected, expected, export)
}
func prepare(root string, protected []string, expected *string, export Exporter) (Config, string, error) {
	dir := filepath.Join(root, ".aide-data", "storage", "data", "local-mounts")
	desired, err := Read(filepath.Join(dir, "desired.json"))
	if err != nil {
		return Config{}, "", err
	}
	if expected != nil && Revision(desired) != *expected {
		return Config{}, "", errors.New("directory configuration changed before preparation; review it again")
	}
	active := desired
	active.Entries = append([]Entry{}, desired.Entries...)
	volumes := []Volume{}
	definitions := map[string]any{}
	resolved := []string{}
	for i, e := range active.Entries {
		if Windows(e.Path) != (os.PathSeparator == '\\') {
			active.Entries[i].Error = "directory belongs to a different operating system"
			continue
		}
		real, err := filepath.EvalSymlinks(e.Path)
		if err != nil {
			active.Entries[i].Error = "directory unavailable; reconnect the drive and apply again"
			continue
		}
		info, err := os.Stat(real)
		if err != nil || !info.IsDir() {
			active.Entries[i].Error = "not an available directory"
			continue
		}
		for _, p := range append(protected, resolved...) {
			if p == "" {
				continue
			}
			pReal, err := filepath.EvalSymlinks(p)
			if err == nil && Overlap(pReal, real) {
				return Config{}, "", fmt.Errorf("%s overlaps an existing mounted directory; narrow AIDE_LOCAL_ROOT or choose a separate directory", e.Name)
			}
		}
		if export == nil {
			return Config{}, "", errors.New("selected directories require the native object bridge; use the current launcher")
		}
		opened, err := os.OpenRoot(real)
		if err != nil {
			return Config{}, "", err
		}
		pinned, err := opened.Stat(".")
		if err != nil || !os.SameFile(info, pinned) {
			opened.Close()
			return Config{}, "", errors.New("selected directory changed while opening; select it again")
		}
		name, definition, err := export(e, opened)
		if err != nil {
			opened.Close()
			return Config{}, "", err
		}
		if name == "" || definition == nil {
			opened.Close()
			return Config{}, "", errors.New("directory bridge returned no volume")
		}
		resolved = append(resolved, real)
		definitions[name] = definition
		volumes = append(volumes, Volume{Type: "volume", Source: name, Target: Target(e), ReadOnly: e.ReadOnly})
	}
	native := filepath.Join(root, ".aide-data", "local-mounts")
	if err := os.MkdirAll(native, 0700); err != nil {
		return Config{}, "", err
	}
	snapshot := filepath.Join(native, "active.json")
    previousMode:=os.FileMode(0600);if st,e:=os.Stat(snapshot);e==nil{previousMode=st.Mode().Perm()}
    previous, previousErr := os.ReadFile(snapshot)
	if previousErr != nil && !os.IsNotExist(previousErr) {
		return Config{}, "", previousErr
	}
	if err := Write(snapshot, active); err != nil {
		return Config{}, "", err
	}
	// The parent is host-private; the read-only metadata bind must be readable
	// by the configured container UID. It contains paths/rights, never export keys.
	if err := os.Chmod(snapshot, 0644); err != nil {
		return Config{}, "", err
	}
	volumes = append(volumes, bind(snapshot, "/run/aide-local-mounts.json", true))
	override := filepath.Join(native, "compose.json")
	value := map[string]any{"volumes": definitions, "services": map[string]any{"aide": map[string]any{"volumes": volumes, "environment": map[string]string{"AIDE_LOCAL_MOUNTS": "/run/aide-local-mounts.json", "AIDE_LOCAL_MOUNTS_REVISION": Revision(active)}}}}
	if err := Write(override, value); err != nil {
		var rollback error
		if os.IsNotExist(previousErr) {
			rollback = os.Remove(snapshot)
		} else {
            rollback=Write(snapshot,json.RawMessage(previous));if rollback==nil{rollback=os.Chmod(snapshot,previousMode)}
		}
		if rollback != nil {
			return Config{}, "", fmt.Errorf("prepare failed: %v; snapshot rollback failed: %w", err, rollback)
		}
		return Config{}, "", err
	}
	return active, override, nil
}
func ValidateNativeRoot(root string) error {
	info, err := os.Stat(filepath.Join(root, "compose.yaml"))
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("launcher root has no compose.yaml")
	}
	return nil
}
