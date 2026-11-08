package compiler

import "testing"

func init() { nonGoCarriers["elixir"] = append(nonGoCarriers["elixir"], carryArrayRoot) }

func TestElixirCarriesArrayRoot(t *testing.T) {
	u := unitFromSource(t, `package probe
 table Root | json="array" { values []int32 }
 `)
	if !carries("elixir", carryArrayRoot) {
		t.Fatal("array-root not registered")
	}
	if _, err := New().Generate(u, "elixir", nil); err != nil {
		t.Fatal(err)
	}
}
