package ci

import (
	"os"
	"path/filepath"
	"regexp"
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
		"PLANTED: the id-table count sign check is removed",
		"REPLAY-FAIL|nc-input.bin|",
		"replays green on the unplanted reader",
		"stays green at the same seed and time",
		"fold(why) ~= fold(e.reason)",
		"luau-nightly-fuzz: luau-nightly-fuzz-toolchain $(LUAU_NF_HOST)",
		"$(LUAU_NF_HOST) $(LUAU_NF)/fuzz.luau $(LUAU_FUZZ_TIME)",
		"LUAU_SRC_SHA := ",
		"not coverage guided",
		"HOST.coverage(",
		"guided=coverage",
		"REPLAY-PENDING|",
		"r.rt.load.loadTolerant or r.rt.load.load",
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
	for _, want := range []string{"schedule:", "workflow_dispatch:", "luau-nightly-fuzz LUAU_FUZZ_TIME=600", "luau-nightly-fuzz-replay", "actions/upload-artifact@", "build/luau-nightly-fuzz/findings/"} {
		if !strings.Contains(w, want) {
			t.Errorf("the workflow does not contain %q", want)
		}
	}
	if strings.Count(w, "make luau-nightly-fuzz LUAU_FUZZ_TIME=600") != 1 {
		t.Error("the ten-minute target must appear exactly once, on the scheduled job")
	}
	host, err := os.ReadFile(filepath.Join(root, "tools", "luaufuzzhost", "main.cpp"))
	if err != nil {
		t.Fatal(err)
	}
	h := string(host)
	for _, want := range []string{"opts.coverageLevel = 1", "lua_getcoverage(", "lua_callbacks(L)->interrupt"} {
		if !strings.Contains(h, want) {
			t.Errorf("tools/luaufuzzhost/main.cpp does not contain %q: the loop is not coverage guided without it", want)
		}
	}
}

// TestLuauNightlyFuzzSavedInputs pins the saved corpus: every input has a
// .meta naming its kind, unit and root (what the replay needs), and a
// pending input names the fix card that owns its red, so a pending red is
// never anonymous.
func TestLuauNightlyFuzzSavedInputs(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(repoRoot(t), "testdata", "luau-nightly-fuzz")
	bins, err := filepath.Glob(filepath.Join(dir, "*.bin"))
	if err != nil {
		t.Fatal(err)
	}
	slug := regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)+$`)
	for _, b := range bins {
		meta, err := os.ReadFile(b + ".meta")
		if err != nil {
			t.Errorf("%s has no .meta: %v", filepath.Base(b), err)
			continue
		}
		fields := map[string]string{}
		for line := range strings.SplitSeq(string(meta), "\n") {
			if k, v, ok := strings.Cut(line, "="); ok {
				fields[k] = v
			}
		}
		for _, k := range []string{"kind", "unit", "root", "seed", "reason"} {
			if fields[k] == "" {
				t.Errorf("%s.meta has no %s=", filepath.Base(b), k)
			}
		}
		switch fields["kind"] {
		case "load", "strict", "txt":
		default:
			t.Errorf("%s.meta kind=%q is not load, strict or txt", filepath.Base(b), fields["kind"])
		}
		if p, ok := fields["pending"]; ok && !slug.MatchString(p) {
			t.Errorf("%s.meta pending=%q does not name a fix card", filepath.Base(b), p)
		}
	}
}
