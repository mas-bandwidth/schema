package tablewire_test

import (
	"bytes"
	"encoding/binary"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
	"testing"
)

func TestMapKeyWidenCountsOnceAndValueStillCounts(t *testing.T) {
	source := listModel(t, "package probe\ntable Root { entries map[int8]int8 }\n")
	target := listModel(t, "package probe\ntable Root { entries map[int32]int32 }\n")
	wire, err := tablewire.Encode(source, place(t, source, "Root", `{"entries":{"-3":-5,"2":9}}`))
	if err != nil {
		t.Fatal(err)
	}
	value := target.New(target.Lookup("Root"))
	var report tabletext.Report
	ok, err := tablewire.Decode(target, value, wire, &report)
	if err != nil || !ok || report.Widened != 3 || report.Refused {
		t.Fatalf("map contributes one, values two: %+v, %v", report, err)
	}
}

// A REPEATED FIELD ID IS DAMAGE (docs/SPEC-TABLES.md §4, §6.5), whatever the
// repeat carries: a foreign element kind, a body too short for its header, or
// a count that cannot complete. The negative control is the wire without the
// repeat, which reads and writes back byte-identical.
func TestMapFieldRepeatRefuses(t *testing.T) {
	m := listModel(t, "package probe\ntable Root { entries map[int32]int32 }\n")
	original := place(t, m, "Root", `{"entries":{"3":9}}`)
	wire, err := tablewire.Encode(m, original)
	if err != nil {
		t.Fatal(err)
	}
	control := m.New(m.Lookup("Root"))
	var clean tabletext.Report
	if ok, err := tablewire.Decode(m, control, wire, &clean); !ok || err != nil {
		t.Fatalf("control: %v %+v", err, clean)
	}
	if back, err := tablewire.Encode(m, control); err != nil || !bytes.Equal(back, wire) {
		t.Fatalf("control: wrote %x, want %x (%v)", back, wire, err)
	}
	trailer := len(wire) - 8 - int(binary.LittleEndian.Uint64(wire[len(wire)-8:]))*8
	for _, repeat := range [][]byte{
		{1, 14, 2, 5, 0},    // a foreign element kind
		{1, 14, 0},          // a body too short for a header
		{1, 14, 2, 13, 128}, // a count that cannot complete
	} {
		damaged := append([]byte(nil), wire[:trailer-1]...)
		damaged = append(damaged, repeat...)
		damaged = append(damaged, wire[trailer-1:]...)
		var report tabletext.Report
		if ok, err := tablewire.Decode(m, m.New(m.Lookup("Root")), damaged, &report); ok || tablewire.RefusalReason(err) != tablewire.ReasonWireDamaged {
			t.Fatalf("repeat %x: %v %v, want wire_damaged", repeat, ok, err)
		}
	}
}

// A key field repeated inside one map entry is a repeated field id, damage on
// the wire alone, whichever kinds the occurrences carry (§4, §6.5).
func TestMapRepeatedKeyRefuses(t *testing.T) {
	m := listModel(t, "package probe\ntable Root { entries map[uint32]int32 }\n")
	for _, tc := range []struct {
		name string
		keys []byte
	}{
		{"bad_then_widened", []byte{2, 9, 0, 0, 0, 0, 0, 0, 0, 0, 2, 6, 7}},
		{"widened_then_bad", []byte{2, 6, 7, 2, 9, 0, 0, 0, 0, 0, 0, 0, 0}},
		{"bad_then_exact", []byte{2, 9, 0, 0, 0, 0, 0, 0, 0, 0, 2, 8, 7, 0, 0, 0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entry := append(append([]byte(nil), tc.keys...), 0)
			payload := append([]byte{13, 1, byte(len(entry))}, entry...)
			wire := append([]byte{1, 1, 14, byte(len(payload))}, payload...)
			wire = append(wire, 0)
			wire = binary.LittleEndian.AppendUint64(wire, ir.TableFieldWireId(m.Lookup("Root").Fields[0]))
			wire = binary.LittleEndian.AppendUint64(wire, ir.MapKeyWireId)
			wire = binary.LittleEndian.AppendUint64(wire, 2)
			for _, region := range []bool{false, true} {
				v := m.New(m.Lookup("Root"))
				var report tabletext.Report
				decode := tablewire.Decode
				if region {
					decode = tablewire.DecodeRegion
				}
				if ok, err := decode(m, v, wire, &report); ok || tablewire.RefusalReason(err) != tablewire.ReasonWireDamaged {
					t.Fatalf("region=%v: %v %v, want wire_damaged", region, ok, err)
				}
			}
		})
	}
}
