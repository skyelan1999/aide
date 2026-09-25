package server

// ── 一次性平铺 → 分层迁移 ──────────────────────────────────────────────────────
// 权威方案：proposals/data-integrity/data-layering-and-self-healing.html（④ 自动恢复与迁移）
//
// 流程（严格 复制 → 校验 → 再动原文件，全程可重入、可回滚）：
//   1. DetectLayout：当前是 flat（旧平铺）/ layered（已分层）/ empty（空卷）。
//   2. 完整备份 /data 下将被迁移的文件到 .integrity/migration-backup-<ts>/。
//   3. EnsureDirs() 建立分层骨架。
//   4. 按归属把平铺文件【复制】到新位置（不删原文件）。
//   5. 校验：逐文件 SHA-256 一致；会话文件可逐条 JSON 解析。
//   6. 校验通过：把原平铺文件【移入】.quarantine/migrated-<ts>/（保留一个版本周期，不直接删）。
//   7. 任一不一致：中止，清理本次复制出的分层副本，原文件纹丝不动（备份仍在）。
//
// 幂等：已分层直接跳过；部分迁移（上次中断）从断点续作——分层目标已存在且哈希一致则视为已复制。
// 写 .integrity/migration-state.json 记录结果。

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// LayoutKind 数据目录布局形态。
type LayoutKind string

const (
	LayoutFlat    LayoutKind = "flat"    // 旧平铺：关键文件直接散落在 /data 根
	LayoutLayered LayoutKind = "layered" // 新分层：已有 auth/ config/ sessions/ 等
	LayoutEmpty   LayoutKind = "empty"   // 空卷或空目录，无既有数据
)

// flatFileMap 平铺根文件名 → 分层目标解析函数。
// 仅收录“当前版本确实在 /data 根下产生”的文件；sources.json 在 work 缓存目录，不在此列。
func flatFileMap(data string) map[string]string {
	return map[string]string{
		AccessTokenFileName:  AccessTokenPath(data),
		KdfSaltFileName:      KdfSaltPath(data),
		WebAuthnCredsFile:    WebAuthnCredsPath(data),
		SettingsFileName:     SettingsPath(data),
		VoiceHistoryFileName: VoiceHistoryPath(data),
		VoiceMemoryFileName:  VoiceMemoryPath(data),
		DebugAuditFileName:   DebugAuditPath(data),
		TokenStatsFileName:   TokenStatsPath(data),
		TokenPricingFileName: TokenPricingPath(data),
		SourcesSecretsFile:   SourcesSecretsPath(data),
		WorkspaceSecretsFile: WorkspaceSecretsPath(data),
	}
}

// DetectLayout 判断 /data 当前布局。
// 判定依据：根下是否存在任一已知平铺文件名 → flat；否则若已出现分层目录 → layered；否则 empty。
// 隐藏目录（.integrity/ .quarantine/）与备份产物不影响判定。
func DetectLayout(data string) (LayoutKind, error) {
	entries, err := os.ReadDir(data)
	if errors.Is(err, os.ErrNotExist) {
		return LayoutEmpty, nil
	}
	if err != nil {
		return "", err
	}
	// 根下是否有已知平铺文件
	known := map[string]bool{}
	for name := range flatFileMap(data) {
		known[name] = true
	}
	hasFlat := false
	hasLayeredDir := false
	hasAny := false
	for _, e := range entries {
		name := e.Name()
		if name == "" || name[0] == '.' { // 忽略隐藏目录（.integrity/.quarantine/...）
			continue
		}
		hasAny = true
		if e.IsDir() {
			switch name {
			case authDirName, configDirName, sessionsDirName, assistantDirName,
				memoryDirName, statsDirName, auditDirName, secretsDirName, certsDirName:
				hasLayeredDir = true
			}
			continue
		}
		if known[name] {
			hasFlat = true
		}
		if matched, _ := filepath.Match("session-*.json", name); matched {
			hasFlat = true
		}
	}
	if hasFlat {
		return LayoutFlat, nil
	}
	if hasLayeredDir || hasAny {
		// 没有平铺文件、但有分层目录或其他内容 → 视为分层（新安装已建骨架）
		return LayoutLayered, nil
	}
	return LayoutEmpty, nil
}

// migrationState 迁移结果标记。
type migrationState struct {
	Stage         string   `json:"stage"` // done | rolled-back
	MigratedAt    string   `json:"migratedAt,omitempty"`
	Version       string   `json:"version,omitempty"`
	Files         []string `json:"files,omitempty"`
	BackupDir     string   `json:"backupDir,omitempty"`
	QuarantineDir string   `json:"quarantineDir,omitempty"`
}

