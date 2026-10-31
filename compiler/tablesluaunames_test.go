package compiler

import (
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	"github.com/mas-bandwidth/schema/v2/internal/tablenames"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE LUAU RUNTIME NAMES, SCANNED BOTH WAYS WITH A CONTROL (docs/PORTING.md I6;
// docs/SPEC-TABLES.md §11; internal/tablenames/luau.go).
//
// WHAT A LUAU RUNTIME CAN COLLIDE WITH. Every Luau file is its own scope: a
// `local` at module scope is private to its file, and everything a module
// shares is a KEY OF THE TABLE its file returns (`require("./schema_load")
// .loadTolerant`, `_T.HullConfigDescriptor`). A schema name reaches a module
// table only in the spellings the emitter writes per declaration (the
// declaration's own name, `New<Name>`, `<Name>Descriptor`), none of which a
// runtime key spells. So every Luau name is SCOPED (it claims nothing), and what
// holds the list honest is this scan: every key of every returned module table
// the emitter spells is registered, and every registered Luau key is spelled,
// in both directions. No Luau FILE name is claimable either: the runtime's
// files are schema_*.luau and a schema file's modules are <Base>Table.luau,
// <Base>View.luau and <Base>Block.luau, so no base a schema file can have
// emits a runtime module's file name.
//
// The scan reads the emitted text, never a list: a key a later change adds and
// forgets to register fails here whatever it is called.

var (
	luauModuleFunction = regexp.MustCompile(`(?m)^function ([A-Za-z_]\w*)[.:]([A-Za-z_]\w*)\s*\(`)
	luauModuleAssign   = regexp.MustCompile(`(?m)^([A-Za-z_]\w*)\.([A-Za-z_]\w*)\s*=[^=]`)
	luauModuleReturn   = regexp.MustCompile(`(?m)^return (?:table\.freeze\()?([A-Za-z_]\w*)\)?\s*$`)
	luauLiteralReturn  = regexp.MustCompile(`(?ms)^return table\.freeze\(\{\n(.*?)^\}\)`)
	luauLiteralKey     = regexp.MustCompile(`(?m)^\t([A-Za-z_]\w*)\s*=`)
)

// luauModuleKeys is every key the file's returned module table gets at column
// zero, under any convention: `function M.key(`, `function M:key(` and
// `M.key = `, and the keys of a `return table.freeze({ ... })` literal. The
// module table is the identifier the file returns; a helper table the file
// keeps to itself is not a name anything outside the file can reach.
func luauModuleKeys(text string) []string {
	seen := map[string]bool{}
	if lit := luauLiteralReturn.FindStringSubmatch(text); lit != nil {
		for _, m := range luauLiteralKey.FindAllStringSubmatch(lit[1], -1) {
			seen[m[1]] = true
		}
	}
	if ret := luauModuleReturn.FindAllStringSubmatch(text, -1); len(ret) > 0 {
		module := ret[len(ret)-1][1]
		for _, re := range []*regexp.Regexp{luauModuleFunction, luauModuleAssign} {
			for _, m := range re.FindAllStringSubmatch(text, -1) {
				if m[1] == module {
					seen[m[2]] = true
				}
			}
		}
	}
	return slices.Sorted(maps.Keys(seen))
}

// luauManifestUnits is every unit of the conformance manifest: the corpus
// reaches every form, so the union of what its modules spell is the whole
// emitted surface.
func luauManifestUnits(t *testing.T) []*ir.Unit {
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
		var roots []string
		for _, p := range f[2:] {
			roots = append(roots, filepath.Join("..", p))
		}
		paths, err := GatherPaths(roots)
		if err != nil {
			t.Fatalf("unit %s: %v", f[1], err)
		}
		u, err := New().Load(paths)
		if err != nil {
			continue // a unit that does not load spells nothing
		}
		units = append(units, u)
	}
	if len(units) == 0 {
		t.Fatal("the manifest names no unit")
	}
	return units
}

// luauEmittedKeys is the union, over the units the Luau table backend
// generates, of every module-table key its emitted table sources spell (every
// schema_*.luau runtime module, and every <Base>Table, View and Block module),
// minus the spellings the emitter writes per declaration in the latter.
func luauEmittedKeys(t *testing.T, units []*ir.Unit) map[string]bool {
	t.Helper()
	emitted := map[string]bool{}
	for _, u := range units {
		files, err := luautable.Generate(u)
		if err != nil {
			continue // a unit the backend refuses by name spells nothing
		}
		decls := make([]string, 0, len(u.DeclFile))
		for name := range u.DeclFile {
			decls = append(decls, name)
		}
		for file, data := range files {
			for _, k := range luauModuleKeys(string(data)) {
				if !strings.HasPrefix(file, "schema_") && perDeclaration(k, decls) {
					continue
				}
				emitted[k] = true
			}
		}
	}
	return emitted
}

// perDeclaration reports whether a key of a generated <Base>*.luau module is
// one the emitter spells from a declaration's own name: the name itself,
// New<Name>, EnumName<Name>Type (a union's tag names), or the name followed by
// a suffix (<Name>Descriptor, <Name>FixedLayout, <Name>MaxBits, <Name>Block, a
// flags set's <Name><Variant>).
// A schema reaches a module table through these and nothing else, so they
// claim nothing a schema does not already own.
func perDeclaration(key string, decls []string) bool {
	for _, d := range decls {
		if key == d || strings.HasPrefix(key, d) || strings.HasPrefix(key, "New"+d) || strings.HasPrefix(key, "EnumName"+d) {
			return true
		}
	}
	return false
}

// luauNameFindings holds one emitted key set against the Luau backend's list
// of module keys (tablenames.LuauModuleKeys), both ways.
func luauNameFindings(emitted map[string]bool) (unregistered, unemitted []string) {
	known := map[string]bool{}
	for _, name := range tablenames.LuauModuleKeys() {
		known[name] = true
	}
	for _, name := range slices.Sorted(maps.Keys(emitted)) {
		if !known[name] {
			unregistered = append(unregistered, name)
		}
	}
	for _, name := range tablenames.LuauModuleKeys() {
		if !emitted[name] {
			unemitted = append(unemitted, name)
		}
	}
	return unregistered, unemitted
}

func TestLuauTableRuntimeNamesAreClaimed(t *testing.T) {
	t.Parallel()
	emitted := luauEmittedKeys(t, luauManifestUnits(t))
	if len(emitted) == 0 {
		t.Fatal("the scan found no module-table key in the emitted Luau at all: the scan, not the registry, is what broke")
	}
	unregistered, unemitted := luauNameFindings(emitted)
	for _, name := range unregistered {
		t.Errorf("the Luau table emitter spells the module-table key %s and internal/tablenames does not register it: "+
			"register it in internal/tablenames/luau.go (a module key claims nothing, but the scan needs an answer for it)", name)
	}
	for _, name := range unemitted {
		t.Errorf("internal/tablenames says the Luau backend defines %s, but nothing in the emitted Luau names it: "+
			"drop the registration or fix the backend; a claim nothing needs takes a name away from every schema for free", name)
	}
	// the shared registry's Luau names are module keys too, and claim nothing
	known := map[string]bool{}
	for _, name := range tablenames.LuauModuleKeys() {
		known[name] = true
	}
	for _, name := range tablenames.DefinedBy(tablenames.Luau) {
		if !known[name] {
			t.Errorf("the registry says the Luau backend defines %s, and it is not a module key LuauModuleKeys lists", name)
		}
		if tablenames.By(name) == tablenames.Luau && slices.Contains(tablenames.Claimed(), name) {
			t.Errorf("%s is claimed by the Luau backend alone, and every Luau runtime spelling is a module-table key that claims nothing", name)
		}
	}
}

// TestLuauRuntimeNameScanGoesRed is the scan's own NEGATIVE CONTROL: it feeds
// the scan a runtime module that spells a key nobody registered, in each
// convention the scan reads, and an emitted set from which a registered name
// is gone, and requires both findings.
func TestLuauRuntimeNameScanGoesRed(t *testing.T) {
	t.Parallel()
	sample := strings.Join([]string{
		"local M = {}",
		"local Reader = {}",
		"function M.TableRogueFunction() end",
		"function M:TableRogueMethod() end",
		"M.TableRogueValue = 1",
		"M.TableRogueEq = function() end",
		"function Reader.hidden() end", // a helper the file keeps to itself
		"\tM.nested = 1",               // indented: not module scope
		"local TableRogueLocal = 2",    // a local is file-private
		"return table.freeze(M)",
	}, "\n")
	got := luauModuleKeys(sample)
	want := []string{"TableRogueEq", "TableRogueFunction", "TableRogueMethod", "TableRogueValue"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("the module-key scan is not seeing what it must:\n got %v\nwant %v", got, want)
	}
	literal := "local function a() end\nreturn table.freeze({\n\tTableRogueLiteral = a,\n})\n"
	if lit := luauModuleKeys(literal); strings.Join(lit, ",") != "TableRogueLiteral" {
		t.Fatalf("the literal-return scan is not seeing what it must: got %v", lit)
	}
	emitted := map[string]bool{"TableRogueLiteral": true}
	for _, k := range got {
		emitted[k] = true
	}
	for k := range emitted {
		if slices.Contains(tablenames.LuauModuleKeys(), k) {
			t.Fatalf("the planted %s is listed, so the control proves nothing: pick a name the list does not hold", k)
		}
	}
	unregistered, _ := luauNameFindings(emitted)
	if strings.Join(unregistered, ",") != strings.Join(slices.Sorted(maps.Keys(emitted)), ",") {
		t.Fatalf("the planted keys were not reported unregistered: %v", unregistered)
	}
	some := tablenames.LuauModuleKeys()[0]
	if _, unemitted := luauNameFindings(map[string]bool{}); !slices.Contains(unemitted, some) {
		t.Fatalf("an emitted set without %s was not reported as a registered name nothing emits", some)
	}
}
