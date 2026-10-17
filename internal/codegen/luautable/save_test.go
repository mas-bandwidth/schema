package luautable_test

import (
	"bufio"
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
// (which registers the descriptors), loads the wire at the root, saves it
// through save.luau, and prints the saved bytes as a hex line then the measure.
func saveProbe(g *generatedUnit, rootName string) string {
	var b strings.Builder
	b.WriteString("local inputs = require(\"./inputs\")\nlocal load = require(\"./load\")\nlocal save = require(\"./save\")\nlocal St = require(\"./schema_tables\")\n")
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
	b.WriteString("if report.refused > 0 then print(\"REFUSED\"); return end\n")
	fmt.Fprintf(&b, "local d = St.lookup(%s)\n", q)
	b.WriteString("local out = save.save(d, v)\n")
	b.WriteString("if out == nil then print(\"SAVENIL\"); return end\n")
	b.WriteString("local parts = table.create(buffer.len(out.bytes))\n")
	b.WriteString("for i = 0, buffer.len(out.bytes) - 1 do\n")
	b.WriteString("\tparts[i + 1] = string.format(\"%02x\", buffer.readu8(out.bytes, i))\n")
	b.WriteString("end\n")
	b.WriteString("print(table.concat(parts))\n")
	b.WriteString("print(out.measure)\n")
	return b.String()
}

// saveRoot loads one instance's wire file into a Luau value and saves it
// through the emitted save.luau under the pinned interpreter, returning the
// saved bytes and the measure.
func saveRoot(t *testing.T, g *generatedUnit, key, root string) ([]byte, int, error) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(saveProbe(g, root)), 0o644); err != nil {
		return nil, 0, err
	}
	out := strings.TrimSpace(string(luaurun.Run(t, g.dir, "main.luau", map[string][]byte{"wire": wireBytes(t, key, root)})))
	if out == "REFUSED" {
		return nil, 0, fmt.Errorf("the load refused the wire")
	}
	if out == "SAVENIL" {
		return nil, 0, fmt.Errorf("the save refused")
	}
	lines := strings.Split(out, "\n")
	if len(lines) < 2 {
		return nil, 0, fmt.Errorf("unexpected output %q", out)
	}
	saved, err := hex.DecodeString(lines[0])
	if err != nil {
		return nil, 0, fmt.Errorf("bad hex line %q: %v", lines[0], err)
	}
	var measure int
	if _, err := fmt.Sscanf(lines[1], "%d", &measure); err != nil {
		return nil, 0, fmt.Errorf("bad measure line %q: %v", lines[1], err)
	}
	return saved, measure, nil
}

// wireBytes returns the conformance wire file's bytes for one instance.
func wireBytes(t *testing.T, key, inst string) []byte {
	t.Helper()
	f, err := os.Open(filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) >= 5 && fields[0] == "instance" && fields[2] == key && fields[3] == inst {
			data, err := os.ReadFile(filepath.Join(root, fields[4]))
			if err != nil {
				t.Fatalf("instance %s/%s: %v", key, inst, err)
			}
			return data
		}
	}
	return nil
}

// TestLuauTableSaveRoundTripsEveryInstance is the variable table wire's SAVE
// conformance gate (docs/SPEC-TABLES.md §3): for every instance row of the
// tables manifest whose unit the Luau target generates, load the wire file,
// save it through the emitted save.luau walk, and byte-compare against the
// same file. Measure equals the saved length.
func TestLuauTableSaveRoundTripsEveryInstance(t *testing.T) {
	t.Parallel()
	units, roots := readManifest(t)
	if len(units) == 0 {
		t.Fatal("the manifest names no units")
	}
	generated := 0
	compared := 0
	var keys []string
	for k := range units {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		g := generateUnit(t, units, key)
		if g.err != nil {
			t.Logf("luau refuses unit %s: %v", key, g.err)
			continue
		}
		generated++
		for _, root := range roots[key] {
			wire := wireBytes(t, key, root)
			if wire == nil {
				t.Errorf("instance %s/%s: no wire file found", key, root)
				continue
			}
			saved, measure, err := saveRoot(t, g, key, root)
			if err != nil {
				t.Errorf("instance %s/%s: %v", key, root, err)
				continue
			}
			if string(saved) != string(wire) {
				t.Errorf("instance %s/%s: save length %d != wire length %d", key, root, len(saved), len(wire))
				continue
			}
			if measure != len(saved) {
				t.Errorf("instance %s/%s: measure %d != saved length %d", key, root, measure, len(saved))
				continue
			}
			compared++
		}
	}
	if generated == 0 {
		t.Fatal("no unit generated")
	}
	t.Logf("compared %d instances", compared)
}

// TestLuauTableSaveNegativeControl patches the emitted save.luau in memory —
// one anchor, matched exactly once — so a field at its declared default is
// written rather than elided, and requires the byte comparison to go red
// (docs/SPEC-TABLES.md §3 elision).
func TestLuauTableSaveNegativeControl(t *testing.T) {
	t.Parallel()
	units, roots := readManifest(t)
	if len(units) == 0 {
		t.Fatal("the manifest names no units")
	}
	var keys []string
	for k := range units {
		if len(roots[k]) > 0 {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	tested := false
	for _, key := range keys {
		g := generateUnit(t, units, key)
		if g.err != nil {
			continue
		}
		root := roots[key][0]
		wire := wireBytes(t, key, root)
		if wire == nil {
			continue
		}
		saved, _, err := saveRoot(t, g, key, root)
		if err != nil {
			continue
		}
		if string(saved) != string(wire) {
			t.Skip("control baseline already red; no elision to falsify")
		}
		path := filepath.Join(g.dir, "save.luau")
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		const anchor = "if val ~= fd.def then\n\t\t\theader(w, ids, fd, 1)\n\t\t\tw:u8(val and 1 or 0)"
		if strings.Count(string(src), anchor) != 1 {
			t.Fatalf("the bool elision anchor is not in the emitted runtime exactly once")
		}
		patched := strings.Replace(string(src), anchor, "if true then\n\t\t\theader(w, ids, fd, 1)\n\t\t\tw:u8(val and 1 or 0)", 1)
		if patched == string(src) {
			t.Fatal("the patch changed nothing")
		}
		if err := os.WriteFile(path, []byte(patched), 0o644); err != nil {
			t.Fatal(err)
		}
		got, _, err := saveRoot(t, g, key, root)
		if err != nil {
			t.Fatalf("the patched runtime failed: %v", err)
		}
		if string(got) == string(wire) {
			t.Fatalf("the patched runtime still elides the defaulted field")
		}
		tested = true
		t.Logf("the save comparison went red after patching the elision")
		break
	}
	if !tested {
		t.Skip("no unit generated that round-trips")
	}
}
