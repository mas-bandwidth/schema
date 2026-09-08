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

func TestSkippedMapRepeatPreservesEarlierValue(t *testing.T) {
	m := listModel(t, "package probe\ntable Root { entries map[int32]int32 }\n")
	original := place(t, m, "Root", `{"entries":{"3":9}}`)
	wire, err := tablewire.Encode(m, original)
	if err != nil {
		t.Fatal(err)
	}
	trailer := len(wire) - 8 - int(binary.LittleEndian.Uint64(wire[len(wire)-8:]))*8
	for _, repeat := range [][]byte{{1, 14, 2, 5, 0}, {1, 14, 0}, {1, 14, 2, 13, 128}} {
		damaged := append([]byte(nil), wire[:trailer-1]...)
		damaged = append(damaged, repeat...)
		damaged = append(damaged, wire[trailer-1:]...)
		value := m.New(m.Lookup("Root"))
		var report tabletext.Report
		_, err = tablewire.Decode(m, value, damaged, &report)
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := tablewire.Encode(m, value)
		if err != nil || !bytes.Equal(encoded, wire) {
			t.Fatalf("skipped repeat %x destroyed earlier value: %x %+v %v", repeat, encoded, report, err)
		}
	}
}
