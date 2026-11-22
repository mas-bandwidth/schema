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
// The schema (the JavaScript leg's, jstable compressed_float_negzero_f32) and
// one row per case are the committed corpus
// testdata/compressed-float-f32/{Probe.schema,cases.tsv} (F-203); the driver
// below runs every row under the JDK.
const javaCompressedDir = "testdata/compressed-float-f32"

const javaCompressedDriver = `package probe;

import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Arrays;

public final class Driver {
    static void say(String label, String why) {
        System.out.println("CHECK " + label + (why.isEmpty() ? " ok" : " FAIL " + why));
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

    static long id(String field) {
        switch (field) {
            case "z": return ProbeVariable.F_z;
            case "nz": return ProbeVariable.F_nz;
            case "tenth": return ProbeVariable.F_tenth;
            default: throw new IllegalArgumentException("no scalar field " + field);
        }
    }

    static float get(ProbeVariable.Value v, String field) {
        switch (field) {
            case "z": return v.z;
            case "nz": return v.nz;
            case "tenth": return v.tenth;
            case "list": return v.list.get(0);
            default: throw new IllegalArgumentException("no field " + field);
        }
    }

    static void set(ProbeVariable.Value v, String field, float x) {
        switch (field) {
            case "z": v.z = x; break;
            case "nz": v.nz = x; break;
            case "tenth": v.tenth = x; break;
            case "list": v.list.add(x); break;
            default: throw new IllegalArgumentException("no field " + field);
        }
    }

    static int bits(String hex) { return Integer.parseUnsignedInt(hex.substring(2), 16); }

    static String held(ProbeVariable.Value v, String field, String want) {
        float got = get(v, field);
        int gb = Float.floatToRawIntBits(got);
        return gb == bits(want) ? "" : "landed " + got + " bits 0x" + Integer.toHexString(gb) + ", want " + want;
    }

    static String reason(String got, String want) {
        return want.equals("-") || want.equals(got) ? "" : "refused with " + got + ", want " + want;
    }

    static String variableRead(String field, String input, String want, String why) {
        int in = bits(input);
        byte[] wire;
        if (field.equals("list")) {
            ProbeVariable.Value seed = new ProbeVariable.Value();
            seed.list.add(SENTINEL);
            byte[] saved = ProbeVariable.save(seed);
            if (saved == null) { return "the save refused"; }
            wire = plant(saved, in);
        } else {
            wire = scalar(id(field), in);
        }
        TableVariable.Report r = new TableVariable.Report();
        ProbeVariable.Value v = ProbeVariable.load(wire, r);
        if (want.equals("refused")) {
            return v == null && r.refused ? reason(r.reason, why) : "loaded=" + (v != null) + " refused=" + r.refused + " reason=" + r.reason;
        }
        if (v == null || r.refused || r.damaged) { return "refused: " + r.reason; }
        return held(v, field, want);
    }

    static String variableWrite(String field, String input, String want) {
        int in = bits(input);
        ProbeVariable.Value v = new ProbeVariable.Value();
        set(v, field, Float.intBitsToFloat(in));
        byte[] wire = ProbeVariable.save(v);
        if (want.equals("refused")) { return wire == null ? "" : "the save wrote a value past max"; }
        if (wire == null) { return "the save refused"; }
        if (want.equals("elided")) {
            return Arrays.equals(wire, ProbeVariable.save(new ProbeVariable.Value())) ? "" : "the save wrote the value under its default";
        }
        if (in == 0x80000000) {
            for (int i = 0; i + 4 <= wire.length; i++) {
                if (wire[i] == 0 && wire[i + 1] == 0 && wire[i + 2] == 0 && wire[i + 3] == (byte) 0x80) { return "the save stored -0's bits"; }
            }
        }
        TableVariable.Report r = new TableVariable.Report();
        ProbeVariable.Value back = ProbeVariable.load(wire, r);
        if (back == null || r.refused || r.damaged) { return "its own save refused on read: " + r.reason; }
        return held(back, field, want);
    }

    static String jsonRead(String field, String text, String want, String why) {
        TableVariable.Report r = new TableVariable.Report();
        ProbeVariable.Value v = ProbeVariable.fromJson(text.getBytes(StandardCharsets.UTF_8), r);
        if (want.equals("refused")) {
            return r.refused ? reason(r.reason, why) : "refused=" + r.refused + " reason=" + r.reason + " loaded=" + (v != null);
        }
        if (v == null || r.refused) { return "refused: " + r.reason; }
        return held(v, field, want);
    }

    public static void main(String[] args) throws Exception {
        for (String line : Files.readAllLines(Path.of(args[0]), StandardCharsets.UTF_8)) {
            if (line.isEmpty() || line.startsWith("#")) { continue; }
            String[] f = line.split("\t", -1);
            String label = f[0], op = f[1], field = f[2], input = f[3], want = f[4], why = f[5];
            try {
                switch (op) {
                    case "variable-read": say(label, variableRead(field, input, want, why)); break;
                    case "variable-write": say(label, variableWrite(field, input, want)); break;
                    case "json-read": say(label, jsonRead(field, input, want, why)); break;
                    default: say(label, "no op " + op);
                }
            } catch (Throwable e) {
                say(label, "threw " + e);
            }
        }
    }
}
`

// javaCompressedCovers is what the committed rows must hold between them:
// every form and operation the fix changed, each rule at the float32 end, past
// it, and at -0, with a lawful control.
var javaCompressedCovers = []string{
	"variable-read:negzero", "variable-read:negzero-nonzero-default", "variable-read:f32-end",
	"variable-read:past-end", "variable-read:list-negzero", "variable-read:list-past-end",
	"variable-write:f32-end", "variable-write:past-end", "variable-write:negzero-nonzero-default",
	"variable-write:negzero-elided", "variable-write:list-negzero",
	"json-read:f32-end", "json-read:past-end", "json-read:negzero",
	"json-read:list-f32-end", "json-read:list-negzero", "json-read:list-past-end",
}

// TestJavaCompressedFloatVariableAndJson pins the emitted shape (pure) and
// runs every committed case under the JDK (gated).
func TestJavaCompressedFloatVariableAndJson(t *testing.T) {
	t.Parallel()
	casesPath := filepath.Join(javaCompressedDir, "cases.tsv")
	rows := corpusRows(t, casesPath, 6)
	inCorpus := map[string]bool{}
	for _, r := range rows {
		inCorpus[r[0]] = true
	}
	for _, label := range javaCompressedCovers {
		if !inCorpus[label] {
			t.Errorf("%s has no %s row", casesPath, label)
		}
	}
	u := unitFrom(t, corpusSchema(t, javaCompressedDir, "Probe.schema"))
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
		`TableVariable.compressedF32(java.lang.Float.intBitsToFloat(fbits), ` + lo + `, ` + hi + `, report, "Probe.tenth")`,
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
		cases, err := filepath.Abs(casesPath)
		if err != nil {
			t.Fatal(err)
		}
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
		out, err := testproc.Command(t, java, "-ea", "-cp", dir, u.Package+".Driver", cases).CombinedOutput()
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
		for _, r := range rows {
			if !seen[r[0]] {
				t.Errorf("the probe made no %s check\n%s", r[0], out)
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
