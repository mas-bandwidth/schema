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
// In the texts below @ stands for a backslash and is replaced before use, so
// this file itself holds no escape any tool could translate.
const docEscapeSchemaAt = `package docescape

/// a type doc @u000a and @@u0041 and @@@u0042 and @uuuu0043 and @u
type T
{
    /// a field doc @u000a with @u002a@u002f inside
    x uint8
}

/// a const doc @u000A
const K = 3

/// an enum doc @u
enum E
{
    /// a variant doc @u0022
    A
    B
}
`

func unAt(s string) string { return strings.ReplaceAll(s, "@", "\\") }

// escapedBackslash is the one Unicode escape a doc line may carry: the
// escape of a backslash, which javac reads as a backslash and never
// translates again.
var escapedBackslash = unAt("@u005c")

func generateDocEscapeJava(t *testing.T) string {
	t.Helper()
	src := unAt(docEscapeSchemaAt)
	f, errs := parser.Parse("Probe.schema", []byte(src))
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(src), AST: f,
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
	if docs < 5 {
		t.Fatalf("found %d doc lines, want at least 5:\n%s", docs, src)
	}
}

// The escaped doc reads back as its own text: each escapedBackslash read as
// the backslash it stands for gives the doc as written.
func TestDocCommentKeepsItsText(t *testing.T) {
	src := generateDocEscapeJava(t)
	for _, want := range []string{
		unAt("// a type doc @u000a and @@u0041 and @@@u0042 and @uuuu0043 and @u"),
		unAt("// a field doc @u000a with @u002a@u002f inside"),
		unAt("// a variant doc @u0022"),
	} {
		found := false
		for line := range strings.SplitSeq(src, "\n") {
			if strings.TrimSpace(strings.ReplaceAll(line, escapedBackslash, "\\")) == want {
				found = true
			}
		}
		if !found {
			t.Errorf("doc not found as its own text: %s", want)
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
