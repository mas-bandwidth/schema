package compiler

import "testing"

func init() { nonGoCarriers["elixir"] = append(nonGoCarriers["elixir"], carryForms) }
func TestElixirCarriesForms(t *testing.T) {
	u := unitFromSource(t, "package probe\ntable Root | forms=\"wire\" { n int32 }\n")
	if !carries("elixir", carryForms) {
		t.Fatal("forms not registered")
	}
	if _, err := New().Generate(u, "elixir", nil); err != nil {
		t.Fatal(err)
	}
}
