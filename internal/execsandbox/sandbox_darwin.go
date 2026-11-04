//go:build darwin

package execsandbox

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/sys/unix"
)

var (
	darwinAvailableOnce sync.Once
	darwinAvailableErr  error
)

func ABI() (int, error) {
	if err := Available(); err != nil {
		return 0, err
	}
	return 1, nil // Aide's macOS profile backend is not a Landlock ABI.
}

func Available() error {
	darwinAvailableOnce.Do(func() {
		const profile = `(version 1)
(import "system.sb")
(deny default)
(allow process*)
(allow file-read* file-test-existence (subpath "/usr") (subpath "/bin") (subpath "/System"))`
		cmd := exec.Command("/usr/bin/sandbox-exec", "-p", profile, "/usr/bin/true")
		if out, err := cmd.CombinedOutput(); err != nil {
			darwinAvailableErr = fmt.Errorf("macOS sandbox-exec unavailable: %w %s", err, strings.TrimSpace(string(out)))
		}
	})
	return darwinAvailableErr
}

func writeDarwinProfile(tmp string, p policy) (string, error) {
	if err := Available(); err != nil {
		return "", err
	}
	if p.Mode != "read-only" && p.Mode != "workspace-write" {
		return "", fmt.Errorf("invalid mode")
	}
	if !filepath.IsAbs(p.Workspace) || !filepath.IsAbs(p.Temporary) || p.Workspace == "/" || p.Temporary == "/" {
		return "", fmt.Errorf("invalid sandbox roots")
	}
	quote := func(value string) (string, error) {
		if strings.ContainsRune(value, 0) {
			return "", fmt.Errorf("sandbox path contains NUL")
		}
		value = strings.ReplaceAll(value, `\`, `\\`)
		value = strings.ReplaceAll(value, `"`, `\"`)
		value = strings.ReplaceAll(value, "\n", `\n`)
		value = strings.ReplaceAll(value, "\r", `\r`)
		return `"` + value + `"`, nil
	}
	workspace, err := quote(p.Workspace)
	if err != nil {
		return "", err
	}
	temporary, err := quote(p.Temporary)
	if err != nil {
		return "", err
	}
	var rules []string
	rules = append(rules,
		`(version 1)`,
		`(import "system.sb")`,
		`(deny default)`,
		`(allow process*)`,
		`(allow sysctl-read)`,
		`(allow network*)`,
		`(allow file-read* file-test-existence (subpath "/usr") (subpath "/bin") (subpath "/sbin") (subpath "/System") (subpath "/etc") (subpath "/private/etc") (subpath "/opt/homebrew"))`,
		`(allow file-map-executable (subpath "/usr") (subpath "/bin") (subpath "/sbin") (subpath "/System") (subpath "/opt/homebrew"))`,
		`(allow file-read* file-test-existence (subpath `+workspace+`) (subpath `+temporary+`))`,
		`(allow file-map-executable (subpath `+workspace+`) (subpath `+temporary+`))`,
		`(allow file-read* file-test-existence (path-ancestors `+workspace+`) (path-ancestors `+temporary+`))`,
		`(allow file-write* (subpath `+temporary+`))`,
		`(allow file-write* (subpath `+workspace+`))`,
	)
	if p.Mode == "read-only" {
		rules = rules[:len(rules)-1]
	}
	if p.Runtime != "" {
		runtimeRoot, err := quote(p.Runtime)
		if err != nil {
			return "", err
		}
		rules = append(rules,
			`(allow file-read* file-test-existence (subpath `+runtimeRoot+`))`,
			`(allow file-map-executable (subpath `+runtimeRoot+`))`,
			`(allow file-read* file-test-existence (path-ancestors `+runtimeRoot+`))`,
		)
	}
	profilePath := filepath.Join(tmp, "sandbox.sb")
	if err := os.WriteFile(profilePath, []byte(strings.Join(rules, "\n")+"\n"), 0600); err != nil {
		return "", err
	}
	return profilePath, nil
}

func darwinShellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func runChild(p policy, command string) error {
	if err := Available(); err != nil {
		return err
	}
	if p.Profile == "" || filepath.Dir(p.Profile) != p.Temporary {
		return fmt.Errorf("invalid sandbox profile path")
	}
	path := p.Path
	if path == "" {
		path = "/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"
	}
	env := []string{
		"PATH=" + path,
		"LANG=C.UTF-8",
		"TERM=dumb",
		"HOME=" + filepath.Join(p.Temporary, "home"),
		"TMPDIR=" + p.Temporary,
		"GOCACHE=" + filepath.Join(p.Temporary, "cache"),
		"GOPATH=" + filepath.Join(p.Temporary, "go"),
		"AIDE_CACHE=" + filepath.Join(p.Temporary, "cache"),
		"AIDE_SANDBOX=1",
	}
	argv := []string{"sandbox-exec", "-f", p.Profile, "/bin/bash", "--noprofile", "--norc", "-c", "cd " + darwinShellQuote(p.Workspace) + " && " + command}
	return unix.Exec("/usr/bin/sandbox-exec", argv, env)
}
