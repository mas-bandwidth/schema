package tablecook_test

import (
	"encoding/binary"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// §7.4's ELEMENT-ARRAY CLAUSE (docs/SPEC-TABLES.md §2.9, §7.4): an unbounded
// array's slot must point its array inside the holder's own extent, aligned,
// fitting, and overlapping no other array in that node. The tool cannot cook a
// list yet, so the cook under test is assembled BY HAND from §7.1 and §7.2:
// one root node, `Ints` from tables/lists, whose sixteen-byte slot names a
// three-element array laid after the record.

// intsCook writes a cook of one Ints root with the given slot and count. The
// record is 24 bytes (the slot, then `after` and its padding), the array of
// three int32 follows at 24, and the data part rounds to 40.
func intsCook(u *ir.Unit, delta int64, count int32) []byte {
	const header, data, attrib = int64(64), int64(40), int64(16)
	out := make([]byte, header+data+attrib)
	le := binary.LittleEndian
	le.PutUint64(out[0:], tablecook.Magic)
	le.PutUint64(out[8:], ir.BuildVersion(u))
	le.PutUint64(out[16:], tablecook.ByteOrderLittle)
	le.PutUint64(out[24:], uint64(data))
	le.PutUint64(out[32:], uint64(attrib))
	le.PutUint64(out[40:], 8)
	record := out[header:]
	le.PutUint64(record[0:], uint64(delta))
	le.PutUint32(record[8:], uint32(count))
	le.PutUint32(record[16:], 5) // after
	for i := 0; i < 3; i++ {
		le.PutUint32(record[24+i*4:], uint32(10*(i+1)))
	}
	dir := out[header+data:]
	le.PutUint64(dir[0:], 0)
	le.PutUint64(dir[8:], ir.TableTypeId("Ints"))
	return out
}

// TestCookCheckListSlot: the clause reads CONTAINMENT, ALIGNMENT, FIT and NO
// OVERLAP, and nothing else, and a null reference is an empty list and only
// that. The Makefile's negative control drops the containment test through an
// overlay and requires this test to go red.
func TestCookCheckListSlot(t *testing.T) {
	u := unit(t, "../../tables/lists")
	m := tabletext.NewModel(u)

	res, err := tablecook.Check(m, intsCook(u, 24, 3))
	if err != nil {
		t.Fatalf("a cook whose list slot names its array inside the node was refused: %v", err)
	}
	if res.Root != "Ints" || res.Nodes != 1 {
		t.Fatalf("checked the wrong shape: %+v", res)
	}
	if _, err := tablecook.Check(m, intsCook(u, 0, 0)); err != nil {
		t.Fatalf("an empty list, null reference and zero count, was refused: %v", err)
	}

	cases := []struct {
		name  string
		delta int64
		count int32
		want  string
	}{
		{"the array leaves the node", 40, 3, "leaves the node"},
		{"the array leaves the region", 4096, 3, "leaves the node"},
		{"the array is not aligned", 26, 3, "not aligned"},
		{"the count does not fit", 24, 5, "leaves the node"},
		{"the count is negative", 24, -1, "never negative"},
		{"a null reference with a count", 0, 3, "empty list"},
		{"a reference with no count", 24, 0, "reference is not null"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := tablecook.Check(m, intsCook(u, c.delta, c.count))
			if err == nil {
				t.Fatalf("FAILED: cook-check accepted a list slot that %s", c.name)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("refused, but not on the element-array clause: %v", err)
			}
		})
	}
}

// squadCook writes a cook of one Squad root from tables/lists, the holder of
// `roster map[uint8]Item`: a 24-byte record, the sixteen-byte map slot at
// null and `name` after it, and one directory entry.
func squadCook(u *ir.Unit) []byte {
	const header, data, attrib = int64(64), int64(24), int64(16)
	out := make([]byte, header+data+attrib)
	le := binary.LittleEndian
	le.PutUint64(out[0:], tablecook.Magic)
	le.PutUint64(out[8:], ir.BuildVersion(u))
	le.PutUint64(out[16:], tablecook.ByteOrderLittle)
	le.PutUint64(out[24:], uint64(data))
	le.PutUint64(out[32:], uint64(attrib))
	le.PutUint64(out[40:], 8)
	le.PutUint32(out[header+16:], 7) // name
	dir := out[header+data:]
	le.PutUint64(dir[0:], 0)
	le.PutUint64(dir[8:], ir.TableTypeId("Squad"))
	return out
}

// TestCookCheckMapSlotRefusedByName: §7.4's map-slot clause is schema#380's
// next PR, so the scan refuses a map slot BY NAME where it meets one, naming
// the field, the reference that reads it and the PR that lands the clause,
// and a cook of a map-free root in the same unit checks as any other does.
func TestCookCheckMapSlotRefusedByName(t *testing.T) {
	u := unit(t, "../../tables/lists")
	m := tabletext.NewModel(u)
	_, err := tablecook.Check(m, squadCook(u))
	if err == nil {
		t.Fatalf("FAILED: cook-check walked past a map slot it has no clause for")
	}
	for _, want := range []string{"Squad.roster", "schema#380", "cpp", "§7.4"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %q: %v", want, err)
		}
	}
	if _, err := tablecook.Check(m, intsCook(u, 24, 3)); err != nil {
		t.Fatalf("a map-free root in a unit that declares a map elsewhere was refused: %v", err)
	}
}
