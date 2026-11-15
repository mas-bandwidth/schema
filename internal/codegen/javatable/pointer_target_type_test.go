package javatable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	javacodegen "github.com/mas-bandwidth/schema/v2/internal/codegen/java"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestJavaPointerTargetTypeChecked holds the Java leg to docs/SPEC-TABLES.md
// §3.1's rule for a pointer's target type (docs/rulings.json
// pointer-target-type-checked) over the shared vectors in
// testdata/wire/pointer-target-type: in the variable form, a pointer slot
// naming a record of a type this reader DECLARES that is not the type the slot
// requires is damage and refuses the whole load; a record of a type the reader
// cannot name at all is counted unknown once and every pointer naming it reads
// null. Each manifest row runs through every variable-form read that answers a
// verdict: load and the retaining loadRetain; loadMeasure, which answers from
// the framing (§6.5), must size every lawful row. Only the verdict is
// compared, never a refusal's name (docs/rulings.json refusal-verdict-only).
func TestJavaPointerTargetTypeChecked(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "javac and java (it builds and runs the generated Java over the shared vectors)")

	repo := filepath.Join("..", "..", "..")
	dir, err := filepath.Abs(filepath.Join(repo, "testdata", "wire", "pointer-target-type"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile(filepath.Join(dir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatalf("vectors manifest: %v", err)
	}
	type row struct {
		name, unit, root, file, verdict string
		unknown                         int
	}
	var rows []row
	for line := range strings.SplitSeq(string(manifest), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 6 {
			t.Fatalf("manifest line %q: %d columns, want 6", line, len(f))
		}
		r := row{name: f[0], unit: f[1], root: f[2], file: f[3], verdict: f[4], unknown: -1}
		if r.verdict != "read" && r.verdict != "refused" {
			t.Fatalf("manifest row %s: verdict %q", r.name, r.verdict)
		}
		if f[5] != "-" {
			if r.unknown, err = strconv.Atoi(f[5]); err != nil {
				t.Fatalf("manifest row %s: unknown %q", r.name, f[5])
			}
		}
		rows = append(rows, r)
	}
	if len(rows) == 0 {
		t.Fatal("the manifest names no vectors")
	}
	unit := rows[0].unit
	for _, r := range rows {
		if r.unit != unit || r.root != "Node" {
			t.Fatalf("row %s reads %s at %s; this test reads every row at Node in %s", r.name, r.unit, r.root, unit)
		}
	}

	javac, java := javaTools(t)
	u := unitFromDir(t, filepath.Join(repo, unit))
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	packet, err := javacodegen.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Int128.java", "UInt128.java"} {
		if data := packet[name]; data != nil {
			files[name] = data
		}
	}
	pkg := javaPackageOf(u)
	build := t.TempDir()
	classes := filepath.Join(build, "classes")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	var sources []string
	for name, data := range files {
		p := filepath.Join(build, name)
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, p)
	}
	driver := fmt.Sprintf(`import %s.*;
import java.nio.file.Files;
import java.nio.file.Paths;

public final class PointerTargetDriver {
    private PointerTargetDriver() {}

    // args: name wire-path want(read|refused) unknown(-1 when not pinned) nullNext(true|false)
    public static void main(String[] args) throws Exception {
        String name = args[0];
        byte[] wire = Files.readAllBytes(Paths.get(args[1]));
        boolean want = args[2].equals("read");
        int unknown = Integer.parseInt(args[3]);
        boolean nullNext = Boolean.parseBoolean(args[4]);
        int bad = 0;

        TableVariable.Report report = new TableVariable.Report();
        NodeVariable.Value v = NodeVariable.load(wire, report);
        if ((v != null) != want) {
            System.out.println(name + ": load: value=" + (v != null) + " damaged=" + report.damaged + " refused=" + report.refused);
            bad++;
        } else if (want && unknown >= 0 && report.unknown != unknown) {
            System.out.println(name + ": load: unknown=" + report.unknown + ", want " + unknown);
            bad++;
        } else if (want && nullNext && v.next != null) {
            System.out.println(name + ": load: next is not null");
            bad++;
        }

        TableRetain retain = new TableRetain(1 << 16, 1024);
        TableVariable.Report rr = new TableVariable.Report();
        NodeVariable.Value kept = NodeRetain.loadRetain(wire, retain, rr);
        if ((kept != null) != want) {
            System.out.println(name + ": loadRetain: value=" + (kept != null) + " damaged=" + rr.damaged + " refused=" + rr.refused);
            bad++;
        } else if (want && unknown >= 0 && rr.unknown != unknown) {
            System.out.println(name + ": loadRetain: unknown=" + rr.unknown + ", want " + unknown);
            bad++;
        } else if (want && nullNext && kept.next != null) {
            System.out.println(name + ": loadRetain: next is not null");
            bad++;
        } else if (!want && (retain.used != 0 || retain.idUsed != 0)) {
            System.out.println(name + ": loadRetain: a refused load kept a tail");
            bad++;
        }

        // loadMeasure answers from the framing alone (§6.5): a pointer's
        // target type is not framing, so a lawful file must measure, and a
        // wrong-type file is refused by the loads above, not by the measure
        long measure = NodeVariable.loadMeasure(wire);
        if (want && measure < 0) {
            System.out.println(name + ": loadMeasure: " + measure);
            bad++;
        }

        if (bad == 0) {
            System.out.println(name + ": " + (want ? "read" : "refused") + " on every read");
        }
    }
}
`, pkg)
	driverPath := filepath.Join(build, "PointerTargetDriver.java")
	if err := os.WriteFile(driverPath, []byte(driver), 0o644); err != nil {
		t.Fatal(err)
	}
	sources = append(sources, driverPath)
	args := append([]string{"--release", "17", "-d", classes}, sources...)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}

	for _, r := range rows {
		// the writer-only row is the one whose next names a record this reader
		// cannot name: its pointer must read null
		nullNext := r.unknown > 0
		out, err := exec.Command(java, "-ea", "-cp", classes, "PointerTargetDriver",
			r.name, filepath.Join(dir, r.file), r.verdict, strconv.Itoa(r.unknown), strconv.FormatBool(nullNext)).CombinedOutput()
		if err != nil {
			t.Errorf("%s: java: %v\n%s", r.name, err, out)
			continue
		}
		text := strings.TrimSpace(string(out))
		if !strings.HasSuffix(text, r.name+": "+r.verdict+" on every read") {
			t.Errorf("%s\n", text)
			continue
		}
		t.Logf("%s", text)
	}
}
