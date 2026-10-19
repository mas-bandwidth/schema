package javatable

import (
	"crypto/sha256"
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

// TestJavaFixListCountPastBodyInMapValue pins the wire-fuzzer cause J14: a
// LIST COUNT INSIDE A MAP VALUE runs past its body. On mapdemo.Spans the map
// value is an unbounded []Item, so the list header lives inside the entry
// body. The oracle's framing walk (internal/tablewire/extent.go
// FileRegionMeasure) reaches that header whatever the decode does and refuses
// the count as count_over_length (docs/SPEC-TABLES.md §6.5, §2.8, §2.9). The
// Java load stopped at the entry's damaged key scan before it reached the
// list, and loaded the file malformed. It must refuse exactly where the
// oracle's framing refuses, and keep answering the clean wire as the oracle
// does (saved bytes and every report counter).
func TestJavaFixListCountPastBodyInMapValue(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	u := unitFromDir(t, filepath.Join(repo, "tables", "maps"))
	model := tabletext.NewModel(u)
	const root = "Spans"
	clean, err := os.ReadFile(filepath.Join(repo, "testdata", "wire", "tables", "map_spans.bin"))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, wireHex, sha string
	}{
		// the clean corpus seed map_spans: loads, and saves what the oracle saves.
		{"clean", hex.EncodeToString(clean), ""},
		// the fuzzer's random pass at run seed 24845619678 (harness replay:
		// --unit mapdemo --root Spans): an insertion drives the first entry's
		// tracks-list count to 0x35 inside a 0x1a list body AND leaves the
		// entry's key scan one byte short, so the read stopped before the list.
		{"keyscan-stops-first", "01010e3e0d0221020601030e1a0d354b40fa06d491030704040a000000000704040b000000000704040c000000000019020608030e120d02070404500000000007040451000000000005040400000000bd0f479c60325375ec105b36194ac93dea0ce83094fde47c74a279448ee2e5b1e9ea716f0f0182bf0500000000000000", "d57d43710dec3bc9b77aa4298d01763c07592afa84a8140e8004bd8e85f9d9c0"},
		// the same cause in an INTACT entry: only the list count (byte 14) is
		// raised from 3 to 255, the key scan does not stop and the list load
		// itself refuses.
		{"intact-entry", "01010e3e0d0221020601030e1a0dff0704040a000000000704040b000000000704040c000000000019020608030e120d02070404500000000007040451000000000005040400000000bd0f479c60325375ec105b36194ac93dea0ce83094fde47c74a279448ee2e5b1e9ea716f0f0182bf0500000000000000", "8d172fc24a366c769e52ce987562475494cdbcfa0839014a6837337a057b973c"},
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
		if tc.sha != "" {
			sum := sha256.Sum256(wire)
			if got := hex.EncodeToString(sum[:]); got != tc.sha {
				t.Fatalf("%s: mutant sha256 = %s, want %s", tc.name, got, tc.sha)
			}
		}
		out, err := exec.Command(java, "-ea", "-cp", classes, "Driver", tc.wireHex).CombinedOutput()
		if err != nil {
			t.Fatalf("%s: java: %v\n%s", tc.name, err, out)
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
			t.Fatalf("%s: driver output %q", tc.name, out)
		}
		measure, saved, counters := strings.TrimPrefix(parts[0], "measure="), parts[1], parts[2]

		// The oracle: its framing walk decides refusal before any value is
		// decoded (FileRegionMeasure); a file it does not refuse is decoded
		// and re-encoded, and the Java must save the same bytes and report the
		// same counters.
		_, _, refused := tablewire.FileRegionMeasure(u, u.Tables[root], wire)
		if refused {
			if tc.name == "clean" {
				t.Fatalf("the oracle refuses the clean map_spans wire")
			}
			if measure != "-1" {
				t.Errorf("%s: Java loadMeasure = %s, the oracle framing refuses (-1)", tc.name, measure)
			}
			if saved != "null" {
				t.Errorf("%s: Java load accepted a list count past its body inside a map value (report %s); the oracle refuses (encoded=refused)", tc.name, counters)
			}
			continue
		}
		if tc.name != "clean" {
			t.Fatalf("%s: the oracle framing did not refuse: the bytes are not of this cause", tc.name)
		}
		oracle := model.New(u.Tables[root])
		oreport := new(tabletext.Report)
		if _, err := tablewire.Decode(model, oracle, wire, oreport); err != nil {
			t.Fatalf("%s: oracle decode: %v %+v", tc.name, err, oreport)
		}
		owire, err := tablewire.Encode(model, oracle)
		if err != nil {
			t.Fatal(err)
		}
		want := fmt.Sprintf("%d,%d,%d,%d,%d,%v,%v", oreport.Unknown, oreport.KindMismatch, oreport.Widened, oreport.Clamped, oreport.Duplicate, oreport.Malformed, oreport.Refused)
		if counters != want {
			t.Errorf("%s: report java=%s oracle=%s", tc.name, counters, want)
		}
		if saved != hex.EncodeToString(owire) {
			t.Errorf("%s: saved bytes differ: java %s, oracle %x", tc.name, saved, owire)
		}
	}
}
