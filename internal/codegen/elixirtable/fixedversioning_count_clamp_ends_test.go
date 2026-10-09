package elixirtable

// §5.8 row count_clamp_ends — the two ENDS of the count clamp, the pair the
// roadmap carries under the array-bounds work set as elixir/C1 ("count clamp
// v<0") and elixir/C2 ("count clamp v>Max"), their titles quoted verbatim.
// Row 4's forge of 7 exercises neither end: 7 is past the writer's 4 but is
// neither negative nor past the reader's own 8. The NEGATIVE arm was measured
// on 2026-09-19 to be guarded by nothing: it was deleted from the emitted
// runtime of five legs — c, cs, dart, elixir and js — and not one of their
// landed fixed-table tests went red. The clamped counter is asserted == 1 and
// never >= 1 because a leg that counts the clamp in the count op AND again in
// the bounds pass lands 2, and a looser read is the thing this row exists to
// catch. The C2 landed count of 4 is the WRITER's bound, carried by the plan,
// never the reader's own 8 and never the forged 9.
//
// THIS LEG CLAMPS THE NEGATIVE COUNT TWICE, AND THAT IS WORTH KNOWING BEFORE
// SOMEONE "SIMPLIFIES" EITHER SITE. Measured on space at `610b8599`, three
// runs, each edit counted back to exactly 1 and the original to 0:
//
//	the PLAN's arm alone disabled          `fixedruntime.go`, `raw < 0 -> {0, bump(report, :clamped)}`   C1 GREEN
//	the PROJECTION's floor alone disabled  `fixedelixir.go`,  `min(max(n_<v>, 0), <bound>)`              C1 GREEN
//	BOTH disabled                                                                                        C1 RED
//	                                       "the negative count clamps to zero, not 1"
//	                                       "the negative count lands no element: [0]"
//
// The plan's `count` step floors the wire word and counts, and the projection
// then floors `n` again with `min(max(n, 0), bound)` and counts with
// `R.clamps/4`. Either one alone produces the same answer — count 0, no
// element, `clamped == 1` — so the row asserts the LEG'S PROPERTY and cannot
// name which line delivered it. It is not vacuous: with both gone it is red.
// It is REDUNDANT COVER, and a change that removes one of the two silently
// leaves the other carrying the whole of C1.

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestFixedVersioningCountClampEnds(t *testing.T) {
	corpus := fixedCorpus(t)
	elixirBin := elixirBinary(t)

	oldFile := filepath.Join(corpus, "old_array_bounded_grow.bin")
	data, err := os.ReadFile(oldFile)
	if err != nil {
		t.Fatal(err)
	}

	needle := []byte{0xAA, 0xAA, 0xAA, 0xAA, 0x04, 0x00, 0x00, 0x00}
	if n := bytes.Count(data, needle); n != 1 {
		t.Fatalf("the locator %X occurs %d times, want exactly once", needle, n)
	}
	at := bytes.Index(data, needle)

	negative := append([]byte(nil), data...)
	negative[at+4], negative[at+5], negative[at+6], negative[at+7] = 0xFF, 0xFF, 0xFF, 0xFF
	negativeFile := filepath.Join(t.TempDir(), "negative_count_array_bounded_grow.bin")
	if err := os.WriteFile(negativeFile, negative, 0o600); err != nil {
		t.Fatal(err)
	}

	past := append([]byte(nil), data...)
	past[at+4] = 0x09 // the forge: the count word 4 -> 9, past the reader's own 8
	pastFile := filepath.Join(t.TempDir(), "past_count_array_bounded_grow.bin")
	if err := os.WriteFile(pastFile, past, 0o600); err != nil {
		t.Fatal(err)
	}

	c1 := `  data = File.read!(file())
  {tag, values, report} = load(data)
  check(tag == :ok, "the negative-count OLD file refused on the NEW build: #{inspect(tag)} #{why(report)}")
  check(length(values) == 1, "the negative-count file reads one record, not #{length(values)}")
  v = hd(values)
  check(v.lead == 0xAAAAAAAA and v.trail == 0xBBBBBBBB,
    "the negative forged count moved a neighbour: #{inspect({v.lead, v.trail})}")
  check(length(v.vals) == 0,
    "the negative count clamps to zero, not #{length(v.vals)}")
  check(v.vals == [],
    "the negative count lands no element: #{inspect(v.vals)}")
  check(report.clamped == 1,
    "the negative count clamped exactly once, not #{report.clamped}: #{why(report)}")
  check(
    report.unknown == 0 and report.kind_mismatch == 0 and report.widened == 0 and
      report.duplicate == 0,
    "a count forge is a clamp and nothing else: #{why(report)}"
  )
  check(report.malformed == false, "a clean clamped read is not malformed")`

	c2 := `  data = File.read!(file())
  {tag, values, report} = load(data)
  check(tag == :ok, "the past-bound OLD file refused on the NEW build: #{inspect(tag)} #{why(report)}")
  check(length(values) == 1, "the past-bound file reads one record, not #{length(values)}")
  v = hd(values)
  check(v.lead == 0xAAAAAAAA and v.trail == 0xBBBBBBBB,
    "the past-bound forged count moved a neighbour: #{inspect({v.lead, v.trail})}")
  check(length(v.vals) == 4,
    "the past-bound count lands the writer's bound, not #{length(v.vals)}")
  check(v.vals == [1000, 1001, 1002, 1003],
    "the writer's four elements landed and only four: #{inspect(v.vals)}")
  check(report.clamped == 1,
    "the past-bound count clamped exactly once, not #{report.clamped}: #{why(report)}")
  check(
    report.unknown == 0 and report.kind_mismatch == 0 and report.widened == 0 and
      report.duplicate == 0,
    "a count forge is a clamp and nothing else: #{why(report)}"
  )
  check(report.malformed == false, "a clean clamped read is not malformed")`

	out, err := runVersionProbe(t, elixirBin, "VNEW_array_bounded_grow", []string{"VOLD_array_bounded_grow"}, 0, negativeFile, c1)
	if err != nil {
		t.Fatalf("count_clamp_ends C1 (negative): %v\n%s", err, out)
	}
	out, err = runVersionProbe(t, elixirBin, "VNEW_array_bounded_grow", []string{"VOLD_array_bounded_grow"}, 0, pastFile, c2)
	if err != nil {
		t.Fatalf("count_clamp_ends C2 (past bound): %v\n%s", err, out)
	}
}
