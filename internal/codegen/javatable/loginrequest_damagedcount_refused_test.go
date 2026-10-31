package javatable

import (
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	javacodegen "github.com/mas-bandwidth/schema/v2/internal/codegen/java"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestJavaFixLoginRequestDamagedCountRefused holds the Java leg to the
// refusal on a LoginRequest whose bytes field carries a damaged count: DAMAGE
// DISCARDS THE WHOLE LOAD (docs/SPEC-TABLES.md §4), so the leg owes malformed
// and no counter or saved byte is compared.
func TestJavaFixLoginRequestDamagedCountRefused(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "javac and java (it builds and runs the generated Java)")
	repo := filepath.Join("..", "..", "..")
	dir := filepath.Join(repo, "tables", "backend")
	u := unitFromDir(t, dir)
	model := tabletext.NewModel(u)
	inst := model.New(u.Tables["LoginRequest"])

	mutant, err := hex.DecodeString("0101090100000000002000020e2281888f969da4abb2b9c0c7ced5dc0620030a11181f262d343b424950575e656c737a81888f969da4abb2b9c0c7ced5dc0308c9230200041e050038c48d74ffe34ac10dfa34688b0f8626f9080bbd37d565b8dd240af523a655c7cf5a96b5075ffb080500000000000000")
	if err != nil {
		t.Fatal(err)
	}

	var oracle tabletext.Report
	if _, err := tablewire.Decode(model, inst, mutant, &oracle); err != nil || !oracle.Malformed {
		t.Fatalf("oracle does not read the mutant as damaged: %v %+v", err, oracle)
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
	if files["LoginRequestVariable.java"] == nil {
		t.Fatal("LoginRequestVariable.java is not emitted")
	}
	pkg := javaPackageOf(u)
	work := t.TempDir()
	classes := filepath.Join(work, "classes")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	var sources []string
	for name, data := range files {
		path := filepath.Join(work, name)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, path)
	}
	wirePath := filepath.Join(work, "mutant.wire")
	if err := os.WriteFile(wirePath, mutant, 0o644); err != nil {
		t.Fatal(err)
	}
	driver := filepath.Join(work, "OracleDriver.java")
	if err := os.WriteFile(driver, []byte(oracleDriver(pkg, "LoginRequest")), 0o644); err != nil {
		t.Fatal(err)
	}
	sources = append(sources, driver)
	sort.Strings(sources)
	javac, javaBin := javaTools(t)
	args := append([]string{"--release", "17", "-d", classes}, sources...)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	out, err := exec.Command(javaBin, "-ea", "-cp", classes, "OracleDriver", wirePath).CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
	leg := legLine(string(out))
	if fields := strings.Fields(strings.ReplaceAll(leg, ",", " ")); len(fields) < 6 || fields[5] != "true" {
		t.Fatalf("Java LoginRequest did not refuse a damaged load: %q", leg)
	}
}

// legLine extracts the last non-empty line of a Java driver's output, dropping
// the JVM's stderr "Picked up JAVA_TOOL_OPTIONS" banner.
func legLine(out string) string {
	lines := strings.Split(out, "\n")
	var last string
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		if ln != "" && !strings.HasPrefix(ln, "Picked up JAVA_TOOL_OPTIONS") {
			last = ln
		}
	}
	return last
}

// oracleDriver loads a wire file, decodes it through the generated table codec,
// and prints the report counters plus the number of bytes the leg re-saves.
func oracleDriver(pkg, root string) string {
	return `import java.nio.file.Files;
import java.nio.file.Paths;

public final class OracleDriver {
    private OracleDriver() {}

    public static void main(String[] args) throws Exception {
        byte[] wire = Files.readAllBytes(Paths.get(args[0]));
        ` + pkg + `.TableVariable.Report report = new ` + pkg + `.TableVariable.Report();
        ` + pkg + `.` + root + `Variable.Value v = ` + pkg + `.` + root + `Variable.load(wire, report);
        if (v == null) { System.out.println(report.unknown + "," + report.kindMismatch + "," + report.widened + "," + report.clamped + "," + report.duplicate + "," + report.malformed + " null"); return; }
        byte[] saved = ` + pkg + `.` + root + `Variable.save(v);
        System.out.println(report.unknown + "," + report.kindMismatch + "," + report.widened + "," + report.clamped + "," + report.duplicate + "," + report.malformed + " " + saved.length);
    }
}
`
}
