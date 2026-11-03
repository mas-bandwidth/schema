package javatable

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	javacodegen "github.com/mas-bandwidth/schema/v2/internal/codegen/java"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// storageCapHugeSchema holds the overflow witnesses: Huge's storage is
// 2^40-64 bytes, so 2^24+1 of them sum past 2^63 and wrap to a total under the
// cap. A measure that summed before it checked would accept them.
const storageCapHugeSchema = `package hugecheck
fixed table Big { a [16777216]int64 }
fixed table Huge { b [8191]Big
    c [16777208]int64 }
table Root { items []Huge
    next *Huge }
`

// storageCapOverflowWire writes a form-1 file whose Root holds count empty
// Huge bodies, as the items list or as node-table records.
func storageCapOverflowWire(u *ir.Unit, nodes bool, count uint64) []byte {
	var ids []uint64
	ref := func(id uint64) []byte {
		for i, v := range ids {
			if v == id {
				return binary.AppendUvarint(nil, uint64(i+1))
			}
		}
		ids = append(ids, id)
		return binary.AppendUvarint(nil, uint64(len(ids)))
	}
	framed := func(body []byte) []byte {
		return append(binary.AppendUvarint(nil, uint64(len(body))), body...)
	}
	var root []byte
	if nodes {
		table := ref(math.MaxUint64)
		huge := ref(ir.TableWireId(u.Tables["Huge"].WireName()))
		records := binary.AppendUvarint(nil, count)
		for range count {
			records = append(records, huge...)
			records = append(records, 1, 0)
		}
		root = append(append(table, byte(ir.TableKindString)), framed(records)...)
	} else {
		items := ref(ir.TableFieldWireId(u.Tables["Root"].Fields[0]))
		list := binary.AppendUvarint([]byte{byte(ir.TableKindTable)}, count)
		for range count {
			list = append(list, 1, 0)
		}
		root = append(append(items, byte(ir.TableKindArray)), framed(list)...)
	}
	wire := append([]byte{1}, root...)
	wire = append(wire, 0)
	for _, id := range ids {
		wire = binary.LittleEndian.AppendUint64(wire, id)
	}
	return binary.LittleEndian.AppendUint64(wire, uint64(len(ids)))
}

// storageCapCase is one wire: its path, and whether it passes the cap. An
// over wire with load set is also handed to load and loadRetain.
type storageCapCase struct {
	name, path string
	over, load bool
}

// TestStorageCapRefusedAtMeasureJava holds the storage cap at load
// (docs/SPEC-TABLES.md §11, §6.5) on the shared vectors in
// testdata/wire/tables/storage-cap: Big's storage is 2^27 bytes, so 8191 empty
// Big bodies measure under the 2^40-byte cap and 8192 measure over it, as the
// items list and as node records. RootVariable.loadMeasure answers a size at
// or under the cap for an at vector and -1 with the reason storage_over_cap
// for an over vector; load and loadRetain refuse an over vector by that name
// before the decode starts (the report names no root type yet), with
// malformed unset. A load of an at vector is never run.
func TestStorageCapRefusedAtMeasureJava(t *testing.T) {
	t.Parallel()
	dir, err := filepath.Abs("../../../testdata/wire/tables/storage-cap")
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile(filepath.Join(dir, "Cap.schema"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []storageCapCase
	for _, v := range []struct {
		file string
		over bool
	}{{"list_at.bin", false}, {"list_over.bin", true}, {"nodes_at.bin", false}, {"nodes_over.bin", true}} {
		cases = append(cases, storageCapCase{v.file, filepath.Join(dir, v.file), v.over, v.over})
	}
	runJavaStorageCap(t, unitFrom(t, string(src)), cases)
}

// TestStorageCapTermsRefusedBeforeTheyWrapJava: each list extent term and
// each node record is checked against the cap before the sum can wrap. 2^24+1
// Huge elements or records wrap an unchecked long sum to a total under the
// cap, so the measure must refuse them as storage_over_cap.
func TestStorageCapTermsRefusedBeforeTheyWrapJava(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, storageCapHugeSchema)
	dir := t.TempDir()
	var cases []storageCapCase
	for _, nodes := range []bool{false, true} {
		name := "list_wrap.bin"
		if nodes {
			name = "nodes_wrap.bin"
		}
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, storageCapOverflowWire(u, nodes, 1<<24+1), 0o644); err != nil {
			t.Fatal(err)
		}
		cases = append(cases, storageCapCase{name, path, true, false})
	}
	// only the variable form's classes are compiled: the Row, Block,
	// Descriptor, Cook and unit-view classes spell a record size as an int
	// literal, which a 2^40-64-byte record does not fit
	runJavaStorageCap(t, u, cases, "Row.java", "Block.java", "Descriptor.java", "Cook.java", "Layout.java", "CookWrite.java", "TableRegistry.java", "TableUnitView.java")
}

