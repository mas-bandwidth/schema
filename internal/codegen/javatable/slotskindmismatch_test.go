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

// A map header whose ELEMENT KIND is not 13 counts ONE kind_mismatch before
// anything about the count is judged. The count LEB128 is read against the
// PARENT buffer, not against the map's own L, so a count spelled wide enough to
// leave the framed body still leaves the element kind mismatch visible
// (docs/SPEC-TABLES.md §2.8, §4; internal/tablewire/decode.go mapField/subTo).
// The map then resets to empty and the rest of the body is skipped by its L.
func TestJavaFixSlotsKindMismatchUndercount(t *testing.T) {
	t.Parallel()
	const (
		dir   = "tables/maps"
		table = "Slots"
		// mapdemo.Slots random #6807 (115 bytes): the map field's body is two
		// bytes, element kind 4 and the first byte of a wide count, so the
		// count runs past the body's L while the element kind is still read.
		wire = "01010e0204feffffff03100e0402040402000000050465000000001802040600000003100e0402040401000000050464000000000604050000000061b072303048657fec105b36194ac93dea0ce83094fde47c8be145c2cb04da4f8794b696a762b5a0e9ea716f0f0182bf0600000000000000"
	)
	javac, java := javaTools(t)
	repo := filepath.Join("..", "..", "..")
	u := unitFromDir(t, filepath.Join(repo, dir))
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	varClass := table + "Variable"
	if files[varClass+".java"] == nil {
		t.Fatalf("%s.java is not emitted", varClass)
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
	mutant, err := hex.DecodeString(wire)
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
        %s.%s.Value v = %s.%s.load(wire, report);
        System.out.println("R " + report.unknown + "," + report.kindMismatch + "," + report.widened + "," + report.clamped + "," + report.duplicate + "," + report.malformed + "," + report.refused);
        if (v == null) { System.out.println("S -"); } else {
            byte[] saved = %s.%s.save(v);
            StringBuilder hex = new StringBuilder();
            for (byte b : saved) hex.append(String.format("%%02x", b & 0xff));
            System.out.println("S " + hex);
        }
    }
}
`, pkg, pkg, pkg, varClass, pkg, varClass, pkg, varClass)
	dpath := filepath.Join(srcdir, "Driver.java")
	if err := os.WriteFile(dpath, []byte(driver), 0o644); err != nil {
		t.Fatal(err)
	}
	sources = append(sources, dpath)
	sort.Strings(sources)
	args := append([]string{"--release", "17", "-d", classes}, sources...)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	out, err := exec.Command(java, "-ea", "-cp", classes, "Driver", wp).CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
	var javaReport, javaSaved string
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "R ") {
			javaReport = strings.TrimPrefix(line, "R ")
		}
		if strings.HasPrefix(line, "S ") {
			javaSaved = strings.TrimPrefix(line, "S ")
		}
	}
	if javaReport == "" || javaSaved == "" {
		t.Fatalf("no java report or save in:\n%s", out)
	}

	model := tabletext.NewModel(u)
	want := new(tabletext.Report)
	value := model.New(u.Tables[table])
	if _, err := tablewire.Decode(model, value, mutant, want); err != nil {
		t.Fatalf("oracle decode: %v", err)
	}
	if want.KindMismatch != 1 || !want.Malformed {
		t.Fatalf("test setup: oracle report %+v, want kind_mismatch=1 and malformed=true", want)
	}
	wantSaved, err := tablewire.Encode(model, value)
	if err != nil {
		t.Fatalf("oracle encode: %v", err)
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
	if javaSaved != hex.EncodeToString(wantSaved) {
		t.Errorf("saved bytes mismatch:\njava:   %s\noracle: %s", javaSaved, hex.EncodeToString(wantSaved))
	}
}
