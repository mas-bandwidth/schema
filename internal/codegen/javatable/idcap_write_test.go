package javatable

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	javacodegen "github.com/mas-bandwidth/schema/v2/internal/codegen/java"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// THE WRITER REFUSES AN ID TABLE ITS READERS REFUSE (docs/SPEC-TABLES.md §3,
// ruling R8), for Java: the save-side id table used to grow to 65536. The unit
// is tablewire.IdCapSchema, so the three values need 4095, 4096 and 4097 ids:
// the first two write (the count word says so and the strict load accepts
// them), and the third answers null.
func TestJavaWriterRefusesMoreThan4096Ids(t *testing.T) {
	slowtest.Gate(t, "the JDK (it compiles and runs the generated Java)")
	t.Parallel()
	const driver = `import idcap.*;

public final class Main {
    private static void check(boolean ok, String msg) {
        if (!ok) throw new AssertionError(msg);
    }

    private static RootVariable.Value value(int x, int y) {
        RootVariable.Value v = new RootVariable.Value();
        for (int i = 0; i < v.keys.size(); i++) v.keys.set(i, 1);
        v.x = x;
        v.y = y;
        return v;
    }

    private static int count(byte[] b) {
        int n = 0;
        for (int i = 0; i < 8; i++) n |= (b[b.length - 8 + i] & 0xff) << (8 * i);
        return n;
    }

    public static void main(String[] args) {
        int[][] rows = {{0, 0, 4095}, {1, 0, 4096}};
        for (int[] row : rows) {
            byte[] wire = RootVariable.save(value(row[0], row[1]));
            check(wire != null, row[2] + " ids: save refused");
            check(count(wire) == row[2], "id count " + count(wire) + ", want " + row[2]);
            TableVariable.Report rep = new TableVariable.Report();
            check(RootVariable.load(wire, rep) != null && !rep.damaged && !rep.refused, row[2] + " ids: load refused the file");
        }
        check(RootVariable.save(value(1, 1)) == null, "4097 ids: save wrote a file");
    }
}
`
	// TableUnitView is the unit's descriptor listing, one method that spells
	// every one of the 4094 variants: javac refuses it as "code too large", and
	// no save or load names it, so the probe compiles the unit without it.
	compileAndRunJavaProbeSkipping(t, tablewire.IdCapSchema(), "idcap", driver, "TableUnitView.java")
}

func compileAndRunJavaProbeSkipping(t *testing.T, schema, pkg, driver string, skip ...string) {
	t.Helper()
	u := unitFrom(t, schema)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	pkt, err := javacodegen.Generate(u)
	if err != nil {
		t.Fatalf("java.Generate: %v", err)
	}
	for name, data := range pkt {
		if _, ok := files[name]; !ok && strings.HasSuffix(name, ".java") {
			files[name] = data
		}
	}
	dir := t.TempDir()
	srcDir := filepath.Join(dir, pkg)
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	var sources []string
	for name, data := range files {
		if !strings.HasSuffix(name, ".java") || slices.Contains(skip, name) {
			continue
		}
		path := filepath.Join(srcDir, name)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, path)
	}
	mainPath := filepath.Join(dir, "Main.java")
	if err := os.WriteFile(mainPath, []byte(driver), 0o644); err != nil {
		t.Fatal(err)
	}
	sources = append(sources, mainPath)
	classesDir := filepath.Join(dir, "classes")
	if err := os.MkdirAll(classesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	args := append([]string{"--release", "17", "-encoding", "UTF-8", "-d", classesDir}, sources...)
	if out, err := testproc.Command(t, "javac", args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	if out, err := testproc.Command(t, "java", "-ea", "-cp", classesDir, "Main").CombinedOutput(); err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
}
