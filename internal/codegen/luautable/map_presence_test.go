package luautable_test

// TestLuauMapPresence holds docs/SPEC-TABLES.md §2.8's OPTIONAL MAP, read
// through §2.3, in the Luau table runtime: `tags ?map[string(8)]int32` carries
// its TagsPresent companion, and ABSENT, PRESENT-EMPTY and
// PRESENT-WITH-ENTRIES are three values. Each wire is computed in this test by
// schema-go's own engine (internal/tabletext reads the canonical text,
// internal/tablewire encodes it), never typed by hand, and the Luau runtime
// loads it and saves it back byte for byte.
//
// The present-empty oracle spells the same root, the same field id and the
// same framing (kind 14 over element kind 13) as an optional counted array of
// the entry table: schema-go's map encoder elides an empty map by content, so
// the array framing is where the engine writes a PRESENT empty collection.

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const luauMapPresenceSchema = `package probe

table M
{
    id   int32
    tags ?map[string(8)]int32
}
`

const luauMapPresenceEmptyOracle = `package probe

table M
{
    id   int32
    tags ?[..4]Entry
}

table Entry
{
    key   string(8)
    value int32
}
`

// luauMapPresenceUnit loads one schema source for the oracle.
func luauMapPresenceUnit(t *testing.T, src string) *ir.Unit {
	t.Helper()
	path := filepath.Join(t.TempDir(), "Probe.schema")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{path})
	if err != nil {
		t.Fatalf("the unit does not load: %v", err)
	}
	return u
}

// luauMapPresenceOracle encodes one canonical JSON text through schema-go's
// engine: tabletext reads it into the instance, tablewire writes the form-1
// wire.
func luauMapPresenceOracle(t *testing.T, u *ir.Unit, text string) []byte {
	t.Helper()
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup("M"))
	var report tabletext.Report
	if !m.Read(inst, []byte(text), &report) || !report.Silent() {
		t.Fatalf("oracle text read %s: %+v", text, report)
	}
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatalf("oracle encode %s: %v", text, err)
	}
	return wire
}

