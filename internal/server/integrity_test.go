package server

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestIntegrityBaseline 生成基线 → 校验通过（二进制一致、目录齐全）。
func TestIntegrityBaseline(t *testing.T) {
	data := t.TempDir()
	if err := EnsureDirs(data); err != nil {
		t.Fatal(err)
	}
	if err := BuildBaseline(data); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(baselinePath(data)); err != nil {
		t.Fatalf("基线未生成: %v", err)
	}
	rep := SelfHeal(data, VerifyIntegrity(data))
	if rep.Status == IntegrityCorrupted {
		t.Fatalf("基线校验应通过: %+v", rep)
	}
	// binary 检查应为 ok
	for _, c := range rep.Checks {
		if c.Name == "binary" && !c.OK {
			t.Errorf("binary 检查失败: %+v", c)
		}
	}
}

// TestIntegrityMissingDir 缺失目录 → 自愈重建。
func TestIntegrityMissingDir(t *testing.T) {
	data := t.TempDir()
	EnsureDirs(data)
	BuildBaseline(data)
	// 删掉一个必需目录
	if err := os.RemoveAll(SecretsDir(data)); err != nil {
		t.Fatal(err)
	}
	rep := SelfHeal(data, VerifyIntegrity(data))
	// 自愈后目录应被重建
	if st, err := os.Stat(SecretsDir(data)); err != nil || !st.IsDir() {
		t.Errorf("缺失目录未被自愈重建: %v", err)
	}
	found := false
	for _, h := range rep.Healed {
		if strings.Contains(h, "missing-dirs") {
			found = true
		}
	}
	if !found {
		t.Logf("healed=%v（目录已重建即可）", rep.Healed)
	}
}

// TestIntegrityCorruptedFile 损坏关键文件 → 移入 .quarantine 隔离，不删除。
func TestIntegrityCorruptedFile(t *testing.T) {
	data := t.TempDir()
	EnsureDirs(data)
	BuildBaseline(data)
	// 写入损坏的 settings.json
	if err := os.WriteFile(SettingsPath(data), []byte(`{{{broken`), 0600); err != nil {
		t.Fatal(err)
	}
	rep := SelfHeal(data, VerifyIntegrity(data))
	// 损坏文件应被隔离
	if len(rep.Quarantined) == 0 {
		t.Fatalf("损坏文件未隔离: %+v", rep)
	}
	// 原位置已无损坏文件
	if _, err := os.Stat(SettingsPath(data)); !os.IsNotExist(err) {
		t.Error("损坏文件未从原位置移走")
	}
	// 隔离区存在
	ents, _ := os.ReadDir(QuarantineDir(data))
	if len(ents) == 0 {
		t.Error("隔离区为空")
	}
}

// TestIntegrityTamperedBinary 程序二进制与基线不一致 → degraded 告警（不自动修复）。
func TestIntegrityTamperedBinary(t *testing.T) {
	data := t.TempDir()
	EnsureDirs(data)
	if err := BuildBaseline(data); err != nil {
		t.Fatal(err)
	}
	// 篡改基线：把记录的哈希改成错误值，模拟二进制被替换
	b, _ := os.ReadFile(baselinePath(data))
	var bl baselineManifest
	json.Unmarshal(b, &bl)
	bl.BinarySHA256 = strings.Repeat("00", 32)
	tampered, _ := json.MarshalIndent(bl, "", "  ")
	os.WriteFile(baselinePath(data), tampered, 0600)

	rep := VerifyIntegrity(data)
	foundBinaryBad := false
	for _, c := range rep.Checks {
		if c.Name == "binary" && !c.OK {
			foundBinaryBad = true
		}
	}
	if !foundBinaryBad {
		t.Fatalf("应检出二进制篡改: %+v", rep.Checks)
	}
	// 状态应为 degraded（不判 corrupted，提示重新部署）
	if rep.Status != IntegrityDegraded {
		t.Fatalf("篡改应为 degraded，got %s", rep.Status)
	}
}

