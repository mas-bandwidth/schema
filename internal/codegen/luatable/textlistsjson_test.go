package luatable

// Text lists and unbounded text in the JSON text form (docs/SPEC-TABLES.md
// §2.13, §2.13.1, §16.2): a text list's text is a plain array of strings
// (`null` for an absent ?string element) and an unbounded map key is an object
// key with no length bound. The two binary cards' units are driven through the
// Lua J.FromJson / J.ToJson and through the GENERATED GO's FromJsonTolerant /
// ToJson; every case's text and report must agree.

import (
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// jsonTextListsSchema is the bounded text-list binary card's unit: the four
// spellings, with and without a total, and a plain twin.
const jsonTextListsSchema = `package probe

table L
{
    names [..4]string(8)
    first [..4]?string(8)
    repos []string(8)
    bases []?string(8)
}

table T
{
    names [..4]string(8) | total = 12
    first [..4]?string(8) | total = 12
    repos []string(8) | total = 12
    bases []?string(8) | total = 12
}

table W
{
    names [..4]string(8)
    first [..4]?string(8)
    repos []string(8)
    bases []?string(8)
}
`

// jsonUnboundedTextSchema is the unbounded binary card's unit: a plain
// unbounded list, a counted `?` list, a counted plain list and an unbounded
// map key, beside their bounded twin.
const jsonUnboundedTextSchema = `package probe

table Where
{
    rules []string
    first [..4]?string
    capped [..2]string
    byname map[string]int32
}

table Bounded
{
    rules []string(8)
    first [..4]?string(8)
    capped [..2]string(8)
    byname map[string(8)]int32
}
`

type jsonCase struct {
	name string
	root string
	text string
}

// TestLuaTextListsJson holds §16.2's text-list and unbounded-text rows: the
// Lua read of each case agrees with the generated Go's report (the six
// counters, the verdict and the named reason), and the Lua write of the value
// it read is the generated Go's text byte for byte. The cases cover the plain
// shape, an absent ? element, the element and count clamps, a `total`'s kept
// prefix, wrong JSON types, a duplicate map key and a key and element longer
// than the walker's key scratch (the unbounded rows' no-bound rule).
func TestLuaTextListsJson(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "go and the pinned lua")
	cases := map[string][]jsonCase{
		"textlists": {
			{"plain", "L", `{"names":["a","b"],"first":["x",null],"repos":["r1",""],"bases":[null,"z"]}`},
			{"element-clamp", "L", `{"names":["123456789"],"first":[null,""],"repos":["r1"],"bases":[]}`},
			{"count-clamp", "L", `{"names":["a","b","c","d",""],"first":[],"repos":[],"bases":[]}`},
			{"wrong-types", "L", `{"names":[null,1,true],"first":[1,2],"repos":[],"bases":[]}`},
			{"total-prefix", "T", `{"names":["abcdefgh","ijkl","m"],"first":["abcdefgh",null,"m"],"repos":["abcdefgh","ijkl"],"bases":[null,"abcdefgh","","ijkl"]}`},
			{"plain-twin", "W", `{"names":["a","b"],"first":["x",null],"repos":["r1",""],"bases":[null,"z"]}`},
		},
		"unbounded": {
			{"plain", "Where", `{"rules":["a","b"],"first":["x",null],"capped":["c"],"byname":{"k1":1,"k2":2}}`},
			{"long-element-and-key", "Where", `{"rules":["` + strings.Repeat("x", 300) + `"],"first":[],"capped":[],"byname":{"` + strings.Repeat("k", 300) + `":1}}`},
			{"count-clamp", "Where", `{"rules":["a"],"first":[],"capped":["a","b","c"],"byname":{}}`},
			{"wrong-types-and-duplicate", "Where", `{"rules":[1,null],"first":[null,"x"],"capped":[null],"byname":{"k":1,"k":2}}`},
			{"bounded-twin", "Bounded", `{"rules":["a"],"first":[],"capped":["c"],"byname":{"k1":1}}`},
		},
	}
	schemas := map[string]string{
		"textlists": jsonTextListsSchema,
		"unbounded": jsonUnboundedTextSchema,
	}
	for name, unitCases := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			schemaDir := t.TempDir()
			path := filepath.Join(schemaDir, name+".schema")
			if err := os.WriteFile(path, []byte(schemas[name]), 0o644); err != nil {
				t.Fatal(err)
			}
			u := luarun.LoadUnit(t, path)
			files, err := Generate(u)
			if err != nil {
				t.Fatalf("generate Lua: %v", err)
			}
			dir := t.TempDir()
			luarun.WriteFiles(t, dir, files)
			for _, c := range unitCases {
				if err := os.MkdirAll(filepath.Join(dir, "cases"), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "cases", c.name+".json"), []byte(c.text), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want := generatedJSONOracle(t, u, unitCases)
			var probe strings.Builder
			probe.WriteString(`local T = require("` + ModuleName(u) + `")
local J = require("json")
local function hex(s)
  if s == nil then return "" end
  return (s:gsub(".", function(c) return string.format("%02x", c:byte()) end))
end
local cases = {`)
			for _, c := range unitCases {
				fmt.Fprintf(&probe, "\n  { name = %q, root = %q },", c.name, c.root)
			}
			probe.WriteString(`
}
for _, c in ipairs(cases) do
  local f = assert(io.open("cases/" .. c.name .. ".json", "rb"))
  local text = f:read("*a")
  f:close()
  local r = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
  -- the TOLERANT read, as the Go oracle's FromJsonTolerant (§16.11): the
  -- plain FromJson is the strict read and answers nil on a clamp
  local v = J.FromJsonTolerant(T, c.root, text, r)
  local ok = not r.malformed
  local out = nil
  if ok then out = J.ToJson(T, c.root, v) end
  local refused = ok and out == nil
  print(table.concat({ c.name, tostring(ok), tostring(r.unknown), tostring(r.kindMismatch),
    tostring(r.widened), tostring(r.clamped), tostring(r.duplicate), tostring(r.malformed),
    (r.Reason ~= nil and r.Reason ~= "") and r.Reason or (r.ClampedField and ("clamped: " .. r.ClampedField) or ""),
    hex(out), tostring(refused) }, "|"))
end
`)
			if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe.String()), 0o644); err != nil {
				t.Fatal(err)
			}
			got := strings.TrimRight(string(luarun.Run(t, dir, "main.lua", nil)), "\n")
			want = strings.TrimRight(want, "\n")
			if got != want {
				t.Fatalf("the Lua JSON walk differs from the generated Go\n--- got ---\n%s\n--- want ---\n%s", got, want)
			}
		})
	}
	t.Run("total-write", func(t *testing.T) {
		t.Parallel()
		schemaDir := t.TempDir()
		path := filepath.Join(schemaDir, "total.schema")
		if err := os.WriteFile(path, []byte(jsonTextListsSchema), 0o644); err != nil {
			t.Fatal(err)
		}
		u := luarun.LoadUnit(t, path)
		files, err := Generate(u)
		if err != nil {
			t.Fatalf("generate Lua: %v", err)
		}
		dir := t.TempDir()
		luarun.WriteFiles(t, dir, files)
		probe := `local T = require("` + ModuleName(u) + `")
local J = require("json")
local function refused(v)
  return J.ToJson(T, "T", v) == nil
end
local within = T.NewT()
within.names = { { value = "abcd" }, { value = "efgh" }, { value = "ijkl" } }
local over = T.NewT()
over.names = { { value = "abcd" }, { value = "efgh" }, { value = "ijkl" }, { value = "m" } }
print(tostring(refused(within)) .. " " .. tostring(refused(over)))
`
		if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0o644); err != nil {
			t.Fatal(err)
		}
		got := strings.TrimRight(string(luarun.Run(t, dir, "main.lua", nil)), "\n")
		want := strings.TrimRight(generatedJSONTotalWriteOracle(t, u), "\n")
		if got != want {
			t.Fatalf("the Lua write past a text list's total differs from the generated Go\n--- got ---\n%s\n--- want ---\n%s", got, want)
		}
	})
}

