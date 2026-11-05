package mootable_test

import (
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/moo/moorun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/mootable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/viewlisting"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// viewUnit is one unit the view gate lists: a VIEW_CORPUS entry or a
// conformance manifest unit.
type viewUnit struct {
	key   string
	paths []string
}

// viewUnits is THE VIEW CORPUS (docs/SPEC-TABLES.md §8.7), read from the
// Makefile's VIEW_CORPUS as internal/viewlisting reads it, followed by every
// unit of testdata/conformance/tables/MANIFEST.txt.
func viewUnits(t *testing.T) []viewUnit {
	t.Helper()
	makefile, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	entries := viewlisting.CorpusEntries(string(makefile))
	if len(entries) == 0 {
		t.Fatal("the Makefile declares no VIEW_CORPUS entries")
	}
	var out []viewUnit
	for _, entry := range entries {
		dir, _, ok := strings.Cut(entry, ":")
		if !ok {
			t.Fatalf("VIEW_CORPUS entry %q is not dir:package", entry)
		}
		source := filepath.Join(root, "tables", dir)
		if dir == "wide" {
			source = filepath.Join(root, "examples-wide")
		}
		paths, err := compiler.GatherPaths([]string{source})
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, viewUnit{"corpus-" + dir, paths})
	}
	units, _ := readManifest(t)
	for _, key := range sortedKeys(units) {
		paths, err := compiler.GatherPaths(prefix(units[key]))
		if err != nil {
			t.Fatalf("unit %s: %v", key, err)
		}
		out = append(out, viewUnit{"manifest-" + key, paths})
	}
	return out
}

// viewPrinter is the listing program, test/tables/view_moo.mootext, for one
// unit.
func viewPrinter(t *testing.T, u *ir.Unit) string {
	t.Helper()
	text, err := os.ReadFile(filepath.Join(root, "test", "tables", "view_moo.mootext"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(text), "VIEW_MODULE", mootable.ViewModule(u))
}

// generateView loads one unit and runs both moo surfaces over it.
func generateView(t *testing.T, paths []string) (*ir.Unit, map[string][]byte, error) {
	t.Helper()
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	files, err := generate(u)
	return u, files, err
}

// listView runs the listing program over generated files and returns what it
// printed.
func listView(t *testing.T, files map[string][]byte, printer string) string {
	t.Helper()
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.moo"), []byte(printer), 0o644); err != nil {
		t.Fatal(err)
	}
	return string(moorun.Run(t, dir, "main.moo", nil))
}

// viewFirstDifference names the first line two listings disagree about.
func viewFirstDifference(pin, printed string) string {
	want, got := strings.Split(pin, "\n"), strings.Split(printed, "\n")
	for i := 0; i < len(want) || i < len(got); i++ {
		w, g := "", ""
		if i < len(want) {
			w = want[i]
		}
		if i < len(got) {
			g = got[i]
		}
		if w != g {
			return "line " + strconv.Itoa(i+1) + ":\n  compiler: " + w + "\n  moo:      " + g
		}
	}
	return "the listings differ in trailing bytes alone"
}

