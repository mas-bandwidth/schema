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
const textJSON3 = `{"lo128":"-170141183460469231731687303715884105728","hi128":"340282366920938463463374607431768211455","fraction":0.0000152587890625,"flags":["Fast","Safe"],"keyed":{"unknown":4,"done":7},"choice":{"ping":null},"nodes":[{"id":"a"}],"tags":["x"],"ratio":1.5}`

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
    print(drec(doc.rec))
textmain()
`

type textRun struct {
	wire, rewrite, edited, dump string
}

// textSweepProgram is one manifest unit's default document through the text
// form: every table without a required field reads "()", writes canonical
// JSON, and exposes its value; a table with one refuses "()" by its §16.9.4
// name, since a missing required field is refused (§16.9.1). Each answer is
// named by its root.
func textSweepProgram(g *loadUnit, roots []string) string {
	var b strings.Builder
	for _, m := range g.mods {
		fmt.Fprintf(&b, "import .%s\n", m)
	}
	b.WriteString("import .load\n")
	b.WriteString(dumpLobster)
	b.WriteString(sweepChunksLobster)
	for _, r := range roots {
		fmt.Fprintf(&b, "def sweep_%s():\n", r)
		if tableHasRequired(g.u.Tables[r]) {
			fmt.Fprintf(&b, "    let doc, why = schema_rt.text_read(%s, \"()\")\n", lobsterString(r))
			fmt.Fprintf(&b, "    print(%s + \" \" + (if doc: \"read\" else: \"refused \" + why))\n", lobsterString(r))
			fmt.Fprintf(&b, "sweep_%s()\n", r)
			continue
		}
		fmt.Fprintf(&b, "    let doc = schema_rt.text_load(%s, \"()\")\n", lobsterString(r))
		b.WriteString("    assert doc\n")
		b.WriteString("    let text, reason = schema_rt.json_write(doc.rec)\n    assert reason == \"\"\n")
		fmt.Fprintf(&b, "    pchunks(%s, hexs(text))\n", lobsterString(r))
		fmt.Fprintf(&b, "    print(%s + \" read\")\n", lobsterString(r))
		fmt.Fprintf(&b, "    pchunks(%s, drec(doc.rec))\n", lobsterString(r))
		fmt.Fprintf(&b, "sweep_%s()\n", r)
	}
	return b.String()
}

// sweepChunksLobster prints one long answer as a count line and that many
// lines of at most 50000 bytes, each named by its root: Lobster's print cuts
// a string past 100000 bytes to "..", and a [..70000] array's value dump
// (tabledemo.WideBlob) is twice that.
const sweepChunksLobster = `def pchunks(name:string, s:string):
    print(name + " " + string((length(s) + 49999) / 50000))
    var at = 0
    while at < length(s):
        let w = min(50000, length(s) - at)
        print(name + " " + substring(s, at, w))
        at += w
`

// takeChunks reads one pchunks answer for root name from out at *at.
func takeChunks(t *testing.T, out []string, at *int, unit, name string) string {
	t.Helper()
	line := func() string {
		if *at >= len(out) {
			t.Fatalf("%s.%s: the sweep printed %d lines, want more", unit, name, len(out))
		}
		l := out[*at]
		*at++
		if !strings.HasPrefix(l, name+" ") {
			t.Fatalf("%s.%s: line %q is not this root's", unit, name, l)
		}
		return l[len(name)+1:]
	}
	var n int
	if _, err := fmt.Sscanf(line(), "%d", &n); err != nil {
		t.Fatalf("%s.%s: no chunk count: %v", unit, name, err)
	}
	var b strings.Builder
	for range n {
		b.WriteString(line())
	}
	return b.String()
}

func lobsterString(s string) string {
	return fmt.Sprintf("%q", s)
}

// tableHasRequired reports whether a root declares a `| required` field, whose
// absence from "()" the text form refuses (§16.9.1).
func tableHasRequired(st *ir.Struct) bool {
	for _, f := range st.Fields {
		if f.Required {
			return true
		}
	}
	return false
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
	seen, refused := 0, 0
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
			if tableHasRequired(g.u.Tables[name]) {
				// the oracle refuses the empty record too (verdict only), and
				// the leg names the refusal the spec names for its own form
				var rep tabletext.Report
				if m.Read(m.New(m.Lookup(name)), []byte("{}"), &rep) {
					t.Fatalf("%s.%s: Go read {} for a table with a required field", unit.key, name)
				}
				if at >= len(out) {
					t.Fatalf("%s.%s: the sweep printed %d lines, want a verdict", unit.key, name, len(out))
				}
				if got, want := out[at][len(name)+1:], "refused text_missing_required"; got != want {
					t.Fatalf("%s.%s: \"()\" answered %q, want %q", unit.key, name, got, want)
				}
				at++
				refused++
				continue
			}
			want, err := m.Write(inst)
			want = append(want, '\n')
			if err != nil {
				t.Fatalf("%s.%s: %v", unit.key, name, err)
			}
			if got := takeChunks(t, out, &at, unit.key, name); got != hex.EncodeToString(want) {
				t.Fatalf("%s.%s: the text form saved %s, Go saved %x", unit.key, name, got, want)
			}
			dumper := &goDumper{labels: map[*tabletext.Instance]int{inst: 0}, next: 1}
			wantReport, wantDump := "read", dumper.rec(inst)
			if at >= len(out) {
				t.Fatalf("%s.%s: the sweep printed %d lines, want a verdict", unit.key, name, len(out))
			}
			if got := out[at][len(name)+1:]; got != wantReport {
				t.Fatalf("%s.%s: text verdict %q, Go %q", unit.key, name, got, wantReport)
			}
			at++
			if got := takeChunks(t, out, &at, unit.key, name); got != wantDump {
				t.Fatalf("%s.%s: decoded value is not Go's at byte %s", unit.key, name, divergence(got, wantDump))
			}
			seen++
		}
		if at != len(out) {
			t.Fatalf("%s: the sweep printed %d lines, the roots answered %d", unit.key, len(out), at)
		}
	}
	if refused == 0 {
		t.Fatalf("manifest sweep: no root with a required field was swept, so the refusal branch held nothing")
	}
	t.Logf("manifest sweep: %d units; %d table roots read and written as Go did; %d roots with a required field refused \"()\" as text_missing_required", len(units), seen, refused)
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
		t.Fatalf("the decoded value is not Go's at byte %s", divergence(got.dump, wantDump))
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
		t.Fatalf("the decoded value is not Go's at byte %s", divergence(got2.dump, wantDump2))
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
		t.Fatalf("the decoded value is not Go's at byte %s", divergence(got3.dump, wantDump3))
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
