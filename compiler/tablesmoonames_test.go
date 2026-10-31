//go:build schema_leg_moo

package compiler

import (
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/mootable"
	"github.com/mas-bandwidth/schema/v2/internal/tablenames"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE MOO MODULE KEYS, SCANNED BOTH WAYS WITH A CONTROL (docs/PORTING.md I6;
// docs/SPEC-TABLES.md §11; internal/tablenames/moo.go).
//
// WHAT A MOO RUNTIME CAN COLLIDE WITH. Every moo file is its own scope: a
// `local` at module scope is private to its file, and everything a module
// shares is a KEY OF THE TABLE its file returns. A schema name reaches a
// module table only in the spellings the emitter writes per declaration (the
// declaration's own name, New<Name>, <Name>Descriptor, <Name>ToJson, ...), none
// of which a runtime key spells, so no moo key claims a name; what holds the
// list honest is this scan: every key of every returned module table the table
// emitter spells is listed, and every listed key is spelled, both ways. The
// Table* spellings are held by TestTableRuntimeNamesAreClaimedMoo beside it.
//
// The scan reads the emitted text, never a list: a key a later change adds
// and forgets to list fails here whatever it is called.

var (
	mooModuleFunction = regexp.MustCompile(`(?m)^function ([A-Za-z_]\w*)[.:]([A-Za-z_]\w*)\s*\(`)
	mooModuleAssign   = regexp.MustCompile(`(?m)^([A-Za-z_]\w*)\.([A-Za-z_]\w*)\s*=[^=]`)
	mooModuleReturn   = regexp.MustCompile(`(?m)^return (?:table\.freeze\()?([A-Za-z_]\w*)\)?\s*$`)
	mooLiteralReturn  = regexp.MustCompile(`(?ms)^return table\.freeze\(\{\n(.*?)^\}\)`)
	mooLiteralKey     = regexp.MustCompile(`(?m)^\t([A-Za-z_]\w*)\s*=`)
)

// mooModuleKeys is every key the file's returned module table gets at column
// zero: `function M.key(`, `function M:key(` and `M.key = `, and the keys of
// a `return table.freeze({ ... })` literal. A helper table the file keeps to
// itself is not a name anything outside the file can reach.
func mooModuleKeys(text string) []string {
	seen := map[string]bool{}
	if lit := mooLiteralReturn.FindStringSubmatch(text); lit != nil {
		for _, m := range mooLiteralKey.FindAllStringSubmatch(lit[1], -1) {
			seen[m[1]] = true
		}
	}
	if ret := mooModuleReturn.FindAllStringSubmatch(text, -1); len(ret) > 0 {
		module := ret[len(ret)-1][1]
		for _, re := range []*regexp.Regexp{mooModuleFunction, mooModuleAssign} {
			for _, m := range re.FindAllStringSubmatch(text, -1) {
				if m[1] == module {
					seen[m[2]] = true
				}
			}
		}
	}
	return slices.Sorted(maps.Keys(seen))
}

// mooManifestUnits is every unit of the conformance manifest: the corpus
// reaches every form, so the union of what its modules spell is the whole
// emitted surface.
func mooManifestUnits(t *testing.T) []*ir.Unit {
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

// mooDeclaredKey reports whether a key of a generated <Base>Table.moo module
// is one the emitter spells from a declaration's own name: the name itself,
// New<Name>, or the name followed by a suffix (<Name>Descriptor,
// <Name>ToJson, ...). A schema reaches a module table through these and
// nothing else, so they claim nothing a schema does not already own.
func mooDeclaredKey(key string, decls []string) bool {
	for _, d := range decls {
		if strings.HasPrefix(key, d) || strings.HasPrefix(key, "New"+d) {
			return true
		}
	}
	return false
}

// mooEmittedKeys is the union, over the units the moo table backend
// generates, of every module-table key its emitted sources spell, minus the
// per-declaration spellings of the <Base>Table modules.
func mooEmittedKeys(t *testing.T, units []*ir.Unit) map[string]bool {
	t.Helper()
	emitted := map[string]bool{}
	for _, u := range units {
		files, err := mootable.Generate(u)
		if err != nil {
			continue // a unit the backend refuses by name spells nothing
		}
		decls := slices.Collect(maps.Keys(u.DeclFile))
		for file, data := range files {
			for _, k := range mooModuleKeys(string(data)) {
				if strings.HasSuffix(file, "Table.moo") && mooDeclaredKey(k, decls) {
					continue
				}
				emitted[k] = true
			}
		}
	}
	return emitted
}

// mooKeyFindings holds one emitted key set against the moo backend's list,
// both ways.
func mooKeyFindings(emitted map[string]bool) (unlisted, unemitted []string) {
	known := map[string]bool{}
	for _, name := range tablenames.MooModuleKeys() {
		known[name] = true
	}
	for _, name := range slices.Sorted(maps.Keys(emitted)) {
		if !known[name] {
			unlisted = append(unlisted, name)
		}
	}
	for _, name := range tablenames.MooModuleKeys() {
		if !emitted[name] {
			unemitted = append(unemitted, name)
		}
	}
	return unlisted, unemitted
}

func TestMooTableModuleKeysAreClaimed(t *testing.T) {
	t.Parallel()
	emitted := mooEmittedKeys(t, mooManifestUnits(t))
	if len(emitted) == 0 {
		t.Fatal("the scan found no module-table key in the emitted moo at all: the scan, not the list, is what broke")
	}
	unlisted, unemitted := mooKeyFindings(emitted)
	for _, name := range unlisted {
		t.Errorf("the moo table emitter spells the module-table key %s and internal/tablenames/moo.go does not list it", name)
	}
	for _, name := range unemitted {
		t.Errorf("internal/tablenames/moo.go lists %s, but nothing in the emitted moo names it: drop it or fix the backend", name)
	}
}

// TestMooModuleKeyScanGoesRed is the scan's own NEGATIVE CONTROL: it feeds the
// scan a module that spells keys nobody listed, in each convention the scan
// reads, and an emitted set from which a listed name is gone, and requires
// both findings.
func TestMooModuleKeyScanGoesRed(t *testing.T) {
	t.Parallel()
	sample := strings.Join([]string{
		"local M = {}",
		"local Reader = {}",
		"function M.RogueFunction() end",
		"function M:RogueMethod() end",
		"M.RogueValue = 1",
		"M.RogueEq = function() end",
		"function Reader.hidden() end", // a helper the file keeps to itself
		"\tM.nested = 1",               // indented: not module scope
		"local RogueLocal = 2",         // a local is file-private
		"return table.freeze(M)",
	}, "\n")
	got := mooModuleKeys(sample)
	want := []string{"RogueEq", "RogueFunction", "RogueMethod", "RogueValue"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("the module-key scan is not seeing what it must:\n got %v\nwant %v", got, want)
	}
	literal := "local function a() end\nreturn table.freeze({\n\tRogueLiteral = a,\n})\n"
	if lit := mooModuleKeys(literal); strings.Join(lit, ",") != "RogueLiteral" {
		t.Fatalf("the literal-return scan is not seeing what it must: got %v", lit)
	}
	emitted := map[string]bool{"RogueLiteral": true}
	for _, k := range got {
		emitted[k] = true
	}
	for k := range emitted {
		if slices.Contains(tablenames.MooModuleKeys(), k) {
			t.Fatalf("the planted %s is listed, so the control proves nothing: pick a name the list does not hold", k)
		}
	}
	unlisted, _ := mooKeyFindings(emitted)
	if strings.Join(unlisted, ",") != strings.Join(slices.Sorted(maps.Keys(emitted)), ",") {
		t.Fatalf("the planted keys were not reported unlisted: %v", unlisted)
	}
	some := tablenames.MooModuleKeys()[0]
	if _, unemitted := mooKeyFindings(map[string]bool{}); !slices.Contains(unemitted, some) {
		t.Fatalf("an emitted set without %s was not reported as a listed name nothing emits", some)
	}
}
