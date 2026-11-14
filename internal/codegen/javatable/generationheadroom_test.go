package javatable

// The Java file reader's per-thread wire check must never refuse a valid file
// because of its own generation counter, however many bodies the thread has
// read before (internal/codegen/javatable/variableform.go, TableVariable's
// WireCheck). This is the Java cell for that rule: a probe positions the
// reused check's counter by reflection, loads a valid file larger than the
// counter's old reset window, then loads valid files across the threshold and
// across the place the counter would wrap. Every load must stay valid, and a
// body that names one id twice must still be refused at every position tried.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const generationHeadroomJavaSchema = `package probe
table Sample {
 a int32
}
table Root {
 rows []Sample
 z int32
 y int32
 w int32
}
`

// generationHeadroomBodies is one more than 2^20 bodies: a file at the old
// reset window carries the generation counter past Integer.MAX_VALUE.
const generationHeadroomBodies = 1 << 20

// generationHeadroomJavaProbe builds valid form-1 files and holds
// TableVariable.open to them. The counter is read and written through the
// same private ThreadLocal the reader uses, so no product API is added.
const generationHeadroomJavaProbe = `package probe;

import java.lang.reflect.Field;

public final class WireGenerationProbe {
    private WireGenerationProbe() {}

    static Object wireCheck() throws Exception {
        Field tl = TableVariable.class.getDeclaredField("WIRE_CHECK");
        tl.setAccessible(true);
        return ((ThreadLocal<?>) tl.get(null)).get();
    }

    static int getGen() throws Exception {
        Object check = wireCheck();
        Field gen = check.getClass().getDeclaredField("gen");
        gen.setAccessible(true);
        return gen.getInt(check);
    }

    static void setGen(int value) throws Exception {
        Object check = wireCheck();
        Field gen = check.getClass().getDeclaredField("gen");
        gen.setAccessible(true);
        gen.setInt(check, value);
    }

    static TableVariable.Writer begin(int capacity) {
        return new TableVariable.Saver(capacity).begin();
    }

    static void field(TableVariable.Writer w, long id, int value) {
        w.putLeb(w.ref(id));
        w.put8(4);
        w.put32(value);
    }

    // bigWire is a valid root whose rows list carries the given number of
    // inline Sample bodies. Each element body is one generation.
    static byte[] bigWire(int bodies) {
        TableVariable.Writer w = begin(Math.max(256, bodies * 10 + 64));
        w.put8(1);
        w.putLeb(w.ref(RootVariable.F_rows));
        w.put8(14);
        int list = w.off;
        w.put8(13);
        w.putLeb(bodies);
        for (int i = 0; i < bodies; i++) {
            int body = w.off;
            field(w, SampleVariable.F_a, 1);
            w.put8(0);
            w.frame(body);
        }
        w.frame(list);
        w.put8(0);
        w.trailer();
        if (w.overflow) {
            throw new AssertionError("big wire overflow");
        }
        return java.util.Arrays.copyOf(w.buf, w.off);
    }

    // smallWire is a valid root with three scalar fields, so its body names
    // references 1, 2 and 3 and can meet a stale slot at any of them.
    static byte[] smallWire() {
        TableVariable.Writer w = begin(96);
        w.put8(1);
        field(w, RootVariable.F_z, 1);
        field(w, RootVariable.F_y, 2);
        field(w, RootVariable.F_w, 3);
        w.put8(0);
        w.trailer();
        return java.util.Arrays.copyOf(w.buf, w.off);
    }

    // duplicateWire is structurally valid except that one body names the same
    // id twice, which the wire check must call damage at any counter position.
    static byte[] duplicateWire() {
        TableVariable.Writer w = begin(64);
        w.put8(1);
        field(w, RootVariable.F_z, 1);
        field(w, RootVariable.F_z, 2);
        w.put8(0);
        w.trailer();
        return java.util.Arrays.copyOf(w.buf, w.off);
    }

    static void loadValid(String what, byte[] wire) throws Exception {
        TableVariable.Report report = new TableVariable.Report();
        TableVariable.Reader r = TableVariable.open(wire, report);
        if (r == null || report.damaged || report.refused) {
            throw new AssertionError("valid file refused (" + what + ") at gen " + getGen()
                    + ": damaged=" + report.damaged + " refused=" + report.refused
                    + " reason=" + report.reason);
        }
    }

    static void loadDuplicate(String what, byte[] wire) throws Exception {
        TableVariable.Report report = new TableVariable.Report();
        TableVariable.Reader r = TableVariable.open(wire, report);
        if (r != null || !report.damaged) {
            throw new AssertionError("a body naming one id twice was not damage ("
                    + what + ") at gen " + getGen());
        }
    }

    public static void main(String[] args) throws Exception {
        int bodies = Integer.parseInt(args[0]);
        byte[] big = bigWire(bodies);
        byte[] small = smallWire();
        byte[] duplicate = duplicateWire();

        // The old reset window is 2^20 generations wide. Start exactly at its
        // edge so a file larger than the window carries the counter past
        // Integer.MAX_VALUE.
        setGen(Integer.MAX_VALUE - (1 << 20));
        loadValid("the file that crosses the threshold", big);

        // The counter has wrapped; position it just below the next generation
        // a never-stamped slot still holds, rather than looping 2^31 times.
        setGen(-1);
        loadValid("a valid file at the wrap point", small);

        int[] positions = {
            -1,
            1,
            Integer.MAX_VALUE - (1 << 20),
            Integer.MAX_VALUE - 1,
        };
        for (int pos : positions) {
            setGen(pos);
            loadValid("a valid file at gen " + pos, small);
            setGen(pos);
            loadDuplicate("a repeated id at gen " + pos, duplicate);
        }
        System.out.println("ok");
    }
}
`

func TestJavaWireCheckGenerationHeadroom(t *testing.T) {
	t.Parallel()
	javac, javaBin := javaToolsRequired(t)
	files, err := Generate(unitFrom(t, generationHeadroomJavaSchema))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	root := t.TempDir()
	srcdir := filepath.Join(root, "src", "probe")
	classes := filepath.Join(root, "classes")
	for _, d := range []string{srcdir, classes} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	var srcs []string
	for name, data := range files {
		if !strings.HasSuffix(name, ".java") {
			continue
		}
		p := filepath.Join(srcdir, name)
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
		srcs = append(srcs, p)
	}
	probe := filepath.Join(srcdir, "WireGenerationProbe.java")
	if err := os.WriteFile(probe, []byte(generationHeadroomJavaProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	args := append([]string{"--release", "17", "-d", classes}, srcs...)
	args = append(args, probe)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	out, err := exec.Command(javaBin, "-ea", "-cp", classes, "probe.WireGenerationProbe",
		strconv.Itoa(generationHeadroomBodies)).CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "ok") {
		t.Fatalf("probe: %s", out)
	}
}
