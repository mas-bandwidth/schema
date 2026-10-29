// The Squirrel table runtime's HOME is the package (docs/PORTING.md J2,
// #347): a unit's table module is <Package>Tables.nut and its shared runtime
// is the schema_table_*.nut modules beside it, never a file that happens to
// sort first. A schema file added ahead of every other one must move nothing:
// the same module name, and the runtime modules byte for byte. The control puts
// the file-order rule back and requires the module to move, so the gate is
// known to watch something.
package compiler

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// squirrelHomeTrees generates tables/examples twice: as it stands, and with
// Aaa.schema, a file that sorts ahead of Guarded.schema, added to a copy.
func squirrelHomeTrees(t *testing.T) (base, added map[string][]byte, baseUnit, addedUnit *ir.Unit) {
	t.Helper()
	src := t.TempDir()
	matches, err := filepath.Glob("../tables/examples/*.schema")
	if err != nil || len(matches) == 0 {
		t.Fatalf("no schema files in tables/examples: %v", err)
	}
	for _, m := range matches {
		data, err := os.ReadFile(m)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(src, filepath.Base(m)), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(src, "Aaa.schema"), []byte("package tabledemo\n\ntable AaaRow\n{\n    tag uint8\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gen := func(dir string) (map[string][]byte, *ir.Unit) {
		paths, err := GatherPaths([]string{dir})
		if err != nil {
			t.Fatal(err)
		}
		c := New()
		c.OnWarn = func(string) {}
		u, err := c.Load(paths)
		if err != nil {
			t.Fatalf("%s: %v", dir, err)
		}
		files, err := c.Generate(u, "squirrel", Options{})
		if err != nil {
			t.Fatalf("%s: %v", dir, err)
		}
		return files, u
	}
	base, baseUnit = gen("../tables/examples")
	added, addedUnit = gen(src)
	return base, added, baseUnit, addedUnit
}

func squirrelRuntimeModules(files map[string][]byte) []string {
	var names []string
	for name := range files {
		if strings.HasPrefix(name, "schema_") {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func TestSquirrelTableRuntimeHomeIsThePackage(t *testing.T) {
	base, added, baseUnit, addedUnit := squirrelHomeTrees(t)
	home := squirreltable.ModuleName(baseUnit) + ".nut"
	if home != "TabledemoTables.nut" {
		t.Fatalf("the base tree's table module is %s, want TabledemoTables.nut", home)
	}
	if squirreltable.ModuleName(addedUnit)+".nut" != home {
		t.Fatalf("the table module moved from %s to %s when the unit gained a file", home, squirreltable.ModuleName(addedUnit)+".nut")
	}
	for _, tree := range []map[string][]byte{base, added} {
		if _, ok := tree[home]; !ok {
			t.Fatalf("a tree has no %s", home)
		}
	}
	a, b := squirrelRuntimeModules(base), squirrelRuntimeModules(added)
	if len(a) == 0 {
		t.Fatal("the base tree has no schema_table runtime modules — the gate is comparing two nothings")
	}
	if !slices.Equal(a, b) {
		t.Fatalf("the runtime modules moved when the unit gained a file: %v vs %v", a, b)
	}
	for _, name := range a {
		if !bytes.Equal(base[name], added[name]) {
			t.Errorf("%s changed when the unit gained a file — a runtime module carries nothing of the unit", name)
		}
	}
}

// TestSquirrelRuntimeHomeNegativeControl puts the file-order rule back, the
// module named by the first file of the unit, and requires the home to move
// across the two trees. The rule is the one the J2 gate was written against:
// ir.ProtocolIdHome(u), the file that sorts first.
func TestSquirrelRuntimeHomeNegativeControl(t *testing.T) {
	_, _, baseUnit, addedUnit := squirrelHomeTrees(t)
	fileOrder := func(u *ir.Unit) string { return ir.ProtocolIdHome(u) + "Tables.nut" }
	if fileOrder(baseUnit) == fileOrder(addedUnit) {
		t.Fatalf("the file-order rule kept the home in %s with Aaa.schema added — the gate is watching nothing", fileOrder(baseUnit))
	}
}
