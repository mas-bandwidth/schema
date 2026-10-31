package tablewire_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// A FLOAT IS FINITE, AND THE ORACLE REFUSES ONE THAT IS NOT (docs/SPEC-TABLES.md
// §3, §3.3; Glenn, 2026-10-27: "Tables go finite-only too, same rule as
// packets. NaN/inf refused on write and read, -0 and subnormals kept").
//
// `internal/tablewire` is the compiler's own engine and the wire fuzzer's
// divergence oracle: a third reading of §3, written from the page rather than
// from a backend, and it holds a float32 in a float64 CELL. The pin the C++
// reference writes, testdata/wire/tables/floats_nan.bin, carries a signalling
// NaN, a NaN with a payload, a negative NaN and two float64 NaNs; under the
// finite rule it is a MUST-REFUSE vector, and the oracle refuses it on the raw
// bits as non_finite_float, naming the first such field, at both widths and as
// array elements. Every writer refuses one too, and -0 and the subnormals ride
// bit for bit.

// the patterns test/tables/floatnan.h pins, plus the infinities.
var (
	nonFinite32 = []uint32{0x7F800001, 0x7FC0DEAD, 0xFFA5A5A5, 0x7F812345, 0xFF800042, 0x7FC00000, 0x7F800000, 0xFF800000}
	nonFinite64 = []uint64{0x7FF8000000000000, 0x7FF00DEFACED0001, 0xFFF0000000000001, 0x7FF0000000000000, 0xFFF0000000000000}
)

func floatField(t *testing.T, inst *tabletext.Instance, name string) *tabletext.Field {
	t.Helper()
	for i := range inst.Fields {
		if inst.Fields[i].Def.Name == name {
			return &inst.Fields[i]
		}
	}
	t.Fatalf("Floats declares no field %q", name)
	return nil
}

// refusedAs is the oracle's refusal of a non-finite float, by name: the typed
// error, the report's own slot and its Reason.
func refusedAs(t *testing.T, what string, err error, report tabletext.Report, field string) {
	t.Helper()
	var refusal *tablewire.NonFiniteRefusal
	if !errors.As(err, &refusal) || refusal.Field != field {
		t.Errorf("%s: the oracle answered %v, want the non_finite_float refusal naming %s", what, err, field)
		return
	}
	if err.Error() != "non_finite_float: "+field {
		t.Errorf("%s: the refusal reads %q", what, err.Error())
	}
	if report.NonFiniteFloat != field || report.Reason != "non_finite_float: "+field {
		t.Errorf("%s: the report names %q, Reason %q; want %s", what, report.NonFiniteFloat, report.Reason, field)
	}
}

// finiteFloats is F1's Floats with a distinct finite value in every float,
// so each one's bytes can be found in the wire and replaced.
func finiteFloats(t *testing.T, m *tabletext.Model) *tabletext.Instance {
	t.Helper()
	inst := m.New(m.Lookup("Floats"))
	floatField(t, inst, "signalling").Cell.F = float64(float32(1.25))
	floatField(t, inst, "payload").Cell.F = float64(float32(-2.5))
	floatField(t, inst, "negative").Cell.F = float64(float32(3.75))
	floatField(t, inst, "quiet").Cell.F = 1.0e100
	floatField(t, inst, "wide").Cell.F = -7.0e-200
	samples := floatField(t, inst, "samples")
	for i, v := range []float32{11.5, -12.5, 13.5} {
		samples.Elems[i].F = float64(v)
	}
	return inst
}

// replaced is the wire with the one occurrence of `from` replaced by `to`.
func replaced(t *testing.T, wire, from, to []byte) []byte {
	t.Helper()
	if n := bytes.Count(wire, from); n != 1 {
		t.Fatalf("the pattern %x occurs %d times in the wire, want once", from, n)
	}
	return bytes.Replace(wire, from, to, 1)
}

func le32(v uint32) []byte { return binary.LittleEndian.AppendUint32(nil, v) }
func le64(v uint64) []byte { return binary.LittleEndian.AppendUint64(nil, v) }