// TestIntegrityRecoveryLogIsWritten 自愈时写入恢复日志。
func TestIntegrityRecoveryLogIsWritten(t *testing.T) {
	data := t.TempDir()
	EnsureDirs(data)
	BuildBaseline(data)
	os.WriteFile(SettingsPath(data), []byte(`{{{broken`), 0600)
	SelfHeal(data, VerifyIntegrity(data))
	if _, err := os.Stat(recoveryLogPath(data)); err != nil {
		t.Errorf("恢复日志未写: %v", err)
	}
}

func TestIntegrityReadFailurePreservesValidOriginal(t *testing.T) {
	for _, bucket := range []string{"settings", "active", "archived"} {
		t.Run(bucket, func(t *testing.T) {
			data := t.TempDir()
			if err := EnsureDirs(data); err != nil {
				t.Fatal(err)
			}
			if err := BuildBaseline(data); err != nil {
				t.Fatal(err)
			}
			file := SettingsPath(data)
			if bucket != "settings" {
				file = filepath.Join(data, "sessions", bucket, "session-transient.json")
			}
			if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
				t.Fatal(err)
			}
			body := []byte(`{"id":"transient","messages":[],"runs":[]}`)
			if err := os.WriteFile(file, body, 0600); err != nil {
				t.Fatal(err)
			}
			report := verifyIntegrity(data, func(path string) ([]byte, error) {
				if path == file {
					return nil, errors.New("synthetic transient I/O failure")
				}
				return os.ReadFile(path)
			})
			if report.Status != IntegrityDegraded {
				t.Fatalf("read failure must be visible: %+v", report)
			}
			report = SelfHeal(data, report)
			got, err := os.ReadFile(file)
			if err != nil || string(got) != string(body) || len(report.Quarantined) != 0 {
				t.Fatalf("read failure moved original: %v %+v", err, report)
			}
			if next := VerifyIntegrity(data); next.Status != IntegrityOK {
				t.Fatalf("later readable data did not recover: %+v", next)
			}
		})
	}
}

func TestIntegrityStaleCorruptionReportPreservesReplacement(t *testing.T) {
	for _, replacement := range []string{`{"id":"repaired","messages":[],"runs":[]}`, `{"new incomplete write"`} {
		t.Run(hash([]byte(replacement))[:8], func(t *testing.T) {
			data := t.TempDir()
			if err := EnsureDirs(data); err != nil {
				t.Fatal(err)
			}
			if err := BuildBaseline(data); err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(data, "sessions", "archived", "session-replaced.json")
			if err := os.WriteFile(file, []byte(`{{original invalid`), 0600); err != nil {
				t.Fatal(err)
			}
			report := VerifyIntegrity(data)
			if len(report.invalidFiles) != 1 {
				t.Fatal("missing original corruption observation")
			}
			stage := file + ".replacement"
			if err := os.WriteFile(stage, []byte(replacement), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(stage, file); err != nil {
				t.Fatal(err)
			}
			report = SelfHeal(data, report)
			got, err := os.ReadFile(file)
			if err != nil || string(got) != replacement || len(report.Quarantined) != 0 {
				t.Fatalf("stale report moved replacement: %v %+v", err, report)
			}
		})
	}
}

func TestIntegrityUnchangedCorruptArchiveKeepsOriginalBytes(t *testing.T) {
	data := t.TempDir()
	if err := EnsureDirs(data); err != nil {
		t.Fatal(err)
	}
	if err := BuildBaseline(data); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(data, "sessions", "archived", "session-invalid.json")
	body := []byte(`{{stable invalid archive`)
	if err := os.WriteFile(file, body, 0600); err != nil {
		t.Fatal(err)
	}
	report := SelfHeal(data, VerifyIntegrity(data))
	if len(report.Quarantined) != 1 {
		t.Fatalf("genuine corruption not isolated: %+v", report)
	}
	files, err := os.ReadDir(QuarantineDir(data))
	if err != nil || len(files) != 1 {
		t.Fatalf("quarantine: %v %v", files, err)
	}
	got, err := os.ReadFile(filepath.Join(QuarantineDir(data), files[0].Name()))
	if err != nil || string(got) != string(body) {
		t.Fatalf("quarantine lost original bytes: %v", err)
	}
}
