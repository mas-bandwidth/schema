package goosetable_test

// A GOOSE DRIVER DIES WITH ITS PARENT (internal/codegen/goose/gooserun).
//
// gooserun.Command starts every process a Goose test runs with a parent-death
// signal on Linux, so a driver that outlives the test binary cannot keep a
// machine busy. The test re-executes this test binary as the parent: its helper
// test starts a long sleep through gooserun.Command, writes the sleep's pid
// down, and waits. The test then kills the helper without warning and requires
// the sleep to go with it. A helper run on its own (the done-when pattern also
// matches its name) skips.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
)

const (
	dieWithParentHelperEnv = "GOOSETABLE_DIE_WITH_PARENT_HELPER"
	dieWithParentPIDEnv    = "GOOSETABLE_DIE_WITH_PARENT_CHILD_PID"
)

func TestGooseDieWithParent(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("gooserun.Command sets a parent-death signal on Linux only")
	}
	if os.Getenv(dieWithParentHelperEnv) != "" {
		t.Skip("the helper mode is driven by TestGooseDieWithParent")
	}

	self, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve the test binary: %v", err)
	}
	pidFile := filepath.Join(t.TempDir(), "driver.pid")
	cmd := exec.Command(self, "-test.run=^TestGooseDieWithParentHelper$")
	cmd.Env = append(os.Environ(),
		dieWithParentHelperEnv+"=1",
		dieWithParentPIDEnv+"="+pidFile,
	)
	var output strings.Builder
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the parent helper: %v", err)
	}
	helperDone := false
	t.Cleanup(func() {
		if !helperDone {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
		if pid := readDieWithParentPID(pidFile); pid > 0 {
			if p, err := os.FindProcess(pid); err == nil {
				_ = p.Kill()
			}
		}
	})

	pid := waitForDieWithParentPID(t, pidFile, &output)
	if !dieWithParentRunning(pid) {
		t.Fatalf("the helper's child %d was not running; helper output:\n%s", pid, output.String())
	}
	if err := cmd.Process.Kill(); err != nil {
		// The helper may already have exited, reporting that its driver
		// died on its own; Wait below reaps it and its output is named
		// in the failure if the driver is somehow still running.
		t.Logf("kill the parent helper: %v (helper output follows if it exited early)", err)
	}
	_ = cmd.Wait()
	helperDone = true

	deadline := time.Now().Add(10 * time.Second)
	for dieWithParentRunning(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("the driver %d outlived its parent; helper output:\n%s", pid, output.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestGooseDieWithParentHelper is the parent process of
// TestGooseDieWithParent. Run without its environment it is a no-op.
func TestGooseDieWithParentHelper(t *testing.T) {
	if os.Getenv(dieWithParentHelperEnv) != "1" {
		t.Skip("helper for TestGooseDieWithParent")
	}
	pidFile := os.Getenv(dieWithParentPIDEnv)
	if pidFile == "" {
		t.Fatal("the helper has no pid file")
	}
	cmd := gooserun.Command(t, "sleep", "300")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the driver: %v", err)
	}
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(cmd.Process.Pid)), 0o644); err != nil {
		t.Fatalf("write the driver pid: %v", err)
	}
	// Stay alive until the parent test kills this helper. Waiting on the
	// driver (instead of blocking forever) means a driver that dies on
	// its own ends the helper at once with the reason in its output,
	// rather than leaving a parent whose driver is already gone.
	if err := cmd.Wait(); err != nil {
		t.Fatalf("the driver exited before its parent was killed: %v", err)
	}
	t.Fatal("the driver exited before its parent was killed")
}

func readDieWithParentPID(path string) int {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		return 0
	}
	return pid
}

func waitForDieWithParentPID(t *testing.T, path string, output *strings.Builder) int {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if pid := readDieWithParentPID(path); pid > 0 {
			return pid
		}
		if time.Now().After(deadline) {
			t.Fatalf("the helper never wrote its driver's pid; helper output:\n%s", output.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// dieWithParentRunning reports whether pid is a live process. A zombie is not
// running: the kernel killed it, and its new parent may not have reaped it yet.
func dieWithParentRunning(pid int) bool {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return false
	}
	s := string(b)
	end := strings.LastIndexByte(s, ')')
	if end < 0 || end+2 >= len(s) {
		return true
	}
	state := s[end+2]
	return state != 'Z' && state != 'X'
}
