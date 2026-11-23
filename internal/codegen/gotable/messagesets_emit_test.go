package gotable

import (
	"regexp"
	"strings"
	"testing"
)

// The schema review of 2026-11-21, emitted-text pins for the generated Go
// leg (GO-1, GO-2); the generated code itself is exercised by
// internal/codegen/gotable/fuzzunit/messagesets_test.go and
// messagesetops_test.go against the committed unit.

const messageSetsSchema = `package probe
enum Team { Red, Blue }
fixed table Slot { v uint8 }
fixed table Root
{
    by    [Team]Slot
    n     uint16 | min = 0, max = 1000
    label string(8)
}
`

func generatedText(t *testing.T, src string) string {
	t.Helper()
	out, err := Generate(unitFrom(t, src))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	var all strings.Builder
	for _, data := range out {
		all.Write(data)
	}
	return all.String()
}

// TestGoMessageDuplicateTestsAreOneLook: the keyed read and the skip path's
// keyed and body duplicate tests are one look at the named entry
// (tableMessageSetRepeat), never a scan of the keys seen so far over a slice
// that grows on the heap (GO-1).
func TestGoMessageDuplicateTestsAreOneLook(t *testing.T) {
	t.Parallel()
	text := generatedText(t, messageSetsSchema)
	for _, banned := range []string{"slices.Contains(seenKey", "slices.Contains(keys, id)", "slices.Contains(ids, e.ID)", "seenKey = append", "keys = append(keys, id)", "ids = append(ids, e.ID)"} {
		if strings.Contains(text, banned) {
			t.Errorf("the generated message reader still scans: %q", banned)
		}
	}
	if n := strings.Count(text, "tableMessageSetRepeat(r.Vocabulary, keys, ref)"); n != 1 {
		t.Errorf("the keyed read takes the set test %d times, want 1 (the [Team]Slot field)", n)
	}
	skip := regexp.MustCompile(`(?s)func tableMessageSkipBody\(.*?\n}\n`).FindString(text)
	if !strings.Contains(skip, "tableMessageSetRepeat(v, level, ref)") {
		t.Errorf("the skipped body's id test is not the set test:\n%s", skip)
	}
}

// TestGoMessageSkipHoldsWhatTheReadHolds: the skip path refuses a union
// field's arm 0 and reconstructs a ranged integer through
// tableMessageReadScalar, which holds the sum to the announced kind.
func TestGoMessageSkipHoldsWhatTheReadHolds(t *testing.T) {
	t.Parallel()
	text := generatedText(t, messageSetsSchema)
	skip := regexp.MustCompile(`(?s)func tableMessageSkip\(.*?\n}\n`).FindString(text)
	for _, want := range []string{"return ok && !field", "tableMessageReadScalar(r, s, &raw)", "e.Element.Packing != 1 || e.Element.Bits == 0"} {
		if !strings.Contains(skip, want) {
			t.Errorf("the skip path lacks %q", want)
		}
	}
	scalar := regexp.MustCompile(`(?s)func tableMessageReadScalar\(.*?\n}\n`).FindString(text)
	if !strings.Contains(scalar, "tableMessageKindBits(s.Kind)") {
		t.Errorf("the ranged reconstruction does not hold the announced kind:\n%s", scalar)
	}
}

// TestGoJSONMeasuredLengthSaturates: a string measured without a destination
// counts its length in int32; past 2 GiB it saturates rather than wrapping
// negative into a slice bound (GO-2). The input that reaches it is 2 GiB, so
// the pin is on the emitted scanner.
func TestGoJSONMeasuredLengthSaturates(t *testing.T) {
	t.Parallel()
	text := generatedText(t, messageSetsSchema)
	scan := regexp.MustCompile(`(?s)func \(in \*tableJSONIn\) scanText\(.*?\n}\n`).FindString(text)
	if scan == "" {
		t.Fatal("no JSON scanner was emitted")
	}
	if !strings.Contains(scan, "if placed > 0x7fffffff-unitLength {") || !strings.Contains(scan, "placed = 0x7fffffff") {
		t.Errorf("the measured length can wrap:\n%s", scan)
	}
}
