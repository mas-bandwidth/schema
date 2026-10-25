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
)

// TestJavaFixA1ArmBodyAsA2SavedBytes pins the a1_arm_body_as_a2 read
// (docs/SPEC-TABLES.md §3.3): under the A2 schema, an A1 arm body read with a
// damaged union header must answer exactly as the Go oracle does — the arm
// content already decoded stands and is re-encoded whole, and every report
// counter matches. Before the null-keep fix, the Java leg dropped the arm
// content a damaged header occurrence left standing and saved 24 bytes where
// the oracle saved 60 (the same counters in both engines).
//
// The mutant is the representative random pass #6001 at seed 24845619678 on
// corpus seed a1_arm_body_as_a2 (tbla2.Root). The oracle keeps the arm and
// re-encodes 60 bytes; the buggy leg saved only 24 and lost the decoded value.
func TestJavaFixA1ArmBodyAsA2SavedBytes(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	u := unitFromPath(t, filepath.Join(repo, "test", "tables", "A2.schema"))
	model := tabletext.NewModel(u)

	mutantHex := "0101080b000000020f030d0704080500000000020f030d850408050000000005081600000000c03a5cb5072eb708ea0ce83094fde47cc0e501864cd863af71f801864ce363af23d79807ef564cd90500000000000000"
	mutant, err := hex.DecodeString(mutantHex)
	if err != nil {
		t.Fatal(err)
	}

	oracle := model.New(u.Tables["Root"])
	oreport := new(tabletext.Report)
	if _, err := tablewire.Decode(model, oracle, mutant, oreport); err != nil {
		t.Fatalf("oracle decode: %v %+v", err, oreport)
	}
	owire, err := tablewire.Encode(model, oracle)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("oracle: unknown=%d kindMismatch=%d widened=%d clamped=%d duplicate=%d malformed=%v saved=%d",
		oreport.Unknown, oreport.KindMismatch, oreport.Widened, oreport.Clamped, oreport.Duplicate, oreport.Malformed, len(owire))

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
	root := "Root"

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
	dsrc := fmt.Sprintf(`import java.nio.charset.StandardCharsets;
public final class Driver {
    static byte[] hex(String s){byte[] o=new byte[s.length()/2];for(int i=0;i<o.length;i++)o[i]=(byte)Integer.parseInt(s.substring(2*i,2*i+2),16);return o;}
    static String hexstr(byte[] b){StringBuilder sb=new StringBuilder();for(byte x:b)sb.append(String.format("%%02x",x));return sb.toString();}
    public static void main(String[] args)throws Exception{
        byte[] data=hex(args[0]);
        %s.TableVariable.Report report=new %s.TableVariable.Report();
        %s.%sVariable.Value v=%s.%sVariable.load(data,report);
        if(v==null){System.out.println("null");return;}
        byte[] saved=%s.%sVariable.save(v);
        System.out.println(report.unknown+" "+report.kindMismatch+" "+report.widened+" "+report.clamped+" "+report.duplicate+" "+report.malformed+" "+hexstr(saved));
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
	out, err := exec.Command(java, "-ea", "-cp", classes, "Driver", mutantHex).CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
	line := ""
	for ln := range strings.SplitSeq(string(out), "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.HasPrefix(ln, "Picked up") {
			continue
		}
		line = ln
		break
	}
	fields := strings.Fields(line)
	if len(fields) != 7 {
		t.Fatalf("driver output: %q", line)
	}
	javaMalformed := fields[5] == "true"
	javaSaved, err := hex.DecodeString(fields[6])
	if err != nil {
		t.Fatalf("saved hex: %v", err)
	}
	if fields[0] != strconv.Itoa(oreport.Unknown) {
		t.Errorf("unknown: java=%s oracle=%d", fields[0], oreport.Unknown)
	}
	if fields[1] != strconv.Itoa(oreport.KindMismatch) {
		t.Errorf("kindMismatch: java=%s oracle=%d", fields[1], oreport.KindMismatch)
	}
	if fields[2] != strconv.Itoa(oreport.Widened) {
		t.Errorf("widened: java=%s oracle=%d", fields[2], oreport.Widened)
	}
	if fields[3] != strconv.Itoa(oreport.Clamped) {
		t.Errorf("clamped: java=%s oracle=%d", fields[3], oreport.Clamped)
	}
	if fields[4] != strconv.Itoa(oreport.Duplicate) {
		t.Errorf("duplicate: java=%s oracle=%d", fields[4], oreport.Duplicate)
	}
	if javaMalformed != oreport.Malformed {
		t.Errorf("malformed: java=%v oracle=%v", javaMalformed, oreport.Malformed)
	}
	if string(javaSaved) != string(owire) {
		t.Errorf("saved bytes differ: java %d bytes, oracle %d bytes", len(javaSaved), len(owire))
	}
}
