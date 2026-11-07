package javatable

import (
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	javacodegen "github.com/mas-bandwidth/schema/v2/internal/codegen/java"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// A map count that runs past its body (wire fuzzer cause C3, the count pass)
// makes the oracle's LoadMeasure refuse with count_over_length; no Load runs
// (docs/SPEC-TABLES.md §2.9, §6.5). The Java leg used to accept the count and
// load a short map. Each case is the fuzzer's own mutant at seed 24845619678,
// for the affected unit's root, embedded as hex: the Java answer must be the
// oracle's refusal.
func TestJavaFixContainerCountMeasureRefusal(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	for _, c := range []struct {
		name, dir, root, mutant string
	}{
		// mapdemo.Cells, pass count #3: the map count 2 raised to 53
		{"cells", "maps", "Cells", "01010e370d351a020c05616c706861030e0e04030a0000000b0000000c0000000019020c0462657461030e0e0403000000000100000002000000000404070000000038810af11f06a7a3ec105b36194ac93dea0ce83094fde47ce9ea716f0f0182bf0400000000000000"},
		// listdemo.Army, pass count #12: a map count inside a list element, 2 raised to 30
		{"army", "lists", "Army", "01010e580d022a020e200d1e0e030602040d0705041400000000000e030609040d0705045a0000000000060432000000002a020e200d020e030603040d0705041500000000000e030608040d0705045b0000000000060433000000000704080000000026a9020c9b014878424f4f300d39841cec105b36194ac93dea0ce83094fde47c74a279448ee2e5b1861b638ebaadbcc4e9ea716f0f0182bf0700000000000000"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			mutant, err := hex.DecodeString(c.mutant)
			if err != nil {
				t.Fatal(err)
			}
			u := unitFromDir(t, filepath.Join(repo, "tables", c.dir))
			if _, _, refused := tablewire.FileRegionMeasure(u, u.Tables[c.root], mutant); !refused {
				t.Fatalf("test setup: the oracle does not refuse the %s mutant", c.root)
			}
			measure, report, _, loaded := javaCountMeasureVerdict(t, u, c.root, mutant)
			if measure != "-1" || loaded || !strings.HasSuffix(report, ",true") {
				t.Errorf("%s.%s: java loadMeasure=%s report=%s loaded=%t, oracle refuses (count_over_length)", c.dir, c.root, measure, report, loaded)
			}
		})
	}
}

// javaCountMeasureVerdict generates and compiles the unit's Java, then answers
// the root's loadMeasure, the load report (unknown, kindMismatch, widened,
// clamped, duplicate, malformed, refused), the report's reason and whether
// load returned a value.
func javaCountMeasureVerdict(t *testing.T, u *ir.Unit, root string, wire []byte) (measure, report, reason string, loaded bool) {
	t.Helper()
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	if files[root+"Variable.java"] == nil {
		t.Fatalf("%sVariable.java is not emitted", root)
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
	dir := t.TempDir()
	classes := filepath.Join(dir, "classes")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	var sources []string
	for name, data := range files {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, path)
	}
	wp := filepath.Join(dir, "mutant.bin")
	if err := os.WriteFile(wp, wire, 0o644); err != nil {
		t.Fatal(err)
	}
	driver := fmt.Sprintf(`
import java.nio.file.Files;
import java.nio.file.Paths;
public final class Driver {
    public static void main(String[] args) throws Exception {
        byte[] wire = Files.readAllBytes(Paths.get(args[0]));
        System.out.println("M " + %[1]s.%[2]sVariable.loadMeasure(wire));
        %[1]s.TableVariable.Report report = new %[1]s.TableVariable.Report();
        %[1]s.%[2]sVariable.Value v = %[1]s.%[2]sVariable.load(wire, report);
        System.out.println("R " + report.unknown + "," + report.kindMismatch + "," + report.widened + "," + report.clamped + "," + report.duplicate + "," + report.malformed + "," + report.refused);
        System.out.println("N " + report.reason);
        System.out.println(v == null ? "V null" : "V loaded");
    }
}
`, pkg, root)
	dpath := filepath.Join(dir, "Driver.java")
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
	out, err := exec.Command(java, "-ea", "-cp", classes, "Driver", wp).CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
	seen := 0
	for line := range strings.SplitSeq(string(out), "\n") {
		switch {
		case strings.HasPrefix(line, "M "):
			measure, seen = strings.TrimPrefix(line, "M "), seen+1
		case strings.HasPrefix(line, "R "):
			report, seen = strings.TrimPrefix(line, "R "), seen+1
		case strings.HasPrefix(line, "N "):
			reason, seen = strings.TrimPrefix(line, "N "), seen+1
		case strings.HasPrefix(line, "V "):
			loaded, seen = line == "V loaded", seen+1
		}
	}
	if seen != 4 {
		t.Fatalf("driver output:\n%s", out)
	}
	return measure, report, reason, loaded
}

