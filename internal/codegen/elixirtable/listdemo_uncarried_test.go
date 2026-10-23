package elixirtable_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestListdemoNamesUncarriedRoots pins docs/SPEC-TABLES.md §2.6, §2.8 and §11:
// tables/lists generates through the elixir target, and the roots that reach
// a map or a union are named in @uncarried — the set the cook write and the
// wire-fuzz roster do not yet walk; the id-table wire and the text form carry
// them (TestMapsAndUnionArmsOnTheTableWireElixir). A map-only unit, refused by
// name before the elixir target registered as a map carrier, now generates.
// No Elixir: this test never leaves the generator.
//
// The test is an external package. elixirtable's own tests cannot import
// compiler: target_elixir.go imports elixirtable, and that cycle is refused.
func TestListdemoNamesUncarriedRoots(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "tables", "lists")
	unit := loadListdemo(t,
		filepath.Join(dir, "Holders.schema"),
		filepath.Join(dir, "Migrate.schema"),
		filepath.Join(dir, "Report.schema"),
		filepath.Join(dir, "Save.schema"),
		filepath.Join(dir, "Shared.schema"),
	)
	files, err := compiler.New().Generate(unit, "elixir", compiler.Options{})
	if err != nil {
		t.Fatalf("listdemo generate: %v", err)
	}
	src, ok := files["Table.ex"]
	if !ok {
		t.Fatal("listdemo generated no Table.ex")
	}
	text := string(src)
	start := strings.Index(text, "@uncarried [")
	end := strings.Index(text, "def carried?")
	if start < 0 || end < start {
		t.Fatalf("Table.ex has no @uncarried block before carried?/1")
	}
	block := text[start:end]
	want := []string{"Army", "Mixed", "Squad", "SquadRosterEntry"}
	if got := quotedNames(block); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("uncarried = %v, want %v", got, want)
	}
	for _, carried := range []string{"Save", "Album", "Sheet", "Unbounded", "Bytes", "Ints", "Floats", "Deck", "Item", "Point", "Row"} {
		if strings.Contains(block, `"`+carried+`"`) {
			t.Errorf("%s is carried and was named uncarried", carried)
		}
	}
	if !strings.Contains(text, "def carried?(table), do: table not in @uncarried") {
		t.Error("Table.ex does not export carried?/1")
	}

	only := listdemoFrom(t, `package p

fixed table Item
{
    count int32
}

table Fleet
{
    ships map[uint8]Item
}
`)
	files, err = compiler.New().Generate(only, "elixir", compiler.Options{})
	if err != nil {
		t.Fatalf("a map-only unit generates now that elixir carries maps (docs/SPEC-TABLES.md §2.8), got %v", err)
	}
	if _, ok := files["Table.ex"]; !ok {
		t.Fatal("a map-only unit generated no Table.ex")
	}
}

func quotedNames(block string) []string {
	var names []string
	rest := block
	for {
		i := strings.Index(rest, `"`)
		if i < 0 {
			return names
		}
		rest = rest[i+1:]
		j := strings.Index(rest, `"`)
		if j < 0 {
			return names
		}
		names = append(names, rest[:j])
		rest = rest[j+1:]
	}
}

func listdemoFrom(t *testing.T, src string) *ir.Unit {
	t.Helper()
	ast, errs := parser.Parse("Probe.schema", []byte(src))
	if len(errs) != 0 {
		t.Fatalf("parse: %v", errs)
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(src), AST: ast,
	}})
	if len(cerrs) != 0 {
		t.Fatalf("check: %v", cerrs)
	}
	return u
}

func loadListdemo(t *testing.T, paths ...string) *ir.Unit {
	t.Helper()
	var files []check.SourceFile
	for _, p := range paths {
		src, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		base := strings.TrimSuffix(filepath.Base(p), ".schema")
		ast, errs := parser.Parse(filepath.Base(p), src)
		if len(errs) != 0 {
			t.Fatalf("parse %s: %v", p, errs)
		}
		files = append(files, check.SourceFile{Path: p, Name: filepath.Base(p), Base: base, Bytes: src, AST: ast})
	}
	u, errs := check.Unit(files)
	if len(errs) != 0 {
		t.Fatalf("check: %v", errs)
	}
	return u
}
