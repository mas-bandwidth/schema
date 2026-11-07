package luautable_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

const nullElemSchema = `package p

table Leaf { a int32 }

table Box {
	lptrs  []*Leaf
	mbytes map[string(8)]*bytes
	mstr   map[string(8)]*string
	mptrs  map[string(8)]*Leaf
}
`

// nullElemProbe reads one text, writes the report and the text ToJson gives
// back, then saves, loads and writes that again (docs/SPEC-TABLES.md §16.2,
// §16.5). Every call's result is checked in Luau before it is printed.
const nullElemProbe = `require("./BoxTable")
local json = require("./schema_json")
local load = require("./schema_load")
local save = require("./schema_save")
local St = require("./schema_tables")
local inputs = require("./inputs")
local function hex(b)
	if b == nil then return "nil" end
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do parts[i + 1] = string.format("%02x", buffer.readu8(b, i)) end
	return table.concat(parts)
end
local function report()
	return { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
end
local r = report()
local v = json.FromJsonTolerant("Box", inputs.text, r)
if v == nil then print("refused"); return end
print(string.format("km=%d u=%d c=%d d=%d w=%d malformed=%s",
	r.kindMismatch, r.unknown, r.clamped, r.duplicate, r.widened, tostring(r.malformed)))
local text = json.ToJson("Box", v)
if text == nil then print("write-failed"); return end
print(hex(text))
local wire = save.save(St.lookup("Box"), v)
if wire == nil then print("save-failed"); return end
local back = load.load("Box", wire, report())
if back == nil then print("load-failed"); return end
local again = json.ToJson("Box", back)
if again == nil then print("rewrite-failed"); return end
print(hex(again))
`

// nullElemReadOnlyProbe is the reversed witness's probe: it never saves, so a
// planted reader defect is the only thing that can move its answer.
const nullElemReadOnlyProbe = `require("./BoxTable")
local json = require("./schema_json")
local inputs = require("./inputs")
local function hex(b)
	if b == nil then return "nil" end
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do parts[i + 1] = string.format("%02x", buffer.readu8(b, i)) end
	return table.concat(parts)
end
local r = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
local v = json.FromJsonTolerant("Box", inputs.text, r)
if v == nil then print("refused"); return end
print(string.format("km=%d u=%d c=%d d=%d w=%d malformed=%s",
	r.kindMismatch, r.unknown, r.clamped, r.duplicate, r.widened, tostring(r.malformed)))
local text = json.ToJson("Box", v)
if text == nil then print("write-failed"); return end
print(hex(text))
`

// nullElemUnit loads nullElemSchema for the Go reference walk.
func nullElemUnit(t *testing.T) *tabletext.Model {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "Box.schema")
	if err := os.WriteFile(path, []byte(nullElemSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{path})
	if err != nil {
		t.Fatalf("the Go reference unit does not load: %v", err)
	}
	return tabletext.NewModel(u)
}

// nullElemPlantedReport is the planted runtime's report line: each null
// counted as a kind_mismatch and no other counter moved.
var nullElemPlantedReport = regexp.MustCompile(`^km=[1-9][0-9]* u=0 c=0 d=0 w=0 malformed=false$`)

// nullElemBox is a fresh Box instance of the Go reference.
func nullElemBox(t *testing.T, m *tabletext.Model) *tabletext.Instance {
	t.Helper()
	box := m.Lookup("Box")
	if box == nil {
		t.Fatal("the Go reference has no table Box")
	}
	return m.New(box)
}

// nullMapEntry clears one map entry's value in the Go reference instance.
func nullMapEntry(t *testing.T, inst *tabletext.Instance, field, key string) {
	t.Helper()
	fv, ok := inst.FieldByKey(field)
	if !ok {
		t.Fatalf("the Go reference has no field %s", field)
	}
	for i := range fv.Entries {
		entry := fv.Entries[i].Tab
		if string(entry.Fields[0].Cell.Str) == key {
			entry.Fields[1].Cell = tabletext.Cell{}
			return
		}
	}
	t.Fatalf("the Go reference has no key %q in %s", key, field)
}

