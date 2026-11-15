package javatable

// REFUSE IS TOTAL OVER THE BATCH (docs/FIXED-FORM-ALGORITHM.md §5.3, §7 item
// 3): the generated Java load checks every record — its hash, its live
// counts, lengths and text — before the first record lands, so a refusal at
// record k leaves out[0..k-1] exactly as untouched as out[k]. The caller's
// values are filled with a sentinel, a two-record file is forged twice (the
// second record's hash, then the second record's count past its bound), and
// every value must still hold its sentinel after each refusal.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const refusalUntouchedSchema = `package probe

fixed table Pair
{
    marks [..4]int32
    seq   int32
}
`

const refusalUntouchedDriver = `import @PKG@.PairFixed;
import @PKG@.TableFixed;

public final class Untouched {
    private Untouched() {}

    static final int sentinel = 0x5A5A5A5A;

    static PairFixed.Value[] sentinels() {
        final PairFixed.Value[] v = { new PairFixed.Value(), new PairFixed.Value() };
        for (PairFixed.Value x : v) {
            x.seq = sentinel;
            x.marksCount = 3;
            java.util.Arrays.fill(x.marks, sentinel);
        }
        return v;
    }

    static String untouched(PairFixed.Value[] v) {
        for (int k = 0; k < v.length; k++) {
            if (v[k].seq != sentinel) { return "values[" + k + "].seq=" + v[k].seq; }
            if (v[k].marksCount != 3) { return "values[" + k + "].marksCount=" + v[k].marksCount; }
            for (int i = 0; i < v[k].marks.length; i++) {
                if (v[k].marks[i] != sentinel) { return "values[" + k + "].marks[" + i + "]=" + v[k].marks[i]; }
            }
        }
        return "";
    }

    static int load(PairFixed.Value[] v, byte[] file, TableFixed.Report r) {
        return PairFixed.load(v, v.length, file, TableFixed.plan(1 << 10), new short[1 << 10], PairFixed.image(), r);
    }

    public static void main(String[] args) {
        final PairFixed.Value[] src = { new PairFixed.Value(), new PairFixed.Value() };
        src[0].seq = 1;
        src[0].marksCount = 1;
        src[0].marks[0] = 10;
        src[1].seq = 2;
        src[1].marksCount = 2;
        src[1].marks[0] = 20;
        src[1].marks[1] = 21;
        final byte[] file = new byte[PairFixed.measure(2)];
        if (PairFixed.save(src, 2, file) < 0) { System.out.println("FAILED save"); return; }

        // the control: the file as written reads both records
        final PairFixed.Value[] good = sentinels();
        final TableFixed.Report gr = new TableFixed.Report();
        final int gn = load(good, file, gr);
        if (gn != 2 || gr.refused || gr.damaged || good[0].seq != 1 || good[1].seq != 2) {
            System.out.println("FAILED control n=" + gn + " refused=" + gr.refused + " damaged=" + gr.damaged);
            return;
        }

        final int second = PairFixed.headerBytes + PairFixed.recordBytes;

        final byte[] badHash = file.clone();
        for (int i = 0; i < 8; i++) { badHash[second + i] ^= (byte) 0xFF; }
        report("hash", badHash);

        final byte[] badCount = file.clone();
        TableFixed.put32(badCount, second + 8, 99);
        report("count", badCount);
    }

    static void report(String what, byte[] file) {
        final PairFixed.Value[] v = sentinels();
        final TableFixed.Report r = new TableFixed.Report();
        final int n = load(v, file, r);
        System.out.println("CASE " + what + " n=" + n + " refused=" + (r.refused || r.damaged) + " widened=" + r.widened + " untouched=" + untouched(v));
    }
}
`

func TestFixedRefusalLeavesDestination(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "javac and java (it builds and runs the generated Java)")
	javac, javaBin := javaToolsRequired(t)

	u := unitFrom(t, refusalUntouchedSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	classes := filepath.Join(dir, "classes")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	pkg := u.Package
	writeFixedJava(t, src, pkg, files)
	driver := strings.ReplaceAll(refusalUntouchedDriver, "@PKG@", pkg)
	if err := os.WriteFile(filepath.Join(src, "Untouched.java"), []byte(driver), 0o644); err != nil {
		t.Fatal(err)
	}
	pkgSrc, err := filepath.Glob(filepath.Join(src, pkg, "*.java"))
	if err != nil {
		t.Fatal(err)
	}
	args := append([]string{"--release", "17", "-d", classes}, pkgSrc...)
	args = append(args, filepath.Join(src, "Untouched.java"))
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	out, err := exec.Command(javaBin, "-ea", "-cp", classes, "Untouched").CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
	got := map[string]string{}
	for ln := range strings.SplitSeq(string(out), "\n") {
		ln = strings.TrimSpace(ln)
		if strings.HasPrefix(ln, "FAILED") {
			t.Fatalf("driver: %s", ln)
		}
		if rest, ok := strings.CutPrefix(ln, "CASE "); ok {
			name, line, _ := strings.Cut(rest, " ")
			got[name] = line
		}
	}
	for _, name := range []string{"hash", "count"} {
		line, ok := got[name]
		if !ok {
			t.Errorf("%s: the driver printed no case\n%s", name, out)
			continue
		}
		if want := "n=-1 refused=true widened=0 untouched="; line != want {
			t.Errorf("%s: %s, want %s (every value still holds its sentinel)", name, line, want)
		}
	}
}
