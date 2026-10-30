package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const transferChunkSize int64 = 4 << 20

type transferPart struct {
	Source  string           `json:"source"`
	Target  string           `json:"target"`
	Size    int64            `json:"size"`
	Mod     int64            `json:"mod"`
	Dir     bool             `json:"dir"`
	Chunks  map[int64]string `json:"-"`
	Deleted bool             `json:"deleted,omitempty"`
}
type transferTop struct {
	Source     string `json:"source"`
	Target     string `json:"target"`
	Stage      string `json:"stage"`
	Dir        bool   `json:"dir"`
	Published  bool   `json:"published"`
	Publishing bool   `json:"publishing,omitempty"`
}
type transferJob struct {
	ID                  string           `json:"id"`
	Operation           string           `json:"operation"`
	Workspace           string           `json:"workspace"`
	Source              string           `json:"source"`
	Destination         string           `json:"destination"`
	SourceIdentity      string           `json:"sourceIdentity"`
	DestinationIdentity string           `json:"destinationIdentity"`
	Status              string           `json:"status"`
	Error               string           `json:"error,omitempty"`
	Created             int64            `json:"created"`
	Updated             int64            `json:"updated"`
	Size                int64            `json:"size"`
	Completed           int64            `json:"completed"`
	Concurrency         int              `json:"concurrency"`
	ChunkSize           int64            `json:"chunkSize"`
	Parts               []*transferPart  `json:"parts"`
	Tops                []*transferTop   `json:"tops"`
	UploadName          string           `json:"uploadName,omitempty"`
	UploadChunks        map[int64]string `json:"-"`
	UploadCommitted     bool             `json:"uploadCommitted,omitempty"`
	RawDownload         bool             `json:"rawDownload,omitempty"`
	DownloadVersion     string           `json:"downloadVersion,omitempty"`
	uploading           map[int64]context.CancelFunc
	uploadWG            sync.WaitGroup
	checkpointAt        time.Time
	mu                  sync.Mutex
	cancel              context.CancelFunc
}
type transferManager struct {
	app      *App
	dir      string
	mu       sync.Mutex
	jobs     map[string]*transferJob
	wg       sync.WaitGroup
	closed   bool
	slots    chan struct{}
	runners  chan struct{}
	builders chan struct{}
}
type transferCreate struct {
	Workspace       string   `json:"workspaceId"`
	Operation       string   `json:"operation"`
	Source          string   `json:"source"`
	Paths           []string `json:"paths"`
	Destination     string   `json:"destination"`
	DestinationPath string   `json:"destinationPath"`
	Name            string   `json:"name"`
	Size            int64    `json:"size"`
	Concurrency     int      `json:"concurrency"`
	RawDownload     bool     `json:"rawDownload"`
}

