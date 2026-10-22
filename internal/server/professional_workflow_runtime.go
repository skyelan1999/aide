package server

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const pwRuntimePublicKey = "vhPPCNuLw2ukPR2erTYDx7jUqIj8vIhcK56FyccdesQ="
const pwRuntimeMaxInput = 8 << 20
const pwRuntimeMaxOutput = 256 << 10

type pwRuntimeTarget struct {
	File   string `json:"file"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}
type pwRuntimeDescriptor struct {
	SchemaVersion int                        `json:"schemaVersion"`
	Protocol      int                        `json:"protocol"`
	PluginID      string                     `json:"pluginId"`
	Version       string                     `json:"version"`
	Targets       map[string]pwRuntimeTarget `json:"targets"`
	Assets        map[string]pwRuntimeTarget `json:"assets"`
}
type pwEngineRequest struct {
	Protocol   int               `json:"protocol"`
	Op         string            `json:"op"`
	Definition pwDefinition      `json:"definition"`
	Input      string            `json:"input,omitempty"`
	Values     map[string]string `json:"values,omitempty"`
	Completed  map[string]bool   `json:"completed,omitempty"`
	Active     map[string]bool   `json:"active,omitempty"`
}
type pwEngineStep struct {
	NodeID    string         `json:"nodeId"`
	Type      string         `json:"type"`
	Skipped   bool           `json:"skipped"`
	Pure      bool           `json:"pure"`
	Text      string         `json:"text"`
	Arguments map[string]any `json:"arguments"`
	Output    string         `json:"output"`
	Final     bool           `json:"final"`
}
type pwEngineReply struct {
	Protocol int           `json:"protocol"`
	OK       bool          `json:"ok"`
	Error    string        `json:"error,omitempty"`
	Done     bool          `json:"done,omitempty"`
	Step     *pwEngineStep `json:"step,omitempty"`
}

func pwReadBounded(root *os.Root, name string, limit int64) ([]byte, error) {
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Size() < 0 || st.Size() > limit {
		return nil, errors.New("PW runtime file invalid or too large")
	}
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(b)) > limit {
		return nil, errors.New("PW runtime file read limit exceeded")
	}
	return b, nil
}
func loadPWRuntime(directory string) ([]byte, error) {
	if runtime.GOOS != "linux" || (runtime.GOARCH != "arm64" && runtime.GOARCH != "amd64") {
		return nil, errors.New("PW runtime requires Linux ARM64 or AMD64")
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, errors.New("PW runtime package missing")
	}
	defer root.Close()
	descriptor, err := pwSignedDescriptor(root)
	if err != nil {
		return nil, err
	}
	if len(descriptor.Assets) != 3 {
		return nil, errors.New("PW signed asset set invalid")
	}
	for _, name := range []string{"manifest.json", "index.js", "workbench.html"} {
		asset, ok := descriptor.Assets[name]
		if !ok || asset.Bytes <= 0 || asset.Bytes > 16<<20 {
			return nil, errors.New("PW signed asset metadata invalid")
		}
		raw, err := pwReadBounded(root, name, asset.Bytes)
		if err != nil || int64(len(raw)) != asset.Bytes {
			return nil, fmt.Errorf("PW signed asset missing or size mismatch: %s", name)
		}
		sum := sha256.Sum256(raw)
		if asset.SHA256 != hex.EncodeToString(sum[:]) {
			return nil, fmt.Errorf("PW signed asset checksum mismatch: %s", name)
		}
	}
	target, ok := descriptor.Targets["linux-"+runtime.GOARCH]
	expected := "bin/linux-" + runtime.GOARCH + "/pw-engine"
	if !ok || target.File != expected || target.Bytes <= 0 || target.Bytes > 64<<20 {
		return nil, errors.New("PW runtime architecture or binary size unsupported")
	}
	image, err := pwReadBounded(root, expected, target.Bytes)
	if err != nil || int64(len(image)) != target.Bytes {
		return nil, errors.New("PW runtime binary missing or size mismatch")
	}
	sum := sha256.Sum256(image)
	if target.SHA256 != hex.EncodeToString(sum[:]) {
		return nil, errors.New("PW runtime binary checksum mismatch")
	}
	return image, nil
}
func pwSignedDescriptor(root *os.Root) (pwRuntimeDescriptor, error) {
	raw, err := pwReadBounded(root, "runtime.json", 64<<10)
	if err != nil {
		return pwRuntimeDescriptor{}, fmt.Errorf("PW signed descriptor missing: %w", err)
	}
	sigRaw, err := pwReadBounded(root, "runtime.sig", 256)
	if err != nil {
		return pwRuntimeDescriptor{}, errors.New("PW runtime signature missing")
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sigRaw)))
	key, _ := base64.StdEncoding.DecodeString(pwRuntimePublicKey)
	if err != nil || !ed25519.Verify(key, raw, sig) {
		return pwRuntimeDescriptor{}, errors.New("PW runtime signature invalid")
	}
	var descriptor pwRuntimeDescriptor
	if err = json.Unmarshal(raw, &descriptor); err != nil || descriptor.SchemaVersion != 1 || descriptor.Protocol != 1 || descriptor.PluginID != "professional-workflow" || descriptor.Version != "0.4.0" {
		return pwRuntimeDescriptor{}, errors.New("PW runtime descriptor unsupported")
	}
	return descriptor, nil
}

// Bind the exact HTML bytes being served, not a different path read performed
// before or after ticket creation. Generic workbench ticket digests remain intact.
func verifyPWWorkbenchPage(directory string, page []byte) error {
	if _, err := loadPWRuntime(directory); err != nil {
		return err
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return err
	}
	defer root.Close()
	descriptor, err := pwSignedDescriptor(root)
	if err != nil {
		return err
	}
	asset, ok := descriptor.Assets["workbench.html"]
	sum := sha256.Sum256(page)
	if !ok || int64(len(page)) != asset.Bytes || asset.SHA256 != hex.EncodeToString(sum[:]) {
		return errors.New("PW workbench page does not match signed package")
	}
	return nil
}
func (a *App) pwEngine(ctx context.Context, request pwEngineRequest) (pwEngineReply, error) {
	var reply pwEngineReply
	image, err := loadPWRuntime(filepath.Join(a.pluginsPath, "professional-workflow"))
	if err != nil {
		return reply, err
	}
	request.Protocol = 1
	raw, err := json.Marshal(request)
	if err != nil || len(raw) > pwRuntimeMaxInput {
		return reply, errors.New("PW runtime input exceeds limit")
	}
	bounded, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	output, err := executePWImage(bounded, image, raw)
	if err != nil {
		return reply, fmt.Errorf("PW runtime execution failed: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(output))
	if err = decoder.Decode(&reply); err != nil {
		return reply, errors.New("PW runtime response invalid")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return reply, errors.New("PW runtime response has trailing data")
	}
	if reply.Protocol != 1 || !reply.OK {
		if reply.Error == "" {
			reply.Error = "PW runtime rejected request"
		}
		if len(reply.Error) > 4096 {
			reply.Error = reply.Error[:4096]
		}
		return reply, errors.New(reply.Error)
	}
	return reply, nil
}
func (a *App) validatePWRuntimeLocked(d pwDefinition) error {
	if err := validatePW(d); err != nil {
		return err
	}
	_, err := a.pwEngine(context.Background(), pwEngineRequest{Op: "validate", Definition: d})
	return err
}
func (a *App) advancePW(ctx context.Context, d pwDefinition, input string, values map[string]string, completed, active map[string]bool) (*pwEngineStep, bool, error) {
	reply, err := a.pwEngine(ctx, pwEngineRequest{Op: "advance", Definition: d, Input: input, Values: values, Completed: completed, Active: active})
	if err != nil {
		return nil, false, err
	}
	if reply.Done && len(completed) != len(d.Nodes) {
		return nil, false, errors.New("PW runtime completed before all nodes")
	}
	return reply.Step, reply.Done, nil
}

type pwBoundedOutput struct {
	bytes.Buffer
	limit int
}

func (w *pwBoundedOutput) Write(p []byte) (int, error) {
	if len(p) > w.limit-w.Len() {
		return 0, errors.New("PW runtime output exceeds limit")
	}
	return w.Buffer.Write(p)
}
