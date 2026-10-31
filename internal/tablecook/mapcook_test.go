package tablecook_test

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE TOOL'S COOK AND UNCOOK HALVES FOR A MAP (docs/SPEC-TABLES.md §2.8, §7.6):
// the generated `{ key, value }` entries ride in the HOLDER'S node extent as a
// list's elements do, the whole entry array first at `alignof( Entry )` in
// ASCENDING KEY ORDER, then, entry by entry, the arrays any entry holds by
// value. Every map instance of the corpus (tables/maps) is cooked in both
// byte orders, passes the tool's own cook-check, which bounds every entry
// array inside its holder and checks the keys ascend, and uncooks to the wire
// it came from.

var mapCookInstances = []struct{ name, root string }{
	{"map_cells", "Cells"}, {"map_chunks", "Chunks"}, {"map_crews", "Crews"},
	{"map_depth", "Depth"}, {"map_docs", "Docs"}, {"map_empty", "Fleet"},
	{"map_full", "Fleet"}, {"map_pairs", "Pairs"}, {"map_slots", "Slots"},
	{"map_spans", "Spans"}, {"map_text", "Text"}, {"map_trails", "Trails"},
	{"map_runs", "Runs"},
}

func TestTheToolCooksAndUncooksAMap(t *testing.T) {
	u := unit(t, "../../tables/maps")
	m := tabletext.NewModel(u)
	for _, c := range mapCookInstances {
		wire, err := os.ReadFile(filepath.Join("../../testdata/wire/tables", c.name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		canonical, err := tablewire.Encode(m, decode(t, m, c.root, wire))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		for _, big := range []bool{false, true} {
			cooked, err := tablecook.Cook(m, decode(t, m, c.root, wire), tablecook.Options{Big: big})
			if err != nil {
				t.Fatalf("%s big=%v: the tool refused to cook a map: %v", c.name, big, err)
			}
			if _, err := tablecook.Check(m, cooked); err != nil {
				t.Fatalf("%s big=%v: the tool's own cook did not pass cook-check: %v", c.name, big, err)
			}
			back, err := tablecook.Uncook(m, m.Lookup(c.root), cooked)
			if err != nil {
				t.Fatalf("%s big=%v: %v", c.name, big, err)
			}
			again, err := tablewire.Encode(m, back)
			if err != nil {
				t.Fatalf("%s big=%v: %v", c.name, big, err)
			}
			if !bytes.Equal(canonical, again) {
				t.Fatalf("%s big=%v: the value did not survive the cook: %d bytes back, %d in", c.name, big, len(again), len(canonical))
			}
		}
	}
}

// TestAMapRidesInItsHolderNodeExtent reads the SLOT out of the cooked bytes
// rather than trusting the round trip: map_full's root Fleet holds its maps by
// value, so each map's sixteen-byte slot holds a SELF-RELATIVE delta into the
// root node's own extent — after the record's storage, rounded to the region's
// alignment floor — at the entry's alignment, and the count. Two mistakes that
// cancel in a round trip are red here.
func TestAMapRidesInItsHolderNodeExtent(t *testing.T) {
	u := unit(t, "../../tables/maps")
	m := tabletext.NewModel(u)
	wire, err := os.ReadFile("../../testdata/wire/tables/map_full.bin")
	if err != nil {
		t.Fatal(err)
	}
	cooked, err := tablecook.Cook(m, decode(t, m, "Fleet", wire), tablecook.Options{})
	if err != nil {
		t.Fatal(err)
	}
	h, err := tablecook.ReadHeader(cooked, ir.BuildVersion(u))
	if err != nil {
		t.Fatal(err)
	}
	region := h.Data(cooked)
	le := binary.LittleEndian
	ml := ir.RecordLayout(u, m.Lookup("Fleet"))
	record := ir.RegionAlignFloor * ((ml.Size + ir.RegionAlignFloor - 1) / ir.RegionAlignFloor)
	seen := 0
	prevEnd := record
	for i := range ml.Fields {
		fl := &ml.Fields[i]
		if !fl.Field.IsMap() {
			continue
		}
		at := fl.Offset
		delta := int64(le.Uint64(region[at:]))
		count := int64(int32(le.Uint32(region[at+8:])))
		if count == 0 {
			if delta != 0 {
				t.Errorf("%s: an empty map's reference is %d, not null", fl.Field.Name, delta)
			}
			continue
		}
		seen++
		entry := ir.RecordLayout(u, fl.Field.MapEntry)
		start := at + delta
		if start < prevEnd {
			t.Errorf("%s: the entry array starts at %d, before %d where the record or the previous array ends", fl.Field.Name, start, prevEnd)
		}
		if start%entry.Align != 0 {
			t.Errorf("%s: the entry array at %d is not at alignof( Entry ) = %d", fl.Field.Name, start, entry.Align)
		}
		prevEnd = start + count*entry.Size
		if prevEnd > int64(len(region)) {
			t.Errorf("%s: the entry array leaves the region", fl.Field.Name)
		}
	}
	if seen == 0 {
		t.Fatal("map_full's Fleet carried no non-empty map to read")
	}
}
