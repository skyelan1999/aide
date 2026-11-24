package server

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type compressedStaticAsset struct {
	content []byte
	modTime time.Time
}

var staticReferencePattern = regexp.MustCompile(`(?:src|href)="/([^"?#]+)`)

func staticAcceptsGzip(header string) bool {
	for _, item := range strings.Split(header, ",") {
		parts := strings.Split(item, ";")
		coding := strings.ToLower(strings.TrimSpace(parts[0]))
		if coding != "gzip" && coding != "*" {
			continue
		}
		accepted := true
		for _, parameter := range parts[1:] {
			key, value, ok := strings.Cut(strings.TrimSpace(parameter), "=")
			if ok && strings.EqualFold(strings.TrimSpace(key), "q") {
				quality, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
				accepted = err == nil && quality > 0
			}
		}
		if accepted {
			return true
		}
	}
	return false
}

func compressedStaticETag(tag string) string {
	return strings.TrimSuffix(tag, "\"") + "-gzip\""
}

func compressibleStaticExtension(ext string) bool {
	return ext == ".js" || ext == ".mjs" || ext == ".css" || ext == ".svg" || ext == ".json" || ext == ".wasm"
}

// Only embedded public assets receive validators. HTML, APIs and user files
// retain the outer handler's no-store policy. A new binary has a new cache.
// Initial document references are compressed before the server becomes ready;
// other representations are cached on demand. Range requests and clients
// without gzip support retain the original FileServer behavior.
func staticAssetServer(public fs.FS) http.Handler {
	files := http.FileServer(http.FS(public))
	var validators sync.Map
	var compressed sync.Map
	validator := func(name string) string {
		if tag, ok := validators.Load(name); ok {
			return tag.(string)
		}
		f, err := public.Open(name)
		if err != nil {
			return ""
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil || info.IsDir() {
			return ""
		}
		h := sha256.New()
		if _, err := io.Copy(h, f); err != nil {
			return ""
		}
		tag := fmt.Sprintf("\"%x\"", h.Sum(nil))
		validators.Store(name, tag)
		return tag
	}
	compress := func(name string) (compressedStaticAsset, bool) {
		if value, ok := compressed.Load(name); ok {
			return value.(compressedStaticAsset), true
		}
		f, err := public.Open(name)
		if err != nil {
			return compressedStaticAsset{}, false
		}
		info, statErr := f.Stat()
		content, readErr := io.ReadAll(f)
		f.Close()
		if statErr != nil || readErr != nil || len(content) < 1024 {
			return compressedStaticAsset{}, false
		}
		var encoded bytes.Buffer
		writer, _ := gzip.NewWriterLevel(&encoded, gzip.BestSpeed)
		_, writeErr := writer.Write(content)
		closeErr := writer.Close()
		if writeErr != nil || closeErr != nil || encoded.Len() >= len(content) {
			return compressedStaticAsset{}, false
		}
		asset := compressedStaticAsset{content: encoded.Bytes(), modTime: info.ModTime()}
		compressed.Store(name, asset)
		return asset, true
	}
	if index, err := fs.ReadFile(public, "index.html"); err == nil {
		for _, match := range staticReferencePattern.FindAllSubmatch(index, -1) {
			name := string(match[1])
			if compressibleStaticExtension(strings.ToLower(path.Ext(name))) {
				validator(name)
				compress(name)
			}
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		ext := strings.ToLower(path.Ext(name))
		allowed := compressibleStaticExtension(ext) || ext == ".png" || ext == ".jpg" || ext == ".webp" || ext == ".woff2" || ext == ".woff"
		compressible := allowed && compressibleStaticExtension(ext)
		useGzip := compressible && (r.Method == http.MethodGet || r.Method == http.MethodHead) && r.Header.Get("Range") == "" && staticAcceptsGzip(r.Header.Get("Accept-Encoding"))
		if compressible {
			w.Header().Add("Vary", "Accept-Encoding")
		}
		if ext == ".wasm" {
			w.Header().Set("Content-Type", "application/wasm")
		}
		if allowed && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
			etag := validator(name)
			if etag != "" {
				if useGzip {
					if asset, ok := compress(name); ok {
						w.Header().Set("ETag", compressedStaticETag(etag))
						w.Header().Set("Cache-Control", "public, max-age=0, must-revalidate")
						w.Header().Set("Content-Encoding", "gzip")
						w.Header().Set("Content-Length", strconv.Itoa(len(asset.content)))
						http.ServeContent(w, r, name, asset.modTime, bytes.NewReader(asset.content))
						return
					}
				}
				w.Header().Set("ETag", etag)
				w.Header().Set("Cache-Control", "public, max-age=0, must-revalidate")
			}
		}
		files.ServeHTTP(w, r)
	})
}
