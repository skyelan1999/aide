package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// This file is produced by the Docker build, outside the persistent data volume.
// No request parameter, environment variable or file in /data selects its path.
const imageBinaryChecksum = "/usr/local/bin/aide.sha256"

var integrityCommitPattern = regexp.MustCompile(`^[a-f0-9]{40}$`)
var integrityDigestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

type integrityUpgrade struct {
	previous []byte
	next     baselineManifest
	checksum string
}

func readImageBinaryChecksum(path string) (string, error) {
	st, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !st.Mode().IsRegular() || !integrityRootOwned(st) || st.Mode().Perm()&0022 != 0 || st.Size() > 256 {
		return "", fmt.Errorf("unsafe image checksum file")
	}
	b, err := os.ReadFile(path)
	fields := strings.Fields(string(b))
	if err != nil || len(fields) != 2 || !integrityDigestPattern.MatchString(fields[0]) || fields[1] != "/usr/local/bin/aide" {
		return "", fmt.Errorf("invalid image binary checksum")
	}
	return fields[0], nil
}

func onlyBinaryMismatch(rep IntegrityReport) bool {
	if rep.Status != IntegrityDegraded || len(rep.Errors) != 0 || len(rep.Quarantined) != 0 {
		return false
	}
	badBinary := false
	for _, c := range rep.Checks {
		if !c.OK {
			if c.Name != "binary" {
				return false
			}
			badBinary = true
		}
	}
	return badBinary
}

func prepareIntegrityUpgrade(data, checksum, version, commit, digest string, rep IntegrityReport) *integrityUpgrade {
	if !onlyBinaryMismatch(rep) || version == "" || version == "dev" || !integrityCommitPattern.MatchString(commit) {
		return nil
	}
	trusted, err := readImageBinaryChecksum(checksum)
	if err != nil || trusted != digest {
		return nil
	}
	st, err := os.Lstat(baselinePath(data))
	if err != nil || !st.Mode().IsRegular() || st.Size() > 16384 {
		return nil
	}
	previous, err := os.ReadFile(baselinePath(data))
	var old baselineManifest
	if err != nil || json.Unmarshal(previous, &old) != nil || !integrityDigestPattern.MatchString(old.BinarySHA256) || old.Version == "" || old.Commit == "" || old.BinarySHA256 == digest {
		return nil
	}
	// A different binary claiming exactly the same build identity is not an upgrade.
	// Older verified image identities remain eligible, allowing normal slot rollback.
	if old.Version == version && old.Commit == commit {
		return nil
	}
	return &integrityUpgrade{previous: previous, checksum: checksum, next: baselineManifest{Version: version, Commit: commit, BinarySHA256: digest}}
}

func (a *App) prepareImageIntegrityUpgrade() {
	exe, err := os.Executable()
	if err != nil || exe != "/usr/local/bin/aide" {
		return // Unpackaged executables have no trusted image identity.
	}
	digest, err := currentBinarySHA256()
	if err == nil {
		a.integrityUpgrade = prepareIntegrityUpgrade(a.dataPath, imageBinaryChecksum, buildVersion, buildCommit, digest, a.integrityRep)
	}
}

func (u *integrityUpgrade) commit(data string) error {
	previous, err := os.ReadFile(baselinePath(data))
	if err != nil || !bytes.Equal(previous, u.previous) {
		return fmt.Errorf("baseline changed since startup")
	}
	trusted, err := readImageBinaryChecksum(u.checksum)
	if err != nil || trusted != u.next.BinarySHA256 {
		return fmt.Errorf("image checksum changed since startup")
	}
	digest, err := currentBinarySHA256()
	if err != nil || digest != trusted || !onlyBinaryMismatch(VerifyIntegrity(data)) {
		return fmt.Errorf("program or data integrity changed before readiness")
	}
	// Stable content-addressed backups are never overwritten. Failure to back up
	// leaves the current baseline intact and the warning in place.
	sum := sha256.Sum256(previous)
	backup := filepath.Join(IntegrityDir(data), "baseline-before-"+hex.EncodeToString(sum[:])+".json")
	f, err := os.OpenFile(backup, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if os.IsExist(err) {
		st, statErr := os.Lstat(backup)
		if statErr != nil || !st.Mode().IsRegular() {
			return fmt.Errorf("unsafe baseline backup")
		}
		b, readErr := os.ReadFile(backup)
		if readErr != nil || !bytes.Equal(b, previous) {
			return fmt.Errorf("baseline backup conflict")
		}
	} else if err != nil {
		return err
	} else {
		_, err = f.Write(previous)
		if err == nil {
			err = f.Sync()
		}
		closeErr := f.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	u.next.BuiltAt = time.Now().UTC().Format(time.RFC3339Nano)
	return atomicJSON(baselinePath(data), u.next)
}

// Called only by the ready application's health handler, never by startup
// self-healing or the periodic checker. A candidate is attempted once per boot.
func (a *App) confirmImageIntegrityUpgrade() {
	a.integrityMu.Lock()
	defer a.integrityMu.Unlock()
	u := a.integrityUpgrade
	if u == nil {
		return
	}
	a.integrityUpgrade = nil
	if err := u.commit(a.dataPath); err != nil {
		log.Printf("integrity: upgrade baseline retained: %v", err)
	} else {
		log.Printf("integrity: verified ready build baseline updated (%s, %s)", u.next.Version, u.next.Commit)
	}
	a.integrityRep = VerifyIntegrity(a.dataPath)
}

// Serialize periodic reads with the readiness transition, so an older report
// cannot overwrite the freshly confirmed result after a baseline rename.
func (a *App) runIntegrityChecks() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-a.bgCtx.Done():
			return
		case <-ticker.C:
			a.integrityMu.Lock()
			a.integrityRep = SelfHeal(a.dataPath, VerifyIntegrity(a.dataPath))
			a.integrityMu.Unlock()
		}
	}
}
