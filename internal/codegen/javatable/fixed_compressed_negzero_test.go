package javatable

// A COMPRESSED FLOAT'S `-0` IS `+0` (docs/SPEC-TABLES.md §3.3 and §3.4,
// docs/FIXED-FORM-ALGORITHM.md §3.1 and §4.6, docs/rulings.json
// fixed-compressed-negzero). The shared vector (fixedvector.NegativeZero) is
// the reference writer's file holding the sign bit of `-0` alone in the
// compressed float of test/tables/FNZ.schema's NegZero. This leg reads it
// through its generated load, lands the value 0 with the sign bit clear,
// refuses nothing and counts nothing, and saves the canonical bytes the `+0`
// record writes. A PLAIN float32's `-0` is a different rule: it is carried
// bit-exact. The Go leg's test of the same name is the worked example.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedvector"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const compressedNegZeroProbe = `import tblfnz.*;
import java.nio.file.Files;
import java.nio.file.Paths;

public final class NegZeroProbe {
    static void fail(String why) {
        System.out.println("FAIL " + why);
        System.exit(1);
    }

    public static void main(String[] args) throws Exception {
        byte[] data = Files.readAllBytes(Paths.get(args[0]));
        final int layoutBytes = TableFixed.get32(data, NegZeroFixed.headerBytes);
        final int valueAt = NegZeroFixed.headerBytes + 4 + layoutBytes + 8 + 4;
        if (valueAt + 4 > data.length) {
            fail("the vector is too short for its value slot: " + data.length);
        }
        int raw = TableFixed.get32(data, valueAt);
        if (raw != 0x80000000) {
            fail("the vector does not hold a compressed float's -0: 0x" + Integer.toHexString(raw));
        }

        NegZeroFixed.Value[] back = { new NegZeroFixed.Value() };
        TableFixed.Report r = new TableFixed.Report();
        int n = NegZeroFixed.load(back, 1, data, TableFixed.plan(1 << 14),
                new short[1 << 14], NegZeroFixed.image(), r);
        if (n != 1) {
            fail("the reader refused a compressed -0: n=" + n + " reason=" + r.reason);
        }
        if (r.unknown != 0 || r.widened != 0 || r.damaged || r.refused
                || r.reason != TableFixed.Reason.none || r.nonFiniteFloat != null || r.layoutHash != 0) {
            fail("the report is not at its zero value: unknown=" + r.unknown + " widened=" + r.widened
                    + " damaged=" + r.damaged + " refused=" + r.refused + " reason=" + r.reason
                    + " nonFiniteFloat=" + r.nonFiniteFloat + " layoutHash=" + r.layoutHash);
        }
        if (!(back[0].value == 0.0f)) {
            fail("the read did not land zero: " + back[0].value);
        }
        int bits = Float.floatToRawIntBits(back[0].value);
        if (bits != 0) {
            fail("the read kept the sign bit: 0x" + Integer.toHexString(bits));
        }

        byte[] saved = new byte[NegZeroFixed.measure(1)];
        int m = NegZeroFixed.save(back, 1, saved);
        if (m != data.length || saved.length != data.length) {
            fail("the save wrote " + m + " bytes into " + saved.length + ", want " + data.length);
        }
        byte[] want = data.clone();
        TableFixed.put32(want, valueAt, 0);
        for (int i = 0; i < want.length; i++) {
            if (saved[i] != want[i]) {
                fail("saving the landed +0 did not write the canonical bytes: byte " + i
                        + " is 0x" + Integer.toHexString(saved[i] & 0xff) + ", want 0x"
                        + Integer.toHexString(want[i] & 0xff));
            }
        }
        System.out.println("OK negative_zero: landed +0, saved the canonical bytes");
    }
}
`

func TestFixedCompressedNegZeroRead(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "javac and java (it runs the generated load over the vector)")
	corpus := corpusDir(t)
	javac, javaBin := javaToolsRequired(t)

	u := versioningSchemaUnit(t, "FNZ.schema")
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

	for _, vec := range fixedvector.NegativeZero {
		t.Run(vec.Name, func(t *testing.T) {
			t.Parallel()
			if vec.Root != "NegZero" {
				t.Fatalf("%s names root %s; this probe reads NegZero", vec.File, vec.Root)
			}
			file := filepath.Join(corpus, vec.File)
			if _, err := os.Stat(file); err != nil {
				t.Fatalf("the negative-zero vector %s is not in the built corpus: %v", vec.File, err)
			}
			abs, err := filepath.Abs(file)
			if err != nil {
				t.Fatal(err)
			}
			out, err := runJava(t, javaBin, classes, "NegZeroProbe", abs)
			if err != nil {
				t.Fatalf("%s: java: %v\n%s", vec.File, err, out)
			}
			if !strings.Contains(out, "OK negative_zero") {
				t.Fatalf("%s: probe: %s", vec.File, out)
			}
			t.Logf("%s", out)
		})
	}
}
