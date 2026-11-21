package javatable

import (
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// A COMPRESSED FLOAT ON THE VARIABLE AND JSON FORMS of the Java leg holds two
// rules (docs/SPEC-TABLES.md §3.3; SPEC.md §4.6; ruling
// fixed-compressed-negzero), the cross-leg cells float32-range and
// negative-zero:
//
//   - its range is held at float32: the float32 0.1f is inside max = 0.1 on
//     the variable read, the variable write and the JSON read, and the next
//     float32 past it is refused out_of_range on both reads, naming the field;
//   - its -0 IS +0: the variable and JSON reads land it as +0, the variable
//     save stores +0 for it (as a scalar under a non-zero default and as a
//     list element) and elides it under a +0 default.
//
// The schema is the JavaScript leg's (jstable compressed_float_negzero_f32).
const javaCompressedSchema = `package probe

table Probe
{
    z     float32 | min = -1, max = 1, resolution = 0.01
    nz    float32 = 0.5 | min = -1, max = 1, resolution = 0.01
    tenth float32 | min = 0, max = 0.1, resolution = 0.01
    list  [..4]float32 | min = -1, max = 0.1, resolution = 0.01
}
`

const javaCompressedDriver = `package probe;

import java.nio.charset.StandardCharsets;
import java.util.Arrays;

public final class Driver {
    static void say(String label, String why) {
        System.out.println("CHECK " + label + (why.isEmpty() ? " ok" : " FAIL " + why));
    }

    interface Check { String run() throws Exception; }

    static void check(String label, Check c) {
        try {
            say(label, c.run());
        } catch (Throwable e) {
            say(label, "threw " + e);
        }
    }

    static byte[] scalar(long id, int bits) {
        TableVariable.Writer w = new TableVariable.Writer(new byte[4096], new TableVariable.Ids(64));
        w.put8(1);
        w.putLeb(w.ref(id)); w.put8(10); w.put32(bits);
        w.put8(0);
        w.trailer();
        return Arrays.copyOf(w.buf, w.off);
    }

    // a SENTINEL is a value whose bits are found once in the saved wire,
    // overwritten where they lie
    static final float SENTINEL = 0.0123f;

    static byte[] plant(byte[] wire, int bits) {
        int sentinel = Float.floatToRawIntBits(SENTINEL);
        int found = -1;
        for (int i = 0; i + 4 <= wire.length; i++) {
            boolean same = true;
            for (int k = 0; k < 4 && same; k++) { same = wire[i + k] == (byte) (sentinel >>> (8 * k)); }
            if (same) {
                if (found >= 0) { throw new IllegalStateException("the sentinel lies twice"); }
                found = i;
            }
        }
        if (found < 0) { throw new IllegalStateException("the sentinel is not in the wire"); }
        byte[] out = wire.clone();
        for (int k = 0; k < 4; k++) { out[found + k] = (byte) (bits >>> (8 * k)); }
        return out;
    }

    static ProbeVariable.Value load(byte[] wire, TableVariable.Report r) {
        return ProbeVariable.load(wire, r);
    }

    static String lands(byte[] wire, java.util.function.Function<ProbeVariable.Value, Float> get, float want) {
        TableVariable.Report r = new TableVariable.Report();
        ProbeVariable.Value v = load(wire, r);
        if (v == null || r.refused || r.damaged) { return "refused: " + r.reason; }
        float got = get.apply(v);
        return Float.floatToRawIntBits(got) == Float.floatToRawIntBits(want) ? "" : "landed " + got + " bits 0x" + Integer.toHexString(Float.floatToRawIntBits(got)) + ", want " + want;
    }

    static String refuses(TableVariable.Report r, Object v, String reason) {
        if (v == null && r.refused && reason.equals(r.reason)) { return ""; }
        return "loaded=" + (v != null) + " refused=" + r.refused + " reason=" + r.reason;
    }

    static ProbeVariable.Value json(String text, TableVariable.Report r) {
        return ProbeVariable.fromJson(text.getBytes(StandardCharsets.UTF_8), r);
    }

    static String jsonLands(String text, java.util.function.Function<ProbeVariable.Value, Float> get, float want) {
        TableVariable.Report r = new TableVariable.Report();
        ProbeVariable.Value v = json(text, r);
        if (v == null || r.refused) { return "refused: " + r.reason; }
        float got = get.apply(v);
        return Float.floatToRawIntBits(got) == Float.floatToRawIntBits(want) ? "" : "landed " + got + ", want " + want;
    }

    public static void main(String[] args) {
        final float past = Math.nextUp(0.1f);

        check("variable-read:negzero", () -> lands(scalar(ProbeVariable.F_z, 0x80000000), v -> v.z, 0.0f));
        check("variable-read:negzero-nonzero-default", () -> lands(scalar(ProbeVariable.F_nz, 0x80000000), v -> v.nz, 0.0f));
        check("variable-read:f32-end", () -> lands(scalar(ProbeVariable.F_tenth, Float.floatToRawIntBits(0.1f)), v -> v.tenth, 0.1f));
        check("variable-read:past-end", () -> {
            TableVariable.Report r = new TableVariable.Report();
            return refuses(r, load(scalar(ProbeVariable.F_tenth, Float.floatToRawIntBits(past)), r), "out_of_range: Probe.tenth");
        });
        check("variable-read:list-negzero", () -> {
            ProbeVariable.Value v = new ProbeVariable.Value();
            v.list.add(SENTINEL);
            byte[] wire = ProbeVariable.save(v);
            if (wire == null) { return "the save refused"; }
            return lands(plant(wire, 0x80000000), x -> x.list.get(0), 0.0f);
        });
        check("variable-read:list-past-end", () -> {
            ProbeVariable.Value v = new ProbeVariable.Value();
            v.list.add(SENTINEL);
            byte[] wire = ProbeVariable.save(v);
            if (wire == null) { return "the save refused"; }
            TableVariable.Report r = new TableVariable.Report();
            return refuses(r, load(plant(wire, Float.floatToRawIntBits(past)), r), "out_of_range: Probe.list");
        });

        check("variable-write:f32-end", () -> {
            ProbeVariable.Value v = new ProbeVariable.Value();
            v.tenth = 0.1f;
            v.list.add(0.1f);
            byte[] wire = ProbeVariable.save(v);
            if (wire == null) { return "the save refused 0.1f under max = 0.1"; }
            return lands(wire, x -> x.tenth, 0.1f);
        });
        check("variable-write:past-end", () -> {
            ProbeVariable.Value v = new ProbeVariable.Value();
            v.tenth = past;
            return ProbeVariable.save(v) == null ? "" : "the save wrote a value past max";
        });
        check("variable-write:negzero-nonzero-default", () -> {
            ProbeVariable.Value v = new ProbeVariable.Value();
            v.nz = -0.0f;
            byte[] wire = ProbeVariable.save(v);
            if (wire == null) { return "the save refused"; }
            for (int i = 0; i + 4 <= wire.length; i++) {
                if (wire[i] == 0 && wire[i + 1] == 0 && wire[i + 2] == 0 && wire[i + 3] == (byte) 0x80) { return "the save stored -0's bits"; }
            }
            return lands(wire, x -> x.nz, 0.0f);
        });
        check("variable-write:negzero-elided", () -> {
            ProbeVariable.Value v = new ProbeVariable.Value();
            v.z = -0.0f;
            byte[] wire = ProbeVariable.save(v);
            byte[] plain = ProbeVariable.save(new ProbeVariable.Value());
            return Arrays.equals(wire, plain) ? "" : "a -0 under a +0 default was written";
        });
        check("variable-write:list-negzero", () -> {
            ProbeVariable.Value v = new ProbeVariable.Value();
            v.list.add(-0.0f);
            byte[] wire = ProbeVariable.save(v);
            if (wire == null) { return "the save refused"; }
            for (int i = 0; i + 4 <= wire.length; i++) {
                if (wire[i] == 0 && wire[i + 1] == 0 && wire[i + 2] == 0 && wire[i + 3] == (byte) 0x80) { return "the save stored -0's bits"; }
            }
            return "";
        });

        check("json-read:f32-end", () -> jsonLands("{\"tenth\":0.1}", v -> v.tenth, 0.1f));
        check("json-read:past-end", () -> {
            TableVariable.Report r = new TableVariable.Report();
            ProbeVariable.Value v = json("{\"tenth\":" + past + "}", r);
            return r.refused && ("out_of_range: tenth").equals(r.reason) ? "" : "refused=" + r.refused + " reason=" + r.reason + " loaded=" + (v != null);
        });
        check("json-read:negzero", () -> jsonLands("{\"z\":-0}", v -> v.z, 0.0f));
        check("json-read:list-f32-end", () -> jsonLands("{\"list\":[0.1]}", v -> v.list.get(0), 0.1f));
        check("json-read:list-negzero", () -> jsonLands("{\"list\":[-0]}", v -> v.list.get(0), 0.0f));
        check("json-read:list-past-end", () -> {
            TableVariable.Report r = new TableVariable.Report();
            json("{\"list\":[" + past + "]}", r);
            return r.refused && ("out_of_range: list").equals(r.reason) ? "" : "refused=" + r.refused + " reason=" + r.reason;
        });
    }
}
`

var javaCompressedLabels = []string{
	"variable-read:negzero", "variable-read:negzero-nonzero-default", "variable-read:f32-end",
	"variable-read:past-end", "variable-read:list-negzero", "variable-read:list-past-end",
	"variable-write:f32-end", "variable-write:past-end", "variable-write:negzero-nonzero-default",
	"variable-write:negzero-elided", "variable-write:list-negzero",
	"json-read:f32-end", "json-read:past-end", "json-read:negzero",
	"json-read:list-f32-end", "json-read:list-negzero", "json-read:list-past-end",
}

// TestJavaCompressedFloatVariableAndJson pins the emitted shape (pure) and
// runs the probe under the JDK (gated).
func TestJavaCompressedFloatVariableAndJson(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, javaCompressedSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	var all strings.Builder
	for _, name := range sortedNames(files) {
		all.Write(files[name])
	}
	squeeze := func(s string) string { return strings.Join(strings.Fields(s), "") }
	src := squeeze(all.String())
	lo, hi := fixedFloatLit(0), fixedFloatLit(0.1)
	for _, want := range []string{
		`TableVariable.compressedF32(Float.intBitsToFloat(fbits), ` + lo + `, ` + hi + `, report, "Probe.tenth")`,
		`TableVariable.compressedF32(placed, ` + lo + `, ` + hi + `, in.rep, "tenth")`,
		`putF32In(v.tenth, ` + lo + `, ` + hi + `)`,
		`public void putF32In(float v, float lo, float hi)`,
	} {
		if !strings.Contains(src, squeeze(want)) {
			t.Errorf("the generated Java lacks %s", want)
		}
	}

	t.Run("jdk", func(t *testing.T) {
		slowtest.Gate(t, "the JDK (it compiles and runs the generated Java)")
		javac, java := javaTools(t)
		files["Driver.java"] = []byte(javaCompressedDriver)
		dir := t.TempDir()
		var sources []string
		for name, data := range files {
			p := filepath.Join(dir, name)
			if err := os.WriteFile(p, data, 0o644); err != nil {
				t.Fatal(err)
			}
			if strings.HasSuffix(name, ".java") {
				sources = append(sources, p)
			}
		}
		sort.Strings(sources)
		args := append([]string{"--release", "17", "-nowarn", "-d", dir}, sources...)
		if out, err := testproc.Command(t, javac, args...).CombinedOutput(); err != nil {
			t.Fatalf("javac: %v\n%s", err, out)
		}
		out, err := testproc.Command(t, java, "-ea", "-cp", dir, u.Package+".Driver").CombinedOutput()
		if err != nil {
			t.Fatalf("java: %v\n%s", err, out)
		}
		seen := map[string]bool{}
		for line := range strings.SplitSeq(string(out), "\n") {
			rest, ok := strings.CutPrefix(line, "CHECK ")
			if !ok {
				continue
			}
			label, verdict, _ := strings.Cut(rest, " ")
			seen[label] = true
			if verdict != "ok" {
				t.Errorf("%s: %s", label, strings.TrimPrefix(verdict, "FAIL "))
			}
		}
		for _, label := range javaCompressedLabels {
			if !seen[label] {
				t.Errorf("the probe made no %s check\n%s", label, out)
			}
		}
	})
}

func sortedNames(files map[string][]byte) []string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