// TestMooUnitViewCorpus is the CORPUS GATE for the moo registry
// (docs/SPEC-TABLES.md §8.7). For every VIEW_CORPUS unit, and every unit of
// the tables conformance manifest, the listing a moo program prints from
// UnitView() — with the generated modules and nothing else — is byte-identical
// to internal/viewlisting's listing of the unit's IR: the PIN every other
// registry is held to, so a unit listed equal here is listed equal to theirs.
// The listing carries every declaration, field, variant, bit, arm and constant
// with its file, id, text key, type spelling, doc and tags; the program also
// holds the two pairs the spec spells twice (a ViewType's annotation equal to
// its descriptor's, a general arm's field descriptor equal to its registry
// row) and prints a line the pin does not have when one disagrees. A unit the
// moo target refuses has no view and is listed.
func TestMooUnitViewCorpus(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the unit view's listing program)")
	t.Parallel()
	units := viewUnits(t)
	refusedBy := map[string]string{}
	listed := 0
	for _, vu := range units {
		u, files, err := generateView(t, vu.paths)
		if err != nil {
			refusedBy[vu.key] = err.Error()
			continue
		}
		if len(u.Tables) == 0 {
			continue
		}
		listed++
		t.Run(vu.key, func(t *testing.T) {
			t.Parallel()
			pin := viewlisting.Listing(u)
			got := listView(t, files, viewPrinter(t, u))
			if got != pin {
				t.Errorf("unit view listing differs from internal/viewlisting.\n%s", viewFirstDifference(pin, got))
			}
		})
	}
	for key, why := range refusedBy {
		if strings.HasPrefix(key, "corpus-") {
			t.Errorf("VIEW_CORPUS unit %s is refused by the moo target: %s", key, why)
			continue
		}
		t.Logf("moo refuses %s: %s", key, why)
	}
	if listed == 0 {
		t.Fatal("no unit was listed")
	}
	t.Logf("%d of %d units listed from UnitView(); %d refused", listed, len(units), len(refusedBy))
}

// TestMooUnitViewNegativeControl breaks each claim the corpus gate holds, one
// at a time in a copy of the generated text, and requires the gate RED on the
// line that claim owns (docs/SPEC-TABLES.md §8.7: a gate that cannot go red
// proves nothing):
//
//   - unflattened: a printer that writes a doc's newline verbatim splits the
//     exhibit's multi-line doc (tabledemo) across lines;
//   - drop: one declaration dropped from the registry's tables set;
//   - arm-offset: a general arm's offset moved one byte off the payload base
//     (armdemo's `plain` arm prints overlay=1);
//   - doc: one field's doc column emptied, where the pin has the text;
//   - id: one field of a type no table reaches given a table-wire id.
func TestMooUnitViewNegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the unit view's listing program)")
	t.Parallel()
	load := func(t *testing.T, dir string) (*ir.Unit, map[string][]byte) {
		paths, err := compiler.GatherPaths([]string{filepath.Join(root, dir)})
		if err != nil {
			t.Fatal(err)
		}
		u, files, err := generateView(t, paths)
		if err != nil {
			t.Fatal(err)
		}
		return u, files
	}
	red := func(t *testing.T, u *ir.Unit, files map[string][]byte, printer, expect string) {
		t.Helper()
		pin := viewlisting.Listing(u)
		got := listView(t, files, printer)
		if got == pin {
			t.Fatal("NEGATIVE CONTROL FAILED: the sabotage left the listing equal to the pin")
		}
		diff := viewFirstDifference(pin, got)
		if !strings.Contains(diff, expect) {
			t.Fatalf("NEGATIVE CONTROL FAILED: the gate went red, but not on %q:\n%s", expect, diff)
		}
		t.Logf("red as required:\n%s", diff)
	}
	patch := func(t *testing.T, files map[string][]byte, file string, re *regexp.Regexp, repl func(string) string) {
		t.Helper()
		data, ok := files[file]
		if !ok {
			t.Fatalf("no generated %s", file)
		}
		loc := re.FindIndex(data)
		if loc == nil {
			t.Fatalf("the sabotage anchor %s is not in %s", re, file)
		}
		t.Logf("sabotage in %s: %s", file, data[loc[0]:loc[1]])
		files[file] = []byte(string(data[:loc[0]]) + repl(string(data[loc[0]:loc[1]])) + string(data[loc[1]:]))
	}
	t.Run("unflattened", func(t *testing.T) {
		t.Parallel()
		u, files := load(t, "tables/examples")
		printer := viewPrinter(t, u)
		anchor := `flat = string.gsub(flat, "\n", "\\n")`
		if !strings.Contains(printer, anchor) {
			t.Fatal("the flattening anchor is not in the printer")
		}
		red(t, u, files, strings.Replace(printer, anchor, ``, 1), `\n`)
	})
	t.Run("drop", func(t *testing.T) {
		t.Parallel()
		u, files := load(t, "tables/examples")
		patch(t, files, mootable.ViewModule(u)+".moo", regexp.MustCompile(`\ttables = \{\n\t\t[^\n]*\n`), func(string) string {
			return "\ttables = {\n"
		})
		red(t, u, files, viewPrinter(t, u), "table ")
	})
	t.Run("arm-offset", func(t *testing.T) {
		t.Parallel()
		u, files := load(t, "tables/arms")
		patch(t, files, "CarryTable.moo", regexp.MustCompile(`name = "plain",[^\n]*?arm_offset = \d+`), func(s string) string {
			i := strings.LastIndex(s, " ")
			n, _ := strconv.Atoi(s[i+1:])
			return s[:i+1] + strconv.Itoa(n+1)
		})
		red(t, u, files, viewPrinter(t, u), "overlay=1")
	})
	t.Run("doc", func(t *testing.T) {
		t.Parallel()
		u, files := load(t, "tables/examples")
		module := ""
		docField := regexp.MustCompile(`type_name = "[^"\n]*", doc = "(?:[^"\\\n]|\\.)+"`)
		for _, name := range sortedKeys(files) {
			if strings.HasSuffix(name, "Table.moo") && docField.Match(files[name]) {
				module = name
				break
			}
		}
		if module == "" {
			t.Fatal("no table module carries a documented field")
		}
		patch(t, files, module, docField, func(s string) string {
			return s[:strings.LastIndex(s, `doc = "`)] + "doc = St.DocNone"
		})
		red(t, u, files, viewPrinter(t, u), "doc=")
	})
	t.Run("id", func(t *testing.T) {
		t.Parallel()
		u, files := load(t, "test/tables/CsView.schema")
		patch(t, files, mootable.ViewModule(u)+".moo", regexp.MustCompile(`\{ id = "(?:\\x00){8}", name = "grade"`), func(s string) string {
			return strings.Replace(s, `\x00`, `\x01`, 1)
		})
		red(t, u, files, viewPrinter(t, u), "Orphan field grade")
	})
}

