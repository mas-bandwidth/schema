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
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestJavaFixKeyedCountExtreme pins the wire-fuzzer cause C5: a keyed body
// whose element count is 2^64-1. The count rides as an unsigned LEB128
// (docs/SPEC-TABLES.md §3.2, §2.4), so the Go oracle iterates and reads past
// the short body, reporting malformed; the Java leg's signed long loop bound
// turns the count negative and skips the body entirely, reporting clean. The
// Java loop must compare the count unsigned to agree with the oracle.
func TestJavaFixKeyedCountExtreme(t *testing.T) {
	t.Parallel()
	const wireHex = "0101102b0dffffffffffffffffff01020d030408000000040c0372656400050f030402000000040c05677265656e000610480d02073b080a00007a43090a000060400a102b0d020b0b0c0a000020420d0d0100000e1a0c0a0000a0400f0a000010410d0d0a100a0000403f1101010000001207090a0000404100130d1214100e040202040a00000005041e00000000006dfaa8a548b0aeba7c1bacfe19def19f74b65dd6e299eccecf0ca0c7b1daa0bc1c3f95d58fd700cfffd89456c2c30ace1fbc0d42e1244d33cfa98b28b5d4697fb10a7bcea257371f8c6083c20b26f8847c762e3bbede5458c07fb38abe08637faa44cdc048b6db407652ffa8ae16dc04483d345369be2cdc6a64012266a35ab7bcb002469a71bfa6db230ed0a321236cb20f40270b6b980160160e9a73ad0ff11400000000000000"
	wire, err := hex.DecodeString(wireHex)
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join("..", "..", "..")
	u := unitFromDir(t, filepath.Join(repo, "tables", "examples"))

	oracleMalformed := goOracleKeyedMalformed(t, u, wire)
	if !oracleMalformed {
		t.Fatalf("oracle did not report malformed; this mutant does not pin the cause")
	}

	if javaKeyedMalformed(t, u, "KeyedConfig", wire) != oracleMalformed {
		t.Fatalf("Java keyed count 2^64-1 diverges from the oracle")
	}
}

func goOracleKeyedMalformed(t *testing.T, u *ir.Unit, wire []byte) bool {
	t.Helper()
	model := tabletext.NewModel(u)
	value := model.New(u.Tables["KeyedConfig"])
	report := new(tabletext.Report)
	ok, err := tablewire.Decode(model, value, wire, report)
	if err != nil {
		t.Fatal(err)
	}
	return !ok || report.Malformed
}

func javaKeyedMalformed(t *testing.T, u *ir.Unit, root string, wire []byte) bool {
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
public final class Driver {
    private Driver() {}
    public static void main(String[] args) throws Exception {
        byte[] wire = Files.readAllBytes(Paths.get(args[0]));
        %s.TableVariable.Report report = new %s.TableVariable.Report();
        %s.%sVariable.Value v = %s.%sVariable.load(wire, report);
        System.out.println("malformed=" + report.malformed + " unknown=" + report.unknown +
            " kindMismatch=" + report.kindMismatch + " widened=" + report.widened);
    }
}`, pkg, pkg, pkg, root, pkg, root)
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
	for line := range strings.SplitSeq(string(out), "\n") {
		if strings.HasPrefix(line, "malformed=") {
			return strings.Contains(line, "malformed=true")
		}
	}
	t.Fatalf("driver output has no malformed line:\n%s", out)
	return false
}
