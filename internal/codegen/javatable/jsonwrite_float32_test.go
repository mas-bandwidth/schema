package javatable

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestJSONWriterFloat32RoundsAtFieldWidth pins the Java JSON writer's float32
// read-back at the field's own width (docs/SPEC-TABLES.md §16.14, W12): a
// float32 candidate's text is parsed with Float.parseFloat and compared there,
// never parsed as a double and then narrowed, which for two float32 magnitudes
// would round twice and part from the canonical spelling. The two magnitudes
// are exactly the pair text_float32_double_round carries (0x15AE43FE and
// 0x15AE43FD), whose canonical text differs only in the byte 223 digit ('3'
// vs the double-rounded '1' the old path would emit), so the emitted strings
// below are the byte-exact expectation that row's golden holds.
func TestJSONWriterFloat32RoundsAtFieldWidth(t *testing.T) {
	t.Parallel()
	javac, javaBin := javaTools(t)
	files, err := Generate(unitFrom(t, `package probe
table Card { score float32 }
`))
	if err != nil {
		t.Fatal(err)
	}
	tj, ok := files["TableJson.java"]
	if !ok {
		t.Fatal("TableJson.java not emitted for a table with a float32 field")
	}
	tv, ok := files["TableVariable.java"]
	if !ok {
		t.Fatal("TableVariable.java not emitted for a table with a float32 field")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	classes := filepath.Join(dir, "classes")
	if err := os.MkdirAll(filepath.Join(src, "probe"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "probe", "TableJson.java"), tj, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "probe", "TableVariable.java"), tv, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "probe", "Probe.java"), []byte(float32WidthProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(javac, "--release", "17", "-d", classes,
		filepath.Join(src, "probe", "TableJson.java"),
		filepath.Join(src, "probe", "TableVariable.java"),
		filepath.Join(src, "probe", "Probe.java"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	out, err := exec.Command(javaBin, "-ea", "-cp", classes, "probe.Probe").CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "ok") {
		t.Fatalf("probe: %s", out)
	}
}

// float32WidthProbe calls the generated writer's writeFloat for the exact two
// float32 magnitudes text_float32_double_round carries and asserts the
// canonical text: parse-at-width must spell the first as 7.0385313e-26 (its
// byte 223 digit is '3'), while the double-rounded double-then-narrowed path
// spells it 7.038531e-26 and would send '1' to byte 223.
const float32WidthProbe = `package probe;

public final class Probe {
    static void check(String got, String want, String what) {
        if (!want.equals(got)) {
            throw new AssertionError(what + ": got <" + got + "> want <" + want + ">");
        }
    }
    public static void main(String[] args) {
        StringBuilder b = new StringBuilder();
        TableJson.writeFloat(b, (double) Float.intBitsToFloat(0x15AE43FE), true);
        check(b.toString(), "7.0385313e-26", "0x15AE43FE");
        b.setLength(0);
        TableJson.writeFloat(b, (double) Float.intBitsToFloat(0x15AE43FD), true);
        check(b.toString(), "7.038531e-26", "0x15AE43FD");
        System.out.println("ok");
    }
}
`