// TestMooUnitViewPacketStorage holds a type no table reaches to §8.2: it has
// a descriptor in the view module, its fields carry no table-wire identity (id
// 0, no text key), a vocabulary nothing in a table closure reaches carries id
// 0 on every variant, a reached type keeps its ids, viewing a type adds no
// codec to it, and nothing outside the view module requires it (§8.4).
func TestMooUnitViewPacketStorage(t *testing.T) {
	slowtest.Gate(t, "moo (it runs a probe over the unit view)")
	t.Parallel()
	g := generateFiles(t, map[string]string{"Probe.schema": `package probe

enum Hidden { A, B }

type Child
{
    n int32 = 7
}

union Choice
{
    a Child
    b Child
}

type Shared
{
    choice Choice
    past uint32 = 19
}

type Packet
{
    nested Shared
    hidden Hidden
    text string(8) = "ready"
}

table Root
{
    shared Shared
    next *Root
}
`})
	view := string(g.files["ProbeView.moo"])
	if view == "" {
		t.Fatal("the unit has no ProbeView.moo")
	}
	if !strings.Contains(view, "M.PacketDescriptor = St.table({") {
		t.Fatal("the type no table reaches has no descriptor in the view module")
	}
	for name, data := range g.files {
		if strings.Contains(string(data), "PacketSave") || strings.Contains(string(data), "PacketLoad") {
			t.Errorf("%s: a viewed type gained a table codec", name)
		}
		if name != "ProbeView.moo" && strings.Contains(string(data), "ProbeView") {
			t.Errorf("%s: names the view module, which is on the side (docs/SPEC-TABLES.md §8.4)", name)
		}
	}
	out := g.run(t, `local V = require("./ProbeView")
local St = require("./schema_tables")
local zero = string.rep("\0", 8)
local bad = 0
local function check(ok: boolean, what: string)
	if not ok then
		print("BAD " .. what)
		bad += 1
	end
end
local v = V.UnitView()
local p = V.PacketDescriptor
check(#p.fields == 3, "Packet has three fields")
check(p.fields[1].id == zero and p.fields[1].json == "", "an outside field carries no wire identity")
check(p.fields[3].id == zero and p.fields[3].json == "", "an outside text field carries no wire identity")
check(p.fields[2].vs[1][2] == zero and p.fields[2].vs[2][2] == zero, "an unreached enum's variants carry id 0")
check(St.lookup("Packet") == p, "the outside descriptor is registered")
local s = St.lookup("Shared")
check(s.fields[2].id ~= zero and s.fields[2].json ~= "", "a reached type keeps its wire identity")
check(#v.types == 3 and #v.tables == 1 and #v.enums == 1 and #v.unions == 1, "registry counts")
check(#v.enums[1].variants == 3 and v.enums[1].variants[2].id == zero and v.enums[1].variants[3].id == zero, "an unreached enum's registry rows carry id 0")
check(v.unions[1].variants[2].id ~= zero, "a reached union's arms keep their ids")
check(v.unions[1].variants[2].payload == St.lookup("Child"), "a record arm names its payload descriptor")
check(v.types[2].descriptor == p, "the Packet row names Packet's descriptor")
check(table.isfrozen(v) and table.isfrozen(v.types[1]), "the registry is constant data")
if bad == 0 then
	print("OK")
end
`, nil)
	if out != "OK" {
		t.Fatalf("packet storage probe printed:\n%s", out)
	}
}

