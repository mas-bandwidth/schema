package gotable

// THE COMPRESSED FLOAT'S SPECIAL VALUES, PINNED THROUGH THE GO ORACLE
// (docs/SPEC-TABLES.md §3.3, the compressed-float section of the R30 law).
//
// A compressed float has one quantizer on the packet wire and the message wire
// (SPEC.md §4.3): `min + index * step`, with `step = delta / count` and
// `count = ceil(delta / res)`. The law is silent on the values outside
// [min, max] — a -0, a NaN, the infinities, and the below-min / above-max
// neighbours — so each leg does what its arithmetic does. This test pins what
// the Go oracle does TODAY through the pure-Go surface that holds the rule: the
// MESSAGE-FORM quantizer, ir.TableMessageQuantize / Dequantize, which the
// gotable message writer and the tablewire engine both call.
//
// It is a PIN, not a repro: green at BASE, it says the law cannot change
// silently. The one-line sabotage that turns it red is the `normalized = 0`
// half of the NaN clamp in ir.TableMessageQuantize — change it to
// `normalized = 1` and the NaN rows fail — left in the report, undone.
// The non-quantized surfaces (the FIXED form and the VARIABLE table, which ride
// the raw float, and the TEXT/JSON writer, which refuses a non-finite value) are
// deliberate differences, each its own card; they are not asserted here.

import (
	"math"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// cfloatShape is the fixed-form triple the fixture units declare: [0, 1] at
// resolution 0.01, so `count` is 100.
func cfloatShape() ir.TableMessageShape {
	return ir.TableMessageShape{
		Packing: ir.TableMessageQuantized,
		Bits:    7,
		QMin:    0,
		QMax:    1,
		QRes:    0.01,
	}
}

func TestCompressedFloatSpecialValuesGo(t *testing.T) {
	t.Parallel()

	s := cfloatShape()
	count, delta, ok := ir.TableMessageQuantization(s)
	if !ok {
		t.Fatal("the triple [0, 1] at 0.01 is non-conforming")
	}
	if count != 100 {
		t.Fatalf("count = %d, want 100 for [0, 1] at 0.01", count)
	}
	_ = delta

	// rows: input bits, the quantized index, the decoded float's bits. The
	// table is the spec paragraph's, asserted through the Go oracle.
	rows := []struct {
		name string
		in   uint32
		idx  uint32
		out  uint32
	}{
		{"plus_zero", 0x00000000, 0, 0x00000000},
		{"negative_zero", 0x80000000, 0, 0x00000000}, // -0 quantizes as +0
		{"nan_quiet", 0x7FC00000, 0, 0x00000000},     // NaN clamps to min
		{"nan_payload", 0x7FC0DEAD, 0, 0x00000000},
		{"plus_inf", 0x7F800000, 100, 0x3F800000}, // +Inf clamps to max
		{"minus_inf", 0xFF800000, 0, 0x00000000},   // -Inf clamps to min
		{"below_min", 0xC0A00000, 0, 0x00000000},   // -5.0 < min
		{"above_max", 0x40000000, 100, 0x3F800000}, // 2.0 > max
	}
	for _, r := range rows {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			v := math.Float32frombits(r.in)
			idx := ir.TableMessageQuantize(s, v)
			if idx != r.idx {
				t.Fatalf("quantize(%s = %g) = index %d, want %d", r.name, v, idx, r.idx)
			}
			dq := ir.TableMessageDequantize(s, idx)
			if bits := math.Float32bits(dq); bits != r.out {
				t.Fatalf("dequantize(id %d) = %08x, want %08x", idx, bits, r.out)
			}
		})
	}

	// The index is in [0, count] for every special: no out-of-range index, no
	// overflow into a neighbour bit, no decoded value outside [min, max].
	for _, r := range rows {
		if r.idx > count {
			t.Errorf("index %d for %s overflows the declared width %d", r.idx, r.name, count)
		}
	}
}
