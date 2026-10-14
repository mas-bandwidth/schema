package elixirtable_test

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
)

// TestCardsCommandReachesTheTableBackend pins that `bin/schema generate
// --lang elixir tables/cards` now runs to completion. The packet emitter's
// name check no longer refuses the `Card` table for colliding with the module
// it writes for schema file Card.schema (docs/SPEC-TABLES.md §11): a table is
// deliberately absent from the packet wire and lowers to a module in the table
// backend under its own suffix rules, so the packet emitter skips it and the
// command reaches elixirtable, whose variable-form boundary admits the card's
// constructs. The command's success is what step 5's cards-elixir-generate
// target depends on.
func TestCardsCommandReachesTheTableBackend(t *testing.T) {
	t.Parallel()

	c := compiler.New()
	u, err := c.Load([]string{"../../../tables/cards/Card.schema"})
	if err != nil {
		t.Fatalf("load tables/cards: %v", err)
	}
	files, err := c.Generate(u, "elixir", compiler.Options{})
	if err != nil {
		t.Fatalf("tables/cards refused for elixir: %v", err)
	}
	if _, ok := files["BuildVersion.ex"]; !ok {
		names := make([]string, 0, len(files))
		for k := range files {
			names = append(names, k)
		}
		t.Errorf("table backend output missing BuildVersion.ex; generated %v", names)
	}
}
