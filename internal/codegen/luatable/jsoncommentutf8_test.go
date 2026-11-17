package luatable

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// TestLuaJsonCommentIsWellFormedUTF8 reads every vector under
// testdata/wire/json-comment-text-rules through the generated Lua FromJson at
// tables/examples' root ProfileConfig and compares its verdict, read or
// refused, with the compiler's own engine (internal/tabletext) on the same
// text (docs/SPEC-TABLES.md §16.2, COMMENTS; §16.3): the bytes inside a
// comment, line or block, are well-formed UTF-8 and hold no NUL, a line
// comment ends at a line feed or the end of input, and the read's depth cap
// counts the value under an unknown key. The engine is checked against the
// manifest first, so the reference itself is confirmed on every row. Only the
// verdict is compared; which reason a refusal names is per-language detail.
func TestLuaJsonCommentIsWellFormedUTF8(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	dir := filepath.Join(repo, "testdata", "wire", "json-comment-text-rules")
	const root = "ProfileConfig"

	type row struct{ name, verdict string }
	manifest, err := os.Open(filepath.Join(dir, "manifest.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	defer manifest.Close()
	var rows []row
	scanner := bufio.NewScanner(manifest)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cols := strings.SplitN(line, "\t", 3)
		if len(cols) != 3 || (cols[1] != "read" && cols[1] != "refused") {
			t.Fatalf("bad manifest line %q", line)
		}
		rows = append(rows, row{cols[0], cols[1]})
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("the manifest names no comment-rule vectors")
	}

	u := luarun.LoadUnit(t, filepath.Join(repo, "tables", "examples"))
	m := tabletext.NewModel(u)
	st := m.Lookup(root)
	if st == nil {
		t.Fatalf("tables/examples has no table %s", root)
	}

	texts := map[string][]byte{}
	engine := map[string]string{}
	var names []string
	for _, r := range rows {
		text, err := os.ReadFile(filepath.Join(dir, r.name))
		if err != nil {
			t.Fatal(err)
		}
		texts[r.name] = text
		names = append(names, luarun.Quote([]byte(r.name)))
		var report tabletext.Report
		ok := m.Read(m.New(st), text, &report)
		verdict := "read"
		if !ok || report.Refused {
			verdict = "refused"
		}
		if verdict != r.verdict {
			t.Fatalf("the reference engine answers %s on %s, the manifest says %s", verdict, r.name, r.verdict)
		}
		engine[r.name] = verdict
	}

	probe := `local T = require("` + ModuleName(u) + `")
local J = require("json")
local inputs = require("inputs")
for _, name in ipairs({ ` + strings.Join(names, ", ") + ` }) do
  local report = {}
  local v = J.FromJson(T, "` + root + `", inputs[name], report)
  print(name .. "\t" .. ((v == nil or report.damaged) and "refused" or "read"))
end
`
	out := jsonRun(t, []string{filepath.Join(repo, "tables", "examples")}, ModuleName(u), root, probe, texts, nil)
	lua := map[string]string{}
	for line := range strings.SplitSeq(out, "\n") {
		cols := strings.SplitN(line, "\t", 2)
		if len(cols) != 2 {
			t.Fatalf("bad probe line %q in %q", line, out)
		}
		lua[cols[0]] = cols[1]
	}
	var diffs []string
	for _, r := range rows {
		if lua[r.name] != engine[r.name] {
			diffs = append(diffs, fmt.Sprintf("%s: Lua answers %q, the engine %s", r.name, lua[r.name], engine[r.name]))
		}
	}
	if len(diffs) > 0 {
		t.Fatalf("%d of %d comment-rule vectors differ from the engine:\n%s", len(diffs), len(rows), strings.Join(diffs, "\n"))
	}
}
