package luatable

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

// TestLuaJsonSkipsLeadingByteOrderMark pins the JSON read's byte-order mark
// rule (docs/SPEC-TABLES.md, the JSONTestSuite table:
// i_structure_UTF-8_BOM_empty_object.json is accepted, RFC 8259 §8.1): one
// leading UTF-8 byte-order mark (EF BB BF) is skipped and the text after it
// reads as if the mark were absent. A mark anywhere else (after leading
// whitespace, or a second mark) is not whitespace and is refused. The
// hand-editable text form refuses a byte-order mark (§16.9.1, text_syntax) and
// keeps refusing it, though it lands through the JSON read.
func TestLuaJsonSkipsLeadingByteOrderMark(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "doc.schema"), []byte(textFormSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u := luarun.LoadUnit(t, dir)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	luaDir := t.TempDir()
	luarun.WriteFiles(t, luaDir, files)

	const bom = "\xef\xbb\xbf"
	const body = `{"count": 2, "title": "bom"}`
	cases := []struct {
		name, reader, text string
		wantRead           bool
	}{
		{"plain", "json", body, true},
		{"leading_bom", "json", bom + body, true},
		{"bom_after_space", "json", " " + bom + body, false},
		{"two_boms", "json", bom + bom + body, false},
		{"textform_plain", "textform", `(:count 2 :title "bom")`, true},
		{"textform_bom", "textform", bom + `(:count 2 :title "bom")`, false},
	}
	inputs := map[string][]byte{}
	var rows []string
	for _, c := range cases {
		inputs[c.name] = []byte(c.text)
		rows = append(rows, "{ "+strconv.Quote(c.name)+", "+strconv.Quote(c.reader)+" }")
	}
	probe := `local T = require("` + ModuleName(u) + `")
local J = require("json")
local TF = require("textform")
local inputs = require("inputs")
local rows = { ` + strings.Join(rows, ", ") + ` }
for _, r in ipairs(rows) do
  local name, reader = r[1], r[2]
  local v
  if reader == "json" then
    v = J.FromJson(T, "Document", inputs[name], {})
  else
    local ok, got = pcall(TF.LoadTextForm, T, "Document", inputs[name])
    if ok then v = got end
  end
  if v == nil then
    print(name .. "\tREFUSED\t-")
  else
    print(name .. "\tREAD\t" .. (J.ToJson(T, "Document", v):gsub("\n", " ")))
  end
end
`
	if err := os.WriteFile(filepath.Join(luaDir, "main.lua"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	out := string(luarun.Run(t, luaDir, "main.lua", inputs))

	type result struct{ verdict, value string }
	results := map[string]result{}
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			t.Fatalf("bad probe line %q", line)
		}
		results[parts[0]] = result{parts[1], parts[2]}
	}
	want := results["plain"]
	if want.verdict != "READ" {
		t.Fatalf("plain: the JSON read answered %s, want READ", want.verdict)
	}
	for _, c := range cases {
		r, ok := results[c.name]
		if !ok {
			t.Fatalf("%s: no probe result", c.name)
		}
		if !c.wantRead {
			if r.verdict != "REFUSED" {
				t.Errorf("%s: %s answered %s, want REFUSED", c.name, c.reader, r.verdict)
			}
			continue
		}
		if r.verdict != "READ" {
			t.Errorf("%s: %s answered %s, want READ", c.name, c.reader, r.verdict)
			continue
		}
		if r.value != want.value {
			t.Errorf("%s: %s read %s, want the value read without a mark, %s", c.name, c.reader, r.value, want.value)
		}
	}
}
