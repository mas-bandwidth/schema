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
//   - every make leg gate exports SCHEMA_REQUIRE_CORPUS=1, and Enabled counts
//     that as the slow half being ON — so not one of the nine leg gates, and
//     not one of ci-fast.yml's per-leg rows that drive them, changed at all,
//   - every POSITIVE gate target runs its `go test` through test/slowgate/proof,
//     which sets SCHEMA_SLOW=1, refuses a slowtest skip in its own log, and
//     requires a `--- PASS` for each gate it names (schema#988),
//   - ci-full.yml's two `go test` steps set SCHEMA_SLOW=1 explicitly, so the
//     merge and nightly lanes run the whole of both halves.
//
// THE SECOND BULLET WAS FALSE FOR ONE DAY AND IT COST US A DAY (schema#988, G5).
// This comment said "NOTHING IS CHECKED LESS THAN BEFORE" while fourteen
// positive gate targets — tables-go-fixedform, -containers, -block-build,
// -block-race-negative-control, -retain, -allocator, -blob-span,
// -blob-span-negative-control, -builders, -typed-refusals, -view, -release,
// tables-c-retain and tables-reference-review — ran a BARE `go test`. Under
// `make test` the gate each one names skipped, `go test` exited 0, and the
// target went green having run nothing. The claim is a claim about the
// Makefile, so `make slow-gate-scan` now CHECKS IT on every run: a recipe line
// that runs `go test` on a package holding gated tests without setting either
// variable fails by name, and anything deliberately left to ci-full.yml is
// named with its reason in make/slow-gate-exceptions.txt. A sentence in a
// comment is not a gate; the scan is.
//
// NOTHING IS CHECKED LESS THAN BEFORE. A gate that quietly stopped running is
// worse than a slow one, which is why Gate never reads a toolchain's presence:
// absence still fails where SCHEMA_REQUIRE_CORPUS says it must.
package slowtest

import (
	"os"
	"testing"
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
