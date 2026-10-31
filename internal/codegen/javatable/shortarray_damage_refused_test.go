package javatable

import (
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	javacodegen "github.com/mas-bandwidth/schema/v2/internal/codegen/java"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// A bytes(N) array body whose L is one byte short of its declared count is
// damage, and DAMAGE DISCARDS THE WHOLE LOAD (docs/SPEC-TABLES.md §4): the
// Java leg owes the refusal, malformed, and nothing it decoded is compared.
// The minimal mutant is a backenddemo.LoginRequest whose session_token
// (bytes(32)) declares 32 bytes inside a body that carries 31.
func TestJavaFixShortArrayDamageRefused(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	u := unitFromDir(t, filepath.Join(repo, "tables", "backend"))

	mutant, err := hex.DecodeString("01010e2106200102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f000dfa34688b0f86260100000000000000")
	if err != nil {
		t.Fatal(err)
	}

	model := tabletext.NewModel(u)
	def := model.Lookup("LoginRequest")
	if def == nil {
		t.Fatal("backenddemo declares no LoginRequest")
	}
	oracle := model.New(def)
	var report tabletext.Report
	if _, err := tablewire.Decode(model, oracle, mutant, &report); err != nil {
		t.Fatalf("oracle refused the mutant's form: %v", err)
	}
	if !report.Malformed {
		t.Fatalf("oracle report %+v: the short body is not malformed", report)
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
	driver := filepath.Join(dir, "ShortArrayDriver.java")
	if err := os.WriteFile(driver, []byte(shortArrayDriver(u.Package)), 0o644); err != nil {
		t.Fatal(err)
	}
	sources = append(sources, driver)
	javac, javaBin := javaTools(t)
	if out, err := exec.Command(javac, append([]string{"--release", "17", "-d", classes}, sources...)...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}

	wirePath := filepath.Join(dir, "mutant.bin")
	if err := os.WriteFile(wirePath, mutant, 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(javaBin, "-ea", "-cp", classes, "ShortArrayDriver", wirePath).CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var malformed int
	if _, err := fmt.Sscanf(lines[len(lines)-1], "%d", &malformed); err != nil {
		t.Fatalf("driver output %q: %v", out, err)
	}
	if malformed != 1 {
		t.Fatalf("Java LoginRequest did not refuse a damaged load: %q", out)
	}
}

func shortArrayDriver(pkg string) string {
	return `import java.nio.file.Files;
import java.nio.file.Paths;

public final class ShortArrayDriver {
    private ShortArrayDriver() {}

    public static void main(String[] args) throws Exception {
        byte[] wire = Files.readAllBytes(Paths.get(args[0]));
        ` + pkg + `.TableVariable.Report report = new ` + pkg + `.TableVariable.Report();
        ` + pkg + `.LoginRequestVariable.load(wire, report);
        System.out.println(report.malformed ? 1 : 0);
    }
}
`
}
