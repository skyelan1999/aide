package main

// Preparation is a native process operation. The bridge exposes no HTTP/RPC
// control API and cannot be asked over its data-plane connection to export a path.
import (
	"aide/internal/hostbridge"
	"aide/internal/localmounts"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"
)

const bridgeProtocol = 1

type bridgeReady struct {
	Protocol, PID   int
	Override, Error string
	Volumes         []string
}
type bridgeIdentity struct{ Path, Identity string }

func bridgeRunDir(root, ticket string) (string, error) {
	if len(ticket) != 64 {
		return "", errors.New("invalid bridge generation")
	}
	if _, e := hex.DecodeString(ticket); e != nil {
		return "", errors.New("invalid bridge generation")
	}
	return filepath.Join(root, ".aide-data", "local-mounts", "bridge-runs", ticket), nil
}
func bridgeTicket() (string, error) {
	b := make([]byte, 32)
	_, e := rand.Read(b)
	return hex.EncodeToString(b), e
}
func readBridgeReady(root, ticket string) (bridgeReady, error) {
	dir, e := bridgeRunDir(root, ticket)
	if e != nil {
		return bridgeReady{}, e
	}
	b, e := os.ReadFile(filepath.Join(dir, "ready.json"))
	if e != nil {
		return bridgeReady{}, e
	}
	var out bridgeReady
	if len(b) > 8192 || json.Unmarshal(b, &out) != nil {
		return out, errors.New("invalid bridge readiness receipt")
	}
	return out, nil
}

// Keep Windows executables independent of the helper that the launcher replaces.
// These are verified copies of the selected native helper, not new releases.
func immutableBridgeBinary(root string) (string, error) {
	source, e := os.Executable()
	if e != nil {
		return "", e
	}
	b, e := os.ReadFile(source)
	if e != nil {
		return "", e
	}
	if len(b) > 64<<20 {
		return "", errors.New("native bridge executable exceeds budget")
	}
	sum := sha256.Sum256(b)
	dir := filepath.Join(root, ".aide-data", "local-mounts", "bridge-binaries")
	if e = os.MkdirAll(dir, 0700); e != nil {
		return "", e
	}
	name := hex.EncodeToString(sum[:])
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	target := filepath.Join(dir, name)
	if current, e := os.ReadFile(target); e == nil {
		if sha256.Sum256(current) != sum {
			return "", errors.New("native bridge copy changed")
		}
		return target, nil
	}
	temporary := target + ".tmp-" + strconv.Itoa(os.Getpid())
	if e = os.WriteFile(temporary, b, 0700); e != nil {
		return "", e
	}
	defer os.Remove(temporary)
	if e = os.Rename(temporary, target); e != nil {
		return "", e
	}
	return target, nil
}

