package server

// 少样本音色采集与管理（#36）。
//
// 样本存储布局（/data/assistant/voice-samples/{profile}/）：
//   index.json              样本元数据索引（不含音频本体；明文，但仅本地 0700 目录）
//   <id>.enc                AES-256-GCM 加密音频（nonce||ciphertext，二进制；密钥=小秘账户密码派生）
//
// 安全模型（GDPR Art.9 生物特征）：
//   - 音频本体绝不明文落盘；加密密钥只在内存（a.personaKey），锁屏即失效。
//   - index.json 不存任何可还原声纹的内容；transcript 可选，默认空。
//   - 默认不向第三方云发送；仅"创建音色"时按用户显式动作把样本送自托管克隆服务。
//   - 删除失败明确报告并保留失败项索引；清空支持部分结果与重试。
//
// 与 #29 复用：deriveKey(password) → AES-256-GCM；不新造加密原语。

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// voiceProfileRe / voiceSampleIDRe 白名单：profile 仅允许小写字母数字连字符；
// id 仅允许 hex（newID() 产 32 hex，upload 拼接 64 hex）。拒绝 ../ 与绝对路径越界。
var (
	voiceProfileRe  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	voiceSampleIDRe = regexp.MustCompile(`^[a-f0-9]{16,128}$`)
)

// safeVoiceProfile 规范化 profile：空 → xiaomi；非法值返回错误。
func safeVoiceProfile(profile string) (string, error) {
	profile = strings.TrimSpace(profile)
	if profile == "" {
		return personaXiaomi, nil
	}
	if !voiceProfileRe.MatchString(profile) {
		return "", errors.New("非法 profile：仅允许小写字母/数字/连字符，且不得含路径分隔符")
	}
	return profile, nil
}

// safeVoiceSampleID 校验样本 id 不含路径分隔符或 ..。
func safeVoiceSampleID(id string) error {
	id = strings.TrimSpace(id)
	if !voiceSampleIDRe.MatchString(id) {
		return errors.New("非法样本 id：应为 hex 字符串")
	}
	return nil
}

const (
	voiceSampleMaxBytes       = 15 << 20 // 单个样本上限 15MB（3min opus ≈ 3-5MB，留余量）
	voiceSampleIndex          = "index.json"
	voiceSampleCipherOverhead = 12 + 16 // AES-GCM nonce and tag
	voiceCloneMaxSamples      = 16
	voiceCloneMaxAudioBytes   = 32 << 20
)

// VoiceSampleMeta 一条样本元数据（index.json 内；不含音频本体）。
type VoiceSampleMeta struct {
	ID          string `json:"id"`
	CreatedAt   string `json:"createdAt"`
	Bytes       int    `json:"bytes"`
	MIMEType    string `json:"mimeType"`
	DurationSec int    `json:"durationSec,omitempty"`
	Transcript  string `json:"transcript,omitempty"` // 浏览器 STT 转写（可选，性格推断用）
	Consented   bool   `json:"consented"`            // 录制前已勾选"本人或已授权"
	Note        string `json:"note,omitempty"`
}

type voiceSampleIndexFile struct {
	Profile string            `json:"profile"`
	Items   []VoiceSampleMeta `json:"items"`
}

// voiceSamplesDir 小秘样本目录：/data/assistant/voice-samples/{profile}/
func (a *App) voiceSamplesDir(profile string) string {
	if strings.TrimSpace(profile) == "" {
		profile = personaXiaomi
	}
	return filepath.Join(AssistantDir(a.dataPath), "voice-samples", profile)
}

// voiceSampleIndexPath 索引文件路径。
func (a *App) voiceSampleIndexPath(profile string) string {
	return filepath.Join(a.voiceSamplesDir(profile), voiceSampleIndex)
}

// loadVoiceSampleIndex 读索引；目录/文件缺失返回空索引（不致命）。
func (a *App) loadVoiceSampleIndex(profile string) voiceSampleIndexFile {
	idx, _ := a.readVoiceSampleIndex(profile)
	return idx
}

