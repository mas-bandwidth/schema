package javatable

import (
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// idTableProbeLoads is enough loads of a 4096-entry trailer that a pairwise
// distinctness walk cannot finish inside the gate's 90 s test timeout, while
// the fixed 8192-slot probe finishes far inside a quarter of it.
const idTableProbeLoads = 6000

// TestJavaIdTableDistinctProbeFunctional holds the Java file reader's
// distinctness check to the bounded walk of docs/SPEC-TABLES.md §3: the id
// table's entries are proved distinct by one open-addressing probe over a
// fixed 8192-slot table, and a load proves them distinct once rather than
// again in its measure. A 4096-entry trailer therefore loads many times fast;
// a repeated id and an entry count over the 4096 bound are damaged; ids that
// collide in the probe's low bits stay legal. Every verdict is the engine's
// (internal/tablewire) on the same bytes.
func TestJavaIdTableDistinctProbeFunctional(t *testing.T) {
	t.Parallel()
	javac, javaBin := javaToolsRequired(t)

	u := unitFrom(t, `package probe
table Root { value int32 }
`)
	model := tabletext.NewModel(u)
	root := "Root"
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	pkg := javaPackageOf(u)

	// A form-1 file whose root body is the terminator and whose trailer is the
	// given ids and their little-endian count (§3).
	wire := func(ids []uint64) []byte {
		out := make([]byte, 0, 2+8*len(ids)+8)
		out = append(out, 1, 0)
		for _, id := range ids {
			out = binary.LittleEndian.AppendUint64(out, id)
		}
		return binary.LittleEndian.AppendUint64(out, uint64(len(ids)))
	}
	probeSlot := func(id uint64) uint32 {
		hash := id
		hash ^= hash >> 33
		hash *= 0xff51afd7ed558ccd
		hash ^= hash >> 33
		return uint32(hash) & 8191
	}

	distinct := make([]uint64, 4096)
	for i := range distinct {
		distinct[i] = uint64(i + 1)
	}
	repeated := append([]uint64(nil), distinct...)
	repeated[len(repeated)-1] = repeated[0]
	over := make([]uint64, 4097)
	for i := range over {
		over[i] = uint64(i + 1)
	}
	// Ids that all land in one probe slot still read as distinct entries: the
	// probe resolves a collision by comparing the stored id, never the hash.
	collide := []uint64{}
	for id := uint64(1); len(collide) < 64; id++ {
		if probeSlot(id) == 0 {
			collide = append(collide, id)
		}
	}

	wires := map[string][]byte{
		"distinct": wire(distinct),
		"repeated": wire(repeated),
		"over":     wire(over),
		"collide":  wire(collide),
	}
	// The engine is the verdict reference for these same bytes.
	engineLoads := map[string]bool{}
	for name, data := range wires {
		rep := new(tabletext.Report)
		ok, err := tablewire.Decode(model, model.New(u.Tables[root]), data, rep)
		engineLoads[name] = ok && err == nil && !rep.Refused
	}
	if !engineLoads["distinct"] || !engineLoads["collide"] {
		t.Fatalf("engine verdicts: distinct=%t collide=%t, want both legal", engineLoads["distinct"], engineLoads["collide"])
	}
	if engineLoads["repeated"] || engineLoads["over"] {
		t.Fatalf("engine verdicts: repeated=%t over=%t, want both refused", engineLoads["repeated"], engineLoads["over"])
	}

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
	for name, data := range wires {
		if err := os.WriteFile(filepath.Join(td, name+".bin"), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	driver := filepath.Join(td, "IdProbeDriver.java")
	dsrc := fmt.Sprintf(`import java.nio.file.Files;
import java.nio.file.Paths;

public final class IdProbeDriver {
    static byte[] read(String path) throws Exception {
        return Files.readAllBytes(Paths.get(path));
    }

    static String verdict(byte[] data) {
        %[1]s.TableVariable.Report report = new %[1]s.TableVariable.Report();
        %[1]s.%[2]sVariable.Value v = %[1]s.%[2]sVariable.load(data, report);
        if (v != null && !report.damaged && !report.refused) {
            return "loaded";
        }
        if (report.damaged) {
            return "damaged";
        }
        return "refused";
    }

    public static void main(String[] args) throws Exception {
        String dir = args[0];
        int n = Integer.parseInt(args[1]);
        byte[] distinct = read(dir + "/distinct.bin");
        long start = System.nanoTime();
        for (int i = 0; i < n; i++) {
            if (!"loaded".equals(verdict(distinct))) {
                System.out.println("distinct failed at " + i);
                System.exit(1);
            }
        }
        long ms = (System.nanoTime() - start) / 1000000L;
        System.out.println("distinct loaded " + ms);
        System.out.println("repeated " + verdict(read(dir + "/repeated.bin")));
        System.out.println("over " + verdict(read(dir + "/over.bin")));
        System.out.println("collide " + verdict(read(dir + "/collide.bin")));
    }
}
`, pkg, root)
	if err := os.WriteFile(driver, []byte(dsrc), 0o644); err != nil {
		t.Fatal(err)
	}
	sources = append(sources, driver)

	args := append([]string{"--release", "17", "-d", classes}, sources...)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	out, err := exec.Command(javaBin, "-ea", "-cp", classes, "IdProbeDriver",
		td, strconv.Itoa(idTableProbeLoads)).CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}

	got := map[string]string{}
	distinctMillis := int64(-1)
	for line := range strings.SplitSeq(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if fields[0] == "distinct" && fields[1] == "loaded" && len(fields) == 3 {
			distinctMillis, err = strconv.ParseInt(fields[2], 10, 64)
			if err != nil {
				t.Fatalf("distinct timing %q: %v", fields[2], err)
			}
			got["distinct"] = "loaded"
			continue
		}
		if _, named := wires[fields[0]]; named {
			got[fields[0]] = fields[1]
		}
	}
	if distinctMillis < 0 {
		t.Fatalf("no distinct timing in output:\n%s", out)
	}
	t.Logf("%d distinct loads in %d ms", idTableProbeLoads, distinctMillis)

	for name := range wires {
		loaded := got[name] == "loaded"
		if loaded != engineLoads[name] {
			t.Errorf("%s: Java verdict %q, engine loads=%t", name, got[name], engineLoads[name])
		}
	}
	// The repeated id and the count over the 4096 bound are DAMAGE (§3).
	for _, name := range []string{"repeated", "over"} {
		if got[name] != "damaged" {
			t.Errorf("%s: Java verdict %q, want damaged", name, got[name])
		}
	}
}
