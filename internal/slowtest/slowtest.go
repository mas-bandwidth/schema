// Package slowtest is ONE GATE with ONE SENTENCE behind it, the owner's, today,
// twice:
//
//	"Remember the 1-2 minute iteration rule on unit tests."
//
// A unit test that shells out to a foreign toolchain — cc, c++, dotnet, javac,
// cargo, dart, node, elixir — or that reads the C++ reference's corpus out of
// build/fixedform-corpus does not cost milliseconds. It costs one to thirty
// SECONDS of somebody else's compiler, every run, and a package full of them
// costs minutes. `go test ./compiler/` was 185-262 s on the Studio and
// `go test ./internal/codegen/...` several minutes more; at that price a child
// stops running the tests, which is the only failure mode that matters.
//
// SO THE SPLIT IS BY WHEN, NOT BY WHETHER — the same rule ci-fast.yml already
// carries. The DEFAULT `go test` runs everything that is pure Go: the IR, the
// emitters' text, the goldens, the refusals. The toolchain half runs under
// SCHEMA_SLOW=1, and it runs there ALWAYS:
//
//   - every make leg gate already exports SCHEMA_REQUIRE_CORPUS=1, and Enabled
//     counts that as the slow half being ON — so not one of the nine leg gates,
//     and not one of ci-fast.yml's per-leg rows that drive them, changed at all,
//   - ci-full.yml's two `go test` steps set SCHEMA_SLOW=1 explicitly, so the
//     merge and nightly lanes run the whole of both halves.
//
// NOTHING IS CHECKED LESS THAN BEFORE. A gate that quietly stopped running is
// worse than a slow one, which is why Gate never reads a toolchain's presence:
// absence still fails where SCHEMA_REQUIRE_CORPUS says it must.
package slowtest

import (
	"context"
	"os"
	"testing"
	"time"
)

// Enabled reports whether the slow half runs in this process.
//
// SCHEMA_REQUIRE_CORPUS counts as well, and deliberately: that variable is the
// repo's existing promise that a named gate built the oracle and means to prove
// it. A target that set it and not SCHEMA_SLOW=1 would otherwise go green
// having run nothing — the exact silence make/cs.mk:425 and its eight siblings
// were written to forbid.
func Enabled() bool {
	return os.Getenv("SCHEMA_SLOW") == "1" || os.Getenv("SCHEMA_REQUIRE_CORPUS") != ""
}

// Gate skips the calling test unless the slow half is enabled. `what` names the
// cost in the skip line — the toolchain or the corpus — so the reader of a
// default run can see exactly what did not run and why.
func Gate(t *testing.T, what string) {
	t.Helper()
	if Enabled() {
		return
	}
	t.Skipf("SCHEMA_SLOW: this test shells out to %s; set SCHEMA_SLOW=1 to run it (ci-full.yml and the make leg gates always do)", what)
}

// ProbeContext is the context a compiled probe runs under: the TEST'S OWN
// deadline (go test -timeout, ten minutes by default) less a margin wide enough
// for the harness to print the failure by name, and no deadline at all when the
// harness has none. It replaces a fixed 30 s wall bound, which was measured
// wrong on 2026-09-11 (PR #950's gate): macOS assesses every freshly written
// executable on its first exec, about 2 s idle and unbounded when twenty-two
// gates each spawn fresh probes through the one syspolicyd — a probe whose own
// work is microseconds died at 30.48 s with 0 % CPU. A probe that truly hangs
// still fails under its test's name, before the harness's own timeout panic;
// the margin is what keeps the two apart.
func ProbeContext(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	deadline, ok := t.Deadline()
	if !ok {
		return context.WithCancel(t.Context())
	}
	return context.WithDeadline(t.Context(), deadline.Add(-probeMargin))
}

// probeMargin is the slice of the test's deadline the harness keeps for itself.
const probeMargin = 5 * time.Second
