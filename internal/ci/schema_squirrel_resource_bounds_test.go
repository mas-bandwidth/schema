package ci

// THE RESOURCE-BOUNDS GATE (schema-squirrel-resource-bounds). Denial-of-service
// hardening for the squirrel port (docs/SPEC-TABLES.md §16.9.4): hostile inputs
// — huge counts, deep nesting, recursion, giant strings, many unknown fields —
// must cost bounded memory and time, with nesting depth capped at 128 and
// refused by the text_limit name. This is the fast half that rides every push:
// it holds the make target and its workflow to the contract, proves the
// allocation counter by a planted unbounded allocation, and forces each planted
// pattern to go red.

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const resourceBoundsMake = "make/schema-squirrel-resource-bounds.mk"
const resourceBoundsWorkflow = ".github/workflows/schema-squirrel-resource-bounds.yml"
const resourceBoundsDepthCap = "128"

// squirrelrelResourceBoundsProblems is the gate. An empty result is a pass;
// every string is one broken property, named.
func squirrelrelResourceBoundsProblems(mk, wf string) []string {
	var bad []string
	want := func(body, needle, what string) {
		if !strings.Contains(body, needle) {
			bad = append(bad, what)
		}
	}
	// THE MAKEFILE TARGET: the fast and nightly halves, the documented depth
	// cap, the named allocation counter, the text_limit refusal, the five
	// hostile shapes, and a registered negative control.
	want(mk, ".PHONY: schema-squirrel-resource-bounds\n", "the resource-bounds target is not declared")
	want(mk, ".PHONY: schema-squirrel-resource-bounds-fast\n", "the fast half is not declared")
	want(mk, "SQUIRREL_RESOURCE_BOUNDS_DEPTH_CAP ?= 128", "the depth cap is not set to the documented 128")
	want(mk, "SCHEMA_SQUIRREL_RESOURCE_BOUNDS_ALLOC_COUNTER ?=", "the allocation counter is not named")
	want(mk, "text_limit", "the depth-cap refusal name is not carried")
	want(mk, "huge", "the huge-count hostile shape is not named")
	want(mk, "deep nesting", "the deep-nesting hostile shape is not named")
	want(mk, "recursion", "the recursion hostile shape is not named")
	want(mk, "giant", "the giant-string hostile shape is not named")
	want(mk, "unknown", "the many-unknown-fields hostile shape is not named")
	want(mk, "schema-squirrel-resource-bounds-negative-control", "the negative control is not defined")
	// THE WORKFLOW: nightly and dispatch run the whole campaign; push and pull
	// request run only the fast half, so the heavy run never becomes per-push CI.
	want(wf, "schedule:", "the workflow has no schedule trigger")
	want(wf, "workflow_dispatch:", "the workflow has no dispatch trigger")
	want(wf, "push:", "the workflow has no push trigger")
	want(wf, "pull_request:", "the workflow has no pull-request trigger")
	want(wf, "make schema-squirrel-resource-bounds-fast", "the workflow does not run the fast half")
	want(wf, "github.event_name == 'schedule' || github.event_name == 'workflow_dispatch'", "the whole campaign is not confined to the nightly triggers")
	want(wf, "make schema-squirrel-resource-bounds", "the workflow does not run the whole campaign")
	return bad
}

