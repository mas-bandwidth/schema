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

// TestElixirEmitsTableModules: the elixir target adds the BLOCK and COOK read
// halves, <Base>Block.ex and <Base>Cook.ex with their runtime modules and the
// unit's BuildVersion, beside the packet ones for a unit with tables, and adds
// NOTHING for a unit without. It emits no <Base>Table.ex and no
// TableRuntime.ex at all: the table wire's Elixir port wrote the form that
// preceded the id-table wire and was removed rather than carried (schema#515
// brings the current wire to Elixir).
func TestElixirEmitsTableModules(t *testing.T) {
	c := New()
	with, err := c.Generate(unitFromSource(t, tableSrc), "elixir", Options{})
	if err != nil {
		t.Fatalf("--lang elixir: %v", err)
	}
	for _, want := range []string{"ProbeBlock.ex", "ProbeCook.ex", "BuildVersion.ex", "BlockRuntime.ex", "CookRuntime.ex"} {
		if _, ok := with[want]; !ok {
			t.Errorf("--lang elixir emitted no %s for a unit with tables; got %d files", want, len(with))
		}
	}
	for name := range with {
		if strings.HasSuffix(name, "Table.ex") || name == "TableRuntime.ex" {
			t.Errorf("--lang elixir emitted %s: the previous-form table wire was removed and nothing emits it", name)
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

// TestElixirRuntimeNamesAreClaimed is the Elixir half of the §11 promise, and
// its SCAN IS THE LANGUAGE'S OWN COLLISION CLASS rather than C#'s.
//
// An Elixir declaration lowers to a MODULE under the unit's namespace —
// `<Package>.<Name>` — so the names a schema can collide with are exactly the
// unit-level module segments the emitter defines, whatever they are spelled.
// A Table* prefix would have been blind to BlockRuntime, CookRuntime and
// BuildVersion, which are the three the Elixir backend actually defines;
// scanning for the segment finds any module the emitter grows, including one
// nobody thought to prefix.
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
		// "Fixed" is the FIXED FORM's per-file module (docs/SPEC-TABLES.md
		// §3.4), which is FILE-derived like Block and Cook: no unit-level
		// registry can hold a name a schema file's basename makes, so the
		// emitter refuses the collision itself (TestElixirRefusesFileModuleCollision)
		// and this scan treats it as the author's own segment.
		for _, suffix := range []string{"", "Table", "Block", "Cook", "Fixed"} {
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

// TestElixirRuntimeNameCollisionRepro is the REPRO the scan above exists for:
// a declaration named for a generated module is refused by the checker in a
// unit that declares a table, because that is where the backend writes the
// module. None of the three is view surface: the descriptors are the view
// file's, and these are the block and cook runtimes and the build version, so
// a TABLE-FREE unit keeps every one of them (docs/SPEC-TABLES.md §11,
// schema#672).
func TestElixirRuntimeNameCollisionRepro(t *testing.T) {
	for _, name := range []string{"BlockRuntime", "CookRuntime", "BuildVersion"} {
		t.Run(name, func(t *testing.T) {
			refused := "package probe\n\nenum " + name + " { A, B }\n\ntable Holder\n{\n    g " + name + "\n}\n"
			errs := checkErrors(t, refused)
			if len(errs) == 0 {
				t.Fatalf("a declaration named %s was accepted — the Elixir table backend defines "+
					"the module probe.%s, so the unit cannot compile", name, name)
			}
			if tablenames.InEveryUnit(name) {
				t.Fatalf("%s is marked view surface: this repro is about the runtime modules, and a "+
					"view-surface name is claimed in every unit instead", name)
			}
			// and a table-free unit keeps it: no view file spells any of the
			// three, and the modules that do are written into table sources
			// that unit never gets
			free := "package probe\n\nenum " + name + " { A, B }\n\ntype Holder\n{\n    g " + name + "\n}\n"
			if errs := checkErrors(t, free); len(errs) > 0 {
				t.Errorf("a TABLE-FREE unit declaring %s was refused: %v", name, errs)
			}
		})
	}
}

// TestElixirRefusesFileModuleCollision: a declaration lowers to a MODULE under
// the unit's namespace, so one named for a generated file's module would merge
// two unrelated modules. The unit-level runtime names are the checker's claim;
// these two are derived from a schema FILE's own basename, which no
// unit-level registry can hold, so the backend refuses them by name.
func TestElixirRefusesFileModuleCollision(t *testing.T) {
	c := New()
	for _, suffix := range []string{"Block", "Cook", "Fixed"} {
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
	if _, err := c.Generate(unitFromSource(t, packetSrc+"\ntype ProbeBlock\n{\n    x int32\n}\n"), "elixir", Options{}); err != nil {
		t.Errorf("a TABLE-FREE unit must keep the name ProbeBlock: %v", err)
	}
}

// TestElixirModuleNamesAreAliases: a generated MODULE name is not a filename.
// An Elixir alias segment must begin upper-case, so `my_frame.schema` emits
// `my_frameBlock.ex` — the packet emitter's own file convention — carrying
// `<Ns>.MyFrameBlock`. Emitting the basename raw produced
// `defmodule Probe.my_frameBlock`, which is an ArgumentError at compile time,
// and left this backend's own collision check (which already reads the exported
// form) naming a different module than the emitter wrote.
//
// The corpus is CamelCase throughout, which is exactly why nothing saw it.
func TestElixirModuleNamesAreAliases(t *testing.T) {
	u := unitFromNamedSource(t, "my_frame", packetSrc+`
fixed table Holder
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
	for _, want := range []string{"my_frameBlock.ex", "my_frameCook.ex"} {
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
	// the two the basename derives, spelled out so a rename of the helper
	// cannot make the check vacuous
	for _, want := range []string{"Probe.MyFrameBlock", "Probe.MyFrameCook"} {
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

// ---- THE FIXED FORM, form byte 3 (docs/SPEC-TABLES.md §3.4) ----------------

// elixirFixedSrc reaches every shape §3.4's constant-size table names that this
// port lays out: a leaf of every width, a bool, an enum, a flags mask, text of
// all three flavours, a fixed array, a counted array, a nested table, an
// enum-keyed array, an optional and a union.
//
// NO `wstring(N)`: compiler/widetext.go refuses wide text in a TABLE closure for
// every target but C, C++, C# and Go, and this leg does not move that refusal —
// the fixed form's LAYOUT and PLAN carry kind 33 so a PEER's wide text reads
// here, but a declaration of one is still the named follow-on it already was.
const elixirFixedSrc = `package probe

enum Slot { Alpha, Beta, Gamma }

flags Mark { One, Two }

table Leaf
{
    a int32 = 7 | min = 0, max = 1000
    b uint16 = 3
}

type Boost
{
    power int32 = 2
}

type Ward
{
    charge float32 = 1.5
}

union Effect
{
    boost Boost
    ward  Ward
}

table FixedProbe
{
    small    uint8 = 5
    wide     uint64 = 9
    negative int16 = -4
    fl       float32 = 2.5
    db       float64 = 1.25
    yes      bool = true
    slot     Slot = Beta
    mark     Mark
    label    string(12)
    raw      bytes(6)
    trio     [3]uint32
    counted  [1..4]Leaf
    nested   Leaf
    perslot  [Slot]Leaf
    maybe    ?Leaf
    effect   Effect
}
`

// TestElixirFixedFormLayoutIsTheCppReferenceByteForByte is what keeps this
// port's §3.4 layout walk from becoming a SECOND WIRE.
//
// The LAYOUT is the form's whole self-description and its fnv1a64 is the eight
// bytes every record carries, so two backends whose layouts differ anywhere are
// two backends that never read each other's records — silently, as `no_layout`,
// which looks like a deployment problem and is not one. The C++ backend is the
// REFERENCE for this form (internal/codegen/cpptable/fixedform.go), so this
// generates the SAME unit for both and requires the bytes and the hash to be
// identical, per root.
//
// It is the both-directions form the registry scans already use: every root
// Elixir emits a layout for is compared against the reference's, and the
// comparison set has to be non-empty or the test, not the emitter, is what
// broke.
func TestElixirFixedFormLayoutIsTheCppReferenceByteForByte(t *testing.T) {
	cppFiles, err := New().Generate(unitFromSource(t, elixirFixedSrc), "cpp", Options{})
	if err != nil {
		t.Fatalf("--lang cpp: %v", err)
	}
	exFiles, err := New().Generate(unitFromSource(t, elixirFixedSrc), "elixir", Options{})
	if err != nil {
		t.Fatalf("--lang elixir: %v", err)
	}
	cppLayouts, cppHashes := cppFixedLayoutsSnake(joinFiles(cppFiles))
	exLayouts, exHashes := elixirFixedLayouts(joinFiles(exFiles))
	if len(exLayouts) == 0 {
		t.Fatal("the Elixir backend emitted no fixed-form layout at all for a unit of fixed tables — " +
			"the scan, or the emitter, is what broke")
	}
	names := make([]string, 0, len(exLayouts))
	for name := range exLayouts {
		names = append(names, name)
	}
	sort.Strings(names)
	compared := 0
	for _, name := range names {
		want, ok := cppLayouts[name]
		if !ok {
			t.Errorf("the Elixir backend emits a fixed-form layout for %s and the C++ REFERENCE does not — "+
				"a port may carry fewer shapes than the reference, never more", name)
			continue
		}
		compared++
		if want != exLayouts[name] {
			t.Errorf("%s's layout differs from the C++ reference's:\n  cpp    %s\n  elixir %s",
				name, want, exLayouts[name])
		}
		if cppHashes[name] != exHashes[name] {
			t.Errorf("%s's fixed-form hash differs from the C++ reference's: cpp %s, elixir %s — "+
				"a record written by one is `no_layout` to the other", name, cppHashes[name], exHashes[name])
		}
	}
	if compared == 0 {
		t.Fatal("no root was compared against the reference at all")
	}
}

// joinFiles, cppFixedLayoutRe and cppFixedByteRe are the Java leg's
// (compiler/tablesjava_test.go); they are one package and the scan of the C++
// reference is the same scan. Only the SNAKE-keyed rendering below is this
// leg's own, so only it is spelled separately.
var (
	// The Java leg's cppFixedHashRe captures the `0x` with the digits; this
	// scan wants the digits alone, so it keeps its own pattern.
	cppFixedHashSnakeRe = regexp.MustCompile(`constexpr uint64_t (\w+)FixedHash = 0x([0-9a-f]+)ull;`)
	exFixedLayoutRe     = regexp.MustCompile(`@(\w+)_layout "((?:\\x[0-9A-F]{2})+)"`)
	exFixedHashRe       = regexp.MustCompile(`@(\w+)_hash 0x([0-9A-F]{16})`)
)

// The two scans key on the SNAKE spelling, which is the one shape both targets
// map onto without either one's naming rules leaking into the other's.
func cppFixedLayoutsSnake(text string) (map[string]string, map[string]string) {
	layouts, hashes := map[string]string{}, map[string]string{}
	for _, m := range cppFixedLayoutRe.FindAllStringSubmatch(text, -1) {
		bytes := cppFixedByteRe.FindAllString(m[2], -1)
		layouts[ir.RustSnake(m[1])] = strings.ToLower(strings.Join(bytes, " "))
	}
	for _, m := range cppFixedHashSnakeRe.FindAllStringSubmatch(text, -1) {
		hashes[ir.RustSnake(m[1])] = strings.ToLower(m[2])
	}
	return layouts, hashes
}

func elixirFixedLayouts(text string) (map[string]string, map[string]string) {
	layouts, hashes := map[string]string{}, map[string]string{}
	for _, m := range exFixedLayoutRe.FindAllStringSubmatch(text, -1) {
		var bytes []string
		for esc := range strings.SplitSeq(m[2], `\x`) {
			if esc != "" {
				bytes = append(bytes, "0x"+strings.ToLower(esc))
			}
		}
		layouts[m[1]] = strings.Join(bytes, " ")
	}
	for _, m := range exFixedHashRe.FindAllStringSubmatch(text, -1) {
		hashes[m[1]] = strings.ToLower(strings.TrimLeft(m[2], "0"))
	}
	return layouts, hashes
}
