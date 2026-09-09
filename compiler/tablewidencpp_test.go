// THE FOUR WIDENING HELPERS AND THE SUPERSET THAT CARRIES THEM
// (docs/SPEC-TABLES.md §2.2, §3, §4).
//
// The C++ Table header used to carry the whole widening runtime in EVERY unit.
// Four of its five functions are reached only from a kind the unit DECLARES:
//
//   - TableWidenF32, the float rung: a declared kind 11.
//   - TableReadSignedAt: a declared kind 3, 4, 5 or 18, the signed ladder's
//     tops, which are the only kinds that have a rung below them.
//   - TableReadUnsignedAt: a declared kind 7, 8, 9 or 19.
//   - TableKindWidth, the one place a payload WIDTH is a run-time fact: an ARM
//     on a ladder, whose L must be the wire kind's own width (§3), and the
//     list runtime's extent term, which asks for that width on any list.
//
// The fifth, TableKindWidens, stays in every unit: a kind comparison is what
// every reader does.
//
// THE CENSUS IS A SAFE SUPERSET AND THE TESTS BELOW ARE WRITTEN TO THAT.
// Each helper's call sites are emitted from four to eight places, each behind
// its own shape test — plainScalar at a field, widenableElement at an array, a
// keyed body or a list, plainScalar again at an arm, widenable at a map key —
// so the census asks instead the one question §4 says the rule is decided by:
// does any field, arm, element or map key in the closure DECLARE a kind with a
// rung below it? A unit can therefore keep a helper it never calls. What it can
// never do is call one it does not carry, and that is the direction that
// decides whether the header compiles.
//
// A GREEN GATE ON THE UNITS THAT KEEP THEM IS NOT THE PROOF WANTED, because a
// census that is always true and one that is always false both leave those
// units compiling. So each helper is asked for TWICE against a NEAREST
// NEIGHBOUR: one schema with the declaration at the top of its ladder, which
// must carry the helper, must CALL it, and must compile and run at -Werror
// with the sanitizers on; and the same schema with that one declaration
// respelled at the ladder's BOTTOM — int8, uint8, float32 — which must carry
// not one symbol of it. TableKindWidens is named on the absent side on purpose:
// it is every unit's, and a change that took it out with the rest is red here.
package compiler

import (
	"strings"
	"testing"
)

// THE NEAREST-NEIGHBOUR BASE: every scalar at the BOTTOM of its own ladder, so
// no kind in the unit has a rung below it and no call site can exist. `pick`
// is a union with an int8 arm, so the unit HAS arms and the arm's own width is
// still not a run-time fact — which is what separates TableKindWidth's census
// from "the unit declares a union".
const widenNoneSrc = `package probe

union Pick
{
    small int8
    tag
}

table Note
{
    tally int8
    count uint8
    ratio float32
    label string(4)
    pick  Pick
}
`

// int64 for int8: the signed ladder's top, and nothing else moved.
const widenSignedSrc = `package probe

union Pick
{
    small int8
    tag
}

table Note
{
    tally int64
    count uint8
    ratio float32
    label string(4)
    pick  Pick
}
`

// uint32 for uint8: the unsigned ladder, and nothing else moved.
const widenUnsignedSrc = `package probe

union Pick
{
    small int8
    tag
}

table Note
{
    tally int8
    count uint32
    ratio float32
    label string(4)
    pick  Pick
}
`

// float64 for float32: the float rung's top, and nothing else moved.
const widenF32Src = `package probe

union Pick
{
    small int8
    tag
}

table Note
{
    tally int8
    count uint8
    ratio float64
    label string(4)
    pick  Pick
}
`

// THE ARM moved instead of a field: int64 for int8 inside the union, so the
// arm's L is the wire kind's width and TableKindWidth has a call site. The
// signed reader comes with it, because an arm that widens decodes through it.
const widenArmSrc = `package probe

union Pick
{
    small int64
    tag
}

table Note
{
    tally int8
    count uint8
    ratio float32
    label string(4)
    pick  Pick
}
`

// A LIST OF int8: nothing in the unit widens, and TableKindWidth is still
// reached — the list runtime's extent term asks for a wire kind's width on any
// list at all (lists.go's TableListWireExtent). This is the half of the width
// census that is NOT about a declared kind, and it is asked for on its own so
// that folding anyList in is a claim under test rather than an assumption.
const widenListSrc = `package probe

union Pick
{
    small int8
    tag
}

table Note
{
    tally int8
    count uint8
    ratio float32
    label string(4)
    marks []int8
    pick  Pick
}
`

// widenHelper is one helper, its definition's spelling, and the CALL its own
// positive fixture must contain: present is not the same as reached.
type widenHelper struct {
	symbol string
	def    string
	call   string
}

