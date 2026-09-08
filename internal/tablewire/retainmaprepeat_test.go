package tablewire_test

import (
	"os"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestRetainedMapReplacementAfterDamage(t *testing.T) {
	m := listModel(t, `package mapdemo
 table Text {
 names map[string(8)]string(16)
 wide map[uint16]wstring(6)
 blobs map[int32]bytes(10)
 after int32
 }`)
	wire, err := os.ReadFile("../../testdata/wire/tables/fuzz-vectors/map_retained_replaced.bin")
	if err != nil {
		t.Fatal(err)
	}
	// The third field is a compatible replacement of names. Without it, the
	// earlier key damage has orphaned one retained body and the save loses it.
	if len(wire) != 168 || wire[66] != 1 || wire[67] != 14 || wire[68] != 36 {
		t.Fatal("replacement vector changed")
	}
	without := append(append([]byte(nil), wire[:66]...), wire[105:]...)
	for _, tc := range []struct {
		name string
		wire []byte
		lost int
	}{{"replacement", wire, 0}, {"damage only", without, 1}} {
		t.Run(tc.name, func(t *testing.T) {
			value := m.New(m.Lookup("Text"))
			keep := &tablewire.Retain{Capacity: 8192, IdCapacity: 1024}
			var read, save tabletext.Report
			ok, err := tablewire.DecodeRetain(m, value, tc.wire, keep, &read)
			if !ok || err != nil || read.Retained != 2 || read.RetainLost != 0 {
				t.Fatalf("load: %v %v %+v", ok, err, read)
			}
			out, err := tablewire.EncodeRetain(m, value, keep, &save)
			if err != nil || len(out) == 0 || save.RetainLost != tc.lost {
				t.Fatalf("save: %v %+v, want lost %d", err, save, tc.lost)
			}
		})
	}
}
