//go:build linux

package execsandbox

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestBoundedMemoryChild(t *testing.T) {
	if os.Getenv("AIDE_MEMORY_TEST_CHILD") == "tree" {
		exe, _ := os.Executable()
		child := exec.Command(exe, "-test.run=^TestBoundedMemoryChild$")
		child.Env = append(os.Environ(), "AIDE_MEMORY_TEST_CHILD=allocate")
		child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := child.Start(); err != nil {
			t.Fatal(err)
		}
		fmt.Println(child.Process.Pid)
		_ = child.Wait()
		return
	}
	if os.Getenv("AIDE_MEMORY_TEST_CHILD") != "allocate" {
		return
	}
	block := make([]byte, 192<<20)
	for i := 0; i < len(block); i += os.Getpagesize() {
		block[i] = 1
	}
	fmt.Println("allocated")
	time.Sleep(10 * time.Second)
	runtime.KeepAlive(block)
}

func TestRunBoundedTerminatesRegroupedDescendant(t *testing.T) {
	exe, _ := os.Executable()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "-test.run=^TestBoundedMemoryChild$")
	cmd.Env = append(os.Environ(), "AIDE_MEMORY_TEST_CHILD=tree")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	err := RunBounded(cmd, 128)
	if err == nil || !strings.Contains(err.Error(), "memory budget exceeded") {
		t.Fatalf("regrouped descendant escaped memory accounting: %v %s", err, output.String())
	}
	fields := strings.Fields(output.String())
	if len(fields) == 0 {
		t.Fatal("missing descendant PID")
	}
	pid, err := strconv.Atoi(fields[0])
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Kill(pid, syscall.SIGKILL)
	for i := 0; i < 20; i++ {
		data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		if os.IsNotExist(err) {
			return
		}
		if err == nil {
			end := strings.LastIndex(string(data), ")")
			if end >= 0 && strings.HasPrefix(strings.TrimSpace(string(data[end+1:])), "Z ") {
				return // Exited; container init may not have reaped its orphan yet.
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("regrouped descendant remained alive after over-budget termination")
}

func TestRunBoundedTerminatesMemoryHeavyCommand(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "-test.run=^TestBoundedMemoryChild$")
	cmd.Env = append(os.Environ(), "AIDE_MEMORY_TEST_CHILD=allocate")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	started := time.Now()
	err = RunBounded(cmd, 128)
	if err == nil || !strings.Contains(err.Error(), "memory budget exceeded") || time.Since(started) > 3*time.Second {
		t.Fatalf("memory guard did not terminate promptly: %v (%s)", err, output.String())
	}
	if ctx.Err() != nil {
		t.Fatal("command reached timeout instead of memory guard")
	}
	// A subsequent ordinary command still executes; only the isolated group died.
	cmd = exec.Command("/bin/sh", "-c", "printf healthy")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	output.Reset()
	cmd.Stdout = &output
	if err := RunBounded(cmd, 128); err != nil || output.String() != "healthy" {
		t.Fatalf("following command failed: %v %s", err, output.String())
	}
}

func TestRunBoundedReportsExitAndCancellation(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "exit 17")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := RunBounded(cmd, 128); err == nil || cmd.ProcessState.ExitCode() != 17 {
		t.Fatalf("lost exit status: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	cmd = exec.CommandContext(ctx, "/bin/sh", "-c", "sleep 10 & wait")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	started := time.Now()
	if err := RunBounded(cmd, 128); err == nil || time.Since(started) > 2*time.Second {
		t.Fatalf("cancellation failed: %v", err)
	}
}

func TestProcessMemoryStatWithParentheses(t *testing.T) {
	fields := []string{"S", "41", "42", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "1", "0", "12"}
	row, err := parseProcessMemory(43, "43 (name with ) spaces) "+strings.Join(fields, " "))
	if err != nil || row.parent != 41 || row.group != 42 || row.rss != 12*uint64(os.Getpagesize()) {
		t.Fatalf("stat parse: %+v %v", row, err)
	}
	if _, err := parseProcessMemory(1, "broken"); err == nil {
		t.Fatal("malformed stat accepted")
	}
}
