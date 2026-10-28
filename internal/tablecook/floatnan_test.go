package tablecook_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// pinnedWire is one of the table wire goldens the C++ reference writes.
func pinnedWire(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "wire", "tables", name+".bin"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// A COOKED FLOAT IS FINITE (docs/SPEC-TABLES.md §3, §3.3, §7.2; Glenn,
// 2026-10-27: "Tables go finite-only too, same rule as packets").
//
// The cook is the wire's own bytes laid out for a mapped read, so what §3 says
// about a float32 field it says about the float32 SLOT: the four bytes are the
// pattern of a FINITE value, -0 and the subnormals included, and nothing on
// either side of the region normalizes one. The tool holds a float32 in a
// float64 cell, and every finite float32 crosses the two widths exactly by the
// ordinary conversion, so no bit surgery is owed. A NaN or an infinity is
// refused by the cook's writer, by `schema cook-check` (the cook's strict
// validator, ruling 2 of 2026-10-27) and by the uncook, which decodes every
// float, each as non_finite_float naming the field.

// floatsSlot is the region offset of one of F1's Floats fields.
func floatsSlot(t *testing.T, u *ir.Unit, field string) int64 {
	t.Helper()
	ml := ir.RecordLayout(u, u.Tables["Floats"])
	for i := range ml.Fields {
		if ml.Fields[i].Field.Name == field {
			return ml.Fields[i].Offset
		}
	}
	t.Fatalf("Floats declares no field %q", field)
	return 0
}

func floatsCell(t *testing.T, inst *tabletext.Instance, field string) *tabletext.Field {
	t.Helper()
	for i := range inst.Fields {
		if inst.Fields[i].Def.Name == field {
			return &inst.Fields[i]
		}
	}
	t.Fatalf("Floats declares no field %q", field)
	return nil
}

func nonFiniteNamed(t *testing.T, what string, err error, field string) {
	t.Helper()
	var refusal *tablewire.NonFiniteRefusal
	if !errors.As(err, &refusal) || refusal.Field != field {
		t.Errorf("%s answered %v, want the non_finite_float refusal naming %s", what, err, field)
	}
}

// TestACookCarriesAFiniteFloatBitPattern: -0 and the subnormals ride the cook
// bit for bit, in BOTH byte orders, and the wire comes back byte for byte.
func TestACookCarriesAFiniteFloatBitPattern(t *testing.T) {
	u := unit(t, "../../test/tables/F1.schema")
	m := tabletext.NewModel(u)
	root := m.Lookup("Floats")
	patterns := map[string]uint32{"signalling": 0x80000000, "payload": 0x007FFFFF, "negative": 0x80000001}
	inst := m.New(root)
	for name, bits := range patterns {
		floatsCell(t, inst, name).Cell.F = float64(math.Float32frombits(bits))
	}
	floatsCell(t, inst, "wide").Cell.F = math.Float64frombits(0x800FFFFFFFFFFFFF)
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatal(err)
	}
	for _, big := range []bool{false, true} {
		cooked, err := tablecook.Cook(m, decode(t, m, "Floats", wire), tablecook.Options{Big: big})
		if err != nil {
			t.Fatalf("big=%v: %v", big, err)
		}
		if _, err := tablecook.Check(m, cooked); err != nil {
			t.Fatalf("big=%v: the tool refused its own cook: %v", big, err)
		}
		h, err := tablecook.ReadHeader(cooked, ir.BuildVersion(u))
		if err != nil {
			t.Fatal(err)
		}
		ord := binary.ByteOrder(binary.LittleEndian)
		if big {
			ord = binary.BigEndian
		}
		data := h.Data(cooked)
		for name, bits := range patterns {
			if got := ord.Uint32(data[floatsSlot(t, u, name):]); got != bits {
				t.Errorf("big=%v: the %s slot holds 0x%08X and the pattern is 0x%08X", big, name, got, bits)
			}
		}
		back, err := tablecook.Uncook(m, root, cooked)
		if err != nil {
			t.Fatalf("big=%v: %v", big, err)
		}
		again, err := tablewire.Encode(m, back)
		if err != nil {
			t.Fatalf("big=%v: %v", big, err)
		}
		if !bytes.Equal(wire, again) {
			t.Fatalf("big=%v: the wire did not survive the cook: %d bytes back, %d written", big, len(again), len(wire))
		}
	}
}

// TestACookRefusesANonFiniteFloat: the writer refuses a NaN or an infinity in
// a scalar and in an element; a region that carries one anyway is refused by
// the check and by the uncook, on its raw bits, in both byte orders. The NaN
// pin the reference writes is refused before a cook is ever made of it.
func TestACookRefusesANonFiniteFloat(t *testing.T) {
	u := unit(t, "../../test/tables/F1.schema")
	m := tabletext.NewModel(u)
	root := m.Lookup("Floats")

	var report tabletext.Report
	if _, err := tablewire.Decode(m, m.New(root), pinnedWire(t, "floats_nan"), &report); err == nil {
		t.Fatal("the oracle read the NaN pin: there is nothing to cook")
	}

	for _, v := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		for _, name := range []string{"payload", "wide", "samples"} {
			inst := m.New(root)
			if f := floatsCell(t, inst, name); name == "samples" {
				f.Elems[2].F = v
			} else {
				f.Cell.F = v
			}
			_, err := tablecook.Cook(m, inst, tablecook.Options{})
			nonFiniteNamed(t, "Cook", err, "Floats."+name)
		}
	}

	for _, big := range []bool{false, true} {
		cooked, err := tablecook.Cook(m, m.New(root), tablecook.Options{Big: big})
		if err != nil {
			t.Fatal(err)
		}
		h, err := tablecook.ReadHeader(cooked, ir.BuildVersion(u))
		if err != nil {
			t.Fatal(err)
		}
		ord := binary.ByteOrder(binary.LittleEndian)
		if big {
			ord = binary.BigEndian
		}
		for _, c := range []struct {
			field string
			wide  bool
		}{{"negative", false}, {"quiet", true}} {
			for _, bits := range []uint64{0x7F800001, 0xFF800000, 0x7FF8000000000000, 0xFFF0000000000000} {
				if !c.wide && bits > 0xFFFFFFFF || c.wide && bits <= 0xFFFFFFFF {
					continue
				}
				bad := append([]byte(nil), cooked...)
				data := h.Data(bad) // the data part of the copy, in place
				if c.wide {
					ord.PutUint64(data[floatsSlot(t, u, c.field):], bits)
				} else {
					ord.PutUint32(data[floatsSlot(t, u, c.field):], uint32(bits))
				}
				_, err := tablecook.Check(m, bad)
				nonFiniteNamed(t, "Check", err, "Floats."+c.field)
				_, err = tablecook.Uncook(m, root, bad)
				nonFiniteNamed(t, "Uncook", err, "Floats."+c.field)
			}
		}
	}
}
