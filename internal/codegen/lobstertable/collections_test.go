package lobstertable_test

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// collectionCounts is one unit's census of the collections constructs this
// card carries: maps (docs/SPEC-TABLES.md §2.8), byte buffers (§2.5) and
// unbounded arrays (§2.9).
type collectionCounts struct {
	maps, blobs, lists int
}

func (c collectionCounts) total() int { return c.maps + c.blobs + c.lists }

// countCollections walks a unit's declarations and generated map-entry tables
// and counts every field of the three constructs, once per declaration.
func countCollections(u *ir.Unit) collectionCounts {
	var c collectionCounts
	seen := map[string]bool{}
	var visitStruct func(*ir.Struct)
	var visitUnion func(*ir.Union)
	visitField := func(f *ir.Field) {
		if f.IsMap() {
			c.maps++
			if f.MapEntry != nil {
				visitStruct(f.MapEntry)
			}
		} else if f.Array == ir.ArrayList {
			c.lists++
		}
		if f.Type.Pointer && f.Type.Blob() {
			c.blobs++
		}
		if f.Type.Kind == ir.TNamed {
			switch r := f.Type.Ref.(type) {
			case *ir.Struct:
				visitStruct(r)
			case *ir.Union:
				visitUnion(r)
			}
		}
	}
	visitUnion = func(un *ir.Union) {
		if seen["u:"+un.Name] {
			return
		}
		seen["u:"+un.Name] = true
		for _, v := range un.Variants {
			if v.F != nil {
				visitField(v.F)
			}
		}
	}
	visitStruct = func(st *ir.Struct) {
		if seen["s:"+st.Name] {
			return
		}
		seen["s:"+st.Name] = true
		for _, f := range st.Fields {
			visitField(f)
		}
	}
	for _, f := range u.Files {
		for _, t := range f.Tables {
			visitStruct(t)
		}
		for _, un := range f.TableUnions {
			visitUnion(un)
		}
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ir.Struct:
				visitStruct(d)
			case *ir.Union:
				visitUnion(d)
			}
		}
	}
	return c
}

// collectionsLoadProgram is loadProgram with collections.lobster imported, so
// the group's own runtime file joins the load walker's per-kind hook
// (docs/PORT-LOBSTER.md, THE LOAD).
func collectionsLoadProgram(g *loadUnit, roots []string) string {
	return strings.Replace(loadProgram(g, roots), "import .load\n", "import .load\nimport .collections\n", 1)
}

// runCollectionsLoad runs the load half of one unit through the collections
// hook and reads back each row's report line and value dump, as runUnit does.
func runCollectionsLoad(t *testing.T, g *loadUnit, rows []loadRow) (lob, goLine, goDump []string) {
	t.Helper()
	var roots []string
	inputs := map[string][]byte{}
	for i, row := range rows {
		wire := row.data
		if wire == nil {
			var err error
			if wire, err = os.ReadFile(filepath.Join(root, row.wire)); err != nil {
				t.Fatalf("%s: %v", row.name, err)
			}
		}
		inputs[fmt.Sprintf("w%d", i)] = wire
		roots = append(roots, row.root)
		line, dump := goLoad(g.u, row.root, wire)
		goLine = append(goLine, line)
		goDump = append(goDump, dump)
	}
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(collectionsLoadProgram(g, roots)), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.Split(strings.TrimRight(string(lobsterrun.Run(t, g.dir, "main.lobster", inputs)), "\n"), "\n")
	for range rows {
		var n int
		if len(out) < 2 {
			t.Fatalf("unit %s: the program's output ends early", rows[0].unit)
		}
		if _, err := fmt.Sscanf(out[1], "%d", &n); err != nil || len(out) < 2+n {
			t.Fatalf("unit %s: the program's output is not report, count, dump: %q", rows[0].unit, out[1])
		}
		lob = append(lob, out[0], strings.Join(out[2:2+n], ""))
		out = out[2+n:]
	}
	if len(out) != 0 {
		t.Fatalf("unit %s: %d lines past the last row", rows[0].unit, len(out))
	}
	return lob, goLine, goDump
}

