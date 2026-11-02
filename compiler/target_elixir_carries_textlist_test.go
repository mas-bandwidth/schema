package compiler

import "testing"

func init() { nonGoCarriers["elixir"] = append(nonGoCarriers["elixir"], carryTextList) }

func TestElixirCarriesTextList(t *testing.T) {
	u := unitFromSource(t, `package probe
 table R { plain [..3]string(5)
 maybe []?string(5) }
 `)
	if !carries("elixir", carryTextList) {
		t.Fatal("text lists not registered")
	}
	if _, err := New().Generate(u, "elixir", nil); err != nil {
		t.Fatal(err)
	}
}
