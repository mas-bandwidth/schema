package luautable_test

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
)

// saveProbe is the Luau main for one instance: it loads every table module
// (which registers the descriptors), loads the wire at the root through
// schema_load.luau, saves the value through schema_save.luau, and prints the saved bytes as
// one hex line, then the measure (docs/PORT-LUAU.md, THE HARNESS).
func saveProbe(g *generatedUnit, rootName string) string {
	var b strings.Builder
	b.WriteString("local inputs = require(\"./inputs\")\nlocal load = require(\"./schema_load\")\nlocal save = require(\"./schema_save\")\nlocal St = require(\"./schema_tables\")\n")
	var mods []string
	for name := range g.files {
		if strings.HasSuffix(name, "Table.luau") {
			mods = append(mods, strings.TrimSuffix(name, ".luau"))
		}
	}
	sort.Strings(mods)
	for _, m := range mods {
		fmt.Fprintf(&b, "require(\"./%s\")\n", m)
	}
	q := strconv.Quote(rootName)
	b.WriteString("local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }\n")
	fmt.Fprintf(&b, "local v = load.load(%s, inputs.wire, report)\n", q)
	b.WriteString("if v == nil or report.refused > 0 then print(\"REFUSED\"); return end\n")
	fmt.Fprintf(&b, "local d = St.lookup(%s)\n", q)
	b.WriteString("local out = save.save(d, v)\n")
	b.WriteString("local measure = save.measure(d, v)\n")
	b.WriteString("if out == nil or measure == nil then print(\"SAVENIL\"); return end\n")
	b.WriteString("local parts = table.create(buffer.len(out))\n")
	b.WriteString("for i = 0, buffer.len(out) - 1 do\n")
	b.WriteString("\tparts[i + 1] = string.format(\"%02x\", buffer.readu8(out, i))\n")
	b.WriteString("end\n")
	b.WriteString("print(\"x\" .. table.concat(parts))\n")
	b.WriteString("print(measure)\n")
	return b.String()
}

// saveRoot loads one instance's wire file into a Luau value and saves it
// through the emitted schema_save.luau under the pinned interpreter, returning the
// saved bytes and the measure.
func saveRoot(t *testing.T, g *generatedUnit, wire []byte, root string) ([]byte, int, error) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(saveProbe(g, root)), 0o644); err != nil {
		return nil, 0, err
	}
	out := strings.TrimSpace(string(luaurun.Run(t, g.dir, "main.luau", map[string][]byte{"wire": wire})))
	if out == "REFUSED" {
		return nil, 0, fmt.Errorf("the load refused the wire")
	}
	if out == "SAVENIL" {
		return nil, 0, fmt.Errorf("the save refused")
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

// firstDiff names the first byte at which two wires differ.
func firstDiff(a, b []byte) string {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return fmt.Sprintf("first difference at byte %d: saved %02x, wire %02x", i, a[i], b[i])
		}
	}
	return fmt.Sprintf("one is a prefix of the other at byte %d", min(len(a), len(b)))
}

// instanceRows is every instance row of the tables manifest, in file order,
// grouped by unit: one row per wire file (a root can have several).
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

func rowWire(t *testing.T, row reportRow) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, row.wire))
	if err != nil {
		t.Fatalf("instance %s: %v", row.caseName, err)
	}
	return data
}

// TestLuauTableSaveRoundTripsEveryInstance is the variable table wire's SAVE
// conformance gate (docs/SPEC-TABLES.md §3, §3.1): for every instance row of
// the tables manifest whose unit the Luau target generates, load the wire
// file, save the value through the emitted schema_save.luau walk, and byte-compare
// against the same file (the conformance wire surface). Measure equals the
// saved length. A unit the target refuses is named with its reason, and its
// instances are counted out.
func TestLuauTableSaveRoundTripsEveryInstance(t *testing.T) {
	t.Parallel()
	units, _ := readManifest(t)
	rows := instanceRows(t)
	var keys []string
	for k := range rows {
		if _, ok := units[k]; !ok {
			t.Errorf("instance rows name unit %s, which the manifest does not declare", k)
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	total, compared, refusedRows, refusedSaves := 0, 0, 0, 0
	for _, key := range keys {
		total += len(rows[key])
		g := generateUnit(t, units, key)
		if g.err != nil {
			if _, ok := refused[key]; !ok {
				t.Errorf("unit %s is refused and the refused list does not name it: %v", key, g.err)
			}
			refusedRows += len(rows[key])
			t.Logf("refused unit: %s (%d instances): %v", key, len(rows[key]), g.err)
			continue
		}
		for _, row := range rows[key] {
			wire := rowWire(t, row)
			saved, measure, err := saveRoot(t, g, wire, row.root)
			if err != nil {
				// a blob_shared wire (go-refusal=blob_shared) names one blob
				// record from two slots: every writer refuses it (docs/SPEC-TABLES.md
				// §2.5), as Go's does, and TestLuauBlobNamedTwice holds the
				// refusal to the oracle's own value.
				if row.blobShared {
					refusedSaves++
					continue
				}
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
	if compared+refusedRows+refusedSaves != total {
		t.Errorf("%d instances compared, %d save refusals and %d of refused units, of %d in the manifest", compared, refusedSaves, refusedRows, total)
	}
	t.Logf("compared %d instances byte for byte (%d save refusals, %d in the manifest, %d of refused units)", compared, refusedSaves, total, refusedRows)
}

// TestLuauTableSaveNegativeControl patches the emitted schema_save.luau in memory —
// one anchor, matched exactly once — so a scalar field at its declared
// default is written rather than elided, and requires the byte comparison to
// go red on an instance that round-trips clean unpatched (docs/SPEC-TABLES.md
// §3 elision).
func TestLuauTableSaveNegativeControl(t *testing.T) {
	t.Parallel()
	units, _ := readManifest(t)
	rows := instanceRows(t)
	var keys []string
	for k := range rows {
		if _, ok := units[k]; ok {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	const anchor = "\tif not required and at_default(fd, val) then return true end\n"
	for _, key := range keys {
		g := generateUnit(t, units, key)
		if g.err != nil {
			continue
		}
		for _, row := range rows[key] {
			r := row.root
			wire := rowWire(t, row)
			saved, _, err := saveRoot(t, g, wire, r)
			if err != nil || string(saved) != string(wire) {
				continue // the round-trip gate names it; the control needs a clean baseline
			}
			path := filepath.Join(g.dir, "schema_save.luau")
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Count(string(src), anchor) != 1 {
				t.Fatalf("the scalar elision anchor is not in the emitted runtime exactly once")
			}
			patched := strings.Replace(string(src), anchor, "\tif false then return true end\n", 1)
			if err := os.WriteFile(path, []byte(patched), 0o644); err != nil {
				t.Fatal(err)
			}
			got, _, err := saveRoot(t, g, wire, r)
			if werr := os.WriteFile(path, src, 0o644); werr != nil {
				t.Fatal(werr)
			}
			if err != nil {
				t.Fatalf("instance %s/%s: the patched runtime failed: %v", key, r, err)
			}
			if string(got) == string(wire) {
				continue // every scalar of this instance rides; try the next one
			}
			t.Logf("instance %s/%s: the save comparison went red after patching the elision (%d bytes, wire %d)", key, r, len(got), len(wire))
			return
		}
	}
	t.Fatal("no clean instance went red under the patched elision: the control proves nothing")
}
