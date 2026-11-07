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
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// A bounded array [3] whose count is raised past the bound (3 to 4) clamps to
// the bound in both engines; the oracle counts clamped and does NOT flag
// malformed (docs/SPEC-TABLES.md §4: "A count past the READER's bound keeps
// the bounded prefix and counts clamped; ... malformed is reserved for a count
// the BODY cannot cover"). The Java leg used to scan the over-bound tail and
// flag malformed when the body had no bytes for it: this pins the Java answer
// to the oracle's.
func TestJavaFixClampedArrayNotMalformed(t *testing.T) {
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
        System.out.println("R " + report.unknown + "," + report.kindMismatch + "," + report.widened + "," + report.clamped + "," + report.duplicate + "," + report.malformed + "," + report.refused);
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
	ok, err := tablewire.Decode(model, value, mutant, want)
	if !ok || err != nil {
		t.Fatalf("oracle decode: ok=%v err=%v report=%+v", ok, err, want)
	}
	gotFields := strings.Split(javaReport, ",")
	if len(gotFields) != 7 {
		t.Fatalf("java report shape: %q", javaReport)
	}
	wantFields := []string{
		fmt.Sprintf("%d", want.Unknown),
		fmt.Sprintf("%d", want.KindMismatch),
		fmt.Sprintf("%d", want.Widened),
		fmt.Sprintf("%d", want.Clamped),
		fmt.Sprintf("%d", want.Duplicate),
		fmt.Sprintf("%t", want.Malformed),
		fmt.Sprintf("%t", want.Refused),
	}
	for i := range wantFields {
		if gotFields[i] != wantFields[i] {
			t.Errorf("report field %d: java=%s oracle=%s (java report %q, oracle %+v)", i, gotFields[i], wantFields[i], javaReport, want)
		}
	}
	if want.Clamped != 1 || want.Malformed {
		t.Fatalf("test setup: oracle report %+v, want clamped=1 and not malformed", want)
	}
}
