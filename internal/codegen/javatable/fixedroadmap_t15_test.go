package javatable

// TestFixedRoadmapJavaT15GuardChain is the java leg's row of the card
// the-guard-chain-answers-the-outer-tag: docs/roadmap.sexp java/W16 ("nested
// union answers outer tag") and java/R28 ("the guard chain: the plan entry
// carries offset+count into a pool of (guard, arg, argw) links, outermost
// first, the only bound the layout's 64; an arm inside an arm answers to the
// OUTER tag"). Law: docs/FIXED-FORM-ALGORITHM.md §4.1.
//
// THE FIXTURE IS THE FIVE LEGS' ONE (t15GuardChainSchema, the same text in
// gotable, cstable, jstable and darttable): a union whose arm is a union whose
// arm is a union whose arm is a table, three tags deep on one path. The
// generated Java is compiled with javac and run; the deepest leaf must land as
// written and must NOT land while any one of the three tags names another arm,
// on the IDENTITY read (one copy, then the scatter) and on a plan COMPILED from
// the layout by TableFixed.compile — the plan the lineage reads a peer through.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// t15GuardChainSchema is THE fixture of the five legs' T15GuardChain tests.
// The record body is [0] Outer's tag, [1] Middle's, [2] Inner's, [3..7) x.
const t15GuardChainSchema = `package probe

type LeafA { x int32 = 0 }
type LeafB { y int32 = 0 }
union Inner
{
    a LeafA
    b LeafB
}
union Middle
{
    i Inner
    n LeafB
}
union Outer
{
    m Middle
    p LeafB
}
fixed table Root
{
    o Outer
    mid Middle
    inn Inner
}
`

func TestFixedRoadmapJavaT15GuardChain(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the JDK (it compiles and runs the generated Java)")
	rows := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"java/W16", func(t *testing.T) { t15RunJava(t, "W16", false) }},
		{"java/R28", func(t *testing.T) { t15RunJava(t, "R28", true) }},
	}
	for _, row := range rows {
		t.Run(row.id, func(t *testing.T) {
			t.Parallel()
			row.run(t)
		})
	}
}

