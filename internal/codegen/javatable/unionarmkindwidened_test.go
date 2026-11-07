package javatable

import (
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	javacodegen "github.com/mas-bandwidth/schema/v2/internal/codegen/java"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// A union arm whose wire kind is changed from 4 to 2: the arm is declared
// int64 (kind 5) and arrives as kind 2 whose L is the declared width, not the
// incoming kind's. Both engines report malformed; the oracle counts widened=0
// (SPEC-TABLES §4: a widening arm's length must be the incoming kind's width)
// and the Java reader must answer the same.
func TestJavaFixUnionArmKindWidened(t *testing.T) {
	t.Parallel()
	hexMutant := "01010801000000020f030208cb04fb711f01000004080200000000c03a5cb5072eb708ea0ce83094fde47c8cec01864cdc63af23d79807ef564cd90400000000000000"
	mutant, err := hex.DecodeString(hexMutant)
	if err != nil {
		t.Fatal(err)
	}

	repo := filepath.Join("..", "..", "..")
	dir := t.TempDir()
	schema, err := os.ReadFile(filepath.Join(repo, "test", "tables", "A2.schema"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "A2.schema"), schema, 0o644); err != nil {
		t.Fatal(err)
	}
	u := unitFromDir(t, dir)
	pkg := javaPackageOf(u)

	model := tabletext.NewModel(u)
	oracle := new(tabletext.Report)
	oval := model.New(u.Tables["Root"])
	if _, err := tablewire.Decode(model, oval, mutant, oracle); err != nil {
		t.Fatal(err)
	}
	if !oracle.Malformed || oracle.Widened != 0 || oracle.KindMismatch != 0 {
		t.Fatalf("oracle report is not the widening-off-width case: %+v", oracle)
	}

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
	classes := filepath.Join(dir, "classes")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	sources := []string{}
	for name, data := range files {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, p)
	}
	driver := filepath.Join(dir, "Driver.java")
	if err := os.WriteFile(driver, []byte(unionKindWidenedDriver(pkg)), 0o644); err != nil {
		t.Fatal(err)
	}
	sources = append(sources, driver)
	javac, java := javaTools(t)
	args := append([]string{"--release", "17", "-d", classes}, sources...)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	wirePath := filepath.Join(dir, "mutant.bin")
	if err := os.WriteFile(wirePath, mutant, 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(java, "-ea", "-cp", classes, "Driver", wirePath).CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
	report := string(out)
	t.Logf("java report: %s", strings.TrimSpace(report))
	for _, want := range []string{"malformed=true", "widened=0", "kindMismatch=0", "refused=false", "unknown=0"} {
		if !strings.Contains(report, want) {
			t.Errorf("java report lacks %s: %s", want, report)
		}
	}
}

func unionKindWidenedDriver(pkg string) string {
	return `import java.nio.file.Files;
import java.nio.file.Paths;

public final class Driver {
    private Driver() {}
    public static void main(String[] args) throws Exception {
        byte[] wire = Files.readAllBytes(Paths.get(args[0]));
        ` + pkg + `.TableVariable.Report report = new ` + pkg + `.TableVariable.Report();
        ` + pkg + `.RootVariable.Value v = ` + pkg + `.RootVariable.load(wire, report);
        System.out.println("REPORT malformed=" + report.malformed + " refused=" + report.refused + " unknown=" + report.unknown + " kindMismatch=" + report.kindMismatch + " widened=" + report.widened + " clamped=" + report.clamped);
    }
}
`
}
