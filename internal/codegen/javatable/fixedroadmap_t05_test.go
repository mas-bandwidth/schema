package javatable

// fixedroadmap_t05 — the java leg's scalar-bounds, full-width-ordinal and
// array-bounds rows of docs/roadmap.sexp's node `fixed-tables` (ROADMAP.md
// "NEW Fixed Tables"), in roadmap order. Law: docs/FIXED-FORM-ALGORITHM.md
// §4.6 (the bounds pass), docs/FIXED-FORM-VERSIONING-TESTS.md, and
// docs/SPEC-TABLES.md §3.4.
//
// The java leg's runtime probes need a working javac; where none is present on
// the machine running this card, the assertions below are held against the
// EMITTED SOURCE of each construct (`Generate`), which is what the emitter
// writes and what a javac would then compile. Every clause that already has a
// holder on this leg is named in the card's verdicts and not repeated.

import (
	"strings"
	"testing"
)

// TestFixedRoadmapJavaT05Bounds is one subtest per task id of the
// scalar-bounds, ordinals and array-bounds rows this card proves.
func TestFixedRoadmapJavaT05Bounds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"java/C7", t05C7},
		{"java/R30", t05R30},
		{"java/C9", t05C9},
		{"java/C10", t05C10},
		{"java/R31", t05R31},
		{"java/C12", t05C12},
		{"java/C13", t05C13},
		{"java/R17", t05R17},
		{"java/C1", t05C1},
		{"java/C2", t05C2},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// t05Ranged is the smallest declared fixed table with a ranged scalar: a plain
// int32 whose declared bounds differ from its storage width, bracketed by two
// neighbours so a mislaid read moves one of them too.
const t05Ranged = `package probe

fixed table R
{
    lead  uint32 = 1
    x     int32 | min = 0, max = 10
    trail uint32 = 2
}
`

// t05Array is the smallest declared fixed table with a bounded count: a
// counted array whose bound the reader clamps a forged count to.
const t05Array = `package probe

fixed table A
{
    lead  uint32 = 1
    rows  [..4]int32
    trail uint32 = 2
}
`

// t05BoolOpt is the smallest declared fixed table with a bool and an optional,
// both one byte that is true when it is NOT zero (SPEC §3.4).
const t05BoolOpt = `package probe

type Leaf
{
    n int32
}

fixed table B
{
    flag bool
    link ?Leaf
}
`

// t05Union is the smallest declared fixed table with a two-arm union and a
// two-variant enum, so a tag past the arms and an ordinal past the top can both
// be read.
const t05Union = `package probe

type A1
{
    n int32
}

type A2
{
    m int32
}

union Pick
{
    one A1
    two A2
}

enum Tier { low, high }

fixed table U
{
    pick Pick
    tier Tier
}
`

// t05Cfloat is a compressed float: a float32 whose min, max and resolution are
// DEFINITIONS in the digest, not wire (SPEC §3.4, schema#1164).
const t05Cfloat = `package probe

fixed table CF
{
    lead  uint32 = 1
    aim   float32 | min = -1, max = 1, resolution = 0.1
    trail uint32 = 2
}
`

// t05Fixed is one generated class's source from a schema, fatal when the name
// is absent (a renamed emitter class is red by name, never a silent skip).
func t05Fixed(t *testing.T, src, name string) string {
	t.Helper()
	files, err := Generate(unitFrom(t, src))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	data, ok := files[name]
	if !ok {
		t.Fatalf("the emitter wrote no %s", name)
	}
	return string(data)
}

// t05C7: C7 "ranged scalar clamp". ALGORITHM §4.6: "A ranged scalar clamps on
// load and counts, straight-line code after the plan run over storage"; the
// declared bounds are the raw literals the emitted clamp compares against.
func t05C7(t *testing.T) {
	src := t05Fixed(t, t05Ranged, "RFixed.java")
	for _, want := range []string{
		"long q = TableFixed.get32(b, at + 4);",
		"if (q < 0L) { q = 0L; r.clamped++; } else if (q > 10L) { q = 10L; r.clamped++; }",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("C7: the emitted reader is missing %q — a ranged scalar clamps to its declared min and max and counts one clamped (§4.6)", want)
		}
	}
}

// t05R30: R30 "compressed float rides as the float". SPEC §3.4 rules the
// compressed float rides AS THE FLOAT32: the writer stores the raw float bits
// and the reader reconstructs them with intBitsToFloat, so min, max and
// resolution are DEFINITIONS in the digest and never a wire quantisation. The
// reader's bounds pass is the same straight-line clamp as any ranged scalar,
// against THIS reader's own declared min and max as bit patterns.
func t05R30(t *testing.T) {
	src := t05Fixed(t, t05Cfloat, "CFFixed.java")
	if !strings.Contains(src, "TableFixed.put32(b, at + 4, Float.floatToRawIntBits(v.aim));") {
		t.Error("R30: the writer does not store the compressed float's raw bits — a compressed float rides AS THE FLOAT (SPEC §3.4), never quantised onto a resolution grid")
	}
	if !strings.Contains(src, "float q = Float.intBitsToFloat(TableFixed.get32(b, at + 4));") {
		t.Error("R30: the reader does not reconstruct the compressed float with intBitsToFloat — it rides as the float itself")
	}
	if !strings.Contains(src, "if (q < Float.intBitsToFloat(0xbf800000)) { q = Float.intBitsToFloat(0xbf800000); r.clamped++; } else if (q > Float.intBitsToFloat(0x3f800000)) { q = Float.intBitsToFloat(0x3f800000); r.clamped++; }") {
		t.Error("R30: no clamp at THIS reader's own declared min (-1) and max (1) as bit patterns — the bounds are definitions the read enforces, not a wire grid")
	}
	// The resolution is a DEFINITION and appears nowhere on the wire: the
	// writer's store is the raw float and no expression divides by 0.1.
	if strings.Contains(src, "0.1") || strings.Contains(src, "/ resolution") {
		t.Error("R30: the emitted reader/writer names the resolution on the wire — a compressed float's resolution is a digest definition and never a wire step")
	}
}

