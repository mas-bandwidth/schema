package javatable

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestCardDescriptorsNameEveryField (docs/SPEC-TABLES.md §8) holds the generated
// descriptors of tables/cards to an explicit pinned vocabulary — table, field,
// kind, shape and bound in declaration order — written by hand in this test and
// NOT derived from the descriptors. Adding a field to the declaration without
// updating the vocabulary below fails the comparison.
func TestCardDescriptorsNameEveryField(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile(filepath.Join("..", "..", "..", "tables", "cards", "Card.schema"))
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(unitFrom(t, string(src)))
	if err != nil {
		t.Fatal(err)
	}
	desc, ok := files["CardDescriptor.java"]
	if !ok {
		t.Fatal("CardDescriptor.java not emitted for tables/cards")
	}

	// The pinned vocabulary, hand-written from tables/cards/Card.schema in
	// declaration order: field name, wire kind (§3), shape (§8.1), bound.
	vocab := []struct {
		name, shape string
		kind, bound int64
	}{
		{"id", "value", 12, 64},
		{"sha", "value", 12, 40},
		{"tier", "value", 30, 0},
		{"repo", "value", 12, 96},
		{"_base", "value", 12, 96},
		{"kind", "value", 12, 24},
		{"priority", "value", 30, 0},
		{"stream", "value", 12, 32},
		{"paths", "list", 14, 0},
		{"_new", "list", 14, 0},
		{"shared", "list", 14, 0},
		{"needs", "list", 14, 0},
		{"test", "value", 12, 192},
		{"done", "blob", 17, 0},
		{"deadline_minutes", "value", 4, 0},
		{"source", "blob", 17, 0},
		{"epoch", "value", 4, 0},
		{"column", "value", 30, 0},
		{"holder", "value", 12, 32},
		{"attempt", "value", 4, 0},
		{"head", "value", 12, 40},
		{"branch", "value", 12, 128},
		{"body", "blob", 17, 0},
		{"extra", "list", 14, 0},
		{"consumers", "list", 14, 0},
	}

	text := string(desc)
	last := 0
	for _, v := range vocab {
		needle := "TableFieldInfo.of(" + `"` + v.name + `", ` + itoa(v.kind) + ", " + `"` + v.shape + `", ` + itoa(v.bound) + "L)"
		at := strings.Index(text[last:], needle)
		if at < 0 {
			t.Fatalf("descriptor for field %s missing or out of order; want %q", v.name, needle)
		}
		last += at + len(needle)
	}
	if n := strings.Count(text, "TableFieldInfo.of("); n != len(vocab) {
		t.Fatalf("descriptor carries %d fields, vocabulary pins %d (a field was added to the declaration without the vocabulary)", n, len(vocab))
	}

	// The unit registry lists every card table descriptor (§8.3).
	reg := string(files["TableRegistry.java"])
	for _, table := range []string{"CardDescriptor", "PathTextDescriptor", "CardIDDescriptor", "KeyValueDescriptor", "ConsumerDescriptor"} {
		if !strings.Contains(reg, table+".tableType()") {
			t.Fatalf("registry does not list %s", table)
		}
	}
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	var b []byte
	for v > 0 {
		b = append([]byte{byte('0' + v%10)}, b...)
		v /= 10
	}
	return string(b)
}

// writeVarPackages generates the two declarations of testdata/cards into their
// own packages under a temp source dir, mirroring the round-trip test.
func writeVarPackages(t *testing.T, srcdir string) {
	t.Helper()
	writeVarPkg := func(pkg, schemaName string) {
		s, err := os.ReadFile(filepath.Join("testdata", "cards", schemaName))
		if err != nil {
			t.Fatal(err)
		}
		files, err := Generate(unitFrom(t, string(s)))
		if err != nil {
			t.Fatal(err)
		}
		pdir := filepath.Join(srcdir, "..", pkg)
		if err := os.MkdirAll(pdir, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, data := range files {
			if strings.HasSuffix(name, ".java") {
				if err := os.WriteFile(filepath.Join(pdir, name), data, 0o644); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	writeVarPkg("newpkg", "newer.schema")
	writeVarPkg("oldpkg", "older.schema")
}

// TestCardConstructsRetainUnknownFields (docs/SPEC-TABLES.md §6.6) proves a
// retained load and save keeps the unknown scalar and string fields the older
// declaration cannot name, re-emits them so the newer declaration rereads every
// value equal, and reports the excluded *string node index as loss — never
// retained silently. The save is idempotent.
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
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac (retain): %v\n%s", err, out)
	}
	out, err := exec.Command(javaBin, "-ea", "-cp", classes, "RetainDriver").CombinedOutput()
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
        byte[] wire = newpkg.CardVariable.save(v);

        oldpkg.TableRetain retain = new oldpkg.TableRetain(1 << 16, 16);
        oldpkg.TableVariable.Report report = new oldpkg.TableVariable.Report();
        oldpkg.CardVariable.Value o = oldpkg.CardRetain.loadRetain(wire, retain, report);
        if (o == null) { fail("load returned null"); }
        if (!"hi".equals(o.id)) { fail("id=" + o.id); }
        if (o.oldName != 42) { fail("oldName=" + o.oldName); }
        if (report.unknown != 4) { fail("unknown=" + report.unknown); }
        if (report.retained != 3) { fail("retained=" + report.retained); }
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
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac (loss): %v\n%s", err, out)
	}
	out, err := exec.Command(javaBin, "-ea", "-cp", classes, "LossDriver").CombinedOutput()
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
        byte[] wire = newpkg.CardVariable.save(v);
        byte[] copy = java.util.Arrays.copyOf(wire, wire.length);

        // A buffer one byte short of the first unknown field's record (the
        // string "extra" is a 15-byte record): it is dropped whole and counted
        // once, nothing is retained, and the input is untouched (§6.6).
        oldpkg.TableRetain shortRetain = new oldpkg.TableRetain(14, 16);
        oldpkg.TableVariable.Report sr = new oldpkg.TableVariable.Report();
        oldpkg.CardVariable.Value o = oldpkg.CardRetain.loadRetain(wire, shortRetain, sr);
        if (o == null) { fail("load returned null"); }
        if (sr.retainLost != 4) { fail("one-byte-short buffer reported retainLost=" + sr.retainLost); }
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

        System.out.println("ok");
    }
}
`