// largestBlob is the largest byte buffer Go's decode of the wire keeps: the
// M17 measurement (one contiguous blob at whatever length the caller asked
// for, docs/PORTING.md M17).
func largestBlob(t *testing.T, g *loadUnit, rootName string, wire []byte) int {
	t.Helper()
	m := tabletext.NewModel(g.u)
	inst := m.New(m.Lookup(rootName))
	var rep tabletext.Report
	if _, err := tablewire.Decode(m, inst, wire, &rep); err != nil {
		t.Fatalf("%s: the reference refuses the wire: %v", rootName, err)
	}
	seen := map[*tabletext.Instance]bool{}
	largest := 0
	var visitInstance func(*tabletext.Instance)
	var visitCell func(tabletext.Cell)
	visitCell = func(c tabletext.Cell) {
		if c.Blob != nil && len(c.Blob.Data) > largest {
			largest = len(c.Blob.Data)
		}
		if c.Tab != nil {
			visitInstance(c.Tab)
		}
		if c.Node != nil {
			visitInstance(c.Node)
		}
		if c.Arm != nil {
			for i := range c.Arm.Elems {
				visitCell(c.Arm.Elems[i])
			}
			visitCell(c.Arm.Cell)
		}
	}
	visitInstance = func(in *tabletext.Instance) {
		if in == nil || seen[in] {
			return
		}
		seen[in] = true
		for i := range in.Fields {
			fv := &in.Fields[i]
			visitCell(fv.Cell)
			for j := range fv.Elems {
				visitCell(fv.Elems[j])
			}
			for j := range fv.Entries {
				visitCell(fv.Entries[j])
			}
		}
	}
	visitInstance(inst)
	return largest
}

// TestLobsterCollections generates every tables-manifest unit that uses a map,
// a byte buffer or an unbounded array, loads every row of those units through
// runtime/collections.lobster joining runtime/load.lobster's per-kind hook,
// saves and re-loads every instance, and holds each answer to the Go
// reference: report for report, byte for byte, dump for dump
// (docs/SPEC-TABLES.md §2.5, §2.8, §2.9; docs/PORT-LOBSTER.md, THE CARD MAP).
// Each construct is named with its count, and the M17 gate is the blob past a
// 64 KiB slab (blob_large), which must ride whole and intact through both
// walks.
func TestLobsterCollections(t *testing.T) {
	t.Parallel()
	units, _ := readManifest(t)
	expect := reportLines(t)
	byUnit := map[string][]loadRow{}
	for _, row := range loadRows(t) {
		if _, ok := units[row.unit]; !ok {
			t.Fatalf("row %s names unit %s, which the manifest does not declare", row.name, row.unit)
		}
		byUnit[row.unit] = append(byUnit[row.unit], row)
	}
	var order []string
	for key := range units {
		order = append(order, key)
	}
	sort.Strings(order)
	var total collectionCounts
	selected, reports, instances, values, saved := 0, 0, 0, 0, 0
	skipped, myBlob, m17 := 0, 0, false
	for _, key := range order {
		paths, err := compiler.GatherPaths(prefix(units[key]))
		if err != nil {
			t.Fatalf("unit %s: %v", key, err)
		}
		g, err := generateLoadUnit(t, paths)
		if err != nil {
			t.Fatalf("unit %s is refused by the collections surface: %v", key, err)
		}
		c := countCollections(g.u)
		if c.total() == 0 {
			continue
		}
		selected++
		total.maps += c.maps
		total.blobs += c.blobs
		total.lists += c.lists
		rows := byUnit[key]
		if len(rows) == 0 {
			// generated as the manifest declares it, but every row it names
			// belongs to another surface (retain, cook, ...) and not to the
			// load/save pair this card holds.
			t.Logf("unit %s uses maps=%d blobs=%d unbounded-arrays=%d; generated, no load row in the manifest", key, c.maps, c.blobs, c.lists)
			continue
		}

		lob, goLine, goDump := runCollectionsLoad(t, g, rows)
		var instRows []loadRow
		for i, row := range rows {
			line, dump := lob[2*i], lob[2*i+1]
			if row.kind == "report" {
				if want, ok := expect[row.name]; ok && line != want {
					t.Errorf("report %s (%s):\nlobster: %s\nwant:    %s", row.name, key, line, want)
				}
				reports++
			} else {
				instRows = append(instRows, row)
				instances++
			}
			if line != goLine[i] {
				t.Errorf("%s %s: the report is not the reference's:\nlobster: %s\ngo:      %s", row.kind, row.name, line, goLine[i])
				continue
			}
			if dump != goDump[i] {
				t.Errorf("%s %s: the loaded value is not the reference's at byte %s", row.kind, row.name, divergence(dump, goDump[i]))
				continue
			}
			values++
		}
		if len(instRows) == 0 {
			continue
		}
		// the save walk reads the same descriptors: every instance is saved
		// byte for byte and re-loaded to the reference's value.
		var want [][]byte
		var kept []loadRow
		for _, row := range instRows {
			wire := row.data
			if wire == nil {
				if wire, err = os.ReadFile(filepath.Join(root, row.wire)); err != nil {
					t.Fatalf("%s: %v", row.name, err)
				}
			}
			out, goErr := goSave(g, row.root, wire)
			if goErr != nil {
				t.Logf("instance %s is refused by the Go writer and not compared: %v", row.name, goErr)
				skipped++
				continue
			}
			kept = append(kept, row)
			want = append(want, out)
			if row.name == "blob_large" {
				myBlob = largestBlob(t, g, row.root, wire)
			}
		}
		if len(kept) == 0 {
			continue
		}
		res := runSaveRows(t, g, kept)
		for i, row := range kept {
			wantHex := hex.EncodeToString(want[i])
			if res.saved[i] != wantHex {
				t.Errorf("instance %s: the saved bytes are not the reference's:\nlobster: %s\ngo:      %s", row.name, res.saved[i], wantHex)
				continue
			}
			_, goReload := goLoad(g.u, row.root, want[i])
			if res.dump[i] != goReload {
				t.Errorf("instance %s: the re-loaded value is not the reference's at byte %s", row.name, divergence(res.dump[i], goReload))
				continue
			}
			saved++
			if row.name == "blob_large" {
				if myBlob <= 64*1024 {
					t.Errorf("M17: blob_large's largest blob is %d bytes, not the past-a-slab buffer the row exists for", myBlob)
				} else {
					m17 = true
					t.Logf("M17 blob_large: a %d-byte byte buffer (past a 64 KiB slab) loaded and saved whole, one node, equal to Go", myBlob)
				}
			}
		}
	}
	if selected == 0 || total.total() == 0 {
		t.Fatalf("the census found %d units and %d collections constructs: the comparison is vacuous", selected, total.total())
	}
	if !m17 {
		t.Fatalf("M17 was not exercised: no blob_large instance was compared")
	}
	t.Logf("maps=%d blobs=%d unbounded-arrays=%d over %d units; %d report rows and %d instance rows loaded, %d values equal the reference's; %d instances saved byte for byte and re-loaded; %d skipped by name",
		total.maps, total.blobs, total.lists, selected, reports, instances, values, saved, skipped)
}