func (a *App) transfers() *transferManager {
	a.transferMu.Lock()
	defer a.transferMu.Unlock()
	if a.transferManager != nil {
		return a.transferManager
	}
	m := &transferManager{app: a, dir: filepath.Join(a.dataPath, "file-transfers"), jobs: map[string]*transferJob{}, slots: make(chan struct{}, 8), runners: make(chan struct{}, 2), builders: make(chan struct{}, 2)}
	_ = os.MkdirAll(m.dir, 0700)
	entries, _ := os.ReadDir(m.dir)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(m.dir, e.Name(), "job.json"))
		if err != nil {
			continue
		}
		var j transferJob
		if json.Unmarshal(b, &j) != nil || j.ID != e.Name() {
			continue
		}
		if j.Status != "complete" && j.Status != "cancelled" && j.Status != "failed" {
			j.Status = "paused"
		}
		if err = m.recoverChunks(&j); err != nil {
			j.Status = "failed"
			j.Error = err.Error()
		}
		m.jobs[j.ID] = &j
	}
	a.transferManager = m
	return m
}
func (m *transferManager) jobDir(j *transferJob) string { return filepath.Join(m.dir, j.ID) }
func (m *transferManager) save(j *transferJob) error {
	j.Updated = time.Now().UnixMilli()
	return atomicJSON(filepath.Join(m.jobDir(j), "job.json"), j)
}
func (m *transferManager) close() {
	m.mu.Lock()
	m.closed = true
	for _, j := range m.jobs {
		j.mu.Lock()
		if j.cancel != nil {
			j.cancel()
		}
		for _, cancel := range j.uploading {
			cancel()
		}
		j.mu.Unlock()
	}
	m.mu.Unlock()
	m.wg.Wait()
	for _, j := range m.jobs {
		j.uploadWG.Wait()
	}
}
func (a *App) transferBinding(id string, write bool) (string, error) {
	loc, e := a.transferLocation(id, write)
	if e != nil {
		return "", e
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if loc.hasSource {
		if loc.source.Type == "relay" {
			digest, err := a.relayCredentialDigestLocked(loc.source.ID)
			if err != nil {
				return "", err
			}
			return hash([]byte(markdownSourceIdentity(loc.source) + digest)), nil
		}
		return markdownSourceIdentity(loc.source), nil
	}
	b, _ := json.Marshal(a.wsConfig.Workspace)
	return hash([]byte(a.wsID() + a.workspace.Name() + string(b))), nil
}
func (m *transferManager) check(j *transferJob) error {
	a := m.app
	a.mu.Lock()
	ws := a.wsID()
	a.mu.Unlock()
	if ws != j.Workspace {
		return errors.New("工作区已切换，传输已暂停")
	}
	if j.Operation != "upload" {
		v, e := a.transferBinding(j.Source, j.Operation == "move")
		if e != nil {
			return e
		}
		if v != j.SourceIdentity {
			return errors.New("来源配置已变化，请重新创建传输")
		}
	}
	if j.Operation != "download" {
		v, e := a.transferBinding(j.Destination, true)
		if e != nil {
			return e
		}
		if v != j.DestinationIdentity {
			return errors.New("目标配置或写权限已变化")
		}
	}
	return nil
}
func transferWorkers(n int) int {
	if n < 1 {
		return 4
	}
	if n > 8 {
		return 8
	}
	return n
}
func (m *transferManager) create(ctx context.Context, in transferCreate) (*transferJob, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case m.builders <- struct{}{}:
		defer func() { <-m.builders }()
	default:
		return nil, errors.New("传输清单创建繁忙，请稍后重试")
	}
	if in.Operation != "upload" && in.Operation != "copy" && in.Operation != "move" && in.Operation != "download" {
		return nil, errors.New("无效传输操作")
	}
	if in.RawDownload && (in.Operation != "download" || len(in.Paths) != 1) {
		return nil, errors.New("原文件快照下载需要选择一个普通文件")
	}
	if in.Destination == "" {
		in.Destination = "workspace"
	}
	if in.Source == "" {
		in.Source = "workspace"
	}
	if in.DestinationPath == "" {
		in.DestinationPath = "."
	}
	if in.DestinationPath != "." {
		if e := validTransferPath(in.DestinationPath); e != nil {
			return nil, e
		}
	}
	j := &transferJob{ID: newID(), Operation: in.Operation, Source: in.Source, Destination: in.Destination, Status: "paused", Created: time.Now().UnixMilli(), Concurrency: transferWorkers(in.Concurrency), ChunkSize: transferChunkSize}
	j.RawDownload = in.RawDownload
	m.app.mu.Lock()
	j.Workspace = m.app.wsID()
	m.app.mu.Unlock()
	if in.Workspace != "" && in.Workspace != j.Workspace {
		return nil, errors.New("工作区已切换")
	}
	var err error
	if in.Operation != "upload" {
		j.SourceIdentity, err = m.app.transferBinding(j.Source, in.Operation == "move")
		if err != nil {
			return nil, err
		}
	}
	var dst *transferFS
	if in.Operation != "download" {
		j.DestinationIdentity, err = m.app.transferBinding(j.Destination, true)
		if err != nil {
			return nil, err
		}
		dst, err = m.app.streamTransferFS(ctx, j.Destination, true)
		if err != nil {
			return nil, err
		}
		defer dst.Close()
		if err = dst.MkdirAll(in.DestinationPath); err != nil {
			return nil, err
		}
	}
	budget := transferManifestBudget{bytes: 4096 + len(j.SourceIdentity) + len(j.DestinationIdentity)}
	if in.Operation == "upload" {
		if in.Size < 0 || validTransferPath(in.Name) != nil {
			return nil, errors.New("无效文件路径或大小")
		}
		target := path.Join(in.DestinationPath, in.Name)
		if _, e := dst.Stat(target); e == nil {
			return nil, fmt.Errorf("目标已存在：%s", target)
		} else if !os.IsNotExist(e) {
			return nil, e
		}
		j.Size = in.Size
		j.UploadName = in.Name
		j.UploadChunks = map[int64]string{}
		j.Status = "uploading"
		stage := path.Join(path.Dir(target), ".aide-transfer-"+j.ID)
		j.Tops = []*transferTop{{Target: target, Stage: stage}}
		j.Parts = []*transferPart{{Source: "upload.data", Target: stage, Size: in.Size, Chunks: map[int64]string{}}}
	} else {
		if len(in.Paths) == 0 || len(in.Paths) > 1000 {
			return nil, errors.New("请选择1–1000个顶层文件或目录")
		}
		src, e := m.app.streamTransferFS(ctx, j.Source, false)
		if e != nil {
			return nil, e
		}
		defer src.Close()
		seen := map[string]bool{}
		for i, p := range in.Paths {
			if err := budget.discover(); err != nil {
				return nil, err
			}
			if validTransferPath(p) != nil || seen[p] {
				return nil, errors.New("源路径无效或重复")
			}
			for q := range seen {
				if strings.HasPrefix(p, q+"/") || strings.HasPrefix(q, p+"/") {
					return nil, errors.New("不能同时选择目录与其子项")
				}
			}
			seen[p] = true
			info, e := src.Stat(p)
			if e != nil {
				return nil, e
			}
			if !info.IsDir() && !info.Mode().IsRegular() {
				return nil, errors.New("仅支持普通文件与目录")
			}
			if in.RawDownload && info.IsDir() {
				return nil, errors.New("原文件快照下载不支持目录，请使用打包下载")
			}
			target := path.Join(in.DestinationPath, path.Base(p))
			stage := path.Join(in.DestinationPath, fmt.Sprintf(".aide-transfer-%s-%d", j.ID, i))
			if in.Operation != "download" {
				if _, e = dst.Stat(target); e == nil {
					return nil, fmt.Errorf("目标已存在：%s", target)
				} else if !os.IsNotExist(e) {
					return nil, e
				}
				sl, _ := m.app.transferLocation(j.Source, false)
				dl, _ := m.app.transferLocation(j.Destination, true)
				si, e := m.app.transferIdentity(sl, p)
				if e != nil {
					return nil, e
				}
				di, e := m.app.transferIdentity(dl, target)
				if e != nil {
					return nil, e
				}
				if si == di || info.IsDir() && strings.HasPrefix(di, si+"/") {
					return nil, errors.New("不能传输到自身或子目录")
				}
			}
			top := &transferTop{Source: p, Target: target, Stage: stage, Dir: info.IsDir()}
			if err := budget.add(top); err != nil {
				return nil, err
			}
			j.Tops = append(j.Tops, top)
			var walk func(string, string, os.FileInfo, int) error
			walk = func(p, to string, info os.FileInfo, depth int) error {
				if e := ctx.Err(); e != nil {
					return e
				}
				if depth > 128 {
					return errors.New("目录层级过深")
				}
				if info.Mode()&os.ModeSymlink != 0 {
					return errors.New("传输不跟随符号链接")
				}
				if !info.IsDir() && !info.Mode().IsRegular() {
					return errors.New("不支持设备文件或管道")
				}
				part := &transferPart{Source: p, Target: to, Size: info.Size(), Mod: info.ModTime().UnixNano(), Dir: info.IsDir(), Chunks: map[int64]string{}}
				if err := budget.add(part); err != nil {
					return err
				}
				j.Parts = append(j.Parts, part)
				if !info.IsDir() {
					if info.Size() < 0 || j.Size > int64(^uint64(0)>>1)-info.Size() {
						return errors.New("文件大小超出64位范围")
					}
					j.Size += info.Size()
					return nil
				}
				return src.readDirEach(ctx, p, func(c os.FileInfo) error {
					if err := budget.discover(); err != nil {
						return err
					}
					if validTransferPath(path.Join(p, c.Name())) != nil {
						return nil
					}
					return walk(path.Join(p, c.Name()), path.Join(to, c.Name()), c, depth+1)
				})
			}
			if e = walk(p, stage, info, 0); e != nil {
				return nil, e
			}
		}
	}
	if err = m.check(j); err != nil {
		return nil, err
	}
	j.Updated = time.Now().UnixMilli()
	if err = validateTransferManifestSize(j); err != nil {
		return nil, err
	}
	if err = os.MkdirAll(m.jobDir(j), 0700); err != nil {
		return nil, err
	}
	registered, staged := false, false
	defer func() {
		if !registered {
			if staged {
				_ = dst.Remove(j.Parts[0].Target, false)
			}
			_ = os.RemoveAll(m.jobDir(j))
		}
	}()
	if j.Operation == "upload" {
		if err = dst.CheckSpace(j.Size); err != nil {
			return nil, err
		}
		if err = dst.MkdirAll(path.Dir(j.Parts[0].Target)); err != nil {
			return nil, err
		}
		f, e := dst.Open(j.Parts[0].Target, os.O_CREATE|os.O_EXCL|os.O_RDWR)
		if e != nil {
			return nil, e
		}
		staged = true
		err = f.Truncate(j.Size)
		if err == nil {
			err = syncTransferFile(f)
		}
		f.Close()
		if err != nil {
			return nil, err
		}
	}
	if err = m.save(j); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, errors.New("传输服务正在关闭")
	}
	m.jobs[j.ID] = j
	registered = true
	return j, nil
}
func (m *transferManager) start(j *transferJob) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("传输服务正在关闭")
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.cancel != nil {
		return nil
	}
	if j.Status == "complete" || j.Status == "cancelled" {
		return errors.New("任务已经结束")
	}
	if e := m.check(j); e != nil {
		return e
	}
	if j.Operation == "upload" && !j.UploadCommitted {
		for n := int64(0); n < j.Size; n += transferChunkSize {
			if j.UploadChunks[n/transferChunkSize] == "" {
				return errors.New("上传分块尚未完成")
			}
		}
		j.UploadCommitted = true
		j.Parts[0].Chunks = make(map[int64]string, len(j.UploadChunks))
		for n, h := range j.UploadChunks {
			j.Parts[0].Chunks[n] = h
		}

	}
	ctx, cancel := context.WithCancel(context.Background())
	j.cancel = cancel
	j.Status = "queued"
	j.Error = ""
	if e := m.save(j); e != nil {
		cancel()
		j.cancel = nil
		return e
	}
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		var err error
		select {
		case m.runners <- struct{}{}:
			j.mu.Lock()
			if ctx.Err() == nil {
				j.Status = "running"
				_ = m.save(j)
			}
			j.mu.Unlock()
			if ctx.Err() != nil {
				err = ctx.Err()
			} else {
				err = m.run(ctx, j)
			}
			<-m.runners
		case <-ctx.Done():
			err = ctx.Err()
		}
		j.mu.Lock()
		cancelled := j.Status == "cancelling"
		j.mu.Unlock()
		var cleanupErr error
		if cancelled {
			cleanupErr = m.cleanup(j)
		}
		j.mu.Lock()
		defer j.mu.Unlock()
		j.cancel = nil
		cancel()
		if err == nil {
			j.Status = "complete"
			j.Completed = j.Size
			j.Parts = nil
			j.UploadChunks = nil
		} else if j.Status == "cancelling" {
			j.Status = "cancelled"
			j.Error = ""
			if cleanupErr != nil {
				j.Error = "暂存清理未完成：" + cleanupErr.Error()
			}
		} else if errors.Is(err, context.Canceled) {
			j.Status = "paused"
		} else {
			j.Status = "failed"
			j.Error = err.Error()
		}
		_ = m.save(j)
	}()
	return nil
}
func (m *transferManager) uploadChunk(w http.ResponseWriter, r *http.Request, j *transferJob) {
	j.mu.Lock()
	totalSize := j.Size
	j.mu.Unlock()
	index, e := strconv.ParseInt(r.PathValue("chunk"), 10, 64)
	if e != nil || index < 0 || index > totalSize/transferChunkSize {
		fail(w, 400, errors.New("无效分块"))
		return
	}
	offset := index * transferChunkSize
	size := min(transferChunkSize, totalSize-offset)
	if size <= 0 {
		fail(w, 400, errors.New("无效分块"))
		return
	}
	select {
	case m.slots <- struct{}{}:
		defer func() { <-m.slots }()
	case <-r.Context().Done():
		return
	}
	b, e := io.ReadAll(http.MaxBytesReader(w, r.Body, size+1))
	if e != nil || int64(len(b)) != size {
		fail(w, 400, errors.New("分块长度不匹配"))
		return
	}
	digest := hash(b)
	if r.Header.Get("X-Chunk-SHA256") != digest {
		fail(w, 400, errors.New("分块校验失败"))
		return
	}
	if e = m.writeUploadChunk(r.Context(), j, index, b, digest); e != nil {
		fail(w, 409, e)
		return
	}
	jsonOut(w, 200, map[string]any{"sha256": digest})
}
func (m *transferManager) writeUploadChunk(parent context.Context, j *transferJob, index int64, b []byte, digest string) error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return errors.New("传输服务正在关闭")
	}
	j.mu.Lock()
	// The lifecycle gate and job lock order matches close(). Register an upload
	// before close can begin waiting, never Add concurrently with a zero Wait.
	m.mu.Unlock()
	if e := m.check(j); e != nil {
		j.mu.Unlock()
		return e
	}
	if j.Operation != "upload" || j.UploadCommitted || j.Status != "uploading" {
		j.mu.Unlock()
		return errors.New("任务未处于上传状态")
	}
	if previous := j.UploadChunks[index]; previous != "" {
		j.mu.Unlock()
		if previous != digest {
			return errors.New("已确认分块内容不同")
		}
		return nil
	}
	if j.uploading == nil {
		j.uploading = map[int64]context.CancelFunc{}
	}
	if j.uploading[index] != nil {
		j.mu.Unlock()
		return errors.New("分块正在写入，请重试")
	}
	ctx, cancel := context.WithCancel(parent)
	j.uploading[index] = cancel
	j.uploadWG.Add(1)
	j.mu.Unlock()
	defer func() { cancel(); j.mu.Lock(); delete(j.uploading, index); j.mu.Unlock(); j.uploadWG.Done() }()
	fs, e := m.app.streamTransferFS(ctx, j.Destination, true)
	if e != nil {
		return e
	}
	defer fs.Close()
	if e = fs.CheckSpace(int64(len(b))); e != nil {
		return e
	}
	f, e := fs.Open(j.Parts[0].Target, os.O_RDWR)
	if e != nil {
		return e
	}
	defer f.Close()
	offset := index * transferChunkSize
	if _, e = f.WriteAt(b, offset); e != nil {
		return e
	}
	if e = syncTransferFile(f); e != nil {
		return e
	}

	j.mu.Lock()
	defer j.mu.Unlock()
	if e = ctx.Err(); e != nil {
		return e
	}
	if j.Status != "uploading" {
		return errors.New("任务已经暂停")
	}
	if e = m.check(j); e != nil {
		return e
	}
	if e = m.acknowledge(j, 0, index, digest, true); e != nil {
		return e
	}
	j.UploadChunks[index] = digest
	j.Parts[0].Chunks[index] = digest
	j.Completed += int64(len(b))
	return m.checkpoint(j)
}

