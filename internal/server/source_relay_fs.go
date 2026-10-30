package server

// Relay random-access adapters keep uploads/downloads on the existing durable
// transfer engine. File descriptors stay on the agent and expire if abandoned.
import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"math"
	"os"
	"regexp"
	"strconv"
	"sync"
	"time"
)

type relayExpectedCredential struct{}

func relayExpectedContext(ctx context.Context, digest string) context.Context {
	return context.WithValue(ctx, relayExpectedCredential{}, digest)
}
func (a *App) relayCredentialDigestLocked(id string) (string, error) {
	key, err := a.relayKeyLocked(id)
	if err != nil {
		return "", err
	}
	defer zeroBytes(key)
	return hash(key), nil
}

type sourceRelayInfo struct {
	NameValue    string `json:"name"`
	SizeValue    int64  `json:"size"`
	Directory    bool   `json:"dir"`
	Permissions  uint32 `json:"permissions"`
	ModifiedNano int64  `json:"modifiedNano"`
}

func (v sourceRelayInfo) Name() string { return v.NameValue }
func (v sourceRelayInfo) Size() int64  { return v.SizeValue }
func (v sourceRelayInfo) Mode() os.FileMode {
	mode := os.FileMode(v.Permissions & 0777)
	if v.Directory {
		mode |= os.ModeDir
	}
	return mode
}
func (v sourceRelayInfo) ModTime() time.Time { return time.Unix(0, v.ModifiedNano) }
func (v sourceRelayInfo) IsDir() bool        { return v.Directory }
func (v sourceRelayInfo) Sys() any           { return nil }
func relayFileInfo(value *sourceRelayInfo) (os.FileInfo, error) {
	if value == nil || value.SizeValue < 0 || len(value.NameValue) > 4096 {
		return nil, errors.New("中继文件状态无效")
	}
	return *value, nil
}

var relayHandleID = regexp.MustCompile(`^[a-f0-9]{32}$`)

type sourceRelayFS struct {
	app        *App
	src        Source
	ctx        context.Context
	credential string
	cancel     context.CancelFunc
	mu         sync.Mutex
	files      map[*sourceRelayFile]bool
	closed     bool
}

