package elixirtable

// The union_unselected_arm row (docs/FIXED-FORM-VERSIONING-TESTS.md, schema#1157,
// 2026-09-19, LAWFUL AS IS): after a read that RETURNS, an arm the landed tag did
// not select is UNSPECIFIED. THIS ROW'S WHOLE CONTENT IS THE ASSERTION IT REFUSES
// TO MAKE: it does NOT compare pick.beta or pick.gamma. The selected arm and the
// tag are the whole of what a union read promises, and a reader who "completes"
// this test by asserting pick.gamma.p == 0 has reversed a ruling and should read
// the issue first. On this leg the read builds a fresh value and the runtime
// assembles the record image from the prefill, so an unselected arm holds its
// construction form — its declared default — and that is lawful, not a defect.
// THE BEAM HAS NO CALLER DESTINATION, so there is no poison to lay and none to
// discard: #1212 deleted the poison that was once computed and thrown away here,
// and this test writes no poison. (Named *_arm_row_test.go, not *_arm_test.go:
// Go reads a trailing _arm before _test.go as a GOARCH build constraint and
// silently files the test under IgnoredGoFiles.)

import (
	"path/filepath"
	"testing"
)

func TestFixedVersioningUnionUnselectedArm(t *testing.T) {
	corpus := fixedCorpus(t)
	elixirBin := elixirBinary(t)
	file := filepath.Join(corpus, "old_union_append.bin")

	// THE SELECTED ARM AND THE TAG ARE THE WHOLE OF WHAT A UNION READ PROMISES.
	// Nothing here names beta or gamma.
	checks := `  data = File.read!(file())
  {tag, values, report} = load(data)
  check(tag == :ok, "the reader refused the writer's file: #{inspect(tag)} #{why(report)}")
  check(length(values) == 1, "the file carries one record, not #{length(values)}")
  v = hd(values)
  check(v.pick.type == 1, "pick.type is the writer's alpha arm (1), not #{inspect(v.pick.type)}")
  check(v.pick.alpha.m == 7, "pick.alpha.m is the writer's 7, not #{inspect(v.pick.alpha.m)}")
  check(v.seq == 15, "seq is the writer's 15, not #{inspect(v.seq)}")
  check(
    report.clamped == 0 and report.unknown == 0 and report.kind_mismatch == 0 and
      report.widened == 0 and report.duplicate == 0,
    "a clean backward read moved a counter: #{why(report)}"
  )
  check(report.malformed == false, "a clean backward read is not malformed: #{why(report)}")`

	// probe 1, the compiled column: reader VNEW, older VOLD.
	out, err := runVersionProbe(t, elixirBin, "VNEW_union_append", []string{"VOLD_union_append"}, 0, file, checks)
	if err != nil {
		t.Fatalf("union_unselected_arm, compiled column: %v\n%s", err, out)
	}

	// probe 2, the identity column: reader VOLD, no older. Its own hash, its own
	// plan, the same file, the same assertions — without the arm the old build
	// does not declare.
	out, err = runVersionProbe(t, elixirBin, "VOLD_union_append", nil, 0, file, checks)
	if err != nil {
		t.Fatalf("union_unselected_arm, identity column: %v\n%s", err, out)
	}
}
