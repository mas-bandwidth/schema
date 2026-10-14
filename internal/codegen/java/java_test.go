package java

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// The Java compressed-float writer must clamp the quantized integer to the step
// count. Once the step count is in [2^23, 2^24) the float32 ulp is 1 and
// n*miv + 0.5 at n = 1.0 is a tie that rounds to even, so the writer emits
// maxInt + 1 for a legal top-of-range value: for 8388609 the generated reader
// refuses the 8388610 it receives, and for 16777215 the writer puts 2^24 into a
// 24-bit field. The integer clamp is normative (SPEC §4.3; schema#109); the
// Elixir emitter and every serialize runtime already carry it, and this inline
// emitter did not. Witnesses: 8388609 and 16777215 at resolution 1.
const compressedFloatClampSchema = `package cfloatclamp

type Low
{
    f float32 | min = 0.0, max = 8388609.0, resolution = 1.0
}

type High
{
    f float32 | min = 0.0, max = 16777215.0, resolution = 1.0
}
`

func generateCompressedFloatJava(t *testing.T) string {
	t.Helper()
	src := compressedFloatClampSchema
	f, errs := parser.Parse("Probe.schema", []byte(src))
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(src), AST: f,
	}})
	if len(cerrs) != 0 {
		t.Fatal(cerrs)
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	out := files["Probe.java"]
	if len(out) == 0 {
		t.Fatal("Probe.java not emitted")
	}
	return string(out)
}

func TestJavaCompressedFloatClampsTheTopOfRange(t *testing.T) {
	t.Parallel()

	text := generateCompressedFloatJava(t)
	for _, want := range []string{
		"if (v > 8388609L) v = 8388609L;",
		"if (v > 16777215L) v = 16777215L;",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("emitted writer missing the normative clamp %q", want)
		}
	}

	slowtest.Gate(t, "javac")
	runCompressedFloatProbe(t, text)
}

// compressedFloatProbe is the Java driver: it writes each declaration's top of
// range, byte-compares the writer against the C++ reference's bytes for the same
// field (the reference clamps to the step count, so the wire is the step count
// itself, little-endian in the family bit order), reads it back, and demands the
// read succeed with the exact float32 value.
const compressedFloatProbe = `import cfloatclamp.Probe;

public final class Driver {
    private Driver() {}

    static void fail(String what) {
        System.out.println("FAILED: " + what);
        System.exit(1);
    }

    static String hex(byte[] b, int n) {
        StringBuilder s = new StringBuilder();
        for (int i = 0; i < n; i++) {
            s.append(String.format("%02x", b[i]));
        }
        return s.toString();
    }

    static void one(String label, float value, int maxBytes, byte[] want,
                    int written, byte[] got, boolean read, float roundTrip) {
        if (!java.util.Arrays.equals(java.util.Arrays.copyOf(got, written), want)) {
            fail(label + " bytes: want " + hex(want, want.length) + ", got " + hex(got, written));
        }
        if (!read) {
            fail(label + " read rejected the wire the writer just produced");
        }
        if (Float.floatToRawIntBits(roundTrip) != Float.floatToRawIntBits(value)) {
            fail(label + " round-trip: want " + value + ", got " + roundTrip);
        }
    }

    public static void main(String[] args) {
        Probe.Low low = new Probe.Low();
        low.f = 8388609.0f;
        byte[] lb = new byte[Probe.lowMaxBytes];
        int ln = Probe.writeLow(low, lb);
        Probe.Low lout = new Probe.Low();
        boolean lread = Probe.readLow(lout, lb, ln * 8);
        one("low", low.f, Probe.lowMaxBytes, new byte[] {0x01, 0x00, (byte) 0x80}, ln, lb, lread, lout.f);

        Probe.High high = new Probe.High();
        high.f = 16777215.0f;
        byte[] hb = new byte[Probe.highMaxBytes];
        int hn = Probe.writeHigh(high, hb);
        Probe.High hout = new Probe.High();
        boolean hread = Probe.readHigh(hout, hb, hn * 8);
        one("high", high.f, Probe.highMaxBytes, new byte[] {(byte) 0xFF, (byte) 0xFF, (byte) 0xFF}, hn, hb, hread, hout.f);

        System.out.println("ok");
    }
}
`

