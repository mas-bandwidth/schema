package squirreltable_test

// M5 and I10 of docs/PORTING.md, the Squirrel cells.
//
// M5: an enum-keyed array [E]T is a Squirrel array indexed by SLOT, key k at
// slot k-1 (docs/SPEC-TABLES.md §2.4), and the language's own bound is the
// refusal at both ends: the slot of None (-1) and the slot past the last
// variant (len) both throw "the index does not exist", on read and on write,
// and every key in between round-trips through the wire.
//
// I10: a unit that declares no table emits no table module at all.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func probeUnit(t *testing.T, src string) *ir.Unit {
	t.Helper()
	ast, perrs := parser.Parse("Probe.schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs)
	}
	u, cerrs := check.Unit([]check.SourceFile{{Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(src), AST: ast}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs)
	}
	return u
}

func TestSquirrelKeyedArraysAreHeldAtBothEnds(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Squirrel interpreter")
	u := probeUnit(t, `package keyedprobe

enum Team { Red, Blue, Green }

table Root {
	scores [Team]int32
}
`)
	files, err := squirrel.Generate(u)
	if err != nil {
		t.Fatalf("the type surface refused: %v", err)
	}
	tables, err := squirreltable.Generate(u)
	if err != nil {
		t.Fatalf("the table surface refused: %v", err)
	}
	dir := t.TempDir()
	unit := filepath.Join(dir, "keyedprobe")
	if err := os.MkdirAll(unit, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		writeFile(t, filepath.Join(unit, name), data)
	}
	for name, data := range tables {
		writeFile(t, filepath.Join(unit, name), data)
	}
	main := `local T = dofile("keyedprobe/schema_module.nut", true).require("` + squirreltable.ModuleName(u) + `");
local v = T.Root();
print("slots " + v.Scores.len() + "\n");
for (local key = 1; key <= 3; key++) v.Scores[key - 1] = key * 10;
local back = T.RootLoad(T.RootSave(v), {});
for (local key = 1; key <= 3; key++) print("key " + key + " " + back.Scores[key - 1] + "\n");
foreach (slot in [-1, 3]) {
  try { local x = v.Scores[slot]; print("read " + slot + " answered\n"); } catch (e) print("read " + slot + " refused\n");
  try { v.Scores[slot] = 1; print("write " + slot + " answered\n"); } catch (e) print("write " + slot + " refused\n");
}
`
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(main))
	got := string(sqrun.Run(t, dir, "main.nut", nil))
	want := "slots 3\nkey 1 10\nkey 2 20\nkey 3 30\nread -1 refused\nwrite -1 refused\nread 3 refused\nwrite 3 refused\n"
	if got != want {
		t.Errorf("keyed probe:\n got %q\nwant %q", got, want)
	}
}

func TestSquirrelTableFreeUnitEmitsNothing(t *testing.T) {
	t.Parallel()
	u := probeUnit(t, `package tablefree

enum Mode { A, B }

type Pos {
	x int32
	mode Mode
}
`)
	out, err := squirreltable.Generate(u)
	if err != nil {
		t.Fatalf("the table surface refused a table-free unit: %v", err)
	}
	if len(out) != 0 {
		var names []string
		for name := range out {
			names = append(names, name)
		}
		t.Fatalf("a table-free unit emitted %d table files: %s", len(out), strings.Join(names, " "))
	}
	// the control: one table in the same unit emits the module and its runtime
	withTable := probeUnit(t, "package tablefree\n\ntable Holder {\n\tn int32\n}\n")
	if out, err := squirreltable.Generate(withTable); err != nil || len(out) == 0 {
		t.Fatalf("a unit with a table emitted nothing (err %v)", err)
	}
}
