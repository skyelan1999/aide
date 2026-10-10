package server

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

const memoryTokenFile = "memory-tokens.json"
const memoryTextLimit = 24 * 1024

// Tokenizers are versioned codecs. Persisted records keep their codec; changing
// the registry must not reinterpret existing IDs or claim provider BPE parity.
type memoryTokenizer struct {
	ID          string
	Name        string
	Description string
	Split       func(string) []string
}

var memoryTokenizers = map[string]memoryTokenizer{
	"unicode-scalar-v1": {ID: "unicode-scalar-v1", Name: "Unicode 字符级", Description: "本地 Unicode 标量分词；非模型 BPE", Split: func(s string) []string {
		out := make([]string, 0, utf8.RuneCountInString(s))
		for _, r := range s {
			out = append(out, string(r))
		}
		return out
	}},
}

type memoryRecord struct {
	ID      string   `json:"id"`
	Tokens  []string `json:"tokens"`
	Region  string   `json:"region"`
	Origin  string   `json:"origin"`
	Created string   `json:"created"`
}
type memoryTokenStore struct {
	Version        int               `json:"version"`
	Tokenizer      string            `json:"tokenizer"`
	Vocabulary     map[string]string `json:"vocabulary"`
	Records        []memoryRecord    `json:"records"`
	ProjectionHash string            `json:"projectionHash"`
}

func newMemoryTokenStore() memoryTokenStore {
	return memoryTokenStore{Version: 1, Tokenizer: "unicode-scalar-v1", Vocabulary: map[string]string{}, Records: []memoryRecord{}}
}
func memoryRegion(text string) string {
	for _, p := range []struct {
		region string
		words  []string
	}{
		{"constraints", []string{"禁止", "不要", "不得", "must not"}},
		{"preferences", []string{"喜欢", "偏好", "希望", "prefer"}},
		{"decisions", []string{"决定", "选择", "采用", "decision"}},
		{"procedures", []string{"步骤", "流程", "先", "procedure"}},
		{"facts", []string{"参数", "事实", "版本", "fact"}},
	} {
		for _, word := range p.words {
			if strings.Contains(strings.ToLower(text), word) {
				return p.region
			}
		}
	}
	return "notes"
}
func (s *memoryTokenStore) append(text, origin string) {
	codec := memoryTokenizers[s.Tokenizer]
	record := memoryRecord{ID: newID(), Region: memoryRegion(text), Origin: origin, Created: time.Now().UTC().Format(time.RFC3339), Tokens: []string{}}
	for _, token := range codec.Split(text) {
		id := "T" + hash([]byte(s.Tokenizer + "\x00" + token))[:16]
		s.Vocabulary[id] = token
		record.Tokens = append(record.Tokens, id)
	}
	s.Records = append(s.Records, record)
}
func (s memoryTokenStore) text() string {
	var b strings.Builder
	for _, r := range s.Records {
		for _, id := range r.Tokens {
			b.WriteString(s.Vocabulary[id])
		}
	}
	return b.String()
}
func (s memoryTokenStore) validate() error {
	if s.Vocabulary == nil || s.Records == nil {
		return fmt.Errorf("token 记忆结构不完整")
	}
	if s.Version != 1 {
		return fmt.Errorf("不支持的 token 记忆版本")
	}
	if _, ok := memoryTokenizers[s.Tokenizer]; !ok {
		return fmt.Errorf("分词器 %s 尚未注册", s.Tokenizer)
	}
	if len(s.Vocabulary) > memoryTextLimit || len(s.Records) > memoryTextLimit {
		return fmt.Errorf("token 记忆超出容量")
	}
	total := 0
	for id, text := range s.Vocabulary {
		if !utf8.ValidString(text) || text == "" || id != "T"+hash([]byte(s.Tokenizer + "\x00" + text))[:16] {
			return fmt.Errorf("token 字典损坏")
		}
	}
	for _, r := range s.Records {
		for _, id := range r.Tokens {
			text, ok := s.Vocabulary[id]
			if !ok {
				return fmt.Errorf("记忆引用了不存在的 token")
			}
			total += len(text)
			if total > memoryTextLimit {
				return fmt.Errorf("token 记忆超过 24 KB")
			}
		}
	}
	return nil
}

