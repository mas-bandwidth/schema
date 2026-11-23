package tablewire_test

import (
	"bytes"
	"math"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

const negZeroUnit = `package negzero

table Var
{
    zero float32 | min = -1, max = 1, resolution = 0.01
    half float32 = 0.5 | min = -1, max = 1, resolution = 0.01
    many [3]float32 | min = -1, max = 1, resolution = 0.01
    plain float32 = 0.5
}

fixed table Fix
{
    zero float32 | min = -1, max = 1, resolution = 0.01
    half float32 = 0.5 | min = -1, max = 1, resolution = 0.01
    plain float32 = 0.5
}
`

// TestCompressedNegativeZeroStoresPlusZero: A COMPRESSED FLOAT'S -0 IS +0
// (docs/SPEC-TABLES.md §3.3; ruling fixed-compressed-negzero): under a zero
// default it is elided, and wherever it rides (a non-zero default, an array
// element, a fixed record) the writer stores the +0 bits, never 0x80000000.
// The plain float beside it is the control: its -0 rides bit for bit.
func TestCompressedNegativeZeroStoresPlusZero(t *testing.T) {
	t.Parallel()
	m := floatBoundModel(t, negZeroUnit)
	negZero := math.Copysign(0, -1)
	for _, root := range []string{"Var", "Fix"} {
		inst := m.New(m.Lookup(root))
		floatField(t, inst, "zero").Cell.F = negZero
		floatField(t, inst, "half").Cell.F = negZero
		if root == "Var" {
			for i := range floatField(t, inst, "many").Elems {
				floatField(t, inst, "many").Elems[i].F = manyAt(i, negZero)
			}
		}
		floatField(t, inst, "plain").Cell.F = negZero
		wire, err := floatBoundWrite(m, root, inst)
		if err != nil {
			t.Fatalf("%s: %v", root, err)
		}
		// exactly one 0x80000000: the plain float's
		if n := bytes.Count(wire, le32(0x80000000)); n != 1 {
			t.Fatalf("%s: the wire holds %d copies of the -0 bits, want 1 (the plain float's): %x", root, n, wire)
		}
		// the same value holding +0 in the compressed fields writes the same bytes
		plus := m.New(m.Lookup(root))
		floatField(t, plus, "zero").Cell.F = 0
		floatField(t, plus, "half").Cell.F = 0
		if root == "Var" {
			for i := range floatField(t, plus, "many").Elems {
				floatField(t, plus, "many").Elems[i].F = manyAt(i, 0)
			}
		}
		floatField(t, plus, "plain").Cell.F = negZero
		want, err := floatBoundWrite(m, root, plus)
		if err != nil {
			t.Fatalf("%s: %v", root, err)
		}
		if !bytes.Equal(wire, want) {
			t.Fatalf("%s: -0 wrote other bytes than +0:\n-0 %x\n+0 %x", root, wire, want)
		}
		back := m.New(m.Lookup(root))
		var report tabletext.Report
		if ok, err := tablewire.Decode(m, back, wire, &report); !ok || err != nil || report.Refused {
			t.Fatalf("%s: %v %v %+v", root, ok, err, report)
		}
		for _, name := range []string{"zero", "half"} {
			if got := math.Float64bits(floatField(t, back, name).Cell.F); got != 0 {
				t.Fatalf("%s.%s landed %#x, want +0", root, name, got)
			}
		}
	}
}

// TestFixedRecordRefusesNonFinite: a NaN or an infinity forged into a fixed
// record is refused on its raw bits, in a compressed and in a plain float.
func TestFixedRecordRefusesNonFinite(t *testing.T) {
	t.Parallel()
	m := floatBoundModel(t, negZeroUnit)
	inst := m.New(m.Lookup("Fix"))
	floatField(t, inst, "zero").Cell.F = float64(float32(0.25))
	floatField(t, inst, "plain").Cell.F = float64(float32(-3.75))
	wire, err := tablewire.EncodeFixed(m, inst)
	if err != nil {
		t.Fatal(err)
	}
	for _, from := range []float32{0.25, -3.75} {
		for _, bits := range nonFinite32 {
			bad := replaced(t, wire, le32(math.Float32bits(from)), le32(bits))
			var report tabletext.Report
			ok, err := tablewire.Decode(m, m.New(m.Lookup("Fix")), bad, &report)
			if ok || tablewire.RefusalReason(err) != tablewire.ReasonNonFiniteFloat {
				t.Errorf("fixed %v -> %#08x: want non_finite_float, got %v %v", from, bits, ok, err)
			}
		}
	}
}

// manyAt is the array's element i: a zero (of the given sign) around a 0.5, so
// the array is not all-default and rides with its zeros in it.
func manyAt(i int, zero float64) float64 {
	if i == 1 {
		return 0.5
	}
	return zero
}
