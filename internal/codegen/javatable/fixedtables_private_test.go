package javatable

// fixedtables_private — a generated fixed table's layout, defaults, dest and
// known arrays are PRIVATE to the generated class. A caller in another package
// reaches a COPY through an accessor, so no caller can rewrite the tables the
// whole process decodes through, and the read path still reads the private
// array directly with no accessor and no allocation.
//
// The block form's projectionOffsets is held to the same rule.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	javacodegen "github.com/mas-bandwidth/schema/v2/internal/codegen/java"
)

const fixedTablesPrivateSchema = `package probe

fixed table T
{
    a uint32
    b float32
    c bool
}
`

// tableArray is one named table: the source name and whether its generated
// declaration must ride in the fixed root (the projectionOffsets table belongs
// to a block class, not to T).
type tableArray struct {
	name string
}

var fixedTableArrays = []tableArray{
	{"layout"},
	{"defaults"},
	{"dest"},
	{"known"},
}

// fixedTablesPrivateArrays walks generated source and holds every array of the
// named tables to `private static final`. It answers the errors rather than
// failing, so the caller can name the file and line.
func fixedTablesPrivateArrays(src string, names []string) []string {
	var bad []string
	for _, line := range strings.Split(src, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "//") || strings.HasPrefix(t, "*") {
			continue
		}
		for _, name := range names {
			if !strings.Contains(t, "[] "+name+" =") {
				continue
			}
			if !strings.HasPrefix(t, "private static final") {
				bad = append(bad, t)
			}
		}
	}
	return bad
}

