package goosetable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goosetable"
)

// Damage discards the whole read (§4), including the retaining path. Keep the
// already-pinned node damage vectors and hold their verdict, not partial values.
func TestGooseDamagedLoadReturnsNoRoot(t *testing.T) {
	t.Parallel()
	got := gooseDamageProbe(t, nil)
	lines := strings.Split(strings.TrimSpace(got), "\n")
	if len(lines) != 12 {
		t.Fatalf("got %d probe rows, want 12: %s", len(lines), got)
	}
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) != 4 {
			t.Fatalf("unexpected probe output: %q", line)
		}
		want := "0 1"
		if fields[0] == "node_clean" || fields[0] == "node_index_null" {
			want = "1 0"
		}
		if strings.Join(fields[2:], " ") != want {
			t.Errorf("%s: got %s, want %s", strings.Join(fields[:2], " "), strings.Join(fields[2:], " "), want)
		}
	}
}

func TestGooseDamagedLoadNegativeControl(t *testing.T) {
	t.Parallel()
	baseline := gooseDamageProbe(t, nil)
	if !strings.Contains(baseline, "node_count_disagrees plain 0 1") || !strings.Contains(baseline, "node_count_disagrees retain 0 1") {
		t.Fatalf("control requires a clean refusing baseline:\n%s", baseline)
	}
	src := string(goosetable.Runtime()["wire_load.goose"])
	const anchor = "    if report.body_damaged { return -1; }\n"
	if strings.Count(src, anchor) != 1 {
		t.Fatal("discard control anchor must occur exactly once")
	}
	changed := strings.Replace(src, anchor, "", 1)
	got := gooseDamageProbe(t, map[string][]byte{"wire_load.goose": []byte(changed)})
	if !strings.Contains(got, "node_count_disagrees plain 1 1") || !strings.Contains(got, "node_count_disagrees retain 1 1") {
		t.Fatalf("removing the real runtime discard did not expose the partial roots:\n%s", got)
	}
}

// gooseLoadProbe loads `<name>.bin` as a ListNode through the plain and the
// retaining load and prints whether each returned a root and reported damage.
const gooseLoadProbe = `import os;
import .conformance_unit;
import .wire_load;
import .wire_retain;
fn probe(name: const u8[:]) {
    var data: u8[>..] = [];
    if !read_file(str(name, ".bin"), data) { exit(1); }
    var vals: schema_rt::LoadVal[>..] = [];
    var nodes: schema_rt::LoadNode[>..] = [];
    var rep = schema_rt::LoadReport { .. };
    let plain = schema_rt::load_table(conformance_unit(), "ListNode", data, vals, nodes, rep);
    print(name, " plain ", (if plain >= 0 { 1 } else { 0 }), " ", (if rep.body_damaged { 1 } else { 0 }));
    var rv: schema_rt::LoadVal[>..] = [];
    var rn: schema_rt::LoadNode[>..] = [];
    var rr = schema_rt::LoadReport { .. };
    var bytes: u8[>..<] = [];
    var recs: schema_rt::RetainRec[>..<] = [];
    var ids: u64[>..<] = [];
    var store = schema_rt::RetainStore { capacity: -1, id_capacity: -1, used: 0, bytes: bytes, recs: recs, ids: ids };
    let retained = schema_rt::load_table_retain(conformance_unit(), "ListNode", data, rv, rn, rr, store);
    print(name, " retain ", (if retained >= 0 { 1 } else { 0 }), " ", (if rr.body_damaged { 1 } else { 0 }));
}
`

func gooseDamageProbe(t *testing.T, override map[string][]byte) string {
	t.Helper()
	c := readWireCorpus(t)
	files := wireUnitFiles(t, "graphdemo", c.units["graphdemo"], override)
	names := []string{"node_clean", "node_index_null", "node_count_disagrees", "node_record_L_past_field", "node_index_above_count", "node_reserved_id_in_record"}
	var main strings.Builder
	main.WriteString(gooseLoadProbe)
	main.WriteString("fn main() {\n")
	for _, name := range names {
		b, err := os.ReadFile(filepath.Join(root, "testdata/wire/tables/framing", name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		files[name+".bin"] = b
		fmt.Fprintf(&main, "    probe(%q);\n", name)
	}
	main.WriteString("}\n")
	files["main.goose"] = []byte(main.String())
	return string(gooserun.Run(t, t.TempDir(), "main.goose", files))
}

// gooseCycleVectors are the pinned pointer-cycle vectors (fuzz-vectors/INDEX.txt):
// the unit, the root and the vector's name.
var gooseCycleVectors = [][3]string{
	{"streamdemo", "Feed", "stream_chain_link_cycle"},
	{"blobdemo", "Catalog", "blob_catalog_cycle"},
	{"graphdemo", "Scene", "graph_tree_cycle"},
}

// A CYCLIC WIRE IS DAMAGE (docs/SPEC-TABLES.md §3.1, §4): the plain and the
// retaining load each discard the whole read as malformed.
func TestGooseCyclicWireIsDamage(t *testing.T) {
	t.Parallel()
	for _, v := range gooseCycleVectors {
		got := gooseCycleProbe(t, v, nil)
		want := v[2] + " plain 0 1\n" + v[2] + " retain 0 1"
		if strings.TrimSpace(got) != want {
			t.Errorf("%s.%s: got\n%s\nwant\n%s", v[0], v[1], got, want)
		}
	}
}

// The control takes the cycle check out of the emitted runtime and requires
// every cycle vector to load.
func TestGooseCyclicWireNegativeControl(t *testing.T) {
	t.Parallel()
	src := string(goosetable.Runtime()["wire_load.goose"])
	const anchor = " && !acyclic(c) { c.report.body_damaged = true; }"
	if strings.Count(src, anchor) != 1 {
		t.Fatal("cycle control anchor must occur exactly once")
	}
	changed := strings.Replace(src, anchor, " && false { c.report.body_damaged = true; }", 1)
	for _, v := range gooseCycleVectors {
		got := gooseCycleProbe(t, v, map[string][]byte{"wire_load.goose": []byte(changed)})
		want := v[2] + " plain 1 0\n" + v[2] + " retain 1 0"
		if strings.TrimSpace(got) != want {
			t.Errorf("%s.%s: removing the cycle check did not let the cyclic wire load:\n%s", v[0], v[1], got)
		}
	}
}

func gooseCycleProbe(t *testing.T, v [3]string, override map[string][]byte) string {
	t.Helper()
	c := readWireCorpus(t)
	files := wireUnitFiles(t, v[0], c.units[v[0]], override)
	b, err := os.ReadFile(filepath.Join(root, "testdata/wire/tables/fuzz-vectors", v[2]+".bin"))
	if err != nil {
		t.Fatal(err)
	}
	files[v[2]+".bin"] = b
	program := strings.NewReplacer("ListNode", v[1]).Replace(gooseLoadProbe)
	files["main.goose"] = []byte(program + fmt.Sprintf("fn main() {\n    probe(%q);\n}\n", v[2]))
	return string(gooserun.Run(t, t.TempDir(), "main.goose", files))
}
