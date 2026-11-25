package goosetable_test

// A GOOSE DRIVER DIES WITH ITS PARENT (internal/codegen/goose/gooserun).
//
// gooserun.Command starts every process a Goose test runs with a parent-death
// signal on Linux, so a driver that outlives the test binary cannot keep a
// machine busy. The test re-executes this test binary as the parent: its helper
// test starts a long sleep through gooserun.Command, writes the sleep's pid
// down, and waits. The test then kills the helper without warning and requires
// the sleep to go with it. A helper run on its own (the done-when pattern also
// matches its name) never sees it: TestMain dispatches the helper
// before any test runs.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve the test binary: %v", err)
	}
	pidFile := filepath.Join(t.TempDir(), "driver.pid")
	cmd := exec.Command(self, "-test.run=^$")
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
	waitErr := cmd.Wait()
	helperDone = true
	// The helper must have died by the test's kill. A helper that exited on
	// its own did so because its driver died first, and a driver that dies
	// on its own says nothing about dying with its parent.
	if cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != -1 {
		t.Fatalf("the parent helper was not killed by the test (wait: %v); helper output:\n%s", waitErr, output.String())
	}

	timeout := time.NewTimer(10 * time.Second)
	defer timeout.Stop()
	poll := time.NewTicker(10 * time.Millisecond)
	defer poll.Stop()
	for dieWithParentRunning(pid) {
		select {
		case <-timeout.C:
			t.Fatalf("the driver %d outlived its parent; helper output:\n%s", pid, output.String())
		case <-poll.C:
		}
	}
}

// TestMain runs the helper body when TestGooseDieWithParent started this binary
// as its parent process, and the tests otherwise.
func TestMain(m *testing.M) {
	if os.Getenv(dieWithParentHelperEnv) == "1" {
		os.Exit(dieWithParentHelper())
	}
	os.Exit(m.Run())
}

// helperTB is the testing.TB gooserun.Command needs in the helper process,
// which runs before any test: it uses only Helper and Cleanup.
type helperTB struct{ testing.TB }

func (helperTB) Helper()        {}
func (helperTB) Cleanup(func()) {}

// dieWithParentHelper is the parent process of TestGooseDieWithParent: it
// starts the driver, writes its pid down and waits to be killed. Any return
// is a failure, named on stderr.
func dieWithParentHelper() int {
	pidFile := os.Getenv(dieWithParentPIDEnv)
	if pidFile == "" {
		fmt.Fprintln(os.Stderr, "the helper has no pid file")
		return 1
	}
	cmd := gooserun.Command(helperTB{}, "sleep", "300")
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "start the driver: %v\n", err)
		return 1
	}
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(cmd.Process.Pid)), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write the driver pid: %v\n", err)
		return 1
	}
	// Stay alive until the parent test kills this helper. Waiting on the
	// driver (instead of blocking forever) means a driver that dies on
	// its own ends the helper at once with the reason in its output,
	// rather than leaving a parent whose driver is already gone.
	if err := cmd.Wait(); err != nil {
		fmt.Fprintf(os.Stderr, "the driver exited before its parent was killed: %v\n", err)
		return 1
	}
	fmt.Fprintln(os.Stderr, "the driver exited before its parent was killed")
	return 1
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
	timeout := time.NewTimer(10 * time.Second)
	defer timeout.Stop()
	poll := time.NewTicker(10 * time.Millisecond)
	defer poll.Stop()
	for {
		if pid := readDieWithParentPID(path); pid > 0 {
			return pid
		}
		select {
		case <-timeout.C:
			t.Fatalf("the helper never wrote its driver's pid; helper output:\n%s", output.String())
		case <-poll.C:
		}
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
