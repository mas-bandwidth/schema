package c

import (
	"regexp"
	"strings"
	"testing"
)

// offsetCorpus covers every shape the ranged-offset fold takes: a signed
// 64-bit range floored at INT64_MIN (the overflow this test was written
// for), a doubled floor, an int32 whose span exceeds 2^31 (the same overflow
// in the 32-bit path), a symbolic floor, a zero floor, and an unsigned
// 64-bit range above zero.
const offsetCorpus = `package offsettest

const FLOOR = -9223372036854775808
const UNITS = 1000

type Offsets {
    floor_bound   int64  | min = FLOOR, max = 100
    doubled_floor int64  | min = FLOOR * 2 / 2, max = 0
    wide_int32    int32  | min = -2000000000, max = 2000000000
    symbolic      int32  | min = -UNITS, max = UNITS
    zero_floor    int32  | min = 0, max = UNITS - 1
    unsigned_wide uint64 = 1 | min = 1, max = 18446744073709551614
    in_array      [3]int64 | min = FLOOR, max = 7
}
`

// signedOffset is the shape that was undefined behaviour: the field minus the
// floor, both in the field's own (signed) type, cast only after the
// subtraction has already overflowed.
var signedOffset = regexp.MustCompile(`\(value->[A-Za-z0-9_\[\]]+\) - `)

// TestRangedOffsetsSubtractUnsigned pins the item-1 fix: every ranged write
// takes value - min in UNSIGNED arithmetic with BOTH operands cast first,
// the C twin of C++'s uint64_t( value ) - uint64_t( min ). The old form,
// (value->floor_bound) - (( -9223372036854775807LL - 1 )), overflowed int64
// for every value >= 0, and the int32 form overflowed int for any span past
// 2^31. The test runs over this corpus AND the committed examples, so a new
// emission path with the old shape fails here.
func TestRangedOffsetsSubtractUnsigned(t *testing.T) {
	u := unitFromSource(t, "Offsets.schema", offsetCorpus)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	wire := string(files["OffsetsWire.h"])
	for _, want := range []string{
		"serialize_uint64_t offset_value = (serialize_uint64_t) value->floor_bound - (serialize_uint64_t) FLOOR;",
		"serialize_uint64_t offset_value = (serialize_uint64_t) value->doubled_floor - (serialize_uint64_t) ( -9223372036854775807LL - 1 );",
		"serialize_write_bits( stream, (serialize_uint32_t) value->wide_int32 - (serialize_uint32_t) ( -2000000000 ), 32 )",
		"serialize_write_bits( stream, (serialize_uint32_t) value->symbolic - (serialize_uint32_t) ( -UNITS ), 11 )",
		"serialize_write_bits( stream, (serialize_uint32_t) ( value->zero_floor ), 10 )",
		"serialize_uint64_t offset_value = (serialize_uint64_t) value->unsigned_wide - (serialize_uint64_t) 1;",
		"serialize_uint64_t offset_value = (serialize_uint64_t) value->in_array[i] - (serialize_uint64_t) FLOOR;",
		// the read side adds the floor back in the same unsigned arithmetic
		"value->floor_bound = (int64_t) ( offset_value + (serialize_uint64_t) FLOOR );",
		"value->doubled_floor = (int64_t) ( offset_value + (serialize_uint64_t) ( -9223372036854775807LL - 1 ) );",
		"value->wide_int32 = (int32_t) ( offset_value + (serialize_uint64_t) ( -2000000000 ) );",
	} {
		if !strings.Contains(wire, want) {
			t.Errorf("ranged offset must subtract in unsigned arithmetic: want %q in:\n%s", want, wire)
		}
	}

	all := map[string]string{"OffsetsWire.h": wire}
	examples := loadExamplesUnit(t)
	exampleFiles, err := Generate(examples)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range exampleFiles {
		all["examples/"+name] = string(data)
	}
	for name, text := range all {
		if m := signedOffset.FindString(text); m != "" {
			t.Errorf("%s subtracts a ranged floor in the field's signed type (%q…): undefined behaviour on overflow", name, m)
		}
	}
}

