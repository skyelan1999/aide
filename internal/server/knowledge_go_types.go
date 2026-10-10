package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/build"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path"
	"sort"
	"syscall"
	"time"
)

// Input is the authorized indexed snapshot; the helper does not run project code.
type goTypeFile struct {
	ID     string `json:"id"`
	Path   string `json:"path"`
	Root   string `json:"root"`
	Source string `json:"source"`
	Module string `json:"module"`
	Text   string `json:"text"`
}
type goTypeBinding struct {
	File       string `json:"file"`
	Caller     string `json:"caller"`
	Line       int    `json:"line"`
	Column     int    `json:"column"`
	TargetFile string `json:"targetFile"`
	Target     string `json:"target"`
	TargetLine int    `json:"targetLine"`
}
type goTypeResult struct {
	Engine      string          `json:"engine"`
	Packages    int             `json:"packages"`
	Checked     int             `json:"checked"`
	Bindings    []goTypeBinding `json:"bindings"`
	Diagnostics []string        `json:"diagnostics"`
	Truncated   bool            `json:"truncated"`
}
type goTypePackage struct {
	scope      string
	importPath string
	trees      []*ast.File
	info       *types.Info
	pkg        *types.Package
	checking   bool
	done       bool
	err        error
}
type goSnapshotImporter struct {
	scope    string
	packages map[string]*goTypePackage
	check    func(*goTypePackage) error
	standard types.Importer
}

