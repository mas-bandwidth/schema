package javatable

// TestFixedRoadmapJavaT14CompressedFloats is java/R30. The dequantize fixture
// is index 2 under min=-1, max=2, resolution=0.1 (count=30, delta=3): unfused
// bits 0xbf4ccccc, fused bits 0xbf4ccccd. docs/SPEC.md Floating-point discipline,
// docs/FIXED-FORM-ALGORITHM.md §5.1.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestFixedRoadmapJavaT14CompressedFloats(t *testing.T) {
	t.Parallel()
	t.Run("java/R30", func(t *testing.T) {
		t.Parallel()
		slowtest.Gate(t, "javac (it compiles and runs the generated unit)")
		javac, javaBin := javaTools(t)
		t14Cite(t)
		u := unitFrom(t, t14JavaOld)
		files, err := Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		body := string(files["ReadingFixed.java"])
		if !strings.Contains(body, "Float.floatToRawIntBits(") {
			t.Fatal("the fixed write does not store the float32 bits")
		}
		if !strings.Contains(string(files["TableVocabulary.java"]), "dequantizeBits") {
			t.Fatal("the leg emits no dequantize")
		}
		out := runMessageJava(t, u, `
    public static void main(String[] args) {
        // min=-1 max=2 res=0.1 count=30 index=2: unfused 0xbf4ccccc, fused 0xbf4ccccd.
        int bits = TableVocabulary.dequantizeBits(2, -1.0f, 3.0f, 30);
        if (bits != 0xbf4ccccc) { fail("dequantize " + Integer.toHexString(bits)); }
    }
`)
		if strings.Contains(out, "FAILED") {
			t.Fatal(out)
		}
		dir := t.TempDir()
		file := filepath.Join(dir, "old.bin")
		t14JavaRun(t, javac, javaBin, t14JavaOld, nil, file, true)
		t14JavaRun(t, javac, javaBin, t14JavaOld, nil, file, false)
		t14JavaRun(t, javac, javaBin, t14JavaFine, []string{t14JavaOld}, file, false)
		t14JavaRun(t, javac, javaBin, t14JavaDropped, []string{t14JavaOld}, file, false)
	})
}

const t14JavaOld = `package cfold

fixed table Reading
{
    lead uint32 = 1
    aim float32 | min = -1, max = 2, resolution = 0.1
    trail uint32 = 2
}
`

const t14JavaFine = `package cfold

fixed table Reading
{
    lead uint32 = 1
    aim float32 | min = -1, max = 2, resolution = 0.01
    trail uint32 = 2
}
`

const t14JavaDropped = `package cfold

fixed table Reading
{
    lead uint32 = 1
    aim float32
    trail uint32 = 2
}
`

func t14Cite(t *testing.T) {
	t.Helper()
	b, err := os.ReadFile("../../lockfile/cfloatrange_test.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "func TestLockCFloatResolutionCoarsenedRefuses") || !strings.Contains(string(b), "resolution coarsened") {
		t.Fatal("the lockfile citation is gone")
	}
}

func t14JavaRun(t *testing.T, javac, javaBin, schema string, older []string, file string, write bool) {
	t.Helper()
	u := unitFrom(t, schema)
	lineage := map[string][]FixedLineageEntry{}
	for _, src := range older {
		o := unitFrom(t, src)
		for _, st := range ir.TableFixedRoots(o) {
			e, ok := FixedLineageOf(o, st.Name)
			if !ok {
				t.Fatal("no lineage")
			}
			lineage[st.Name] = append(lineage[st.Name], e)
		}
	}
	files, err := GenerateLineage(u, lineage)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	srcDir := filepath.Join(dir, "src", u.Package)
	classes := filepath.Join(dir, "classes")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	var sources []string
	for name, data := range files {
		if !strings.HasSuffix(name, ".java") || strings.Contains(name, "/") {
			continue
		}
		path := filepath.Join(srcDir, name)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, path)
	}
	mode := "read"
	if write {
		mode = "write"
	}
	driver := fmt.Sprintf(`package cfold;
public final class Driver {
    public static void main(String[] args) throws Exception {
        if ("%s".equals("write")) {
            ReadingFixed.Value v = new ReadingFixed.Value();
            v.lead = 1;
            v.aim = Float.intBitsToFloat(0x3E99999A);
            v.trail = 2;
            byte[] bytes = new byte[ReadingFixed.measure(1)];
            if (ReadingFixed.save(new ReadingFixed.Value[]{v}, 1, bytes) < 0) { throw new RuntimeException("save"); }
            boolean found = false;
            for (int i = 0; i + 4 <= bytes.length; i++) {
                if ((bytes[i] & 0xff) == 0x9a && (bytes[i+1] & 0xff) == 0x99 && (bytes[i+2] & 0xff) == 0x99 && (bytes[i+3] & 0xff) == 0x3e) { found = true; }
            }
            if (!found) { throw new RuntimeException("float bits missing"); }
            java.nio.file.Files.write(java.nio.file.Path.of(args[0]), bytes);
            return;
        }
        byte[] bytes = java.nio.file.Files.readAllBytes(java.nio.file.Path.of(args[0]));
        ReadingFixed.Value[] back = new ReadingFixed.Value[]{ new ReadingFixed.Value() };
        TableFixed.Report report = new TableFixed.Report();
        int n = ReadingFixed.load(back, 1, bytes, TableFixed.plan(1 << 14), new short[1 << 14], ReadingFixed.image(), report);
        if (n != 1) { throw new RuntimeException("load " + n + " " + report.reason); }
        if (Float.floatToRawIntBits(back[0].aim) != 0x3E99999A || back[0].lead != 1 || back[0].trail != 2) {
            throw new RuntimeException("moved");
        }
        if (report.clamped != 0) { throw new RuntimeException("clamped"); }
    }
}
`, mode)
	driverPath := filepath.Join(srcDir, "Driver.java")
	if err := os.WriteFile(driverPath, []byte(driver), 0o644); err != nil {
		t.Fatal(err)
	}
	args := append([]string{"--release", "17", "-nowarn", "-d", classes}, sources...)
	args = append(args, driverPath)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	if out, err := exec.Command(javaBin, "-ea", "-cp", classes, "cfold.Driver", file).CombinedOutput(); err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
}
