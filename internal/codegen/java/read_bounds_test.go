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

// The generated Java read entries promise never to throw on hostile input. Two
// holes: (1) the fused bound check is int arithmetic in bitsRead + need, which
// wraps for a large caller-supplied bitsRead so a bulk copy goes on to
// System.arraycopy and throws; (2) a type that reads no bits emits no entry
// check at all, so zero-bit reads accept any numBits. Every read entry must
// validate bitsRead >= 0, numBits >= 0, numBits <= data.length * 8L, and do the
// bound arithmetic in long so it cannot wrap — for every type, zero-bit ones
// included.
const readBoundsSchema = `package hostile

type Bulk
{
    align
    data [4]uint8
}

type Ping
{
}
`

func parseReadBoundsUnit(t *testing.T) *ir.Unit {
	t.Helper()
	f, errs := parser.Parse("Hostile.schema", []byte(readBoundsSchema))
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Hostile.schema", Name: "Hostile.schema", Base: "Hostile", Bytes: []byte(readBoundsSchema), AST: f,
	}})
	if len(cerrs) != 0 {
		t.Fatal(cerrs)
	}
	return u
}

// TestJavaReadBoundsGuardsAreEmitted pins the emitter's text: the zero-bit
// type's read entry carries the validation, and the bulk read's fused bound
// check is long arithmetic that cannot wrap.
func TestJavaReadBoundsGuardsAreEmitted(t *testing.T) {
	t.Parallel()

	u := parseReadBoundsUnit(t)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	text := string(files["Hostile.java"])
	if len(text) == 0 {
		t.Fatal("Hostile.java not emitted")
	}

	readPing := funcBody(t, text, "public static int readPingBits(")
	if !strings.Contains(readPing, "numBits < 0") {
		t.Errorf("readPingBits lacks the entry validation for the zero-bit type:\n%s", readPing)
	}
	if !strings.Contains(readPing, "bitOffset > numBits") {
		t.Errorf("readPingBits lacks the offset-past-payload validation:\n%s", readPing)
	}
	if !strings.Contains(readPing, "numBits > (long) data.length * 8") {
		t.Errorf("readPingBits lacks the buffer-size validation:\n%s", readPing)
	}
	if !strings.Contains(readPing, "return -1;") {
		t.Errorf("readPingBits lacks a refusal verdict:\n%s", readPing)
	}

	readBulk := funcBody(t, text, "public static int readBulkBits(")
	if !strings.Contains(readBulk, "(long) bitsRead +") {
		t.Errorf("readBulkBits bound check is not long arithmetic:\n%s", readBulk)
	}
	if !strings.Contains(readBulk, "bitOffset > numBits") {
		t.Errorf("readBulkBits lacks the offset-past-payload validation:\n%s", readBulk)
	}
}

// TestJavaReadBoundsRefuseHostileInput runs the generated Java under the JDK:
// a zero-bit type refuses absurd and negative numBits, and a bulk copy with a
// huge byte-aligned bitOffset refuses instead of throwing or returning garbage.
func TestJavaReadBoundsRefuseHostileInput(t *testing.T) {
	t.Parallel()

	u := parseReadBoundsUnit(t)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	text := string(files["Hostile.java"])
	if len(text) == 0 {
		t.Fatal("Hostile.java not emitted")
	}

	slowtest.Gate(t, "javac")
	runReadBoundsProbe(t, text)
}

const readBoundsDriver = `import hostile.Hostile;

public final class Driver {
    private Driver() {}

    static void fail(String what) {
        System.out.println("FAILED: " + what);
        System.exit(1);
    }

    public static void main(String[] args) {
        Hostile.Ping ping = new Hostile.Ping();
        if (Hostile.readPing(ping, new byte[0], 999999)) {
            fail("readPing accepted 999999");
        }
        if (Hostile.readPing(ping, new byte[0], -5)) {
            fail("readPing accepted -5");
        }

        Hostile.Bulk bulk = new Hostile.Bulk();
        byte[] buf = new byte[Hostile.BULK_MAX_BYTES];
        try {
            int r = Hostile.readBulkBits(bulk, buf, 0x7FFFFFF8, 32);
            if (r != -1) {
                fail("readBulkBits did not refuse a huge offset: " + r);
            }
        } catch (RuntimeException e) {
            fail("readBulkBits threw on a huge offset: " + e);
        }

        try {
            int r = Hostile.readBulkBits(bulk, buf, Integer.MAX_VALUE - 5, 32);
            if (r != -1) {
                fail("readBulkBits did not refuse a non-byte-aligned huge offset: " + r);
            }
        } catch (RuntimeException e) {
            fail("readBulkBits threw on a non-byte-aligned huge offset: " + e);
        }

        System.out.println("ok");
    }
}
`

func runReadBoundsProbe(t *testing.T, generated string) {
	t.Helper()
	javac, javaBin := compressedFloatJavaTools(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	classes := filepath.Join(dir, "classes")
	if err := os.MkdirAll(filepath.Join(src, "hostile"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "hostile", "Hostile.java"), []byte(generated), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "Driver.java"), []byte(readBoundsDriver), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(javac, "--release", "17", "-nowarn", "-d", classes,
		filepath.Join(src, "hostile", "Hostile.java"), filepath.Join(src, "Driver.java"))
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
