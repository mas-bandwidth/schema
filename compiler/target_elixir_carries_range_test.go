package compiler

import "testing"

func init() { nonGoCarriers["elixir"] = append(nonGoCarriers["elixir"], carryOneSidedRange) }
func TestElixirCarriesOneSidedRange(t *testing.T) {
	for _, form := range []string{"table", "fixed table"} {
		u := unitFromSource(t, "package probe\n"+form+" Root {\n value int64 | min = 0\n upper uint16 | max = 9\n}\n")
		if !carries("elixir", carryOneSidedRange) {
			t.Fatal("one-sided-range not registered")
		}
		if _, err := New().Generate(u, "elixir", nil); err != nil {
			t.Fatal(err)
		}
	}
}
