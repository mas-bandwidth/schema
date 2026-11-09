package javatable

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// A LINEAGE ENTRY WHOSE KIND MOVED IS NOT A LAWFUL LINEAGE
// (docs/plans/strict-only.md: a kind mismatch refuses, never a counted
// repair). The OLD writer's `a` is an int32 and the NEW reader's is a
// float32, a move no widening covers. The Go leg's tableFixedCompile marks
// such a plan unlawful and the entry refuses by layout_malformed; the Java
// leg used to build the plan, count kindChanged and land the record. The
// entry is HANDED IN the way the unknown-census row hands in its unlawful
// pair, never read from a lock.
const (
	javaKindMovedOld = "package kindmoved\n\nfixed table T\n{\n    a int32\n    b int32\n}\n"
	javaKindMovedNew = "package kindmoved\n\nfixed table T\n{\n    a float32\n    b int32\n}\n"
)

const javaKindMovedProbe = `import java.nio.file.*;

public final class KindMovedProbe {
    public static void main(String[] args) throws Exception {
        byte[] data = Files.readAllBytes(Paths.get(args[0]));
        kindmoved.TFixed.Value[] vals = new kindmoved.TFixed.Value[4];
        for (int i = 0; i < vals.length; i++) { vals[i] = new kindmoved.TFixed.Value(); }
        kindmoved.TableFixed.Entry[] plan = kindmoved.TableFixed.plan(1 << 10);
        short[] remap = new short[1 << 10];
        kindmoved.TableFixed.Report r = new kindmoved.TableFixed.Report();
        int n = kindmoved.TFixed.load(vals, vals.length, data, plan, remap, kindmoved.TFixed.image(), r);
        System.out.println("n=" + n + " refused=" + r.refused + " damaged=" + r.damaged + " reason=" + r.reason + " b=" + vals[0].b);
    }
}
`

// javaKindMovedRun generates the NEW reader with the OLD entry in its
// lineage, applies sabotage to the generated runtime (nil for none), and
// answers what the probe printed for an OLD-written file.
func javaKindMovedRun(t *testing.T, sabotage func(string) string) string {
	t.Helper()
	oldU := unitFrom(t, javaKindMovedOld)
	newU := unitFrom(t, javaKindMovedNew)
	entry, ok := FixedLineageOf(oldU, "T")
	if !ok {
		t.Fatal("no lineage entry for the OLD T")
	}
	files, err := GenerateLineage(newU, map[string][]FixedLineageEntry{"T": {entry}})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if sabotage != nil {
		for name, data := range files {
			if strings.HasSuffix(name, "TableFixed.java") {
				files[name] = []byte(sabotage(string(data)))
			}
		}
	}

	m := tabletext.NewModel(oldU)
	inst := m.New(m.Lookup("T"))
	a, _ := inst.FieldByKey("a")
	a.Cell.I = 7
	b, _ := inst.FieldByKey("b")
	b.Cell.I = 9
	wire, err := tablewire.EncodeFixed(m, inst)
	if err != nil {
		t.Fatalf("encode the OLD record: %v", err)
	}

	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	classes := filepath.Join(dir, "classes")
	writeFixedJava(t, src, "kindmoved", files)
	probe := filepath.Join(src, "KindMovedProbe.java")
	if err := os.WriteFile(probe, []byte(javaKindMovedProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "old.bin")
	if err := os.WriteFile(file, wire, 0o644); err != nil {
		t.Fatal(err)
	}
	sources, err := filepath.Glob(filepath.Join(src, "kindmoved", "*.java"))
	if err != nil {
		t.Fatal(err)
	}
	javac, javaBin := javaTools(t)
	args := append([]string{"--release", "17", "-nowarn", "-d", classes}, sources...)
	if out, err := exec.Command(javac, append(args, probe)...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	out, err := exec.Command(javaBin, "-ea", "-cp", classes, "KindMovedProbe", file).CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestJavaLineageKindMovedRefuses(t *testing.T) {
	slowtest.Gate(t, "javac and java (it builds and runs the generated Java)")
	t.Parallel()
	const want = "refused=true damaged=false reason=layoutMalformed"
	t.Run("refuses", func(t *testing.T) {
		t.Parallel()
		got := javaKindMovedRun(t, nil)
		if strings.HasPrefix(got, "n=1 ") || !strings.Contains(got, want) {
			t.Fatalf("an OLD file whose a moved int32 -> float32: %s, want no record and %s", got, want)
		}
	})
	// NEGATIVE CONTROL: the compile no longer marks a moved kind unlawful, so
	// the plan builds, skips a and lands b; the row must go red.
	t.Run("negative_control", func(t *testing.T) {
		t.Parallel()
		const mark = "            c.unlawful = true;\n            return;\n"
		got := javaKindMovedRun(t, func(src string) string {
			if n := strings.Count(src, mark); n != 1 {
				t.Fatalf("the sabotage target matched %d times, want 1", n)
			}
			return strings.Replace(src, mark, "            return;\n", 1)
		})
		if !strings.HasPrefix(got, "n=1 ") || strings.Contains(got, want) {
			t.Fatalf("NEGATIVE CONTROL FAILED: with the kind check gone the read still refused: %s", got)
		}
	})
}
