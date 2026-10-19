package javatable

import (
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	javacodegen "github.com/mas-bandwidth/schema/v2/internal/codegen/java"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestJavaFixMeasureRenamedNodeTable holds loadMeasure to
// tablewire.FileRegionMeasure on the wire fuzzer's cause C1: a node table
// record whose type id names a POINTER-REACHABLE TABLE still commands its own
// rounded record region, not only the +16 directory (docs/SPEC-TABLES.md
// §3.1, §6.5). The mutant's root field id was renamed to an unknown id by the
// reference pass; the node table itself is whole, with one Node record, and
// the oracle sizes that record's body.
func TestJavaFixMeasureRenamedNodeTable(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	dir := t.TempDir()
	schema, err := os.ReadFile(filepath.Join(repo, "test", "tables", "G1.schema"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "G1.schema"), schema, 0o644); err != nil {
		t.Fatal(err)
	}
	u := unitFromDir(t, dir)
	root := u.Tables["Guarded"]
	if root == nil {
		t.Fatal("tblg1 has no Guarded root")
	}
	wire, err := hex.DecodeString("01041102020c0a010307040401000000000044e5f7fadfa6c4f7ffffffffffffffff8db6f6d2c61cbd66ea0ce83094fde47c0400000000000000")
	if err != nil {
		t.Fatal(err)
	}
	oracle, whole, refused := tablewire.FileRegionMeasure(u, root, wire)
	if !whole || refused || oracle != 64 {
		t.Fatalf("oracle = %d whole=%t refused=%t, want 64 whole and not refused", oracle, whole, refused)
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
	pkg := javaPackageOf(u)
	build := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(build, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	driver := `import java.nio.file.Files;
import java.nio.file.Paths;

public final class MeasureDriver {
    private MeasureDriver() {}
    public static void main(String[] args) throws Exception {
        byte[] wire = Files.readAllBytes(Paths.get(args[0]));
        System.out.println(` + pkg + `.GuardedVariable.loadMeasure(wire));
    }
}
`
	driverPath := filepath.Join(build, "MeasureDriver.java")
	if err := os.WriteFile(driverPath, []byte(driver), 0o644); err != nil {
		t.Fatal(err)
	}
	classes := filepath.Join(build, "classes")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	var sources []string
	for name := range files {
		sources = append(sources, filepath.Join(build, name))
	}
	sources = append(sources, driverPath)
	javac, javaBin := javaTools(t)
	if out, err := exec.Command(javac, append([]string{"--release", "17", "-d", classes}, sources...)...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	wirePath := filepath.Join(build, "mutant.bin")
	if err := os.WriteFile(wirePath, wire, 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(javaBin, "-ea", "-cp", classes, "MeasureDriver", wirePath).CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
	var got int64
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if n, err := strconv.ParseInt(strings.TrimSpace(line), 10, 64); err == nil {
			got = n
		}
	}
	if got != oracle {
		t.Fatalf("Java loadMeasure = %d, oracle FileRegionMeasure = %d", got, oracle)
	}
}
