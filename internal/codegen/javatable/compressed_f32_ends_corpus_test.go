package javatable

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// A COMPRESSED FLOAT'S RANGE IS HELD AT FLOAT32 (docs/SPEC-TABLES.md §3.3),
// on the Java leg, run on the cross-port corpus
// testdata/conformance/feature-witnesses/compressed-f32-ends: every row of its
// cases.tsv runs through the generated Java under the JDK (-ea, so the
// debug-only write checks are on). internal/tablewire's
// TestCompressedF32EndsCorpus is the reference runner; the row logic is its.
//
// The fixed root's JSON is read the way the leg reads a fixed root's text
// (FVariable.fromJson) and carried into FFixed.Value field by field, so the
// fixed save is the fixed writer's.
const javaF32EndsDir = "../../../testdata/conformance/feature-witnesses/compressed-f32-ends"

const javaF32EndsDriver = `package f32ends;

import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Arrays;

public final class F32EndsDriver {
    static Path dir;
    static String reason = "";

    static byte[] file(String name) throws Exception { return Files.readAllBytes(dir.resolve(name)); }

    static String hex(byte[] b) {
        if (b == null) { return "null"; }
        StringBuilder s = new StringBuilder();
        for (byte x : b) { s.append(String.format("%02x", x & 0xff)); }
        return s.toString();
    }

    static String same(byte[] got, String want) throws Exception {
        byte[] w = file(want);
        return Arrays.equals(got, w) ? "" : "saved " + hex(got) + ", want " + want + " " + hex(w);
    }

    static byte[] saveFixed(FFixed.Value v) {
        FFixed.Value[] vs = { v };
        byte[] buf = new byte[FFixed.measure(1)];
        int n = FFixed.save(vs, 1, buf);
        return n < 0 ? null : Arrays.copyOf(buf, n);
    }

    static FFixed.Value carry(FVariable.Value v) {
        FFixed.Value f = new FFixed.Value();
        f.r = v.r;
        for (int i = 0; i < f.xs.length && i < v.xs.size(); i++) { f.xs[i] = v.xs.get(i); }
        return f;
    }

    static VVariable.Value jsonV(String stem) throws Exception {
        TableVariable.Report r = new TableVariable.Report();
        VVariable.Value v = VVariable.fromJson(file(stem + ".json"), r);
        reason = String.valueOf(r.reason);
        return v == null || r.refused || r.damaged ? null : v;
    }

    static FFixed.Value jsonF(String stem) throws Exception {
        TableVariable.Report r = new TableVariable.Report();
        FVariable.Value v = FVariable.fromJson(file(stem + ".json"), r);
        reason = String.valueOf(r.reason);
        return v == null || r.refused || r.damaged ? null : carry(v);
    }

    static FFixed.Value readFixed(String stem) throws Exception {
        FFixed.Value[] vs = { new FFixed.Value() };
        TableFixed.Report r = new TableFixed.Report();
        int n = FFixed.load(vs, 1, file(stem + ".fixed.bin"), null, null, FFixed.image(), r);
        reason = String.valueOf(r.reason);
        return n == 1 && !r.refused && !r.damaged ? vs[0] : null;
    }

    static VVariable.Value readVariable(String stem) throws Exception {
        TableVariable.Report r = new TableVariable.Report();
        VVariable.Value v = VVariable.load(file(stem + ".variable.bin"), r);
        reason = String.valueOf(r.reason);
        return v == null || r.refused || r.damaged ? null : v;
    }

    static String read(String form, String root, String input, String expect) throws Exception {
        byte[] saved;
        boolean got;
        if (root.equals("V")) {
            VVariable.Value v = form.equals("json") ? jsonV(input) : readVariable(input);
            got = v != null;
            saved = got && !expect.equals("refused") ? VVariable.save(v) : null;
        } else {
            FFixed.Value v = form.equals("json") ? jsonF(input) : readFixed(input);
            got = v != null;
            saved = got && !expect.equals("refused") ? saveFixed(v) : null;
        }
        if (expect.equals("refused")) {
            if (got) { return "the " + form + " read was not refused"; }
            // Only the verdict must agree across ports (docs/rulings.json,
            // refusal-verdict-only): the reason is this port's own.
            return "";
        }
        if (!got) { return "the " + form + " read refused the value at the float32 end"; }
        if (saved == null) { return "saving again what was just read was refused"; }
        return same(saved, expect + "." + (root.equals("V") ? "variable" : "fixed") + ".bin");
    }

    static String write(String form, String root, String input, String path, String bits, String expect) throws Exception {
        float x = Float.intBitsToFloat(Integer.parseUnsignedInt(bits, 16));
        byte[] saved;
        try {
            if (form.equals("variable")) {
                VVariable.Value v = jsonV(input);
                if (v == null) { return input + ".json is refused"; }
                if (path.equals("r")) { v.r = x; } else { v.xs.set(index(path), x); }
                saved = VVariable.save(v);
            } else {
                FFixed.Value v = jsonF(input);
                if (v == null) { return input + ".json is refused"; }
                if (path.equals("r")) { v.r = x; } else { v.xs[index(path)] = x; }
                saved = saveFixed(v);
            }
        } catch (AssertionError e) {
            saved = null;
        }
        if (expect.equals("refused")) { return saved == null ? "" : "the " + form + " save was not refused: " + hex(saved); }
        if (saved == null) { return "the " + form + " save was refused"; }
        return same(saved, expect + "." + form + ".bin");
    }

    static int index(String path) {
        return Integer.parseInt(path.substring(path.indexOf('[') + 1, path.indexOf(']')));
    }

    public static void main(String[] args) throws Exception {
        dir = Path.of(args[0]);
        for (String line : Files.readAllLines(dir.resolve("cases.tsv"))) {
            if (line.isEmpty() || line.startsWith("#")) { continue; }
            String[] f = line.split("\t", -1);
            String why;
            try {
                why = f[1].equals("read") ? read(f[2], f[3], f[4], f[7]) : write(f[2], f[3], f[4], f[5], f[6], f[7]);
            } catch (Throwable e) {
                why = "threw " + e;
            }
            System.out.println("CHECK " + f[0] + (why.isEmpty() ? " ok" : " FAIL " + why));
        }
    }
}
`

// TestJavaCompressedF32EndsCorpus runs every row of the cross-port corpus under
// the JDK (gated).
func TestJavaCompressedF32EndsCorpus(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the JDK (it compiles and runs the generated Java)")
	rows := corpusRows(t, filepath.Join(javaF32EndsDir, "cases.tsv"), 8)
	u := unitFromPath(t, filepath.Join(javaF32EndsDir, "Probe.schema"))
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	javac, java := javaTools(t)
	corpus, err := filepath.Abs(javaF32EndsDir)
	if err != nil {
		t.Fatal(err)
	}
	files["F32EndsDriver.java"] = []byte(javaF32EndsDriver)
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
	out, err := testproc.Command(t, java, "-ea", "-cp", dir, u.Package+".F32EndsDriver", corpus).CombinedOutput()
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
			t.Errorf("the driver made no %s check\n%s", r[0], out)
		}
	}
}
