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

// A bytes(N) array body whose L is one byte short of its declared count keeps
// the bytes it decoded before the damage: the body's L says where the next
// field begins, so the missing tail is malformed and the prefix stands
// (docs/SPEC-TABLES.md §4, "Framing damage"). The minimal mutant is a
// backenddemo.LoginRequest whose session_token (bytes(32)) declares 32 bytes
// inside a body that carries 31.
func TestJavaFixShortArrayKeepsDecoded(t *testing.T) {
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
	if ok, err := tablewire.Decode(model, oracle, mutant, &report); err != nil || !ok {
		t.Fatalf("oracle refused the mutant's framing: ok=%v err=%v", ok, err)
	}
	if !report.Malformed {
		t.Fatalf("oracle report %+v: the short body is not malformed", report)
	}
	token, ok := oracle.FieldByKey("session_token")
	if !ok {
		t.Fatal("oracle has no session_token field")
	}
	if got := len(token.Cell.Str); got != 31 {
		t.Fatalf("oracle keeps %d session_token bytes, want the 31 decoded before the damage", got)
	}
	want, err := tablewire.Encode(model, oracle)
	if err != nil {
		t.Fatalf("oracle encode: %v", err)
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
	savedPath := filepath.Join(dir, "saved.bin")
	out, err := exec.Command(javaBin, "-ea", "-cp", classes, "ShortArrayDriver", wirePath, savedPath).CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	answer := lines[len(lines)-1]
	var malformed, refused int
	var clamped, widened, kindMismatch, unknown, duplicate int
	if _, err := fmt.Sscanf(answer, "%d %d %d %d %d %d %d", &malformed, &refused, &clamped, &widened, &kindMismatch, &unknown, &duplicate); err != nil {
		t.Fatalf("driver output %q: %v", out, err)
	}
	wantReport := [7]int{
		boolInt(report.Malformed), boolInt(report.Refused), report.Clamped,
		report.Widened, report.KindMismatch, report.Unknown, report.Duplicate,
	}
	gotReport := [7]int{malformed, refused, clamped, widened, kindMismatch, unknown, duplicate}
	if gotReport != wantReport {
		t.Fatalf("Java report %v, oracle %v", gotReport, wantReport)
	}

	got, err := os.ReadFile(savedPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("Java saves %d bytes, oracle %d (the short array's decoded prefix)", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Java saved value differs from the oracle at byte %d: %d vs %d", i, got[i], want[i])
		}
	}
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func shortArrayDriver(pkg string) string {
	return `import java.nio.file.Files;
import java.nio.file.Paths;

public final class ShortArrayDriver {
    private ShortArrayDriver() {}

    public static void main(String[] args) throws Exception {
        byte[] wire = Files.readAllBytes(Paths.get(args[0]));
        ` + pkg + `.TableVariable.Report report = new ` + pkg + `.TableVariable.Report();
        ` + pkg + `.LoginRequestVariable.Value value = ` + pkg + `.LoginRequestVariable.load(wire, report);
        byte[] saved = ` + pkg + `.LoginRequestVariable.save(value);
        Files.write(Paths.get(args[1]), saved);
        System.out.println(
                (report.malformed ? 1 : 0) + " "
                        + (report.refused ? 1 : 0) + " "
                        + report.clamped + " "
                        + report.widened + " "
                        + report.kindMismatch + " "
                        + report.unknown + " "
                        + report.duplicate);
    }
}
`
}