// readVoiceSampleIndex 保留读取/解析错误；删除不能把不可读索引当作空索引。
func (a *App) readVoiceSampleIndex(profile string) (voiceSampleIndexFile, error) {
	idx := voiceSampleIndexFile{Profile: profile}
	b, err := os.ReadFile(a.voiceSampleIndexPath(profile))
	if errors.Is(err, os.ErrNotExist) {
		return idx, nil
	}
	if err != nil {
		return idx, err
	}
	if err := json.Unmarshal(b, &idx); err != nil {
		return idx, err
	}
	if idx.Items == nil {
		idx.Items = []VoiceSampleMeta{}
	}
	return idx, nil
}

// saveVoiceSampleIndex 原子写索引。
func (a *App) saveVoiceSampleIndex(profile string, idx voiceSampleIndexFile) error {
	dir := a.voiceSamplesDir(profile)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	return atomicJSON(a.voiceSampleIndexPath(profile), &idx)
}

// voiceSampleKey 当前小秘样本加密密钥。工作台锁定或小秘未解锁时拒绝派生。
func (a *App) voiceSampleKey() ([]byte, error) {
	pw, err := a.voiceSamplePassword()
	if err != nil {
		return nil, err
	}
	return deriveKey(pw), nil
}

var errVoiceSampleLocked = errors.New("小秘未解锁：请先解锁账户/小秘后再管理声音样本")

// voiceSamplePassword 在进入敏感操作及排队后复核权威锁屏状态，无需重复执行 KDF。
func (a *App) voiceSamplePassword() (string, error) {
	a.mu.Lock()
	locked := a.loadLockStateLocked().Locked
	pw := a.personaKey
	a.mu.Unlock()
	if locked || pw == "" {
		return "", errVoiceSampleLocked
	}
	return pw, nil
}

// encryptSampleFile 把明文音频 AES-256-GCM 加密后写到 <id>.enc（二进制 nonce||ciphertext，0600）。
func encryptSampleFile(key []byte, path string, plaintext []byte) error {
	block, err := aesGCMBlock(key)
	if err != nil {
		return err
	}
	nonce := make([]byte, block.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return err
	}
	ct := block.Seal(nonce, nonce, plaintext, nil)
	return os.WriteFile(path, ct, 0600)
}

// decryptSampleFile 读 <id>.enc 并解密。
func decryptSampleFile(key []byte, path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	ct, err := io.ReadAll(io.LimitReader(f, voiceSampleMaxBytes+voiceSampleCipherOverhead+1))
	if err != nil {
		return nil, err
	}
	if len(ct) > voiceSampleMaxBytes+voiceSampleCipherOverhead {
		return nil, errors.New("样本密文超过大小上限")
	}
	block, err := aesGCMBlock(key)
	if err != nil {
		return nil, err
	}
	ns := block.NonceSize()
	if len(ct) < ns {
		return nil, errors.New("样本密文损坏")
	}
	return block.Open(nil, ct[:ns], ct[ns:], nil)
}

// ── HTTP ──────────────────────────────────────────────────────────────────────

