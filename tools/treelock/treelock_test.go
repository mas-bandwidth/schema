//go:build !windows

package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

var (
	builtBinOnce sync.Once
	builtBinDir  string
	builtBinPath string
	builtBinErr  error
)

func TestMain(m *testing.M) {
	code := m.Run()
	if builtBinDir != "" {
		_ = os.RemoveAll(builtBinDir)
	}
	os.Exit(code)
}

func buildTreelock(t *testing.T) string {
	t.Helper()
	builtBinOnce.Do(func() {
		dir, err := os.MkdirTemp("", "treelock-test-bin-*")
		if err != nil {
			builtBinErr = err
			return
		}
		builtBinDir = dir
		bin := filepath.Join(dir, "treelock")
		cmd := exec.Command("go", "build", "-o", bin, ".")
		if out, err := cmd.CombinedOutput(); err != nil {
			builtBinErr = fmt.Errorf("build treelock: %w\n%s", err, out)
			return
		}
		builtBinPath = bin
	})
	if builtBinErr != nil {
		t.Fatalf("failed to build treelock: %v", builtBinErr)
	}
	return builtBinPath
}

func createFifo(t *testing.T, path string) {
	t.Helper()
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatalf("failed to create fifo %s: %v", path, err)
	}
}

func releaseFifo(path string, timeout ...time.Duration) error {
	wait := 100 * time.Millisecond
	if len(timeout) > 0 {
		wait = timeout[0]
	}
	deadline := time.Now().Add(wait)
	for {
		fd, err := syscall.Open(path, syscall.O_WRONLY|syscall.O_NONBLOCK, 0)
		if err == nil {
			f := os.NewFile(uintptr(fd), path)
			_, writeErr := f.Write([]byte("ok\n"))
			closeErr := f.Close()
			if writeErr != nil {
				return writeErr
			}
			return closeErr
		}
		if !errors.Is(err, syscall.ENXIO) {
			return fmt.Errorf("open fifo nonblock %s: %w", path, err)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("release fifo timed out after %v (no reader on %s): %w", wait, path, err)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func waitForFile(t *testing.T, path string, done <-chan error, stderrPath string, timeout time.Duration) []byte {
	t.Helper()
	readStderr := func() string {
		data, err := os.ReadFile(stderrPath)
		if err != nil {
			return fmt.Sprintf("<error reading stderr: %v>", err)
		}
		return string(data)
	}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case err := <-done:
			t.Fatalf("process exited prematurely with err %v while waiting for %s; stderr:\n%s", err, path, readStderr())
		default:
		}
		if data, err := os.ReadFile(path); err == nil && len(bytes.TrimSpace(data)) > 0 {
			return bytes.TrimSpace(data)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out after %v waiting for %s; stderr:\n%s", timeout, path, readStderr())
	return nil
}

func TestReviewerFifoWithoutReaderIsBounded(t *testing.T) {
	p := filepath.Join(t.TempDir(), "release.fifo")
	createFifo(t, p)
	err := releaseFifo(p)
	if err == nil {
		t.Fatal("expected error releasing fifo without reader, got nil")
	}
}

func TestFifoEarlyChildExitIsBounded(t *testing.T) {
	bin := buildTreelock(t)
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "test.lock")
	ownerPath := filepath.Join(dir, "owner")
	readyPath := filepath.Join(dir, "ready")
	fifoPath := filepath.Join(dir, "release.fifo")
	stderrPath := filepath.Join(dir, "early_exit.stderr")

	createFifo(t, fifoPath)

	stderrFile, err := os.Create(stderrPath)
	if err != nil {
		t.Fatalf("failed to create stderr file: %v", err)
	}
	defer stderrFile.Close()

	cmd := exec.Command(bin, "-lock", lockPath, "-owner-file", ownerPath, "-name", "test-lock",
		"sh", "-c", "echo ready > \"$1\"; exit 1", "--", readyPath)
	cmd.Stderr = stderrFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start cmd: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
	})

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected child to exit with error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for child to exit")
	}

	start := time.Now()
	err = releaseFifo(fifoPath, 100*time.Millisecond)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected releaseFifo to fail when child exited early, got nil")
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("releaseFifo took too long (%v), expected bounded return under 500ms", elapsed)
	}
}

