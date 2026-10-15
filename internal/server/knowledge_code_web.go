package server

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"
)

func knowledgeWebArea(n knowledgeNode) string { return n.Root + "\x00" + n.Source + "\x00" + n.Region }

// Static source candidates only: root-relative browser paths may correspond
// to the page directory or the selected source root. Ambiguity rejects both.
func knowledgeWebScriptFile(page knowledgeCodeFile, src string, files map[string]knowledgeCodeFile) string {
	u, err := url.Parse(src)
	if err != nil || u.IsAbs() || u.Host != "" || strings.HasPrefix(src, "//") {
		return ""
	}
	candidates := map[string]bool{}
	if strings.HasPrefix(src, "/") {
		rel := knowledgeLocalReference(strings.TrimPrefix(src, "/"))
		if rel == "" {
			return ""
		}
		candidates[path.Clean(rel)] = true
		candidates[path.Clean(path.Join(path.Dir(page.Node.Path), rel))] = true
	} else if rel := knowledgeLocalReference(src); rel != "" {
		candidates[path.Clean(path.Join(path.Dir(page.Node.Path), rel))] = true
	}
	matches := []string{}
	for id, f := range files {
		if knowledgeWebArea(f.Node) == knowledgeWebArea(page.Node) && codeLanguage(f.Node.Path) == "JavaScript" && candidates[f.Node.Path] {
			matches = append(matches, id)
		}
	}
	if len(matches) == 1 {
		return matches[0]
	}
	return ""
}

// Only compile source patterns into the standard library matcher. The handler
// is product-owned and never invoked; no workspace code is executed.
func knowledgeWebMux(pattern string) (mux *http.ServeMux) {
	defer func() {
		if recover() != nil {
			mux = nil
		}
	}()
	mux = http.NewServeMux()
	mux.HandleFunc(pattern, func(http.ResponseWriter, *http.Request) {})
	return mux
}

