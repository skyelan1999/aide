package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const knowledgeTopicBytes = 2 << 20

type knowledgeConclusionTopic struct {
	ID       string                         `json:"id"`
	Label    string                         `json:"label"`
	Members  []knowledgeConclusionSelection `json:"members"`
	Disabled bool                           `json:"disabled"`
}
type knowledgeConclusionTopics struct {
	Version   int                        `json:"version"`
	Workspace string                     `json:"workspace"`
	Code      bool                       `json:"code"`
	Revision  int64                      `json:"revision"`
	Topics    []knowledgeConclusionTopic `json:"topics"`
}

func knowledgeTopicFile(data, workspace string, code bool) string {
	return filepath.Join(KnowledgeTopicsDir(data), hash([]byte(fmt.Sprintf("%s:%t", workspace, code)))+".json")
}
func knowledgeTopicKey(token string) []byte { return knowledgeTimeKey("manual-topics-v1\x00" + token) }
func knowledgeTopicDirectory(file string, create bool) error {
	dir := filepath.Dir(file)
	for _, p := range []string{filepath.Dir(dir), dir} {
		info, err := os.Lstat(p)
		if os.IsNotExist(err) {
			if !create {
				continue
			}
			if err = os.Mkdir(p, 0700); err != nil && !os.IsExist(err) {
				return err
			}
			info, err = os.Lstat(p)
		}
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("人工主题目录无效；保留原数据")
		}
	}
	return nil
}
func validateKnowledgeTopics(s knowledgeConclusionTopics, workspace string, code bool) error {
	bad := func() error { return fmt.Errorf("人工主题身份或格式无效；保留原文件") }
	if s.Version != 1 || s.Workspace != workspace || s.Code != code || s.Revision < 0 || s.Revision > 1<<50 || len(s.Topics) > 32 {
		return bad()
	}
	seen := map[string]bool{}
	total := 0
	for _, topic := range s.Topics {
		if len(topic.ID) != 32 || seen[topic.ID] || strings.TrimSpace(topic.Label) == "" || len(topic.Label) > 256 || !utf8.ValidString(topic.Label) || len(topic.Members) > 32 {
			return bad()
		}
		seen[topic.ID] = true
		members := map[string]bool{}
		for _, m := range topic.Members {
			c, q, p := m.Candidate, m.Candidate.Quote, m.Pick
			if c.Code != code {
				return bad()
			}
			if members[p.Snapshot] || len(p.Snapshot) == 0 || len(p.Snapshot) > 64 || len(p.Candidate) != 64 || p.Candidate != c.Token || p.Node != c.Node || p.Start < 0 || p.End <= p.Start || p.End > 8192 || len(m.SourceTextHash) != 64 || len(m.Recorded) > 64 {
				return bad()
			}
			members[p.Snapshot] = true
			if c.SourceKind != "document" && c.SourceKind != "document_excerpt" && c.SourceKind != "assistant_conclusion" && c.SourceKind != "assistant_reply" {
				return bad()
			}
			units := 0
			for _, r := range q.Text {
				if r > 0xffff {
					units += 2
				} else {
					units++
				}
			}
			if p.End-p.Start != units {
				return bad()
			}
			for _, text := range []string{c.Node, c.Path, c.Root, c.Source, c.Region, c.Session, q.Key, q.Heading} {
				if !utf8.ValidString(text) {
					return bad()
				}
			}
			if len(c.Node) > 128 || len(c.Node) == 0 || len(c.Path) > 4096 || len(c.Root) > 64 || len(c.Source) > 128 || len(c.Region) > 256 || len(c.Session) > 128 || len(q.Key) > 80 || len(q.Heading) > 256 || len(q.HeadingPath) > 6 || len(q.Text) > knowledgeConclusionTextBytes || !utf8.ValidString(q.Text) || strings.TrimSpace(q.Text) == "" || q.TextHash != hash([]byte(q.Text)) || q.Line < 1 || q.EndLine < q.Line {
				return bad()
			}
			for _, h := range q.HeadingPath {
				if len(h) > 256 || !utf8.ValidString(h) {
					return bad()
				}
				total += len(h)
			}
			if ai := c.AI; ai != nil {
				if len(ai.Run) == 0 || len(ai.Run) > 128 || len(ai.QuestionHash) > 64 || len(ai.QuestionPreview) > 512 || len(ai.Status) > 64 || len(ai.Model) > 256 || c.Session == "" {
					return bad()
				}
				if ai.QuestionBound && len(ai.QuestionHash) != 64 || !ai.QuestionBound && ai.QuestionHash != "" || ai.Status != "completed" && !q.Partial {
					return bad()
				}
			}
			if strings.HasPrefix(c.SourceKind, "assistant_") && c.AI == nil {
				return bad()
			}
			if !strings.HasPrefix(c.SourceKind, "assistant_") && (c.AI != nil || c.Session != "" || c.Path == "") {
				return bad()
			}
			total += len(q.Text) + len(q.Heading) + len(c.Path) + len(c.Node) + len(c.Source) + len(c.Region) + len(c.Session)
			if total > knowledgeTopicBytes {
				return bad()
			}
		}
	}
	return nil
}
func loadKnowledgeTopics(file, workspace string, code bool, key []byte) (knowledgeConclusionTopics, error) {
	s := knowledgeConclusionTopics{Version: 1, Workspace: workspace, Code: code, Topics: []knowledgeConclusionTopic{}}
	if err := knowledgeTopicDirectory(file, false); err != nil {
		return s, err
	}
	info, err := os.Lstat(file)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil || !info.Mode().IsRegular() {
		return s, fmt.Errorf("人工主题不是可读的普通文件")
	}
	f, err := os.Open(file)
	if err != nil {
		return s, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, (4<<20)+1))
	if err != nil || len(raw) > 4<<20 {
		return s, fmt.Errorf("人工主题文件超过容量")
	}
	var envelope knowledgeVectorEnvelope
	if json.Unmarshal(raw, &envelope) != nil || envelope.Version != 1 {
		return s, fmt.Errorf("人工主题封装损坏；保留原文件")
	}
	plain, err := openWithKey(key, envelope.Ciphertext, envelope.Nonce)
	if err != nil {
		return s, fmt.Errorf("人工主题解密失败；保留原文件")
	}
	defer zeroBytes(plain)
	if len(plain) > knowledgeTopicBytes || json.Unmarshal(plain, &s) != nil {
		return s, fmt.Errorf("人工主题内容损坏；保留原文件")
	}
	if err := validateKnowledgeTopics(s, workspace, code); err != nil {
		return s, err
	}
	return s, nil
}
func saveKnowledgeTopics(file string, s knowledgeConclusionTopics, key []byte) error {
	if err := validateKnowledgeTopics(s, s.Workspace, s.Code); err != nil {
		return err
	}
	plain, err := json.Marshal(s)
	if err != nil {
		return err
	}
	defer zeroBytes(plain)
	if len(plain) > knowledgeTopicBytes {
		return fmt.Errorf("人工主题容量已满；原标记保留")
	}
	if err := knowledgeTopicDirectory(file, true); err != nil {
		return err
	}
	if info, err := os.Lstat(file); err == nil && (!info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
		return fmt.Errorf("人工主题写入目标无效")
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	ct, nonce, err := sealWithKey(key, plain)
	if err != nil {
		return err
	}
	return atomicJSON(file, knowledgeVectorEnvelope{1, ct, nonce})
}

func knowledgeSelectionDigest(m knowledgeConclusionSelection) string {
	m.Recorded = ""
	b, _ := json.Marshal(m)
	return hash(b)
}
func knowledgeTopicSelectionCurrent(s knowledgeTimeSnapshot, m knowledgeConclusionSelection) bool {
	current, err := knowledgeConclusionSelect(s, m.Pick, m.Recorded, m.Candidate.Code)
	return err == nil && knowledgeSelectionDigest(current) == knowledgeSelectionDigest(m)
}
