package luautable_test

// THE TOLERANT READ'S VALUE WRITES BACK (docs/SPEC-TABLES.md §16.1, §16.2):
// a value the tolerant read leaves may hold no slot for a field it never
// placed (an absent optional array, an arm's list), and ToJson writes it all
// the same. Every json-hostile row of messagedemo and tabledemo whose read
// reads is read here and written back, and the text is compared with the
// engine's `<tree>.value.json` (test/conformance/README.md, the decoded value).
// The reversed witness indexes the length and count memos by the slot without
// asking whether it is held, and holds that it is red on exactly the
// messagedemo rows: every value of that unit leaves such a slot, and no
// tabledemo value does.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
)

var jsonValueUnits = []string{"messagedemo", "tabledemo"}

// jsonValueHeldGuard is the clause the reversed witness replaces, and
// jsonValueUnguarded what it puts back.
const jsonValueHeldGuard = `    local held = v[luau_slot]
    if held ~= nil and (fd.raw.s == "str" or fd.raw.s == "bytes") then
      memo.len = memo.len or {}
      memo.len[held] = v[luau_slot .. "Length"] or 0
    end
    if held ~= nil and fd.raw.s == "arr" then
      memo.count = memo.count or {}
      if fd.raw.array == "counted" then memo.count[held] = v[luau_slot .. "Count"] or 0
      elseif fd.raw.array == "list" then memo.count[held] = held.n or #held
      elseif fd.raw.array == "fixed" or fd.raw.array == "keyed" then memo.count[held] = fd.raw.bound or 0 end
    end
`

const jsonValueUnguarded = `    if fd.raw.s == "str" or fd.raw.s == "bytes" then
      memo.len = memo.len or {}
      memo.len[v[luau_slot]] = v[luau_slot .. "Length"] or 0
    end
    if fd.raw.s == "arr" then
      memo.count = memo.count or {}
      local av = v[luau_slot]
      if fd.raw.array == "counted" then memo.count[av] = v[luau_slot .. "Count"] or 0
      elseif fd.raw.array == "list" then memo.count[av] = av ~= nil and (av.n or #av) or 0
      elseif fd.raw.array == "fixed" or fd.raw.array == "keyed" then memo.count[av] = fd.raw.bound or 0 end
    end
`

type jsonValueRow struct {
	unit, name, root string
	text, value      []byte
}

// jsonValueRows is every hostile row of the two units whose read reads and
// whose engine value is pinned.
func jsonValueRows(t *testing.T) []jsonValueRow {
	t.Helper()
	var rows []jsonValueRow
	for _, r := range jsonManifestRows(t) {
		if r.kind != "json-hostile" || r.expected == "refused" || !strings.HasSuffix(r.expected, ",false,read") {
			continue
		}
		if !strings.Contains(" "+strings.Join(jsonValueUnits, " ")+" ", " "+r.unit+" ") {
			continue
		}
		value, err := os.ReadFile(filepath.Join(root, r.path+".value.json"))
		if err != nil {
			continue // a damaged read owes no value
		}
		text, err := os.ReadFile(filepath.Join(root, r.path, r.root+".json"))
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, jsonValueRow{unit: r.unit, name: r.caseName, root: r.root, text: text, value: value})
	}
	return rows
}

// jsonValueRun answers, per row, the written text as hex, or `error <why>`
// when ToJson fails; plant, when set, rewrites the unit's runtime first.
func jsonValueRun(t *testing.T, rows []jsonValueRow, plant func(string) string) map[string]string {
	t.Helper()
	units, _ := readManifest(t)
	got := map[string]string{}
	for _, unit := range jsonValueUnits {
		g := generateUnit(t, units, unit)
		if g.err != nil {
			t.Fatalf("%s does not generate: %v", unit, g.err)
		}
		if plant != nil {
			path := filepath.Join(g.dir, "schema_json.luau")
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(plant(string(src))), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		var b strings.Builder
		b.WriteString(jsonModules(g) + jsonFlags(g))
		b.WriteString(`local json = require("./schema_json")
json.SetFlags(SCHEMA_JSON_FLAGS)
local inputs = require("./inputs")
local function probe(root: string, text: buffer)
	local r = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
	local v = json.FromJsonTolerant(root, text, r)
	if v == nil or r.malformed then print("refused") return end
	local ok, out = pcall(json.ToJson, root, v, r)
	if not ok then print("error " .. string.gsub(tostring(out), "\n", " ")) return end
	if out == nil then print("error nil") return end
	local parts = table.create(buffer.len(out))
	for i = 0, buffer.len(out) - 1 do
		parts[i + 1] = string.format("%02x", buffer.readu8(out, i))
	end
	print(table.concat(parts))
end
`)
		inputs := map[string][]byte{}
		var names []string
		for _, r := range rows {
			if r.unit != unit {
				continue
			}
			key := fmt.Sprintf("in%d", len(names))
			inputs[key] = r.text
			names = append(names, r.name)
			fmt.Fprintf(&b, "probe(%q, inputs.%s)\n", r.root, key)
		}
		if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		out := strings.Split(strings.TrimRight(string(luaurun.Run(t, g.dir, "main.luau", inputs)), "\n"), "\n")
		if len(out) != len(names) {
			t.Fatalf("%s: %d answers for %d rows:\n%s", unit, len(out), len(names), strings.Join(out, "\n"))
		}
		for i, name := range names {
			got[name] = out[i]
		}
	}
	return got
}

// jsonValueRed is every row whose written text is not the engine's, sorted.
func jsonValueRed(rows []jsonValueRow, got map[string]string) []string {
	var red []string
	for _, r := range rows {
		if got[r.name] != hexBytes(r.value) {
			red = append(red, r.name)
		}
	}
	sort.Strings(red)
	return red
}

func TestLuauJsonTolerantValueWritesBack(t *testing.T) {
	t.Parallel()
	rows := jsonValueRows(t)
	per := map[string]int{}
	for _, r := range rows {
		per[r.unit]++
	}
	for _, unit := range jsonValueUnits {
		if per[unit] == 0 {
			t.Fatalf("no %s row reads with a pinned value: the gate would pass vacuously", unit)
		}
	}
	got := jsonValueRun(t, rows, nil)
	for _, r := range rows {
		if got[r.name] != hexBytes(r.value) {
			t.Errorf("%s (%s %s): ToJson of the tolerant read is not the engine's value: %.200s", r.name, r.unit, r.root, got[r.name])
		}
	}
	t.Logf("values compared: %d messagedemo rows, %d tabledemo rows", per["messagedemo"], per["tabledemo"])
}

func TestLuauJsonTolerantValueNilSlotNegativeControl(t *testing.T) {
	t.Parallel()
	rows := jsonValueRows(t)
	got := jsonValueRun(t, rows, func(src string) string {
		if strings.Count(src, jsonValueHeldGuard) != 1 {
			t.Fatalf("the held-slot guard does not stand once in schema_json.luau")
		}
		return strings.Replace(src, jsonValueHeldGuard, jsonValueUnguarded, 1)
	})
	var want []string
	for _, r := range rows {
		if r.unit == "messagedemo" {
			want = append(want, r.name)
		}
	}
	sort.Strings(want)
	red := jsonValueRed(rows, got)
	if strings.Join(red, " ") != strings.Join(want, " ") {
		t.Fatalf("the unguarded memo is red on %q, want exactly the messagedemo rows %q", red, want)
	}
	for _, name := range red {
		if !strings.HasPrefix(got[name], "error ") || !strings.Contains(got[name], "table index is nil") {
			t.Fatalf("%s went red without the nil index: %.200s", name, got[name])
		}
	}
}