func (i goSnapshotImporter) Import(name string) (*types.Package, error) {
	if p := i.packages[i.scope+"\x00"+name]; p != nil {
		if err := i.check(p); err != nil {
			return nil, err
		}
		return p.pkg, nil
	}
	p, err := build.Default.Import(name, "", build.FindOnly)
	if err != nil || !p.Goroot {
		return nil, fmt.Errorf("dependency outside indexed snapshot: %s", name)
	}
	return i.standard.Import(name)
}
func checkGoTypes(files []goTypeFile) (goTypeResult, error) {
	result := goTypeResult{Engine: "go/types-indexed-snapshot-v1", Bindings: []goTypeBinding{}, Diagnostics: []string{}}
	if len(files) > 80 {
		return result, errors.New("超过80个Go文件")
	}
	invalid := map[string]bool{}
	total := 0
	ids := map[string]bool{}
	packages := map[string]*goTypePackage{}
	lookup := map[string]*goTypePackage{}
	fs := token.NewFileSet()
	for _, f := range files {
		total += len(f.Text)
		if len(f.Text) > 256*1024 || total > 4*1024*1024 {
			return result, errors.New("Go类型解析输入超限")
		}
		if f.ID == "" || ids[f.ID] {
			return result, errors.New("文件编号为空或重复")
		}
		ids[f.ID] = true
		scope := f.Root + "\x00" + f.Source
		dir := path.Dir(f.Path)
		tree, err := parser.ParseFile(fs, f.ID, f.Text, 0)
		if err != nil {
			invalid[scope+"\x00"+dir] = true
			result.Diagnostics = append(result.Diagnostics, f.ID+": syntax error")
			continue
		}
		key := scope + "\x00" + dir + "\x00" + tree.Name.Name
		p := packages[key]
		if p == nil {
			importPath := path.Join(f.Module, dir)
			if f.Module == "" {
				importPath = "snapshot/" + dir
			}
			p = &goTypePackage{scope: scope, importPath: importPath, info: &types.Info{Uses: map[*ast.Ident]types.Object{}, Defs: map[*ast.Ident]types.Object{}, Selections: map[*ast.SelectorExpr]*types.Selection{}}}
			packages[key] = p
		}
		p.trees = append(p.trees, tree)
	}
	for _, f := range files {
		if invalid[f.Root+"\x00"+f.Source+"\x00"+path.Dir(f.Path)] {
			for key, p := range packages {
				if key == f.Root+"\x00"+f.Source+"\x00"+path.Dir(f.Path)+"\x00"+p.trees[0].Name.Name {
					p.done = true
					p.err = errors.New("package contains syntax-invalid indexed file")
				}
			}
		}
	}
	keys := []string{}
	ambiguous := map[string]bool{}
	for key, p := range packages {
		keys = append(keys, key)
		importKey := p.scope + "\x00" + p.importPath
		if lookup[importKey] != nil {
			ambiguous[importKey] = true
		}
		lookup[importKey] = p
	}
	for key := range ambiguous {
		delete(lookup, key)
	}
	sort.Strings(keys)
	result.Packages = len(packages)
	var check func(*goTypePackage) error
	check = func(p *goTypePackage) error {
		if p.done {
			return p.err
		}
		if p.checking {
			return errors.New("snapshot import cycle")
		}
		p.checking = true
		cfg := types.Config{Importer: goSnapshotImporter{p.scope, lookup, check, importer.Default()}, Error: func(err error) {
			if len(result.Diagnostics) < 100 {
				result.Diagnostics = append(result.Diagnostics, p.importPath+": "+err.Error())
			} else {
				result.Truncated = true
			}
		}}
		p.pkg, p.err = cfg.Check(p.importPath, fs, p.trees, p.info)
		p.checking = false
		p.done = true
		if p.err == nil {
			result.Checked++
		}
		return p.err
	}
	for _, key := range keys {
		_ = check(packages[key])
	}
	// Definition object identity, rather than textual method names, selects targets.
	type definition struct {
		file, key string
		line      int
	}
	definitions := map[types.Object]definition{}
	receiverName := func(e ast.Expr) string {
		if ptr, ok := e.(*ast.StarExpr); ok {
			e = ptr.X
		}
		if id, ok := e.(*ast.Ident); ok {
			return id.Name
		}
		return ""
	}
	for _, key := range keys {
		p := packages[key]
		if p.err != nil {
			continue
		}
		for _, tree := range p.trees {
			for _, decl := range tree.Decls {
				if fn, ok := decl.(*ast.FuncDecl); ok {
					name := fn.Name.Name
					if fn.Recv != nil && len(fn.Recv.List) > 0 {
						name = receiverName(fn.Recv.List[0].Type) + "." + name
					}
					obj := p.info.Defs[fn.Name]
					if obj != nil {
						pos := fs.Position(fn.Pos())
						definitions[obj] = definition{pos.Filename, name, pos.Line}
					}
				}
			}
		}
	}
	for _, key := range keys {
		p := packages[key]
		if p.err != nil {
			continue
		}
		var walk func(ast.Node, string)
		walk = func(node ast.Node, caller string) {
			ast.Inspect(node, func(node ast.Node) bool {
				if node == nil {
					return false
				}
				if literal, ok := node.(*ast.FuncLit); ok {
					pos := fs.Position(literal.Pos())
					walk(literal.Body, fmt.Sprintf("%s.anonymous@%d:%d", caller, pos.Line, pos.Column))
					return false
				}
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				var identifier *ast.Ident
				switch fun := call.Fun.(type) {
				case *ast.Ident:
					identifier = fun
				case *ast.SelectorExpr:
					identifier = fun.Sel
					if selection := p.info.Selections[fun]; selection != nil {
						if _, dynamic := selection.Recv().Underlying().(*types.Interface); dynamic {
							return true
						}
					}
				}
				if identifier == nil {
					return true
				}
				obj, ok := p.info.Uses[identifier].(*types.Func)
				if !ok {
					return true
				}
				target, ok := definitions[obj]
				if !ok {
					return true
				}
				pos := fs.Position(call.Pos())
				if len(result.Bindings) < 6500 {
					result.Bindings = append(result.Bindings, goTypeBinding{pos.Filename, caller, pos.Line, pos.Column, target.file, target.key, target.line})
				} else {
					result.Truncated = true
				}
				return true
			})
		}
		for _, tree := range p.trees {
			for _, decl := range tree.Decls {
				if fn, ok := decl.(*ast.FuncDecl); ok && fn.Body != nil {
					name := fn.Name.Name
					if fn.Recv != nil && len(fn.Recv.List) > 0 {
						name = receiverName(fn.Recv.List[0].Type) + "." + name
					}
					walk(fn.Body, name)
				}
			}
		}
	}
	sort.Strings(result.Diagnostics)
	return result, nil
}

