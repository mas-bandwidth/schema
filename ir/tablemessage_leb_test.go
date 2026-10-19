// THE MESSAGE FORM's LEB READ (docs/SPEC-TABLES.md §3.3): a tenth byte with a
// bit above the 64th value bit is malformed. readLebBytes decodes a peer's
// announcement, which is untrusted, so the tenth byte must be refused before
// it is OR-ed in.
package ir

import (
	"testing"
)

// TestReadLebRefusesAnOverflowingTenthByte pins the tenth-byte rule:
// {9 x 0x80, 0x02} is refused (2 would set bits above the 64th value bit),
// {9 x 0x80, 0x01} is accepted and decodes as 2^63, a non-minimal spelling is
// refused, and a run that never terminates is refused.
func TestReadLebRefusesAnOverflowingTenthByte(t *testing.T) {
	t.Parallel()
	continuations := []byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80}

	refused := []struct {
		name string
		in   []byte
	}{
		{"tenth byte 0x02", append(append([]byte{}, continuations...), 0x02)},
		{"non-minimal", []byte{0x80, 0x00}},
		{"truncated run", continuations},
	}
	for _, tc := range refused {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, _, ok := readLebBytes(tc.in); ok {
				t.Errorf("readLebBytes(%x) accepted a malformed LEB", tc.in)
			}
		})
	}

	t.Run("tenth byte 0x01 is 2^63", func(t *testing.T) {
		t.Parallel()
		in := append(append([]byte{}, continuations...), 0x01)
		v, n, ok := readLebBytes(in)
		if !ok {
			t.Fatalf("readLebBytes(%x) refused a canonical 2^63", in)
		}
		if want := uint64(1) << 63; v != want {
			t.Errorf("readLebBytes(%x) = %d, want %d", in, v, want)
		}
		if n != len(in) {
			t.Errorf("readLebBytes(%x) consumed %d bytes, want %d", in, n, len(in))
		}
	})
}
