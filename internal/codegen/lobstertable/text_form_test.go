package lobstertable_test

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// textSchema is this card's probe unit: the kinds the first slice of the
// hand-editable text form carries (docs/SPEC-TABLES.md §16.9.2): string(N),
// a ranged int, bool and a bounded array of int.
const textSchema = `package textdemo

table Config
{
    name    string(16)
    count   int32 | min = 0, max = 10
    enabled bool
    tags    [..3]int32
}
`

const textInput = `(:name "ship" :count 3 :enabled true :tags (1 2))`
const textJSON = `{"name":"ship","count":3,"enabled":true,"tags":[1,2]}`

// textSchema2 is this card's second probe unit: the kinds the second slice
// of the hand-editable text form carries (docs/SPEC-TABLES.md §16.9.2) —
// float32, float64, bytes(N) as padded base64, an optional string, and a
// fixed array.
const textSchema2 = `package textdemo2

table Config
{
    ratio    float32
    weight   float64
    blob     bytes(4)
    note     ?int32
    pair     [2]int32
}
`

const textInput2 = `(:ratio 1.5 :weight 2.25 :blob "AP8=" :note 42 :pair (7 8))`
const textJSON2 = `{"ratio":1.5,"weight":2.25,"blob":"AP8=","note":42,"pair":[7,8]}`

// textSchema3 is the third probe unit: the remaining §16.9.2 shapes this
// slice carries — a 128-bit integer each signedness, a fixed value on its
// grid, a flags mask, an enum-keyed array, a union arm with no payload, a
// nested array and bounded strings.
const textSchema3 = `package textdemo3

enum State { unknown, done }

flags Features { Fast, Safe }

union Choice
{
    amount int32
    ping
}

type Node
{
    id string(16)
}

table Config
{
    lo128    int128 | min = -170141183460469231731687303715884105728, max = 170141183460469231731687303715884105727
    hi128    uint128
    fraction fixed(16, 16) | min = -32768, max = 32767
    flags    Features
    keyed    [State]int32
    choice   Choice
    nodes    [..3]Node
    tags     [..3]string(8)
    ratio    float32
}
`

const textInput3 = `(:lo128 -170141183460469231731687303715884105728 :hi128 340282366920938463463374607431768211455 :fraction 0.0000152587890625 :flags (:Fast "Safe") :keyed (:unknown 4 :done 7) :choice (:ping null) :nodes ((:id "a")) :tags ("x") :ratio 1.5)`
const textJSON3 = `{"lo128":-170141183460469231731687303715884105728,"hi128":340282366920938463463374607431768211455,"fraction":0.0000152587890625,"flags":["Fast","Safe"],"keyed":{"unknown":4,"done":7},"choice":{"ping":null},"nodes":[{"id":"a"}],"tags":["x"],"ratio":1.5}`

// textProgram is the unit's main: read the document, save its value, write
// the no-op rewrite, apply one leaf edit, and load the saved bytes back for
// THE VALUE DUMP.
const textProgram = `import .ConfigTable
import .text_form
%s
def textmain():
    let text = read_file("in.txt")
    assert text
    let doc = schema_rt.text_load("Config", text)
    assert doc
    let wire = schema_rt.save(doc.rec)
    print(hexs(wire))
    print(hexs(schema_rt.text_save(doc)))
    let ed = schema_rt.text_edit(doc, "%s", "%s")
    assert ed
    print(schema_rt.text_save(ed))
    let rep = schema_rt.LoadReport {}
    let back = schema_rt.load_tolerant("Config", wire, rep)
    assert back
    print(drec(back))
textmain()
`

type textRun struct {
	wire, rewrite, edited, dump string
}

// textCarried reports whether this card's reader carries every field kind the
// unit declares; a unit with a wide string, a pointer or a timestamp is left
// to its own card and named as not done.
func textCarried(u *ir.Unit) bool {
	for _, st := range u.Tables {
		for _, f := range st.Fields {
			if !textFieldCarried(f) {
				return false
			}
		}
	}
	for _, st := range u.Structs {
		for _, f := range st.Fields {
			if !textFieldCarried(f) {
				return false
			}
		}
	}
	return true
}