func runCompressedFloatProbe(t *testing.T, generated string) {
	t.Helper()
	javac, javaBin := compressedFloatJavaTools(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	classes := filepath.Join(dir, "classes")
	if err := os.MkdirAll(filepath.Join(src, "cfloatclamp"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "cfloatclamp", "Probe.java"), []byte(generated), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "Driver.java"), []byte(compressedFloatProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(javac, "--release", "17", "-nowarn", "-d", classes,
		filepath.Join(src, "cfloatclamp", "Probe.java"), filepath.Join(src, "Driver.java"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	run := exec.Command(javaBin, "-ea", "-cp", classes, "Driver")
	out, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "ok") {
		t.Fatalf("driver: %s", out)
	}
}

func compressedFloatJavaTools(t *testing.T) (javac, javaBin string) {
	t.Helper()
	try := func(jc, j string) bool {
		if jc == "" || j == "" {
			return false
		}
		if st, err := os.Stat(jc); err != nil || st.IsDir() {
			return false
		}
		if st, err := os.Stat(j); err != nil || st.IsDir() {
			return false
		}
		return exec.Command(jc, "-version").Run() == nil
	}
	if try(os.Getenv("JAVAC"), os.Getenv("JAVA")) {
		return os.Getenv("JAVAC"), os.Getenv("JAVA")
	}
	if p, err := exec.LookPath("javac"); err == nil {
		if j, jerr := exec.LookPath("java"); jerr == nil && try(p, j) {
			return p, j
		}
	}
	t.Skip("no working javac (set JAVAC/JAVA or install a JDK)")
	return "", ""
}

// TestJavaWStringWriteChecksAreDebugOnly generates the writer for
// `type Wide { text wstring(2) }` and asserts that its text carries
// no unconditional check (release) while the checks are still present
// as asserts (debug tier).
func TestJavaWStringWriteChecksAreDebugOnly(t *testing.T) {
	t.Parallel()

	const src = `package wtest

type Wide {
	text wstring(2)
}
`
	f, errs := parser.Parse("W.schema", []byte(src))
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "W.schema", Name: "W.schema", Base: "W", Bytes: []byte(src), AST: f,
	}})
	if len(cerrs) != 0 {
		t.Fatal(cerrs)
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	text := string(files["W.java"])
	if text == "" {
		t.Fatal("W.java not emitted")
	}

	// no unconditional throw anywhere: release never refuses or throws
	for _, bad := range []string{
		`throw new IllegalArgumentException("wstring length")`,
		`throw new IllegalArgumentException("wstring null unit")`,
	} {
		if strings.Contains(text, bad) {
			t.Errorf("release writer carries unconditional check %q", bad)
		}
	}

	// The hot write body carries exactly one dormant assert — the single
	// checkWrite predicate call — and no inline contract checks. Routing the
	// checks through the predicate keeps them out of the hot body while the
	// predicate's contract walk still runs them under -ea (issue #156).
	write := funcBody(t, text, "public static int writeWide(")
	if n := strings.Count(write, "assert "); n != 1 {
		t.Errorf("writeWide carries %d asserts, want exactly the checkWrite predicate call", n)
	}
	if !strings.Contains(write, "assert checkWriteWide(value, data);") {
		t.Errorf("writeWide missing its single checkWrite predicate call")
	}
	for _, bad := range []string{
		"assert value.textLength",
		"assert v != 0",
		"throw ",
	} {
		if strings.Contains(write, bad) {
			t.Errorf("writeWide carries an inline contract check %q", bad)
		}
	}

	// The whole write contract, null units included, lives in checkWriteWide.
	check := funcBody(t, text, "private static boolean checkWriteWide(")
	for _, want := range []string{
		"assert value.textLength >= 0;",
		"assert value.textLength <= 2;",
		"assert value.text[wideIndex] != 0;",
	} {
		if !strings.Contains(check, want) {
			t.Errorf("checkWriteWide missing contract check %q", want)
		}
	}
}

// funcBody returns the text of the first function whose signature line
// contains sig, from that signature through its outer closing brace (the
// first line holding exactly four spaces and a closing brace).
func funcBody(t *testing.T, text, sig string) string {
	t.Helper()
	i := strings.Index(text, sig)
	if i < 0 {
		t.Fatalf("function %q not found in generated text", sig)
	}
	rest := text[i:]
	j := strings.Index(rest, "\n    }\n")
	if j < 0 {
		t.Fatalf("closing brace of %q not found", sig)
	}
	return rest[:j]
}
