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
// registers the descriptors), reads the wire from inputs.moo, and reads it at
// the root through the one read (table_refusal.moo's ReadStrict, which takes
// a fixed root's form 3 and a variable root's form 1), printing the VERDICT:
// read or refused (docs/SPEC-TABLES.md §4, §6.5). A read that refuses
// returns no value.
func loadProbe(g *generatedUnit, rootName string) string {
	return "--!strict\nlocal inputs = require(\"./inputs\")\nlocal load = require(\"./table_load\")\nlocal R = require(\"./table_refusal\")\n" + g.requireAll() +
		fmt.Sprintf(`local T = require("./%[2]s")
local report = load.newReport()
local v = R.ReadStrict(T, %[1]q, inputs.wire, report)
print(if v ~= nil and report.reason == nil then "read" else "refused")
`, rootName, tableModuleOf(g, rootName))
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

// TestMooTableLoadMatchesTheReports reads every report row of the tables
// conformance manifest through the emitted one read and compares its verdict
// with reports.txt (the Go engine's own verdicts). Every instance row's wire
// reads, except a wire-refused row, which refuses (docs/SPEC-TABLES.md §3,
// §3.1, §4). The verdict is the whole contract: no counter or reason is
// compared (docs/rulings.json refusal-verdict-only).
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
			want := "read"
			if row.blobShared {
				want = "refused"
			}
			if out != want {
				t.Errorf("instance %s: the one read answers %s, want %s", row.caseName, out, want)
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
			t.Errorf("report %s (%s/%s): moo %s, want %s", row.caseName, row.unit, row.root, out, want)
			continue
		}
		compared++
	}
	if compared == 0 || instances == 0 {
		t.Fatalf("compared %d report rows and %d instance rows: the gate proves nothing", compared, instances)
	}
	t.Logf("%d report rows compared, %d instance rows loaded, %d rows of refused units", compared, instances, refusedRows)
}

// TestMooTableLoadNegativeControl: a repeated map key is damage
// (docs/plans/strict-only.md decision 5), so the one read refuses a wire with
// one. The control patches the emitted load.moo (one anchor, matched exactly
// once) so the repeat is not seen, and requires the same wire to read.
func TestMooTableLoadNegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted load.moo)")
	t.Parallel()
	g := generateFiles(t, map[string]string{"Dup.schema": "package dup\ntable Box\n{\n    m map[uint8]uint8\n}\n"})
	wire := duplicateMapWire()
	if got := runWire(t, g, "Box", wire); got != "refused" {
		t.Fatalf("the control, an unpatched repeated key: got %s, want refused", got)
	}
	path := filepath.Join(g.dir, "table_load.moo")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	const anchor = "report.repeatedKey += 1"
	if strings.Count(string(src), anchor) != 1 {
		t.Fatalf("the repeated-key anchor is not in the emitted runtime exactly once")
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(src), anchor, "report.repeatedKey += 0", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := runWire(t, g, "Box", wire); got != "read" {
		t.Fatalf("the patched runtime still refuses the repeated key: %s", got)
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
