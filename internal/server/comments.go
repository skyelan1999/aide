package server

// ── 侧车批注（sidecar comments）──────────────────────────────────────────────
//
// #63 Word 查看 + 批注（后端）。本层是跨格式通用的批注持久化：不写死 docx，
// 后续 xlsx / pptx / pdf 批注直接复用同一结构与端点，仅前端扩展名路由不同。
//
// 存储布局（/data/comments/，目录 0700、单文件 0600）：
//
//	comments/
//	  index.json                 id → docPath 索引（原子写，定位用）
//	  <sha256(docPath)>/         按文档路径哈希建子目录
//	    <commentID>.json         单条批注一个文件（含回复）
//
// 为什么每批注一个文件而非单一大 JSON：
//   - 增/删/回复都是局部写，不需要整文件读-改-写，并发互不踩踏；
//   - 删除即 unlink，无"最后写覆盖"问题；
//   - 权限按文件收紧（0600），目录 0700。
//
// 锚点容错：创建时记录文档内容哈希 DocHash；读取时与当前文件实算哈希比对，
// 不一致（或文件已删除）则标记 stale=true，不删除批注，由前端提示用户。

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// 批注字段上限（个人本地单用户场景，主要防误传巨串）。
const (
	maxCommentText    = 4000
	maxAnchorQuote    = 500
	maxCommentAuthor  = 100
	maxCommentReplies = 50
)

// CommentReply 批注的一条回复。
type CommentReply struct {
	Text      string `json:"text"`
	Author    string `json:"author,omitempty"`
	CreatedAt string `json:"createdAt"`
}

// Comment 一条侧车批注。JSON 字段即前端契约（见 docs/architecture/office-viewer.md）。
type Comment struct {
	ID          string         `json:"id"`
	DocPath     string         `json:"docPath"`     // 工作区相对路径
	DocHash     string         `json:"docHash"`     // 创建时文档内容 SHA-256（hex）
	AnchorQuote string         `json:"anchorQuote"` // 选中的原文片段
	AnchorIndex int            `json:"anchorIndex"` // 该 quote 在文档中第几次出现（0 起始）
	Text        string         `json:"text"`        // 批注正文
	Author      string         `json:"author,omitempty"`
	CreatedAt   string         `json:"createdAt"`
	UpdatedAt   string         `json:"updatedAt"`
	Status      string         `json:"status"` // open | resolved
	Replies     []CommentReply `json:"replies"`
	// Stale 不由持久层写死：读取时由实算文档哈希与 DocHash 比对得出。
	Stale bool `json:"stale"`
}

// commentIndex id → docPath 索引（PUT/DELETE 按 id 定位子目录用）。
type commentIndex map[string]string

// commentStore 批注存储。每个 App 持有一个（按 dataPath 隔离）。
type commentStore struct {
	dir string // <data>/comments
}

func (a *App) commentStore() *commentStore {
	return &commentStore{dir: CommentsDir(a.dataPath)}
}

// commentPathKey 文档路径 → 子目录名（SHA-256 hex，避免路径分隔符/空格问题）。
func commentPathKey(docPath string) string {
	sum := sha256.Sum256([]byte(docPath))
	return hex.EncodeToString(sum[:])
}

// docDir 某文档批注的子目录（不创建）。
func (cs *commentStore) docDir(docPath string) string {
	return filepath.Join(cs.dir, commentPathKey(docPath))
}

func nowRFC3339() string { return time.Now().UTC().Format(time.RFC3339Nano) }

// loadIndex 读 id→docPath 索引；文件不存在时返回空索引。
func (cs *commentStore) loadIndex() (commentIndex, error) {
	b, err := os.ReadFile(filepath.Join(cs.dir, "index.json"))
	if errors.Is(err, os.ErrNotExist) {
		return commentIndex{}, nil
	}
	if err != nil {
		return nil, err
	}
	idx := commentIndex{}
	if err := json.Unmarshal(b, &idx); err != nil {
		return nil, fmt.Errorf("批注索引损坏: %w", err)
	}
	return idx, nil
}

// saveIndex 原子写索引（0600）。
func (cs *commentStore) saveIndex(idx commentIndex) error {
	return atomicJSON(filepath.Join(cs.dir, "index.json"), idx)
}

// writeComment 写单条批注文件（原子，0600）。
func (cs *commentStore) writeComment(c *Comment) error {
	dir := cs.docDir(c.DocPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	path := filepath.Join(dir, c.ID+".json")
	if err := atomicJSON(path, c); err != nil {
		return err
	}
	return os.Chmod(path, 0600)
}

// readCommentFile 读单个批注文件。
func readCommentFile(path string) (*Comment, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Comment
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("批注文件损坏 %s: %w", filepath.Base(path), err)
	}
	return &c, nil
}

