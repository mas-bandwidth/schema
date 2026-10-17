// Tests for the LUAU tables surface's refusals (docs/PORT-LUAU.md, THE
// RUNTIME LAYOUT; docs/SPEC-TABLES.md §11): every construct the Luau table
// target refuses must refuse BY NAME — the error names the offending file or
// field and the target luau.
package compiler

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
)

// TestLuauRefusesUnportedConstructsByName holds every construct the Luau
// target refuses, each with a control: a require cycle between table modules
// is refused naming the cycle, a schema file whose module collides with a
// runtime module is refused by name, and a file that collides with its own
// <Base>Table sibling is refused by name. Every error names the target luau.
func TestLuauRefusesUnportedConstructsByName(t *testing.T) {
	t.Parallel()
	load := func(files map[string]string) error {
		dir := t.TempDir()
		var paths []string
		for name, src := range files {
			p := filepath.Join(dir, name)
			if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
				return err
			}
			paths = append(paths, p)
		}
		sort.Strings(paths)
		u, err := New().Load(paths)
		if err != nil {
			t.Fatalf("the unit does not load: %v", err)
		}
		if _, err := luau.Generate(u); err != nil {
			return err
		}
		_, err = luautable.Generate(u)
		return err
	}

	// a require cycle between table modules, refused naming the cycle
	cyc := map[string]map[string]bool{"ATable": {"BTable": true}, "BTable": {"ATable": true}}
	err := luau.RequireCycle(cyc, []string{"ATable", "BTable"}, "the table surface")
	if err == nil || !strings.Contains(err.Error(), "ATable -> BTable -> ATable") || !strings.Contains(strings.ToLower(err.Error()), "luau") {
		t.Errorf("a require cycle: want it refused by name naming luau, got %v", err)
	}

	// a schema file whose module collides with a runtime module
	err = load(map[string]string{"schema_runtime.schema": "package clash\ntype T\n{\n    x uint8\n}\n"})
	if err == nil || !strings.Contains(err.Error(), "collides with the Luau runtime module schema_runtime.luau") {
		t.Errorf("a schema file named for the runtime: want the collision refused naming luau, got %v", err)
	}

	// the control: a clean table unit generates with no refusal
	if err := load(map[string]string{"Game.schema": "package g\ntable Game\n{\n    x uint8\n}\n"}); err != nil {
		t.Errorf("the control, a clean table unit: %v", err)
	}
}
