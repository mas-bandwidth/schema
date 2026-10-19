package javatable

// THE FUNCTIONAL TIER'S ONE DOOR TO javac AND java. Every program execution in
// this package goes through boundedCommand: it gates on the repo's slow tier
// (slowtest.Gate under SCHEMA_SLOW=1 — there is one mechanism per repo, no
// build tag) and it takes one slot of a process-wide semaphore sized
// max(1, GOMAXPROCS/4), so the bound holds across t.Parallel subtests and
// across top-level parallel tests. The slot is held around the exec only, never
// while waiting on another test.

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func javaSlotCap() int {
	n := runtime.GOMAXPROCS(0) / 4
	if n < 1 {
		return 1
	}
	return n
}

var javaSlots = make(chan struct{}, javaSlotCap())

type boundedCmd struct{ cmd *exec.Cmd }

func boundedCommand(t *testing.T, name string, args ...string) *boundedCmd {
	t.Helper()
	slowtest.Gate(t, "javac/java (it compiles and runs a generated program)")
	return &boundedCmd{cmd: exec.Command(name, args...)}
}

func (c *boundedCmd) Run() error {
	javaSlots <- struct{}{}
	defer func() { <-javaSlots }()
	return c.cmd.Run()
}

func (c *boundedCmd) CombinedOutput() ([]byte, error) {
	javaSlots <- struct{}{}
	defer func() { <-javaSlots }()
	return c.cmd.CombinedOutput()
}

// ---------------------------------------------------------------------------
// THE BOUND PROOF. TestJavaExecutionsStayBounded must fail if the semaphore in
// boundedCommand is removed, and must pass when it is present. To be a real
// proof it has to drive ACTUAL wrapper callers: 4x cap goroutines each start a
// fake program through boundedCommand, and a shared on-disk counter (updated
// under flock) records the peak number of fake programs running at once. The
// fake program is this same test binary re-executed (os.Executable) with
// JAVATABLE_FAKE_PROGRAM set: no javac, no java, no JDK — a few milliseconds of
// process that holds one semaphore slot while it works.
// ---------------------------------------------------------------------------

const fakeProgramEnv = "JAVATABLE_FAKE_PROGRAM"
const fakeCounterEnv = "JAVATABLE_FAKE_COUNTER"

// TestFakeProgram is the subprocess target for the bound proof. When
// JAVATABLE_FAKE_PROGRAM is set, invoking it via `go test -run ^TestFakeProgram$`
// makes it stand in for javac/java: it holds one slot of the shared counter,
// records the concurrent peak, does a little work, and exits 0. In a normal
// suite run (the env var unset) it is a no-op PASS, so it never spawns anything
// and never touches the counter.
func TestFakeProgram(t *testing.T) {
	t.Parallel()
	if os.Getenv(fakeProgramEnv) == "" {
		return
	}
	counterPath := os.Getenv(fakeCounterEnv)
	if counterPath == "" {
		t.Fatal("fake program: " + fakeCounterEnv + " is unset")
	}
	f, err := os.OpenFile(counterPath, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	now, err := lockedBump(f, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := lockedRecordPeak(f, now); err != nil {
		t.Fatal(err)
	}
	if err := lockedBumpRuns(f); err != nil {
		t.Fatal(err)
	}
	time.Sleep(15 * time.Millisecond)
	if _, err := lockedBump(f, -1); err != nil {
		t.Fatal(err)
	}
}

// TestJavaExecutionsStayBounded proves the cap with a fake workload: 4x cap
// callers all run a fake program through the same boundedCommand wrapper the
// javac tests use. Every caller must run (its fake program must complete), and
// the recorded peak of concurrently-running fake programs must never exceed the
// cap. If the semaphore is removed, the peak rises above the cap and the test
// fails. No javac, no clock: the fake program is the test binary itself.
func TestJavaExecutionsStayBounded(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the bound proof's fake executions")

	cap := javaSlotCap()

	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	counterPath := filepath.Join(t.TempDir(), "counter")
	if err := os.Setenv(fakeProgramEnv, "1"); err != nil {
		t.Fatal(err)
	}
	if err := os.Setenv(fakeCounterEnv, counterPath); err != nil {
		t.Fatal(err)
	}

	wantRuns := 4 * cap
	var wg sync.WaitGroup
	errCh := make(chan error, wantRuns)
	for i := 0; i < wantRuns; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cmd := boundedCommand(t, self, "-test.run", "^TestFakeProgram$", "-test.count=1")
			if _, err := cmd.CombinedOutput(); err != nil {
				errCh <- fmt.Errorf("boundedCommand(%s): %w", self, err)
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}

	c, err := readFakeCounter(counterPath)
	if err != nil {
		t.Fatal(err)
	}
	if c.runs != wantRuns {
		t.Fatalf("expected %d fake programs to run, got %d — callers were dropped, not bounded", wantRuns, c.runs)
	}
	if c.count != 0 {
		t.Errorf("after all callers finished, %d fake programs still hold a slot", c.count)
	}
	if c.peak > cap {
		t.Fatalf("recorded peak of %d concurrent fake executions exceeds the cap %d (max(1, GOMAXPROCS/4))", c.peak, cap)
	}
	if c.peak < 1 {
		t.Fatalf("recorded peak %d < 1: no fake execution was observed running concurrently", c.peak)
	}
}

// ---------------------------------------------------------------------------
// The shared counter file. It carries three integers: count (fake programs
// currently running), peak (the largest count observed), runs (fake programs
// that completed a full start->work cycle). Updates are serialized with flock.
// ---------------------------------------------------------------------------

type fakeCounter struct {
	count int
	peak  int
	runs  int
}

func readFakeCounter(path string) (*fakeCounter, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return loadCounter(f)
}

func loadCounter(f *os.File) (*fakeCounter, error) {
	if _, err := f.Seek(0, 0); err != nil {
		return nil, err
	}
	b, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	c := &fakeCounter{}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "count "):
			c.count, _ = strconv.Atoi(strings.TrimPrefix(line, "count "))
		case strings.HasPrefix(line, "peak "):
			c.peak, _ = strconv.Atoi(strings.TrimPrefix(line, "peak "))
		case strings.HasPrefix(line, "runs "):
			c.runs, _ = strconv.Atoi(strings.TrimPrefix(line, "runs "))
		}
	}
	return c, nil
}

func saveCounter(f *os.File, c *fakeCounter) error {
	if err := f.Truncate(0); err != nil {
		return err
	}
	if _, err := f.Seek(0, 0); err != nil {
		return err
	}
	_, err := fmt.Fprintf(f, "count %d\npeak %d\nruns %d\n", c.count, c.peak, c.runs)
	return err
}

func withLock(f *os.File, fn func() error) error {
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
		if err == nil {
			break
		}
		if err == syscall.EINTR {
			continue
		}
		return err
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return fn()
}

func lockedBump(f *os.File, delta int) (int, error) {
	var now int
	err := withLock(f, func() error {
		c, err := loadCounter(f)
		if err != nil {
			return err
		}
		c.count += delta
		now = c.count
		return saveCounter(f, c)
	})
	return now, err
}

func lockedRecordPeak(f *os.File, n int) error {
	return withLock(f, func() error {
		c, err := loadCounter(f)
		if err != nil {
			return err
		}
		if n > c.peak {
			c.peak = n
		}
		return saveCounter(f, c)
	})
}

func lockedBumpRuns(f *os.File) error {
	return withLock(f, func() error {
		c, err := loadCounter(f)
		if err != nil {
			return err
		}
		c.runs++
		return saveCounter(f, c)
	})
}
