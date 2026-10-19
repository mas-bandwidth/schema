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

// A kind pass rewrites a map key's wire kind (Slots: int32 4 -> 5, Runs:
// uint16 7 -> 15). The oracle's key scan (§2.8) SKIPS the mis-kind key,
// reads a later correct key, and the desynced body tail counts malformed; it
// never reports a kind mismatch for the skipped occurrence. The Java leg used
// to count kind_mismatch for that occurrence: this pins the Java answer to
// the oracle's (saved bytes and every report counter).
func TestJavaFixMapKeyKind5Malformed(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	dir := filepath.Join(repo, "tables", "maps")
	u := unitFromDir(t, dir)
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
	var sources []string
	for name, data := range files {
		path := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, path)
	}
	mutants := map[string]string{
		"Slots": "01010e340d02180205feffffff03100e0402040402000000050465000000001802040600000003100e0402040401000000050464000000000604050000000061b072303048657fec105b36194ac93dea0ce83094fde47c8be145c2cb04da4f8794b696a762b5a0e9ea716f0f0182bf0600000000000000",
		"Runs":  "01010e300d021a020f0400030e120d0207040428000000000704042900000000001202070900030e0a0d010704045a000000000005040300000000166856b28afc7d43ec105b36194ac93dea0ce83094fde47c74a279448ee2e5b1e9ea716f0f0182bf0500000000000000",
	}
	for root, hexs := range mutants {
		root, hexs := root, hexs
		t.Run(root, func(t *testing.T) {
			t.Parallel()
			classes := filepath.Join(t.TempDir(), "classes")
			if err := os.MkdirAll(classes, 0o755); err != nil {
				t.Fatal(err)
			}
			mutant, err := hex.DecodeString(hexs)
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
        %s.%sVariable.Value v = %s.%sVariable.load(wire, report);
        System.out.println("R " + report.unknown + "," + report.kindMismatch + "," + report.widened + "," + report.clamped + "," + report.duplicate + "," + report.malformed + "," + report.refused);
        byte[] saved = %s.%sVariable.save(v);
        System.out.println("S " + java.util.HexFormat.of().formatHex(saved));
    }
}
`, pkg, pkg, pkg, root, pkg, root, pkg, root)
			dpath := filepath.Join(t.TempDir(), "Driver.java")
			if err := os.WriteFile(dpath, []byte(driver), 0o644); err != nil {
				t.Fatal(err)
			}
			src := append(append([]string{}, sources...), dpath)
			sort.Strings(src)
			javac, java := javaTools(t)
			args := append([]string{"--release", "17", "-d", classes}, src...)
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
				t.Fatalf("no java answer in:\n%s", out)
			}
			model := tabletext.NewModel(u)
			want := new(tabletext.Report)
			value := model.New(u.Tables[root])
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
			if !want.Malformed || want.KindMismatch != 0 {
				t.Fatalf("test setup: oracle report %+v, want malformed and no kind mismatch", want)
			}
			encoded, err := tablewire.Encode(model, value)
			if err != nil {
				t.Fatal(err)
			}
			if len(encoded) != 24 {
				t.Fatalf("test setup: oracle saves %d bytes, want 24", len(encoded))
			}
			if javaSaved != hex.EncodeToString(encoded) {
				t.Errorf("saved bytes: java=%s oracle=%s", javaSaved, hex.EncodeToString(encoded))
			}
		})
	}
}
