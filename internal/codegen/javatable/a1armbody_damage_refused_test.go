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

// TestJavaFixA1ArmBodyAsA2DamageRefused pins the Java read of a tbla2.Root
// whose union header occurrence is damaged: DAMAGE DISCARDS THE WHOLE LOAD
// (docs/SPEC-TABLES.md §4), so the leg owes the refusal, malformed, and no
// counter or saved byte is compared.
//
// The mutant is the representative random pass #6001 at seed 24845619678 on
// corpus seed a1_arm_body_as_a2 (tbla2.Root).
func TestJavaFixA1ArmBodyAsA2DamageRefused(t *testing.T) {
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
        if(v==null){System.out.println("null "+report.malformed);return;}
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
	if javaMalformed := len(fields) == 7 && fields[5] == "true" || len(fields) == 2 && fields[0] == "null" && fields[1] == "true"; !javaMalformed {
		t.Errorf("Java did not refuse a damaged load: %q", line)
	}
}
