package compiler

import (
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/internal/tablenames"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE SQUIRREL RUNTIME NAMES, SCANNED BOTH WAYS WITH A CONTROL (docs/PORTING.md
// I6; docs/SPEC-TABLES.md §11; internal/tablenames/squirrel.go).
//
// WHAT A SQUIRREL NAME CAN COLLIDE WITH. Every runtime file is its own module
// table (`schema_table_load.nut`'s `_M.load`), reached through the
// loader by name, so no schema declaration lands on a runtime export. The
// unit's table module is ONE exports table that holds both what the emitter
// writes per declaration (the class `_M.<Name>`, its `<Name>Descriptor`,
// `<Name>Save`, ...) and the unit's own names (`Package`, `Descriptors`,
// `Roots`, `Keyed`, the cook and block facts). A declaration spelled like one
// of those would replace it, so the emitter refuses a unit whose module would
// assign a key twice (squirreltable.refuseKeyCollisions), and this scan holds
// the list of the unit's own names, tablenames.SquirrelModuleKeys, to the
// emitted text in both directions, together with every runtime file's exports:
// a key a later card adds and forgets to list fails here whatever it is called.

// squirrelRuntimeExport is a key a runtime file's module table gets at column
// zero, named either canonically (_M.name) or by a bracket assignment
// (_M["name"]): both are module slots, so neither may hide from the scan.
var squirrelRuntimeExport = regexp.MustCompile(`(?m)^_M(?:\.|\[\s*")([A-Za-z_]\w*)(?:"\s*\])? <-`)

// squirrelDeclaration is a class the unit's table module defines: a
// declaration of the schema (a table, a type, a union, a map's entry).
var squirrelDeclaration = regexp.MustCompile(`(?m)^_M\.([A-Za-z_]\w*) <- class`)

// squirrelPerDeclaration reports whether key is a name the emitter writes for
// one of the declarations: the class itself, or the class's name followed by
// the suffix of a generated function or fact.
func squirrelPerDeclaration(key string, decls []string) bool {
	for _, d := range decls {
		rest, ok := strings.CutPrefix(key, d)
		if !ok {
			continue
		}
		if rest == "" || slices.Contains(tablenames.SquirrelDeclarationSuffixes(), rest) || strings.HasPrefix(rest, "Fixed") {
			return true
		}
	}
	return false
}

// squirrelUnitKeys is the unit's own names in one emitted table module: every
// key it assigns that is not a declaration or a declaration's generated name.
func squirrelUnitKeys(text string) []string {
	var decls []string
	for _, m := range squirrelDeclaration.FindAllStringSubmatch(text, -1) {
		decls = append(decls, m[1])
	}
	seen := map[string]bool{}
	for _, k := range squirreltable.ModuleKeys(text) {
		if !squirrelPerDeclaration(k, decls) {
			seen[k] = true
		}
	}
	return slices.Sorted(maps.Keys(seen))
}

// squirrelManifestUnits is every unit of the conformance manifest; the corpus
// reaches every form, so the union of what its modules spell is the whole
// emitted surface.
func squirrelManifestUnits(t *testing.T) []*ir.Unit {
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
		units = append(units, unitFromPaths(t, paths))
	}
	if len(units) == 0 {
		t.Fatal("the manifest names no unit")
	}
	return units
}

// squirrelEmittedKeys is the union over the manifest's units of the unit names
// each table module spells, and every runtime file's exports.
func squirrelEmittedKeys(t *testing.T, units []*ir.Unit) map[string]bool {
	t.Helper()
	emitted := map[string]bool{}
	for _, u := range units {
		files, err := squirreltable.Generate(u)
		if err != nil {
			continue // a unit the backend refuses by name spells nothing
		}
		module := squirreltable.ModuleName(u) + ".nut"
		for _, k := range squirrelUnitKeys(string(files[module])) {
			emitted[k] = true
		}
	}
	for _, src := range squirreltable.Runtime() {
		for _, m := range squirrelRuntimeExport.FindAllStringSubmatch(string(src), -1) {
			emitted[m[1]] = true
		}
	}
	return emitted
}

