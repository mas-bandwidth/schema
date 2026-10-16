package darttable

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// TestUnitViewDescriptorsDart pins the declared vocabulary, including
// annotations, outside the card's wire closure (SPEC-TABLES §8.1–8.3).
func TestUnitViewDescriptorsDart(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	paths, err := filepath.Glob(filepath.Join(repo, "tables", "examples", "*.schema"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("VIEW_CORPUS examples: %v, %d schema files", err, len(paths))
	}
	u := loadUnit(t, paths...)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	name := runtimeHome(u) + "View.dart"
	view := string(files[name])
	if view == "" {
		t.Fatalf("no %s unit view: the card-only descriptors omit the unit registry", name)
	}
	for _, set := range []struct {
		kind  string
		names []string
	}{
		{"table", sortedKeys(u.Tables)},
		{"enum", sortedKeys(u.Enums)},
		{"flags", sortedKeys(u.Flags)},
		{"union", sortedKeys(u.Unions)},
		{"constant", sortedKeys(u.Consts)},
	} {
		for _, n := range set.names {
			if set.kind == "table" && u.Tables[n].IsMapEntry() {
				continue
			}
			want := fmt.Sprintf("%s %s", set.kind, n)
			if !strings.Contains(view, want) {
				t.Errorf("%s missing from %s", want, name)
			}
		}
	}
	for _, row := range []string{"WeaponConfig", "Grade", "Effect", "MaxLoadoutSlots", "designer_facing", "ui_slider"} {
		if !strings.Contains(view, row) {
			t.Errorf("declared name or tag %q missing from %s", row, name)
		}
	}
	// §16's top-level text and wire entry points are needed even for a table
	// held by another table: the manifest may name RootConfig directly.
	wire := string(files[runtimeHome(u)+"Wire.dart"])
	for _, name := range sortedKeys(u.Tables) {
		st := u.Tables[name]
		if st.IsMapEntry() || !cardWireTable(st) {
			continue
		}
		fn := dartName(name)
		for _, suffix := range []string{"FromJson(", "ToJson(", "WireSave(", "WireLoad("} {
			if !strings.Contains(wire, fn+suffix) {
				t.Errorf("table %s lacks top-level %s%s", name, fn, suffix)
			}
		}
	}
}
