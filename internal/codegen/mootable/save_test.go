package mootable_test

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// saveProbe is the moo main for one instance: it loads every table module,
// loads the wire at the root through load.moo, saves the value through
// save.moo and prints the saved bytes as one hex line, then the measure.
func saveProbe(g *generatedUnit, rootName string) string {
	return "--!strict\nlocal inputs = require(\"./inputs\")\nlocal load = require(\"./table_load\")\nlocal save = require(\"./table_save\")\nlocal St = require(\"./schema_tables\")\n" +
		g.requireAll() + fmt.Sprintf(`local report = load.newReport()
local v = load.loadTolerant(%q, inputs.wire, report)
if v == nil or report.refused > 0 then print("REFUSED") return end
local d = St.lookup(%q)
local out, why = save.save(d, v)
local measure = save.measure(d, v)
if out == nil or measure == nil then print("SAVENIL " .. (if why ~= nil then why.name .. " " .. tostring(why.field) else "")) return end
local parts = table.create(buffer.len(out))
for i = 0, buffer.len(out) - 1 do
	parts[i + 1] = string.format("%%02x", buffer.readu8(out, i))
end
print("x" .. table.concat(parts))
print(measure)
`, rootName, rootName)
}

// saveRoot loads one wire into a moo value and saves it, returning the saved
// bytes and the measure.
func saveRoot(t *testing.T, g *generatedUnit, wire []byte, root string) ([]byte, int, error) {
	t.Helper()
	out := g.run(t, saveProbe(g, root), map[string][]byte{"wire": wire})
	if out == "REFUSED" {
		return nil, 0, fmt.Errorf("the load refused the wire")
	}
	if strings.HasPrefix(out, "SAVENIL") {
		return nil, 0, fmt.Errorf("the save refused: %s", out)
	}
	lines := strings.Split(out, "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "x") {
		return nil, 0, fmt.Errorf("unexpected output %q", out)
	}
	saved, err := hex.DecodeString(strings.TrimPrefix(lines[0], "x"))
	if err != nil {
		return nil, 0, fmt.Errorf("bad hex line %q: %w", lines[0], err)
	}
	measure, err := strconv.Atoi(lines[1])
	if err != nil {
		return nil, 0, fmt.Errorf("bad measure line %q: %w", lines[1], err)
	}
	return saved, measure, nil
}

func firstDiff(a, b []byte) string {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return fmt.Sprintf("first difference at byte %d: saved %02x, wire %02x", i, a[i], b[i])
		}
	}
	return fmt.Sprintf("one is a prefix of the other at byte %d", min(len(a), len(b)))
}

func instanceRows(t *testing.T) map[string][]reportRow {
	t.Helper()
	out := map[string][]reportRow{}
	for _, row := range manifestRows(t) {
		if row.kind == "instance" {
			out[row.unit] = append(out[row.unit], row)
		}
	}
	return out
}

// TestMooTableSaveRoundTripsEveryInstance is the variable table wire's SAVE
// conformance gate (docs/SPEC-TABLES.md §3, §3.1): for every instance row of
// the tables manifest (wires the C++ reference and the Go engine wrote), load
// the wire, save the value through the emitted save.moo, and compare byte for
// byte with the same file; measure equals the saved length. A blob_shared
// instance is refused by every writer (§2.5), and is held to that refusal.
func TestMooTableSaveRoundTripsEveryInstance(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted save.moo)")
	t.Parallel()
	units, _ := readManifest(t)
	rows := instanceRows(t)
	total, compared, blobShared := 0, 0, 0
	for _, key := range sortedKeys(rows) {
		if _, ok := units[key]; !ok {
			t.Errorf("instance rows name unit %s, which the manifest does not declare", key)
			continue
		}
		total += len(rows[key])
		g := generateUnit(t, units, key)
		if g.err != nil {
			t.Errorf("unit %s is refused: %v", key, g.err)
			continue
		}
		for _, row := range rows[key] {
			wire := rowWire(t, row)
			saved, measure, err := saveRoot(t, g, wire, row.root)
			if row.blobShared {
				if err == nil || !strings.Contains(err.Error(), "blob_shared") {
					t.Errorf("instance %s (%s/%s): a shared blob must be refused as blob_shared, got %v", row.caseName, key, row.root, err)
				}
				blobShared++
				continue
			}
			if err != nil {
				t.Errorf("instance %s (%s/%s): %v", row.caseName, key, row.root, err)
				continue
			}
			if string(saved) != string(wire) {
				t.Errorf("instance %s (%s/%s): the save is not the wire: %d bytes saved, %d in the file; %s", row.caseName, key, row.root, len(saved), len(wire), firstDiff(saved, wire))
				continue
			}
			if measure != len(saved) {
				t.Errorf("instance %s (%s/%s): measure %d != saved length %d", row.caseName, key, row.root, measure, len(saved))
				continue
			}
			compared++
		}
	}
	if compared == 0 {
		t.Fatal("no instance compared")
	}
	if compared+blobShared != total {
		t.Errorf("%d instances compared and %d blob_shared refusals of %d in the manifest", compared, blobShared, total)
	}
	t.Logf("compared %d instances byte for byte, %d blob_shared refusals, of %d", compared, blobShared, total)
}

// TestMooTableSaveNegativeControl patches the emitted save.moo — one anchor,
// matched exactly once — so a scalar at its declared default is written
// rather than elided, and requires the byte comparison to go red on an
// instance that round-trips clean unpatched (docs/SPEC-TABLES.md §3 elision).
func TestMooTableSaveNegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted save.moo)")
	t.Parallel()
	units, _ := readManifest(t)
	rows := instanceRows(t)
	const anchor = "\tif not required and at_default(fd, val) then\n"
	for _, key := range sortedKeys(rows) {
		g := generateUnit(t, units, key)
		if g.err != nil {
			continue
		}
		for _, row := range rows[key] {
			wire := rowWire(t, row)
			saved, _, err := saveRoot(t, g, wire, row.root)
			if err != nil || string(saved) != string(wire) {
				continue
			}
			path := filepath.Join(g.dir, "table_save.moo")
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Count(string(src), anchor) != 1 {
				t.Fatalf("the scalar elision anchor is not in the emitted runtime exactly once")
			}
			if err := os.WriteFile(path, []byte(strings.Replace(string(src), anchor, "\tif false then\n", 1)), 0o644); err != nil {
				t.Fatal(err)
			}
			got, _, err := saveRoot(t, g, wire, row.root)
			if werr := os.WriteFile(path, src, 0o644); werr != nil {
				t.Fatal(werr)
			}
			if err != nil {
				t.Fatalf("instance %s/%s: the patched runtime failed: %v", key, row.root, err)
			}
			if string(got) == string(wire) {
				continue
			}
			t.Logf("instance %s/%s: the save comparison went red after patching the elision", key, row.root)
			return
		}
	}
	t.Fatal("no clean instance went red under the patched elision: the control proves nothing")
}