// TestOracleRefusesTheFloatNaNPin: the reference's NaN pin is refused under
// F1, and under F2, where every float32 field is spelled float64 and the
// refusal comes before the float rung (§4) is taken.
func TestOracleRefusesTheFloatNaNPin(t *testing.T) {
	t.Parallel()
	pinned := retainVector(t, "floats_nan")
	for _, schema := range []string{"F1.schema", "F2.schema"} {
		m := retainModel(t, schema)
		inst := m.New(m.Lookup("Floats"))
		var report tabletext.Report
		ok, err := tablewire.Decode(m, inst, pinned, &report)
		if ok {
			t.Errorf("%s: the oracle accepted the NaN pin", schema)
		}
		refusedAs(t, schema, err, report, "Floats.signalling")
	}
}

// TestOracleRefusesEachNonFinitePattern: every NaN and infinity, in every
// float position — a float32 scalar, a float64 scalar, an array element and a
// float32 read into a float64 (F2's widened rung) — is refused on its raw
// bits, naming its own field.
func TestOracleRefusesEachNonFinitePattern(t *testing.T) {
	t.Parallel()
	m1, m2 := retainModel(t, "F1.schema"), retainModel(t, "F2.schema")
	wire, err := tablewire.Encode(m1, finiteFloats(t, m1))
	if err != nil {
		t.Fatal(err)
	}
	// the finite wire reads back clean under both, and the float32 fields widen
	for _, m := range []*tabletext.Model{m1, m2} {
		var report tabletext.Report
		if ok, err := tablewire.Decode(m, m.New(m.Lookup("Floats")), wire, &report); !ok || err != nil || report.NonFiniteFloat != "" {
			t.Fatalf("the finite wire did not read: %v %v %+v", ok, err, report)
		}
	}
	type site struct {
		field string
		from  []byte
	}
	sites32 := []site{
		{"Floats.signalling", le32(math.Float32bits(1.25))},
		{"Floats.payload", le32(math.Float32bits(-2.5))},
		{"Floats.negative", le32(math.Float32bits(3.75))},
		{"Floats.samples", le32(math.Float32bits(-12.5))},
	}
	sites64 := []site{
		{"Floats.quiet", le64(math.Float64bits(1.0e100))},
		{"Floats.wide", le64(math.Float64bits(-7.0e-200))},
	}
	for _, s := range sites32 {
		for _, bits := range nonFinite32 {
			bad := replaced(t, wire, s.from, le32(bits))
			for name, m := range map[string]*tabletext.Model{"F1": m1, "F2": m2} {
				var report tabletext.Report
				ok, err := tablewire.Decode(m, m.New(m.Lookup("Floats")), bad, &report)
				if ok {
					t.Errorf("%s %s %#08x: accepted", name, s.field, bits)
				}
				refusedAs(t, name+" "+s.field, err, report, s.field)
			}
		}
	}
	for _, s := range sites64 {
		for _, bits := range nonFinite64 {
			bad := replaced(t, wire, s.from, le64(bits))
			var report tabletext.Report
			_, err := tablewire.Decode(m1, m1.New(m1.Lookup("Floats")), bad, &report)
			refusedAs(t, s.field, err, report, s.field)
		}
	}
}

// TestOracleWriterRefusesNonFinite: no form carries a NaN or an infinity, so
// the encoder refuses one by name, in a scalar and in an element.
func TestOracleWriterRefusesNonFinite(t *testing.T) {
	t.Parallel()
	m := retainModel(t, "F1.schema")
	for _, v := range []float64{math.NaN(), math.Inf(1), math.Inf(-1), math.Float64frombits(0x7FF00DEFACED0001)} {
		for _, name := range []string{"signalling", "quiet", "samples"} {
			inst := finiteFloats(t, m)
			f := floatField(t, inst, name)
			if name == "samples" {
				f.Elems[1].F = v
			} else {
				f.Cell.F = v
			}
			_, err := tablewire.Encode(m, inst)
			var refusal *tablewire.NonFiniteRefusal
			if !errors.As(err, &refusal) || refusal.Field != "Floats."+name {
				t.Errorf("Encode of %v in %s answered %v, want the non_finite_float refusal", v, name, err)
			}
		}
	}
}