func (a *App) transferJobsAPI(w http.ResponseWriter, r *http.Request) {
	m := a.transfers()
	id := r.PathValue("id")
	if id == "" {
		if r.Method == "GET" {
			offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
			if offset < 0 {
				offset = 0
			}
			m.mu.Lock()
			all := make([]*transferJob, 0, len(m.jobs))
			for _, j := range m.jobs {
				all = append(all, j)
			}
			m.mu.Unlock()
			sort.Slice(all, func(i, k int) bool { return all[i].Created > all[k].Created })
			out := make([]map[string]any, 0, 100)
			matched := 0
			for _, j := range all {
				j.mu.Lock()
				if m.check(j) == nil {
					if matched >= offset && len(out) < 100 {
						out = append(out, transferSummary(j, false))
					}
					matched++
				}
				j.mu.Unlock()
			}
			jsonOut(w, 200, out)
			return
		}
		var in transferCreate
		if e := decode(w, r, &in); e != nil {
			fail(w, 400, e)
			return
		}
		j, e := m.create(r.Context(), in)
		if e != nil {
			code := 400
			if errors.Is(e, os.ErrExist) || strings.Contains(e.Error(), "已存在") {
				code = 409
			}
			fail(w, code, e)
			return
		}
		if in.Operation != "upload" {
			if e = m.start(j); e != nil {
				fail(w, 409, e)
				return
			}
		}
		j.mu.Lock()
		defer j.mu.Unlock()
		jsonOut(w, 201, transferSummary(j, false))
		return
	}
	m.mu.Lock()
	j := m.jobs[id]
	m.mu.Unlock()
	if j == nil {
		fail(w, 404, errors.New("传输任务不存在"))
		return
	}
	j.mu.Lock()
	e := m.check(j)
	j.mu.Unlock()
	if e != nil {
		fail(w, 409, e)
		return
	}
	if r.Method == "DELETE" {
		j.mu.Lock()
		if j.cancel != nil || len(j.uploading) > 0 {
			j.mu.Unlock()
			fail(w, 409, errors.New("请先暂停任务再移除记录"))
			return
		}
		j.Status = "cancelled"
		_ = m.save(j)
		j.mu.Unlock()
		if e = m.cleanup(j); e != nil {
			fail(w, 500, e)
			return
		}
		m.mu.Lock()
		if m.closed {
			m.mu.Unlock()
			fail(w, 503, errors.New("传输服务已关闭"))
			return
		}
		delete(m.jobs, id)
		m.mu.Unlock()
		if e = os.RemoveAll(m.jobDir(j)); e != nil {
			fail(w, 500, e)
			return
		}
		jsonOut(w, 200, map[string]bool{"ok": true})
		return
	}
	if r.Method == "PUT" {
		m.uploadChunk(w, r, j)
		return
	}
	if r.Method == "GET" {
		j.mu.Lock()
		defer j.mu.Unlock()
		jsonOut(w, 200, transferSummary(j, true))
		return
	}
	action := r.PathValue("action")
	if action == "resume" || action == "commit" {
		var opts struct {
			Concurrency int `json:"concurrency"`
		}
		if r.ContentLength != 0 {
			if e = decode(w, r, &opts); e != nil {
				fail(w, 400, e)
				return
			}
		}
		j.mu.Lock()
		terminal := j.Status == "cancelled" || j.Status == "complete"
		if opts.Concurrency != 0 && j.cancel == nil {
			j.Concurrency = transferWorkers(opts.Concurrency)
		}
		j.mu.Unlock()
		if terminal {
			fail(w, 409, errors.New("任务已结束"))
			return
		}
	}
	if action == "resume" || action == "commit" {
		j.mu.Lock()
		upload := j.Operation == "upload" && !j.UploadCommitted
		if upload && action == "resume" {
			j.Status = "uploading"
			j.Error = ""
			e = m.save(j)
		}
		j.mu.Unlock()
		if !upload || action == "commit" {
			e = m.start(j)
		}
		if e != nil {
			fail(w, 409, e)
			return
		}
	} else if action == "pause" || action == "cancel" {
		j.mu.Lock()
		if j.Status != "complete" && j.Status != "cancelled" {
			j.Status = "paused"
			for _, cancel := range j.uploading {
				cancel()
			}
			if j.cancel != nil {
				j.Status = "pausing"
				if action == "cancel" {
					j.Status = "cancelling"
				}
				j.cancel()
			} else if action == "cancel" {
				j.Status = "cancelled"
			}
			e = m.save(j)
		}
		j.mu.Unlock()
		if e != nil {
			fail(w, 500, e)
			return
		}
	} else {
		fail(w, 400, errors.New("无效传输动作"))
		return
	}
	if action == "cancel" {
		j.uploadWG.Wait()
		j.mu.Lock()
		idle := j.cancel == nil
		j.mu.Unlock()
		if idle {
			if cleanupErr := m.cleanup(j); cleanupErr != nil {
				j.mu.Lock()
				j.Error = "暂存清理未完成：" + cleanupErr.Error()
				_ = m.save(j)
				j.mu.Unlock()
			}
		}
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	jsonOut(w, 200, transferSummary(j, true))
}

func transferDigestAt(f io.ReaderAt, offset, size int64) (string, error) {
	if size < 0 || size > transferChunkSize {
		return "", errors.New("校验分块超出范围")
	}
	// One bounded ReadAt lets SFTP pipeline its packets. Hashing through 32KiB
	// CopyN reads would turn verification into one round trip per packet.
	b := make([]byte, int(size))
	if _, e := io.ReadFull(io.NewSectionReader(f, offset, size), b); e != nil {
		return "", e
	}
	return hash(b), nil
}
