package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const (
	configBackupFormat  = "aide-config-backup"
	configBackupVersion = 1
	// configSettingsVersion 标记导出信封里 settings 的结构版本（semver 主.次.修订）。
	// 导入时据此跑跨版本语义迁移钩子；缺失该字段视为 0.1.11 之前的旧备份（v0）。
	configSettingsVersion = "0.1.11.0"
)

// configBackup 配置备份信封。Settings 为完整设置快照；敏感字段按 IncludeSecrets 决定是否随附；
// VoiceHistory 为小秘历史原始文件（可能是加密信封）。
type configBackup struct {
	Format           string          `json:"format"`
	FormatVersion    int             `json:"formatVersion"`
	SettingsVersion  string          `json:"settingsVersion,omitempty"` // 导出时的设置结构版本，供跨版本迁移判断
	ExportedAt       string          `json:"exportedAt"`
	AppVersion       string          `json:"appVersion"`
	IncludeSecrets   bool            `json:"includeSecrets"`
	IncludeVoiceData bool            `json:"includeVoiceData"`
	Settings         json.RawMessage `json:"settings"`
	SourcesSecrets   json.RawMessage `json:"sourcesSecrets,omitempty"`
	WorkspaceSecrets json.RawMessage `json:"workspaceSecrets,omitempty"`
	VoiceHistory     json.RawMessage `json:"voiceHistory,omitempty"`
}