func textFieldCarried(f *ir.Field) bool {
	t := f.Type
	if t.Kind == ir.TWString || t.Timestamp || t.Pointer {
		return false
	}
	// a default document of a huge bound is the load walk's business, not
	// this card's text mapping; keep the sweep to units a person writes.
	if f.Array != ir.ArrayNone && f.ArrayBound > 1000 {
		return false
	}
	return true
}

// textSweepProgram is one manifest unit's default document through the text
// form: every table the unit declares reads "()", saves, and re-loads, each
// answer named by its root.
func textSweepProgram(g *loadUnit, roots []string) string {
	var b strings.Builder
	for _, m := range g.mods {
		fmt.Fprintf(&b, "import .%s\n", m)
	}
	b.WriteString("import .load\n")
	b.WriteString(dumpLobster)
	for _, r := range roots {
		fmt.Fprintf(&b, "def sweep_%s():\n", r)
		fmt.Fprintf(&b, "    let doc = schema_rt.text_load(%s, \"()\")\n", lobsterString(r))
		b.WriteString("    assert doc\n")
		b.WriteString("    let wire = schema_rt.save(doc.rec)\n")
		fmt.Fprintf(&b, "    print(%s + \" \" + hexs(wire))\n", lobsterString(r))
		b.WriteString("    let rep = schema_rt.LoadReport {}\n")
		fmt.Fprintf(&b, "    let back = schema_rt.load_tolerant(%s, wire, rep)\n", lobsterString(r))
		b.WriteString("    assert back\n")
		fmt.Fprintf(&b, "    print(%s + \" \" + report_line(rep))\n", lobsterString(r))
		fmt.Fprintf(&b, "    print(%s + \" \" + drec(back))\n", lobsterString(r))
		fmt.Fprintf(&b, "sweep_%s()\n", r)
	}
	return b.String()
}

func lobsterString(s string) string {
	return fmt.Sprintf("%q", s)
}

// sweepManifest runs every unit the tables manifest names, one empty document
// per declared table, and holds load, save and re-load to Go's engine.
func sweepManifest(t *testing.T) {
	t.Helper()
	f, err := os.Open(filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var units []struct {
		key   string
		paths []string
	}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		x := strings.Fields(sc.Text())
		if len(x) >= 3 && x[0] == "unit" {
			units = append(units, struct {
				key   string
				paths []string
			}{x[1], x[2:]})
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	seen := 0
	skipped := 0
	for _, unit := range units {
		var full []string
		for _, p := range unit.paths {
			full = append(full, filepath.Join(root, p))
		}
		ps, err := compiler.GatherPaths(full)
		if err != nil {
			t.Fatalf("manifest unit %s: %v", unit.key, err)
		}
		g, err := generateLoadUnit(t, ps)
		if err != nil {
			t.Fatalf("manifest unit %s is refused: %v", unit.key, err)
		}
		if !textCarried(g.u) {
			skipped++
			continue
		}
		var roots []string
		for name := range g.u.Tables {
			roots = append(roots, name)
		}
		sort.Strings(roots)
		if len(roots) == 0 {
			continue
		}
		if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(textSweepProgram(g, roots)), 0o644); err != nil {
			t.Fatal(err)
		}
		out := strings.Split(strings.TrimRight(string(lobsterrun.Run(t, g.dir, "main.lobster", nil)), "\n"), "\n")
		at := 0
		for _, name := range roots {
			m := tabletext.NewModel(g.u)
			inst := m.New(m.Lookup(name))
			want, err := tablewire.Encode(m, inst)
			if err != nil {
				t.Fatalf("%s.%s: %v", unit.key, name, err)
			}
			if at+3 > len(out) {
				t.Fatalf("%s.%s: the sweep printed %d lines, want 3 per root", unit.key, name, len(out))
			}
			if got := out[at][len(name)+1:]; got != hex.EncodeToString(want) {
				t.Fatalf("%s.%s: the text form saved %s, Go saved %x", unit.key, name, got, want)
			}
			wantReport, wantDump := goLoad(g.u, name, want)
			if got := out[at+1][len(name)+1:]; got != wantReport {
				t.Fatalf("%s.%s: re-load report %q, Go %q", unit.key, name, got, wantReport)
			}
			if got := out[at+2][len(name)+1:]; got != wantDump {
				t.Fatalf("%s.%s: re-loaded value is not Go's at byte %s", unit.key, name, divergence(got, wantDump))
			}
			at += 3
			seen++
		}
	}
	t.Logf("manifest sweep: %d units, %d carried, %d skipped for a kind a later card names; %d table roots read, saved and re-loaded as Go did", len(units), len(units)-skipped, skipped, seen)
}

func runText(t *testing.T, schema, input, editPath, editToken string) textRun {
	t.Helper()
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "Config.schema")
	if err := os.WriteFile(schemaPath, []byte(schema), 0o644); err != nil {
		t.Fatal(err)
	}
	paths, err := compiler.GatherPaths([]string{schemaPath})
	if err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, paths)
	if err != nil {
		t.Fatalf("the probe unit is refused: %v", err)
	}
	main := fmt.Sprintf(textProgram, dumpLobster, editPath, editToken)
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.Split(strings.TrimRight(string(lobsterrun.Run(t, g.dir, "main.lobster", map[string][]byte{"in.txt": []byte(input)})), "\n"), "\n")
	if len(out) != 4 {
		t.Fatalf("the text-form program printed %d lines, want 4: %q", len(out), out)
	}
	return textRun{wire: out[0], rewrite: out[1], edited: out[2], dump: out[3]}
}

