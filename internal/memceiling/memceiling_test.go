package memceiling

import (
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
)

// childHold keeps the child allocations reachable for the life of the
// re-run test process, so the allocator cannot reclaim them before the
// ceiling guard stops the process.
var childHold [][]byte

// child allocates and touches memory without bound when the test binary is
// re-run as one: the process each guard must stop.
func init() {
	switch os.Getenv("MEMCEILING_CHILD") {
	case "resident":
		for {
			b := make([]byte, 64<<20)
			for i := 0; i < len(b); i += 4096 {
				b[i] = 1
			}
			childHold = append(childHold, b)
		}
	case "heap":
		stop := Heap(256 << 20)
		for range 1 << 20 {
			childHold = append(childHold, make([]byte, 1<<20))
		}
		stop()
		os.Exit(0)
	}
}

// TestStartKillsADescendantPastTheCeiling: a child that grows without bound
// is killed once it passes a 256 MiB ceiling, and the kill is reported.
func TestStartKillsADescendantPastTheCeiling(t *testing.T) {
	slowtest.Gate(t, "an external toolchain")

	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), "MEMCEILING_CHILD=resident")
	var mu sync.Mutex
	var killed []string
	stop := Start(256<<20, func(what string) {
		mu.Lock()
		killed = append(killed, what)
		mu.Unlock()
	})
	err := cmd.Run()
	stop()
	if err == nil || len(killed) == 0 {
		t.Fatalf("the unbounded child was not stopped: err %v, killed %v", err, killed)
	}
}

// TestHeapPanicsPastTheCeiling: Heap ends a process whose allocation passes
// its ceiling, with the memceiling message.
func TestHeapPanicsPastTheCeiling(t *testing.T) {
	slowtest.Gate(t, "an external toolchain")

	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), "MEMCEILING_CHILD=heap")
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "memceiling:") {
		t.Fatalf("the allocating child was not stopped: err %v\n%s", err, out)
	}
}