func (a *App) newSourceRelayFS(ctx context.Context, src Source, write bool) (*sourceRelayFS, error) {
	key, err := a.sourceRelayCredentials(src, write)
	if err != nil {
		return nil, err
	}
	digest := hash(key)
	zeroBytes(key)
	child, cancel := context.WithCancel(relayExpectedContext(ctx, digest))
	return &sourceRelayFS{app: a, src: src, ctx: child, credential: digest, cancel: cancel, files: map[*sourceRelayFile]bool{}}, nil
}
func (f *sourceRelayFS) call(mode string, request map[string]any, data []byte, limit int64) ([]byte, sourceRelayHeader, error) {
	f.mu.Lock()
	closed := f.closed
	f.mu.Unlock()
	if closed {
		return nil, sourceRelayHeader{}, os.ErrClosed
	}
	return f.app.relayCall(f.ctx, f.src, mode, request, data, limit)
}
func (f *sourceRelayFS) Close() {
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		return
	}
	f.closed = true
	files := make([]*sourceRelayFile, 0, len(f.files))
	for file := range f.files {
		files = append(files, file)
	}
	f.mu.Unlock()
	f.cancel()
	for _, file := range files {
		file.Close()
	}
}
func (f *sourceRelayFS) Stat(p string) (os.FileInfo, error) {
	if err := sourceRelayPath(p, true); err != nil {
		return nil, err
	}
	_, h, err := f.call("stat", map[string]any{"path": p}, nil, 0)
	if err != nil {
		return nil, err
	}
	return relayFileInfo(h.Info)
}
func (f *sourceRelayFS) Open(p string, flags int) (transferFile, error) {
	if err := sourceRelayPath(p, false); err != nil {
		return nil, err
	}
	write := flags&(os.O_WRONLY|os.O_RDWR|os.O_CREATE|os.O_TRUNC|os.O_APPEND) != 0
	if flags&os.O_APPEND != 0 {
		return nil, errors.New("中继传输不支持追加模式")
	}
	mode := "openread"
	if write {
		mode = "openwrite"
	}
	_, h, err := f.call(mode, map[string]any{"path": p, "create": flags&os.O_CREATE != 0, "exclusive": flags&os.O_EXCL != 0, "truncate": flags&os.O_TRUNC != 0}, nil, 0)
	if err != nil {
		return nil, err
	}
	if !relayHandleID.MatchString(h.Handle) {
		return nil, errors.New("中继文件句柄无效")
	}
	file := &sourceRelayFile{fs: f, id: h.Handle, write: write}
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		file.Close()
		return nil, os.ErrClosed
	}
	f.files[file] = true
	f.mu.Unlock()
	return file, nil
}
func (f *sourceRelayFS) ReadDir(p string) ([]os.FileInfo, error) {
	if err := sourceRelayPath(p, true); err != nil {
		return nil, err
	}
	values, err := f.app.relayListSource(f.ctx, f.src, p)
	if err != nil {
		return nil, err
	}
	result := make([]os.FileInfo, 0, len(values))
	for _, v := range values {
		sizeText, _ := v["sizeBytes"].(string)
		nanoText, _ := v["modifiedNano"].(string)
		size, err := strconv.ParseInt(sizeText, 10, 64)
		if err != nil || size < 0 {
			return nil, errors.New("中继目录文件大小无效")
		}
		nano, err := strconv.ParseInt(nanoText, 10, 64)
		if err != nil {
			return nil, errors.New("中继目录时间无效")
		}
		name, _ := v["name"].(string)
		dir, _ := v["dir"].(bool)
		result = append(result, sourceRelayInfo{NameValue: name, SizeValue: size, Directory: dir, ModifiedNano: nano})
	}
	return result, nil
}
func (f *sourceRelayFS) mutate(mode, p, destination string) error {
	if err := sourceRelayPath(p, false); err != nil {
		return err
	}
	if mode == "rename" {
		if err := sourceRelayPath(destination, false); err != nil {
			return err
		}
	}
	_, _, err := f.call(mode, map[string]any{"path": p, "destination": destination}, nil, 0)
	return err
}
func (f *sourceRelayFS) CheckSpace(additional int64) error {
	_, h, err := f.call("space", map[string]any{}, nil, 0)
	if err != nil {
		return err
	}
	if h.Available < 64<<20 || additional > h.Available-(64<<20) {
		return errors.New("目标磁盘剩余空间不足（保留64MiB用于正常运行），可释放空间后继续")
	}
	return nil
}

type sourceRelayFile struct {
	fs          *sourceRelayFS
	id          string
	write       bool
	state       sync.Mutex
	position    sync.Mutex
	offset      int64
	closed      bool
	cacheMu     sync.Mutex
	cache       []byte
	cacheOffset int64
	cacheValid  bool
}

func (f *sourceRelayFile) call(mode string, request map[string]any, data []byte, limit int64) ([]byte, sourceRelayHeader, error) {
	f.state.Lock()
	closed := f.closed
	f.state.Unlock()
	if closed {
		return nil, sourceRelayHeader{}, os.ErrClosed
	}
	request["handle"] = f.id
	return f.fs.call(mode, request, data, limit)
}
func (f *sourceRelayFile) Close() error {
	f.state.Lock()
	if f.closed {
		f.state.Unlock()
		return nil
	}
	f.closed = true
	f.state.Unlock()
	f.fs.mu.Lock()
	delete(f.fs.files, f)
	f.fs.mu.Unlock()
	ctx, cancel := context.WithTimeout(relayExpectedContext(context.Background(), f.fs.credential), time.Second)
	defer cancel()
	_, _, err := f.fs.app.relayCall(ctx, f.fs.src, "close", map[string]any{"handle": f.id}, nil, 0)
	return err
}
func (f *sourceRelayFile) ReadAt(p []byte, offset int64) (int, error) {
	if offset < 0 || offset > math.MaxInt64-int64(len(p)) {
		return 0, errors.New("无效偏移")
	}
	done := 0
	// One bounded transfer block per open descriptor avoids a new WAN round trip
	// for every small io.ReaderAt buffer used by hashing and ZIP preparation.
	f.cacheMu.Lock()
	defer f.cacheMu.Unlock()
	if err := f.checkRead(); err != nil {
		return 0, err
	}
	for done < len(p) {
		if err := f.checkRead(); err != nil {
			return done, err
		}
		position := offset + int64(done)
		block := position / transferChunkSize * transferChunkSize
		if !f.cacheValid || f.cacheOffset != block {
			data, _, err := f.call("readat", map[string]any{"offset": block, "size": transferChunkSize}, nil, transferChunkSize)
			if err != nil {
				return done, err
			}
			f.cache = data
			f.cacheOffset = block
			f.cacheValid = true
		}
		index := position - block
		if index >= int64(len(f.cache)) {
			return done, io.EOF
		}
		n := copy(p[done:], f.cache[index:])
		done += n
	}
	return done, nil
}

