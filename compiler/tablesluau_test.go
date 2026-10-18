// Tests for the LUAU tables surface's refusals (docs/PORT-LUAU.md, THE
// RUNTIME LAYOUT; docs/SPEC-TABLES.md §11): every construct the Luau target
// refuses must refuse BY NAME — the error names the offending file, module or
// cycle and the target luau.
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

// TestLuauRefusesUnportedConstructsByName holds every construct
// compiler/target_luau.go refuses. Its Generate is luau.Generate then
// luautable.Generate, and this test calls those two in that order (the
// target itself sits behind the schema_leg_luau build tag). The constructs,
// each required to name what it refuses and the target luau:
//
//   - a schema file whose module collides with a runtime module of the type
//     surface (luau.CheckBases) or of the table surface (luautable.Refuse);
//   - a schema file X beside a file XTable (luautable.Refuse);
//   - schema files whose modules require each other in a cycle
//     (luau.CheckAcyclic);
//   - table modules that require each other in a cycle (luau.RequireCycle
//     over the table surface's graph, called directly: a unit that reaches it
//     is refused first as a cycle of its schema files, since a pointer is a
//     require edge of the type surface too).
//
// The control, a clean table unit, generates with no refusal.
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
	named := func(what string, err error, names ...string) {
		t.Helper()
		if err == nil {
			t.Errorf("%s: want it refused by name, got no error", what)
			return
		}
		if !strings.Contains(strings.ToLower(err.Error()), "luau") {
			t.Errorf("%s: the refusal does not name the target luau: %v", what, err)
		}
		for _, n := range names {
			if !strings.Contains(err.Error(), n) {
				t.Errorf("%s: the refusal does not name %q: %v", what, n, err)
			}
		}
	}

	// a schema file named for a runtime module of the type surface
	named("a schema file named for the type runtime",
		load(map[string]string{"schema_runtime.schema": "package clash\ntype T\n{\n    x uint8\n}\n"}),
		"schema_runtime.schema", "schema_runtime.luau")

	// a schema file named for a runtime module of the table surface; it
	// declares a table, so the table surface (and its runtime) is emitted
	for _, mod := range []string{"load", "refusal", "schema_tables"} {
		named("a schema file named for the table runtime module "+mod,
			load(map[string]string{mod + ".schema": "package clash\ntable T\n{\n    x uint8\n}\n"}),
			mod+".schema", mod+".luau")
	}

	// a schema file X beside a file XTable
	named("a file beside its own table module's name",
		load(map[string]string{
			"Game.schema":      "package g\ntable Game\n{\n    x uint8\n}\n",
			"GameTable.schema": "package g\ntype Other\n{\n    y uint8\n}\n",
		}),
		"Game", "GameTable", "GameTable.luau")

	// schema files whose modules require each other in a cycle
	named("schema files in a require cycle",
		load(map[string]string{
			"A.schema": "package c\ntype A\n{\n    b B\n}\ntype C\n{\n    x uint8\n}\n",
			"B.schema": "package c\ntype B\n{\n    c C\n}\n",
		}),
		"A -> B -> A")

	// table modules that require each other in a cycle
	cyc := map[string]map[string]bool{"ATable": {"BTable": true}, "BTable": {"ATable": true}}
	named("table modules in a require cycle",
		luau.RequireCycle(cyc, []string{"ATable", "BTable"}, "the table surface"),
		"ATable -> BTable -> ATable", "the table surface")

	// the control: a clean table unit generates with no refusal
	if err := load(map[string]string{"Game.schema": "package g\ntable Game\n{\n    x uint8\n}\n"}); err != nil {
		t.Errorf("the control, a clean table unit: %v", err)
	}
}
