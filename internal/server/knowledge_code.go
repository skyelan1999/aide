package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Parsers are product-owned assets, never loaded from the workspace.
//
//go:embed code_analysis/*
var codeAssets embed.FS

type knowledgeCodeFile struct {
	ID     string        `json:"id"`
	Text   string        `json:"text"`
	Node   knowledgeNode `json:"-"`
	Module string        `json:"-"`
}
type codeSymbol struct {
	Key       string `json:"key"`
	Parent    string `json:"parent"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Line      int    `json:"line"`
	EndLine   int    `json:"endLine"`
	Signature string `json:"signature"`
	Anonymous bool   `json:"anonymous"`
}
type codeCall struct {
	Caller    string `json:"caller"`
	Name      string `json:"name"`
	Qualifier string `json:"qualifier"`
	Line      int    `json:"line"`
	Column    int    `json:"column,omitempty"`
	Dynamic   bool   `json:"dynamic"`
}
type codeImport struct {
	Alias string `json:"alias"`
	Path  string `json:"path"`
	Name  string `json:"name"`
}
type codeUnit struct {
	File        string       `json:"file"`
	Language    string       `json:"language"`
	Package     string       `json:"package"`
	Symbols     []codeSymbol `json:"symbols"`
	Calls       []codeCall   `json:"calls"`
	Imports     []codeImport `json:"imports"`
	Diagnostics []string     `json:"diagnostics"`
}
type knowledgeCodeSummary struct {
	LanguageService     *goTypeResult                 `json:"languageService,omitempty"`
	TypedCalls          int                           `json:"typedCalls,omitempty"`
	Files               int                           `json:"files"`
	Symbols             int                           `json:"symbols"`
	Calls               int                           `json:"calls"`
	Unresolved          int                           `json:"unresolved"`
	Diagnostics         []string                      `json:"diagnostics"`
	Engine              string                        `json:"engine"`
	UnresolvedImports   int                           `json:"unresolvedImports"`
	UnresolvedRelations []knowledgeUnresolvedRelation `json:"unresolvedRelations,omitempty"`
	RelationsTruncated  bool                          `json:"relationsTruncated,omitempty"`
}

type knowledgeUnresolvedRelation struct {
	From   string `json:"from"`
	File   string `json:"file"`
	Line   int    `json:"line,omitempty"`
	Column int    `json:"column,omitempty"`
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Reason string `json:"reason"`
}

func knowledgeUnresolvedCallDiagnostic(file string, r knowledgeUnresolvedRelation) string {
	return fmt.Sprintf("%s:%d:%d %s（外部、动态或歧义调用；未连线）", file, r.Line, r.Column, r.Name)
}

func codeLanguage(p string) string {
	switch strings.ToLower(path.Ext(p)) {
	case ".go":
		return "Go"
	case ".js", ".mjs", ".cjs":
		return "JavaScript"
	case ".py":
		return "Python"
	case ".ts", ".tsx", ".jsx":
		return "unsupported"
	}
	return ""
}
func codeModule(root *os.Root) string {
	info, e := root.Lstat("go.mod")
	if e != nil || !info.Mode().IsRegular() || info.Size() > 8192 {
		return ""
	}
	f, e := root.Open("go.mod")
	if e != nil {
		return ""
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, 8192))
	if e != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "module ") {
			return strings.Trim(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "module ")), "\"")
		}
	}
	return ""
}

type codeOutput struct{ bytes.Buffer }

func (b *codeOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 8*1024*1024 {
		return 0, fmt.Errorf("解析输出超过8MiB")
	}
	return b.Buffer.Write(p)
}
func codeText(root *os.Root, p string) (string, error) {
	f, e := root.Open(p)
	if e != nil {
		return "", e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, 256*1024+1))
	if e != nil {
		return "", e
	}
	if len(b) > 256*1024 {
		return "", fmt.Errorf("超过256KiB分析上限")
	}
	return string(b), nil
}
func codeID(file string, s codeSymbol) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%d", file, s.Key, s.Line)))
	return "C" + hex.EncodeToString(h[:6])
}
func codeGo(f knowledgeCodeFile) codeUnit {
	u := codeUnit{File: f.ID, Language: "Go"}
	fs := token.NewFileSet()
	tree, e := parser.ParseFile(fs, f.Node.Path, f.Text, 0)
	if e != nil {
		u.Diagnostics = append(u.Diagnostics, "Go语法解析失败；该文件未建立调用关系")
		return u
	}
	u.Package = tree.Name.Name
	for _, i := range tree.Imports {
		p, _ := strconv.Unquote(i.Path.Value)
		alias := path.Base(p)
		if i.Name != nil {
			alias = i.Name.Name
		}
		u.Imports = append(u.Imports, codeImport{alias, p, "*"})
	}
	typename := func(e ast.Expr) string {
		if p, ok := e.(*ast.StarExpr); ok {
			e = p.X
		}
		if i, ok := e.(*ast.Ident); ok {
			return i.Name
		}
		return ""
	}
	var body func(ast.Node, string)
	body = func(node ast.Node, caller string) {
		ast.Inspect(node, func(n ast.Node) bool {
			if n == nil {
				return false
			}
			if literal, ok := n.(*ast.FuncLit); ok {
				line := fs.Position(literal.Pos()).Line
				key := fmt.Sprintf("%s.anonymous@%d:%d", caller, line, fs.Position(literal.Pos()).Column)
				u.Symbols = append(u.Symbols, codeSymbol{Key: key, Parent: caller, Name: fmt.Sprintf("anonymous@%d", line), Kind: "closure", Line: line, EndLine: fs.Position(literal.End()).Line, Signature: "func (…) { … }", Anonymous: true})
				body(literal.Body, key)
				return false
			}
			if c, ok := n.(*ast.CallExpr); ok {
				call := codeCall{Caller: caller, Line: fs.Position(c.Pos()).Line, Column: fs.Position(c.Pos()).Column, Name: "<dynamic>", Dynamic: true}
				switch fun := c.Fun.(type) {
				case *ast.Ident:
					call.Name = fun.Name
					call.Dynamic = false
					if fun.Obj != nil {
						if _, ok := fun.Obj.Decl.(*ast.FuncDecl); !ok {
							call.Dynamic = true
							call.Qualifier = "<variable>"
						}
					}
				case *ast.SelectorExpr:
					call.Name = fun.Sel.Name
					if i, ok := fun.X.(*ast.Ident); ok {
						call.Qualifier = i.Name
					} else {
						call.Qualifier = "<expression>"
					}
				}
				u.Calls = append(u.Calls, call)
			}
			return true
		})
	}
	for _, d := range tree.Decls {
		switch d := d.(type) {
		case *ast.GenDecl:
			for _, s := range d.Specs {
				if t, ok := s.(*ast.TypeSpec); ok {
					u.Symbols = append(u.Symbols, codeSymbol{Key: t.Name.Name, Name: t.Name.Name, Kind: "type", Line: fs.Position(t.Pos()).Line, EndLine: fs.Position(t.End()).Line, Signature: "type " + t.Name.Name})
				}
			}
		case *ast.FuncDecl:
			name, key, parent := d.Name.Name, d.Name.Name, ""
			kind := "function"
			if d.Recv != nil && len(d.Recv.List) > 0 {
				parent = typename(d.Recv.List[0].Type)
				key = parent + "." + name
				kind = "method"
			}
			end := d.End()
			if d.Body != nil {
				end = d.Body.Pos()
			}
			startOffset, endOffset := fs.Position(d.Pos()).Offset, fs.Position(end).Offset
			u.Symbols = append(u.Symbols, codeSymbol{Key: key, Parent: parent, Name: name, Kind: kind, Line: fs.Position(d.Pos()).Line, EndLine: fs.Position(d.End()).Line, Signature: knowledgeClip(f.Text[startOffset:endOffset], 240)})
			if d.Body != nil {
				body(d.Body, key)
			}
		}
	}
	return u
}
func codeParseBatch(ctx context.Context, lang string, files []knowledgeCodeFile) ([]codeUnit, error) {
	if len(files) == 0 {
		return nil, nil
	}
	raw, _ := json.Marshal(files)
	var cmd *exec.Cmd
	if lang == "Python" {
		script, _ := codeAssets.ReadFile("code_analysis/python.py")
		cmd = exec.CommandContext(ctx, "python3", "-I", "-c", string(script))
	} else {
		parserJS, _ := codeAssets.ReadFile("code_analysis/acorn.cjs")
		script, _ := codeAssets.ReadFile("code_analysis/javascript.cjs")
		prefix := "const acorn=(()=>{const module={exports:{}};const exports=module.exports;\n" + string(parserJS) + "\nreturn module.exports;})();\n"
		encoded, _ := json.Marshal(string(raw))
		program := strings.Replace(string(script), "const files=JSON.parse(fs.readFileSync(0,'utf8')), out=[];", "const files=JSON.parse("+string(encoded)+"), out=[];", 1)
		cmd = exec.CommandContext(ctx, "node", "--max-old-space-size=128", "-")
		cmd.Stdin = strings.NewReader(prefix + program)
	}
	cmd.Dir = os.TempDir()
	if cmd.Stdin == nil {
		cmd.Stdin = bytes.NewReader(raw)
	}
	// Exclude workspace module paths and runtime hook variables.
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "LANG=C.UTF-8"}
	var output codeOutput
	cmd.Stdout = &output
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s解析器不可用或超时；未执行项目代码", lang)
	}
	var units []codeUnit
	if e := json.Unmarshal(output.Bytes(), &units); e != nil {
		return nil, fmt.Errorf("解析器输出无效")
	}
	return units, nil
}
func codePackage(f knowledgeCodeFile, u codeUnit) string {
	return f.Node.Root + "\x00" + f.Node.Source + "\x00" + path.Dir(f.Node.Path) + "\x00" + u.Package
}
func codeSnippet(lines []string, line, end int) string {
	if line < 1 || line > len(lines) {
		return ""
	}
	end = min(end, line+35, len(lines))
	return knowledgeClip(strings.Join(lines[line-1:end], "\n"), 1500)
}

var knowledgeCodeGate = make(chan struct{}, 2)

func expandKnowledgeCode(ctx context.Context, g *knowledgeGraph, files []knowledgeCodeFile) {
	summary := &knowledgeCodeSummary{Files: len(files), Diagnostics: []string{}, Engine: "ast-name-resolution"}
	recordUnresolved := func(r knowledgeUnresolvedRelation) {
		if len(summary.UnresolvedRelations) < 1000 {
			summary.UnresolvedRelations = append(summary.UnresolvedRelations, r)
		} else {
			summary.RelationsTruncated = true
		}
	}
	g.Code = summary
	select {
	case knowledgeCodeGate <- struct{}{}:
		defer func() { <-knowledgeCodeGate }()
	case <-ctx.Done():
		g.Truncated = true
		summary.Diagnostics = append(summary.Diagnostics, "代码分析等待超时；请稍后刷新")
		return
	}
	sourceLines := map[string][]string{}
	for _, f := range files {
		sourceLines[f.ID] = strings.Split(f.Text, "\n")
	}
	units := []codeUnit{}
	byFile := map[string]knowledgeCodeFile{}
	js, py := []knowledgeCodeFile{}, []knowledgeCodeFile{}
	for _, f := range files {
		if ctx.Err() != nil {
			g.Truncated = true
			summary.Diagnostics = append(summary.Diagnostics, "代码分析超时，索引未完整")
			break
		}
		f.Node.ContentHash = hash([]byte(f.Text))
		f.Node.Language = codeLanguage(f.Node.Path)
		for i := range g.Nodes {
			if g.Nodes[i].ID == f.ID {
				g.Nodes[i].ContentHash = f.Node.ContentHash
				g.Nodes[i].Language = f.Node.Language
				break
			}
		}
		byFile[f.ID] = f
		switch codeLanguage(f.Node.Path) {
		case "Go":
			units = append(units, codeGo(f))
		case "JavaScript":
			js = append(js, f)
		case "Python":
			py = append(py, f)
		}
	}
	for _, batch := range []struct {
		lang  string
		files []knowledgeCodeFile
	}{{"JavaScript", js}, {"Python", py}} {
		child, cancel := context.WithTimeout(ctx, 3*time.Second)
		parsed, e := codeParseBatch(child, batch.lang, batch.files)
		cancel()
		if e != nil {
			summary.Diagnostics = append(summary.Diagnostics, e.Error())
		} else {
			units = append(units, parsed...)
		}
	}
	// AST evidence locates declarations; name resolution is deliberately conservative.
	type decl struct {
		node knowledgeNode
		s    codeSymbol
		unit codeUnit
	}
	declarations := map[string]decl{}
	symbolNodes := map[string]decl{}
	ambiguous := map[string]bool{}
	byPackage := map[string][]decl{}
	fileUnits := map[string]codeUnit{}
	for _, u := range units {
		f := byFile[u.File]
		fileUnits[u.File] = u
		for _, d := range u.Diagnostics {
			if len(summary.Diagnostics) < 100 {
				summary.Diagnostics = append(summary.Diagnostics, f.Node.Path+": "+d)
			}
		}
		for _, s := range u.Symbols {
			if summary.Symbols >= 1200 {
				g.Truncated = true
				break
			}
			n := f.Node
			n.ID = codeID(f.ID, s)
			n.Name = s.Key
			n.Kind = "symbol"
			n.Language = u.Language
			n.SymbolKind = s.Kind
			n.Line = s.Line
			n.EndLine = s.EndLine
			n.ParentFile = f.ID
			n.Signature = s.Signature
			n.Text = codeSnippet(sourceLines[f.ID], s.Line, s.EndLine)
			n.Size = 0
			g.Nodes = append(g.Nodes, n)
			d := decl{n, s, u}
			key := u.File + "\x00" + s.Key
			symbolNodes[n.ID] = d
			if _, exists := declarations[key]; exists {
				delete(declarations, key)
				ambiguous[key] = true
			} else if !ambiguous[key] {
				declarations[key] = d
			}
			byPackage[codePackage(f, u)] = append(byPackage[codePackage(f, u)], d)
			summary.Symbols++
		}
	}
	fileLookup := func(from knowledgeCodeFile, p string) *knowledgeCodeFile {
		for _, f := range files {
			if f.Node.Root == from.Node.Root && f.Node.Source == from.Node.Source && f.Node.Path == p {
				copy := f
				return &copy
			}
		}
		return nil
	}
	resolveImport := func(f knowledgeCodeFile, u codeUnit, im codeImport) *knowledgeCodeFile {
		if u.Language == "Go" {
			prefix := strings.TrimSuffix(f.Module, "/") + "/"
			if f.Module == "" || (im.Path != f.Module && !strings.HasPrefix(im.Path, prefix)) {
				return nil
			}
			dir := strings.TrimPrefix(im.Path, prefix)
			if im.Path == f.Module {
				dir = "."
			}
			for _, other := range files {
				if other.Node.Root == f.Node.Root && other.Node.Source == f.Node.Source && path.Dir(other.Node.Path) == dir && fileUnits[other.ID].Package != "" {
					copy := other
					return &copy
				}
			}
			return nil
		}
		var base string
		if u.Language == "JavaScript" {
			if !strings.HasPrefix(im.Path, ".") {
				return nil
			}
			base = path.Clean(path.Join(path.Dir(f.Node.Path), im.Path))
			for _, p := range []string{base, base + ".js", base + ".mjs", base + ".cjs", path.Join(base, "index.js")} {
				if other := fileLookup(f, p); other != nil {
					return other
				}
			}
			return nil
		}
		module := im.Path
		dots := len(module) - len(strings.TrimLeft(module, "."))
		if dots > 0 {
			base = path.Dir(f.Node.Path)
			for i := 1; i < dots; i++ {
				base = path.Dir(base)
			}
			module = strings.TrimLeft(module, ".")
			base = path.Join(base, strings.ReplaceAll(module, ".", "/"))
		} else {
			base = strings.ReplaceAll(module, ".", "/")
		}
		for _, p := range []string{base + ".py", path.Join(base, "__init__.py")} {
			if other := fileLookup(f, p); other != nil {
				return other
			}
		}
		return nil
	}
	addEdge := func(e knowledgeEdge) bool {
		if len(g.Edges) < 6500 {
			g.Edges = append(g.Edges, e)
			return true
		} else {
			g.Truncated = true
		}
		return false
	}
	// Iterate stable AST order rather than map order: IDs and reference lists stay predictable.
	for _, u := range units {
		f := byFile[u.File]
		pool := byPackage[codePackage(f, u)]
		for _, s := range u.Symbols {
			d, ok := symbolNodes[codeID(f.ID, s)]
			if !ok {
				continue
			}
			parent := f.ID
			if p, ok := declarations[u.File+"\x00"+s.Parent]; s.Parent != "" && ok {
				parent = p.node.ID
			} else if u.Language == "Go" && s.Parent != "" {
				for _, p := range pool {
					if p.s.Key == s.Parent && p.s.Kind == "type" {
						parent = p.node.ID
						break
					}
				}
			}
			addEdge(knowledgeEdge{From: parent, To: d.node.ID, Kind: "defines", Line: s.Line, Evidence: "AST declaration", Confidence: "source_declaration"})
		}
		for _, im := range u.Imports {
			if other := resolveImport(f, u, im); other != nil {
				addEdge(knowledgeEdge{From: f.ID, To: other.ID, Kind: "imports", Evidence: "source import path matched inside current index; not module loader proof", Confidence: "source_path"})
			} else {
				summary.UnresolvedImports++
				recordUnresolved(knowledgeUnresolvedRelation{From: f.ID, File: f.ID, Name: im.Path, Kind: "import", Reason: "outside_index_or_unresolved_path"})
			}
		}
		for _, c := range u.Calls {
			from := f.ID
			if d, ok := declarations[u.File+"\x00"+c.Caller]; ok {
				from = d.node.ID
			}
			candidates := []decl{}
			evidence := "AST call; name candidate, not runtime/type proof"
			if c.Qualifier == "" && !c.Dynamic {
				// Nearest lexical declaration first; duplicates remain unresolved.
				scope := c.Caller
				for {
					key := c.Name
					if scope != "" {
						key = scope + "." + c.Name
					}
					if d, ok := declarations[u.File+"\x00"+key]; ok {
						candidates = append(candidates, d)
						break
					}
					i := strings.LastIndex(scope, ".")
					if i < 0 {
						if scope == "" {
							break
						}
						scope = ""
					} else {
						scope = scope[:i]
					}
				}
				if len(candidates) == 0 && u.Language == "Go" {
					for _, d := range pool {
						if d.s.Key == c.Name && d.s.Kind == "function" {
							candidates = append(candidates, d)
						}
					}
				}
				if len(candidates) == 0 {
					for _, im := range u.Imports {
						if im.Alias == c.Name && im.Name != "*" && im.Name != "default" {
							if other := resolveImport(f, u, im); other != nil {
								if d, ok := declarations[other.ID+"\x00"+im.Name]; ok {
									candidates = append(candidates, d)
								}
							}
						}
					}
				}
			} else if c.Qualifier != "<variable>" {
				imported := false
				for _, im := range u.Imports {
					if im.Alias == c.Qualifier {
						imported = true
						if other := resolveImport(f, u, im); other != nil {
							if u.Language == "Go" {
								for _, d := range byPackage[codePackage(*other, fileUnits[other.ID])] {
									if d.s.Key == c.Name {
										candidates = append(candidates, d)
									}
								}
							} else if d, ok := declarations[other.ID+"\x00"+c.Name]; ok {
								candidates = append(candidates, d)
							}
						}
					}
				}
				if !imported {
					if u.Language == "Go" {
						for _, d := range pool {
							if d.s.Name == c.Name && d.s.Kind == "method" {
								candidates = append(candidates, d)
							}
						}
					} else if c.Qualifier == "self" || c.Qualifier == "cls" || c.Qualifier == "this" {
						scope := c.Caller
						if i := strings.LastIndex(scope, "."); i >= 0 {
							scope = scope[:i]
							if d, ok := declarations[u.File+"\x00"+scope+"."+c.Name]; ok {
								candidates = append(candidates, d)
							}
						}
					}
				}
			}
			if len(candidates) == 1 {
				if addEdge(knowledgeEdge{From: from, To: candidates[0].node.ID, Kind: "call_candidate", Line: c.Line, Column: c.Column, Evidence: evidence, Confidence: "name_candidate"}) {
					summary.Calls++
				}
			} else {
				summary.Unresolved++
				reason := "outside_index_or_unknown_binding"
				if c.Dynamic {
					reason = "dynamic_binding"
				} else if len(candidates) > 1 {
					reason = "ambiguous_name"
				}
				name := c.Name
				if c.Qualifier != "" {
					name = c.Qualifier + "." + name
				}
				relation := knowledgeUnresolvedRelation{From: from, File: f.ID, Line: c.Line, Column: c.Column, Name: name, Kind: "call", Reason: reason}
				recordUnresolved(relation)
				if len(summary.Diagnostics) < 100 {
					summary.Diagnostics = append(summary.Diagnostics, knowledgeUnresolvedCallDiagnostic(f.Node.Path, relation))
				}
			}
		}
	}
	sort.Strings(summary.Diagnostics)
	g.Warnings = append(g.Warnings, "代码视图：Go/JavaScript/Python AST。调用线是静态名称候选，不是类型检查或运行时跟踪；反射、动态分派、函数变量、外部依赖与歧义调用可能缺失。TypeScript/JSX暂不解析。每文件最多256KiB，总读取4MiB/80文件/1200符号。")
}