func hexText(b []byte) string { return hexBytes(b) }

// TestLuauJsonNullElemsArrayRefusal pins why the card's two array-of-byte-buffer
// cases (an array of `*bytes` and an array of `*string`, each with a null in
// the middle) cannot be generated: the schema language refuses an array of
// byte buffers at declaration, so no Luau unit can declare one and no subtest
// can be written against it (docs/SPEC-TABLES.md §2.5, §15). The map form of
// the same null is covered above; only the array spelling is refused.
func TestLuauJsonNullElemsArrayRefusal(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		field string
	}{
		{name: "unbounded-bytes", field: "xs []*bytes"},
		{name: "bounded-bytes", field: "xs [..4]*bytes"},
		{name: "unbounded-string", field: "xs []*string"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, "Box.schema")
			src := "package p\n\ntable Box {\n\t" + tc.field + "\n}\n"
			if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := compiler.New().Load([]string{path})
			if err == nil {
				t.Fatalf("the schema %q loads; an array of byte buffers must be refused at declaration", src)
			}
			if !strings.Contains(err.Error(), "an array of byte buffers is a named follow-on") {
				t.Fatalf("the refusal does not name the byte-buffer array rule: %v", err)
			}
		})
	}
}

// TestLuauJsonNullElementsInArraysAndMaps pins a JSON null in a list of
// pointers and in a map of pointers and byte buffers (docs/SPEC-TABLES.md
// §16.2, the pointer, `*bytes`, `*string` and map rows): the null is the null
// reference, no kind_mismatch is counted, the list keeps its length and every
// other element's position, and a map keeps its key with an absent value.
// ToJson writes the same bytes as the Go reference, and Save then Load then
// ToJson writes them again. A planted reader treats the null element as a
// kind mismatch and the test goes red.
func TestLuauJsonNullElementsInArraysAndMaps(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		text   string
		oracle string
		clear  func(*testing.T, *tabletext.Instance)
	}{
		{
			name:   "message-pointer-array-null-first-and-last",
			text:   `{"lptrs":[null,{"a":1},{"a":2},null]}`,
			oracle: `{"lptrs":[null,{"a":1},{"a":2},null]}`,
		},
		{
			name:   "bytes-map-null-value-in-the-middle",
			text:   `{"mbytes":{"a":"AQI=","b":null,"c":"Aw=="}}`,
			oracle: `{"mbytes":{"a":"AQI=","b":"BA==","c":"Aw=="}}`,
			clear: func(t *testing.T, inst *tabletext.Instance) {
				nullMapEntry(t, inst, "mbytes", "b")
			},
		},
		{
			name:   "string-map-null-value-in-the-middle",
			text:   `{"mstr":{"a":"x","b":null,"c":"z"}}`,
			oracle: `{"mstr":{"a":"x","b":"y","c":"z"}}`,
			clear: func(t *testing.T, inst *tabletext.Instance) {
				nullMapEntry(t, inst, "mstr", "b")
			},
		},
		{
			name:   "message-map-null-value-in-the-middle",
			text:   `{"mptrs":{"a":{"a":1},"b":null,"c":{"a":3}}}`,
			oracle: `{"mptrs":{"a":{"a":1},"b":{"a":2},"c":{"a":3}}}`,
			clear: func(t *testing.T, inst *tabletext.Instance) {
				nullMapEntry(t, inst, "mptrs", "b")
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := nullElemUnit(t)
			g := generateFiles(t, map[string]string{"Box.schema": nullElemSchema})
			if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(nullElemProbe), 0o644); err != nil {
				t.Fatal(err)
			}
			inst := nullElemBox(t, m)
			var report tabletext.Report
			if !m.Read(inst, []byte(tc.oracle), &report) || !report.Silent() {
				t.Fatalf("the Go reference refused the oracle text: %+v", report)
			}
			if tc.clear != nil {
				tc.clear(t, inst)
			}
			want, err := m.Write(inst)
			if err != nil {
				t.Fatalf("the Go reference write: %v", err)
			}
			// the form's text ends with exactly one newline (§16.1); the
			// engine's Write returns the object and the file adds it.
			want = append(want, '\n')
			out := string(luaurun.Run(t, g.dir, "main.luau", map[string][]byte{"text": []byte(tc.text)}))
			lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
			if len(lines) != 3 {
				t.Fatalf("%s: bad probe output %q", tc.name, out)
			}
			if lines[0] != "km=0 u=0 c=0 d=0 w=0 malformed=false" {
				t.Errorf("%s: report %q, want no kind_mismatch and no other counter", tc.name, lines[0])
			}
			if lines[1] != hexText(want) {
				t.Errorf("%s: FromJson then ToJson wrote %s, want the Go reference %s", tc.name, lines[1], hexText(want))
			}
			if lines[2] != hexText(want) {
				t.Errorf("%s: Save then Load then ToJson wrote %s, want the Go reference %s", tc.name, lines[2], hexText(want))
			}
		})
	}

	// THE REVERSED WITNESS: put the null-element read of the list and of the
	// map back to a kind_mismatch count; every case must then report exactly
	// its nulls as kind_mismatch and nothing else, so a crash or a refusal of the planted runtime
	// cannot pass for red. The genuine runtime is the subtests' run.
	m := nullElemUnit(t)
	wg := generateFiles(t, map[string]string{"Box.schema": nullElemSchema})
	src, err := os.ReadFile(filepath.Join("runtime", "schema_json.luau"))
	if err != nil {
		t.Fatal(err)
	}
	bad := string(src)
	replacements := []struct{ old, new string }{
		{
			"    elseif fd.elem.shape == \"pointer\" and vs == 'z' then",
			"    elseif false and vs == 'z' then",
		},
		{
			"    if not skip and value_shape(inp) == 'z' and (vf.shape == \"pointer\"\n        or vf.shape == \"string_blob\" or vf.shape == \"bytes_blob\") then",
			"    if false and value_shape(inp) == 'z' and (vf.shape == \"pointer\"\n        or vf.shape == \"string_blob\" or vf.shape == \"bytes_blob\") then",
		},
	}
	for _, r := range replacements {
		if strings.Count(bad, r.old) != 1 {
			t.Fatalf("the witness anchor %q is not in the runtime exactly once", r.old)
		}
		bad = strings.Replace(bad, r.old, r.new, 1)
	}
	if err := os.WriteFile(filepath.Join(wg.dir, "schema_json.luau"), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wg.dir, "main.luau"), []byte(nullElemReadOnlyProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		inst := nullElemBox(t, m)
		var report tabletext.Report
		if !m.Read(inst, []byte(tc.oracle), &report) || !report.Silent() {
			t.Fatalf("the Go reference refused the oracle text: %+v", report)
		}
		if tc.clear != nil {
			tc.clear(t, inst)
		}
		want, err := m.Write(inst)
		if err != nil {
			t.Fatalf("the Go reference write: %v", err)
		}
		want = append(want, '\n')
		out := strings.TrimRight(string(luaurun.Run(t, wg.dir, "main.luau", map[string][]byte{"text": []byte(tc.text)})), "\n")
		first, _, _ := strings.Cut(out, "\n")
		if !nullElemPlantedReport.MatchString(first) {
			t.Errorf("%s: the planted null-element reader reported %q, want only the kind_mismatch the plant counts (output %q)", tc.name, first, out)
		}
		if out == "km=0 u=0 c=0 d=0 w=0 malformed=false\n"+hexText(want) {
			t.Errorf("%s: the planted null-element reader still matches the Go reference, so the witness is not red", tc.name)
		}
	}
}
