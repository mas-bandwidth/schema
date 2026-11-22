package cstable

import (
	"strings"
	"testing"
)

// DAMAGE DISCARDS THE WHOLE LOAD (docs/SPEC-TABLES.md §4), and a file refused
// or damaged AT ITS FRAME is such a load: the retaining region load empties
// its store on every exit of the frame verdict, the refused form byte and the
// failed integrity check among them, not only on a damaged body. The run-time
// proof is test/cs-tables' damage-discard check over the framing corpus
// (frame_form_three_at_variable_root: a fixed-form header at a variable root).
func TestCSharpRetainingLoadEmptiesTheStoreOnAFrameRefusal(t *testing.T) {
	t.Parallel()
	files, err := Generate(csUnitOf(t, "package probe\ntable Root { n int32 }\n"))
	if err != nil {
		t.Fatal(err)
	}
	var all strings.Builder
	for _, data := range files {
		all.Write(data)
	}
	src := all.String()
	start := strings.Index(src, "static unsafe IntPtr RetainLoadRegion(")
	if start < 0 {
		t.Fatal("the retaining region load is not emitted")
	}
	end := strings.Index(src[start:], "long attributionBytes")
	if end < 0 {
		t.Fatal("the retaining region load has no frame verdict block")
	}
	frame := src[start : start+end]
	const empties = " retain->Used = 0; retain->IdUsed = 0; retain->Count = 0;"
	for _, exit := range []string{
		"IntegrityDamage(report);",
		"Finish(report, Verdict.Refused);",
		"Finish(report, Verdict.Damaged);",
	} {
		if n := strings.Count(frame, exit); n != 1 {
			t.Errorf("the frame verdict has %q %d times, want 1", exit, n)
		} else if !strings.Contains(frame, exit+empties) {
			t.Errorf("the frame verdict's exit %q leaves the retention store as it was", exit)
		}
	}
}
