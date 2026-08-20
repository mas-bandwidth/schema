// Tests for the C backend's symbolic expression rendering (issue #92): the
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
    object_id int32  [min = 0, max = MaxObjects - 1]
    doubled   int32  [min = --MaxUnits, max = MaxUnits * 2]
    hexed     int32  [min = 0, max = 0x10]
    overflow  int64  [min = 0, max = 2000000000 * 3]
    wide_ok   int64  [min = -Wide, max = Wide]
    team_size uint8  [min = 0, max = Team.Max]
    counted   [<= MaxObjects]uint8
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
// parenthesized, because "--X" is a decrement in C (issue #22's class).
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
			t.Errorf("generated C contains the decrement spelling --MAX_UNITS (issue #22's class):\n%s", header)
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
}
