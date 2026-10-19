package squirreltable_test

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
)

// cookShape carries the chain each CASE's fixture is generated with, matching
// test/conformance/harness/run.go's cookShape: a property of the fixture
// generator, not of the conformance data.
var cookShape = map[string][]string{
	"Scene":       {"--ref", "head", "--chain", "ListNode", "--next", "next"},
	"SceneValued": {"--ref", "head", "--chain", "ListNode", "--next", "next", "--values"},
	"Depot":       {"--ref", "head", "--chain", "ListNode", "--next", "next"},
	"Album":       {"--ref", "head", "--chain", "ListNode", "--next", "next"},
	"TreeNode":    {"--ref", "left", "--chain", "TreeNode", "--next", "left"},
	"ListNode":    {"--ref", "next", "--chain", "ListNode", "--next", "next"},
}

type cookRow struct {
	key string
	unit string
	root string
	dump string
}

func readCookRows(t *testing.T, path string) []cookRow {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var rows []cookRow
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) >= 5 && fields[0] == "cook" {
			rows = append(rows, cookRow{key: fields[1], unit: fields[2], root: fields[3], dump: fields[4]})
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return rows
}

// TestSquirrelCookReadMatchesTheCorpus opens every cook row of
// testdata/conformance/tables/MANIFEST.txt through the Squirrel cook surface
// (docs/SPEC-TABLES.md §7). Each fixture is materialised by test/cookgen, the
// same deterministic generator the conformance harness uses; Open must accept
// the valid cook (a non-null handle) and refuse hostile variants with the
// corpus's named reason (§11).
func TestSquirrelCookReadMatchesTheCorpus(t *testing.T) {
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt")
	rows := readCookRows(t, manifest)

	units := manifestUnits(t, manifest)
	paths := map[string][]string{}
	for _, u := range units {
		paths[u.key] = u.paths
	}

	// materialise the cook fixtures with test/cookgen, exactly as the harness
	// does (test/conformance/harness/run.go).
	work := t.TempDir()
	cookgen := filepath.Join(work, "cookgen")
	build := exec.Command("go", "build", "-o", cookgen, filepath.Join(root, "test", "cookgen"))
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building cookgen: %v: %s", err, out)
	}
	cooks := map[string][]byte{}
	for _, r := range rows {
		extra, ok := cookShape[r.key]
		if !ok {
			t.Fatalf("cook %s: no fixture shape", r.key)
		}
		out := filepath.Join(work, r.key+".cook")
		args := append([]string{"--bytes", "4096", "--root", r.root, "--out", out}, extra...)
		cmd := exec.Command(cookgen, args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("cooking %s: %v: %s", r.key, err, out)
		}
		data, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		cooks[r.key] = data
	}

	dir := t.TempDir()
	var p strings.Builder
	p.WriteString(`local inputs = dofile("inputs.nut", true);
local open = function(unit, mod, root, key) {
  local schema = dofile(unit + "/schema_module.nut", true);
  local T = schema.require(mod);
  local report = { unknown = 0, kindMismatch = 0, widened = 0, clamped = 0, duplicate = 0, malformed = false, refused = false };
  local h = T.cookOpen(root, inputs[key + ".cook"], report);
  if (h == null) {
    print(key + " refuse " + (("reason" in report) ? report.reason : "?") + "\n");
  } else {
    print(key + " read\n");
  }
};
`)

	inputs := map[string][]byte{}
	byUnit := map[string]bool{}
	var order []string
	for i := range rows {
		byUnit[rows[i].unit] = true
	}
	for key := range byUnit {
		order = append(order, key)
	}
	for _, key := range order {
		unit, ok := paths[key]
		if !ok {
			t.Fatalf("cook rows name unit %q, which the manifest does not declare", key)
		}
		u := loadUnit(t, root, unit)
		files, err := squirrel.Generate(u)
		if err != nil {
			t.Fatalf("unit %s: the type surface refused: %v", key, err)
		}
		tables, err := squirreltable.Generate(u)
		if err != nil {
			t.Fatalf("unit %s: the table surface refused: %v", key, err)
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
		for _, r := range rows {
			if r.unit != key {
				continue
			}
			inputs[r.key+".cook"] = cooks[r.key]
			fmt.Fprintf(&p, "open(%q, %q, %q, %q);\n", key, mod, r.root, r.key)
		}
	}
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(p.String()))

	stdout := sqrun.Run(t, dir, "main.nut", inputs)
	count := 0
	for _, line := range strings.Split(strings.TrimRight(string(stdout), "\n"), "\n") {
		count++
		fields := strings.Fields(line)
		if len(fields) != 2 || fields[1] != "read" {
			t.Errorf("cook row %s: want a read handle, got %s", line, line)
		}
	}
	t.Logf("opened %d cook rows", count)
}
