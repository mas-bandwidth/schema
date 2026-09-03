// Tests for the JAVASCRIPT tables surface (docs/SPEC-TABLES.md): the js target
// grows <Base>Table.js plus the two accelerators' readers for a unit with
// tables, adds nothing for one without, refuses the variable class by name, and
// — the §11 half — spells no MODULE-SCOPE name a legal schema could collide
// with.
package compiler

import (
	"regexp"
	"sort"
	"strings"
	"testing"
)

// TestJsEmitsTableSources: the js target adds the three table modules beside
// the packet modules for a unit with tables, and adds NOTHING for one without —
// the same contract the cpp and cs targets hold, under both spellings of the
// target name.
func TestJsEmitsTableSources(t *testing.T) {
	c := New()
	for _, target := range []string{"js", "javascript"} {
		with, err := c.Generate(unitFromSource(t, tableSrc), target, Options{})
		if err != nil {
			t.Fatalf("--lang %s: %v", target, err)
		}
		for _, want := range []string{"ProbeTable.js", "ProbeBlock.js", "ProbeCook.js"} {
			if _, ok := with[want]; !ok {
				t.Fatalf("--lang %s emitted no %s for a unit with tables; got %d files", target, want, len(with))
			}
		}
		without, err := c.Generate(unitFromSource(t, packetSrc), target, Options{})
		if err != nil {
			t.Fatalf("--lang %s: %v", target, err)
		}
		for name := range without {
			for _, suffix := range []string{"Table.js", "Block.js", "Cook.js"} {
				if strings.HasSuffix(name, suffix) {
					t.Errorf("--lang %s emitted %s for a table-free unit", target, name)
				}
			}
		}
		// ZERO COST: a table moves NO packet byte. Every module a table-free
		// unit emits is byte-identical in the unit that adds a table, and the
		// only modules a table adds are the three table ones.
		for name, data := range without {
			got, ok := with[name]
			if !ok {
				t.Errorf("--lang %s: module %s disappeared when a table was added", target, name)
				continue
			}
			if string(got) != string(data) {
				t.Errorf("--lang %s: module %s changed when a table was added — tables must move no packet byte", target, name)
			}
		}
		for name := range with {
			if _, ok := without[name]; ok {
				continue
			}
			if !strings.HasSuffix(name, "Table.js") && !strings.HasSuffix(name, "Block.js") &&
				!strings.HasSuffix(name, "Cook.js") {
				t.Errorf("--lang %s: adding a table grew unexpected non-table module %s", target, name)
			}
		}
	}
}

// TestJsRefusesPointeredTables: the JavaScript variable-class refusal is a
// refusal of the WIRE SURFACE and of nothing else (docs/SPEC-TABLES.md §11), the
// same shape the C# one takes. The two ACCELERATORS are POINTED AT, not parsed,
// so both are emitted and the cook's Open opens this unit's cooked assets in
// full.
func TestJsRefusesPointeredTables(t *testing.T) {
	c := New()
	u := unitFromSource(t, packetSrc+`
table Node
{
    value int32
    next  *Node
}
`)
	files, err := c.Generate(u, "js", Options{})
	if err != nil {
		t.Fatalf("--lang js refused a pointered unit outright — the accelerators need no codec: %v", err)
	}
	var cooks int
	for name, data := range files {
		if strings.HasSuffix(name, "Table.js") {
			t.Errorf("--lang js emitted the WIRE surface %s for a pointered unit", name)
		}
		if !strings.HasSuffix(name, "Cook.js") && !strings.HasSuffix(name, "Block.js") {
			continue
		}
		if strings.HasSuffix(name, "Cook.js") {
			cooks++
		}
		text := string(data)
		if !strings.Contains(text, "THE JAVASCRIPT WIRE SURFACE OF THIS UNIT IS REFUSED, BY NAME") {
			t.Errorf("%s carries no refusal banner", name)
		}
		if !strings.Contains(text, "Node") || !strings.Contains(text, "named follow-on") {
			t.Errorf("%s's banner does not name the table and the follow-on", name)
		}
	}
	if cooks == 0 {
		t.Error("--lang js emitted no cook reader for a pointered unit — a cook needs no codec")
	}
}

// jsModuleScopeDeclaration matches a MODULE-SCOPE binding in generated
// JavaScript: a declaration at column zero, exported or not. Column zero is the
// whole test, and it is the right one for this language rather than a happy
// accident of the emitter's style: an ES module's scope is its top level, and
// this emitter indents every nested binding — the descriptors inside a block
// handle's closure, the whole text-form walk — by at least two spaces.
var jsModuleScopeDeclaration = regexp.MustCompile(`^(?:export\s+)?(?:const|let|var|function|class)\s+([A-Za-z_$][A-Za-z0-9_$]*)`)

