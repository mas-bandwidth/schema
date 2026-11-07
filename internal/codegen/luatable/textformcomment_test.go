package luatable

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

// TestLuaTextFormCommentScalarsValidated is the Lua textform comment gate
// (docs/SPEC-TABLES.md §16.9.1): a comment is ";" followed by any valid UTF-8
// scalar except CR or LF, so invalid UTF-8 in a comment is text_encoding at
// the offending byte, through both the strict reader (LoadTextForm) and the
// lossless document loader. Valid multibyte comment bytes load.
func TestLuaTextFormCommentScalarsValidated(t *testing.T) {
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
	for _, name := range []string{"textform.lua", "textformwrite.lua"} {
		if _, ok := files[name]; !ok {
			t.Fatalf("the emitted unit has no %s", name)
		}
	}
	luaDir := t.TempDir()
	luarun.WriteFiles(t, luaDir, files)

	type testCase struct {
		name       string
		text       []byte
		wantRefuse bool
		wantOffset int
	}
	lead := func(payload string) []byte { return []byte(";" + payload + "\n()") }
	// comment payloads start at byte offset 1 (the ";" is offset 0), except
	// where the text says otherwise.
	cases := []testCase{
		{name: "overlong", text: lead("\xc0\x80"), wantRefuse: true, wantOffset: 1},
		{name: "overlong_three", text: lead("\xe0\x80\x80"), wantRefuse: true, wantOffset: 1},
		{name: "lone_continuation", text: lead("\x80"), wantRefuse: true, wantOffset: 1},
		{name: "truncated_lead", text: lead("\xe2\x82"), wantRefuse: true, wantOffset: 1},
		{name: "surrogate", text: lead("\xed\xa0\x80"), wantRefuse: true, wantOffset: 1},
		{name: "invalid_after_ascii", text: []byte(";ok \xff\n()"), wantRefuse: true, wantOffset: 4},
		{name: "truncated_lead_at_eof", text: []byte("() ;\xc3"), wantRefuse: true, wantOffset: 4},
		{name: "valid_multibyte", text: []byte("; caf\xc3\xa9 \xe2\x98\x83 \xf0\x9f\x98\x80\n()")},
		{name: "valid_inside", text: []byte("(:count 2 ; caf\xc3\xa9\n)")},
	}

	inputs := map[string][]byte{}
	var quoted []string
	for _, c := range cases {
		inputs[c.name] = c.text
		quoted = append(quoted, strconv.Quote(c.name))
	}
	probe := `local T = require("ProbeTables")
local TF = require("textform")
local W = require("textformwrite")
local inputs = require("inputs")
local names = { ` + strings.Join(quoted, ", ") + ` }
for _, name in ipairs(names) do
  for _, reader in ipairs({ "strict", "document" }) do
    local fn
    if reader == "strict" then
      fn = function() return TF.LoadTextForm(T, "Document", inputs[name]) end
    else
      fn = function() return W.LoadTextForm(T, "Document", inputs[name]) end
    end
    local ok, v = pcall(fn)
    if ok then
      print(name .. "\t" .. reader .. "\tOK\t-\t-1")
    else
      local nm = type(v) == "table" and tostring(v.name) or tostring(v)
      local off = type(v) == "table" and tostring(v.offset) or "-1"
      print(name .. "\t" .. reader .. "\tREFUSE\t" .. nm .. "\t" .. off)
    end
  end
end
`
	if err := os.WriteFile(filepath.Join(luaDir, "main.lua"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	out := string(luarun.Run(t, luaDir, "main.lua", inputs))

	type result struct {
		verdict, name string
		offset        int
	}
	results := map[string]result{}
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		parts := strings.Split(line, "\t")
		if len(parts) != 5 {
			t.Fatalf("bad probe line %q", line)
		}
		off, err := strconv.Atoi(parts[4])
		if err != nil {
			t.Fatalf("bad probe line %q: %v", line, err)
		}
		results[parts[0]+"/"+parts[1]] = result{parts[2], parts[3], off}
	}
	for _, c := range cases {
		for _, reader := range []string{"strict", "document"} {
			key := c.name + "/" + reader
			r, ok := results[key]
			if !ok {
				t.Fatalf("%s: no probe result", key)
			}
			if c.wantRefuse {
				if r.verdict != "REFUSE" || r.name != "text_encoding" || r.offset != c.wantOffset {
					t.Errorf("%s: got %s %s offset %d, want REFUSE text_encoding offset %d", key, r.verdict, r.name, r.offset, c.wantOffset)
				}
			} else if r.verdict != "OK" {
				t.Errorf("%s: got %s %s offset %d, want OK", key, r.verdict, r.name, r.offset)
			}
		}
	}
}
