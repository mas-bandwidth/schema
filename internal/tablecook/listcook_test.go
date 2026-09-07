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

// THE TOOL'S COOK AND UNCOOK HALVES FOR AN UNBOUNDED ARRAY (schema#380,
// docs/SPEC-TABLES.md §2.9, §7.6).
//
// Before this PR both surfaces refused a list-bearing unit BY NAME rather than
// laying out a region short of the element arrays:
//
//	unit declares an unbounded array in a table closure (Save.placements): the
//	tool's WIRE and TEXT halves carry the construct and `cook-check` reads one,
//	and its COOK half does not [...] The C++ reference carries the cook
//
// They carry it now: the elements ride in the HOLDER'S node extent, laid after
// the record's storage in the PRE-ORDER the reference lays them, and the round
// trip through a cook and back is byte for byte on the wire.
//
// `tables/lists` cannot be the corpus here — it declares a map, which the
// tool's cook still refuses by name — so test/tables/L1.schema is the same
// shapes with no map in them.

func listUnit(t *testing.T) (*ir.Unit, *tabletext.Model) {
	t.Helper()
	u := unit(t, "../../test/tables/L1.schema")
	return u, tabletext.NewModel(u)
}

// buildListSave is the instance every row below cooks: a list of tables that
// hold lists (the extent at two depths), a list of scalars beside it, a list of
// POINTER elements naming one shared node and one of its own, a pointed-at
// holder whose node carries its own list, and a field after them all.
func buildListSave(m *tabletext.Model) *tabletext.Instance {
	save := m.New(m.Lookup("Save"))
	setInt(save, "after", 41)

	row := func(label int64, samples ...int64) *tabletext.Instance {
		r := m.New(m.Lookup("Row"))
		setInt(r, "label", label)
		items := field(r, "items")
		for _, v := range samples {
			s := m.New(m.Lookup("Sample"))
			setInt(s, "v", v)
			items.Elems = append(items.Elems, tabletext.Cell{Tab: s})
		}
		items.Count = len(items.Elems)
		return r
	}

	rows := field(save, "rows")
	for _, r := range []*tabletext.Instance{
		row(1, 10, 20, 30),
		row(2),         // an EMPTY list one depth down: null reference, zero count
		row(3, 40, 50), //
	} {
		rows.Elems = append(rows.Elems, tabletext.Cell{Tab: r})
	}
	rows.Count = len(rows.Elems)

	marks := field(save, "marks")
	for _, v := range []int64{7, -7, 1 << 20} {
		marks.Elems = append(marks.Elems, tabletext.Cell{I: v, U: uint64(v)})
	}
	marks.Count = len(marks.Elems)

	// the POINTER elements and the pointed-at holder: `linked[0]` and `pinned`
	// name ONE node, so the numbering carries it once and two slots hold two
	// different deltas to it
	held := row(9, 99)
	other := row(8)
	linked := field(save, "linked")
	linked.Elems = append(linked.Elems, tabletext.Cell{Node: held}, tabletext.Cell{Node: other})
	linked.Count = len(linked.Elems)
	setNode(save, "pinned", held)
	return save
}

// TestTheToolCooksAndUncooksAList: the round trip the tool owes, in both byte
// orders — the wire in, a cook the tool's own `cook-check` accepts, and the
// same wire back out, byte for byte.
func TestTheToolCooksAndUncooksAList(t *testing.T) {
	_, m := listUnit(t)
	root := m.Lookup("Save")

	wire, err := tablewire.Encode(m, buildListSave(m))
	if err != nil {
		t.Fatal(err)
	}
	for _, big := range []bool{false, true} {
		inst := decode(t, m, "Save", wire)
		cooked, err := tablecook.Cook(m, inst, tablecook.Options{Big: big})
		if err != nil {
			t.Fatalf("big=%v: the tool refused to cook a list: %v", big, err)
		}
		res, err := tablecook.Check(m, cooked)
		if err != nil {
			t.Fatalf("big=%v: the tool's own cook did not pass cook-check: %v", big, err)
		}
		// the root, the two pointed-at Rows, and nothing else: a list's
		// elements are NOT nodes, they ride in the holder's extent
		if res.Nodes != 3 {
			t.Errorf("big=%v: the region carries %d nodes, and a list's elements are not nodes: want 3", big, res.Nodes)
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
			t.Fatalf("big=%v: the wire did not survive the cook: %d bytes back, %d in", big, len(again), len(wire))
		}
	}
}

