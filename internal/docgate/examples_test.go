package docgate

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

var updateCounts = flag.Bool("update", false, "rewrite testdata/example-counts.tsv from the pages")

// TestExamplesAreMarked is the fast gate (review finding F-173): every fenced
// target-language block on the README, the docs pages and the skill pages is
// a compile example, a leg's own usage example or a marked fragment, every
// compile example names a unit and a harness that exist, every schema fence
// tied to a unit equals the unit's file, and every leg marker is still read
// by the gate registered for it.
func TestExamplesAreMarked(t *testing.T) {
	root, err := FindRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	_, problems, err := CheckExamples(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range problems {
		t.Errorf("%s", p)
	}
}

// countLines renders the tally the way testdata/example-counts.tsv holds it:
// one line per page, language and class, sorted.
func countLines(examples []Example) []string {
	var lines []string
	for key, n := range CountExamples(examples) {
		class := key.Class
		if class == "" {
			class = "unmarked"
		}
		lines = append(lines, fmt.Sprintf("%s\t%s\t%s\t%d", key.Page, key.Lang, class, n))
	}
	sort.Strings(lines)
	return lines
}

// TestExampleCounts holds the number of examples found on each page, by
// language and class, to the recorded count. An extractor that stops finding
// a page's examples would otherwise pass every other check with nothing to
// check, so a count that moves in either direction is red until the record
// moves with it (`go test ./internal/docgate -run TestExampleCounts -update`),
// and a page whose examples are all gone is red by its missing lines.
func TestExampleCounts(t *testing.T) {
	root, err := FindRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	examples, _, err := Examples(root)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(countLines(examples), "\n") + "\n"
	record := filepath.Join("testdata", "example-counts.tsv")
	if *updateCounts {
		if err := os.WriteFile(record, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(data)), "\n") {
		want[line] = true
	}
	if len(want) < 20 {
		t.Fatalf("%s records %d lines: the record itself is short", record, len(want))
	}
	have := map[string]bool{}
	for _, line := range countLines(examples) {
		have[line] = true
		if !want[line] {
			t.Errorf("found and not recorded: %s", line)
		}
	}
	for line := range want {
		if !have[line] {
			t.Errorf("recorded and not found: %s", line)
		}
	}
	compiled := 0
	for _, e := range examples {
		if e.Class == ClassCompile || e.Class == ClassLeg {
			compiled++
		}
	}
	if compiled == 0 {
		t.Fatal("no page carries a compiled example: the gate has nothing to hold")
	}
}

// TestExamplesGateGoesRed is the gate's own control: one planted fault of
// each kind surfaces by name, and the clean examples beside them are found
// and counted.
func TestExamplesGateGoesRed(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	fence := "```"
	write("go.mod", "module fixture\n\ngo 1.26\n")
	write("unit/A.schema", "package a\n")
	write("internal/docgate/testdata/harness/wrap.cpp", "int main() {\n// EXAMPLE\n}\n")
	write("make/c.mk", "nothing reads the marker here\n")
	write("README.md", strings.Join([]string{
		"# Fixture", "",
		"<!-- example: compile unit=unit harness=wrap -->", fence + "cpp", "return 0;", fence, "",
		"<!-- example: fragment -->", fence + "go", "x := 1", fence, "",
		"<!-- example: schema unit=unit file=A.schema -->", fence, "package a", fence, "",
		fence + "sh", "$ ls", fence, "",
	}, "\n"))
	write("docs/BAD.md", strings.Join([]string{
		"# Bad", "",
		fence + "rust", "fn main() {}", fence, "",
		fence + "cobol", "MOVE 1 TO X.", fence, "",
		"<!-- example: compile unit=gone -->", fence + "c", "int x;", fence, "",
		"<!-- example: compile unit=unit harness=gone -->", fence + "c", "int x;", fence, "",
		"<!-- example: compile -->", fence + "c", "int x;", fence, "",
		"<!-- example: fragment -->", "", "prose where a fence was owed", "",
		"<!-- example: schema unit=unit file=A.schema -->", fence, "package b", fence, "",
		"<!-- c-table-usage -->", fence + "c", "int y;", fence, "<!-- /c-table-usage -->", "",
		"<!-- nobody-usage -->", fence + "c", "int z;", fence, "<!-- /nobody-usage -->", "",
	}, "\n"))
	write("skills/s/SKILL.md", "# Skill\n\n"+fence+"lua\nlocal x = 1\n"+fence+"\n")

	examples, problems, err := CheckExamples(root)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, p := range problems {
		reason, _, _ := strings.Cut(p.Reason, ":")
		got[p.File+" "+reason]++
	}
	want := map[string]int{
		"docs/BAD.md example-unmarked":       2, // the rust block, and the compile marker with no unit
		"docs/BAD.md example-language":       1,
		"docs/BAD.md example-unit":           1,
		"docs/BAD.md example-harness":        1,
		"docs/BAD.md example-marker":         2, // no unit; no fence
		"docs/BAD.md example-schema":         1,
		"docs/BAD.md example-leg":            1, // the unregistered marker
		"make/c.mk example-leg":              1, // the registered gate that no longer reads it
		"skills/s/SKILL.md example-unmarked": 1,
	}
	for key, n := range want {
		if got[key] != n {
			t.Errorf("planted %s: want %d finding(s), got %d", key, n, got[key])
		}
	}
	for key, n := range got {
		if want[key] == 0 {
			t.Errorf("unplanned finding %s (%d)", key, n)
		}
	}
	if t.Failed() {
		for _, p := range problems {
			t.Logf("%s", p)
		}
	}
	lines := strings.Join(countLines(examples), "\n")
	for _, line := range []string{
		"README.md\tcpp\tcompile\t1",
		"README.md\tgo\tfragment\t1",
		"README.md\tschema\tschema\t1",
		"docs/BAD.md\tc\tleg\t2",
		"docs/BAD.md\trust\tunmarked\t1",
		"skills/s/SKILL.md\tlua\tunmarked\t1",
	} {
		if !strings.Contains(lines, line) {
			t.Errorf("the count is missing %q:\n%s", line, lines)
		}
	}
	source, err := examples[0].Source(root)
	if err != nil || source != "int main() {\nreturn 0;\n}\n" {
		t.Errorf("the harness splice gave %q, %v", source, err)
	}
}
