package ci

import (
	"testing"
)

// squirrelPerCommitCapSeconds is the fleet rule a runner enforces by killing
// a per-commit job (exit 137): two minutes. The recorded times below are the
// measured walls of the squirrel card rows after the split, not a live timer.
const squirrelPerCommitCapSeconds = 120

// recordedPartOverCap is the predicate the squirrel rows are held to. A
// planted 132 must trip it; a planted 104 must not.
func recordedPartOverCap(seconds float64) bool {
	return seconds > squirrelPerCommitCapSeconds
}

// squirrelCardParts are the squirrel card leg's parts, each a registry row
// of its own, with the wall each measured as one row of the retired
// leg-gates-nightly.yml (row-seconds test 93, conformance 103, negative 104;
// the three negative controls together were the 104 s part). One leg-gate
// row ran dist-squirrel, the facade tests, the card corpus and the three
// negative controls together and measured 132 s wall (run 42599413205, job
// 129101577381).
var squirrelCardParts = map[string]float64{
	"cards-squirrel-test":                                 93,
	"cards-squirrel-conformance":                          103,
	"cards-squirrel-conformance-negative-control":         104,
	"cards-squirrel-conformance-upgrade-negative-control": 104,
	"cards-squirrel-conformance-hash-negative-control":    104,
}

// TestSquirrelCardRowsFitTheCap holds the fixed-form squirrel card work to
// the per-commit cap: each part is a registry row of its own (no row runs
// them together), each part's recorded wall and each row's measured seconds
// fit the cap. The rows run every night (schema-nightly.yml); keeping each
// inside the cap keeps it ready to return per pull request
// (docs/CI-TIERS.md, held).
func TestSquirrelCardRowsFitTheCap(t *testing.T) {
	if !recordedPartOverCap(132) {
		t.Fatal("PLANTED: a recorded 132 s per-commit part must exceed the cap")
	}
	if recordedPartOverCap(104) {
		t.Fatal("PLANTED: a recorded 104 s part fits the cap")
	}
	rows := registryMakeRows(t, repoRoot(t))
	seen := map[string]string{}
	for target, recorded := range squirrelCardParts {
		if recordedPartOverCap(recorded) {
			t.Errorf("part %s records %.0f s, past the %d s per-commit cap", target, recorded, squirrelPerCommitCapSeconds)
		}
		row, ok := rows[target]
		if !ok {
			t.Errorf("no registry row runs make %s: the squirrel card part is run by nothing", target)
			continue
		}
		if prev, dup := seen[row.Key]; dup {
			t.Errorf("row %s runs both %s and %s: the parts are rows of their own", row.Key, prev, target)
		}
		seen[row.Key] = target
		if recordedPartOverCap(row.Seconds) {
			t.Errorf("row %s measured %.0f s, past the %d s per-commit cap", row.Key, row.Seconds, squirrelPerCommitCapSeconds)
		}
	}
}
