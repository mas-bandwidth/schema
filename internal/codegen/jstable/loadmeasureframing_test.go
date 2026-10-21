package jstable

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestLoadMeasureCoversVariableFramingJavaScript pins the JavaScript variable
// LoadMeasure (docs/SPEC-TABLES.md §6.5, §20.3) to internal/tablewire's
// FileRegionMeasure, byte for byte, on the variable-class corpus roots the wire
// fuzzer's cause (a) found: graphdemo.Scene graph_tree, blobdemo.Catalog,
// streamdemo.Feed, tblw1.Fleet, a listdemo root and a mapdemo root.
func TestLoadMeasureCoversVariableFramingJavaScript(t *testing.T) {
	t.Parallel()
	node := jsNode(t)

	cases := []struct {
		unitDir string
		root    string
		wire    string
	}{
		{"tables/pointers", "Scene", "testdata/wire/tables/graph_tree.bin"},
		{"tables/blobs", "Catalog", "testdata/wire/tables/blob_small.bin"},
		{"tables/stream", "Feed", "testdata/wire/tables/stream_chain.bin"},
		{"test/tables/W1.schema", "Fleet", "testdata/wire/tables/w1_fleet.bin"},
		{"tables/lists", "Save", "testdata/wire/tables/list_tables.bin"},
		{"tables/lists", "Save", "testdata/wire/tables/list_scalars.bin"},
		{"tables/lists", "Save", "testdata/wire/tables/list_empty.bin"},
		{"tables/lists", "Save", "testdata/wire/tables/list_erased.bin"},
		{"tables/lists", "Mixed", "testdata/wire/tables/list_mixed.bin"},
		{"tables/lists", "Album", "testdata/wire/tables/list_shared.bin"},
		{"tables/lists", "Album", "testdata/wire/tables/list_before_pointer.bin"},
		{"tables/lists", "Sheet", "testdata/wire/tables/list_nested.bin"},
		{"tables/lists", "Army", "testdata/wire/tables/list_of_maps.bin"},
		{"tables/lists", "Unbounded", "testdata/wire/tables/list_migrates.bin"},
		{"tables/maps", "Cells", "testdata/wire/tables/map_cells.bin"},
		{"tables/maps", "Chunks", "testdata/wire/tables/map_chunks.bin"},
		{"tables/maps", "Crews", "testdata/wire/tables/map_crews.bin"},
		{"tables/maps", "Depth", "testdata/wire/tables/map_depth.bin"},
		{"tables/maps", "Docs", "testdata/wire/tables/map_docs.bin"},
		{"tables/maps", "Fleet", "testdata/wire/tables/map_empty.bin"},
		{"tables/maps", "Fleet", "testdata/wire/tables/map_full.bin"},
		{"tables/maps", "Pairs", "testdata/wire/tables/map_pairs.bin"},
		{"tables/maps", "Slots", "testdata/wire/tables/map_slots.bin"},
		{"tables/maps", "Spans", "testdata/wire/tables/map_spans.bin"},
		{"tables/maps", "Text", "testdata/wire/tables/map_text.bin"},
		{"tables/maps", "Trails", "testdata/wire/tables/map_trails.bin"},
		{"tables/maps", "Runs", "testdata/wire/tables/map_runs.bin"},
	}

	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}

	for _, c := range cases {
		t.Run(c.root+"@"+c.wire, func(t *testing.T) {
			t.Parallel()
			u := loadUnitForMeasure(t, repo, c.unitDir)
			root := findTable(t, u, c.root)

			wireData, err := os.ReadFile(filepath.Join(repo, c.wire))
			if err != nil {
				t.Fatal(err)
			}

			want, whole, refused := tablewire.FileRegionMeasure(u, root, wireData)
			if refused {
				t.Fatalf("oracle refused the wire for %s", c.root)
			}
			if !whole {
				t.Fatalf("oracle did not read the whole wire for %s", c.root)
			}

			files, err := generateVariableFiles(u, nil)
			if err != nil {
				t.Fatalf("generateVariableFiles: %v", err)
			}

			dir := t.TempDir()
			for name, content := range files {
				if err := os.WriteFile(filepath.Join(dir, name), content, 0o600); err != nil {
					t.Fatal(err)
				}
			}

			home := runtimeHome(u)
			script := filepath.Join(dir, "probe.mjs")
			probe := `
import { readFileSync } from "node:fs";
import { %[1]sLoadMeasure } from "./%[2]sVariable.js";
const wire = new Uint8Array(readFileSync(process.argv[2]));
console.log(%[1]sLoadMeasure(wire).toString());
`
			src := strings.ReplaceAll(strings.ReplaceAll(probe, "%[1]s", c.root), "%[2]s", home)
			if err := os.WriteFile(script, []byte(src), 0o600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(node, script, filepath.Join(repo, c.wire))
			cmd.Dir = dir
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("node probe failed: %v\n%s", err, out)
			}
			got, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
			if err != nil {
				t.Fatalf("could not parse LoadMeasure %q: %v", out, err)
			}
			if got != want {
				t.Fatalf("LoadMeasure for %s: JS=%d, oracle FileRegionMeasure=%d", c.root, got, want)
			}
		})
	}
}

// loadUnitForMeasure loads a unit's schema files: a single .schema file when
// unitDir names one, else every .schema in the directory, sorted.
func loadUnitForMeasure(t *testing.T, repo, unitDir string) *ir.Unit {
	t.Helper()
	if strings.HasSuffix(unitDir, ".schema") {
		return loadUnit(t, filepath.Join(repo, unitDir))
	}
	matches, err := filepath.Glob(filepath.Join(repo, unitDir, "*.schema"))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(matches)
	return loadUnit(t, matches...)
}