// Cached bytes remain subject to the same revocation and cancellation gates as RPCs.
func (f *sourceRelayFile) checkRead() error {
	f.state.Lock()
	closed := f.closed
	f.state.Unlock()
	if closed {
		return os.ErrClosed
	}
	if err := f.fs.ctx.Err(); err != nil {
		return err
	}
	key, err := f.fs.app.sourceRelayCredentials(f.fs.src, false)
	if err != nil {
		return err
	}
	defer zeroBytes(key)
	if hash(key) != f.fs.credential {
		return errors.New("中继凭据已变化")
	}
	return nil
}
func (f *sourceRelayFile) WriteAt(p []byte, offset int64) (int, error) {
	f.cacheMu.Lock()
	f.cacheValid = false
	f.cacheMu.Unlock()
	defer func() { f.cacheMu.Lock(); f.cacheValid = false; f.cacheMu.Unlock() }()
	if !f.write {
		return 0, os.ErrPermission
	}
	if offset < 0 || offset > math.MaxInt64-int64(len(p)) {
		return 0, errors.New("无效偏移")
	}
	done := 0
	for done < len(p) {
		count := min(len(p)-done, int(transferChunkSize))
		data := p[done : done+count]
		sum := sha256.Sum256(data)
		_, _, err := f.call("writeat", map[string]any{"offset": offset + int64(done), "size": count, "sha256": hex.EncodeToString(sum[:])}, data, 0)
		if err != nil {
			return done, err
		}
		done += count
	}
	return done, nil
}
func (f *sourceRelayFile) Read(p []byte) (int, error) {
	f.position.Lock()
	defer f.position.Unlock()
	n, err := f.ReadAt(p, f.offset)
	f.offset += int64(n)
	return n, err
}
func (f *sourceRelayFile) Seek(offset int64, whence int) (int64, error) {
	f.position.Lock()
	defer f.position.Unlock()
	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		offset += f.offset
	case io.SeekEnd:
		info, err := f.Stat()
		if err != nil {
			return 0, err
		}
		offset += info.Size()
	default:
		return 0, errors.New("无效定位模式")
	}
	if offset < 0 {
		return 0, errors.New("无效偏移")
	}
	f.offset = offset
	return offset, nil
}
func (f *sourceRelayFile) Stat() (os.FileInfo, error) {
	f.cacheMu.Lock()
	f.cacheValid = false
	f.cacheMu.Unlock()
	_, h, err := f.call("hstat", map[string]any{}, nil, 0)
	if err != nil {
		return nil, err
	}
	return relayFileInfo(h.Info)
}
func (f *sourceRelayFile) Truncate(size int64) error {
	f.cacheMu.Lock()
	f.cacheValid = false
	f.cacheMu.Unlock()
	if !f.write {
		return os.ErrPermission
	}
	_, _, err := f.call("truncate", map[string]any{"size": size}, nil, 0)
	return err
}
func (f *sourceRelayFile) Sync() error {
	if !f.write {
		return nil
	}
	_, _, err := f.call("sync", map[string]any{}, nil, 0)
	return err
}
