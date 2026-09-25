package server

// ── 完整性校验与自愈 ────────────────────────────────────────────────────────────
// 权威方案：proposals/data-integrity/data-layering-and-self-healing.html（③ 完整性校验 / ④ 自动恢复）
//
// 三类时机：
//   ① 构建期：BuildBaseline 对程序二进制计算 SHA-256，写入 .integrity/baseline.json。
//   ② 启动校验：VerifyIntegrity 比程序哈希、查目录结构、验关键文件可解析；SelfHeal 自动恢复。
//   ③ 运行巡检：RunPeriodicIntegrity 每 5 分钟轻量复查一次，结果回写 healthz。
//
// 自愈安全边界：只对“有模板/有备份/可再生”的内容自动恢复；用户数据（会话/记忆/小蜜历史）
// 一旦异常移入 .quarantine 隔离，绝不自动删除。程序篡改只告警标 degraded，提示重新部署。

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// IntegrityStatus 完整性状态机（与方案 healthz 输出对齐）。
type IntegrityStatus string

const (
	IntegrityOK         IntegrityStatus = "ok"         // 一切正常
	IntegrityDegraded    IntegrityStatus = "degraded"    // 可自愈/降级运行（程序疑似篡改、re-wrap 失败等）
	IntegrityCorrupted   IntegrityStatus = "corrupted"  // 严重损坏，需人工介入
)

// IntegrityCheck 单项校验结果。
type IntegrityCheck struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

// IntegrityReport 一次完整性校验/自愈的完整报告。
type IntegrityReport struct {
	Status     IntegrityStatus   `json:"status"`
	Checks     []IntegrityCheck  `json:"checks"`
	Quarantined []string         `json:"quarantined,omitempty"` // 移入隔离区的用户数据
	Healed     []string          `json:"healed,omitempty"`        // 已自动恢复的项
	Errors     []string          `json:"errors,omitempty"`       // 未解决的错误
	CheckedAt  string            `json:"checkedAt"`
}

// baselineManifest 程序层基线清单（首次启动生成，随运行期比对）。
type baselineManifest struct {
	Version       string `json:"version"`
	Commit        string `json:"commit"`
	BinarySHA256  string `json:"binarySha256"`
	BuiltAt       string `json:"builtAt"`
}

// BuildBaseline 首次启动生成程序层基线：对运行二进制计算 SHA-256 写入 .integrity/baseline.json。
// 已存在则不覆盖（基线是本卷首次启动时的程序身份；升级后由部署流程重建）。
func BuildBaseline(data string) error {
	if err := EnsureDirs(data); err != nil {
		return err
	}
	p := baselinePath(data)
	if _, err := os.Stat(p); err == nil {
		return nil // 已有基线，不覆盖
	}
	sum, err := currentBinarySHA256()
	if err != nil {
		return err
	}
	m := baselineManifest{
		Version:      buildVersion,
		Commit:       buildCommit,
		BinarySHA256: sum,
		BuiltAt:      time.Now().UTC().Format(time.RFC3339),
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0600)
}

// currentBinarySHA256 计算当前运行二进制的 SHA-256。
func currentBinarySHA256() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(exe)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// VerifyIntegrity 启动/巡检校验：程序哈希、目录结构、关键文件可解析。只读，不改盘。
func VerifyIntegrity(data string) IntegrityReport {
	rep := IntegrityReport{CheckedAt: time.Now().UTC().Format(time.RFC3339Nano)}

	// ① 程序层哈希比对
	if b, err := os.ReadFile(baselinePath(data)); err == nil {
		var bl baselineManifest
		if json.Unmarshal(b, &bl) == nil && bl.BinarySHA256 != "" {
			cur, err := currentBinarySHA256()
			if err != nil {
				rep.Checks = append(rep.Checks, IntegrityCheck{Name: "binary", OK: false, Detail: err.Error()})
			} else if cur != bl.BinarySHA256 {
				// 程序被篡改/替换：只告警标 degraded，不自动修复（提示重新部署镜像）
				rep.Checks = append(rep.Checks, IntegrityCheck{Name: "binary", OK: false, Detail: "程序二进制与基线不一致（疑似篡改/换版）"})
			} else {
				rep.Checks = append(rep.Checks, IntegrityCheck{Name: "binary", OK: true})
			}
		}
	} else {
		rep.Checks = append(rep.Checks, IntegrityCheck{Name: "baseline", OK: false, Detail: "无基线（将在首次自愈后生成）"})
	}

	// ② 目录结构：全部分层目录应存在
	missing := []string{}
	for _, d := range layeredDirs(data) {
		if st, err := os.Stat(d); err != nil || !st.IsDir() {
			missing = append(missing, d)
		}
	}
	rep.Checks = append(rep.Checks, IntegrityCheck{Name: "dirs", OK: len(missing) == 0, Detail: joinMissing(missing)})

	// ③ 关键文件可解析性（存在但损坏才算问题；缺失视为正常——首次安装无数据）
	corrupt := []string{}
	if b, err := os.ReadFile(SettingsPath(data)); err == nil {
		var s map[string]any
		if json.Unmarshal(b, &s) != nil {
			corrupt = append(corrupt, SettingsPath(data))
		}
	} else if !os.IsNotExist(err) {
		rep.Errors = append(rep.Errors, err.Error())
	}
	// 会话文件可逐条解析（坏文件只记录，不阻断）
	for _, dir := range sessionBucketDirs(data) {
		ents, _ := filepath.Glob(filepath.Join(dir, "session-*.json"))
		for _, f := range ents {
			b, err := os.ReadFile(f)
			if err != nil {
				corrupt = append(corrupt, f)
				continue
			}
			var s Session
			if json.Unmarshal(b, &s) != nil {
				corrupt = append(corrupt, f)
			}
		}
	}
	rep.Checks = append(rep.Checks, IntegrityCheck{Name: "keyfiles", OK: len(corrupt) == 0, Detail: joinMissing(corrupt)})
	rep.Errors = append(rep.Errors, corrupt...)

	rep.Status = summarizeStatus(rep)
	return rep
}

