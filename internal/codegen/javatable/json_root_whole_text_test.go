package javatable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// TestJavaJsonRootIsWholeText is a functional test. It compiles and runs the
// generated Java the way TestJsonLeadingBOMBoundaryJava does, and compares
// one verdict with the engine's on the same text (docs/SPEC-TABLES.md §16.1,
// THE ROOT OBJECT IS THE WHOLE TEXT): whitespace and comments after the root
// are part of the text, and any other trailing byte is not a read.
func TestJavaJsonRootIsWholeText(t *testing.T) {
	slowtest.Gate(t, "the JDK (it compiles and runs the generated Java)")
	t.Parallel()
	javac, javaBin := javaToolsRequired(t)

	const schemaSrc = `package probe
table Root {
 value int32 = 7
}
`
	u := unitFrom(t, schemaSrc)
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

	cases := []struct {
		name  string
		input []byte
	}{
		{"after-x", []byte("{} x")},
		{"second-object", []byte("{}{}")},
		{"comma", []byte("{},")},
		{"nul", []byte("{}\x00")},
		{"slash", []byte("{} /")},
		{"unclosed-comment", []byte("{} /* x")},
		{"spaces-lf", []byte("{}  \n")},
		{"line-comment", []byte("{} // c")},
		{"block-comment", []byte("{} /* c */")},
	}

	model := tabletext.NewModel(u)
	def := model.Lookup("Root")
	if def == nil {
		t.Fatal("missing Root")
	}
	want := make([]string, len(cases))
	var caseFiles []string
	for i, c := range cases {
		name := fmt.Sprintf("case-%02d.json", i)
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, c.input, 0o600); err != nil {
			t.Fatal(err)
		}
		caseFiles = append(caseFiles, p)
		var report tabletext.Report
		inst := model.New(def)
		if model.Read(inst, c.input, &report) {
			want[i] = "read"
		} else {
			want[i] = "refused"
		}
	}

	const driverSrc = `package probe;
import java.nio.file.Files;
import java.nio.file.Path;

public final class RootWholeProbe {
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
	p := filepath.Join(dir, "RootWholeProbe.java")
	if err := os.WriteFile(p, []byte(driverSrc), 0o600); err != nil {
		t.Fatal(err)
	}
	sources = append(sources, p)
	args := append([]string{"--release", "17", "-d", dir}, sources...)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	runArgs := append([]string{"-ea", "-cp", dir, "probe.RootWholeProbe"}, caseFiles...)
	out, err := exec.Command(javaBin, runArgs...).CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != len(cases) {
		t.Fatalf("got %d lines, want %d:\n%s", len(lines), len(cases), out)
	}
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if lines[i] != want[i] {
				t.Errorf("Java %s, Go %s, bytes %x", lines[i], want[i], c.input)
			}
		})
	}
}
