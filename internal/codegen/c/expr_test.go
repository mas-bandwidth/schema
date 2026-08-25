// Tests for the C backend's symbolic expression rendering: the
// sixth client of ir.RenderExprIdent. The cases that matter are the HOSTILE
// ones — every spelling that would compile to the wrong C program must fold,
// because a fold is always correct and a bad symbolic rendering is a wire
// bug in the consumer's tree.
package c

import (
	"math/big"
	"strings"
	"testing"
)

// exprCorpus drives every rendering decision through real generation: safe
// spellings must come out symbolic, unsafe ones must come out folded.
const exprCorpus = `package exprtest

const MaxObjects = 256
const MaxUnits = 1024
const Wide = 3000000000
const Product = MaxObjects * MaxUnits
const IntOverflow = 2000000000 * 3
const WidePlus = Wide + 1
const HexId = 0xDEADBEEF

enum Team { Red, Blue }
const NumTeams = Team.Max

type Probe {
    object_id int32  | min = 0, max = MaxObjects - 1
    doubled   int32  | min = --MaxUnits, max = MaxUnits * 2
    hexed     int32  | min = 0, max = 0x10
    overflow  int64  | min = 0, max = 2000000000 * 3
    wide_ok   int64  | min = -Wide, max = Wide
    team_size uint8  | min = 0, max = Team.Max
    counted   [..MaxObjects]uint8
    name      string(MaxUnits)
}
`

func generateExprCorpus(t *testing.T) (data, wire string) {
	t.Helper()
	u := unitFromSource(t, "Expr.schema", exprCorpus)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	return string(files["Expr.h"]), string(files["ExprWire.h"])
}

// TestExprConstDefines pins the #define forms: symbolic where the whole
// expansion is provably safe C arithmetic, folded literals everywhere else.
func TestExprConstDefines(t *testing.T) {
	data, _ := generateExprCorpus(t)
	for _, want := range []string{
		// a reference chain renders as the author's expression
		"#define PRODUCT (MAX_OBJECTS * MAX_UNITS)\n",
		// int carrier would overflow on the way to the value: 2000000000 * 3
		// is an int * int in C — the fold is the only correct spelling
		"#define INT_OVERFLOW (6000000000)\n",
		// a wide leaf (3000000000 self-promotes past int) makes the subtree
		// long long, so the symbolic form is safe
		"#define WIDE_PLUS (WIDE + 1)\n",
		// hex source spelling survives
		"#define HEX_ID (0xDEADBEEF)\n",
		// E.Max has no C twin: fold, schema spelling in the comment
		"#define NUM_TEAMS (2) /* = Team.Max */\n",
	} {
		if !strings.Contains(data, want) {
			t.Errorf("generated data header must contain %q:\n%s", want, data)
		}
	}
}

// TestExprBounds pins the wire-function bound spellings, the hostile
// parenthesization case included: a doubled unary minus must render
// parenthesized, because "--X" is a decrement in C (the doubled-minus class).
func TestExprBounds(t *testing.T) {
	data, wire := generateExprCorpus(t)
	for _, want := range []string{
		// the issue's own example: a declared bound as the author's expression
		"(serialize_int64_t) value->object_id > MAX_OBJECTS - 1",
		// doubled minus parenthesizes — "--MAX_UNITS" would be a decrement
		"(serialize_int64_t) value->doubled < -(-MAX_UNITS)",
		"(serialize_int64_t) value->doubled > MAX_UNITS * 2",
		// hex bound keeps its source spelling
		"(serialize_int64_t) value->hexed > 0x10",
		// int-carrier overflow folds to the literal, suffixed as always
		"(serialize_int64_t) value->overflow > 6000000000LL",
		// a wide leaf rides symbolically, no suffix needed
		"(serialize_int64_t) value->wide_ok < -WIDE",
		// an enum-max bound folds
		"(serialize_int64_t) value->team_size > 2LL",
		// counted array bound and string size
		"serialize_read_int( stream, &value->counted_count, 0, MAX_OBJECTS )",
		"serialize_read_int( stream, &value->name_length, 0, MAX_UNITS )",
	} {
		if !strings.Contains(wire, want) {
			t.Errorf("generated wire header must contain %q:\n%s", want, wire)
		}
	}
	for _, want := range []string{
		"uint8_t counted[MAX_OBJECTS];",
		"char name[MAX_UNITS + 1];",
	} {
		if !strings.Contains(data, want) {
			t.Errorf("generated data header must contain %q:\n%s", want, data)
		}
	}
	// the decrement spelling must appear NOWHERE
	for _, header := range []string{data, wire} {
		if strings.Contains(header, "--MAX_UNITS") {
			t.Errorf("generated C contains the decrement spelling --MAX_UNITS (the doubled-minus class):\n%s", header)
		}
	}
}

