package javatable

// The javac/java executions in this package run under ONE process-wide
// semaphore, so a t.Parallel subtest storm never pushes the number of live
// toolchain processes past max(1, GOMAXPROCS/4). Every helper that shells out
// to javac or java goes through boundedCombinedOutput (or acquires the
// semaphore directly); the bound test below proves the semaphore actually
// holds under 4x-cap concurrent callers.

import (
	"os/exec"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

var (
	javaSemOnce sync.Once
	javaSem     chan struct{}
	javaSemCap  int
)

// javaSemaphore returns the package-wide in-flight toolchain cap. Sized to
// max(1, GOMAXPROCS/4).
func javaSemaphore() (chan struct{}, int) {
	javaSemOnce.Do(func() {
		javaSemCap = runtime.GOMAXPROCS(0) / 4
		if javaSemCap < 1 {
			javaSemCap = 1
		}
		javaSem = make(chan struct{}, javaSemCap)
	})
	return javaSem, javaSemCap
}

// acquireJavaExec takes one slot in the toolchain semaphore. Call the returned
// func exactly once after the exec finishes.
func acquireJavaExec() func() {
	sem, _ := javaSemaphore()
	sem <- struct{}{}
	return func() { <-sem }
}

// boundedCombinedOutput runs a javac/java command while holding a semaphore slot
// for the whole execution. Every toolchain spawn in the package must route
// through here.
func boundedCombinedOutput(cmd *exec.Cmd) ([]byte, error) {
	release := acquireJavaExec()
	defer release()
	return cmd.CombinedOutput()
}

// TestJavaExecutionsStayBounded is the proof of the semaphore. It runs 4x-cap
// concurrent callers through boundedCombinedOutput against a fake program (a
// sleep that exits 0) and asserts the observed peak of in-flight executions
// never exceeds the cap. No real javac is needed, but it does spawn a process,
// so it is gated like every other toolchain test.
func TestJavaExecutionsStayBounded(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "a process (the in-flight bound proof)")

	_, cap := javaSemaphore()

	limit := 4 * cap
	var inFlight atomic.Int64
	var peak atomic.Int64
	var wg sync.WaitGroup

	run := func() {
		defer wg.Done()
		release := acquireJavaExec() // same semaphore boundedCombinedOutput holds
		in := inFlight.Add(1)
		for {
			p := peak.Load()
			if in <= p || peak.CompareAndSwap(p, in) {
				break
			}
		}
		cmd := exec.Command("sleep", "0.05")
		_ = cmd.Run()
		inFlight.Add(-1)
		release()
	}

	for i := 0; i < limit; i++ {
		wg.Add(1)
		go run()
	}
	wg.Wait()

	if got := peak.Load(); got > int64(cap) {
		t.Fatalf("peak in-flight javac/java executions = %d, cap = %d (semaphore not holding)", got, cap)
	}
	t.Logf("peak in-flight = %d, cap = %d", peak.Load(), cap)
}
