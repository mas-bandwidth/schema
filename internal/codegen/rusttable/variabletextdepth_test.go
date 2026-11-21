package rusttable

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// variableDepthSrc reaches a table through every container the JSON reader
// nests: a nested table, an array, an enum-keyed array, a map and a union arm,
// plus a pointer chain through a field and through a pointer arm
// (docs/SPEC-TABLES.md §16.2, §16.7; the hostile corpus's depth-*-past-cap and
// label rows ride the same shapes in tables/maps, tables/stream and
// tables/pointers).
const variableDepthSrc = `package vdepth

enum Slot { Alpha, Beta }

table Leaf
{
    n int32
}

table Node
{
    data int32
    next *Node
}

union Pick
{
    leaf  Leaf
    link  *Node
    count int32
}

table Root
{
    one   Leaf
    many  [..3]Leaf
    keyed [Slot]Leaf
    dict  map[uint8]Leaf
    arm   Pick
    head  *Node
    alias *Node
}
`

// TestRustVariableJsonDepthAndSkippedLabels holds the Rust JSON reader to the
// Go oracle (internal/tabletext Read) on the two rules the hostile corpus
// caught it breaking:
//
//   - THE DEPTH CAP (§16.2, 128 levels): an array, an enum-keyed array, a map
//     and a union each open a level in the oracle's count, so an unknown value
//     nested under any of them is refused at exactly the oracle's depth; a
//     pointer chain is counted the same way through a field and through a
//     pointer arm.
//   - THE LABEL RULES IN A SKIPPED VALUE (§16.7): "&node" only, first key only,
//     an integer label; a reference needs a closed definition; a definition
//     needs an undefined label; and a reference may carry a trailing comma.
//
// Refusal is judged by verdict only; an accepted text must be written back as
// the oracle writes it.
//
// A FUNCTIONAL test: it compiles Rust, skips by name without rustc, and fails
// by name when SCHEMA_REQUIRE_CORPUS is set.
func TestRustVariableJsonDepthAndSkippedLabels(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "rustc (it compiles and runs the generated JSON depth probe)")
	u := unitFrom(t, variableDepthSrc)
	m := tabletext.NewModel(u)

	type jcase struct {
		name, text string
		accept     bool
		wantText   string
	}
	var cases []jcase
	add := func(name, text string) {
		inst := m.New(m.Lookup("Root"))
		var rep tabletext.Report
		c := jcase{name: name, text: text, accept: m.Read(inst, []byte(text), &rep)}
		if c.accept {
			out, err := m.Write(inst)
			if err != nil {
				t.Fatal(err)
			}
			c.wantText = string(out)
		}
		cases = append(cases, c)
	}

	nest := func(k int) string { return strings.Repeat("[", k) + strings.Repeat("]", k) }
	paths := map[string]string{
		"one":   `{"one":{"zz":%s}}`,
		"many":  `{"many":[{"zz":%s}]}`,
		"keyed": `{"keyed":{"Alpha":{"zz":%s}}}`,
		"dict":  `{"dict":{"1":{"zz":%s}}}`,
		"arm":   `{"arm":{"leaf":{"zz":%s}}}`,
	}
	for _, p := range []string{"one", "many", "keyed", "dict", "arm"} {
		for k := 1; k <= 135; k++ {
			if k > 3 && k < 118 {
				continue
			}
			add(fmt.Sprintf("%s_nest_%d", p, k), fmt.Sprintf(paths[p], nest(k)))
		}
	}
	chain := func(n int) string {
		return strings.Repeat(`{"data":1,"next":`, n) + "null" + strings.Repeat("}", n)
	}
	for n := 1; n <= 135; n++ {
		if n > 3 && (n < 55 || n > 70) && n < 118 {
			continue
		}
		add(fmt.Sprintf("head_chain_%d", n), `{"head":`+chain(n)+`}`)
		add(fmt.Sprintf("arm_chain_%d", n), `{"arm":{"link":`+chain(n)+`}}`)
	}
	labels := []struct{ name, text string }{
		{"reference_trailing_comma", `{"head": {"&node": 1, "data": 5}, "alias": {"&node": 1,}}`},
		{"skipped_defined_twice", `{"head": {"&node": 1, "data": 1}, "zz": {"&node": 1, "data": 2}}`},
		{"skipped_fraction", `{"zz": {"&node": 1.5}}`},
		{"skipped_misspelled", `{"zz": {"&foo": 1}}`},
		{"skipped_not_first", `{"zz": {"data": 1, "&node": 2}}`},
		{"skipped_plain_object", `{"zz": {"data": 1}, "head": {"&node": 5, "data": 1}}`},
		{"skipped_reference_before_definition", `{"zz": {"&node": 5}, "head": {"&node": 5, "data": 1}}`},
		{"skipped_reference_undefined", `{"zz": {"&node": 5}}`},
		{"skipped_reference_closed", `{"head": {"&node": 5, "data": 1}, "zz": {"&node": 5}}`},
		{"skipped_reference_open", `{"head": {"&node": 5, "data": 1, "zz": {"&node": 5}}}`},
		{"skipped_definition_then_reference", `{"zz": {"&node": 5, "data": 1}, "head": {"&node": 5}}`},
		{"skipped_definition_then_skipped_reference", `{"zz": {"&node": 5, "data": 1}, "yy": {"&node": 5}}`},
		{"skipped_two_labels", `{"zz": {"&node": 5, "&node": 6}}`},
	}
	for _, l := range labels {
		add(l.name, l.text)
	}

	// the oracle's answers must reach the rules, or the test proves nothing:
	// every shallow case reads, every path is refused at its deepest case, and
	// the corpus's label rows answer as the corpus says they do
	verdict := map[string]bool{}
	for _, c := range cases {
		verdict[c.name] = c.accept
	}
	for _, p := range []string{"one", "many", "keyed", "dict", "arm"} {
		if !verdict[p+"_nest_1"] || verdict[p+"_nest_135"] {
			t.Fatalf("%s: the oracle reads nest_1=%v nest_135=%v; the probe does not reach the cap", p, verdict[p+"_nest_1"], verdict[p+"_nest_135"])
		}
	}
	for _, p := range []string{"head", "arm"} {
		if !verdict[p+"_chain_1"] || verdict[p+"_chain_135"] {
			t.Fatalf("%s: the oracle reads chain_1=%v chain_135=%v; the probe does not reach the cap", p, verdict[p+"_chain_1"], verdict[p+"_chain_135"])
		}
	}
	for name, want := range map[string]bool{
		"reference_trailing_comma": true, "skipped_plain_object": true,
		"skipped_defined_twice": false, "skipped_fraction": false, "skipped_misspelled": false,
		"skipped_not_first": false, "skipped_reference_before_definition": false, "skipped_reference_undefined": false,
	} {
		if verdict[name] != want {
			t.Fatalf("%s: the oracle accept=%v, the corpus says %v", name, verdict[name], want)
		}
	}

	if _, err := exec.LookPath("rustc"); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and rustc is not on PATH, so the JSON depth probe cannot be built: %v", err)
		}
		t.Skipf("rustc is not on PATH and SCHEMA_REQUIRE_CORPUS is unset: this is a functional test that compiles and runs Rust: %v", err)
	}
	modules, err := Generate(u)
	if err != nil {
		t.Fatalf("rust refused the depth unit: %v", err)
	}
	dir := t.TempDir()
	layoutRustUnit(t, dir, "tblvdepth", modules)
	const main = `mod tblvdepth;
use std::io::BufRead;
fn main() {
    for line in std::io::stdin().lock().lines() {
        let line = line.unwrap();
        let mut report = tblvdepth::TableReport::default();
        match tblvdepth::root_from_json(&line, &mut report) {
            Some(v) if !report.damaged && !report.refused => match tblvdepth::root_to_json_checked(&v) {
                Some(text) => println!("accept {}", text.replace('\n', " ")),
                None => println!("accept-unwritable"),
            },
            _ => println!("refuse"),
        }
    }
}
`
	if err := os.WriteFile(filepath.Join(dir, "main.rs"), []byte(main), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := rustcBuild(t, dir)

	var stdin strings.Builder
	for _, c := range cases {
		stdin.WriteString(c.text + "\n")
	}
	cmd := exec.Command(bin)
	cmd.Stdin = strings.NewReader(stdin.String())
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("the Rust probe failed: %v\n%s", err, stderr.String())
	}
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) != len(cases) {
		t.Fatalf("the Rust probe answered %d lines, want %d", len(lines), len(cases))
	}
	oneLine := func(s string) string { return strings.Join(strings.Fields(s), " ") }
	for i, c := range cases {
		got := lines[i]
		if !c.accept {
			if got != "refuse" {
				t.Errorf("%s: the oracle refuses, the Rust reader accepts (%s)", c.name, strings.Fields(got)[0])
			}
			continue
		}
		text, ok := strings.CutPrefix(got, "accept ")
		if !ok {
			t.Errorf("%s: the oracle accepts, the Rust reader refuses", c.name)
			continue
		}
		if oneLine(text) != oneLine(c.wantText) {
			t.Errorf("%s: the Rust text differs from the oracle's:\n%s\nwant:\n%s", c.name, text, c.wantText)
		}
	}
}
