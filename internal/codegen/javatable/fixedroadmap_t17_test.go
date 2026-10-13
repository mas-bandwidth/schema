package javatable

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// java/W6: THE PLAN IS PARTITIONED UNGUARDED-FIRST (docs/FIXED-FORM-ALGORITHM.md
// §4.1, §4.4). TableFixed.compile states the split: every entry before it is
// unguarded, every entry at or after it is guarded, and no coalesced run
// crosses it. The driver compiles the fixture's own layout against itself, so
// the walk pushes the unguarded tail past the union behind guarded arm entries
// and only the partition repairs that order. Compiling and running generated
// Java is a functional test behind slowtest.Gate.
func TestFixedRoadmapJavaT17PlanPartition(t *testing.T) {
	t.Parallel()
	t.Run("java/W6", func(t *testing.T) {
		t.Parallel()
		slowtest.Gate(t, "javac/java (it compiles and runs the generated unit)")
		javac, javaBin := javaTools(t)
		const schema = `package probe
type ArmA { n int32 }
type ArmB {
    s string(8)
    m int32
}
union Pick
{
    a ArmA
    b ArmB
}
fixed table Root
{
    head int32
    pick Pick
    tail int32
}
`
		u := unitFrom(t, schema)
		files, err := Generate(u)
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}
		dir := t.TempDir()
		src := filepath.Join(dir, "src")
		classes := filepath.Join(dir, "classes")
		if err := os.MkdirAll(classes, 0o755); err != nil {
			t.Fatal(err)
		}
		pkg := u.Package
		writeFixedJava(t, src, pkg, files)
		driver := strings.ReplaceAll(t17JavaDriver, "@PKG@", pkg)
		if err := os.WriteFile(filepath.Join(src, "T17.java"), []byte(driver), 0o644); err != nil {
			t.Fatal(err)
		}
		pkgSrc, err := filepath.Glob(filepath.Join(src, pkg, "*.java"))
		if err != nil {
			t.Fatal(err)
		}
		args := append([]string{"--release", "17", "-d", classes}, pkgSrc...)
		args = append(args, filepath.Join(src, "T17.java"))
		if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
			t.Fatalf("W6: javac: %v\n%s", err, out)
		}
		out, err := exec.Command(javaBin, "-ea", "-cp", classes, "T17").CombinedOutput()
		if err != nil {
			t.Fatalf("W6: java: %v\n%s", err, out)
		}
		if !strings.Contains(string(out), "W6 ok") {
			t.Fatalf("W6: %s", strings.TrimSpace(string(out)))
		}
		t.Log(strings.TrimSpace(string(out)))
	})
}

const t17JavaDriver = `import @PKG@.TableFixed;
import @PKG@.RootFixed;

public final class T17 {
    private T17() {}

    static void fail(String what) {
        System.out.println("FAILED: " + what);
        System.exit(1);
    }

    public static void main(String[] args) {
        final TableFixed.Report r = new TableFixed.Report();
        final TableFixed.Layout mine = TableFixed.parse(RootFixed.layout, 0, RootFixed.layout.length, r);
        if (mine == null) { fail("own layout: " + r.reason); }
        final TableFixed.Entry[] plan = TableFixed.plan(256);
        final short[] remap = new short[4096];
        final int made = TableFixed.compile(mine, mine, RootFixed.dest, plan, remap, r);
        if (made <= 0) { fail("compile from own layout: " + r.reason); }
        // The split is the first guarded index (FIXED-FORM-ALGORITHM.md §4.1).
        int split = made;
        for (int i = 0; i < made; i++) {
            if (plan[i].guard != TableFixed.noGuard) { split = i; break; }
        }
        if (split <= 0 || split >= made) { fail("split=" + split + " of " + made + " is vacuous"); }
        for (int i = 0; i < made; i++) {
            final boolean guarded = plan[i].guard != TableFixed.noGuard;
            if (i < split && guarded) { fail("entry " + i + " below the split carries a guard"); }
            if (i >= split && !guarded) { fail("entry " + i + " at or above the split carries no guard"); }
        }
        System.out.println("W6 ok split=" + split + " of " + made);
    }
}
`
