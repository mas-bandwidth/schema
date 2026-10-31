package javatable

import (
	"bytes"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestJavaOptionalVariableClosureG1 holds docs/SPEC-TABLES.md §2.3 and §3.1
// (schema#526) on the Java leg over test/tables/G1.schema, the unit the
// conformance rows tblg1.Guarded ride: `optional ?Wrapped` is an optional over
// a VARIABLE closure, and an absent optional's table is not descended — the
// numbering, the pack measure and the pack gate on the presence companion.
//
// The probe loads optional_present_edge.bin, clears the presence companion and
// leaves the stale pointer inside the optional's table, the case a load can
// never produce and so the case the conformance row cannot reach. Saved, it
// must be optional_absent_no_edge.bin byte for byte; written as text, the
// absent load's text; cooked, the absent load's cook, both byte orders.
//
// THE REVERSED WITNESS plants the old walk-every-slot behaviour back into the
// generated Java: the variable form's buildGraph without its presence gate,
// and the cook plan's absentRecord answering false. Each planted walk must
// turn its row red, so a regression that descends an absent optional is
// caught here.
func TestJavaOptionalVariableClosureG1(t *testing.T) {
	slowtest.Gate(t, "the JDK (it compiles and runs the generated Java)")
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	dir := t.TempDir()
	src, err := os.ReadFile(filepath.Join(repo, "test", "tables", "G1.schema"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "G1.schema"), src, 0o644); err != nil {
		t.Fatal(err)
	}
	u := unitFromDir(t, dir)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("java refused G1: %v", err)
	}
	const variableGate = "        if (v.optionalPresent) {\n" +
		"            if (v.optional != null) {\n" +
		"                if (!WrappedVariable.buildGraph(g, v.optional)) return false;\n" +
		"            }\n" +
		"        }\n"
	const variableWalkAll = "        if (v.optional != null) {\n" +
		"            if (!WrappedVariable.buildGraph(g, v.optional)) return false;\n" +
		"        }\n"
	const cookGate = "        return f.presentOffset >= 0\n" +
		"                && f.storage == TableCookStorage.RECORD\n" +
		"                && !Boolean.TRUE.equals(field(value, f.sourceName + \"Present\"));\n"
	if !bytes.Contains(files["GuardedVariable.java"], []byte(variableGate)) {
		t.Fatalf("GuardedVariable.buildGraph carries no presence gate:\n%s", files["GuardedVariable.java"])
	}
	if !bytes.Contains(files["TableCookWrite.java"], []byte(cookGate)) {
		t.Fatal("TableCookWrite carries no absent-record gate")
	}

	absent, err := filepath.Abs(filepath.Join(repo, "testdata", "wire", "tables", "optional_absent_no_edge.bin"))
	if err != nil {
		t.Fatal(err)
	}
	present, err := filepath.Abs(filepath.Join(repo, "testdata", "wire", "tables", "optional_present_edge.bin"))
	if err != nil {
		t.Fatal(err)
	}
	javac, javaBin := javaTools(t)
	run := func(name string, files map[string][]byte) string {
		t.Helper()
		build := filepath.Join(dir, name)
		classes := filepath.Join(build, "classes")
		if err := os.MkdirAll(classes, 0o755); err != nil {
			t.Fatal(err)
		}
		var sources []string
		for file, data := range files {
			path := filepath.Join(build, file)
			if err := os.WriteFile(path, data, 0o644); err != nil {
				t.Fatal(err)
			}
			sources = append(sources, path)
		}
		driver := filepath.Join(build, "OptionalG1Driver.java")
		if err := os.WriteFile(driver, []byte(optionalG1Driver), 0o644); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, driver)
		if out, err := exec.Command(javac, append([]string{"--release", "17", "-nowarn", "-d", classes}, sources...)...).CombinedOutput(); err != nil {
			t.Fatalf("%s: javac: %v\n%s", name, err, out)
		}
		out, err := exec.Command(javaBin, "-ea", "-cp", classes, "OptionalG1Driver", absent, present).CombinedOutput()
		if err != nil {
			t.Fatalf("%s: java: %v\n%s", name, err, out)
		}
		return string(out)
	}

	const want = "present-roundtrip OK\nstale-held OK\nstale-save OK\nstale-json OK\nstale-cook-le OK\nstale-cook-be OK\n"
	if got := run("carried", files); got != want {
		t.Fatalf("the Java leg over G1's absent optional (§2.3, §3.1):\n%s\nwant:\n%s", got, want)
	}

	// THE REVERSED WITNESS, the variable form: buildGraph walks every slot
	plant := func(file, from, to string) map[string][]byte {
		t.Helper()
		out := make(map[string][]byte, len(files))
		maps.Copy(out, files)
		out[file] = bytes.Replace(files[file], []byte(from), []byte(to), 1)
		return out
	}
	walked := run("planted-variable", plant("GuardedVariable.java", variableGate, variableWalkAll))
	if !strings.Contains(walked, "present-roundtrip OK\n") || !strings.Contains(walked, "stale-held OK\n") {
		t.Fatalf("the planted variable form did not answer the present case, so the witness proves nothing:\n%s", walked)
	}
	for _, row := range []string{"stale-save"} {
		if strings.Contains(walked, row+" OK\n") {
			t.Errorf("%s: the variable form with its presence gate removed still wrote the absent wire — the gate is not what this test holds\n%s", row, walked)
		}
	}
	// THE REVERSED WITNESS, the cook: the plan numbers an absent record
	cooked := run("planted-cook", plant("TableCookWrite.java", cookGate, "        return false;\n"))
	if !strings.Contains(cooked, "stale-save OK\n") {
		t.Fatalf("the planted cook moved the variable form, so the witness proves nothing:\n%s", cooked)
	}
	for _, row := range []string{"stale-cook-le", "stale-cook-be"} {
		if strings.Contains(cooked, row+" OK\n") {
			t.Errorf("%s: the cook with its presence gate removed still matched the absent cook — the gate is not what this test holds\n%s", row, cooked)
		}
	}
}

