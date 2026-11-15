package javatable

// A COMPRESSED FLOAT'S `-0` IS `+0` (docs/SPEC-TABLES.md §3.3 and §3.4,
// docs/FIXED-FORM-ALGORITHM.md §3.1 and §4.6, docs/rulings.json
// fixed-compressed-negzero). A fixed record holding the sign bit of `-0` in a
// compressed float reads that field as `+0` with its sign bit clear, refusing
// nothing and counting nothing, and the save writes the `+0` image. A PLAIN
// float32's `-0` is a different rule: it is carried bit-exact.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const compressedNegZeroSchema = `package probe

fixed table NegZero
{
    c float32 | min = -1, max = 1, resolution = 0.01
}
`

const compressedNegZeroProbe = `import probe.*;

public final class NegZeroProbe {
    static void fail(String why) {
        System.out.println("FAIL " + why);
        System.exit(1);
    }

    public static void main(String[] args) {
        NegZeroFixed.Value v = new NegZeroFixed.Value();
        v.c = 0.0f;
        byte[] clean = new byte[NegZeroFixed.measure(1)];
        if (NegZeroFixed.save(new NegZeroFixed.Value[] { v }, 1, clean) < 0) {
            fail("clean save");
        }
        final int at = NegZeroFixed.headerBytes + 8;
        TableFixed.put32(clean, at, Float.floatToRawIntBits(-0.0f));
        if (TableFixed.get32(clean, at) != Float.floatToRawIntBits(-0.0f)) {
            fail("the vector does not hold -0");
        }

        NegZeroFixed.Value[] back = { new NegZeroFixed.Value() };
        TableFixed.Report r = new TableFixed.Report();
        int n = NegZeroFixed.load(back, 1, clean, TableFixed.plan(1 << 14),
                new short[1 << 14], NegZeroFixed.image(), r);
        if (n != 1 || r.refused || r.damaged) {
            fail("load n=" + n + " refused=" + r.refused + " damaged=" + r.damaged);
        }
        if (!(back[0].c == 0.0f)) {
            fail("the read did not land zero: " + back[0].c);
        }
        int bits = Float.floatToRawIntBits(back[0].c);
        if (bits != 0) {
            fail("the read kept the sign bit: 0x" + Integer.toHexString(bits));
        }

        byte[] saved = new byte[NegZeroFixed.measure(1)];
        if (NegZeroFixed.save(back, 1, saved) < 0) {
            fail("save");
        }
        int stored = TableFixed.get32(saved, at);
        if (stored != 0) {
            fail("the save wrote 0x" + Integer.toHexString(stored));
        }
        System.out.println("OK");
    }
}
`

func TestFixedCompressedNegZeroRead(t *testing.T) {
	t.Parallel()
	javac, javaBin := javaToolsRequired(t)

	u := unitFrom(t, compressedNegZeroSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	pkg := javaPackageOf(u)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	writeFixedJava(t, src, pkg, files)
	if err := os.WriteFile(filepath.Join(src, "NegZeroProbe.java"), []byte(compressedNegZeroProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	sources, err := filepath.Glob(filepath.Join(src, "*", "*.java"))
	if err != nil {
		t.Fatal(err)
	}
	sources = append(sources, filepath.Join(src, "NegZeroProbe.java"))
	classes := filepath.Join(dir, "classes")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	args := append([]string{"--release", "17", "-nowarn", "-d", classes}, sources...)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}

	out, err := runJava(t, javaBin, classes, "NegZeroProbe")
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
	if !strings.Contains(out, "OK") {
		t.Fatalf("probe: %s", out)
	}
}
