package tablepack_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tablepack"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// A ZERO IN TEXT TAKES ONE RULE IN EVERY FORM (docs/SPEC-TABLES.md §16.3),
// through the pack path: a root text, or a field file of a tree, holding
// \u0000 in a string(N) refuses the pack, so Pack never writes a wire its own
// Decode calls malformed; the \u0001 control packs and decodes clean.
func TestPackRefusesZeroInText(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	schema := "package probe\n\ntable Memo\n{\n    note string(8)\n    n int32\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "probe.schema"), []byte(schema), 0o644); err != nil {
		t.Fatal(err)
	}
	paths, err := compiler.GatherPaths([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(u)

	trees := map[string]map[string]string{
		"root text": {"Memo.json": `{"note":"a%sb","n":1}`},
		"tree":      {"note.json": `"a%sb"`, "n.json": `1`},
	}
	for name, files := range trees {
		for _, escape := range []string{`\u0000`, `\u0001`} {
			tree := t.TempDir()
			for file, text := range files {
				if err := os.WriteFile(filepath.Join(tree, file), []byte(strings.ReplaceAll(text, "%s", escape)), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			wire, _, report, err := tablepack.Pack(m, "Memo", tree)
			if escape == `\u0000` {
				if err == nil || !report.Malformed {
					var back tabletext.Report
					ok, derr := tablewire.Decode(m, m.New(m.Unit.Tables["Memo"]), wire, &back)
					t.Errorf("%s: Pack accepted a zero in text (err=%v report=%+v); its wire decodes ok=%v err=%v report=%+v", name, err, report, ok, derr, back)
				}
				if name == "root text" && (err == nil || !strings.Contains(err.Error(), "text_holds_zero: Memo.note")) {
					t.Errorf("%s: refusal %v does not name text_holds_zero: Memo.note", name, err)
				}
				continue
			}
			if err != nil || !report.Silent() {
				t.Fatalf("%s: the \\u0001 control did not pack clean: %v %+v", name, err, report)
			}
			var back tabletext.Report
			if ok, derr := tablewire.Decode(m, m.New(m.Unit.Tables["Memo"]), wire, &back); !ok || derr != nil || back.Malformed {
				t.Errorf("%s: the control's wire decodes damaged: ok=%v err=%v %+v", name, ok, derr, back)
			}
		}
	}
}
