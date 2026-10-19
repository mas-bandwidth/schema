package javatable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// TestJsonHostileReportClampCounterJava pins the Java text reader's `clamped`
// counter to the Go oracle (internal/tabletext's Report) on every json-hostile
// row of testdata/conformance/tables/MANIFEST.txt whose oracle report counts a
// clamp (docs/SPEC-TABLES.md §16.2, §16.3, §17.5). The Java report's clamped
// count and the verdict (read vs refused) must equal the oracle's, one subtest
// per row.
func TestJsonHostileReportClampCounterJava(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	u := unitFromDir(t, filepath.Join(repo, "tables", "examples"))
	root := "RootConfig"
	if u.Tables[root] == nil {
		t.Fatalf("unit %s has no table %s", u.Package, root)
	}

	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	pkg := javaPackageOf(u)
	if files[root+"Variable.java"] == nil {
		t.Fatalf("%s is not emitted", root+"Variable.java")
	}

	manifest, err := os.ReadFile(filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}

	type row struct {
		name string
		tree string
		text []byte
		want tabletext.Report
		ok   bool
	}
	var rows []row
	model := tabletext.NewModel(u)
	for _, line := range strings.Split(string(manifest), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 6 || fields[0] != "json-hostile" || fields[2] != "tabledemo" || fields[3] != root {
			continue
		}
		p := filepath.Join(repo, fields[4], root+".json")
		text, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("row %s: read %s: %v", fields[1], p, err)
		}
		inst := model.New(u.Tables[root])
		rep := new(tabletext.Report)
		ok := model.Read(inst, text, rep)
		if rep.Clamped == 0 {
			continue
		}
		rows = append(rows, row{fields[1], fields[4], text, *rep, ok})
	}
	if len(rows) == 0 {
		t.Fatal("manifest has no json-hostile row the oracle counts a clamp on")
	}

	classes := filepath.Join(t.TempDir(), "classes")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	var sources []string
	for name, data := range files {
		if !strings.HasSuffix(name, ".java") {
			continue
		}
		p := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, p)
	}
	driver := fmt.Sprintf(`
import java.nio.file.Files;
import java.nio.file.Paths;
public final class Driver {
    public static void main(String[] args) throws Exception {
        byte[] text = Files.readAllBytes(Paths.get(args[0]));
        %s.TableVariable.Report report = new %s.TableVariable.Report();
        %s.%sVariable.Value v = %s.%sVariable.fromJson(text, report);
        System.out.println("R " + report.unknown + "," + report.kindMismatch + "," + report.widened + "," + report.clamped + "," + report.duplicate + "," + report.malformed + "," + report.refused);
        if (v == null) System.out.println("V null"); else System.out.println("V ok");
    }
}
`, pkg, pkg, pkg, root, pkg, root)
	dpath := filepath.Join(t.TempDir(), "Driver.java")
	if err := os.WriteFile(dpath, []byte(driver), 0o644); err != nil {
		t.Fatal(err)
	}
	sources = append(sources, dpath)
	sort.Strings(sources)
	javac, java := javaTools(t)
	args := append([]string{"--release", "17", "-d", classes}, sources...)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}

	for _, r := range rows {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			tp := filepath.Join(t.TempDir(), "row.json")
			if err := os.WriteFile(tp, r.text, 0o644); err != nil {
				t.Fatal(err)
			}
			out, err := exec.Command(java, "-ea", "-cp", classes, "Driver", tp).CombinedOutput()
			if err != nil {
				t.Fatalf("java: %v\n%s", err, out)
			}
			var javaFields []string
			for _, line := range strings.Split(string(out), "\n") {
				if strings.HasPrefix(line, "R ") {
					javaFields = strings.Split(strings.TrimPrefix(line, "R "), ",")
				}
			}
			if len(javaFields) != 7 {
				t.Fatalf("java report shape for %s: %q\n%s", r.name, strings.Join(javaFields, ","), out)
			}
			gotClamped := javaFields[3]
			gotRefused := javaFields[6]
			wantClamped := fmt.Sprintf("%d", r.want.Clamped)
			wantRefused := fmt.Sprintf("%t", r.want.Refused)
			if gotClamped != wantClamped {
				t.Errorf("row %s: java clamped=%s oracle clamped=%s (java %q, oracle %+v)", r.name, gotClamped, wantClamped, strings.Join(javaFields, ","), r.want)
			}
			if gotRefused != wantRefused {
				t.Errorf("row %s: java refused=%s oracle refused=%s (java %q, oracle %+v)", r.name, gotRefused, wantRefused, strings.Join(javaFields, ","), r.want)
			}
		})
	}
}