// TestAListRidesInItsHolderNodeExtent reads the SLOT out of the cooked bytes
// rather than trusting the round trip: a sixteen-byte slot holding a
// SELF-RELATIVE delta and a count, its array inside the holder's own extent,
// after the record's storage rounded to the region's alignment floor. A round
// trip with two mistakes that cancel would pass the row above and fail here.
func TestAListRidesInItsHolderNodeExtent(t *testing.T) {
	u, m := listUnit(t)
	wire, err := tablewire.Encode(m, buildListSave(m))
	if err != nil {
		t.Fatal(err)
	}
	cooked, err := tablecook.Cook(m, decode(t, m, "Save", wire), tablecook.Options{})
	if err != nil {
		t.Fatal(err)
	}
	h, err := tablecook.ReadHeader(cooked, ir.BuildVersion(u))
	if err != nil {
		t.Fatal(err)
	}
	data := h.Data(cooked)
	le := binary.LittleEndian

	ml := ir.RecordLayout(u, m.Lookup("Save"))
	at := int64(-1)
	for i := range ml.Fields {
		if ml.Fields[i].Field.Name == "rows" {
			at = ml.Fields[i].Offset
		}
	}
	if at != 0 {
		t.Fatalf("Save.rows is the record's first field and its slot is at %d", at)
	}
	delta := int64(le.Uint64(data[at:]))
	count := int32(le.Uint32(data[at+8:]))
	if count != 3 {
		t.Fatalf("the rows slot carries a count of %d", count)
	}
	// the record's storage is rounded to the region's alignment floor, and the
	// FIRST array carved is `rows` — the walk takes the fields in declaration
	// order, whole array first
	record := ir.RegionAlignFloor * ((ml.Size + ir.RegionAlignFloor - 1) / ir.RegionAlignFloor)
	if start := at + delta; start != record {
		t.Errorf("the rows array starts at %d and the record ends at %d: the first array carved sits at the extent's base", start, record)
	}
	rowSize, rowAlign := ir.ListElementLayout(u, ml.Fields[0].Field)
	end := at + delta + int64(count)*rowSize
	if (at+delta)%rowAlign != 0 {
		t.Errorf("the rows array starts at %d, which is not aligned to %d", at+delta, rowAlign)
	}
	if end > h.DataLength {
		t.Errorf("the rows array runs to %d and the data part is %d", end, h.DataLength)
	}
	// AND THE ARRAYS DO NOT OVERLAP: `marks` is carved after `rows` and after
	// the arrays each row's own `items` took, which is the PRE-ORDER rule
	marksAt := ml.Fields[1].Offset
	marksStart := marksAt + int64(le.Uint64(data[marksAt:]))
	if marksStart < end {
		t.Errorf("the marks array starts at %d and the rows array ends at %d: the whole array comes first, then the elements' own", marksStart, end)
	}
}

// TestTheToolWritesTheReferencesListCook hands the C++ leg what it needs: the
// wire this engine writes and the two cooks it produces from it, which
// `make tables-lists-tool-cook` then holds the generated `SaveCook` to BYTE
// FOR BYTE. A cook is content-addressed by (asset hash, build version), so two
// writers of one instance produce ONE artifact or the pair means nothing.
func TestTheToolWritesTheReferencesListCook(t *testing.T) {
	dir := os.Getenv("SCHEMA_LIST_TOOL_COOK_DIR")
	if dir == "" {
		t.Skip("set SCHEMA_LIST_TOOL_COOK_DIR to write the fixtures the C++ leg reads")
	}
	_, m := listUnit(t)
	wire, err := tablewire.Encode(m, buildListSave(m))
	if err != nil {
		t.Fatal(err)
	}
	write := func(name string, data []byte) {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("l1.bin", wire)
	for _, c := range []struct {
		name string
		big  bool
	}{{"l1.cook", false}, {"l1-be.cook", true}} {
		cooked, err := tablecook.Cook(m, decode(t, m, "Save", wire), tablecook.Options{Big: c.big})
		if err != nil {
			t.Fatal(err)
		}
		write(c.name, cooked)
	}
}
