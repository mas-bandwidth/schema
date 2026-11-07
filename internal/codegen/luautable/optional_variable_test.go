package luautable_test

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestLuauOptionalVariableClosureTable transcribes the scalar rows of
// gotable/optionalvariable_test.go (§2.3). The Go tablewire engine supplies
// each expected wire; Luau cannot certify its own encoder by self-comparison.
func TestLuauOptionalVariableClosureTable(t *testing.T) {
	t.Parallel()
	const src = "package probe\n\ntable Inner\n{\n    text *string\n    n int32\n}\n\ntable Outer\n{\n    inner ?Inner\n    tail int32\n}\n"
	schemaDir := t.TempDir()
	schemaPath := filepath.Join(schemaDir, "Probe.schema")
	if err := os.WriteFile(schemaPath, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{schemaPath})
	if err != nil {
		t.Fatal(err)
	}
	model := tabletext.NewModel(u)
	outer := model.Lookup("Outer")
	if outer == nil {
		t.Fatal("Go oracle has no Outer")
	}

	oracle := func(name, input string, clear bool) []byte {
		t.Helper()
		inst := model.New(outer)
		var report tabletext.Report
		if !model.Read(inst, []byte(input), &report) || !report.Silent() {
			t.Fatalf("%s: Go text read: %+v", name, report)
		}
		if clear {
			inner, ok := inst.FieldByKey("inner")
			if !ok || !inner.Present || inner.Cell.Tab == nil {
				t.Fatalf("%s: Go oracle lacks the present pointer before clear", name)
			}
			inner.Present = false // the pointer is deliberately left in storage
		}
		wire, err := tablewire.Encode(model, inst)
		if err != nil {
			t.Fatalf("%s: Go wire encode: %v", name, err)
		}
		back := model.New(outer)
		report = tabletext.Report{}
		ok, err := tablewire.Decode(model, back, wire, &report)
		if err != nil || !ok || !report.Silent() {
			t.Fatalf("%s: Go wire decode: ok=%v err=%v report=%+v", name, ok, err, report)
		}
		again, err := tablewire.Encode(model, back)
		if err != nil || hex.EncodeToString(again) != hex.EncodeToString(wire) {
			t.Fatalf("%s: Go oracle round trip differs: %v", name, err)
		}
		return wire
	}
	absentText := `{"tail":5}`
	presentText := `{"inner":{"text":"hi"},"tail":5}`
	emptyText := `{"inner":{"text":""},"tail":5}`
	want := map[string][]byte{
		"absent":  oracle("absent", absentText, false),
		"present": oracle("present", presentText, false),
		"empty":   oracle("empty", emptyText, false),
		"stale":   oracle("stale", presentText, true),
		"cleared": oracle("cleared", presentText, true),
	}
	if hex.EncodeToString(want["absent"]) != hex.EncodeToString(want["stale"]) ||
		hex.EncodeToString(want["absent"]) != hex.EncodeToString(want["cleared"]) {
		t.Fatal("the Go oracle did not elide an absent value's stale pointer")
	}
	if hex.EncodeToString(want["absent"]) == hex.EncodeToString(want["present"]) ||
		hex.EncodeToString(want["present"]) == hex.EncodeToString(want["empty"]) {
		t.Fatal("the Go oracle did not distinguish absent, present text, and present empty")
	}

	g := generateFiles(t, map[string]string{"Probe.schema": src})
	main := jsonModules(g) + `
local json = require("./schema_json")
local save = require("./schema_save")
local load = require("./schema_load")
local St = require("./schema_tables")
local inputs = require("./inputs")
local d = St.lookup("Outer")
local function report()
	return { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
end
local function from(text)
	local r = report()
	local value = json.FromJson("Outer", text, r)
	assert(value ~= nil and not r.malformed and r.refused == 0 and r.unknown == 0
		and r.kindMismatch == 0 and r.clamped == 0 and r.widened == 0 and r.duplicate == 0,
		"FromJson refused a clean oracle case")
	return value
end
local function hex(w)
	local p = table.create(buffer.len(w))
	for i = 0, buffer.len(w) - 1 do
		p[i + 1] = string.format("%02x", buffer.readu8(w, i))
	end
	return table.concat(p)
end
local function probe(name, value, present, hasText)
	local wire = save.save(d, value)
	local n = save.measure(d, value)
	assert(wire ~= nil and n ~= nil and n == buffer.len(wire), name .. ": save/measure")
	local r = report()
	local back = load.load("Outer", wire, r)
	assert(back ~= nil and not r.malformed and r.refused == 0 and r.unknown == 0
		and r.kindMismatch == 0 and r.clamped == 0 and r.widened == 0 and r.duplicate == 0,
		name .. ": load verdict")
	assert((back.InnerPresent == true) == present, name .. ": loaded presence")
	local textSet = back.Inner ~= nil and back.Inner.Text ~= nil
	assert(textSet == hasText, name .. ": loaded pointer")
	local again = save.save(d, back)
	assert(again ~= nil, name .. ": re-save")
	print(name .. " " .. hex(wire) .. " " .. tostring(n) .. " " .. hex(again))
end
local absent = from(inputs.absent)
local present = from(inputs.present)
local empty = from(inputs.empty)
local stale = from(inputs.present)
stale.InnerPresent = false
assert(stale.Inner ~= nil and stale.Inner.Text ~= nil, "stale pointer vanished before save")
probe("absent", absent, false, false)
probe("present", present, true, true)
probe("empty", empty, true, true)
probe("stale", stale, false, false)
present.InnerPresent = false
assert(present.Inner ~= nil and present.Inner.Text ~= nil, "cleared pointer vanished before save")
probe("cleared", present, false, false)
`
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	out := string(luaurun.Run(t, g.dir, "main.luau", map[string][]byte{
		"absent": []byte(absentText), "present": []byte(presentText), "empty": []byte(emptyText),
	}))
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 5 {
		t.Fatalf("Luau probe produced %d lines, want five: %q", len(lines), out)
	}
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) != 4 {
			t.Fatalf("bad Luau probe line %q", line)
		}
		name := fields[0]
		expected, ok := want[name]
		if !ok {
			t.Fatalf("unexpected Luau case %q", name)
		}
		hexWant := hex.EncodeToString(expected)
		if fields[1] != hexWant || fields[3] != hexWant {
			t.Errorf("%s: Luau save/reload bytes %s / %s, Go writer %s", name, fields[1], fields[3], hexWant)
		}
		measured, err := strconv.Atoi(fields[2])
		if err != nil || measured != len(expected) {
			t.Errorf("%s: Luau measure %q, Go wire length %d (%v)", name, fields[2], len(expected), err)
		}
	}
}
