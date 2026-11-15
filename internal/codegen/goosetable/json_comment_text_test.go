package goosetable

// A JSON COMMENT IS TEXT (docs/SPEC-TABLES.md §16.2): the bytes inside a line
// comment (`//` to the end of the line feed or of the input) and inside a
// block comment (`/* */`) are well-formed UTF-8 and hold no NUL (0x00). A
// comment holding an ill-formed sequence or a zero byte refuses the whole
// read, and a lone carriage return does not end a line comment.
//
// The vectors under testdata/wire/json-comment-text-rules state the verdict
// each text owes at the root ProfileConfig of tables/examples. This test reads
// the comment rows through the generated typed accessor, so the walk in
// runtime/schema_json.goose (json_space) is held to the rule in both comment
// forms.

import (
	"bufio"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// commentVector is one manifest line: a vector's file name and the verdict the
// read owes it.
type commentVector struct {
	name    string
	verdict string
}

const commentVectorDir = "testdata/wire/json-comment-text-rules"

// commentVectors reads the manifest's comment rows: every line-comment and
// block-comment vector, each named with the verdict it must hold.
func commentVectors(t *testing.T) []commentVector {
	t.Helper()
	f, err := os.Open(filepath.Join(fixedRoot, commentVectorDir, "manifest.tsv"))
	if err != nil {
		t.Fatalf("the vectors' manifest: %v", err)
	}
	defer f.Close()
	var rows []commentVector
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cols := strings.SplitN(line, "\t", 3)
		if len(cols) != 3 {
			t.Fatalf("manifest line %q is not <file> TAB <verdict> TAB <what it holds>", line)
		}
		name, verdict := cols[0], cols[1]
		if !strings.HasPrefix(name, "line-comment-") && !strings.HasPrefix(name, "block-comment-") {
			continue
		}
		if verdict != "read" && verdict != "refused" {
			t.Fatalf("the vector %q has verdict %q, want read or refused", name, verdict)
		}
		rows = append(rows, commentVector{name: name, verdict: verdict})
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("the vectors' manifest: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("the manifest names no comment vector: the gate would pass vacuously")
	}
	return rows
}

// commentProbe writes the generated modules, the vectors and main.goose into
// dir and answers the probe's name. Each probe reads one vector into a fresh
// ProfileConfig through the typed accessor and prints the verdict.
func commentProbe(t *testing.T, ns string, files map[string][]byte, dir string, rows []commentVector) string {
	t.Helper()
	var mods []string
	for name := range files {
		if before, ok := strings.CutSuffix(name, ".goose"); ok {
			mods = append(mods, before)
		}
	}
	sort.Strings(mods)
	var b strings.Builder
	b.WriteString("import os;\n")
	for _, m := range mods {
		fmt.Fprintf(&b, "import .%s;\n", m)
	}
	b.WriteString("\nfn probe(name: const u8[:]) {\n")
	b.WriteString("    var text: u8[>..] = [];\n")
	b.WriteString("    if !read_file(name, text) { print(\"read-failed\"); return; }\n")
	fmt.Fprintf(&b, "    var v = %s::NewProfileConfig();\n", ns)
	b.WriteString("    var report = schema_rt::json_report();\n")
	fmt.Fprintf(&b, "    let ok = %s::ProfileConfigFromJson(v, text, report);\n", ns)
	b.WriteString("    if ok && !report.damaged { print(\"read\"); } else { print(\"refused\"); }\n")
	b.WriteString("}\n\nfn main() {\n")
	for i := range rows {
		fmt.Fprintf(&b, "    probe(\"in%d.json\");\n", i)
	}
	b.WriteString("}\n")
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.goose"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return "main.goose"
}

func TestGooseJsonCommentIsText(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Goose toolchain (it compiles and runs the JSON read)")

	rows := commentVectors(t)
	paths, err := compiler.GatherPaths([]string{filepath.Join(fixedRoot, "tables", "examples")})
	if err != nil {
		t.Fatalf("gather tables/examples: %v", err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatalf("load tables/examples: %v", err)
	}
	files, err := goose.Generate(u)
	if err != nil {
		t.Fatalf("goose: %v", err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatalf("goosetable: %v", err)
	}
	maps.Copy(files, tables)

	dir := t.TempDir()
	main := commentProbe(t, goose.Namespace(u), files, dir, rows)
	inputs := map[string][]byte{}
	for i, r := range rows {
		data, err := os.ReadFile(filepath.Join(fixedRoot, commentVectorDir, r.name))
		if err != nil {
			t.Fatalf("the vector %q: %v", r.name, err)
		}
		inputs[fmt.Sprintf("in%d.json", i)] = data
	}
	out := strings.Split(strings.TrimRight(string(gooserun.Run(t, dir, main, inputs)), "\n"), "\n")
	if len(out) != len(rows) {
		t.Fatalf("%d answers for %d comment vectors:\n%s", len(out), len(rows), strings.Join(out, "\n"))
	}
	for i, r := range rows {
		if out[i] != r.verdict {
			t.Errorf("%s: got %s, want %s", r.name, out[i], r.verdict)
		}
	}
	t.Logf("json comment text: %d comment vectors hold their verdict", len(rows))
}
