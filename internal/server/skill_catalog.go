package server

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

const maxSkillFiles = 128
const maxSkillBytes = 8 << 20

var skillRepoPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,99}/[A-Za-z0-9][A-Za-z0-9_.-]{0,99}$`)
var skillCommitPattern = regexp.MustCompile(`^[a-f0-9]{40}$`)
var skillKeyPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var skillNamePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

type skillLocation struct {
	Repository string `json:"repository"`
	Path       string `json:"path"`
	Ref        string `json:"ref"`
}
type skillEntry struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
	Size int64  `json:"size"`
}
type skillTree struct {
	Commit    string       `json:"commit"`
	Entries   []skillEntry `json:"tree"`
	Truncated bool         `json:"truncated"`
}
type skillCandidate struct {
	Repository  string `json:"repository"`
	Path        string `json:"path"`
	Commit      string `json:"commit"`
	Name        string `json:"name"`
	Description string `json:"description"`
	URL         string `json:"url"`
	Score       int    `json:"score,omitempty"`
}

func skillRelative(p string, empty bool) error {
	if empty && p == "" {
		return nil
	}
	if len(p) > 512 || p == "" || path.Clean(p) != p || strings.HasPrefix(p, "/") || strings.Count(p, "/") > 12 || strings.ContainsAny(p, "\\:\x00\r\n") {
		return errors.New("Skill 路径无效")
	}
	for _, part := range strings.Split(p, "/") {
		if part == "." || part == ".." || part == "" || strings.EqualFold(part, ".git") || strings.HasPrefix(part, ".aide-") {
			return errors.New("Skill 路径越界或使用保留名称")
		}
	}
	return nil
}
func normalizeSkillLocation(l skillLocation, raw string) (skillLocation, error) {
	if raw != "" {
		if l.Repository != "" || l.Path != "" || l.Ref != "" {
			return l, errors.New("url 与 repository/path/ref 二选一")
		}
		u, e := url.Parse(raw)
		if e != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Port() != "" {
			return l, errors.New("请提供不含凭据的公开 GitHub Skill HTTPS 链接")
		}
		pieces := strings.Split(strings.Trim(u.Path, "/"), "/")
		switch u.Hostname() {
		case "github.com":
			if len(pieces) < 5 || (pieces[2] != "tree" && pieces[2] != "blob") {
				return l, errors.New("GitHub 链接需指向 tree 或 blob 中的 Skill 目录")
			}
			l = skillLocation{Repository: pieces[0] + "/" + pieces[1], Ref: pieces[3], Path: strings.Join(pieces[4:], "/")}
		case "raw.githubusercontent.com":
			if len(pieces) < 4 {
				return l, errors.New("Skill 原始链接不完整")
			}
			l = skillLocation{Repository: pieces[0] + "/" + pieces[1], Ref: pieces[2], Path: strings.Join(pieces[3:], "/")}
		default:
			return l, errors.New("仅支持公开 GitHub 仓库 Skill")
		}
		if path.Base(l.Path) == "SKILL.md" {
			l.Path = strings.TrimSuffix(l.Path, "/SKILL.md")
			if l.Path == "SKILL.md" {
				l.Path = ""
			}
		}
	}
	if !skillRepoPattern.MatchString(l.Repository) || strings.Contains(l.Repository, "..") {
		return l, errors.New("仓库需为 owner/repository")
	}
	if e := skillRelative(l.Path, true); e != nil {
		return l, e
	}
	if l.Ref == "" {
		l.Ref = "HEAD"
	}
	if len(l.Ref) > 200 || strings.ContainsAny(l.Ref, "?#\\\x00\r\n") || strings.HasPrefix(l.Ref, "/") || strings.Contains(l.Ref, "..") {
		return l, errors.New("Skill 分支、标签或提交无效")
	}
	return l, nil
}
func skillClient(base *http.Client) *http.Client {
	if base == nil {
		base = newPluginUpdateClient()
	}
	client := *base
	prior := client.CheckRedirect
	client.CheckRedirect = func(r *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return errors.New("Skill 重定向过多")
		}
		if e := skillRemoteURL(r.URL.String()); e != nil {
			return e
		}
		if prior != nil {
			return prior(r, via)
		}
		return nil
	}
	return &client
}
func skillRemoteURL(raw string) error {
	u, e := pluginUpdateURL(raw)
	if e != nil {
		return e
	}
	if (u.Hostname() != "api.github.com" && u.Hostname() != "raw.githubusercontent.com") || u.Port() != "" {
		return errors.New("Skill 下载仅允许 GitHub API 和原始文件 HTTPS 主机")
	}
	return nil
}
func skillFetch(ctx context.Context, c *http.Client, raw string, limit int64) ([]byte, error) {
	if e := skillRemoteURL(raw); e != nil {
		return nil, e
	}
	b, e := pluginFetch(ctx, c, raw, limit)
	if e != nil {
		return nil, fmt.Errorf("Skill 获取失败: %s", strings.ReplaceAll(e.Error(), "插件", "Skill"))
	}
	return b, nil
}
func readSkillTree(ctx context.Context, c *http.Client, l skillLocation) (skillTree, error) {
	var tree skillTree
	raw, e := skillFetch(ctx, c, "https://api.github.com/repos/"+l.Repository+"/commits/"+url.PathEscape(l.Ref), 1<<20)
	if e != nil {
		return tree, e
	}
	var commit struct {
		SHA string `json:"sha"`
	}
	if json.Unmarshal(raw, &commit) != nil || !skillCommitPattern.MatchString(commit.SHA) {
		return tree, errors.New("Skill 提交信息无效")
	}
	raw, e = skillFetch(ctx, c, "https://api.github.com/repos/"+l.Repository+"/git/trees/"+commit.SHA+"?recursive=1", 8<<20)
	if e != nil {
		return tree, e
	}
	if json.Unmarshal(raw, &tree) != nil || tree.Truncated || len(tree.Entries) > 20000 {
		return tree, errors.New("Skill 仓库目录不完整或超过限制，请使用较小的技能仓库")
	}
	if skillCommitPattern.MatchString(l.Ref) && l.Ref != commit.SHA {
		return tree, errors.New("Skill 固定提交与上游响应不一致")
	}
	tree.Commit = commit.SHA
	seen := map[string]bool{}
	for _, entry := range tree.Entries {
		if skillRelative(entry.Path, false) != nil || seen[entry.Path] {
			return tree, errors.New("Skill 仓库包含无效或重复路径")
		}
		seen[entry.Path] = true
	}
	return tree, nil
}
func escapeSkillPath(value string) string {
	parts := strings.Split(value, "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return strings.Join(parts, "/")
}
func skillBlob(ctx context.Context, c *http.Client, repo, commit string, e skillEntry, limit int64) ([]byte, error) {
	if e.Type != "blob" || (e.Mode != "100644" && e.Mode != "100755") || !skillCommitPattern.MatchString(e.SHA) || e.Size < 0 || e.Size > limit {
		return nil, errors.New("Skill 文件是链接、特殊对象或超过限制")
	}
	b, err := skillFetch(ctx, c, "https://raw.githubusercontent.com/"+repo+"/"+commit+"/"+escapeSkillPath(e.Path), limit)
	if err != nil {
		return nil, err
	}
	digest := sha1.Sum(append([]byte(fmt.Sprintf("blob %d\x00", len(b))), b...))
	if int64(len(b)) != e.Size || hex.EncodeToString(digest[:]) != e.SHA {
		return nil, errors.New("Skill Git blob 摘要或长度不匹配")
	}
	return b, nil
}

// Only name and description are consumed; the rest of frontmatter is inert data.
// Support the plain, quoted and block scalar forms used in public skill catalogs.
func skillMetadata(b []byte, dir string) (string, string, error) {
	if len(b) > 256<<10 || !utf8.Valid(b) {
		return "", "", errors.New("SKILL.md 需为不超过 256KiB 的 UTF-8 文本")
	}
	text := strings.ReplaceAll(string(b), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	if len(lines) < 3 || lines[0] != "---" {
		return "", "", errors.New("SKILL.md 缺少 YAML frontmatter")
	}
	name, desc := "", ""
	end := false
	scalar := func(s string) string {
		s = strings.TrimSpace(s)
		if strings.HasPrefix(s, "\"") {
			v, e := strconv.Unquote(s)
			if e != nil {
				return ""
			}
			return v
		}
		if strings.HasPrefix(s, "'") && strings.HasSuffix(s, "'") {
			return strings.ReplaceAll(s[1:len(s)-1], "''", "'")
		}
		return s
	}
	for i := 1; i < len(lines) && i < 200; i++ {
		line := lines[i]
		if line == "---" {
			end = true
			break
		}
		if len(line) > 16384 {
			return "", "", errors.New("Skill frontmatter 过大")
		}
		if strings.HasPrefix(line, "name:") {
			if name != "" {
				return "", "", errors.New("Skill name 重复")
			}
			name = scalar(strings.TrimPrefix(line, "name:"))
		}
		if strings.HasPrefix(line, "description:") {
			if desc != "" {
				return "", "", errors.New("Skill description 重复")
			}
			v := strings.TrimSpace(strings.TrimPrefix(line, "description:"))
			if v == "|" || v == ">" || v == "|-" || v == ">-" || v == "|+" || v == ">+" {
				parts := []string{}
				for i+1 < len(lines) && (strings.HasPrefix(lines[i+1], " ") || lines[i+1] == "") {
					i++
					parts = append(parts, strings.TrimSpace(lines[i]))
				}
				desc = strings.Join(parts, " ")
			} else {
				desc = scalar(v)
			}
		}
	}
	if !end || len(name) > 64 || !skillNamePattern.MatchString(name) || (dir != "" && name != path.Base(dir)) || strings.TrimSpace(desc) == "" || utf8.RuneCountInString(desc) > 4096 {
		return "", "", errors.New("Skill name/description 无效，或名称与目录不一致")
	}
	return name, desc, nil
}
func catalogFromTree(ctx context.Context, c *http.Client, repo string, tree skillTree) ([]skillCandidate, []string) {
	entries := []skillEntry{}
	for _, e := range tree.Entries {
		if path.Base(e.Path) == "SKILL.md" && e.Type == "blob" && e.Mode == "100644" {
			entries = append(entries, e)
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	warnings := []string{}
	if len(entries) > 128 {
		entries = entries[:128]
		warnings = append(warnings, repo+": 仅索引前128个技能，请用精确链接安装其他技能")
	}
	out := []skillCandidate{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	slots := make(chan struct{}, 6)
	for _, entry := range entries {
		if ctx.Err() != nil {
			break
		}
		slots <- struct{}{}
		wg.Add(1)
		go func(e skillEntry) {
			defer wg.Done()
			defer func() { <-slots }()
			b, err := skillBlob(ctx, c, repo, tree.Commit, e, 256<<10)
			dir := path.Dir(e.Path)
			if dir == "." {
				dir = ""
			}
			name, description := "", ""
			if err == nil {
				name, description, err = skillMetadata(b, dir)
			}
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if len(warnings) < 8 {
					warnings = append(warnings, repo+"/"+e.Path+": "+err.Error())
				}
				return
			}
			if utf8.RuneCountInString(description) > 1024 {
				description = string([]rune(description)[:1024]) + "…"
				if len(warnings) < 8 {
					warnings = append(warnings, repo+"/"+e.Path+": 说明超过标准1024字符，目录摘要已截断；安装保留完整原文")
				}
			}
			out = append(out, skillCandidate{Repository: repo, Path: dir, Commit: tree.Commit, Name: name, Description: description, URL: "https://github.com/" + repo + "/tree/" + tree.Commit + "/" + dir})
		}(entry)
	}
	wg.Wait()
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, warnings
}
func rankSkills(items []skillCandidate, query string) []skillCandidate {
	query = strings.ToLower(query)
	for cn, en := range map[string]string{"文档": " document docx", "表格": " spreadsheet xlsx", "演示": " presentation pptx", "幻灯": " presentation pptx", "图片": " image", "设计": " design", "测试": " test testing", "网页": " web frontend", "代码": " code", "审查": " review", "安全": " security", "数据": " data", "笔记": " notebook", "邮件": " email"} {
		if strings.Contains(query, cn) {
			query += en
		}
	}
	terms := strings.FieldsFunc(query, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	out := []skillCandidate{}
	for _, item := range items {
		score := 0
		seen := map[string]bool{}
		for _, term := range terms {
			if len(term) < 2 || seen[term] {
				continue
			}
			seen[term] = true
			if strings.Contains(strings.ToLower(item.Name+" "+item.Path), term) {
				score += 8
			}
			if strings.Contains(strings.ToLower(item.Description), term) {
				score += 2
			}
		}
		if score > 0 {
			item.Score = score
			out = append(out, item)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			return out[i].Repository+out[i].Path < out[j].Repository+out[j].Path
		}
		return out[i].Score > out[j].Score
	})
	if len(out) > 12 {
		out = out[:12]
	}
	return out
}
