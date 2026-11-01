package server

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

type codingFixtureCase struct{ Name, Spec, Broken, Fixed, Test string }

func codingFixtureCases() []codingFixtureCase {
	return []codingFixtureCase{
		{"Clamp", "Clamp(n,lo,hi)返回闭区间边界内的n，假定lo<=hi。", `func Clamp(n,lo,hi int) int { return lo }`, `func Clamp(n,lo,hi int) int { if n<lo{return lo};if n>hi{return hi};return n }`, `func TestClamp(t *testing.T){for n:=-20;n<=20;n++ {w:=n;if w< -3{w=-3};if w>7{w=7};if Clamp(n,-3,7)!=w{t.Fatal(n)}}}`},
		{"Page", "Page(total,offset,limit)返回[start,end)；负total视0，offset夹到[0,total]，负limit视0，end不超过total；不能整数溢出。", `func Page(total,offset,limit int)(int,int){return offset,offset+limit}`, `func Page(total,offset,limit int)(int,int){if total<0{total=0};if offset<0{offset=0};if offset>total{offset=total};if limit<0{limit=0};if limit>total-offset{limit=total-offset};return offset,offset+limit}`, `func TestPage(t *testing.T){for _,x:=range[][5]int{{10,-2,4,0,4},{10,8,99,8,10},{5,99,2,5,5},{-1,2,3,0,0},{8,2,-5,2,2},{10,1,int(^uint(0)>>1),1,10}}{a,b:=Page(x[0],x[1],x[2]);if a!=x[3]||b!=x[4]{t.Fatal(x,a,b)}}}`},
		{"Unique", "Unique(xs)稳定去重，保留首次顺序，不修改输入；nil输入可返回nil或空切片。", `func Unique(xs []int)[]int{return xs}`, `func Unique(xs []int)[]int{var out []int;seen:=map[int]bool{};for _,v:=range xs{if !seen[v]{seen[v]=true;out=append(out,v)}};return out}`, `func TestUnique(t *testing.T){x:=[]int{3,1,3,2,1};y:=Unique(x);if !reflect.DeepEqual(y,[]int{3,1,2})||!reflect.DeepEqual(x,[]int{3,1,3,2,1}){t.Fatal(x,y)};if len(Unique(nil))!=0{t.Fatal("nil")}}`},
		{"Expired", "Expired(now,deadline)：deadline<=0表示永不过期，否则now>=deadline即过期。", `func Expired(now,deadline int64)bool{return now>deadline}`, `func Expired(now,deadline int64)bool{return deadline>0&&now>=deadline}`, `func TestExpired(t *testing.T){for _,x:=range[][3]int64{{10,10,1},{9,10,0},{11,10,1},{99,0,0},{99,-1,0}}{if Expired(x[0],x[1])!=(x[2]==1){t.Fatal(x)}}}`},
		{"CAS", "CAS(current,expected,next)仅current==expected时返回(next,true)，否则返回(current,false)。", `func CAS(current,expected,next int)(int,bool){return next,true}`, `func CAS(current,expected,next int)(int,bool){if current!=expected{return current,false};return next,true}`, `func TestCAS(t *testing.T){a,b:=CAS(3,2,9);if a!=3||b{t.Fatal(a,b)};a,b=CAS(3,3,9);if a!=9||!b{t.Fatal(a,b)}}`},
		{"ContainsRange", "ContainsRange(lo,hi,start,end)判断闭合边界lo<=start<=end<=hi，外层倒置一律false；允许空区间含端点。", `func ContainsRange(lo,hi,start,end int)bool{return start>=lo}`, `func ContainsRange(lo,hi,start,end int)bool{return lo<=hi&&lo<=start&&start<=end&&end<=hi}`, `func TestContainsRange(t *testing.T){for _,x:=range[][5]int{{1,8,1,8,1},{1,8,8,8,1},{1,8,2,9,0},{1,8,5,2,0},{8,1,8,8,0},{1,8,0,2,0}}{if ContainsRange(x[0],x[1],x[2],x[3])!=(x[4]==1){t.Fatal(x)}}}`},
		{"Reverse", "Reverse(s)按Unicode码点逆序，不按字节；不要求组合字素聚类。", `func Reverse(s string)string{b:=[]byte(s);for i,j:=0,len(b)-1;i<j;i,j=i+1,j-1{b[i],b[j]=b[j],b[i]};return string(b)}`, `func Reverse(s string)string{r:=[]rune(s);for i,j:=0,len(r)-1;i<j;i,j=i+1,j-1{r[i],r[j]=r[j],r[i]};return string(r)}`, `func TestReverse(t *testing.T){for a,b:=range map[string]string{"":"","abc":"cba","天a🐈":"🐈a天"}{if Reverse(a)!=b{t.Fatal(a,Reverse(a))}}}`},
		{"Percentile", "Percentile(xs,p)：空输入返回0；p夹到1..100；复制排序后用nearest-rank ceil(n*p/100)的第rank个值，不修改输入。允许只import sort。", `func Percentile(xs []int,p int)int{if len(xs)==0{return 0};return xs[0]}`, `import "sort"
func Percentile(xs []int,p int)int{if len(xs)==0{return 0};if p<1{p=1};if p>100{p=100};ys:=append([]int(nil),xs...);sort.Ints(ys);rank:=(len(ys)*p+99)/100;return ys[rank-1]}`, `func TestPercentile(t *testing.T){x:=[]int{9,1,5,3};for p,w:=range map[int]int{0:1,25:1,26:3,50:3,75:5,100:9,101:9}{if Percentile(x,p)!=w{t.Fatal(p,Percentile(x,p))}};if !reflect.DeepEqual(x,[]int{9,1,5,3})||Percentile(nil,50)!=0{t.Fatal(x)}}`},
		{"Overlap", "Overlap(a,b,c,d)：两个半开区间[a,b)、[c,d)是否有非空交集；空或倒置区间为false，端点相触不算交叠。", `func Overlap(a,b,c,d int)bool{return a<=d&&c<=b}`, `func Overlap(a,b,c,d int)bool{return a<b&&c<d&&a<d&&c<b}`, `func TestOverlap(t *testing.T){for _,x:=range[][5]int{{0,2,2,4,0},{0,3,2,4,1},{2,2,0,4,0},{4,2,0,9,0},{-3,0,-1,1,1}}{if Overlap(x[0],x[1],x[2],x[3])!=(x[4]==1){t.Fatal(x)}}}`},
		{"NextBackoff", "NextBackoff(attempt,base,cap)：base<=0或cap<=0返回0；attempt<0视0；返回min(cap,base*2^attempt)，整数溢出时仍正确截到cap。", `func NextBackoff(attempt,base,cap int)int{return base<<uint(attempt)}`, `func NextBackoff(attempt,base,cap int)int{if base<=0||cap<=0{return 0};if base>=cap{return cap};if attempt<0{attempt=0};v:=base;for i:=0;i<attempt;i++{if v>=cap-v{return cap};v*=2};return v}`, `func TestNextBackoff(t *testing.T){for _,x:=range[][4]int{{-1,3,99,3},{2,3,99,12},{1000,3,99,99},{0,100,99,99},{3,-1,99,0},{4,3,0,0},{1000,1,int(^uint(0)>>1),int(^uint(0)>>1)}}{if NextBackoff(x[0],x[1],x[2])!=x[3]{t.Fatal(x)}}}`},
	}
}
func setupCodingFixture(t *testing.T, a *App, code, threshold string) (string, []string) {
	t.Helper()
	question := fmt.Sprintf("修复当前工作区十个相互独立的Go纯函数文件。先用read_file读取现有文件，再用write_file提交完整修改提案；保持package bench和函数签名，禁止改测试或go.mod。每个文件只保留该函数，允许import sort，无全局变量、init、其他函数或外部I/O。不运行Shell，验收器会在提案实际批准应用后运行隐藏边界回归。最终说明识别码%s、阈值%s，以及哪些修改尚待批准和测试；不要称提案已应用或测试已通过。分项：\n", code, threshold)
	var labels []string
	for i, c := range codingFixtureCases() {
		p := fmt.Sprintf("case%02d.go", i+1)
		if err := a.workspace.WriteFile(p, []byte("package bench\n"+c.Broken+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
		label := fmt.Sprintf("[CASE%02d]", i+1)
		labels = append(labels, label)
		question += label + " " + p + " " + c.Spec + "\n"
	}
	return question, labels
}

// Enforce the fixture's pure-function contract before compiling model output.
// Hidden tests and module metadata are authored only by the harness.
func validateCodingSource(source, name string) error {
	f, err := parser.ParseFile(token.NewFileSet(), "candidate.go", source, 0)
	if err != nil {
		return err
	}
	if f.Name.Name != "bench" {
		return fmt.Errorf("package changed")
	}
	funcs := 0
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			if d.Name.Name != name || d.Recv != nil {
				return fmt.Errorf("unexpected function")
			}
			funcs++
		case *ast.GenDecl:
			if d.Tok != token.IMPORT {
				return fmt.Errorf("non-function declaration")
			}
			for _, s := range d.Specs {
				v := s.(*ast.ImportSpec)
				p, e := strconv.Unquote(v.Path.Value)
				if e != nil || p != "sort" || v.Name != nil {
					return fmt.Errorf("non-whitelisted import")
				}
			}
		default:
			return fmt.Errorf("unexpected declaration")
		}
	}
	if funcs != 1 {
		return fmt.Errorf("expected one function")
	}
	return nil
}
func verifyCodingFixture(t *testing.T, a *App) (string, error) {
	t.Helper()
	dir := t.TempDir()
	tests := "package bench\nimport(\"testing\";\"reflect\")\n"
	for i, c := range codingFixtureCases() {
		p := fmt.Sprintf("case%02d.go", i+1)
		b, err := a.workspace.ReadFile(p)
		if err != nil {
			return "", err
		}
		if err = validateCodingSource(string(b), c.Name); err != nil {
			return "", fmt.Errorf("%s: %w", p, err)
		}
		if err = os.WriteFile(filepath.Join(dir, p), b, 0600); err != nil {
			return "", err
		}
		tests += c.Test + "\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module fixture\ngo 1.23\n"), 0600); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "hidden_test.go"), []byte(tests), 0600); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "test", "-count=1", "-timeout=10s", "-v", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOPROXY=off", "CGO_ENABLED=0")
	out, err := cmd.CombinedOutput()
	return string(out), err
}
func TestCodingBenchmarkFixture(t *testing.T) {
	a := testApp(t)
	_, labels := setupCodingFixture(t, a, "FIXTURE", "27.4 MHz")
	if len(labels) != 10 {
		t.Fatal("missing assignments")
	}
	out, err := verifyCodingFixture(t, a)
	if err == nil || strings.Count(out, "--- FAIL:") != 10 {
		t.Fatalf("baseline must expose all ten defects: %v %s", err, out)
	}
	for i, c := range codingFixtureCases() {
		if err := a.workspace.WriteFile(fmt.Sprintf("case%02d.go", i+1), []byte("package bench\n"+c.Fixed+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if out, err = verifyCodingFixture(t, a); err != nil {
		t.Fatalf("reference repairs failed: %v %s", err, out)
	}
	if validateCodingSource("package bench\nimport \"os\"\nfunc Clamp(n,lo,hi int)int{os.Exit(0);return 0}", "Clamp") == nil {
		t.Fatal("external effects allowed in fixture")
	}
	if validateCodingSource("package bench\nfunc init(){}\nfunc Clamp(n,lo,hi int)int{return n}", "Clamp") == nil {
		t.Fatal("init injection allowed")
	}
}
