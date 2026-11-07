package luautable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestLuauJsonElemReadsAreNotNil pins the three JSON reads that the old
// `elem and false or nil` idiom always made nil (docs/SPEC-TABLES.md §16.2,
// the enum and map rows): an enum "None", an unknown enum name (counted
// unknown), and a `*bytes` body that is not base64 (counted kind_mismatch).
// In an element slot each must stay present as the zero element — false for
// the enum and the null reference — so a list keeps its length and an element
// position, and a map keeps its entry and key. The expected bytes and counts
// come from schema-go's own engine (internal/tabletext and internal/tablewire)
// over the same unit, never typed by hand: json-read compares the Luau FromJson
// then Save wire with the Go FromJson then Encode wire, and json-write compares
// the Luau ToJson of the read value with the Go Write of the same instance.
func TestLuauJsonElemReadsAreNotNil(t *testing.T) {
	t.Parallel()
	// A list of `*bytes` is refused by the checker (§2.5, §15: "an array of byte
	// buffers is a named follow-on"), so the blob read is pinned where it is
	// expressible as an element: a map value.
	const schemaSrc = `package p

enum E { A, B }

table Box
{
    l []E
    m map[string(8)]E
    mb map[string(8)]*bytes
}
`
	g := generateFiles(t, map[string]string{"Box.schema": schemaSrc})

	// The Go reference over the same source: the unit, its instance model and
	// the two walks the emitted code mirrors (§16.1, §16.5).
	srcDir := t.TempDir()
	schemaPath := filepath.Join(srcDir, "Box.schema")
	if err := os.WriteFile(schemaPath, []byte(schemaSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{schemaPath})
	if err != nil {
		t.Fatalf("the Go reference unit does not load: %v", err)
	}
	model := tabletext.NewModel(u)
	st := model.Lookup("Box")
	if st == nil {
		t.Fatal("the Go reference unit has no Box table")
	}

	for _, tc := range []struct{ name, text string }{
		// "None" in the middle of the enum list and as a map value.
		{"enum-none-element", `{"l":["A","None","B"],"m":{"mid":"None","tail":"B"},"mb":{"ok":"AQI="}}`},
		// An unknown enum name in the same two places; the reference counts it.
		{"enum-unknown-element", `{"l":["A","NOPE","B"],"m":{"mid":"NOPE","tail":"B"},"mb":{"ok":"AQI="}}`},
		// A non-base64 body as a `*bytes` map value; the enum list stays clean.
		{"bytes-not-base64-element", `{"l":["A","B"],"m":{"mid":"B"},"mb":{"bad":"!!","ok":"AQI="}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inst := model.New(st)
			var report tabletext.Report
			if !model.Read(inst, []byte(tc.text), &report) {
				t.Fatalf("the Go reference FromJson refused %s: %+v", tc.text, report)
			}
			wantText, err := model.Write(inst)
			if err != nil {
				t.Fatalf("the Go reference ToJson: %v", err)
			}
			wantWire, err := tablewire.Encode(model, inst)
			if err != nil {
				t.Fatalf("the Go reference wire: %v", err)
			}
			wantReport := fmt.Sprintf("km=%d u=%d", report.KindMismatch, report.Unknown)

			l, _ := inst.FieldByKey("l")
			m, _ := inst.FieldByKey("m")
			mb, _ := inst.FieldByKey("mb")
			want := []string{
				wantReport,
				fmt.Sprintf("len=%d", l.Count),
				"keys=" + joinMapKeys(m),
				"bkeys=" + joinMapKeys(mb),
				hexBytes(wantWire),
				hexBytes(append(wantText, '\n')),
			}

			if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(jsonModules(g)+jsonFlags(g)+jsonElemReadsProbe), 0o644); err != nil {
				t.Fatal(err)
			}
			out := string(luaurun.Run(t, g.dir, "main.luau", map[string][]byte{"text": []byte(tc.text)}))
			got := strings.Split(strings.TrimRight(out, "\n"), "\n")
			if len(got) != len(want) {
				t.Fatalf("%s: the probe wrote %d lines, want %d:\n%s", tc.name, len(got), len(want), out)
			}
			for i := range want {
				if got[i] != want[i] {
					t.Errorf("%s: line %d is %q, want %q", tc.name, i+1, got[i], want[i])
				}
			}
			if l.Count == 0 || len(m.Entries) == 0 || len(mb.Entries) == 0 {
				t.Fatalf("%s: the reference kept no element to assert", tc.name)
			}
			t.Logf("%s: list length %d and map keys %s/%s kept, report %s",
				tc.name, l.Count, joinMapKeys(m), joinMapKeys(mb), wantReport)
		})
	}
}

// joinMapKeys renders a map field's entry keys in the writer's ascending
// order, the key set the Luau probe prints (docs/SPEC-TABLES.md §2.8).
func joinMapKeys(f *tabletext.Field) string {
	var keys []string
	for _, e := range f.Entries {
		keys = append(keys, hexBytes(tabletext.MapKeyOf(f.Def, e.Tab).Str))
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}

// jsonElemReadsProbe reads one text, prints the read's kind_mismatch and
// unknown counts, the kept list lengths and map keys, then the wire Save writes
// and the text ToJson writes from the read value (docs/SPEC-TABLES.md §16.2,
// §16.5).
const jsonElemReadsProbe = `local json = require("./schema_json")
json.SetFlags(SCHEMA_JSON_FLAGS)
local save = require("./schema_save")
local St = require("./schema_tables")
local inputs = require("./inputs")
local function hex(b)
	if b == nil then return "nil" end
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do
		parts[i + 1] = string.format("%02x", buffer.readu8(b, i))
	end
	return table.concat(parts)
end
local function keyhex(b)
	local n = buffer.len(b)
	while n > 0 and buffer.readu8(b, n - 1) == 0 do n = n - 1 end
	local parts = table.create(n)
	for i = 0, n - 1 do
		parts[i + 1] = string.format("%02x", buffer.readu8(b, i))
	end
	return table.concat(parts)
end
local function report()
	return { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
end
local r = report()
local v = json.FromJsonTolerant("Box", inputs.text, r)
if v == nil then print("refused"); return end
print(string.format("km=%d u=%d", r.kindMismatch, r.unknown))
print(string.format("len=%d", #v.L))
local keys = {}
for _, e in ipairs(v.M) do keys[#keys + 1] = keyhex(e.Key) end
table.sort(keys)
print("keys=" .. table.concat(keys, ","))
local bkeys = {}
for _, e in ipairs(v.Mb) do bkeys[#bkeys + 1] = keyhex(e.Key) end
table.sort(bkeys)
print("bkeys=" .. table.concat(bkeys, ","))
print(hex(save.save(St.lookup("Box"), v)))
print(hex(json.ToJson("Box", v)))
`