// list 列出某文档全部批注（按创建时间升序）。stale 由调用方填。
func (cs *commentStore) list(docPath string) ([]*Comment, error) {
	dir := cs.docDir(docPath)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return []*Comment{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []*Comment{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		c, err := readCommentFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue // 损坏文件跳过，不阻塞整列表
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt < out[j].CreatedAt })
	return out, nil
}

// get 按 id 定位一条批注。优先查索引；索引缺失时回退全子目录扫描并修复索引。
func (cs *commentStore) get(id string) (*Comment, error) {
	if id == "" {
		return nil, errors.New("缺少批注 id")
	}
	idx, err := cs.loadIndex()
	if err != nil {
		return nil, err
	}
	if docPath, ok := idx[id]; ok {
		c, err := readCommentFile(filepath.Join(cs.docDir(docPath), id+".json"))
		if err == nil {
			return c, nil
		}
	}
	// 索引缺失/过期：扫描所有文档子目录找该文件（容忍索引漂移）
	entries, err := os.ReadDir(cs.dir)
	if err != nil {
		return nil, errors.New("批注不存在")
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		c, err := readCommentFile(filepath.Join(cs.dir, e.Name(), id+".json"))
		if err == nil {
			// 顺带修复索引
			idx[id] = c.DocPath
			_ = cs.saveIndex(idx)
			return c, nil
		}
	}
	return nil, errors.New("批注不存在")
}

// createInput 新建批注入参。
type createInput struct {
	DocPath     string `json:"path"`
	DocHash     string `json:"hash"`
	AnchorQuote string `json:"anchorQuote"`
	AnchorIndex int    `json:"anchorIndex"`
	Text        string `json:"text"`
	Author      string `json:"author"`
}

func (cs *commentStore) create(in createInput) (*Comment, error) {
	in.DocPath = strings.TrimSpace(in.DocPath)
	in.Text = strings.TrimSpace(in.Text)
	if in.DocPath == "" {
		return nil, errors.New("缺少 path")
	}
	if err := safePath(in.DocPath); err != nil {
		return nil, err
	}
	if in.Text == "" {
		return nil, errors.New("批注内容不能为空")
	}
	if len([]rune(in.Text)) > maxCommentText {
		return nil, fmt.Errorf("批注内容超过 %d 字上限", maxCommentText)
	}
	if len([]rune(in.AnchorQuote)) > maxAnchorQuote {
		return nil, fmt.Errorf("引用片段超过 %d 字上限", maxAnchorQuote)
	}
	if len([]rune(in.Author)) > maxCommentAuthor {
		return nil, fmt.Errorf("作者名过长")
	}
	if in.AnchorIndex < 0 {
		in.AnchorIndex = 0
	}
	now := nowRFC3339()
	c := &Comment{
		ID:          newID(),
		DocPath:     in.DocPath,
		DocHash:     strings.TrimSpace(in.DocHash),
		AnchorQuote: strings.TrimSpace(in.AnchorQuote),
		AnchorIndex: in.AnchorIndex,
		Text:        in.Text,
		Author:      strings.TrimSpace(in.Author),
		CreatedAt:   now,
		UpdatedAt:   now,
		Status:      "open",
		Replies:     []CommentReply{},
	}
	if err := cs.writeComment(c); err != nil {
		return nil, err
	}
	idx, err := cs.loadIndex()
	if err != nil {
		return nil, err
	}
	idx[c.ID] = c.DocPath
	if err := cs.saveIndex(idx); err != nil {
		return nil, err
	}
	return c, nil
}

// updateInput 更新批注入参（全部可选；未传字段保持原值）。
type updateInput struct {
	Text   *string `json:"text"`
	Status *string `json:"status"` // open | resolved
	Reply  *struct {
		Text   string `json:"text"`
		Author string `json:"author"`
	} `json:"reply"`
}

