package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

var stepNativeGate = make(chan struct{}, 1)

type stepRawHeader struct {
	status int
	header http.Header
}
type stepRawPipe struct {
	header http.Header
	ready  chan stepRawHeader
	pipe   *io.PipeWriter
	once   sync.Once
}

func (p *stepRawPipe) Header() http.Header { return p.header }
func (p *stepRawPipe) WriteHeader(status int) {
	p.once.Do(func() { p.ready <- stepRawHeader{status, p.header.Clone()} })
}
func (p *stepRawPipe) Write(b []byte) (int, error) { p.WriteHeader(200); return p.pipe.Write(b) }

// Reuse the existing source/root/workspace guards and remote adapters. This
// response stays inside the authenticated request; credentials never enter URLs
// sent to the isolated CAD service. io.Pipe keeps local input streaming bounded.
func (a *App) stepNativePreview(w http.ResponseWriter, r *http.Request) {
	if !isStepRaw(r.URL.Query().Get("path")) {
		fail(w, 400, errors.New("仅支持 STEP 文件"))
		return
	}
	endpoint := os.Getenv("AIDE_CAD_URL")
	if endpoint == "" {
		fail(w, 501, errors.New("当前运行环境未安装原生 CAD 转换服务"))
		return
	}
	select {
	case stepNativeGate <- struct{}{}:
		defer func() { <-stepNativeGate }()
	default:
		fail(w, 429, errors.New("已有 CAD 模型正在转换，请等待或取消后重试"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Minute)
	defer cancel()
	input, output := io.Pipe()
	defer input.Close()
	upstream := &stepRawPipe{header: make(http.Header), ready: make(chan stepRawHeader, 1), pipe: output}
	original := r.Clone(ctx)
	original.Method = http.MethodGet
	original.Header = make(http.Header)
	go func() {
		defer func() {
			if recover() != nil {
				upstream.WriteHeader(500)
				_ = output.CloseWithError(errors.New("STEP 文件读取异常"))
				return
			}
			upstream.WriteHeader(200)
			_ = output.Close()
		}()
		a.readFileRaw(upstream, original)
	}()
	var head stepRawHeader
	select {
	case head = <-upstream.ready:
	case <-ctx.Done():
		return
	}
	if head.status != 200 {
		body, _ := io.ReadAll(io.LimitReader(input, 4096))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(head.status)
		_, _ = w.Write(body)
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/convert", input)
	if err != nil {
		fail(w, 503, errors.New("CAD 服务配置无效"))
		return
	}
	req.Header.Set("Authorization", "Bearer "+a.token)
	req.Header.Set("Content-Type", "application/step")
	if size, e := strconv.ParseInt(head.header.Get("Content-Length"), 10, 64); e == nil && size > 0 {
		req.ContentLength = size
	}
	transport := &http.Transport{DisableKeepAlives: true, ResponseHeaderTimeout: 120 * time.Second}
	defer transport.CloseIdleConnections()
	response, err := (&http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(req)
	if err != nil {
		if ctx.Err() == nil {
			fail(w, 502, errors.New("原生 CAD 服务未响应，请检查转换服务状态"))
		}
		return
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(response.StatusCode)
		_, _ = w.Write(body)
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	// Flush each bounded read, including heartbeats: closing the viewer cancels
	// this request, the helper connection and finally the conversion child.
	buffer := make([]byte, 64<<10)
	for {
		n, e := response.Body.Read(buffer)
		if n > 0 {
			if _, err = w.Write(buffer[:n]); err != nil {
				return
			}
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
		if e != nil {
			return
		}
	}
}
