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
)

// configBackup 配置备份信封。Settings 为完整设置快照；敏感字段按 IncludeSecrets 决定是否随附；
// VoiceHistory 为小秘历史原始文件（可能是加密信封）。
type configBackup struct {
	Format           string          `json:"format"`
	FormatVersion    int             `json:"formatVersion"`
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
		ExportedAt:       time.Now().UTC().Format(time.RFC3339Nano),
		AppVersion:       version,
		IncludeSecrets:   in.IncludeSecrets,
		IncludeVoiceData: in.IncludeVoiceData,
		Settings:         sb,
	}
	if in.IncludeSecrets {
		if b, e := os.ReadFile(filepath.Join(a.dataPath, sourcesSecretsFN)); e == nil {
			bk.SourcesSecrets = b
		}
		if b, e := os.ReadFile(filepath.Join(a.dataPath, wsSecretsFile)); e == nil {
			bk.WorkspaceSecrets = b
		}
	}
	if in.IncludeVoiceData {
		if b, e := os.ReadFile(filepath.Join(a.dataPath, "voice-history.json")); e == nil {
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
	var imp Settings
	if err := json.Unmarshal(bk.Settings, &imp); err != nil {
		fail(w, 400, errors.New("备份中的设置解析失败："+err.Error()))
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	// 回滚点：导入前把当前完整设置另存，便于手动恢复。
	if rb, e := json.MarshalIndent(a.settings, "", "  "); e == nil {
		_ = os.WriteFile(filepath.Join(a.dataPath, "settings.json.pre-import"), rb, 0600)
	}

	// 非敏感设置：完整采用备份快照（零值也是有效状态，如锁屏时间 0=不锁屏）。
	merged := imp
	// 敏感字段：默认保留当前值，仅在用户允许且备份确实随附时采用备份值。
	merged.APIKey = a.settings.APIKey
	merged.UserPasswordHash = a.settings.UserPasswordHash
	merged.PersonaCiphers = a.settings.PersonaCiphers
	merged.PersonaCipher = a.settings.PersonaCipher
	passwordChanged := false
	if req.ImportSecrets && bk.IncludeSecrets {
		merged.APIKey = imp.APIKey
		if imp.UserPasswordHash != "" {
			merged.UserPasswordHash = imp.UserPasswordHash
			passwordChanged = true
		}
		merged.PersonaCiphers = imp.PersonaCiphers
		merged.PersonaCipher = imp.PersonaCipher
		if len(bk.SourcesSecrets) != 0 {
			_ = os.WriteFile(filepath.Join(a.dataPath, sourcesSecretsFN), bk.SourcesSecrets, 0600)
		}
		if len(bk.WorkspaceSecrets) != 0 {
			_ = os.WriteFile(filepath.Join(a.dataPath, wsSecretsFile), bk.WorkspaceSecrets, 0600)
		}
	}

	a.settings = merged
	if err := atomicJSON(filepath.Join(a.dataPath, "settings.json"), merged); err != nil {
		fail(w, 500, err)
		return
	}

	voiceImported := false
	if req.ImportVoice && bk.IncludeVoiceData && len(bk.VoiceHistory) != 0 {
		if e := os.WriteFile(filepath.Join(a.dataPath, "voice-history.json"), bk.VoiceHistory, 0600); e == nil {
			voiceImported = true
			a.voiceAgent = newVoiceAgent(a.dataPath) // 重新从文件加载小秘历史
		}
	}

	jsonOut(w, 200, map[string]any{
		"ok": true, "passwordChanged": passwordChanged,
		"voiceImported": voiceImported,
		"exportedAt": bk.ExportedAt, "appVersion": bk.AppVersion,
	})
}
