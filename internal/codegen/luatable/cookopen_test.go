package luatable

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestLuaCookOpenIsTheHeader holds the Lua cook's Open to the runtime's
// MATCH AND POINT (docs/SPEC-TABLES.md §7, §7.1): it reads the header and the
// base's placement and nothing else, so a forged reference slot or directory
// entry OPENS (cook-check's refusals are the tool's, not the runtime's, §7.4),
// while Check, which runs cook-check's two passes, refuses the same bytes by
// name. The lead is the caller's placement: a base one byte past an aligned one
// is unaligned_base, a base a whole alignment past one opens, and no buffer is
// the reference's null base (testdata/conformance/tables/MANIFEST.txt, the
// cook_lead_* and cook_open_* forgeries).
//
// REVERSED WITNESSES: an Open that runs the scan again answers bad_layout on
// the forged cooks, and an Open that drops the base check opens the lead-1
// buffer; either fails a row below.
func TestLuaCookOpenIsTheHeader(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	u := luarun.LoadUnit(t, filepath.Join(repo, "tables", "pointers"))
	m := tabletext.NewModel(u)
	wire, err := os.ReadFile(filepath.Join(repo, "testdata", "wire", "tables", "graph_tree.bin"))
	if err != nil {
		t.Fatal(err)
	}
	inst := m.New(m.Lookup("Scene"))
	var report tabletext.Report
	if _, err := tablewire.Decode(m, inst, wire, &report); err != nil {
		t.Fatal(err)
	}
	valid, err := tablecook.Cook(m, inst, tablecook.Options{})
	if err != nil {
		t.Fatal(err)
	}
	h, err := tablecook.ReadHeader(valid, ir.BuildVersion(u))
	if err != nil {
		t.Fatal(err)
	}
	le := binary.LittleEndian
	forward := append([]byte(nil), valid...)
	slot := h.DataOffset() + cookSlot(t, u, m, "Scene", "head", 0)
	le.PutUint64(forward[slot:], 0x7fffffffffffffff)
	directory := append([]byte(nil), valid...)
	le.PutUint64(directory[h.AttribOffset()+16:], tablecook.NotMaterialized)

	cases := []struct {
		name string
		cook []byte
		call string
		want string
	}{
		{"a valid cook opens", valid, `cook.Open(T, "Scene", inputs.cook)`, "open"},
		{"a forward delta opens", forward, `cook.Open(T, "Scene", inputs.cook)`, "open"},
		{"a forward delta fails the check", forward, `cook.Check(T, "Scene", inputs.cook)`, "bad_layout"},
		{"a forged directory entry opens", directory, `cook.Open(T, "Scene", inputs.cook)`, "open"},
		{"a forged directory entry fails the check", directory, `cook.Check(T, "Scene", inputs.cook)`, "bad_layout"},
		{"a base one byte past aligned", valid, `cook.Open(T, "Scene", inputs.cook, 1)`, "unaligned_base"},
		{"a base seven bytes past aligned", valid, `cook.Open(T, "Scene", inputs.cook, 7)`, "unaligned_base"},
		{"a base one alignment past aligned", valid, `cook.Open(T, "Scene", inputs.cook, T.RegionAlign)`, "open"},
		{"no buffer", valid, `cook.Open(T, "Scene", nil)`, "unaligned_base"},
		{"the check refuses an unaligned base too", valid, `cook.Check(T, "Scene", inputs.cook, 3)`, "unaligned_base"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := runCookVerb(t, u, tc.cook, tc.call); got != tc.want {
				t.Errorf("%s: got %q, want %q", tc.call, got, tc.want)
			}
		})
	}
}
