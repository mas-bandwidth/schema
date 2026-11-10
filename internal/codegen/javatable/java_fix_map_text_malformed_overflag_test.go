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

// TestJavaFixMapTextMalformedOverflag pins the mapdemo.Text "length pass"
// mutant (corpus seed map_text, pass length #130, run seed 24845619678)
// against the Go oracle. A length pass changes a length so it still passes the
// framing, decodes the same value (the same saved bytes) in both engines, but
// the Java map read once flagged damage where the oracle does not
// (docs/SPEC-TABLES.md §2.8, §3, §4). The Java leg must answer the oracle's
// VERDICT (the whole value, or a refusal of the whole load), and where both
// keep the value, the same unknown and widened counts and the same saved
// value bytes. Reason names are never compared.
func TestJavaFixMapTextMalformedOverflag(t *testing.T) {
	slowtest.Gate(t, "an external toolchain")

	t.Parallel()
	const mutantHex = "01010e260d0211020c05616c706861030c0566697273740011020c0462657461030c067365636f6e6400040e150d01120207050003210a4100e9002d4e3dd800de00050e240d02100204fdffffff030e000604deadbeef0010020402000000030e060604000180ff00060409000000004f8168f2ff28b7afec105b36194ac93dea0ce83094fde47cca5c7155f6f133a65b92de9cabeae214e9ea716f0f0182bf0600000000000000"
	wire, err := hex.DecodeString(mutantHex)
	if err != nil {
		t.Fatal(err)
	}

	javac, java := javaTools(t)
	dir := t.TempDir()
	classes := filepath.Join(dir, "classes")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}

	// The oracle first: decode the mutant and re-encode it, so the reference
	// answer (report counters and saved value bytes) is known.
	repo := filepath.Join("..", "..", "..")
	u := unitFromDir(t, filepath.Join(repo, "tables", "maps"))
	model := tabletext.NewModel(u)
	value := model.New(u.Tables["Text"])
	oreport := new(tabletext.Report)
	ok, err := tablewire.Decode(model, value, wire, oreport)
	if err != nil && !tablewire.ReadRefusal(err) {
		t.Fatalf("oracle Decode: %v", err)
	}
	oloaded := ok && err == nil && !oreport.Refused
	var encoded []byte
	if oloaded {
		if encoded, err = tablewire.Encode(model, value); err != nil {
			t.Fatalf("oracle Encode: %v", err)
		}
	}

	// Generate the Java leg for the unit and compile it beside a driver that
	// loads the mutant and prints the report and saved bytes.
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
	wirePath := filepath.Join(dir, "mutant.wire")
	if err := os.WriteFile(wirePath, wire, 0o644); err != nil {
		t.Fatal(err)
	}
	driver := strings.ReplaceAll(variableReportDriver(pkg, "Text"), "Driver", "MapTextFixDriver")
	driverPath := filepath.Join(dir, "MapTextFixDriver.java")
	if err := os.WriteFile(driverPath, []byte(driver), 0o644); err != nil {
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
	sources = append(sources, driverPath)
	sort.Strings(sources)
	args := append([]string{"--release", "17", "-d", classes}, sources...)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	out, err := exec.Command(java, "-ea", "-cp", classes, "MapTextFixDriver", wirePath).CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}

	loaded, jrep, saved := parseReportDriver(string(out))
	if loaded != oloaded {
		t.Fatalf("verdict differs: leg loaded=%t (%s), oracle loaded=%t (%+v)\n%s", loaded, jrep, oloaded, oreport, out)
	}
	if !loaded {
		// both refuse the whole load: the leg keeps no value and says so
		if !strings.Contains(jrep, "dmg=true") && !strings.Contains(jrep, "ref=true") {
			t.Errorf("the leg kept no value but reports neither damage nor refusal: %s", jrep)
		}
		return
	}
	jwant := fmt.Sprintf("unknown=%d wid=%d dmg=false ref=false", oreport.Unknown, oreport.Widened)
	if jrep != jwant {
		t.Errorf("report differs: leg %s, oracle %s", jrep, jwant)
	}
	if !equalBytes(saved, encoded) {
		t.Errorf("saved value differs: leg %d bytes, oracle %d bytes", len(saved), len(encoded))
	}
}

// variableReportDriver loads a wire, reports the verdict, the evolution
// counters and the saved value bytes, so the Go test can compare the leg with
// the oracle.
func variableReportDriver(pkg, root string) string {
	return `import java.nio.file.Files;
import java.nio.file.Paths;
import java.io.ByteArrayOutputStream;

public final class Driver {
    private Driver() {}

    public static void main(String[] args) throws Exception {
        byte[] wire = Files.readAllBytes(Paths.get(args[0]));
        ` + pkg + `.TableVariable.Report report = new ` + pkg + `.TableVariable.Report();
        ` + pkg + `.` + root + `Variable.Value v = ` + pkg + `.` + root + `Variable.load(wire, report);
        byte[] saved = v == null ? new byte[0] : ` + pkg + `.` + root + `Variable.save(v);
        ByteArrayOutputStream b = new ByteArrayOutputStream();
        for (byte x : saved) { b.write(x); }
        System.out.println("loaded=" + (v != null)
            + " unknown=" + report.unknown
            + " wid=" + report.widened
            + " dmg=" + report.damaged
            + " ref=" + report.refused);
        System.out.print("BYTES ");
        for (byte x : saved) { System.out.printf("%02x", x & 0xff); }
        System.out.println();
    }
}
`
}

func parseReportDriver(s string) (loaded bool, rep string, saved []byte) {
	for line := range strings.SplitSeq(s, "\n") {
		if strings.HasPrefix(line, "loaded=") {
			loaded = strings.Contains(line, "loaded=true")
			vals := map[string]string{}
			for kv := range strings.FieldsSeq(strings.TrimPrefix(line, "loaded=")) {
				parts := strings.SplitN(kv, "=", 2)
				if len(parts) == 2 {
					vals[parts[0]] = parts[1]
				}
			}
			rep = fmt.Sprintf("unknown=%s wid=%s dmg=%s ref=%s",
				vals["unknown"], vals["wid"], vals["dmg"], vals["ref"])
		}
		if after, ok := strings.CutPrefix(line, "BYTES "); ok {
			h := after
			if b, err := hex.DecodeString(h); err == nil {
				saved = b
			}
		}
	}
	return
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
