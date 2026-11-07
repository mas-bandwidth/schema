package lobstertable_test

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"os"
	"path/filepath"
	"testing"
)

// Extent-bearing holders must preserve the file header and write map entries,
// nested extents and wide elements at the reference's exact addresses.
func TestLobsterCookExtentCoordinates(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "native Lobster extent bytes, map ordering and malformed spans")
	selected := map[string]bool{"map_cells": true, "map_chunks": true, "map_full": true, "map_slots": true, "map_text": true, "map_runs": true, "list_scalars": true, "list_nested": true, "list_of_maps": true, "widelist_wide": true}
	found := 0
	for _, row := range cookWriteRows(t) {
		if !selected[row.name] {
			continue
		}
		found++
		t.Run(row.name, func(t *testing.T) {
			g := generateCookUnit(t, row)
			le, err := os.ReadFile(filepath.Join(root, row.little))
			if err != nil {
				t.Fatal(err)
			}
			be, err := os.ReadFile(filepath.Join(root, row.big))
			if err != nil {
				t.Fatal(err)
			}
			bad := bytes.Clone(le)
			bad[0] ^= 0xff
			out := runCookProbe(t, g, row, bad)
			if out.measure != len(le) || !bytes.Equal(out.little, le) || !bytes.Equal(out.big, be) {
				t.Errorf("extent bytes: measured=%d want=%d; little=%s; big=%s", out.measure, len(le), divergence(string(out.little), string(le)), divergence(string(out.big), string(be)))
			}
			if out.open != [3]string{"ok", "no", "no"} {
				t.Errorf("extent open: got %v", out.open)
			}
			wire, err := os.ReadFile(filepath.Join(root, row.wire))
			if err != nil {
				t.Fatal(err)
			}
			want, err := goSaved(g.u, row, wire)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(out.saved, want) {
				t.Errorf("extent read lost fields: %s", divergence(string(out.saved), string(want)))
			}
			extentReadProbe(t, g, row, wire, le, be, want)
		})
	}
	if found != len(selected) {
		t.Fatalf("selected %d extent witnesses, want %d", found, len(selected))
	}
}

// Read both native byte orders, then reverse caller maps and require unchanged
// cook bytes and caller order. Forge only extent slots of otherwise valid cooks.
func extentReadProbe(t *testing.T, g *cookUnit, row cookWriteRow, wire, le, be, want []byte) {
	t.Helper()
	var src strings.Builder
	for _, mod := range g.mods {
		fmt.Fprintf(&src, "import .%s\n", mod)
	}
	src.WriteString("import .load\nimport .schema_save\n")
	src.WriteString(mapSortHelpers)
	fmt.Fprintf(&src, `
let wire = read_file("wire")
let le = read_file("le")
let be = read_file("be")
let want = read_file("want")
assert wire and le and be and want
let rec = schema_rt.load(%q, wire, schema_rt.LoadReport {})
assert rec
let backle = %s.%sReadCook(le)
let backbe = %s.%sReadCook(be)
assert backle and backbe
assert schema_rt.save(backle) == want
assert schema_rt.save(backbe) == want
let reversed = reverse_maps(rec)
let ct = schema_rt.cook_table_of(rec.d.name)
assert ct
var checked = 0
for(rec.fields) sl, i:
    if sl.fd.s == "map":
        let before = copy(sl.entries)
        assert %s.%sCookMeasure(rec) == length(le)
        assert %s.%sCook(rec, false) == le
        assert %s.%sCook(rec, true) == be
        for(before) entry, j:
            assert sl.entries[j] == entry
    if sl.fd.s == "map" or (sl.fd.s == "arr" and sl.fd.array == "list"):
        let at = 64 + ct.fields[i].off
        let positive = write_int32_le(copy(le), at + 8, 1)
        let high = write_int64_le(copy(positive), at, 0x7fffffffffffffff)
        assert not %s.%sReadCook(high)
        let low = write_int64_le(copy(positive), at, 0x8000000000000000)
        assert not %s.%sReadCook(low)
        let count = write_int32_le(copy(le), at + 8, 0x7fffffff)
        assert not %s.%sReadCook(count)
        let cycle = write_int64_le(copy(positive), at, 0)
        assert not %s.%sReadCook(cycle)
        checked++
print("extent read, reversed maps and malformed spans passed")
`, row.root, g.pkg, row.root, g.pkg, row.root, g.pkg, row.root, g.pkg, row.root,
		g.pkg, row.root, g.pkg, row.root, g.pkg, row.root, g.pkg, row.root, g.pkg, row.root)
	if strings.HasPrefix(row.name, "map_") {
		src.WriteString("assert reversed > 0 and checked > 0\n")
	}
	if err := os.WriteFile(filepath.Join(g.dir, "extent.lobster"), []byte(src.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	out := lobsterrun.Run(t, g.dir, "extent.lobster", map[string][]byte{"wire": wire, "le": le, "be": be, "want": want})
	if !strings.Contains(string(out), "extent read, reversed maps and malformed spans passed") {
		t.Fatalf("extent probe did not finish: %s", out)
	}
}
