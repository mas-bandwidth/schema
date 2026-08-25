// Tests for the Rust backend's symbolic expression rendering. The cases that
// matter are the HOSTILE ones — every spelling that would compile to the
// wrong Rust program must fold, because a fold is always correct and a bad
// symbolic rendering is a build break in the consumer's tree: rustc DENIES
// const arithmetic overflow (`arithmetic_overflow` is deny by default), so a
// symbolic form whose INTERMEDIATE overflows i64 never compiles even when
// the final value fits (issue #99).
package rust

import (
	"math/big"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/internal/check"
	"github.com/mas-bandwidth/schema/internal/parser"
	"github.com/mas-bandwidth/schema/ir"
)

func unitFromSource(t *testing.T, name, src string) *ir.Unit {
	t.Helper()
	f, perrs := parser.Parse(name, []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: name, Name: name, Base: strings.TrimSuffix(name, ".schema"),
		Bytes: []byte(src), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	return u
}

// ---- issue #99: the doubled unary minus AT the extreme ----
//
// min = --FloorLimit folds to INT64_MIN, but its symbolic rendering
// -(-FLOOR_LIMIT) carries an intermediate of +2^63 — one past i64::MAX.
// Schema folding is arbitrary-precision; Rust is not, and unlike C (whose
// carrierEval already folds this shape) rustc has no carrier that survives
// the overflow: `attempt to negate i64::MIN, which would overflow` is a
// deny-by-default error. The fold spelling is the bare decimal
// -9223372036854775808 (suffixed _i64 in cast contexts) — exactly what
// generated Rust already emits for a PLAIN INT64_MIN bound, which rustc
// accepts as a directly negated literal.
const extremesCorpus = `package extremetest

const FloorLimit = -9223372036854775808
const RotationUnits = 3000

type ExtremeProbe {
    floor_bound   int64 | min = -9223372036854775808, max = 100
    doubled_floor int64 | min = --FloorLimit, max = 100
    spin_rate     int32 | min = --RotationUnits, max = RotationUnits * 2
    floor_default int64 = -9223372036854775808
}
`

func generateExtremesCorpus(t *testing.T) string {
	t.Helper()
	u := unitFromSource(t, "Extreme.schema", extremesCorpus)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	return string(files["extreme.rs"])
}

// TestDoubledMinusAtExtremeFolds pins both sites that render the
// doubled-minus bound: the write-side fold offset and the read-side
// serialize_int64 arguments. At the extreme they must fold to the proven
// plain-INT64_MIN spellings; in range the doubled minus keeps its symbolic,
// parenthesized form (issue #22's pin — a fold here would be a silent
// retreat from symbolic rendering).
func TestDoubledMinusAtExtremeFolds(t *testing.T) {
	rs := generateExtremesCorpus(t)
	for _, want := range []string{
		// the write-side fold offset: same spelling as the plain floor_bound
		"let mut offset_value = (value.doubled_floor as u64).wrapping_sub((-9223372036854775808_i64) as u64);",
		// the read-side runtime call: same spelling as the plain floor_bound
		"stream.serialize_int64(&mut value.doubled_floor, -9223372036854775808, 100)?;",
		// the in-range doubled minus stays symbolic and parenthesized, both sides
		"let mut offset_value = (value.spin_rate as u32).wrapping_sub(((-(-ROTATION_UNITS)) as i32) as u32);",
		"stream.serialize_int(&mut value.spin_rate, (-(-ROTATION_UNITS)) as i32, (ROTATION_UNITS * 2) as i32)?;",
	} {
		if !strings.Contains(rs, want) {
			t.Errorf("generated Rust must contain %q:\n%s", want, rs)
		}
	}
	// the overflowing spelling must appear NOWHERE: -(-FLOOR_LIMIT) is an
	// arithmetic_overflow build break, --FLOOR_LIMIT a parse error
	for _, broken := range []string{"-(-FLOOR_LIMIT)", "--FLOOR_LIMIT"} {
		if strings.Contains(rs, broken) {
			t.Errorf("generated Rust contains the overflowing spelling %q (rustc denies it):\n%s", broken, rs)
		}
	}
}

// TestRenderArgOverflowGate pins the rendering decision directly: an
// expression whose intermediate escapes i64 folds, an in-range expression
// over the same constants stays symbolic, and the plain INT64_MIN literal
// keeps its (already-compiling) folded spelling.
func TestRenderArgOverflowGate(t *testing.T) {
	u := unitFromSource(t, "Extreme.schema", extremesCorpus)
	g := &gen{unit: u}
	probe := u.Structs["ExtremeProbe"]
	if probe == nil {
		t.Fatal("corpus type ExtremeProbe missing")
	}
	fields := map[string]*ir.Field{}
	for _, f := range probe.Fields {
		fields[f.Name] = f
	}

	doubled := fields["doubled_floor"]
	if got := g.renderArg(doubled.IntMinExpr, doubled.IntMin, "i64"); got != "-9223372036854775808" {
		t.Errorf("renderArg(--FloorLimit, i64) = %q, want the fold %q", got, "-9223372036854775808")
	}
	if got := g.foldArg64(doubled.IntMinExpr, doubled.IntMin); got != "-9223372036854775808_i64" {
		t.Errorf("foldArg64(--FloorLimit) = %q, want the suffixed fold %q", got, "-9223372036854775808_i64")
	}

	spin := fields["spin_rate"]
	if got := g.renderArg(spin.IntMinExpr, spin.IntMin, "i64"); got != "-(-ROTATION_UNITS)" {
		t.Errorf("renderArg(--RotationUnits, i64) = %q — an in-range doubled minus must stay symbolic", got)
	}

	plain := fields["floor_bound"]
	if got := g.renderArg(plain.IntMinExpr, plain.IntMin, "i64"); got != "-9223372036854775808" {
		t.Errorf("renderArg(plain INT64_MIN, i64) = %q, want %q", got, "-9223372036854775808")
	}

	// an unexceptional fold must not move
	if got := g.foldArg64(nil, big.NewInt(-30000)); got != "-30000_i64" {
		t.Errorf("foldArg64(nil, -30000) = %q, want %q", got, "-30000_i64")
	}
}
