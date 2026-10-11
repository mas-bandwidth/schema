package ctable

// TestFixedRoadmapCT06Bounds is the c leg's card schema-c-t06-bounds, the rows
// scalar-bounds, ordinals, normalization and array-bounds of docs/roadmap.sexp's
// node `fixed-tables`. One subtest per task id, each asserting the clause of
// docs/FIXED-FORM-ALGORITHM.md and docs/SPEC-TABLES.md §3.4 the task names on
// the C emitter and the C runtime this leg ships.
//
// THE ORACLE HERE IS THE EMITTED TEXT, NOT A RUN OF IT. The runtime rows'
// probes compile the generated C against the sibling `../serialize.c` runtime
// (fixedversioning_test.go's cRunVersionProbe); with no such checkout those
// probes are named not run, never faked (the card's TOOLCHAIN rule). What CAN
// run in every tree is the emitter and the package-scoped runtime constant, so
// each clause is pinned where it is written: the `case` that implements it in
// tableFixedRuntime, or the plan the emitter lays down. A planted edit to the
// guarded site turns the matching subtest red, which is the bite.
//
// c/R30 is NOT here: its clauses (finer widens, coarser refuses by name,
// dropping the triple widens, the 'Q' digest) need the versioning corpus and
// the sibling runtime, and are named unknown in $JOB/verdicts.txt rather than
// asserted against the emitter alone.

import (
	"strings"
	"testing"
)

// t06OpCase is one `case kTableFixed<name>:` body of the runtime constant.
func t06OpCase(t *testing.T, name string) string {
	t.Helper()
	const lead = "\n        case kTableFixed"
	at := strings.Index(tableFixedRuntime, lead+name+":")
	if at < 0 {
		t.Fatalf("the runtime no longer carries the %s op", name)
	}
	body := tableFixedRuntime[at+len(lead+name)+1:]
	if next := strings.Index(body, lead); next >= 0 {
		body = body[:next]
	}
	return body
}

// t06EntryStruct is the TableFixedEntry aggregate the plan carries.
func t06EntryStruct(t *testing.T) string {
	t.Helper()
	at := strings.Index(tableFixedRuntime, "typedef struct TableFixedEntry")
	end := strings.Index(tableFixedRuntime[at:], "} TableFixedEntry;")
	if at < 0 || end < 0 {
		t.Fatal("the runtime no longer declares TableFixedEntry")
	}
	return tableFixedRuntime[at : at+end]
}

// t06Header generates one schema's C unit and returns its Table.h.
func t06Header(t *testing.T, base string) string {
	t.Helper()
	u := cUnitOf(t, cReadSchema(t, base))
	files, err := GenerateLineage(u, map[string][]FixedLineageEntry{})
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, data := range files {
		b.Write(data)
	}
	if b.Len() == 0 {
		t.Fatal("the unit generated no C")
	}
	return b.String()
}

// t06Want is "the shipped text carries every needle", and nothing more.
func t06Want(t *testing.T, text, what string, needles ...string) {
	t.Helper()
	for _, n := range needles {
		if !strings.Contains(text, n) {
			t.Fatalf("%s does not carry %q", what, n)
		}
	}
}

// t06WantNot is the negative half: a clause that says a thing is NOT done.
func t06WantNot(t *testing.T, text, what string, needles ...string) {
	t.Helper()
	for _, n := range needles {
		if strings.Contains(text, n) {
			t.Fatalf("%s carries %q, which the clause forbids", what, n)
		}
	}
}

func TestFixedRoadmapCT06Bounds(t *testing.T) {
	t.Parallel()
	rows := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"c/C7", t06RangedScalarClamp},
		{"c/C9", t06UnionTagPastArms},
		{"c/C10", t06EnumOrdinalPastTop},
		{"c/R31", t06FullWidthLanes},
		{"c/C12", t06BoolByte},
		{"c/C13", t06PresentFlagByte},
		{"c/R17", t06NormalizeCountsNothing},
		{"c/C1", t06CountClampBelow},
		{"c/C2", t06CountClampAbove},
	}
	for _, row := range rows {
		t.Run(row.id, func(t *testing.T) {
			t.Parallel()
			row.run(t)
		})
	}
}

