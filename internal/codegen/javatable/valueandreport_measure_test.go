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
)

// TestJavaFixValueAndReportWithMeasureRight pins the wire-fuzzer cause J3: the
// Java variable-leg's saved bytes or report differ where its measure agrees
// with the oracle. The representative is streamdemo.Feed (corpus seed
// stream_chain, pass length #3 and the pinned stream_count_past_body vector):
// the Java leg reads on past the damage and saves 46 bytes where the oracle
// clamps and saves 24, reporting clamped=1. The generated Java reader must
// clamp and count clamped exactly as the Go oracle does (docs/SPEC-TABLES.md
// §3, §16.2).
func TestJavaFixValueAndReportWithMeasureRight(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	dir := filepath.Join(repo, "tables", "stream")
	u := unitFromDir(t, dir)
	model := tabletext.NewModel(u)
	root := "Feed"

	length3Hex := "01010803000000020f030d0b040e0306010105110200060c1602070a040e03060102051103000707040e030601030000c03a5cb5072eb7082652b3285633d4d8228e3c877681830f054aa33067555b8528f025a0ba6c31e5ffffffffffffffff82e051f9dc6843cf0700000000000000"
	length3, err := hex.DecodeString(length3Hex)
	if err != nil {
		t.Fatal(err)
	}
	pinned, err := os.ReadFile(filepath.Join(repo, "testdata", "wire", "tables", "fuzz-vectors", "stream_count_past_body.bin"))
	if err != nil {
		t.Fatal(err)
	}

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

	td := t.TempDir()
	classes := filepath.Join(td, "classes")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	var sources []string
	for name, data := range files {
		p := filepath.Join(td, name)
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
        %s.TableVariable.Report report=new %s.TableVariable.Report();
        %s.%sVariable.Value v=%s.%sVariable.load(data,report);
        if(v==null){System.out.println("null "+report.unknown+","+report.kindMismatch+","+report.widened+","+report.clamped+","+report.duplicate+","+report.malformed+","+report.refused);return;}
        byte[] saved=%s.%sVariable.save(v);
        System.out.println(report.unknown+","+report.kindMismatch+","+report.widened+","+report.clamped+","+report.duplicate+","+report.malformed+","+report.refused+","+hexstr(saved));
    }
}`, pkg, pkg, pkg, root, pkg, root, pkg, root)
	if err := os.WriteFile(driver, []byte(dsrc), 0o644); err != nil {
		t.Fatal(err)
	}
	sources = append(sources, driver)
	javac, java := javaTools(t)
	args := append([]string{"--release", "17", "-d", classes}, sources...)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}

	for name, wire := range map[string][]byte{"length3": length3, "pinned": pinned} {
		out, err := exec.Command(java, "-ea", "-cp", classes, "Driver", hex.EncodeToString(wire)).CombinedOutput()
		if err != nil {
			t.Fatalf("%s: java: %v\n%s", name, err, out)
		}
		line := ""
		for _, ln := range strings.Split(string(out), "\n") {
			ln = strings.TrimSpace(ln)
			if ln == "" || strings.HasPrefix(ln, "Picked up") {
				continue
			}
			line = ln
			break
		}
		if strings.HasPrefix(line, "null ") {
			t.Fatalf("%s: driver returned null: %q", name, line)
		}
		fields := strings.SplitN(line, ",", 8)
		if len(fields) != 8 {
			t.Fatalf("%s: driver output %q", name, line)
		}

		oracle := model.New(u.Tables[root])
		oreport := new(tabletext.Report)
		if ok, err := tablewire.Decode(model, oracle, wire, oreport); !ok || err != nil {
			t.Fatalf("%s: oracle decode: %v %+v", name, err, oreport)
		}
		owire, err := tablewire.Encode(model, oracle)
		if err != nil {
			t.Fatal(err)
		}
		javaSaved, err := hex.DecodeString(fields[7])
		if err != nil {
			t.Fatalf("%s: saved hex: %v", name, err)
		}
		got := strings.Join(fields[:7], ",")
		want := fmt.Sprintf("%d,%d,%d,%d,%d,%v,%v", oreport.Unknown, oreport.KindMismatch, oreport.Widened, oreport.Clamped, oreport.Duplicate, oreport.Malformed, oreport.Refused)
		if got != want {
			t.Errorf("%s: report java=%s oracle=%s (saved %d vs oracle %d)", name, got, want, len(javaSaved), len(owire))
		}
		if string(javaSaved) != string(owire) {
			t.Errorf("%s: saved bytes differ: java %d bytes, oracle %d bytes", name, len(javaSaved), len(owire))
		}
	}
}
