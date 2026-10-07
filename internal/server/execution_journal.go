package server

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// Intent is persisted before dispatch. It is not proof that an external effect
// happened, and must never be used to replay a side-effecting call automatically.
type ToolExecutionIntent struct {
	CallID string `json:"callId"`
	Tool   string `json:"tool"`
	State  string `json:"state"` // dispatched, completed
}

type ExecutionEvent struct {
	Version  int    `json:"version"`
	TaskID   string `json:"taskId"`
	Sequence uint64 `json:"sequence"`
	Kind     string `json:"kind"`
	Step     string `json:"step"`
	CallID   string `json:"callId,omitempty"`
	Tool     string `json:"tool,omitempty"`
	Digest   string `json:"digest,omitempty"`
	At       string `json:"at"`
}

// checkpointExecution is a fail-closed barrier for the central execution loop.
// Session snapshots own recovery; JSONL is an append-only audit trail, not a
// second state store. Tool payloads stay in the existing session store.
func (a *App) checkpointExecution(task *Task, step string, chain []Message, kind string, call *ToolCall, result string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	var session *Session
	for _, s := range a.sessions {
		for _, run := range s.Runs {
			if run == task {
				session = s
				break
			}
		}
		if session != nil {
			break
		}
	}
	if session == nil || session.Deleted {
		return errors.New("执行检查点没有有效的所属会话")
	}
	if task.ID == "" || filepath.Base(task.ID) != task.ID || task.ID == "." || task.ID == ".." {
		return errors.New("无效任务标识")
	}
	task.CheckpointStep = step
	task.CheckpointMessages = append([]Message(nil), chain...)
	task.CanResume = true
	task.ExecutionSequence++
	event := ExecutionEvent{Version: 1, TaskID: task.ID, Sequence: task.ExecutionSequence, Kind: kind, Step: step, At: time.Now().UTC().Format(time.RFC3339Nano)}
	if call != nil {
		event.CallID, event.Tool = call.ID, call.Function.Name
		payload := call.Function.Arguments
		if kind == "tool_result" {
			payload = result
		}
		sum := sha256.Sum256([]byte(payload))
		event.Digest = hex.EncodeToString(sum[:])
		found := false
		for i := range task.ToolExecutionIntents {
			if task.ToolExecutionIntents[i].CallID == call.ID {
				found = true
				if kind == "tool_result" {
					task.ToolExecutionIntents[i].State = "completed"
				}
				break
			}
		}
		if kind == "tool_dispatch" && !found {
			task.ToolExecutionIntents = append(task.ToolExecutionIntents, ToolExecutionIntent{call.ID, call.Function.Name, "dispatched"})
		}
	}
	if err := a.save(session); err != nil {
		return fmt.Errorf("保存执行检查点: %w", err)
	}
	dir := filepath.Join(a.dataPath, "execution-events")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	path := filepath.Join(dir, task.ID+".jsonl")
	if st, err := os.Lstat(path); err == nil && !st.Mode().IsRegular() {
		return errors.New("执行日志必须是普通文件")
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	if err = f.Chmod(0600); err != nil {
		_ = f.Close()
		return err
	}
	if err = json.NewEncoder(f).Encode(event); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	// Retire completed intent entries; the durable transcript is authoritative.
	if kind == "tool_result" {
		kept := task.ToolExecutionIntents[:0]
		for _, intent := range task.ToolExecutionIntents {
			if intent.State != "completed" {
				kept = append(kept, intent)
			}
		}
		task.ToolExecutionIntents = kept
	}
	return nil
}

// Complete the protocol after interruption without inventing a tool result.
// Legacy checkpoints lack dispatch markers, so unmatched calls are unknown.
func recoverExecutionCheckpoint(task *Task) {
	states := map[string]string{}
	transcriptCalls := map[string]bool{}
	for _, intent := range task.ToolExecutionIntents {
		states[intent.CallID] = intent.State
	}
	var recovered []Message
	for i := 0; i < len(task.CheckpointMessages); i++ {
		msg := task.CheckpointMessages[i]
		recovered = append(recovered, msg)
		if msg.Role != "assistant" || len(msg.ToolCalls) == 0 {
			continue
		}
		answered := map[string]bool{}
		for i+1 < len(task.CheckpointMessages) && task.CheckpointMessages[i+1].Role == "tool" {
			i++
			response := task.CheckpointMessages[i]
			answered[response.ToolCallID] = true
			recovered = append(recovered, response)
		}
		for _, call := range msg.ToolCalls {
			transcriptCalls[call.ID] = true
			if answered[call.ID] {
				continue
			}
			text := "TOOL_OUTCOME_UNKNOWN：此工具调用没有已持久化的结果，可能已经产生外部副作用。不得声称成功或未执行。先核查文件、远端或工具状态；有副作用且不能核实的操作需用户确认后才可重试。"
			if task.ExecutionSequence > 0 && states[call.ID] == "" {
				text = "TOOL_NOT_DISPATCHED：此调用未进入持久化执行入口。恢复后可根据当前任务和权限重新决定是否调用。"
			}
			recovered = append(recovered, Message{Role: "tool", ToolCallID: call.ID, Content: text})
		}
	}
	for _, intent := range task.ToolExecutionIntents {
		if !transcriptCalls[intent.CallID] && intent.State != "completed" {
			task.HookRecoveryRequired = true
			recovered = append(recovered, Message{Role: "user", Content: "运行时恢复记录：TOOL_OUTCOME_UNKNOWN，后台 Hook 调用 " + intent.CallID + " 没有已持久化结果。先核查其副作用，无法核实的重试需人工确认。"})
		}
	}
	task.CheckpointMessages = recovered
	task.ToolExecutionIntents = nil
}

// Read receipts belonging to this session only, never arbitrary paths. A torn
// trailing record is reported, not invented or used as recovery state.
func (a *App) executionJournal(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	s := a.sessions[r.PathValue("id")]
	var task *Task
	if s != nil && !s.Deleted {
		for _, run := range s.Runs {
			if run.ID == r.PathValue("run") {
				task = run
				break
			}
		}
	}
	if task == nil {
		a.mu.Unlock()
		fail(w, 404, errors.New("任务不存在"))
		return
	}
	id := task.ID
	a.mu.Unlock()
	if id == "" || filepath.Base(id) != id || id == "." || id == ".." {
		fail(w, 400, errors.New("无效任务标识"))
		return
	}
	after := uint64(0)
	if raw := r.URL.Query().Get("after"); raw != "" {
		value, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			fail(w, 400, err)
			return
		}
		after = value
	}
	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 1000 {
			fail(w, 400, errors.New("limit 必须为1–1000"))
			return
		}
		limit = value
	}
	path := filepath.Join(a.dataPath, "execution-events", id+".jsonl")
	st, err := os.Lstat(path)
	if os.IsNotExist(err) {
		jsonOut(w, 200, map[string]any{"events": []ExecutionEvent{}, "next": after, "hasMore": false})
		return
	}
	if err != nil {
		fail(w, 500, err)
		return
	}
	if !st.Mode().IsRegular() || st.Size() > 64<<20 {
		fail(w, 409, errors.New("日志不是普通文件或超过64MiB读取上限"))
		return
	}
	f, err := os.Open(path)
	if err != nil {
		fail(w, 500, err)
		return
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 4096), 64*1024)
	events := []ExecutionEvent{}
	next := after
	more := false
	warning := ""
	for scanner.Scan() {
		var event ExecutionEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil || event.TaskID != id || event.Version != 1 {
			warning = "存在未完整写入或无效的日志记录，未将其视为执行结果"
			break
		}
		if event.Sequence <= after {
			continue
		}
		if len(events) == limit {
			more = true
			break
		}
		events = append(events, event)
		next = event.Sequence
	}
	if err := scanner.Err(); err != nil {
		warning = err.Error()
	}
	jsonOut(w, 200, map[string]any{"events": events, "next": next, "hasMore": more, "warning": warning})
}

// Session deletion owns retention. No background age sweep can erase recovery
// evidence for active or interrupted tasks.
func (a *App) removeExecutionJournals(session *Session) {
	for _, task := range session.Runs {
		if task.ID == "" || filepath.Base(task.ID) != task.ID || task.ID == "." || task.ID == ".." {
			continue
		}
		path := filepath.Join(a.dataPath, "execution-events", task.ID+".jsonl")
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			log.Printf("删除执行日志失败 %s: %v", task.ID, err)
		}
	}
}