func goText(t *testing.T, schema, json string) ([]byte, string) {
	t.Helper()
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "Config.schema")
	if err := os.WriteFile(schemaPath, []byte(schema), 0o644); err != nil {
		t.Fatal(err)
	}
	paths, err := compiler.GatherPaths([]string{schemaPath})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup("Config"))
	var rep tabletext.Report
	if !m.Read(inst, []byte(json), &rep) {
		t.Fatalf("the Go JSON read refused the equivalent value: report=%+v", rep)
	}
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatal(err)
	}
	_, dump := goLoad(u, "Config", wire)
	return wire, dump
}

// TestLobsterTextForm reads a hand-editable document, saves its value byte
// for byte as Go's engine does, rewrites the document unchanged and replaces
// one scalar's token span (docs/SPEC-TABLES.md §16.9.1-§16.9.3).
func TestLobsterTextForm(t *testing.T) {
	t.Parallel()
	want, wantDump := goText(t, textSchema, textJSON)
	got := runText(t, textSchema, textInput, "count", "6")
	if got.wire != hex.EncodeToString(want) {
		t.Fatalf("the text form saved %s, Go saved %x", got.wire, want)
	}
	if got.rewrite != hex.EncodeToString([]byte(textInput)) {
		t.Fatalf("the no-op rewrite is not the input byte for byte: %q", got.rewrite)
	}
	const edited = `(:name "ship" :count 6 :enabled true :tags (1 2))`
	if got.edited != edited {
		t.Fatalf("the leaf edit is not the one-token change: got %q want %q", got.edited, edited)
	}
	if got.dump != wantDump {
		t.Fatalf("the re-loaded value is not Go's at byte %s", divergence(got.dump, wantDump))
	}
	t.Logf("constructs: string(N) 1, int 1, bool 1, [..N]int 1; saved %d bytes as Go did, rewrite and one leaf edit green", len(want))

	// The second slice's probe: the kinds §16.9.2 gives their own rule — a
	// float, a bytes field spelled as padded base64, an optional that is
	// present, and a fixed array of an exact length — read, saved byte for
	// byte as Go does, rewritten unchanged and leaf-edited.
	want2, wantDump2 := goText(t, textSchema2, textJSON2)
	got2 := runText(t, textSchema2, textInput2, "ratio", "2.5")
	if got2.wire != hex.EncodeToString(want2) {
		t.Fatalf("the text form saved %s, Go saved %x", got2.wire, want2)
	}
	if got2.rewrite != hex.EncodeToString([]byte(textInput2)) {
		t.Fatalf("the no-op rewrite is not the input byte for byte: %q", got2.rewrite)
	}
	const edited2 = `(:ratio 2.5 :weight 2.25 :blob "AP8=" :note 42 :pair (7 8))`
	if got2.edited != edited2 {
		t.Fatalf("the leaf edit is not the one-token change: got %q want %q", got2.edited, edited2)
	}
	if got2.dump != wantDump2 {
		t.Fatalf("the re-loaded value is not Go's at byte %s", divergence(got2.dump, wantDump2))
	}
	t.Logf("constructs: float32 1, float64 1, bytes(N) 1, ?int32 1, [N]int 1; saved %d bytes as Go did, rewrite and one leaf edit green", len(want2))

	// The third slice's probe: the remaining §16.9.2 kinds — int128 and
	// uint128, a fixed on its Q grid, a flags mask, an enum-keyed array, a
	// payload-free union arm, a nested array and bounded strings.
	want3, wantDump3 := goText(t, textSchema3, textJSON3)
	got3 := runText(t, textSchema3, textInput3, "hi128", "7")
	if got3.wire != hex.EncodeToString(want3) {
		t.Fatalf("the text form saved %s, Go saved %x", got3.wire, want3)
	}
	if got3.rewrite != hex.EncodeToString([]byte(textInput3)) {
		t.Fatalf("the no-op rewrite is not the input byte for byte: %q", got3.rewrite)
	}
	const edited3 = `(:lo128 -170141183460469231731687303715884105728 :hi128 7 :fraction 0.0000152587890625 :flags (:Fast "Safe") :keyed (:unknown 4 :done 7) :choice (:ping null) :nodes ((:id "a")) :tags ("x") :ratio 1.5)`
	if got3.edited != edited3 {
		t.Fatalf("the leaf edit is not the one-token change: got %q want %q", got3.edited, edited3)
	}
	if got3.dump != wantDump3 {
		t.Fatalf("the re-loaded value is not Go's at byte %s", divergence(got3.dump, wantDump3))
	}
	t.Logf("constructs: int128 1, uint128 1, fixed(16,16) 1, flags 1, keyed 1, union 1, [..N]Node 1, [..N]string 1, float32 1; saved %d bytes as Go did, rewrite and one leaf edit green", len(want3))

	sweepManifest(t)
}

