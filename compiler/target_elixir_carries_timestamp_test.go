package compiler

import "testing"

func init() { nonGoCarriers["elixir"] = append(nonGoCarriers["elixir"], carryTimestamp) }
func TestElixirCarriesTimestamp(t *testing.T) {
	u := unitFromSource(t, "package probe\ntable Clock { at timestamp }\nfixed table Tick { at timestamp }\n")
	if !carries("elixir", carryTimestamp) {
		t.Fatal("timestamp not registered")
	}
	if _, err := New().Generate(u, "elixir", nil); err != nil {
		t.Fatal(err)
	}
}