// generatedJSONTotalWriteOracle answers whether the generated Go refuses a
// write within and past a text list's `| total = N` (docs/SPEC-TABLES.md
// §2.13): the value past the total is built by appending within it and then
// growing the last element in place, because Append itself refuses the
// crossing element.
func generatedJSONTotalWriteOracle(t *testing.T, u *ir.Unit) string {
	t.Helper()
	files, err := golang.Generate(u)
	if err != nil {
		t.Fatalf("generate Go: %v", err)
	}
	tables, err := gotable.Generate(u)
	if err != nil {
		t.Fatalf("generate Go tables: %v", err)
	}
	maps.Copy(files, tables)
	files["go.mod"] = []byte("module probe\n\ngo 1.26\n")
	dir := t.TempDir()
	for name, data := range files {
		target := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	src := fmt.Sprintf(`package %s

import (
	"fmt"
	"os"
	"testing"
)

func oracleTotal() (bool, bool) {
	var b TBuilder
	if !b.Init() { panic("init") }
	defer b.Shutdown()
	v := b.GetRoot()
	if !TNamesAppend(v, "abcd") || !TNamesAppend(v, "efgh") || !TNamesAppend(v, "ijkl") { panic("append") }
	within := TToJSONMeasure(v) < 0
	at := v.NamesCount
	copy(v.Names[at].Value[:], "m")
	v.Names[at].ValueLength = 1
	v.NamesCount++
	over := TToJSONMeasure(v) < 0
	return within, over
}

func TestOracleTotal(t *testing.T) {
	within, over := oracleTotal()
	if within { t.Error("the generated Go refused a value within the total") }
	if !over { t.Error("the generated Go took a value past the total") }
	if err := os.WriteFile("oracle.txt", []byte(fmt.Sprintf("%%t %%t", within, over)), 0o644); err != nil { t.Fatal(err) }
}
`, u.Package)
	if err := os.WriteFile(filepath.Join(dir, "oracle_total_test.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "test", "-count=1", "-timeout", "600s", "-run", "^TestOracleTotal$", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=readonly", "NOVA_TEST_NO_HOST=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated Go total oracle: %v\n%s", err, out)
	}
	data, err := os.ReadFile(filepath.Join(dir, "oracle.txt"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// generatedJSONOracle generates the Go unit for u and runs one tolerant read
// and write per case in a t.TempDir(), answering a line per case in the Lua
// probe's shape: name|ok|unknown|kind_mismatch|widened|clamped|duplicate|
// malformed|reason|hex(text)|write_refused.
func generatedJSONOracle(t *testing.T, u *ir.Unit, cases []jsonCase) string {
	t.Helper()
	files, err := golang.Generate(u)
	if err != nil {
		t.Fatalf("generate Go: %v", err)
	}
	tables, err := gotable.Generate(u)
	if err != nil {
		t.Fatalf("generate Go tables: %v", err)
	}
	maps.Copy(files, tables)
	files["go.mod"] = []byte("module probe\n\ngo 1.26\n")
	dir := t.TempDir()
	for name, data := range files {
		target := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, "cases"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		if err := os.WriteFile(filepath.Join(dir, "cases", c.name+".json"), []byte(c.text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var src strings.Builder
	fmt.Fprintf(&src, `package %s

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func oracle(root string, text []byte) (bool, TableReport, []byte, bool) {
	switch root {
`, u.Package)
	var roots []string
	seen := map[string]bool{}
	for _, c := range cases {
		if !seen[c.root] {
			seen[c.root] = true
			roots = append(roots, c.root)
		}
	}
	for _, root := range roots {
		fmt.Fprintf(&src, `	case %q:
		var b %sBuilder
		if !b.Init() { panic("init") }
		defer b.Shutdown()
		var r TableReport
		if !%sFromJSONTolerant(&b, text, &r) { return false, r, nil, false }
		if !b.Lock() { panic("lock") }
		v := b.AsConst()
		n := %sToJSONMeasure(v)
		if n < 0 { return true, r, nil, true }
		out := make([]byte, n)
		if %sToJSON(v, out) != n { panic("tojson") }
		return true, r, out, false
`, root, root, root, root, root)
	}
	src.WriteString(`	}
	panic("unknown root " + root)
}

func TestOracle(t *testing.T) {
	cases := []struct{ name, root string }{`)
	for _, c := range cases {
		fmt.Fprintf(&src, "\n\t\t{%q, %q},", c.name, c.root)
	}
	src.WriteString(`
	}
	var lines []string
	for _, c := range cases {
		text, err := os.ReadFile(filepath.Join("cases", c.name+".json"))
		if err != nil { t.Fatal(err) }
		ok, r, out, refused := oracle(c.root, text)
		reason := r.Reason
		if reason == "" && r.ClampedField != "" {
			reason = "clamped: " + r.ClampedField
		}
		lines = append(lines, fmt.Sprintf("%s|%t|%d|%d|%d|%d|%d|%t|%s|%s|%t",
			c.name, ok, r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate,
			r.Malformed, reason, hex.EncodeToString(out), refused))
	}
	if err := os.WriteFile("oracle.txt", []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil { t.Fatal(err) }
}
`)
	if err := os.WriteFile(filepath.Join(dir, "oracle_test.go"), []byte(src.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "test", "-count=1", "-timeout", "600s", "-run", "^TestOracle$", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=readonly", "NOVA_TEST_NO_HOST=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated Go JSON oracle: %v\n%s", err, out)
	}
	data, err := os.ReadFile(filepath.Join(dir, "oracle.txt"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
