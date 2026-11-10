package tablewire_test

import (
	"os"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// A MAP WHOSE KEY IS DAMAGED, repeated or not, refuses the retaining read
// whole (docs/SPEC-TABLES.md §4, §6.5): nothing is retained and the store is
// empty. The vector carries ill-formed text in a key and a second occurrence
// of the map field.
func TestRetainedMapReplacementAfterDamage(t *testing.T) {
	m := listModel(t, `package mapdemo
 table Text {
 names map[string(8)]string(16)
 wide map[uint16]string(6)
 blobs map[int32]bytes(10)
 past int32 | was = "after"
 }`)
	wire, err := os.ReadFile("../../testdata/wire/tables/fuzz-vectors/map_retained_replaced.bin")
	if err != nil {
		t.Fatal(err)
	}
	if len(wire) != 168 || wire[66] != 1 || wire[67] != 14 || wire[68] != 36 {
		t.Fatal("replacement vector changed")
	}
	without := append(append([]byte(nil), wire[:66]...), wire[105:]...)
	for _, tc := range []struct {
		name string
		wire []byte
	}{{"replacement", wire}, {"damage only", without}} {
		t.Run(tc.name, func(t *testing.T) {
			value := m.New(m.Lookup("Text"))
			keep := &tablewire.Retain{Capacity: 8192, IdCapacity: 1024}
			var read tabletext.Report
			ok, err := tablewire.DecodeRetain(m, value, tc.wire, keep, &read)
			if ok || !tablewire.ReadRefusal(err) || keep.Used() != 0 {
				t.Fatalf("load: %v %v %+v used %d", ok, err, read, keep.Used())
			}
		})
	}
}
