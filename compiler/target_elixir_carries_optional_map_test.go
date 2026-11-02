package compiler

import "testing"

func init() { nonGoCarriers["elixir"] = append(nonGoCarriers["elixir"], carryOptionalMap) }

func TestElixirCarriesOptionalMap(t *testing.T) {
	u := unitFromSource(t, `package probe
 table Leaf { value int32 }
 table Root { tags ?map[string(8)]int32
 children ?map[uint32]*Leaf }
 `)
	if !carries("elixir", carryOptionalMap) {
		t.Fatal("optional-map not registered")
	}
	if _, err := New().Generate(u, "elixir", nil); err != nil {
		t.Fatal(err)
	}
}
