package luautable_test

// A TEXT READ ANSWERS THE CONSTRUCTOR'S PRESENCE (docs/PORT-LUAU.md, THE TYPE
// MAPPING): an optional's <Name>Present companion is a boolean, false when the
// key is absent or null and true when the key is present, at the root, in a
// nested table and in an array element. The expected answer for each text
// comes from schema-go's generated <Root>FromJSON over the same unit; none is
// typed into this test.

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

const luauJsonPresenceSchema = `package probe

table Inner
{
    n ?int32
}

table Root
{
    o ?int32
    inner Inner
    items [2]Inner
}
`

var luauJsonPresenceCases = []struct {
	name, text string
}{
	{"empty", `{}`},
	{"null", `{"o":null}`},
	{"value", `{"o":5}`},
	{"nested-empty", `{"inner":{}}`},
	{"nested-null", `{"inner":{"n":null}}`},
	{"nested-value", `{"inner":{"n":7}}`},
	{"array-empty", `{"items":[{},{}]}`},
	{"array-null", `{"items":[{"n":null},{"n":null}]}`},
	{"array-value", `{"items":[{"n":9},{"n":11}]}`},
}

// luauJsonPresenceOracle answers each text with schema-go's generated read:
// "refused", or every optional's presence and value at the root, in the nested
// table, and in both fixed-array elements.
func luauJsonPresenceOracle(t *testing.T, u *ir.Unit) map[string]string {
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

func TestJSONPresenceOracle(t *testing.T) {
	check := func(name, text string) {
		var b RootBuilder
		if !b.Init() {
			t.Fatal("builder")
		}
		defer b.Shutdown()
		var r TableReport
		if !RootFromJSON(&b, []byte(text), &r) || r.Err() != nil {
			fmt.Printf("@%s|refused\n", name)
			return
		}
		if !b.Lock() {
			t.Fatal("lock")
		}
		v := b.AsConst()
		field := func(name string, present bool, value int32) string {
			if present {
				return fmt.Sprintf("%s.present=true,value=%d", name, value)
			}
			return fmt.Sprintf("%s.present=false", name)
		}
		fmt.Printf("@%s|%s,%s,%s,%s\n", name,
			field("o", v.OPresent, v.O),
			field("inner.n", v.Inner.NPresent, v.Inner.N),
			field("items[0].n", v.Items[0].NPresent, v.Items[0].N),
			field("items[1].n", v.Items[1].NPresent, v.Items[1].N))
	}
`)
	for _, c := range luauJsonPresenceCases {
		fmt.Fprintf(&b, "\tcheck(%q, %q)\n", c.name, c.text)
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
	cmd := exec.Command("go", "test", "-count=1", "-run", "^TestJSONPresenceOracle$", "-v", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the generated Go oracle failed: %v\n%s", err, out)
	}
	got := map[string]string{}
	for line := range strings.SplitSeq(string(out), "\n") {
		if after, ok := strings.CutPrefix(line, "@"); ok {
			if name, answer, ok := strings.Cut(after, "|"); ok {
				got[name] = answer
			}
		}
	}
	return got
}

// luauJsonPresenceRead reads each text through J.FromJson and prints the same
// presence and value fields as the oracle.
func luauJsonPresenceRead(t *testing.T, u *ir.Unit) map[string]string {
	t.Helper()
	g := requiredFiles(t, u)
	inputs := map[string][]byte{}
	var b strings.Builder
	b.WriteString(jsonModules(g))
	b.WriteString(jsonFlags(g))
	b.WriteString(`local json = require("./schema_json")
json.SetFlags(SCHEMA_JSON_FLAGS)
local inputs = require("./inputs")
local cases = {
`)
	for _, c := range luauJsonPresenceCases {
		inputs[c.name] = []byte(c.text)
		fmt.Fprintf(&b, "\t{ %q, %q },\n", c.name, "Root")
	}
	b.WriteString(`}
local function field(name, p, v)
	local s = name .. ".present=" .. tostring(p)
	if p then s = s .. ",value=" .. tostring(v) end
	return s
end
for _, c in ipairs(cases) do
	local name, root = c[1], c[2]
	local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
	local v = json.FromJson(root, inputs[name], report)
	if v == nil then
		print("@" .. name .. "|refused")
	else
		local items = v.Items
		print("@" .. name ..
			"|" .. field("o", v.OPresent, v.O) ..
			"," .. field("inner.n", v.Inner.NPresent, v.Inner.N) ..
			"," .. field("items[0].n", items[1].NPresent, items[1].N) ..
			"," .. field("items[1].n", items[2].NPresent, items[2].N))
	end
end
`)
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	out := luaurun.Run(t, g.dir, "main.luau", inputs)
	got := map[string]string{}
	for line := range strings.SplitSeq(string(out), "\n") {
		if after, ok := strings.CutPrefix(line, "@"); ok {
			if name, answer, ok := strings.Cut(after, "|"); ok {
				got[name] = answer
			}
		}
	}
	return got
}

func TestLuauJsonPresenceSlotsAreBooleans(t *testing.T) {
	slowtest.Gate(t, "the pinned Luau interpreter and the schema-go oracle")
	t.Parallel()
	path := filepath.Join(t.TempDir(), "Presence.schema")
	if err := os.WriteFile(path, []byte(luauJsonPresenceSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{path})
	if err != nil {
		t.Fatalf("the presence schema does not load: %v", err)
	}
	want := luauJsonPresenceOracle(t, u)
	got := luauJsonPresenceRead(t, u)
	falseSlots, trueSlots := 0, 0
	for _, c := range luauJsonPresenceCases {
		if want[c.name] == "" {
			t.Fatalf("%s: the Go oracle printed no answer", c.name)
		}
		falseSlots += strings.Count(want[c.name], "present=false")
		trueSlots += strings.Count(want[c.name], "present=true")
		if got[c.name] != want[c.name] {
			t.Errorf("%s %s: Luau %q, Go %q", c.name, c.text, got[c.name], want[c.name])
		}
	}
	if falseSlots == 0 || trueSlots == 0 {
		t.Fatalf("the cases do not cover both absent and present optionals (%d false, %d true)", falseSlots, trueSlots)
	}
}
