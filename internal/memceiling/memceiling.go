// Package memceiling holds a test, and every process it starts, under a hard
// resident-memory ceiling, so a test whose subject has stopped refusing an
// input fails fast instead of exhausting the machine.
//
// The storage cap tests (docs/SPEC-TABLES.md §11) feed every leg wires that
// command terabytes of storage from kilobytes of bytes. A leg that refuses
// them allocates nothing; a regression, or a negative control that removes
// the refusal, would allocate toward 1 TiB. Each such test runs under one of
// the two guards here:
//
//   - Heap, for Go code the test calls in its own process: a watcher spins
//     on the runtime's allocation counter (runtime/metrics, no clock) and
//     panics past the ceiling, which ends the test binary at once.
//   - Guard, for a leg run as a subprocess (an interpreter, a compiled probe,
//     a VM): a watcher samples the resident size of the test process and of
//     every descendant, and kills any process past the ceiling. The killed
//     probe fails its test, and the test reports which process was killed.
//
// The OS's own address-space limit is not used: macOS does not enforce
// RLIMIT_AS, and a VM reserves address space it never touches.
package memceiling

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"runtime/metrics"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// Default is the ceiling the storage cap tests use: far above what a refusal
// costs (kilobytes) and what a probe's toolchain needs, and far below what
// takes a bench down.
const Default = int64(4) << 30

// Heap starts a watcher over this process's cumulative heap allocation. Past
// limit bytes allocated since the call, it panics, ending the test binary
// before the allocation can grow further. The returned function stops it and
// answers the bytes allocated while it ran.
func Heap(limit int64) (stop func() uint64) {
	sample := []metrics.Sample{{Name: "/gc/heap/allocs:bytes"}}
	read := func() uint64 {
		metrics.Read(sample)
		return sample[0].Value.Uint64()
	}
	base := read()
	var mu sync.Mutex
	done := false
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		for {
			mu.Lock()
			d := done
			mu.Unlock()
			if d {
				return
			}
			if spent := read() - base; spent > uint64(limit) {
				panic(fmt.Sprintf("memceiling: %d bytes allocated, past the %d-byte ceiling; the code under test is building what it should refuse", spent, limit))
			}
			runtime.Gosched()
		}
	}()
	return func() uint64 {
		mu.Lock()
		done = true
		mu.Unlock()
		<-finished
		return read() - base
	}
}

// interval is how often Guard samples the process tree. A process growing at
// a few GiB a second passes the ceiling by at most a fraction of a GiB.
const interval = 25 * time.Millisecond

// Guard holds the test process and every descendant under limit resident
// bytes until the test ends. A process past it is killed, and the test fails
// naming it, so a probe that builds what it should refuse is stopped within
// one sample.
func Guard(t testing.TB, limit int64) {
	t.Helper()
	var mu sync.Mutex
	var killed []string
	stop := Start(limit, func(what string) {
		mu.Lock()
		killed = append(killed, what)
		mu.Unlock()
	})
	t.Cleanup(func() {
		stop()
		mu.Lock()
		defer mu.Unlock()
		for _, k := range killed {
			t.Errorf("memceiling: killed %s, past the %d-byte ceiling; the leg is building what it should refuse", k, limit)
		}
	})
}

// Start is Guard's watcher on its own: it samples this process and every
// descendant, kills a descendant past limit resident bytes and calls killed
// with a description of it, and panics if this process passes it. The
// returned function stops the watcher.
func Start(limit int64, killed func(string)) (stop func()) {
	self := os.Getpid()
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
			}
			for _, p := range overCeiling(self, limit) {
				if p.pid == self {
					panic(fmt.Sprintf("memceiling: the test process holds %d resident bytes, past the %d-byte ceiling", p.rss, limit))
				}
				_ = syscall.Kill(p.pid, syscall.SIGKILL)
				killed(fmt.Sprintf("pid %d (%s) at %d resident bytes", p.pid, p.command, p.rss))
			}
		}
	}()
	return func() {
		close(done)
		<-finished
	}
}

type process struct {
	pid, ppid int
	rss       int64
	command   string
}

// overCeiling answers root and its descendants whose resident size passes
// limit, read from ps (the one view of another process's memory that macOS
// and Linux share).
func overCeiling(root int, limit int64) []process {
	out, err := exec.Command("ps", "-A", "-o", "pid=,ppid=,rss=,comm=").Output()
	if err != nil {
		return nil
	}
	children := map[int][]int{}
	byPid := map[int]process{}
	scan := bufio.NewScanner(bytes.NewReader(out))
	for scan.Scan() {
		f := strings.Fields(scan.Text())
		if len(f) < 4 {
			continue
		}
		pid, e1 := strconv.Atoi(f[0])
		ppid, e2 := strconv.Atoi(f[1])
		kb, e3 := strconv.ParseInt(f[2], 10, 64)
		if e1 != nil || e2 != nil || e3 != nil {
			continue
		}
		p := process{pid: pid, ppid: ppid, rss: kb * 1024, command: strings.Join(f[3:], " ")}
		byPid[pid] = p
		children[ppid] = append(children[ppid], pid)
	}
	var over []process
	queue := []int{root}
	seen := map[int]bool{}
	for len(queue) > 0 {
		pid := queue[0]
		queue = queue[1:]
		if seen[pid] {
			continue
		}
		seen[pid] = true
		if p, ok := byPid[pid]; ok && p.rss > limit {
			over = append(over, p)
		}
		queue = append(queue, children[pid]...)
	}
	return over
}
