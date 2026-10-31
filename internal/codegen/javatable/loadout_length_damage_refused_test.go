package javatable

import (
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	javacodegen "github.com/mas-bandwidth/schema/v2/internal/codegen/java"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestJavaFixLoadoutLengthDamageRefused pins the wire-fuzzer cause J9: on
// tabledemo.LoadoutConfig a length pass mutant is malformed, and DAMAGE
// DISCARDS THE WHOLE LOAD (docs/SPEC-TABLES.md §4): the Java leg owes the
// refusal, malformed, and nothing it decoded or would save is compared. The
// mutant is the loadout_full length #36 mutant of the all-root gate.
func TestJavaFixLoadoutLengthDamageRefused(t *testing.T) {
	t.Parallel()
	const wireHex = "01011e02030e051e03040502060e051e0300020007090100000000000000080d16090a000048410a01010b0f0c0d080d0a0000003f00000e0e090d020100040f063f00100e100d010d110406000000120a0000803e0000d48ac477299ba83291c5c4aa397792c4c599f782764e0ad9b318320d7ec916c48025f1eade1ae5c34925d7235af46fb44ae5967084f5064b39e983f77f1331bfc07fb38abe08637f593f2e60bc8765ad9443f2513bc8c136cc69e4e29bbeb5ffc6875c803a62dc9a24cc8a11f5f028dea4edcad59a3e01a5419a244003aa01f9d131fef61816776ae6b4e78a35d1f9ee1200000000000000"
	wire, err := hex.DecodeString(wireHex)
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join("..", "..", "..")
	u := unitFromDir(t, filepath.Join(repo, "tables", "examples"))

	ans := goOracleLoadout(t, u, "LoadoutConfig", wire)
	if !ans.report.Malformed {
		t.Fatalf("oracle did not report malformed; this mutant does not pin the cause")
	}
	leg := javaLoadout(t, u, "LoadoutConfig", wire)
	if !leg.report.malformed {
		t.Fatalf("Java LoadoutConfig did not refuse a damaged load: %+v", leg.report)
	}
}

type oracleLoadoutAnswer struct {
	encoded []byte
	report  tabletext.Report
}

func goOracleLoadout(t *testing.T, u *ir.Unit, root string, wire []byte) oracleLoadoutAnswer {
	t.Helper()
	model := tabletext.NewModel(u)
	value := model.New(u.Tables[root])
	report := new(tabletext.Report)
	if _, err := tablewire.DecodeRegion(model, value, wire, report); err != nil {
		t.Fatal(err)
	}
	encoded, eerr := tablewire.Encode(model, value)
	if eerr != nil {
		t.Fatal(eerr)
	}
	return oracleLoadoutAnswer{encoded: encoded, report: *report}
}

type javaLoadoutReport struct {
	malformed, refused                                 bool
	unknown, kindMismatch, widened, clamped, duplicate int
}

type javaLoadoutAnswer struct {
	saved  []byte
	report javaLoadoutReport
}

func javaLoadout(t *testing.T, u *ir.Unit, root string, wire []byte) javaLoadoutAnswer {
	t.Helper()
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
	dir := t.TempDir()
	classes := filepath.Join(dir, "classes")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	var sources []string
	for name, data := range files {
		if !strings.HasSuffix(name, ".java") {
			continue
		}
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, path)
	}
	driverSrc := fmt.Sprintf(`import java.nio.file.Files;
import java.nio.file.Paths;
import java.nio.charset.StandardCharsets;
public final class Driver {
    private Driver() {}
    public static void main(String[] args) throws Exception {
        byte[] wire = Files.readAllBytes(Paths.get(args[0]));
        %s.TableVariable.Report report = new %s.TableVariable.Report();
        %s.%sVariable.Value v = %s.%sVariable.load(wire, report);
        System.out.println("malformed=" + report.malformed + " refused=" + report.refused +
            " unknown=" + report.unknown + " kindMismatch=" + report.kindMismatch +
            " widened=" + report.widened + " clamped=" + report.clamped +
            " duplicate=" + report.duplicate);
        if (v == null) { System.out.println("saved=null"); return; }
        byte[] saved = %s.%sVariable.save(v);
        System.out.println("saved=" + new String(java.util.HexFormat.of().formatHex(saved)));
        System.out.println("json=" + new String(%s.%sVariable.toJson(v), StandardCharsets.UTF_8));
    }
}`, pkg, pkg, pkg, root, pkg, root, pkg, root, pkg, root)
	dp := filepath.Join(dir, "Driver.java")
	if err := os.WriteFile(dp, []byte(driverSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	sources = append(sources, dp)
	javac, java := javaTools(t)
	args := append([]string{"--release", "17", "-d", classes}, sources...)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	wp := filepath.Join(dir, "mutant.wire")
	if err := os.WriteFile(wp, wire, 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(java, "-ea", "-cp", classes, "Driver", wp).CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
	t.Logf("java output:\n%s", out)
	var ans javaLoadoutAnswer
	haveReport, haveSaved := false, false
	for line := range strings.SplitSeq(string(out), "\n") {
		if strings.HasPrefix(line, "malformed=") {
			fields := strings.Fields(line)
			if len(fields) != 7 {
				t.Fatalf("bad java report line %q", line)
			}
			for _, field := range fields {
				parts := strings.SplitN(field, "=", 2)
				if len(parts) != 2 {
					t.Fatalf("bad java report field %q", field)
				}
				switch parts[0] {
				case "malformed":
					ans.report.malformed = parts[1] == "true"
				case "refused":
					ans.report.refused = parts[1] == "true"
				case "unknown":
					ans.report.unknown = javaCounter(t, parts[0], parts[1])
				case "kindMismatch":
					ans.report.kindMismatch = javaCounter(t, parts[0], parts[1])
				case "widened":
					ans.report.widened = javaCounter(t, parts[0], parts[1])
				case "clamped":
					ans.report.clamped = javaCounter(t, parts[0], parts[1])
				case "duplicate":
					ans.report.duplicate = javaCounter(t, parts[0], parts[1])
				default:
					t.Fatalf("unknown java report field %q", field)
				}
			}
			haveReport = true
		}
		if strings.HasPrefix(line, "saved=") {
			haveSaved = true
			if line == "saved=null" {
				// the load handed back nothing, which is a damaged load's
				// whole answer (docs/SPEC-TABLES.md §4)
				continue
			}
			b, err := hex.DecodeString(strings.TrimPrefix(line, "saved="))
			if err != nil {
				t.Fatalf("bad saved hex %q: %v", line, err)
			}
			ans.saved = b
		}
	}
	if !haveReport || !haveSaved {
		t.Fatalf("driver output lacks a report or saved line:\n%s", out)
	}
	return ans
}

func javaCounter(t *testing.T, name, value string) int {
	t.Helper()
	n, err := strconv.Atoi(value)
	if err != nil {
		t.Fatalf("bad java report counter %s=%q: %v", name, value, err)
	}
	return n
}
