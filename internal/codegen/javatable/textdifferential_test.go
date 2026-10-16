package javatable

// I13 (docs/PORTING.md, "I13 — The text form is held against a third
// implementation") is the text differential: the leg emits random instances as
// (wire, text), the compiler's own engine (internal/tabletext and
// internal/tablepack, written from the spec and from neither backend) reads the
// same wire and writes its text, and the two texts are byte-compared; then the
// engine packs the leg's text back and the wires are compared; then the leg's
// fromJson reads the ENGINE's text and its save is compared with the wire. This
// is the Java leg's cell, in the shape of the Go row
// (internal/codegen/gotable/textdifferential_test.go).
//
// The probe schema is VARIABLE-FORM tables (docs/SPEC-TABLES.md §3, form byte 1;
// §16.7), carrying floats through every float-bearing construct the Java
// variable form admits (variableFormRefusal): scalar float32 and float64
// fields, a nested table by value, a []float32 and a []float64 list, and a list
// of tables. A pinned corpus never reaches a float tie, so the random source is
// built into the probe. Non-finite patterns are redrawn: JSON has no spelling
// for them and both writers refuse them (§16.3), which the probe also checks.
// Instance 0 is the all-default root.

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablepack"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const textDifferentialJavaSchema = `package probe
table Sample {
 s float32
 d float64
}
table Root {
 a float32
 b float64
 pair Sample
 many []float32
 deep []float64
 rows []Sample
}
`

const (
	textDifferentialJavaSeed = 24845619678
	textDifferentialJavaN    = 200
)

// textDifferentialJavaProbe is the Java probe main. `write <dir>` draws N
// random instances (a splitmix64, the seed and N from the environment) and
// writes each one's wire (save) and text (toJson); `read <dir>` reads each
// engine-<i>.json through fromJson and writes the saved wire as back-<i>.wire;
// `refuse <dir>` holds toJson to refusing a non-finite float (§16.3).
const textDifferentialJavaProbe = `import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;

public final class TextDiffProbe {
    private TextDiffProbe() {}

    static long state;

    static long next() {
        state += 0x9E3779B97F4A7C15L;
        long z = state;
        z = (z ^ (z >>> 30)) * 0xBF58476D1CE4E5B9L;
        z = (z ^ (z >>> 27)) * 0x94D049BB133111EBL;
        return z ^ (z >>> 31);
    }

    static float nextFloat32() {
        while (true) {
            float f = Float.intBitsToFloat((int) next());
            if (Float.isFinite(f)) { return f; }
        }
    }

    static double nextFloat64() {
        while (true) {
            double d = Double.longBitsToDouble(next());
            if (Double.isFinite(d)) { return d; }
        }
    }

    static void fail(String what) {
        System.out.println("PROBE FAILED: " + what);
        System.exit(1);
    }