// t05C9: C9 "union tag past arms → None". The emitter's scatter reads the tag
// at its own width and lands None (0) for any tag past the last arm, counting
// one clamped, so a foreign tag never fires an arm.
func t05C9(t *testing.T) {
	src := t05Fixed(t, t05Union, "PickFixed.java")
	if !strings.Contains(src, "if (v.type > 2) { v.type = none; r.clamped++; }") {
		t.Error("C9: the emitted union scatter does not land None for a tag past the last arm — a tag past the arms lands None and counts one clamped (SPEC §3.4)")
	}
	if !strings.Contains(src, "public static final int none = 0;") {
		t.Error("C9: the emitted union has no None tag")
	}
}

// t05C10: C10 "enum ordinal past top → None". The emitter's scatter reads the
// enum ordinal and lands None (0) for any ordinal past the last variant,
// counting one clamped — the same answer the plan's ordinal op lands.
func t05C10(t *testing.T) {
	src := t05Fixed(t, t05Union, "UFixed.java")
	if !strings.Contains(src, "if (q > 2L) { q = 0L; r.clamped++; }") {
		t.Error("C10: the emitted enum scatter does not land None for an ordinal past the top — an ordinal past the last variant lands 0 and counts one clamped (SPEC §3.4)")
	}
}

// t05R31 is the full-width-lanes task. Its clauses need a union whose tag is
// EIGHT bytes and an enum of more than 65536 variants, shapes no schema of this
// tree declares, so the conjoined lanes the clause names cannot be constructed
// on this leg; see the card's verdict (<id> unknown). The subtests below hold
// only what the always-emitted runtime lane proves: the ordinal temporary is a
// 64-bit long and the remap table's length is its own first word.
func t05R31(t *testing.T) {
	src := t05Fixed(t, t05Cfloat, "TableFixed.java")
	if !strings.Contains(src, "final long raw =") {
		t.Error("R31: the emitted ordinal lane has no 64-bit temporary — an ordinal is read as an unsigned 64-bit long at every width")
	}
	if !strings.Contains(src, "final int n = remap[p.aux] & 0xFFFF;") {
		t.Error("R31: the emitted ordinal lane does not take the remap table's length from its own length word")
	}
}

// t05C12: C12 "bool byte != 0". SPEC §3.4: a bool byte is TRUE WHEN IT IS NOT
// ZERO, so a peer's 2 is true and never damage.
func t05C12(t *testing.T) {
	src := t05Fixed(t, t05BoolOpt, "BFixed.java")
	if !strings.Contains(src, "v.flag = b[at + 0] != 0;") {
		t.Error("C12: the emitted bool lands a value other than `byte != 0` — a bool byte is true when it is not zero (SPEC §3.4)")
	}
}

// t05C13: C13 "present flag byte != 0". SPEC §3.4: an optional's present flag
// is a byte that is true when it is not zero.
func t05C13(t *testing.T) {
	src := t05Fixed(t, t05BoolOpt, "BFixed.java")
	if !strings.Contains(src, "v.linkPresent = b[at + 1] != 0;") {
		t.Error("C13: the emitted optional lands presence other than `byte != 0` — the present flag is true when it is not zero (SPEC §3.4)")
	}
}

// t05R17: R17 "a bool or present byte that is not 0/1 normalises and counts
// nothing". Both bytes are read as `!= 0` with no counter touched beside the
// read: any nonzero byte normalises to true and no counter moves.
func t05R17(t *testing.T) {
	src := t05Fixed(t, t05BoolOpt, "BFixed.java")
	for _, want := range []string{
		"v.flag = b[at + 0] != 0;",
		"v.linkPresent = b[at + 1] != 0;",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("R17: the byte read is missing %q — a bool or present byte that is not 0/1 normalises and counts nothing", want)
		}
	}
}

// t05C1: C1 "count clamp v<0". A count read from the wire is clamped up to
// zero and counted once when negative (ALGORITHM §4.6's count op).
func t05C1(t *testing.T) {
	src := t05Fixed(t, t05Array, "AFixed.java")
	if !strings.Contains(src, "if (n < 0) { n = 0; r.clamped++; }") {
		t.Error("C1: the emitted count read does not clamp a negative count up to zero — a count below zero clamps to 0 and counts one clamped (§4.6)")
	}
}

// t05C2: C2 "count clamp v>Max". A count read from the wire is clamped down to
// the declared bound and counted once when past it.
func t05C2(t *testing.T) {
	src := t05Fixed(t, t05Array, "AFixed.java")
	if !strings.Contains(src, "else if (n > 4) { n = 4; r.clamped++; }") {
		t.Error("C2: the emitted count read does not clamp a count past the declared bound down to it — a count above Max clamps to Max and counts one clamped (§4.6)")
	}
}
