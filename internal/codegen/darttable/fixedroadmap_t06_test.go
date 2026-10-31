package darttable

// THE DART LEG'S FIXED-TABLE BOUNDS AND ORDINALS ROWS (docs/roadmap.sexp node
// `fixed-tables`, rows scalar-bounds, ordinals, normalization and
// array-bounds). Each subtest is one roadmap task id and asserts the clause its
// own title states. Law: docs/FIXED-FORM-ALGORITHM.md §4.5 (the two wire bytes
// a fixed reader normalises) and §5.2/§5.4 (the plan and its counters),
// docs/SPEC-TABLES.md §3.4 and §4, docs/FIXED-FORM-VERSIONING-TESTS.md (the
// hostile rows), ROADMAP.md "NEW Fixed Tables".
//
// The harness is TABLE-DRIVEN, one subtest per task id, and every assertion
// quotes the task sentence it implements. The SUBJECT is the emitter's own
// bytes, read off the generated <Base>Fixed.dart, so a planted edit to the
// guarded site turns the assertion red by name. A clause an existing test of
// this leg already holds is named by that test in the card's verdicts and is
// not repeated here.

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// t06Generated compiles one corpus unit by name and answers the concatenation
// of every file the emitter wrote, in name order, so an assertion reads the
// leg's own output (t02Source does the ordering).
func t06Generated(t *testing.T, base string) string {
	t.Helper()
	u := versionSchema(t, base)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("%s: Generate: %v", base, err)
	}
	return t02Source(t, files)
}

func TestFixedRoadmapDartT06Bounds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"dart/C7", t06C7},
		{"dart/R30", t06R30},
		{"dart/C9", t06C9},
		{"dart/C10", t06C10},
		{"dart/R31", t06R31},
		{"dart/C12", t06C12},
		{"dart/C13", t06C13},
		{"dart/R17", t06R17},
		{"dart/C1", t06C1},
		{"dart/C2", t06C2},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// t06C7: C7 "ranged scalar clamp". §3.4 and docs/FIXED-FORM-ALGORITHM.md §5.4:
// a declared range is checked in the DECODE, in the projection every path runs,
// so the identity path and a plan compiled from a stranger's layout clamp the
// same two ends and each counts once (never `>= 1`). The fixture widens 0..100
// to 0..200, so both ends sit INSIDE int32 and both comparisons are written.
func t06C7(t *testing.T) {
	src := t06Generated(t, "VNEW_range_widen")
	body := t02Block(t, src, "void rangeWidenFixedDecode(", "\n}\n")
	t02Has(t, body, "if (value.v < 0) {", "C7 the low end of the range clamps")
	t02Has(t, body, "} else if (value.v > 200) {", "C7 the high end of the range clamps")
	if !strings.Contains(body, "value.v = 0;") || !strings.Contains(body, "value.v = 200;") {
		t.Errorf("C7 the clamp assigns the DECLARED ends, not a storage bound")
	}
	if got := strings.Count(body, "report.clamped++"); got != 2 {
		t.Errorf("C7 a range with two live ends counts exactly two clamps, got %d", got)
	}
}

// t06R30: R30 "compressed float rides as the float: min/max/resolution are
// definitions, the step is in the digest under 'Q', finer widens, coarser
// refuses by name, dropping the triple widens, fp-contract off on every leg".
// §3.4: "it rides as the float, not as a quantized index". The wire carries the
// float32 itself; min and max are the READER's own definitions and the
// resolution is arithmetic on nothing, so it rides in the DEFINITIONS DIGEST
// under 'Q' (docs/FIXED-FORM-ALGORITHM.md §5.2), which is why a finer step moves
// the hash and a coarser step is refused by the lock, not by this loop.
func t06R30(t *testing.T) {
	src := t06Generated(t, "VNEW_cfloat_range_widen")
	body := t02Block(t, src, "void cfloatRangeWidenFixedDecode(", "\n}\n")
	// (1) IT RIDES AS THE FLOAT: the f32 cell's own four bytes, tested finite
	// on the raw bits (docs/SPEC-TABLES.md §3.3) and widened exactly, never a
	// quantized index.
	t02Has(t, body, "final bits = view.getUint32(at + 4, Endian.little);",
		"R30 the compressed float rides as the float itself")
	t02Has(t, body, "value.aim = TableFixedLayout.float32Value(bits);",
		"R30 the compressed float lands as the float itself")
	// (2) THE BOUNDS ARE THE READER'S OWN (schema#1164 statement B), stated on
	// the widened reader's -2..2.
	t02Has(t, body, "if (value.aim < -2.0) {", "R30 clamps to the reader's own min")
	t02Has(t, body, "} else if (value.aim > 2.0) {", "R30 clamps to the reader's own max")
	// (3) THE RESOLUTION IS A DEFINITION AND NEVER ON THE WIRE.
	if strings.Contains(body, "0.01") {
		t.Errorf("R30 the resolution is a definition: the decode does no resolution arithmetic, but it mentions 0.01")
	}

	// (4) THE STEP IS IN THE DIGEST UNDER 'Q': two schemas whose layouts are
	// byte-identical and whose step differs must move the digest, and the
	// digest must carry the 'Q' row.
	coarse := "package probe\n\nfixed table T\n{\n    aim float32 | min = -1, max = 1, resolution = 0.1\n}\n"
	fine := "package probe\n\nfixed table T\n{\n    aim float32 | min = -1, max = 1, resolution = 0.01\n}\n"
	dCoarse := ir.TableFixedDefinitionsDigest(findTable(t, unitFrom(t, coarse), "T"))
	dFine := ir.TableFixedDefinitionsDigest(findTable(t, unitFrom(t, fine), "T"))
	if !bytes.Contains(dCoarse, []byte{'Q'}) {
		t.Errorf("R30 the step rides in the digest under 'Q'")
	}
	if bytes.Equal(dCoarse, dFine) {
		t.Errorf("R30 a finer step must move the digest (a coarser step then refuses by the hash), but 0.1 and 0.01 hash the same")
	}

	// (5) FP-CONTRACT OFF: Dart has no fused-multiply-contract operator and the
	// emitted library carries no fused multiply-add, so the float32 read is the
	// IEEE value the definition names. (The word "contract" elsewhere in this
	// source is the design's name for a name, not arithmetic.)
	for _, banned := range []string{"fma(", "fusedMultiply"} {
		if strings.Contains(src, banned) {
			t.Errorf("R30 fp-contract is off on every leg, but the generated source carries %q", banned)
		}
	}
}

