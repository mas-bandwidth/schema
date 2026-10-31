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
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestJavaFixUnionFieldRepeatedAfterShortLengthRefused pins the
// repeated-union-field read (docs/SPEC-TABLES.md §3.3): a union field (kind
// 15) that appears twice, the second occurrence's arm length so large it
// cannot be delimited, is damage, and DAMAGE DISCARDS THE WHOLE LOAD (§4): the
// generated Java reader owes the refusal and nothing it decoded is compared.
//
// The mutant is a widedemo.Caption wire (corpus seed wide_narrow_arm, pass
// random #868 at seed 24845619678) whose body union field occurs twice.
func TestJavaFixUnionFieldRepeatedAfterShortLengthRefused(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "javac and java (it builds and runs the generated Java)")
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
	if _, err := tablewire.Decode(model, oracle, mutant, oreport); err != nil || !oreport.Malformed {
		t.Fatalf("oracle does not read the mutant as damaged: %v %+v", err, oreport)
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
	for ln := range strings.SplitSeq(string(out), "\n") {
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
	if javaMalformed := fields[0] == "true" || fields[0] == "null" && fields[1] == "true"; !javaMalformed {
		t.Errorf("Java did not refuse a damaged load: %q", line)
	}
}
