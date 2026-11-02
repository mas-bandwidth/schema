package compiler

import "testing"

func init() { nonGoCarriers["elixir"] = append(nonGoCarriers["elixir"], carryRequired) }

func TestElixirCarriesRequired(t *testing.T) {
	u := unitFromSource(t, `package probe
 table R { value int32 | required
 values []int32 | required }
 `)
	if !carries("elixir", carryRequired) {
		t.Fatal("required not registered")
	}
	if _, err := New().Generate(u, "elixir", nil); err != nil {
		t.Fatal(err)
	}
}
