package lockfile_test

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/lockfile"
)

// A UNION TAG'S WIDTH IS EXACTLY 1, 2, 4 OR 8 BYTES, NEVER "1..8".
//
// THE LAW: docs/FIXED-FORM-ALGORITHM.md, THE GUARD'S WIDTH — a tag is an
// ordinal held at an ordinal width (§1's union row: 8, 16, 32 or 64 bits), so
// the widths a lock can record are exactly 1, 2, 4 and 8, and a lineage entry whose
// union lays any other width (`size - widest arm`: 0, 3, 5, 6, 7, 9 or more) is a
// LOCK-VALIDATION refusal, `layout_malformed` on its lane; nothing clamps it.
// The page said `1..8` until 2026-11-25, which reads as admitting a 3-byte tag;
// the R14 card found Lua, Luau and Goose taking 3, 5, 6, 7 and 9.
//
// ValidateLayout is the reference check every generate-time leg runs over a
// lock's layouts (Python, Ruby, Unity and the lock itself), so it is held here
// for every width 0 through 16 over one union of one u32 arm: the four ordinal
// widths pass, every other width refuses layout_size_mismatch by name (the
// reason a lane reports as layout_malformed).
//
// THE PLANT: widen layoutOrdinalWidth to `n >= 1 && n <= 8` and widths 3, 5, 6
// and 7 pass: red by name.
func TestValidateLayoutUnionTagWidthIsExactlyAnOrdinalWidth(t *testing.T) {
	t.Parallel()
	for w := uint32(0); w <= 16; w++ {
		layout := makeLayoutBytes(
			makeLayoutEntry(1, 13, w+4, 1),
			makeLayoutEntry(7, 15, w+4, 1),
			makeLayoutEntry(9, 8, 4, 0),
		)
		got := lockfile.ValidateLayout(layout)
		switch w {
		case 1, 2, 4, 8:
			if got != "" {
				t.Errorf("a union tag of %d bytes is an ordinal width and validates, got %q", w, got)
			}
		default:
			if got != "layout_size_mismatch" {
				t.Errorf("a union tag of %d bytes is no ordinal width (exactly 1, 2, 4 or 8): want layout_size_mismatch, got %q", w, got)
			}
		}
	}
}
