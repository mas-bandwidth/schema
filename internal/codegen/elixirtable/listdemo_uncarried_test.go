package elixirtable

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
)

// TestListdemoNamesUncarriedRoots pins docs/SPEC-TABLES.md §2.6, §2.8 and §11:
// tables/lists generates, because it carries unbounded arrays, and the roots
// that reach a map or a union are named uncarried rather than refusing the
// unit. A map-only unit, which declares no unbounded array, stays refused by
// name. No Elixir: this test never leaves the generator.
func TestListdemoNamesUncarriedRoots(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "tables", "lists")
	unit := loadUnit(t,
		filepath.Join(dir, "Holders.schema"),
		filepath.Join(dir, "Migrate.schema"),
		filepath.Join(dir, "Report.schema"),
		filepath.Join(dir, "Save.schema"),
		filepath.Join(dir, "Shared.schema"),
	)
	got := uncarriedRootNames(unit)
	want := []string{"Army", "Mixed", "Squad", "SquadRosterEntry"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("uncarried = %v, want %v", got, want)
	}
	for _, carried := range []string{"Save", "Album", "Sheet", "Unbounded", "Bytes", "Ints", "Floats", "Deck", "Item", "Point", "Row"} {
		for _, name := range got {
			if name == carried {
				t.Errorf("%s is carried and was named uncarried", carried)
			}
		}
	}

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
	for _, name := range want {
		if !strings.Contains(block, `"`+name+`"`) {
			t.Errorf("@uncarried does not name %s", name)
		}
	}
	if strings.Contains(block, `"Save"`) {
		t.Error("@uncarried names Save")
	}
	if !strings.Contains(text, "def carried?(table), do: table not in @uncarried") {
		t.Error("Table.ex does not export carried?/1")
	}

	only := unitFrom(t, `package p

fixed table Item
{
    count int32
}

table Fleet
{
    ships map[uint8]Item
}
`)
	_, err = compiler.New().Generate(only, "elixir", compiler.Options{})
	if err == nil || !strings.Contains(err.Error(), "map") {
		t.Fatalf("a map-only unit must still be refused by name, got %v", err)
	}
}