// TestExprHostileDirect pins the rendering helpers on inputs generation
// cannot produce through a checked unit: a nil expression folds, and a
// reference to a constant the unit does not hold folds rather than emitting
// an undefined name.
func TestExprHostileDirect(t *testing.T) {
	u := unitFromSource(t, "Expr.schema", exprCorpus)
	g := &gen{unit: u, file: u.Files[0], emitted: map[string]bool{}}
	if got := g.renderInt(nil, big.NewInt(7)); got != "7" {
		t.Errorf("renderInt(nil) = %q, want the fold %q", got, "7")
	}
	if got := g.renderIntSuffixed(nil, big.NewInt(-7), "LL"); got != "-7LL" {
		t.Errorf("renderIntSuffixed(nil) = %q, want the fold %q", got, "-7LL")
	}
	// a same-file constant that has not been #defined yet must fold: the
	// preprocessor expands at use, so an early use of a late name is an
	// undefined-identifier build break in the consumer's tree
	c, ok := u.Consts["Product"]
	if !ok {
		t.Fatal("corpus const Product missing")
	}
	if got := g.renderInt(c.Expr, c.Int); got != "262144" {
		t.Errorf("renderInt before the referenced #defines = %q, want the fold %q", got, "262144")
	}
	g.emitted["MaxObjects"] = true
	g.emitted["MaxUnits"] = true
	if got := g.renderInt(c.Expr, c.Int); got != "MAX_OBJECTS * MAX_UNITS" {
		t.Errorf("renderInt after the referenced #defines = %q, want %q", got, "MAX_OBJECTS * MAX_UNITS")
	}

	// the extreme folds: INT64_MIN has no literal spelling in C
	// — the literal half overflows long long before the unary minus applies
	// — and a value past INT64_MAX has no signed rung to land on, so the
	// suffix the site asked for gives way to ULL
	min64 := new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), 63))
	if got := intLit(min64, "LL"); got != "( -9223372036854775807LL - 1 )" {
		t.Errorf("intLit(INT64_MIN, LL) = %q, want the guarded spelling", got)
	}
	if got := intLit(min64, ""); got != "( -9223372036854775807LL - 1 )" {
		t.Errorf("intLit(INT64_MIN, unsuffixed) = %q, want the guarded spelling", got)
	}
	huge := new(big.Int).SetUint64(18446744073709551615)
	if got := intLit(huge, ""); got != "18446744073709551615ULL" {
		t.Errorf("intLit(UINT64_MAX, unsuffixed) = %q, want the ULL spelling", got)
	}
	if got := intLit(huge, "ULL"); got != "18446744073709551615ULL" {
		t.Errorf("intLit(UINT64_MAX, ULL) = %q, want a single suffix", got)
	}
	if got := intLit(big.NewInt(-30000), "LL"); got != "-30000LL" {
		t.Errorf("intLit(-30000, LL) = %q — an unexceptional fold must not move", got)
	}
}

