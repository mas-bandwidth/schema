package luatable

// THE CLOSED ARRAY ELEMENT KINDS (docs/SPEC-TABLES.md §3): an array body (kind
// 14) may carry element kinds 1–13, 15, 17 and 18–30, and an enum-keyed body
// (kind 16) may carry 1–11, 13 and 18–30. Any other element kind is damage
// under any count, a count of 0 included, in a field the reader cannot name as
// in one it declares. Each vector below rides an unknown root field of
// graphdemo Scene, so only the wire-alone walk can see it; the last two are the
// read controls, element kind 17 at count 0 and at count 1 where its node index
// names a reachable record.

import (
	"os"
	"path/filepath"
	"testing"
)

// luaArrayElemVerdicts are the pinned element-kind vectors and the verdict the
// engine answers for each: the element kind byte is judged against its body
// kind's closed list before any count.
var luaArrayElemVerdicts = []struct {
	name string
	read bool
}{
	{name: "array_elem_kind_32_count_2", read: false},
	{name: "array_elem_kind_31_count_1", read: false},
	{name: "array_elem_kind_16_count_1", read: false},
	{name: "array_elem_unassigned_count_0", read: false},
	{name: "array_elem_reserved_count_0", read: false},
	{name: "array_elem_zero_count_0", read: false},
	{name: "array_elem_kind_17_count_0", read: true},
	{name: "array_elem_kind_17_count_1", read: true},
}

// TestLuaArrayElementKindsClosed runs every pinned element-kind vector through
// the emitted strict load and compares the verdict alone
// (docs/rulings.json refusal-verdict-only).
func TestLuaArrayElementKindsClosed(t *testing.T) {
	t.Parallel()
	_, units := luaFramingRows(t)
	if len(units["graphdemo"]) == 0 {
		t.Fatal("the manifest names no graphdemo unit")
	}
	rows := make([]luaFramingRow, 0, len(luaArrayElemVerdicts))
	for _, tc := range luaArrayElemVerdicts {
		wire := filepath.ToSlash(filepath.Join("testdata", "wire", "tables", "framing", tc.name+".bin"))
		if _, err := os.Stat(filepath.Join("..", "..", "..", wire)); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		rows = append(rows, luaFramingRow{name: tc.name, unit: "graphdemo", root: "Scene", wire: wire})
	}
	got := luaFramingVerdicts(t, rows, units, nil)
	for _, tc := range luaArrayElemVerdicts {
		want := "refused"
		if tc.read {
			want = "read"
		}
		if got[tc.name] != want {
			t.Errorf("%s: the strict load answers %q, want %q", tc.name, got[tc.name], want)
		}
	}
}
