package ci

import (
	"testing"
)

// squirrelPerCommitCapSeconds is the fleet rule a runner enforces by killing
// a per-commit job (exit 137): two minutes.
const squirrelPerCommitCapSeconds = 120

// recordedPartOverCap is the predicate the squirrel rows are held to. A
// planted 132 must trip it; a planted 104 must not.
func recordedPartOverCap(seconds float64) bool {
	return seconds > squirrelPerCommitCapSeconds
}

// squirrelCardParts are the squirrel card leg's parts, each a registry row
// of its own. As rows of the retired leg-gates-nightly.yml they measured
// test 93 s, conformance 103 s and the three negative controls together
// 104 s; one leg-gate row that ran them all together measured 132 s wall
// (run 42599413205, job 129101577381), which is why they are split.
var squirrelCardParts = []string{
	"cards-squirrel-test",
	"cards-squirrel-conformance",
	"cards-squirrel-conformance-negative-control",
	"cards-squirrel-conformance-upgrade-negative-control",
	"cards-squirrel-conformance-hash-negative-control",
}

// TestSquirrelCardRowsFitTheCap holds the fixed-form squirrel card work to
// the per-commit cap: each part is a registry row of its own, and each row
// records its measured seconds (schema-test run --write-seconds), within the
// cap. A row with no measurement is red: the cap is then held by nothing.
// The rows run every night (schema-nightly.yml); keeping each inside the cap
// keeps it ready to return per pull request (docs/CI-TIERS.md, held).
func TestSquirrelCardRowsFitTheCap(t *testing.T) {
	if !recordedPartOverCap(132) {
		t.Fatal("PLANTED: a recorded 132 s per-commit part must exceed the cap")
	}
	if recordedPartOverCap(104) {
		t.Fatal("PLANTED: a recorded 104 s part fits the cap")
	}
	rows := registryMakeRows(t, repoRoot(t))
	for _, target := range squirrelCardParts {
		row, ok := rows[target]
		if !ok {
			t.Errorf("no registry row runs make %s: the squirrel card part is run by nothing", target)
			continue
		}
		if row.Seconds <= 0 {
			t.Errorf("row %s records no measured seconds: the squirrel %s part is held to the %d s cap by nothing", row.Key, target, squirrelPerCommitCapSeconds)
			continue
		}
		if recordedPartOverCap(row.Seconds) {
			t.Errorf("row %s measured %.0f s, past the %d s per-commit cap", row.Key, row.Seconds, squirrelPerCommitCapSeconds)
		}
	}
}
