package javatable

// THE SKIPPED-VALUE RULE IN THE JAVA LEG (docs/SPEC-TABLES.md §3.3, §4,
// §16.3; docs/rulings.json skipped-value-is-validated): a value the reader
// SKIPS is held to what a read of it would hold, and damage inside it refuses
// the whole read. The shared vectors under testdata/wire/skipped-value/ are
// the cases, each with its verdict in MANIFEST.tsv. A message row decodes a
// batch against the writer unit's announcement; a JSON row reads at the reader
// unit's Root. Verdicts only: a refusal's name is the leg's own diagnostic and
// is never compared (docs/rulings.json refusal-verdict-only).

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const skippedValueProbe = `package skippedreader;

import java.nio.file.Files;
import java.nio.file.Paths;

public final class SkippedValueProbe {
    private SkippedValueProbe() {}

    public static void main(String[] args) throws Exception {
        byte[] announcement = Files.readAllBytes(Paths.get(args[0]));
        for (int i = 1; i + 2 < args.length; i += 3) {
            String name = args[i];
            String form = args[i + 1];
            byte[] data = Files.readAllBytes(Paths.get(args[i + 2]));
            String verdict;
            if (form.equals("json")) {
                TableVariable.Report report = new TableVariable.Report();
                RootVariable.Value value = RootVariable.fromJson(data, report);
                verdict = (value != null && !report.refused && !report.damaged) ? "read" : "refused";
            } else if (form.equals("announce")) {
                TableVocabulary vocab = new TableVocabulary();
                vocab.reset(4096);
                TableVocabulary.Report report = new TableVocabulary.Report();
                boolean ok = TableVocabulary.announceRead(vocab, data, report);
                verdict = (ok && !report.refused && !report.damaged) ? "read" : "refused";
            } else {
                TableVocabulary vocab = new TableVocabulary();
                vocab.reset(4096);
                TableVocabulary.Report announced = new TableVocabulary.Report();
                boolean ok = TableVocabulary.announceRead(vocab, announcement, announced);
                if (!ok || announced.refused || announced.damaged) {
                    verdict = "refused";
                } else {
                    RootFixed.Value[] values = new RootFixed.Value[TableMessage.batchMax];
                    for (int j = 0; j < values.length; j++) {
                        values[j] = new RootFixed.Value();
                    }
                    TableVocabulary.Report report = new TableVocabulary.Report();
                    int count = RootMessage.loadMessages(values, values.length, vocab, data, report);
                    verdict = (count == 1 && !report.refused && !report.damaged) ? "read" : "refused";
                }
            }
            System.out.println(name + "," + verdict);
        }
    }
}
`

type skippedValueRow struct {
	name    string
	form    string
	verdict string
	path    string
	probe   string
}

func TestJavaSkippedValueIsValidated(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the JDK (it compiles and runs the generated Java)")
	javac, javaBin := javaToolsRequired(t)

	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	vectors := filepath.Join(root, "testdata", "wire", "skipped-value")
	manifest, err := os.ReadFile(filepath.Join(vectors, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []skippedValueRow
	for i, line := range strings.Split(string(manifest), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 3 {
			t.Fatalf("MANIFEST.tsv:%d: a vector line is <file> TAB <form> TAB <verdict>, got %q", i+1, line)
		}
		if f[1] != "json" && f[1] != "message" {
			t.Fatalf("MANIFEST.tsv:%d: form %q, want json or message", i+1, f[1])
		}
		if f[2] != "read" && f[2] != "refused" {
			t.Fatalf("MANIFEST.tsv:%d: verdict %q, want read or refused", i+1, f[2])
		}
		probe := f[1]
		if f[0] == "announcement.bin" {
			probe = "announce"
		}
		rows = append(rows, skippedValueRow{
			name:    f[0],
			form:    f[1],
			verdict: f[2],
			path:    filepath.Join(vectors, f[0]),
			probe:   probe,
		})
	}
	if len(rows) == 0 {
		t.Fatal("MANIFEST.tsv names no vectors")
	}

	u := loadUnitDir(t, root, "test/skipped-value/reader")
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("the table surface refused: %v", err)
	}
	dir := t.TempDir()
	var sources []string
	for name, data := range files {
		if !strings.HasSuffix(name, ".java") {
			continue
		}
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, p)
	}
	probe := filepath.Join(dir, "SkippedValueProbe.java")
	if err := os.WriteFile(probe, []byte(skippedValueProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	classes := filepath.Join(dir, "classes")
	if err := os.Mkdir(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	args := append([]string{"--release", "17", "-d", classes}, sources...)
	args = append(args, probe)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}

	run := []string{"-ea", "-cp", classes, "skippedreader.SkippedValueProbe", filepath.Join(vectors, "announcement.bin")}
	for _, row := range rows {
		run = append(run, row.name, row.probe, row.path)
	}
	out, err := exec.Command(javaBin, run...).CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		name, verdict, ok := strings.Cut(strings.TrimSpace(line), ",")
		if !ok {
			t.Fatalf("driver line %q: want name,verdict", line)
		}
		got[name] = verdict
	}
	if len(got) != len(rows) {
		t.Fatalf("driver answered %d of %d rows:\n%s", len(got), len(rows), out)
	}
	for _, row := range rows {
		answer, ok := got[row.name]
		if !ok {
			t.Errorf("%s: no driver answer", row.name)
			continue
		}
		if answer != row.verdict {
			t.Errorf("%s (%s): Java verdict = %s, want %s", row.name, row.form, answer, row.verdict)
		}
	}
}
