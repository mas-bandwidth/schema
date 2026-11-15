package javatable

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// TestJavaJsonCommentIsWellFormedUTF8 holds the Java JSON reader to
// docs/SPEC-TABLES.md §16.2's comment text rules: the bytes inside a line or
// block comment are well-formed UTF-8 and hold no NUL, so a comment carrying an
// ill-formed sequence or a zero refuses the read wherever a comment may sit.
// The compiler's own engine (internal/tabletext) is the reference: every vector
// under testdata/wire/json-comment-text-rules is read by the engine first, and
// the Java verdict is compared with it on the verdict alone.
func TestJavaJsonCommentIsWellFormedUTF8(t *testing.T) {
	t.Parallel()

	const schemaSrc = `package probe
table Root {
    name string(32)
    experience uint32 = 0
}
`
	u := unitFrom(t, schemaSrc)
	model := tabletext.NewModel(u)
	def := model.Lookup("Root")
	if def == nil {
		t.Fatal("missing Root")
	}

	const vectorDir = "../../../testdata/wire/json-comment-text-rules"
	manifest, err := os.Open(filepath.Join(vectorDir, "manifest.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	defer manifest.Close()

	type row struct {
		name    string
		verdict string
		path    string
	}
	var rows []row
	scanner := bufio.NewScanner(manifest)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cols := strings.SplitN(line, "\t", 3)
		if len(cols) != 3 {
			t.Fatalf("bad manifest line %q", line)
		}
		if cols[1] != "read" && cols[1] != "refused" {
			t.Fatalf("bad manifest verdict %q for %s", cols[1], cols[0])
		}
		rows = append(rows, row{name: cols[0], verdict: cols[1], path: filepath.Join(vectorDir, cols[0])})
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("manifest contains no comment-rule vectors")
	}

	for _, r := range rows {
		text, err := os.ReadFile(r.path)
		if err != nil {
			t.Fatal(err)
		}
		var report tabletext.Report
		ok := model.Read(model.New(def), text, &report)
		got := "refused"
		if ok && !report.Refused {
			got = "read"
		}
		if got != r.verdict {
			t.Fatalf("the engine reads %s as %s, the standard says %s (ok=%v report=%+v)", r.name, got, r.verdict, ok, report)
		}
	}

	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	var sources []string
	for name, src := range files {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, src, 0o600); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, ".java") {
			sources = append(sources, p)
		}
	}

	const driverSrc = `package probe;
import java.nio.file.Files;
import java.nio.file.Path;

public final class CommentProbe {
    public static void main(String[] args) throws Exception {
        for (String path : args) {
            byte[] input = Files.readAllBytes(Path.of(path));
            TableVariable.Report r = new TableVariable.Report();
            RootVariable.Value v = RootVariable.fromJson(input, r);
            boolean ok = v != null && !r.damaged && !r.refused;
            System.out.println(ok ? "read" : "refused");
        }
    }
}
`
	p := filepath.Join(dir, "CommentProbe.java")
	if err := os.WriteFile(p, []byte(driverSrc), 0o600); err != nil {
		t.Fatal(err)
	}
	sources = append(sources, p)

	javac, javaBin := javaToolsRequired(t)
	args := append([]string{"--release", "17", "-d", dir}, sources...)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	caseFiles := make([]string, len(rows))
	for i, r := range rows {
		caseFiles[i] = r.path
	}
	runArgs := append([]string{"-ea", "-cp", dir, "probe.CommentProbe"}, caseFiles...)
	out, err := exec.Command(javaBin, runArgs...).CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if len(lines) != len(rows) {
		t.Fatalf("got %d lines, want %d:\n%s", len(lines), len(rows), out)
	}
	for i, r := range rows {
		got := lines[i]
		if got != r.verdict {
			t.Errorf("%s: Java reads %s, the standard and the engine say %s", r.name, got, r.verdict)
		}
	}
}
