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

// A map whose KEY kind is narrower than the reader's declaration is a widening
// (docs/SPEC-TABLES.md §2.8: "A key kind the declaration's own kind WIDENS is
// not a disagreement: it decodes exactly, the entries land, and ONE widened
// counts for the map"). The Java map read used to accept the narrow key and
// then break on the framing damage before counting it, reporting widened=0
// where the oracle reports 1.
func TestJavaFixMapWidenedCount(t *testing.T) {
	t.Parallel()
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
	// the entry scan runs off into framing damage. The key still WIDENS before
	// that damage is seen, and the MAP counts it (§2.8): the oracle says
	// widened=1 with malformed=true, and a read that breaks on the damage
	// before counting the widening is not the oracle's answer.
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
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "R ") {
			javaReport = strings.TrimPrefix(line, "R ")
		}
	}
	if javaReport == "" {
		t.Fatalf("no java report in:\n%s", out)
	}
	model := tabletext.NewModel(u)
	want := new(tabletext.Report)
	value := model.New(u.Tables["Slots"])
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
	if want.Widened != 1 || !want.Malformed {
		t.Fatalf("test setup: oracle report %+v, want widened=1 and malformed", want)
	}
}
