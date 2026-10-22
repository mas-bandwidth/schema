package luatable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

// base64CanonicalProbe is the Lua main: it builds a WeaponConfig from JSON,
// writes its canonical SaveText, reads it back and re-encodes it, then offers
// each row of gotable TestGoLoadTextRefusesNonCanonicalBase64 to LoadText and
// prints one "label=verdict" line per row, the verdict the refusal name or
// "taken".
const base64CanonicalProbe = `local T = require("tables")
local json = require("json")
local text = require("text")
local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
local v = json.FromJson(T, "WeaponConfig", '{ "damage": 42, "speed": 120.5, "homing": true }', report)
local good = text.SaveText(T, "WeaponConfig", v)
local loaded = text.LoadText(T, "WeaponConfig", good)
print("clean=" .. ((text.SaveText(T, "WeaponConfig", loaded) == good) and "same" or "diff"))
local function verdict(s)
  local ok, err = pcall(function() return text.LoadText(T, "WeaponConfig", s) end)
  if ok then return "taken" end
  return tostring(err)
end
local rows = {
  { "pad_bits", "AQAAAAAAAAAAAB==" },
  { "mid_lf", good:sub(1, 10) .. "\n" .. good:sub(11) },
  { "appended_crlf", good .. "\r\n" },
  { "outside_alphabet", "!" .. good:sub(2) },
  { "embedded_space", good:sub(1, 10) .. " " .. good:sub(11) },
  { "missing_pad", "AQAAAAAAAAAAAB=" },
  { "extra_pad", "AQAAAAAAAAAAAB===" },
}
for _, r in ipairs(rows) do print(r[1] .. "=" .. verdict(r[2])) end
`

// TestLuaLoadTextRefusesNonCanonicalBase64 pins docs/SPEC-TABLES.md §16.8 in
// the Lua port, over tables/examples/Tables.schema root WeaponConfig, row for
// row with the oracle gotable TestGoLoadTextRefusesNonCanonicalBase64: the
// base64 carrier is CANONICAL (standard alphabet, padded, pad bits zero, no
// byte outside the alphabet and "="), so a text that decodes once its CR and
// LF are dropped but is not the canonical encoding (a pad bit set, a CR or LF
// present) refuses base64_not_canonical, and every other failure stays
// base64_damaged. A canonical SaveText reads clean and re-encodes to itself.
// Red at base: AQAAAAAAAAAAAB== decoded leniently and a CR or LF was damage.
func TestLuaLoadTextRefusesNonCanonicalBase64(t *testing.T) {
	t.Parallel()
	u := luarun.LoadUnit(t, filepath.Join("..", "..", "..", "tables", "examples", "Tables.schema"))
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate Tables.schema: %v", err)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	probe := strings.Replace(base64CanonicalProbe, `require("tables")`, `require("`+ModuleName(u)+`")`, 1)
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(string(luarun.Run(t, dir, "main.lua", nil)), "\n"), "\n") {
		k, v, _ := strings.Cut(line, "=")
		got[k] = v
	}
	want := []struct{ row, verdict string }{
		{"clean", "same"},
		{"pad_bits", "base64_not_canonical"},
		{"mid_lf", "base64_not_canonical"},
		{"appended_crlf", "base64_not_canonical"},
		{"outside_alphabet", "base64_damaged"},
		{"embedded_space", "base64_damaged"},
		{"missing_pad", "base64_damaged"},
		{"extra_pad", "base64_damaged"},
	}
	for _, w := range want {
		if got[w.row] != w.verdict {
			t.Errorf("%s: LoadText answered %q, want %q", w.row, got[w.row], w.verdict)
		}
	}
}