const optionalG1Driver = `import java.nio.file.Files;
import java.nio.file.Paths;
import java.util.Arrays;

public final class OptionalG1Driver {
    private OptionalG1Driver() {}

    private static void say(String row, boolean ok) {
        System.out.println(row + (ok ? " OK" : " DIFF"));
    }

    private static byte[] cook(Object v, int order) {
        long size = tblg1.GuardedCook.cookMeasure(v);
        if (size <= 0) return null;
        byte[] out = new byte[(int) size];
        return tblg1.GuardedCook.cookFrom(v, out, order) ? out : null;
    }

    public static void main(String[] args) throws Exception {
        byte[] absentWire = Files.readAllBytes(Paths.get(args[0]));
        byte[] presentWire = Files.readAllBytes(Paths.get(args[1]));
        tblg1.GuardedVariable.Value absent = tblg1.GuardedVariable.load(absentWire, new tblg1.TableVariable.Report());
        tblg1.GuardedVariable.Value stale = tblg1.GuardedVariable.load(presentWire, new tblg1.TableVariable.Report());
        say("present-roundtrip", Arrays.equals(tblg1.GuardedVariable.save(stale), presentWire));
        // the presence companion cleared, the pointer left in the table
        stale.optionalPresent = false;
        say("stale-held", stale.optional != null && stale.optional.vertex != null);
        say("stale-save", Arrays.equals(tblg1.GuardedVariable.save(stale), absentWire));
        say("stale-json", Arrays.equals(tblg1.GuardedVariable.toJson(stale), tblg1.GuardedVariable.toJson(absent)));
        byte[] le = cook(absent, tblg1.TableCookWrite.LITTLE);
        byte[] be = cook(absent, tblg1.TableCookWrite.BIG);
        say("stale-cook-le", le != null && Arrays.equals(cook(stale, tblg1.TableCookWrite.LITTLE), le));
        say("stale-cook-be", be != null && Arrays.equals(cook(stale, tblg1.TableCookWrite.BIG), be));
    }
}
`