func TestTreelockExclusivity(t *testing.T) {
	bin := buildTreelock(t)
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "test.lock")
	ownerPath := filepath.Join(dir, "owner")
	readyPath := filepath.Join(dir, "ready")
	fifoPath := filepath.Join(dir, "release.fifo")
	stderrPath := filepath.Join(dir, "holder.stderr")

	createFifo(t, fifoPath)

	stderrFile, err := os.Create(stderrPath)
	if err != nil {
		t.Fatalf("failed to create stderr file: %v", err)
	}
	defer stderrFile.Close()

	// Start holder that signals readiness and blocks on FIFO until explicitly released
	holder := exec.Command(bin, "-lock", lockPath, "-owner-file", ownerPath, "-name", "test-lock",
		"sh", "-c", "echo ready > \"$1\"; read -r line < \"$2\"", "--", readyPath, fifoPath)
	holder.Stderr = stderrFile
	holder.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := holder.Start(); err != nil {
		t.Fatalf("holder failed to start: %v", err)
	}
	holderDone := make(chan error, 1)
	go func() {
		holderDone <- holder.Wait()
	}()

	t.Cleanup(func() {
		if holder.Process != nil {
			_ = syscall.Kill(-holder.Process.Pid, syscall.SIGKILL)
		}
	})

	// Wait for holder to acquire lock and signal readiness
	waitForFile(t, readyPath, holderDone, stderrPath, 10*time.Second)

	// Contender attempts to acquire the lock while holder is running
	contender := exec.Command(bin, "-lock", lockPath, "-owner-file", ownerPath, "-name", "test-lock", "echo", "should-not-run")
	var contenderStderr bytes.Buffer
	contender.Stderr = &contenderStderr
	err = contender.Run()
	if err == nil {
		t.Fatalf("contender succeeded unexpectedly while lock is held (holder err: %s)", stderrPath)
	}

	out := contenderStderr.String()
	if !strings.Contains(out, "REFUSED") {
		t.Errorf("contender stderr does not say REFUSED: %q", out)
	}
	if strings.Contains(out, "pid unknown") {
		t.Errorf("contender stderr reported unknown pid: %q", out)
	}

	// Verify holder did not terminate during contention check
	select {
	case err := <-holderDone:
		t.Fatalf("holder terminated early during contention check: %v", err)
	default:
	}

	// Now release holder and wait for clean exit
	if err := releaseFifo(fifoPath, 2*time.Second); err != nil {
		t.Fatalf("failed to release fifo: %v", err)
	}
	select {
	case err := <-holderDone:
		if err != nil {
			t.Fatalf("holder exited with error after release: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("holder timed out waiting to exit after release")
	}

	// After holder exits, a second contender must acquire lock and succeed
	second := exec.Command(bin, "-lock", lockPath, "-owner-file", ownerPath, "-name", "test-lock", "echo", "success")
	if secondOut, err := second.CombinedOutput(); err != nil {
		t.Fatalf("second run failed after holder exit: %v\n%s", err, secondOut)
	}
}

func TestTreelockOwnerDeathRelease(t *testing.T) {
	bin := buildTreelock(t)
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "test.lock")
	ownerPath := filepath.Join(dir, "owner")
	readyPath := filepath.Join(dir, "ready")
	fifoPath := filepath.Join(dir, "release.fifo")
	stderrPath := filepath.Join(dir, "holder.stderr")

	createFifo(t, fifoPath)

	stderrFile, err := os.Create(stderrPath)
	if err != nil {
		t.Fatalf("failed to create stderr file: %v", err)
	}
	defer stderrFile.Close()

	// Start holder in its own process group
	holder := exec.Command(bin, "-lock", lockPath, "-owner-file", ownerPath, "-name", "test-lock",
		"sh", "-c", "echo ready > \"$1\"; read -r line < \"$2\"", "--", readyPath, fifoPath)
	holder.Stderr = stderrFile
	holder.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := holder.Start(); err != nil {
		t.Fatalf("holder failed to start: %v", err)
	}
	holderDone := make(chan error, 1)
	go func() {
		holderDone <- holder.Wait()
	}()

	t.Cleanup(func() {
		if holder.Process != nil {
			_ = syscall.Kill(-holder.Process.Pid, syscall.SIGKILL)
		}
	})

	// Wait for holder readiness
	waitForFile(t, readyPath, holderDone, stderrPath, 10*time.Second)

	// Kill entire holder process group with SIGKILL
	if err := syscall.Kill(-holder.Process.Pid, syscall.SIGKILL); err != nil {
		t.Fatalf("failed to kill process group %d: %v", holder.Process.Pid, err)
	}
	select {
	case <-holderDone:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for holder process to terminate after SIGKILL")
	}

	// Kernel should have released lock immediately. Reclaimer must succeed.
	reclaimer := exec.Command(bin, "-lock", lockPath, "-owner-file", ownerPath, "-name", "test-lock", "echo", "reclaimed")
	if out, err := reclaimer.CombinedOutput(); err != nil {
		t.Fatalf("reclaimer failed to acquire lock after holder death: %v\n%s", err, out)
	}
}