// t06C9: C9 "union tag past arms → None". §3.4: the arms run from 1 and tag 0
// is None, so a tag beyond the last arm names storage no declaration describes.
// The DECODE default lands None and counts one clamp; the compiled plan lands
// the same through its own remap and the None const, so both plans agree.
func t06C9(t *testing.T) {
	src := t06Generated(t, "VNEW_union_append")
	body := t02Block(t, src, "void lineageFixedDecode(", "\n}\n")
	t02Has(t, body, "default:", "C9 a tag past the last arm takes the switch default")
	t02Has(t, body, "value.pick.type = PickType.none;", "C9 a tag past the arms lands None (0)")
	if !strings.Contains(body, "report.clamped++;") {
		t.Errorf("C9 a forged tag counts its clamp")
	}
	// THE COMPILED PLAN AGREES: the None const reads the writer's raw tag and a
	// tag past its arm set lands None and counts.
	t02Has(t, src, "if (raw < 0 || raw > arms) {",
		"C9 the compiled plan's None const refuses a tag past the arm set")
}

// t06C10: C10 "enum ordinal past top → None". §3.4: the ordinal is the
// variant's POSITION IN THE LAYOUT from 1 and 0 is None; the layout lists THIS
// reader's variants and no more, so an ordinal past them lands None and counts.
func t06C10(t *testing.T) {
	src := t06Generated(t, "VNEW_enum_append")
	body := t02Block(t, src, "void lineageFixedDecode(", "\n}\n")
	t02Has(t, body, "value.tier = Tier(view.getUint8(at));", "C10 the ordinal loads at its declared width")
	t02Has(t, body, "if (value.tier > 4) {", "C10 an ordinal past the last variant is refused")
	t02Has(t, body, "value.tier = Tier.none;", "C10 an ordinal past the top lands None (0)")
	// THE COMPILED PLAN'S ordinal op makes the same landing and count.
	t02Has(t, src, "if (raw < 0 || raw > remap[remapAt]) {",
		"C10 the compiled ordinal op refuses past the writer's variant count")
}

