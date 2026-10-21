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

// TestJavaFixTblwFleetKindPass pins the wire-fuzzer kind #140 mutant on both
// tblw1.Fleet (test/tables/W1.schema, corpus seed w1_fleet) and tblw2.Fleet
// (test/tables/W2.schema, corpus seed w2_fleet). At kind #140, the root
// node-table field (id 0xFFFFFFFFFFFFFFFF, TableNodeWireId, at byte offset 21)
// has its wire kind byte mutated from 12 (TableKindString) to 0. Under
// SPEC-TABLES §3.1 and §3.3 (internal/tablewire/decode.go, decodenodes.go), a
// node-table field arriving under a kind other than 12 marks the numbering bad
// and counts malformed, so every pointer in the save resolves to null while a
// decoded pointer array (Fleet.escorts, [..4]*Vessel / [..4]*Ship, count 2)
// keeps its 2 decoded null-pointer slots ([null, null]). The generated Java
// reader must answer byte-for-byte as the Go oracle does on both value and
// report.
func TestJavaFixTblwFleetKindPass(t *testing.T) {
	t.Parallel()
	// Exact kind #140 mutant bytes (170 bytes, sha256
	// 66b60d6e9d158cdabb0cf777b921f25bb810af40dcaf2260855b96840dafb53f):
	// byte 21 (the kind byte of field ref 4, TableNodeWireId) changed from
	// 0x0c (kind 12) to 0x00 (kind 0).
	const kind140Hex = "01010c0661726d616461021102030e041102030204003202052c060c064175726f7261070e0506037a7a7108090200000000000000090d070a0c03766970000b04fa000000000501000029601b0c6c2931da5d81b3a4c4a0df6392e316b22ebbe524ffffffffffffffff7b7b5dc28e0c0d11861b638ebaadbcc4f3a4484419abd756f85775e8906b5ab5185509fe81a9bd103d62cb8fecfcf739f6adda11cc8cda800b00000000000000"
	wire, err := hex.DecodeString(kind140Hex)
	if err != nil {
		t.Fatal(err)
	}

	repo := filepath.Join("..", "..", "..")
	units := []struct {
		name   string
		schema string
		root   string
	}{
		{"tblw1.Fleet", filepath.Join(repo, "test", "tables", "W1.schema"), "Fleet"},
		{"tblw2.Fleet", filepath.Join(repo, "test", "tables", "W2.schema"), "Fleet"},
	}

	javac, java := javaTools(t)
	for _, tc := range units {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			u := unitFromPath(t, tc.schema)
			model := tabletext.NewModel(u)

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
}`, pkg, pkg, pkg, tc.root, pkg, tc.root, pkg, tc.root)
			if err := os.WriteFile(driver, []byte(dsrc), 0o644); err != nil {
				t.Fatal(err)
			}
			sources = append(sources, driver)
			args := append([]string{"--release", "17", "-d", classes}, sources...)
			if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
				t.Fatalf("javac: %v\n%s", err, out)
			}

			out, err := exec.Command(java, "-ea", "-cp", classes, "Driver", kind140Hex).CombinedOutput()
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
			fields := strings.SplitN(line, "|", 2)
			if len(fields) != 2 {
				t.Fatalf("driver output %q", line)
			}

			oracle := model.New(u.Tables[tc.root])
			oreport := new(tabletext.Report)
			if _, err := tablewire.DecodeRegion(model, oracle, wire, oreport); err != nil {
				t.Fatalf("oracle decode: %v %+v", err, oreport)
			}
			owire, err := tablewire.Encode(model, oracle)
			if err != nil {
				t.Fatal(err)
			}
			javaSaved, err := hex.DecodeString(fields[1])
			if err != nil {
				t.Fatalf("saved hex: %v", err)
			}
			got := fields[0]
			want := fmt.Sprintf("%d,%d,%d,%d,%d,%v,%v", oreport.Unknown, oreport.KindMismatch, oreport.Widened, oreport.Clamped, oreport.Duplicate, oreport.Malformed, oreport.Refused)
			if got != want {
				t.Errorf("report java=%s oracle=%s", got, want)
			}
			if string(javaSaved) != string(owire) {
				t.Errorf("saved bytes differ: java %d bytes (%x), oracle %d bytes (%x)", len(javaSaved), javaSaved, len(owire), owire)
			}
		})
	}
}
