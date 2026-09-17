package gotable

import "testing"

// THE RUNTIME THE GENERATED PROBES' FLOORS ARE CLAIMED FOR (docs/PORTING.md
// I14, docs/SPEC-TABLES.md "What allocates, and what never does").
//
// The generated probe holds its allocation claim with `testing.AllocsPerRun`.
// That number is a property of the COMPILER, not only of the code: Go's escape
// analysis and inliner move between releases, so a body the pinned compiler
// proves does not escape can allocate under another, and a floor measured on
// whatever `go` a PATH lookup found says nothing about the runtime the claim is
// for. The allocator probe pins itself inside the generated source; every other
// probe in this package was left on PATH. This is the one decision that carries
// the pin to the whole harness.
//
// `SCHEMA_GO_ALLOC_ANY_GO=1` is the escape hatch: the probes run on whatever
// toolchain is on PATH and the run REPORTS without certifying. An explicit
// `SCHEMA_GO_ALLOC_TOOLCHAIN` wins over both, so a caller may name the runtime
// under test (and, off the pinned one, the generated probe refuses by name).
const pinnedGoToolchain = "go1.26.0"

// probeToolchain answers the GOTOOLCHAIN every generated probe is built and run
// under: the explicit override if one is named, "local" for the observation
// escape hatch, and the pinned toolchain otherwise.
func probeToolchain(anyGo, override string) string {
	if override != "" {
		return override
	}
	if anyGo == "1" {
		return "local"
	}
	return pinnedGoToolchain
}

// TestGeneratedProbesReportTheRuntimeTheyCertify is the harness's own gate: the
// default must be the pinned runtime, not PATH. It is red before the pin is
// carried to the harness because `probeToolchain("", "")` answers "local" —
// the same defect the issue names for Go's `testing.AllocsPerRun`.
func TestGeneratedProbesReportTheRuntimeTheyCertify(t *testing.T) {
	if got := probeToolchain("", ""); got != pinnedGoToolchain {
		t.Fatalf("the generated probes default to GOTOOLCHAIN=%q, want the pinned %q: "+
			"a floor measured wherever `go` a PATH lookup found says nothing about the "+
			"runtime the claim is for (set SCHEMA_GO_ALLOC_ANY_GO=1 to observe another, "+
			"or SCHEMA_GO_ALLOC_TOOLCHAIN=<version> to name one)", got, pinnedGoToolchain)
	}
	if got := probeToolchain("1", ""); got != "local" {
		t.Fatalf("SCHEMA_GO_ALLOC_ANY_GO=1 must observe the runtime on PATH, not %q", got)
	}
	if got := probeToolchain("", "go1.27.1"); got != "go1.27.1" {
		t.Fatalf("an explicit SCHEMA_GO_ALLOC_TOOLCHAIN must win, got %q", got)
	}
	if got := probeToolchain("1", "go1.27.1"); got != "go1.27.1" {
		t.Fatalf("an explicit toolchain must win over the escape hatch, got %q", got)
	}
}
