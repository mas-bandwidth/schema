package main

import (
	"encoding/binary"
	"math"
	"testing"

	"tblv1"
)

// 32-bit floating-point fields: valid-data write/read acceptance
// (docs/roadmap.sexp float32-values/go/valid-data; contract "§3.4 record,
// float32"). The law: a float32 rides the fixed-form record as the IEEE-754
// bit pattern of a FINITE value, four bytes, little-endian, with NO
// canonicalisation (docs/FIXED-FORM-ALGORITHM.md, docs/SPEC-TABLES.md §3,
// §3.3), so a same-schema write/read round trip lands every valid value
// BIT-EXACT — an ordinary fraction, a negative, the range boundaries,
// negative zero and the subnormals alike. A NaN or an infinity is not valid
// data (Glenn, 2026-10-27): TestRowFloat32ValuesNonFiniteRefused holds the
// writer's and the reader's refusal. The bench gate (bench/tables/go/table_main.go:403,413)
// proves external corpus byte fidelity; this row asserts the decoded values
// directly, per the roadmap's remaining note.
//
// The subject is tblv1's Cfg, the fixed table the conformance driver already
// links: `b float32` at top level, `factor float32` inside the nested Inner,
// and `charge float32` under Effect's ward arm. The wire offsets pinned below
// are the generated writer's own (CfgFixedWriteBody: b at body[4:], inner at
// body[45:] with factor its first field, the effect tag at body[137:] with
// the ward payload at body[138:]); each record is the 8-byte hash and then
// the body (CfgFixedRecordBytes), behind the 16-byte header, the 4-byte
// layout length and the layout itself.
func TestRowFloat32ValuesValidData(t *testing.T) {
	values := []tblv1.Cfg{
		{
			B:     0.75,                      // 0x3F400000: an exact binary fraction
			Inner: tblv1.Inner{Factor: -2.5}, // 0xC0200000: the sign set
			Effect: tblv1.Effect{
				Type: tblv1.EffectTypeWard,
				Ward: tblv1.Ward{Charge: math.MaxFloat32}, // 0x7F7FFFFF: the top of the range
			},
		},
		{
			B:     math.Float32frombits(0x007FFFFF),                 // the largest subnormal: a flush to zero would lose it
			Inner: tblv1.Inner{Factor: math.SmallestNonzeroFloat32}, // 0x00000001: the subnormal floor
			Effect: tblv1.Effect{
				Type: tblv1.EffectTypeWard,
				Ward: tblv1.Ward{Charge: math.Float32frombits(0x80000000)}, // negative zero: == 0 but not its pattern
			},
		},
	}

	buf := make([]byte, tblv1.CfgFixedMeasure(int64(len(values))))
	n := tblv1.CfgFixedSave(values, buf)
	if n < 0 || int(n) != len(buf) {
		t.Fatalf("CfgFixedSave: n=%d want %d", n, len(buf))
	}

	// The byte pins: each float32 on the wire IS its bit pattern, at the
	// writer's own offset (the derivation in the header comment).
	body := func(k int) []byte {
		off := tblv1.TableFixedHeaderBytes + 4 + len(tblv1.CfgFixedLayout) + k*tblv1.CfgFixedRecordBytes
		return buf[off+8 : off+tblv1.CfgFixedRecordBytes]
	}
	pin := func(k, off int, v float32, what string) {
		var want [4]byte
		binary.LittleEndian.PutUint32(want[:], math.Float32bits(v))
		got := body(k)[off : off+4]
		if string(got) != string(want[:]) {
			t.Errorf("record %d %s: wire bytes %x want %x (the IEEE-754 bit pattern)", k, what, got, want)
		}
	}
	pin(0, 4, values[0].B, "b")
	pin(0, 45, values[0].Inner.Factor, "inner.factor")
	pin(0, 138, values[0].Effect.Ward.Charge, "effect.ward.charge")
	pin(1, 4, values[1].B, "b")
	pin(1, 45, values[1].Inner.Factor, "inner.factor")
	pin(1, 138, values[1].Effect.Ward.Charge, "effect.ward.charge")

	// The read acceptance: the valid file loads clean, moves no counter, and
	// every float32 lands bit-exact.
	scratch := make([]tblv1.Cfg, len(values))
	plan := make([]tblv1.TableFixedEntry, 8192)
	var report tblv1.TableReport
	got := tblv1.CfgFixedLoad(scratch, buf, plan, &report)
	if got != int64(len(values)) {
		t.Fatalf("CfgFixedLoad: n=%d want %d (verdict=%v reason=%q malformed=%v)",
			got, len(values), report.Verdict, report.Reason, report.Malformed)
	}
	if report.Verdict != tblv1.TableOpenOk || report.Malformed ||
		report.Widened != 0 || report.Unknown != 0 || report.KindMismatch != 0 || report.Clamped != 0 {
		t.Fatalf("valid data read dirty: verdict=%v malformed=%v widened=%d unknown=%d kind_mismatch=%d clamped=%d",
			report.Verdict, report.Malformed, report.Widened, report.Unknown, report.KindMismatch, report.Clamped)
	}
	for k := range values {
		if scratch[k].Effect.Type != tblv1.EffectTypeWard {
			t.Errorf("record %d: effect.type=%v want ward", k, scratch[k].Effect.Type)
		}
		if math.Float32bits(scratch[k].B) != math.Float32bits(values[k].B) {
			t.Errorf("record %d: b bits %08x want %08x", k, math.Float32bits(scratch[k].B), math.Float32bits(values[k].B))
		}
		if math.Float32bits(scratch[k].Inner.Factor) != math.Float32bits(values[k].Inner.Factor) {
			t.Errorf("record %d: inner.factor bits %08x want %08x", k, math.Float32bits(scratch[k].Inner.Factor), math.Float32bits(values[k].Inner.Factor))
		}
		if math.Float32bits(scratch[k].Effect.Ward.Charge) != math.Float32bits(values[k].Effect.Ward.Charge) {
			t.Errorf("record %d: effect.ward.charge bits %08x want %08x", k, math.Float32bits(scratch[k].Effect.Ward.Charge), math.Float32bits(values[k].Effect.Ward.Charge))
		}
	}
}

