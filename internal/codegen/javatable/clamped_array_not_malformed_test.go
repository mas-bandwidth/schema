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

// A bounded array [3] whose count is raised past the bound (3 to 4) once
// clamped to the bound. The clamp is retired (docs/plans/strict-only.md,
// docs/SPEC-TABLES.md §4): a count past the reader's bound refuses the whole
// load in both engines, and the Java leg hands back no value.
func TestJavaFixClampedArrayNotMalformed(t *testing.T) {
	slowtest.Gate(t, "an external toolchain")

	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	dir := filepath.Join(repo, "tables", "scalars")
	u := unitFromDir(t, dir)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	if files["SimStateVariable.java"] == nil {
		t.Fatal("SimStateVariable.java is not emitted")
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
	var sources []string
	for name, data := range files {
		path := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, path)
	}
	// scalars.SimState with samples=[1,2,3], count raised 3->4 (§4 over-bound).
	mutant, err := hex.DecodeString("01010e0e160400000100000002000000030000dcdd483b6acab1e30100000000000000")
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
        %s.SimStateVariable.Value v = %s.SimStateVariable.load(wire, report);
        System.out.println("R " + report.unknown + "," + report.widened + "," + report.damaged + "," + report.refused);
        if (v == null) System.out.println("V null"); else { System.out.print("V "); System.out.write(%s.SimStateVariable.toJson(v)); System.out.println(); }
    }
}
`, pkg, pkg, pkg, pkg, pkg)
	dpath := filepath.Join(t.TempDir(), "Driver.java")
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
	value := model.New(u.Tables["SimState"])
	if ok, err := tablewire.Decode(model, value, mutant, want); ok || !tablewire.ReadRefusal(err) {
		t.Fatalf("test setup: the oracle read a count past the bound: ok=%v err=%v report=%+v", ok, err, want)
	}
	gotFields := strings.Split(javaReport, ",")
	if len(gotFields) != 4 {
		t.Fatalf("java report shape: %q", javaReport)
	}
	if gotFields[2] != "true" && gotFields[3] != "true" {
		t.Errorf("Java SimState did not refuse a count past the bound: %q (oracle %+v)", javaReport, want)
	}
	if !strings.Contains(string(out), "V null") {
		t.Errorf("Java SimState handed back a value for a count past the bound:\n%s", out)
	}
}
