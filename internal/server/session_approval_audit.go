package server

import (
	"bufio"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// The approval audit is tamper-evident: every record authenticates its complete
// contents and the preceding record with a separate local HMAC key. It detects
// edits, deletion, truncation, reordering and later appends on the next write or
// verification. Like every local audit scheme, an attacker controlling both the
// key and log can rewrite them together; the key is therefore kept separately
// with 0600 permissions and is never returned by an API.
type approvalAuditEntry struct {
	Sequence    uint64 `json:"sequence"`
	Time        string `json:"time"`
	Action      string `json:"action"`
	SessionID   string `json:"sessionId,omitempty"`
	RunID       string `json:"runId,omitempty"`
	AuthMethod  string `json:"authMethod,omitempty"`
	FromMode    string `json:"fromMode,omitempty"`
	Mode        string `json:"mode,omitempty"`
	Kind        string `json:"kind,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
	Detail      string `json:"detail,omitempty"`
	Previous    string `json:"previousHash,omitempty"`
	Hash        string `json:"hash"`
}

type approvalAuditLog struct {
	mu       sync.Mutex
	path     string
	keyPath  string
	key      []byte
	sequence uint64
	head     string
}

type approvalAuditKey struct {
	Key      string `json:"key"`
	Sequence uint64 `json:"sequence,omitempty"`
	Head     string `json:"head,omitempty"`
}

func approvalAuditDigest(key []byte, entry approvalAuditEntry) string {
	entry.Hash = ""
	encoded, _ := json.Marshal(entry)
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(entry.Previous))
	_, _ = mac.Write([]byte{'\n'})
	_, _ = mac.Write(encoded)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func openApprovalAudit(data string) (*approvalAuditLog, error) {
	log := &approvalAuditLog{path: filepath.Join(data, "approval-audit.jsonl"), keyPath: filepath.Join(ConfigDir(data), "approval-audit-key.json")}
	if err := os.MkdirAll(ConfigDir(data), 0700); err != nil {
		return nil, fmt.Errorf("审批审计配置目录创建失败: %w", err)
	}
	var stored approvalAuditKey
	raw, err := os.ReadFile(log.keyPath)
	if errors.Is(err, os.ErrNotExist) {
		key := make([]byte, 32)
		if _, err = rand.Read(key); err != nil {
			return nil, fmt.Errorf("审批审计密钥生成失败: %w", err)
		}
		stored.Key = base64.RawURLEncoding.EncodeToString(key)
		if err = atomicJSON(log.keyPath, stored); err != nil {
			return nil, fmt.Errorf("审批审计密钥保存失败: %w", err)
		}
		if err = os.Chmod(log.keyPath, 0600); err != nil {
			return nil, err
		}
		raw, _ = json.Marshal(stored)
	} else if err != nil {
		return nil, fmt.Errorf("审批审计密钥读取失败: %w", err)
	}
	if err = json.Unmarshal(raw, &stored); err != nil {
		return nil, fmt.Errorf("审批审计密钥无效: %w", err)
	}
	log.key, err = base64.RawURLEncoding.DecodeString(stored.Key)
	if err != nil || len(log.key) != 32 {
		return nil, errors.New("审批审计密钥长度无效")
	}
	log.sequence, log.head = stored.Sequence, stored.Head
	if err = log.verifyLocked(); err != nil {
		return nil, err
	}
	return log, nil
}

func (a *approvalAuditLog) verifyLocked() error {
	if a == nil {
		return errors.New("审批审计日志未初始化")
	}
	file, err := os.Open(a.path)
	if errors.Is(err, os.ErrNotExist) {
		if a.sequence != 0 || a.head != "" {
			return errors.New("审批审计日志已被删除")
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("审批审计日志读取失败: %w", err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	var sequence uint64
	previous := ""
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) == "" {
			return errors.New("审批审计日志包含空行")
		}
		var entry approvalAuditEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			return fmt.Errorf("审批审计日志格式无效: %w", err)
		}
		sequence++
		if entry.Sequence != sequence || entry.Previous != previous || !hmac.Equal([]byte(entry.Hash), []byte(approvalAuditDigest(a.key, entry))) {
			return fmt.Errorf("审批审计日志完整性校验失败于第 %d 条", sequence)
		}
		previous = entry.Hash
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("审批审计日志读取失败: %w", err)
	}
	if sequence != a.sequence || previous != a.head {
		return errors.New("审批审计日志在运行期间被追加、截断或改写")
	}
	return nil
}

func (a *approvalAuditLog) append(entry approvalAuditEntry) error {
	if a == nil {
		return errors.New("审批审计日志未初始化")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.verifyLocked(); err != nil {
		return err
	}
	entry.Sequence = a.sequence + 1
	entry.Time = time.Now().UTC().Format(time.RFC3339Nano)
	entry.Previous = a.head
	entry.Hash = approvalAuditDigest(a.key, entry)
	encoded, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(a.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("审批审计日志写入失败: %w", err)
	}
	if _, err = file.Write(append(encoded, '\n')); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return fmt.Errorf("审批审计日志写入失败: %w", err)
	}
	if closeErr != nil {
		return fmt.Errorf("审批审计日志关闭失败: %w", closeErr)
	}
	previousSequence, previousHead := a.sequence, a.head
	if err = atomicJSON(a.keyPath, approvalAuditKey{Key: base64.RawURLEncoding.EncodeToString(a.key), Sequence: entry.Sequence, Head: entry.Hash}); err != nil {
		a.sequence, a.head = previousSequence, previousHead
		return fmt.Errorf("审批审计链头保存失败: %w", err)
	}
	a.sequence, a.head = entry.Sequence, entry.Hash
	return nil
}

func (a *approvalAuditLog) verify() (uint64, string, error) {
	if a == nil {
		return 0, "", errors.New("审批审计日志未初始化")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.verifyLocked(); err != nil {
		return 0, "", err
	}
	return a.sequence, a.head, nil
}