// Compatibility text can still be edited by existing clients. A changed text
// projection is imported as a replacement snapshot, never duplicated/appended.
func loadMemoryTokenStore(dir string) (memoryTokenStore, string, error) {
	s := newMemoryTokenStore()
	for _, name := range []string{memoryTokenFile, "memory.md"} {
		info, err := os.Lstat(filepath.Join(dir, name))
		if err != nil && !os.IsNotExist(err) {
			return s, "", err
		}
		if err == nil && (!info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
			return s, "", fmt.Errorf("记忆文件必须为普通文件，不跟随符号链接")
		}
	}
	raw, err := os.ReadFile(filepath.Join(dir, memoryTokenFile))
	if err == nil {
		if len(raw) > 2*1024*1024 {
			return s, "", fmt.Errorf("token 记忆文件过大")
		}
		if err = json.Unmarshal(raw, &s); err != nil {
			return s, "", err
		}
		if err = s.validate(); err != nil {
			return s, "", err
		}
	} else if !os.IsNotExist(err) {
		return s, "", err
	}
	legacy, legacyErr := os.ReadFile(filepath.Join(dir, "memory.md"))
	if legacyErr != nil && !os.IsNotExist(legacyErr) {
		return s, "", legacyErr
	}
	warning := ""
	if legacyErr == nil && hash(legacy) != s.ProjectionHash {
		// Validate the stored projection before accepting external text edits.
		if err == nil && s.ProjectionHash != hash([]byte(s.text())) {
			return s, "", fmt.Errorf("token 记忆投影校验失败")
		}
		s = newMemoryTokenStore()
		if len(legacy) > memoryTextLimit || !utf8.Valid(legacy) {
			return s, "", fmt.Errorf("兼容记忆必须为 UTF-8 且不超过 24 KB")
		}
		if len(legacy) > 0 {
			stamp := ""
			if info, statErr := os.Stat(filepath.Join(dir, "memory.md")); statErr == nil {
				stamp = info.ModTime().UTC().Format(time.RFC3339)
			}
			for i, line := range strings.SplitAfter(string(legacy), "\n") {
				if line != "" {
					s.append(line, "legacy-text")
					record := &s.Records[len(s.Records)-1]
					record.ID = "L" + hash([]byte(fmt.Sprintf("%d:%s", i, line)))[:16]
					record.Created = stamp
				}
			}
		}
		s.ProjectionHash = hash(legacy)
		warning = "兼容文本快照；下次 write_memory 保存为 token 记录"
	}
	return s, warning, nil
}
func writeMemoryProjection(path, text string) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".memory-projection-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.WriteString(text); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(name, path)
}
func (a *App) appendTokenMemory(content string) string {
	if !utf8.ValidString(content) || strings.TrimSpace(content) == "" {
		return "记忆内容不能为空，且必须为 UTF-8。"
	}
	dir := a.projectCacheDir()
	s, _, err := loadMemoryTokenStore(dir)
	if err != nil {
		return "读取 token 记忆失败: " + err.Error()
	}
	addition := "\n- " + content + "\n"
	if len(s.text())+len(addition) > memoryTextLimit {
		return "项目记忆超过 24 KB；请先整理记忆。"
	}
	s.append(addition, "write_memory")
	projection := s.text()
	s.ProjectionHash = hash([]byte(projection))
	if err = os.MkdirAll(dir, 0700); err != nil {
		return "创建记忆目录失败: " + err.Error()
	}
	// Write projection first, then authoritative JSON. An interrupted commit is
	// imported from the complete projection on recovery; no partial text append.
	if err = writeMemoryProjection(a.memoryPath(), projection); err != nil {
		return "写入兼容记忆失败: " + err.Error()
	}
	if err = atomicJSON(filepath.Join(dir, memoryTokenFile), s); err != nil {
		return "文本已保存，token 保存失败（下次读取可恢复）: " + err.Error()
	}
	if err = a.pushProjectCacheFiles("aide", "memory.md", memoryTokenFile); err != nil {
		return "本地记忆已保存，远端同步失败: " + err.Error()
	}
	return "已写入 token 记忆。"
}