// TestOracleSubnormalAndMinusZeroRide: the finite rule keeps every finite
// pattern, -0 and the subnormals included, bit for bit through the oracle,
// at both widths and across the float rung.
func TestOracleSubnormalAndMinusZeroRide(t *testing.T) {
	t.Parallel()
	m1, m2 := retainModel(t, "F1.schema"), retainModel(t, "F2.schema")
	inst := finiteFloats(t, m1)
	floatField(t, inst, "signalling").Cell.F = float64(math.Float32frombits(0x80000000)) // -0
	floatField(t, inst, "payload").Cell.F = float64(math.Float32frombits(0x007FFFFF))    // the largest subnormal
	floatField(t, inst, "negative").Cell.F = float64(math.Float32frombits(0x80000001))   // the smallest negative subnormal
	floatField(t, inst, "quiet").Cell.F = math.Float64frombits(0x8000000000000000)
	floatField(t, inst, "wide").Cell.F = math.Float64frombits(0x000FFFFFFFFFFFFF)
	wire, err := tablewire.Encode(m1, inst)
	if err != nil {
		t.Fatal(err)
	}
	back := m1.New(m1.Lookup("Floats"))
	var report tabletext.Report
	if ok, err := tablewire.Decode(m1, back, wire, &report); !ok || err != nil {
		t.Fatalf("the finite patterns did not read: %v %v %+v", ok, err, report)
	}
	for name, want := range map[string]uint32{"signalling": 0x80000000, "payload": 0x007FFFFF, "negative": 0x80000001} {
		if got := math.Float32bits(float32(floatField(t, back, name).Cell.F)); got != want {
			t.Errorf("%s came back %#08x, want %#08x", name, got, want)
		}
	}
	for name, want := range map[string]uint64{"quiet": 0x8000000000000000, "wide": 0x000FFFFFFFFFFFFF} {
		if got := math.Float64bits(floatField(t, back, name).Cell.F); got != want {
			t.Errorf("%s came back %#016x, want %#016x", name, got, want)
		}
	}
	if again, err := tablewire.Encode(m1, back); err != nil || !bytes.Equal(again, wire) {
		t.Errorf("the finite patterns do not re-save byte for byte: %v", err)
	}
	// the float rung: every finite float32 is exact in a float64
	wide := m2.New(m2.Lookup("Floats"))
	report = tabletext.Report{}
	if ok, err := tablewire.Decode(m2, wide, wire, &report); !ok || err != nil || report.Widened != 3 {
		t.Fatalf("the widened read: %v %v %+v", ok, err, report)
	}
	for name, want := range map[string]uint32{"signalling": 0x80000000, "payload": 0x007FFFFF, "negative": 0x80000001} {
		if got := math.Float64bits(floatField(t, wide, name).Cell.F); got != math.Float64bits(float64(math.Float32frombits(want))) {
			t.Errorf("%s widened to %#016x, want the exact value of %#08x", name, got, want)
		}
	}
}

// replacedBits is a bitpacked batch with the one `width`-bit occurrence of
// `from` replaced by `to`, bits read low first as the bit writer puts them.
func replacedBits(t *testing.T, wire []byte, from, to uint64, width int) []byte {
	t.Helper()
	get := func(at int) uint64 {
		var v uint64
		for i := range width {
			if wire[(at+i)/8]>>uint((at+i)%8)&1 == 1 {
				v |= 1 << uint(i)
			}
		}
		return v
	}
	found := -1
	for at := 0; at+width <= len(wire)*8; at++ {
		if get(at) == from {
			if found >= 0 {
				t.Fatalf("the %d-bit pattern %#x occurs twice in the batch", width, from)
			}
			found = at
		}
	}
	if found < 0 {
		t.Fatalf("the %d-bit pattern %#x is not in the batch", width, from)
	}
	out := append([]byte(nil), wire...)
	for i := range width {
		bit := byte(1) << uint((found+i)%8)
		if to>>uint(i)&1 == 1 {
			out[(found+i)/8] |= bit
		} else {
			out[(found+i)/8] &^= bit
		}
	}
	return out
}

