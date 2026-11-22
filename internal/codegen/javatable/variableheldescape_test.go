package javatable

import (
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// TestHeldKind31RefusesTheLoadJava: kind 31 is held by name and never written
// (R17, docs/SPEC-TABLES.md §3: the newer reader reads old data, never the
// other way). A Java reader meets it only as damage and refuses the whole load
// wherever it stands: a field it cannot name (with payload bytes or with L 0),
// a field it declares and a union arm, exactly as it refuses 33 and 34. Red on
// the old escape, which skipped an unnamed 31 by its L and counted it unknown.
// The control, an unnamed string, reads, so the driver can tell a read from a
// refusal.
func TestHeldKind31RefusesTheLoadJava(t *testing.T) {
	slowtest.Gate(t, "an external toolchain")

	t.Parallel()
	javac, javaBin := javaTools(t)
	files, err := Generate(unitFrom(t, `package probe
table Card { id string(64) }
`))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := files["CardVariable.java"]; !ok {
		t.Fatal("CardVariable.java not emitted")
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
	if err := os.WriteFile(filepath.Join(dir, "Driver.java"), []byte(heldKind31Driver), 0o644); err != nil {
		t.Fatal(err)
	}
	srcs, err := filepath.Glob(filepath.Join(srcdir, "*.java"))
	if err != nil {
		t.Fatal(err)
	}
	args := append([]string{"--release", "17", "-d", classes}, srcs...)
	args = append(args, filepath.Join(dir, "Driver.java"))
	if out, err := testproc.Command(t, javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	out, err := testproc.Command(t, javaBin, "-ea", "-cp", classes, "Driver").CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "PROBE OK") {
		t.Fatalf("driver: %s", out)
	}
}

// heldKind31Driver writes form-1 wires by hand, each carrying kind 31 at one
// position, and loads each through the generated codec: every one refuses.
const heldKind31Driver = `import java.nio.charset.StandardCharsets;
import java.util.Arrays;

public final class Driver {
    private Driver() {}

    static int bad = 0;
    static byte[] buf;
    static probe.TableVariable.Writer w;
    static final long UNNAMED = 0x1122334455667788L;

    static void begin() {
        buf = new byte[256];
        w = new probe.TableVariable.Writer(buf, new probe.TableVariable.Ids(16));
        w.put8(1); // form byte 1
    }

    // known writes the declared id field under kind 12.
    static void known() {
        w.putLeb(w.ref(probe.CardVariable.F_id));
        w.put8(12);
        byte[] s = "hi".getBytes(StandardCharsets.UTF_8);
        w.putLeb(s.length);
        w.putBytes(s, 0, s.length);
    }

    static void check(String name, boolean expect) {
        w.put8(0); // terminator
        w.trailer();
        byte[] wire = Arrays.copyOf(buf, w.off);
        probe.TableVariable.Report r = new probe.TableVariable.Report();
        probe.CardVariable.Value v = probe.CardVariable.load(wire, r);
        boolean read = v != null && !r.refused && !r.damaged;
        if (read != expect) {
            System.out.println("FAIL " + name + ": read=" + read + " unknown=" + r.unknown);
            bad++;
        }
    }

    public static void main(String[] args) {
        begin(); w.putLeb(w.ref(UNNAMED)); w.put8(12); w.putLeb(2); w.put8('a'); w.put8('b'); known();
        check("control, an unnamed string", true);
        begin(); w.putLeb(w.ref(UNNAMED)); w.put8(31); w.putLeb(4); w.put8('a'); w.put8('b'); w.put8('c'); w.put8('d'); known();
        check("unnamed field", false);
        begin(); w.putLeb(w.ref(UNNAMED)); w.put8(31); w.putLeb(0); known();
        check("unnamed field, L 0", false);
        begin(); w.putLeb(w.ref(probe.CardVariable.F_id)); w.put8(31); w.putLeb(1); w.put8(0);
        check("named field", false);
        begin(); w.putLeb(w.ref(UNNAMED)); w.put8(15); w.putLeb(w.ref(0x0102030405060708L)); w.put8(31); w.putLeb(1); w.put8(0xAA); known();
        check("union arm", false);
        if (bad != 0) {
            System.out.println("FAILED " + bad);
            System.exit(1);
        }
        System.out.println("PROBE OK");
    }
}
`
