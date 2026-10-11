package javatable

// fixedroadmap_t05 — the java leg's scalar-bounds, ordinal, normalization and
// array-bounds rows of docs/roadmap.sexp's node `fixed-tables` (ROADMAP.md "NEW
// Fixed Tables"), the assertions no other test of this leg makes. Law:
// docs/FIXED-FORM-ALGORITHM.md §4.6 (the bounds pass) and §5's op table,
// docs/SPEC-TABLES.md §3.4, docs/FIXED-FORM-VERSIONING-TESTS.md. One subtest per
// task id, and each subtest asserts the clause its task's own title states.
//
// The assertion is read off the EMITTED Java source this leg writes, the way
// fixedroadmap_t02 reads the plan and the digest: the compiled probes need a JDK
// this cell does not carry, so the clause is pinned where the leg itself states
// it — the straight-line scatter the emitter writes and the static plan's lanes.

import (
	"strings"
	"testing"
)

// t05Files is every emitted file of one schema, keyed by file name.
func t05Files(t *testing.T, schema string) map[string]string {
	t.Helper()
	u := versioningSchemaUnit(t, schema)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate(%s): %v", schema, err)
	}
	out := map[string]string{}
	for name, src := range files {
		out[name] = string(src)
	}
	return out
}

// t05Has is a required substring of one emitted file, named by rule and by the
// page sentence the clause implements.
func t05Has(t *testing.T, files map[string]string, file, want, what string) {
	t.Helper()
	src, ok := files[file]
	if !ok {
		t.Fatalf("%s: the emitter wrote no %s", what, file)
	}
	if !strings.Contains(src, want) {
		t.Errorf("%s: %s does not carry %q", what, file, want)
	}
}

func TestFixedRoadmapJavaT05Bounds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"java/C1", t05C1},
		{"java/C2", t05C2},
		{"java/C7", t05C7},
		{"java/C9", t05C9},
		{"java/C10", t05C10},
		{"java/C12", t05C12},
		{"java/C13", t05C13},
		{"java/R17", t05R17},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// t05C1 is C1 "count clamp v<0": ALGORITHM §5's `count` op, "if v < 0 then
// v := 0, COUNT clamped". The emitted scatter reads a count into a local and
// clamps the negative end to 0 before it walks.
func t05C1(t *testing.T) {
	files := t05Files(t, "VNEW_array_bounded_grow.schema")
	t05Has(t, files, "ArrayBoundedGrowFixed.java",
		"if (n < 0) { n = 0; r.clamped++; }",
		"C1 the negative end of the count clamp")
}

// t05C2 is C2 "count clamp v>Max": the same op's "else if v > size then v :=
// size, COUNT clamped", against the bound THE DECLARATION CARRIES — the OLD
// unit's `[..4]` and the NEW unit's `[..8]` — never a bound of the peer's.
func t05C2(t *testing.T) {
	old := t05Files(t, "VOLD_array_bounded_grow.schema")
	t05Has(t, old, "ArrayBoundedGrowFixed.java",
		"else if (n > 4) { n = 4; r.clamped++; }",
		"C2 the high end at the OLD declaration's bound 4")
	newer := t05Files(t, "VNEW_array_bounded_grow.schema")
	t05Has(t, newer, "ArrayBoundedGrowFixed.java",
		"else if (n > 8) { n = 8; r.clamped++; }",
		"C2 the high end at the NEW declaration's bound 8")
}

// t05C7 is C7 "ranged scalar clamp": ALGORITHM §4.6, "a RANGED SCALAR clamps to
// the min and max the plan carries, COUNT clamped". The emitted scatter clamps
// both ends of a `int32 | min = 0, max = 200` field to its own declaration.
func t05C7(t *testing.T) {
	files := t05Files(t, "VNEW_range_widen.schema")
	t05Has(t, files, "RangeWidenFixed.java",
		"if (q < 0L) { q = 0L; r.clamped++; } else if (q > 200L) { q = 200L; r.clamped++; }",
		"C7 the ranged scalar clamps to its declared min and max")
}

// t05C9 is C9 "union tag past arms → None": ALGORITHM §5's union scatter, "a
// union tag past the arm count ... lands None and counts". The emitted union
// scatter reads the tag and lands `none` (the union's own 0) past the last arm.
func t05C9(t *testing.T) {
	files := t05Files(t, "VNEW_union_append.schema")
	t05Has(t, files, "PickFixed.java",
		"if (v.type > 3) { v.type = none; r.clamped++; }",
		"C9 a tag past the arm count lands None")
}

// t05C10 is C10 "enum ordinal past top → None": the emitted enum scatter reads
// the ordinal through a wide temporary and lands position 0, `None`, past the
// last variant.
func t05C10(t *testing.T) {
	files := t05Files(t, "VNEW_enum_append.schema")
	t05Has(t, files, "LineageFixed.java",
		"long q = (b[at + 0] & 0xFF);",
		"C10 the enum ordinal read into a temporary")
	t05Has(t, files, "LineageFixed.java",
		"if (q > 4L) { q = 0L; r.clamped++; }",
		"C10 an ordinal past the top variant lands None")
}

// t05C12 is C12 "bool byte != 0": ALGORITHM §5, "a bool ... lands as `byte !=
// 0`, normalised to the language's own true. 0x02 is not a bool a reader stores
// verbatim". The emitted scatter writes the field from `!= 0`.
func t05C12(t *testing.T) {
	files := t05Files(t, "VNEW_hostile_bool.schema")
	t05Has(t, files, "HbbFixed.java",
		"v.flag = b[at + 0] != 0;",
		"C12 a bool byte lands as `byte != 0`")
}

// t05C13 is C13 "present flag byte != 0": the same rule for an optional's
// present flag, which rides as one byte and lands by the same `!= 0`.
func t05C13(t *testing.T) {
	files := t05Files(t, "VNEW_hostile_bool.schema")
	t05Has(t, files, "HbbFixed.java",
		"v.linkPresent = b[at + 1] != 0;",
		"C13 a present flag byte lands as `byte != 0`")
}

// t05R17 is R17 "a bool or present byte that is not 0/1 normalises and counts
// nothing": the two landings above carry no counter beside them, so a peer's
// 0x02 is true and moves no counter (fix 2).
func t05R17(t *testing.T) {
	files := t05Files(t, "VNEW_hostile_bool.schema")
	src, ok := files["HbbFixed.java"]
	if !ok {
		t.Fatal("R17: the emitter wrote no HbbFixed.java")
	}
	for _, line := range []string{
		"v.flag = b[at + 0] != 0;",
		"v.linkPresent = b[at + 1] != 0;",
	} {
		if !strings.Contains(src, line) {
			t.Errorf("R17: the normalising line %q is not emitted (a counter beside it would change the line)", line)
		}
	}
}