func appendKnowledgeWeb(ctx context.Context, g *knowledgeGraph, files map[string]knowledgeCodeFile, units []codeUnit, addEdge func(knowledgeEdge) bool, unresolved func(knowledgeUnresolvedRelation)) {
	summary := g.Code
	diagnostic := func(f knowledgeCodeFile, line, column int, kind, reason string) {
		summary.WebUnresolved++
		unresolved(knowledgeUnresolvedRelation{From: f.ID, File: f.ID, Line: line, Column: column, Kind: "web_" + kind, Reason: reason})
		if len(summary.Diagnostics) < 100 {
			summary.Diagnostics = append(summary.Diagnostics, fmt.Sprintf("%s:%d:%d web %s: %s", f.Node.Path, line, column, kind, reason))
		}
	}
	symbols := map[string][]knowledgeNode{}
	columns := map[string]int{}
	for _, u := range units {
		for _, s := range u.Symbols {
			columns[u.File+"\x00"+s.Key] = s.Column
		}
	}
	for _, n := range g.Nodes {
		if n.Kind == "symbol" {
			symbols[n.ParentFile+"\x00"+n.Name] = append(symbols[n.ParentFile+"\x00"+n.Name], n)
		}
	}
	lookup := func(file, key string) knowledgeNode {
		if nodes := symbols[file+"\x00"+key]; len(nodes) == 1 {
			return nodes[0]
		}
		return knowledgeNode{}
	}
	link := func(from, to knowledgeNode, kind, confidence string, line, column int, evidence string) {
		if from.ID != "" && to.ID != "" && addEdge(knowledgeEdge{From: from.ID, To: to.ID, Kind: kind, Confidence: confidence, Line: line, Column: column, Evidence: evidence}) {
			summary.WebRelations++
		}
	}
	lineCache := map[string][]string{}
	newNode := func(f knowledgeCodeFile, key, name, kind string, line int) knowledgeNode {
		if ctx.Err() != nil || summary.Symbols >= 1200 {
			g.Truncated = true
			return knowledgeNode{}
		}
		s := codeSymbol{Key: key, Line: line}
		n := f.Node
		n.ID = codeID(f.ID, s)
		n.Name = name
		n.Kind = "symbol"
		n.SymbolKind = kind
		n.ParentFile = f.ID
		n.Line = line
		n.EndLine = line
		n.Size = 0
		if lineCache[f.ID] == nil {
			lineCache[f.ID] = strings.Split(f.Text, "\n")
		}
		n.Text = codeSnippet(lineCache[f.ID], line, line)
		g.Nodes = append(g.Nodes, n)
		summary.Symbols++
		addEdge(knowledgeEdge{From: f.ID, To: n.ID, Kind: "defines", Line: line, Evidence: "source declaration", Confidence: "source_declaration"})
		return n
	}
	type route struct {
		file        knowledgeCodeFile
		declaration codeHTTPRoute
		node        knowledgeNode
		mux         *http.ServeMux
	}
	routes := []route{}
	blocked := map[string]bool{}
	for _, u := range units {
		f := files[u.File]
		area := knowledgeWebArea(f.Node)
		if u.WebReferencesTruncated {
			g.Truncated = true
			summary.RelationsTruncated = true
			blocked[area] = true
		}
		for _, r := range u.UnresolvedWeb {
			diagnostic(f, r.Line, r.Column, r.Kind, r.Reason)
			if r.Kind == "route" && r.Reason == "dynamic_route_pattern" {
				blocked[area] = true
			}
		}
		for _, r := range u.Routes {
			if ctx.Err() != nil {
				g.Truncated = true
				return
			}
			mux := knowledgeWebMux(r.Pattern)
			if mux == nil {
				diagnostic(f, r.Line, r.Column, "route", "invalid_http_pattern")
				blocked[area] = true
				continue
			}
			n := newNode(f, fmt.Sprintf("route@%d:%d", r.Line, r.Column), r.Pattern, "http route", r.Line)
			if n.ID == "" {
				blocked[area] = true
				continue
			}
			routes = append(routes, route{f, r, n, mux})
			handlers := []knowledgeNode{}
			for _, other := range units {
				peer := files[other.File]
				if other.Language == "Go" && knowledgeWebArea(peer.Node) == area && codePackage(peer, other) == codePackage(f, u) && r.Handler != "" {
					if h := lookup(other.File, r.Handler); h.ID != "" {
						handlers = append(handlers, h)
					}
				}
			}
			if len(handlers) == 1 {
				link(n, handlers[0], "route_handler", "name_candidate", r.Line, r.Column, "Go HTTP registration handler source binding; not proof this mux is served")
			} else {
				diagnostic(f, r.Line, r.Column, "route", "missing_or_ambiguous_handler")
			}
		}
	}
	for _, u := range units {
		if u.Language != "JavaScript" {
			continue
		}
		f := files[u.File]
		for _, e := range u.Events {
			if ctx.Err() != nil {
				g.Truncated = true
				return
			}
			callback := lookup(f.ID, e.Callback)
			if callback.ID == "" {
				diagnostic(f, e.Line, e.Column, "event", "missing_or_ambiguous_callback")
				continue
			}
			binding := newNode(f, fmt.Sprintf("event@%d:%d", e.Line, e.Column), "#"+e.Target+" · "+e.Event, "event binding", e.Line)
			link(binding, callback, "event_callback", "source_binding", e.Line, e.Column, "JS AST lexical callback binding; registration may not have executed")
			matched := false
			for _, page := range units {
				if page.Language != "HTML" || page.HTMLBase {
					continue
				}
				h := files[page.File]
				if knowledgeWebArea(h.Node) != knowledgeWebArea(f.Node) {
					continue
				}
				loaded := false
				for _, script := range page.Scripts {
					if knowledgeWebScriptFile(h, script.Path, files) == f.ID {
						loaded = true
					}
				}
				if loaded {
					if button := lookup(h.ID, "button#"+e.Target); button.ID != "" {
						matched = true
						link(button, binding, "dom_event", "dom_selector_candidate", button.Line, max(1, columns[h.ID+"\x00button#"+e.Target]), "HTML ID and JS selector matched through declared script "+f.Node.Path+fmt.Sprintf(":%d; not runtime DOM proof", e.Line))
					}
				}
			}
			if !matched {
				diagnostic(f, e.Line, e.Column, "event", "no_unique_button_in_declared_script_page")
			}
		}
		for _, r := range u.Requests {
			if ctx.Err() != nil {
				g.Truncated = true
				return
			}
			caller := f.Node
			if r.Caller != "" {
				caller = lookup(f.ID, r.Caller)
			}
			n := newNode(f, fmt.Sprintf("request@%d:%d", r.Line, r.Column), r.Method+" "+r.URL, "http request", r.Line)
			confidence, evidence := "source_declaration", "JS AST fetch literal; not a network observation"
			if r.Wrapper != "" {
				confidence = "source_template"
				evidence = fmt.Sprintf("JS lexical wrapper %s at %s:%d; fixed-prefix URL and method substituted at this call site; not a network observation", r.Wrapper, f.Node.Path, r.WrapperLine)
			}
			link(caller, n, "http_request", confidence, r.Line, r.Column, evidence)
			parsed, err := url.Parse(r.URL)
			if err != nil || parsed.IsAbs() || parsed.Host != "" || !strings.HasPrefix(r.URL, "/") || strings.HasPrefix(r.URL, "//") || parsed.RawPath != "" {
				diagnostic(f, r.Line, r.Column, "http", "nonlocal_or_dynamic_request_origin")
				continue
			}
			if blocked[knowledgeWebArea(f.Node)] {
				diagnostic(f, r.Line, r.Column, "http", "route_set_incomplete")
				continue
			}
			req, err := http.NewRequestWithContext(ctx, r.Method, r.URL, nil)
			if err != nil {
				diagnostic(f, r.Line, r.Column, "http", "invalid_request_literal")
				continue
			}
			matches := []route{}
			for _, candidate := range routes {
				if ctx.Err() != nil {
					g.Truncated = true
					return
				}
				if knowledgeWebArea(candidate.file.Node) != knowledgeWebArea(f.Node) {
					continue
				}
				_, pattern := candidate.mux.Handler(req)
				if pattern != "" {
					matches = append(matches, candidate)
				}
			}
			if len(matches) != 1 {
				diagnostic(f, r.Line, r.Column, "http", "missing_or_ambiguous_route")
				continue
			}
			match := matches[0]
			// A canonicalization redirect is not a direct call to this handler.
			if path.Clean(parsed.Path) != strings.TrimSuffix(parsed.Path, "/") && parsed.Path != "/" || strings.HasSuffix(match.declaration.Path, "/") && parsed.Path+"/" == match.declaration.Path {
				diagnostic(f, r.Line, r.Column, "http", "route_redirect_not_direct_handler")
				continue
			}
			link(n, match.node, "http_route", "http_route_candidate", r.Line, r.Column, "Same-source unique HTTP method/path pattern "+match.declaration.Pattern+" at "+match.file.Node.Path+fmt.Sprintf(":%d; server origin is not runtime-proven", match.declaration.Line))
		}
	}
}
