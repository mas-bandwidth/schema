package luatable

// TestLuaSkippedValueIsValidated runs every row of the shared skipped-value
// vectors through the Lua leg's reads (docs/SPEC-TABLES.md §4, §16.3;
// docs/rulings.json skipped-value-is-validated): the JSON rows through the
// reader root's text read, and the message rows through its batch load under
// the newer writer unit's announcement. A value the reader SKIPS is held to
// what a read of it would hold, and damage inside it refuses the whole read.
// The verdict is the contract; a refusal's name is the leg's own diagnostic.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func TestLuaSkippedValueIsValidated(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Lua interpreter (it runs the generated skipped-value probe)")

	root := filepath.Join("..", "..", "..")
	dir := filepath.Join(root, "testdata", "wire", "skipped-value")
	manifest, err := os.ReadFile(filepath.Join(dir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}

	type row struct{ file, form, verdict string }
	var rows []row
	annIdx := -1
	for i, line := range strings.Split(string(manifest), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 3 {
			t.Fatalf("%s:%d: a vector line is <file> TAB <form> TAB <verdict>, got %q", manifest, i+1, line)
		}
		if f[1] != "json" && f[1] != "message" {
			t.Fatalf("%s:%d: form %q, want json or message", manifest, i+1, f[1])
		}
		if f[2] != "read" && f[2] != "refused" {
			t.Fatalf("%s:%d: verdict %q, want read or refused", manifest, i+1, f[2])
		}
		if f[1] == "message" && f[0] == "announcement.bin" {
			annIdx = len(rows)
		}
		rows = append(rows, row{f[0], f[1], f[2]})
	}
	if len(rows) == 0 {
		t.Fatalf("%s names no rows", manifest)
	}
	if annIdx < 0 {
		t.Fatalf("%s names no announcement.bin message row", manifest)
	}

	u := luarun.LoadUnit(t, filepath.Join(root, "test", "skipped-value", "reader", "Reader.schema"))
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	module := ModuleName(u)

	inputs := map[string][]byte{}
	for i, r := range rows {
		data, err := os.ReadFile(filepath.Join(dir, r.file))
		if err != nil {
			t.Fatalf("%s: %v", r.file, err)
		}
		inputs[fmt.Sprintf("row_%d", i)] = data
	}

	var probe strings.Builder
	fmt.Fprintf(&probe, "local T = require(%q)\n", module)
	probe.WriteString(`local J = require("json")
local inputs = require("inputs")
local function verdict(ok, report)
  if ok and not report.damaged and (report.refused or 0) == 0 then return "read" end
  return "refused"
end
local vocab = {}
for i = 1, T.MessageEntries do vocab[i] = {} end
local arep = {}
local announced = T.AnnounceRead(vocab, inputs.row_`)
	fmt.Fprintf(&probe, "%d", annIdx)
	probe.WriteString(`, arep)
`)
	for i, r := range rows {
		switch r.form {
		case "json":
			fmt.Fprintf(&probe, `do
  local r = {}
  local v = J.FromJson(T, "Root", inputs.row_%d, r)
  print(%d .. " " .. verdict(v ~= nil, r))
end
`, i, i)
		case "message":
			if i == annIdx {
				fmt.Fprintf(&probe, `print(%d .. " " .. (announced and "read" or "refused"))
`, i)
				continue
			}
			fmt.Fprintf(&probe, `do
  local r = {}
  local got = T.LoadMessages("Root", vocab, inputs.row_%d, r)
  print(%d .. " " .. verdict(got ~= nil and #got == 1, r))
end
`, i, i)
		}
	}

	out, stderr, err := try(t, files, probe.String(), inputs)
	if err != nil {
		t.Fatalf("skipped-value probe failed: %v\nout=%s\nstderr=%s", err, out, stderr)
	}

	got := map[int]string{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		var index int
		var v string
		if _, err := fmt.Sscanf(line, "%d %s", &index, &v); err != nil {
			t.Fatalf("the probe answered %q: %v", line, err)
		}
		got[index] = v
	}
	if len(got) != len(rows) {
		t.Fatalf("the probe answered %d rows of %d:\n%s", len(got), len(rows), out)
	}
	for i, r := range rows {
		if got[i] != r.verdict {
			t.Errorf("%s (%s): verdict %s, want %s", r.file, r.form, got[i], r.verdict)
		}
	}
}

// TestLuaJsonPlacedObjectKeyZero holds every placed-object key scan to §16.3:
// a key that decodes to U+0000 refuses the read under a table, a pointer, an
// enum-keyed array and a union, exactly as a skipped object's key does.
func TestLuaJsonPlacedObjectKeyZero(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Lua interpreter (it runs the generated placed-key probe)")

	src := `package probe

enum Color { Red, Green }

table Inner { n uint8 }

union Body
{
    narrow string(8)
    tally  int32
}

table Root
{
    p *Inner
    k [Color]uint8
    u Body
    n uint8
}
`
	dir := t.TempDir()
	path := filepath.Join(dir, "Probe.schema")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	u := luarun.LoadUnit(t, path)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	runDir := t.TempDir()
	luarun.WriteFiles(t, runDir, files)
	probe := `local T = require("` + ModuleName(u) + `")
local J = require("json")
local inputs = require("inputs")
local r = {}
local v = J.FromJson(T, "Root", inputs.text, r)
print((v ~= nil and not r.damaged) and "read" or "refused")
`
	if err := os.WriteFile(filepath.Join(runDir, "main.lua"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct{ name, text, want string }{
		{"control", `{"n":1}`, "read"},
		{"table", `{"n\u0000":1}`, "refused"},
		{"pointer", `{"p":{"n\u0000":1}}`, "refused"},
		{"keyed", `{"k":{"Red\u0000":1}}`, "refused"},
		{"union", `{"u":{"narrow\u0000":"x"}}`, "refused"},
	}
	for _, c := range cases {
		got := strings.TrimSpace(string(luarun.Run(t, runDir, "main.lua", map[string][]byte{"text": []byte(c.text)})))
		if got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