// A bridge generation remains alive while its own Docker volumes are in use.
// It is never killed by PID; stale ready receipts are not process ownership.
func prepareViaBridge(ctx context.Context, root, docker string, expected *string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 115*time.Second)
	defer cancel()
	desired, e := localmounts.Read(filepath.Join(root, ".aide-data/storage/data/local-mounts/desired.json"))
	if e != nil {
		return "", e
	}
	if expected != nil && localmounts.Revision(desired) != *expected {
		return "", errors.New("directory configuration changed before preparation")
	}
	if len(desired.Entries) == 0 {
		p, e := protected(ctx, docker)
		if e != nil {
			return "", e
		}
		release, e := lockPreparation(root)
		if e != nil {
			return "", e
		}
		defer release()
		saved, e := saveBridgeMetadata(root)
		if e != nil {
			return "", e
		}
		_, out, e := localmounts.PrepareWithBridge(root, p, expected, nil)
		if e == nil {
			e = localmounts.Write(filepath.Join(root, ".aide-data/local-mounts/bridge-identities.json"), map[string]bridgeIdentity{})
		}
		if e != nil {
			if rollback := restoreBridgeMetadata(saved); rollback != nil {
				return "", fmt.Errorf("prepare failed: %v; metadata restore failed: %w", e, rollback)
			}
		}
		return out, e
	}
	binary, e := immutableBridgeBinary(root)
	if e != nil {
		return "", e
	}
	ticket, e := bridgeTicket()
	if e != nil {
		return "", e
	}
	dir, _ := bridgeRunDir(root, ticket)
	if e = os.MkdirAll(dir, 0700); e != nil {
		return "", e
	}
	args := []string{"-mode", "bridge-prepare", "-root", root, "-docker", docker, "-bridge-ticket", ticket}
	if expected != nil {
		args = append(args, "-bridge-revision", *expected)
	}
	log, e := os.OpenFile(filepath.Join(dir, "bridge.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if e != nil {
		return "", e
	}
	defer log.Close()
	child := exec.Command(binary, args...)
	child.Stdin = nil
	child.Stdout = log
	child.Stderr = log
	detachBridge(child)
	if e = child.Start(); e != nil {
		return "", e
	}
	pid := child.Process.Pid
	finished := make(chan error, 1)
	go func() { finished <- child.Wait() }()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-finished:
			if ready, e := readBridgeReady(root, ticket); e == nil && ready.PID == pid && ready.Protocol == bridgeProtocol {
				if ready.Error != "" {
					return "", errors.New(ready.Error)
				}
				if ready.Override == filepath.Join(root, ".aide-data/local-mounts/compose.json") {
					return ready.Override, nil
				}
			}
			return "", errors.New("native directory bridge exited before readiness; inspect its log")
		case <-ctx.Done():
			_ = os.WriteFile(filepath.Join(dir, "stop"), []byte("stop"), 0600)
			return "", ctx.Err()
		case <-ticker.C:
			ready, e := readBridgeReady(root, ticket)
			if os.IsNotExist(e) {
				continue
			}
			if e != nil {
				return "", e
			}
			if ready.PID != pid || ready.Protocol != bridgeProtocol {
				return "", errors.New("native bridge readiness identity mismatch")
			}
			if ready.Error != "" {
				return "", errors.New(ready.Error)
			}
			if ready.Override != filepath.Join(root, ".aide-data/local-mounts/compose.json") {
				return "", errors.New("unexpected native bridge override")
			}
			return ready.Override, nil
		}
	}
}

func bridgeAddress(ctx context.Context, docker string) (string, error) {
	// The native Linux daemon mounts in its host namespace. Desktop daemons
	// instead reach a loopback service through their existing host proxy.
	if runtime.GOOS == "linux" {
		out, e := command(ctx, docker, "info", "--format", "{{.OperatingSystem}}")
		if e != nil {
			return "", errors.New("cannot identify Docker host")
		}
		if !strings.Contains(strings.ToLower(string(out)), "docker desktop") {
			return "127.0.0.1", nil
		}
	}
	out, e := compose(ctx, docker, "config", "--format", "json")
	if e != nil {
		return "", e
	}
	var cfg struct {
		Services map[string]struct{ Image string }
	}
	if json.Unmarshal(out, &cfg) != nil || cfg.Services["aide"].Image == "" {
		return "", errors.New("missing selected Aide image")
	}
	// No pull/build and no user volumes. Resolve Desktop's proxy in the daemon's namespace.
	ip, e := command(ctx, docker, "run", "--rm", "--pull=never", "--entrypoint", "getent", cfg.Services["aide"].Image, "ahostsv4", "host.docker.internal")
	if e != nil {
		return "", errors.New("Docker Desktop host proxy is unavailable")
	}
	fields := strings.Fields(string(ip))
	if len(fields) == 0 || net.ParseIP(fields[0]) == nil {
		return "", errors.New("invalid Docker host proxy address")
	}
	return fields[0], nil
}
func bridgeUser(ctx context.Context, docker string) (uint32, uint32, error) {
	out, e := compose(ctx, docker, "config", "--format", "json")
	if e != nil {
		return 0, 0, e
	}
	var cfg struct {
		Services map[string]struct{ User string }
	}
	if json.Unmarshal(out, &cfg) != nil {
		return 0, 0, errors.New("invalid Compose user")
	}
	parts := strings.Split(cfg.Services["aide"].User, ":")
	if len(parts) != 2 {
		return 0, 0, errors.New("directory bridge requires numeric UID:GID")
	}
	u, e := strconv.ParseUint(parts[0], 10, 32)
	if e != nil {
		return 0, 0, e
	}
	g, e := strconv.ParseUint(parts[1], 10, 32)
	return uint32(u), uint32(g), e
}

