// Branches of the C++ packet emitter that no test reached at base f28b1a58
// (docs/CPP-REFERENCE-REVIEW.md, part 2: the merged coverage over
// internal/codegen/cpp, cpptable, ci, goldens, fuzz and the compiler's C++
// tests left each at count 0). Each test emits the smallest unit that reaches
// the branch and pins what the branch writes.
package cpp

import (
	"strings"
	"testing"
)

// TestGenerateRefusesHeaderCycleAndWireCollision reaches Generate's two
// refusals (cpp.go:35, cpp.go:44): a file graph whose headers would include
// each other — types compose order-free across files, headers cannot — and a
// file named <Base>Wire beside <Base>, whose wire header it would overwrite.
func TestGenerateRefusesHeaderCycleAndWireCollision(t *testing.T) {
	t.Parallel()
	cycle := unitFromSources(t, map[string]string{
		"Alpha.schema": `package t
type AlphaOuter {
    inner BetaInner
}
type AlphaInner {
    x int32
}
`,
		"Beta.schema": `package t
type BetaOuter {
    inner AlphaInner
}
type BetaInner {
    y int32
}
`,
	})
	if _, err := Generate(cycle); err == nil || !strings.Contains(err.Error(), "include each other in a cycle") {
		t.Errorf("a two-file header cycle must be refused by name; got %v", err)
	} else if !strings.Contains(err.Error(), "Alpha -> Beta -> Alpha") {
		t.Errorf("the cycle refusal must print the cycle; got %v", err)
	}

	collide := unitFromSources(t, map[string]string{
		"Probe.schema": `package t
type Probe {
    x int32
}
`,
		"ProbeWire.schema": `package t
type Other {
    y int32
}
`,
	})
	if _, err := Generate(collide); err == nil || !strings.Contains(err.Error(), "schema files Probe and ProbeWire collide") {
		t.Errorf("Probe.schema beside ProbeWire.schema must be refused by name; got %v", err)
	}
}

// TestFlagAppendHelperOncePerFile reaches emitFlagAppendHelper's once-per-file
// guard (cpp.go:460): with two flags declarations in one file the bounded
// append pair is defined exactly once, inside its per-package guard.
func TestFlagAppendHelperOncePerFile(t *testing.T) {
	t.Parallel()
	u := unitFromSources(t, map[string]string{
		"Bits.schema": `package t
flags Caps { Jump, Crouch }
flags Moods { Calm, Angry, Sleepy }
`,
	})
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	h := string(files["Bits.h"])
	for _, def := range []string{
		"inline int SchemaFlagAppend_( char * buffer, int bufferSize, int position, const char * name )",
		"inline int SchemaFlagAppendHex_( char * buffer, int bufferSize, int position, uint64_t bits )",
		"#ifndef SCHEMA_T_FLAG_APPEND_DEFINED",
	} {
		if got := strings.Count(h, def); got != 1 {
			t.Errorf("Bits.h defines %q %d times, want once per file", def, got)
		}
	}
	for _, use := range []string{"FlagNamesCaps(", "FlagNamesMoods("} {
		if !strings.Contains(h, use) {
			t.Errorf("Bits.h lost %s", use)
		}
	}
}

// TestZeroWireAndStoragelessBodies reaches the (void) casts emitStructWire
// writes for a struct whose every range is degenerate (zero wire bits:
// functions.go:146, 159) and for one with wire items but no storage
// (functions.go:148, 162). Without them a -Wall -Wextra -Werror consumer
// breaks under NDEBUG, where the only value uses are compiled-away asserts.
func TestZeroWireAndStoragelessBodies(t *testing.T) {
	t.Parallel()
	u := unitFromSources(t, map[string]string{
		"Shapes.schema": `package t
type Pinned {
    x int32 = 5 | min = 5, max = 5
}
type Padding {
    reserved(3)
    align
}
`,
	})
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	w := string(files["ShapesWire.h"])
	for _, want := range []string{
		"bool WritePinned( serialize::WriteStream & stream, const Pinned & value )\n{\n    (void) stream;\n    (void) value; // zero wire bits — asserts compile away under NDEBUG\n",
		"bool ReadPinned( serialize::ReadStream & stream, Pinned & value )\n{\n    (void) stream; // zero wire bits — nothing to read, defaults prefill below\n",
		"bool WritePadding( serialize::WriteStream & stream, const Padding & value )\n{\n    (void) value; // items only — reserved/const/align carry no storage\n",
		"bool ReadPadding( serialize::ReadStream & stream, Padding & value )\n{\n    (void) value; // items only — reserved/const/align carry no storage\n",
	} {
		if !strings.Contains(w, want) {
			t.Errorf("ShapesWire.h: want\n%s\nin\n%s", want, w)
		}
	}
}

