// The Squirrel table emitter's claimed-name scan (docs/SPEC-TABLES.md §11,
// docs/PORTING.md I6), the self-maintaining half: for every name the emitted
// unit modules bind on their export table, a schema declaring that name is
// refused, and every name the registry says Squirrel defines is spelled by
// some emitted module. The scan lists nothing by hand: a name an emitter
// starts to bind and nobody claims fails here whatever it looks like.
//
// A Squirrel module's scope is its export table, `_M`, handed in by the loader
// (schema_module.nut): a `local` is private to the file and collides with
// nothing, and every name a unit exports is a column-zero `_M.<Name> <-`. The
// scan reads exactly that, so a binding in any other spelling would not be
// seen; TestSquirrelModuleScopeScanSeesEveryConvention feeds it one of each.
package compiler

import (
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablenames"
)

// squirrelModuleScopeBinding matches a column-zero export of a generated
// module: `_M.Name <- ...` or `_M["Name"] <- ...`. The emitter indents every
// nested assignment by at least two spaces, so column zero is the module's own
// scope.
var squirrelModuleScopeBinding = regexp.MustCompile(`^_M(?:\.([A-Za-z_][A-Za-z0-9_]*)|\["([^"]+)"\])\s*<-`)

// squirrelModuleScopeLocal matches a column-zero `local Name`, which is private
// to the file; the scan counts it only to say it is not a name a schema can
// collide with.
var squirrelModuleScopeLocal = regexp.MustCompile(`^local\s+[A-Za-z_]`)

func squirrelModuleScopeNames(text string) []string {
	seen := map[string]bool{}
	for line := range strings.SplitSeq(text, "\n") {
		if squirrelModuleScopeLocal.MatchString(line) {
			continue
		}
		if m := squirrelModuleScopeBinding.FindStringSubmatch(line); m != nil {
			name := m[1]
			if name == "" {
				name = m[2]
			}
			seen[name] = true
		}
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// TestSquirrelModuleScopeScanSeesEveryConvention is the scan's own NEGATIVE
// CONTROL: it feeds one binding of each spelling an emitter could reach for,
// and requires the scan to see them and to skip a local and a nested
// assignment. A scan that only saw today's convention would go quietly blind.
func TestSquirrelModuleScopeScanSeesEveryConvention(t *testing.T) {
	sample := strings.Join([]string{
		"_M.TableReport <- class {}",
		"_M.lowerHelper <- 1;",
		"_M.SCREAMING_CONST <- 2",
		"_M[\"quoted\"] <- 3",
		"_M.spaced   <- 4",
		"local _private = 5;",
		"  _M.nested <- 6;",
		"_M.assigned = 7;",
	}, "\n")
	got := squirrelModuleScopeNames(sample)
	want := []string{"SCREAMING_CONST", "TableReport", "lowerHelper", "quoted", "spaced"}
	if !slices.Equal(got, want) {
		t.Fatalf("the module-scope scan is not seeing what it must:\n got %v\nwant %v", got, want)
	}
}

// TestSquirrelTableModuleScopeNamesAreClaimed is §11's promise for Squirrel: no
// legal schema reaches a generated module with two bindings for one name.
func TestSquirrelTableModuleScopeNamesAreClaimed(t *testing.T) {
	files, err := New().Generate(unitFromSource(t, runtimeSrc), "squirrel", Options{})
	if err != nil {
		t.Fatal(err)
	}
	declared := map[string]bool{}
	for _, name := range []string{"Point", "Kind", "Probe", "Config", "Keyed"} {
		declared[name] = true
	}
	emitted := map[string]bool{}
	scanned := map[string]bool{}
	for name, data := range files {
		if strings.HasPrefix(name, "schema_") {
			// a runtime module binds on its own export table, reached as
			// _rt_<module>.<Name>, so a name it exports meets no schema name
			// (internal/tablenames registers them Scoped all the same)
			continue
		}
		for _, spelled := range squirrelModuleScopeNames(string(data)) {
			emitted[spelled] = true
			scanned[name] = true
		}
	}
	if len(emitted) == 0 {
		t.Fatal("the scan found no module-scope binding in the emitted Squirrel at all — the scan, not the claim, is what broke")
	}
	if !scanned["ProbeTables.nut"] {
		t.Fatal("the scan saw no binding in the unit's table module — the scan is blind to the file that carries the claims")
	}
	names := make([]string, 0, len(emitted))
	for name := range emitted {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		if declared[name] {
			// a table's own storage class takes the declaration's name, which
			// the front end has claimed since there were declarations
			continue
		}
		t.Run(name, func(t *testing.T) {
			if errs := checkErrors(t, runtimeSrc+"\ntype "+name+"\n{\n    y int32\n}\n"); len(errs) == 0 {
				t.Fatalf("the Squirrel table emitter binds %s at module scope and a declaration of that "+
					"name was accepted — the generated module would carry two bindings for it. Claim it: a "+
					"unit-level runtime name goes in internal/tablenames, a per-declaration one in "+
					"internal/check (docs/SPEC-TABLES.md §11)", name)
			}
		})
	}
}

// TestSquirrelRegistryNamesAreEmitted is the scan's other direction: a claim
// nothing needs takes a name away from every schema for free. A registered
// Squirrel name must be spelled somewhere in what the emitter writes, as an
// export, a local of a runtime module, or a field of its exports.
func TestSquirrelRegistryNamesAreEmitted(t *testing.T) {
	files, err := New().Generate(unitFromSource(t, runtimeSrc), "squirrel", Options{})
	if err != nil {
		t.Fatal(err)
	}
	var all strings.Builder
	for _, data := range files {
		all.Write(data)
		all.WriteByte('\n')
	}
	text := all.String()
	for _, name := range tablenames.DefinedBy(tablenames.Squirrel) {
		if !regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\b`).MatchString(text) {
			t.Errorf("internal/tablenames says the Squirrel backend defines %s, but nothing in the emitted Squirrel names it", name)
		}
	}
}
