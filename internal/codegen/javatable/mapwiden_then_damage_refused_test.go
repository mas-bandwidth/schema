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
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// A map whose first KEY widens and whose entry scan then runs off into framing
// damage is damaged input, and DAMAGE DISCARDS THE WHOLE LOAD
// (docs/SPEC-TABLES.md §4): the Java leg owes the refusal, malformed, and the
// widened count the scan made before the damage is not compared.
func TestJavaFixMapWidenThenDamageRefused(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "javac and java (it builds and runs the generated Java)")
	repo := filepath.Join("..", "..", "..")
	dir := filepath.Join(repo, "tables", "maps")
	u := unitFromDir(t, dir)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	if files["SlotsVariable.java"] == nil {
		t.Fatal("SlotsVariable.java is not emitted")
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
	classes := filepath.Join(t.TempDir(), "classes")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	srcdir := t.TempDir()
	var sources []string
	for name, data := range files {
		path := filepath.Join(srcdir, name)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, path)
	}
	// mapdemo.Slots: the first key's kind byte is 2 (one-byte int8) under a
	// field declared int32, and its old four-byte payload is left behind, so
	// the entry scan runs off into framing damage.
	mutant, err := hex.DecodeString("01010e340d02180202feffffff03100e0402040402000000050465000000001802040600000003100e0402040401000000050464000000000604050000000061b072303048657fec105b36194ac93dea0ce83094fde47c8be145c2cb04da4f8794b696a762b5a0e9ea716f0f0182bf0600000000000000")
	if err != nil {
		t.Fatal(err)
	}
	wp := filepath.Join(t.TempDir(), "mutant.bin")
	if err := os.WriteFile(wp, mutant, 0o644); err != nil {
		t.Fatal(err)
	}
	driver := fmt.Sprintf(`
import java.nio.file.Files;
import java.nio.file.Paths;
public final class Driver {
    public static void main(String[] args) throws Exception {
        byte[] wire = Files.readAllBytes(Paths.get(args[0]));
        %s.TableVariable.Report report = new %s.TableVariable.Report();
        %s.SlotsVariable.Value v = %s.SlotsVariable.load(wire, report);
        System.out.println("R " + report.unknown + "," + report.kindMismatch + "," + report.widened + "," + report.clamped + "," + report.duplicate + "," + report.malformed + "," + report.refused);
        if (v == null) System.out.println("V null"); else { System.out.print("V "); System.out.write(%s.SlotsVariable.toJson(v)); System.out.println(); }
    }
}
`, pkg, pkg, pkg, pkg, pkg)
	dpath := filepath.Join(srcdir, "Driver.java")
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
	var javaReport string
	for line := range strings.SplitSeq(string(out), "\n") {
		if after, ok := strings.CutPrefix(line, "R "); ok {
			javaReport = after
		}
	}
	if javaReport == "" {
		t.Fatalf("no java report in:\n%s", out)
	}
	model := tabletext.NewModel(u)
	want := new(tabletext.Report)
	if _, err := tablewire.Decode(model, model.New(u.Tables["Slots"]), mutant, want); err != nil || !want.Malformed {
		t.Fatalf("oracle does not read the mutant as damaged: %v %+v", err, want)
	}
	if gotFields := strings.Split(javaReport, ","); len(gotFields) != 7 || gotFields[5] != "true" {
		t.Errorf("Java Slots did not refuse a damaged load: %q", javaReport)
	}
}
