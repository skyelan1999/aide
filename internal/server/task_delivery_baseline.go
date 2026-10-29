package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf8"
)

const deliveryBaselineReadBytes = 64 << 20
const deliveryBaselineMetadataBytes = 128 << 10
const deliveryBaselineEntries = 20000

// Backend evidence captured before this task can run tools or call a provider.
// Scanned directories prove absence without pretending an incomplete tree walk
// covered the entire workspace. Resume copies retain this evidence with the task.
type TaskDeliveryBaseline struct {
	WorkspaceID string                        `json:"workspaceId"`
	Revision    uint64                        `json:"revision"`
	Files       map[string]deliveryFileBefore `json:"files"`
	Directories map[string]bool               `json:"directories"`
	Scanned     map[string]bool               `json:"scanned"`
	Excluded    []string                      `json:"excluded,omitempty"`
}

type deliveryFileBefore struct {
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256,omitempty"`
}

func deliveryFileType(p string) bool {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".png", ".jpg", ".jpeg", ".webp", ".gif", ".bmp", ".ico", ".tif", ".tiff", ".avif", ".mp4", ".mp3", ".wav", ".pdf", ".docx", ".xlsx", ".pptx", ".dxf", ".stl", ".step", ".stp", ".zip":
		return true
	}
	return false
}

// Nonblocking open also rejects a FIFO/device swapped in after Lstat. os.Root
// confines ancestor resolution; SameFile rejects a different leaf after open.
func openDeliveryFile(root *os.Root, p string) (*os.File, os.FileInfo, error) {
	before, err := root.Lstat(p)
	if err != nil || !before.Mode().IsRegular() {
		return nil, nil, errors.New("交付文件不可读取")
	}
	f, err := root.OpenFile(p, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, nil, err
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || !os.SameFile(before, info) {
		f.Close()
		return nil, nil, errors.New("交付文件已变化")
	}
	return f, info, nil
}

func deliveryDigest(f *os.File, info os.FileInfo, remaining *int64) string {
	if info.Size() < 0 || info.Size() > *remaining {
		return ""
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return ""
	}
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(f, info.Size()+1))
	*remaining -= n
	after, statErr := f.Stat()
	if err != nil || statErr != nil || n != info.Size() || !os.SameFile(info, after) || after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (a *App) prepareTaskDeliveryBaseline(ctx context.Context, task *Task) error {
	a.filesMu.Lock()
	defer a.filesMu.Unlock()
	a.mu.Lock()
	prompt := task.Prompt
	if task.MediaPrompt != "" {
		prompt = task.MediaPrompt
	}
	needed := requiredDeliveryKind(prompt) != "" || generationModelOutput(task.Model) != ""
	root, workspaceID, revision := a.workspace, a.wsID(), a.wsRevision
	if !needed || root == nil || a.workspaceMode() == "ssh" ||
		task.DeliveryBaseline != nil && task.DeliveryBaseline.WorkspaceID == workspaceID && task.DeliveryBaseline.Revision == revision {
		a.mu.Unlock()
		return nil
	}
	if task.WorkspaceID != "" && (task.WorkspaceID != workspaceID || task.WorkspaceRev != revision) {
		a.mu.Unlock()
		return errors.New("工作区已变化，未建立交付验证基线")
	}
	dataPath := a.dataPath
	a.mu.Unlock()
	b := &TaskDeliveryBaseline{WorkspaceID: workspaceID, Revision: revision, Files: map[string]deliveryFileBefore{}, Directories: map[string]bool{".": true}, Scanned: map[string]bool{}}
	queue := []string{"."}
	remaining, entries, metadata := int64(deliveryBaselineReadBytes), 0, 0
	stopped := false
	for cursor := 0; cursor < len(queue) && !stopped; cursor++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		dir := queue[cursor]
		if ok, _ := canAccessMemory(dataPath, callerAide, filepath.Join(root.Name(), filepath.FromSlash(dir)), opRead); !ok {
			b.Excluded = append(b.Excluded, dir)
			continue
		}
		before, err := root.Lstat(dir)
		if err != nil || !before.IsDir() {
			b.Excluded = append(b.Excluded, dir)
			continue
		}
		f, err := root.OpenFile(dir, os.O_RDONLY|syscall.O_NONBLOCK, 0)
		if err != nil {
			b.Excluded = append(b.Excluded, dir)
			continue
		}
		info, err := f.Stat()
		if err != nil || !info.IsDir() || !os.SameFile(before, info) {
			f.Close()
			b.Excluded = append(b.Excluded, dir)
			continue
		}
		complete := true
		for {
			if err := ctx.Err(); err != nil {
				f.Close()
				return err
			}
			children, readErr := f.ReadDir(128)
			for _, child := range children {
				entries++
				if entries > deliveryBaselineEntries {
					stopped = true
					break
				}
				p := path.Join(dir, child.Name())
				if !utf8.ValidString(p) {
					complete = false // JSON cannot persist this key without changing it.
					continue
				}
				if child.Type()&os.ModeSymlink != 0 || child.IsDir() || deliveryFileType(p) {
					metadata += 2*len(p) + 128
				}
				if metadata > deliveryBaselineMetadataBytes {
					stopped = true
					break
				}
				if child.Type()&os.ModeSymlink != 0 {
					b.Excluded = append(b.Excluded, p)
					continue
				}
				if child.IsDir() {
					b.Directories[p] = true
					switch child.Name() {
					case ".git", "node_modules", "vendor", ".venv", ".cache":
						b.Excluded = append(b.Excluded, p)
					default:
						queue = append(queue, p)
					}
					continue
				}
				if !deliveryFileType(p) {
					continue
				}
				// Record existence even when hashing fails or exhausts its budget.
				b.Files[p] = deliveryFileBefore{Size: -1}
				if ok, _ := canAccessMemory(dataPath, callerAide, filepath.Join(root.Name(), filepath.FromSlash(p)), opRead); !ok {
					continue
				}
				if file, stat, err := openDeliveryFile(root, p); err == nil {
					b.Files[p] = deliveryFileBefore{Size: stat.Size(), SHA256: deliveryDigest(file, stat, &remaining)}
					file.Close()
				}
			}
			if stopped || readErr != nil {
				if !stopped && complete && readErr == io.EOF {
					b.Scanned[dir] = true
				}
				break
			}
		}
		f.Close()
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if root != a.workspace || workspaceID != a.wsID() || revision != a.wsRevision {
		return errors.New("工作区已变化，交付验证基线已丢弃")
	}
	owner := a.approvalSessionLocked(task)
	if owner == nil || owner.Deleted {
		return errors.New("任务会话已不存在，未建立交付验证基线")
	}
	old := task.DeliveryBaseline
	task.DeliveryBaseline = b
	if err := a.save(owner); err != nil {
		task.DeliveryBaseline = old
		return err
	}
	return nil
}

// Recover actual directory-entry spelling, using file identity for aliases.
// A case/normalization/short-name alias must not become a new namespace entry.
func deliveryCanonicalPath(root *os.Root, p string, remaining *int) (string, bool) {
	parent := "."
	parts := strings.Split(p, "/")
	for i, part := range parts {
		before, err := root.Lstat(parent)
		if err != nil || !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
			return "", false
		}
		wanted, err := root.Lstat(filepath.FromSlash(path.Join(parent, part)))
		if err != nil || i < len(parts)-1 && (!wanted.IsDir() || wanted.Mode()&os.ModeSymlink != 0) {
			return "", false
		}
		dir, err := root.OpenFile(filepath.FromSlash(parent), os.O_RDONLY|syscall.O_NONBLOCK, 0)
		if err != nil {
			return "", false
		}
		stat, err := dir.Stat()
		if err != nil || !stat.IsDir() || !os.SameFile(before, stat) {
			dir.Close()
			return "", false
		}
		actual, alias := "", ""
		ambiguous, eof := false, false
		for actual == "" && *remaining > 0 {
			children, readErr := dir.ReadDir(128)
			for _, child := range children {
				*remaining -= 1
				if *remaining < 0 {
					break
				}
				if child.Name() == part {
					actual = part
					break
				}
				alternative, err := root.Lstat(filepath.FromSlash(path.Join(parent, child.Name())))
				if err == nil && os.SameFile(wanted, alternative) {
					if alias != "" {
						ambiguous = true
					}
					alias = child.Name()
				}
			}
			if readErr != nil {
				eof = readErr == io.EOF
				break
			}
		}
		dir.Close()
		if actual == "" && eof && !ambiguous {
			actual = alias
		}
		if actual == "" || !utf8.ValidString(actual) {
			return "", false
		}
		parent = path.Join(parent, actual)
	}
	return parent, true
}

