//go:build unix

package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// These are FUNCTIONAL tests: they start real processes and wait on real
// time, so they run under SCHEMA_SLOW=1 (ci-full.yml's go-test steps) and skip
// in a default `go test`.

func buildPgroup(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "pgroup")
	out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput()
	if err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

// orphanScript starts a sleep in the background, records its pid and exits
// 3 at once: the sleep is the `sq` a dead test leaves behind.
const orphanScript = `sleep 600 & echo $! > "$1"; exit 3`

func alive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

func readPid(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	return pid
}

// gone polls until pid is reaped by whoever inherited it.
func gone(pid int) bool {
	for range 50 {
		if !alive(pid) {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

func TestOrphanDiesWithTheCommand(t *testing.T) {
	slowtest.Gate(t, "sh and sleep")
	bin := buildPgroup(t)
	pidFile := filepath.Join(t.TempDir(), "pid")

	err := exec.Command(bin, "--", "sh", "-c", orphanScript, "sh", pidFile).Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 3 {
		t.Fatalf("pgroup exit = %v, want the command's status 3", err)
	}
	pid := readPid(t, pidFile)
	if !gone(pid) {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		t.Fatalf("the background sleep (pid %d) outlived the command under pgroup", pid)
	}
}

// TestOrphanOutlivesABareCommand is the negative control: the same script
// without pgroup leaves the sleep running, so the test above can go red.
func TestOrphanOutlivesABareCommand(t *testing.T) {
	slowtest.Gate(t, "sh and sleep")
	pidFile := filepath.Join(t.TempDir(), "pid")
	_ = exec.Command("sh", "-c", orphanScript, "sh", pidFile).Run()
	pid := readPid(t, pidFile)
	defer syscall.Kill(pid, syscall.SIGKILL)
	time.Sleep(500 * time.Millisecond)
	if !alive(pid) {
		t.Fatalf("the background sleep (pid %d) died without pgroup: the orphan test above proves nothing", pid)
	}
}

func TestWallStopsTheGroup(t *testing.T) {
	slowtest.Gate(t, "sh and sleep")
	bin := buildPgroup(t)
	pidFile := filepath.Join(t.TempDir(), "pid")
	start := time.Now()
	err := exec.Command(bin, "-wall", "1s", "-grace", "1s", "--",
		"sh", "-c", `sleep 600 & echo $! > "$1"; wait`, "sh", pidFile).Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 124 {
		t.Fatalf("pgroup exit = %v, want 124 after the wall", err)
	}
	if d := time.Since(start); d > 30*time.Second {
		t.Fatalf("pgroup took %s to stop a 1s wall", d)
	}
	if pid := readPid(t, pidFile); !gone(pid) {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		t.Fatalf("the background sleep (pid %d) outlived the wall", pid)
	}
}

// TestEnvWallStopsTheGroupAndNamesTheRule is the per-commit CI path: the
// budget comes from PGROUP_WALL, not the -wall flag, and when it runs out
// pgroup names the CI rule and the step so a reader knows to split, cache or
// move it before the runner kills the job.
func TestEnvWallStopsTheGroupAndNamesTheRule(t *testing.T) {
	slowtest.Gate(t, "sh and sleep")
	bin := buildPgroup(t)
	pidFile := filepath.Join(t.TempDir(), "pid")
	cmd := exec.Command(bin, "--", "sh", "-c", `sleep 600 & echo $! > "$1"; wait`, "sh", pidFile)
	cmd.Env = append(os.Environ(), "PGROUP_WALL=1s", "PGROUP_STEP_NAME=env-wall-test", "GITHUB_JOB=")
	out, err := cmd.CombinedOutput()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 124 {
		t.Fatalf("pgroup exit = %v, want 124 after the env wall\n%s", err, out)
	}
	if !strings.Contains(string(out), "CI rule: step 'env-wall-test' ran past 1s") {
		t.Errorf("stderr did not name the CI rule and step:\n%s", out)
	}
	if pid := readPid(t, pidFile); !gone(pid) {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		t.Fatalf("the background sleep (pid %d) outlived the env wall", pid)
	}
}