func (cs *commentStore) update(id string, in updateInput) (*Comment, error) {
	c, err := cs.get(id)
	if err != nil {
		return nil, err
	}
	changed := false
	if in.Text != nil {
		t := strings.TrimSpace(*in.Text)
		if t == "" {
			return nil, errors.New("批注内容不能为空")
		}
		if len([]rune(t)) > maxCommentText {
			return nil, fmt.Errorf("批注内容超过 %d 字上限", maxCommentText)
		}
		c.Text = t
		changed = true
	}
	if in.Status != nil {
		s := strings.TrimSpace(*in.Status)
		if s != "open" && s != "resolved" {
			return nil, errors.New("status 只支持 open | resolved")
		}
		c.Status = s
		changed = true
	}
	if in.Reply != nil {
		t := strings.TrimSpace(in.Reply.Text)
		if t == "" {
			return nil, errors.New("回复内容不能为空")
		}
		if len(c.Replies) >= maxCommentReplies {
			return nil, fmt.Errorf("回复超过 %d 条上限", maxCommentReplies)
		}
		c.Replies = append(c.Replies, CommentReply{
			Text:      t,
			Author:    strings.TrimSpace(in.Reply.Author),
			CreatedAt: nowRFC3339(),
		})
		changed = true
	}
	if !changed {
		return c, nil
	}
	c.UpdatedAt = nowRFC3339()
	if err := cs.writeComment(c); err != nil {
		return nil, err
	}
	return c, nil
}

// remove 删除批注与索引项。文件不存在视为已删（幂等）。
func (cs *commentStore) remove(id string) error {
	c, err := cs.get(id)
	if err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(cs.docDir(c.DocPath), id+".json")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	idx, err := cs.loadIndex()
	if err != nil {
		return err
	}
	delete(idx, id)
	return cs.saveIndex(idx)
}

// ── 审计日志（audit/comments-audit.jsonl，0600，追加写）──────────────────────

// commentsAudit 追加一条批注审计。只记动作/id/路径，不记批注正文。
func (a *App) commentsAudit(action, id, docPath string) {
	entry := map[string]any{
		"time":    nowRFC3339(),
		"action":  action,
		"comment": id,
		"docPath": docPath,
	}
	b, err := json.Marshal(entry)
	if err != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(AuditDir(a.dataPath), "comments-audit.jsonl"),
		os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(b, '\n'))
}

// ── HTTP 端点（统一走 /api/ Bearer 鉴权，见 buildHandler）────────────────────

// currentDocHash 实算工作区文件内容哈希；文件不存在/不可读返回 ""（视为 stale）。
func (a *App) currentDocHash(docPath string) string {
	b, err := readRawBytes(a.workspace, docPath)
	if err != nil {
		return ""
	}
	return hash(b)
}

// decorateStale 用当前文档哈希标记 stale（在持锁/读快照后调用）。
func markStale(list []*Comment, currentHash string) {
	for _, c := range list {
		c.Stale = c.DocHash == "" || c.DocHash != currentHash
	}
}

// GET /api/comments?path=<docPath>
func (a *App) listComments(w http.ResponseWriter, r *http.Request) {
	docPath := r.URL.Query().Get("path")
	if err := safePath(docPath); err != nil {
		fail(w, 400, err)
		return
	}
	cs := a.commentStore()
	list, err := cs.list(docPath)
	if err != nil {
		fail(w, 500, err)
		return
	}
	markStale(list, a.currentDocHash(docPath))
	jsonOut(w, 200, map[string]any{"comments": list})
}

// POST /api/comments  body: {path, hash, anchorQuote, anchorIndex, text, author?}
func (a *App) createComment(w http.ResponseWriter, r *http.Request) {
	var in createInput
	if err := decode(w, r, &in); err != nil {
		return
	}
	cs := a.commentStore()
	c, err := cs.create(in)
	if err != nil {
		fail(w, 400, err)
		return
	}
	a.commentsAudit("comment-create", c.ID, c.DocPath)
	jsonOut(w, 201, c)
}

// PUT /api/comments/{id}  body: {text?, status?, reply?{text, author?}}
func (a *App) updateComment(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var in updateInput
	if err := decode(w, r, &in); err != nil {
		return
	}
	cs := a.commentStore()
	c, err := cs.update(id, in)
	if err != nil {
		fail(w, 400, err)
		return
	}
	switch {
	case in.Reply != nil:
		a.commentsAudit("comment-reply", c.ID, c.DocPath)
	case in.Status != nil:
		a.commentsAudit("comment-status:"+c.Status, c.ID, c.DocPath)
	default:
		a.commentsAudit("comment-update", c.ID, c.DocPath)
	}
	jsonOut(w, 200, c)
}

// DELETE /api/comments/{id}
func (a *App) deleteComment(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	cs := a.commentStore()
	c, err := cs.get(id)
	if err != nil {
		fail(w, 404, err)
		return
	}
	if err := cs.remove(id); err != nil {
		fail(w, 500, err)
		return
	}
	a.commentsAudit("comment-delete", id, c.DocPath)
	jsonOut(w, 200, map[string]bool{"ok": true})
}
