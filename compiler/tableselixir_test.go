// The tables tests of ONE language, in its own file so a port adds a file and
// edits no shared one (docs/CONTRIBUTING.md, "Adding a language").
package compiler

import (
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablenames"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestElixirEmitsTableModules: the elixir target adds the table modules beside
// the packet ones for a unit with tables, and adds NOTHING for a unit without —
// the same contract the cpp, cs and rust targets hold.
func TestElixirEmitsTableModules(t *testing.T) {
	c := New()
	with, err := c.Generate(unitFromSource(t, tableSrc), "elixir", Options{})
	if err != nil {
		t.Fatalf("--lang elixir: %v", err)
	}
	for _, want := range []string{"ProbeTable.ex", "TableRuntime.ex", "ProbeBlock.ex", "ProbeCook.ex", "BuildVersion.ex"} {
		if _, ok := with[want]; !ok {
			t.Errorf("--lang elixir emitted no %s for a unit with tables; got %d files", want, len(with))
		}
	}

	without, err := c.Generate(unitFromSource(t, packetSrc), "elixir", Options{})
	if err != nil {
		t.Fatal(err)
	}
	for name := range without {
		if strings.HasSuffix(name, "Table.ex") || strings.HasSuffix(name, "Block.ex") ||
			strings.HasSuffix(name, "Cook.ex") || name == "BuildVersion.ex" {
			t.Errorf("--lang elixir emitted %s for a table-free unit", name)
		}
	}
	// ZERO COST, and it is a gate rather than a hope (§2.2): adding a table
	// moves not one byte of the PACKET modules.
	for name, data := range without {
		got, ok := with[name]
		if !ok {
			t.Errorf("elixir module %s disappeared when a table was added", name)
			continue
		}
		if string(got) != string(data) {
			t.Errorf("elixir module %s changed when a table was added — tables must move no packet byte", name)
		}
	}
}

// TestElixirRefusesPointeredTables: the Elixir variable-class refusal is a
// refusal of the WIRE SURFACE and of nothing else (docs/SPEC-TABLES.md §11),
// exactly as the C# and Rust ones are. The wire codec is the half the variable
// class is missing — the arena, the builder, the region, the node table — and
// the two ACCELERATORS need none of it: a block and a cook are POINTED AT, not
// parsed.
func TestElixirRefusesPointeredTables(t *testing.T) {
	c := New()
	u := unitFromSource(t, packetSrc+`
table Node
{
    value int32
    next  *Node
}
`)
	files, err := c.Generate(u, "elixir", Options{})
	if err != nil {
		t.Fatalf("--lang elixir refused a pointered unit outright — the accelerators need no codec: %v", err)
	}
	cooks := 0
	for name, data := range files {
		if strings.HasSuffix(name, "Table.ex") || name == "TableRuntime.ex" {
			t.Errorf("--lang elixir emitted the WIRE surface %s for a pointered unit", name)
		}
		if strings.HasSuffix(name, "Cook.ex") && !strings.HasSuffix(name, "CookRuntime.ex") {
			cooks++
		}
		if strings.HasSuffix(name, "Cook.ex") || strings.HasSuffix(name, "Block.ex") ||
			name == "BuildVersion.ex" {
			// and the refusal RIDES on every file the unit does emit, so a
			// consumer meets it wherever they look
			if !strings.Contains(string(data), "REFUSED, BY NAME") {
				t.Errorf("%s carries no refusal banner for a pointered unit", name)
			}
		}
	}
	if cooks == 0 {
		t.Error("--lang elixir emitted no cook module for a pointered unit — a cook's root is one")
	}
}

// TestElixirRuntimeNamesAreClaimed is the Elixir half of the §11 promise, and
// its SCAN IS THE LANGUAGE'S OWN COLLISION CLASS rather than C#'s.
//
// An Elixir declaration lowers to a MODULE under the unit's namespace —
// `<Package>.<Name>` — so the names a schema can collide with are exactly the
// unit-level module segments the emitter defines, whatever they are spelled.
// A Table* prefix would have been blind to BlockRuntime, CookRuntime and
// BuildVersion, which are three of the four the Elixir backend actually
// defines; scanning for the segment finds any module the emitter grows,
// including one nobody thought to prefix.
//
// The segments a DECLARATION or a schema FILE produces are excluded, because
// those are the schema author's own names and their collisions are refused
// elsewhere (a declaration colliding with a file's module, and a generated
// filename claimed twice).
func TestElixirRuntimeNamesAreClaimed(t *testing.T) {
	u := unitFromSource(t, runtimeSrc)
	files, err := New().Generate(u, "elixir", Options{})
	if err != nil {
		t.Fatal(err)
	}
	namespace := ir.GoExportName(u.Package)

	// the author's own segments: every declaration, every union's generated
	// tag module, and every module a schema FILE produces
	mine := map[string]bool{}
	for name := range u.DeclFile {
		mine[name] = true
	}
	for _, un := range u.Unions {
		mine[un.Name+"Type"] = true
	}
	for name := range u.Tables {
		mine[name] = true
	}
	for _, f := range u.Files {
		for _, suffix := range []string{"", "Table", "Block", "Cook"} {
			mine[ir.GoExportName(f.Base)+suffix] = true
		}
	}

	segment := regexp.MustCompile(`\b` + namespace + `\.([A-Z][A-Za-z0-9_]*)`)
	emitted := map[string]bool{}
	for _, data := range files {
		for line := range strings.SplitSeq(string(data), "\n") {
			// a `#` comment is prose, not an identifier; scanning it would
			// make this gate a spelling police for the runtime's own
			// documentation
			if i := strings.Index(line, "#"); i >= 0 {
				line = line[:i]
			}
			for _, m := range segment.FindAllStringSubmatch(line, -1) {
				if !mine[m[1]] {
					emitted[m[1]] = true
				}
			}
		}
	}
	if len(emitted) == 0 {
		t.Fatal("the scan found no unit-level module segment in the emitted Elixir at all — the scan, not the registry, is what broke")
	}

	names := make([]string, 0, len(emitted))
	for name := range emitted {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !tablenames.Registered(name) {
			t.Errorf("the Elixir table emitter defines the unit-level module %s.%s and "+
				"internal/tablenames does not register it — a schema declaring that name would "+
				"generate Elixir that does not compile; register it (with the backends that "+
				"define it) in internal/tablenames", namespace, name)
		}
	}
	for _, name := range tablenames.DefinedBy(tablenames.Elixir) {
		if !emitted[name] {
			t.Errorf("internal/tablenames says the Elixir backend defines %s, but nothing in the "+
				"emitted Elixir names it — drop the registration or fix the backend; a claim "+
				"nothing needs takes a name away from every schema for free", name)
		}
	}
}

// TestElixirRuntimeNameCollisionRepro is the REPRO the scan above exists for,
// and its NEGATIVE CONTROL is the second half: a declaration named for a
// generated module is refused by the checker, and the same declaration in a
// TABLE-FREE unit is accepted — which is what says the claim is scoped to
// units that declare a table rather than taken from every schema.
func TestElixirRuntimeNameCollisionRepro(t *testing.T) {
	for _, name := range []string{"TableRuntime", "BlockRuntime", "CookRuntime", "BuildVersion"} {
		t.Run(name, func(t *testing.T) {
			refused := "package probe\n\nenum " + name + " { A, B }\n\ntable Holder\n{\n    g " + name + "\n}\n"
			errs := checkErrors(t, refused)
			if len(errs) == 0 {
				t.Fatalf("a declaration named %s was accepted — the Elixir table backend defines "+
					"the module probe.%s, so the unit cannot compile", name, name)
			}
			// the NEGATIVE CONTROL: the same name in a table-free unit is the
			// author's, and taking it there would be a claim nothing needs
			free := "package probe\n\nenum " + name + " { A, B }\n\ntype Holder\n{\n    g " + name + "\n}\n"
			if errs := checkErrors(t, free); len(errs) > 0 {
				t.Errorf("a TABLE-FREE unit must keep the name %s: %v", name, errs)
			}
		})
	}
}

// TestElixirRefusesFileModuleCollision: a declaration lowers to a MODULE under
// the unit's namespace, so one named for a generated file's module would merge
// two unrelated modules. The unit-level runtime names are the checker's claim;
// these three are derived from a schema FILE's own basename, which no
// unit-level registry can hold, so the backend refuses them by name.
func TestElixirRefusesFileModuleCollision(t *testing.T) {
	c := New()
	for _, suffix := range []string{"Table", "Block", "Cook"} {
		t.Run(suffix, func(t *testing.T) {
			u := unitFromSource(t, tableSrc+"\ntype Probe"+suffix+"\n{\n    x int32\n}\n")
			_, err := c.Generate(u, "elixir", Options{})
			if err == nil {
				t.Fatalf("--lang elixir accepted a declaration named Probe%s — it is the module the "+
					"backend writes for Probe.schema", suffix)
			}
			if !strings.Contains(err.Error(), "Probe"+suffix) || !strings.Contains(err.Error(), "Probe.schema") {
				t.Errorf("the refusal names neither the declaration nor the file: %v", err)
			}
		})
	}
	// and the same names in a TABLE-FREE unit are the author's: this backend
	// emits no such module for one, so nothing collides
	if _, err := c.Generate(unitFromSource(t, packetSrc+"\ntype ProbeTable\n{\n    x int32\n}\n"), "elixir", Options{}); err != nil {
		t.Errorf("a TABLE-FREE unit must keep the name ProbeTable: %v", err)
	}
}

// TestElixirModuleNamesAreAliases: a generated MODULE name is not a filename.
// An Elixir alias segment must begin upper-case, so `my_frame.schema` emits
// `my_frameTable.ex` — the packet emitter's own file convention — carrying
// `<Ns>.MyFrameTable`. Emitting the basename raw produced
// `defmodule Probe.my_frameTable`, which is an ArgumentError at compile time,
// and left this backend's own collision check (which already reads the exported
// form) naming a different module than the emitter wrote.
//
// The corpus is CamelCase throughout, which is exactly why nothing saw it.
func TestElixirModuleNamesAreAliases(t *testing.T) {
	u := unitFromNamedSource(t, "my_frame", packetSrc+`
table Holder
{
    x int32
    p Point
}
`)
	files, err := New().Generate(u, "elixir", Options{})
	if err != nil {
		t.Fatalf("--lang elixir: %v", err)
	}
	// the FILES keep the schema basename, as the packet emitter's do
	for _, want := range []string{"my_frameTable.ex", "my_frameBlock.ex", "my_frameCook.ex"} {
		if _, ok := files[want]; !ok {
			t.Errorf("no %s emitted; got %d files", want, len(files))
		}
	}
	// and every MODULE the output names is a legal alias: an upper-case first
	// letter after every dot
	segment := regexp.MustCompile(`\bProbe\.([A-Za-z0-9_]+)`)
	for name, data := range files {
		for _, m := range segment.FindAllStringSubmatch(string(data), -1) {
			first := m[1][0]
			if first < 'A' || first > 'Z' {
				t.Errorf("%s names the module Probe.%s — an Elixir alias segment must begin "+
					"upper-case, so this unit does not compile", name, m[1])
			}
		}
	}
	// the three the basename derives, spelled out so a rename of the helper
	// cannot make the check vacuous
	for _, want := range []string{"Probe.MyFrameTable", "Probe.MyFrameBlock", "Probe.MyFrameCook"} {
		found := false
		for _, data := range files {
			if strings.Contains(string(data), "defmodule "+want+" do") {
				found = true
			}
		}
		if !found {
			t.Errorf("no file declares %s", want)
		}
	}
}
