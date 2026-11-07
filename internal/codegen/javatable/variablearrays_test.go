package javatable

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestUnboundedArraysRoundTripJava carries the unbounded scalar/string/enum/
// flags arrays (§2.9) on the variable wire: a generated Java program builds a
// value holding one of each element class, saves it, loads it back and re-saves
// it byte for byte. This is the "carrying unbounded arrays" construct of the
// variable class beyond the card constructs.
func TestUnboundedArraysRoundTripJava(t *testing.T) {
	t.Parallel()
	javac, javaBin := javaTools(t)
	src := `package probe
enum Grade { A, B, C }
flags Perm { Read, Write }
table T
{
    xs []int32
    gs []Grade
    ps []Perm
    fs []float32
}
`
	files, err := Generate(unitFrom(t, src))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := files["TVariable.java"]; !ok {
		t.Fatal("TVariable.java not emitted: unbounded arrays are not carried")
	}
	dir := t.TempDir()
	classes := filepath.Join(dir, "classes")
	srcdir := filepath.Join(dir, "src", "probe")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(srcdir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		if strings.HasSuffix(name, ".java") {
			if err := os.WriteFile(filepath.Join(srcdir, name), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "Driver.java"), []byte(unboundedArraysDriver), 0o644); err != nil {
		t.Fatal(err)
	}
	srcs, err := filepath.Glob(filepath.Join(srcdir, "*.java"))
	if err != nil {
		t.Fatal(err)
	}
	args := append([]string{"--release", "17", "-d", classes}, srcs...)
	args = append(args, filepath.Join(dir, "Driver.java"))
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	if out, err := exec.Command(javaBin, "-ea", "-cp", classes, "Driver").CombinedOutput(); err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	} else if !strings.Contains(string(out), "ok") {
		t.Fatalf("driver: %s", out)
	}
}

// unboundedArraysDriver builds one T with a populated scalar, string, enum and
// flags list, then holds load/save byte identity.
const unboundedArraysDriver = `import java.util.Arrays;

public final class Driver {
    private Driver() {}

    static void fail(String what) {
        System.out.println("FAILED: " + what);
        System.exit(1);
    }

    public static void main(String[] args) {
        probe.TVariable.Value v = new probe.TVariable.Value();
        v.xs.add(10); v.xs.add(-2); v.xs.add(300);
        v.gs.add(1); v.gs.add(3); v.gs.add(0); v.gs.add(2);
        v.ps.add(1L); v.ps.add(3L);
        v.fs.add(1.5f); v.fs.add(-0.25f);
        byte[] wire = probe.TVariable.save(v);

        probe.TableVariable.Report report = new probe.TableVariable.Report();
        probe.TVariable.Value back = probe.TVariable.load(wire, report);
        if (back == null) { fail("load returned null"); }
        if (report.malformed || report.refused) { fail("load reported malformed/refused"); }
        if (!v.xs.equals(back.xs)) { fail("xs: " + back.xs); }
        if (!v.gs.equals(back.gs)) { fail("gs: " + back.gs); }
        if (!v.ps.equals(back.ps)) { fail("ps: " + back.ps); }
        if (!v.fs.equals(back.fs)) { fail("fs: " + back.fs); }

        byte[] saved = probe.TVariable.save(back);
        if (!Arrays.equals(wire, saved)) {
            int d = 0;
            while (d < wire.length && d < saved.length && wire[d] == saved[d]) { d++; }
            fail("re-save differs at byte " + d + " (wire " + wire.length + " saved " + saved.length + ")");
        }
        System.out.println("ok");
    }
}
`
