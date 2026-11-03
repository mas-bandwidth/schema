package goosetable_test

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goosetable"
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
	for _, key := range slicesSorted(units) {
		paths, err := compiler.GatherPaths(prefix(units[key]))
		if err != nil {
			t.Fatalf("unit %s: %v", key, err)
		}
		out = append(out, viewUnit{"manifest-" + key, paths})
	}
	return out
}

func slicesSorted(m map[string][]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// viewPrinter is the listing program, test/tables/view_goose.goosetext, for
// one unit.
func viewPrinter(t *testing.T, u *ir.Unit) string {
	t.Helper()
	text, err := os.ReadFile(filepath.Join(root, "test", "tables", "view_goose.goosetext"))
	if err != nil {
		t.Fatal(err)
	}
	s := strings.ReplaceAll(string(text), "VIEW_PACKAGE", goose.Namespace(u))
	return strings.ReplaceAll(s, "VIEW_MODULE", goosetable.ViewModule(u))
}

// generateView loads one unit and runs both Goose surfaces over it.
func generateView(t *testing.T, paths []string) (*ir.Unit, map[string][]byte, error) {
	t.Helper()
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	files, err := goose.Generate(u)
	if err != nil {
		return u, nil, err
	}
	tables, err := goosetable.Generate(u)
	if err != nil {
		return u, nil, err
	}
	maps.Copy(files, tables)
	return u, files, nil
}

// listView runs the listing program over generated files and returns what it
// printed.
func listView(t *testing.T, u *ir.Unit, files map[string][]byte, printer string) string {
	t.Helper()
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.goose"), []byte(printer), 0o644); err != nil {
		t.Fatal(err)
	}
	return string(gooserun.Run(t, dir, "main.goose", nil))
}

// firstDifference names the first line two listings disagree about.
func firstDifference(pin, printed string) string {
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
			return "line " + strconv.Itoa(i+1) + ":\n  compiler: " + w + "\n  goose:    " + g
		}
	}
	return "the listings differ in trailing bytes alone"
}

// TestGooseUnitViewCorpus is the CORPUS GATE for the Goose registry
// (docs/SPEC-TABLES.md §8.7). For every VIEW_CORPUS unit, and every unit of
// the tables conformance manifest, the listing a Goose program prints from
// UnitView() — with the generated modules and nothing else — is byte-identical
// to internal/viewlisting's listing of the unit's IR: the PIN the Go, C++, C,
// Rust, Java, JavaScript, Dart and Elixir registries are held to, so a unit
// listed equal here is listed equal to theirs. The listing carries every
// declaration, field, variant, bit, arm and constant with its file, id, text
// key, type spelling, doc and tags; the program also holds the two pairs the
// spec spells twice (a ViewType's annotation equal to its descriptor's, a
// general arm's field descriptor equal to its registry row) and every count
// column to its list, and prints a line the pin does not have when one
// disagrees. A unit the Goose target refuses has no view and is listed.
func TestGooseUnitViewCorpus(t *testing.T) {
	t.Parallel()
	units := viewUnits(t)
	refused := map[string]string{}
	listed := 0
	for _, vu := range units {
		u, files, err := generateView(t, vu.paths)
		if err != nil {
			refused[vu.key] = err.Error()
			continue
		}
		if len(u.Tables) == 0 {
			continue
		}
		listed++
		t.Run(vu.key, func(t *testing.T) {
			t.Parallel()
			pin := viewlisting.Listing(u)
			got := listView(t, u, files, viewPrinter(t, u))
			if got != pin {
				t.Errorf("unit view listing differs from internal/viewlisting.\n%s", firstDifference(pin, got))
			}
		})
	}
	for key, why := range refused {
		if strings.HasPrefix(key, "corpus-") {
			t.Errorf("VIEW_CORPUS unit %s is refused by the Goose target: %s", key, why)
			continue
		}
		t.Logf("goose refuses %s: %s", key, why)
	}
	if listed == 0 {
		t.Fatal("no unit was listed")
	}
	t.Logf("%d of %d units listed from UnitView(); %d refused", listed, len(units), len(refused))
}

