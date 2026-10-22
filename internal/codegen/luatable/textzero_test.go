package luatable

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

// TestLuaTextZeroByteTakesOneRuleInEveryForm pins docs/SPEC-TABLES.md §16.3
// on the Lua JSON read: a string that decodes to U+0000 and would land in text
// storage refuses the read as text_holds_zero naming the innermost field, so
// no read lands a value a writer refuses (§2.13, §3). The rows are the Go
// oracle's (internal/codegen/gotable/textzero_test.go), for every construct
// Lua carries: string(8), *string, a map[string(8)] key, a wstring(8), a map
// key and a string one table down, with \u0000 at the front, middle and end.
// A \u0001 in the same place reads clean and writes, reads and writes the same
// text. A text list, an unbounded key and a timestamp are named and skipped:
// the Lua JSON read does not carry them yet.
func TestLuaTextZeroByteTakesOneRuleInEveryForm(t *testing.T) {
	t.Parallel()
	work := t.TempDir()
	schema := `package probe

table Inner
{
    n string(8)
}

table Root
{
    t string(8)
    s *string
    l []string(8)
    m map[string(8)]int32
    w wstring(8)
    i Inner
    at timestamp
    k map[int32]int32
    u map[string]int32
}
`
	path := filepath.Join(work, "probe.schema")
	if err := os.WriteFile(path, []byte(schema), 0o644); err != nil {
		t.Fatal(err)
	}
	u := luarun.LoadUnit(t, path)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(textZeroLuaProbe), 0o644); err != nil {
		t.Fatal(err)
	}

	forms := []struct {
		name, template, field string
		control, skip         bool
	}{
		{"t", `{"t":"%s"}`, "Root.t", true, false},
		{"s", `{"s":"%s"}`, "Root.s", true, false},
		{"m", `{"m":{"ok":1,"%s":2}}`, "Root.m", true, false},
		{"w", `{"w":"%s"}`, "Root.w", true, false},
		{"i", `{"i":{"n":"%s"}}`, "Inner.n", true, false},
		{"k", `{"k":{"7":1,"%s":2}}`, "Root.k", false, false},
		{"l", `{"l":["ok","%s"]}`, "Root.l", false, true},
		{"u", `{"u":{"ok":1,"%s":2}}`, "Root.u", false, true},
		{"at", `{"at":"%s"}`, "Root.at", false, true},
	}
	for _, f := range forms {
		if f.skip {
			t.Logf("skipped %s: the Lua JSON read does not carry this construct yet", f.name)
			continue
		}
		for _, at := range []string{`\u0000ab`, `a\u0000b`, `ab\u0000`} {
			text := fmt.Sprintf(f.template, at)
			lines := textZeroRun(t, dir, text)
			want := "text_holds_zero: " + f.field
			if len(lines) < 2 || lines[0] != "refused" || lines[1] != want {
				t.Errorf("%s %s: got %q, want refused with %q", f.name, text, lines, want)
				continue
			}
			if !f.control {
				continue
			}
			control := strings.Replace(text, `\u0000`, `\u0001`, 1)
			got := textZeroRun(t, dir, control)
			if len(got) < 5 || got[0] != "read" || got[1] != "-" ||
				got[3] != "clean" || got[4] != "same" {
				t.Errorf("%s %s: the control did not read clean and round-trip: %q", f.name, control, got)
				continue
			}
			out, err := hex.DecodeString(got[2])
			if err != nil {
				t.Errorf("%s %s: the written text is not hex: %v", f.name, control, err)
				continue
			}
			if !strings.Contains(string(out), `\u0001`) {
				t.Errorf("%s %s: the control text did not carry U+0001:\n%s", f.name, control, out)
			}
		}
	}
}

// textZeroRun reads one text through the generated JSON runtime and returns
// the probe's lines: the verdict and reason, then on a clean read the hex of
// the written text, whether that text reads clean and whether it writes the
// same text again.
func textZeroRun(t *testing.T, dir, text string) []string {
	t.Helper()
	out := luarun.Run(t, dir, "main.lua", map[string][]byte{"text": []byte(text)})
	return strings.Split(strings.TrimRight(string(out), "\n"), "\n")
}

const textZeroLuaProbe = `local T = require("ProbeTables")
local J = require("json")
local inputs = require("inputs")
local function hex(s)
  return (s:gsub(".", function(c) return string.format("%02x", c:byte()) end))
end
local function fresh()
  return { malformed = false, refused = 0, unknown = 0, kindMismatch = 0,
    clamped = 0, widened = 0, duplicate = 0 }
end
local report = fresh()
local v = J.FromJsonTolerant(T, "Root", inputs.text, report)
if v == nil or report.malformed then
  print("refused")
  print(report.Reason or "-")
else
  print("read")
  print(report.Reason or "-")
  local out = J.ToJson(T, "Root", v)
  if out == nil then
    print("write_refused")
  else
    print(hex(out))
    local again_report = fresh()
    local again = J.FromJsonTolerant(T, "Root", out, again_report)
    print(again ~= nil and not again_report.malformed and again_report.Reason == nil
      and "clean" or "dirty")
    local out2 = again and J.ToJson(T, "Root", again)
    print(out2 ~= nil and out2 == out and "same" or "moved")
  end
end
`
