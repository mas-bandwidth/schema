//go:build unix

package main

import (
	"context"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestARestartedShardKillsWhatItsDeadPredecessorLeft: a unit runs in a
// process group of its own, so a signal to the shard does not reach it
// first, and a shard started again kills every group still carrying its
// marker and nothing else. The processes are killed, never waited out.
func TestARestartedShardKillsWhatItsDeadPredecessorLeft(t *testing.T) {
	t.Parallel()
	marker := t.TempDir()
	if _, err := os.Stat("/proc"); err != nil {
		// a unix bench with no /proc: a shard started again refuses to run
		if _, err := reapShard(marker, func(time.Duration) {}); err == nil || !strings.Contains(err.Error(), "dead shard left running") {
			t.Fatalf("with no /proc reapShard returned %v, want a refusal", err)
		}
		return
	}
	start := func(m string) *os.Process {
		c := unitCmd(context.Background(), []string{"sleep", "100000"}, map[string]string{shardMarker: m}, io.Discard)
		if err := c.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = c.Process.Kill(); _ = c.Wait() })
		if g, err := syscall.Getpgid(c.Process.Pid); err != nil || g != c.Process.Pid {
			t.Fatalf("a unit runs in process group %d (%v), want its own, %d", g, err, c.Process.Pid)
		}
		return c.Process
	}
	left := start(marker)
	other := start(marker + "-another-shard")
	n, err := reapShard(marker, func(time.Duration) { runtime.Gosched() })
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("reapShard found %d processes, want the 1 left running", n)
	}
	if procLive(left.Pid) {
		t.Fatal("the unit the dead shard left running still runs")
	}
	if !procLive(other.Pid) {
		t.Fatal("reapShard killed another shard's unit")
	}
}

// TestOneShardAtATimeInAnOutDirectory: a second shard in an out directory
// refuses while the first holds it, and runs once it is gone; a launcher
// pid a new process took over is not the launcher.
//
// Serial, not parallel: a sibling test's fork shares the lock's open file
// description until its exec, so a lock taken again at once could see it
// held for that moment.
func TestOneShardAtATimeInAnOutDirectory(t *testing.T) {
	dir := t.TempDir()
	unlock, err := lockOut(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lockOut(dir); err == nil || !strings.Contains(err.Error(), "another shard runs") {
		t.Fatalf("a second shard in a held out directory: %v", err)
	}
	r := &Runner{M: Manifest{Shard: "s", Units: []Unit{makeUnit("a")}}, Out: dir, Cmd: (&fakeCmd{}).run,
		Now: func() time.Time { return time.Unix(0, 0) }, Log: io.Discard}
	if err := r.Run(context.Background()); err == nil || !strings.Contains(err.Error(), "another shard runs") {
		t.Fatalf("a shard ran in an out directory another holds: %v", err)
	}
	unlock()
	again, err := lockOut(dir)
	if err != nil {
		t.Fatalf("an out directory its shard left: %v", err)
	}
	again()
	if _, err := os.Stat("/proc/self/cmdline"); err != nil {
		if !launcherAlive(os.Getpid(), dir) {
			t.Fatal("with no /proc, a running pid is not alive")
		}
		return
	}
	c := unitCmd(context.Background(), []string{"sleep", "100000"}, nil, io.Discard)
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Process.Kill(); _ = c.Wait() })
	// the kernel shows the command line once exec has set it up
	cmdline := "/proc/" + strconv.Itoa(c.Process.Pid) + "/cmdline"
	for i := 0; ; i++ {
		if b, err := os.ReadFile(cmdline); err != nil || len(b) > 0 {
			break
		}
		if i == 1_000_000 {
			t.Fatal("the child's command line never showed")
		}
		runtime.Gosched()
	}
	if !launcherAlive(c.Process.Pid, "100000") {
		t.Fatal("a launcher running with its out directory is not alive")
	}
	if launcherAlive(c.Process.Pid, dir) {
		t.Fatal("a process running for another out directory counts as the launcher")
	}
}

// TestAStartBesideALiveShardRefuses: starting a shard whose launcher still
// runs (its bench stopped answering, and it was taken for dead) refuses,
// and leaves the live shard's temporary files alone.
func TestAStartBesideALiveShardRefuses(t *testing.T) {
	t.Parallel()
	st := &State{Base: t.TempDir(), RunID: "r1"}
	s := &ShardState{Shard: Shard{ID: "h-1", Bench: "h", Clone: 1}}
	o := st.shardDir(s)
	for _, d := range []string{o, o + ".tmp", st.runDir() + "/c1/schema", st.runDir() + "/bin"} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for p, body := range map[string]string{
		o + "/launcher.pid":          "1\n",
		o + ".tmp/keep":              "x",
		st.runDir() + "/bin/fullrun": "#!/bin/sh\necho '{\"done\":false,\"alive\":true,\"results\":0,\"current\":\"\"}'\n",
	} {
		if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	script := startScript(st, s)
	if i, j := strings.Index(script, "shard-status"), strings.Index(script, "env.sh"); i < 0 || j < 0 || i > j {
		t.Errorf("the start reads the bench environment before it checks for a live shard")
	}
	// testinputs:none — the start script and its fake launcher are written under t.TempDir
	out, err := exec.Command("bash", "-c", script).CombinedOutput()
	if err == nil || !strings.Contains(string(out), "still runs") {
		t.Fatalf("a start beside a live shard: %v: %s", err, out)
	}
	if _, err := os.Stat(o + ".tmp/keep"); err != nil {
		t.Fatalf("the live shard's temporary files went: %v", err)
	}
}
