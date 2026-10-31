package compiler

import (
	"slices"
	"testing"
)

// TestLuaCarriesRegistryOneFilePerConstruct holds the Lua carries registry to
// its shape (compiler/carries_lua.go): each carries_lua_<construct>.go file
// adds one known construct once, and the target is registered with exactly
// the constructs those files name.
func TestLuaCarriesRegistryOneFilePerConstruct(t *testing.T) {
	t.Parallel()
	seen := map[string]bool{}
	for _, construct := range luaCarried {
		if seen[construct] {
			t.Errorf("lua carries %q from more than one file", construct)
		}
		seen[construct] = true
		if !slices.Contains(carryConstructs, construct) {
			t.Errorf("lua carries unknown construct %q", construct)
		}
		if !carries("lua", construct) {
			t.Errorf("lua names %q but the registry does not carry it", construct)
		}
	}
	if got, want := len(targetCarries["lua"]), len(seen); got != want {
		t.Errorf("lua registry holds %d constructs, its files name %d: %v", got, want, targetCarries["lua"])
	}
}