// exportConfigBackup POST /api/config/export：按选项打包配置并以附件下载。
func (a *App) exportConfigBackup(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IncludeSecrets   bool `json:"includeSecrets"`
		IncludeVoiceData bool `json:"includeVoiceData"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in) // 请求体可空，默认都不包含

	a.mu.Lock()
	exportSettings := a.settings
	version := a.version
	a.mu.Unlock()

	if !in.IncludeSecrets {
		// 剔除敏感字段，得到可较安全分享的设置快照。
		// 除密码哈希外，还需清除：API Key、人格/小秘历史密文、调试令牌哈希等任何可解密封面的材料。
		exportSettings.APIKey = ""
		exportSettings.TTSAPIKey = ""
		exportSettings.UserPasswordHash = ""
		exportSettings.PersonaCiphers = nil
		exportSettings.PersonaCipher = ""
		exportSettings.DebugTokenHash = ""
		// 小秘历史密文仅在显式 IncludeVoiceData 时随附；非敏感导出不带任何密文引用。
	}
	sb, err := json.MarshalIndent(exportSettings, "", "  ")
	if err != nil {
		fail(w, 500, err)
		return
	}
	bk := configBackup{
		Format:           configBackupFormat,
		FormatVersion:    configBackupVersion,
		SettingsVersion:  configSettingsVersion,
		ExportedAt:       time.Now().UTC().Format(time.RFC3339Nano),
		AppVersion:       version,
		IncludeSecrets:   in.IncludeSecrets,
		IncludeVoiceData: in.IncludeVoiceData,
		Settings:         sb,
	}
	if in.IncludeSecrets {
		if b, e := os.ReadFile(SourcesSecretsPath(a.dataPath)); e == nil {
			bk.SourcesSecrets = b
		}
		// #38：工作空间 SSH 凭据随附【加密信封】（/data/secrets/vault.enc），绝不回退成明文。
		// 旧安装（无 vault 文件）回退读旧明文文件，仅作迁移过渡。
		if a.vault != nil {
			if b, e := os.ReadFile(a.vault.Path()); e == nil && len(b) > 0 {
				bk.WorkspaceSecrets = b
			}
		}
		if len(bk.WorkspaceSecrets) == 0 {
			if b, e := os.ReadFile(WorkspaceSecretsPath(a.dataPath)); e == nil {
				bk.WorkspaceSecrets = b
			}
		}
	}
	if in.IncludeVoiceData {
		if b, e := os.ReadFile(VoiceHistoryPath(a.dataPath)); e == nil {
			bk.VoiceHistory = b
		}
	}
	out, err := json.MarshalIndent(bk, "", "  ")
	if err != nil {
		fail(w, 500, err)
		return
	}
	name := "aide-config-" + time.Now().Format("20060102-150405") + ".json"
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(200)
	_, _ = w.Write(out)
}

// importConfigBackup POST /api/config/import：校验备份、留存回滚点、合并设置、按需导入敏感与小秘历史。
func (a *App) importConfigBackup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Backup        json.RawMessage `json:"backup"`
		ImportSecrets bool            `json:"importSecrets"`
		ImportVoice   bool            `json:"importVoice"`
	}
	if err := decode(w, r, &req); err != nil {
		fail(w, 400, err)
		return
	}
	var bk configBackup
	if err := json.Unmarshal(req.Backup, &bk); err != nil {
		fail(w, 400, errors.New("备份文件解析失败："+err.Error()))
		return
	}
	if bk.Format != configBackupFormat {
		fail(w, 400, errors.New("不是有效的 aide 配置备份文件"))
		return
	}
	if bk.FormatVersion > configBackupVersion {
		fail(w, 400, errors.New("该备份来自更新版本的 aide，当前版本无法导入"))
		return
	}
	// 跨版本兼容：以当前版本默认 Settings 打底，再用备份 JSON 覆盖。
	// 旧备份缺失的新字段自动获得当前默认值；备份显式出现的字段（含显式零值）覆盖默认。
	merged := defaultSettings()
	if err := json.Unmarshal(bk.Settings, &merged); err != nil {
		fail(w, 400, errors.New("备份中的设置解析失败："+err.Error()))
		return
	}
	// 语义迁移钩子：只管字段重命名/枚举转换/拆分；缺失字段补默认已由"默认底"自动完成。
	migrateSettings(bk.SettingsVersion, &merged)
	// 旧格式迁移 + 合法性回退 + 模型归一化（与 New() 启动加载同一函数，导入即生效、无需重启）。
	if err := normalizeLoadedSettings(&merged); err != nil {
		fail(w, 400, errors.New("备份设置校验失败："+err.Error()))
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	// 回滚点：导入前把当前完整设置另存，便于手动恢复。
	if rb, e := json.MarshalIndent(a.settings, "", "  "); e == nil {
		_ = os.WriteFile(filepath.Join(ConfigBackupsDir(a.dataPath), "settings.json.pre-import"), rb, 0600)
	}

	// 敏感字段：默认保留当前值（即便是脱敏备份里的空串也不会清空现网密钥），
	// 仅在用户勾选"导入密钥"且备份确实随附敏感数据时才采用备份值。
	// 先暂存备份侧敏感值，再用当前值覆盖。
	bkAPIKey := merged.APIKey
	bkTTSAPIKey := merged.TTSAPIKey
	bkPasswordHash := merged.UserPasswordHash
	bkCiphers := merged.PersonaCiphers
	bkCipher := merged.PersonaCipher
	merged.APIKey = a.settings.APIKey
	merged.TTSAPIKey = a.settings.TTSAPIKey
	merged.UserPasswordHash = a.settings.UserPasswordHash
	merged.PersonaCiphers = a.settings.PersonaCiphers
	merged.PersonaCipher = a.settings.PersonaCipher
	merged.DebugTokenHash = a.settings.DebugTokenHash
	passwordChanged := false
	if req.ImportSecrets && bk.IncludeSecrets {
		merged.APIKey = bkAPIKey
		merged.TTSAPIKey = bkTTSAPIKey
		if bkPasswordHash != "" {
			merged.UserPasswordHash = bkPasswordHash
			passwordChanged = true
		}
		merged.PersonaCiphers = bkCiphers
		merged.PersonaCipher = bkCipher
		if len(bk.SourcesSecrets) != 0 {
			_ = os.WriteFile(SourcesSecretsPath(a.dataPath), bk.SourcesSecrets, 0600)
		}
		// #38：工作空间凭据随附的是加密信封——合并进 vault（保持密文，不回退明文）。
		// 旧备份若是明文 workspaceSecrets JSON，则仍写回旧文件，下次解锁时迁移入 vault。
		if len(bk.WorkspaceSecrets) != 0 {
			if a.vault != nil && IsEnvelope(bk.WorkspaceSecrets) {
				if n, e := a.vault.ImportEnvelope(bk.WorkspaceSecrets); e == nil && n > 0 {
					_ = a.vault.Save()
				}
			} else {
				_ = os.WriteFile(WorkspaceSecretsPath(a.dataPath), bk.WorkspaceSecrets, 0600)
			}
		}
	}

	a.settings = merged
	if err := atomicJSON(SettingsPath(a.dataPath), merged); err != nil {
		fail(w, 500, err)
		return
	}

	voiceImported := false
	if req.ImportVoice && bk.IncludeVoiceData && len(bk.VoiceHistory) != 0 {
		if e := os.WriteFile(VoiceHistoryPath(a.dataPath), bk.VoiceHistory, 0600); e == nil {
			voiceImported = true
			a.voiceAgent = newVoiceAgent(a.dataPath) // 重新从文件加载小秘历史
		}
	}

	jsonOut(w, 200, map[string]any{
		"ok": true, "passwordChanged": passwordChanged,
		"voiceImported": voiceImported,
		"exportedAt":    bk.ExportedAt, "appVersion": bk.AppVersion,
	})
}

// migrateSettings 跨版本设置迁移钩子：按备份导出时的 settingsVersion 逐段处理语义变化
// （字段重命名 / 枚举转换 / 字段拆分）。
//
// 设计边界：
//   - 缺失字段补默认由"默认底 + Unmarshal 覆盖"自动完成，本函数不负责补默认；
//   - 非法数值/枚举回退由 validateSettings 负责，本函数只管语义迁移；
//   - 备份来自更高版本已在信封层拒绝（FormatVersion 校验），这里只处理更早版本。
//
// 未来新增破坏性变更时，在此按版本追加 case 即可，保持可扩展。
func migrateSettings(fromVersion string, merged *Settings) {
	switch fromVersion {
	case "":
		// 无 settingsVersion 标签 = 0.1.11 之前的旧备份（记为 v0）。
		migrateV0ToV1(merged)
		// 未来示例：
		// case "0.1.11.0":
		// 	migrateV1ToV2(merged)
	}
}

// migrateV0ToV1 示例迁移：v0（单人格 personaCipher 字符串）→ v1（personaCiphers 映射）。
// 把旧单人格时代的性格密文播种为 aide 人格的自定义密文，字段形状由 string 拆分为 map。
// （normalizeLoadedSettings 内有同款兜底，此处作为迁移钩子的可测示例，幂等重复执行无副作用。）
func migrateV0ToV1(merged *Settings) {
	if merged.PersonaCipher != "" && merged.PersonaCiphers == nil {
		merged.PersonaCiphers = map[string]string{personaAide: merged.PersonaCipher}
	}
}