func TestTreelockWrapperOnlyDeathInheritedLock(t *testing.T) {
	bin := buildTreelock(t)
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "test.lock")
	ownerPath := filepath.Join(dir, "owner")
	childPidFile := filepath.Join(dir, "child.pid")
	fifoPath := filepath.Join(dir, "child.fifo")
	stderrPath := filepath.Join(dir, "holder.stderr")

	createFifo(t, fifoPath)

	stderrFile, err := os.Create(stderrPath)
	if err != nil {
		t.Fatalf("failed to create stderr file: %v", err)
	}
	defer stderrFile.Close()

	// Start treelock with a child that records its PID and blocks on FIFO until released
	holder := exec.Command(bin, "-lock", lockPath, "-owner-file", ownerPath, "-name", "test-lock",
		"sh", "-c", "echo $$ > \"$1\"; read -r line < \"$2\"", "--", childPidFile, fifoPath)
	holder.Stderr = stderrFile
	holder.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := holder.Start(); err != nil {
		t.Fatalf("holder failed to start: %v", err)
	}
	wrapperPid := holder.Process.Pid
	holderDone := make(chan error, 1)
	go func() {
		holderDone <- holder.Wait()
	}()

	var childPidInt int
	t.Cleanup(func() {
		if holder.Process != nil {
			_ = syscall.Kill(-holder.Process.Pid, syscall.SIGKILL)
		}
		if childPidInt > 0 {
			_ = syscall.Kill(childPidInt, syscall.SIGKILL)
		}
	})

	// Wait for child process to write its PID
	childPidBytes := waitForFile(t, childPidFile, holderDone, stderrPath, 10*time.Second)
	childPidStr := string(childPidBytes)
	var convErr error
	childPidInt, convErr = strconv.Atoi(childPidStr)
	if convErr != nil || childPidInt <= 0 {
		t.Fatalf("invalid child pid %q: %v", childPidStr, convErr)
	}

	// Verify both wrapper and child are alive
	if !isAlive(strconv.Itoa(wrapperPid)) {
		t.Fatalf("wrapper process %d is not alive", wrapperPid)
	}
	if !isAlive(childPidStr) {
		t.Fatalf("child process %s is not alive", childPidStr)
	}

	// Kill ONLY the treelock wrapper PID (leaving child alive)
	if err := syscall.Kill(wrapperPid, syscall.SIGKILL); err != nil {
		t.Fatalf("failed to kill wrapper %d: %v", wrapperPid, err)
	}
	select {
	case <-holderDone:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for wrapper to exit after SIGKILL")
	}

	// Child must still be alive
	if !isAlive(childPidStr) {
		t.Fatalf("child process %s died unexpectedly after wrapper was killed", childPidStr)
	}

	// Because child inherited lock FD via ExtraFiles, lock remains held by child.
	// Contender must be refused.
	contender := exec.Command(bin, "-lock", lockPath, "-owner-file", ownerPath, "-name", "test-lock", "echo", "should-not-run")
	var contenderStderr bytes.Buffer
	contender.Stderr = &contenderStderr
	err = contender.Run()
	if err == nil {
		t.Fatalf("contender succeeded unexpectedly while child still holds lock!")
	}
	out := contenderStderr.String()
	if !strings.Contains(out, "REFUSED") {
		t.Errorf("contender stderr does not say REFUSED: %q", out)
	}

	// Release the surviving child process cleanly via FIFO
	if err := releaseFifo(fifoPath, 2*time.Second); err != nil {
		t.Fatalf("failed to release child fifo: %v", err)
	}

	// Wait up to 5s for child to terminate cleanly
	deadline := time.Now().Add(5 * time.Second)
	for isAlive(childPidStr) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if isAlive(childPidStr) {
		_ = syscall.Kill(childPidInt, syscall.SIGKILL)
		t.Fatalf("child %s failed to terminate after FIFO release", childPidStr)
	}

	// Now that child is dead, lock should be released. A new invocation must succeed.
	reclaimer := exec.Command(bin, "-lock", lockPath, "-owner-file", ownerPath, "-name", "test-lock", "echo", "reclaimed")
	if out, err := reclaimer.CombinedOutput(); err != nil {
		t.Fatalf("reclaimer failed to acquire lock after child death: %v\n%s", err, out)
	}
}