// TestJavaFixedTablesPrivate is the card's one harness: the generated arrays
// are private, another package cannot assign an element of any of them, and a
// mutated accessor copy leaves a second decode of the same bytes unchanged.
func TestJavaFixedTablesPrivate(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, fixedTablesPrivateSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	pkg := javaPackageOf(u)
	root := files["TFixed.java"]
	if len(root) == 0 {
		for name := range files {
			t.Fatalf("no TFixed.java in %v", name)
		}
	}
	for _, name := range []string{"layout", "defaults", "dest", "known", "known0WriterBounds"} {
		if bad := fixedTablesPrivateArrays(string(root), []string{name}); len(bad) > 0 {
			t.Errorf("TFixed.java: %s is not private static final: %q", name, bad)
		}
	}
	for _, name := range []string{"layout", "defaults", "dest", "known"} {
		if !strings.Contains(string(root), "private static final") || !strings.Contains(string(root), "[] "+name+" =") {
			t.Errorf("TFixed.java: no private static final %s", name)
		}
	}

	blockFiles, err := Generate(unitFromDir(t, "../../../tables/block"))
	if err != nil {
		t.Fatalf("generate block: %v", err)
	}
	sawProjection := false
	for name, body := range blockFiles {
		if !strings.Contains(string(body), "projectionOffsets") {
			continue
		}
		sawProjection = true
		for _, line := range strings.Split(string(body), "\n") {
			line = strings.TrimSpace(line)
			if strings.Contains(line, "[] projectionOffsets =") && !strings.HasPrefix(line, "private static final") {
				t.Errorf("%s: projectionOffsets is not private static final: %q", name, line)
			}
		}
	}
	if !sawProjection {
		t.Error("no block class carries projectionOffsets")
	}

	javac, javaBin := javaTools(t)
	if javac == "" {
		t.Skip("no JDK on this bench: the compile half is named and skipped")
	}

	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	classes := filepath.Join(dir, "classes")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFixedJava(t, src, pkg, files)
	pkt, err := javacodegen.Generate(u)
	if err != nil {
		t.Fatalf("packet generate: %v", err)
	}
	for _, name := range []string{"Int128.java", "UInt128.java"} {
		if data, ok := pkt[name]; ok {
			if err := os.WriteFile(filepath.Join(src, pkg, name), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	generated, err := filepath.Glob(filepath.Join(src, pkg, "*.java"))
	if err != nil {
		t.Fatal(err)
	}

	// THE COMPILE HALF: from another package, every assignment must be refused
	// because the field is private (not because the name is missing or the
	// types disagree).
	assign := `package outside;

public final class Assign {
    public static void main(String[] args) {
        probe.TFixed.layout[0] = 1;
        probe.TFixed.defaults[0] = 1;
        probe.TFixed.dest[0] = 1;
        probe.TFixed.known[0] = null;
    }
}
`
	assignPath := filepath.Join(src, "Assign.java")
	if err := os.WriteFile(assignPath, []byte(assign), 0o644); err != nil {
		t.Fatal(err)
	}
	args := append([]string{"--release", "17", "-nowarn", "-d", classes}, generated...)
	args = append(args, assignPath)
	out, err := exec.Command(javac, args...).CombinedOutput()
	if err == nil {
		t.Fatalf("javac accepted an element assignment through a public field from another package:\n%s", out)
	}
	for _, name := range []string{"layout", "defaults", "dest", "known"} {
		if !strings.Contains(string(out), name+" has private access") {
			t.Errorf("javac did not refuse %s with a private-access error:\n%s", name, out)
		}
	}

	// THE ACCESSOR HALF: the copies are mutable, and mutating them does not move
	// a decode of the same bytes.
	probe := `package outside;

import java.util.Arrays;

public final class Accessor {
    private static int failures = 0;

    private static void check(boolean ok, String what) {
        if (!ok) { System.out.println("FAILED: " + what); failures++; }
    }

    public static void main(String[] args) {
        probe.TFixed.Value v = new probe.TFixed.Value();
        v.a = 123;
        v.b = 1.5f;
        v.c = true;
        probe.TFixed.Value[] vals = { v };
        byte[] file = new byte[probe.TFixed.measure(1)];
        check(probe.TFixed.save(vals, 1, file) == file.length, "save writes the measured file");
        byte[] before = file.clone();

        byte[] layout = probe.TFixed.layout();
        if (layout.length > 0) { layout[0] = (byte) 0xff; }
        byte[] defaults = probe.TFixed.defaults();
        if (defaults.length > 0) { defaults[0] = (byte) 0xff; }
        int[] dest = probe.TFixed.dest();
        if (dest.length > 0) { dest[0] = 0x7fffffff; }
        probe.TableFixed.KnownLayout[] known = probe.TFixed.known();
        if (known.length > 0) {
            if (known[0].layout.length > 0) { known[0].layout[0] = (byte) 0xff; }
            known[0] = null;
        }

        probe.TFixed.Value[] out = { new probe.TFixed.Value() };
        probe.TableFixed.Report r = new probe.TableFixed.Report();
        int n = probe.TFixed.load(out, 1, file, probe.TableFixed.plan(1024), new short[1024],
                probe.TFixed.image(), r);
        check(n == 1, "a second decode reads the same bytes");
        check(out[0].a == 123 && out[0].b == 1.5f && out[0].c,
                "a mutated accessor copy did not change the decode");

        byte[] again = new byte[probe.TFixed.measure(1)];
        check(probe.TFixed.save(vals, 1, again) == again.length, "a second save writes");
        check(Arrays.equals(again, before), "a mutated accessor copy did not change the write");

        System.out.println(failures == 0 ? "OK" : "FAIL");
        if (failures != 0) { System.exit(1); }
    }
}
`
	probePath := filepath.Join(src, "Accessor.java")
	if err := os.WriteFile(probePath, []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	args = append([]string{"--release", "17", "-nowarn", "-d", classes}, generated...)
	args = append(args, probePath)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac accessor probe: %v\n%s", err, out)
	}
	out, err = exec.Command(javaBin, "-ea", "-cp", classes, "outside.Accessor").CombinedOutput()
	if err != nil {
		t.Fatalf("java outside.Accessor: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "OK") {
		t.Fatalf("accessor probe did not pass:\n%s", out)
	}
}
