package server

import (
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"sync"
)

// Only embedded public assets receive validators. HTML, APIs and user files
// retain the outer handler's no-store policy. A new binary has a new cache.
func staticAssetServer(public fs.FS) http.Handler {
	files := http.FileServer(http.FS(public))
	var validators sync.Map
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		ext := strings.ToLower(path.Ext(name))
		allowed := ext == ".js" || ext == ".mjs" || ext == ".css" || ext == ".svg" || ext == ".png" || ext == ".jpg" || ext == ".webp" || ext == ".woff2" || ext == ".woff" || ext == ".json"
		if allowed && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
			tag, ok := validators.Load(name)
			if !ok {
				if f, err := public.Open(name); err == nil {
					if info, err := f.Stat(); err == nil && !info.IsDir() {
						h := sha256.New()
						if _, err := io.Copy(h, f); err == nil {
							tag = fmt.Sprintf("\"%x\"", h.Sum(nil))
							validators.Store(name, tag)
						}
					}
					f.Close()
				}
			}
			if tag != nil {
				w.Header().Set("ETag", tag.(string))
				w.Header().Set("Cache-Control", "public, max-age=0, must-revalidate")
			}
		}
		files.ServeHTTP(w, r)
	})
}
