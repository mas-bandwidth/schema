package compiler

import "testing"

func init() { nonGoCarriers["elixir"] = append(nonGoCarriers["elixir"], carryTextListTotal) }

func TestElixirCarriesTextListTotal(t *testing.T) {
	u := unitFromSource(t, `package probe
 table R { plain [..3]string(5) | total=5
 maybe []?string(5) | total=5 }
 `)
	if !carries("elixir", carryTextListTotal) {
		t.Fatal("text list totals not registered")
	}
	if _, err := New().Generate(u, "elixir", nil); err != nil {
		t.Fatal(err)
	}
}