func servePreparedBridge(ctx context.Context, root, docker, ticket, revision string) error {
	dir, e := bridgeRunDir(root, ticket)
	if e != nil {
		return e
	}
	if e = os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	release, e := lockHelperFile(filepath.Join(dir, "worker.guard"))
	if e != nil {
		return e
	}
	if release == nil {
		return errors.New("bridge generation is already running")
	}
	defer release()
	ready := bridgeReady{Protocol: bridgeProtocol, PID: os.Getpid()}
	preparation, e := lockPreparation(root)
	if e != nil {
		ready.Error = e.Error()
		_ = localmounts.Write(filepath.Join(dir, "ready.json"), ready)
		return e
	}
	defer preparation()
	var rollback func() error
	fail := func(cause error) error {
		if rollback != nil {
			if r := rollback(); r != nil {
				cause = fmt.Errorf("%v; metadata restore failed: %w", cause, r)
			}
			rollback = nil
		}
		ready.Error = cause.Error()
		_ = localmounts.Write(filepath.Join(dir, "ready.json"), ready)
		return cause
	}
	saved, e := saveBridgeMetadata(root)
	if e != nil {
		return fail(e)
	}
	rollback = func() error { return restoreBridgeMetadata(saved) }
	debug.SetMemoryLimit(512 << 20)
	protectedPaths, e := protected(ctx, docker)
	if e != nil {
		return fail(e)
	}
	address, e := bridgeAddress(ctx, docker)
	if e != nil {
		return fail(e)
	}
	uid, gid, e := bridgeUser(ctx, docker)
	if e != nil {
		return fail(e)
	}
	server, e := hostbridge.New(ctx)
	if e != nil {
		return fail(e)
	}
	defer server.Close()
	ledgerPath := filepath.Join(root, ".aide-data/local-mounts/bridge-identities.json")
	previous := map[string]bridgeIdentity{}
	if b, e := os.ReadFile(ledgerPath); e == nil {
		if len(b) > 128<<10 || json.Unmarshal(b, &previous) != nil {
			return fail(errors.New("directory identity receipt is invalid"))
		}
	} else if !os.IsNotExist(e) {
		return fail(e)
	}
	next := map[string]bridgeIdentity{}
	volumes := []string{}
	prefix := sha256.Sum256([]byte(root))
	rootID := hex.EncodeToString(prefix[:6])
	exporter := func(entry localmounts.Entry, opened *os.Root) (string, map[string]any, error) {
		identity, e := hostbridge.Identity(opened)
		if e != nil {
			return "", nil, e
		}
		if old, ok := previous[entry.ID]; ok && old.Path == entry.Path && old.Identity != identity {
			return "", nil, errors.New("selected directory object changed; remove and select the directory again")
		}
		token, e := server.Export(opened, entry.ReadOnly, uid, gid)
		if e != nil {
			return "", nil, e
		}
		next[entry.ID] = bridgeIdentity{entry.Path, identity}
		name := "aide-dir-" + rootID + "-" + ticket[:16] + "-" + entry.ID
		volumes = append(volumes, name)
		options := fmt.Sprintf("addr=%s,port=%d,mountport=%d,nfsvers=3,proto=tcp,mountproto=tcp,nolock,noacl,rsize=1048576,wsize=1048576,soft,timeo=30,retrans=2", address, server.Port(), server.Port())
		definition := map[string]any{"name": name, "driver": "local", "driver_opts": map[string]string{"type": "nfs", "o": options, "device": ":/" + token}}
		return name, definition, nil
	}
	var expected *string
	if revision != "" {
		expected = &revision
	}
	// Preparation can restore its own metadata on failure; selected directories
	// are never created or used as pathname binds.
	active, override, e := localmounts.PrepareWithBridge(root, protectedPaths, expected, exporter)
	if e != nil {
		return fail(e)
	}
	// An unavailable drive keeps its last confirmed object identity.
	for _, entry := range active.Entries {
		if _, ok := next[entry.ID]; !ok {
			if old, exists := previous[entry.ID]; exists && old.Path == entry.Path {
				next[entry.ID] = old
			}
		}
	}
	if e = localmounts.Write(ledgerPath, next); e != nil {
		return fail(e)
	}
	ready.Override = override
	ready.Volumes = volumes
	if _, stop := os.Stat(filepath.Join(dir, "stop")); stop == nil {
		return fail(errors.New("directory preparation cancelled"))
	}
	if e = localmounts.Write(filepath.Join(dir, "ready.json"), ready); e != nil {
		return fail(e)
	}
	rollback = nil
	preparation()
	if len(volumes) == 0 {
		return nil
	}
	lastUse := time.Now()
	timer := time.NewTicker(10 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
			_, stopErr := os.Stat(filepath.Join(dir, "stop"))
			used, known := false, true
			for _, name := range volumes {
				check, cancel := context.WithTimeout(ctx, 5*time.Second)
				out, e := command(check, docker, "ps", "-q", "--filter", "volume="+name)
				cancel()
				if e != nil {
					known = false
					break
				}
				if strings.TrimSpace(string(out)) != "" {
					used = true
					break
				}
			}
			if stopErr == nil && known && !used {
				return nil
			}
			if used || !known {
				lastUse = time.Now()
			}
			if time.Since(lastUse) > 5*time.Minute {
				return nil
			}
		}
	}
}