// TestUntakenBranchZeroesFloatsAndUnions reaches emitZeroField's float32,
// float64 and union arms (functions.go:267, 269, 291): an untaken branch side
// reads as ZERO values (SPEC §5), not as the declared defaults.
func TestUntakenBranchZeroesFloatsAndUnions(t *testing.T) {
	t.Parallel()
	u := unitFromSources(t, map[string]string{
		"Guarded.schema": `package t
type Ring {
    radius int32
}
union Shape {
    ring Ring
}
type Guarded {
    on bool
    if on
    {
        f float32 = 1.5
        d float64 = 2.5
        s Shape
    }
}
`,
	})
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	w := string(files["GuardedWire.h"])
	read := w[strings.Index(w, "bool ReadGuarded("):]
	for _, want := range []string{
		"value.f = 0.0f;",
		"value.d = 0.0;",
		"memset( (void*) &value.s, 0, sizeof( value.s ) );",
	} {
		if !strings.Contains(read, want) {
			t.Errorf("ReadGuarded must zero the untaken side with %q:\n%s", want, read)
		}
	}
	for _, bad := range []string{"value.f = 1.5f;", "value.d = 2.5;"} {
		if strings.Contains(read, bad) {
			t.Errorf("ReadGuarded restores a declared default on the untaken side (%q); §5 wants zero", bad)
		}
	}
}

// TestRangeShapesPastInt32 reaches the range shapes no corpus declared
// (functions.go:389, 498, 501, 654): a degenerate int64 range outside int32
// (zero bits, assert only on write, materialized on read), a full-range-raw
// uint64 whose low bound is vacuous and high bound is not (the one-sided
// assert), and a degenerate uint64 above INT64_MAX (the ull spelling on both
// sides, no wire call).
func TestRangeShapesPastInt32(t *testing.T) {
	t.Parallel()
	u := unitFromSources(t, map[string]string{
		"Ranges.schema": `package t
type Ranges {
    pinned64 int64  = 5000000000 | min = 5000000000, max = 5000000000
    capped   uint64 | min = 0, max = 10000000000000000000
    pinnedU  uint64 = 10000000000000000000 | min = 10000000000000000000, max = 10000000000000000000
}
`,
	})
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	w := string(files["RangesWire.h"])
	write := w[strings.Index(w, "bool WriteRanges("):strings.Index(w, "bool ReadRanges(")]
	read := w[strings.Index(w, "bool ReadRanges("):]
	for _, want := range []string{
		"serialize_assert( int64_t( value.pinned64 ) >= int64_t( 5000000000 ) && int64_t( value.pinned64 ) <= int64_t( 5000000000 ) );",
		"serialize_assert( value.capped <= 10000000000000000000ull );",
		"write_bits( stream, value.capped, 64 );",
		"serialize_assert( value.pinnedU >= 10000000000000000000ull && value.pinnedU <= 10000000000000000000ull );",
	} {
		if !strings.Contains(write, want) {
			t.Errorf("WriteRanges: want %q\n%s", want, write)
		}
	}
	for _, line := range strings.Split(write, "\n") {
		if strings.Contains(line, "write_bits(") && (strings.Contains(line, "pinned64") || strings.Contains(line, "pinnedU")) {
			t.Errorf("WriteRanges puts a degenerate range on the wire: %s", strings.TrimSpace(line))
		}
	}
	for _, want := range []string{
		"value.pinned64 = int64_t( 5000000000 );",
		"value.pinnedU = uint64_t( 10000000000000000000ull );",
	} {
		if !strings.Contains(read, want) {
			t.Errorf("ReadRanges must materialize the degenerate range with %q:\n%s", want, read)
		}
	}
}

// TestCStringLitEscapes reaches cStringLit's backslash arm (cpp.go:912) and
// pins its octal escapes: a default's bytes are the literal's bytes with no
// escapes in the schema, so a backslash in a default is a real byte the C++
// literal must escape.
func TestCStringLitEscapes(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		`a\b`:          `"a\\b"`,
		`say "x`:       `"say \"x"`,
		"tab\t7":       `"tab\0117"`,
		"\xe2\x82\xac": `"\342\202\254"`,
	} {
		if got := cStringLit([]byte(in)); got != want {
			t.Errorf("cStringLit(%q) = %s, want %s", in, got, want)
		}
	}
	u := unitFromSources(t, map[string]string{
		"Text.schema": `package t
type Text {
    path string(8) = "a\b"
}
`,
	})
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	if want := `char path[8 + 1] = "a\\b";`; !strings.Contains(string(files["Text.h"]), want) {
		t.Errorf("Text.h: want %s\n%s", want, files["Text.h"])
	}
}
