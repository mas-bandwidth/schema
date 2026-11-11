package javatable

import (
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestJavaRetainNamesEveryOwnId holds the retaining save's id interner to
// docs/SPEC-TABLES.md §6.6: an id this build's own KNOWN table names takes the
// generated table and costs the caller's id list nothing, so a retaining save
// never charges one of this build's ids to that list. The generated KNOWN is
// ascending by UNSIGNED id, and a Java long compares signed, so the search
// must compare unsigned across the whole table, including both sides of 2^63.
func TestJavaRetainNamesEveryOwnId(t *testing.T) {
	slowtest.Gate(t, "the JDK (it compiles and runs the generated Java)")
	t.Parallel()
	javac, javaBin := javaToolsRequired(t)

	units := []struct{ name, dir string }{
		{"examples", filepath.Join("..", "..", "..", "tables", "examples")},
		{"lists", filepath.Join("..", "..", "..", "tables", "lists")},
		{"maps", filepath.Join("..", "..", "..", "tables", "maps")},
	}
	for _, unit := range units {
		unit := unit
		t.Run(unit.name, func(t *testing.T) {
			t.Parallel()
			u := unitFromDir(t, unit.dir)
			files, err := Generate(u)
			if err != nil {
				t.Fatalf("generate %s: %v", unit.dir, err)
			}
			if files["TableRetain.java"] == nil {
				t.Fatalf("%s emits no TableRetain.java, so it has no KNOWN table", unit.dir)
			}
			known := ir.TableWireIds(u)
			inTable := make(map[uint64]bool, len(known))
			below, above := 0, 0
			for _, id := range known {
				inTable[id] = true
				if id < 1<<63 {
					below++
				} else {
					above++
				}
			}
			if below == 0 || above == 0 {
				t.Fatalf("%s: KNOWN holds %d ids below and %d at or above 2^63, so it does not witness both halves", unit.dir, below, above)
			}
			absent := absentIds(known, inTable)
			driver := retainNameableDriver(u.Package, known, absent)
			out := runRetainJava(t, javac, javaBin, files, u.Package, "RetainNameableProbe", driver)
			t.Logf("%s: %s", unit.dir, strings.TrimSpace(string(out)))
		})
	}

	t.Run("round_trip", func(t *testing.T) {
		t.Parallel()
		dir := filepath.Join("..", "..", "..", "tables", "maps")
		u := unitFromDir(t, dir)
		files := cookUnitFiles(t, dir)
		wire := retainUnknownEnumWire(t, u)
		path := filepath.Join(t.TempDir(), "retain_unknown_enum.bin")
		if err := os.WriteFile(path, wire, 0o644); err != nil {
			t.Fatal(err)
		}
		out := runRetainJava(t, javac, javaBin, files, u.Package, "RetainOwnIdProbe", retainOwnIdDriver(u.Package, "WideRow"), path)
		t.Logf("%s", strings.TrimSpace(string(out)))
	})
}

// absentIds is a deterministic sample of ids the unit's KNOWN table does not
// hold, drawn on both sides of the signed pivot so the search is tried past
// the half a signed compare would visit.
func absentIds(known []uint64, inTable map[uint64]bool) []uint64 {
	var absent []uint64
	low, high := 0, 0
	add := func(id uint64) bool {
		if inTable[id] {
			return false
		}
		for _, have := range absent {
			if have == id {
				return false
			}
		}
		absent = append(absent, id)
		if id < 1<<63 {
			low++
		} else {
			high++
		}
		return true
	}
	for _, id := range []uint64{
		1, 2, 3, 4,
		1<<63 - 1, 1 << 63, 1<<63 + 1,
		^uint64(0) - 1, 0x5555555555555555, 0xaaaaaaaaaaaaaaaa,
	} {
		add(id)
	}
	for i := 0; low == 0 || high == 0 || len(absent) < 8; i++ {
		add(ir.TableWireId(fmt.Sprintf("not-a-name-in-this-unit-%d", i)))
	}
	return absent
}

func retainNameableDriver(pkg string, known, absent []uint64) string {
	var b strings.Builder
	fmt.Fprintf(&b, "package %s;\n\n", pkg)
	b.WriteString("public final class RetainNameableProbe {\n")
	b.WriteString("    static void check(boolean ok, String msg) { if (!ok) throw new AssertionError(msg); }\n")
	b.WriteString("    public static void main(String[] args) {\n")
	b.WriteString("        long[] known = {\n")
	for _, id := range known {
		fmt.Fprintf(&b, "            0x%016xL,\n", id)
	}
	b.WriteString("        };\n")
	b.WriteString("        long[] absent = {\n")
	for _, id := range absent {
		fmt.Fprintf(&b, "            0x%016xL,\n", id)
	}
	b.WriteString("        };\n")
	b.WriteString("        for (long id : known) { check(TableRetain.nameable(id), \"a KNOWN id is not nameable: 0x\" + Long.toHexString(id)); }\n")
	b.WriteString("        for (long id : absent) { check(!TableRetain.nameable(id), \"an id outside KNOWN is nameable: 0x\" + Long.toHexString(id)); }\n")
	b.WriteString("        System.out.println(\"nameable holds \" + known.length + \" known and rejects \" + absent.length + \" absent ids\");\n")
	b.WriteString("    }\n")
	b.WriteString("}\n")
	return b.String()
}

func retainOwnIdDriver(pkg, root string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "package %s;\n\n", pkg)
	b.WriteString("import java.nio.file.Files;\n")
	b.WriteString("import java.nio.file.Path;\n")
	b.WriteString("import java.util.Arrays;\n\n")
	b.WriteString("public final class RetainOwnIdProbe {\n")
	b.WriteString("    static void check(boolean ok, String msg) { if (!ok) throw new AssertionError(msg); }\n\n")
	b.WriteString("    public static void main(String[] args) throws Exception {\n")
	b.WriteString("        byte[] wire = Files.readAllBytes(Path.of(args[0]));\n")
	b.WriteString("        TableRetain retain = new TableRetain(1 << 20, 1);\n")
	b.WriteString("        TableVariable.Report report = new TableVariable.Report();\n")
	b.WriteString("        " + root + "Variable.Value v = " + root + "Retain.loadRetain(wire, retain, report);\n")
	b.WriteString("        check(v != null && !report.damaged && !report.refused, \"the retaining load refused a sound wire\");\n")
	b.WriteString("        check(report.retainLost == 0, \"the retaining load lost a record it had room for\");\n")
	b.WriteString("        byte[] got = " + root + "Retain.saveRetain(v, retain, report);\n")
	b.WriteString("        check(got != null, \"the retaining save refused\");\n")
	b.WriteString("        check(report.retainLost == 0, \"the save charged one of this build's own ids to the caller's id list\");\n")
	b.WriteString("        check(Arrays.equals(got, wire), \"the retained field did not save back byte for byte\");\n")
	b.WriteString("        System.out.println(\"an unknown enum field naming a KNOWN id saved back byte for byte with retain_lost=0\");\n")
	b.WriteString("    }\n")
	b.WriteString("}\n")
	return b.String()
}

