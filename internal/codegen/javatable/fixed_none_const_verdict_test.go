package javatable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// THE JAVA None CONST LANDS None BEFORE IT REFUSES A FORGED TAG, AND THE
// VERDICT IS STILL outOfRange.
//
// THE LAW: docs/FIXED-FORM-ALGORITHM.md §4.6 — a union tag past the WRITER's
// arm set is forged and refuses out_of_range; §5.2 EMIT kind 15 — the None
// const reads the writer's raw tag at the writer's tag width. A refusal is
// compared by its VERDICT only (the load answers no value and names the
// refusal); what a refused run left in the reader's scratch image is not
// compared on any leg, and Java is a debug-assert leg.
//
// WHAT THIS HOLDS: run's opConst case stores the None (putUint over my tag)
// FIRST and tests the writer's raw tag against the writer's arm count after,
// so a forged tag leaves my tag's bytes written to 0 in the scratch image and
// the report refused outOfRange. The row asserts the verdict on the direct run
// (refused, outOfRange, nothing counted) and through the lineage lane laid at
// class initialization; the order it observes is printed, never asserted, so a
// leg that moves the check first (as §4.6 and the JavaScript leg have it) stays
// green. Only the R14 vector's one-arm union is read here: Java, like C, C++,
// C# and Dart, carries the writer's arm count in one byte (a union past 255
// arms is not held to it), and on a debug-assert leg that is not a defect. The vector is R14's (test/conformance/java/rows/R14.java): the
// writer's record of 5 bytes, one union of a 1-byte tag over a u32, read by
// a reader whose tag grew to 8.
//
// THE PLANT: delete `report.refuse(Reason.outOfRange);` from run's opConst case
// and the forged tag lands None and reads: red by name.
func TestJavaFixedNoneConstForgedTagVerdictIsOutOfRange(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, "package probe\n\nfixed table Host\n{\n    keep uint32\n}\n")
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	td := t.TempDir()
	classes := filepath.Join(td, "classes")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	var sources []string
	for name, data := range files {
		if strings.Contains(name, "/") || !strings.HasSuffix(name, ".java") {
			continue
		}
		p := filepath.Join(td, name)
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, p)
	}
	pkg := javaPackageOf(u)
	driver := `import ` + pkg + `.TableFixed;

public final class Driver {
    static int failures = 0;
    static void check(boolean ok, String line) {
        if (!ok) { System.out.println("FAIL: " + line); failures++; }
    }
    static byte[] block(long[][] entries) {
        final byte[] b = new byte[TableFixed.layoutHeaderBytes + entries.length * TableFixed.entryBytes];
        TableFixed.put32(b, 0, entries.length);
        for (int i = 0; i < entries.length; i++) {
            final int at = TableFixed.layoutHeaderBytes + i * TableFixed.entryBytes;
            TableFixed.put64(b, at, entries[i][0]);
            b[at + 8] = (byte) entries[i][1];
            TableFixed.put32(b, at + 9, (int) entries[i][2]);
            TableFixed.put32(b, at + 13, (int) entries[i][3]);
        }
        return b;
    }
    static final byte[] THEIRS = block(new long[][] {{1, 13, 5, 1}, {7, 15, 5, 1}, {9, 8, 4, 0}});
    static final byte[] MINE = block(new long[][] {{1, 13, 12, 1}, {7, 15, 12, 1}, {9, 8, 4, 0}});
    static final int[] DEST = {0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 8, 0, 0, 0, 0};

    public static void main(String[] args) {
        final TableFixed.Report pr = new TableFixed.Report();
        final TableFixed.Layout t = TableFixed.parse(THEIRS, 0, THEIRS.length, pr);
        final TableFixed.Layout m = TableFixed.parse(MINE, 0, MINE.length, pr);
        check(t != null && m != null && !pr.refused, "both layouts parse: " + pr.reason);
        final TableFixed.Entry[] plan = TableFixed.plan(256);
        final short[] remap = new short[1024];
        final TableFixed.Report cr = new TableFixed.Report();
        final int made = TableFixed.compile(t, m, DEST, plan, remap, cr);
        check(made > 0 && !cr.refused, "THEIRS vs MINE(8) compiles whole: " + made + " " + cr.reason);

        // 1. THE CONTROL: arm 1 reads, so the refusal below is the forged tag.
        {
            final byte[] image = new byte[12];
            final TableFixed.Report r = new TableFixed.Report();
            TableFixed.run(plan, made, remap, new byte[] {1, 0x44, 0x33, 0x22, 0x11}, 0, 5, image, r);
            check(!r.refused && TableFixed.get64(image, 0) == 1L && TableFixed.get32(image, 8) == 0x11223344,
                    "arm 1 reads: refused=" + r.refused + " " + r.reason);
        }
        // 2. THE FORGED TAG over a POISONED image: the verdict is outOfRange.
        for (int forged : new int[] {2, 0x7f, 0xff}) {
            final byte[] image = new byte[12];
            java.util.Arrays.fill(image, (byte) 0x5A);
            final TableFixed.Report r = new TableFixed.Report();
            TableFixed.run(plan, made, remap, new byte[] {(byte) forged, 0, 0, 0, 0}, 0, 5, image, r);
            check(r.refused && r.reason == TableFixed.Reason.outOfRange,
                    "a forged writer tag " + forged + " refuses outOfRange, got refused=" + r.refused + " " + r.reason);
            check(r.unknown == 0 && r.widened == 0, "the forged tag " + forged + " counts nothing");
            // THE ORDER is observed and printed, never asserted: the verdict is
            // the contract, and a leg that moves the check first stays green.
            System.out.println("order: forged " + forged + " left my scratch tag 0x"
                    + Long.toHexString(TableFixed.get64(image, 0)) + " (0 = the None landed before the refusal)");
        }
        // 3. THE PRODUCTION LANE: the lineage plan laid at class init, run by
        //    the same loop, refuses the same forged bytes by the same name.
        final TableFixed.KnownLayout[] known = {new TableFixed.KnownLayout(0x4444444433333333L, THEIRS, THEIRS.length, 8 + 5)};
        final TableFixed.Plan lane = TableFixed.lineagePlans(known, MINE, DEST, 0x2222222211111111L, 12)[0];
        check(lane != null && lane.why == TableFixed.Reason.none && lane.count > 0, "the lineage lane is ready");
        if (lane != null && lane.count > 0) {
            final byte[] image = new byte[12];
            final TableFixed.Report r = new TableFixed.Report();
            TableFixed.run(lane.entries, lane.count, lane.remap, new byte[] {2, 0, 0, 0, 0}, 0, 5, image, r);
            check(r.refused && r.reason == TableFixed.Reason.outOfRange,
                    "the lineage lane refuses a forged tag 2 outOfRange, got " + r.reason);
        }
        System.out.println(failures == 0 ? "ok" : "FAILED " + failures);
        if (failures != 0) { System.exit(1); }
    }
}
`
	dpath := filepath.Join(td, "Driver.java")
	if err := os.WriteFile(dpath, []byte(driver), 0o644); err != nil {
		t.Fatal(err)
	}
	sources = append(sources, dpath)
	javac, java := javaTools(t)
	args := append([]string{"--release", "17", "-d", classes}, sources...)
	if out, err := testproc.Command(t, javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	for _, ea := range []string{"-ea", "-da"} {
		out, err := testproc.Command(t, java, ea, "-cp", classes, "Driver").CombinedOutput()
		if err != nil || !strings.HasSuffix(strings.TrimSpace(string(out)), "ok") {
			t.Fatalf("java %s: %v\n%s", ea, err, out)
		}
	}
}
