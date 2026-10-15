package server

import (
	"fmt"
	"go/ast"
	"go/token"
	"strconv"
	"strings"
)

// Extract declarations only. Neither a registered source pattern nor a handler
// name proves that this mux is served or that any request has executed.
func codeGoHTTPRoutes(tree *ast.File, fs *token.FileSet, u *codeUnit) {
	aliases := map[string]bool{}
	for _, im := range u.Imports {
		if im.Path == "net/http" && im.Alias != "." && im.Alias != "_" {
			aliases[im.Alias] = true
		}
	}
	writes := map[*ast.Object]int{}
	ast.Inspect(tree, func(n ast.Node) bool {
		if a, ok := n.(*ast.AssignStmt); ok {
			for _, lhs := range a.Lhs {
				if id, ok := lhs.(*ast.Ident); ok && id.Obj != nil {
					writes[id.Obj]++
				}
			}
		}
		return true
	})
	stdSelector := func(expr ast.Expr, name string) bool {
		s, ok := expr.(*ast.SelectorExpr)
		if !ok || s.Sel.Name != name {
			return false
		}
		id, ok := s.X.(*ast.Ident)
		return ok && id.Obj == nil && aliases[id.Name]
	}
	newMux := func(expr ast.Expr) bool {
		if call, ok := expr.(*ast.CallExpr); ok {
			return len(call.Args) == 0 && stdSelector(call.Fun, "NewServeMux")
		}
		return false
	}
	var mux func(*ast.Ident) bool
	mux = func(id *ast.Ident) bool {
		if id.Obj == nil {
			return false
		}
		switch d := id.Obj.Decl.(type) {
		case *ast.AssignStmt:
			if writes[id.Obj] != 1 || len(d.Lhs) != len(d.Rhs) {
				return false
			}
			for i, lhs := range d.Lhs {
				if v, ok := lhs.(*ast.Ident); ok && v.Obj == id.Obj {
					return newMux(d.Rhs[i])
				}
			}
		case *ast.ValueSpec:
			if writes[id.Obj] != 0 || len(d.Names) != len(d.Values) {
				return false
			}
			for i, name := range d.Names {
				if name.Obj == id.Obj {
					return newMux(d.Values[i])
				}
			}
		case *ast.Field:
			pointer, ok := d.Type.(*ast.StarExpr)
			return ok && writes[id.Obj] == 0 && stdSelector(pointer.X, "ServeMux")
		}
		return false
	}
	typeName := func(expr ast.Expr) string {
		if p, ok := expr.(*ast.StarExpr); ok {
			expr = p.X
		}
		if id, ok := expr.(*ast.Ident); ok {
			return id.Name
		}
		return ""
	}
	recordUnresolved := func(pos token.Pos, reason string) {
		if len(u.UnresolvedWeb) < 256 {
			p := fs.Position(pos)
			u.UnresolvedWeb = append(u.UnresolvedWeb, codeWebUnresolved{Kind: "route", Reason: reason, Line: p.Line, Column: p.Column})
		} else {
			u.WebReferencesTruncated = true
		}
	}
	var body func(ast.Node, string)
	body = func(node ast.Node, caller string) {
		ast.Inspect(node, func(n ast.Node) bool {
			if n == nil {
				return false
			}
			if literal, ok := n.(*ast.FuncLit); ok {
				p := fs.Position(literal.Pos())
				body(literal.Body, fmt.Sprintf("%s.anonymous@%d:%d", caller, p.Line, p.Column))
				return false
			}
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "HandleFunc" {
				return true
			}
			owner, simple := selector.X.(*ast.Ident)
			if !simple || !(stdSelector(call.Fun, "HandleFunc") || mux(owner)) {
				recordUnresolved(call.Pos(), "unresolved_http_mux")
				return true
			}
			if len(call.Args) != 2 {
				recordUnresolved(call.Pos(), "unsupported_route_arguments")
				return true
			}
			literal, ok := call.Args[0].(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				recordUnresolved(call.Pos(), "dynamic_route_pattern")
				return true
			}
			pattern, err := strconv.Unquote(literal.Value)
			parts := strings.Fields(pattern)
			method, target := "", ""
			if len(parts) == 1 {
				target = parts[0]
			} else if len(parts) == 2 {
				method, target = parts[0], parts[1]
			}
			if err != nil || len(pattern) > 2048 || !strings.HasPrefix(target, "/") {
				recordUnresolved(call.Pos(), "unsupported_route_pattern")
				return true
			}
			handler := ""
			switch h := call.Args[1].(type) {
			case *ast.Ident:
				if h.Obj != nil {
					if _, ok := h.Obj.Decl.(*ast.FuncDecl); ok {
						handler = h.Name
					}
				}
			case *ast.SelectorExpr:
				if receiver, ok := h.X.(*ast.Ident); ok && receiver.Obj != nil {
					if field, ok := receiver.Obj.Decl.(*ast.Field); ok {
						if typ := typeName(field.Type); typ != "" {
							handler = typ + "." + h.Sel.Name
						}
					}
				}
			case *ast.FuncLit:
				p := fs.Position(h.Pos())
				handler = fmt.Sprintf("%s.anonymous@%d:%d", caller, p.Line, p.Column)
			}
			if handler == "" {
				recordUnresolved(call.Pos(), "unresolved_route_handler")
			}
			if len(u.Routes) < 256 {
				p := fs.Position(call.Pos())
				u.Routes = append(u.Routes, codeHTTPRoute{Pattern: pattern, Method: method, Path: target, Caller: caller, Handler: handler, Line: p.Line, Column: p.Column})
			} else {
				u.WebReferencesTruncated = true
			}
			return true
		})
	}
	for _, decl := range tree.Decls {
		if fun, ok := decl.(*ast.FuncDecl); ok && fun.Body != nil {
			caller := fun.Name.Name
			if fun.Recv != nil && len(fun.Recv.List) > 0 {
				caller = typeName(fun.Recv.List[0].Type) + "." + caller
			}
			body(fun.Body, caller)
		}
	}
}
