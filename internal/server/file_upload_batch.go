package server

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"strings"
)

var (
	maxBatchFiles = 1000
	maxBatchBytes = 256 << 20
)

type batchFileResult struct {
	Path  string `json:"path"`
	Size  int64  `json:"size"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

func cleanBatchDest(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" || p == "." {
		return "", nil
	}
	if strings.ContainsAny(p, "\r\n\x00") {
		return "", errors.New("\u8def\u5f84\u5305\u542b\u4e0d\u652f\u6301\u7684\u63a7\u5236\u5b57\u7b26")
	}
	if err := safePath(p); err != nil {
		return "", err
	}
	cleaned := path.Clean(p)
	if cleaned == "." {
		return "", nil
	}
	return cleaned, nil
}

func cleanBatchRel(name string) (string, error) {
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return "", errors.New("\u8def\u5f84\u5305\u542b\u4e0d\u652f\u6301\u7684\u63a7\u5236\u5b57\u7b26")
		}
	}
	name = strings.ReplaceAll(name, "\\", "/")
	if name == "" {
		return "", errors.New("\u7f3a\u5c11\u6587\u4ef6\u540d")
	}
	if strings.HasPrefix(name, "/") {
		return "", errors.New("\u9700\u8981\u5de5\u4f5c\u533a\u5185\u7684\u76f8\u5bf9\u8def\u5f84")
	}
	cleaned := path.Clean(name)
	if cleaned == "." || strings.HasPrefix(cleaned, "..") {
		return "", errors.New("\u8def\u5f84\u4e0d\u53ef\u8bbf\u95ee")
	}
	if err := safePath(cleaned); err != nil {
		return "", err
	}
	return cleaned, nil
}

func (a *App) uploadBatchFile(w http.ResponseWriter, r *http.Request) {
	dest, err := cleanBatchDest(r.URL.Query().Get("path"))
	if err != nil {
		fail(w, 400, err)
		return
	}
	var src Source
	hasSource := false
	if sourceID := strings.TrimSpace(r.URL.Query().Get("source")); sourceID != "" {
		a.mu.Lock()
		got, ok := a.findSource(sourceID)
		a.mu.Unlock()
		if !ok || !got.Enabled {
			fail(w, 400, errors.New("\u6765\u6e90\u4e0d\u5b58\u5728\u6216\u5df2\u505c\u7528"))
			return
		}
		if !got.RW {
			fail(w, 403, errors.New("\u8be5\u6765\u6e90\u4e3a\u53ea\u8bfb"))
			return
		}
		src = got
		hasSource = true
	}
	r.Body = http.MaxBytesReader(w, r.Body, int64(maxBatchBytes)+1)
	mp, err := r.MultipartReader()
	if err != nil {
		fail(w, 400, err)
		return
	}
	if !hasSource {
		a.filesMu.Lock()
		defer a.filesMu.Unlock()
	}
	results := []batchFileResult{}
	fileCount := 0
	for {
		part, nerr := mp.NextPart()
		if nerr == io.EOF {
			break
		}
		if nerr != nil {
			break
		}
		_, dispParams, _ := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
		rawName := dispParams["filename"]
		if rawName == "" {
			_, _ = io.Copy(io.Discard, part)
			_ = part.Close()
			continue
		}
		fileCount++
		if fileCount > maxBatchFiles {
			_, _ = io.Copy(io.Discard, part)
			_ = part.Close()
			results = append(results, batchFileResult{Path: rawName, OK: false,
				Error: fmt.Sprintf("\u6587\u4ef6\u6570\u91cf\u8d85\u8fc7 %d \u4e0a\u9650", maxBatchFiles)})
			break
		}
		rel, perr := cleanBatchRel(rawName)
		full := rawName
		if perr == nil {
			full = path.Join(dest, rel)
			perr = safePath(full)
		}
		if perr != nil {
			_, _ = io.Copy(io.Discard, part)
			_ = part.Close()
			results = append(results, batchFileResult{Path: rawName, OK: false, Error: perr.Error()})
			continue
		}
		b, rerr := io.ReadAll(io.LimitReader(part, maxRawFile+1))
		if rerr == nil {
			_, rerr = io.Copy(io.Discard, part)
		}
		_ = part.Close()
		if rerr != nil {
			results = append(results, batchFileResult{Path: full, OK: false,
				Error: "\u6279\u6b21\u603b\u5927\u5c0f\u8d85\u8fc7\u9650\u5236"})
			break
		}
		if len(b) > maxRawFile {
			results = append(results, batchFileResult{Path: full, OK: false,
				Error: fmt.Sprintf("\u6587\u4ef6\u8d85\u8fc7 %d MiB \u9650\u5236", maxRawFile>>20)})
			continue
		}
		var werr error
		if hasSource {
			if _, e := a.sourceEntry(src, full); e == nil {
				werr = errors.New("\u540c\u540d\u6587\u4ef6\u5df2\u5b58\u5728")
			} else if !errors.Is(e, os.ErrNotExist) {
				werr = e
			} else {
				werr = a.writeSourceText(src, full, b)
			}
		} else {
			if _, e := a.workspaceFileProperties(full); e == nil {
				werr = errors.New("\u540c\u540d\u6587\u4ef6\u5df2\u5b58\u5728")
			} else if !errors.Is(e, os.ErrNotExist) {
				werr = e
			} else {
				werr = a.writeWorkspaceText(full, b)
			}
		}
		if werr != nil {
			results = append(results, batchFileResult{Path: full, OK: false, Error: werr.Error()})
			continue
		}
		results = append(results, batchFileResult{Path: full, Size: int64(len(b)), OK: true})
	}
	if len(results) == 0 {
		fail(w, 400, errors.New("\u672a\u6536\u5230\u4e0a\u4f20\u6587\u4ef6"))
		return
	}
	succeeded, failed := 0, 0
	for _, res := range results {
		if res.OK {
			succeeded++
		} else {
			failed++
		}
	}
	code := 201
	if failed > 0 {
		code = 207
	}
	jsonOut(w, code, map[string]any{
		"results": results,
		"summary": map[string]int{"succeeded": succeeded, "failed": failed},
	})
}
