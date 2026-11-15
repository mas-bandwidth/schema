package squirreltable_test

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// TestSquirrelJsonCommentIsWellFormedUTF8 holds the Squirrel leg's JSON read
// to docs/SPEC-TABLES.md §16.2's comment text rule: the bytes inside a
// comment, line or block, are well-formed UTF-8 and hold no NUL, so a comment
// holding an ill-formed sequence or a NUL refuses the read. Every vector under
// testdata/wire/json-comment-text-rules/ is read at tables/examples' root
// ProfileConfig, and so is each comment payload at a placed position and
// inside an unknown key's value. The compiler's own engine, internal/tabletext,
// is held to each vector's stated verdict first, and every row is then compared
// on the verdict alone (read or refused) with that engine, never on a reason.
func TestSquirrelJsonCommentIsWellFormedUTF8(t *testing.T) {
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnit(t, root, []string{"tables/examples/Tables.schema"})
	model := tabletext.NewModel(u)
	st := model.Lookup("ProfileConfig")
	if st == nil {
		t.Fatal("tables/examples has no table ProfileConfig")
	}
	engineReads := func(text []byte) bool {
		var report tabletext.Report
		ok := model.Read(model.New(st), text, &report)
		return ok && !report.Refused
	}

	type row struct {
		name string
		text []byte
		read bool
	}
	var rows []row

	dir := filepath.Join(root, "testdata", "wire", "json-comment-text-rules")
	manifest, err := os.Open(filepath.Join(dir, "manifest.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	defer manifest.Close()
	scanner := bufio.NewScanner(manifest)
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cols := strings.SplitN(line, "\t", 3)
		if len(cols) != 3 || (cols[1] != "read" && cols[1] != "refused") {
			t.Fatalf("bad manifest line %q", line)
		}
		text, err := os.ReadFile(filepath.Join(dir, cols[0]))
		if err != nil {
			t.Fatal(err)
		}
		want := cols[1] == "read"
		if got := engineReads(text); got != want {
			t.Fatalf("the engine reads %s as read=%t, the manifest says %s", cols[0], got, cols[1])
		}
		rows = append(rows, row{cols[0], text, want})
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("the manifest names no vectors")
	}

	payloads := []struct{ name, bytes string }{
		{"utf8", "café 日本 \U0001F600"},
		{"lone-continuation", "a\x80b"},
		{"overlong", "a\xc0\xafb"},
		{"truncated-three-byte", "a\xe2\x82"},
		{"encoded-surrogate", "a\xed\xa0\x80b"},
		{"nul", "a\x00b"},
	}
	places := []struct{ name, text string }{
		{"placed-line", "{\"name\": // %s\n\"a\"}\n"},
		{"placed-block", "{\"name\": /* %s */ \"a\"}\n"},
		{"unknown-line", "{\"name\": \"a\", \"zz_unknown\": [1, // %s\n2]}\n"},
		{"unknown-block", "{\"name\": \"a\", \"zz_unknown\": {\"k\": /* %s */ 2}}\n"},
	}
	for _, p := range payloads {
		for _, place := range places {
			text := []byte(fmt.Sprintf(place.text, p.bytes))
			want := p.name == "utf8"
			if got := engineReads(text); got != want {
				t.Fatalf("the engine reads %s %s as read=%t, the rule says read=%t", place.name, p.name, got, want)
			}
			rows = append(rows, row{place.name + "-" + p.name, text, want})
		}
	}

	files, err := squirrel.Generate(u)
	if err != nil {
		t.Fatalf("the type surface refused: %v", err)
	}
	tables, err := squirreltable.Generate(u)
	if err != nil {
		t.Fatalf("the table surface refused: %v", err)
	}
	work := t.TempDir()
	outDir := filepath.Join(work, "example")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		writeFile(t, filepath.Join(outDir, name), data)
	}
	for name, data := range tables {
		writeFile(t, filepath.Join(outDir, name), data)
	}

	var probe strings.Builder
	probe.WriteString("local inputs = dofile(\"inputs.nut\", true);\n")
	probe.WriteString("local schema = dofile(\"example/schema_module.nut\", true);\n")
	fmt.Fprintf(&probe, "local T = schema.require(%q);\n", squirreltable.ModuleName(u))
	probe.WriteString("local J = schema.require(\"schema_table_json\");\n")
	probe.WriteString("local cases = [")
	for i, r := range rows {
		if i > 0 {
			probe.WriteString(", ")
		}
		fmt.Fprintf(&probe, "%q", r.name)
	}
	probe.WriteString("];\n")
	probe.WriteString(`foreach (key in cases) {
  local report = { unknown = 0, mismatch = 0, widened = 0, oor = 0,
    repeated = 0, bodyDamaged = false, refused = false };
  local v = J.FromJson(T, "ProfileConfig", inputs[key], report);
  print(key + " " + (v == null ? "refused" : "read") + "\n");
}
`)
	writeFile(t, filepath.Join(work, "main.nut"), []byte(probe.String()))

	inputs := map[string][]byte{}
	for _, r := range rows {
		inputs[r.name] = r.text
	}
	got := map[string]bool{}
	for line := range strings.SplitSeq(strings.TrimRight(string(sqrun.Run(t, work, "main.nut", inputs)), "\n"), "\n") {
		name, verdict, ok := strings.Cut(line, " ")
		if !ok || (verdict != "read" && verdict != "refused") {
			t.Fatalf("probe line %q is not a verdict", line)
		}
		got[name] = verdict == "read"
	}
	for _, r := range rows {
		read, seen := got[r.name]
		if !seen {
			t.Errorf("%s: the probe printed no verdict", r.name)
			continue
		}
		if read != r.read {
			t.Errorf("%s: the emitted reader answers %v where the engine answers %v", r.name, read, r.read)
		}
	}
}