// luauMapPresenceGenerated emits the Luau type and table surfaces into a
// fresh directory, as the package's other probe tests do.
func luauMapPresenceGenerated(t *testing.T, u *ir.Unit) *generatedUnit {
	t.Helper()
	files, err := luau.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := luautable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	g := &generatedUnit{dir: t.TempDir(), files: files, loaded: true}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(g.dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return g
}

// luauMapPresenceProbe loads each of the three wires, saves the value back
// through schema_save.luau and prints name, the presence companion, the entry count
// and the saved bytes.
func luauMapPresenceProbe(g *generatedUnit) string {
	var b strings.Builder
	b.WriteString("local inputs = require(\"./inputs\")\n")
	b.WriteString("local load = require(\"./schema_load\")\n")
	b.WriteString("local save = require(\"./schema_save\")\n")
	b.WriteString("local St = require(\"./schema_tables\")\n")
	b.WriteString("local json = require(\"./schema_json\")\n")
	var mods []string
	for name := range g.files {
		if strings.HasSuffix(name, "Table.luau") {
			mods = append(mods, strings.TrimSuffix(name, ".luau"))
		}
	}
	sort.Strings(mods)
	for _, m := range mods {
		fmt.Fprintf(&b, "require(\"./%s\")\n", m)
	}
	b.WriteString(`for _, name in { "absent", "empty", "entries" } do
	local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
	local v = load.load("M", inputs[name], report)
	if v == nil or report.refused > 0 then print(name .. ":REFUSED"); return end
	local bytes = save.save(St.lookup("M"), v)
	if bytes == nil then print(name .. ":SAVENIL"); return end
	local parts = table.create(buffer.len(bytes))
	for i = 0, buffer.len(bytes) - 1 do
		parts[i + 1] = string.format("%02x", buffer.readu8(bytes, i))
	end
	print(name .. ":" .. tostring(v.TagsPresent) .. ":" .. tostring(#v.Tags) .. ":" .. table.concat(parts))
	local text = json.ToJson("M", v)
	if text == nil then
		print(name .. ":json:NIL")
	else
		local tp = table.create(buffer.len(text))
		for i = 0, buffer.len(text) - 1 do tp[i + 1] = string.format("%02x", buffer.readu8(text, i)) end
		print(name .. ":json:" .. table.concat(tp))
	end
	local from = json.FromJson("M", inputs[name .. "_json"], report)
	if from == nil then print(name .. ":jsonread:REFUSED"); return end
	local again = save.save(St.lookup("M"), from)
	print(name .. ":jsonread:" .. (again == nil and "SAVENIL" or table.concat((function()
		local out = table.create(buffer.len(again))
		for i = 0, buffer.len(again) - 1 do out[i + 1] = string.format("%02x", buffer.readu8(again, i)) end
		return out
	end)())))
end
local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
local nullv = json.FromJson("M", inputs.null_json, report)
if nullv == nil then print("null:REFUSED"); return end
local nullbytes = save.save(St.lookup("M"), nullv)
if nullbytes == nil then print("null:SAVENIL"); return end
local np = table.create(buffer.len(nullbytes))
for i = 0, buffer.len(nullbytes) - 1 do np[i + 1] = string.format("%02x", buffer.readu8(nullbytes, i)) end
print("null:" .. tostring(nullv.TagsPresent == true) .. ":" .. tostring(nullv.Tags == nil and 0 or #nullv.Tags) .. ":" .. table.concat(np))
`)
	return b.String()
}

func TestLuauMapPresence(t *testing.T) {
	t.Parallel()
	u := luauMapPresenceUnit(t, luauMapPresenceSchema)
	empty := luauMapPresenceUnit(t, luauMapPresenceEmptyOracle)
	shapes := []struct {
		name, present, count, json string
		wire                       []byte
	}{
		{"absent", "false", "0", `{"id":1}`, luauMapPresenceOracle(t, u, `{"id":1}`)},
		{"empty", "true", "0", `{"id":1,"tags":{}}`, luauMapPresenceOracle(t, empty, `{"id":1,"tags":[]}`)},
		{"entries", "true", "1", `{"id":1,"tags":{"a":5}}`, luauMapPresenceOracle(t, u, `{"id":1,"tags":{"a":5}}`)},
	}
	inputs := map[string][]byte{}
	seen := map[string]string{}
	for _, shape := range shapes {
		inputs[shape.name] = shape.wire
		inputs[shape.name+"_json"] = []byte(shape.json)
		if prior, dup := seen[string(shape.wire)]; dup {
			t.Errorf("%s and %s have the same oracle wire %x", prior, shape.name, shape.wire)
		}
		seen[string(shape.wire)] = shape.name
	}
	inputs["null_json"] = []byte(`{"id":1,"tags":null}`)
	g := luauMapPresenceGenerated(t, u)
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(luauMapPresenceProbe(g)), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.TrimSpace(string(luaurun.Run(t, g.dir, "main.luau", inputs)))
	lines := strings.Split(out, "\n")
	if len(lines) != len(shapes)*3+1 {
		t.Fatalf("the probe printed %d lines, want %d:\n%s", len(lines), len(shapes)*3+1, out)
	}
	for _, shape := range shapes {
		line := func(kind string) string {
			prefix := shape.name + ":"
			if kind != "" {
				prefix = shape.name + ":" + kind + ":"
			}
			for _, candidate := range lines {
				if strings.HasPrefix(candidate, prefix) {
					return candidate
				}
			}
			return ""
		}
		got := strings.SplitN(line(""), ":", 4)
		if len(got) != 4 {
			t.Errorf("%s: unreadable probe line %q", shape.name, line(""))
			continue
		}
		if got[1] != shape.present || got[2] != shape.count {
			t.Errorf("%s: presence %s count %s, want %s and %s", shape.name, got[1], got[2], shape.present, shape.count)
		}
		if want := fmt.Sprintf("%x", shape.wire); got[3] != want {
			t.Errorf("%s: saved wire\n got  %s\n want %s", shape.name, got[3], want)
		}
		var compact bytes.Buffer
		if err := json.Compact(&compact, []byte(shape.json)); err != nil {
			t.Fatalf("%s: expected JSON: %v", shape.name, err)
		}
		gotJSON, err := hex.DecodeString(strings.TrimPrefix(line("json"), shape.name+":json:"))
		if err != nil {
			t.Errorf("%s: JSON line is not hex: %v", shape.name, err)
		} else {
			var gotCompact bytes.Buffer
			if err := json.Compact(&gotCompact, gotJSON); err != nil {
				t.Errorf("%s: JSON read does not compact: %v\n%s", shape.name, err, gotJSON)
			} else if !bytes.Equal(gotCompact.Bytes(), compact.Bytes()) {
				t.Errorf("%s: JSON\n got  %s\n want %s", shape.name, gotCompact.String(), compact.String())
			}
		}
		if want := shape.name + ":jsonread:" + fmt.Sprintf("%x", shape.wire); line("jsonread") != want {
			t.Errorf("%s: JSON read then save\n got  %s\n want %s", shape.name, line("jsonread"), want)
		}
	}
	if want := "null:false:0:" + fmt.Sprintf("%x", shapes[0].wire); lines[len(lines)-1] != want {
		t.Errorf("JSON null\n got  %s\n want %s", lines[len(lines)-1], want)
	}
}
