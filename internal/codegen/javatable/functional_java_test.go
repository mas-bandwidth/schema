package javatable

// THE FUNCTIONAL TIER'S ONE DOOR TO javac AND java. Every program execution in
// this package is built by boundedCommand: it gates on the repo's slow tier
// (slowtest.Gate under SCHEMA_SLOW=1; one mechanism per repo, no build tag)
// and hands the command to javaDoor, which runs it holding one slot of a
// process-wide semaphore sized max(1, GOMAXPROCS/4). The bound therefore holds
// across t.Parallel subtests and across top-level parallel tests. The slot is
// held around the exec only, never while a test waits on another test.
//
// TestUnitTierSpawnsNoProgram (unit_spawns_nothing_test.go) holds the shape:
// exec.Command may appear in boundedCommand alone, and every exec site in the
// package must sit under a slowtest.Gate. TestJavaExecutionsStayBounded below
// holds the bound itself.

import (
	"fmt"
	"os/exec"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// javaSlotCap is the in-flight bound: max(1, GOMAXPROCS/4).
func javaSlotCap() int {
	n := runtime.GOMAXPROCS(0) / 4
	if n < 1 {
		return 1
	}
	return n
}

// programDoor runs programs, at most cap(slots) at once. run is the program
// executor: the real one for javaDoor, a fake one in the bound proof. Both go
// through the same acquire in do, so a proof of a door is a proof of javaDoor.
type programDoor struct {
	slots chan struct{}
	run   func(cmd *exec.Cmd, combined bool) ([]byte, error)
}

func newProgramDoor(capacity int, run func(cmd *exec.Cmd, combined bool) ([]byte, error)) *programDoor {
	return &programDoor{slots: make(chan struct{}, capacity), run: run}
}

// runProgram is the real executor: it starts the program. Only javaDoor uses it.
func runProgram(cmd *exec.Cmd, combined bool) ([]byte, error) {
	if combined {
		return cmd.CombinedOutput()
	}
	return nil, cmd.Run()
}

// javaDoor is the package's one bounded door; every boundedCommand goes through it.
var javaDoor = newProgramDoor(javaSlotCap(), runProgram)

// do runs cmd holding one slot: the acquire is here and nowhere else.
func (d *programDoor) do(cmd *exec.Cmd, combined bool) ([]byte, error) {
	d.slots <- struct{}{}
	defer func() { <-d.slots }()
	return d.run(cmd, combined)
}

// wrap ties a built command to this door.
func (d *programDoor) wrap(cmd *exec.Cmd) *boundedCmd {
	return &boundedCmd{cmd: cmd, door: d}
}

// boundedCmd is a command that can only run through its door.
type boundedCmd struct {
	cmd  *exec.Cmd
	door *programDoor
}

func (c *boundedCmd) Run() error {
	_, err := c.door.do(c.cmd, false)
	return err
}

func (c *boundedCmd) CombinedOutput() ([]byte, error) {
	return c.door.do(c.cmd, true)
}

// boundedCommand is the only place in the package that builds a program: gated
// on the slow tier, and run through javaDoor.
func boundedCommand(t *testing.T, name string, args ...string) *boundedCmd {
	t.Helper()
	slowtest.Gate(t, "javac/java (it compiles and runs a generated program)")
	return javaDoor.wrap(exec.Command(name, args...))
}

// TestJavaExecutionsStayBounded proves the door holds its bound. It is a unit
// test: no process, no clock. 4x cap callers run a fake program through
// boundedCmd.Run and boundedCmd.CombinedOutput, the two methods every
// boundedCommand caller uses, on a door built exactly as javaDoor is but with a
// fake executor that blocks until the test lets it go. testing/synctest's
// Wait returns once every caller is either inside the fake program or blocked
// at the door's acquire, so the in-flight count read then is exact, not a race:
// with the acquire it is min(cap, remaining) every round and the peak is cap;
// with the acquire removed every caller is inside at once and the test fails.
func TestJavaExecutionsStayBounded(t *testing.T) {
	t.Parallel()
	want := runtime.GOMAXPROCS(0) / 4
	if want < 1 {
		want = 1
	}
	if got := javaSlotCap(); got != want {
		t.Fatalf("javaSlotCap() = %d, want max(1, GOMAXPROCS/4) = %d", got, want)
	}
	if got := cap(javaDoor.slots); got != want {
		t.Fatalf("javaDoor admits %d at once, want max(1, GOMAXPROCS/4) = %d", got, want)
	}
	caps := []int{1, 2, 3}
	if want > 3 {
		caps = append(caps, want)
	}
	for _, capacity := range caps {
		t.Run(fmt.Sprintf("cap=%d", capacity), func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) { proveDoorBound(t, capacity) })
		})
	}
}

func proveDoorBound(t *testing.T, capacity int) {
	var inFlight, peak, ran atomic.Int64
	release := make(chan struct{})
	door := newProgramDoor(capacity, func(cmd *exec.Cmd, combined bool) ([]byte, error) {
		n := inFlight.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		_, open := <-release
		inFlight.Add(-1)
		ran.Add(1)
		if !open {
			return nil, fmt.Errorf("drained after a failure")
		}
		if combined {
			return []byte("ok " + cmd.Path), nil
		}
		return nil, nil
	})

	callers := 4 * capacity
	var wg sync.WaitGroup
	for i := range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c := door.wrap(&exec.Cmd{Path: fmt.Sprintf("/fake/javac-%d", i)})
			if i%2 == 0 {
				out, err := c.CombinedOutput()
				if err == nil && string(out) != "ok "+c.cmd.Path {
					t.Errorf("caller %d: fake output %q", i, out)
				}
				return
			}
			_ = c.Run()
		}()
	}

	released := 0
	for released < callers {
		synctest.Wait()
		n := int(inFlight.Load())
		expect := min(capacity, callers-released)
		if n != expect {
			t.Errorf("round after %d released: %d fake programs in flight at once, want %d (cap %d, %d callers)",
				released, n, expect, capacity, callers)
			close(release)
			break
		}
		for range n {
			release <- struct{}{}
		}
		released += n
	}
	wg.Wait()

	if got := peak.Load(); got > int64(capacity) {
		t.Fatalf("peak in-flight fake programs = %d, cap = %d: the door did not hold its bound", got, capacity)
	} else if got != int64(capacity) {
		t.Fatalf("peak in-flight fake programs = %d, cap = %d: the door admitted fewer than its cap", got, capacity)
	}
	if got := ran.Load(); got != int64(callers) {
		t.Fatalf("%d of %d callers ran: callers were dropped, not bounded", got, callers)
	}
}
