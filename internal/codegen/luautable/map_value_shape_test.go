package luautable_test

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// mapShapeProbe reads each case of the map-value-shape corpus through the
// strict walk and answers its verdict and, for a read, the bytes Save writes.
const mapShapeProbe = `require("./ProbeTable")
local json = require("./schema_json")
local save = require("./schema_save")
local St = require("./schema_tables")
local inputs = require("./inputs")
local function report()
	return { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
end
local function hex(b)
	local out = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do out[i + 1] = string.format("%02x", buffer.readu8(b, i)) end
	return table.concat(out)
end
local i = 0
while inputs["case" .. i] ~= nil do
	local v = json.fromJsonWalk("Maps", inputs["case" .. i], report())
	if v == nil then
		print(i .. " refused")
	else
		print(i .. " " .. hex(save.save(St.lookup("Maps"), v)))
	end
	i = i + 1
end
`

// TestLuauMapValueShapeNative holds `map[K]?T` presence (docs/SPEC-TABLES.md
// §16.2, the map row; §2.3): `null` is the one spelling of an ABSENT value
// and its key is still an entry; every other lawful value is PRESENT, its
// default included, and rides the wire. Each case of the map-value-shape
// feature-witness corpus is read by the strict Luau walk with the Go
// engine's verdict, and a read saves the bytes the engine encodes. Then a
// canonical text, the engine's own write of its read, round-trips byte for
// byte through FromJson and ToJson, and again through Save and Load. Before
// the fix the walk counted the `null` a kind mismatch and refused the
// document (the text form read refused every document holding one), and the
// wire load dropped a map value's presence, so a reload wrote it `null`.
func TestLuauMapValueShapeNative(t *testing.T) {
	t.Parallel()
	fixture := filepath.Join("..", "..", "..", "testdata", "conformance", "feature-witnesses", "map-value-shape")
	schema, err := os.ReadFile(filepath.Join(fixture, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(fixture, "cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct{ Name, Input string }
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("the map value shape corpus has no case: the check would pass over the empty set")
	}
	schemaPath := filepath.Join(t.TempDir(), "Probe.schema")
	if err := os.WriteFile(schemaPath, schema, 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{schemaPath})
	if err != nil {
		t.Fatalf("the Go reference unit does not load: %v", err)
	}
	model := tabletext.NewModel(u)
	g := generateFiles(t, map[string]string{"Probe.schema": string(schema)})
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(mapShapeProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	inputs := map[string][]byte{}
	want := make([]string, len(cases))
	reads := 0
	for i, tc := range cases {
		inputs[fmt.Sprintf("case%d", i)] = []byte(tc.Input)
		value := model.New(u.Tables["Maps"])
		var rep tabletext.Report
		if !model.Read(value, []byte(tc.Input), &rep) {
			want[i] = fmt.Sprintf("%d refused", i)
			continue
		}
		wire, err := tablewire.Encode(model, value)
		if err != nil {
			t.Fatalf("%s: the Go reference does not encode its read: %v", tc.Name, err)
		}
		want[i] = fmt.Sprintf("%d %s", i, hex.EncodeToString(wire))
		reads++
	}
	if reads == 0 {
		t.Fatal("the Go reference reads no case: no presence is compared")
	}
	got := strings.Split(strings.TrimRight(string(luaurun.Run(t, g.dir, "main.luau", inputs)), "\n"), "\n")
	if len(got) != len(cases) {
		t.Fatalf("the Luau probe answered %d cases, want %d:\n%s", len(got), len(cases), strings.Join(got, "\n"))
	}
	for i, tc := range cases {
		if got[i] != want[i] {
			t.Errorf("%s: the Luau walk and save answer %q, the engine %q", tc.Name, got[i], want[i])
		}
	}

	// the canonical text: absent, present at the default and present past
	// it, keyed by a string and by an integer, over an int32 and a bool
	const box = "package px\ntable Box\n{\n\tm map[string(8)]?int32\n\tn map[int32]?bool\n}\n"
	boxPath := filepath.Join(t.TempDir(), "Carton.schema")
	if err := os.WriteFile(boxPath, []byte(box), 0o644); err != nil {
		t.Fatal(err)
	}
	bu, err := compiler.New().Load([]string{boxPath})
	if err != nil {
		t.Fatalf("the Go reference unit does not load: %v", err)
	}
	bm := tabletext.NewModel(bu)
	const canonical = "{\n  \"m\": {\n    \"east\": 2,\n    \"none\": null,\n    \"zero\": 0\n  },\n  \"n\": {\n    \"1\": null,\n    \"2\": false,\n    \"3\": true\n  }\n}\n"
	var rep tabletext.Report
	inst := bm.New(bm.Lookup("Box"))
	if !bm.Read(inst, []byte(canonical), &rep) {
		t.Fatalf("the Go reference refuses the canonical text (%+v)", rep)
	}
	if goText, err := bm.Write(inst); err != nil || string(goText)+"\n" != canonical {
		t.Fatalf("the Go reference writes %q (%v), want the canonical text %q with its final newline", goText, err, canonical)
	}
	bg := generateFiles(t, map[string]string{"Carton.schema": box})
	if err := os.WriteFile(filepath.Join(bg.dir, "main.luau"), []byte(jsonElemProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	out := string(luaurun.Run(t, bg.dir, "main.luau", map[string][]byte{"text": []byte(canonical)}))
	head, rest, _ := strings.Cut(out, "\n")
	if head != "km=0 u=0" {
		t.Fatalf("FromJson reported %q, want km=0 u=0 (output %q)", head, out)
	}
	if want := canonical + "\n" + canonical + "\n"; rest != want {
		t.Errorf("ToJson, then Save, Load and ToJson, wrote\n%s\nwant every entry and each value's presence kept, twice:\n%s", rest, want)
	}
}
