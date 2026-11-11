package javatable

// THE FIXED-FORM SAVE HOLDS THE WRITE CONTRACT (docs/SPEC.md §5.1) on the Java
// leg, an assert leg: every write check is `assert checkWrite(v)`, active under
// `-ea` and gone without it. One case per row 1-6 and 8 (ill-formed bytes and a
// zero byte as two row-8 cases), plus row 1 in every slot shape the save
// stores (a fixed and a keyed array element, a present optional, a union arm,
// a nested table) and row 8 in a union arm:
//
//   - under -ea each refuses by AssertionError, and its lawful twin at the same
//     boundary saves and loads back equal;
//   - without -ea nothing asserts: the writer trusts, so the save writes its
//     bytes and throws nothing, and the twin still loads back equal.
//
// Row 7 (a NaN or an infinity in a plain float) is refused under -ea. The
// contract asks an assert leg's release build for no write check, so this
// test requires none there. Row 4's case is a length past N, which no
// release build can store into an N-byte array: without -ea it is the JVM's
// own bounds check that stops it, never the leg's assert.

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const fixedSaveContractSchema = `package probe

enum Mode { Walk, Run }

type Leaf { lv int32 | min = 0, max = 9 }

union Pick
{
    n int32 | min = 0, max = 9
    t string(4)
}

fixed table Rec
{
    r    int32 | min = -5, max = 5
    u    uint8 = 10 | min = 10, max = 200
    b    bits(6)
    arr  [2..4]int32
    s    string(8)
    m    Mode
    p    Pick
    f    float32
    ra   [2]int32 | min = 0, max = 9
    ms   [Mode]int32 | min = 0, max = 9
    ro   ?int32 | min = 0, max = 9
    leaf Leaf
}
`

const fixedSaveContractDriver = `package probe;

import java.util.Arrays;

public final class Driver {
    private Driver() {}

    interface Set { void set(RecFixed.Value v); }

    interface Get { long get(RecFixed.Value v); }

    static RecFixed.Value lawful() {
        final RecFixed.Value v = new RecFixed.Value();
        v.r = 1;
        v.u = (byte) 50;
        v.b = 3;
        v.arrCount = 2;
        v.arr[0] = 1;
        v.arr[1] = 2;
        v.s[0] = 'o';
        v.s[1] = 'k';
        v.sLength = 2;
        v.m = 1;
        v.p.type = PickFixed.n;
        v.p.n = 1;
        v.f = 1.5f;
        v.ra[0] = 1;
        v.ra[1] = 2;
        for (int i = 0; i < v.ms.length; i++) { v.ms[i] = 3; }
        v.roPresent = true;
        v.ro = 4;
        v.leaf.lv = 5;
        return v;
    }

    static void text(byte[] dst, int... units) {
        Arrays.fill(dst, (byte) 0);
        for (int i = 0; i < units.length; i++) { dst[i] = (byte) units[i]; }
    }

    // the outcome of one save: "assert" (the debug check), "refused" (save
    // answered -1), "threw" (any other throwable), "roundtrip" (saved, and the
    // load lands the same value and the same bytes again) or "saved" (saved,
    // and the reader does not land it back equal)
    static String outcome(Set set, Get get) {
        final RecFixed.Value v = lawful();
        set.set(v);
        final byte[] buf = new byte[RecFixed.measure(1)];
        final int n;
        try {
            n = RecFixed.save(new RecFixed.Value[] { v }, 1, buf);
        } catch (AssertionError e) {
            return "assert";
        } catch (Throwable e) {
            return "threw";
        }
        if (n < 0) { return "refused"; }
        final RecFixed.Value back = new RecFixed.Value();
        final TableFixed.Report r = new TableFixed.Report();
        final int got = RecFixed.load(new RecFixed.Value[] { back }, 1, buf,
                TableFixed.plan(256), new short[256], RecFixed.image(), r);
        if (got != 1 || r.refused || r.damaged) { return "saved"; }
        if (get.get(back) != get.get(v)) { return "saved"; }
        final byte[] again = new byte[buf.length];
        try {
            if (RecFixed.save(new RecFixed.Value[] { back }, 1, again) != n) { return "saved"; }
        } catch (Throwable e) {
            return "saved";
        }
        return Arrays.equals(buf, again) ? "roundtrip" : "saved";
    }

    static void run(String name, Set set, Get get) {
        System.out.println("CASE " + name + " " + outcome(set, get));
    }

    static long textKey(byte[] s, int len) {
        long k = len;
        for (int i = 0; i < len; i++) { k = k * 257 + (s[i] & 0xFF); }
        return k;
    }

    public static void main(String[] args) {
        run("row1.max.lawful", v -> v.r = 5, v -> v.r);
        run("row1.max.refused", v -> v.r = 6, v -> v.r);
        run("row1.min.lawful", v -> v.r = -5, v -> v.r);
        run("row1.min.refused", v -> v.r = -6, v -> v.r);
        run("row1.unsigned.lawful", v -> v.u = (byte) 200, v -> v.u & 0xFF);
        run("row1.unsigned.refused", v -> v.u = (byte) 201, v -> v.u & 0xFF);
        run("row1.array.lawful", v -> v.ra[1] = 9, v -> v.ra[1]);
        run("row1.array.refused", v -> v.ra[1] = 10, v -> v.ra[1]);
        run("row1.keyed.lawful", v -> v.ms[v.ms.length - 1] = 9, v -> v.ms[v.ms.length - 1]);
        run("row1.keyed.refused", v -> v.ms[v.ms.length - 1] = 10, v -> v.ms[v.ms.length - 1]);
        run("row1.optional.lawful", v -> v.ro = 9, v -> v.ro);
        run("row1.optional.refused", v -> v.ro = 10, v -> v.ro);
        run("row1.optional.absent.lawful", v -> { v.roPresent = false; v.ro = 10; }, v -> v.roPresent ? 1 : 0);
        run("row1.arm.lawful", v -> v.p.n = 9, v -> v.p.n);
        run("row1.arm.refused", v -> v.p.n = 10, v -> v.p.n);
        run("row1.nested.lawful", v -> v.leaf.lv = 9, v -> v.leaf.lv);
        run("row1.nested.refused", v -> v.leaf.lv = 10, v -> v.leaf.lv);
        run("row2.lawful", v -> v.b = 63, v -> v.b);
        run("row2.refused", v -> v.b = 64, v -> v.b);
        run("row3.lawful", v -> v.arrCount = 4, v -> v.arrCount);
        run("row3.refused", v -> v.arrCount = 1, v -> v.arrCount);
        run("row4.lawful", v -> { text(v.s, 'a', 'b', 'c', 'd', 'e', 'f', 'g', 'h'); v.sLength = 8; }, v -> textKey(v.s, v.sLength));
        run("row4.refused", v -> v.sLength = 9, v -> v.sLength);
        run("row5.lawful", v -> v.m = 2, v -> v.m);
        run("row5.refused", v -> v.m = 3, v -> v.m);
        run("row6.lawful", v -> { v.p.type = PickFixed.t; text(v.p.t, 'x'); v.p.tLength = 1; }, v -> v.p.type);
        run("row6.refused", v -> v.p.type = 3, v -> v.p.type);
        run("row7.lawful", v -> v.f = Float.MAX_VALUE, v -> Float.floatToRawIntBits(v.f));
        run("row7.refused", v -> v.f = Float.POSITIVE_INFINITY, v -> Float.floatToRawIntBits(v.f));
        run("row8.illformed.lawful", v -> { text(v.s, 0xC3, 0xA9); v.sLength = 2; }, v -> textKey(v.s, v.sLength));
        run("row8.illformed.refused", v -> { text(v.s, 0xFF, 0x00); v.sLength = 2; }, v -> textKey(v.s, v.sLength));
        run("row8.zero.lawful", v -> { text(v.s, 'a', 'b'); v.sLength = 2; }, v -> textKey(v.s, v.sLength));
        run("row8.zero.refused", v -> { text(v.s, 'a', 0x00); v.sLength = 2; }, v -> textKey(v.s, v.sLength));
        run("row8.arm.lawful", v -> { v.p.type = PickFixed.t; text(v.p.t, 0xC3, 0xA9); v.p.tLength = 2; }, v -> textKey(v.p.t, v.p.tLength));
        run("row8.arm.refused", v -> { v.p.type = PickFixed.t; text(v.p.t, 0xC3, 0x28); v.p.tLength = 2; }, v -> textKey(v.p.t, v.p.tLength));
    }
}
`