var (
	widenSignedHelper = widenHelper{
		"TableReadSignedAt",
		"inline bool TableReadSignedAt( TableReader & r, uint8_t kind, int64_t & out )",
		"if ( !TableReadSignedAt( r, kind, widened_v ) )",
	}
	widenUnsignedHelper = widenHelper{
		"TableReadUnsignedAt",
		"inline bool TableReadUnsignedAt( TableReader & r, uint8_t kind, uint64_t & out )",
		"if ( !TableReadUnsignedAt( r, kind, widened_v ) )",
	}
	widenF32Helper = widenHelper{
		"TableWidenF32",
		"inline double TableWidenF32( uint32_t bits )",
		"TableWidenF32( r.get32() )",
	}
	widenWidthArm = widenHelper{
		"TableKindWidth",
		"inline int64_t TableKindWidth( uint8_t kind )",
		"if ( sub.size != TableKindWidth( arm_kind ) )",
	}
	widenWidthList = widenHelper{
		"TableKindWidth",
		"inline int64_t TableKindWidth( uint8_t kind )",
		"elem_floor = TableKindWidth( wire_kind );",
	}
)

// widenBothWays is the shape every case below takes: the fixture that declares
// the kind carries the helper, calls it, and compiles and runs; the nearest
// neighbour carries not one symbol of it, and still carries the ladder
// predicate every unit owns.
func widenBothWays(t *testing.T, src string, want widenHelper, absent []string) {
	t.Helper()
	with, files := deadCppHeader(t, src)
	if !strings.Contains(with, want.def) {
		t.Errorf("a unit that declares the kind must define %s", want.symbol)
	}
	if !strings.Contains(with, want.call) {
		t.Errorf("%s must be CALLED by the unit that carries it; no %q in the header", want.symbol, want.call)
	}
	for _, sym := range absent {
		if strings.Contains(with, sym) {
			t.Errorf("this unit declares no kind that reaches %s and must carry none of it", sym)
		}
	}
	deadCppCompile(t, files)

	without, base := deadCppHeader(t, widenNoneSrc)
	if strings.Contains(without, want.symbol) {
		t.Errorf("the nearest neighbour reaches no %s call site and must carry none of it", want.symbol)
	}
	// the ladder predicate is EVERY unit's: a kind comparison is what every
	// reader does, and it is named by the walks
	if !strings.Contains(without, "inline bool TableKindWidens") {
		t.Error("TableKindWidens is every unit's and left one")
	}
	deadCppCompile(t, base)
}

func TestCppTableWidenSignedFollowsTheCensus(t *testing.T) {
	widenBothWays(t, widenSignedSrc, widenSignedHelper,
		[]string{"TableReadUnsignedAt", "TableWidenF32", "TableKindWidth"})
}

func TestCppTableWidenUnsignedFollowsTheCensus(t *testing.T) {
	widenBothWays(t, widenUnsignedSrc, widenUnsignedHelper,
		[]string{"TableReadSignedAt", "TableWidenF32", "TableKindWidth"})
}

func TestCppTableWidenF32FollowsTheCensus(t *testing.T) {
	widenBothWays(t, widenF32Src, widenF32Helper,
		[]string{"TableReadSignedAt", "TableReadUnsignedAt", "TableKindWidth"})
}

// AN ARM ON A LADDER is TableKindWidth's declared-kind half: the arm's L must
// be the WIRE kind's width, which no constant can spell. The signed reader
// rides with it, because that is what the arm's payload decodes through — so
// it is not named absent here.
func TestCppTableWidenArmWidthFollowsTheCensus(t *testing.T) {
	widenBothWays(t, widenArmSrc, widenWidthArm,
		[]string{"TableReadUnsignedAt", "TableWidenF32"})
}

// A LIST is the other half, and it is reached with nothing in the unit
// widening at all.
func TestCppTableWidenListWidthFollowsTheCensus(t *testing.T) {
	widenBothWays(t, widenListSrc, widenWidthList,
		[]string{"TableReadSignedAt", "TableReadUnsignedAt", "TableWidenF32"})
}

// THE ALL-ABSENT UNIT, asked for once on its own: a schema whose every scalar
// sits at the bottom of its ladder carries none of the four, and still
// compiles and runs. It is the claim the five cases above each lean on.
func TestCppTableWidenRuntimeAbsentFromALadderlessUnit(t *testing.T) {
	header, files := deadCppHeader(t, widenNoneSrc)
	for _, sym := range []string{
		"TableKindWidth", "TableReadSignedAt", "TableReadUnsignedAt", "TableWidenF32",
	} {
		if strings.Contains(header, sym) {
			t.Errorf("a unit with no kind above a ladder's bottom must carry no %s", sym)
		}
	}
	if !strings.Contains(header, "inline bool TableKindWidens") {
		t.Error("TableKindWidens is every unit's and left one")
	}
	deadCppCompile(t, files)
}
