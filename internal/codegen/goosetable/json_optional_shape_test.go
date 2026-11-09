package goosetable

// THE KEY IS PRESENCE (docs/SPEC-TABLES.md §16.2): an optional field whose key
// is in the text is present, whatever its value turned out to be. A value of
// the wrong JSON shape is skipped and counted as a kind mismatch, and the
// field is still present, at the value it already held. Only `null` is the
// absence. Each row's report line and written text are the Go engine's
// (internal/tabletext) over the same unit. The reversed witness drops the
// presence from the mismatch branch and holds that it is red on exactly the
// wrong-shape rows.

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const jsonOptionalShapeSchema = `package probe
fixed table Inner {
    a int32 = 7
}
fixed table Root {
    t ?Inner
    n ?int32
    f ?float64
    x int32
}
`

// jsonOptionalWrongShape are the rows the mismatch branch's presence decides.
var jsonOptionalWrongShape = []string{
	`{"t": 5}`,
	`{"n": "x"}`,
	`{"f": [1]}`,
	`{"t": [], "n": {}, "x": 2}`,
}

// jsonOptionalSteady are rows the walk has always landed as the engine does.
var jsonOptionalSteady = []string{
	`{}`,
	`{"t": {"a": 3}}`,
	`{"t": null, "n": null}`,
	`{"t": {"a": 3}, "t": 5}`,
	`{"n": 4, "n": null}`,
	`{"x": "y"}`,
}

func jsonOptionalTexts() []string {
	return append(append([]string{}, jsonOptionalWrongShape...), jsonOptionalSteady...)
}

func jsonOptionalUnit(t *testing.T) *ir.Unit {
	t.Helper()
	path := filepath.Join(t.TempDir(), "Probe.schema")
	if err := os.WriteFile(path, []byte(jsonOptionalShapeSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := loadSchemaPaths([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// jsonOptionalRun answers one line per text, `<report line> <ToJson hex>`;
// edit plants a reversal in the generated runtime.
func jsonOptionalRun(t *testing.T, edit func(map[string][]byte)) []string {
	t.Helper()
	u := jsonOptionalUnit(t)
	files, err := goose.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	if edit != nil {
		edit(files)
	}
	var mods []string
	for name := range files {
		if before, ok := strings.CutSuffix(name, ".goose"); ok {
			mods = append(mods, before)
		}
	}
	ns := goose.Namespace(u)
	var b strings.Builder
	b.WriteString("import os;\n")
	for _, m := range mods {
		fmt.Fprintf(&b, "import .%s;\n", m)
	}
	b.WriteString("\n" + gooserun.HexProbe + "\n")
	b.WriteString("fn probe(name: const u8[:]) {\n")
	b.WriteString("    var text: u8[>..] = [];\n    if !read_file(name, text) { print(\"read-failed\"); return; }\n")
	fmt.Fprintf(&b, "    var v = %s::NewRoot();\n    var r = schema_rt::json_report();\n", ns)
	fmt.Fprintf(&b, "    let ok = %s::RootFromJson(v, text, r);\n", ns)
	b.WriteString("    if !ok || r.damaged { print(\"refused\"); print(\"-\"); return; }\n")
	b.WriteString("    print(r.unknown, \",\", r.mismatch, \",\", r.widened, \",\", r.oor, \",\", r.repeated, \",false,read\");\n")
	fmt.Fprintf(&b, "    var out: u8[>..] = [];\n    let n = %s::RootToJson(v, out);\n    if n != out.len { print(\"short\"); }\n    hex(out);\n", ns)
	b.WriteString("}\n\nfn main() {\n")
	inputs := map[string][]byte{}
	for i, text := range jsonOptionalTexts() {
		name := fmt.Sprintf("in%d.json", i)
		inputs[name] = []byte(text)
		fmt.Fprintf(&b, "    probe(%q);\n", name)
	}
	b.WriteString("}\n")
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.goose"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(gooserun.Run(t, dir, "main.goose", inputs)), "\n"), "\n")
	var got []string
	for i := 0; i+1 < len(lines); i += 2 {
		got = append(got, lines[i]+" "+lines[i+1])
	}
	if len(got) != len(jsonOptionalTexts()) {
		t.Fatalf("the probe answered %d rows for %d texts:\n%s", len(got), len(jsonOptionalTexts()), strings.Join(lines, "\n"))
	}
	return got
}

func jsonOptionalOracle(t *testing.T) []string {
	t.Helper()
	u := jsonOptionalUnit(t)
	model := tabletext.NewModel(u)
	var want []string
	for _, text := range jsonOptionalTexts() {
		inst := model.New(model.Lookup("Root"))
		var r tabletext.Report
		if !model.Read(inst, []byte(text), &r) || r.Refused {
			want = append(want, "refused -")
			continue
		}
		out, err := model.Write(inst)
		if err != nil {
			t.Fatalf("%s: the Go engine's text: %v", text, err)
		}
		out = append(out, '\n') // ToJson ends its text with a newline
		want = append(want, fmt.Sprintf("%d,%d,%d,%d,%d,false,read %x", r.Unknown, 0, r.Widened, 0, 0, out))
	}
	return want
}

func TestGooseJSONOptionalKeyIsPresence(t *testing.T) {
	t.Parallel()
	got, want := jsonOptionalRun(t, nil), jsonOptionalOracle(t)
	texts := jsonOptionalTexts()
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%s: Goose lands %s, the Go engine %s", texts[i], got[i], want[i])
		}
	}
}

func TestGooseJSONOptionalKeyIsPresenceNegativeControl(t *testing.T) {
	t.Parallel()
	got := jsonOptionalRun(t, func(files map[string][]byte) {
		const anchor = "    if good && (jr.rep.mismatch > 0 || jr.rep.oor > 0 || jr.rep.repeated > 0) { good = false; }\n"
		const reversed = "    if good && (jr.rep.oor > 0 || jr.rep.repeated > 0) { good = false; }\n"
		var hit string
		for name, data := range files {
			if strings.Contains(string(data), anchor) {
				if hit != "" {
					t.Fatalf("the plant's anchor stands in %s and %s", hit, name)
				}
				hit = name
			}
		}
		if hit == "" || strings.Count(string(files[hit]), anchor) != 1 {
			t.Fatalf("the plant's anchor does not stand once in the generated runtime: %q", anchor)
		}
		files[hit] = []byte(strings.Replace(string(files[hit]), anchor, reversed, 1))
	})
	want := jsonOptionalOracle(t)
	texts := jsonOptionalTexts()
	var red []string
	for i := range want {
		if got[i] != want[i] {
			red = append(red, texts[i])
		}
	}
	wantRed := append(append([]string{}, jsonOptionalWrongShape...), `{"x": "y"}`)
	if strings.Join(red, " | ") != strings.Join(wantRed, " | ") {
		t.Fatalf("the absent mismatch refusal is red on %q, want %q", red, wantRed)
	}
}
