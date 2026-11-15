// TestPortLuaPageMatchesTheTree pins docs/PORT-LUA.md to the tree: a bare
// float on the type wire is finite only (SPEC.md §4.3, packet-floats-finite),
// the read is one strict load (strict-only-read), the type-wire runtime is the
// vendored serialize*.lua set, and a message payload is read at the sender's
// announced width and base (SPEC-TABLES.md §3.3). The retired sentences are
// the stales: the bit-for-bit NaN claim, `LoadTolerant`, the private
// bitreader/bitwriter, and the receiver-shape payload read.
package compiler

import (
	"os"
	"strings"
	"testing"
)

func TestPortLuaPageMatchesTheTree(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../docs/PORT-LUA.md")
	if err != nil {
		t.Fatal(err)
	}
	page := strings.Join(strings.Fields(string(data)), " ")

	wants := []string{
		"A BARE FLOAT IS FINITE ON THE TYPE WIRE",
		"SPEC.md §4.3",
		"refuses a NaN on write and refuses one on read as `non_finite_float`",
		"never a silent coercion",
		"serialize*.lua",
		"reads a ranged payload at the sender's announced width and base",
	}
	stales := []string{
		"must ride bit for bit",
		"LoadTolerant",
		"bitreader.lua",
		"bitwriter.lua",
		"Reading payloads still uses the receiver's declared shape",
	}
	for _, want := range wants {
		if !strings.Contains(page, want) {
			t.Errorf("docs/PORT-LUA.md no longer states %q", want)
		}
	}
	for _, stale := range stales {
		if strings.Contains(page, stale) {
			t.Errorf("docs/PORT-LUA.md still states the stale %q", stale)
		}
	}
}
