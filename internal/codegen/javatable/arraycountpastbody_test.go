package javatable

import (
	"crypto/sha256"
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
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestJavaFixArrayCountReadPastBody pins the wire-fuzzer cause AC1: a bounded
// or fixed array (wire kind 14) whose count LEB runs past the body's L. The
// oracle reads the body's HEADER against the parent buffer, not against the
// body's own L (internal/tablewire/decode.go arrayBody, §3.3, §4): the count
// decodes good even when it runs past L, clamps to the bound, and the element
// read over the empty span flags malformed. The Java read used to take the
// count with ar.leb() on the body reader bounded by L, so the LEB failed at L
// and no clamped counted.
func TestJavaFixArrayCountReadPastBody(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")

	// Case 1, tblv1.Cfg (V1.schema): field podium ([3]Grade, element kind 30)
	// with count LEB cf e1 a4 9d 87 4f, the body ending on the fifth count
	// byte, so the count runs past L, is over the bound 3, and clamps.
	schema := filepath.Join(repo, "test", "tables", "V1.schema")
	b, err := os.ReadFile(schema)
	if err != nil {
		t.Fatal(err)
	}
	sdir := t.TempDir()
	if err := os.WriteFile(filepath.Join(sdir, "V1.schema"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	u1 := unitFromDir(t, sdir)
	runArrayCountPastBodyCase(t, u1, "Cfg", "cfg14505",
		"01010a0000f040020100031e04050c056672657368060d0d070a00001841080a0000804000090e0a04020a000000c80000000a1e0b0c0e051e030d0b0e0f0e051ecfe1a49d874f03000d00100e0e0403070000000800000009000000111c1204040000000003000000000000ed09000000120f130d0714040600000000008cec01864cdc63aff2ef01864cde63afb2ad1dc4a2eb3d0d8be145c2cb04da4f861b638ebaadbcc48792885f8f064c2da6ecee9a99eb45453e203d04727ee88a6f2c414fbf84783ed48ac477299ba832b318320d7ec916c4c599f782764e0ad98025f1eade1ae5c391c5c4aa397792c44925d7235af46fb44656eeb16b2e8ce6dd542dd0294d3faa9443f2513bc8c136ca74e32f19244a339d707c9d0ac9dde81400000000000000",
		"b1e8185e35622b8e6eafd46818c5b2f8ebd6fc66b00c0b1d69a2b1271947006f",
		"tblv1")

	// Case 2, a hand-built WIDE fixed array ([3]uint128, the variablewide.go
	// read): the same count-straddles-L shape — L=2 holds the element kind and
	// one count byte, the count's second byte cb past the body, so the count
	// decodes past L, is over the bound 3, and clamps.
	wideSchema := `package wk
fixed table T {
    arr [3]uint128
}
`
	wsdir := t.TempDir()
	if err := os.WriteFile(filepath.Join(wsdir, "W.schema"), []byte(wideSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u2 := unitFromDir(t, wsdir)
	runArrayCountPastBodyCase(t, u2, "T", "wide-fixed",
		"01010e0213874f0100000000000000000000000000000002000000000000000000000000000000030000000000000000000000000000000038b3700519b156e70100000000000000",
		"", "wk")
}

func runArrayCountPastBodyCase(t *testing.T, u *ir.Unit, root, name, wireHex, sha, pkg string) {
	t.Helper()
	model := tabletext.NewModel(u)

	wire, err := hex.DecodeString(wireHex)
	if err != nil {
		t.Fatal(err)
	}
	if sha != "" {
		sum := sha256.Sum256(wire)
		if got := hex.EncodeToString(sum[:]); got != sha {
			t.Fatalf("%s: mutant sha256 = %s, want %s", name, got, sha)
		}
	}

	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	if files[root+"Variable.java"] == nil {
		t.Fatalf("%s: %sVariable.java is not emitted", name, root)
	}
	packet, err := javacodegen.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"Int128.java", "UInt128.java"} {
		if data := packet[n]; data != nil {
			files[n] = data
		}
	}
	td := t.TempDir()
	classes := filepath.Join(td, "classes")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	var sources []string
	for n, data := range files {
		p := filepath.Join(td, n)
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, p)
	}
	driver := filepath.Join(td, "Driver.java")
	dsrc := fmt.Sprintf(`public final class Driver {
    static byte[] hex(String s){byte[] o=new byte[s.length()/2];for(int i=0;i<o.length;i++)o[i]=(byte)Integer.parseInt(s.substring(2*i,2*i+2),16);return o;}
    static String hexstr(byte[] b){StringBuilder sb=new StringBuilder();for(byte x:b)sb.append(String.format("%%02x",x));return sb.toString();}
    public static void main(String[] args)throws Exception{
        byte[] data=hex(args[0]);
        long m=%s.%sVariable.loadMeasure(data);
        %s.TableVariable.Report report=new %s.TableVariable.Report();
        %s.%sVariable.Value v=%s.%sVariable.load(data,report);
        String counters=report.unknown+","+report.kindMismatch+","+report.widened+","+report.clamped+","+report.duplicate+","+report.malformed+","+report.refused;
        if(v==null){System.out.println("measure="+m+"|null|"+counters);return;}
        System.out.println("measure="+m+"|"+hexstr(%s.%sVariable.save(v))+"|"+counters);
    }
}`, pkg, root, pkg, pkg, pkg, root, pkg, root, pkg, root)
	if err := os.WriteFile(driver, []byte(dsrc), 0o644); err != nil {
		t.Fatal(err)
	}
	sources = append(sources, driver)
	sort.Strings(sources)
	javac, java := javaTools(t)
	args := append([]string{"--release", "17", "-d", classes}, sources...)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("%s: javac: %v\n%s", name, err, out)
	}

	out, err := exec.Command(java, "-ea", "-cp", classes, "Driver", wireHex).CombinedOutput()
	if err != nil {
		t.Fatalf("%s: java: %v\n%s", name, err, out)
	}
	line := ""
	for _, ln := range strings.Split(string(out), "\n") {
		if ln = strings.TrimSpace(ln); strings.HasPrefix(ln, "measure=") {
			line = ln
			break
		}
	}
	parts := strings.Split(line, "|")
	if len(parts) != 3 {
		t.Fatalf("%s: driver output %q", name, out)
	}
	measure, saved, counters := strings.TrimPrefix(parts[0], "measure="), parts[1], parts[2]

	_, _, refused := tablewire.FileRegionMeasure(u, u.Tables[root], wire)
	if refused {
		// The cause is a value-level clamp, not a framing refusal: the oracle
		// loads the wire, and the Java must answer it the same way.
		t.Fatalf("%s: the oracle framing refuses a wire of this cause; the case is misbuilt", name)
	}
	if measure == "-1" {
		t.Errorf("%s: Java loadMeasure = -1, the oracle framing does not refuse", name)
	}
	oracle := model.New(u.Tables[root])
	oreport := new(tabletext.Report)
	if _, err := tablewire.Decode(model, oracle, wire, oreport); err != nil {
		t.Fatalf("%s: oracle decode: %v %+v", name, err, oreport)
	}
	owire, err := tablewire.Encode(model, oracle)
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("%d,%d,%d,%d,%d,%v,%v", oreport.Unknown, oreport.KindMismatch, oreport.Widened, oreport.Clamped, oreport.Duplicate, oreport.Malformed, oreport.Refused)
	if counters != want {
		t.Errorf("%s: report java=%s oracle=%s", name, counters, want)
	}
	if saved != hex.EncodeToString(owire) {
		t.Errorf("%s: saved bytes differ: java %s, oracle %x", name, saved, owire)
	}
	if oreport.Clamped != 1 {
		t.Fatalf("%s: test setup: oracle report %+v, want clamped=1", name, oreport)
	}
}
