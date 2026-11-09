package goosetable_test

// THE ID TABLE'S MAXIMUM AND ITS BOUNDED DISTINCTNESS CHECK, for Goose
// (docs/SPEC-TABLES.md §3, THE DISTINCTNESS CHECK IS BOUNDED WORK;
// docs/LAYERS/1-framing.md F6):
//
//	TestGooseIdTableBound  the pinned vectors id_table_4096 and id_table_4097
//	    (testdata/wire/tables/fuzz-vectors/INDEX.txt), on a variable root and
//	    a value-only root, through load_table (the wire fuzz's plain and
//	    builder arms) and, on the variable root, load_table_retain (its
//	    builder+retain arm; a value-only root carries no retention): 4096 entries resolve with nothing counted, 4097 are malformed
//	    and the strict read names wire_damaged, as Go's tableOpen answers
//	    TableOpenDamaged; a repeat at the bound is refused the same way
//	TestGooseIdTableBoundNegativeControl  the bound one wider: the 4097 rows
//	    resolve and go red, and only they
//	TestGooseIdDistinctnessWork  the distinctness check's comparisons, counted
//	    in an instrumented build over 4096 entries in shapes chosen against a
//	    sort and against a probe, stay inside an n log n budget, and every
//	    shape with one repeat is refused
//	TestGooseIdDistinctnessWorkNegativeControl  the pairwise scan put back for
//	    every size: the budget goes red

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goosetable"
)

// idBoundRoots are the vectors' two roots: their unit, root and file suffix.
var idBoundRoots = [][3]string{
	{"graphdemo", "Scene", ""},
	{"tblr1", "Cfg", "_value"},
}

const idBoundProbe = `import os;
import .conformance_unit;
import .wire_load;
import .wire_retain;

fn probe(name: const u8[:], root: const u8[:]) {
    var data: u8[>..] = [];
    if !read_file(str(name, ".bin"), data) { exit(1); }
    var vals: schema_rt::LoadVal[>..] = [];
    var nodes: schema_rt::LoadNode[>..] = [];
    var rep = schema_rt::LoadReport { .. };
    let plain = schema_rt::load_table(conformance_unit(), root, data, vals, nodes, rep);
    print(name, " plain ", (if plain >= 0 { 1 } else { 0 }), " ", (if rep.body_damaged { 1 } else { 0 }), " ",
        rep.unknown, " ", schema_rt::load_refusal_name(schema_rt::load_error(rep, plain)), "_");
    var rv: schema_rt::LoadVal[>..] = [];
    var rn: schema_rt::LoadNode[>..] = [];
    var rr = schema_rt::LoadReport { .. };
    var bytes: u8[>..<] = [];
    var recs: schema_rt::RetainRec[>..<] = [];
    var ids: u64[>..<] = [];
    var store = schema_rt::RetainStore { capacity: -1, id_capacity: -1, used: 0, bytes: bytes, recs: recs, ids: ids };
    let retained = schema_rt::load_table_retain(conformance_unit(), root, data, rv, rn, rr, store);
    print(name, " retain ", (if retained >= 0 { 1 } else { 0 }), " ", (if rr.body_damaged { 1 } else { 0 }), " ",
        rr.unknown, " ", schema_rt::load_refusal_name(schema_rt::load_error(rr, retained)), "_");
}
`

// idBoundRepeat is id_table_4096 with its last entry made its first: a
// repeat at the bound, which the sorted path must refuse.
func idBoundRepeat(t *testing.T, at []byte) []byte {
	t.Helper()
	b := slices.Clone(at)
	n := len(b) - 8
	copy(b[n-8:n], b[2:10])
	return b
}

// idBoundAnswers runs the probe over both roots and returns one line per load:
// "<unit>.<name> <path> <root returned> <malformed> <unknown> <strict reason>".
func idBoundAnswers(t *testing.T, override map[string][]byte) map[string]string {
	t.Helper()
	c := readWireCorpus(t)
	got := map[string]string{}
	for _, r := range idBoundRoots {
		files := wireUnitFiles(t, r[0], c.units[r[0]], override)
		var main strings.Builder
		main.WriteString(idBoundProbe)
		main.WriteString("fn main() {\n")
		for _, n := range []string{"4096", "4097"} {
			name := "id_table_" + n + r[2]
			b, err := os.ReadFile(filepath.Join(root, "testdata/wire/tables/fuzz-vectors", name+".bin"))
			if err != nil {
				t.Fatal(err)
			}
			files[name+".bin"] = b
			fmt.Fprintf(&main, "    probe(%q, %q);\n", name, r[1])
			if n == "4096" {
				files["id_repeat_4096"+r[2]+".bin"] = idBoundRepeat(t, b)
				fmt.Fprintf(&main, "    probe(%q, %q);\n", "id_repeat_4096"+r[2], r[1])
			}
		}
		main.WriteString("}\n")
		files["main.goose"] = []byte(main.String())
		out := string(gooserun.Run(t, t.TempDir(), "main.goose", files))
		for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
			f := strings.Fields(line)
			if len(f) != 6 {
				t.Fatalf("%s: the probe printed %q", r[0], line)
			}
			got[f[0]+" "+f[1]] = strings.Join(f[2:5], " ") + " " + strings.TrimSuffix(f[5], "_")
		}
	}
	return got
}