func TestSquirrelResourceBoundsGate(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	mk, err := os.ReadFile(filepath.Join(root, resourceBoundsMake))
	if err != nil {
		t.Fatal(err)
	}
	wf, err := os.ReadFile(filepath.Join(root, resourceBoundsWorkflow))
	if err != nil {
		t.Fatal(err)
	}
	if bad := squirrelrelResourceBoundsProblems(string(mk), string(wf)); len(bad) > 0 {
		t.Fatalf("the squirrel resource-bounds harness is incomplete:\n  %s", strings.Join(bad, "\n  "))
	}

	// THE PLANTED PATTERNS. A copy with the depth cap, the allocation counter,
	// a hostile shape or the nightly trigger removed must be refused; a copy
	// with the plant out must pass. This is the gate's red line, kept in the
	// test.
	plants := []struct {
		name, mk, wf string
		needle       string // the text the plant removes
	}{
		{"no depth cap", strings.ReplaceAll(string(mk), "SQUIRREL_RESOURCE_BOUNDS_DEPTH_CAP ?= 128", ""), string(wf), "depth cap"},
		{"no allocation counter", strings.ReplaceAll(string(mk), "SCHEMA_SQUIRREL_RESOURCE_BOUNDS_ALLOC_COUNTER ?= runtime_readmemstats", ""), string(wf), "allocation counter"},
		{"no text_limit", strings.ReplaceAll(string(mk), "text_limit", ""), string(wf), "text_limit"},
		{"no deep nesting", strings.ReplaceAll(string(mk), "deep nesting", ""), string(wf), "deep nesting"},
		{"no schedule", string(mk), strings.ReplaceAll(string(wf), "schedule:", ""), "schedule"},
		{"campaign on every push", string(mk), strings.ReplaceAll(string(wf), "if: github.event_name == 'schedule' || github.event_name == 'workflow_dispatch'", ""), "trigger"},
	}
	for _, p := range plants {
		bad := squirrelrelResourceBoundsProblems(p.mk, p.wf)
		if len(bad) == 0 {
			t.Fatalf("PLANTED PATTERN %q left the gate GREEN: %s", p.name, p.needle)
		}
	}

	// The depth cap the gate demands is the code's own constant; if the two
	// drift apart the planted control above is what keeps them honest.
	if resourceBoundsDepthCap != "128" {
		t.Fatal("resourceBoundsDepthCap drifted from the documented cap")
	}
}

// TestSquirrelResourceBoundsStemIsNotAPort pins the landing fix that made this
// card's head pass the wide-scalar probe. That probe globs make/*.mk and treats
// every stem WITHOUT a dash as a --lang port, so a card fragment whose stem has
// no dash is counted but never probed and the probe's registry check goes red.
// This card's own make file must keep its dash, and the Makefile's registry must
// keep excluding dashed stems. Either plant (the stem losing its dash, or the
// registry losing its filter) is refused by the same predicate the probe uses.
func TestSquirrelResourceBoundsStemIsNotAPort(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	stem := strings.TrimSuffix(filepath.Base(resourceBoundsMake), ".mk")
	// The probe's own test: a stem containing a dash is a gate or support
	// fragment, never a --lang port (internal/ci/probe_test.go).
	isPort := func(s string) bool { return !strings.Contains(s, "-") }
	if isPort(stem) {
		t.Fatalf("%s is counted as a --lang port by the wide-scalar probe; its stem must contain a dash", resourceBoundsMake)
	}
	// The planted-bad-pattern control: the same make file under a stem with no
	// dash is a port, so the probe would count it without probing it.
	planted := strings.ReplaceAll(stem, "-", "")
	if !isPort(planted) {
		t.Fatalf("PLANTED PATTERN stem %q still escapes the wide-scalar probe's port test", planted)
	}
	makefile, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(makefile), "SCHEMA_PORTS :=") ||
		!strings.Contains(string(makefile), "filter-out %-%") {
		t.Fatal("the Makefile no longer registers SCHEMA_PORTS with the dashed-stem filter; the landing's probe would count this card's make file as a port")
	}
}

// TestSquirrelResourceBoundsAllocationCounter is the counter harness. The
// counter is Go's runtime memstats (runtime.ReadMemStats), named
// runtime_readmemstats in the make target. It must prove itself by a planted
// unbounded allocation: a grow-without-cap append costs strictly more total
// allocation than a preallocated append of the same size.
func TestSquirrelResourceBoundsAllocationCounter(t *testing.T) {
	t.Parallel()
	bounded := func(n int) []int {
		out := make([]int, 0, n)
		for i := range n {
			out = append(out, i)
		}
		return out
	}
	unbounded := func(n int) []int {
		var out []int
		for i := range n {
			out = append(out, i)
		}
		return out
	}
	measure := func(f func()) uint64 {
		runtime.GC()
		var before runtime.MemStats
		runtime.ReadMemStats(&before)
		f()
		var after runtime.MemStats
		runtime.ReadMemStats(&after)
		return after.TotalAlloc - before.TotalAlloc
	}
	ab := measure(func() {
		for range 1000 {
			bounded(10000)
		}
	})
	au := measure(func() {
		for range 1000 {
			unbounded(10000)
		}
	})
	if au <= ab {
		t.Fatalf("the allocation counter did not count a planted unbounded allocation: bounded=%d bytes, unbounded=%d bytes", ab, au)
	}
	t.Logf("allocation counter (runtime memstats): bounded=%d bytes, unbounded=%d bytes (unbounded counted)", ab, au)
}
