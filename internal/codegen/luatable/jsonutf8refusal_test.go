package luatable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

// TestJSONRefusesIllFormedAtBound feeds the json-hostile row
// str-illformed-at-bound to the generated Lua JSON reader. The name field is a
// string bounded to 32 bytes and the text carries one 0xFF byte: §16.3 refuses
// ill-formed UTF-8 by name at every string site, BEFORE any bound clamp, so the
// read answers `refused` with the reason `string_not_utf8`.
func TestJSONRefusesIllFormedAtBound(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	manifestPath := filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt")
	var row jsonRow
	found := false
	for _, r := range jsonManifestRows(t, manifestPath, "json-hostile") {
		if r.caseName == "str-illformed-at-bound" {
			row, found = r, true
			break
		}
	}
	if !found {
		t.Fatal("the manifest names no json-hostile row str-illformed-at-bound")
	}
	units := manifestUnits(t, manifestPath)
	paths, ok := units[row.unit]
	if !ok {
		t.Fatalf("json-hostile %s: no manifest unit", row.caseName)
	}
	for i, p := range paths {
		paths[i] = filepath.Join(repo, p)
	}
	textData, err := os.ReadFile(filepath.Join(repo, row.path, row.root+".json"))
	if err != nil {
		t.Fatalf("json-hostile %s: read text: %v", row.caseName, err)
	}
	u := luarun.LoadUnit(t, paths...)
	probe := `local T = require("` + ModuleName(u) + `")
local J = require("json")
local inputs = require("inputs")
local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
local v = J.FromJsonTolerant(T, "` + row.root + `", inputs.text, report)
print(report.Reason or "")
print(report.malformed and "1" or "0")
`
	out := jsonRun(t, paths, ModuleName(u), row.root, probe, map[string][]byte{"text": textData}, nil)
	lines := strings.Split(out, "\n")
	if len(lines) != 2 {
		t.Fatalf("json-hostile %s: bad output %q", row.caseName, out)
	}
	if !strings.HasPrefix(lines[0], "string_not_utf8") {
		t.Fatalf("json-hostile %s: reason %q, want string_not_utf8", row.caseName, lines[0])
	}
	if lines[1] != "1" {
		t.Fatalf("json-hostile %s: not refused (report malformed %q)", row.caseName, lines[1])
	}
}