// TestLobsterCollectionsNegativeControl breaks one construct — the map
// header's collection kind in the emitted load runtime (docs/SPEC-TABLES.md
// §3, §2.8), matched exactly once — and requires the report comparison to go
// red on map_key_widen, with the unpatched control green against Go.
func TestLobsterCollectionsNegativeControl(t *testing.T) {
	t.Parallel()
	units, _ := readManifest(t)
	var row loadRow
	found := false
	for _, r := range loadRows(t) {
		if r.name == "map_key_widen" {
			row, found = r, true
			break
		}
	}
	if !found {
		t.Fatalf("the manifest names no row map_key_widen")
	}
	paths, err := compiler.GatherPaths(prefix(units[row.unit]))
	if err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, paths)
	if err != nil {
		t.Fatal(err)
	}
	control, goLine, _ := runCollectionsLoad(t, g, []loadRow{row})
	if control[0] != goLine[0] {
		t.Fatalf("the control, unpatched: got %s, want %s", control[0], goLine[0])
	}
	path := filepath.Join(g.dir, "load.lobster")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	const anchor = "            if ek != 13:\n                rep.kind_mismatch++\n"
	if n := strings.Count(string(src), anchor); n != 1 {
		t.Fatalf("the map header-kind anchor is in the emitted runtime %d times, want 1", n)
	}
	patched := strings.Replace(string(src), anchor, "            if ek != 12:\n                rep.kind_mismatch++\n", 1)
	if err := os.WriteFile(path, []byte(patched), 0o644); err != nil {
		t.Fatal(err)
	}
	got, _, _ := runCollectionsLoad(t, g, []loadRow{row})
	if got[0] == goLine[0] {
		t.Fatalf("the patched runtime still matches the reference: %s", got[0])
	}
	t.Logf("the map comparison went red under the patch: map_key_widen got %s, reference %s", got[0], goLine[0])
}