// idBoundWant is the reference's answer per vector: the 4096-entry table
// resolves and counts nothing (no body names an entry); 4097 entries and a
// repeat are the whole wire malformed, which the strict read names
// wire_damaged (gotable's fixed_dup_ids pin is the same verdict).
func idBoundWant(name string) string {
	if strings.HasPrefix(name, "id_table_4096") {
		return "1 0 0 "
	}
	return "0 1 0 wire_damaged"
}

func idBoundRed(t *testing.T, got map[string]string) []string {
	t.Helper()
	var red []string
	for _, r := range idBoundRoots {
		for _, name := range []string{"id_table_4096" + r[2], "id_repeat_4096" + r[2], "id_table_4097" + r[2]} {
			for _, path := range []string{"plain", "retain"} {
				if path == "retain" && r[2] == "_value" {
					continue // a value-only root carries no retention: load_table_retain refuses it (§6.6)
				}
				key := name + " " + path
				a, ok := got[key]
				if !ok {
					t.Fatalf("%s: no probe answer", key)
				}
				if want := strings.TrimSpace(idBoundWant(name)); strings.TrimSpace(a) != want {
					red = append(red, fmt.Sprintf("%s: got %q, want %q", key, a, want))
				}
			}
		}
	}
	return red
}

func TestGooseIdTableBound(t *testing.T) {
	t.Parallel()
	for _, r := range idBoundRed(t, idBoundAnswers(t, nil)) {
		t.Error(r)
	}
}

func TestGooseIdTableBoundNegativeControl(t *testing.T) {
	t.Parallel()
	// The bound one wider: the same bytes then resolve, so it is the bound
	// that refuses them, and not the buffer or the entries.
	red := idBoundRed(t, idBoundAnswers(t, sabotage(t, "wire_load.goose", "let IdTableBound: i64 = 4096;\n", "let IdTableBound: i64 = 4097;\n")))
	var names []string
	for _, r := range red {
		name, _, _ := strings.Cut(r, ":")
		names = append(names, name)
	}
	want := []string{"id_table_4097 plain", "id_table_4097 retain", "id_table_4097_value plain"}
	if !slices.Equal(names, want) {
		t.Fatalf("without the bound, red rows %v, want exactly %v", names, want)
	}
}

// THE COMPLEXITY GUARD: the comparisons are counted, never timed.
// idWorkInstrument rewrites the four comparison sites of ids_distinct and
// ids_sift to count into a global the probe reads; each anchor must occur
// exactly once.
var idWorkInstrument = [][2]string{
	{"                if le_at(file, first + j * 8, 8) == a { return false; }\n",
		"                if id_probe_eq(le_at(file, first + j * 8, 8), a) { return false; }\n"},
	{"        if c + 1 < n && ids[c] < ids[c + 1] { c += 1; }\n", "        if c + 1 < n && id_probe_less(ids[c], ids[c + 1]) { c += 1; }\n"},
	{"        if !(ids[i] < ids[c]) { return; }\n", "        if !id_probe_less(ids[i], ids[c]) { return; }\n"},
	{"        if ids[i] == ids[i + 1] { return false; }\n", "        if id_probe_eq(ids[i], ids[i + 1]) { return false; }\n"},
}

const idWorkHelpers = `
var id_probe_comparisons: i64 = 0;
fn id_probe_eq(a: u64, b: u64) -> bool { id_probe_comparisons += 1; return a == b; }
fn id_probe_less(a: u64, b: u64) -> bool { id_probe_comparisons += 1; return a < b; }
`

const idWorkProbe = `import os;
import .wire_load;

fn probe(name: const u8[:]) {
    var data: u8[>..] = [];
    if !read_file(str(name, ".bin"), data) { exit(1); }
    let n = (data.len / 8) as! i64;
    schema_rt::id_probe_comparisons = 0;
    let distinct = schema_rt::ids_distinct(data, 0, n);
    print(name, " ", n, " ", (if distinct { 1 } else { 0 }), " ", schema_rt::id_probe_comparisons);
}
`

