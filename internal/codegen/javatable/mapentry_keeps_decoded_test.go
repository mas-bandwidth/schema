package javatable

import (
	"bytes"
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
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestJavaFixMapEntryKeepsDecoded pins the wire-fuzzer cause J6a: a damaged
// map entry must keep the entries decoded before it, exactly as the Go oracle
// does (docs/SPEC-TABLES.md §2.8: "the map keeps the ascending prefix it has").
// The Java leg saved fewer bytes than the oracle on the map roots. The mutant
// is the Cells entry #8 mutant of the all-root gate (mapdemo.Cells, pass entry
// #8): the oracle re-encodes the retained value to 64 bytes, the pre-fix Java
// leg to 39.
func TestJavaFixMapEntryKeepsDecoded(t *testing.T) {
	t.Parallel()
	const wireHex = "01010e370d021a020c05616c706861030e0e04030a0000000b0000000c0000000019020c0462657461030e0e0403000000000100000002000000000404070000000038810af11f06a7a3ec105b36194ac93dffffffffffffffffe9ea716f0f0182bf0400000000000000"
	wire, err := hex.DecodeString(wireHex)
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join("..", "..", "..")
	u := unitFromDir(t, filepath.Join(repo, "tables", "maps"))

	ans := goOracleMapEntry(t, u, "Cells", wire)
	t.Logf("oracle: encoded=%d hex=%s malformed=%v", len(ans.encoded), hex.EncodeToString(ans.encoded), ans.malformed)
	if !ans.malformed {
		t.Fatalf("oracle did not report malformed; this mutant does not pin the cause")
	}

	saved := javaMapEntrySaved(t, u, "Cells", wire)
	if !bytes.Equal(saved, ans.encoded) {
		t.Fatalf("Java map entry keeps different bytes than the oracle: leg saves %d bytes, oracle %d\nleg=%s\noracle=%s",
			len(saved), len(ans.encoded), hex.EncodeToString(saved), hex.EncodeToString(ans.encoded))
	}
}

type oracleMapEntryAnswer struct {
	encoded   []byte
	malformed bool
}

func goOracleMapEntry(t *testing.T, u *ir.Unit, root string, wire []byte) oracleMapEntryAnswer {
	t.Helper()
	model := tabletext.NewModel(u)
	value := model.New(u.Tables[root])
	report := new(tabletext.Report)
	ok, err := tablewire.DecodeRegion(model, value, wire, report)
	if err != nil {
		t.Fatal(err)
	}
	encoded, eerr := tablewire.Encode(model, value)
	if eerr != nil {
		t.Fatal(eerr)
	}
	return oracleMapEntryAnswer{encoded: encoded, malformed: report.Malformed || (!ok && report.Refused)}
}

func javaMapEntrySaved(t *testing.T, u *ir.Unit, root string, wire []byte) []byte {
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
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "saved=") {
			if line == "saved=null" {
				t.Fatalf("Java load returned null:\n%s", out)
			}
			for _, rl := range strings.Split(string(out), "\n") {
				if strings.HasPrefix(rl, "malformed=") {
					t.Logf("java report: %s", rl)
				}
			}
			b, err := hex.DecodeString(strings.TrimPrefix(line, "saved="))
			if err != nil {
				t.Fatalf("bad saved hex %q: %v", line, err)
			}
			return b
		}
	}
	t.Fatalf("driver output has no saved line:\n%s", out)
	return nil
}