// MigrateFlatToLayered 把平铺 /data 一次性迁移到分层布局。幂等、可重入、可回滚。
func MigrateFlatToLayered(data string) error {
	layout, err := DetectLayout(data)
	if err != nil {
		return err
	}
	if layout != LayoutFlat {
		return nil // 已分层 / 空卷：跳过
	}
	if err := EnsureDirs(data); err != nil {
		return fmt.Errorf("迁移：建立分层目录失败: %w", err)
	}

	ts := time.Now().UTC().Format("20060102T150405")
	backupDir := filepath.Join(IntegrityDir(data), "migration-backup-"+ts)
	quarantineMigrated := filepath.Join(QuarantineDir(data), "migrated-"+ts)
	if err := os.MkdirAll(backupDir, 0700); err != nil {
		return err
	}

	var plan []copied

	// ── 阶段 A：普通文件复制 + 逐文件校验 ──
	for name, dest := range flatFileMap(data) {
		src := filepath.Join(data, name)
		if _, err := os.Stat(src); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue // 该文件不存在，无需迁移
			}
			return err
		}
		ok, err := copyVerified(src, dest, backupDir, name)
		if err != nil {
			rollbackCopies(plan)
			return fmt.Errorf("迁移 %s: %w", name, err)
		}
		if ok {
			plan = append(plan, copied{src, dest})
		}
	}

	// ── 阶段 B：会话文件复制 + 可逐条解析校验 ──
	sessionEntries, err := filepath.Glob(filepath.Join(data, "session-*.json"))
	if err != nil {
		rollbackCopies(plan)
		return err
	}
	for _, src := range sessionEntries {
		name := filepath.Base(src)
		dest := SessionPath(data, sessionIDFromName(name), "active")
		// 会话额外校验：先确认可逐条 JSON 解析，再复制。损坏会话不强行迁入分层区，
		// 留在原平铺位置（下次启动仍按坏文件跳过/隔离处理），不丢数据。
		if b, err := os.ReadFile(src); err == nil {
			var s Session
			if json.Unmarshal(b, &s) != nil {
				log.Printf("迁移：会话 %s 已损坏，保留在平铺根待隔离，未移入分层区", name)
				continue
			}
		}
		ok, err := copyVerified(src, dest, backupDir, name)
		if err != nil {
			rollbackCopies(plan)
			return fmt.Errorf("迁移会话 %s: %w", name, err)
		}
		if ok {
			plan = append(plan, copied{src, dest})
		}
	}

	// ── 阶段 C：全部校验通过 → 原文件移入隔离区（不直接删）──
	if err := os.MkdirAll(quarantineMigrated, 0700); err != nil {
		rollbackCopies(plan)
		return err
	}
	moved := []string{}
	for _, c := range plan {
		if err := moveToQuarantine(c.src, filepath.Join(quarantineMigrated, filepath.Base(c.src))); err != nil {
			rollbackCopies(plan)
			return fmt.Errorf("迁移：隔离原文件 %s 失败: %w", filepath.Base(c.src), err)
		}
		moved = append(moved, filepath.Base(c.src))
	}
	sort.Strings(moved)

	st := migrationState{
		Stage:         "done",
		MigratedAt:    time.Now().UTC().Format(time.RFC3339),
		Version:       buildVersion,
		Files:         moved,
		BackupDir:     backupDir,
		QuarantineDir: quarantineMigrated,
	}
	if b, err := json.MarshalIndent(st, "", "  "); err == nil {
		_ = os.WriteFile(migrationStatePath(data), b, 0600)
	}
	log.Printf("数据分层迁移完成：%d 个文件已归位（原件移入 %s，备份 %s）", len(moved), quarantineMigrated, backupDir)
	return nil
}