// ---- the integer extremes ----
//
// Two folded values have no direct decimal spelling in C. INT64_MIN's literal
// half (9223372036854775808) overflows long long before the unary minus
// applies, and a value past INT64_MAX exceeds every signed rung of the
// decimal ladder, so only the ULL spelling can hold it. The C++ backend
// guards both (( -9223372036854775807ll - 1 ), the ull suffix); the C fold
// paths must agree in shape.
const extremesCorpus = `package extremetest

const FloorLimit = -9223372036854775808
const CeilingCount uint64 = 18446744073709551615

type ExtremeProbe {
    floor_bound int64 | min = -9223372036854775808, max = 100
    ceiling_range uint64 | min = 1, max = 18446744073709551615
    floor_default int64 = -9223372036854775808
    ceiling_default uint64 = 18446744073709551615
    doubled_floor int64 | min = --FloorLimit, max = 100
    deep_floor fixed(128, 0) | min = -9223372036854775808, max = 0
}

type ExtremeRow {
    clamped_floor int64 | min = -9223372036854775808, max = 100
    clamped_ceiling uint64 | min = 1, max = 18446744073709551614
    floor_def int64 = -9223372036854775808
    ceiling_def uint64 = 18446744073709551615
}
`

func generateExtremesCorpus(t *testing.T) (data, wire string) {
	t.Helper()
	u := unitFromSource(t, "Extreme.schema", extremesCorpus)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	return string(files["Extreme.h"]), string(files["ExtremeWire.h"])
}

// TestExtremeLiterals pins every C spelling of the two extremes: the guarded
// INT64_MIN form and the ULL form above INT64_MAX, at every site that folds
// one — #define bodies, defaults, wire offsets, fixed bounds, table default
// comparisons and clamps.
func TestExtremeLiterals(t *testing.T) {
	data, wire := generateExtremesCorpus(t)
	for _, want := range []string{
		// #define bodies: the C twin of C++'s emitConst guards
		"#define FLOOR_LIMIT (( -9223372036854775807LL - 1 ))",
		"#define CEILING_COUNT (18446744073709551615ULL)",
		// defaults fold through the same guard
		"value.floor_default = ( -9223372036854775807LL - 1 );",
		"value.ceiling_default = 18446744073709551615ULL;",
		"value.floor_def = ( -9223372036854775807LL - 1 );",
		"value.ceiling_def = 18446744073709551615ULL;",
	} {
		if !strings.Contains(data, want) {
			t.Errorf("generated data header must contain %q", want)
		}
	}
	for _, want := range []string{
		// the wire offset arithmetic folds the INT64_MIN bound guarded
		"(value->floor_bound) - (( -9223372036854775807LL - 1 ))",
		"offset_value + (( -9223372036854775807LL - 1 ))",
		// the doubled minus AT the extreme folds too: the intermediate
		// -(-9223372036854775808) overflows the long long carrier even
		// though the final value fits (two extreme classes meet)
		"(value->doubled_floor) - (( -9223372036854775807LL - 1 ))",
		// a 128-bit fixed bound that fits int64 rides from_int64, guarded
		"serialize_write_fixed128( stream, fixed_value, 128, 0, ( -9223372036854775807LL - 1 ), 0 )",
	} {
		if !strings.Contains(wire, want) {
			t.Errorf("generated wire header must contain %q", want)
		}
	}
	for _, header := range []string{wire, data} {
		if strings.Contains(header, "--FLOOR_LIMIT") || strings.Contains(header, "-(-FLOOR_LIMIT)") {
			t.Errorf("the doubled-minus-at-the-extreme bound must fold, not render symbolically")
		}
	}
}

// TestExtremeSpellingsAbsent proves the broken spellings appear NOWHERE in
// any generated file: the raw INT64_MIN decimal (whose literal half is out
// of long long range no matter the suffix), and any above-INT64_MAX decimal
// not immediately suffixed ULL.
func TestExtremeSpellingsAbsent(t *testing.T) {
	data, wire := generateExtremesCorpus(t)
	for name, text := range map[string]string{"data": data, "wire": wire} {
		if strings.Contains(text, "-9223372036854775808") {
			t.Errorf("%s header spells the unspellable INT64_MIN literal", name)
		}
		for _, huge := range []string{"18446744073709551615", "18446744073709551614", "9223372036854775808"} {
			for at := 0; ; {
				i := strings.Index(text[at:], huge)
				if i < 0 {
					break
				}
				at += i + len(huge)
				if at >= len(text) || text[at] != 'U' {
					t.Errorf("%s header carries the above-INT64_MAX decimal %s without the ULL suffix", name, huge)
				}
			}
		}
	}
}
