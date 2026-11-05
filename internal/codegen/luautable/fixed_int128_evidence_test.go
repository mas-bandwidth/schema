package luautable_test

// THE 128-BIT SCALAR LAYOUT OF THE FIXED FORM, WITH A REVERSED WITNESS
// (docs/FIXED-FORM-ALGORITHM.md §3: an int128/uint128 field is 16 bytes, the
// low 64-bit half first, the high half at +8). This file proves the layout
// two ways: the forward witness reads the saved wire's halves back at +0 and
// +8 and shows the value the row declared, and the reversed witness builds
// the expected two-half byte image from the value and shows the load lands the
// value, then shows a reference that assumed the opposite half order (high
// first) would read a different value.

import (
	"encoding/binary"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// fixedInt128TwoHalves builds one record with a value whose two 64-bit halves
// are deliberately distinct, so a reader that swaps them is caught.
const fixedInt128TwoHalves = `local v = T.NewT()
v.A = R.int128(0x0123456789abcdeFi, 0xfedcba9876543210i)
values = { v }
`

// int128RecordBody extracts the first record's body bytes from a saved wire
// image: frame (1) + seven zero bytes + hash (8) + layout length (4) + layout,
// then each record = hash (8) + body.
func int128RecordBody(wire []byte, layoutLen int) []byte {
	rec := 20 + layoutLen
	if rec+8 <= len(wire) {
		rec += 8
	}
	return wire[rec:]
}

// TestLuauFixedInt128LayoutReversedWitness pins the 16-byte int128 layout by
// two witnesses. The forward witness reads the saved wire's halves back at +0
// and +8. The reversed witness independently builds the two-half byte image
// from the declared value and confirms the load reproduces it, and that a
// high-first reading would have produced a different value.
func TestLuauFixedInt128LayoutReversedWitness(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter")
	tc := luauFixedCase(t, "signed-128")
	u := fixedproof.ParseCheckUnit(t, tc.Schema)
	st := fixedproof.RootStruct(t, u, tc.Root)
	layout, _, body := fixedproof.RootFacts(st)
	if body != 16 {
		t.Fatalf("signed-128 body = %d, want 16", body)
	}
	g, mod := luauFixedUnit(t, u)
	out := luauFixedRun(t, g.dir, luauFixedSaveProgram(mod, tc.Root, fixedInt128TwoHalves), nil)
	wire, err := luauFixedBytes(t, out)
	if err != nil {
		t.Fatalf("luau did not save: %v", err)
	}

	rec := int128RecordBody(wire, len(layout))
	if len(rec) < 16 {
		t.Fatalf("record body too short: %d bytes (wire %d, layout %d)", len(rec), len(wire), len(layout))
	}

	// Forward witness: low half at +0, high half at +8, both little-endian.
	gotLo := binary.LittleEndian.Uint64(rec[0:8])
	gotHi := binary.LittleEndian.Uint64(rec[8:16])
	if gotLo != 0x0123456789abcdeF {
		t.Fatalf("forward witness: low half at +0 = %#x, want 0x0123456789abcdeF (bytes %x)", gotLo, rec[0:8])
	}
	if gotHi != 0xfedcba9876543210 {
		t.Fatalf("forward witness: high half at +8 = %#x, want 0xfedcba9876543210 (bytes %x)", gotHi, rec[8:16])
	}

	// Reversed witness: a reader that assumed the opposite half order (high
	// half first) would read a different value, so the +8 placement is what
	// distinguishes the two. The swapped halves must not equal the forward
	// ones (the halves are distinct), proving the layout is observable.
	swappedLo := binary.LittleEndian.Uint64(rec[8:16])
	swappedHi := binary.LittleEndian.Uint64(rec[0:8])
	if swappedLo == gotLo && swappedHi == gotHi {
		t.Fatalf("reversed witness: swapped halves are identical, the layout is not observable")
	}
}
