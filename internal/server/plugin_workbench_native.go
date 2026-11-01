package server

// Manual editor saves are separate from AI tool proposals. Only the trusted
// parent can commit; the frame requests a save through the validated parent bridge.
import (
	"aide/internal/messaging"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path"
	"strings"
	"time"
)

func (a *App) callWorkbenchNative(w http.ResponseWriter, r *http.Request, req workbenchBridgeRequest) {
	id, key := r.PathValue("id"), r.PathValue("instance")
	a.mu.Lock()
	b, e := a.workbenchBridgeLocked(id, key, true)
	if e == nil && (b.Busy || b.Seen[req.RequestID] || len(b.Seen) >= 128) {
		e = errors.New("workbench busy or replayed request")
	}
	if e != nil {
		a.mu.Unlock()
		fail(w, 403, e)
		return
	}
	b.Seen[req.RequestID] = true
	b.Busy = true
	if b.CaptureOwner == "" {
		b.CaptureOwner = key[:32]
	}
	owner, wsid := b.CaptureOwner, a.wsID()
	allowed := false
	for _, p := range a.pluginRegistry.Plugins {
		if p.ID == id && p.Workbench != nil {
			if req.Method == "capture.network" {
				allowed = p.Workbench.MobileCapture
			} else {
				for _, ext := range p.Workbench.FileExtensions {
					if strings.EqualFold(ext, path.Ext(stringValue(req.Arguments["path"]))) {
						allowed = true
					}
				}
			}
		}
	}
	a.mu.Unlock()
	defer func() { a.mu.Lock(); b.Busy = false; a.mu.Unlock() }()
	if !allowed {
		fail(w, 403, errors.New("capability not declared"))
		return
	}
	result := map[string]any{}
	if req.Method == "capture.network" {
		action := stringValue(req.Arguments["action"])
		if action != "start" && action != "status" && action != "stop" {
			fail(w, 400, errors.New("invalid network action"))
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		info, err := messaging.CaptureLAN(ctx, os.Getenv("AIDE_MESSAGE_LAN_BRIDGE"), owner, action == "stop")
		if err != nil {
			fail(w, 503, errors.New("手机局域网桥不可用，请通过 start.command 启动新版 Aide："+err.Error()))
			return
		}
		result["addresses"] = info.Addresses
		result["port"] = 45841
	} else {
		p := stringValue(req.Arguments["path"])
		if e = safePath(p); e != nil || p == "." {
			fail(w, 400, errors.New("invalid save path"))
			return
		}
		if req.Method == "file.target" {
			a.filesMu.Lock()
			data, err := a.readWorkspaceText(p)
			a.filesMu.Unlock()
			if err == nil {
				result["hash"] = hash(data)
				result["exists"] = true
			} else if errors.Is(err, os.ErrNotExist) || isSFTPNotExistErr(err) {
				result["hash"] = ""
				result["exists"] = false
			} else {
				fail(w, 400, err)
				return
			}
		} else if req.Method == "file.save.commit" {
			content, ok := req.Arguments["content"].(string)
			version, hasHash := req.Arguments["hash"].(string)
			if !ok || !hasHash || len(content) > maxPluginDocumentFile {
				fail(w, 400, errors.New("save requires text and original version"))
				return
			}
			a.mu.Lock()
			_, e = a.workbenchBridgeLocked(id, key, true)
			a.mu.Unlock()
			if e != nil {
				fail(w, 403, e)
				return
			}
			body, _ := json.Marshal(map[string]any{"path": p, "content": content, "hash": version, "workspaceId": wsid})
			next := r.Clone(r.Context())
			next.Body = io.NopCloser(bytes.NewReader(body))
			a.writeFile(w, next)
			return
		} else {
			fail(w, 403, errors.New("unknown native method"))
			return
		}
	}
	jsonOut(w, 200, map[string]any{"status": "result", "requestId": req.RequestID, "result": result})
}
func stringValue(v any) string { s, _ := v.(string); return s }