// TestOracleMessageFormRefusesNonFinite: the message form's writer refuses a
// NaN or an infinity, and its read refuses one on the raw bits and delivers
// no body of the batch.
func TestOracleMessageFormRefusesNonFinite(t *testing.T) {
	t.Parallel()
	m := retainModel(t, "F1.schema")
	var vocabulary tablewire.Vocabulary
	var announced tabletext.Report
	if err := vocabulary.AnnounceRead(tablewire.Announce(m.Unit), &announced); err != nil {
		t.Fatal(err)
	}
	inst := finiteFloats(t, m)
	floatField(t, inst, "quiet").Cell.F = math.Inf(-1)
	if _, err := tablewire.EncodeMessage(m, inst); err == nil || err.Error() != "non_finite_float: Floats.quiet" {
		t.Errorf("the message writer answered %v, want the non_finite_float refusal of Floats.quiet", err)
	}
	batch, err := tablewire.EncodeMessage(m, finiteFloats(t, m))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		field    string
		from, to uint64
		width    int
	}{
		{"Floats.signalling", uint64(math.Float32bits(1.25)), 0x7F800001, 32},
		{"Floats.samples", uint64(math.Float32bits(13.5)), 0xFF800000, 32},
		{"Floats.wide", math.Float64bits(-7.0e-200), 0x7FF8000000000000, 64},
	} {
		bad := replacedBits(t, batch, c.from, c.to, c.width)
		var report tabletext.Report
		n, ok, err := tablewire.DecodeMessages(m, []*tabletext.Instance{m.New(m.Lookup("Floats"))}, bad, &vocabulary, &report)
		if ok || n != 0 {
			t.Errorf("%s: the batch delivered %d bodies", c.field, n)
		}
		refusedAs(t, "message "+c.field, err, report, c.field)
	}
}

// TestOracleRetainedFloatIsTested is ruling 3 of 2026-10-27 (SPEC-TABLES §3.3,
// §6.6): an unknown entry a retaining read keeps, whose kind is a float kind,
// is tested on its raw bits like a known field and refused when it is not
// finite; the plain read, which retains nothing, steps over it by its kind.
func TestOracleRetainedFloatIsTested(t *testing.T) {
	t.Parallel()
	writer := listModel(t, "package rf\n\ntable T\n{\n    x int32\n    f float32\n    d float64\n}\n")
	reader := listModel(t, "package rf\n\ntable T\n{\n    x int32\n}\n")
	inst := writer.New(writer.Lookup("T"))
	oracleField(t, inst, "x").Cell.I = 7
	oracleField(t, inst, "f").Cell.F = float64(float32(0.75))
	oracleField(t, inst, "d").Cell.F = 2.0e50
	wire, err := tablewire.Encode(writer, inst)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][]byte{
		replaced(t, wire, le32(math.Float32bits(0.75)), le32(0x7FC00001)),
		replaced(t, wire, le64(math.Float64bits(2.0e50)), le64(0xFFF0000000000000)),
	} {
		retain := tablewire.Retain{Capacity: 8192, IdCapacity: 1024}
		var report tabletext.Report
		ok, err := tablewire.DecodeRetain(reader, reader.New(reader.Lookup("T")), bad, &retain, &report)
		var refusal *tablewire.NonFiniteRefusal
		if ok || !errors.As(err, &refusal) {
			t.Errorf("the retaining read answered %v %v, want the non_finite_float refusal", ok, err)
		}
		var plain tabletext.Report
		if ok, err := tablewire.Decode(reader, reader.New(reader.Lookup("T")), bad, &plain); !ok || err != nil || plain.Unknown != 2 {
			t.Errorf("the plain read of the same wire answered %v %v %+v, want two unknowns stepped over", ok, err, plain)
		}
	}
}
