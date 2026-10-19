package elixirtable_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
)

// TestMapKeyJSONRefusal pins the nightly-fuzz finding that a JSON object key
// that is not the decimal spelling of an integer on an integer-keyed map is
// refused whole, never handed to the writer's String.to_integer to raise
// (binary_to_integer("roster") on mapdemo Depth). The generated Table module
// must carry the integer-key validation on the JSON read boundary; before the
// fix the guard was absent and the source read on, so this test is red at BASE
// and green at the fixed head. It inspects generated source only: no Elixir,
// no clock.
func TestMapKeyJSONRefusal(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	schemaDir := filepath.Join(root, "tables", "maps")
	paths, err := compiler.GatherPaths([]string{schemaDir})
	if err != nil {
		t.Fatalf("mapdemo gather: %v", err)
	}
	c := compiler.New()
	unit, err := c.Load(paths)
	if err != nil {
		t.Fatalf("mapdemo load: %v", err)
	}
	files, err := c.Generate(unit, "elixir", compiler.Options{})
	if err != nil {
		t.Fatalf("mapdemo generate elixir: %v", err)
	}
	src, ok := files["Table.ex"]
	if !ok {
		t.Fatal("mapdemo generated no Table.ex")
	}
	text := string(src)

	if !strings.Contains(text, "defp bad_map_key?(") {
		t.Error("generated Table.ex lacks the integer-key JSON refusal on the map read boundary")
	}
	if strings.Contains(text, "bad_map_key?_") {
		t.Error("generated Table.ex has an unguarded map-key path")
	}
}
