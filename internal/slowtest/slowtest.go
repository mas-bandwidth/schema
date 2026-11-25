// Package slowtest holds the helpers a test that uses a real thing (a
// foreign toolchain, a built corpus, a subprocess) shares.
//
// NOTHING HERE DECIDES WHETHER A TEST RUNS. Glenn, 2026-11-25: "if you don't
// want to run a test, don't run it (i mean at the root)". The root is the
// one driver's query (tools/schema-test: a name, a language, a tier, a type
// or a stored group), expanded to names before the run starts. A test that
// needs a toolchain is tiered functional, and one that runs long is tiered
// nightly: the tier is data (its registry row, or its tier line in
// test/registry/unmoved.tsv until it has one), never an if inside the test.
// Once a test runs, what it needs is there or the test FAILS naming what is
// missing (Tool, Program, Missing). There is no skip, no SCHEMA_SLOW and no
// SCHEMA_REQUIRE_* lane promise any more: every lane that runs a test has
// promised everything the test needs.
package slowtest

import (
	"context"
	"testing"
	"time"
)

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
