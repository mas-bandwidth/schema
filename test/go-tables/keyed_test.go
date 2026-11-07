// THE ENUM-KEYED ARRAY's indexing guard (docs/SPEC-TABLES.md §2.4).
//
// The storage holds one slot per NAMED variant, the key k at index k-1, and
// nothing for None. So TWO keys are program errors and not values: 0, which
// would index the element BEFORE the array, and anything past E.Max, which
// would index past its end. §2.4's argument for the first is the argument for
// the second, and this is the test that says the port took both.
//
// It is a GATE and not a nicety: Go's own bounds check panics at both ends
// too, but it names an INDEX, and the technique is the guard that names the
// KEY in every build (docs/PORTING.md M5). So each test requires the guard's
// own refusal, and a build that dropped either compare, leaving only the
// runtime's index panic, is red.
package schematables

import (
	"strings"
	"testing"

	"tabledemo"
)

// refusal is the TableKeyed refusal f raised, or "" when f returned or
// panicked for any other reason. The guard is the technique, not Go's own
// bounds check: `slots[key-1]` with key 0 or key past E.Max panics anyway,
// but with a runtime.Error that names an INDEX, so a test that only asked
// whether f panicked would stay green with the guard removed (docs/PORTING.md
// M5: the refusal names a key, in every build).
func refusal(f func()) (refused string) {
	defer func() {
		if r := recover(); r != nil {
			if s, ok := r.(string); ok && strings.Contains(s, "enum-keyed array") {
				refused = s
			}
		}
	}()
	f()
	return ""
}

func TestKeyedRefusesNone(t *testing.T) {
	var hull tabledemo.HullConfig
	if refusal(func() { _ = tabledemo.TableKeyed(hull.Turrets[:], 0) }) == "" {
		t.Error("None keyed a slot, or was refused by the runtime's bounds check rather than by the key guard — it names no variant and the storage holds nothing for it")
	}
}

func TestKeyedRefusesPastMax(t *testing.T) {
	var hull tabledemo.HullConfig
	past := len(hull.Turrets) + 1
	if refusal(func() { _ = tabledemo.TableKeyed(hull.Turrets[:], past) }) == "" {
		t.Errorf("key %d keyed a slot in an array of %d, or was refused by the runtime's bounds check rather than by the key guard — nothing is stored past E.Max", past, len(hull.Turrets))
	}
	// and the boundary itself: E.Max IS a named variant and must be reachable
	if refused := func() (panicked bool) {
		defer func() { panicked = recover() != nil }()
		_ = tabledemo.TableKeyed(hull.Turrets[:], len(hull.Turrets))
		return false
	}(); refused {
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
