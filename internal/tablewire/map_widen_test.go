package tablewire_test

import (
	"bytes"
	"encoding/binary"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
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

// A MAP FIELD CARRIED TWICE is a field id repeated in one body, which no
// writer produces: damage, whatever the repeat holds (docs/SPEC-TABLES.md §3,
// §4), and the root comes back at its declared defaults.
func TestMapFieldRepeatIsDamage(t *testing.T) {
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
		{[]byte{1, 14, 2, 5, 0}, true},    // a foreign element kind
		{[]byte{1, 14, 0}, true},          // a body too short for a header
		{[]byte{1, 14, 2, 13, 128}, true}, // a count that cannot complete
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