// t15JavaProbe is the driven reader. chain asserts, on the compiled plan, the
// deepest entry's offset+count into the pool, its links OUTERMOST FIRST and its
// two lanes the chain's innermost two.
func t15JavaProbe(class string, chain bool) string {
	check := ""
	if chain {
		check = `
        boolean found = false;
        for (int i = 0; i < made; i++) {
            probe.TableFixed.Entry e = plan[i];
            if (e.gcount != 3) { continue; }
            found = true;
            if (e.gpool == null || 2 * (e.gindex + 3) > e.gpool.length) { bad += fail("the chain's offset+count point outside the pool"); break; }
            for (int j = 0; j < 3; j++) {
                long gl = e.gpool[2 * (e.gindex + j)];
                long arg = e.gpool[2 * (e.gindex + j) + 1];
                if ((int) gl != j || arg != 1 || ((gl >>> 32) & 0xFF) != 1) { bad += fail("link " + j + " is not (" + j + ", 1, 1): the chain is not OUTERMOST FIRST"); }
            }
            if (e.guard != 2 || e.guard2 != 1) { bad += fail("the two lanes are not the chain's innermost two"); }
            break; // the first three-link entry is arm a's, in push order
        }
        if (!found) { bad += fail("no compiled entry carries a three-link chain (offset+count into the pool)"); }
`
	}
	return fmt.Sprintf(`public final class %s {
    static int fail(String what) { System.out.println("FAIL " + what); return 1; }

    public static void main(String[] args) throws Exception {
        int bad = 0;
        probe.RootFixed.Value v = new probe.RootFixed.Value();
        v.o.type = probe.OuterFixed.m;
        v.o.m.type = probe.MiddleFixed.i;
        v.o.m.i.type = probe.InnerFixed.a;
        v.o.m.i.a.x = 42;
        probe.RootFixed.Value[] vals = { v };
        byte[] buf = new byte[probe.RootFixed.measure(1)];
        probe.RootFixed.save(vals, 1, buf);
        int rec = probe.RootFixed.headerBytes + 8;
        if (buf[rec] != 1 || buf[rec + 1] != 1 || buf[rec + 2] != 1) { bad += fail("the writer did not put the three tags where the chain says"); }

        for (int poison = 0; poison <= 3; poison++) {
            byte[] file = buf.clone();
            if (poison > 0) { file[rec + poison - 1] = 2; }
            probe.RootFixed.Value[] back = { new probe.RootFixed.Value() };
            probe.TableFixed.Report r = new probe.TableFixed.Report();
            int n = probe.RootFixed.load(back, 1, file, probe.TableFixed.plan(1 << 12), new short[1 << 13], probe.RootFixed.image(), r);
            if (n != 1 || r.refused) { bad += fail("identity poison " + poison + ": n=" + n); continue; }
            int x = back[0].o.m.i.a.x;
            if (poison == 0 && x != 42) { bad += fail("identity: the record as written did not land its deepest leaf"); }
            if (poison > 0 && x == 42) { bad += fail("identity: the deepest leaf landed while tag " + (poison - 1) + " names another arm"); }
        }

        byte[] lay = probe.RootFixed.known[probe.RootFixed.known.length - 1].layout;
        probe.TableFixed.Report cr = new probe.TableFixed.Report();
        probe.TableFixed.Layout layout = probe.TableFixed.parse(lay, 0, lay.length, cr);
        if (layout == null) { bad += fail("the layout does not parse"); }
        probe.TableFixed.Entry[] plan = probe.TableFixed.plan(1 << 12);
        short[] remap = new short[1 << 13];
        int made = layout == null ? -1 : probe.TableFixed.compile(layout, layout, probe.RootFixed.dest, plan, remap, cr);
        if (made <= 0) { bad += fail("the compiled plan refused a three-link chain: made=" + made + " refused=" + cr.refused); }
%s
        for (int poison = 0; made > 0 && poison <= 3; poison++) {
            byte[] file = buf.clone();
            if (poison > 0) { file[rec + poison - 1] = 2; }
            byte[] image = probe.RootFixed.image();
            probe.TableFixed.Report r = new probe.TableFixed.Report();
            probe.TableFixed.run(plan, made, remap, file, rec, probe.RootFixed.bodyBytes, image, r);
            if (r.malformed || r.refused) { bad += fail("compiled poison " + poison + ": malformed=" + r.malformed); continue; }
            probe.RootFixed.Value back = new probe.RootFixed.Value();
            probe.RootFixed.scatter(image, 0, back, r);
            int x = back.o.m.i.a.x;
            if (poison == 0 && x != 42) { bad += fail("compiled: the record as written did not land its deepest leaf"); }
            if (poison > 0 && x == 42) { bad += fail("compiled: the deepest leaf landed while tag " + (poison - 1) + " names another arm"); }
        }
        System.out.println(bad == 0 ? "PROBE OK" : "FAILED " + bad);
    }
}
`, class, check)
}

// t15RunJava generates the fixture's fixed-form Java, compiles it with javac
// beside the probe and runs it, failing with the probe's words on anything but
// PROBE OK. It fails, never skips.
func t15RunJava(t *testing.T, id string, chain bool) {
	t.Helper()
	u := unitFrom(t, t15GuardChainSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	writeFixedJava(t, src, javaPackageOf(u), files)
	class := "Probe_t15_" + id
	if err := os.WriteFile(filepath.Join(src, class+".java"), []byte(t15JavaProbe(class, chain)), 0o644); err != nil {
		t.Fatal(err)
	}
	javac, javaBin := javaTools(t)
	classes := filepath.Join(dir, "classes")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	sources, _ := filepath.Glob(filepath.Join(src, "*", "*.java"))
	args := append([]string{"--release", "17", "-nowarn", "-d", classes}, sources...)
	args = append(args, filepath.Join(src, class+".java"))
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	out, err := runJava(t, javaBin, classes, class)
	if err != nil || strings.Contains(out, "FAIL ") || !strings.Contains(out, "PROBE OK") {
		t.Fatalf("the driven reader failed (%v):\n%s", err, out)
	}
}