// TestMooEmptyDocIsOneDefinition is §8.7's ABSENCE IS ONE SHARED EMPTY
// STRING, held on the emitted TEXT. Over every corpus unit, the runtime
// defines the empty doc once (St.DocNone), every row with no doc names it,
// and no generated module writes an empty doc literal or an empty tag list of
// its own. Its control writes one row's doc as an inline empty literal and
// requires the scan red.
func TestMooEmptyDocIsOneDefinition(t *testing.T) {
	t.Parallel()
	runtime := string(mootable.Runtime()[mootable.RuntimeModule+".moo"])
	if n := strings.Count(runtime, `S.DocNone = ""`); n != 1 {
		t.Fatalf("the runtime defines the empty doc %d times", n)
	}
	scan := func(files map[string][]byte) (refs int, inline []string) {
		for _, name := range sortedKeys(files) {
			if _, ok := mootable.Runtime()[name]; ok || !strings.HasSuffix(name, "Table.moo") && !strings.HasSuffix(name, "View.moo") {
				continue
			}
			data := string(files[name])
			refs += strings.Count(data, "doc = St.DocNone")
			if strings.Contains(data, `doc = ""`) || strings.Contains(data, "tags = {  }") || strings.Contains(data, "tags = {}") {
				inline = append(inline, name)
			}
		}
		return refs, inline
	}
	total := 0
	var sample map[string][]byte
	for _, vu := range viewUnits(t) {
		u, err := compiler.New().Load(vu.paths)
		if err != nil {
			t.Fatalf("%s: %v", vu.key, err)
		}
		files, err := mootable.Generate(u)
		if err != nil || len(u.Tables) == 0 {
			continue
		}
		refs, inline := scan(files)
		if len(inline) > 0 {
			t.Errorf("unit %s: %v write an empty doc or tag list inline rather than the one definition", vu.key, inline)
		}
		total += refs
		if sample == nil && refs > 0 {
			sample = files
		}
	}
	if total == 0 || sample == nil {
		t.Fatal("no row names St.DocNone; the scan is reading the wrong text")
	}
	// the control: one row's absent doc written as its own empty literal
	broken := map[string][]byte{}
	maps.Copy(broken, sample)
	for _, name := range sortedKeys(broken) {
		if strings.Contains(string(broken[name]), "doc = St.DocNone") {
			broken[name] = []byte(strings.Replace(string(broken[name]), "doc = St.DocNone", `doc = ""`, 1))
			break
		}
	}
	if _, inline := scan(broken); len(inline) != 1 {
		t.Fatalf("NEGATIVE CONTROL FAILED: an inline empty doc was not seen (%v)", inline)
	}
	t.Logf("%d rows name the one empty doc", total)
}