// voiceSampleUpload 接收一段录音/上传音频，AES-256-GCM 加密存盘。
// 请求体为原始音频字节（Content-Type: audio/*），查询参数带 durationSec/transcript/consented。
// 未解锁小秘返回 403；未勾选 consented 拒绝（GDPR Art.9）。
func (a *App) voiceSampleUpload(w http.ResponseWriter, r *http.Request) {
	profile, err := safeVoiceProfile(r.URL.Query().Get("profile"))
	if err != nil {
		fail(w, 400, err)
		return
	}
	consented := r.URL.Query().Get("consented") == "1" || r.URL.Query().Get("consented") == "true"
	if !consented {
		fail(w, 400, errors.New("录制/上传前必须勾选「本人或已获被录制者同意」（GDPR Art.9）"))
		return
	}
	key, err := a.voiceSampleKey()
	if err != nil {
		fail(w, 403, err)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, voiceSampleMaxBytes)
	audio, err := io.ReadAll(r.Body)
	if err != nil {
		fail(w, 400, fmt.Errorf("读取音频失败：%w", err))
		return
	}
	if len(audio) == 0 {
		fail(w, 400, errors.New("音频为空"))
		return
	}

	id := newID() + newID()
	meta := VoiceSampleMeta{
		ID:        id,
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Bytes:     len(audio),
		MIMEType:  r.Header.Get("Content-Type"),
		Consented: true,
	}
	if ds := r.URL.Query().Get("durationSec"); ds != "" {
		var d int
		fmt.Sscanf(ds, "%d", &d)
		meta.DurationSec = d
	}
	meta.Transcript = strings.TrimSpace(r.URL.Query().Get("transcript"))
	release, err := a.voiceSampleMutations.acquire(r.Context(), profile)
	if err != nil {
		fail(w, 409, err)
		return
	}
	defer release()

	if _, err := a.voiceSamplePassword(); err != nil {
		fail(w, 403, err)
		return
	}
	dir := a.voiceSamplesDir(profile)
	if err := os.MkdirAll(dir, 0700); err != nil {
		fail(w, 500, err)
		return
	}
	if err := encryptSampleFile(key, filepath.Join(dir, id+".enc"), audio); err != nil {
		fail(w, 500, err)
		return
	}
	idx := a.loadVoiceSampleIndex(profile)
	idx.Items = append(idx.Items, meta)
	if err := a.saveVoiceSampleIndex(profile, idx); err != nil {
		_ = os.Remove(filepath.Join(dir, id+".enc"))
		fail(w, 500, err)
		return
	}
	jsonOut(w, 200, map[string]any{"id": id, "bytes": meta.Bytes, "createdAt": meta.CreatedAt})
}

// voiceSamplesList 列出样本元数据（绝不返回音频本体）。
func (a *App) voiceSamplesList(w http.ResponseWriter, r *http.Request) {
	profile, err := safeVoiceProfile(r.URL.Query().Get("profile"))
	if err != nil {
		fail(w, 400, err)
		return
	}
	idx := a.loadVoiceSampleIndex(profile)
	jsonOut(w, 200, map[string]any{"profile": profile, "samples": idx.Items})
}

// voiceSampleDelete 删除一条样本（.enc + 索引条目）。
func (a *App) voiceSampleDelete(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if err := safeVoiceSampleID(id); err != nil {
		fail(w, 400, err)
		return
	}
	profile, err := safeVoiceProfile(r.URL.Query().Get("profile"))
	if err != nil {
		fail(w, 400, err)
		return
	}
	release, err := a.voiceSampleMutations.acquire(r.Context(), profile)
	if err != nil {
		fail(w, 409, err)
		return
	}
	defer release()
	idx, err := a.readVoiceSampleIndex(profile)
	if err != nil {
		fail(w, 500, err)
		return
	}
	found := false
	kept := idx.Items[:0]
	for _, it := range idx.Items {
		if it.ID == id {
			found = true
			continue
		}
		kept = append(kept, it)
	}
	if !found {
		fail(w, 404, errors.New("样本不存在"))
		return
	}
	idx.Items = kept
	removeErr := os.Remove(filepath.Join(a.voiceSamplesDir(profile), id+".enc"))
	if removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
		fail(w, 500, removeErr)
		return
	}
	if err := a.saveVoiceSampleIndex(profile, idx); err != nil {
		removed := 0
		if removeErr == nil {
			removed = 1
		}
		jsonOut(w, 500, map[string]any{"error": "音频已不存在，但索引保存失败", "removed": removed, "indexSaved": false})
		return
	}
	jsonOut(w, 200, map[string]any{"deleted": id})
}

