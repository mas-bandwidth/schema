package ci

// THE C CONFORMANCE DRIVER'S CHILDREN WRITE NO CORE (#4530). C refuses a bad
// write by schema_assert in a debug build, so the driver runs each
// write-refusal case in a forked child and reads death by SIGABRT as the
// refusal. On a host whose core_pattern pipes to a crash handler (Ubuntu's
// apport, on the fleet runners) the kernel starts that handler for every
// abort whatever the core limit is: the four write-refusal surfaces abort
// about two hundred times, and verify-leg (c, conformance) spent 75 to 165 s
// of its wall time there on amd-1 while the driver used under two seconds of
// CPU, past the two-minute cap. A child made not dumpable gets no core and no
// handler, and still dies by SIGABRT.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// checkChildNotDumpable: every forked child in src is made not dumpable
// (prctl PR_SET_DUMPABLE 0) before its first _exit or abort, so nothing it
// runs can abort first.
func checkChildNotDumpable(name, src string) error {
	n := 0
	rest := src
	for {
		i := strings.Index(rest, "fork()")
		if i < 0 {
			break
		}
		rest = rest[i+len("fork()"):]
		n++
		j := strings.Index(rest, "== 0 )")
		if j < 0 {
			return fmt.Errorf("%s: fork %d has no child branch", name, n)
		}
		open := strings.Index(rest[j:], "{")
		if open < 0 {
			return fmt.Errorf("%s: fork %d's child branch has no body", name, n)
		}
		body := rest[j+open:]
		depth, end := 0, -1
		for k, r := range body {
			if r == '{' {
				depth++
			} else if r == '}' {
				depth--
				if depth == 0 {
					end = k
					break
				}
			}
		}
		if end < 0 {
			return fmt.Errorf("%s: fork %d's child body does not close", name, n)
		}
		body = body[:end]
		p := strings.Index(body, "prctl( PR_SET_DUMPABLE, 0")
		if p < 0 {
			return fmt.Errorf("%s: fork %d's child is dumpable: each abort starts the host's crash handler", name, n)
		}
		for _, stop := range []string{"_exit(", "abort("} {
			if s := strings.Index(body, stop); s >= 0 && s < p {
				return fmt.Errorf("%s: fork %d's child can %s before it is made not dumpable", name, n, strings.TrimSuffix(stop, "("))
			}
		}
	}
	if n == 0 {
		return fmt.Errorf("%s: no fork() to check", name)
	}
	return nil
}

func TestCConformanceChildrenWriteNoCore(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(repoRoot(t), "test", "conformance", "c")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	checked := map[string]bool{}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".c") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), "fork()") {
			continue
		}
		checked[e.Name()] = true
		if err := checkChildNotDumpable(e.Name(), string(b)); err != nil {
			t.Error(err)
		}
	}
	for _, want := range []string{"wref.c", "storage_debug.c"} {
		if !checked[want] {
			t.Errorf("test/conformance/c/%s forks no child any more; this test no longer holds it", want)
		}
	}
	// the fixture pair: the valid control passes, the dumpable child is caught
	for name, wantOK := range map[string]bool{"not-dumpable.c": true, "dumpable.c": false} {
		b, err := os.ReadFile(filepath.Join("testdata", "c-no-core-children", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := checkChildNotDumpable(name, string(b)); (err == nil) != wantOK {
			t.Errorf("testdata/c-no-core-children/%s: check = %v, want ok=%v", name, err, wantOK)
		}
	}
}
