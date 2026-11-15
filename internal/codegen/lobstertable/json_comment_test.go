package lobstertable_test

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
)

// commentRow is one text the comment rule decides, with the verdict the
// standard states for it.
type commentRow struct {
	name, verdict string
	text          []byte
}

// commentRows are the vectors of testdata/wire/json-comment-text-rules, read
// at tables/examples' root ProfileConfig, then each comment payload placed
// inside an unknown key's value, where the reader skips rather than stores.
func commentRows(t *testing.T) []commentRow {
	t.Helper()
	dir := filepath.Join(root, "testdata", "wire", "json-comment-text-rules")
	f, err := os.Open(filepath.Join(dir, "manifest.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var rows []commentRow
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
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
		rows = append(rows, commentRow{name: cols[0], verdict: cols[1], text: text})
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	payloads := []struct {
		name, verdict, bytes string
	}{
		{"utf8", "read", "caf\xc3\xa9 \xe6\x97\xa5 \xf0\x9f\x98\x80"},
		{"lone-continuation-0x80", "refused", "\x80"},
		{"overlong-c0-af", "refused", "\xc0\xaf"},
		{"truncated-three-byte", "refused", "\xe6\x97"},
		{"encoded-surrogate-ed-a0-80", "refused", "\xed\xa0\x80"},
		{"nul", "refused", "\x00"},
	}
	for _, p := range payloads {
		rows = append(rows,
			commentRow{
				name:    "unknown-key-line-comment-" + p.name,
				verdict: p.verdict,
				text:    []byte("{ \"not_a_field\": [ // " + p.bytes + "\n 1 ] }\n"),
			},
			commentRow{
				name:    "unknown-key-block-comment-" + p.name,
				verdict: p.verdict,
				text:    []byte("{ \"not_a_field\": [ /* " + p.bytes + " */ 1 ] }\n"),
			})
	}
	return rows
}

// TestLobsterJsonCommentIsWellFormedUTF8 holds runtime/json.lobster to
// docs/SPEC-TABLES.md §16.2's comment text rule: the bytes inside a comment,
// line or block, are well-formed UTF-8 (§16.3) and hold no NUL, in a placed
// position and inside an unknown key's value. Each row's verdict is first
// the reference engine's (internal/tabletext), then the Lobster leg's.
func TestLobsterJsonCommentIsWellFormedUTF8(t *testing.T) {
	t.Parallel()
	const rootName = "ProfileConfig"
	paths, err := compiler.GatherPaths([]string{filepath.Join(root, "tables", "examples")})
	if err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, paths)
	if err != nil {
		t.Fatalf("the Lobster table surface refuses tables/examples: %v", err)
	}
	rows := commentRows(t)
	for _, r := range rows {
		if got := goJsonVerdict(t, g.u, rootName, r.text); got != r.verdict {
			t.Fatalf("%s: the reference %s, the standard %s; the comparison is not grounded", r.name, got, r.verdict)
		}
	}

	var b strings.Builder
	for _, m := range g.mods {
		fmt.Fprintf(&b, "import .%s\n", m)
	}
	b.WriteString("import .load\nimport .json\n")
	b.WriteString(`def comment_row(i:int):
    let rep = schema_rt.LoadReport {}
    let text = read_file("c{i}")
    assert text
    if schema_rt.json_read("` + rootName + `", text, rep):
        print("C{i} read")
    else:
        print("C{i} refused")
`)
	inputs := map[string][]byte{}
	for i, r := range rows {
		inputs[fmt.Sprintf("c%d", i)] = r.text
		fmt.Fprintf(&b, "comment_row(%d)\n", i)
	}
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	out := lobsterrun.Run(t, g.dir, "main.lobster", inputs)
	got := map[int]string{}
	for line := range strings.SplitSeq(strings.TrimRight(string(out), "\n"), "\n") {
		var i int
		var v string
		if _, err := fmt.Sscanf(line, "C%d %s", &i, &v); err != nil {
			t.Fatalf("the program printed %q", line)
		}
		got[i] = v
	}
	for i, r := range rows {
		if got[i] != r.verdict {
			t.Errorf("%s: lobster %q, the reference %s", r.name, got[i], r.verdict)
		}
	}
	t.Logf("%d comment texts compared on the verdict", len(rows))
}
