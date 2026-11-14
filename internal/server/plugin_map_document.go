package server

// Map-only capabilities. All source visibility, containment and write checks
// remain in the existing source/document implementations.
import (
	"context"
	"encoding/hex"
	"errors"
	"net/http"
	"path"
	"strings"
	"time"
)

func workbenchToolArgumentsAllowed(id, tool string, args map[string]any) bool {
	if pluginPlanArgumentsAllowed(args) {
		return true
	}
	prefix := mapWorkbenchToolPrefix(id)
	if prefix == "" || (tool != prefix+"browse" && tool != prefix+"read") {
		return false
	}
	source, ok := args["source"].(string)
	if !ok || (source != "workspace" && source != "context" && !strings.HasPrefix(source, "source:")) {
		return false
	}
	for key := range args {
		if key != "source" && key != "path" && !(tool == prefix+"read" && key == "offset") {
			return false
		}
	}
	// api.sources revalidates the selected source's current visibility and root.
	return true
}

func mapWorkbenchToolPrefix(id string) string {
	switch id {
	case "mapcanvas":
		return "mapcanvas_"
	case "ros-map-editor":
		return "ros_map_"
	default:
		return ""
	}
}

func (a *App) bindMapImageDocument(w http.ResponseWriter, r *http.Request, req workbenchBridgeRequest) {
	id, key := r.PathValue("id"), r.PathValue("instance")
	source, sourceOK := req.Arguments["source"].(string)
	name, pathOK := req.Arguments["path"].(string)
	version, versionOK := req.Arguments["version"].(string)
	_, digestErr := hex.DecodeString(version)
	ext := strings.ToLower(path.Ext(name))
	if mapWorkbenchToolPrefix(id) == "" || !sourceOK || !pathOK || !versionOK || len(req.Arguments) != 3 || len(version) != 64 || digestErr != nil || validTransferPath(name) != nil || name == "." || path.Clean(name) != name || (ext != ".png" && ext != ".pgm") {
		fail(w, 400, errors.New("invalid map image selection"))
		return
	}

	a.mu.Lock()
	b, e := a.workbenchBridgeLocked(id, key, true)
	if e == nil && (b.Busy || b.Seen[req.RequestID] || len(b.Seen) >= 128) {
		e = errors.New("map binding busy or replayed")
	}
	origin := ""
	for _, ar := range a.pluginSourceAreasLocked() {
		if pluginSourceID(ar) == source {
			origin = ar.origin
			break
		}
	}
	if e == nil && origin == "" {
		e = errors.New("map source not visible or unsupported")
	}
	if e == nil && b.Document != nil && b.Document.Stage != nil {
		e = errors.New("save or discard the pending draft first")
	}
	if e != nil {
		a.mu.Unlock()
		fail(w, 403, e)
		return
	}
	b.Seen[req.RequestID] = true
	b.Busy = true
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	b.Cancel = cancel
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

	selection := pluginDocumentSelection{Path: name, Root: source}
	if strings.HasPrefix(source, "source:") {
		selection.Root = "context"
		selection.Source = strings.TrimPrefix(source, "source:")
	}
	a.filesMu.Lock()
	defer a.filesMu.Unlock()
	binding, e := a.bindPluginDocument(ctx, id, selection)
	if e != nil {
		fail(w, 403, e)
		return
	}
	installed := false
	defer func() {
		if !installed {
			disposePluginDocument(binding)
		}
	}()
	fs, e := a.pluginDocumentFS(ctx, selection, false)
	if e != nil {
		fail(w, 400, e)
		return
	}
	info, e := fs.Stat(name)
	if e != nil || !info.Mode().IsRegular() || info.Size() > 8<<20 {
		fs.Close()
		fail(w, 400, errors.New("map image is not a regular file within 8 MiB"))
		return
	}
	actual, e := pluginDocumentVersion(ctx, fs, name)
	fs.Close()
	if e != nil || actual != version {
		fail(w, 409, errors.New("map image changed since import; reimport before saving"))
		return
	}

	a.mu.Lock()
	live, e := a.workbenchBridgeLocked(id, key, true)
	current := false
	for _, ar := range a.pluginSourceAreasLocked() {
		if pluginSourceID(ar) == source && ar.origin == origin {
			current = true
			break
		}
	}
	if e == nil && (live != b || !current || ctx.Err() != nil) {
		e = errors.New("map source scope changed")
	}
	if e == nil && b.Document != nil && b.Document.Stage != nil {
		e = errors.New("save or discard the pending draft first")
	}
	if e == nil {
		disposePluginDocument(b.Document)
		b.Document = binding
		installed = true
	}
	a.mu.Unlock()
	if e != nil {
		fail(w, 409, e)
		return
	}
	jsonOut(w, 200, map[string]any{"requestId": req.RequestID, "status": "result", "result": map[string]any{"path": name, "source": source, "editable": binding.Editable, "version": actual}})
}
