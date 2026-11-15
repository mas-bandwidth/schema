package luatable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

// jsonBase64Vector is one row of testdata/wire/json-base64-canonical/MANIFEST.tsv.
type jsonBase64Vector struct {
	name, schema, root, file, verdict string
}

func jsonBase64Vectors(t *testing.T, dir string) []jsonBase64Vector {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []jsonBase64Vector
	for line := range strings.SplitSeq(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) < 5 {
			t.Fatalf("manifest row %q: want at least 5 columns", line)
		}
		rows = append(rows, jsonBase64Vector{name: f[0], schema: f[1], root: f[2], file: f[3], verdict: f[4]})
	}
	if len(rows) == 0 {
		t.Fatal("the manifest names no vectors")
	}
	return rows
}

// jsonBase64Probe is the Lua main: it reads each input text through the
// strict FromJson of root and prints one "name=verdict" line, the verdict
// "read:<hex of the field's bytes>" or "refused". The row "roundtrip" writes
// the canonical row back with ToJson and reads that text again. blob says the
// field is a `*bytes` node, boxed as { value = <bytes> }.
const jsonBase64Probe = `local T = require("tables")
local J = require("json")
local inputs = require("inputs")
local root, field, blob = ROOT, FIELD, BLOB
local function hex(s)
  return (s:gsub(".", function(c) return string.format("%02x", c:byte()) end))
end
local function bytes_of(v)
  local b = v[field]
  if blob then b = b and b.value end
  return b
end
local function verdict(text)
  local v = J.FromJson(T, root, text, {})
  if v == nil then return "refused", nil end
  local b = bytes_of(v)
  if b == nil then return "read:nil", v end
  return "read:" .. hex(b), v
end
local names = {}
for k in pairs(inputs) do names[#names + 1] = k end
table.sort(names)
for _, k in ipairs(names) do
  local out, v = verdict(inputs[k])
  print(k .. "=" .. out)
  if k == CANONICAL and v ~= nil then
    print("roundtrip=" .. (verdict(J.ToJson(T, root, v))))
  end
end
`

// runJSONBase64Probe generates schema's unit and offers every input text to
// FromJson of root, returning each input's verdict by name.
func runJSONBase64Probe(t *testing.T, schema, root, field string, blob bool, canonical string, inputs map[string][]byte) map[string]string {
	t.Helper()
	u := luarun.LoadUnit(t, schema)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate %s: %v", schema, err)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	probe := strings.NewReplacer(
		`require("tables")`, `require("`+ModuleName(u)+`")`,
		"ROOT, FIELD, BLOB", luarun.Quote([]byte(root))+", "+luarun.Quote([]byte(field))+", "+map[bool]string{true: "true", false: "false"}[blob],
		"k == CANONICAL", "k == "+luarun.Quote([]byte(canonical)),
	).Replace(jsonBase64Probe)
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(string(luarun.Run(t, dir, "main.lua", inputs)), "\n"), "\n") {
		k, v, _ := strings.Cut(line, "=")
		got[k] = v
	}
	return got
}

// TestLuaJsonBytesReadCanonicalBase64Only pins docs/SPEC-TABLES.md §16.2 and
// §16.8 in the Lua JSON read: a `bytes(N)` or `*bytes` value is read only from
// CANONICAL base64 (standard alphabet, padded, pad bits zero), exactly the
// text ToJson writes for those bytes, and every other spelling refuses the
// read. Every vector of testdata/wire/json-base64-canonical gives its
// manifest's verdict (the verdict is the contract, docs/rulings.json
// refusal-verdict-only), the same texts give the same verdicts through the
// `*bytes` field Asset.data of tables/blobs/Assets.schema, and the text ToJson
// writes for the canonical row reads back to the same bytes.
func TestLuaJsonBytesReadCanonicalBase64Only(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	vecDir := filepath.Join(repo, "testdata", "wire", "json-base64-canonical")
	rows := jsonBase64Vectors(t, vecDir)
	// every vector holds the two bytes AB in the field data (MANIFEST.tsv)
	const ab = "read:4142"
	want := map[string]string{}
	inputs := map[string][]byte{}
	canonical := ""
	schema, root := rows[0].schema, rows[0].root
	for _, r := range rows {
		if r.schema != schema || r.root != root {
			t.Fatalf("%s: schema %s root %s, want every vector on %s %s", r.name, r.schema, r.root, schema, root)
		}
		text, err := os.ReadFile(filepath.Join(vecDir, r.file))
		if err != nil {
			t.Fatal(err)
		}
		inputs[r.name] = text
		switch r.verdict {
		case "read":
			want[r.name] = ab
			canonical = r.name
		case "refused":
			want[r.name] = "refused"
		default:
			t.Fatalf("%s: unknown verdict %q", r.name, r.verdict)
		}
	}
	if canonical == "" {
		t.Fatal("the manifest names no canonical vector")
	}
	want["roundtrip"] = ab

	check := func(label string, got map[string]string) {
		t.Helper()
		for name, w := range want {
			if got[name] != w {
				t.Errorf("%s %s: FromJson answered %q, want %q", label, name, got[name], w)
			}
		}
	}
	check("bytes(8) "+root+".data", runJSONBase64Probe(t, filepath.Join(repo, schema), root, "data", false, canonical, inputs))
	check("*bytes Asset.data", runJSONBase64Probe(t, filepath.Join(repo, "tables", "blobs", "Assets.schema"), "Asset", "data", true, canonical, inputs))
}