// This mode skips normal server startup, models, plugins and credential stores.
func RunGoTypeHelper(input io.Reader, output io.Writer) error {
	raw, err := io.ReadAll(io.LimitReader(input, 8*1024*1024+1))
	if err != nil {
		return err
	}
	if len(raw) > 8*1024*1024 {
		return errors.New("Go类型解析请求超限")
	}
	var files []goTypeFile
	if err = json.Unmarshal(raw, &files); err != nil {
		return err
	}
	result, err := checkGoTypes(files)
	if err != nil {
		return err
	}
	return json.NewEncoder(output).Encode(result)
}

type goTypeOutput struct{ bytes.Buffer }

func (b *goTypeOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 2*1024*1024 {
		return 0, errors.New("Go类型解析输出超限")
	}
	return b.Buffer.Write(p)
}

var knowledgeGoTypeGate = make(chan struct{}, 2)

func runGoTypeService(ctx context.Context, files []goTypeFile) (goTypeResult, error) {
	child, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	select {
	case knowledgeGoTypeGate <- struct{}{}:
		defer func() { <-knowledgeGoTypeGate }()
	case <-child.Done():
		return goTypeResult{}, child.Err()
	}
	ctx = child
	binary, err := os.Executable()
	if err != nil {
		return goTypeResult{}, err
	}
	return runGoTypeProcess(ctx, binary, files)
}

// The production caller always uses its own executable, never a workspace path.
func runGoTypeProcess(ctx context.Context, binary string, files []goTypeFile) (goTypeResult, error) {
	var result goTypeResult
	raw, err := json.Marshal(files)
	if err != nil || len(raw) > 8*1024*1024 {
		return result, errors.New("Go类型解析请求超限")
	}
	child, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(child, binary, "--knowledge-go-types")
	cmd.Stdin = bytes.NewReader(raw)
	var out goTypeOutput
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	cmd.Dir = os.TempDir()
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "LANG=C.UTF-8", "HOME=" + os.TempDir(), "GOPROXY=off", "GOSUMDB=off", "GOTOOLCHAIN=local", "GOFLAGS=", "GOENV=off", "GOMAXPROCS=2", "GOMEMLIMIT=256MiB"}
	for _, name := range []string{"GOROOT", "GOCACHE"} {
		if value := os.Getenv(name); value != "" {
			cmd.Env = append(cmd.Env, name+"="+value)
		}
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = time.Second
	if err = cmd.Run(); err != nil {
		if child.Err() != nil {
			return result, child.Err()
		}
		return result, errors.New("Go类型服务失败；保留AST候选关系")
	}
	if err = json.Unmarshal(out.Bytes(), &result); err != nil {
		return result, errors.New("Go类型服务响应无效")
	}
	if result.Engine != "go/types-indexed-snapshot-v1" || result.Packages < 0 || result.Packages > 80 || result.Checked < 0 || result.Checked > result.Packages || len(result.Bindings) > 6500 || len(result.Diagnostics) > 100 {
		return goTypeResult{}, errors.New("Go类型服务响应无效")
	}
	return result, nil
}

