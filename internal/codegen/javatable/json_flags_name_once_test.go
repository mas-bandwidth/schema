package javatable

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// TestJavaJsonFlagsNameOnce holds the Java JSON reader to the flags row of
// docs/SPEC-TABLES.md §16.2: a flags array that names one KNOWN variant twice
// refuses the read, as a repeated key does, while an unknown name keeps its
// skip-and-count treatment and a flags array inside a value the read only
// skips is held to the JSON grammar alone. Every row of
// testdata/wire/json-flags-name-once/ is compared on the verdict alone with
// the engine's (internal/tabletext) verdict on the same text, and the engine
// is first held to the verdict the corpus states. It is a functional test: it
// builds and runs the generated Java.
func TestJavaJsonFlagsNameOnce(t *testing.T) {
	t.Parallel()
	javac, javaBin := javaToolsRequired(t)

	repo := filepath.Join("..", "..", "..")
	corpus := filepath.Join(repo, "testdata", "wire", "json-flags-name-once")
	manifest, err := os.ReadFile(filepath.Join(corpus, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	type row struct {
		name, unit, root, text, verdict, unknown string
	}
	var rows []row
	for line := range strings.SplitSeq(string(manifest), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 6 {
			t.Fatalf("manifest row %q: want 6 tab-separated columns, got %d", line, len(f))
		}
		if f[4] != "read" && f[4] != "refused" {
			t.Fatalf("manifest row %q: verdict %q is neither read nor refused", line, f[4])
		}
		rows = append(rows, row{f[0], f[1], f[2], f[3], f[4], f[5]})
	}
	if len(rows) == 0 {
		t.Fatal("the manifest names no rows")
	}
	unit, root := rows[0].unit, rows[0].root
	for _, r := range rows {
		if r.unit != unit || r.root != root {
			t.Fatalf("row %s: unit %s root %s; this test reads one root (%s %s)", r.name, r.unit, r.root, unit, root)
		}
	}

	u := unitFromDir(t, filepath.Join(repo, unit))
	model := tabletext.NewModel(u)
	def := model.Lookup(root)
	if def == nil {
		t.Fatalf("missing %s", root)
	}

	// THE REFERENCE FIRST: the engine gives the verdict the corpus states,
	// and the unknown count where the row pins one.
	engine := make([]bool, len(rows))
	var paths []string
	for i, r := range rows {
		p := filepath.Join(corpus, r.text)
		text, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			t.Fatal(err)
		}
		paths = append(paths, abs)
		var report tabletext.Report
		ok := model.Read(model.New(def), text, &report) && !report.Refused
		if ok != (r.verdict == "read") {
			t.Fatalf("engine on %s: read=%v (%s), the corpus states %s", r.name, ok, report.Reason, r.verdict)
		}
		if ok && r.unknown != "-" {
			want, err := strconv.Atoi(r.unknown)
			if err != nil {
				t.Fatalf("row %s: unknown %q: %v", r.name, r.unknown, err)
			}
			if report.Unknown != want {
				t.Fatalf("engine on %s: unknown %d, the corpus states %d", r.name, report.Unknown, want)
			}
		}
		engine[i] = ok
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
	driverSrc := `package ` + u.Package + `;
import java.nio.file.Files;
import java.nio.file.Path;

public final class FlagsNameOnceProbe {
    public static void main(String[] args) throws Exception {
        for (String path : args) {
            byte[] input = Files.readAllBytes(Path.of(path));
            TableVariable.Report r = new TableVariable.Report();
            ` + root + `Variable.Value v = ` + root + `Variable.fromJson(input, r);
            boolean ok = v != null && !r.damaged && !r.refused;
            System.out.println(ok ? "read" : "refused");
        }
    }
}
`
	p := filepath.Join(dir, "FlagsNameOnceProbe.java")
	if err := os.WriteFile(p, []byte(driverSrc), 0o600); err != nil {
		t.Fatal(err)
	}
	sources = append(sources, p)
	args := append([]string{"--release", "17", "-d", dir}, sources...)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	runArgs := append([]string{"-ea", "-cp", dir, u.Package + ".FlagsNameOnceProbe"}, paths...)
	out, err := exec.Command(javaBin, runArgs...).CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != len(rows) {
		t.Fatalf("got %d lines, want %d:\n%s", len(lines), len(rows), out)
	}
	for i, r := range rows {
		want := "refused"
		if engine[i] {
			want = "read"
		}
		if lines[i] != want {
			t.Errorf("%s (%s): Java %s, engine %s", r.name, r.text, lines[i], want)
		}
	}
}
