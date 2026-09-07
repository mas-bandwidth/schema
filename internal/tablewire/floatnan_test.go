package tablewire_test

import (
	"bytes"
	"math"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// A FLOAT RIDES AS ITS BIT PATTERN, AND THE ORACLE CARRIES IT (docs/SPEC-TABLES.md
// §3, §4, SPEC.md §4.3, schema#480).
//
// `internal/tablewire` is the compiler's own engine and the wire fuzzer's
// divergence oracle: a third reading of §3, written from the page rather than
// from a backend, and it holds a float32 in a float64 CELL — exactly the shape
// the deliverable is about. It reads the same pin the C++ reference writes,
// testdata/wire/tables/floats_nan.bin, and must land on the same 32 bits: the
// quiet bit still clear on a signalling NaN, the payload still there, and the
// bytes back out byte for byte.
//
// The negative control lives on the reference (make
// tables-float-nan-negative-control), because the reference is what a port
// mirrors; this row is the oracle agreeing with it.

// the patterns test/tables/floatnan.h pins, in the order F1 declares them
const (
	nanSignalling = uint32(0x7F800001) // the quiet bit CLEAR, the smallest payload
	nanPayload    = uint32(0x7FC0DEAD) // quiet, and a mantissa past the quiet bit
	nanNegative   = uint32(0xFFA5A5A5) // the sign set, signalling, a rich payload
	nanSample0    = uint32(0x7F812345)
	nanSample2    = uint32(0xFF800042)
	nanQuiet64    = uint64(0x7FF8000000000000)
	nanWide64     = uint64(0x7FF00DEFACED0001)
)

// widened is the pattern a float32 NaN takes in a float64: the sign, an
// all-ones exponent and the 23 payload bits in the TOP of the double's 52.
func widened(bits uint32) uint64 {
	return uint64(bits>>31)<<63 | 0x7FF0000000000000 | uint64(bits&0x007FFFFF)<<29
}

// cellBits reads one field's stored float back as the 32 bits it rode as. The
// oracle's cell is a float64, so this is the narrowing the deliverable is
// about, taken at the assertion rather than assumed.
func cellBits(t *testing.T, inst *tabletext.Instance, name string) uint32 {
	t.Helper()
	for i := range inst.Fields {
		if inst.Fields[i].Def.Name == name {
			return uint32(widened32(math.Float64bits(inst.Fields[i].Cell.F)))
		}
	}
	t.Fatalf("F1's Floats declares no field %q", name)
	return 0
}

// widened32 is the inverse of widened: the float32 pattern a widened double
// carries, for a NaN, and the plain narrowing otherwise.
func widened32(b uint64) uint32 {
	if b&0x7FF0000000000000 == 0x7FF0000000000000 && b&0x000FFFFFFFFFFFFF != 0 {
		return uint32(b>>63)<<31 | 0x7F800000 | uint32((b>>29)&0x007FFFFF)
	}
	return math.Float32bits(float32(math.Float64frombits(b)))
}

func cellRaw64(t *testing.T, inst *tabletext.Instance, name string) uint64 {
	t.Helper()
	for i := range inst.Fields {
		if inst.Fields[i].Def.Name == name {
			return math.Float64bits(inst.Fields[i].Cell.F)
		}
	}
	t.Fatalf("no field %q", name)
	return 0
}

// TestOracleCarriesTheFloatBitPattern: the pin decodes into the oracle's cells
// with every bit where the reference put it, and encodes back byte for byte.
func TestOracleCarriesTheFloatBitPattern(t *testing.T) {
	m := retainModel(t, "F1.schema")
	st := m.Lookup("Floats")
	if st == nil {
		t.Fatal("F1.schema declares no Floats")
	}
	pinned := retainVector(t, "floats_nan")

	inst := m.New(st)
	var report tabletext.Report
	if _, err := tablewire.Decode(m, inst, pinned, &report); err != nil {
		t.Fatalf("the oracle refused the pin: %v", err)
	}
	if report.Malformed {
		t.Fatal("the oracle read the pin as malformed")
	}

	for _, c := range []struct {
		field string
		want  uint32
	}{
		{"signalling", nanSignalling},
		{"payload", nanPayload},
		{"negative", nanNegative},
	} {
		if got := cellBits(t, inst, c.field); got != c.want {
			t.Errorf("%s came back as 0x%08X and the pin is 0x%08X: the pattern moved", c.field, got, c.want)
		}
	}
	// the QUIET BIT is bit 22, and a conversion is what sets it
	if cellBits(t, inst, "signalling")&0x00400000 != 0 {
		t.Error("the signalling NaN came back quiet: something on the path converted it")
	}
	if cellBits(t, inst, "negative")&0x00400000 != 0 {
		t.Error("the negative signalling NaN came back quiet")
	}
	// the float64 fields cross no width at all
	if got := cellRaw64(t, inst, "quiet"); got != nanQuiet64 {
		t.Errorf("quiet came back as 0x%016X and the pin is 0x%016X", got, nanQuiet64)
	}
	if got := cellRaw64(t, inst, "wide"); got != nanWide64 {
		t.Errorf("wide came back as 0x%016X and the pin is 0x%016X", got, nanWide64)
	}
	// the ARRAY elements, which take the same path one indirection down
	for i := range inst.Fields {
		if inst.Fields[i].Def.Name != "samples" {
			continue
		}
		elems := inst.Fields[i].Elems
		if len(elems) != 3 {
			t.Fatalf("samples came back with %d elements", len(elems))
		}
		if got := widened32(math.Float64bits(elems[0].F)); got != nanSample0 {
			t.Errorf("samples[0] is 0x%08X and the pin is 0x%08X", got, nanSample0)
		}
		if got := widened32(math.Float64bits(elems[2].F)); got != nanSample2 {
			t.Errorf("samples[2] is 0x%08X and the pin is 0x%08X", got, nanSample2)
		}
	}

	back, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatalf("the oracle refused to write the instance back: %v", err)
	}
	if !bytes.Equal(back, pinned) {
		t.Fatalf("the oracle re-saves the pin differently: %d bytes out, %d pinned", len(back), len(pinned))
	}
}

// TestOracleWidensTheFloatBitPattern is §4's float rung through the oracle: the
// same bytes read by F2's declaration, where every float32 field is spelled
// float64. The payload rides in the top of the double's mantissa, and one
// `widened` is counted per field.
func TestOracleWidensTheFloatBitPattern(t *testing.T) {
	m := retainModel(t, "F2.schema")
	st := m.Lookup("Floats")
	if st == nil {
		t.Fatal("F2.schema declares no Floats")
	}
	pinned := retainVector(t, "floats_nan")

	inst := m.New(st)
	var report tabletext.Report
	if _, err := tablewire.Decode(m, inst, pinned, &report); err != nil {
		t.Fatalf("the oracle refused the pin under the widened declaration: %v", err)
	}
	if report.Malformed || report.KindMismatch != 0 || report.Unknown != 0 {
		t.Fatalf("the widened read is not clean: %+v", report)
	}
	if report.Widened != 3 {
		t.Errorf("three float32 fields read into float64 ones; the report counts %d widened", report.Widened)
	}
	for _, c := range []struct {
		field  string
		narrow uint32
	}{
		{"signalling", nanSignalling},
		{"payload", nanPayload},
		{"negative", nanNegative},
	} {
		want := widened(c.narrow)
		if got := cellRaw64(t, inst, c.field); got != want {
			t.Errorf("%s widened to 0x%016X and the pattern is 0x%016X: the payload crossed the two widths through a conversion", c.field, got, want)
		}
	}
}
