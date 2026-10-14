package cstable

// fixedroadmap_t12 — the cs leg's plan-selection, closure, retirement and
// retired-runtime rows of docs/roadmap.sexp's node `fixed-tables`: the leaves
// cs/R7, cs/R14, cs/R15, cs/R32 and cs/R29 that were :unknown at BASE, plus
// cs/E3/other-required-widens' write half. Law: docs/FIXED-FORM-ALGORITHM.md
// §5.2 (the floor and retirement), §5.3 (the identity lane), §4.6 (the bounds
// pass), §5.4 (the counters). A clause already held by an existing test is
// named by that test; this file carries the direct assertion of each clause
// that had none, one subtest per leaf id, red first, then its fix, then its
// flip.

import (
	"strings"
	"testing"
)

func TestFixedRoadmapCSharpT12PlansVersions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"cs/R32", t12R32},
		{"cs/R7", t12R7},
		{"cs/R14", t12R14},
		{"cs/R15", t12R15},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// t12Norm collapses whitespace so an assertion reads an emitted statement and
// not the indentation the string builder chose.
func t12Norm(s string) string { return strings.Join(strings.Fields(s), " ") }

// t12Has is a required substring, named by the rule it holds.
func t12Has(t *testing.T, got, want, what string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Errorf("%s: the emitted source does not carry %q", what, want)
	}
}

// t12Retire builds THREE generations of one table — x, x+y, x+y+z — marks the
// first `retired` of the first two retired in the lineage, and answers the
// emitted source together with the floor it computes (§5.2).
func t12Retire(t *testing.T, retired int) string {
	t.Helper()
	older := unitFrom(t, "package probe\n\nfixed table Lineage\n{\n    x int32\n}\n")
	middle := unitFrom(t, "package probe\n\nfixed table Lineage\n{\n    x int32\n    y int32\n}\n")
	own := unitFrom(t, "package probe\n\nfixed table Lineage\n{\n    x int32\n    y int32\n    z int32\n}\n")
	e1, ok := FixedLineageOf(older, "Lineage")
	if !ok {
		t.Fatal("no lineage entry for the first generation")
	}
	e2, ok := FixedLineageOf(middle, "Lineage")
	if !ok {
		t.Fatal("no lineage entry for the second generation")
	}
	if retired > 0 {
		e1.Retired = true
		e1.Reason = "the operator retired this layout"
	}
	if retired > 1 {
		e2.Retired = true
		e2.Reason = "the operator retired this layout"
	}
	files, err := GenerateLineage(own, map[string][]FixedLineageEntry{"Lineage": {e1, e2}})
	if err != nil {
		t.Fatalf("GenerateLineage: %v", err)
	}
	var b strings.Builder
	for _, data := range files {
		b.Write(data)
	}
	return b.String()
}

// t12R32 holds cs/R32 to §5.2's retirement, read off the emitted load: the
// operator's RETIRED mark and reason are laid down, the floor check sits ONCE,
// after the hash select and BEFORE the byte comparison or the record loop, and
// its whole body is the pure named refusal layout_unsupported — a second load
// of the same file answers the same name (idempotent), and retiring the same
// entry twice yields the same floor.
func t12R32(t *testing.T) {
	text := t12Retire(t, 1)
	t12Has(t, text, "// RETIRED: the operator retired this layout", "R32 the retired entry's mark and reason are laid down")
	load := t12Load(t, text, "Lineage")
	// ONCE, after the select, before the byte comparison and the record loop.
	if n := strings.Count(load, "pick < LineageFixedFloor"); n != 1 {
		t.Errorf("R32: the floor is tested %d times, want once", n)
	}
	at := strings.Index(load, "pick < LineageFixedFloor")
	known := strings.Index(load, "SequenceEqual(known.Layout)")
	records := strings.Index(load, "for (int k = 0; k < n; ++k)")
	if at < 0 || known < 0 || records < 0 {
		t.Fatalf("R32: the load's shape moved (%d, %d, %d)", at, known, records)
	}
	if !(at < known && known < records) {
		t.Errorf("R32: the floor check is not before the byte comparison and the record loop (%d, %d, %d)", at, known, records)
	}
	// THE PURE NAMED REFUSAL: the guarded body reads `pick` and returns
	// layout_unsupported; nothing before it mutates the report or destination.
	body := load[at:]
	if end := strings.Index(body, "return -1;"); end >= 0 {
		body = body[:end]
	}
	if strings.Contains(body, "Unknown") || strings.Contains(body, "values[") {
		t.Errorf("R32: the floor refusal is not a pure named refusal:\n%s", body)
	}
	t12Has(t, body, `Reason = "layout_unsupported"`, "R32 below the floor is layout_unsupported")
	// IDEMPOTENT: retiring the same entry twice yields the same floor, and a
	// second generation retires to a higher floor.
	again := t12Retire(t, 1)
	t12Has(t, again, "public const int LineageFixedFloor = 1;", "R32 retiring the same entry twice gives the same floor (idempotent)")
	both := t12Retire(t, 2)
	t12Has(t, both, "public const int LineageFixedFloor = 2;", "R32 the floor is 1 + the highest retired index")
}

