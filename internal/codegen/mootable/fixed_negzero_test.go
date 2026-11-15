package mootable_test

// A COMPRESSED FLOAT'S NEGATIVE ZERO READS AS +0 (docs/SPEC-TABLES.md §3.3,
// §3.4; docs/FIXED-FORM-ALGORITHM.md §4.6; docs/rulings.json
// fixed-compressed-negzero). The fixed form carries the float itself, so a
// compressed field's -0 lands as the value +0, refusing nothing and counting
// nothing, and a read-then-save writes the canonical +0 image. A PLAIN float's
// -0 is carried bit-exact.

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// negZeroSchema puts a compressed float at a scalar and a fixed array element,
// beside a plain float whose -0 the rule does not touch.
const negZeroSchema = `package probe

fixed table NegZero
{
    lead  uint32 = 1
    aim   float32 = 0.0 | min = -1.0, max = 1.0, resolution = 0.01
    vals  [2]float32 | min = -1.0, max = 1.0, resolution = 0.01
    plain float32 = 0.0
    trail uint32 = 2
}
`

func TestFixedCompressedNegZeroRead(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted fixed-form runtime)")
	t.Parallel()
	u := unitFromSource(t, negZeroSchema)
	g := fixedUnit(t, u, nil)
	const setup = `local v = T.NewNegZero()
v.Lead = 1
v.Aim = %s
v.Vals[1] = %s
v.Vals[2] = %s
v.Plain = %s
v.Trail = 2
values = { v }
`
	save := func(aim, first, second, plain string) []byte {
		return evoSave(t, g, "ProbeTable", "NegZero", fmt.Sprintf(setup, aim, first, second, plain))
	}
	canonical := save("0x00000000", "0x00000000", "0x00000000", "0x80000000")
	file := save("0x80000000", "0x80000000", "0x80000000", "0x80000000")
	if bytes.Equal(file, canonical) {
		t.Fatal("the writer stored a compressed float's -0 as +0; the vector needs the -0 image")
	}
	got := evoLoad(t, g, "ProbeTable", "NegZero", file, -1)
	if got.head != "ok 1 u0 k0 c0 w0 d0 -" {
		t.Fatalf("the vector does not read: %s", got.head)
	}
	for _, field := range []string{"r0.aim", "r0.vals[0]", "r0.vals[1]"} {
		if got.dump[field] != "0x00000000" {
			t.Errorf("%s landed %s, want +0 (0x00000000)", field, got.dump[field])
		}
	}
	if got.dump["r0.plain"] != "0x80000000" {
		t.Errorf("the plain float's -0 landed %s, want its own bits (0x80000000)", got.dump["r0.plain"])
	}
	if !bytes.Equal(got.saved, canonical) {
		t.Errorf("the read-then-save image is not the canonical +0 image:\n got %x\nwant %x", got.saved, canonical)
	}
}
