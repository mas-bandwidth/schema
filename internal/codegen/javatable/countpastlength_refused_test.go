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

// TestJavaFixCountPastLengthRefused pins the wire-fuzzer cause J3 on
// streamdemo.Feed (corpus seed stream_chain): an ARRAY COUNT past the array's
// own L but inside the enclosing body is a count the body cannot cover, which
// is damage, and DAMAGE DISCARDS THE WHOLE LOAD (docs/SPEC-TABLES.md §4): the
// generated Java reader owes the refusal, malformed, and no counter or saved
// byte is compared.
func TestJavaFixCountPastLengthRefused(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "javac and java (it builds and runs the generated Java)")
	repo := filepath.Join("..", "..", "..")
	dir := filepath.Join(repo, "tables", "stream")
	u := unitFromDir(t, dir)
	model := tabletext.NewModel(u)
	root := "Feed"

	cases := []struct{ name, wireHex string }{
		// the pinned stream_count_past_body vector: Chunk.data's count runs
		// past its array L, a count the body cannot cover.
		{"pinned", "01010803000000020f030d0a040e03068fa99e02de62cf010105110200060c1602070a040e03060102051103000707040e030601030000c03a5cb5072eb7082652b3285633d4d8228e3c877681830f054aa33067555b8528f025a0ba6c31e5ffffffffffffffff82e051f9dc6843cf0700000000000000"},
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
        System.out.println(report.unknown+","+report.kindMismatch+","+report.widened+","+report.clamped+","+report.duplicate+","+report.malformed+","+report.refused+"|"+hexstr(saved));
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

	for _, tc := range cases {
		wire, err := hex.DecodeString(tc.wireHex)
		if err != nil {
			t.Fatal(err)
		}
		out, err := exec.Command(java, "-ea", "-cp", classes, "Driver", tc.wireHex).CombinedOutput()
		if err != nil {
			t.Fatalf("%s: java: %v\n%s", tc.name, err, out)
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
		oracle := model.New(u.Tables[root])
		oreport := new(tabletext.Report)
		if _, err := tablewire.Decode(model, oracle, wire, oreport); err != nil || !oreport.Malformed {
			t.Fatalf("%s: oracle does not read the input as damaged: %v %+v", tc.name, err, oreport)
		}
		counts := strings.TrimPrefix(strings.SplitN(line, "|", 2)[0], "null ")
		if parts := strings.Split(counts, ","); len(parts) != 7 || parts[5] != "true" {
			t.Errorf("%s: Java did not refuse a damaged load: %q", tc.name, line)
		}
	}
}
