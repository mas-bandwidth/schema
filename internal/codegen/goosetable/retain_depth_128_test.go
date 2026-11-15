package goosetable_test

// RETAIN-UNKNOWN'S NESTING BOUND (docs/SPEC-TABLES.md §4, ONE NESTING BOUND;
// §6.6, retain_walk_depth): the resolving walk permits 128 framed levels, the
// same bound the plain read holds, and §4 counts a union whose arm is a table
// as two levels. A depth is not a retention capacity: the retaining load of a
// legal file whose unknown field nests 128 levels keeps that field, and the
// retaining save writes it back byte for byte. The depth 129 and deeper
// vectors are refused by the retaining load as the plain read refuses them.

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
)

// gooseDepthReads are the held depth vectors the binary bound accepts.
var gooseDepthReads = []string{
	"depth_tables_128",
	"depth_arrays_128",
	"depth_union_tables_64",
	"depth_keyed_128",
}

// gooseDepthRefused are the held depth vectors past §4's binary bound.
var gooseDepthRefused = []string{
	"depth_tables_129",
	"depth_arrays_129",
	"depth_union_tables_65",
	"depth_keyed_129",
	"depth_tables_5000",
}

// gooseDepthProbe retains one depth vector over graphdemo.Scene, saves it back
// and prints the load's counters, the save's refusal and the saved bytes.
const gooseDepthProbe = `
fn depth_case(name: const u8[:]) {
    let data = input(str(name, ".bin"));
    var vals: schema_rt::LoadVal[>..] = [];
    var nodes: schema_rt::LoadNode[>..] = [];
    var report = schema_rt::LoadReport { .. };
    var rbytes: u8[>..<] = [];
    var rrecs: schema_rt::RetainRec[>..<] = [];
    var rids: u64[>..<] = [];
    var store = schema_rt::RetainStore { capacity: -1, id_capacity: -1, used: 0, bytes: rbytes, recs: rrecs, ids: rids };
    let root = schema_rt::load_table_retain(conformance_unit(), "Scene", data, vals, nodes, report, store);
    var out: u8[>..<] = [];
    var refusal: i64 = 0;
    if root >= 0 {
        refusal = schema_rt::save_table_retain(conformance_unit(), "Scene", data, vals, nodes, root, store, report, out);
    }
    print(name, " ", (if root >= 0 { 1 } else { 0 }), " ", report.retained, " ", report.retain_lost, " ", refusal, " ", hex(out));
}
`

// gooseDepthResult is what the probe printed for one vector.
type gooseDepthResult struct {
	loaded   bool
	retained int64
	lost     int64
	refusal  int64
	saved    []byte
}

func TestGooseRetainKeepsDepth128(t *testing.T) {
	t.Parallel()
	c := readWireCorpus(t)
	files := wireUnitFiles(t, "graphdemo", c.units["graphdemo"], nil)
	names := append(append([]string{}, gooseDepthReads...), gooseDepthRefused...)
	for _, name := range names {
		b, err := os.ReadFile(filepath.Join(root, "testdata/wire/tables/framing", name+".bin"))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		files[name+".bin"] = b
	}
	var main strings.Builder
	main.WriteString(probePrelude)
	main.WriteString(gooseDepthProbe)
	main.WriteString("fn main() {\n")
	for _, name := range names {
		fmt.Fprintf(&main, "    depth_case(%q);\n", name)
	}
	main.WriteString("}\n")
	files["main.goose"] = []byte(main.String())
	out := gooserun.Run(t, t.TempDir(), "main.goose", files)

	got := map[string]gooseDepthResult{}
	for name, f := range probeLines(out) {
		if len(f) != 5 {
			t.Fatalf("%s: the probe printed %q", name, f)
		}
		var n [4]int64
		for i := range n {
			if _, err := fmt.Sscan(f[i], &n[i]); err != nil {
				t.Fatalf("%s: %q is not a count", name, f[i])
			}
		}
		got[name] = gooseDepthResult{loaded: n[0] == 1, retained: n[1], lost: n[2], refusal: n[3], saved: probeHex(t, f[4])}
	}

	for _, name := range gooseDepthReads {
		r, ok := got[name]
		if !ok {
			t.Fatalf("%s: the probe printed no line", name)
		}
		wire, err := os.ReadFile(filepath.Join(root, "testdata/wire/tables/framing", name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		if !r.loaded {
			t.Errorf("%s: the retaining load refused a readable depth", name)
			continue
		}
		if r.retained != 1 || r.lost != 0 {
			t.Errorf("%s: retained %d and lost %d, want 1 and 0", name, r.retained, r.lost)
		}
		if r.refusal != 0 {
			t.Errorf("%s: the retaining save refused with %d", name, r.refusal)
		}
		if !bytes.Equal(r.saved, wire) {
			t.Errorf("%s: the retaining save wrote %d bytes where the file is %d, first difference at %d",
				name, len(r.saved), len(wire), firstDifference(r.saved, wire))
		}
	}
	for _, name := range gooseDepthRefused {
		r, ok := got[name]
		if !ok {
			t.Fatalf("%s: the probe printed no line", name)
		}
		if r.loaded {
			t.Errorf("%s: the retaining load accepted a depth past the binary bound", name)
		}
	}
}
