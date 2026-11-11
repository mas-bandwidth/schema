// SPEC §4.6: a packet counted array whose element encodes in zero bits is
// refused, so a count read from the wire can never be almost the whole cost
// of the field on the wire while the reader still materializes the declared
// bound in memory. A fixed [N]T is harmless: its extent is a compile-time
// constant, so no untrusted count chooses it.
package check

import (
	"strings"
	"testing"
)

func TestCheckRefusesZeroBitPacketArrayElement(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
	}{
		{
			name: "an empty type element",
			src: `package t
type Empty
{
}
type Packet
{
    items [..4]Empty
}
`,
		},
		{
			name: "a degenerate integer range element",
			src: `package t
type Packet
{
    items [..4]int32 | min = 0, max = 0
}
`,
		},
		{
			name: "a type of zero-bit fields",
			src: `package t
enum Nothing
{
}
type Zero
{
    n Nothing
    x int16 | min = 0, max = 0
}
type Packet
{
    items [..4]Zero
}
`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := refuse(t, tc.src)
			if !strings.Contains(got, "SPEC §4.6") {
				t.Fatalf("want the zero-bit array refusal citing SPEC §4.6, got %q", got)
			}
		})
	}

	// The other edge: an element with one wire bit is a real cost, and a
	// fixed array's extent is declared rather than counted.
	for _, tc := range []struct {
		name string
		src  string
	}{
		{
			name: "a one-bit ranged integer element",
			src: `package t
type Packet
{
    items [..4]int32 | min = 0, max = 1
}
`,
		},
		{
			name: "a one-bit bool element",
			src: `package t
type One
{
    b bool
}
type Packet
{
    items [..4]One
}
`,
		},
		{
			name: "a fixed array of a zero-bit element",
			src: `package t
type Empty
{
}
type Packet
{
    items [4]Empty
}
`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			buildUnit(t, tc.src)
		})
	}
}
