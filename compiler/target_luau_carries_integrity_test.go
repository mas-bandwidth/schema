// The Luau target registers `integrity` for itself (docs/SPEC-TABLES.md
// §2.14): a variable and a fixed table that declare the trailer generate for
// Luau, whose save appends the CRC-32C and whose load checks and strips it.
package compiler

import "testing"

func init() { nonGoCarriers["luau"] = append(nonGoCarriers["luau"], carryIntegrity) }

func TestLuauCarriesIntegrity(t *testing.T) {
	t.Parallel()
	if !carries("luau", carryIntegrity) {
		t.Fatal("luau does not carry integrity")
	}
	u := unitFromSource(t, `package probe
 table R | integrity="crc32c" { value int32 }
 fixed table F | integrity="crc32c" { value int32 }
 `)
	files, err := New().Generate(u, "luau", Options{})
	if err != nil || len(files) == 0 {
		t.Fatalf("luau must carry integrity: files=%d err=%v", len(files), err)
	}
}
