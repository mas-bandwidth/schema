package lobstertable_test

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
)

// THE WRITER HOLDS THE SORT (docs/PORTING.md M19; docs/SPEC-TABLES.md §2.8):
// a map is an array of entries in ascending key order on the wire, and the
// save derives that order from the entries it is handed, whatever order the
// caller built them in. A load hands the save entries already ascending (the
// reader's ascending check), so a load-then-save round trip cannot see the
// sort go missing; this gate loads every map instance of the manifest,
// REVERSES every map's entries at every depth, saves, and holds the bytes to
// the Go engine's save of the same wire.

const mapSortHelpers = `def reverse_maps(rec:schema_rt.Record) -> int:
    var n = 0
    for(rec.fields) sl:
        if sl.fd.s == "map" and length(sl.entries) > 1:
            let rev = []::schema_rt.Value
            var j = length(sl.entries) - 1
            while j >= 0:
                push(rev, sl.entries[j])
                j--
            sl.entries = rev
            n++
        for(sl.entries) e:
            let t = e.tab
            if t:
                n += reverse_maps(t)
        let c = sl.cell.tab
        if c:
            n += reverse_maps(c)
        for(sl.elems) e:
            let t = e.tab
            if t:
                n += reverse_maps(t)
    return n
def hx(s:string) -> string:
    var o = ""
    for(s) c:
        o += number_to_string(c, 16, 2)
    return lowercase(o)
`

// mapSortRun saves every map instance of mapdemo with its maps reversed and
// returns the rows whose save is not the engine's, and how many maps were
// reversed in all.
func mapSortRun(t *testing.T, sabotage func(string) string) ([]string, int) {
	t.Helper()
	units, _ := readManifest(t)
	paths, err := compiler.GatherPaths(prefix(units["mapdemo"]))
	if err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, paths)
	if err != nil {
		t.Fatalf("mapdemo is refused: %v", err)
	}
	if sabotage != nil {
		p := filepath.Join(g.dir, "schema_save.lobster")
		src, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(sabotage(string(src))), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// every mapdemo instance the engine saves: a wire whose value the writers
	// refuse (blob_shared, §2.5) has no save to hold the sort to
	var rows []loadRow
	wires := map[string][]byte{}
	want := map[string]string{}
	for _, r := range loadRows(t) {
		if r.kind != "instance" || r.unit != "mapdemo" {
			continue
		}
		wire, err := os.ReadFile(filepath.Join(root, r.wire))
		if err != nil {
			t.Fatal(err)
		}
		saved, err := goSave(g, r.root, wire)
		if err != nil {
			continue
		}
		rows = append(rows, r)
		wires[r.name] = wire
		want[r.name] = hex.EncodeToString(saved)
	}
	if len(rows) == 0 {
		t.Fatal("the manifest names no mapdemo instance: the gate is vacuous")
	}
	var b strings.Builder
	for _, m := range g.mods {
		fmt.Fprintf(&b, "import .%s\n", m)
	}
	b.WriteString("import .load\nimport .schema_save\n")
	b.WriteString(mapSortHelpers)
	inputs := map[string][]byte{}
	for i, r := range rows {
		inputs[fmt.Sprintf("w%d", i)] = wires[r.name]
		fmt.Fprintf(&b, "let d%d = read_file(\"w%d\")\nassert d%d\n", i, i, i)
		fmt.Fprintf(&b, "let r%d = schema_rt.load_tolerant(%q, d%d, schema_rt.LoadReport {})\nassert r%d\n", i, r.root, i, i)
		fmt.Fprintf(&b, "let n%d = reverse_maps(r%d)\n", i, i)
		fmt.Fprintf(&b, "print(%q + \" {n%d} \" + hx(schema_rt.save(r%d)))\n", r.name, i, i)
	}
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	var bad []string
	reversed, seen := 0, 0
	for line := range strings.SplitSeq(strings.TrimSpace(string(lobsterrun.Run(t, g.dir, "main.lobster", inputs))), "\n") {
		f := strings.Fields(line)
		if len(f) != 3 {
			t.Fatalf("not a save answer: %q", line)
		}
		seen++
		var n int
		fmt.Sscanf(f[1], "%d", &n)
		reversed += n
		if f[2] != want[f[0]] {
			bad = append(bad, f[0])
		}
	}
	if seen != len(rows) {
		t.Fatalf("%d answers for %d rows", seen, len(rows))
	}
	return bad, reversed
}

// TestLobsterMapSaveSortsTheEntries holds the writer's sort.
func TestLobsterMapSaveSortsTheEntries(t *testing.T) {
	t.Parallel()
	bad, reversed := mapSortRun(t, nil)
	if reversed == 0 {
		t.Fatal("no map with two or more entries was reversed: the gate is vacuous")
	}
	if len(bad) > 0 {
		t.Fatalf("saves of reversed maps that are not the engine's: %v", bad)
	}
	t.Logf("%d maps reversed across mapdemo's instances; every save is the engine's", reversed)
}

// TestLobsterMapSaveSortNegativeControl takes the sort out of sg_map_order (an
// entry never moves past the one before it, so the save writes the order it is
// handed) and requires the gate red.
func TestLobsterMapSaveSortNegativeControl(t *testing.T) {
	t.Parallel()
	const cmp = "            if map_key_order(key, ka, kb) <= 0:\n"
	bad, _ := mapSortRun(t, func(src string) string {
		if strings.Count(src, cmp) != 1 {
			t.Fatal("the control's anchor, the sort's compare, is not in schema_save.lobster once")
		}
		return strings.Replace(src, cmp, "            if map_key_order(key, ka, kb) <= 0 or true:\n", 1)
	})
	if len(bad) == 0 {
		t.Fatal("NEGATIVE CONTROL FAILED: with the sort removed every save of a reversed map still matched the engine")
	}
	t.Logf("negative control: with the sort removed, %d rows went red: %v", len(bad), bad)
}
