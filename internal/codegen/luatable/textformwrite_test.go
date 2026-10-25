package luatable

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

// TestLuaHandEditableTextWrite is the Lua textform write gate
// (docs/SPEC-TABLES.md §16.9.3): a read answers a document that retains the
// source bytes, so a fresh document writes back byte for byte, and an edit of
// one existing scalar changes only that value's token span. The oracle is the
// generated Go writer's bytes: test/conformance/textform's .rewrite.sexp
// (SaveTextForm of a fresh document) and .edited.sexp (EditTextForm of the same
// document), which the Go leg (gotable's TestDocumentVectors and
// TestDocumentEdits) pins as its own Save and Edit output.
func TestLuaHandEditableTextWrite(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	vectors := filepath.Join(repo, "test", "conformance", "textform")

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "doc.schema"), []byte(textFormSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u := luarun.LoadUnit(t, dir)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	for _, name := range []string{"textform.lua", "textformwrite.lua"} {
		if _, ok := files[name]; !ok {
			t.Fatalf("the emitted unit has no %s", name)
		}
	}
	luaDir := t.TempDir()
	luarun.WriteFiles(t, luaDir, files)

	readFile := func(path string) []byte {
		t.Helper()
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	inputs := map[string][]byte{}
	type row struct {
		name, root, stem string
	}
	rows := []row{
		{"roadmap", "Document", "roadmap"},
		{"layout", "Document", "layout"},
	}
	for _, r := range rows {
		inputs[r.name] = readFile(filepath.Join(vectors, r.stem+".input.sexp"))
		inputs[r.name+"_root"] = []byte(r.root)
		var edit struct {
			Path  string
			Value any
		}
		if err := json.Unmarshal(readFile(filepath.Join(vectors, r.stem+".edit.json")), &edit); err != nil {
			t.Fatalf("%s: edit: %v", r.stem, err)
		}
		token, err := json.Marshal(edit.Value)
		if err != nil {
			t.Fatal(err)
		}
		inputs[r.name+"_edit_path"] = []byte(edit.Path)
		inputs[r.name+"_edit_text"] = token
	}
	// The Lua table surface has no absent optional map value (docs/PORT-LUA.md),
	// as in TestLuaHandEditableTextRead: the Graph row keeps its sharing, map
	// order and present empty blob, without the west null entry.
	inputs["variable"] = []byte(strings.Replace(
		string(readFile(filepath.Join(vectors, "variable.input.sexp"))), `("west" null) `, "", 1))
	inputs["variable_root"] = []byte("Graph")

	probe := `local T = require("ProbeTables")
local W = require("textformwrite")
local inputs = require("inputs")
local function hex(s) return (s:gsub(".", function(c) return string.format("%02x", c:byte()) end)) end
local function run(name, fn)
  local ok, out = pcall(fn)
  if not ok then
    local e = out
    print(name .. "\tREFUSE\t" .. (type(e) == "table" and tostring(e.name) or tostring(e)))
  else
    print(name .. "\tOK\t" .. hex(out))
  end
end
for _, row in ipairs({{ "roadmap", "Document" }, { "layout", "Document" }, { "variable", "Graph" }}) do
  local name, root = row[1], row[2]
  local doc = W.Load(T, root, inputs[name])
  run(name .. "_fresh", function() return W.Save(doc) end)
  if inputs[name .. "_edit_path"] ~= nil then
    local edited = W.Edit(doc, { { path = inputs[name .. "_edit_path"], text = inputs[name .. "_edit_text"] } })
    run(name .. "_edited", function() return W.Save(edited) end)
    run(name .. "_unchanged", function() return W.Save(doc) end)
  end
end
local doc = W.Load(T, "Document", inputs.layout)
run("layout_noop", function() return W.Save(W.Edit(doc, { { path = "count", text = "2" } })) end)
local rdoc = W.Load(T, "Document", inputs.roadmap)
run("refuse_compound", function() return W.Edit(rdoc, { { path = "nodes", text = "()" } }) end)
run("refuse_absent", function() return W.Edit(doc, { { path = "schema", text = "1" } }) end)
run("refuse_bound", function() return W.Edit(doc, { { path = "count", text = "11" } }) end)
run("refuse_two_tokens", function() return W.Edit(doc, { { path = "count", text = "1 2" } }) end)
`
	if err := os.WriteFile(filepath.Join(luaDir, "main.lua"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	out := string(luarun.Run(t, luaDir, "main.lua", inputs))

	results := map[string]struct{ verdict, body string }{}
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			t.Fatalf("bad probe line %q", line)
		}
		results[parts[0]] = struct{ verdict, body string }{parts[1], parts[2]}
	}
	body := func(name string) []byte {
		t.Helper()
		r, ok := results[name]
		if !ok {
			t.Fatalf("%s: no probe result", name)
		}
		if r.verdict != "OK" {
			t.Fatalf("%s: %s %s", name, r.verdict, r.body)
		}
		b, err := hex.DecodeString(r.body)
		if err != nil {
			t.Fatalf("%s: hex: %v", name, err)
		}
		return b
	}
	refusal := func(name, want string) {
		t.Helper()
		r, ok := results[name]
		if !ok {
			t.Fatalf("%s: no probe result", name)
		}
		if r.verdict != "REFUSE" || r.body != want {
			t.Errorf("%s: got %s %s, want REFUSE %s", name, r.verdict, r.body, want)
		}
	}

	for _, r := range rows {
		fresh := body(r.name + "_fresh")
		if want := readFile(filepath.Join(vectors, r.stem+".rewrite.sexp")); string(fresh) != string(want) {
			t.Errorf("%s: the fresh document is not the Go writer's bytes\n got %q\nwant %q", r.name, fresh, want)
		}
		if want := readFile(filepath.Join(vectors, r.stem+".input.sexp")); string(fresh) != string(want) {
			t.Errorf("%s: a read/write moved the source", r.name)
		}
		edited := body(r.name + "_edited")
		if want := readFile(filepath.Join(vectors, r.stem+".edited.sexp")); string(edited) != string(want) {
			t.Errorf("%s: the edit is not the Go writer's bytes\n got %q\nwant %q", r.name, edited, want)
		}
		if got := body(r.name + "_unchanged"); string(got) != string(readFile(filepath.Join(vectors, r.stem+".input.sexp"))) {
			t.Errorf("%s: an edit moved the original document", r.name)
		}
	}
	// the Lua surface omits the west null entry, so the Go rewrite vector does
	// not apply; the retained source is still the writer's whole answer
	if got, want := body("variable_fresh"), inputs["variable"]; string(got) != string(want) {
		t.Errorf("variable: a read/write moved the source\n got %q\nwant %q", got, want)
	}
	if got, want := body("layout_noop"), readFile(filepath.Join(vectors, "layout.input.sexp")); string(got) != string(want) {
		t.Errorf("layout: a semantic no-op rewrote the document\n got %q\nwant %q", got, want)
	}
	refusal("refuse_compound", "text_edit_conflict")
	refusal("refuse_absent", "text_edit_conflict")
	refusal("refuse_bound", "text_bound")
	refusal("refuse_two_tokens", "text_edit_conflict")
}
