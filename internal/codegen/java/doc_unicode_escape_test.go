package java

import (
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
)

// JAVA-1: no doc comment text can break or change the generated Java. javac
// translates Unicode escapes before it reads comments (JLS §3.3), so a doc
// line holding a backslash-u sequence must reach the source with no escape
// javac would translate: the doc keeps its meaning as comment text and never
// becomes code, and a malformed escape never fails the build.
//
// The schema and its doc lines are the committed corpus
// testdata/doc-escape/{Probe.schema,cases.tsv} (F-203): one row per doc line
// as written, beside a plain doc that is the control. In this file @ stands
// for a backslash and is replaced before use, so this file itself holds no
// escape any tool could translate.
const docEscapeDir = "testdata/doc-escape"

func unAt(s string) string { return strings.ReplaceAll(s, "@", "\\") }

// escapedBackslash is the one Unicode escape a doc line may carry: the
// escape of a backslash, which javac reads as a backslash and never
// translates again.
var escapedBackslash = unAt("@u005c")

// docEscapeCase is one row of testdata/doc-escape/cases.tsv.
type docEscapeCase struct{ id, doc string }

func docEscapeCases(t *testing.T) []docEscapeCase {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(docEscapeDir, "cases.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	var out []docEscapeCase
	for n, line := range strings.Split(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		id, doc, ok := strings.Cut(line, "\t")
		if !ok || strings.Contains(doc, "\t") {
			t.Fatalf("cases.tsv line %d: want two tab-separated fields", n+1)
		}
		out = append(out, docEscapeCase{id, doc})
	}
	if len(out) < 6 {
		t.Fatalf("cases.tsv holds %d rows, want the five escaped docs and the plain control", len(out))
	}
	return out
}

func generateDocEscapeJava(t *testing.T) string {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(docEscapeDir, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	f, errs := parser.Parse("Probe.schema", src)
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: src, AST: f,
	}})
	if len(cerrs) != 0 {
		t.Fatal(cerrs)
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	return string(files["Probe.java"])
}

// eligibleUnicodeEscape reports whether s holds a backslash javac would read
// as the start of a Unicode escape (one preceded by an even number of
// contiguous backslashes and followed by a 'u') other than escapedBackslash.
func eligibleUnicodeEscape(s string) bool {
	run := 0
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' {
			run = 0
			continue
		}
		if run%2 == 0 && i+1 < len(s) && s[i+1] == 'u' && !strings.HasPrefix(s[i:], escapedBackslash) {
			return true
		}
		run++
	}
	return false
}

func TestDocCommentHasNoUnicodeEscape(t *testing.T) {
	src := generateDocEscapeJava(t)
	docs := 0
	for line := range strings.SplitSeq(src, "\n") {
		t2 := strings.TrimSpace(line)
		if !strings.HasPrefix(t2, "// a ") && !strings.HasPrefix(t2, "// an ") {
			continue
		}
		docs++
		if eligibleUnicodeEscape(t2) {
			t.Errorf("doc line carries a Unicode escape javac would translate: %s", t2)
		}
	}
	if want := len(docEscapeCases(t)); docs < want {
		t.Fatalf("found %d doc lines, want at least %d:\n%s", docs, want, src)
	}
}

// Every committed doc reads back as its own text, from a line javac reads as
// comment: each escapedBackslash read as the backslash it stands for gives
// the doc as written, and the line itself carries no escape javac would
// translate.
func TestDocCommentKeepsItsText(t *testing.T) {
	src := generateDocEscapeJava(t)
	for _, c := range docEscapeCases(t) {
		want := "// " + c.doc
		var line string
		for l := range strings.SplitSeq(src, "\n") {
			if strings.TrimSpace(strings.ReplaceAll(l, escapedBackslash, "\\")) == want {
				line = strings.TrimSpace(l)
			}
		}
		switch {
		case line == "":
			t.Errorf("%s: doc not found as its own text: %s", c.id, want)
		case eligibleUnicodeEscape(line):
			t.Errorf("%s: doc line carries a Unicode escape javac would translate: %s", c.id, line)
		}
	}
}

// The generated unit compiles with javac where a JDK is installed.
func TestDocCommentEscapeCompiles(t *testing.T) {
	slowtest.Gate(t, "javac")
	javac, _ := compressedFloatJavaTools(t)
	src := generateDocEscapeJava(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "Probe.java")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := testproc.Command(t, javac, "--release", "17", "-nowarn", "-d", filepath.Join(dir, "classes"), path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
}