// copyVerified 把 src 复制到 dest，并：
//  1. 先备份到 backupDir/<name>；
//  2. 逐字节 SHA-256 校验 src 与 dest 一致；
//
// 返回 (didCopy, error)：didCopy=false 表示 dest 已存在且哈希一致（断点续作，无需再动）。
// dest 的父目录由 EnsureDirs 保证存在。
func copyVerified(src, dest, backupDir, name string) (bool, error) {
	srcHash, err := sha256File(src)
	if err != nil {
		return false, err
	}
	// 备份（若尚未备份）
	backupPath := filepath.Join(backupDir, name)
	if _, err := os.Stat(backupPath); errors.Is(err, os.ErrNotExist) {
		if err := copyFile(src, backupPath, 0600); err != nil {
			return false, err
		}
	}
	// dest 已存在：哈希一致 → 跳过（幂等）；不一致 → 冲突中止
	if b, err := os.ReadFile(dest); err == nil {
		destHash := sha256.Sum256(b)
		if hex.EncodeToString(destHash[:]) == srcHash {
			return false, nil
		}
		return false, fmt.Errorf("目标 %s 已存在但内容不一致", dest)
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if err := copyFile(src, dest, 0600); err != nil {
		return false, err
	}
	b, err := os.ReadFile(dest)
	if err != nil {
		return false, err
	}
	destHash := sha256.Sum256(b)
	if hex.EncodeToString(destHash[:]) != srcHash {
		return false, fmt.Errorf("复制后哈希不一致 %s", dest)
	}
	return true, nil
}

// copied 记录一次已校验的分层复制，供中止时回滚。包级类型（迁移函数与 rollbackCopies 共用）。
type copied struct{ src, dest string }

// rollbackCopies 中止时清理本次复制出的分层副本（best-effort）。原平铺文件从未被移动，故无需恢复。
func rollbackCopies(plan []copied) {
	for _, c := range plan {
		_ = os.Remove(c.dest)
	}
}

// moveToQuarantine 把已校验通过的原平铺文件移入隔离区（同卷 rename；失败回退复制+删除）。
func moveToQuarantine(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.WriteFile(dst, b, 0600); err != nil {
		return err
	}
	return os.Remove(src)
}

// sessionIDFromName 从 "session-<id>.json" 提取 <id>。
func sessionIDFromName(name string) string {
	base := name
	base = strings.TrimPrefix(base, "session-")
	base = strings.TrimSuffix(base, ".json")
	return base
}

// sha256File 返回文件内容的 SHA-256 十六进制摘要。
func sha256File(p string) (string, error) {
	b, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// copyFile 复制文件内容并设置权限。
func copyFile(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// migrateLegacyTLSDir 把旧 #29/B 写入 data/tls/ 的 TLS 证书迁到 #31 规范的 data/certs/。
// 保持同一张证书（避免浏览器重新告警）；certs/ 已有证书则跳过不覆盖。
// 迁完删除空的旧 tls/。幂等：无旧目录、目标已就绪或文件不完整均为 no-op。
func migrateLegacyTLSDir(data string) error {
	legacy := filepath.Join(data, "tls")
	dstCert := TLSCertPath(data)
	dstKey := TLSKeyPath(data)

	// 目标已就绪（两文件都在）→ 不覆盖
	if _, err := os.Stat(dstCert); err == nil {
		if _, err := os.Stat(dstKey); err == nil {
			return nil
		}
	}
	srcCert := filepath.Join(legacy, CertFileName)
	srcKey := filepath.Join(legacy, KeyFileName)
	// 旧证书缺失 → 无迁移对象；任一半缺失 → 不动，交给 ensureTLSCert 重新生成
	if _, err := os.Stat(srcCert); err != nil {
		return nil
	}
	if _, err := os.Stat(srcKey); err != nil {
		return nil
	}
	if err := os.MkdirAll(CertsDir(data), 0700); err != nil {
		return err
	}
	// 复制并逐字节 SHA-256 校验，再按目标权限落盘（cert 0644 / key 0600）。
	certHash, err := sha256File(srcCert)
	if err != nil {
		return err
	}
	if err := copyFile(srcCert, dstCert, 0644); err != nil {
		return err
	}
	if h, err := sha256File(dstCert); err != nil || h != certHash {
		return fmt.Errorf("迁移证书 %s 校验失败", CertFileName)
	}
	keyHash, err := sha256File(srcKey)
	if err != nil {
		return err
	}
	if err := copyFile(srcKey, dstKey, 0600); err != nil {
		return err
	}
	if h, err := sha256File(dstKey); err != nil || h != keyHash {
		return fmt.Errorf("迁移私钥 %s 校验失败", KeyFileName)
	}
	// 校验通过 → 删除旧文件；目录变空则一并删除（best-effort）。
	_ = os.Remove(srcCert)
	_ = os.Remove(srcKey)
	_ = os.Remove(legacy) // 仅当为空才成功
	log.Printf("TLS 证书已从旧 %s/ 迁移到 %s/（保留同一张证书）", legacy, CertsDir(data))
	return nil
}
