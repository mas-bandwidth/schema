package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func buildTreelock(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "treelock")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to build treelock: %v\n%s", err, out)
	}
	return bin
}

func TestTreelockExclusivity(t *testing.T) {
	bin := buildTreelock(t)
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "test.lock")
	ownerPath := filepath.Join(dir, "owner")

	// Start holder that sleeps for 2 seconds
	holder := exec.Command(bin, "-lock", lockPath, "-owner-file", ownerPath, "-name", "test-lock", "sleep", "2")
	var holderStderr bytes.Buffer
	holder.Stderr = &holderStderr
	if err := holder.Start(); err != nil {
		t.Fatalf("holder failed to start: %v", err)
	}
	defer func() {
		if holder.Process != nil {
			_ = holder.Process.Kill()
			_ = holder.Wait()
		}
	}()

	// Wait for holder to acquire the lock
	acquired := false
	for i := 0; i < 50; i++ {
		if data, err := os.ReadFile(lockPath); err == nil && len(bytes.TrimSpace(data)) > 0 {
			acquired = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !acquired {
		t.Fatalf("holder never acquired lock: %s", holderStderr.String())
	}

	// Contender attempts to acquire the lock while holder is running
	contender := exec.Command(bin, "-lock", lockPath, "-owner-file", ownerPath, "-name", "test-lock", "echo", "should-not-run")
	var contenderStderr bytes.Buffer
	contender.Stderr = &contenderStderr
	err := contender.Run()
	if err == nil {
		t.Fatalf("contender succeeded unexpectedly while lock is held (holder err: %s)", holderStderr.String())
	}

	out := contenderStderr.String()
	if !strings.Contains(out, "REFUSED") {
		t.Errorf("contender stderr does not say REFUSED: %q", out)
	}
	if strings.Contains(out, "pid unknown") {
		t.Errorf("contender stderr reported unknown pid: %q", out)
	}

	// Now wait for holder to exit
	if err := holder.Wait(); err != nil {
		t.Fatalf("holder failed: %v", err)
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

	// Start holder
	holder := exec.Command(bin, "-lock", lockPath, "-owner-file", ownerPath, "-name", "test-lock", "sleep", "10")
	if err := holder.Start(); err != nil {
		t.Fatalf("holder failed to start: %v", err)
	}

	for i := 0; i < 50; i++ {
		if data, err := os.ReadFile(lockPath); err == nil && len(bytes.TrimSpace(data)) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Kill holder with SIGKILL
	_ = holder.Process.Signal(syscall.SIGKILL)
	_ = holder.Wait()

	// Kernel should have released lock immediately. Reclaimer must succeed.
	reclaimer := exec.Command(bin, "-lock", lockPath, "-owner-file", ownerPath, "-name", "test-lock", "echo", "reclaimed")
	if out, err := reclaimer.CombinedOutput(); err != nil {
		t.Fatalf("reclaimer failed to acquire lock after holder death: %v\n%s", err, out)
	}
}
