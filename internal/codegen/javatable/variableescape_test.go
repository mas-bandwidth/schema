package javatable

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestEscapeKind31SkippedAndCountedJava carries the 64-bit id-table wire's
// ESCAPE KIND 31 (docs/SPEC-TABLES.md §3): a field this reader cannot name,
// arriving under kind 31, is skipped by its L and counted unknown — the one
// kind whose meaning a reader is licensed not to know, the path a later-major
// writer uses to add a kind without becoming a wire change. The runtime's
// skip/skipExtent already step kind 31 by L; this test pins it, emitted into
// a real variable codec and read back, so the escape cannot regress silently.
func TestEscapeKind31SkippedAndCountedJava(t *testing.T) {
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
	if err := os.WriteFile(filepath.Join(dir, "Driver.java"), []byte(escapeKind31Driver), 0o644); err != nil {
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
	out, err := exec.Command(javaBin, "-ea", "-cp", classes, "Driver").CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "ok") {
		t.Fatalf("driver: %s", out)
	}
}

// escapeKind31Driver writes a form-1 wire by hand: an unknown field (an id the
// reader cannot name) under kind 31 with an L-framed payload, then the known
// id field, the terminator and the trailer — and reads it back through the
// generated codec: the escape is skipped by its L and counted unknown (§3),
// the known field lands, and no malformed or refusal moves.
const escapeKind31Driver = `import java.nio.charset.StandardCharsets;
import java.util.Arrays;

public final class Driver {
    private Driver() {}

    static void fail(String what) {
        System.out.println("FAILED: " + what);
        System.exit(1);
    }

    public static void main(String[] args) {
        probe.TableVariable.Ids ids = new probe.TableVariable.Ids(16);
        byte[] buf = new byte[256];
        probe.TableVariable.Writer w = new probe.TableVariable.Writer(buf, ids);

        w.put8(1); // form byte 1
        // An UNKNOWN field under the escape kind 31: ref to an id this codec
        // cannot name, kind 31, L = 4, then four opaque bytes (§3).
        w.putLeb(w.ref(0x1122334455667788L));
        w.put8(31);
        w.putLeb(4);
        w.put8('a'); w.put8('b'); w.put8('c'); w.put8('d');
        // The KNOWN field: id under kind 12.
        w.putLeb(w.ref(probe.CardVariable.F_id));
        w.put8(12);
        byte[] s = "hi".getBytes(StandardCharsets.UTF_8);
        w.putLeb(s.length);
        w.putBytes(s, 0, s.length);
        w.put8(0); // terminator
        w.trailer();
        byte[] wire = Arrays.copyOf(buf, w.off);

        probe.TableVariable.Report r = new probe.TableVariable.Report();
        probe.CardVariable.Value v = probe.CardVariable.load(wire, r);
        if (v == null) { fail("load returned null"); }
        if (r.refused || r.malformed) { fail("load refused/malformed"); }
        if (!"hi".equals(v.id)) { fail("id=" + v.id); }
        if (r.unknown != 1) { fail("unknown=" + r.unknown + ", want 1 for the escape"); }
        if (r.kindMismatch != 0) { fail("kindMismatch=" + r.kindMismatch); }

        // Re-save lands the known field alone: the escape is a field this
        // reader cannot name, so it never re-emits.
        byte[] saved = probe.CardVariable.save(v);
        probe.TableVariable.Report r2 = new probe.TableVariable.Report();
        probe.CardVariable.Value back = probe.CardVariable.load(saved, r2);
        if (back == null || r2.refused || r2.malformed) { fail("re-save does not re-load"); }
        if (!"hi".equals(back.id)) { fail("re-save id=" + back.id); }
        if (r2.unknown != 0) { fail("re-save unknown=" + r2.unknown); }

        System.out.println("ok");
    }
}
`