// javaPinSchemas are the units of internal/codegen/goosetable/testdata/
// load_refusal_reasons.tsv, by its schema column.
var javaPinSchemas = map[string]string{
	"measure": "package probe\n table Child { values []int32 }\n table Root { values []int32\n entries map[int32]int32\n child Child\n next *Root }\n",
	"fixed":   "package probe\n fixed table Root { a int32\n flag bool\n small int8 | max = 5\n f float32 }\n",
	"float":   "package probe\n table Root { f float32\n values []int32 }\n",
}

// javaPin is one row of the shared pin table.
type javaPin struct {
	name, schema, strict, form, measure string
	wire                                []byte
}

// javaPinVerdict is what the Java leg answers for one wire: loadMeasure, the
// load report (unknown, kindMismatch, widened, clamped, duplicate, malformed,
// refused), the report's reason and whether load returned a value.
type javaPinVerdict struct {
	measure, report, reason string
	loaded                  bool
}

// javaPinVerdicts compiles the unit's Java once and reads every wire through
// Root's loadMeasure and load.
func javaPinVerdicts(t *testing.T, u *ir.Unit, wires [][]byte) []javaPinVerdict {
	t.Helper()
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	if files["RootVariable.java"] == nil {
		t.Fatal("RootVariable.java is not emitted")
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
	dir := t.TempDir()
	classes := filepath.Join(dir, "classes")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	var sources []string
	for name, data := range files {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, path)
	}
	args := []string{}
	for i, w := range wires {
		wp := filepath.Join(dir, fmt.Sprintf("w%d.bin", i))
		if err := os.WriteFile(wp, w, 0o644); err != nil {
			t.Fatal(err)
		}
		args = append(args, wp)
	}
	driver := fmt.Sprintf(`
import java.nio.file.Files;
import java.nio.file.Paths;
public final class Driver {
    public static void main(String[] args) throws Exception {
        for (String a : args) {
            byte[] wire = Files.readAllBytes(Paths.get(a));
            %[1]s.TableVariable.Report report = new %[1]s.TableVariable.Report();
            %[1]s.RootVariable.Value v = %[1]s.RootVariable.load(wire, report);
            System.out.println("P " + %[1]s.RootVariable.loadMeasure(wire)
                + "|" + report.unknown + "," + report.kindMismatch + "," + report.widened + "," + report.clamped + "," + report.duplicate + "," + report.malformed + "," + report.refused
                + "|" + report.reason + "|" + (v == null ? "null" : "loaded"));
        }
    }
}
`, pkg)
	dpath := filepath.Join(dir, "Driver.java")
	if err := os.WriteFile(dpath, []byte(driver), 0o644); err != nil {
		t.Fatal(err)
	}
	sources = append(sources, dpath)
	sort.Strings(sources)
	javac, java := javaTools(t)
	if out, err := exec.Command(javac, append([]string{"--release", "17", "-d", classes}, sources...)...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	out, err := exec.Command(java, append([]string{"-ea", "-cp", classes, "Driver"}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
	var got []javaPinVerdict
	for line := range strings.SplitSeq(string(out), "\n") {
		if !strings.HasPrefix(line, "P ") {
			continue
		}
		f := strings.Split(strings.TrimPrefix(line, "P "), "|")
		if len(f) != 4 {
			t.Fatalf("driver line %q", line)
		}
		got = append(got, javaPinVerdict{measure: f[0], report: f[1], reason: f[2], loaded: f[3] == "loaded"})
	}
	if len(got) != len(wires) {
		t.Fatalf("%d driver lines for %d wires:\n%s", len(got), len(wires), out)
	}
	return got
}

// javaPinFault answers how the Java leg's answer differs from one pin row, or
// "" when it agrees. The leg has no strict read and no measure reason, so the
// strict column is held through the load (docs/SPEC-TABLES.md §6.5): a row the
// strict read keeps or refuses only for an evolution counter loads with that
// counter moved, damage discards the value with malformed set, and a refusal
// discards it with its name. The measure column is held as its verdict, a size
// or -1, and the form column as the report's reason. A count its body cannot
// cover is the one row class where the load answers the measure rather than
// the strict read: the load refuses wherever loadMeasure answers -1.
func javaPinFault(p javaPin, v javaPinVerdict) string {
	bad := func(want string) string {
		return fmt.Sprintf("measure=%s report=%s reason=%s loaded=%t, want %s", v.measure, v.report, v.reason, v.loaded, want)
	}
	r := strings.Split(v.report, ",")
	if p.measure == "count_over_length" {
		// THE LOAD REFUSES WHERE loadMeasure ANSWERS -1 (TestJavaFixContainer
		// CountMeasureRefusal): a count its body cannot cover is the measure's
		// refusal, and no value lands
		if v.loaded || v.measure != "-1" || r[5] != "false" || r[6] != "true" {
			return bad("no value, refused as loadMeasure refuses, not malformed")
		}
		return ""
	}
	switch p.strict {
	case "ok":
		if !v.loaded || r[5] != "false" || r[6] != "false" {
			return bad("a value")
		}
	case "kind_mismatch", "clamped":
		at := map[string]int{"kind_mismatch": 1, "clamped": 3}[p.strict]
		if !v.loaded || r[at] == "0" || r[5] != "false" {
			return bad("a value with " + p.strict + " counted")
		}
	case "malformed", "wire_damaged":
		if v.loaded || r[5] != "true" {
			return bad("no value, malformed")
		}
	case "unknown_form":
		if v.loaded || r[6] != "true" || r[5] != "false" {
			return bad("no value, refused, not malformed")
		}
	case "non_finite_float":
		if v.loaded || !strings.HasPrefix(v.reason, "non_finite_float") {
			return bad("no value, non_finite_float")
		}
	case "count_over_extent_cap":
		// the refusal moves no counter and is not damage (§2.9)
		if v.loaded || v.report != "0,0,0,0,0,false,true" || v.reason != p.strict {
			return bad("no value, 0,0,0,0,0,false,true, count_over_extent_cap")
		}
	default:
		return "a strict answer this test does not hold: " + p.strict
	}
	if p.form != "-" && v.reason != p.form {
		return bad("the form reason " + p.form)
	}
	if p.measure != "-" && (p.measure == "ok") != (v.measure != "-1") {
		return bad("the measure verdict " + p.measure)
	}
	return ""
}

// TestJavaLoadRefusalPins holds the Java leg to every row of
// internal/codegen/goosetable/testdata/load_refusal_reasons.tsv, the table Go,
// Goose and MOO are held to (docs/SPEC-TABLES.md §2.9, §6.5): each row's
// strict answer through the load, its form refusal, and its measure verdict.
// A `[]` or map count above the int32 storage cap answers no value, refused by
// the name count_over_extent_cap, malformed not set and no counter moved, a
// widening element kind included; of two refusals in one read, the first in
// read order names it.
func TestJavaLoadRefusalPins(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../goosetable/testdata/load_refusal_reasons.tsv")
	if err != nil {
		t.Fatal(err)
	}
	bySchema := map[string][]javaPin{}
	rows := 0
	for line := range strings.SplitSeq(string(data), "\n") {
		if line == "" || line[0] == '#' {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 6 || javaPinSchemas[f[1]] == "" {
			t.Fatalf("a malformed pin row: %q", line)
		}
		var wire []byte
		if f[2] != "-" {
			if wire, err = hex.DecodeString(f[2]); err != nil {
				t.Fatal(err)
			}
		}
		bySchema[f[1]] = append(bySchema[f[1]], javaPin{name: f[0], schema: f[1], strict: f[3], form: f[4], measure: f[5], wire: wire})
		rows++
	}
	if len(bySchema) != len(javaPinSchemas) {
		t.Fatalf("the pin table names %d schemas, want %d", len(bySchema), len(javaPinSchemas))
	}
	for schema, pins := range bySchema {
		t.Run(schema, func(t *testing.T) {
			t.Parallel()
			wires := make([][]byte, len(pins))
			for i, p := range pins {
				wires[i] = p.wire
			}
			got := javaPinVerdicts(t, unitFrom(t, javaPinSchemas[schema]), wires)
			for i, p := range pins {
				if fault := javaPinFault(p, got[i]); fault != "" {
					t.Errorf("%s: %s", p.name, fault)
				}
			}
		})
	}
	t.Logf("%d pin rows", rows)
}
