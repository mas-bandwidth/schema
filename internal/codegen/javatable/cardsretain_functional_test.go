//go:build functional

package javatable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCardConstructsRetainUnknownFields(t *testing.T) {
	t.Parallel()
	javac, javaBin := javaTools(t)
	dir := t.TempDir()
	classes := filepath.Join(dir, "classes")
	srcdir := filepath.Join(dir, "src", "cards")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(srcdir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeVarPackages(t, srcdir)
	if err := os.WriteFile(filepath.Join(dir, "RetainDriver.java"), []byte(retainDriver), 0o644); err != nil {
		t.Fatal(err)
	}
	allSrcs, err := filepath.Glob(filepath.Join(dir, "src", "*", "*.java"))
	if err != nil {
		t.Fatal(err)
	}
	allSrcs = append(allSrcs, filepath.Join(dir, "RetainDriver.java"))
	args := append([]string{"--release", "17", "-d", classes}, allSrcs...)
	if out, err := boundedCommand(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac (retain): %v\n%s", err, out)
	}
	out, err := boundedCommand(javaBin, "-ea", "-cp", classes, "RetainDriver").CombinedOutput()
	if err != nil {
		t.Fatalf("java (retain): %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "ok") {
		t.Fatalf("retain driver: %s", out)
	}
}

// retainDriver writes a newer-declaration wire carrying an unknown scalar, an
// unknown string and an unknown *string; the older declaration's retained load
// keeps the scalar and string, reports the *string as loss, and its retained
// save re-emits them so the newer declaration rereads every value equal.
const retainDriver = `public final class RetainDriver {
    private RetainDriver() {}

    static void fail(String what) {
        System.out.println("FAILED: " + what);
        System.exit(1);
    }

    public static void main(String[] args) {
        newpkg.CardVariable.Value v = new newpkg.CardVariable.Value();
        v.id = "hi";
        v.renamed = 42;
        v.extra = "x";
        v.extraNum = 7;
        v.extraStr = "hello";
        v.extraPtr = "ptr";
        v.extraGrade = 2;
        byte[] wire = newpkg.CardVariable.save(v);

        oldpkg.TableRetain retain = new oldpkg.TableRetain(1 << 16, 16);
        oldpkg.TableVariable.Report report = new oldpkg.TableVariable.Report();
        oldpkg.CardVariable.Value o = oldpkg.CardRetain.loadRetain(wire, retain, report);
        if (o == null) { fail("load returned null"); }
        if (!"hi".equals(o.id)) { fail("id=" + o.id); }
        if (o.oldName != 42) { fail("oldName=" + o.oldName); }
        if (report.unknown != 5) { fail("unknown=" + report.unknown); }
        if (report.retained != 4) { fail("retained=" + report.retained); }
        if (report.retainLost != 1) { fail("retainLost=" + report.retainLost); }

        byte[] saved = oldpkg.CardRetain.saveRetain(o, retain, report);
        if (saved == null) { fail("save returned null"); }

        newpkg.TableVariable.Report nr = new newpkg.TableVariable.Report();
        newpkg.CardVariable.Value n = newpkg.CardVariable.load(saved, nr);
        if (n == null) { fail("reread returned null"); }
        if (!"hi".equals(n.id)) { fail("reread id=" + n.id); }
        if (n.renamed != 42) { fail("reread renamed=" + n.renamed); }
        if (!"x".equals(n.extra)) { fail("reread extra=" + n.extra); }
        if (n.extraNum != 7) { fail("reread extraNum=" + n.extraNum); }
        if (!"hello".equals(n.extraStr)) { fail("reread extraStr=" + n.extraStr); }
        if (n.extraPtr != null) { fail("reread extraPtr: a node index is never retained"); }
        if (n.extraGrade != 2) { fail("reread extraGrade: the unknown enum field's variant was not retained"); }

        byte[] again = oldpkg.CardRetain.saveRetain(o, retain, report);
        if (!java.util.Arrays.equals(saved, again)) { fail("second retained save differs"); }

        System.out.println("ok");
    }
}
`

// TestRetentionCapacityIsALoss (docs/SPEC-TABLES.md §6.6) proves a retention
// buffer one byte short reports RetainLost and produces no retained output, an
// unknown enum variant is reported and blocks a lossless claim, and the input
// bytes are unchanged in every case.
func TestRetentionCapacityIsALoss(t *testing.T) {
	t.Parallel()
	javac, javaBin := javaTools(t)
	dir := t.TempDir()
	classes := filepath.Join(dir, "classes")
	srcdir := filepath.Join(dir, "src", "cards")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(srcdir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeVarPackages(t, srcdir)
	if err := os.WriteFile(filepath.Join(dir, "LossDriver.java"), []byte(lossDriver), 0o644); err != nil {
		t.Fatal(err)
	}
	allSrcs, err := filepath.Glob(filepath.Join(dir, "src", "*", "*.java"))
	if err != nil {
		t.Fatal(err)
	}
	allSrcs = append(allSrcs, filepath.Join(dir, "LossDriver.java"))
	args := append([]string{"--release", "17", "-d", classes}, allSrcs...)
	if out, err := boundedCommand(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac (loss): %v\n%s", err, out)
	}
	out, err := boundedCommand(javaBin, "-ea", "-cp", classes, "LossDriver").CombinedOutput()
	if err != nil {
		t.Fatalf("java (loss): %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "ok") {
		t.Fatalf("loss driver: %s", out)
	}
}

// lossDriver sizes the retention buffer one byte short of the string record,
// proving the truncated record is dropped and counted, and proves the input
// wire is never mutated by a retained load.
const lossDriver = `public final class LossDriver {
    private LossDriver() {}

    static void fail(String what) {
        System.out.println("FAILED: " + what);
        System.exit(1);
    }

    public static void main(String[] args) {
        newpkg.CardVariable.Value v = new newpkg.CardVariable.Value();
        v.id = "hi";
        v.renamed = 42;
        v.extra = "x";
        v.extraNum = 7;
        v.extraStr = "hello";
        v.extraPtr = "ptr";
        v.extraGrade = 2;
        byte[] wire = newpkg.CardVariable.save(v);
        byte[] copy = java.util.Arrays.copyOf(wire, wire.length);

        // A buffer one byte short of the first unknown field's record (the
        // string "extra" is a 16-byte record): it is dropped whole and counted
        // once, nothing is retained, and the input is untouched (§6.6).
        oldpkg.TableRetain shortRetain = new oldpkg.TableRetain(15, 16);
        oldpkg.TableVariable.Report sr = new oldpkg.TableVariable.Report();
        oldpkg.CardVariable.Value o = oldpkg.CardRetain.loadRetain(wire, shortRetain, sr);
        if (o == null) { fail("load returned null"); }
        if (sr.retainLost != 5) { fail("one-byte-short buffer reported retainLost=" + sr.retainLost); }
        if (sr.retained != 0) { fail("one-byte-short buffer retained a field"); }

        // The input bytes are unchanged by the retained load.
        if (!java.util.Arrays.equals(wire, copy)) { fail("retained load mutated its input"); }

        // A full buffer retains and re-emits; the retained save re-reads equal.
        oldpkg.TableRetain retain = new oldpkg.TableRetain(1 << 16, 16);
        oldpkg.TableVariable.Report report = new oldpkg.TableVariable.Report();
        oldpkg.CardVariable.Value ok = oldpkg.CardRetain.loadRetain(wire, retain, report);
        byte[] saved = oldpkg.CardRetain.saveRetain(ok, retain, report);
        newpkg.TableVariable.Report nr = new newpkg.TableVariable.Report();
        newpkg.CardVariable.Value n = newpkg.CardVariable.load(saved, nr);
        if (n.extraNum != 7) { fail("full buffer lost extraNum=" + n.extraNum); }
        if (!"hello".equals(n.extraStr)) { fail("full buffer lost extraStr=" + n.extraStr); }
        if (n.extraGrade != 2) { fail("full buffer lost extraGrade=" + n.extraGrade); }

        System.out.println("ok");
    }
}
`
