package server

import (
	"encoding/json"
	"os"
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