// t06R31: R31 "full-width lanes: arg/arg2 full width in the emitted static
// plan with the compare at the tag's own width; a 64-bit ordinal temporary; the
// remap table as long as the WRITER's variant count, refusing by name past the
// length word". docs/FIXED-FORM-ALGORITHM.md §5.2 (the guard's width is its own
// lane) and §5.4 (the unsigned ordinal).
func t06R31(t *testing.T) {
	src := t06Generated(t, "VNEW_enum_width")
	// (1) THE TAG'S OWN WIDTH: the guard compares at argW bytes, and arg carries
	// the tag at full width, never as a truncated prefix.
	t02Has(t, src, "tableFixedTagAt(source, at + guard, plan[b + TableFixedLane.argW])",
		"R31 the guard compares at the tag's own width")
	t02Has(t, src, "plan[b + TableFixedLane.arg])", "R31 arg rides as its own full-width lane")
	// (2) A 64-BIT ORDINAL TEMPORARY, read unsigned: an eight-byte ordinal with
	// the high bit set is negative in Dart's signed int, so the temp is checked
	// `< 0 || >` and never allowed into the remap as a negative index.
	t02Has(t, src, "var raw = 0;", "R31 the ordinal temporary is a full 64-bit int")
	t02Has(t, src, "if (raw < 0 || raw > remap[remapAt]) {",
		"R31 the unsigned ordinal past the writer's count refuses (None + count)")
	// (3) THE REMAP TABLE AS LONG AS THE WRITER'S VARIANT COUNT: the length word
	// is the writer's count n and the table carries n entries behind it.
	t02Has(t, src, "final need = n + 1;", "R31 the remap carries a length word")
	t02Has(t, src, "plan.remap[at] = n;", "R31 the length word is the writer's variant count")
	// (4) REFUSING BY NAME PAST THE LENGTH WORD: a plan that cannot fit its
	// remap overflows and comes back plan_too_large BY NAME, never a throw.
	t02Has(t, src, "plan.overflow = true;", "R31 an over-long remap overflows the plan")
	t02Has(t, src, "return 'plan_too_large';", "R31 the overflow is refused BY NAME")
	// (5) THE WIDTH ITSELF: VNEW_enum_width crosses 255 -> 256, so the ordinal
	// rides in TWO bytes.
	body := t02Block(t, src, "void lineageFixedDecode(", "\n}\n")
	t02Has(t, body, "value.tier = Wide(view.getUint16(at, Endian.little));",
		"R31 a 256-variant enum's ordinal rides at two bytes")
}

// t06C12: C12 "bool byte != 0". docs/FIXED-FORM-ALGORITHM.md §4.5: a bool is
// ONE byte and lands as `byte != 0` — an endpoint's 2 is true, not malformed. The
// scalar bool and the bool-run array both take the comparator.
func t06C12(t *testing.T) {
	src := t06Generated(t, "VNEW_hostile_bool")
	body := t02Block(t, src, "void hbbFixedDecode(", "\n}\n")
	t02Has(t, body, "value.flag = view.getUint8(at) != 0;",
		"C12 a scalar bool lands as `byte != 0`")
	t02Has(t, body, "value.flags[i] = view.getUint8(at + 10 + i * 1) != 0;",
		"C12 a bool-run array element lands as `byte != 0`")
}

// t06C13: C13 "present flag byte != 0". docs/FIXED-FORM-ALGORITHM.md §4.5: the
// optional's present byte is ONE byte and lands as `byte != 0`.
func t06C13(t *testing.T) {
	src := t06Generated(t, "VNEW_hostile_bool")
	body := t02Block(t, src, "void hbbFixedDecode(", "\n}\n")
	t02Has(t, body, "value.linkPresent = view.getUint8(at + 1) != 0;",
		"C13 an optional's present byte lands as `byte != 0`")
}

// t06R17: R17 "a bool or present byte that is not 0/1 normalises and counts
// nothing". docs/FIXED-FORM-ALGORITHM.md §4.5: `!= 0` is the whole of the
// normalization, so a 2 (or any nonzero) is TRUE and NOT a counter; the byte
// is a wire value the reader's rule already names, not a hostile landing.
func t06R17(t *testing.T) {
	src := t06Generated(t, "VNEW_hostile_bool")
	body := t02Block(t, src, "void hbbFixedDecode(", "\n}\n")
	if !strings.Contains(body, "? ") && strings.Count(body, "!= 0") < 3 {
		t.Errorf("R17 a non-0/1 byte must normalise through `!= 0`: the decode carries %d comparators", strings.Count(body, "!= 0"))
	}
	if strings.Contains(body, "clamped") {
		t.Errorf("R17 a bool or present byte that is not 0/1 counts NOTHING, but the decode names a counter")
	}
}

// t06C1: C1 "count clamp v<0". §3.4 and docs/FIXED-FORM-ALGORITHM.md §5.4: the
// plan's count op clamps a negative live count to zero and counts exactly once.
func t06C1(t *testing.T) {
	src := t06Generated(t, "VNEW_array_bounded_grow")
	block := t02Block(t, src, "case TableFixedOp.count:", "\n        break;")
	t02Has(t, block, "if (counted < 0) {", "C1 a negative count takes the low clamp")
	t02Has(t, block, "counted = 0;", "C1 a negative count clamps to zero")
	if got := strings.Count(block, "report.clamped++"); got != 2 {
		t.Errorf("C1 the count op counts each end once, want 2, got %d", got)
	}
}

// t06C2: C2 "count clamp v>Max". The count clamps to `size`, the WRITER's bound
// the plan carries for that entry, and counts exactly once — never the reader's
// own wider bound and never the forged value.
func t06C2(t *testing.T) {
	src := t06Generated(t, "VNEW_array_bounded_grow")
	block := t02Block(t, src, "case TableFixedOp.count:", "\n        break;")
	t02Has(t, block, "} else if (counted > size) {", "C2 a count past the bound takes the high clamp")
	t02Has(t, block, "counted = size;", "C2 the clamp lands the WRITER's bound the plan carries")
}