func (scope deliveryWorkspace) deliveryIsCurrent(task *Task, p string, f *os.File, info os.FileInfo, remaining *int64, pathEntries *int) bool {
	p = filepath.ToSlash(filepath.Clean(p))
	if task.WorkspaceID != "" && (task.WorkspaceID != scope.id || task.WorkspaceRev != scope.revision) {
		return false
	}
	// A native generation's asset name is assigned by the server after a
	// successful write, and survives resume without another billable request.
	if g := task.MediaGeneration; g != nil && g.Asset == p && g.Identity != "" && g.RequestHash != "" {
		return true
	}
	for _, change := range task.Files {
		if change.Applied && change.Source == "" && change.Binding == "" && change.TargetPath == "" &&
			filepath.ToSlash(filepath.Clean(change.Path)) == p && int64(len(change.Content)) == info.Size() {
			return deliveryDigest(f, info, remaining) == hash([]byte(change.Content))
		}
	}
	b := task.DeliveryBaseline
	if b == nil || b.WorkspaceID != scope.id || b.Revision != scope.revision || !deliveryFileType(p) {
		return false
	}
	canonical, ok := deliveryCanonicalPath(scope.root, p, pathEntries)
	if !ok {
		return false
	}
	p = canonical
	for _, excluded := range b.Excluded {
		if excluded == "." || p == excluded || strings.HasPrefix(p, excluded+"/") {
			return false
		}
	}
	if before, exists := b.Files[p]; exists {
		if before.Size >= 0 && info.Size() != before.Size {
			return true
		}
		if before.SHA256 == "" {
			return false
		}
		digest := deliveryDigest(f, info, remaining)
		return digest != "" && digest != before.SHA256
	}
	dir := path.Dir(p)
	if b.Scanned[dir] {
		return true
	}
	for dir != "." {
		parent := path.Dir(dir)
		if b.Scanned[parent] && !b.Directories[dir] {
			return true // The output directory itself did not exist before execution.
		}
		dir = parent
	}
	return false
}
