package server

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

const pluginDocumentChunk = 512 << 10

// Stage fields and the binding's Stage pointer are guarded by App.mu. Stages
// contain no open descriptors. Each bounded call opens/closes its own file.
type pluginDocumentStage struct {
	Path           string
	Size, Received int64
	Version        string
	Ready          bool
}

func disposePluginDocument(d *pluginDocumentBinding) {
	if d != nil && d.Stage != nil {
		_ = os.Remove(d.Stage.Path)
		d.Stage = nil
	}
}
func pluginDocumentDigest(ctx context.Context, f io.Reader) (string, error) {
	h := sha256.New()
	buf := make([]byte, pluginDocumentChunk)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		n, e := f.Read(buf)
		if n > 0 {
			h.Write(buf[:n])
		}
		if e == io.EOF {
			break
		}
		if e != nil {
			return "", e
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func pluginDocumentVersion(ctx context.Context, fs *transferFS, p string) (string, error) {
	if e := pluginDocumentSafePath(fs, p); e != nil {
		return "", e
	}
	f, e := fs.Open(p, os.O_RDONLY)
	if e != nil {
		return "", e
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() {
		return "", errors.New("not a regular document")
	}
	return pluginDocumentDigest(ctx, f)
}

type pluginDocumentArguments struct {
	Asset        string `json:"asset,omitempty"`
	MetadataOnly bool   `json:"metadataOnly,omitempty"`
	Offset       int64  `json:"offset,omitempty"`
	Length       int    `json:"length,omitempty"`
	Size         int64  `json:"size,omitempty"`
	Version      string `json:"version,omitempty"`
	Data         string `json:"data,omitempty"`
}

func pluginAssetPath(main, asset string) (string, error) {
	if asset == "" {
		return main, nil
	}
	if len(asset) > 2048 || strings.ContainsAny(asset, "\\:\x00") || path.IsAbs(asset) {
		return "", errors.New("invalid relative asset path")
	}
	for _, part := range strings.Split(asset, "/") {
		if part == "" || strings.HasPrefix(part, ".") {
			return "", errors.New("asset must stay inside the selected document directory")
		}
	}
	p := path.Join(path.Dir(main), asset)
	if e := validTransferPath(p); e != nil {
		return "", e
	}
	return p, nil
}

// Resource calls are bound to a server-owned selection, never arbitrary API
// URLs or caller-supplied roots. The frame is not allowed to request commit;
// the authenticated parent exposes commit only through its trusted Save click.
func (a *App) callPluginDocument(w http.ResponseWriter, r *http.Request, req workbenchBridgeRequest) {
	id, key := r.PathValue("id"), r.PathValue("instance")
	var args pluginDocumentArguments
	raw, _ := json.Marshal(req.Arguments)
	if e := json.Unmarshal(raw, &args); e != nil {
		fail(w, 400, e)
		return
	}
	a.mu.Lock()
	b, e := a.workbenchBridgeLocked(id, key, true)
	if e != nil || b.Document == nil {
		a.mu.Unlock()
		fail(w, 403, errors.New("no selected document capability"))
		return
	}
	if b.Busy || b.Seen[req.RequestID] || len(b.Seen) >= 128 {
		a.mu.Unlock()
		fail(w, 409, errors.New("busy, replayed or exhausted document request"))
		return
	}
	b.Busy = true
	b.Seen[req.RequestID] = true
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	b.Cancel = cancel
	d := b.Document
	s := d.Selection
	identity := d.Identity
	a.mu.Unlock()
	defer func() {
		cancel()
		a.mu.Lock()
		if a.pluginWorkbenchBridges[key] == b {
			b.Busy = false
			b.Cancel = nil
		}
		a.mu.Unlock()
	}()
	a.filesMu.Lock()
	defer a.filesMu.Unlock()
	admission := func(write bool) error {
		a.mu.Lock()
		current, e := a.workbenchBridgeLocked(id, key, true)
		a.mu.Unlock()
		if e != nil {
			return e
		}
		if current != b {
			return errors.New("document scope changed")
		}
		actual, e := a.pluginDocumentIdentity(s, write)
		if e != nil {
			return e
		}
		if actual != identity {
			return errors.New("document source configuration changed")
		}
		return ctx.Err()
	}
	write := strings.HasPrefix(req.Method, "document.save.")
	if e = admission(write); e != nil {
		fail(w, 403, e)
		return
	}
	if write && !d.Editable {
		fail(w, 403, errors.New("selected document is read-only"))
		return
	}
	result := map[string]any{}
	switch req.Method {
	case "document.info", "document.read", "document.package.info", "document.package.read":
		packageAsset := strings.HasPrefix(req.Method, "document.package.")
		target, err := pluginAssetPath(s.Path, args.Asset)
		var fs *transferFS
		if packageAsset {
			if !strings.HasPrefix(args.Asset, "assets/") {
				e = errors.New("plugin resources must be in assets/")
				break
			}
			target, err = pluginAssetPath("entry", args.Asset)
			if err == nil {
				a.mu.Lock()
				dir := filepath.Join(a.pluginsPath, id)
				a.mu.Unlock()
				var root *os.Root
				root, err = os.OpenRoot(dir)
				if err == nil {
					fs = &transferFS{root: root}
				}
			}
		} else if err == nil {
			fs, err = a.pluginDocumentFS(ctx, s, false)
		}
		if err != nil {
			e = err
			break
		}
		defer fs.Close()
		if e = pluginDocumentSafePath(fs, target); e != nil {
			break
		}
		f, err := fs.Open(target, os.O_RDONLY)
		if err != nil {
			e = err
			break
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil {
			e = err
			break
		}
		if !info.Mode().IsRegular() {
			e = errors.New("only regular document files are readable")
			break
		}
		result = map[string]any{"path": s.Path, "asset": args.Asset, "size": info.Size(), "editable": d.Editable && args.Asset == "", "chunkSize": pluginDocumentChunk}
		if strings.HasSuffix(req.Method, ".info") && !args.MetadataOnly {
			var version string
			version, e = pluginDocumentDigest(ctx, f)
			if e == nil {
				result["version"] = version
			}
		} else if strings.HasSuffix(req.Method, ".read") {
			length := args.Length
			if length == 0 {
				length = pluginDocumentChunk
			}
			if args.Offset < 0 || args.Offset > info.Size() || length < 1 || length > pluginDocumentChunk {
				e = errors.New("invalid document byte range")
				break
			}
			if int64(length) > info.Size()-args.Offset {
				length = int(info.Size() - args.Offset)
			}
			data := make([]byte, length)
			n, err := f.ReadAt(data, args.Offset)
			if err != nil && err != io.EOF {
				e = err
				break
			}
			result["data"] = base64.StdEncoding.EncodeToString(data[:n])
			result["offset"] = args.Offset
			result["length"] = n
			result["eof"] = args.Offset+int64(n) >= info.Size()
		}
	case "document.save.begin":
		if args.Asset != "" || args.Size < 0 || len(args.Version) != 64 {
			e = errors.New("save needs a size and the original document version")
			break
		}
		if _, e = hex.DecodeString(args.Version); e != nil {
			break
		}
		dir := filepath.Join(a.dataPath, "plugin-document-staging")
		if e = os.MkdirAll(dir, 0700); e != nil {
			break
		}
		root, err := os.OpenRoot(dir)
		if err != nil {
			e = err
			break
		}
		defer root.Close()
		a.mu.Lock()
		if live, err := a.workbenchBridgeLocked(id, key, true); err != nil || live != b {
			a.mu.Unlock()
			e = errors.New("document scope revoked")
			break
		}
		reserved := int64(0)
		for _, other := range a.pluginWorkbenchBridges {
			if other.Document != nil && other.Document.Stage != nil {
				stage := other.Document.Stage
				rest := stage.Size - stage.Received
				if rest > 0 {
					if reserved > int64(^uint64(0)>>1)-rest {
						e = errors.New("staging reservation overflow")
						break
					}
					reserved += rest
				}
			}
		}
		if e == nil && d.Stage != nil {
			e = errors.New("discard or save the existing document draft first")
		}
		if e == nil && (args.Size > int64(^uint64(0)>>1)-reserved) {
			e = errors.New("staging size overflow")
		}
		if e == nil {
			e = (&transferFS{root: root}).CheckSpace(args.Size + reserved)
		}
		if e == nil {
			var temp *os.File
			temp, e = os.CreateTemp(dir, "draft-")
			if e == nil {
				temp.Close()
				d.Stage = &pluginDocumentStage{Path: temp.Name(), Size: args.Size, Version: args.Version}
				result["size"] = args.Size
				result["chunkSize"] = pluginDocumentChunk
			}
		}
		a.mu.Unlock()
	case "document.save.chunk":
		if len(args.Data) > base64.StdEncoding.EncodedLen(pluginDocumentChunk) {
			e = errors.New("document chunk exceeds 512KiB")
			break
		}
		data, err := base64.StdEncoding.Strict().DecodeString(args.Data)
		if err != nil {
			e = err
			break
		}
		if len(data) == 0 || len(data) > pluginDocumentChunk {
			e = errors.New("invalid document chunk")
			break
		}
		a.mu.Lock()
		stage := d.Stage
		if stage == nil || stage.Ready || args.Offset != stage.Received || int64(len(data)) > stage.Size-stage.Received {
			e = errors.New("invalid draft offset or state")
		}
		a.mu.Unlock()
		if e != nil {
			break
		}
		f, err := os.OpenFile(stage.Path, os.O_WRONLY, 0600)
		if err != nil {
			e = err
			break
		}
		_, e = f.WriteAt(data, args.Offset)
		if e == nil {
			e = f.Sync()
		}
		f.Close()
		if e == nil {
			a.mu.Lock()
			if d.Stage != stage {
				e = errors.New("draft was revoked")
			} else {
				stage.Received += int64(len(data))
				result["received"] = stage.Received
			}
			a.mu.Unlock()
		}
	case "document.save.request":
		a.mu.Lock()
		stage := d.Stage
		if stage == nil || stage.Received != stage.Size {
			e = errors.New("document draft is incomplete")
		} else {
			stage.Ready = true
			result["savePending"] = true
			result["path"] = s.Path
			result["size"] = stage.Size
		}
		a.mu.Unlock()
	case "document.save.abort":
		a.mu.Lock()
		disposePluginDocument(d)
		a.mu.Unlock()
		result["discarded"] = true
	case "document.save.commit":
		a.mu.Lock()
		var stage *pluginDocumentStage
		if d.Stage != nil && d.Stage.Ready {
			copy := *d.Stage
			stage = &copy
		}
		a.mu.Unlock()
		if stage == nil {
			e = errors.New("no complete draft awaits user save")
			break
		}
		var version string
		version, e = a.commitPluginDocument(ctx, s, stage, func() error { return admission(true) }, func() error {
			live, err := a.workbenchBridgeLocked(id, key, true)
			if err != nil {
				return err
			}
			if live != b {
				return errors.New("document instance revoked")
			}
			if s.Source != "" {
				actual, err := a.pluginDocumentIdentityLocked(s)
				if err != nil {
					return err
				}
				if actual != identity {
					return errors.New("reference configuration changed")
				}
			}
			return ctx.Err()
		})
		var post *markdownPostWriteError
		if e == nil || errors.As(e, &post) {
			result["saved"] = true
			result["version"] = version
			if e != nil {
				result["warning"] = e.Error()
				e = nil
			}
			a.mu.Lock()
			disposePluginDocument(d)
			a.mu.Unlock()
		}
	default:
		e = errors.New("unsupported document method")
	}
	if e == nil {
		e = admission(false)
	}
	if e != nil {
		if result["saved"] == true {
			jsonOut(w, 409, map[string]any{"error": "文件保存已提交，但当前范围已变化；请核对原文件后再操作", "saved": true})
			return
		}
		fail(w, 409, e)
		return
	}
	jsonOut(w, 200, map[string]any{"requestId": req.RequestID, "status": "result", "result": result})
}
func (a *App) commitPluginDocument(ctx context.Context, s pluginDocumentSelection, stage *pluginDocumentStage, admission func() error, lockedAdmission func() error) (string, error) {
	fs, e := a.pluginDocumentFS(ctx, s, true)
	if e != nil {
		return "", e
	}
	defer fs.Close()
	if e = pluginDocumentSafePath(fs, s.Path); e != nil {
		return "", e
	}
	original, e := fs.Stat(s.Path)
	if e != nil {
		return "", e
	}
	if e = fs.CheckSpace(stage.Size); e != nil {
		return "", e
	}
	version, e := pluginDocumentVersion(ctx, fs, s.Path)
	if e != nil || version != stage.Version {
		return "", errors.New("file changed since it was read; reopen before saving")
	}
	temp := path.Join(path.Dir(s.Path), ".aide-plugin-edit-"+newID())
	out, e := fs.Open(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
	if e != nil {
		return "", e
	}
	defer fs.Remove(temp, false)
	in, e := os.Open(stage.Path)
	if e != nil {
		out.Close()
		return "", e
	}
	defer in.Close()
	h := sha256.New()
	buffer := make([]byte, pluginDocumentChunk)
	var copied int64
	for {
		if e = ctx.Err(); e != nil {
			break
		}
		var n int
		n, e = in.Read(buffer)
		if n > 0 {
			var written int
			written, e = out.WriteAt(buffer[:n], copied)
			if e != nil {
				break
			}
			if written != n {
				e = io.ErrShortWrite
				break
			}
			h.Write(buffer[:n])
			copied += int64(n)
		}
		if e == io.EOF {
			e = nil
			break
		}
		if e != nil {
			break
		}
	}
	if e == nil && copied != stage.Size {
		e = errors.New("staged document size changed")
	}
	if e == nil {
		e = syncTransferFile(out)
	}
	out.Close()
	if e != nil {
		return "", e
	}
	if fs.root != nil {
		e = fs.root.Chmod(temp, original.Mode().Perm())
	} else {
		var rp string
		rp, e = fs.remotePath(temp)
		if e == nil {
			e = fs.client.Chmod(rp, original.Mode().Perm())
		}
	}
	if e != nil {
		return "", e
	}
	next := hex.EncodeToString(h.Sum(nil))
	verified, verifyErr := pluginDocumentVersion(ctx, fs, temp)
	if verifyErr != nil || verified != next {
		return "", errors.New("staged file verification failed; original preserved")
	}
	publish := func() error {
		if err := admission(); err != nil {
			return err
		}
		current, err := pluginDocumentVersion(ctx, fs, s.Path)
		if err != nil || current != stage.Version {
			return errors.New("file changed before publication; original preserved")
		}
		// Pin scope through atomic replacement. Internal writers hold filesMu.
		a.mu.Lock()
		defer a.mu.Unlock()
		if err := lockedAdmission(); err != nil {
			return err
		}
		if fs.root != nil {
			return fs.root.Rename(temp, s.Path)
		}
		if _, ok := fs.client.HasExtension("posix-rename@openssh.com"); !ok {
			return errors.New("SFTP server lacks atomic replace support; original preserved")
		}
		oldName, err := fs.remotePath(temp)
		if err != nil {
			return err
		}
		newName, err := fs.remotePath(s.Path)
		if err != nil {
			return err
		}
		return fs.client.PosixRename(oldName, newName)
	}
	source := s.Source
	identity, read, err := a.historyTarget(source, s.Path)
	if err != nil {
		return "", err
	}
	var content []byte
	if strings.EqualFold(path.Ext(s.Path), ".md") || strings.EqualFold(path.Ext(s.Path), ".markdown") {
		if stage.Size > maxPluginDocumentFile {
			return "", fmt.Errorf("Markdown edits retain the native %dMiB limit", maxPluginDocumentFile>>20)
		}
		content, e = os.ReadFile(stage.Path)
		if e != nil {
			return "", e
		}
		if _, e = validateTextFileContent(s.Path, content); e != nil {
			return "", e
		}
	}
	e = a.withMarkdownHistory(identity, s.Path, content, read, publish)
	return next, e
}

// Called only while App.mu is already held. Used to pin scope at publication;
// full capability validation also runs immediately before this lock.
func (a *App) pluginDocumentIdentityLocked(s pluginDocumentSelection) (string, error) {
	if s.Source != "" {
		src, ok := a.findSource(s.Source)
		if !ok || !src.Enabled || !src.RW {
			return "", errors.New("reference read-only")
		}
		return markdownSourceIdentity(src), nil
	}
	if s.Root != "workspace" {
		return "", errors.New("read-only root")
	}
	return a.wsID(), nil
}
