package lobstertable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// THE TEXT FORM'S LIMITS AND ITS GAP RULE (docs/SPEC-TABLES.md §16.9.1,
// §16.9.4; the reference is gotable's textform.go): a document past the
// contract's 65536 bytes, 8192 tokens, 4096 nodes or 65536 decoded bytes, or
// nested past 128 lists (the root counts), is text_limit and never a prefix
// read; two atoms need a gap between them (text_syntax); a comment holds only
// well-formed UTF-8 (text_encoding).

const textLimitsSchema = `package textlimitsprobe
table Node { value int32
 other int32
 next *Node
 a [..9000]int32 }
`

func nested(lists int) string {
	return strings.Repeat("(:next ", lists-1) + "()" + strings.Repeat(")", lists-1)
}

func TestLobsterTextFormLimitsAndGaps(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Lobster toolchain")
	path := filepath.Join(t.TempDir(), "Node.schema")
	if err := os.WriteFile(path, []byte(textLimitsSchema), 0644); err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, []string{path})
	if err != nil {
		t.Fatal(err)
	}
	numbers := func(n int) string { return "(:a (" + strings.Repeat("1 ", n) + "))" }
	pad := func(total int) string { return "();" + strings.Repeat("x", total-3) }
	cases := []struct{ name, input, want string }{
		{"depth_128", nested(128), "read"},
		{"depth_129", nested(129), "text_limit"},
		{"tokens_under", numbers(8000), "read"},
		{"tokens_over", numbers(8200), "text_limit"},
		{"bytes_exact", pad(65536), "read"},
		{"bytes_over", pad(65537), "text_limit"},
		{"gap", `(:value 1 :other 2)`, "read"},
		{"no_gap_number_keyword", `(:value 1:other 2)`, "text_syntax"},
		{"no_gap_keyword_keyword", `(:value:other 2)`, "text_syntax"},
		{"quote_needs_no_gap", `(:value 1 "other" 2)`, "read"},
		{"comment_utf8", "(); caf\u00e9 \u4e16\u754c", "read"},
		{"comment_bad_byte", "(); \xff", "text_encoding"},
		{"comment_surrogate", "(); \xed\xa0\x80", "text_encoding"},
		{"comment_overlong", "(); \xc0\x80", "text_encoding"},
		{"comment_truncated", "(); \xe4\xb8", "text_encoding"},
	}
	var program strings.Builder
	for _, mod := range g.mods {
		fmt.Fprintf(&program, "import .%s\n", mod)
	}
	program.WriteString(`import .text_form
def probe(name:string, i:int):
    let input = read_file("input{i}")
    assert input
    let doc, why = schema_rt.text_read("Node", input)
    print(name + " " + (if doc: "read" else: why))
`)
	inputs := map[string][]byte{}
	for i, c := range cases {
		inputs[fmt.Sprintf("input%d", i)] = []byte(c.input)
		fmt.Fprintf(&program, "probe(%q, %d)\n", c.name, i)
	}
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(program.String()), 0644); err != nil {
		t.Fatal(err)
	}
	out := strings.Split(strings.TrimRight(string(lobsterrun.Run(t, g.dir, "main.lobster", inputs)), "\n"), "\n")
	if len(out) != len(cases) {
		t.Fatalf("got %d lines, want %d:\n%s", len(out), len(cases), strings.Join(out, "\n"))
	}
	for i, c := range cases {
		if want := c.name + " " + c.want; out[i] != want {
			t.Errorf("got %q, want %q", out[i], want)
		}
	}
}
