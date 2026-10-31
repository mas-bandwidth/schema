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
					drivers["CookReversed"+root] = cookReversedDriverSource("CookReversed"+root, u.pkg, root)
				}
				dir := compileCookUnit(t, javac, files, u.pkg, drivers)
				for root, rows := range u.rows {
					for _, row := range rows {
						inst, cook := m.instance[row], m.cook[row]
						class := "CookGolden" + root
						if w.name == "unsorted_map" {
							class = "CookReversed" + root
						}
						out, err := exec.Command(javaBin, "-ea", "-cp", dir, class,
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

// THE MAP WRITER HOLDS THE SORT (docs/SPEC-TABLES.md §2.8; docs/PORTING.md
// M19): a loaded value's maps are already ascending, so a cook of one cannot
// show whether the writer sorts. This gate reverses every map's entries at
// every depth before cooking and requires the same golden bytes in both
// orders; the unsorted_map witness above is its control.
func TestJavaCookMapWriterHoldsTheSort(t *testing.T) {
	t.Parallel()
	javac, javaBin := javaTools(t)
	m := readCookManifest(t)
	repo := filepath.Join("..", "..", "..")
	units := []struct {
		schema, pkg string
		rows        map[string][]string
	}{
		{filepath.Join(repo, "tables", "maps"), "mapdemo", map[string][]string{
			"Fleet": {"map_full"}, "Depth": {"map_depth"}, "Cells": {"map_cells"}, "Crews": {"map_crews"}}},
		{filepath.Join(repo, "tables", "lists"), "listdemo", map[string][]string{"Army": {"list_of_maps"}}},
	}
	for _, u := range units {
		drivers := map[string]string{}
		for root := range u.rows {
			drivers["CookReversed"+root] = cookReversedDriverSource("CookReversed"+root, u.pkg, root)
		}
		dir := compileCookUnit(t, javac, cookUnitFiles(t, u.schema), u.pkg, drivers)
		for root, rows := range u.rows {
			for _, row := range rows {
				inst, cook := m.instance[row], m.cook[row]
				out, err := exec.Command(javaBin, "-ea", "-cp", dir, "CookReversed"+root,
					filepath.Join(repo, inst[2]), filepath.Join(repo, cook[0]), filepath.Join(repo, cook[1])).CombinedOutput()
				if err != nil {
					t.Errorf("%s with every map reversed: %v\n%s", row, err, out)
				}
			}
		}
	}
}

func cookReversedDriverSource(class, pkg, root string) string {
	src := cookDriverSource(class, pkg, root)
	// reverse every map's entries, at every depth, right after the load
	src = strings.Replace(src,
		"        if (value == null || report.refused || report.malformed) throw new AssertionError(\"wire load\");\n",
		"        if (value == null || report.refused || report.malformed) throw new AssertionError(\"wire load\");\n"+
			"        reverseMaps(value, PKG.ROOTRow.cookInfo(), new java.util.IdentityHashMap<>());\n", 1)
	src = strings.Replace(src, "PKG.ROOTRow", pkg+"."+root+"Row", 1)
	helper := `
    static void reverseMaps(Object v, PKG.TableCookInfo info, java.util.IdentityHashMap<Object, Boolean> seen) throws Exception {
        if (v == null || info == null || seen.put(v, Boolean.TRUE) != null) return;
        for (PKG.TableCookFieldInfo f : info.fields) {
            Object x;
            try { x = v.getClass().getField(f.sourceName).get(v); } catch (NoSuchFieldException e) { continue; }
            if (x == null) continue;
            if (f.storage == PKG.TableCookStorage.MAP) {
                java.util.List<?> l = (java.util.List<?>) x;
                java.util.Collections.reverse(l);
                for (Object e : l) reverseMaps(e, f.element.record(), seen);
            } else if (f.storage == PKG.TableCookStorage.LIST) {
                if (f.element.record() != null) for (Object e : (java.util.List<?>) x) reverseMaps(e, f.element.record(), seen);
            } else if (f.storage == PKG.TableCookStorage.RECORD && f.record() != null) {
                if (x instanceof java.util.List<?>) { for (Object e : (java.util.List<?>) x) reverseMaps(e, f.record(), seen); }
                else reverseMaps(x, f.record(), seen);
            }
        }
    }
}
`
	src = strings.TrimSuffix(strings.TrimRight(src, "\n"), "}") + strings.ReplaceAll(helper, "PKG", pkg)
	return src
}