// TestLobsterTextFormNegativeControl plants a wrong integer in the emitted
// reader and holds the byte comparison red, with the unpatched control green.
func TestLobsterTextFormNegativeControl(t *testing.T) {
	t.Parallel()
	want, _ := goText(t, textSchema, textJSON)
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "Config.schema")
	if err := os.WriteFile(schemaPath, []byte(textSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	paths, err := compiler.GatherPaths([]string{schemaPath})
	if err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, paths)
	if err != nil {
		t.Fatal(err)
	}
	main := fmt.Sprintf(textProgram, dumpLobster, "count", "6")
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	input := map[string][]byte{"in.txt": []byte(textInput)}
	control, _, _ := strings.Cut(strings.TrimRight(string(lobsterrun.Run(t, g.dir, "main.lobster", input)), "\n"), "\n")
	if control != hex.EncodeToString(want) {
		t.Fatalf("the control, unpatched: got %s want %x", control, want)
	}
	path := filepath.Join(g.dir, "text_form.lobster")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	const anchor = "        sl.cell.i = v.lo\n"
	if n := strings.Count(string(src), anchor); n != 1 {
		t.Fatalf("the integer anchor is in the emitted runtime %d times, want 1", n)
	}
	patched := strings.Replace(string(src), anchor, "        sl.cell.i = 999\n", 1)
	if err := os.WriteFile(path, []byte(patched), 0o644); err != nil {
		t.Fatal(err)
	}
	bad, _, _ := strings.Cut(strings.TrimRight(string(lobsterrun.Run(t, g.dir, "main.lobster", input)), "\n"), "\n")
	if bad == hex.EncodeToString(want) {
		t.Fatalf("the patched reader still saved Go's bytes: %s", bad)
	}
	t.Logf("the comparison went red under the patch: saved %s, want %x", bad, want)
}
