// Package localmounts defines the bounded host-directory mount contract.
package localmounts

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

const Limit = 16

var idPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,39}$`)

type Entry struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Path     string `json:"path"`
	ReadOnly bool   `json:"readOnly"`
	Error    string `json:"error,omitempty"`
}
type Config struct {
	Version int     `json:"version"`
	Entries []Entry `json:"entries"`
}

func Empty() Config { return Config{Version: 1, Entries: []Entry{}} }
func Windows(p string) bool {
	return len(p) > 2 && ((p[0] >= 'A' && p[0] <= 'Z') || (p[0] >= 'a' && p[0] <= 'z')) && p[1] == ':' && (p[2] == '/' || p[2] == '\\')
}
func Normalize(p string) (string, error) {
	if len(p) > 4096 || strings.TrimSpace(p) != p || strings.ContainsAny(p, "\x00\r\n\t") {
		return "", errors.New("invalid host directory")
	}
	if Windows(p) {
		p = strings.ReplaceAll(p, "\\", "/")
		p = strings.ToUpper(p[:1]) + p[1:]
		p = p[:2] + path.Clean("/"+strings.TrimPrefix(p[2:], "/"))
	} else if strings.HasPrefix(p, "/") && !strings.HasPrefix(p, "//") {
		p = path.Clean(p)
	} else {
		return "", errors.New("use an absolute Windows drive or POSIX directory")
	}
	if p == "/" || (Windows(p) && len(p) == 3) {
		return "", errors.New("select a directory, not an entire drive")
	}
	return p, nil
}
func Relative(base, candidate string) (string, bool) {
	b, p := strings.ReplaceAll(base, "\\", "/"), strings.ReplaceAll(candidate, "\\", "/")
	windows := Windows(b)
	b = strings.TrimRight(b, "/")
	baseParts, candidateParts := strings.Split(b, "/"), strings.Split(p, "/")
	if len(candidateParts) < len(baseParts) {
		return "", false
	}
	for i, component := range baseParts {
		match := component == candidateParts[i]
		if windows {
			match = strings.EqualFold(component, candidateParts[i])
		}
		if !match {
			return "", false
		}
	}
	if len(candidateParts) == len(baseParts) {
		return ".", true
	}
	// Use the candidate's original components. Unicode case folding can change
	// UTF-8 lengths, so transformed prefix lengths cannot index the input.
	rel := strings.Join(candidateParts[len(baseParts):], "/")
	// Do not normalize traversal into a different mounted root.
	for _, v := range strings.Split(rel, "/") {
		if v == ".." {
			return "", false
		}
	}
	return rel, true
}
func Overlap(a, b string) bool { _, ab := Relative(a, b); _, ba := Relative(b, a); return ab || ba }
func Validate(c Config) error {
	if c.Version != 1 || len(c.Entries) > Limit {
		return errors.New("invalid mount configuration version or too many directories")
	}
	ids := map[string]bool{}
	for i, e := range c.Entries {
		if !idPattern.MatchString(e.ID) || ids[e.ID] || len(e.Name) == 0 || len(e.Name) > 120 || strings.ContainsAny(e.Name, "\x00\r\n") {
			return errors.New("invalid or duplicate directory ID/name")
		}
		ids[e.ID] = true
		p, err := Normalize(e.Path)
		if err != nil || p != e.Path {
			return fmt.Errorf("%s: use a normalized absolute directory", e.Name)
		}
		for _, prior := range c.Entries[:i] {
			if Overlap(prior.Path, e.Path) {
				return errors.New("mounted directories must not overlap")
			}
		}
	}
	return nil
}
func Revision(c Config) string {
	b, _ := json.Marshal(c)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func Read(p string) (Config, error) {
	b, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return Empty(), nil
	}
	if err != nil {
		return Config{}, err
	}
	if len(b) > 128<<10 {
		return Config{}, errors.New("mount configuration too large")
	}
	var c Config
	err = json.Unmarshal(b, &c)
	if err == nil {
		err = Validate(c)
	}
	return c, err
}
func Write(p string, v any) error {
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(p), ".mounts-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(append(b, '\n'))
	}
	if ce := f.Close(); err == nil {
		err = ce
	}
	if err != nil {
		return err
	}
	return os.Rename(name, p)
}
func Target(e Entry) string { return "/mounts/" + e.ID }
