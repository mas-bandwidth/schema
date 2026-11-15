package luatable

// A JSON string's value is its DECODED value (docs/SPEC-TABLES.md §16.3), and
// an enum is read as the variant NAME and a union as the arm key it carries
// (§16.2): an escaped spelling names the same variant or arm as the plain one.
// Every row is read by the emitted Lua FromJson and by the compiler's own
// engine (internal/tabletext through internal/tablepack), and the two must
// agree on the verdict, the unknown count and the value: the fixed-form wire
// each writes for the value, read back by the engine into its text.

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/tablepack"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

type luaDecodedNameRow struct {
	name string
	text string
}

var luaDecodedNameRows = []luaDecodedNameRow{
	{"enum_plain", `{"loadout":{"grade":"Gold"}}`},
	{"enum_escaped_first", `{"loadout":{"grade":"\u0047old"}}`},
	{"enum_escaped_inner", `{"loadout":{"grade":"G\u006fld"}}`},
	{"enum_escaped_unknown", `{"loadout":{"grade":"Pl\u0061tinum"}}`},
	{"arm_plain", `{"loadout":{"primary":{"effect":{"buff":{"multiplier":2.5}}}}}`},
	{"arm_escaped", `{"loadout":{"primary":{"effect":{"bu\u0066f":{"multiplier":2.5}}}}}`},
	{"arm_escaped_unknown", `{"loadout":{"primary":{"effect":{"bo\u006fst":{"multiplier":2.5}}}}}`},
}

const luaDecodedNameProbe = `local T = require("%s")
local J = require("json")
local inputs = require("inputs")
local function hex(s)
  return (s:gsub(".", function(c) return string.format("%%02x", c:byte()) end))
end
for i = 1, %d do
  local key = "row" .. i
  local report = {}
  local v = J.FromJson(T, "ProfileConfig", inputs[key], report)
  if v == nil then
    print(key .. "|refused|-|-")
  else
    print(key .. "|ok|" .. report.unknown .. "|" .. hex(assert(T.FixedSave("ProfileConfig", { v }))))
  end
end
`

// TestLuaJsonNamesCompareDecoded holds the rule over tables/examples, root
// ProfileConfig: an enum value and a union arm key spelled with an escape read
// as the variant and arm they decode to, as the engine reads them; an escaped
// unknown name reads as unknown in both.
func TestLuaJsonNamesCompareDecoded(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	manifest := filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt")
	units := manifestUnits(t, manifest)
	var paths []string
	for _, p := range units["tabledemo"] {
		paths = append(paths, filepath.Join(repo, p))
	}
	u := luarun.LoadUnit(t, paths...)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("unit tabledemo: %v", err)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	probe := fmt.Sprintf(luaDecodedNameProbe, ModuleName(u), len(luaDecodedNameRows))
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	inputs := map[string][]byte{}
	for i, row := range luaDecodedNameRows {
		inputs["row"+strconv.Itoa(i+1)] = []byte(row.text)
	}
	out := strings.TrimRight(string(luarun.Run(t, dir, "main.lua", inputs)), "\n")
	lines := strings.Split(out, "\n")
	if len(lines) != len(luaDecodedNameRows) {
		t.Fatalf("probe printed %d lines, want %d:\n%s", len(lines), len(luaDecodedNameRows), out)
	}

	m := tabletext.NewModel(u)
	for i, row := range luaDecodedNameRows {
		parts := strings.Split(lines[i], "|")
		if len(parts) != 4 || parts[0] != "row"+strconv.Itoa(i+1) {
			t.Fatalf("%s: bad probe line %q", row.name, lines[i])
		}
		luaVerdict, luaUnknown := parts[1], parts[2]
		engineVerdict, engineUnknown, engineWire := engineDecodedNameRead(t, m, row.text)
		if luaVerdict != engineVerdict || luaUnknown != engineUnknown {
			t.Errorf("%s: %s: lua read %s with %s unknown, the engine %s with %s unknown",
				row.name, row.text, luaVerdict, luaUnknown, engineVerdict, engineUnknown)
			continue
		}
		if luaVerdict != "ok" {
			continue
		}
		luaWire, err := hex.DecodeString(parts[3])
		if err != nil {
			t.Fatalf("%s: bad wire hex: %v", row.name, err)
		}
		// the value, compared as the text the engine writes for each wire
		luaText := engineDecodedNameText(t, m, luaWire)
		engineText := engineDecodedNameText(t, m, engineWire)
		if luaText != engineText {
			t.Errorf("%s: %s: the values differ\n  lua    %s\n  engine %s", row.name, row.text, luaText, engineText)
		}
	}
}

// engineDecodedNameRead reads one text as ProfileConfig through the engine's
// pack: its verdict, its unknown count and the wire it writes.
func engineDecodedNameRead(t *testing.T, m *tabletext.Model, text string) (string, string, []byte) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ProfileConfig.json"), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	wire, _, report, err := tablepack.Pack(m, "ProfileConfig", dir)
	if err != nil || report.Refused {
		return "refused", "-", nil
	}
	return "ok", strconv.Itoa(report.Unknown), wire
}

// engineDecodedNameText is the engine's text for one ProfileConfig wire.
func engineDecodedNameText(t *testing.T, m *tabletext.Model, wire []byte) string {
	t.Helper()
	dir := t.TempDir()
	report, err := tablepack.UnpackOneFile(m, "ProfileConfig", wire, dir)
	if err != nil {
		t.Fatalf("the engine could not read the wire %x: %v", wire, err)
	}
	if report != (tabletext.Report{}) {
		t.Fatalf("the engine's read of the wire %x is not clean: %+v", wire, report)
	}
	text, err := os.ReadFile(filepath.Join(dir, "ProfileConfig.json"))
	if err != nil {
		t.Fatal(err)
	}
	return string(text)
}