// voiceSampleAudio 取回单条解密音频（仅供试听/性格推断内部用；不对外列目录）。
// 注意：这是解密通道，必须鉴权（已由中间件 Bearer token 保护）。
func (a *App) voiceSampleAudio(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if err := safeVoiceSampleID(id); err != nil {
		fail(w, 400, err)
		return
	}
	profile, err := safeVoiceProfile(r.URL.Query().Get("profile"))
	if err != nil {
		fail(w, 400, err)
		return
	}
	key, err := a.voiceSampleKey()
	if err != nil {
		fail(w, 403, err)
		return
	}
	release, err := a.voiceSampleMutations.acquire(r.Context(), profile)
	if err != nil {
		fail(w, 409, err)
		return
	}
	defer release()
	if _, err := a.voiceSamplePassword(); err != nil {
		fail(w, 403, err)
		return
	}
	idx, err := a.readVoiceSampleIndex(profile)
	if err != nil {
		fail(w, 500, err)
		return
	}
	found := false
	for _, sample := range idx.Items {
		if sample.ID == id {
			found = true
			break
		}
	}
	if !found {
		fail(w, 404, errors.New("样本不存在"))
		return
	}
	audio, err := decryptSampleFile(key, filepath.Join(a.voiceSamplesDir(profile), id+".enc"))
	if err != nil {
		fail(w, 404, err)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(200)
	_, _ = w.Write(audio)
}

// voiceCloneCreate 把已上传样本送到自托管克隆服务做"创建音色"，返回 voiceID 并写入设置。
// 请求体：{sampleIds:[...], note?}。未配克隆服务 BaseURL 返回 400；克隆服务失败 502。
func (a *App) voiceCloneCreate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		SampleIDs []string `json:"sampleIds"`
		Note      string   `json:"note"`
	}
	if err := decode(w, r, &in); err != nil {
		return
	}
	if len(in.SampleIDs) == 0 {
		fail(w, 400, errors.New("请至少选择一个声音样本"))
		return
	}
	if len(in.SampleIDs) > voiceCloneMaxSamples {
		fail(w, 400, errors.New("最多选择16个声音样本"))
		return
	}
	seen := map[string]bool{}
	for i, id := range in.SampleIDs {
		id = strings.TrimSpace(id)
		if safeVoiceSampleID(id) != nil || seen[id] {
			fail(w, 400, errors.New("声音样本ID无效或重复"))
			return
		}
		seen[id] = true
		in.SampleIDs[i] = id
	}
	a.mu.Lock()
	base := strings.TrimSpace(a.settings.CloneTTSBaseURL)
	apiKey := a.settings.CloneTTSAPIKey
	backend := cloneBackendName(a.settings.CloneTTSBackend)
	a.mu.Unlock()
	if base == "" {
		fail(w, 400, errors.New("尚未配置克隆服务地址：请先在设置填 CloneTTSBaseURL"))
		return
	}
	key, err := a.voiceSampleKey()
	if err != nil {
		fail(w, 403, err)
		return
	}

	// 解密样本（内存中暂存，不落临时盘），送给克隆服务。
	profile := personaXiaomi
	payload := map[string]any{
		"backend": backend,
		"note":    in.Note,
	}
	if backend == "gpt-sovits" {
		// GPT-SoVITS 走文件上传参考音频
		samples := []map[string]string{}
		err := func() error {
			release, err := a.voiceSampleMutations.acquire(r.Context(), profile)
			if err != nil {
				return err
			}
			defer release()
			if _, err := a.voiceSamplePassword(); err != nil {
				return err
			}
			// Inspect every encrypted size before creating any Base64 payload.
			var declared int64
			for _, sid := range in.SampleIDs {
				st, err := os.Lstat(filepath.Join(a.voiceSamplesDir(profile), sid+".enc"))
				if err != nil {
					return err
				}
				size := st.Size() - voiceSampleCipherOverhead
				if !st.Mode().IsRegular() || size < 0 || size > voiceSampleMaxBytes || size > voiceCloneMaxAudioBytes-declared {
					return errors.New("声音样本大小无效或总音频超过32MiB")
				}
				declared += size
			}
			total := 0
			for _, sid := range in.SampleIDs {
				audio, err := decryptSampleFile(key, filepath.Join(a.voiceSamplesDir(profile), sid+".enc"))
				if err != nil {
					return err
				}
				if len(audio) > voiceCloneMaxAudioBytes-total {
					return errors.New("声音样本总音频超过32MiB")
				}
				total += len(audio)
				samples = append(samples, map[string]string{"id": sid, "audioB64": base64.StdEncoding.EncodeToString(audio)})
			}
			return nil
		}()
		if err != nil {
			status := 400
			if errors.Is(err, errVoiceSampleLocked) {
				status = 403
			}
			fail(w, status, err)
			return
		}
		payload["samples"] = samples
	} else {
		// OpenAI 兼容层：多数克隆服务用 voiceID 已绑定参考音频；这里把样本 id 列表传过去，
		// 由服务端决定是否重新注册。mock 服务据此返回固定 voiceID。
		payload["sampleIds"] = in.SampleIDs
	}

	body, err := json.Marshal(payload)
	if err != nil {
		fail(w, 500, err)
		return
	}
	if _, err := a.voiceSamplePassword(); err != nil {
		fail(w, 403, err)
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, base+"/voices", bytes.NewReader(body))
	if err != nil {
		fail(w, 500, err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		fail(w, 502, fmt.Errorf("克隆服务不可达：%w", err))
		return
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		fail(w, 502, fmt.Errorf("克隆服务 HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody))))
		return
	}
	var out struct {
		VoiceID string `json:"voice_id"`
		Status  string `json:"status"`
	}
	_ = json.Unmarshal(respBody, &out)
	if out.VoiceID == "" {
		// mock/部分服务直接返回字符串或 {id}
		var alt struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(respBody, &alt)
		out.VoiceID = alt.ID
	}
	if out.VoiceID == "" {
		fail(w, 502, errors.New("克隆服务未返回 voice_id"))
		return
	}

	// 绑定 voiceID 到设置
	a.mu.Lock()
	candidate := a.settings
	candidate.CloneVoiceID = out.VoiceID
	if err := atomicJSON(SettingsPath(a.dataPath), candidate); err != nil {
		a.mu.Unlock()
		jsonOut(w, 500, map[string]any{
			"error":   "音色已创建，但本地绑定保存失败；原绑定已保留，可使用返回的 voiceID 手动配置",
			"voiceID": out.VoiceID, "status": out.Status, "bound": false,
		})
		return
	}
	a.settings.CloneVoiceID = out.VoiceID
	a.mu.Unlock()

	jsonOut(w, 200, map[string]any{"voiceID": out.VoiceID, "status": out.Status, "bound": true})
}

// voiceSamplesWipe 一键清空某 profile 全部样本（合规：GDPR 删除权）。
func (a *App) voiceSamplesWipe(w http.ResponseWriter, r *http.Request) {
	profile, err := safeVoiceProfile(r.URL.Query().Get("profile"))
	if err != nil {
		fail(w, 400, err)
		return
	}
	release, err := a.voiceSampleMutations.acquire(r.Context(), profile)
	if err != nil {
		fail(w, 409, err)
		return
	}
	defer release()
	dir := a.voiceSamplesDir(profile)
	idx, err := a.readVoiceSampleIndex(profile)
	if err != nil {
		fail(w, 500, err)
		return
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		jsonOut(w, 200, map[string]any{"removed": 0})
		return
	}
	if err != nil {
		fail(w, 500, err)
		return
	}
	removed := 0
	failed := map[string]bool{}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".enc") {
			err := os.Remove(filepath.Join(dir, e.Name()))
			if err == nil {
				removed++
			} else if !errors.Is(err, os.ErrNotExist) {
				failed[e.Name()] = true
			}
		}
	}
	kept := idx.Items[:0]
	for _, sample := range idx.Items {
		if failed[sample.ID+".enc"] {
			kept = append(kept, sample)
		}
	}
	idx.Items = kept
	if err := a.saveVoiceSampleIndex(profile, idx); err != nil {
		jsonOut(w, 500, map[string]any{"error": "清空后索引保存失败，请重试", "removed": removed, "failed": len(failed), "indexSaved": false})
		return
	}
	if len(failed) != 0 {
		jsonOut(w, 500, map[string]any{"error": "部分声音样本未能删除，请重试", "removed": removed, "failed": len(failed), "indexSaved": true})
		return
	}
	jsonOut(w, 200, map[string]any{"removed": removed})
}

// aesGCMBlock 用 32B 密钥构造 AES-256-GCM 实例（样本加密复用 #29 的加密原语）。
func aesGCMBlock(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
