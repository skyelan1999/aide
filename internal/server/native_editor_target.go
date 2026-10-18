package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type nativeEditorOpenRequest struct {
	Protocol    int    `json:"protocol"`
	RequestID   string `json:"requestId"`
	SessionPath string `json:"sessionPath"`
	View        string `json:"view"`
}

// The configured workspace must match the broker's single mounted project.
// It is never supplied by a plugin or model request.
type nativeEditorTargetConfig struct{ WorkspaceRoot, SessionPath string }
type nativeEditorTarget struct {
	SessionPath, ProjectStem, Generation, View string
	Versions                                   map[string]string
}

var nativeEditorRequestID = regexp.MustCompile(`^editor_[0-9]{1,32}$`)
var nativeEditorGeneration = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func nativeEditorRelative(p string) bool {
	if p == "" || strings.ContainsAny(p, "\\\x00") || filepath.IsAbs(p) || filepath.ToSlash(filepath.Clean(p)) != p {
		return false
	}
	for _, part := range strings.Split(p, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}
func nativeEditorRead(root *os.Root, relative string, limit int64) ([]byte, string, error) {
	if _, ok := pluginResourcePath(root.Name(), relative); !ok {
		return nil, "", errors.New("editor path alias is not allowed")
	}
	f, e := root.Open(relative)
	if e != nil {
		return nil, "", e
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() || st.Size() > limit {
		return nil, "", errors.New("invalid or oversized editor project file")
	}
	raw, e := io.ReadAll(io.LimitReader(f, limit+1))
	if e != nil || int64(len(raw)) > limit {
		return nil, "", errors.New("editor file read failed or exceeded limit")
	}
	sum := sha256.Sum256(raw)
	return raw, hex.EncodeToString(sum[:]), nil
}

// Pure preparation: resolves and snapshots only. It does not open a GUI, contact
// a broker, acquire a lease, or imply that an editing API is already available.
func prepareNativeEditorTarget(root *os.Root, cfg nativeEditorTargetConfig, in nativeEditorOpenRequest) (nativeEditorTarget, error) {
	out := nativeEditorTarget{}
	if root == nil || cfg.WorkspaceRoot == "" || in.Protocol != 1 || !nativeEditorRequestID.MatchString(in.RequestID) || in.View != "schematic" && in.View != "pcb" || !nativeEditorRelative(in.SessionPath) || cfg.SessionPath != in.SessionPath {
		return out, errors.New("invalid or unconfigured native editor request")
	}
	actual, e := root.Open(".")
	if e != nil {
		return out, e
	}
	actualInfo, e := actual.Stat()
	actual.Close()
	if e != nil {
		return out, e
	}
	configured, e := os.Stat(cfg.WorkspaceRoot)
	if e != nil || !os.SameFile(actualInfo, configured) {
		return out, errors.New("editor broker is configured for a different workspace")
	}
	project := in.SessionPath + "/project"
	if _, ok := pluginResourcePath(root.Name(), project); !ok {
		return out, errors.New("editor project alias denied")
	}
	dir, e := root.Open(project)
	if e != nil {
		return out, e
	}
	entries, e := dir.ReadDir(-1)
	dir.Close()
	if e != nil {
		return out, e
	}
	files := map[string]string{}
	stem := ""
	for _, entry := range entries {
		ext := filepath.Ext(entry.Name())
		if ext != ".kicad_pro" && ext != ".kicad_sch" && ext != ".kicad_pcb" {
			continue
		}
		candidate := strings.TrimSuffix(entry.Name(), ext)
		if entry.IsDir() || candidate == "" || stem != "" && candidate != stem || files[ext] != "" {
			return out, errors.New("native editor requires one unambiguous project stem")
		}
		stem = candidate
		files[ext] = project + "/" + entry.Name()
	}
	if len(files) != 3 {
		return out, errors.New("native project/schematic/PCB triplet missing")
	}
	versions := map[string]string{}
	descriptor := in.SessionPath + "/bridge/session.json"
	raw, sha, e := nativeEditorRead(root, descriptor, 32768)
	if e != nil {
		return out, e
	}
	versions[descriptor] = sha
	var bridge struct {
		Generation string `json:"generation"`
	}
	if json.Unmarshal(raw, &bridge) != nil || !nativeEditorGeneration.MatchString(bridge.Generation) {
		return out, errors.New("native editor generation missing or invalid")
	}
	for _, path := range files {
		_, sha, e := nativeEditorRead(root, path, 32<<20)
		if e != nil {
			return out, e
		}
		versions[path] = sha
	}
	return nativeEditorTarget{SessionPath: in.SessionPath, ProjectStem: stem, Generation: bridge.Generation, View: in.View, Versions: versions}, nil
}
