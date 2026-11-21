//go:build linux

package execsandbox

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type processMemory struct {
	pid, parent, group int
	rss                uint64
	started            uint64
}

func parseProcessMemory(pid int, value string) (processMemory, error) {
	end := strings.LastIndex(value, ")")
	if end < 0 {
		return processMemory{}, fmt.Errorf("invalid process stat")
	}
	fields := strings.Fields(value[end+1:])
	if len(fields) < 22 {
		return processMemory{}, fmt.Errorf("short process stat")
	}
	parent, e1 := strconv.Atoi(fields[1])
	group, e2 := strconv.Atoi(fields[2])
	pages, e3 := strconv.ParseInt(fields[21], 10, 64)
	started, e4 := strconv.ParseUint(fields[19], 10, 64)
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil || pages < 0 {
		return processMemory{}, fmt.Errorf("invalid process memory fields")
	}
	return processMemory{pid: pid, parent: parent, group: group, rss: uint64(pages) * uint64(os.Getpagesize()), started: started}, nil
}

func commandRSS(root int) (uint64, []processMemory, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0, nil, err
	}
	rows := make([]processMemory, 0, len(entries))
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		data, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "stat"))
		if err != nil {
			if pid == root && !os.IsNotExist(err) {
				return 0, nil, err
			}
			continue // Short-lived or other-user processes need not be inspectable.
		}
		row, err := parseProcessMemory(pid, string(data))
		if err != nil {
			if pid == root {
				return 0, nil, err
			}
			continue
		}
		rows = append(rows, row)
	}
	selected := map[int]bool{root: true}
	for changed := true; changed; {
		changed = false
		for _, row := range rows {
			if !selected[row.pid] && (row.group == root || selected[row.parent]) {
				selected[row.pid] = true
				changed = true
			}
		}
	}
	var total uint64
	var selectedRows []processMemory
	for _, row := range rows {
		if selected[row.pid] {
			total += row.rss
			selectedRows = append(selectedRows, row)
		}
	}
	return total, selectedRows, nil
}

func terminateMemoryTree(root int, rows []processMemory) {
	// Include observed descendants that created another group. Recheck birth time
	// before signalling each PID so a recycled PID is not mistaken for an old child.
	for _, row := range rows {
		data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(row.pid), "stat"))
		if err != nil {
			continue
		}
		current, err := parseProcessMemory(row.pid, string(data))
		if err == nil && current.started == row.started {
			_ = syscall.Kill(row.pid, syscall.SIGKILL)
		}
	}
	_ = syscall.Kill(-root, syscall.SIGKILL)
}

// RunBounded monitors the complete command group plus current descendants.
// This is a sampled RSS guard, not a cgroup/hard allocation or security boundary.
// Callers must use a new process group and retain their existing cancellation.
func RunBounded(cmd *exec.Cmd, memoryMiB int) error {
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid || cmd.SysProcAttr.Pgid != 0 || memoryMiB < 1 || memoryMiB > 1536 {
		return fmt.Errorf("invalid command resource policy")
	}
	if _, err := os.ReadDir("/proc"); err != nil {
		return fmt.Errorf("command memory monitor unavailable: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan struct{})
	monitored := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(25 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				monitored <- nil
				return
			case <-ticker.C:
				rss, rows, err := commandRSS(cmd.Process.Pid)
				if err == nil && rss <= uint64(memoryMiB)*1024*1024 {
					continue
				}
				terminateMemoryTree(cmd.Process.Pid, rows)
				if err != nil {
					monitored <- fmt.Errorf("command memory monitor failed; command terminated: %w", err)
				} else {
					monitored <- fmt.Errorf("command memory budget exceeded: %d MiB RSS > %d MiB; command group terminated; split the work into smaller batches before retrying", rss/1024/1024, memoryMiB)
				}
				return
			}
		}
	}()
	err := cmd.Wait()
	close(done)
	if resourceErr := <-monitored; resourceErr != nil {
		return resourceErr
	}
	return err
}
