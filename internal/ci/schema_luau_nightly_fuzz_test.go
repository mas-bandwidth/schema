package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLuauNightlyFuzzContract pins the shape of the Luau port's nightly fuzz
// (make/schema-luau-nightly-fuzz.mk): the target and its time variable exist,
// the tool probe refuses by name, the negative control plants a reader bug in
// a SCRATCH copy, and the workflow runs the heavy target only on the schedule
// while a push runs the replay. A gate that loses any of these is the gate
// this file keeps.
func TestLuauNightlyFuzzContract(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	mk, err := os.ReadFile(filepath.Join(root, "make", "schema-luau-nightly-fuzz.mk"))
	if err != nil {
		t.Fatal(err)
	}
	m := string(mk)
	for _, want := range []string{
		"luau-nightly-fuzz:",
		"LUAU_FUZZ_TIME ?= 600",
		"luau-nightly-fuzz-toolchain:",
		"tool missing: luau",
		"luau-nightly-fuzz-replay:",
		"luau-nightly-fuzz-negative-control:",
		"PLANTED: the r_u8 bounds check is removed",
		"the saved input fails replay on the sabotaged reader",
	} {
		if !strings.Contains(m, want) {
			t.Errorf("make/schema-luau-nightly-fuzz.mk does not contain %q", want)
		}
	}
	wf, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "schema-luau-nightly-fuzz.yml"))
	if err != nil {
		t.Fatal(err)
	}
	w := string(wf)
	for _, want := range []string{"schedule:", "workflow_dispatch:", "luau-nightly-fuzz LUAU_FUZZ_TIME=600", "luau-nightly-fuzz-replay"} {
		if !strings.Contains(w, want) {
			t.Errorf("the workflow does not contain %q", want)
		}
	}
	if strings.Count(w, "make luau-nightly-fuzz LUAU_FUZZ_TIME=600") != 1 {
		t.Error("the ten-minute target must appear exactly once, on the scheduled job")
	}
}