// retainUnknownEnumWire is one variable-form body carrying an unknown enum
// field whose value names a KNOWN id below 2^63 that the signed order would not
// reach, then that field's id, the id it names, and the two-entry trailer. A
// retaining save must take the named id from the generated table, not from the
// caller's one-entry list, so it saves back byte for byte.
func retainUnknownEnumWire(t *testing.T, u *ir.Unit) []byte {
	t.Helper()
	known := ir.TableWireIds(u)
	inTable := make(map[uint64]bool, len(known))
	for _, id := range known {
		inTable[id] = true
	}
	var chosen uint64
	found := false
	for _, id := range known {
		if id < 1<<63 && !signedOrderFinds(known, id) {
			chosen = id
			found = true
			break
		}
	}
	if !found {
		t.Fatal("no KNOWN id below 2^63 sits where the signed order cannot reach it")
	}
	foreign := ir.TableWireId("future")
	if inTable[foreign] {
		t.Fatal("the witness's field name is already in KNOWN")
	}
	wire := []byte{ir.TableWireForm, 1, ir.TableKindEnum, 2, 0}
	wire = binary.LittleEndian.AppendUint64(wire, foreign)
	wire = binary.LittleEndian.AppendUint64(wire, chosen)
	wire = binary.LittleEndian.AppendUint64(wire, 2)
	return wire
}

// signedOrderFinds is the search the generated table must not use: it orders
// the unsigned ids as Java's signed long does. The round trip draws its witness
// from what that order misses, so the test holds the unsigned compare.
func signedOrderFinds(known []uint64, id uint64) bool {
	lo, hi := 0, len(known)-1
	want := int64(id)
	for lo <= hi {
		mid := (lo + hi) >> 1
		have := int64(known[mid])
		if have == want {
			return true
		}
		if have < want {
			lo = mid + 1
		} else {
			hi = mid - 1
		}
	}
	return false
}

// runRetainJava compiles one generated unit with a driver in the same package,
// so the driver can reach the package-private TableRetain.nameable, then runs
// it with the given arguments.
func runRetainJava(t *testing.T, javac, javaBin string, files map[string][]byte, pkg, class, driver string, args ...string) []byte {
	t.Helper()
	dir := t.TempDir()
	srcDir := filepath.Join(dir, pkg)
	classes := filepath.Join(dir, "classes")
	for _, d := range []string{srcDir, classes} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	var sources []string
	for name, data := range files {
		if !strings.HasSuffix(name, ".java") {
			continue
		}
		path := filepath.Join(srcDir, name)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, path)
	}
	driverPath := filepath.Join(srcDir, class+".java")
	if err := os.WriteFile(driverPath, []byte(driver), 0o644); err != nil {
		t.Fatal(err)
	}
	sources = append(sources, driverPath)
	compile := append([]string{"--release", "17", "-d", classes}, sources...)
	if out, err := exec.Command(javac, compile...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	run := append([]string{"-ea", "-cp", classes, pkg + "." + class}, args...)
	out, err := exec.Command(javaBin, run...).CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
	return out
}