func TestJavaFixedSaveWriteContract(t *testing.T) {
	t.Parallel()
	javac, javaBin := javaToolsRequired(t)
	files, err := Generate(unitFrom(t, fixedSaveContractSchema))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	classes := filepath.Join(dir, "classes")
	writeFixedJava(t, src, "probe", files)
	driver := filepath.Join(src, "probe", "Driver.java")
	if err := os.WriteFile(driver, []byte(fixedSaveContractDriver), 0o644); err != nil {
		t.Fatal(err)
	}
	sources, err := filepath.Glob(filepath.Join(src, "probe", "*.java"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(javac, append([]string{"--release", "17", "-nowarn", "-d", classes}, sources...)...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}

	outcomes := func(flags ...string) map[string]string {
		t.Helper()
		args := append(append([]string{}, flags...), "-cp", classes, "probe.Driver")
		out, err := exec.Command(javaBin, args...).CombinedOutput()
		if err != nil {
			t.Fatalf("java %v: %v\n%s", flags, err, out)
		}
		got := map[string]string{}
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			f := strings.Fields(line)
			if len(f) != 3 || f[0] != "CASE" {
				t.Fatalf("java %v: unexpected line %q\n%s", flags, line, out)
			}
			got[f[1]] = f[2]
		}
		return got
	}
	debug := outcomes("-ea")
	release := outcomes("-da")

	names := make([]string, 0, len(debug))
	for name := range debug {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) != 35 {
		t.Fatalf("the driver ran %d cases, want 35", len(names))
	}
	for _, name := range names {
		d, r := debug[name], release[name]
		switch {
		case strings.HasSuffix(name, ".lawful"):
			// a lawful value saves and loads back equal in both builds
			if d != "roundtrip" || r != "roundtrip" {
				t.Errorf("%s: -ea %s, release %s; want roundtrip in both", name, d, r)
			}
		case name == "row7.refused":
			// the debug build refuses it; the release build owes nothing
			if d != "assert" && d != "refused" {
				t.Errorf("%s: -ea %s; want a refusal", name, d)
			}
		case name == "row4.refused":
			// a length past N cannot be stored into N bytes: without -ea the
			// JVM's bounds check stops it, and the leg asserts nothing
			if d != "assert" {
				t.Errorf("%s: -ea %s; want assert", name, d)
			}
			if r == "assert" || r == "refused" {
				t.Errorf("%s: release %s; want no write check", name, r)
			}
		default:
			// the debug assert refuses it; without -ea the writer trusts and
			// throws nothing
			if d != "assert" {
				t.Errorf("%s: -ea %s; want assert", name, d)
			}
			if r != "saved" && r != "roundtrip" {
				t.Errorf("%s: release %s; want the save to write, throwing nothing", name, r)
			}
		}
	}
}
