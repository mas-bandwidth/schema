package javatable

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	javacodegen "github.com/mas-bandwidth/schema/v2/internal/codegen/java"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// A COMPRESSED FLOAT'S -0 IS WRITTEN AS +0 BY EVERY JAVA WRITER
// (docs/SPEC-TABLES.md §3.3, the special values; docs/rulings.json
// fixed-compressed-negzero). The variable save and its retaining twin, the
// text writer, the cook, the fixed save and the message form's elision stored
// a compressed -0 as held. The plain float `p` is the control: its -0 is a
// value and rides bit-exact. The normalisation is a value rule, not a check:
// it holds in every build, with -ea and without. The Go reference engine's
// corpus, testdata/conformance/feature-witnesses/compressed-negzero-go, is the
// worked example; its 15 rows run here too.
const javaNegZeroCorpus = "../../../testdata/conformance/feature-witnesses/compressed-negzero-go"

// TestJavaCompressedFloatStoresAreGuarded is the fast half: it reads the
// generated source rather than running it. Every bit pattern taken of a
// float value (elision and the fixed store) and every text spelling of one
// goes through the +0 test for a compressed float and not for the plain one;
// the ranged put the variable form writes through, and the cook, normalise it
// in the runtime.
func TestJavaCompressedFloatStoresAreGuarded(t *testing.T) {
	t.Parallel()
	schema, err := os.ReadFile(filepath.Join(javaNegZeroCorpus, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(unitFrom(t, string(schema)))
	if err != nil {
		t.Fatal(err)
	}
	use := regexp.MustCompile(`(floatToRawIntBits\(|TableJson\.writeFloat\(b, )\s*`)
	guard := regexp.MustCompile(`^\([^()]*(\([^()]*\))? == 0\.0f \? 0\.0f : `)
	literal := regexp.MustCompile(`^(0\.0f\)|0\.05f\)|(java\.lang\.)?Float\.intBitsToFloat\()`)
	guarded, plain := 0, 0
	var runtime strings.Builder
	for name, data := range files {
		src := string(data)
		if strings.HasPrefix(name, "Table") {
			runtime.WriteString(src)
			continue
		}
		for _, at := range use.FindAllStringIndex(src, -1) {
			rest := src[at[1]:]
			line := strings.Count(src[:at[0]], "\n") + 1
			switch {
			case strings.HasPrefix(rest, "v.p)") || strings.HasPrefix(rest, "v.p,"):
				plain++
			case literal.MatchString(rest):
			case guard.MatchString(rest):
				guarded++
			default:
				t.Errorf("%s:%d: a float is stored as held: %.60s", name, line, rest)
			}
		}
	}
	if guarded == 0 || plain == 0 {
		t.Errorf("the writers have %d guarded compressed stores and %d plain ones; want both", guarded, plain)
	}
	if !strings.Contains(string(files["VarRow.java"]), ".compressed()") {
		t.Errorf("VarRow.java marks no compressed cook descriptor")
	}
	for _, want := range []string{
		"putF32(v == 0.0f ? 0.0f : v);",
		"putF64(v == 0.0 ? 0.0 : v);",
		"if (compressed && raw == (width == 4 ? 0x80000000L : 0x8000000000000000L)) {",
	} {
		if !strings.Contains(runtime.String(), want) {
			t.Errorf("the runtime does not carry %q", want)
		}
	}
}

// TestJavaCompressedNegZeroWriters runs the corpus's 15 rows and every
// writer's -0/+0 twin under the JVM, with -ea and without.
func TestJavaCompressedNegZeroWriters(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "javac and java (it runs the generated classes)")
	javac, javaBin := javaTools(t)
	schema, err := os.ReadFile(filepath.Join(javaNegZeroCorpus, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	corpus, err := filepath.Abs(javaNegZeroCorpus)
	if err != nil {
		t.Fatal(err)
	}
	u := unitFrom(t, string(schema))
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	pkt, err := javacodegen.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range pkt {
		if _, ok := files[name]; !ok {
			files[name] = data
		}
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "src", u.Package)
	classes := filepath.Join(dir, "classes")
	for _, d := range []string{src, classes} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	sources := []string{filepath.Join(dir, "src", "NegZeroWriters.java")}
	if err := os.WriteFile(sources[0], []byte(javaNegZeroProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		if !strings.HasSuffix(name, ".java") || strings.Contains(name, "/") {
			continue
		}
		path := filepath.Join(src, name)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, path)
	}
	args := append([]string{"--release", "17", "-nowarn", "-encoding", "UTF-8", "-d", classes}, sources...)
	if out, err := testproc.Command(t, javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	for _, flags := range [][]string{{"-ea"}, {}} {
		cmd := testproc.Command(t, javaBin, append(flags, "-cp", classes, "NegZeroWriters", corpus)...)
		out, err := cmd.CombinedOutput()
		if err != nil || !strings.Contains(string(out), "\nPASS rows=15 twins=84\n") {
			t.Fatalf("java %v: %v\n%s", flags, err, out)
		}
	}
}

const javaNegZeroProbe = `
import nzgo.*;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.List;
import java.util.function.BiConsumer;
import java.util.function.Function;
import java.util.function.Supplier;

public final class NegZeroWriters {
    static int bad = 0;

    static void fail(String s) {
        System.out.println("FAIL " + s);
        bad++;
    }

    static String hex(byte[] b) {
        StringBuilder s = new StringBuilder();
        for (byte x : b) {
            s.append(String.format("%02x", x & 0xff));
        }
        return s.toString();
    }

    static byte[] cook(Function<Object, Long> measure, Object v, int order, boolean var) {
        long n = measure.apply(v);
        if (n < 0) {
            return null;
        }
        byte[] out = new byte[(int) n];
        boolean ok = var ? VarCook.cookFrom(v, out, order) : FixCook.cookFrom(v, out, order);
        return ok ? out : null;
    }

    static byte[] fixed(FixFixed.Value v) {
        byte[] out = new byte[FixFixed.measure(1)];
        return FixFixed.save(new FixFixed.Value[] {v}, 1, out) == out.length ? out : null;
    }

    static byte[] message(FixFixed.Value v) {
        TableVocabulary.Report r = new TableVocabulary.Report();
        FixFixed.Value[] values = {v};
        int n = FixMessage.measureMessages(values, r);
        if (n < 0) {
            return null;
        }
        byte[] out = new byte[n];
        return FixMessage.saveMessages(values, out, r) == n ? out : null;
    }

    static int slotOf(String index) {
        if (index.equals("Low")) {
            return 0;
        }
        if (index.equals("High")) {
            return 1;
        }
        return Integer.parseInt(index);
    }

    // THE CORPUS: load <base>, set <path> to -0 in the native value, save
    // <form> and owe <expect>.<form>.bin. A fixed row loads <base>.fixed.bin,
    // which the Go reference engine pins as its encoding of <base>.json.
    static void setVariable(Object o, String path) {
        float z = -0.0f;
        boolean var = o instanceof VarVariable.Value;
        VarVariable.Value vv = var ? (VarVariable.Value) o : null;
        FixVariable.Value fv = var ? null : (FixVariable.Value) o;
        int dot = path.indexOf('.');
        if (dot >= 0) {
            PickVariable.Value pick = var ? vv.pick : fv.pick;
            if (pick.tag != 1) {
                throw new IllegalStateException(path + ": the base does not set arm v");
            }
            pick.v = z;
            return;
        }
        int br = path.indexOf('[');
        if (br < 0) {
            switch (path) {
                case "a": if (var) { vv.a = z; } else { fv.a = z; } return;
                case "z": if (var) { vv.z = z; } else { fv.z = z; } return;
                case "p": if (var) { vv.p = z; } else { fv.p = z; } return;
                default: throw new IllegalStateException(path + ": no such leaf");
            }
        }
        String name = path.substring(0, br);
        int slot = slotOf(path.substring(br + 1, path.length() - 1));
        List<Float> list = name.equals("xs") ? (var ? vv.xs : fv.xs) : name.equals("ys") ? (var ? vv.ys : fv.ys) : (var ? vv.ks : fv.ks);
        if (slot >= list.size()) {
            throw new IllegalStateException(path + ": slot " + slot + " of " + list.size());
        }
        list.set(slot, z);
    }

    static void setFixed(FixFixed.Value v, String path) {
        float z = -0.0f;
        int dot = path.indexOf('.');
        if (dot >= 0) {
            if (v.pick.type != 1) {
                throw new IllegalStateException(path + ": the base does not set arm v");
            }
            v.pick.v = z;
            return;
        }
        int br = path.indexOf('[');
        if (br < 0) {
            switch (path) {
                case "a": v.a = z; return;
                case "z": v.z = z; return;
                case "p": v.p = z; return;
                default: throw new IllegalStateException(path + ": no such leaf");
            }
        }
        String name = path.substring(0, br);
        int slot = slotOf(path.substring(br + 1, path.length() - 1));
        float[] list = name.equals("xs") ? v.xs : name.equals("ys") ? v.ys : v.ks;
        list[slot] = z;
    }

    record Twin(String name, boolean plain, BiConsumer<Object, Float> setV, BiConsumer<FixFixed.Value, Float> setF) {}

    record Writer(String root, String name, boolean fixedForm, Supplier<Object> make, Function<Object, byte[]> write) {}

    static List<Float> floats(float... xs) {
        List<Float> out = new ArrayList<>();
        for (float x : xs) {
            out.add(x);
        }
        return out;
    }

    public static void main(String[] args) throws Exception {
        Path dir = Path.of(args[0]);
        int rows = 0;
        for (String line : Files.readAllLines(dir.resolve("cases.tsv"))) {
            if (line.isEmpty() || line.startsWith("#")) {
                continue;
            }
            String[] col = line.split("\t");
            String name = col[0], form = col[1], root = col[2], base = col[3], path = col[4], expect = col[5];
            byte[] want = Files.readAllBytes(dir.resolve(expect + "." + form + ".bin"));
            byte[] got;
            try {
                if (form.equals("variable")) {
                    byte[] text = Files.readAllBytes(dir.resolve(base + ".json"));
                    TableVariable.Report r = new TableVariable.Report();
                    Object v = root.equals("Var") ? VarVariable.fromJson(text, r) : FixVariable.fromJson(text, r);
                    if (v == null || r.refused || r.damaged) {
                        fail(name + ": " + base + ".json does not read: " + r.reason);
                        continue;
                    }
                    setVariable(v, path);
                    got = root.equals("Var") ? VarVariable.save((VarVariable.Value) v) : FixVariable.save((FixVariable.Value) v);
                } else {
                    byte[] data = Files.readAllBytes(dir.resolve(base + ".fixed.bin"));
                    FixFixed.Value[] back = new FixFixed.Value[8];
                    for (int k = 0; k < back.length; k++) {
                        back[k] = new FixFixed.Value();
                    }
                    TableFixed.Report r = new TableFixed.Report();
                    if (FixFixed.load(back, back.length, data, TableFixed.plan(1 << 14), new short[1 << 14], FixFixed.image(), r) != 1) {
                        fail(name + ": " + base + ".fixed.bin does not load: " + r.reason);
                        continue;
                    }
                    setFixed(back[0], path);
                    got = fixed(back[0]);
                }
            } catch (RuntimeException e) {
                fail(name + ": " + e);
                continue;
            }
            rows++;
            if (got == null) {
                fail(name + ": the " + form + " save refused");
            } else if (!Arrays.equals(got, want)) {
                fail(name + ": the " + form + " save of -0 at " + path + " is not " + expect + "." + form + ".bin:\n have " + hex(got) + "\n want " + hex(want));
            }
        }

        // THE TWINS: every writer saves a value holding -0 in one cell and owes
        // the bytes its +0 twin writes; the plain float p owes different bytes.
        List<Twin> cases = List.of(
                new Twin("leaf_nonzero_default", false,
                        (o, z) -> { if (o instanceof VarVariable.Value v) { v.a = z; } else { ((FixVariable.Value) o).a = z; } },
                        (v, z) -> v.a = z),
                new Twin("leaf_zero_default", false,
                        (o, z) -> { if (o instanceof VarVariable.Value v) { v.z = z; } else { ((FixVariable.Value) o).z = z; } },
                        (v, z) -> v.z = z),
                new Twin("array_element", false,
                        (o, z) -> { if (o instanceof VarVariable.Value v) { v.xs = floats(z, 0.05f); } else { ((FixVariable.Value) o).xs = floats(z, 0.05f); } },
                        (v, z) -> { v.xs[0] = z; v.xs[1] = 0.05f; }),
                new Twin("counted_element", false,
                        (o, z) -> { if (o instanceof VarVariable.Value v) { v.ys = floats(0.05f, z); } else { ((FixVariable.Value) o).ys = floats(0.05f, z); } },
                        (v, z) -> { v.ys[0] = 0.05f; v.ys[1] = z; v.ysCount = 2; }),
                new Twin("keyed_slot", false,
                        (o, z) -> { if (o instanceof VarVariable.Value v) { v.ks = floats(z, 0.05f); } else { ((FixVariable.Value) o).ks = floats(z, 0.05f); } },
                        (v, z) -> { v.ks[0] = z; v.ks[1] = 0.05f; }),
                new Twin("union_arm", false,
                        (o, z) -> { PickVariable.Value p = o instanceof VarVariable.Value v ? v.pick : ((FixVariable.Value) o).pick; p.tag = 1; p.v = z; },
                        (v, z) -> { v.pick.type = 1; v.pick.v = z; }),
                new Twin("plain_control", true,
                        (o, z) -> { if (o instanceof VarVariable.Value v) { v.p = z; } else { ((FixVariable.Value) o).p = z; } },
                        (v, z) -> v.p = z));
        List<Writer> writers = List.of(
                new Writer("Var", "save", false, VarVariable.Value::new, o -> VarVariable.save((VarVariable.Value) o)),
                new Writer("Var", "save_retain", false, VarVariable.Value::new, o -> VarRetain.saveRetain((VarVariable.Value) o, new TableRetain(1 << 12, 64), new TableVariable.Report())),
                new Writer("Var", "json", false, VarVariable.Value::new, o -> VarVariable.toJson((VarVariable.Value) o)),
                new Writer("Var", "cook_le", false, VarVariable.Value::new, o -> cook(VarCook::cookMeasure, o, TableCookWrite.LITTLE, true)),
                new Writer("Var", "cook_be", false, VarVariable.Value::new, o -> cook(VarCook::cookMeasure, o, TableCookWrite.BIG, true)),
                new Writer("Fix", "save", false, FixVariable.Value::new, o -> FixVariable.save((FixVariable.Value) o)),
                new Writer("Fix", "save_retain", false, FixVariable.Value::new, o -> FixRetain.saveRetain((FixVariable.Value) o, new TableRetain(1 << 12, 64), new TableVariable.Report())),
                new Writer("Fix", "json", false, FixVariable.Value::new, o -> FixVariable.toJson((FixVariable.Value) o)),
                new Writer("Fix", "cook_le", false, FixVariable.Value::new, o -> cook(FixCook::cookMeasure, o, TableCookWrite.LITTLE, false)),
                new Writer("Fix", "cook_be", false, FixVariable.Value::new, o -> cook(FixCook::cookMeasure, o, TableCookWrite.BIG, false)),
                new Writer("Fix", "fixed", true, FixFixed.Value::new, o -> fixed((FixFixed.Value) o)),
                new Writer("Fix", "message", true, FixFixed.Value::new, o -> message((FixFixed.Value) o)));
        int twins = 0;
        for (Writer w : writers) {
            for (Twin c : cases) {
                Object plus = w.make().get(), minus = w.make().get();
                if (w.fixedForm()) {
                    c.setF().accept((FixFixed.Value) plus, 0.0f);
                    c.setF().accept((FixFixed.Value) minus, -0.0f);
                } else {
                    c.setV().accept(plus, 0.0f);
                    c.setV().accept(minus, -0.0f);
                }
                String tag = w.root() + "/" + w.name() + "/" + c.name();
                byte[] p, m;
                try {
                    p = w.write().apply(plus);
                    m = w.write().apply(minus);
                } catch (RuntimeException e) {
                    fail(tag + ": " + e);
                    continue;
                }
                twins++;
                if (p == null || m == null) {
                    fail(tag + ": refused");
                    continue;
                }
                boolean same = Arrays.equals(p, m);
                if (c.plain() && same) {
                    fail(tag + ": the plain float's -0 did not ride");
                }
                if (!c.plain() && !same) {
                    fail(tag + ": a compressed float's -0 was not written as +0:\n -0 " + hex(m) + "\n +0 " + hex(p));
                }
            }
        }
        System.out.println(bad == 0 ? "\nPASS rows=" + rows + " twins=" + twins : "\nBAD " + bad);
    }
}
`