    public static void main(String[] args) throws Exception {
        Path dir = Paths.get(args[1]);
        int n = Integer.parseInt(System.getenv("SCHEMA_TEXT_DIFF_N"));
        if (args[0].equals("write")) {
            state = Long.parseUnsignedLong(System.getenv("SCHEMA_TEXT_DIFF_SEED"));
            for (int i = 0; i < n; i++) {
                probe.RootVariable.Value v = new probe.RootVariable.Value();
                if (i != 0) {
                    v.a = nextFloat32();
                    v.b = nextFloat64();
                    v.pair.s = nextFloat32();
                    v.pair.d = nextFloat64();
                    for (int j = 0; j < 8; j++) { v.many.add(nextFloat32()); }
                    for (int j = 0; j < 8; j++) { v.deep.add(nextFloat64()); }
                    for (int j = 0; j < 3; j++) {
                        probe.SampleVariable.Value s = new probe.SampleVariable.Value();
                        s.s = nextFloat32();
                        s.d = nextFloat64();
                        v.rows.add(s);
                    }
                }
                byte[] wire = probe.RootVariable.save(v);
                if (wire == null) { fail("instance " + i + ": save refused"); }
                byte[] text = probe.RootVariable.toJson(v);
                if (text == null) { fail("instance " + i + ": toJson refused"); }
                Files.write(dir.resolve("inst-" + i + ".wire"), wire);
                Files.write(dir.resolve("inst-" + i + ".json"), text);
            }
            System.out.println("probe wrote " + n + " instances");
            return;
        }
        if (args[0].equals("refuse")) {
            // A non-finite float has no JSON spelling: toJson refuses it (§16.3).
            float[] f32 = { Float.NaN, Float.POSITIVE_INFINITY, Float.NEGATIVE_INFINITY };
            for (float f : f32) {
                probe.RootVariable.Value v = new probe.RootVariable.Value();
                v.many.add(f);
                if (probe.RootVariable.toJson(v) != null) { fail("toJson wrote a non-finite float32 " + f); }
            }
            double[] f64 = { Double.NaN, Double.POSITIVE_INFINITY, Double.NEGATIVE_INFINITY };
            for (double d : f64) {
                probe.RootVariable.Value v = new probe.RootVariable.Value();
                v.pair.d = d;
                if (probe.RootVariable.toJson(v) != null) { fail("toJson wrote a non-finite float64 " + d); }
            }
            System.out.println("probe: toJson refused every non-finite float");
            return;
        }
        if (args[0].equals("read")) {
            for (int i = 0; i < n; i++) {
                byte[] text = Files.readAllBytes(dir.resolve("engine-" + i + ".json"));
                probe.TableVariable.Report r = new probe.TableVariable.Report();
                probe.RootVariable.Value v = probe.RootVariable.fromJson(text, r);
                if (v == null || r.malformed || r.refused || r.unknown != 0 || r.kindMismatch != 0 || r.clamped != 0) {
                    fail("instance " + i + ": fromJson of the engine text reports loss");
                }
                byte[] wire = probe.RootVariable.save(v);
                if (wire == null) { fail("instance " + i + ": save after fromJson refused"); }
                Files.write(dir.resolve("back-" + i + ".wire"), wire);
            }
            System.out.println("probe read " + n + " instances");
            return;
        }
        fail("unknown mode " + args[0]);
    }
}
`

// javaToolsRequired is javaTools, except that SCHEMA_REQUIRE_CORPUS=1 turns
// a missing JDK into a failure by name rather than a skip.
func javaToolsRequired(t *testing.T) (string, string) {
	t.Helper()
	if os.Getenv("SCHEMA_REQUIRE_CORPUS") == "1" {
		ok := false
		if jc, j := os.Getenv("JAVAC"), os.Getenv("JAVA"); jc != "" && j != "" {
			ok = exec.Command(jc, "-version").Run() == nil
		}
		if !ok {
			if jc, err := exec.LookPath("javac"); err == nil {
				if _, err := exec.LookPath("java"); err == nil {
					ok = exec.Command(jc, "-version").Run() == nil
				}
			}
		}
		if !ok {
			t.Fatal("SCHEMA_REQUIRE_CORPUS=1 and no working javac/java (set JAVAC and JAVA): the Java text differential cannot run")
		}
	}
	return javaTools(t)
}

// runTextDifferentialJava generates the probe unit (with an optional in-memory
// edit of the emitted files), compiles it beside the probe main, runs the
// probe's write pass, holds every instance against the engine in both
// directions, then runs the probe's read pass over the engine's texts. It
// returns the divergence messages and a non-nil error when any instance
// differs, so the negative control asserts on what the row itself prints.
func runTextDifferentialJava(t *testing.T, edit func(map[string][]byte)) ([]byte, error) {
	t.Helper()
	javac, javaBin := javaToolsRequired(t)
	u := unitFrom(t, textDifferentialJavaSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if edit != nil {
		edit(files)
	}
	root := t.TempDir()
	srcdir := filepath.Join(root, "src", "probe")
	classes := filepath.Join(root, "classes")
	dir := filepath.Join(root, "inst")
	for _, d := range []string{srcdir, classes, dir} {
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
	probeMain := filepath.Join(root, "TextDiffProbe.java")
	if err := os.WriteFile(probeMain, []byte(textDifferentialJavaProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	args := append([]string{"--release", "17", "-d", classes}, srcs...)
	args = append(args, probeMain)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	env := append(os.Environ(),
		"SCHEMA_TEXT_DIFF_SEED="+strconv.FormatUint(textDifferentialJavaSeed, 10),
		"SCHEMA_TEXT_DIFF_N="+strconv.Itoa(textDifferentialJavaN))
	runProbe := func(mode string) ([]byte, error) {
		cmd := exec.Command(javaBin, "-ea", "-cp", classes, "TextDiffProbe", mode, dir)
		cmd.Env = env
		return cmd.CombinedOutput()
	}
	if out, err := runProbe("write"); err != nil {
		return out, fmt.Errorf("probe write: %w", err)
	}

	m := tabletext.NewModel(u)
	msgs, err := compareTextDifferentialJava(m, dir, textDifferentialJavaN)
	if err != nil {
		return []byte(strings.Join(msgs, "\n\n")), err
	}
	if out, err := runProbe("read"); err != nil {
		return out, fmt.Errorf("probe read: %w", err)
	}
	for i := range textDifferentialJavaN {
		wire, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("inst-%d.wire", i)))
		if err != nil {
			return nil, err
		}
		back, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("back-%d.wire", i)))
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(back, wire) {
			msgs = append(msgs, fmt.Sprintf("instance %d: leg wire saved from fromJson of the engine text differs from leg wire: back=%x leg=%x\nfloats: %s", i, back, wire, floatBitsOfJava(m, wire)))
		}
	}
	if out, err := runProbe("refuse"); err != nil {
		msgs = append(msgs, fmt.Sprintf("probe refuse: %v\n%s", err, out))
	}
	t.Logf("text differential (Java): %d instances, seed %d, both directions and the engine text read back through fromJson", textDifferentialJavaN, textDifferentialJavaSeed)
	if len(msgs) > 0 {
		return []byte(strings.Join(msgs, "\n\n")), fmt.Errorf("%d divergence(s) over %d instances", len(msgs), textDifferentialJavaN)
	}
	return nil, nil
}

// TestTextDifferentialJava is the row: 200 pinned-seed random float instances
// of a variable-form root, held byte for byte against the engine in both
// directions and through the leg's fromJson of the engine's text.
func TestTextDifferentialJava(t *testing.T) {
	t.Parallel()
	out, err := runTextDifferentialJava(t, nil)
	if err != nil {
		t.Fatalf("text differential (Java): %v\n%s", err, out)
	}
}

// textDifferentialJavaAnchor is the float window the emitted writer opens for
// a float32; the control narrows it to six significant digits, so a float32
// that needs seven, eight or nine renders short and only the
// cross-implementation comparison sees it.
const textDifferentialJavaAnchor = "low = 6; high = 9;"

// TestTextDifferentialJavaNegativeControl caps the emitted float32 writer at six
// significant digits in memory. The gate must go red on the TEXT comparison,
// not on a compile error.
func TestTextDifferentialJavaNegativeControl(t *testing.T) {
	t.Parallel()
	out, err := runTextDifferentialJava(t, func(files map[string][]byte) {
		total := 0
		for _, data := range files {
			total += strings.Count(string(data), textDifferentialJavaAnchor)
		}
		if total != 1 {
			t.Fatalf("NEGATIVE CONTROL FAILED: the sabotage anchor %q appeared %d times across the emitted files, want 1", textDifferentialJavaAnchor, total)
		}
		for name, data := range files {
			if strings.Contains(string(data), textDifferentialJavaAnchor) {
				files[name] = []byte(strings.Replace(string(data), textDifferentialJavaAnchor, "low = 6; high = 6; /* SABOTAGED */", 1))
			}
		}
	})
	if err == nil {
		t.Fatalf("NEGATIVE CONTROL FAILED: a float32 writer capped at six significant digits left the gate GREEN\n%s", out)
	}
	if !bytes.Contains(out, []byte("leg text and engine text differ")) {
		t.Fatalf("NEGATIVE CONTROL FAILED: the gate went red, but not on the text comparison: %v\n%s", err, out)
	}
	t.Logf("negative control red on the text comparison, as it must be: %v", err)
}

// compareTextDifferentialJava runs both engine directions for every instance:
// the engine reads the leg's wire and writes its text, byte-compared against
// the leg's text; the engine packs the leg's text, byte-compared against the
// leg's wire. It writes the engine's text as engine-<i>.json for the probe's
// read pass. Divergences are collected, not failed, so the control can assert
// on them.
func compareTextDifferentialJava(m *tabletext.Model, dir string, n int) ([]string, error) {
	var msgs []string
	for i := range n {
		wire, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("inst-%d.wire", i)))
		if err != nil {
			return msgs, fmt.Errorf("read inst-%d.wire: %w", i, err)
		}
		legJSON, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("inst-%d.json", i)))
		if err != nil {
			return msgs, fmt.Errorf("read inst-%d.json: %w", i, err)
		}

		d1, err := os.MkdirTemp(dir, "d1-")
		if err != nil {
			return msgs, err
		}
		report, err := tablepack.UnpackOneFile(m, "Root", wire, d1)
		if err != nil {
			os.RemoveAll(d1)
			return msgs, fmt.Errorf("instance %d: UnpackOneFile: %w", i, err)
		}
		if report != (tabletext.Report{}) {
			os.RemoveAll(d1)
			return msgs, fmt.Errorf("instance %d: UnpackOneFile report not clean: %+v", i, report)
		}
		engineJSON, err := os.ReadFile(filepath.Join(d1, "Root.json"))
		os.RemoveAll(d1)
		if err != nil {
			return msgs, fmt.Errorf("instance %d: read engine Root.json: %w", i, err)
		}
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("engine-%d.json", i)), engineJSON, 0o600); err != nil {
			return msgs, err
		}
		if !bytes.Equal(engineJSON, legJSON) {
			off, win := textWindowJava(legJSON, engineJSON)
			msgs = append(msgs, fmt.Sprintf("instance %d: leg text and engine text differ at byte %d\n%s\nfloats: %s", i, off, win, floatBitsOfJava(m, wire)))
			continue
		}

		d2, err := os.MkdirTemp(dir, "d2-")
		if err != nil {
			return msgs, err
		}
		if err := os.WriteFile(filepath.Join(d2, "Root.json"), legJSON, 0o600); err != nil {
			os.RemoveAll(d2)
			return msgs, err
		}
		wireBack, _, report2, err := tablepack.Pack(m, "Root", d2)
		os.RemoveAll(d2)
		if err != nil {
			return msgs, fmt.Errorf("instance %d: Pack: %w", i, err)
		}
		if report2 != (tabletext.Report{}) {
			return msgs, fmt.Errorf("instance %d: Pack report not clean: %+v", i, report2)
		}
		if !bytes.Equal(wireBack, wire) {
			msgs = append(msgs, fmt.Sprintf("instance %d: engine wire packed from leg text differs from leg wire: engine=%x leg=%x", i, wireBack, wire))
		}
	}
	return msgs, nil
}

// floatBitsOfJava decodes one wire with the engine and spells every float
// slot's bits, so a divergence replays from the bits alone.
func floatBitsOfJava(m *tabletext.Model, wire []byte) string {
	inst := m.New(m.Lookup("Root"))
	var report tabletext.Report
	if _, err := tablewire.Decode(m, inst, wire, &report); err != nil {
		return fmt.Sprintf("<decode error: %v>", err)
	}
	var b strings.Builder
	var walk func(fields []tabletext.Field, prefix string)
	walk = func(fields []tabletext.Field, prefix string) {
		for i := range fields {
			f := &fields[i]
			name := prefix + f.Def.Name
			emit := func(c tabletext.Cell, slot string) {
				switch f.Def.Type.Kind {
				case ir.TFloat32:
					fmt.Fprintf(&b, "%s=0x%08x(f32) ", slot, math.Float32bits(float32(c.F)))
				case ir.TFloat64:
					fmt.Fprintf(&b, "%s=0x%016x(f64) ", slot, math.Float64bits(c.F))
				}
			}
			if len(f.Elems) > 0 {
				for j := 0; j < f.Count && j < len(f.Elems); j++ {
					c := f.Elems[j]
					if c.Tab != nil {
						walk(c.Tab.Fields, fmt.Sprintf("%s[%d].", name, j))
					} else {
						emit(c, fmt.Sprintf("%s[%d]", name, j))
					}
				}
				continue
			}
			if f.Cell.Tab != nil {
				walk(f.Cell.Tab.Fields, name+".")
				continue
			}
			emit(f.Cell, name)
		}
	}
	walk(inst.Fields, "")
	return strings.TrimSpace(b.String())
}

// textWindowJava returns the first byte offset where two texts differ and a
// window of each around it.
func textWindowJava(a, b []byte) (int, string) {
	n := min(len(b), len(a))
	i := 0
	for i < n && a[i] == b[i] {
		i++
	}
	lo := max(i-32, 0)
	hiA := min(i+32, len(a))
	hiB := min(i+32, len(b))
	return i, fmt.Sprintf("leg[%d:%d]=%q engine[%d:%d]=%q", lo, hiA, a[lo:hiA], lo, hiB, b[lo:hiB])
}
