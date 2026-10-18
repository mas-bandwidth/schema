package gotable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// runtimeHomeSchema declares one table, so the unit has a shared runtime to
// place.
const runtimeHomeSchema = `package probe

table Config
{
    value uint32
}
`

// runtimeHomeExtra is a file that declares NOTHING, so the only thing it
// changes is which basename sorts first. Adding a `type` would move the
// protocol id and the build version, and then a moved runtime would be correct
// rather than a defect.
const runtimeHomeExtra = `package probe

// a file that declares nothing, so the only thing it changes is file order
`

// runtimeHomeUnit is the schema alone, or the schema behind a file that sorts
// earlier.
func runtimeHomeUnit(t *testing.T, earlier bool) *ir.Unit {
	t.Helper()
	var files []check.SourceFile
	if earlier {
		files = append(files, runtimeHomeFile(t, "Alpha", runtimeHomeExtra))
	}
	files = append(files, runtimeHomeFile(t, "Probe", runtimeHomeSchema))
	u, errs := check.Unit(files)
	if len(errs) > 0 {
		t.Fatalf("check: %v", errs[0])
	}
	return u
}

func runtimeHomeFile(t *testing.T, base, src string) check.SourceFile {
	t.Helper()
	ast, perrs := parser.Parse(base+".schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse %s: %v", base, perrs[0])
	}
	return check.SourceFile{
		Path: base + ".schema", Name: base + ".schema", Base: base,
		Bytes: []byte(src), AST: ast,
	}
}

// runtimeHomeOf answers which emitted file carries the unit's shared table
// runtime, and refuses a unit that put it in two places.
func runtimeHomeOf(t *testing.T, out map[string][]byte) string {
	t.Helper()
	home := ""
	for name, data := range out {
		if !strings.HasSuffix(name, "Table.go") {
			continue
		}
		if !strings.Contains(string(data), "type TableTypeInfo struct") {
			continue
		}
		if home != "" {
			t.Fatalf("the shared runtime is in two files, %s and %s", home, name)
		}
		home = name
	}
	if home == "" {
		t.Fatal("no file carries the unit's shared table runtime")
	}
	return home
}

// TestSharedRuntimeHomeIsPackageNamed is the Go half of the J2 technique
// (docs/PORTING.md, schema#422): a unit's shared runtime lives in one file
// named by the PACKAGE, never by the file that happens to sort first, so a
// schema file that sorts earlier relocates nothing. The home is
// `ProbeTable.go` before and after an earlier-sorting file joins the unit.
func TestSharedRuntimeHomeIsPackageNamed(t *testing.T) {
	base, err := Generate(runtimeHomeUnit(t, false))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	added, err := Generate(runtimeHomeUnit(t, true))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if got := runtimeHomeOf(t, base); got != "ProbeTable.go" {
		t.Errorf("the shared runtime is in %s; a unit's runtime is named by the PACKAGE, so it is ProbeTable.go", got)
	}
	if got := runtimeHomeOf(t, added); got != "ProbeTable.go" {
		t.Errorf("an earlier-sorting file moved the shared runtime to %s; it is ProbeTable.go (docs/SPEC-TABLES.md §19.2)", got)
	}
}
