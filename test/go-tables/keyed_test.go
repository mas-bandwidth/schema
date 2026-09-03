// THE ENUM-KEYED ARRAY's indexing guard (docs/SPEC-TABLES.md §2.4).
//
// The storage holds one slot per NAMED variant, the key k at index k-1, and
// nothing for None. So TWO keys are program errors and not values: 0, which
// would index the element BEFORE the array, and anything past E.Max, which
// would index past its end. §2.4's argument for the first is the argument for
// the second, and this is the test that says the port took both.
//
// It is a GATE and not a nicety: Go's own bounds check catches the upper case
// on a slice, but `TableKeyed` hands back a POINTER a caller then writes
// through, and the panic is what stops that at the index rather than at some
// later dereference. The lower case Go's bounds check never sees at all —
// `slots[-1]` is `slots[key-1]` with key 0, which is a runtime panic in this
// build and a silent read one slot back in any language that did the
// arithmetic without the compare.
package schematables

import (
	"testing"

	"tabledemo"
)

// refuses reports whether f panicked, so both arms read the same way.
func refuses(f func()) (panicked bool) {
	defer func() { panicked = recover() != nil }()
	f()
	return false
}

func TestKeyedRefusesNone(t *testing.T) {
	var hull tabledemo.HullConfig
	if !refuses(func() { _ = tabledemo.TableKeyed(hull.Turrets[:], 0) }) {
		t.Error("None keyed a slot — it names no variant and the storage holds nothing for it")
	}
}

func TestKeyedRefusesPastMax(t *testing.T) {
	var hull tabledemo.HullConfig
	past := len(hull.Turrets) + 1
	if !refuses(func() { _ = tabledemo.TableKeyed(hull.Turrets[:], past) }) {
		t.Errorf("key %d keyed a slot in an array of %d — nothing is stored past E.Max", past, len(hull.Turrets))
	}
	// and the boundary itself: E.Max IS a named variant and must be reachable
	if refuses(func() { _ = tabledemo.TableKeyed(hull.Turrets[:], len(hull.Turrets)) }) {
		t.Error("E.Max is a named variant and keys the last slot — the guard refused it")
	}
}

func TestKeyedPlacesByKey(t *testing.T) {
	var hull tabledemo.HullConfig
	// the key k lives at index k-1, and TableKeyed is the one place that shift
	// is spelled
	for key := 1; key <= len(hull.Turrets); key++ {
		tabledemo.TableKeyed(hull.Turrets[:], key).Damage = float32(key)
	}
	for i, turret := range hull.Turrets {
		if turret.Damage != float32(i+1) {
			t.Errorf("slot %d holds %v, not the key %d that owns it", i, turret.Damage, i+1)
		}
	}
}
