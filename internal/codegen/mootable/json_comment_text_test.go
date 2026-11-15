package mootable

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/moo/moorun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// A JSON COMMENT IS TEXT (docs/SPEC-TABLES.md §16.2): the bytes inside a line
// comment (`//` to the line feed or the end of the input) and inside a block
// comment (`/* */`) are well-formed UTF-8 and hold no NUL (0x00). A comment
// holding an ill-formed sequence or a zero byte refuses the whole read, and a
// lone carriage return does not end a line comment.
//
// The vectors under testdata/wire/json-comment-text-rules state the verdict
// each text owes at tables/examples' root ProfileConfig. This test reads the
// comment rows through the generated typed accessor, so the walk in
// runtime/json.moo's space is held to the rule in both comment forms. The
// verdict alone is the contract (docs/rulings.json refusal-verdict-only).
func TestMooJsonCommentIsText(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the generated JSON read)")
	t.Parallel()

	rows := commentVectors(t)
	u := loadUnit(t, []string{"tables/examples/Tables.schema"})
	dir := generateUnitDir(t, u, nil)
	module := tableModule(u, "ProfileConfig")

	var main strings.Builder
	main.WriteString(probePrelude)
	fmt.Fprintf(&main, "local mod: any = require(\"./%s\")\n", module)
	main.WriteString("local function verdict(input: buffer)\n")
	main.WriteString("\tlocal value = mod.NewProfileConfig()\n")
	main.WriteString("\tlocal report: any = {}\n")
	main.WriteString("\tlocal ok = mod.ProfileConfigFromJson(value, input, report)\n")
	main.WriteString("\thexs(if ok and not report.damaged then \"read\" else \"refused\")\n")
	main.WriteString("end\n")

	inputs := map[string][]byte{}
	for i, r := range rows {
		data, err := os.ReadFile(filepath.Join(repoRoot, "testdata", "wire", "json-comment-text-rules", r.name))
		if err != nil {
			t.Fatalf("the vector %q: %v", r.name, err)
		}
		name := fmt.Sprintf("f%d", i)
		inputs[name] = data
		fmt.Fprintf(&main, "verdict(inputs[%q])\n", name)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.moo"), []byte(main.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := moorun.RunErr(t, dir, "main.moo", inputs)
	if err != nil {
		t.Fatalf("the probe stopped: %v", err)
	}
	lines := decodeHexLines(t, out)
	if len(lines) != len(rows) {
		t.Fatalf("the probe printed %d verdicts for %d comment vectors", len(lines), len(rows))
	}
	for i, r := range rows {
		if lines[i] != r.verdict {
			t.Errorf("%s: got %s, want %s", r.name, lines[i], r.verdict)
		}
	}
	t.Logf("json comment text: %d comment vectors hold their verdict", len(rows))
}

// commentVector is one manifest line: a vector's file name and its verdict.
type commentVector struct {
	name    string
	verdict string
}

// commentVectors reads the manifest's comment rows, each with the verdict it
// must hold.
func commentVectors(t *testing.T) []commentVector {
	t.Helper()
	f, err := os.Open(filepath.Join(repoRoot, "testdata", "wire", "json-comment-text-rules", "manifest.tsv"))
	if err != nil {
		t.Fatalf("the vectors' manifest: %v", err)
	}
	defer f.Close()
	var rows []commentVector
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
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
