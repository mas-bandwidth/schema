package cstable

// cs/R29's STRICT read (docs/FIXED-FORM-ALGORITHM.md §4.6, §5.3 step 11;
// docs/FIXED-FORM-BILL-READS-BACKWARD.md §12.5): a record read under an older
// lineage entry is held to the bounds THAT writer declared, and a value past
// them refuses the whole read — it is never clamped to the writer's end, and
// the reader's wider own range never accepts it on the compiled lane. The
// identity lane still reads the reader's own file whole.
//
// These are unit tests over the REAL emitted artifact: GenerateLineage is run
// over a genuine widening pair (the writer's 0..100, the reader's 0..200) and
// the writer-bound clamp and load it produced are inspected. They take no
// interpreter, socket or network. The companion negative control plants the
// pre-fix fault (the writer's bound turned back into a clamp, its refusal
// removed, and the refusal's reset dropped) in that same emitted artifact and
// requires this check to go red. The behavioural forge-and-read half runs in
// TestFixedRoadmapCSharpT12PlansVersions/cs/R29 under SCHEMA_REQUIRE_CORPUS.

import (
	"slices"
	"strings"
	"testing"
)

const (
	csFixedR29Writer = "package wband\n\nfixed table Band\n{\n    n int32 = 0 | min = 0, max = 100\n}\n"
	csFixedR29Reader = "package rband\n\nfixed table Band\n{\n    n int32 = 0 | min = 0, max = 200\n}\n"
)

// csFixedR29Artifact builds the real artifact: the reader's generated C# files
// with the older writer's locked layout as the one lineage entry.
func csFixedR29Artifact(t *testing.T) string {
	t.Helper()
	writer := unitFrom(t, csFixedR29Writer)
	reader := unitFrom(t, csFixedR29Reader)
	entry, ok := FixedLineageOf(writer, "Band")
	if !ok {
		t.Fatal("the older writer has no lineage entry")
	}
	files, err := GenerateLineage(reader, map[string][]FixedLineageEntry{"Band": {entry}})
	if err != nil {
		t.Fatalf("GenerateLineage: %v", err)
	}
	var b strings.Builder
	for _, data := range files {
		b.Write(data)
	}
	return b.String()
}

// csFixedR29SpanLoad returns the emitted batch load's body. The [] and single
// overloads follow it and only delegate, so the span overload is the read.
func csFixedR29SpanLoad(src string) string {
	const sig = "public static long BandFixedLoad("
	for off := 0; ; {
		at := strings.Index(src[off:], sig)
		if at < 0 {
			return ""
		}
		at += off
		close := strings.Index(src[at:], ")")
		if close < 0 {
			return ""
		}
		if strings.Contains(src[at:at+close], "Span<Band> values") {
			return csFn(src[at:], sig)
		}
		off = at + len(sig)
	}
}

// csFixedR29ReplaceInWriterClamp mutates the real writer-bound clamp alone and
// splices it back into the emitted artifact.
func csFixedR29ReplaceInWriterClamp(src, old, new string) (string, bool) {
	const sig = "public static bool BandFixedClampKnown0("
	clamp := csFn(src, sig)
	if clamp == "" {
		return src, false
	}
	changed := strings.Replace(clamp, old, new, 1)
	if changed == clamp {
		return src, false
	}
	return strings.Replace(src, clamp, changed, 1), true
}

// csFixedR29StrictFaults holds the emitted writer-bound clamp and load to the
// strict read: the endpoint's own 0..100 refuses out_of_range without writing
// a value, the reader's own 0..200 stays its own pass, and a refusal resets
// every record the read wrote (0..k) and returns -1 — the whole value or a
// refusal, never a partial one beside a counter.
func csFixedR29StrictFaults(src string) []string {
	var faults []string

	clamp := csFn(src, "public static bool BandFixedClampKnown0(")
	if clamp == "" {
		return append(faults, "the writer-bound clamp BandFixedClampKnown0 is not emitted")
	}
	cn := t12Norm(clamp)
	if !strings.Contains(cn, "if (value.N < 0 || value.N > 100) { clamped++; }") {
		faults = append(faults, "the writer-bound pass does not hold the writer's own 0..100")
	}
	if strings.Contains(cn, "value.N = ") {
		faults = append(faults, "the writer-bound pass clamps the value instead of refusing it")
	}
	if !strings.Contains(cn, "report.Refused = true") || !strings.Contains(cn, `report.Reason = "out_of_range"`) {
		faults = append(faults, "a value past the writer's bound does not refuse out_of_range")
	}

	own := t12Norm(csFn(src, "public static void BandFixedClampBody("))
	if !strings.Contains(own, "if (value.N < 0 || value.N > 200) { clamped++; }") {
		faults = append(faults, "the reader's own bounds pass does not hold the reader's 0..200")
	}

	load := csFixedR29SpanLoad(src)
	if load == "" {
		return append(faults, "BandFixedLoad's span overload is not emitted")
	}
	if !strings.Contains(load, "writerClamp = BandFixedClampKnown[pick];") {
		faults = append(faults, "the load does not select the writer's bounds by the resolved lineage index")
	}
	invoke := strings.Index(load, "if (writerClamp != null && !writerClamp.Invoke(values[k], report)) { refused = true; }")
	ownPass := strings.Index(load, "if (!BandFixedClamp(values[k], report)) { refused = true; }")
	if invoke < 0 {
		faults = append(faults, "the load never holds the compiled lane to the writer's bounds")
	}
	if ownPass < 0 || (invoke >= 0 && invoke > ownPass) {
		faults = append(faults, "the writer's bounds do not run before the reader's own pass")
	}

	if invoke >= 0 {
		tail := load[invoke:]
		refuse := strings.Index(tail, "if (refused)")
		reset := strings.Index(tail, "for (int j = 0; j <= k; ++j) { TableReset(values[j]); }")
		ret := strings.Index(tail, "return -1;")
		if refuse < 0 || reset < refuse || ret < reset {
			faults = append(faults, "a writer-bound refusal does not reset values[0..k] and return -1 (the read is not whole-or-refusal)")
		}
	}
	return faults
}

