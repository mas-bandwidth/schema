package cstable

import (
	"strings"
	"testing"
)

// TestWireTimestampSkipAndBoolRule pins two tolerant-read rules in the
// emitted C# runtime, each a red the wire fuzzer found against the Go oracle
// (ci-full run 39774840059):
//
//   - kind 36, the timestamp, is the i64's eight bytes on the wire
//     (docs/SPEC-TABLES.md §2.11, §3), so Width answers 8 and Skip steps over
//     an unnamed or kind-mismatched timestamp instead of calling it damage;
//   - a bool byte that is neither 0 nor 1 reads true and is MALFORMED
//     (docs/SPEC-TABLES.md §6.5), on the managed read and the region read.
func TestWireTimestampSkipAndBoolRule(t *testing.T) {
	t.Parallel()
	src := generateCS(t, `package boolprobe

table Probe
{
    on bool
}
`)
	if want := "case 5: case 9: case 11: case 23: case 28: case 36: return 8;"; !strings.Contains(src, want) {
		t.Errorf("the emitted Width does not give kind 36 its eight bytes: want %q", want)
	}
	want := "if (kind == 1) { if (raw > 1) { report.Malformed = true; } raw = raw == 0 ? 0ul : 1ul; }"
	if !strings.Contains(src, want) {
		t.Errorf("the emitted bool read does not flag a byte above 1 as malformed: want %q", want)
	}
	if strings.Contains(src, "if (kind == 1) { raw = raw == 0 ? 0ul : 1ul; }") {
		t.Errorf("a bool read still folds a byte above 1 to true with no malformed")
	}
}
