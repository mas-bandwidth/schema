package lobstertable_test

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
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
    let ed = schema_rt.text_edit(doc, "count", "6")
    assert ed
    print(schema_rt.text_save(ed))
    let rep = schema_rt.LoadReport {}
    let back = schema_rt.load("Config", wire, rep)
    assert back
    print(drec(back))
textmain()
`

type textRun struct {
	wire, rewrite, edited, dump string
}

func runText(t *testing.T, schema, input string) textRun {
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
	main := fmt.Sprintf(textProgram, dumpLobster)
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
	got := runText(t, textSchema, textInput)
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
	main := fmt.Sprintf(textProgram, dumpLobster)
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	input := map[string][]byte{"in.txt": []byte(textInput)}
	control := strings.Split(strings.TrimRight(string(lobsterrun.Run(t, g.dir, "main.lobster", input)), "\n"), "\n")[0]
	if control != hex.EncodeToString(want) {
		t.Fatalf("the control, unpatched: got %s want %x", control, want)
	}
	path := filepath.Join(g.dir, "text_form.lobster")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	const anchor = "        sl.cell.i = t_int(v.text)\n"
	if n := strings.Count(string(src), anchor); n != 1 {
		t.Fatalf("the integer anchor is in the emitted runtime %d times, want 1", n)
	}
	patched := strings.Replace(string(src), anchor, "        sl.cell.i = 999\n", 1)
	if err := os.WriteFile(path, []byte(patched), 0o644); err != nil {
		t.Fatal(err)
	}
	bad := strings.Split(strings.TrimRight(string(lobsterrun.Run(t, g.dir, "main.lobster", input)), "\n"), "\n")[0]
	if bad == hex.EncodeToString(want) {
		t.Fatalf("the patched reader still saved Go's bytes: %s", bad)
	}
	t.Logf("the comparison went red under the patch: saved %s, want %x", bad, want)
}
