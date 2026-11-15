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
	"github.com/mas-bandwidth/schema/v2/ir"
)

const bitsWidthSchema = `package bitwidth

type Single
{
    value bits(7)
}

type Packed
{
    lead bits(5)
    value bits(7)
    tail bits(3)
}

type Bit7
{
    value bits(7)
}

type Arrayed
{
    values [2]Bit7
}
`

func parseBitsWidthUnit(t *testing.T) *ir.Unit {
	t.Helper()
	f, errs := parser.Parse("BitsWidth.schema", []byte(bitsWidthSchema))
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "BitsWidth.schema", Name: "BitsWidth.schema", Base: "BitsWidth", Bytes: []byte(bitsWidthSchema), AST: f,
	}})
	if len(cerrs) != 0 {
		t.Fatal(cerrs)
	}
	return u
}

// TestJavaWriteAssertsBitsWidth holds the Java packet writer to §5.1 row 2:
// bits(N) is asserted when its Java storage can hold values outside the wire
// width, in standalone fields, packed runs, and array elements. The assertion
// is dormant without -ea, like the other Java write contracts.
func TestJavaWriteAssertsBitsWidth(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "javac")

	files, err := Generate(parseBitsWidthUnit(t))
	if err != nil {
		t.Fatal(err)
	}
	generated := string(files["BitsWidth.java"])
	if generated == "" {
		t.Fatal("BitsWidth.java not emitted")
	}
	runBitsWidthProbe(t, generated)
}

const bitsWidthDriver = `import bitwidth.BitsWidth;

public final class Driver {
    private Driver() {}

    private static void expectAssertion(String what, Runnable write) {
        boolean caught = false;
        try {
            write.run();
        } catch (AssertionError expected) {
            caught = true;
        }
        if (!caught) {
            throw new RuntimeException(what + " did not assert");
        }
    }

    private static void enabled() {
        BitsWidth.Single single = new BitsWidth.Single();
        single.value = 200;
        expectAssertion("standalone bits(7)", () -> BitsWidth.writeSingle(single, new byte[BitsWidth.SINGLE_MAX_BYTES]));

        BitsWidth.Packed packed = new BitsWidth.Packed();
        packed.value = 200;
        expectAssertion("packed bits(7)", () -> BitsWidth.writePacked(packed, new byte[BitsWidth.PACKED_MAX_BYTES]));

        BitsWidth.Arrayed arrayed = new BitsWidth.Arrayed();
        arrayed.values[1].value = 200;
        expectAssertion("array element bits(7)", () -> BitsWidth.writeArrayed(arrayed, new byte[BitsWidth.ARRAYED_MAX_BYTES]));

        single.value = 127;
        byte[] data = new byte[BitsWidth.SINGLE_MAX_BYTES];
        int end = BitsWidth.writeSingleBits(single, data, 0);
        BitsWidth.Single decoded = new BitsWidth.Single();
        if (!BitsWidth.readSingle(decoded, data, end) || decoded.value != 127) {
            throw new RuntimeException("bits(7) maximum did not round-trip");
        }
    }

    private static void disabled() {
        BitsWidth.Single single = new BitsWidth.Single();
        single.value = 200;
        BitsWidth.writeSingle(single, new byte[BitsWidth.SINGLE_MAX_BYTES]);

        BitsWidth.Packed packed = new BitsWidth.Packed();
        packed.value = 200;
        BitsWidth.writePacked(packed, new byte[BitsWidth.PACKED_MAX_BYTES]);

        BitsWidth.Arrayed arrayed = new BitsWidth.Arrayed();
        arrayed.values[1].value = 200;
        BitsWidth.writeArrayed(arrayed, new byte[BitsWidth.ARRAYED_MAX_BYTES]);
    }

    public static void main(String[] args) {
        if (args.length == 1 && args[0].equals("release")) {
            disabled();
        } else {
            enabled();
        }
        System.out.println("ok");
    }
}
`

func runBitsWidthProbe(t *testing.T, generated string) {
	t.Helper()
	javac, javaBin := compressedFloatJavaTools(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	classes := filepath.Join(dir, "classes")
	if err := os.MkdirAll(filepath.Join(src, "bitwidth"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "bitwidth", "BitsWidth.java"), []byte(generated), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "Driver.java"), []byte(bitsWidthDriver), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(javac, "--release", "17", "-nowarn", "-d", classes,
		filepath.Join(src, "bitwidth", "BitsWidth.java"), filepath.Join(src, "Driver.java"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	for _, mode := range [][]string{{"-ea"}, {}} {
		args := append(append([]string{}, mode...), "-cp", classes, "Driver")
		if len(mode) == 0 {
			args = append(args[:0], "-cp", classes, "Driver", "release")
		}
		run := exec.Command(javaBin, args...)
		out, err := run.CombinedOutput()
		if err != nil {
			t.Fatalf("java %v: %v\n%s", mode, err, out)
		}
		if !strings.Contains(string(out), "ok") {
			t.Fatalf("java %v: %s", mode, out)
		}
	}
}
