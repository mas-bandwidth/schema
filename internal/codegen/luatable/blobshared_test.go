package luatable

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestLuaBlobNamedTwiceIsBlobShared pins docs/SPEC-TABLES.md §2.5 and §16.7 on
// the Lua leg: a blob is owned by ONE slot, so every writer refuses a graph
// whose two slots name the one blob record, by the name blob_shared. The wire
// writers Save and Measure answer nil; the TOLERANT read keeps the one node and counts the extra naming. Two DISTINCT
// nodes carrying equal bytes are still two nodes and save byte for byte as the
// Go engine's golden does (§3.1, §6.3).
func TestLuaBlobNamedTwiceIsBlobShared(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	manifest := filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt")
	units := manifestUnits(t, manifest)
	rows := map[string]manifestRow{}
	for _, r := range manifestRows(t, manifest, "instance") {
		rows[r.caseName] = r
	}

	// THE MANIFEST'S SHARED ROWS: the tolerant read keeps both slots naming the
	// one boxed node and counts the extra naming, and Save and Measure refuse
	// the value by name.
	sharedProbe := func(module, root string) string {
		return `local T = require("` + module + `")
local load = require("load")
local save = require("save")
local inputs = require("inputs")
local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0, blobShared = 0 }
local v = load.LoadTolerant(T, "` + root + `", inputs.wire, report)
assert(v ~= nil, "the tolerant read refused a shared-blob wire")
local saved, swhy = save.Save(T, "` + root + `", v)
local measure, mwhy = save.Measure(T, "` + root + `", v)
print(string.format("%d,%s,%s,%s,%s", report.blobShared or 0,
  saved == nil and "nil" or "bytes", tostring(swhy),
  measure == nil and "nil" or "measure", tostring(mwhy)))
`
	}
	for _, name := range []string{"blob_shared_string", "blob_shared"} {
		row, ok := rows[name]
		if !ok {
			t.Fatalf("the manifest has no instance row %s", name)
		}
		var paths []string
		for _, p := range units[row.unit] {
			paths = append(paths, filepath.Join(repo, p))
		}
		u := luarun.LoadUnit(t, paths...)
		files, err := Generate(u)
		if err != nil {
			t.Fatalf("unit %s: %v", row.unit, err)
		}
		dir := t.TempDir()
		luarun.WriteFiles(t, dir, files)
		if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(sharedProbe(ModuleName(u), row.root)), 0o644); err != nil {
			t.Fatal(err)
		}
		wire, err := os.ReadFile(filepath.Join(repo, row.wire))
		if err != nil {
			t.Fatalf("case %s: read wire %s: %v", name, row.wire, err)
		}
		out := strings.TrimRight(string(luarun.Run(t, dir, "main.lua", map[string][]byte{"wire": wire})), "\n")
		if want := "1,nil,blob_shared,nil,blob_shared"; out != want {
			t.Errorf("%s: the shared blob gave %q, want %q", name, out, want)
		}
	}

	// TWO DISTINCT NODES WITH EQUAL BYTES stay two and save byte for byte as
	// the Go engine's wire does (§3.1, §6.3).
	equal, ok := rows["blob_equal_bytes"]
	if !ok {
		t.Fatal("the manifest has no instance row blob_equal_bytes")
	}
	var equalPaths []string
	for _, p := range units[equal.unit] {
		equalPaths = append(equalPaths, filepath.Join(repo, p))
	}
	eu := luarun.LoadUnit(t, equalPaths...)
	efiles, err := Generate(eu)
	if err != nil {
		t.Fatalf("unit %s: %v", equal.unit, err)
	}
	edir := t.TempDir()
	luarun.WriteFiles(t, edir, efiles)
	equalProbe := `local T = require("` + ModuleName(eu) + `")
local load = require("load")
local save = require("save")
local inputs = require("inputs")
local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0, blobShared = 0 }
local v = load.LoadTolerant(T, "` + equal.root + `", inputs.wire, report)
local saved = assert(save.Save(T, "` + equal.root + `", v), "the distinct equal-byte nodes refused")
local measure = assert(save.Measure(T, "` + equal.root + `", v), "the distinct equal-byte nodes did not measure")
print(string.format("%d:%s:%d", #saved, saved == inputs.wire and "same" or "diff", measure))
`
	if err := os.WriteFile(filepath.Join(edir, "main.lua"), []byte(equalProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	equalWire, err := os.ReadFile(filepath.Join(repo, equal.wire))
	if err != nil {
		t.Fatalf("case blob_equal_bytes: read wire %s: %v", equal.wire, err)
	}
	equalOut := strings.TrimRight(string(luarun.Run(t, edir, "main.lua", map[string][]byte{"wire": equalWire})), "\n")
	if want := strconv.Itoa(len(equalWire)) + ":same:" + strconv.Itoa(len(equalWire)); equalOut != want {
		t.Errorf("blob_equal_bytes: got %q, want %q", equalOut, want)
	}

	// A FRESH value whose slots name one boxed node refuses every writer on
	// the same terms (§2.5).
	u := blobSharedPairUnit(t)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("the probe unit: %v", err)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	freshProbe := `local T = require("` + ModuleName(u) + `")
local save = require("save")
local function text_shared()
  local v = T.NewPair()
  local box = { value = "one" }
  v.text_a = box; v.text_b = box
  return v
end
local function bytes_shared()
  local v = T.NewPair()
  local box = { value = "one" }
  v.data_a = box; v.data_b = box
  return v
end
local function refuse(what, v)
  local saved, swhy = save.Save(T, "Pair", v)
  local measure, mwhy = save.Measure(T, "Pair", v)
  print(table.concat({ what,
    saved == nil and "nil" or "bytes", tostring(swhy),
    measure == nil and "nil" or "measure", tostring(mwhy) }, ","))
end
refuse("text", text_shared())
refuse("bytes", bytes_shared())
`
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(freshProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	freshOut := strings.TrimRight(string(luarun.Run(t, dir, "main.lua", nil)), "\n")
	wantFresh := "text,nil,blob_shared,nil,blob_shared\n" +
		"bytes,nil,blob_shared,nil,blob_shared"
	if freshOut != wantFresh {
		t.Errorf("a fresh shared blob gave\n%s\nwant\n%s", freshOut, wantFresh)
	}
}

// blobSharedPairUnit is a unit with a variable Pair holding two *string slots
// and two *bytes slots:
// the fresh values in TestLuaBlobNamedTwiceIsBlobShared name one boxed node
// from each pair.
func blobSharedPairUnit(t *testing.T) *ir.Unit {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "pairs.schema")
	const src = `package pairdemo
table Pair {
    text_a *string
    text_b *string
    data_a *bytes
    data_b *bytes
}
`
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return luarun.LoadUnit(t, path)
}
