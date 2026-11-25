package ci

// A JOB COUNT HONOURS CPU AFFINITY. `getconf _NPROCESSORS_ONLN` counts every
// CPU the host has online and ignores the affinity a nova-reserve slot (taskset,
// a cpuset) puts on the process: inside a one-CPU slot CPP_HOSTILE_JOBS said
// "compiling with 16 jobs" and sixteen compilers fought over one core. `nproc`
// counts the CPUs the process may run on, and sysctl -n hw.ncpu answers where
// a host has no nproc (macOS). Every make file and make check script sizes its
// workers that way, as make/c.mk's C_HOSTILE_JOBS does.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// checkJobCountHonoursAffinity: no line of src counts CPUs with
// _NPROCESSORS_ONLN. A comment line is the exception: it may name the
// variable to say why it is not used.
func checkJobCountHonoursAffinity(name, src string) []string {
	var bad []string
	for i, line := range strings.Split(src, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if strings.Contains(line, "_NPROCESSORS_ONLN") {
			bad = append(bad, fmt.Sprintf("%s:%d counts CPUs with _NPROCESSORS_ONLN, which ignores CPU affinity (a nova-reserve slot): use `nproc 2>/dev/null || sysctl -n hw.ncpu 2>/dev/null || echo 4`", name, i+1))
		}
	}
	return bad
}

func TestMakeJobCountsHonourCPUAffinity(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	var files []string
	for _, pat := range []string{"Makefile", "make/*.mk", "make/checks/*.sh", "tools/*/*.mk"} {
		m, err := filepath.Glob(filepath.Join(root, filepath.FromSlash(pat)))
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, m...)
	}
	counted := 0
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		rel, err := filepath.Rel(root, f)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "nproc") {
			counted++
		}
		for _, msg := range checkJobCountHonoursAffinity(rel, string(b)) {
			t.Error(msg)
		}
	}
	// the scan found the files that size workers: a glob that matched nothing
	// would pass for the wrong reason
	if counted < 5 {
		t.Errorf("only %d scanned files count CPUs with nproc; the scan no longer reaches the make files", counted)
	}
	// the fixture pair: the nproc form passes, the getconf form is caught
	good := "CPP_HOSTILE_JOBS ?= $(shell nproc 2>/dev/null || sysctl -n hw.ncpu 2>/dev/null || echo 4)\n# not getconf _NPROCESSORS_ONLN: it ignores affinity\n"
	if got := checkJobCountHonoursAffinity("good.mk", good); len(got) != 0 {
		t.Errorf("the nproc form was refused: %v", got)
	}
	planted := "X ?= 1\nCPP_HOSTILE_JOBS ?= $(shell getconf _NPROCESSORS_ONLN 2>/dev/null || echo 4)\n"
	if got := checkJobCountHonoursAffinity("planted.mk", planted); len(got) != 1 || !strings.Contains(got[0], "planted.mk:2 counts CPUs with _NPROCESSORS_ONLN") {
		t.Errorf("the getconf form was not caught at its line: %q", got)
	}
}
