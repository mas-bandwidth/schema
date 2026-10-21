package luatable

import (
	"encoding/hex"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

// textFormSchema is the conformance Document contract (test/conformance/
// textform/contract.json) in Lua's table kinds. The contract's int128, uint128
// and fixed(16,16) fields are absent: the Lua table surface refuses those kinds
// by name (docs/PORT-LUA.md, "64-BIT AND 128-BIT INTEGERS"), so the vectors
// that need them are not run here.
const textFormSchema = `package probe
enum State { unknown, done }
flags Features { Fast, Safe }
type Node {
 id string(16)
 title string(32)
 state State
}
type TextItem {
 line string(80)
}
union Choice {
 amount int32
 ping
}
table Document {
 schema uint8 | min = 0, max = 3
 root string(32)
 title string(32)
 state State
 count int32 | min = 0, max = 10
 enabled bool
 text [..4]TextItem
 nodes [..3]Node
 pairs [2]int32
 optional ?[..4]int32
 wide wstring(4)
 blob bytes(4) | json = "bytes"
 real float32 | json = "float"
 time timestamp
 flags Features
 keyed [State]int32
 choice Choice
}
table GNode {
 id int32
}
table Graph {
 items []*GNode
 lookup map[string(8)]int32
 blob *bytes
}
`

// TestLuaHandEditableTextRead is the Lua textform read gate (docs/SPEC-TABLES.md
// §16.9.1, §16.9.2, §16.9.4): every accepted conformance document reads to the
// value the generated Go reads (compared through the JSON form of the same
// value), and every refused one refuses by the Go reader's stable name.
func TestLuaHandEditableTextRead(t *testing.T) {
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
	if _, ok := files["textform.lua"]; !ok {
		t.Fatal("the emitted unit has no textform.lua")
	}
	luaDir := t.TempDir()
	luarun.WriteFiles(t, luaDir, files)

	type testCase struct {
		name, root string
		text       []byte
	}
	var cases []testCase
	add := func(name, root string, text []byte) {
		cases = append(cases, testCase{name: name, root: root, text: text})
	}
	read := func(path string) []byte {
		t.Helper()
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}

	add("empty", "Document", []byte("()"))
	for _, stem := range []string{"roadmap", "layout"} {
		add("accept_"+stem, "Document", read(filepath.Join(vectors, stem+".input.sexp")))
	}
	// The Lua table surface has no absent optional map value (docs/PORT-LUA.md),
	// so the Graph row keeps the sharing, order and empty-blob shapes with the
	// present-value lookup form; the west null entry is omitted.
	variable := strings.Replace(string(read(filepath.Join(vectors, "variable.input.sexp"))), `("west" null) `, "", 1)
	add("accept_variable", "Graph", []byte(variable))

	var refuseNames []string
	names, err := filepath.Glob(filepath.Join(vectors, "refusals", "*.sexp"))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(names)
	for _, path := range names {
		stem := strings.TrimSuffix(filepath.Base(path), ".sexp")
		if stem == "fraction" {
			continue // fixed(16,16) is refused by the Lua table surface
		}
		root := "Document"
		if stem == "duplicate-map" || stem == "forward-reference" {
			root = "Graph"
		}
		add("refuse_"+stem, root, read(path))
		refuseNames = append(refuseNames, stem)
	}
	if len(refuseNames) < 20 {
		t.Fatalf("only %d refusal vectors ran", len(refuseNames))
	}

	inputs := map[string][]byte{}
	var quoted []string
	for _, c := range cases {
		inputs[c.name] = c.text
		inputs[c.name+"_root"] = []byte(c.root)
		quoted = append(quoted, `"`+c.name+`"`)
	}
	probe := `local T = require("ProbeTables")
local TF = require("textform")
local J = require("json")
local inputs = require("inputs")
local names = { ` + strings.Join(quoted, ", ") + ` }
local function hex(s) return (s:gsub(".", function(c) return string.format("%02x", c:byte()) end)) end
for _, name in ipairs(names) do
  local root = inputs[name .. "_root"]
  local ok, v = pcall(TF.Load, T, root, inputs[name])
  if not ok then
    local e = v
    local nm = type(e) == "table" and e.name or tostring(e)
    print(name .. "\tREFUSE\t" .. nm)
  else
    local text = J.ToJson(T, root, v)
    if text == nil then
      print(name .. "\tNIL\t")
    else
      print(name .. "\tOK\t" .. hex(text))
    end
  end
end
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

	decode := func(name string, b []byte) map[string]any {
		t.Helper()
		var m map[string]any
		dec := json.NewDecoder(strings.NewReader(string(b)))
		dec.UseNumber()
		if err := dec.Decode(&m); err != nil {
			t.Fatalf("%s: decode: %v", name, err)
		}
		return m
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

	defaults := decode("empty", body("empty"))
	oracle := func(stem string) map[string]any {
		t.Helper()
		o := decode(stem, read(filepath.Join(vectors, stem+".value.json")))
		want := map[string]any{}
		maps.Copy(want, defaults)
		for k, v := range o["explicit_fields"].(map[string]any) {
			if v == nil {
				delete(want, k)
				continue
			}
			want[k] = v
		}
		return want
	}
	for _, stem := range []string{"roadmap", "layout"} {
		got := decode("accept_"+stem, body("accept_"+stem))
		if !jsonEqual(got, oracle(stem)) {
			g, _ := json.Marshal(got)
			w, _ := json.Marshal(oracle(stem))
			t.Errorf("%s: value\n got %s\nwant %s", stem, g, w)
		}
	}

	graph := decode("accept_variable", body("accept_variable"))
	o := decode("variable", read(filepath.Join(vectors, "variable.value.json")))
	nodes := o["nodes"].(map[string]any)
	labels := map[string]int{}
	var items []any
	for _, item := range o["items"].([]any) {
		if item == nil {
			items = append(items, nil)
			continue
		}
		label := string(item.(map[string]any)["node"].(json.Number))
		if n, ok := labels[label]; ok {
			items = append(items, map[string]any{"&node": json.Number(itoaJSON(n))})
			continue
		}
		labels[label] = len(labels) + 1
		def := map[string]any{"&node": json.Number(itoaJSON(labels[label]))}
		maps.Copy(def, nodes[label].(map[string]any))
		items = append(items, def)
	}
	lookup := o["lookup"].(map[string]any)
	delete(lookup, "west")
	wantGraph := map[string]any{"items": items, "lookup": lookup, "blob": ""}
	if !jsonEqual(graph, wantGraph) {
		g, _ := json.Marshal(graph)
		w, _ := json.Marshal(wantGraph)
		t.Errorf("variable: value\n got %s\nwant %s", g, w)
	}

	for _, stem := range refuseNames {
		name := "refuse_" + stem
		r, ok := results[name]
		if !ok {
			t.Fatalf("%s: no probe result", name)
		}
		reason := strings.TrimSpace(string(read(filepath.Join(vectors, "refusals", stem+".reason"))))
		if r.verdict != "REFUSE" || r.body != reason {
			t.Errorf("%s: got %s %s, want REFUSE %s", stem, r.verdict, r.body, reason)
		}
	}
}

func itoaJSON(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

// jsonEqual compares two decoded JSON values. Numbers are compared as their
// JSON spelling so an integer and a float spelling both compare by value.
func jsonEqual(a, b any) bool {
	aa, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return string(aa) == string(bb)
}