// TestCSharpFixedR29Strict pins the emitted loader that carries the band case:
// the writer-bound clamp refuses a value the old writer could not have written,
// and the read that hits it is total rather than a clamp to the writer's end.
func TestCSharpFixedR29Strict(t *testing.T) {
	t.Parallel()
	src := csFixedR29Artifact(t)
	if faults := csFixedR29StrictFaults(src); len(faults) != 0 {
		t.Errorf("cs/R29's strict read does not hold:\n  %s", strings.Join(faults, "\n  "))
	}
}

// TestCSharpFixedR29StrictNegativeControl plants the pre-fix fault in the REAL
// artifact — the writer's bound clamps the value, the writer's refusal no
// longer names out_of_range, and the refusal no longer resets what the read
// wrote — and requires the check to go red on each. It is the guard that
// TestCSharpFixedR29Strict would go green over a loader that still tolerates.
func TestCSharpFixedR29StrictNegativeControl(t *testing.T) {
	t.Parallel()
	src := csFixedR29Artifact(t)
	if faults := csFixedR29StrictFaults(src); len(faults) != 0 {
		t.Fatalf("NEGATIVE CONTROL setup: the real artifact is already red before sabotage:\n  %s", strings.Join(faults, "\n  "))
	}
	const bound = "if (value.N < 0 || value.N > 100) { clamped++; }"
	clamped, ok := csFixedR29ReplaceInWriterClamp(src, bound,
		"if (value.N < 0) { value.N = 0; } else if (value.N > 100) { value.N = 100; }")
	if !ok {
		t.Fatalf("NEGATIVE CONTROL FAILED: the writer-bound check %q is not in the real artifact", bound)
	}
	if faults := csFixedR29StrictFaults(clamped); !slices.Contains(faults, "the writer-bound pass clamps the value instead of refusing it") {
		t.Fatalf("NEGATIVE CONTROL FAILED: clamping to the writer's bound left the check GREEN: %v", faults)
	}

	const refusal = `if (clamped != 0 && report != null && !report.Refused) { report.Refused = true; report.Reason = "out_of_range"; }`
	unrefused, ok := csFixedR29ReplaceInWriterClamp(src, refusal,
		`if (clamped != 0 && report != null && !report.Refused) { report.Reason = "out_of_range"; }`)
	if !ok {
		t.Fatalf("NEGATIVE CONTROL FAILED: the writer-bound refusal %q is not in the real artifact", refusal)
	}
	if faults := csFixedR29StrictFaults(unrefused); !slices.Contains(faults, "a value past the writer's bound does not refuse out_of_range") {
		t.Fatalf("NEGATIVE CONTROL FAILED: dropping the writer-bound refusal left the check GREEN: %v", faults)
	}

	load := csFixedR29SpanLoad(src)
	const reset = "for (int j = 0; j <= k; ++j) { TableReset(values[j]); }\n                    if (report != null) { report.Verdict = TableWire.Verdict.Refused; }"
	if !strings.Contains(load, reset) {
		t.Fatalf("NEGATIVE CONTROL FAILED: the refusal reset is not in the real load artifact")
	}
	noReset := strings.Replace(src, reset,
		"if (report != null) { report.Verdict = TableWire.Verdict.Refused; }", 1)
	if noReset == src {
		t.Fatalf("NEGATIVE CONTROL FAILED: could not splice the dropped reset back into the real artifact")
	}
	if faults := csFixedR29StrictFaults(noReset); !slices.Contains(faults, "a writer-bound refusal does not reset values[0..k] and return -1 (the read is not whole-or-refusal)") {
		t.Fatalf("NEGATIVE CONTROL FAILED: dropping the refusal reset left the check GREEN: %v", faults)
	}
}