// TestGooseUnitViewNegativeControl breaks each claim the corpus gate holds,
// one at a time in a copy of the generated text, and requires the gate RED on
// the line that claim owns (docs/SPEC-TABLES.md §8.7: a gate that cannot go
// red proves nothing):
//
//   - unflattened: a printer that writes a doc's newline verbatim splits the
//     exhibit's multi-line doc (tabledemo) across lines;
//   - drop: one declaration dropped from the registry's tables set;
//   - arm-offset: a general arm's offset moved one byte off the payload base
//     (armdemo's `plain` arm prints overlay=1);
//   - doc: one field's doc column emptied, where the pin has the text;
//   - id: one field of a type no table reaches given a table-wire id.
func TestGooseUnitViewNegativeControl(t *testing.T) {
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
		got := listView(t, u, files, printer)
		if got == pin {
			t.Fatal("NEGATIVE CONTROL FAILED: the sabotage left the listing equal to the pin")
		}
		diff := firstDifference(pin, got)
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
		anchor := `} else if c == '\n' { format(out, "\\n"); }`
		if !strings.Contains(printer, anchor) {
			t.Fatal("the flattening anchor is not in the printer")
		}
		red(t, u, files, strings.Replace(printer, anchor, `}`, 1), `\n`)
	})
	t.Run("drop", func(t *testing.T) {
		t.Parallel()
		u, files := load(t, "tables/examples")
		view := goosetable.ViewModule(u) + ".goose"
		patch(t, files, view, regexp.MustCompile(`let _ViewTables: ViewType\[(\d+)\] = \[\n    [^\n]*\n`), func(s string) string {
			n, _ := strconv.Atoi(regexp.MustCompile(`\[(\d+)\]`).FindStringSubmatch(s)[1])
			return fmt.Sprintf("let _ViewTables: ViewType[%d] = [\n", n-1)
		})
		patch(t, files, view, regexp.MustCompile(`num_tables: \d+`), func(s string) string {
			n, _ := strconv.Atoi(strings.TrimPrefix(s, "num_tables: "))
			return fmt.Sprintf("num_tables: %d", n-1)
		})
		red(t, u, files, viewPrinter(t, u), "table ")
	})
	t.Run("arm-offset", func(t *testing.T) {
		t.Parallel()
		u, files := load(t, "tables/arms")
		patch(t, files, "CarryTable.goose", regexp.MustCompile(`name: "plain",[^\n]*?arm_offset: \d+`), func(s string) string {
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
		docField := regexp.MustCompile(`type_name: "[^"\n]*", doc: "(?:[^"\\\n]|\\.)+"`)
		for name, data := range files {
			if strings.HasSuffix(name, "Table.goose") && docField.Match(data) {
				module = name
			}
		}
		if module == "" {
			t.Fatal("no table module carries a documented field")
		}
		patch(t, files, module, docField, func(s string) string {
			return s[:strings.LastIndex(s, `doc: "`)] + "doc: schema_rt::TableDocNone"
		})
		red(t, u, files, viewPrinter(t, u), "doc=")
	})
	t.Run("id", func(t *testing.T) {
		t.Parallel()
		u, files := load(t, "test/tables/CsView.schema")
		view := goosetable.ViewModule(u) + ".goose"
		patch(t, files, view, regexp.MustCompile(`FieldDesc \{ id: 0x0000000000000000, name: "grade"`), func(s string) string {
			return strings.Replace(s, "0x0000000000000000", "0x0000000000000001", 1)
		})
		red(t, u, files, viewPrinter(t, u), "Orphan field grade")
	})
}

// TestGooseUnitViewPacketStorage holds a type no table reaches to §8.2: it
// has a descriptor in the view module, its fields carry no table-wire
// identity (id 0, no text key), a vocabulary nothing in a table closure
// reaches carries id 0 on every variant, a reached type keeps its ids, and
// viewing a type adds no codec to it.
func TestGooseUnitViewPacketStorage(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	schema := `package probe

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
`
	if err := os.WriteFile(filepath.Join(dir, "Probe.schema"), []byte(schema), 0o644); err != nil {
		t.Fatal(err)
	}
	u, files, err := generateView(t, []string{filepath.Join(dir, "Probe.schema")})
	if err != nil {
		t.Fatal(err)
	}
	view := string(files["ProbeView.goose"])
	if view == "" {
		t.Fatal("the unit has no ProbeView.goose")
	}
	if !strings.Contains(view, "let PacketDescriptor: schema_rt::TableDesc") {
		t.Fatal("the type no table reaches has no descriptor in the view module")
	}
	for name, data := range files {
		if strings.Contains(string(data), "PacketSave") || strings.Contains(string(data), "PacketLoad") || strings.Contains(string(data), "PacketMeasure") {
			t.Errorf("%s: a viewed type gained a table codec", name)
		}
		if name != "ProbeView.goose" && regexp.MustCompile(`(?m)^(?:struct|fn|let) (?:UnitView|UnitViewInfo|ViewType|ViewVocabulary|ViewVariant|ViewConstant)\b`).Match(data) {
			t.Errorf("%s: a registry symbol is declared outside the view module (docs/SPEC-TABLES.md §8.4)", name)
		}
	}
	main := `import .ProbeView;

var bad: i64 = 0;
fn check(ok: bool, what: const u8[:]) { if !ok { print("BAD ", what); bad += 1; } }

fn main() {
    let v .= probe::UnitView();
    let p .= probe::PacketDescriptor;
    check(p.fields.len == 3, "Packet has three fields");
    check(p.fields[0].id == 0 && p.fields[0].json.len == 0, "an outside field carries no wire identity");
    check(p.fields[2].id == 0 && p.fields[2].json.len == 0, "an outside text field carries no wire identity");
    check(p.fields[1].variants[0].id == 0 && p.fields[1].variants[1].id == 0, "an unreached enum's variants carry id 0");
    let s .= probe::SharedDescriptor;
    check(s.fields[1].id != 0 && s.fields[1].json.len > 0, "a reached type keeps its wire identity");
    check(v.num_types == 3 && v.num_tables == 1 && v.num_enums == 1 && v.num_unions == 1, "registry counts");
    check(v.enums[0].variants.len == 3 && v.enums[0].variants[1].id == 0 && v.enums[0].variants[2].id == 0, "an unreached enum's registry rows carry id 0");
    check(v.unions[0].variants[1].id != 0, "a reached union's arms keep their ids");
    let a = v.unions[0].variants[1].payload;
    guard a else { check(false, "a record arm names its payload descriptor"); return; }
    check(a.name.len == 5, "the arm's payload is Child's descriptor");
    let d = v.types[1].descriptor;
    guard d else { check(false, "every type row names a descriptor"); return; }
    check(d.fields.len == 3, "the Packet row names Packet's descriptor");
    if bad == 0 { print("OK"); }
}
`
	run := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(run, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(run, "main.goose"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(gooserun.Run(t, run, "main.goose", nil))); got != "OK" {
		t.Fatalf("packet storage probe printed:\n%s", got)
	}
	if u.Package != "probe" {
		t.Fatalf("package %s", u.Package)
	}
}

// TestGooseTagListNamesDoNotCollide holds the tag-list globals to the one
// thing a namespace-level name has to be: unique. A table Ship with a tagged
// field config and a tagged table ShipConfig are two rows whose owner and
// member spellings concatenate to the same text, so the ordinal in a field's
// list name is what keeps the unit compiling.
func TestGooseTagListNamesDoNotCollide(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	schema := `package probe

fixed table ShipConfig | outer
{
    scale float32 = 1.0
}

fixed table Ship
{
    config ShipConfig | inner
}
`
	if err := os.WriteFile(filepath.Join(dir, "Probe.schema"), []byte(schema), 0o644); err != nil {
		t.Fatal(err)
	}
	_, files, err := generateView(t, []string{filepath.Join(dir, "Probe.schema")})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]int{}
	for name, data := range files {
		if _, ok := goosetable.Runtime()[name]; ok {
			continue
		}
		for _, m := range regexp.MustCompile(`(?m)^let (_\w*(?:Tags|_t\d+)): `).FindAllSubmatch(data, -1) {
			seen[string(m[1])]++
		}
	}
	if len(seen) != 2 {
		t.Fatalf("expected two tag lists, got %v", seen)
	}
	for name, n := range seen {
		if n != 1 {
			t.Fatalf("tag list %s is declared %d times: two rows share one namespace-level name", name, n)
		}
	}
	main := `import .ProbeView;

fn main() {
    let c .= probe::ShipConfigDescriptor;
    let s .= probe::ShipDescriptor;
    if c.num_tags == 1 && c.tags[0][0] == 'o' && s.fields[0].num_tags == 1 && s.fields[0].tags[0][0] == 'i' { print("OK"); }
}
`
	run := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(run, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(run, "main.goose"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(gooserun.Run(t, run, "main.goose", nil))); got != "OK" {
		t.Fatalf("tag probe printed:\n%s", got)
	}
}

// TestGooseEmptyDocIsOneDefinition is §8.7's ABSENCE IS ONE SHARED EMPTY
// STRING for a language with no address identity: the observable is the
// emitted TEXT. Over every corpus unit, the runtime defines the empty doc
// once (schema_rt::TableDocNone), every row with no doc names it, and no
// generated module writes an empty doc literal or an empty tag list of its
// own. Its control writes one row's doc as an inline empty literal and
// requires the scan red.
func TestGooseEmptyDocIsOneDefinition(t *testing.T) {
	t.Parallel()
	runtime := string(goosetable.Runtime()[goosetable.RuntimeModule+".goose"])
	if n := strings.Count(runtime, "let TableDocNone: const u8[:] = \"\";"); n != 1 {
		t.Fatalf("the runtime defines the empty doc %d times", n)
	}
	scan := func(files map[string][]byte) (refs int, inline []string) {
		for name, data := range files {
			if _, ok := goosetable.Runtime()[name]; ok {
				continue
			}
			refs += strings.Count(string(data), "doc: schema_rt::TableDocNone")
			if strings.Contains(string(data), `doc: ""`) || strings.Contains(string(data), "tags: []") {
				inline = append(inline, name)
			}
		}
		return refs, inline
	}
	total := 0
	var sample map[string][]byte
	for _, vu := range viewUnits(t) {
		u, files, err := generateView(t, vu.paths)
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
		t.Fatal("no row names schema_rt::TableDocNone; the scan is reading the wrong text")
	}
	// the control: one row's absent doc written as its own empty literal
	broken := map[string][]byte{}
	maps.Copy(broken, sample)
	for name, data := range broken {
		if strings.Contains(string(data), "doc: schema_rt::TableDocNone") {
			broken[name] = []byte(strings.Replace(string(data), "doc: schema_rt::TableDocNone", `doc: ""`, 1))
			break
		}
	}
	if _, inline := scan(broken); len(inline) != 1 {
		t.Fatalf("NEGATIVE CONTROL FAILED: an inline empty doc was not seen (%v)", inline)
	}
	t.Logf("%d rows name the one empty doc", total)
}
