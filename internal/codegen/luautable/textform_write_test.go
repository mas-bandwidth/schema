package luautable_test

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

type tfWriteEdit struct {
	path string
	text string
}

type tfWriteCase struct {
	name  string
	input string
	edits []tfWriteEdit
}

// tfWriteCases is the standard edit corpus plus the rule rows of
// gotable/textform_test.go: the two vector edits, the semantic no-ops, a
// bound refusal, the structural edits (absent, compound, compound spelling,
// two tokens, two edits of one span) and two disjoint edits. The verdicts and
// the edited bytes are never typed here: the Go unit the same test generates
// answers them.
func tfWriteCases(t *testing.T) []tfWriteCase {
	t.Helper()
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join(root, "test", "conformance", "textform", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	vectorEdit := func(stem string) tfWriteEdit {
		raw, err := os.ReadFile(filepath.Join(root, "test", "conformance", "textform", stem+".edit.json"))
		if err != nil {
			t.Fatal(err)
		}
		var row struct {
			Operation string
			Path      string
			Value     any
		}
		if err := json.Unmarshal(raw, &row); err != nil {
			t.Fatal(err)
		}
		if row.Operation != "replace_scalar" {
			t.Fatalf("%s: operation %q", stem, row.Operation)
		}
		spelling, err := json.Marshal(row.Value)
		if err != nil {
			t.Fatal(err)
		}
		return tfWriteEdit{path: row.Path, text: string(spelling)}
	}
	roadmap := read("roadmap.input.sexp")
	layout := read("layout.input.sexp")
	cases := []tfWriteCase{
		{name: "save-roadmap", input: roadmap},
		{name: "save-layout", input: layout},
		{name: "edit-roadmap", input: roadmap, edits: []tfWriteEdit{vectorEdit("roadmap")}},
		{name: "edit-layout", input: layout, edits: []tfWriteEdit{vectorEdit("layout")}},
		{name: "noop-2", input: layout, edits: []tfWriteEdit{{path: "count", text: "2"}}},
		{name: "noop-2.0", input: layout, edits: []tfWriteEdit{{path: "count", text: "2.0"}}},
		{name: "noop-20e-1", input: layout, edits: []tfWriteEdit{{path: "count", text: "20e-1"}}},
		{name: "bound-count", input: layout, edits: []tfWriteEdit{{path: "count", text: "11"}}},
		{name: "structural-compound", input: roadmap, edits: []tfWriteEdit{{path: "nodes", text: "()"}}},
		{name: "structural-element", input: roadmap, edits: []tfWriteEdit{{path: "nodes[0]", text: "1"}}},
		{name: "structural-absent", input: roadmap, edits: []tfWriteEdit{{path: "enabled", text: "true"}}},
		{name: "structural-spelling", input: roadmap, edits: []tfWriteEdit{{path: "count", text: "(1)"}}},
		{name: "structural-two-tokens", input: roadmap, edits: []tfWriteEdit{{path: "count", text: "1 2"}}},
		{name: "structural-duplicate", input: roadmap, edits: []tfWriteEdit{{path: "count", text: "1"}, {path: "count", text: "2"}}},
		{name: "disjoint", input: roadmap, edits: []tfWriteEdit{
			{path: "count", text: "7"},
			{path: "nodes[0].title", text: `"a much longer title"`},
		}},
	}
	return cases
}

// tfWriteGoProbe is the oracle: the same unit generated for Go, loaded and
// edited through schema-go's own text form, with each saved document printed
// as hex so a byte difference cannot hide in quoting.
const tfWriteGoProbe = `package probe

import (
	"encoding/hex"
	"fmt"
	"testing"
)

type ec struct {
	name  string
	input string
	edits []TableTextFormEdit
}
var cases = []ec{%s}

func TestOracle(t *testing.T) {
	for _, c := range cases {
		doc, _, err := DocumentLoadTextForm([]byte(c.input), TableTextFormDefaultLimits())
		if err != nil {
			fmt.Printf("%%s\tLOADERR\t%%s\n", c.name, err)
			continue
		}
		edited, _, err := DocumentEditTextForm(doc, c.edits...)
		if err != nil {
			if e, ok := err.(*TableTextFormError); ok {
				fmt.Printf("%%s\tERR\t%%s\n", c.name, e.Name)
			} else {
				fmt.Printf("%%s\tERR\t%%s\n", c.name, err)
			}
			continue
		}
		out, err := DocumentSaveTextForm(edited)
		if err != nil {
			fmt.Printf("%%s\tERR\t%%s\n", c.name, err)
			continue
		}
		fmt.Printf("%%s\tOK\t%%s\n", c.name, hex.EncodeToString(out))
	}
}
`

// TestLuauTextFormEditKeepsTheDocument runs the same unit through schema-go's
// generated Go text form and through the generated Luau text form, in
// t.TempDir() under the pinned interpreter: SaveTextForm of an unedited
// document is its input, every vector edit and disjoint pair saves the Go
// answer byte for byte, a semantic no-op keeps the old spelling, and every
// structural edit refuses text_edit_conflict (docs/SPEC-TABLES.md §16.9.3,
// §16.9.4).
func TestLuauTextFormEditKeepsTheDocument(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter")
	cases := tfWriteCases(t)

	srcDir := t.TempDir()
	schemaPath := filepath.Join(srcDir, "Probe.schema")
	if err := os.WriteFile(schemaPath, []byte(tfFixedSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{schemaPath})
	if err != nil {
		t.Fatalf("the unit does not load: %v", err)
	}

	// Go's own text form: the oracle for every verdict and every saved byte.
	goDir := t.TempDir()
	goFiles, err := golang.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	surfaces, err := gotable.ParseSurfaces("json,wire,textform")
	if err != nil {
		t.Fatal(err)
	}
	tables, err := gotable.GenerateLineageSurface(u, nil, surfaces)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(goFiles, tables)
	runtime := tfSerializeGoRuntime(t)
	goFiles["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\nreplace github.com/mas-bandwidth/serialize.go/v2 => %q\n", runtime))
	var lit strings.Builder
	for i, c := range cases {
		fmt.Fprintf(&lit, "\n\t{name: %q, input: %q, edits: []TableTextFormEdit{", fmt.Sprintf("case%03d", i), c.input)
		for _, e := range c.edits {
			fmt.Fprintf(&lit, "{Path: %q, Text: %q},", e.path, e.text)
		}
		lit.WriteString("}},")
	}
	goFiles["oracle_test.go"] = []byte(fmt.Sprintf(tfWriteGoProbe, lit.String()))
	for name, data := range goFiles {
		if err := os.WriteFile(filepath.Join(goDir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", "-timeout", "600s", "-run", "^TestOracle$", "-v", ".")
	cmd.Dir = goDir
	wantOut, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the Go oracle did not build and run: %v\n%s", err, wantOut)
	}
	want := map[string]string{}
	for line := range strings.SplitSeq(string(wantOut), "\n") {
		if fields := strings.Split(line, "\t"); len(fields) >= 3 {
			want[fields[0]] = line
		}
	}

	// The same unit and cases under the pinned interpreter.
	dir := t.TempDir()
	out, err := luau.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tableFiles, err := luautable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(out, tableFiles)
	for name, data := range out {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	g := &generatedUnit{dir: dir, files: out, loaded: true, flags: flagsMeta(u)}
	var main strings.Builder
	main.WriteString(jsonModules(g))
	main.WriteString(jsonFlags(g))
	main.WriteString("local json = require(\"./schema_json\")\njson.SetFlags(SCHEMA_JSON_FLAGS)\n")
	main.WriteString("local textform = require(\"./schema_textform\")\ntextform.SetFlags(SCHEMA_JSON_FLAGS)\n")
	main.WriteString("local cases = {")
	for i, c := range cases {
		fmt.Fprintf(&main, "\n\t{ name = %s, input = %s, edits = {", tfLuauString(fmt.Sprintf("case%03d", i)), tfLuauString(c.input))
		for _, e := range c.edits {
			fmt.Fprintf(&main, "{ path = %s, text = %s },", tfLuauString(e.path), tfLuauString(e.text))
		}
		main.WriteString("} },")
	}
	main.WriteString("\n}\n")
	main.WriteString(`local function hex(s)
	local out = {}
	for i = 1, #s do out[#out + 1] = string.format("%02x", s:byte(i)) end
	return table.concat(out)
end
for _, c in cases do
	local okp, doc, v, err = pcall(function() return textform.LoadTextForm("Document", c.input, {}) end)
	if not okp then
		print(c.name .. "\tCRASH\t" .. tostring(doc))
	elseif err ~= nil then
		print(c.name .. "\tLOADERR\t" .. err.name)
	else
		local okp2, ed, ev, eerr = pcall(function() return textform.EditTextForm(doc, c.edits) end)
		if not okp2 then
			print(c.name .. "\tCRASH\t" .. tostring(ed))
		elseif eerr ~= nil then
			print(c.name .. "\tERR\t" .. eerr.name)
		else
			local saved, serr = textform.SaveTextForm(ed)
			if serr ~= nil then
				print(c.name .. "\tERR\t" .. serr.name)
			else
				print(c.name .. "\tOK\t" .. hex(saved))
			end
		end
	end
end
`)
	if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(main.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	gotOut := string(luaurun.Run(t, dir, "main.luau", nil))
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(gotOut, "\n"), "\n") {
		if fields := strings.Split(line, "\t"); len(fields) >= 3 {
			got[fields[0]] = line
		}
	}
	if len(got) != len(cases) {
		t.Fatalf("the Luau probe wrote %d lines, want %d:\n%s", len(got), len(cases), gotOut)
	}
	for i, c := range cases {
		key := fmt.Sprintf("case%03d", i)
		w, gk := want[key], got[key]
		if w == "" || gk == "" {
			t.Fatalf("%s: oracle %q, Luau %q", c.name, w, gk)
		}
		wf, gf := strings.Split(w, "\t"), strings.Split(gk, "\t")
		if wf[1] != gf[1] {
			t.Errorf("%s: verdict %s, want %s (%s)", c.name, gf[1], wf[1], strings.Join(gf, " | "))
			continue
		}
		if wf[1] == "ERR" {
			if wf[2] != gf[2] {
				t.Errorf("%s: refusal %s, want %s", c.name, gf[2], wf[2])
			}
			continue
		}
		if wf[2] != gf[2] {
			wb, _ := hex.DecodeString(wf[2])
			gb, _ := hex.DecodeString(gf[2])
			t.Errorf("%s: saved bytes differ:\n got %q\nwant %q", c.name, gb, wb)
			continue
		}
		if len(c.edits) == 0 && gf[2] != hex.EncodeToString([]byte(c.input)) {
			t.Errorf("%s: an unedited save is not the input", c.name)
		}
	}
}
