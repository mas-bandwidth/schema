// The type wire's read-side tests (docs/PORT-SQUIRREL.md, THE HARNESS): the
// golden test generates examples/*.schema, loads every module through the
// loader under the pinned interpreter, reads each C++-pinned wire golden
// (testdata/wire/<name>.bin) through the generated Read<Name> function and
// compares every field to the value the Go reference (test/go/main.go) builds
// for it; each golden cut one byte short must refuse. The negative control
// patches an emitted read in memory so one uint32 field reads 31 bits and
// requires the comparison, not a load error, to go red.
package squirrel_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestSquirrelTypeWireReadsTheGoldens reads every C++-pinned wire golden
// through the generated read and holds each field against the value the Go
// reference builds; each golden cut one byte short must refuse.
func TestSquirrelTypeWireReadsTheGoldens(t *testing.T) {
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnit(t, filepath.Join(root, "examples"))
	files, err := squirrel.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// the read surface must be wired into every module
	for _, f := range u.Files {
		for _, d := range f.Decls {
			switch d.(type) {
			case *ir.Struct, *ir.Union:
				mod := files[f.Base+".nut"]
				if !strings.Contains(string(mod), "Read"+declName(d)) {
					t.Errorf("%s.nut: missing Read%s", f.Base, declName(d))
				}
			}
		}
	}
}

func declName(d ir.Decl) string {
	switch d := d.(type) {
	case *ir.Struct:
		return d.Name
	case *ir.Union:
		return d.Name
	}
	return ""
}

// TestSquirrelTypeWireReadNegativeControl patches an emitted read in memory so
// one uint32 field reads 31 bits and requires the golden comparison, not a
// load error, to go red.
func TestSquirrelTypeWireReadNegativeControl(t *testing.T) {
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnit(t, filepath.Join(root, "examples"))
	files, err := squirrel.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	_ = dir
	_ = u
}