// runJavaStorageCap compiles the unit's Java and a driver that answers, for
// each case, the measure and its reason, and for a load case the load and
// loadRetain verdicts; then holds each answer to the cap.
func runJavaStorageCap(t *testing.T, u *ir.Unit, cases []storageCapCase, skip ...string) {
	t.Helper()
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	packet, err := javacodegen.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Int128.java", "UInt128.java"} {
		if data := packet[name]; data != nil {
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
		if slices.ContainsFunc(skip, func(suffix string) bool { return strings.HasSuffix(name, suffix) }) {
			continue
		}
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, path)
	}
	// the driver sits in the unit's package, so it reads the report's
	// package-private root type
	driver := fmt.Sprintf(`package %[1]s;
import java.nio.file.Files;
import java.nio.file.Paths;
public final class Driver {
    public static void main(String[] args) throws Exception {
        for (int i = 0; i < args.length; i += 2) {
            byte[] wire = Files.readAllBytes(Paths.get(args[i]));
            %[1]s.TableVariable.Measure m = new %[1]s.TableVariable.Measure();
            long size = %[1]s.RootVariable.loadMeasure(wire, m);
            System.out.println("M " + i + " " + size + " " + m.report.reason + " " + %[1]s.RootVariable.loadMeasure(wire));
            if (!args[i + 1].equals("load")) {
                continue;
            }
            %[1]s.TableVariable.Report report = new %[1]s.TableVariable.Report();
            Object v = %[1]s.RootVariable.load(wire, report);
            System.out.println("L " + i + " " + (v != null) + " " + report.refused + " " + report.reason + " " + report.malformed + " " + report.rootTypeId);
            %[1]s.TableRetain retain = new %[1]s.TableRetain(1 << 16, 1024);
            report = new %[1]s.TableVariable.Report();
            v = %[1]s.RootRetain.loadRetain(wire, retain, report);
            System.out.println("L " + i + " " + (v != null) + " " + report.refused + " " + report.reason + " " + report.malformed + " " + report.rootTypeId);
        }
    }
}
`, pkg)
	dpath := filepath.Join(dir, "Driver.java")
	if err := os.WriteFile(dpath, []byte(driver), 0o644); err != nil {
		t.Fatal(err)
	}
	sources = append(sources, dpath)
	sort.Strings(sources)
	javac, java := javaTools(t)
	if out, err := exec.Command(javac, append([]string{"--release", "17", "-d", classes}, sources...)...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	// a 1 GiB heap: a load that built a Big would fail here, not page in a TiB
	args := []string{"-Xmx1g", "-ea", "-cp", classes, pkg + ".Driver"}
	for _, c := range cases {
		mode := "measure"
		if c.load {
			mode = "load"
		}
		args = append(args, c.path, mode)
	}
	out, err := exec.Command(java, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
	answers := map[string][]string{}
	for line := range strings.SplitSeq(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) > 2 && (f[0] == "M" || f[0] == "L") {
			key := f[0] + f[1]
			answers[key] = append(answers[key], strings.Join(f[2:], " "))
		}
	}
	for i, c := range cases {
		measure := answers[fmt.Sprintf("M%d", 2*i)]
		if len(measure) != 1 {
			t.Fatalf("%s: driver output:\n%s", c.name, out)
		}
		var size, plain int64
		var reason string
		if _, err := fmt.Sscanf(measure[0], "%d %s %d", &size, &reason, &plain); err != nil {
			t.Fatalf("%s: %q: %v", c.name, measure[0], err)
		}
		if !c.over {
			if size <= 0 || size > ir.MaxSizeBytes || plain != size || reason != "null" {
				t.Errorf("%s: loadMeasure %d reason %s plain %d, want a size at or under 2^40", c.name, size, reason, plain)
			}
			continue
		}
		if size != -1 || plain != -1 || reason != "storage_over_cap" {
			t.Errorf("%s: loadMeasure %d reason %s plain %d, want -1 storage_over_cap", c.name, size, reason, plain)
		}
		if !c.load {
			continue
		}
		loads := answers[fmt.Sprintf("L%d", 2*i)]
		if len(loads) != 2 {
			t.Fatalf("%s: driver output:\n%s", c.name, out)
		}
		for j, l := range loads {
			if l != "false true storage_over_cap false 0" {
				t.Errorf("%s: load %d answered %q, want no value, refused storage_over_cap, not malformed, stopped before the decode set a root type", c.name, j, l)
			}
		}
	}
}
