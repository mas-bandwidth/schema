//go:build functional

package javatable

import (
	"os/exec"
	"runtime"
	"sync"
	"testing"
	"time"
)

// javaSlotCount is the functional tier's process-wide bound on javac/java
// executions: max(1, GOMAXPROCS/4). It is held only around the exec, never
// across a wait on another test, so parallel subtests cannot deadlock.
var javaSlotCount = max(1, runtime.GOMAXPROCS(0)/4)

var (
	javaSlots    = make(chan struct{}, javaSlotCount)
	javaInFlight atomicInt64
	javaPeak     atomicInt64
)

type atomicInt64 struct {
	mu sync.Mutex
	v  int64
}

func (a *atomicInt64) Add(d int64) int64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.v += d
	return a.v
}

func (a *atomicInt64) Load() int64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.v
}

func withJavaSlot(fn func() error) error {
	javaSlots <- struct{}{}
	defer func() { <-javaSlots }()
	n := javaInFlight.Add(1)
	defer javaInFlight.Add(-1)
	for {
		p := javaPeak.Load()
		if n <= p {
			break
		}
		javaPeak.mu.Lock()
		if javaPeak.v < n {
			javaPeak.v = n
		}
		javaPeak.mu.Unlock()
		break
	}
	return fn()
}

// boundedCmd is the one door every javac and java execution in this package
// goes through. Run, CombinedOutput and Output take the process-wide slot;
// callers that hold a slot while waiting on another test would deadlock, so the
// slot is held around the exec only.
type boundedCmd struct{ *exec.Cmd }

func boundedCommand(name string, args ...string) *boundedCmd {
	return &boundedCmd{exec.Command(name, args...)}
}

func boundedLookPath(file string) (string, error) {
	return exec.LookPath(file)
}

func (c *boundedCmd) Run() error {
	return withJavaSlot(c.Cmd.Run)
}

func (c *boundedCmd) Output() ([]byte, error) {
	var out []byte
	var err error
	err = withJavaSlot(func() error {
		out, err = c.Cmd.Output()
		return err
	})
	return out, err
}

func (c *boundedCmd) CombinedOutput() ([]byte, error) {
	var out []byte
	var err error
	err = withJavaSlot(func() error {
		out, err = c.Cmd.CombinedOutput()
		return err
	})
	return out, err
}

// TestFunctionalJavaExecBound proves the bound without a real javac: four
// times the cap callers enter the same door and the recorded peak in-flight
// count never exceeds the cap.
func TestFunctionalJavaExecBound(t *testing.T) {
	t.Parallel()
	cap := javaSlotCount
	var wg sync.WaitGroup
	for i := 0; i < cap*4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = withJavaSlot(func() error {
				time.Sleep(5 * time.Millisecond)
				return nil
			})
		}()
	}
	wg.Wait()
	if peak := javaPeak.Load(); peak > int64(cap) {
		t.Fatalf("peak in-flight javac/java executions = %d, want <= cap %d", peak, cap)
	}
}