// TestRowFloat32ValuesNonFiniteRefused is the sibling row: a NaN or an
// infinity is refused by the fixed writer, and a record a foreign writer hands
// in carrying one — at the top level, inside the nested Inner, under the ward
// arm — is refused by the load as non_finite_float, naming the field, with the
// batch landing nothing (docs/SPEC-TABLES.md §3.3, the 2026-10-27 rule).
func TestRowFloat32ValuesNonFiniteRefused(t *testing.T) {
	good := []tblv1.Cfg{
		{B: 0.75, Inner: tblv1.Inner{Factor: -2.5}, Effect: tblv1.Effect{Type: tblv1.EffectTypeWard, Ward: tblv1.Ward{Charge: 1.5}}},
		{B: 0.25, Inner: tblv1.Inner{Factor: 3.5}, Effect: tblv1.Effect{Type: tblv1.EffectTypeWard, Ward: tblv1.Ward{Charge: -4.5}}},
	}
	for _, bits := range []uint32{0x7FC0DEAD, 0x7F800001, 0xFFA5A5A5, 0x7F800000, 0xFF800000} {
		bad := append([]tblv1.Cfg(nil), good...)
		bad[1].Inner.Factor = math.Float32frombits(bits)
		if n := tblv1.CfgFixedSave(bad, make([]byte, tblv1.CfgFixedMeasure(2))); n != -1 {
			t.Errorf("%08x: the writer wrote it (n=%d)", bits, n)
		}
	}
	buf := make([]byte, tblv1.CfgFixedMeasure(int64(len(good))))
	if n := tblv1.CfgFixedSave(good, buf); int(n) != len(buf) {
		t.Fatalf("CfgFixedSave: n=%d", n)
	}
	body := tblv1.TableFixedHeaderBytes + 4 + len(tblv1.CfgFixedLayout) + tblv1.CfgFixedRecordBytes + 8
	for _, site := range []struct {
		off  int
		name string
	}{{4, "Cfg.b"}, {45, "Inner.factor"}, {138, "Ward.charge"}} {
		for _, bits := range []uint32{0x7FC0DEAD, 0xFF800000} {
			forged := append([]byte(nil), buf...)
			binary.LittleEndian.PutUint32(forged[body+site.off:], bits)
			scratch := make([]tblv1.Cfg, 2)
			plan := make([]tblv1.TableFixedEntry, 8192)
			var report tblv1.TableReport
			if n := tblv1.CfgFixedLoadTolerant(scratch, forged, plan, &report); n != -1 ||
				report.Reason != "non_finite_float: "+site.name || report.Err() != tblv1.TableRefuseNonFiniteFloat {
				t.Errorf("%s %08x: n=%d reason=%q err=%v, want the non_finite_float refusal", site.name, bits, n, report.Reason, report.Err())
			}
			var reset tblv1.Cfg
			tblv1.CfgReset(&reset)
			if scratch[0].B != reset.B || scratch[1].B != reset.B || scratch[1].Inner.Factor != reset.Inner.Factor {
				t.Errorf("%s %08x: the refused batch kept %v %v %v, want the defaults", site.name, bits, scratch[0].B, scratch[1].B, scratch[1].Inner.Factor)
			}
		}
	}
}