// THE WRITER'S FLOAT RANGE (docs/rulings.json strict-only-read,
// docs/FIXED-FORM-ALGORITHM.md §4.6): a compressed float read under an older
// lineage entry is held to the min and max THAT writer declared, compared in
// the field's own type, the same refusal as an integer's. The pair is a
// widening one (the writer's -1..1, the reader's -2..2) beside a
// float32 whose writer max 0.3 is not a float32, so the bound must be spelled
// rounded to float32 (0.3f) and never as the integer lanes' 0..0.
const (
	csFixedCfloatWriter = "package wcband\n\nfixed table CBand\n{\n    aim float32 | min = -1, max = 1, resolution = 0.01\n    tip float32 | min = 0, max = 0.3, resolution = 0.01\n}\n"
	csFixedCfloatReader = "package rcband\n\nfixed table CBand\n{\n    aim float32 | min = -2, max = 2, resolution = 0.01\n    tip float32 | min = 0, max = 0.6, resolution = 0.01\n}\n"
)

func csFixedCfloatArtifact(t *testing.T) string {
	t.Helper()
	writer := unitFrom(t, csFixedCfloatWriter)
	reader := unitFrom(t, csFixedCfloatReader)
	entry, ok := FixedLineageOf(writer, "CBand")
	if !ok {
		t.Fatal("the older writer has no lineage entry")
	}
	files, err := GenerateLineage(reader, map[string][]FixedLineageEntry{"CBand": {entry}})
	if err != nil {
		t.Fatalf("GenerateLineage: %v", err)
	}
	var b strings.Builder
	for _, data := range files {
		b.Write(data)
	}
	return b.String()
}

const (
	csFixedCfloatAimBound = "if (value.Aim < -1.0f || value.Aim > 1.0f) { clamped++; }"
	csFixedCfloatTipBound = "if (value.Tip < 0.0f || value.Tip > 0.3f) { clamped++; }"
)

func csFixedCfloatFaults(src string) []string {
	var faults []string
	clamp := t12Norm(csFn(src, "public static bool CBandFixedClampKnown0("))
	if clamp == "" {
		return append(faults, "the writer-bound clamp CBandFixedClampKnown0 is not emitted")
	}
	for _, want := range []string{csFixedCfloatAimBound, csFixedCfloatTipBound} {
		if !strings.Contains(clamp, want) {
			faults = append(faults, "the writer-bound pass does not hold the writer's float range in the field's own type: "+want)
		}
	}
	if !strings.Contains(clamp, `report.Reason = "out_of_range"`) {
		faults = append(faults, "a float past the writer's bound does not refuse out_of_range")
	}
	return faults
}

// TestCSharpFixedCfloatWriterRange pins the emitted writer-bound clamp for
// compressed floats.
func TestCSharpFixedCfloatWriterRange(t *testing.T) {
	t.Parallel()
	if faults := csFixedCfloatFaults(csFixedCfloatArtifact(t)); len(faults) != 0 {
		t.Errorf("the writer's float range is not held:\n  %s", strings.Join(faults, "\n  "))
	}
}

// TestCSharpFixedCfloatWriterRangeNegativeControl plants the pre-fix fault
// (the float bound spelled as the integer lanes' 0..0) and a deleted float
// check in the real artifact, and requires the check to go red on each.
func TestCSharpFixedCfloatWriterRangeNegativeControl(t *testing.T) {
	t.Parallel()
	src := csFixedCfloatArtifact(t)
	if faults := csFixedCfloatFaults(src); len(faults) != 0 {
		t.Fatalf("NEGATIVE CONTROL setup: the real artifact is already red before sabotage:\n  %s", strings.Join(faults, "\n  "))
	}
	for _, plant := range []struct{ name, from, to string }{
		{"integer 0..0 bound", csFixedCfloatAimBound, "if (value.Aim < 0 || value.Aim > 0) { clamped++; }"},
		{"float check deleted", csFixedCfloatAimBound, ""},
		{"bound not rounded to float32", csFixedCfloatTipBound, "if (value.Tip < 0.0 || value.Tip > 0.3) { clamped++; }"},
	} {
		planted := strings.Replace(src, plant.from, plant.to, 1)
		if planted == src {
			t.Fatalf("NEGATIVE CONTROL FAILED: %s: %q is not in the real artifact", plant.name, plant.from)
		}
		if faults := csFixedCfloatFaults(planted); len(faults) == 0 {
			t.Fatalf("NEGATIVE CONTROL FAILED: %s left the check GREEN", plant.name)
		}
	}
}
