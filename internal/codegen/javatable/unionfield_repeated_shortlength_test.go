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

// TestJavaFixUnionFieldRepeatedAfterShortLength pins the repeated-union-field
// read (docs/SPEC-TABLES.md §3.3): a union field (kind 15) that appears twice,
// the second occurrence's arm length so large it cannot be delimited, is
// malformed in both engines, and the generated Java reader must answer exactly
// as the Go oracle does — the fields decoded before the damage stand, so the
// first occurrence's arm is kept and the enclosing body stops past the damage.
//
// The mutant is a widedemo.Caption wire (corpus seed wide_narrow_arm, pass
// random #868 at seed 24845619678) whose body union field occurs twice. The
// oracle keeps the first narrow="ok" arm and re-encodes 33 bytes, reporting
// malformed; before this fix the Java leg dropped the arm, saved only 10 bytes
// and lost the decoded value.
func TestJavaFixUnionFieldRepeatedAfterShortLength(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	dir := filepath.Join(repo, "examples-wide")
	u := unitFromDir(t, dir)
	model := tabletext.NewModel(u)

	mutantHex := "01010f020c026f6b010f02abf47b65e6b30c026f6b009532c9c69be74dcd9ac223f2069b56960200000000000000"
	mutant, err := hex.DecodeString(mutantHex)
	if err != nil {
		t.Fatal(err)
	}

	oracle := model.New(u.Tables["Caption"])
	oreport := new(tabletext.Report)
	if _, err := tablewire.Decode(model, oracle, mutant, oreport); err != nil {
		t.Fatalf("oracle decode: %v %+v", err, oreport)
	}
	owire, err := tablewire.Encode(model, oracle)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("oracle: malformed=%v saved=%d", oreport.Malformed, len(owire))

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
	root := "Caption"

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
        if(v==null){System.out.println("null "+report.malformed);return;}
        byte[] saved=%s.%sVariable.save(v);
        System.out.println(report.malformed+" "+hexstr(saved));
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
	for _, ln := range strings.Split(string(out), "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.HasPrefix(ln, "Picked up") {
			continue
		}
		line = ln
		break
	}
	fields := strings.Fields(line)
	if len(fields) != 2 {
		t.Fatalf("driver output: %q", line)
	}
	javaMalformed := fields[0] == "true"
	javaSaved, err := hex.DecodeString(fields[1])
	if err != nil {
		t.Fatalf("saved hex: %v", err)
	}
	if javaMalformed != oreport.Malformed {
		t.Errorf("malformed: java=%v oracle=%v (saved %d vs oracle %d)", javaMalformed, oreport.Malformed, len(javaSaved), len(owire))
	}
	if string(javaSaved) != string(owire) {
		t.Errorf("saved bytes differ: java %d bytes, oracle %d bytes", len(javaSaved), len(owire))
	}
}