// squirrelNameFindings holds an emitted key set against the Squirrel list,
// both ways.
func squirrelNameFindings(emitted map[string]bool) (unregistered, unemitted []string) {
	known := map[string]bool{}
	for _, name := range tablenames.SquirrelModuleKeys() {
		known[name] = true
	}
	for _, name := range slices.Sorted(maps.Keys(emitted)) {
		if !known[name] {
			unregistered = append(unregistered, name)
		}
	}
	for _, name := range tablenames.SquirrelModuleKeys() {
		if !emitted[name] {
			unemitted = append(unemitted, name)
		}
	}
	return unregistered, unemitted
}

func TestSquirrelTableRuntimeNamesAreClaimed(t *testing.T) {
	t.Parallel()
	emitted := squirrelEmittedKeys(t, squirrelManifestUnits(t))
	if len(emitted) == 0 {
		t.Fatal("the scan found no key in the emitted Squirrel at all: the scan, not the list, is what broke")
	}
	unregistered, unemitted := squirrelNameFindings(emitted)
	for _, name := range unregistered {
		t.Errorf("the Squirrel table emitter spells %s and internal/tablenames/squirrel.go does not list it", name)
	}
	for _, name := range unemitted {
		t.Errorf("internal/tablenames/squirrel.go lists %s, but nothing in the emitted Squirrel names it: drop it or fix the backend", name)
	}
	// the shared registry's Squirrel names are on the list too
	known := map[string]bool{}
	for _, name := range tablenames.SquirrelModuleKeys() {
		known[name] = true
	}
	for _, name := range tablenames.DefinedBy(tablenames.Squirrel) {
		if !known[name] {
			t.Errorf("the registry says the Squirrel backend defines %s, and SquirrelModuleKeys does not list it", name)
		}
	}
}

// TestSquirrelRuntimeNameScanGoesRed is the scan's own NEGATIVE CONTROL: a
// table module and a runtime file that spell keys nobody listed, and an
// emitted set from which a listed name is gone, must each be reported.
func TestSquirrelRuntimeNameScanGoesRed(t *testing.T) {
	t.Parallel()
	module := strings.Join([]string{
		"_M.Ship <- class {",
		"};",
		"_M.ShipDescriptor <- {};",
		"_M.ShipSave <- function(v) {};",
		"_M.TableRogueUnitName <- 1;",
		"  _M.nested <- 1;", // indented: not module scope
		"local TableRogueLocal = 2;",
	}, "\n")
	got := squirrelUnitKeys(module)
	if strings.Join(got, ",") != "TableRogueUnitName" {
		t.Fatalf("the unit-key scan is not seeing what it must: got %v, want [TableRogueUnitName]", got)
	}
	emitted := map[string]bool{"TableRogueUnitName": true}
	for _, m := range squirrelRuntimeExport.FindAllStringSubmatch("_M.tableRogueExport <- function() {};\n_M[\"tableRogueBracketExport\"] <- function() {};\n", -1) {
		emitted[m[1]] = true
	}
	unregistered, _ := squirrelNameFindings(emitted)
	if strings.Join(unregistered, ",") != "TableRogueUnitName,tableRogueBracketExport,tableRogueExport" {
		t.Fatalf("the planted keys were not reported unlisted: %v", unregistered)
	}
	some := tablenames.SquirrelModuleKeys()[0]
	if _, unemitted := squirrelNameFindings(map[string]bool{}); !slices.Contains(unemitted, some) {
		t.Fatalf("an emitted set without %s was not reported as a listed name nothing emits", some)
	}
}

// squirrelUnitModuleKeys is every key the unit's table module spells over the
// whole corpus, without repeats: the unit's own names, the ones no declaration
// spells.
func squirrelUnitModuleKeys(t *testing.T) []string {
	t.Helper()
	seen := map[string]bool{}
	for _, u := range squirrelManifestUnits(t) {
		files, err := squirreltable.Generate(u)
		if err != nil {
			continue
		}
		module := squirreltable.ModuleName(u) + ".nut"
		for _, k := range squirrelUnitKeys(string(files[module])) {
			seen[k] = true
		}
	}
	return slices.Sorted(maps.Keys(seen))
}

// generateRefuses runs the generate path the way the command does: load (the
// check), then emit. It returns the refusal, or nil when the target accepted
// the unit.
func generateRefuses(t *testing.T, source, target string) error {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "Probe.schema")
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	c := New()
	u, err := c.Load([]string{path})
	if err != nil {
		return err
	}
	_, err = c.Generate(u, target, Options{})
	return err
}

