package javatable

// J14: a LIST COUNT INSIDE A MAP VALUE RUNS PAST ITS BODY.
//
// The Go oracle's region measure walks every container header a known field
// carries and refuses a count whose elements cannot fit that field's own L
// (internal/tablewire/extent.go, docs/SPEC-TABLES.md §6.5). On mapdemo.Spans
// the map value is an unbounded []Item, so the count lives inside the entry
// body. The random fuzz pass at seed 24845619678 mutates that count past the
// list body AND desynchronizes the map KEY scan; the old Java load stopped at
// the key damage, reported malformed, and returned a value whose LoadMeasure
// refused. The load must refuse exactly where the oracle's framing refuses
// (§3.3, §6.5), even when the key scan stops first.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	javacodegen "github.com/mas-bandwidth/schema/v2/internal/codegen/java"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// The first mutant is the fuzzer's random #311 at seed 24845619678, an
// insertion that drives the map value's list count past its body and
// desynchronizes the key scan.
const listCountPastBodyMutant = "01010e3e0d0221020601030e1a0d354b40fa06d491030704040a000000000704040b000000000704040c000000000019020608030e120d02070404500000000007040451000000000005040400000000bd0f479c60325375ec105b36194ac93dea0ce83094fde47c74a279448ee2e5b1e9ea716f0f0182bf0500000000000000"

// The second mutant (random #556 at seed 24845619678) is the same cause in an
// INTACT entry: only the list count is driven past its body, the key scan does
// not stop, and the Java load previously loaded it where the oracle refused.
// It is the clean map_spans wire with the first entry's tracks-list count (byte
// 14) raised from 3 to 255 (a 0x12 body carried three elements, so 255 cannot
// fit; the oracle refuses).
const listCountPastBodyMutantIntact = "01010e3e0d0221020601030e1a0dff0704040a000000000704040b000000000704040c000000000019020608030e120d02070404500000000007040451000000000005040400000000bd0f479c60325375ec105b36194ac93dea0ce83094fde47c74a279448ee2e5b1e9ea716f0f0182bf0500000000000000"

func TestJavaFixListCountPastBodyInMapValue(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	u := unitFromDir(t, filepath.Join(repo, "tables", "maps"))
	root := u.Tables["Spans"]
	if root == nil {
		t.Fatal("mapdemo declares no Spans")
	}
	clean, err := os.ReadFile(filepath.Join(repo, "testdata", "wire", "tables", "map_spans.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, refused := tablewire.FileRegionMeasure(u, root, clean); refused {
		t.Fatal("the oracle refuses the clean map_spans wire")
	}
	if measure, loaded := javaListCountVerdict(t, u, "Spans", clean); measure < 0 || !loaded {
		t.Fatalf("Java cannot carry the clean Spans wire: measure=%d loaded=%t", measure, loaded)
	}
	for _, tc := range []struct {
		name, hexMutant, sha string
	}{
		{"keyscan-desync", listCountPastBodyMutant, "d57d43710dec3bc9b77aa4298d01763c07592afa84a8140e8004bd8e85f9d9c0"},
		{"intact-entry", listCountPastBodyMutantIntact, "8d172fc24a366c769e52ce987562475494cdbcfa0839014a6837337a057b973c"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mutant, err := hex.DecodeString(tc.hexMutant)
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(mutant)
			if got := hex.EncodeToString(sum[:]); got != tc.sha {
				t.Fatalf("mutant sha256 = %s, want %s", got, tc.sha)
			}
			if _, _, refused := tablewire.FileRegionMeasure(u, root, mutant); !refused {
				t.Fatal("the oracle's region measure did not refuse this mutant: the bytes are not of this cause")
			}
			measure, loaded := javaListCountVerdict(t, u, "Spans", mutant)
			if measure >= 0 {
				t.Fatalf("Java LoadMeasure = %d, want a refusal (the oracle refused the framing)", measure)
			}
			if loaded {
				t.Fatal("Java load accepted a list count past its body inside a map value (the oracle refused)")
			}
		})
	}
}

// javaListCountVerdict compiles the generated Java for the unit and answers
// LoadMeasure's result and whether Load returned a value.
func javaListCountVerdict(t *testing.T, u *ir.Unit, root string, data []byte) (measure int64, loaded bool) {
	t.Helper()
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	packet, err := javacodegen.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Int128.java", "UInt128.java"} {
		if d := packet[name]; d != nil {
			files[name] = d
		}
	}
	pkg := javaPackageOf(u)
	dir := t.TempDir()
	classes := filepath.Join(dir, "classes")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	var sources []string
	for name, d := range files {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, d, 0o644); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, p)
	}
	wirePath := filepath.Join(dir, "wire.bin")
	if err := os.WriteFile(wirePath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	driverName := "ListCountDriver_" + root
	driver := filepath.Join(dir, driverName+".java")
	if err := os.WriteFile(driver, []byte(strings.ReplaceAll(listCountDriver(pkg, root), "Driver", driverName)), 0o644); err != nil {
		t.Fatal(err)
	}
	sources = append(sources, driver)
	sort.Strings(sources)
	javac, java := javaTools(t)
	args := append([]string{"--release", "17", "-d", classes}, sources...)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	out, err := exec.Command(java, "-ea", "-cp", classes, driverName, wirePath).CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
	foundMeasure := false
	for _, line := range strings.Split(string(out), "\n") {
		switch {
		case strings.HasPrefix(line, "measure="):
			n, perr := strconv.ParseInt(strings.TrimSpace(strings.TrimPrefix(line, "measure=")), 10, 64)
			if perr != nil {
				t.Fatalf("driver measure %q: %v", line, perr)
			}
			measure, foundMeasure = n, true
		case strings.HasPrefix(line, "loaded="):
			loaded = strings.TrimSpace(strings.TrimPrefix(line, "loaded=")) == "true"
		}
	}
	if !foundMeasure {
		t.Fatalf("driver output %q: no measure line", out)
	}
	return measure, loaded
}

func listCountDriver(pkg, root string) string {
	return fmt.Sprintf("import java.nio.file.Files;\n"+
		"import java.nio.file.Paths;\n\n"+
		"public final class Driver {\n"+
		"    private Driver() {}\n\n"+
		"    public static void main(String[] args) throws Exception {\n"+
		"        byte[] wire = Files.readAllBytes(Paths.get(args[0]));\n"+
		"        long m = %s.%sVariable.loadMeasure(wire);\n"+
		"        %s.TableVariable.Report report = new %s.TableVariable.Report();\n"+
		"        %s.%sVariable.Value v = %s.%sVariable.load(wire, report);\n"+
		"        System.out.println(\"measure=\" + m);\n"+
		"        System.out.println(\"loaded=\" + (v != null));\n"+
		"    }\n"+
		"}\n", pkg, root, pkg, pkg, pkg, root, pkg, root)
}
