package luautable_test

import (
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
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// AN INTEGER MAP KEY READ FROM JSON IS EXACTLY THE KEY ITS TEXT NAMES, OR THE
// READ REFUSES (docs/SPEC-TABLES.md §16.2, the map[K]V row and Numbers): a key
// past either end of its kind's range refuses, a negative token for an
// unsigned kind refuses, and an integral-valued token such as "1.0" is the key
// 1, so a text naming both "1" and "1.0" repeats a key. Every verdict and
// every landed value is schema-go's generated <T>FromJSON and <T>ToJSON over
// the same unit and the same text; no answer is typed into this test.
const jsonIntMapKeySchema = `package probe
table U64
{
    m map[uint64]uint8
}
table U32
{
    m map[uint32]uint8
}
table I8
{
    m map[int8]uint8
}
`

var jsonIntMapKeyCases = []jsonStrictCase{
	{"u64-low", "U64", `{"m":{"0":1}}`},
	{"u64-high", "U64", `{"m":{"18446744073709551615":1}}`},
	{"u64-past-high", "U64", `{"m":{"18446744073709551616":1}}`},
	{"u64-past-high-5", "U64", `{"m":{"18446744073709551621":1}}`},
	{"u64-minus-one", "U64", `{"m":{"-1":1}}`},
	{"u64-integral", "U64", `{"m":{"1.0":1}}`},
	{"u64-repeat", "U64", `{"m":{"1":1,"1.0":2}}`},
	{"u32-low", "U32", `{"m":{"0":1}}`},
	{"u32-high", "U32", `{"m":{"4294967295":1}}`},
	{"u32-past-high", "U32", `{"m":{"4294967296":1}}`},
	{"u32-minus-one", "U32", `{"m":{"-1":1}}`},
	{"u32-integral", "U32", `{"m":{"1.0":1}}`},
	{"u32-repeat", "U32", `{"m":{"1":1,"1.0":2}}`},
	{"i8-low", "I8", `{"m":{"-128":1}}`},
	{"i8-high", "I8", `{"m":{"127":1}}`},
	{"i8-past-low", "I8", `{"m":{"-129":1}}`},
	{"i8-past-high", "I8", `{"m":{"128":1}}`},
	{"i8-integral", "I8", `{"m":{"1.0":1}}`},
	{"i8-repeat", "I8", `{"m":{"1":1,"1.0":2}}`},
}

// jsonIntMapKeyGoOracle answers each case with schema-go's generated read:
// "refused", or "ok " and the hex of the value written back by <T>ToJSON.
func jsonIntMapKeyGoOracle(t *testing.T, u *ir.Unit) map[string]string {
	t.Helper()
	files, err := golang.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := gotable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	var b strings.Builder
	b.WriteString(`package probe

import (
	"fmt"
	"testing"
)

func TestJSONIntMapKeyOracle(t *testing.T) {
`)
	for _, c := range jsonIntMapKeyCases {
		fmt.Fprintf(&b, "\t{ var v %sBuilder; if !v.Init() { t.Fatal(\"builder\") }; var r TableReport; if %sFromJSON(&v, []byte(%q), &r) && r.Err() == nil { if !v.Lock() { t.Fatal(\"lock\") }; out := make([]byte, %sToJSONMeasure(v.AsConst())); %sToJSON(v.AsConst(), out); fmt.Printf(\"@%s|ok %%x\\n\", out) } else { fmt.Printf(\"@%s|refused\\n\") }; v.Shutdown() }\n",
			c.root, c.root, c.text, c.root, c.root, c.name, c.name)
	}
	b.WriteString("}\n")
	dir := t.TempDir()
	files["go.mod"] = fmt.Appendf(nil, "module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\nreplace github.com/mas-bandwidth/serialize.go/v2 => %q\n", tfSerializeGoRuntime(t))
	files["oracle_test.go"] = []byte(b.String())
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", "-run", "^TestJSONIntMapKeyOracle$", "-v", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the generated Go oracle failed: %v\n%s", err, out)
	}
	return jsonStrictAnswers(string(out))
}

// jsonIntMapKeyLuau answers each case with the Luau plain read (J.FromJson)
// and, when it reads, the hex of the value written back by J.ToJson.
func jsonIntMapKeyLuau(t *testing.T, u *ir.Unit) map[string]string {
	t.Helper()
	g := requiredFiles(t, u)
	inputs := map[string][]byte{}
	var b strings.Builder
	b.WriteString(jsonModules(g))
	b.WriteString(`local json = require("./schema_json")
local inputs = require("./inputs")
local function fresh()
	return { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
end
local cases = {
`)
	for _, c := range jsonIntMapKeyCases {
		inputs[c.name] = []byte(c.text)
		fmt.Fprintf(&b, "\t{ %q, %q },\n", c.name, c.root)
	}
	b.WriteString(`}
for _, c in cases do
	local name, root = c[1], c[2]
	local v = json.FromJson(root, inputs[name], fresh())
	if v == nil then
		print("@" .. name .. "|refused")
	else
		local out = json.ToJson(root, v, fresh())
		local hex = "<no text>"
		if out ~= nil then
			local parts = table.create(buffer.len(out))
			for i = 0, buffer.len(out) - 1 do
				parts[i + 1] = string.format("%02x", buffer.readu8(out, i))
			end
			hex = table.concat(parts)
		end
		print("@" .. name .. "|ok " .. hex)
	end
end
`)
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return jsonStrictAnswers(string(luaurun.Run(t, g.dir, "main.luau", inputs)))
}

func TestLuauJsonIntMapKeyExact(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter and the Go toolchain")
	path := filepath.Join(t.TempDir(), "Keys.schema")
	if err := os.WriteFile(path, []byte(jsonIntMapKeySchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{path})
	if err != nil {
		t.Fatalf("the map-key schema does not load: %v", err)
	}
	want := jsonIntMapKeyGoOracle(t, u)
	got := jsonIntMapKeyLuau(t, u)
	refused := 0
	for _, c := range jsonIntMapKeyCases {
		if want[c.name] == "" {
			t.Fatalf("%s: the Go oracle printed no answer", c.name)
		}
		if want[c.name] == "refused" {
			refused++
		}
		if got[c.name] != want[c.name] {
			t.Errorf("%s %s: Luau %q, Go %q", c.root, c.text, got[c.name], want[c.name])
		}
	}
	if refused == 0 || refused == len(jsonIntMapKeyCases) {
		t.Fatalf("the cases do not split refusals from clean reads (%d of %d refused)", refused, len(jsonIntMapKeyCases))
	}
}
