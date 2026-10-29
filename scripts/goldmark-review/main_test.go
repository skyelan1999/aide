package goldmarkreview

import (
	"fmt"
	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/parser"
	"strings"
	"testing"
	"time"
)

func TestEmphasisReferenceSemantics(t *testing.T) {
	for _, tc := range []struct {
		s          string
		emph, refs int
	}{
		{"*ok* **strong** _yes_", 3, 0},
		{"a *unclosed " + strings.Repeat("words ", 70) + "*closed*", 1, 0},
		{"[a]: /one \"One\"\n[b]: /two\n\n[a] [b]", 0, 2},
		{"[a]: /one\n  \"Long title\"\n[b]: /two\nbody **kept**", 1, 2},
		{strings.Repeat("*a ", 20000), 0, 0},
	} {
		src := []byte(tc.s)
		doc := parser.New().Parse(src)
		em, refs := 0, 0
		ast.Walk(doc, func(n ast.Node, enter bool) (ast.WalkStatus, error) {
			if enter {
				switch n.(type) {
				case *ast.Emphasis, *ast.Strong:
					em++
				case *ast.LinkReferenceDefinition:
					refs++
				}
			}
			return ast.WalkContinue, nil
		})
		if em != tc.emph || refs != tc.refs {
			t.Fatalf("counts emphasis=%d references=%d want %d/%d", em, refs, tc.emph, tc.refs)
		}
	}
}
func TestReferenceCompaction(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 6000; i++ {
		fmt.Fprintf(&b, "[id%d]: /url/%d\n", i, i)
	}
	b.WriteString("retained paragraph\nwith **strong** and [id42]\n")
	src := []byte(b.String())
	doc := parser.New().Parse(src)
	count := 0
	paragraphs := 0
	ast.Walk(doc, func(n ast.Node, enter bool) (ast.WalkStatus, error) {
		if enter {
			if r, ok := n.(*ast.LinkReferenceDefinition); ok {
				want := fmt.Sprintf("/url/%d", count)
				if string(r.Destination.Bytes(src)) != want {
					t.Fatalf("reference %d destination", count)
				}
				count++
			}
			if p, ok := n.(*ast.Paragraph); ok {
				paragraphs++
				if !strings.Contains(string(src[p.Source()[0].Start:p.Source()[0].Stop]), "retained paragraph") {
					t.Fatal("paragraph source lost")
				}
			}
		}
		return ast.WalkContinue, nil
	})
	if count != 6000 || paragraphs != 1 {
		t.Fatalf("refs/paragraphs %d/%d", count, paragraphs)
	}
}
func BenchmarkEmphasis(b *testing.B) {
	for _, n := range []int{2000, 20000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			src := []byte(strings.Repeat("*a ", n))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				parser.New().Parse(src)
			}
		})
	}
}
func BenchmarkReferences(b *testing.B) {
	for _, n := range []int{2000, 20000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			var s strings.Builder
			for i := 0; i < n; i++ {
				fmt.Fprintf(&s, "[r%d]: /%d\n", i, i)
			}
			src := []byte(s.String())
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				parser.New().Parse(src)
			}
		})
	}
}
func TestParseBoundedInput(t *testing.T) {
	start := time.Now()
	parser.New().Parse([]byte(strings.Repeat("*a ", 21000)))
	t.Log("64KiB pathological emphasis:", time.Since(start))
}
