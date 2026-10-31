package luautable_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
)

// TestLuauNoCorpusModuleFoldsOntoTheRuntime pins that a manifest unit either
// emits no module whose name folds case onto a runtime module beside it, or is
// refused by name for exactly that collision (docs/PORT-LUAU.md, THE RUNTIME
// LAYOUT, "never renamed"; the refused list in luautable_test.go). The corpus
// may not smuggle one in as a <Base>.luau or <Base>Table.luau and still generate.
func TestLuauNoCorpusModuleFoldsOntoTheRuntime(t *testing.T) {
	t.Parallel()
	units, _ := readManifest(t)
	if len(units) == 0 {
		t.Fatal("the manifest names no units")
	}
	runtimes := map[string]string{}
	for _, dir := range []string{
		filepath.Join(root, "internal", "codegen", "luautable", "runtime"),
		filepath.Join(root, "internal", "codegen", "luau", "runtime"),
	} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if e.Type().IsRegular() {
				runtimes[strings.ToLower(e.Name())] = e.Name()
			}
		}
	}
	var keys []string
	for k := range units {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		paths, err := compiler.GatherPaths(prefix(units[key]))
		if err != nil {
			t.Fatalf("unit %s: %v", key, err)
		}
		u, err := compiler.New().Load(paths)
		if err != nil {
			t.Fatalf("unit %s does not load: %v", key, err)
		}
		for _, f := range u.Files {
			mods := []string{f.Base + "Table.luau"}
			if luau.EmitsTypeModule(f) {
				mods = append(mods, f.Base+".luau")
			}
			for _, mod := range mods {
				if rt, ok := runtimes[strings.ToLower(mod)]; ok {
					if _, isRefused := refused[key]; isRefused {
						continue // the surface refuses the unit by name for this collision
					}
					t.Errorf("unit %s: schema file %s.schema folds case onto the runtime module %s", key, f.Base, rt)
				}
			}
		}
	}
}