type bridgeSavedFile struct {
	path   string
	raw    []byte
	exists bool
	mode   os.FileMode
}

func saveBridgeMetadata(root string) ([]bridgeSavedFile, error) {
	out := []bridgeSavedFile{}
	for _, name := range []string{"active.json", "compose.json", "bridge-identities.json"} {
		p := filepath.Join(root, ".aide-data/local-mounts", name)
		st, e := os.Lstat(p)
		if os.IsNotExist(e) {
			out = append(out, bridgeSavedFile{path: p})
			continue
		}
		if e != nil {
			return nil, e
		}
		if !st.Mode().IsRegular() || st.Size() > 1<<20 {
			return nil, errors.New("native directory metadata is not a bounded regular file")
		}
		b, e := os.ReadFile(p)
		if e != nil {
			return nil, e
		}
		if !json.Valid(b) {
			return nil, errors.New("native directory metadata is malformed; preserve it for recovery")
		}
		out = append(out, bridgeSavedFile{path: p, raw: b, exists: true, mode: st.Mode().Perm()})
	}
	return out, nil
}
func restoreBridgeMetadata(files []bridgeSavedFile) error {
	for _, f := range files {
		var e error
		if f.exists {
			e = localmounts.Write(f.path, json.RawMessage(f.raw))
			if e == nil {
				e = os.Chmod(f.path, f.mode)
			}
		} else {
			e = os.Remove(f.path)
			if os.IsNotExist(e) {
				e = nil
			}
		}
		if e != nil {
			return e
		}
	}
	return nil
}
func lockPreparation(root string) (func(), error) {
	dir := filepath.Join(root, ".aide-data/local-mounts")
	if e := os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	release, e := lockHelperFile(filepath.Join(dir, "prepare.guard"))
	if e != nil {
		return nil, e
	}
	if release == nil {
		return nil, errors.New("another native directory preparation is still running")
	}
	var once sync.Once
	return func() { once.Do(release) }, nil
}

// Called only after Compose has completed. Stop requests are data-plane
// revocation, never new exports; the worker rechecks live volume users itself.
func retireUnusedDirectoryBridges(ctx context.Context, root, docker string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	base := filepath.Join(root, ".aide-data/local-mounts/bridge-runs")
	dirs, e := os.ReadDir(base)
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	rootSum := sha256.Sum256([]byte(root))
	rootID := hex.EncodeToString(rootSum[:6])
	for _, dir := range dirs {
		if !dir.IsDir() {
			continue
		}
		ticket := dir.Name()
		if _, e = bridgeRunDir(root, ticket); e != nil {
			continue
		}
		release, lockErr := lockHelperFile(filepath.Join(base, ticket, "worker.guard"))
		if lockErr != nil {
			continue
		}
		if release != nil {
			release()
			continue
		}
		ready, e := readBridgeReady(root, ticket)
		if e != nil || ready.Error != "" || ready.Protocol != bridgeProtocol || len(ready.Volumes) == 0 || len(ready.Volumes) > localmounts.Limit {
			continue
		}
		used := false
		for _, name := range ready.Volumes {
			prefix := "aide-dir-" + rootID + "-" + ticket[:16] + "-"
			if !strings.HasPrefix(name, prefix) {
				return errors.New("native directory volume receipt mismatch")
			}
			out, e := command(ctx, docker, "ps", "-q", "--filter", "volume="+name)
			if e != nil {
				return e
			}
			if strings.TrimSpace(string(out)) != "" {
				used = true
				break
			}
		}
		if !used {
			if e = os.WriteFile(filepath.Join(base, ticket, "stop"), []byte("stop"), 0600); e != nil {
				return e
			}
		}
	}
	return nil
}
