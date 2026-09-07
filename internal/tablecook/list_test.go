package tablecook_test

import (
	"encoding/binary"
	"os"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// §7.4's ELEMENT-ARRAY and MAP-SLOT clauses (docs/SPEC-TABLES.md §2.8, §2.9,
// §7.4): a container's slot must point its array inside the holder's own
// extent, aligned, fitting, and overlapping no other array in that node — and
// a map's entries must ascend with no key twice on top of that.
//
// The cooks under test are assembled BY HAND from §7.1 and §7.2 rather than
// written by either writer, because a CHECK has to meet bytes no writer would
// produce: one root node from tables/lists, whose sixteen-byte slot names an
// array laid after the record, with the delta, the count and the keys as the
// forgery wants them. The tool's own cook of a list is exercised in
// listcook_test.go, against the reference's bytes.

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
	for i := range 3 {
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
// `roster map[uint8]Item`: a 24-byte record — the sixteen-byte map slot, then
// `name` — and an entry array of `SquadRosterEntry` (a `uint8` key at 0, an
// `Item` value at 4, eight bytes at four) laid in the node's extent after it.
// `keys` are the stored keys, in the order the forgery wants them.
func squadCook(u *ir.Unit, delta int64, count int32, keys []uint8) []byte {
	const header, data, attrib = int64(64), int64(48), int64(16)
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
	le.PutUint32(record[16:], 7) // name
	for i, k := range keys {
		record[24+i*8] = k
		le.PutUint32(record[24+i*8+4:], uint32(10*(i+1))) // the value's count
	}
	dir := out[header+data:]
	le.PutUint64(dir[0:], 0)
	le.PutUint64(dir[8:], ir.TableTypeId("Squad"))
	return out
}

// TestCookCheckMapSlot is §7.4's MAP-SLOT clause (docs/SPEC-TABLES.md §2.8,
// §7.4): the four clauses a list's slot already takes — CONTAINMENT,
// ALIGNMENT, FIT and NO OVERLAP against the holder's own extent — and the
// FIFTH one a list has no analogue for, the KEYS read ascending with no
// repeat, because a cook's `Find` is a binary search in place over those
// bytes. The Makefile's negative control drops the containment test through an
// overlay and requires this test to go red beside the list's.
func TestCookCheckMapSlot(t *testing.T) {
	u := unit(t, "../../tables/lists")
	m := tabletext.NewModel(u)

	res, err := tablecook.Check(m, squadCook(u, 24, 3, []uint8{1, 2, 200}))
	if err != nil {
		t.Fatalf("a cook whose map slot names its entries inside the node was refused: %v", err)
	}
	if res.Root != "Squad" || res.Nodes != 1 {
		t.Fatalf("checked the wrong shape: %+v", res)
	}
	if _, err := tablecook.Check(m, squadCook(u, 0, 0, nil)); err != nil {
		t.Fatalf("an empty map, null reference and zero count, was refused: %v", err)
	}

	cases := []struct {
		name  string
		delta int64
		count int32
		keys  []uint8
		want  string
	}{
		{"the entries leave the node", 48, 3, []uint8{1, 2, 3}, "leaves the node"},
		{"the entries leave the region", 4096, 3, []uint8{1, 2, 3}, "leaves the node"},
		{"the entries are not aligned", 25, 2, []uint8{1, 2}, "not aligned"},
		{"the count does not fit", 24, 4, []uint8{1, 2, 3}, "leaves the node"},
		{"the count is negative", 24, -1, nil, "never negative"},
		{"a null reference with a count", 0, 3, []uint8{1, 2, 3}, "empty map"},
		{"a reference with no count", 24, 0, nil, "reference is not null"},
		{"the keys descend", 24, 3, []uint8{1, 9, 4}, "descend"},
		{"the keys repeat", 24, 3, []uint8{1, 4, 4}, "SAME key"},
		{"the first two keys descend", 24, 2, []uint8{5, 1}, "descend"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := tablecook.Check(m, squadCook(u, c.delta, c.count, c.keys))
			if err == nil {
				t.Fatalf("FAILED: cook-check accepted a map slot where %s", c.name)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("refused, but not on the map-slot clause: %v", err)
			}
		})
	}
}

// AND THE MAP IS ONE POPULATION WITH THE LIST in a node's extent (§2.8, §2.9):
// the cook the C++ reference writes for `Army`, whose list elements each hold
// a map, is read by the tool with no clause left owed. It is the file
// `make tables-lists` produces, so this row is the two writers meeting.
func TestCookCheckReadsTheReferencesMapHoldingCook(t *testing.T) {
	path := "../../build/lists-cooks/army.cook"
	file, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("%s is written by `make tables-lists`: %v", path, err)
	}
	u := unit(t, "../../tables/lists")
	m := tabletext.NewModel(u)
	res, err := tablecook.Check(m, file)
	if err != nil {
		t.Fatalf("the tool refused a cook the C++ reference wrote: %v", err)
	}
	if res.Root != "Army" {
		t.Fatalf("checked the wrong root: %+v", res)
	}
}
