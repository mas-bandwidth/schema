package squirreltable_test

// M19 of docs/PORTING.md, the Squirrel cell: THE WRITER HOLDS THE SORT
// (docs/SPEC-TABLES.md §2.8). A Squirrel map is an array of entry instances
// in the caller's order, and the save orders it. Loading a corpus wire and
// saving it again cannot see the sort, because the load hands back the entries
// already ascending; so this gate REVERSES every root map's entries before the
// save and still requires the corpus bytes.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
)

// reversedSavePrelude is savePrelude with every map field of the root
// reversed between the load and the save.
var reversedSavePrelude = strings.Replace(savePrelude,
	"  local b = S.save(T, root, v);\n",
	"  foreach (f in T.Descriptors[root].fields) if (f.shape == \"map\" && v[f.member] != null) v[f.member].reverse();\n  local b = S.save(T, root, v);\n", 1)

// runMapSort saves every carried mapdemo instance with its root maps reversed,
// over the save runtime replaced by save when it is not empty, and returns
// the names of the rows whose bytes are not the corpus wire's.
func runMapSort(t *testing.T, save string) (moved []string, compared int) {
	t.Helper()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	u, _ := mapdemoCarried(t, root)
	dir := t.TempDir()
	mod := emitUnit(t, dir, "mapdemo", u)
	if save != "" {
		writeFile(t, filepath.Join(dir, "mapdemo", "schema_table_save.nut"), []byte(save))
	}
	var p strings.Builder
	p.WriteString(reversedSavePrelude)
	inputs := map[string][]byte{}
	var names []string
	for _, in := range readInstanceRows(t, filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt")) {
		if in.unit != "mapdemo" || in.goRefusal != "" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(in.wire)))
		if err != nil {
			t.Fatal(err)
		}
		inputs[in.name] = data
		names = append(names, in.name)
		fmt.Fprintf(&p, "run(%q, %q, %q, %q);\n", in.unit, mod, in.root, in.name)
	}
	if len(names) == 0 {
		t.Fatal("the mapdemo corpus names no instance row Squirrel can carry")
	}
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(p.String()))
	got := parseSaved(t, sqrun.Run(t, dir, "main.nut", inputs))
	for _, name := range names {
		if fmt.Sprintf("%x", inputs[name]) != got[name].hex {
			moved = append(moved, name)
		}
	}
	return moved, len(names)
}

func TestSquirrelMapWriterSort(t *testing.T) {
	t.Parallel()
	moved, n := runMapSort(t, "")
	if len(moved) != 0 {
		t.Fatalf("with every root map reversed, %d of %d mapdemo rows saved other bytes than the corpus: %s", len(moved), n, strings.Join(moved, " "))
	}
	t.Logf("%d mapdemo rows saved byte for byte with their root maps reversed", n)
}

// TestSquirrelMapWriterSortNegativeControl removes the save's sort in a
// scratch copy of the runtime (one anchor, matched exactly once) and requires
// the reversed rows to move.
func TestSquirrelMapWriterSortNegativeControl(t *testing.T) {
	t.Parallel()
	const anchor = "  order.sort(function(x, y) { return mapKeyCmp(val[x][keyMember], val[y][keyMember]); });\n"
	runtime := string(squirreltable.Runtime()["schema_table_save.nut"])
	if n := strings.Count(runtime, anchor); n != 1 {
		t.Fatalf("the control's anchor appears %d times in the save runtime, want exactly once", n)
	}
	moved, n := runMapSort(t, strings.Replace(runtime, anchor, "", 1))
	if len(moved) == 0 {
		t.Fatalf("with the writer's sort removed, all %d reversed mapdemo rows still saved the corpus bytes: the gate is blind", n)
	}
	t.Logf("with the sort removed, %d of %d rows moved: %s", len(moved), n, strings.Join(moved, " "))
}