// c/C7 "ranged scalar clamp" — §3.4's bounds pass clamps a ranged scalar to the
// READER's declared min/max, and counts the clamp. `v int32 | min = 0,
// max = 200` is a ranged scalar, so its generated clamp body carries both ends
// and the branchless count (fixedform.go's emitFixedClampElement, fixedClampBoth).
func t06RangedScalarClamp(t *testing.T) {
	head := t06Header(t, "VNEW_range_widen")
	t06Want(t, head, "the ranged-scalar clamp body",
		"fixed_clamp_body", "value->v", "(*clamped) += (int) (")
}

// c/C9 "union tag past arms → None" — a union's tag is a variant ORDINAL, and
// the kTableFixedOrdinal op lands 0 (None) for a tag past the writer's own set
// rather than reinterpreting it (fixedruntime.go, the variant-ordinal case).
func t06UnionTagPastArms(t *testing.T) {
	op := t06OpCase(t, "Ordinal")
	t06Want(t, op, "the kTableFixedOrdinal op",
		"raw <= (uint64_t) table[0]", "table[raw]",
		"else if ( raw != 0 ) { (*clamped)++; }")
}

// c/C10 "enum ordinal past top → None" — the same ordinal op is the enum's: a
// raw ordinal past the writer's top value (table[0]) lands 0, None, and counts
// clamped exactly once.
func t06EnumOrdinalPastTop(t *testing.T) {
	op := t06OpCase(t, "Ordinal")
	t06Want(t, op, "the kTableFixedOrdinal op",
		"uint64_t v = 0", "memcpy( dst + p->dst, &v, p->dstsize )")
}

// c/R31 "full-width lanes" — arg/arg2 are FULL WIDTH in the emitted static plan
// and the tag compare is at the tag's own byte width; the ordinal temporary is
// 64-bit; and the remap table is as long as the WRITER's variant count, whose
// top value is its own length word (table[0]).
func t06FullWidthLanes(t *testing.T) {
	e := t06EntryStruct(t)
	t06Want(t, e, "the plan entry",
		"uint64_t arg;", "uint64_t arg2;", "uint8_t argw;", "uint8_t argw2;")
	if !strings.Contains(tableFixedRuntime, "p->argw") {
		t.Fatal("the tag compare does not read the tag's own width (p->argw)")
	}
	t06Want(t, t06OpCase(t, "Ordinal"), "the ordinal op",
		"uint64_t raw", "const uint16_t * table", "table[0]")
}

// c/C12 "bool byte != 0" — a bool lands as `byte != 0`, never verbatim; a
// forged 0x02 becomes 1 (fixedruntime.go, the kTableFixedBool op).
func t06BoolByte(t *testing.T) {
	op := t06OpCase(t, "Bool")
	t06Want(t, op, "the kTableFixedBool op", "d[i] = (uint8_t) ( s[i] != 0 );")
}

// c/C13 "present flag byte != 0" — an optional's present byte takes the same
// unguarded normalisation as a bool: the plan compiler stamps a present byte's
// entry with the kTableFixedBool op (fixedruntime.go's optional wrapper).
func t06PresentFlagByte(t *testing.T) {
	if !strings.Contains(tableFixedRuntime, "e.op = kTableFixedBool") {
		t.Fatal("the present byte is not compiled to the byte != 0 op")
	}
	t06Want(t, t06OpCase(t, "Bool"), "the present/bool op", "s[i] != 0")
}

// c/R17 "a bool or present byte that is not 0/1 normalises and counts nothing"
// — the kTableFixedBool case normalises but touches no counter, so a forged
// 0x02 lands 1 with `clamped` and `widened` unmoved (§4.5; the counter is the
// bounds pass's, not this op's).
func t06NormalizeCountsNothing(t *testing.T) {
	op := t06OpCase(t, "Bool")
	t06WantNot(t, op, "the kTableFixedBool op", "clamped", "widened")
}

// c/C1 "count clamp v<0" — a counted array's negative count clamps to 0 and
// counts once (fixedruntime.go, the kTableFixedCount op).
func t06CountClampBelow(t *testing.T) {
	t06Want(t, t06OpCase(t, "Count"), "the kTableFixedCount op",
		"if ( v < 0 ) { v = 0; (*clamped)++; }")
}

// c/C2 "count clamp v>Max" — a count past the WRITER's bound (the entry's own
// size, set from the plan) clamps to that bound, never the reader's, and counts
// once (fixedruntime.go, §5.3 step 11).
func t06CountClampAbove(t *testing.T) {
	t06Want(t, t06OpCase(t, "Count"), "the kTableFixedCount op",
		"else if ( v > (int32_t) p->size ) { v = (int32_t) p->size; (*clamped)++; }")
}
