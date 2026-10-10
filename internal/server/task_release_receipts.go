package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"time"
)

// Imported observations are operator records, never independent verification.
type taskReleaseArtifact struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}
type taskReleaseReceipt struct {
	Schema     int                   `json:"schema"`
	Tag        string                `json:"tag"`
	Commit     string                `json:"commit"`
	URL        string                `json:"url"`
	ObservedAt string                `json:"observedAt"`
	Deployment string                `json:"deployment"`
	Artifacts  []taskReleaseArtifact `json:"artifacts"`
	RecordedAt string                `json:"recordedAt,omitempty"`
	Digest     string                `json:"digest,omitempty"`
	Source     string                `json:"source,omitempty"`
}

var releaseReceiptHex = regexp.MustCompile(`^[a-f0-9]{64}$`)
var releaseReceiptCommit = regexp.MustCompile(`^[a-f0-9]{40}$`)

func validateTaskReleaseReceipt(v taskReleaseReceipt) error {
	if v.Schema != 1 || !releaseTagPattern.MatchString(v.Tag) || !releaseReceiptCommit.MatchString(v.Commit) {
		return errors.New("发布记录 schema、tag 或 commit 无效")
	}
	if _, err := time.Parse(time.RFC3339Nano, v.ObservedAt); err != nil {
		return errors.New("observedAt 必须为 RFC3339 时间")
	}
	u, err := url.Parse(v.URL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || len(v.URL) > 2048 {
		return errors.New("发布 URL 必须为不含凭据或查询参数的 HTTPS 地址")
	}
	if v.Deployment != "not_deployed" && v.Deployment != "unknown" && v.Deployment != "reported_deployed" {
		return errors.New("deployment 必须为 not_deployed、unknown 或 reported_deployed")
	}
	if len(v.Artifacts) < 1 || len(v.Artifacts) > 64 {
		return errors.New("发布记录须包含1–64项资产")
	}
	names := map[string]bool{}
	for _, item := range v.Artifacts {
		if item.Name == "" || len(item.Name) > 255 || names[item.Name] || !releaseReceiptHex.MatchString(item.SHA256) || item.Bytes < 0 {
			return errors.New("资产名称、SHA256 或字节数无效")
		}
		names[item.Name] = true
	}
	return nil
}
func taskReleaseProjection(receipts []taskReleaseReceipt) map[string]any {
	state, message := "not_recorded", "尚无结构化发布验收记录；聊天结论与命令文本不证明远端发布成功"
	if len(receipts) > 0 {
		state = "operator_recorded"
		message = "操作者导入的历史发布记录；未独立核验远端资产、提交或当前部署状态"
	}
	return map[string]any{"state": state, "message": message, "receipts": receipts}
}
func (a *App) taskReleaseReceiptAPI(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Snapshot string             `json:"snapshot"`
		Receipt  taskReleaseReceipt `json:"receipt"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		fail(w, 400, err)
		return
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		fail(w, 400, errors.New("只能提交一个 JSON 对象"))
		return
	}
	if req.Snapshot == "" {
		fail(w, 400, errors.New("缺少任务快照"))
		return
	}
	if err := validateTaskReleaseReceipt(req.Receipt); err != nil {
		fail(w, 400, err)
		return
	}
	// Server-owned provenance cannot be supplied by an import.
	if req.Receipt.RecordedAt != "" || req.Receipt.Digest != "" || req.Receipt.Source != "" {
		fail(w, 400, errors.New("不可指定服务器记录字段"))
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	session := a.sessions[r.PathValue("id")]
	if session == nil || session.Deleted {
		fail(w, 404, errors.New("任务不存在"))
		return
	}
	for _, task := range session.Runs {
		if task == nil || task.ID != r.PathValue("run") {
			continue
		}
		if req.Snapshot != outcomeSnapshotDigest(task, session.ID, session.Number, session.Title) {
			fail(w, 409, errors.New("任务记录已更新，请重新加载后导入"))
			return
		}
		body, _ := json.Marshal(req.Receipt)
		req.Receipt.Digest = hash(body)
		for _, old := range task.ReleaseReceipts {
			if old.Digest == req.Receipt.Digest {
				jsonOut(w, 200, map[string]any{"duplicate": true})
				return
			}
		}
		if len(task.ReleaseReceipts) >= 50 {
			fail(w, 409, errors.New("当前任务已达50条发布记录上限"))
			return
		}
		req.Receipt.Source = "operator_import"
		req.Receipt.RecordedAt = time.Now().UTC().Format(time.RFC3339Nano)
		previous := task.ReleaseReceipts
		task.ReleaseReceipts = append(append([]taskReleaseReceipt(nil), previous...), req.Receipt)
		if err := a.save(session); err != nil {
			task.ReleaseReceipts = previous
			fail(w, 500, err)
			return
		}
		jsonOut(w, 200, map[string]any{"recorded": true, "digest": req.Receipt.Digest})
		return
	}
	fail(w, 404, errors.New("任务不存在"))
}
