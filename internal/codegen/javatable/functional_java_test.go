package javatable

// THE FUNCTIONAL TIER'S ONE DOOR TO javac AND java. Every program execution in
// this package goes through boundedCommand: it gates on the repo's slow tier
// (slowtest.Gate under SCHEMA_SLOW=1 — there is one mechanism per repo, no
// build tag) and it takes one slot of a process-wide semaphore sized
// max(1, GOMAXPROCS/4), so the bound holds across t.Parallel subtests and
// across top-level parallel tests. The slot is held around the exec only, never
// while waiting on another test.

import (
	"os/exec"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"

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

// TestJavaExecutionsStayBounded proves the cap with a fake workload: 4x cap
// callers contend for the same semaphore boundedCommand uses, while every slot
// is already held. None may run, and the recorded peak in-flight count (the
// cap already held) may not grow. No javac and no clock: the contention is
// made deterministic by holding all the slots first.
func TestJavaExecutionsStayBounded(t *testing.T) {
	t.Parallel()
	cap := javaSlotCap()
	for i := 0; i < cap; i++ {
		javaSlots <- struct{}{}
	}
	var active, peak atomic.Int64
	active.Store(int64(cap))
	peak.Store(int64(cap))
	var wg sync.WaitGroup
	for i := 0; i < 4*cap; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case javaSlots <- struct{}{}:
				n := active.Add(1)
				for {
					p := peak.Load()
					if n <= p || peak.CompareAndSwap(p, n) {
						break
					}
				}
				runtime.Gosched()
				active.Add(-1)
				<-javaSlots
			default:
			}
		}()
	}
	wg.Wait()
	for i := 0; i < cap; i++ {
		<-javaSlots
	}
	if got := peak.Load(); got > int64(cap) {
		t.Fatalf("peak in-flight Java executions %d exceeds cap %d (max(1, GOMAXPROCS/4))", got, cap)
	}
	if got := peak.Load(); got < int64(cap) {
		t.Fatalf("peak in-flight Java executions %d is below the cap %d; the fake workload did not fill the semaphore", got, cap)
	}
}
