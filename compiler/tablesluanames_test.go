package compiler

import (
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luatable"
	"github.com/mas-bandwidth/schema/v2/internal/tablenames"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE LUA RUNTIME NAMES, SCANNED BOTH WAYS WITH A CONTROL (docs/PORTING.md I6;
// docs/SPEC-TABLES.md §11; internal/tablenames/lua.go).
//
// WHAT A LUA RUNTIME CAN COLLIDE WITH. Every Lua file is its own scope: a
// `local` at module scope is private to its file, and everything a runtime
// shares is a KEY OF THE MODULE TABLE its file returns (`_M.Tables`,
// `B.BlockOpen`, `R.ReadStrict`). A schema name reaches a runtime table in
// exactly three spellings, all written by the emitter and none able to land on
// a runtime key: the constructor `_M.New<Declaration>` (and no runtime key
// begins with New), the typed save `_M.SaveTyped<Table>` (the unit module's
// one SaveTyped-prefixed key), and the descriptor maps `_M.Tables["<Name>"]`,
// `_M.Unions["<Name>"]` (a string key into a map, never a field of the module).
// So the registry's Lua names are all SCOPED (they claim nothing), and what
// holds the claim is this scan: every key of every returned module table the
// emitter spells is registered, and every registered Lua name is spelled, in
// both directions. The one claimable Lua spelling is the runtime's FILE name,
// because `require` has one flat namespace: a schema file named for a runtime
// module is refused beside a table and kept in a table-free unit
// (TestLuaRuntimeFileNamesAreClaimed).
//
// The scan reads the emitted text, never a list: a key a later card adds and
// forgets to register fails here whatever it is called.

var (
	luaModuleFunction = regexp.MustCompile(`(?m)^function ([A-Za-z_]\w*)[.:]([A-Za-z_]\w*)\s*\(`)
	luaModuleAssign   = regexp.MustCompile(`(?m)^([A-Za-z_]\w*)\.([A-Za-z_]\w*)\s*=[^=]`)
	luaModuleReturn   = regexp.MustCompile(`(?m)^return ([A-Za-z_]\w*)\s*$`)
)

// luaModuleKeys is every key the file's returned module table gets at column
// zero, under any convention: `function M.key(`, `function M:key(` and
// `M.key = `. The module table is the identifier the file returns; a helper
// table the file keeps to itself (a reader class, a local record) is not a name
// anything outside the file can reach, and is not scanned.
func luaModuleKeys(text string) []string {
	ret := luaModuleReturn.FindAllStringSubmatch(text, -1)
	if len(ret) == 0 {
		return nil
	}
	module := ret[len(ret)-1][1]
	seen := map[string]bool{}
	for _, re := range []*regexp.Regexp{luaModuleFunction, luaModuleAssign} {
		for _, m := range re.FindAllStringSubmatch(text, -1) {
			if m[1] == module {
				seen[m[2]] = true
			}
		}
	}
	return slices.Sorted(maps.Keys(seen))
}

// luaManifestUnits is every unit of the conformance manifest the Lua table
// backend generates: the corpus reaches every form, so the union of what its
// modules spell is the whole emitted surface.
func luaManifestUnits(t *testing.T) []*ir.Unit {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var units []*ir.Unit
	for line := range strings.SplitSeq(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) < 3 || f[0] != "unit" {
			continue
		}
		var paths []string
		for _, p := range f[2:] {
			paths = append(paths, filepath.Join("..", p))
		}
		units = append(units, luarun.LoadUnit(t, paths...))
	}
	if len(units) == 0 {
		t.Fatal("the manifest names no unit")
	}
	return units
}

// luaEmittedKeys is the union, over the units the Lua table backend generates,
// of every module-table key its emitted table sources spell (the unit's table
// module, its view and every runtime file), minus the constructors
// `New<Declaration>` and the typed saves `SaveTyped<Table>` the emitter writes
// per declaration.
func luaEmittedKeys(t *testing.T, units []*ir.Unit) map[string]bool {
	t.Helper()
	emitted := map[string]bool{}
	// the manifest reaches every form but a retired enum variant, which is what
	// emits _M.DeprecatedVariants (§2.15)
	units = append(units, unitFromSourceForNames(t))
	for _, u := range units {
		files, err := luatable.Generate(u)
		if err != nil {
			continue // a unit the backend refuses by name spells nothing
		}
		sources := make([]string, 0, len(files)+1)
		for _, data := range files {
			sources = append(sources, string(data))
		}
		if view, ok := luatable.ViewFile(u); ok {
			sources = append(sources, view)
		}
		constructors := map[string]bool{}
		for name := range u.Tables {
			constructors["New"+name] = true
			constructors["SaveTyped"+name] = true
		}
		for name := range u.Structs {
			constructors["New"+name] = true
		}
		for _, src := range sources {
			for _, k := range luaModuleKeys(src) {
				if !constructors[k] {
					emitted[k] = true
				}
			}
		}
	}
	return emitted
}

func unitFromSourceForNames(t *testing.T) *ir.Unit {
	t.Helper()
	return unitFromSource(t, "package probe\n\nenum Ship\n{\n    Scout,\n    Bomber | deprecated,\n}\n\ntable Fleet\n{\n    ship Ship\n}\n")
}

// luaNameFindings holds one emitted key set against the Lua backend's list of
// module keys (tablenames.LuaModuleKeys), both ways.
func luaNameFindings(emitted map[string]bool) (unregistered, unemitted []string) {
	known := map[string]bool{}
	for _, name := range tablenames.LuaModuleKeys() {
		known[name] = true
	}
	for _, name := range slices.Sorted(maps.Keys(emitted)) {
		if !known[name] {
			unregistered = append(unregistered, name)
		}
	}
	for _, name := range tablenames.LuaModuleKeys() {
		if !emitted[name] {
			unemitted = append(unemitted, name)
		}
	}
	return unregistered, unemitted
}

func TestLuaTableRuntimeNamesAreClaimed(t *testing.T) {
	t.Parallel()
	emitted := luaEmittedKeys(t, luaManifestUnits(t))
	if len(emitted) == 0 {
		t.Fatal("the scan found no module-table key in the emitted Lua at all: the scan, not the registry, is what broke")
	}
	unregistered, unemitted := luaNameFindings(emitted)
	for _, name := range unregistered {
		t.Errorf("the Lua table emitter spells the module-table key %s and internal/tablenames does not register it: "+
			"register it in internal/tablenames/lua.go (Scoped: a module key claims nothing, but the scan needs an answer for it)", name)
	}
	for _, name := range unemitted {
		t.Errorf("internal/tablenames says the Lua backend defines %s, but nothing in the emitted Lua names it: "+
			"drop the registration or fix the backend; a claim nothing needs takes a name away from every schema for free", name)
	}
	// the shared registry's Lua names are module keys too, and claim nothing
	// (docs/SPEC-TABLES.md §11)
	known := map[string]bool{}
	for _, name := range tablenames.LuaModuleKeys() {
		known[name] = true
	}
	for _, name := range tablenames.DefinedBy(tablenames.Lua) {
		if !known[name] {
			t.Errorf("the registry says the Lua backend defines %s, and it is not a module key LuaModuleKeys lists", name)
		}
		if tablenames.By(name) == tablenames.Lua && slices.Contains(tablenames.Claimed(), name) {
			t.Errorf("%s is claimed by the Lua backend alone, and every Lua runtime spelling is a module-table key that claims nothing", name)
		}
	}
}

// TestLuaRuntimeNameScanGoesRed is the scan's own NEGATIVE CONTROL (the twin of
// TestGoRuntimeNameScanGoesRed and TestJavaRuntimeNameScanGoesRed): the scan is
// only worth running if it can go red, so this feeds it a runtime module that
// spells a key nobody registered, in each convention the scan reads, and an
// emitted set from which a registered name is gone.
func TestLuaRuntimeNameScanGoesRed(t *testing.T) {
	t.Parallel()
	sample := strings.Join([]string{
		"local M = {}",
		"local Reader = {}",
		"function M.TableRogueFunction() end",
		"function M:TableRogueMethod() end",
		"M.TableRogueValue = 1",
		"M.TableRogueEq = function() end",
		"function Reader.hidden() end", // a helper the file keeps to itself
		"  M.nested = 1",               // indented: not module scope
		"local TableRogueLocal = 2",    // a local is file-private
		"return M",
	}, "\n")
	got := luaModuleKeys(sample)
	want := []string{"TableRogueEq", "TableRogueFunction", "TableRogueMethod", "TableRogueValue"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("the module-key scan is not seeing what it must:\n got %v\nwant %v", got, want)
	}
	emitted := map[string]bool{}
	for _, k := range got {
		if slices.Contains(tablenames.LuaModuleKeys(), k) {
			t.Fatalf("the planted %s is listed, so the control proves nothing: pick a name the list does not hold", k)
		}
		emitted[k] = true
	}
	unregistered, _ := luaNameFindings(emitted)
	if strings.Join(unregistered, ",") != strings.Join(want, ",") {
		t.Fatalf("the planted keys were not reported unregistered: %v", unregistered)
	}
	// and the other direction: with one registered Lua name missing from what is
	// emitted, the scan must say so
	some := tablenames.LuaModuleKeys()[0]
	if _, unemitted := luaNameFindings(map[string]bool{}); !slices.Contains(unemitted, some) {
		t.Fatalf("an emitted set without %s was not reported as a registered name nothing emits", some)
	}
}

// TestLuaRuntimeFileNamesAreClaimed is the claimable half: `require` has one
// flat namespace, so a schema file named for any emitted runtime module (or for
// the unit's own table module) is REFUSED beside a table, and the same file in a
// table-free unit is kept, because no runtime is emitted there.
func TestLuaRuntimeFileNamesAreClaimed(t *testing.T) {
	t.Parallel()
	const withTable = "package probe\n\ntable Gadget\n{\n    n int32\n}\n"
	const tableFree = "package probe\n\ntype Gadget\n{\n    n int32\n}\n"
	probe, err := luatable.Generate(unitFromSource(t, withTable))
	if err != nil {
		t.Fatal(err)
	}
	var homes []string
	for name := range probe {
		if base, ok := strings.CutSuffix(name, ".lua"); ok {
			homes = append(homes, base)
		}
	}
	sort.Strings(homes)
	if len(homes) < 10 {
		t.Fatalf("the Lua backend emitted only %v beside the unit's module: the scan is blind", homes)
	}
	for _, base := range homes {
		t.Run(base, func(t *testing.T) {
			t.Parallel()
			if _, err := luaGenerate(unitFromNamedSource(t, base, withTable), nil); err == nil {
				t.Fatalf("a schema file named %s beside a table was accepted: the unit would emit %s.lua twice", base, base)
			}
			if _, err := luaGenerate(unitFromNamedSource(t, base, tableFree), nil); err != nil {
				t.Fatalf("a schema file named %s in a table-free unit was refused (%v): a claim costs a schema nothing until it declares a table", base, err)
			}
		})
	}
}
