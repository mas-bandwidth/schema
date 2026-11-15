package squirreltable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
)

// ONE NESTING BOUND (docs/SPEC-TABLES.md §4): the root body is at depth 0 and
// each structural payload inside it is one level deeper. Depth 128 passes and
// 129 refuses in every walk of the binary forms, unknown fields included
// (§6.5). And the load answers every input with a value or a refusal; it never
// throws (docs/PORT-SQUIRREL.md, BYTE BUFFERS), whatever the caller's own
// stack holds when it calls.
var sqDepthVectors = []struct {
	name string
	read bool
}{
	{"depth_tables_128", true},
	{"depth_arrays_128", true},
	{"depth_union_tables_64", true},
	{"depth_keyed_128", true},
	{"depth_tables_129", false},
	{"depth_arrays_129", false},
	{"depth_union_tables_65", false},
	{"depth_keyed_129", false},
	{"depth_tables_5000", false},
}

// sqDepthCallerDepths are the caller stacks each vector is loaded from: the
// script's top level, then inside 50 and 200 nested calls of its own.
var sqDepthCallerDepths = []int{0, 50, 200}

// sqDepthPrelude loads one input through the walk and the strict read,
// catching anything either raises, from `depth` nested calls deep. The nesting
// keeps each frame live (no tail call), so the caller's stack really is deep.
const sqDepthPrelude = `local inputs = dofile("inputs.nut", true);
local schema = dofile("graphdemo/schema_module.nut", true);
local T = schema.require(%q);
local L = schema.require("schema_table_load");
local R = schema.require("schema_table_refusal");
local answer = function(key) {
  try {
    local report = {
      unknown = 0, mismatch = 0, widened = 0, oor = 0,
      repeated = 0, bodyDamaged = false, refused = false
    };
    L.loadWalk(T, "Scene", inputs[key], report);
    return R.ReadStrict(T, "Scene", inputs[key], {}) == null ? "refused" : "read";
  } catch (e) {
    return "threw";
  }
};
local nest;
nest = function(n, key) {
  if (n == 0) return answer(key);
  local v = nest(n - 1, key);
  return v;
};
local run = function(depth, key) {
  print(key + " " + depth.tostring() + " " + nest(depth, key) + "\n");
};
`

// TestSquirrelWireCheckNeverThrowsOnDepth loads every pinned depth vector of
// testdata/wire/tables/framing through the Squirrel strict load of
// graphdemo.Scene from three caller depths, and holds each to its verdict:
// the same verdict every time, and never a throw.
func TestSquirrelWireCheckNeverThrowsOnDepth(t *testing.T) {
	t.Parallel()
	sqrun.Require(t)
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, u := range manifestUnits(t, filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt")) {
		if u.key == "graphdemo" {
			paths = u.paths
		}
	}
	if paths == nil {
		t.Fatal("the manifest declares no graphdemo unit")
	}
	unit := loadUnit(t, root, paths)
	files, err := squirrel.Generate(unit)
	if err != nil {
		t.Fatalf("the type surface refused: %v", err)
	}
	tables, err := squirreltable.Generate(unit)
	if err != nil {
		t.Fatalf("the table surface refused: %v", err)
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "graphdemo")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		writeFile(t, filepath.Join(out, name), data)
	}
	for name, data := range tables {
		writeFile(t, filepath.Join(out, name), data)
	}

	var p strings.Builder
	fmt.Fprintf(&p, sqDepthPrelude, squirreltable.ModuleName(unit))
	inputs := map[string][]byte{}
	want := map[string]string{}
	for _, v := range sqDepthVectors {
		data, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "tables", "framing", v.name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		inputs[v.name] = data
		want[v.name] = "refused"
		if v.read {
			want[v.name] = "read"
		}
		for _, d := range sqDepthCallerDepths {
			fmt.Fprintf(&p, "run(%d, %q);\n", d, v.name)
		}
	}
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(p.String()))

	seen := 0
	stdout := sqrun.Run(t, dir, "main.nut", inputs)
	for line := range strings.SplitSeq(strings.TrimRight(string(stdout), "\n"), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 {
			t.Fatalf("probe line %q is not a vector, a caller depth and a verdict", line)
		}
		seen++
		if fields[2] != want[fields[0]] {
			t.Errorf("%s from %s nested calls: the load answered %s, want %s", fields[0], fields[1], fields[2], want[fields[0]])
		}
	}
	if n := len(sqDepthVectors) * len(sqDepthCallerDepths); seen != n {
		t.Errorf("the probe answered %d loads, want %d", seen, n)
	}
}
