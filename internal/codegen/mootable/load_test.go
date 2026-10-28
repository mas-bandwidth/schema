package mootable_test

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// reportsExpect reads reports.txt into case -> expected line.
func reportsExpect(t *testing.T) map[string]string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "testdata", "conformance", "tables", "reports.txt"))
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]string{}
	for line := range strings.SplitSeq(string(data), "\n") {
		if strings.HasPrefix(line, "#") || strings.TrimSpace(line) == "" {
			continue
		}
		f := strings.Fields(line)
		if len(f) >= 2 {
			m[f[0]] = f[1]
		}
	}
	return m
}

// loadProbe is the moo main for one case: it loads every table module (which
// registers the descriptors), reads the wire from inputs.moo, loads it at the
// root through load.moo's tolerant walk and prints the report surface line
// u,k,w,c,d,m,verdict (test/conformance/README.md).
func loadProbe(g *generatedUnit, rootName string) string {
	return "--!strict\nlocal inputs = require(\"./inputs\")\nlocal load = require(\"./table_load\")\n" + g.requireAll() +
		fmt.Sprintf(`local report = load.newReport()
local v = load.loadTolerant(%q, inputs.wire, report)
local verdict = "read"
if report.refused > 0 or (v == nil and report.nonFinite > 0) then verdict = "refused" end
print(string.format("%%d,%%d,%%d,%%d,%%d,%%s,%%s",
	report.unknown, report.kindMismatch, report.widened, report.clamped,
	report.duplicate, tostring(report.malformed), verdict))
`, rootName)
}

func runWire(t *testing.T, g *generatedUnit, rootName string, wire []byte) string {
	t.Helper()
	return g.run(t, loadProbe(g, rootName), map[string][]byte{"wire": wire})
}

func rowWire(t *testing.T, row reportRow) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, row.wire))
	if err != nil {
		t.Fatalf("case %s: %v", row.caseName, err)
	}
	return data
}

// TestMooTableLoadMatchesTheReports loads every report row of the tables
// conformance manifest through the emitted load.moo and prints
// u,k,w,c,d,m,verdict as the conformance report surface does, comparing with
// reports.txt (the Go engine's own answers). Every instance row's wire loads
// and is not refused (docs/SPEC-TABLES.md §3, §3.1, §4).
func TestMooTableLoadMatchesTheReports(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted load.moo)")
	t.Parallel()
	units, _ := readManifest(t)
	rows := manifestRows(t)
	expect := reportsExpect(t)
	cache := map[string]*generatedUnit{}
	compared, instances, refusedRows := 0, 0, 0
	for _, row := range rows {
		if _, ok := units[row.unit]; !ok {
			continue
		}
		g, ok := cache[row.unit]
		if !ok {
			g = generateUnit(t, units, row.unit)
			cache[row.unit] = g
		}
		if g.err != nil {
			refusedRows++
			continue
		}
		out := runWire(t, g, row.root, rowWire(t, row))
		if row.kind == "instance" {
			if strings.HasSuffix(out, ",refused") {
				t.Errorf("instance %s: the load refused a wire its own unit wrote: %s", row.caseName, out)
			}
			instances++
			continue
		}
		want, ok := expect[row.caseName]
		if !ok {
			t.Errorf("report %s: reports.txt names no such case", row.caseName)
			continue
		}
		if out != want {
			t.Errorf("report %s (%s/%s):\nmoo:  %s\nwant: %s", row.caseName, row.unit, row.root, out, want)
			continue
		}
		compared++
	}
	if compared == 0 || instances == 0 {
		t.Fatalf("compared %d report rows and %d instance rows: the gate proves nothing", compared, instances)
	}
	t.Logf("%d report rows compared, %d instance rows loaded, %d rows of refused units", compared, instances, refusedRows)
}

// TestMooTableLoadNegativeControl patches the emitted load.moo — one anchor,
// matched exactly once — so a duplicate map key is not counted, and requires
// the report comparison to go red on a wire with one (docs/SPEC-TABLES.md §4).
func TestMooTableLoadNegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted load.moo)")
	t.Parallel()
	g := generateFiles(t, map[string]string{"Dup.schema": "package dup\ntable Box\n{\n    m map[uint8]uint8\n}\n"})
	const want = "0,0,0,0,1,false,read"
	wire := duplicateMapWire()
	if got := runWire(t, g, "Box", wire); got != want {
		t.Fatalf("the control, an unpatched duplicate: got %s, want %s", got, want)
	}
	path := filepath.Join(g.dir, "table_load.moo")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	const anchor = "report.duplicate += 1"
	if strings.Count(string(src), anchor) != 1 {
		t.Fatalf("the duplicate anchor is not in the emitted runtime exactly once")
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(src), anchor, "report.duplicate += 0", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := runWire(t, g, "Box", wire); got == want {
		t.Fatalf("the patched runtime still reports the duplicate: %s", got)
	}
}

func leb(v uint64) []byte {
	var out []byte
	for v >= 0x80 {
		out = append(out, byte(v)|0x80)
		v >>= 7
	}
	return append(out, byte(v))
}

func u64le(v uint64) []byte {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, v)
	return b
}

// duplicateMapWire is the form-1 wire of Box{m} with two map entries under one
// key: the map field (kind 14), its body's element kind 13 and count 2, the two
// entry bodies, the terminator, and the id table (§3, §2.8).
func duplicateMapWire() []byte {
	entry := append(leb(2), 6, 7)
	entry = append(entry, leb(3)...)
	entry = append(entry, 6, 9, 0)
	body := []byte{13}
	body = append(body, leb(2)...)
	body = append(body, leb(uint64(len(entry)))...)
	body = append(body, entry...)
	body = append(body, leb(uint64(len(entry)))...)
	body = append(body, entry...)
	wire := []byte{1}
	wire = append(wire, leb(1)...)
	wire = append(wire, 14)
	wire = append(wire, leb(uint64(len(body)))...)
	wire = append(wire, body...)
	wire = append(wire, 0)
	wire = append(wire, u64le(ir.TableWireId("m"))...)
	wire = append(wire, u64le(ir.TableWireId("key"))...)
	wire = append(wire, u64le(ir.TableWireId("value"))...)
	wire = append(wire, u64le(3)...)
	return wire
}
