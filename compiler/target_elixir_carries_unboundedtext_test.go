package compiler

import "testing"

func init() { nonGoCarriers["elixir"] = append(nonGoCarriers["elixir"], carryUnboundedText) }

func TestElixirCarriesUnboundedText(t *testing.T) {
	u := unitFromSource(t, `package probe
 table R { plain [..3]string
 maybe []?string
 names map[string]int32 }
 `)
	if !carries("elixir", carryUnboundedText) {
		t.Fatal("unbounded text not registered")
	}
	if _, err := New().Generate(u, "elixir", nil); err != nil {
		t.Fatal(err)
	}
}