func enrichKnowledgeGoTypes(ctx context.Context, g *knowledgeGraph, files []knowledgeCodeFile) {
	input := []goTypeFile{}
	for _, f := range files {
		if codeLanguage(f.Node.Path) == "Go" {
			input = append(input, goTypeFile{f.ID, f.Node.Path, f.Node.Root, f.Node.Source, f.Module, f.Text})
		}
	}
	if len(input) == 0 {
		return
	}
	result, err := runGoTypeService(ctx, input)
	if err != nil {
		message := "Go类型服务失败；保留AST候选关系"
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			message = "Go类型服务超时或已取消；保留AST候选关系"
		}
		g.Code.Diagnostics = append(g.Code.Diagnostics, message)
		return
	}
	applyKnowledgeGoTypes(g, result)
}

// Only bindings to existing indexed symbols can become graph edges. Positions
// include columns so two calls on the same source line are never conflated.
func applyKnowledgeGoTypes(g *knowledgeGraph, result goTypeResult) {
	if g.Code == nil {
		return
	}
	symbols := map[string]knowledgeNode{}
	for _, n := range g.Nodes {
		if n.Kind == "symbol" && n.Language == "Go" {
			symbols[n.ParentFile+"\x00"+n.Name] = n
		}
	}
	position := func(from string, line, column int) string { return fmt.Sprintf("%s\x00%d:%d", from, line, column) }
	replacements := map[string]knowledgeEdge{}
	for _, b := range result.Bindings {
		from, ok := symbols[b.File+"\x00"+b.Caller]
		if !ok {
			continue
		}
		target, ok := symbols[b.TargetFile+"\x00"+b.Target]
		if !ok || target.Line != b.TargetLine || b.Line < 1 || b.Column < 1 {
			continue
		}
		key := position(from.ID, b.Line, b.Column)
		replacements[key] = knowledgeEdge{From: from.ID, To: target.ID, Kind: "call_typed", Line: b.Line, Column: b.Column, Confidence: "type_binding", Evidence: "go/types binding in successfully checked indexed package; not runtime or full project build proof"}
	}
	edges := g.Edges[:0]
	for _, edge := range g.Edges {
		if edge.Kind == "call_candidate" {
			if _, ok := replacements[position(edge.From, edge.Line, edge.Column)]; ok {
				g.Code.Calls--
				continue
			}
		}
		edges = append(edges, edge)
	}
	g.Edges = edges
	accepted := map[string]bool{}
	keys := []string{}
	for key := range replacements {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if len(g.Edges) >= 6500 {
			g.Truncated = true
			break
		}
		g.Edges = append(g.Edges, replacements[key])
		accepted[key] = true
		g.Code.TypedCalls++
	}
	unresolved := g.Code.UnresolvedRelations[:0]
	resolvedDiagnostics := map[string]bool{}
	filePaths := map[string]string{}
	for _, n := range g.Nodes {
		if n.Kind != "symbol" {
			filePaths[n.ID] = n.Path
		}
	}
	for _, r := range g.Code.UnresolvedRelations {
		if r.Kind == "call" && accepted[position(r.From, r.Line, r.Column)] {
			g.Code.Unresolved--
			resolvedDiagnostics[knowledgeUnresolvedCallDiagnostic(filePaths[r.File], r)] = true
			continue
		}
		unresolved = append(unresolved, r)
	}
	g.Code.UnresolvedRelations = unresolved
	diagnostics := g.Code.Diagnostics[:0]
	for _, d := range g.Code.Diagnostics {
		if !resolvedDiagnostics[d] {
			diagnostics = append(diagnostics, "AST: "+d)
		}
	}
	g.Code.Diagnostics = diagnostics
	for _, d := range result.Diagnostics {
		if len(g.Code.Diagnostics) < 200 {
			g.Code.Diagnostics = append(g.Code.Diagnostics, "Go types: "+d)
		}
	}
	result.Bindings = nil // Graph carries bound edges; avoid duplicating large raw data.
	g.Code.LanguageService = &result
	g.Warnings = append(g.Warnings, "可选Go类型服务只检查已索引快照。未加载项目构建标签、第三方模块或运行时代码；接口分派与函数变量仍未确认。")
}