// idWorkShapes are 4096-entry tables in shapes that cost a naive sort or a
// hash probe the most: in order, reversed, an organ pipe, spread, and every entry
// on one slot of the reference's open-addressing probe (C's table_writer_id
// mix, masked to 8192 slots).
func idWorkShapes() map[string][]uint64 {
	const n = 4096
	shapes := map[string][]uint64{}
	asc, desc, pipe, spread := make([]uint64, n), make([]uint64, n), make([]uint64, n), make([]uint64, n)
	for i := range n {
		asc[i] = uint64(i + 1)
		desc[i] = uint64(n - i)
		pipe[i] = uint64(min(i, n-1-i)*2 + i%2 + 1)
		spread[i] = uint64(i+1) * 0x9E3779B97F4A7C15
	}
	shapes["ascending"], shapes["descending"], shapes["organ_pipe"], shapes["spread"] = asc, desc, pipe, spread
	var slot []uint64
	for id := uint64(1); len(slot) < n; id++ {
		h := id
		h ^= h >> 33
		h *= 0xff51afd7ed558ccd
		h ^= h >> 33
		if uint32(h)&8191 == 0 {
			slot = append(slot, id)
		}
	}
	shapes["one_probe_slot"] = slot
	return shapes
}

// idWorkBudget is 4 n log2 n at n = 4096, twice heapsort's worst case; the
// pairwise scan it replaces makes n(n-1)/2 = 8,386,560.
const idWorkBudget = 4 * 4096 * 12

func idWorkAnswers(t *testing.T, threshold string) map[string][2]int {
	t.Helper()
	src := string(goosetable.Runtime()["wire_load.goose"])
	for _, w := range idWorkInstrument {
		if strings.Count(src, w[0]) != 1 {
			t.Fatalf("instrument: wire_load.goose holds %q %d times, want once", w[0], strings.Count(src, w[0]))
		}
		src = strings.Replace(src, w[0], w[1], 1)
	}
	if threshold != "" {
		const anchor = "    if n < 8 {\n"
		if strings.Count(src, anchor) != 1 {
			t.Fatalf("instrument: the pairwise threshold is not there once")
		}
		src = strings.Replace(src, anchor, "    if n < "+threshold+" {\n", 1)
	}
	src += idWorkHelpers
	files := probeFiles(t, probeUnit(t, reasonSchemas["fixed"]), map[string][]byte{"wire_load.goose": []byte(src)})
	var main strings.Builder
	main.WriteString(idWorkProbe)
	main.WriteString("fn main() {\n")
	for name, ids := range idWorkShapes() {
		for _, repeat := range []bool{false, true} {
			v := slices.Clone(ids)
			file := name
			if repeat {
				v[len(v)/2] = v[len(v)/3]
				file += "_repeat"
			}
			var b []byte
			for _, id := range v {
				b = binary.LittleEndian.AppendUint64(b, id)
			}
			files[file+".bin"] = b
			fmt.Fprintf(&main, "    probe(%q);\n", file)
		}
	}
	main.WriteString("}\n")
	files["main.goose"] = []byte(main.String())
	got := map[string][2]int{}
	for name, f := range probeLines(gooserun.Run(t, t.TempDir(), "main.goose", files)) {
		if len(f) != 3 || f[0] != "4096" {
			t.Fatalf("%s: the probe printed %q", name, f)
		}
		d, _ := strconv.Atoi(f[1])
		cmp, err := strconv.Atoi(f[2])
		if err != nil {
			t.Fatalf("%s: the probe printed %q", name, f)
		}
		got[name] = [2]int{d, cmp}
	}
	return got
}

func TestGooseIdDistinctnessWork(t *testing.T) {
	t.Parallel()
	got := idWorkAnswers(t, "")
	if len(got) != 2*len(idWorkShapes()) {
		t.Fatalf("got %d answers, want %d", len(got), 2*len(idWorkShapes()))
	}
	for name, a := range got {
		want := 1
		if strings.HasSuffix(name, "_repeat") {
			want = 0
		}
		if a[0] != want {
			t.Errorf("%s: distinct %d, want %d", name, a[0], want)
		}
		if a[1] > idWorkBudget {
			t.Errorf("%s: %d comparisons over 4096 entries, past the %d budget", name, a[1], idWorkBudget)
		}
		t.Logf("%s: %d comparisons", name, a[1])
	}
}

func TestGooseIdDistinctnessWorkNegativeControl(t *testing.T) {
	t.Parallel()
	for name, a := range idWorkAnswers(t, "4097") {
		if strings.HasSuffix(name, "_repeat") {
			continue // the pairwise scan stops at the repeat
		}
		if a[1] != 4096*4095/2 {
			t.Errorf("%s: the pairwise scan made %d comparisons, want %d", name, a[1], 4096*4095/2)
		}
		if a[1] <= idWorkBudget {
			t.Errorf("%s: the pairwise scan's %d comparisons fit the budget, so the guard cannot see it", name, a[1])
		}
	}
}
