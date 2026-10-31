package javatable

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// THE NODE EXTENT'S NEGATIVE CONTROL (docs/PORTING.md M19; docs/SPEC-TABLES.md
// §2.8, §2.9, §7.6; tla/NodeExtent.tla). The gate is the cook-write surface,
// TestBlockBuildAndCookWriteJava, holding every map and list instance byte for
// byte against `schema cook` in both orders. Each witness here plants one of
// the layout model's reversed rules into the generated writer
// (TableCookWrite.java) and must turn the map and list rows red:
//
//   - extent_over_record: the extent begins at the node's base, over the
//     record's own storage (MCNodeExtentExtentOverRecord.cfg);
//   - skip_align: an array laid at the running offset, unaligned
//     (MCNodeExtentSkipAlign.cfg);
//   - children_first: an element's own arrays laid before its parent's whole
//     array is reserved (MCNodeExtentChildrenFirst.cfg);
//   - unsorted_map: a map's entries laid in the value's order, not by key.
func TestJavaCookExtentNegativeControl(t *testing.T) {
	t.Parallel()
	witnesses := []struct{ name, from, to string }{
		{"extent_over_record", "at + aligned(n.info.size, FLOOR),", "at, /* SABOTAGED: the extent over the record */"},
		{"skip_align", "cursor[0] = aligned(cursor[0], f.extentAlign);", "/* SABOTAGED: no alignment */"},
		{"children_first", "cursor[0] += (long) n * f.extentSize;", "/* SABOTAGED: reserved after the children */"},
		{"unsorted_map", "if (f.storage != TableCookStorage.MAP || out.size() < 2) {", "if (true) { // SABOTAGED: entries in the value's order"},
	}
	units := []struct {
		schema, pkg string
		rows        map[string][]string // root -> rows
	}{
		{filepath.Join("..", "..", "..", "tables", "maps"), "mapdemo", map[string][]string{
			"Fleet": {"map_full"}, "Depth": {"map_depth"}, "Cells": {"map_cells"}, "Crews": {"map_crews"}}},
		{filepath.Join("..", "..", "..", "tables", "lists"), "listdemo", map[string][]string{
			"Save": {"list_tables"}, "Sheet": {"list_nested"}, "Army": {"list_of_maps"}, "Mixed": {"list_mixed"}}},
	}
	m := readCookManifest(t)
	repo := filepath.Join("..", "..", "..")
	for _, w := range witnesses {
		t.Run(w.name, func(t *testing.T) {
			t.Parallel()
			javac, javaBin := javaTools(t)
			red := 0
			for _, u := range units {
				files := cookUnitFiles(t, u.schema)
				src := string(files["TableCookWrite.java"])
				if strings.Count(src, w.from) != 1 {
					t.Fatalf("the %s witness must plant exactly one site in %s's TableCookWrite.java, found %d", w.name, u.pkg, strings.Count(src, w.from))
				}
				if w.name == "children_first" {
					// reserve the parent's array only AFTER its elements' arrays
					src = strings.Replace(src, w.from, w.to, 1)
					// the text is the formatter's (google-java-format), so the
					// anchor is the carve loop's last argument line and its close
					after := "                        f.element);\n            }\n            return;"
					if strings.Count(src, after) != 1 {
						t.Fatalf("children_first: the carve loop's close is not where the witness expects it")
					}
					src = strings.Replace(src, after,
						"                        f.element);\n            }\n            cursor[0] += (long) n * f.extentSize;\n            return;", 1)
				} else {
					src = strings.Replace(src, w.from, w.to, 1)
				}
				files["TableCookWrite.java"] = []byte(src)
				drivers := map[string]string{}
				for root := range u.rows {
					drivers["CookGolden"+root] = cookDriverSource("CookGolden"+root, u.pkg, root)
				}
				dir := compileCookUnit(t, javac, files, u.pkg, drivers)
				for root, rows := range u.rows {
					for _, row := range rows {
						inst, cook := m.instance[row], m.cook[row]
						out, err := exec.Command(javaBin, "-ea", "-cp", dir, "CookGolden"+root,
							filepath.Join(repo, inst[2]), filepath.Join(repo, cook[0]), filepath.Join(repo, cook[1])).CombinedOutput()
						if err != nil && strings.Contains(string(out), "cook bytes order") {
							red++
						} else if err != nil {
							t.Logf("%s %s: red for another reason:\n%s", w.name, row, out)
						}
					}
				}
			}
			if red == 0 {
				t.Fatalf("the %s witness left every map and list row byte-identical: the gate cannot see the layout rule", w.name)
			}
		})
	}
}
