package compiler

import "testing"

func init() { nonGoCarriers["elixir"] = append(nonGoCarriers["elixir"], carryIntegrity) }

func TestElixirCarriesIntegrity(t *testing.T) {
	u := unitFromSource(t, `package probe
 table R | integrity="crc32c" { value int32 }
 fixed table F | integrity="crc32c" { value int32 }
 `)
	if !carries("elixir", carryIntegrity) {
		t.Fatal("integrity not registered")
	}
	if _, err := New().Generate(u, "elixir", nil); err != nil {
		t.Fatal(err)
	}
}
