package tabledemo_test

import (
	"bufio"
	"os"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	tabledemo "github.com/mas-bandwidth/schema/v2/internal/codegen/gotable/fuzzunit"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// commentReader is one generated JSON read entry: ProfileConfigFromJSON reads
// a plain table, PatrolFromJSON reads a variable table through its builder.
type commentReader struct {
	root string
	read func(text []byte) (bool, string)
}

var commentReaders = []commentReader{
	{"ProfileConfig", func(text []byte) (bool, string) {
		var v tabledemo.ProfileConfig
		var r tabledemo.TableReport
		ok := tabledemo.ProfileConfigFromJSON(&v, text, &r)
		return ok, string(r.Reason)
	}},
	{"Patrol", func(text []byte) (bool, string) {
		var b tabledemo.PatrolBuilder
		if !b.Init() {
			return false, "builder init"
		}
		defer b.Shutdown()
		var r tabledemo.TableReport
		ok := tabledemo.PatrolFromJSON(&b, text, &r)
		return ok, string(r.Reason)
	}},
}

// commentPlacements puts one comment's bytes after the opening brace and after
// the closing brace, as a line comment and as a block comment.
func commentPlacements(body string) map[string][]byte {
	return map[string][]byte{
		"line after {":  []byte("{//" + body + "\n}\n"),
		"block after {": []byte("{/*" + body + "*/}\n"),
		"line after }":  []byte("{}//" + body + "\n"),
		"block after }": []byte("{}/*" + body + "*/\n"),
	}
}

// TestGeneratedJSONCommentIsWellFormedUTF8 holds every generated JSON read
// entry to the comment-text rule in docs/SPEC-TABLES.md §16.2, with tabletext
// as reference: the bytes of a comment are well-formed UTF-8 and hold no NUL.
func TestGeneratedJSONCommentIsWellFormedUTF8(t *testing.T) {
	t.Parallel()
	const corpus = "../../../../testdata/wire/json-comment-text-rules/"
	manifest, err := os.Open(corpus + "manifest.tsv")
	if err != nil {
		t.Fatal(err)
	}
	defer manifest.Close()

	c := compiler.New()
	paths, err := compiler.GatherPaths([]string{"../../../../tables/examples"})
	if err != nil {
		t.Fatal(err)
	}
	u, err := c.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(u)

	check := func(name string, text []byte, wantRead bool) {
		for _, reader := range commentReaders {
			root := m.Lookup(reader.root)
			if root == nil {
				t.Fatalf("tabletext has no %s root", reader.root)
			}
			var engineReport tabletext.Report
			engineRead := m.Read(m.New(root), text, &engineReport)
			if engineRead != wantRead {
				t.Errorf("tabletext reference %s on %s: read=%v report=%+v, want read=%v", reader.root, name, engineRead, engineReport, wantRead)
				continue
			}
			generatedRead, reason := reader.read(text)
			if generatedRead != engineRead {
				t.Errorf("%sFromJSON on %s: generated read=%v reason=%q, tabletext read=%v reason=%q", reader.root, name, generatedRead, reason, engineRead, engineReport.Reason)
			}
		}
	}

	s := bufio.NewScanner(manifest)
	for s.Scan() {
		line := s.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cols := strings.SplitN(line, "\t", 3)
		if len(cols) != 3 {
			t.Fatalf("malformed manifest row %q", line)
		}
		text, err := os.ReadFile(corpus + cols[0])
		if err != nil {
			t.Fatal(err)
		}
		check(cols[0], text, cols[1] == "read")
	}
	if err := s.Err(); err != nil {
		t.Fatal(err)
	}

	refused := map[string]string{
		"0xFF":                 "\xff",
		"overlong C0 AF":       "\xc0\xaf",
		"overlong E0 80 AF":    "\xe0\x80\xaf",
		"overlong F0 80 80 AF": "\xf0\x80\x80\xaf",
		"surrogate ED A0 80":   "\xed\xa0\x80",
		"surrogate ED BF BF":   "\xed\xbf\xbf",
		"above U+10FFFF":       "\xf4\x90\x80\x80",
		"NUL":                  "\x00",
	}
	for what, body := range refused {
		for where, text := range commentPlacements(" a " + body + " b ") {
			check(where+" holding "+what, text, false)
		}
	}
	read := map[string]string{
		"multi-byte": " café 日本語 😀 U+10FFFF \xf4\x8f\xbf\xbf ",
	}
	for what, body := range read {
		for where, text := range commentPlacements(body) {
			check(where+" holding "+what, text, true)
		}
	}
	check("'*/' inside a line comment", []byte("{// a */ b\n}\n"), true)
}
