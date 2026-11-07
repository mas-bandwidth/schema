package javatable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	javacodegen "github.com/mas-bandwidth/schema/v2/internal/codegen/java"
	"github.com/mas-bandwidth/schema/v2/internal/memceiling"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// wideRecordDir holds a record past 4 GiB (docs/SPEC-TABLES.md §11): Huge is
// three 2 GiB parts and an int32 whose offset is past 4 GiB.
const wideRecordDir = "../../../testdata/wire/tables/storage-cap/wide"

// TestWideRecordCompilesJava: every class generated for the wide unit
// compiles, and every stored size and offset it carries equals the layout
// model's: the rows' sizes and offsets, the cook and block descriptors, the
// block projection's constants and the unit view's field offset.
//
// A Java array indexes by int, so the byte[] views (Row, Block, Cook) cannot
// reach a position past Integer.MAX_VALUE: the probe holds Huge's tail
// accessor to refusing it by ArithmeticException rather than reading another
// position. Nothing builds a Huge.
func TestWideRecordCompilesJava(t *testing.T) {
	memceiling.Guard(t, memceiling.Default)
	out, err := runWideRecordJava(t, "")
	if err != nil {
		t.Fatalf("wide-record probe: %v\n%s", err, out)
	}
}

// TestWideRecordNegativeControlJava narrows HugeRow's size back to an int, and
// the unit must stop compiling: a record past 2 GiB does not fit one.
func TestWideRecordNegativeControlJava(t *testing.T) {
	memceiling.Guard(t, memceiling.Default)
	out, err := runWideRecordJava(t, "public static final long size = 6442450952L;")
	if err == nil {
		t.Fatalf("the unit compiled with HugeRow.size narrowed to int:\n%s", out)
	}
	if !strings.Contains(out, "integer number too large") {
		t.Fatalf("the narrowed unit failed for another reason:\n%s", out)
	}
}

// runWideRecordJava compiles every generated class and a probe beside them,
// then runs the probe. narrow, when set, is a line of HugeRow.java put back
// at int before the compile.
func runWideRecordJava(t *testing.T, narrow string) (string, error) {
	t.Helper()
	schema, err := os.ReadFile(filepath.Join(wideRecordDir, "Wide.schema"))
	if err != nil {
		t.Fatal(err)
	}
	u := unitFrom(t, string(schema))
	huge := ir.RecordLayout(u, u.Tables["Huge"])
	tail := huge.FieldByName("tail")
	var proj *ir.BlockLayout
	for _, bl := range ir.Blocks(u).Tables {
		if bl.Table.Name == "Huge" {
			proj = bl
		}
	}
	if proj == nil {
		t.Fatal("Huge has no block form")
	}
	projTail := proj.Projection.FieldByName("tail")
	if huge.Size <= 1<<32 || tail.Offset <= 1<<32 {
		t.Fatalf("Huge is %d bytes with tail at %d; the unit no longer reaches past 4 GiB", huge.Size, tail.Offset)
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	packet, err := javacodegen.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range packet {
		if _, ok := files[name]; !ok {
			files[name] = data
		}
	}
	pkg := javaPackageOf(u)
	dir := t.TempDir()
	classes := filepath.Join(dir, "classes")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	var sources []string
	for name, data := range files {
		if narrow != "" && name == "HugeRow.java" {
			if !strings.Contains(string(data), narrow) {
				t.Fatalf("the negative control matched nothing in HugeRow.java: %q", narrow)
			}
			data = []byte(strings.Replace(string(data), narrow, strings.ReplaceAll(strings.ReplaceAll(narrow, "long", "int"), "L;", ";"), 1))
		}
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, path)
	}
	probe := fmt.Sprintf(`package %[1]s;
public final class WideProbe {
    static int failures = 0;
    static void check(boolean ok, String what) {
        if (!ok) { System.out.println("FAIL: " + what); failures++; }
    }
    public static void main(String[] args) {
        final long size = %[2]dL, tail = %[3]dL, projection = %[4]dL, projectionTail = %[5]dL, part = %[6]dL;
        check(HugeRow.size == size, "HugeRow.size " + HugeRow.size);
        check(HugeRow.offsets[1] == tail, "HugeRow.offsets[1] " + HugeRow.offsets[1]);
        check(HugeRow.cookInfo().size == size, "cook size " + HugeRow.cookInfo().size);
        check(HugeRow.cookInfo().fields[1].offset == tail, "cook tail offset " + HugeRow.cookInfo().fields[1].offset);
        check(PartRow.blockInfo().size == part, "Part block size " + PartRow.blockInfo().size);
        check(PartRow.size == part, "PartRow.size " + PartRow.size);
        check(HugeBlock.projectionSize == projection, "projectionSize " + HugeBlock.projectionSize);
        check(HugeBlock.projectionOffsets[1] == projectionTail, "projectionOffsets[1] " + HugeBlock.projectionOffsets[1]);
        check(HugeBlock.type().size == projection, "block descriptor size " + HugeBlock.type().size);
        check(HugeBlock.type().fields[1].offset == projectionTail, "block descriptor tail " + HugeBlock.type().fields[1].offset);
        check(HugeDescriptor.tableType().fields[1].offset == tail, "view tail offset " + HugeDescriptor.tableType().fields[1].offset);
        // a byte[] position past Integer.MAX_VALUE does not exist: the accessor
        // refuses it rather than reading another position
        boolean refused = false;
        try {
            HugeRow.tail(new byte[64], 0);
        } catch (ArithmeticException e) {
            refused = true;
        }
        check(refused, "HugeRow.tail read past Integer.MAX_VALUE");
        if (failures != 0) { System.exit(1); }
        System.out.println("wide record ok");
    }
}
`, pkg, huge.Size, tail.Offset, proj.Projection.Size, projTail.Offset, ir.RecordLayout(u, u.Tables["Part"]).Size)
	ppath := filepath.Join(dir, "WideProbe.java")
	if err := os.WriteFile(ppath, []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	sources = append(sources, ppath)
	sort.Strings(sources)
	javac, java := javaTools(t)
	if out, err := exec.Command(javac, append([]string{"--release", "17", "-d", classes}, sources...)...).CombinedOutput(); err != nil {
		return string(out), err
	}
	// a 1 GiB heap: a probe that built a Huge would fail here, not page in 6 GiB
	out, err := exec.Command(java, "-Xmx1g", "-ea", "-cp", classes, pkg+".WideProbe").CombinedOutput()
	if err == nil && !strings.Contains(string(out), "wide record ok") {
		return string(out), fmt.Errorf("the probe did not report")
	}
	return string(out), err
}
