package squirreltable_test

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
)

// cookWritePrelude is the cook-write probe's helper: load an instance's wire
// into a value, then measure and write its cook in both byte orders and answer
// whether each half is byte-equal to the fixture test/cookgen pinned
// (docs/SPEC-TABLES.md §7.6).
const cookWritePrelude = `local inputs = dofile("inputs.nut", true);
local run = function(mod, root, key, unit) {
  local schema = dofile(unit + "/schema_module.nut", true);
  local T = schema.require(mod);
  local L = schema.require("schema_table_load");
  local report = {
    unknown = 0, kindMismatch = 0, widened = 0, clamped = 0,
    duplicate = 0, malformed = false, refused = false
  };
  local value = L.loadTolerant(T, root, inputs[key + ".wire"], report);
  if (value == null) { print(key + " load refused\n"); return; }
  local size = T.CookMeasure(root, value);
  local arms = [[1, ".little"], [2, ".big"]];
  foreach (a in arms) {
    local order = a[0];
    local want = inputs[key + a[1]];
    if (size != want.len()) { print(key + " " + order + " size " + size + " want " + want.len() + "\n"); continue; }
    local buf = blob(size);
    if (!T.Cook(root, value, buf, order)) { print(key + " " + order + " refused\n"); continue; }
    local bad = -1;
    for (local i = 0; i < size; i++) if (buf[i] != want[i]) { bad = i; break; }
    if (bad < 0) print(key + " " + order + " ok\n");
    else print(key + " " + order + " diff " + bad + " got " + (buf[bad] & 0xff) + " want " + (want[bad] & 0xff) + "\n");
  }
};
`

type cookWriteRow struct {
	instance string
	unit     string
	root     string
	wire     string
	little   string
	big      string
}

func readCookWriteRows(t *testing.T, path string) ([]cookWriteRow, map[string]instanceRow) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var rows []cookWriteRow
	instances := map[string]instanceRow{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		switch fields[0] {
		case "instance":
			if len(fields) >= 5 {
				instances[fields[1]] = instanceRow{name: fields[1], unit: fields[2], root: fields[3], wire: fields[4]}
			}
		case "cook-write":
			if len(fields) >= 4 {
				in := instances[fields[1]]
				rows = append(rows, cookWriteRow{instance: fields[1], unit: in.unit, root: in.root, wire: in.wire, little: fields[2], big: fields[3]})
			}
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return rows, instances
}

// TestSquirrelCookWriterMatchesTheCorpus writes every cook-write row of
// testdata/conformance/tables/MANIFEST.txt through the Squirrel cook write
// surface (docs/SPEC-TABLES.md §7.6) and pins the bytes against test/cookgen's
// in BOTH byte orders, byte for byte, and CookMeasure against each file's
// length.
func TestSquirrelCookWriterMatchesTheCorpus(t *testing.T) {
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt")
	rows, _ := readCookWriteRows(t, manifest)
	units := manifestUnits(t, manifest)
	paths := map[string][]string{}
	for _, u := range units {
		paths[u.key] = u.paths
	}

	type byUnit struct {
		rows []cookWriteRow
	}
	grouped := map[string]*byUnit{}
	var order []string
	for _, r := range rows {
		g, ok := grouped[r.unit]
		if !ok {
			g = &byUnit{}
			grouped[r.unit] = g
			order = append(order, r.unit)
		}
		g.rows = append(g.rows, r)
	}

	dir := t.TempDir()
	var p strings.Builder
	p.WriteString(cookWritePrelude)
	inputs := map[string][]byte{}
	var refused []string
	count := 0
	for _, key := range order {
		unit, ok := paths[key]
		if !ok {
			t.Fatalf("cook-write rows name unit %q, which the manifest does not declare", key)
		}
		u := loadUnit(t, root, unit)
		files, err := squirrel.Generate(u)
		if err != nil {
			t.Fatalf("unit %s: the type surface refused: %v", key, err)
		}
		tables, err := squirreltable.Generate(u)
		if err != nil {
			refused = append(refused, key)
			continue
		}
		out := filepath.Join(dir, key)
		if err := os.MkdirAll(out, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, data := range files {
			writeFile(t, filepath.Join(out, name), data)
		}
		for name, data := range tables {
			writeFile(t, filepath.Join(out, name), data)
		}
		mod := squirreltable.ModuleName(u)
		for _, r := range grouped[key].rows {
			wire, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(r.wire)))
			if err != nil {
				t.Fatal(err)
			}
			inputs[r.instance+".wire"] = wire
			little, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(r.little)))
			if err != nil {
				t.Fatal(err)
			}
			inputs[r.instance+".little"] = little
			big, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(r.big)))
			if err != nil {
				t.Fatal(err)
			}
			inputs[r.instance+".big"] = big
			fmt.Fprintf(&p, "run(%q, %q, %q, %q);\n", mod, r.root, r.instance, key)
			count++
		}
	}
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(p.String()))

	stdout := sqrun.Run(t, dir, "main.nut", inputs)
	results := map[string]string{}
	detail := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(string(stdout), "\n"), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 && len(fields) != 8 {
			t.Errorf("cook-write probe line %q is not a case and a verdict", line)
			continue
		}
		results[fields[0]+" "+fields[1]] = fields[2]
		if len(fields) == 8 {
			detail[fields[0]+" "+fields[1]] = fields[2] + " at byte " + fields[3] + ": got " + fields[5] + " want " + fields[7]
		}
	}
	matched := 0
	for _, r := range rows {
		for _, arm := range []struct {
			order  string
			suffix string
		}{{"1", "little"}, {"2", "big"}} {
			got, ok := results[r.instance+" "+arm.order]
			if !ok {
				t.Errorf("cook-write %s %s: no result (refused unit?)", r.instance, arm.suffix)
				continue
			}
			if got != "ok" {
				t.Errorf("cook-write %s %s: %s", r.instance, arm.suffix, detail[r.instance+" "+arm.order])
				continue
			}
			matched++
		}
	}
	if matched != count*2 {
		t.Errorf("matched %d of %d expected halves", matched, count*2)
	}
	for _, r := range refused {
		t.Logf("refused unit %s", r)
	}
	t.Logf("wrote %d cook-write rows", count)
}
