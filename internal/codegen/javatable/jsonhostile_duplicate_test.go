package javatable

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// jsonHostileDuplicateProbe reads one row per argument triple (name, root,
// file) through the generated fromJson and prints the one counter this row
// is about beside the verdict, in the report spelling the conformance surface
// uses (docs/SPEC-TABLES.md §16.2).
const jsonHostileDuplicateProbe = `package tabledemo;
public final class DuplicateProbe {
    private DuplicateProbe() {}
    public static void main(String[] args) throws Exception {
        for (int i = 0; i + 2 < args.length; i += 3) {
            String name = args[i];
            String root = args[i + 1];
            String file = args[i + 2];
            TableVariable.Report report = new TableVariable.Report();
            byte[] text = java.nio.file.Files.readAllBytes(java.nio.file.Paths.get(file));
            Class<?> codec = Class.forName("tabledemo." + root + "Variable");
            Object value = codec.getMethod("fromJson", byte[].class, TableVariable.Report.class)
                    .invoke(null, text, report);
            String verdict = (value == null || report.malformed) ? "refused" : "read";
            System.out.println(name + "," + report.duplicate + "," + verdict);
        }
    }
}
`

type duplicateRow struct {
	name    string
	root    string
	text    string
	want    int
	verdict string
}

// TestJsonHostileReportDuplicateCounterJava is the card's row: for every
// json-hostile case whose Go oracle counts `duplicate`, the Java text reader
// must count the same number and reach the same verdict (docs/SPEC-TABLES.md
// §16.2, §4). One subtest per row, so a divergence names the case.
func TestJsonHostileReportDuplicateCounterJava(t *testing.T) {
	t.Parallel()
	javac, javaBin := javaToolsRequired(t)
	repo := filepath.Join("..", "..", "..")
	u := unitFromDir(t, filepath.Join(repo, "tables", "examples"))
	model := tabletext.NewModel(u)

	manifest := filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt")
	file, err := os.Open(manifest)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	var rows []duplicateRow
	scan := bufio.NewScanner(file)
	scan.Buffer(make([]byte, 1<<20), 1<<20)
	for scan.Scan() {
		fields := strings.Fields(strings.TrimSpace(scan.Text()))
		if len(fields) != 6 || fields[0] != "json-hostile" {
			continue
		}
		if fields[5] == "refused" {
			continue
		}
		parts := strings.Split(fields[5], ",")
		if len(parts) != 7 {
			t.Fatalf("%s: expected a report, got %q", fields[1], fields[5])
		}
		expected, err := strconv.Atoi(parts[4])
		if err != nil {
			t.Fatalf("%s: duplicate count %q: %v", fields[1], parts[4], err)
		}
		if expected == 0 {
			continue
		}
		// this harness generates tabledemo only; a duplicate row of another
		// unit (list-map-dup-key, listdemo) is held by the conformance
		// harness's json-hostile surface, which every leg runs
		if fields[2] != u.Package {
			continue
		}
		st := model.Lookup(fields[3])
		if st == nil {
			t.Fatalf("%s: no table %q in %s", fields[1], fields[3], u.Package)
		}
		path := filepath.Join(repo, fields[4], fields[3]+".json")
		text, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var report tabletext.Report
		ok := model.Read(model.New(st), text, &report)
		verdict := "read"
		if !ok || report.Malformed {
			verdict = "refused"
		}
		if report.Duplicate == 0 {
			t.Fatalf("%s: the manifest counts duplicate but the oracle counts none: %+v", fields[1], report)
		}
		rows = append(rows, duplicateRow{name: fields[1], root: fields[3], text: string(text), want: report.Duplicate, verdict: verdict})
	}
	if err := scan.Err(); err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("the manifest names no json-hostile row whose oracle counts duplicate")
	}

	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	files["DuplicateProbe.java"] = []byte(jsonHostileDuplicateProbe)
	var sources []string
	for name, data := range files {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, ".java") {
			sources = append(sources, p)
		}
	}
	classes := filepath.Join(dir, "classes")
	if err := os.Mkdir(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	args := append([]string{"--release", "17", "-d", classes}, sources...)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}

	args = nil
	for i, row := range rows {
		p := filepath.Join(dir, fmt.Sprintf("row-%d.json", i))
		if err := os.WriteFile(p, []byte(row.text), 0o644); err != nil {
			t.Fatal(err)
		}
		args = append(args, row.name, row.root, p)
	}
	cmd := exec.Command(javaBin, "-ea", "-cp", classes, "tabledemo.DuplicateProbe")
	cmd.Args = append(cmd.Args, args...)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Split(strings.TrimSpace(line), ",")
		if len(fields) != 3 {
			t.Fatalf("driver line %q: want name,duplicate,verdict", line)
		}
		got[fields[0]] = fields[1] + "," + fields[2]
	}
	if len(got) != len(rows) {
		t.Fatalf("driver answered %d of %d rows:\n%s", len(got), len(rows), out)
	}

	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			// docs/SPEC-TABLES.md §16.2: a repeated key is last-wins and
			// counted `duplicate`, and §4's report is the same event here as
			// it is for the Go walk.
			want := fmt.Sprintf("%d,%s", row.want, row.verdict)
			if answer := got[row.name]; answer != want {
				t.Fatalf("%s: Java report duplicate,verdict = %s, oracle = %s", row.name, answer, want)
			}
		})
	}
}
