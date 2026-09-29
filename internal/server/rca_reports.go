package server

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

/*
rca_reports.go 是「问题解决（RCA）」阶段的只读校验与解析层。

后端写入侧（create_diagram / record_problem_report / createDoc）已在 workflow.go 实现并保持只读；
本文件只承担：
  - 解析 problem-reports-index.md 索引表；
  - 从报告正文提取并解析 draw.io 路径（含 .drawio.svg/.png 导出图的关联修正）；
  - 校验 .drawio 源图是否真实存在（前端据此决定「打开 RCA 图」按钮与缺失告警）；
  - 校验报告是否包含全部必备二级标题。

前端通过既有文件 API（GET /api/files、GET /api/file?root=workspace）读取这些产物，
因此本文件不注册任何新路由。
*/

// RCARequiredSections 是 record_problem_report 强制要求的 Markdown 二级标题集合。
// 与 workflow.go 中 recordProblemReport 的 phaseDocSpec.sections 保持一致。
var RCARequiredSections = []string{"问题", "背景", "排查方向", "RCA 图", "测试", "结论", "建议"}

// RCAIndexEntry 是 problem-reports-index.md 索引表中的一行。
type RCAIndexEntry struct {
	ID      string // RCA-001
	Name    string // 报告标题
	Status  string // 待评审 等
	Created string // 创建日期（YYYY-MM-DD）
	Related string // draw.io: <diagramPath>
}

var rcaIndexRowRe = regexp.MustCompile(`^RCA-\d+`)

// ParseRCAIndex 解析 rewriteDocIndex 产出的 Markdown 表格索引。
// 对格式漂移容错：表头/分隔行被跳过，列数不足时缺失列留空。
func ParseRCAIndex(markdown string) []RCAIndexEntry {
	var out []RCAIndexEntry
	for _, line := range strings.Split(markdown, "\n") {
		t := strings.TrimSpace(line)
		if !strings.HasPrefix(t, "|") {
			continue
		}
		if strings.Contains(t, "编号") { // 表头
			continue
		}
		cells := strings.Split(strings.Trim(t, "|"), "|")
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		if len(cells) == 0 || !rcaIndexRowRe.MatchString(cells[0]) {
			continue // 分隔行 |---|---| 或非 RCA 行
		}
		e := RCAIndexEntry{ID: cells[0]}
		if len(cells) > 1 {
			e.Name = cleanRCAName(cells[1])
		}
		if len(cells) > 2 {
			e.Status = cells[2]
		}
		if len(cells) > 3 {
			e.Created = cells[3]
		}
		if len(cells) > 4 {
			e.Related = cells[4]
		}
		out = append(out, e)
	}
	return out
}

var rcaRelatedRe = regexp.MustCompile(`(?m)^-\s*关联[：:]\s*draw\.io[：:]\s*(\S+)`)
var rcaDrawioSectionRe = regexp.MustCompile(`(?ms)^##\s*RCA\s*图\s*\n+([^\n#]+)`)

// ExtractRCADiagramPath 从报告正文中提取关联的 draw.io 路径。
// 优先级：front-matter 形式的「- 关联：draw.io: X」，其次「## RCA 图」小节首行。
func ExtractRCADiagramPath(body string) string {
	if m := rcaRelatedRe.FindStringSubmatch(body); m != nil {
		return strings.TrimSpace(m[1])
	}
	if m := rcaDrawioSectionRe.FindStringSubmatch(body); m != nil {
		return strings.TrimSpace(m[1])
	}
	return ""
}

// cleanRCAName 去除索引表名称列里前导的孤儿字节。
// 背景：workflow.go 的 rewriteDocIndex 用 strings.Index 定位多字节分隔符 "·"(U+00B7)
// 后按字节 t[i+1:] 切片，会残留第二字节 0xB7（终端显示为 �）。此处只做展示层清洗，
// 不改写入侧（workflow.go 属只读所有权范围）。
func cleanRCAName(s string) string {
	for len(s) > 0 && (s[0] < ' ' || s[0] == 0xb7) {
		s = s[1:]
	}
	return strings.TrimSpace(s)
}

var rcaExportRe = regexp.MustCompile(`(?i)\.drawio\.(svg|png)$`)

// ResolveRCADiagram 把可能引用了导出图（.drawio.svg / .drawio.png）的路径归一化到可编辑的
// .drawio 源图路径。返回 (drawio 源路径, 原始导出引用)。若引用本身已是 .drawio，导出引用为空。
func ResolveRCADiagram(ref string) (drawio string, exportRef string) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", ""
	}
	if rcaExportRe.MatchString(ref) {
		return rcaExportRe.ReplaceAllString(ref, ".drawio"), ref
	}
	return ref, ""
}

// ValidateRCADiagramExists 校验报告关联的 draw.io 是否真实存在。
// root 为工作区根（路径相对 root 解析，与 create_diagram 写入根一致）。
// 返回：drawioExists=源图是否存在；exportExists=导出图是否存在（仅当引用了导出图时有效）；
// resolved=归一化后的 .drawio 相对路径；err 仅在路径非法时非空。
func ValidateRCADiagramExists(root, ref string) (drawioExists bool, exportExists *bool, resolved string, err error) {
	drawio, exportRef := ResolveRCADiagram(ref)
	if drawio == "" {
		return false, nil, "", nil
	}
	if e := safePath(drawio); e != nil {
		return false, nil, drawio, e
	}
	drawioExists = fileExists(root, drawio)
	if exportRef != "" {
		ok := fileExists(root, exportRef)
		exportExists = &ok
	}
	return drawioExists, exportExists, drawio, nil
}

// CheckRCAReportSections 返回报告正文缺失的必备二级标题（按 RCARequiredSections 顺序）。
func CheckRCAReportSections(body string) []string {
	var missing []string
	for _, sec := range RCARequiredSections {
		if strings.TrimSpace(requirementSection(body, sec)) == "" {
			missing = append(missing, sec)
		}
	}
	return missing
}

// rcaListReportFiles 列出 dir 下 RCA-*.md 报告（排除索引文件本身），按编号升序。
func rcaListReportFiles(dir string) ([]string, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if !strings.HasSuffix(n, ".md") || n == "problem-reports-index.md" {
			continue
		}
		if rcaIndexRowRe.MatchString(n) {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out, nil
}

// fileExists 在给定 root 下判断相对路径是否为普通文件。
func fileExists(root, rel string) bool {
	joined := filepath.Join(root, filepath.FromSlash(rel))
	info, err := os.Stat(joined)
	return err == nil && info.Mode().IsRegular()
}
