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

// docs/SPEC-TABLES.md §16.2 Numbers: a JSON number token's LENGTH is never why
// a read refuses; the document's own size is the only bound. Each case's
// verdict and wire are schema-go's over the same unit and text.
const jsonLongNumberSchema = `package probe

table Root
{
    f float64
    u uint64
}
`

type jsonLongNumberCase struct {
	name, text string
}

// jsonLongNumberCases holds the three tokens §16.2 names: an integer longer
// than a conversion's working precision under a key the reader cannot name, a
// float whose fraction runs past it, and an integer past the field's domain.
func jsonLongNumberCases() []jsonLongNumberCase {
	digits := "1" + strings.Repeat("0", 599)
	return []jsonLongNumberCase{
		{"unknown-600-digit-integer", `{"zz":` + digits + `}`},
		{"float-600-zeros", `{"f":1.` + strings.Repeat("0", 600) + `}`},
		{"uint64-600-digit-integer", `{"u":` + digits + `}`},
	}
}

// jsonLongNumberGoOracle runs schema-go's generated read over every case and
// prints "@<name>|<read or refused>|<saved wire hex>".
func jsonLongNumberGoOracle(t *testing.T, u *ir.Unit, cases []jsonLongNumberCase) map[string]string {
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
	"encoding/hex"
	"fmt"
	"testing"
)

func TestJSONLongNumberOracle(t *testing.T) {
	cases := []struct{ name, text string }{
`)
	for _, c := range cases {
		fmt.Fprintf(&b, "\t\t{%q, %q},\n", c.name, c.text)
	}
	b.WriteString(`	}
	for _, c := range cases {
		var value RootBuilder
		if !value.Init() {
			t.Fatal("builder")
		}
		var report TableReport
		ok := RootFromJSON(&value, []byte(c.text), &report)
		if ok && report.Err() != nil {
			ok = false
		}
		wire := ""
		if ok {
			if !value.Lock() {
				t.Fatalf("%s: lock", c.name)
			}
			out := make([]byte, RootMeasure(value.AsConst()))
			if RootSave(value.AsConst(), out) != int64(len(out)) {
				t.Fatalf("%s: save", c.name)
			}
			wire = hex.EncodeToString(out)
		}
		value.Shutdown()
		fmt.Printf("@%s|%v|%s\n", c.name, ok, wire)
	}
}
`)
	dir := t.TempDir()
	files["go.mod"] = fmt.Appendf(nil, "module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\nreplace github.com/mas-bandwidth/serialize.go/v2 => %q\n", tfSerializeGoRuntime(t))
	files["oracle_test.go"] = []byte(b.String())
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", "-run", "^TestJSONLongNumberOracle$", "-v", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the generated Go oracle failed: %v\n%s", err, out)
	}
	return jsonStrictAnswers(string(out))
}

// jsonLongNumberLuau runs the strict read and the save over every case under
// the pinned interpreter.
func jsonLongNumberLuau(t *testing.T, g *generatedUnit, cases []jsonLongNumberCase) map[string]string {
	t.Helper()
	inputs := map[string][]byte{}
	var b strings.Builder
	b.WriteString(jsonModules(g))
	b.WriteString(`local json = require("./schema_json")
local save = require("./schema_save")
local St = require("./schema_tables")
local inputs = require("./inputs")
local function hex(b)
	if b == nil then return "" end
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do
		parts[i + 1] = string.format("%02x", buffer.readu8(b, i))
	end
	return table.concat(parts)
end
local cases = {
`)
	for _, c := range cases {
		inputs[c.name] = []byte(c.text)
		fmt.Fprintf(&b, "\t{ %q },\n", c.name)
	}
	b.WriteString(`}
for _, c in ipairs(cases) do
	local name = c[1]
	local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
	local v = json.FromJson("Root", inputs[name], report)
	local wire = ""
	if v ~= nil then wire = hex(save.save(St.lookup("Root"), v)) end
	print("@" .. name .. "|" .. tostring(v ~= nil) .. "|" .. wire)
end
`)
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return jsonStrictAnswers(string(luaurun.Run(t, g.dir, "main.luau", inputs)))
}

// TestLuauJsonLongNumberTokens holds the strict read's verdict and the saved
// wire to schema-go's on a 600-digit integer under an unknown key, a float
// with 600 fraction digits, and a 600-digit integer past a uint64's domain.
func TestLuauJsonLongNumberTokens(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter and the Go toolchain")
	dir := t.TempDir()
	path := filepath.Join(dir, "LongNumber.schema")
	if err := os.WriteFile(path, []byte(jsonLongNumberSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{path})
	if err != nil {
		t.Fatalf("the long-number schema does not load: %v", err)
	}
	cases := jsonLongNumberCases()
	want := jsonLongNumberGoOracle(t, u, cases)
	got := jsonLongNumberLuau(t, requiredFiles(t, u), cases)
	read := 0
	for _, c := range cases {
		if want[c.name] == "" {
			t.Fatalf("%s: the Go oracle printed no answer", c.name)
		}
		if got[c.name] != want[c.name] {
			t.Errorf("%s: Luau %q, Go %q", c.name, got[c.name], want[c.name])
		}
		if strings.HasPrefix(want[c.name], "true|") {
			read++
		}
	}
	if read == 0 || read == len(cases) {
		t.Fatalf("the cases do not split reads from refusals (%d of %d read)", read, len(cases))
	}
}
