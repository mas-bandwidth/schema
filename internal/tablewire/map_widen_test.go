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
	if err != nil || !ok || report.Widened != 3 || report.Malformed || report.KindMismatch != 0 {
		t.Fatalf("map contributes one, values two: %+v, %v", report, err)
	}
}

func TestSkippedMapRepeatPreservesEarlierValue(t *testing.T) {
	m := listModel(t, "package probe\ntable Root { entries map[int32]int32 }\n")
	original := place(t, m, "Root", `{"entries":{"3":9}}`)
	wire, err := tablewire.Encode(m, original)
	if err != nil {
		t.Fatal(err)
	}
	trailer := len(wire) - 8 - int(binary.LittleEndian.Uint64(wire[len(wire)-8:]))*8
	for _, tc := range []struct {
		repeat  []byte
		damaged bool
	}{
		{[]byte{1, 14, 2, 5, 0}, false},   // a foreign element kind: kind_mismatch, skipped
		{[]byte{1, 14, 0}, false},         // a body too short for a header: inert
		{[]byte{1, 14, 2, 13, 128}, true}, // a count that cannot complete: damage
	} {
		repeat := tc.repeat
		damaged := append([]byte(nil), wire[:trailer-1]...)
		damaged = append(damaged, repeat...)
		damaged = append(damaged, wire[trailer-1:]...)
		value := m.New(m.Lookup("Root"))
		var report tabletext.Report
		_, err = tablewire.Decode(m, value, damaged, &report)
		if err != nil {
			t.Fatal(err)
		}
		if report.Malformed != tc.damaged {
			t.Fatalf("repeat %x: malformed %v, want %v", repeat, report.Malformed, tc.damaged)
		}
		want := wire
		if tc.damaged {
			// DAMAGE DISCARDS THE WHOLE LOAD (docs/SPEC-TABLES.md §4): the
			// root is handed back at its declared defaults
			if want, err = tablewire.Encode(m, m.New(m.Lookup("Root"))); err != nil {
				t.Fatal(err)
			}
		}
		encoded, err := tablewire.Encode(m, value)
		if err != nil || !bytes.Equal(encoded, want) {
			t.Fatalf("repeat %x: the value is %x, want %x (%+v %v)", repeat, encoded, want, report, err)
		}
	}
}

// A repeated key selects its final occurrence, including a later widening kind
// (SPEC-TABLES 2.8). A stale mismatch must not empty an otherwise valid map.
func TestMapRepeatedKeyLastKindWins(t *testing.T) {
	m := listModel(t, "package probe\ntable Root { entries map[uint32]int32 }\n")
	for _, tc := range []struct {
		name            string
		keys            []byte
		count, mismatch int
	}{
		{"bad_then_widened", []byte{2, 9, 0, 0, 0, 0, 0, 0, 0, 0, 2, 6, 7}, 1, 1},
		{"widened_then_bad", []byte{2, 6, 7, 2, 9, 0, 0, 0, 0, 0, 0, 0, 0}, 0, 1},
		{"bad_then_exact", []byte{2, 9, 0, 0, 0, 0, 0, 0, 0, 0, 2, 8, 7, 0, 0, 0}, 1, 1},
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
				ok, err := decode(m, v, wire, &report)
				field, _ := v.FieldByKey("entries")
				if !ok || err != nil || report.Malformed || len(field.Entries) != tc.count || report.KindMismatch != tc.mismatch {
					t.Fatalf("region=%v count=%d report=%+v ok=%v err=%v", region, len(field.Entries), report, ok, err)
				}
				if tc.count != 0 && tabletext.MapKeyOf(field.Def, field.Entries[0].Tab).U != 7 {
					t.Fatal("last key value was not kept")
				}
			}
		})
	}
}