// SelfHeal 根据校验报告自动恢复：
//   - 缺失目录 → EnsureDirs 重建
//   - 损坏关键文件 → 移入 .quarantine（用户数据不自动删）
//   - 程序篡改 → 不修复，仅保持 degraded
//   - 基线缺失 → 重新生成
// 返回更新后的报告（补充 healed/quarantined）。
func SelfHeal(data string, rep IntegrityReport) IntegrityReport {
	// 缺失目录重建
	if err := EnsureDirs(data); err != nil {
		rep.Errors = append(rep.Errors, "重建目录失败: "+err.Error())
	} else {
		for _, c := range rep.Checks {
			if c.Name == "dirs" && !c.OK {
				rep.Healed = append(rep.Healed, "missing-dirs-rebuilt")
			}
		}
	}

	// 基线缺失 → 重新生成
	if _, err := os.Stat(baselinePath(data)); os.IsNotExist(err) {
		if err := BuildBaseline(data); err == nil {
			rep.Healed = append(rep.Healed, "baseline-rebuilt")
		}
	}

	// 损坏关键文件 → 移入隔离区（不自动删除）
	for _, f := range rep.Errors {
		if st, err := os.Stat(f); err != nil || st.IsDir() {
			continue
		}
		dst := filepath.Join(QuarantineDir(data), filepath.Base(f)+".corrupted."+time.Now().UTC().Format("20060102T150405"))
		if err := os.Rename(f, dst); err == nil {
			rep.Quarantined = append(rep.Quarantined, filepath.Base(f))
			logRecovery(data, "quarantine", f, "-> "+dst)
		}
	}
	// 去重排序
	rep.Quarantined = uniqSorted(rep.Quarantined)
	rep.Healed = uniqSorted(rep.Healed)

	rep.Status = summarizeStatus(rep)
	return rep
}

// summarizeStatus 汇总单项结果为整体状态：
// 有未解决错误（且非程序告警）→ corrupted；程序/可降级项 → degraded；否则 ok。
func summarizeStatus(rep IntegrityReport) IntegrityStatus {
	hasFatal := false
	for _, c := range rep.Checks {
		if c.OK {
			continue
		}
		switch c.Name {
		case "binary":
			// 程序篡改：degraded，不判 fatal
		case "baseline":
			// 无基线：可自愈（重建），不算 fatal
		default:
			// dirs / keyfiles 等：若仍存在未处理的损坏文件则 fatal
			if c.Name == "keyfiles" && len(rep.Quarantined) == 0 && len(rep.Errors) > 0 {
				hasFatal = true
			}
		}
	}
	switch {
	case hasFatal:
		return IntegrityCorrupted
	case len(rep.Quarantined) > 0 || hasDegradedCheck(rep):
		return IntegrityDegraded
	default:
		return IntegrityOK
	}
}

func hasDegradedCheck(rep IntegrityReport) bool {
	for _, c := range rep.Checks {
		if !c.OK && c.Name == "binary" {
			return true
		}
	}
	return false
}

// RunPeriodicIntegrity 运行时周期巡检（goroutine）。每 interval 复查一次并回调 onUpdate。
// ctx 取消时退出（由 App.Close 触发）。interval<=0 时默认 5 分钟。
func RunPeriodicIntegrity(ctx <-chan struct{}, data string, interval time.Duration, onUpdate func(IntegrityReport)) {
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx:
			return
		case <-t.C:
			rep := SelfHeal(data, VerifyIntegrity(data))
			if onUpdate != nil {
				onUpdate(rep)
			}
		}
	}
}

// ── 小工具 ────────────────────────────────────────────────────────────────────

func joinMissing(list []string) string {
	if len(list) == 0 {
		return ""
	}
	out := ""
	for i, s := range list {
		if i > 0 {
			out += ", "
		}
		out += filepath.Base(s)
	}
	return out
}

func uniqSorted(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// logRecovery 追加一条完整性恢复日志到 .integrity/recovery.log。
func logRecovery(data, kind, src, detail string) {
	f, err := os.OpenFile(recoveryLogPath(data), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	defer f.Close()
	entry, _ := json.Marshal(struct {
		Time   string `json:"time"`
		Kind   string `json:"kind"`
		Src    string `json:"src"`
		Detail string `json:"detail"`
	}{Time: time.Now().UTC().Format(time.RFC3339Nano), Kind: kind, Src: src, Detail: detail})
	f.Write(append(entry, '\n'))
	log.Printf("integrity[%s] %s %s", kind, src, detail)
}