// t12Load returns one emitted load function's body, from its `name(` to the
// function's closing brace.
func t12Load(t *testing.T, text, name string) string {
	t.Helper()
	sig := "public static long " + name + "FixedLoad("
	at := strings.Index(text, sig)
	if at < 0 {
		t.Fatalf("%sFixedLoad is not emitted", name)
	}
	rest := text[at:]
	// cut at the next top-level method: the [] overload follows.
	if end := strings.Index(rest, "public static long "+name+"FixedLoad("+name+"[]"); end > 0 {
		rest = rest[:end]
	}
	return rest
}

// t12R7 holds cs/R7 to §5.3: the identity lane is an INDEX COMPARISON — the
// resolved lineage index against the reader's own index (the last entry) —
// never a recomputed or re-compared hash, and TableFixedWire.HashOf is gone.
func t12R7(t *testing.T) {
	text := t12Retire(t, 0)
	n := t12Norm(text)
	// The reader's own entry is the LAST lineage entry (§5.2 appends it last),
	// so the lane is chosen by comparing the resolved index with len-1.
	t12Has(t, n, "if (pick != 2)", "R7 the identity lane compares the resolved lineage index with the reader's own index")
	if strings.Contains(n, "if (hash != LineageFixedHash)") {
		t.Error("R7: the lane is still chosen by comparing the header hash with the compiler-handed constant")
	}
	if strings.Contains(text, "HashOf(") {
		t.Error("R7: the emitted runtime still defines TableFixedWire.HashOf, a layout hasher nothing calls")
	}
}

// t12R14 holds cs/R14 to §5.2's PLAN: the runtime names layout_record_too_large
// for an entry whose source extent reaches past the writer's OWN declared
// record size, refusing the plan WHOLE and never landing a short read. The
// check is the reference's TableFixedSrcEnd (docs/FIXED-FORM-ALGORITHM.md §5.2,
// docs/FIXED-FORM-BILL-READS-BACKWARD.md §12.5) carried into the compiled plan:
// the record bound is the writer's root size, the over-reach is recorded on the
// compiler, the compile answers -3, and the lineage lane carries the name.
func t12R14(t *testing.T) {
	runtime := t12Norm(tableFixedWireSource)
	t12Has(t, runtime, "public static uint SrcEnd(in TableFixedEntry e)", "R14 the entry bound is carried")
	t12Has(t, runtime, "public bool TooLarge;", "R14 the over-reach is recorded")
	t12Has(t, runtime, "public uint Record;", "R14 the writer's declared record is carried")
	t12Has(t, runtime, "if (Record != 0 && SrcEnd(stamped) > Record) { TooLarge = true; return; }", "R14 an entry reaching past the writer's declared record")
	t12Has(t, runtime, "if (c.TooLarge) { return -3; }", "R14 the plan is refused WHOLE")
	t12Has(t, runtime, `lane.Why = "layout_record_too_large";`, "R14 the lane carries the name")
	t12Has(t, runtime, "Record = EntryAt(theirs, 0).Size", "R14 the record bound is the writer's root size")
	// The generated build carries the runtime, so the ban is on the emitted
	// build and not on this package's constant alone.
	text := t12Retire(t, 0)
	t12Has(t, text, "SrcEnd", "R14 the emitted build carries the entry bound")
}

// t12R15 holds cs/R15 to §5.6's retirement list and §5.3 step 5: the four
// forward-read clamps are retired, each layout_newer now. A hash the lineage
// does not hold is refused BEFORE any record and before any plan selection, on
// the file's hash; the count and text clamps that REMAIN are the writer's
// bounds carried by the plan (a backward read over a locked entry), and the
// unknown-field count lands from the COMPILE census after a returning read,
// never per record.
func t12R15(t *testing.T) {
	text := t12Retire(t, 0)
	load := t12Load(t, text, "Lineage")
	miss := strings.Index(load, `Reason = "layout_newer"`)
	records := strings.Index(load, "for (int k = 0; k < n; ++k)")
	if miss < 0 || records < 0 {
		t.Fatalf("R15: the load's refusals moved (%d, %d)", miss, records)
	}
	if miss > records {
		t.Error("R15: an unknown hash is not refused before the first record")
	}
	t12Has(t, load, "census_unknown = lane.Unknown;", "R15 unknown is the compile census")
	runtime := t12Norm(text)
	t12Has(t, runtime, "else if ((uint)v > p.Size)", "R15 the count clamp is the writer's bound")
	t12Has(t, runtime, "else if ((uint)v > cap)", "R15 the text clamp is the writer's span")
}
