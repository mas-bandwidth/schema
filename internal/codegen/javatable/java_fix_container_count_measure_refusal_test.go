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
			measure, report, loaded := javaCountMeasureVerdict(t, u, c.root, mutant)
			if measure != "-1" || loaded || !strings.HasSuffix(report, ",true") {
				t.Errorf("%s.%s: java loadMeasure=%s report=%s loaded=%t, oracle refuses (count_over_length)", c.dir, c.root, measure, report, loaded)
			}
		})
	}
}

// javaCountMeasureVerdict generates and compiles the unit's Java, then answers
// the root's loadMeasure, the load report (unknown, kindMismatch, widened,
// clamped, duplicate, malformed, refused) and whether load returned a value.
func javaCountMeasureVerdict(t *testing.T, u *ir.Unit, root string, wire []byte) (measure, report string, loaded bool) {
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
		case strings.HasPrefix(line, "V "):
			loaded, seen = line == "V loaded", seen+1
		}
	}
	if seen != 3 {
		t.Fatalf("driver output:\n%s", out)
	}
	return measure, report, loaded
}
