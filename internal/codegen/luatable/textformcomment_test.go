package luatable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

// TestLuaTextFormCommentScalarsValidated pins docs/SPEC-TABLES.md §16.9.1:
// comment text is held to the same valid-scalar rule as the rest of the
// document. A semicolon comment holding an overlong sequence, a lone
// continuation byte, a truncated lead or an encoded surrogate is refused by
// the value reader and by the document reader as text_encoding at the byte
// offset of the bad scalar; a comment of valid multibyte text still loads.
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
	luaDir := t.TempDir()
	luarun.WriteFiles(t, luaDir, files)

	type testCase struct {
		name, text, want string
	}
	cases := []testCase{
		{"overlong", "; a\xc0\x80b\n()", "REFUSE\ttext_encoding\t3"},
		{"continuation", "; ab\x80\n()", "REFUSE\ttext_encoding\t4"},
		{"truncated", "()\n; \xe2\x82\n", "REFUSE\ttext_encoding\t5"},
		{"truncated_at_end", "() ; x\xf0\x9f", "REFUSE\ttext_encoding\t6"},
		{"surrogate", "(); \xed\xa0\x80", "REFUSE\ttext_encoding\t4"},
		{"inside_list", "(\"title\" \"x\" ; bad \xff\n)", "REFUSE\ttext_encoding\t19"},
		{"valid_multibyte", "; caf\xc3\xa9 \xe2\x82\xac \xf0\x9f\x98\x80\n()", "OK\t\t"},
	}
	inputs := map[string][]byte{}
	var quoted []string
	for _, c := range cases {
		inputs[c.name] = []byte(c.text)
		quoted = append(quoted, `"`+c.name+`"`)
	}
	probe := `local T = require("ProbeTables")
local TF = require("textform")
local W = require("textformwrite")
local inputs = require("inputs")
local names = { ` + strings.Join(quoted, ", ") + ` }
local function show(label, ok, e)
  if ok then
    print(label .. "\tOK\t\t")
  else
    print(label .. "\tREFUSE\t" .. tostring(e.name) .. "\t" .. tostring(e.offset))
  end
end
for _, name in ipairs(names) do
  local ok, e = pcall(TF.Load, T, "Document", inputs[name])
  show("value:" .. name, ok, e)
  ok, e = pcall(W.Load, T, "Document", inputs[name])
  show("document:" .. name, ok, e)
end
`
	if err := os.WriteFile(filepath.Join(luaDir, "main.lua"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	out := string(luarun.Run(t, luaDir, "main.lua", inputs))
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		label, rest, ok := strings.Cut(line, "\t")
		if !ok {
			t.Fatalf("bad probe line %q", line)
		}
		got[label] = strings.TrimRight(rest, "\t")
	}
	for _, c := range cases {
		want := strings.TrimRight(c.want, "\t")
		for _, reader := range []string{"value", "document"} {
			if g := got[reader+":"+c.name]; g != want {
				t.Errorf("%s reader, %s: got %q, want %q", reader, c.name, g, want)
			}
		}
	}
}