// TestSquirrelModuleKeysAreClaimedForEveryTarget pins the claim on the unit's
// table module for EVERY target: a table named for a key that module defines
// is refused by the check, before any target is chosen, and so by
// `generate --lang go` as well. The check is the one every generation runs, so
// the refusal does not depend on the Squirrel emitter being the target. A near
// name is the author's.
func TestSquirrelModuleKeysAreClaimedForEveryTarget(t *testing.T) {
	t.Parallel()
	keys := squirrelUnitModuleKeys(t)
	if len(keys) == 0 {
		t.Fatal("the scan found no key in the unit's emitted table module at all: the scan, not the registry, is what broke")
	}
	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			t.Parallel()
			src := "package probe\n\ntable Gadget\n{\n    n int32\n}\n\ntable " + key + "\n{\n    n int32\n}\n"
			if errs := checkErrors(t, src); len(errs) == 0 {
				t.Fatalf("check accepted a table named %s: the Squirrel table module defines that key, so the unit cannot be generated for any target", key)
			}
			if err := generateRefuses(t, src, "go"); err == nil {
				t.Fatalf("generate --lang go accepted a table named %s: the check that runs before every generation must refuse it", key)
			}
		})
	}
	// THE CONTROL: a near name is not a key of the module, and stays legal.
	const near = "package probe\n\ntable Gadget\n{\n    n int32\n}\n\ntable CookBook\n{\n    n int32\n}\n"
	if errs := checkErrors(t, near); len(errs) != 0 {
		t.Fatalf("check refused CookBook, which the unit's table module does not define: %v", errs[0])
	}
	if err := generateRefuses(t, near, "go"); err != nil {
		t.Fatalf("generate --lang go refused CookBook: %v", err)
	}
}

// unitWithTableNamed loads a unit that declares table Template and renames it
// to name, so the Squirrel emitter's own key guard can be reached after the
// checker has refused the same declaration: the two guards are separate, and
// this test is the emitter's.
func unitWithTableNamed(t *testing.T, src, name string) *ir.Unit {
	t.Helper()
	u := unitFromSource(t, src)
	st := u.Tables["Template"]
	if st == nil {
		t.Fatal("the fixture declares no table Template")
	}
	delete(u.Tables, "Template")
	st.Name = name
	u.Tables[name] = st
	if file, ok := u.DeclFile["Template"]; ok {
		delete(u.DeclFile, "Template")
		u.DeclFile[name] = file
	}
	for _, f := range u.Files {
		for _, tbl := range f.Tables {
			if tbl == st {
				tbl.Name = name
			}
		}
	}
	return u
}

// TestSquirrelRefusesADeclarationOnAModuleKey is the claimable half: a
// declaration named for one of the unit's own names is REFUSED beside a table, naming the key,
// and the same declaration in a table-free unit is kept, because no table
// module is emitted there.
func TestSquirrelRefusesADeclarationOnAModuleKey(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"Roots", "Descriptors", "Package", "Keyed"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			withTable := "package probe\n\ntable Gadget\n{\n    n int32\n    k [Team]int32\n}\n\nenum Team { Red, Blue }\n\ntable Template\n{\n    n int32\n}\n"
			if _, err := squirreltable.Generate(unitWithTableNamed(t, withTable, name)); err == nil || !strings.Contains(err.Error(), name+" twice") {
				t.Fatalf("a table named %s beside the module's own %s was not refused by name: %v", name, name, err)
			}
			tableFree := "package probe\n\ntype " + name + "\n{\n    n int32\n}\n"
			if _, err := squirreltable.Generate(unitFromSource(t, tableFree)); err != nil {
				t.Fatalf("a type named %s in a table-free unit was refused (%v): a claim costs a schema nothing until it declares a table", name, err)
			}
		})
	}
	// THE CONTROL: the same check with no colliding name accepts the unit
	ok := "package probe\n\ntable Gadget\n{\n    n int32\n}\n"
	if _, err := squirreltable.Generate(unitFromSource(t, ok)); err != nil {
		t.Fatalf("a unit with no colliding name was refused: %v", err)
	}
}

// unitFromPaths loads a unit from its schema files and directories.
func unitFromPaths(t *testing.T, paths []string) *ir.Unit {
	t.Helper()
	gathered, err := GatherPaths(paths)
	if err != nil {
		t.Fatal(err)
	}
	u, err := New().Load(gathered)
	if err != nil {
		t.Fatalf("load %v: %v", paths, err)
	}
	return u
}