// jsModuleScopeReExport matches `export { a, b } from "./X.js";` — a re-export
// is a module-scope binding too, and a declaration of one of those names would
// be a second binding for it.
var jsModuleScopeReExport = regexp.MustCompile(`^export\s*\{([^}]*)\}`)

// jsModuleScopeNames is every module-scope binding one generated module spells.
//
// IT DOES NOT LOOK FOR A PREFIX. The C# leg's scan collects `Table*`
// identifiers, which is right for a language whose runtime names all start that
// way; ported verbatim to JavaScript it would be blind to every lowercase or
// SCREAMING_SNAKE helper an emitter might reach for, which is exactly the class
// of name §11 has no claim over. So this collects the DECLARATIONS instead:
// whatever an emitter spells at module scope, under any convention, is a name a
// schema could collide with, and it lands here.
func jsModuleScopeNames(text string) []string {
	seen := map[string]bool{}
	for line := range strings.SplitSeq(text, "\n") {
		if m := jsModuleScopeDeclaration.FindStringSubmatch(line); m != nil {
			seen[m[1]] = true
			continue
		}
		if m := jsModuleScopeReExport.FindStringSubmatch(line); m != nil {
			for part := range strings.SplitSeq(m[1], ",") {
				name := strings.TrimSpace(part)
				// `export { a as b }` binds b; the emitter writes no alias, and
				// this keeps the scan honest if one ever appears
				if i := strings.LastIndex(name, " "); i >= 0 {
					name = strings.TrimSpace(name[i+1:])
				}
				if name != "" {
					seen[name] = true
				}
			}
		}
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// TestJsModuleScopeScanSeesEveryConvention is the scan's own NEGATIVE CONTROL:
// the property that makes it worth running is that it recognizes a module-scope
// binding under any spelling, so the test that proves it feeds one of each. A
// scan that only saw the emitter's own convention would go quietly blind the
// day a port reached for another.
func TestJsModuleScopeScanSeesEveryConvention(t *testing.T) {
	sample := strings.Join([]string{
		"export class TableReport {}",
		"const TABLE_SCRATCH = 1;",
		"let lowerCaseHelper = 2;",
		"var oldStyle = 3;",
		"function plainFunction() {}",
		"export const $dollar = 4;",
		"export { Alpha, Beta } from \"./OtherTable.js\";",
		"  const indentedIsNotModuleScope = 5;",
		"    function alsoNested() {}",
	}, "\n")
	got := jsModuleScopeNames(sample)
	want := []string{"$dollar", "Alpha", "Beta", "TABLE_SCRATCH", "TableReport", "lowerCaseHelper", "oldStyle", "plainFunction"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("the module-scope scan is not seeing what it must:\n got %v\nwant %v", got, want)
	}
}

// TestJsTableModuleScopeNamesAreClaimed is §11's promise for JavaScript: no
// legal schema reaches a generated module with two bindings for one name.
//
// The registry (internal/tablenames) covers the unit-level runtime spellings,
// and the front end claims the name-first surface per declaration. What this
// test asserts is the WHOLE of it, from the other side and without a list: for
// every module-scope binding the emitter actually spells, a schema declaring
// that name IS REFUSED — and a table-free unit keeps it, so the claim costs a
// schema nothing until it declares a table.
//
// It needs no suffix list and no registry lookup, which is what makes it
// self-maintaining: a port that adds a module-scope name and forgets to claim
// it fails here, whatever the name looks like.
func TestJsTableModuleScopeNamesAreClaimed(t *testing.T) {
	files, err := New().Generate(unitFromSource(t, runtimeSrc), "js", Options{})
	if err != nil {
		t.Fatal(err)
	}
	declared := map[string]bool{}
	for _, name := range []string{"Point", "Kind", "Probe", "Config", "Keyed"} {
		declared[name] = true
	}
	emitted := map[string]bool{}
	for name, data := range files {
		if !strings.HasSuffix(name, "Table.js") && !strings.HasSuffix(name, "Block.js") &&
			!strings.HasSuffix(name, "Cook.js") {
			continue // the packet emitter's own modules are its own gate's business
		}
		for _, spelled := range jsModuleScopeNames(string(data)) {
			emitted[spelled] = true
		}
	}
	if len(emitted) == 0 {
		t.Fatal("the scan found no module-scope binding in the emitted JavaScript at all — the scan, not the claim, is what broke")
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
			// the SAME unit plus one declaration of the emitted name: every
			// claim the unit's own declarations make is in force, which is what
			// makes a name-first spelling like ConfigMeasure testable at all
			if errs := checkErrors(t, runtimeSrc+"\ntype "+name+"\n{\n    y int32\n}\n"); len(errs) == 0 {
				t.Fatalf("the JavaScript table emitter binds %s at module scope and a declaration of that "+
					"name was accepted — the generated module would carry two bindings for it. Claim it: a "+
					"unit-level runtime name goes in internal/tablenames, a per-declaration one in "+
					"internal/check (docs/SPEC-TABLES.md §11)", name)
			}
		})
	}
}
