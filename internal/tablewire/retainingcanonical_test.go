package tablewire_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// The retaining fuzz witnesses keep their original unknown IDs and values;
// their canonical ID tables must let every strict entry reach that mutation.
func TestCanonicalRetainingFuzzInputsRead(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, unit, root string }{
		{"retain_declared_default", "pointers", "Scene"},
		{"retain_byte_blob_reachability", "blobs", "Catalog"},
		{"retain_enum_list_unknown", "lists", "Mixed"},
		{"retain_union_arm_frame", "stream", "Feed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			paths, err := compiler.GatherPaths([]string{filepath.Join("../../tables", tc.unit)})
			if err != nil {
				t.Fatal(err)
			}
			u, err := compiler.New().Load(paths)
			if err != nil {
				t.Fatal(err)
			}
			m := tabletext.NewModel(u)
			wire, err := os.ReadFile(filepath.Join("../../testdata/wire/tables/fuzz-vectors", tc.name+".bin"))
			if err != nil {
				t.Fatal(err)
			}
			for _, region := range []bool{false, true} {
				for _, retaining := range []bool{false, true} {
					value := m.New(m.Lookup(tc.root))
					var report tabletext.Report
					rt := &tablewire.Retain{Capacity: 1 << 20, IdCapacity: 4096}
					var ok bool
					switch {
					case retaining && region:
						ok, err = tablewire.DecodeRegionRetain(m, value, wire, rt, &report)
					case retaining:
						ok, err = tablewire.DecodeRetain(m, value, wire, rt, &report)
					case region:
						ok, err = tablewire.DecodeRegion(m, value, wire, &report)
					default:
						ok, err = tablewire.Decode(m, value, wire, &report)
					}
					if !ok || err != nil || report.Refused || report.Unknown == 0 {
						t.Fatalf("region=%v retain=%v: ok=%v err=%v report=%+v", region, retaining, ok, err, report)
					}
					if !retaining {
						continue
					}
					saved, err := tablewire.EncodeRetain(m, value, rt, &tabletext.Report{})
					if err != nil {
						t.Fatal(err)
					}
					back := m.New(value.Def)
					next := &tablewire.Retain{Capacity: 1 << 20, IdCapacity: 4096}
					if ok, err := tablewire.DecodeRetain(m, back, saved, next, &tabletext.Report{}); !ok || err != nil {
						t.Fatalf("retained writer output refused: %v", err)
					}
					again, err := tablewire.EncodeRetain(m, back, next, &tabletext.Report{})
					if err != nil || !bytes.Equal(saved, again) {
						t.Fatalf("retained writer output is not a fixed point: %v", err)
					}
				}
			}
		})
	}
}