// TestReadRefusalsLatchTheStream pins item 6: every read-side refusal the
// GENERATED code makes (a value out of its declared range, an enum or union
// tag past its max, a const or reserved mismatch, malformed UTF-8, an
// interior null) fails the stream through
// serialize_read_fail, so the wire header's documented "a caller may check
// once at the end of a message" holds. A bare `return 0` is legal only right
// after a runtime or nested read call, which has already latched the stream.
func TestReadRefusalsLatchTheStream(t *testing.T) {
	u := unitFromSource(t, "Refuse.schema", `package refusetest

const LIMIT = 10

enum Mode { A, B, C, D, E }

type Inner {
    x int32 | min = 0, max = LIMIT
}

union Pick {
    a Inner
    b Inner
}

type Refuse {
    ranged int32 | min = -LIMIT, max = LIMIT
    mode   Mode
    pick   Pick
    const(3, 5)
    reserved(4)
    name   string(12)
    inner  Inner
}
`)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	wire := string(files["RefuseWire.h"])
	for _, fn := range []string{"read_inner", "read_pick", "read_refuse"} {
		i := strings.Index(wire, "int "+fn+"( ")
		if i < 0 {
			t.Fatalf("no %s in:\n%s", fn, wire)
		}
		body, _, _ := strings.Cut(wire[i:], "\n}\n")
		lines := strings.Split(body, "\n")
		for n, line := range lines {
			trimmed := strings.TrimSpace(line)
			if !strings.HasPrefix(trimmed, "return 0") && !strings.Contains(trimmed, "{ return 0; }") {
				continue
			}
			// the guarding condition: the same line (one-line form) or the
			// nearest `if` above it
			cond := trimmed
			for k := n - 1; k >= 0 && !strings.Contains(cond, "if ("); k-- {
				cond = strings.TrimSpace(lines[k])
			}
			if !strings.HasPrefix(cond, "if ( !serialize_") && !strings.HasPrefix(cond, "if ( !read_") {
				t.Errorf("%s refuses without latching the stream (guard %q): use serialize_read_fail", fn, cond)
			}
		}
	}
	for _, want := range []string{
		"return serialize_read_fail( stream, SERIALIZE_REFUSAL_VALUE_OUT_OF_RANGE ); /* not a wire-legal value */",
		"return serialize_read_fail( stream, SERIALIZE_REFUSAL_VALUE_OUT_OF_RANGE ); /* not a wire-legal tag */",
		"return serialize_read_fail( stream, SERIALIZE_REFUSAL_INVALID_UTF8 ); /* malformed UTF-8 is content the read refuses (SPEC §4.7) */",
		"return serialize_read_fail( stream, SERIALIZE_REFUSAL_EMBEDDED_NUL ); /* an interior null is content the read refuses (SPEC §4.7) */",
	} {
		if !strings.Contains(wire, want) {
			t.Errorf("want %q in:\n%s", want, wire)
		}
	}
}

// TestReadBoundsAreSymbolic pins item 3: the read side spells its refusal
// bound from the same symbolic expressions the write side asserts against,
// never a pre-folded literal (9999ULL against MAX_OBJECTS - 1), and an enum's
// refusal and write assert both name <ENUM>_MAX.
func TestReadBoundsAreSymbolic(t *testing.T) {
	u := unitFromSource(t, "Sym.schema", `package symtest

const MAX_OBJECTS = 10000
const UNITS = 4096

enum Kind { A, B, C, D, E }

type Sym {
    id   int32 | min = 0, max = MAX_OBJECTS - 1
    pos  int32 | min = -UNITS, max = UNITS
    kind Kind
}
`)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	wire := string(files["SymWire.h"])
	for _, want := range []string{
		"serialize_assert( (serialize_int64_t) value->id >= 0 && (serialize_int64_t) value->id <= MAX_OBJECTS - 1 );",
		"if ( offset_value > (serialize_uint64_t) ( MAX_OBJECTS - 1 ) )",
		"if ( offset_value > (serialize_uint64_t) UNITS - (serialize_uint64_t) ( -UNITS ) )",
		"serialize_assert( value->kind <= KIND_MAX );",
		"if ( enum_value > KIND_MAX )",
	} {
		if !strings.Contains(wire, want) {
			t.Errorf("want %q in:\n%s", want, wire)
		}
	}
	for _, banned := range []string{"9999ULL", "8192ULL", "enum_value > 5", "<= 5 )"} {
		if strings.Contains(wire, banned) {
			t.Errorf("read bound folded to a literal: found %q in:\n%s", banned, wire)
		}
	}
}
