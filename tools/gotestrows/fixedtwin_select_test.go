package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fixedtwinList is the real `go list -e -test` of the two C fixed-runtime
// emitters and tools/fixedtwin, run in the checkout with the module proxy
// off: nothing is fetched, and a missing module fails by name.
func fixedtwinList(t *testing.T) string {
	t.Helper()
	cmd := exec.Command("go", "list", "-e", "-test", "-f", listFormat, "./internal/codegen/ctable", "./internal/codegen/cpptable", "./tools/fixedtwin")
	cmd.Dir = filepath.Join("..", "..")
	cmd.Env = append(os.Environ(), "GOPROXY=off", "GOFLAGS=-mod=readonly")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list -e -test: %v", err)
	}
	return string(out)
}

// TestACtableFixedRuntimeChangeSelectsFixedtwin: a diff to ctable's fixed
// runtime (internal/codegen/ctable/fixedruntime.go, the emitter of the C twin
// header) selects ./tools/fixedtwin, which holds the C runtime to the C++ one
// (#4952's diff). fixedtwin imports ctable, and through the compiler cpptable,
// so its tests build on both emitters and -dependents-of finds it from the
// real import graph. THE REVERSED WITNESS: the diff's own directories alone,
// what the plan's .go rule names, do not select fixedtwin, so without the
// dependents expansion its red would run in no row.
func TestACtableFixedRuntimeChangeSelectsFixedtwin(t *testing.T) {
	t.Parallel()
	list := fixedtwinList(t)
	for _, emitter := range []string{"./internal/codegen/ctable", "./internal/codegen/cpptable"} {
		dir := t.TempDir()
		diff := filepath.Join(dir, "diff-files.txt")
		changed := strings.TrimPrefix(emitter, "./") + "/fixedruntime.go\n"
		if err := os.WriteFile(diff, []byte(changed), 0o600); err != nil {
			t.Fatal(err)
		}
		selected := func(seeds string) []string {
			out := filepath.Join(dir, "selected.txt")
			var rows bytes.Buffer
			golist := func() (string, error) { return list, nil }
			if err := run([]string{"-table", filepath.Join("..", "..", tablePath), "-dependents-of", seeds, "-changed-files", diff, "-packages-out", out, emitter}, golist, allExist, &rows); err != nil {
				t.Fatal(err)
			}
			b, err := os.ReadFile(out)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(rows.String(), "./tools/fixedtwin") && slices.Contains(strings.Fields(string(b)), "./tools/fixedtwin") {
				t.Fatalf("%s: fixedtwin is selected and in no row: %s", emitter, rows.String())
			}
			return strings.Fields(string(b))
		}
		// THE CONTROL: the changed directory alone misses fixedtwin.
		if got := selected(""); slices.Contains(got, "./tools/fixedtwin") {
			t.Fatalf("%s alone selected %v: the control does not reproduce the miss", emitter, got)
		}
		if got := selected(emitter); !slices.Contains(got, "./tools/fixedtwin") {
			t.Errorf("a change to %s selected %v, without ./tools/fixedtwin", strings.TrimSuffix(changed, "\n"), got)
		}
	}
}
