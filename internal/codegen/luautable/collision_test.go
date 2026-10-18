package luautable_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
)

// TestLuauNoCorpusModuleFoldsOntoTheRuntime pins that no manifest unit emits a
// module whose name folds case onto a runtime module beside it
// (docs/PORT-LUAU.md, THE RUNTIME LAYOUT, "never renamed"). A schema file
// whose module would collide with a runtime module is refused by name; the
// corpus may not smuggle one in as a <Base>.luau or <Base>Table.luau.
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
		for _, p := range paths {
			if !strings.HasSuffix(p, ".schema") {
				continue
			}
			base := strings.TrimSuffix(filepath.Base(p), ".schema")
			for _, mod := range []string{base + ".luau", base + "Table.luau"} {
				if rt, ok := runtimes[strings.ToLower(mod)]; ok {
					t.Errorf("unit %s: schema file %s folds case onto the runtime module %s", key, filepath.Base(p), rt)
				}
			}
		}
	}
}
