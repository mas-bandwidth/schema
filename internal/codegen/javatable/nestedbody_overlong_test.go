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

// TestJavaFixNestedBodyOverLong pins the nested-table-body over-long read
// (docs/SPEC-TABLES.md §3): a union arm whose table body's L runs past its own
// zero terminator is malformed in both engines, and the generated Java reader
// must answer exactly as the Go oracle does — the arm reads None and the
// enclosing body continues past L.
//
// The mutant is a tabledemo.RootConfig wire (corpus root_full, pass length #36
// at seed 24845619678) whose weapons[0].effect buff arm body declares L=8 but
// terminates at byte 7. The oracle re-encodes 543 bytes and reports malformed;
// before this fix the Java leg kept the buff arm and re-encoded 557 bytes.
func TestJavaFixNestedBodyOverLong(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	dir := filepath.Join(repo, "tables", "examples")
	u := unitFromDir(t, dir)
	model := tabletext.NewModel(u)

	mutantHex := "01010c09676f6c64656e2d7631020e360d0225030a00002242040a00007a4305040700000006062d070101080f090d080a0a0000504000000d080f0b0d070c042a00000000000d0eb8010d01b4010e0c0a706c61796572206f6e650f0e0506030102fa1008090300001102f41203d08a1305000efad5feffffff1406c81507409c16098877665544332211170b0000000000000440180e120a0400000000000000000000003f000000001901011a0d511b1e1c1d0e041e021e1c1f0e051e031c002021090600000000000000220d0705040700000000230e0c0d0207030a0000803f000100240e180d020d250403000000260a0000004000072504050000000000000094d3468772c9678ab6ab7fb801d9cb41c07fb38abe08637fd2b1ac025705331732e9139e30c5314fa4edcad59a3e01a5593f2e60bc8765ad9443f2513bc8c136cc69e4e29bbeb5ffc6875c803a62dc9ad72f3de7edc5cd136969b1a27efe1381676743c01fe68181861b638ebaadbcc4f092206cc54cffdb69b9801f526d8babc6af9b2eef88501e750fca90918228b10334f58e3392dc5d185509fe81a9bd10a63f93a80ddb2c8ca66b6e19d19796fcf705bdbaf5278428f9b00c3d8aeb21b93c258e140a5e6fa0a3b5bb8675ce5957d48ac477299ba832b318320d7ec916c4c599f782764e0ad991c5c4aa397792c44925d7235af46fb48025f1eade1ae5c34ae5967084f5064b39e983f77f1331bf24cc8a11f5f028de419a244003aa01f9d131fef61816776ae6b4e78a35d1f9ee2600000000000000"
	mutant, err := hex.DecodeString(mutantHex)
	if err != nil {
		t.Fatal(err)
	}

	oracle := model.New(u.Tables["RootConfig"])
	oreport := new(tabletext.Report)
	if ok, err := tablewire.Decode(model, oracle, mutant, oreport); !ok || err != nil {
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
	root := "RootConfig"

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
