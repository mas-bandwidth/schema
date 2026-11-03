package goosetable_test

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// The storage cap vectors (docs/SPEC-TABLES.md §6.5, §11) live in
// testdata/wire/tables/storage-cap beside Cap.schema. Big's storage is 2^27
// bytes and one element or node record of it costs two wire bytes, so 8191
// of them measure under the 2^40-byte cap and 8192 measure over it, through
// the list extent (list_*) or the node scan (nodes_*). list_term, built here,
// is 8193 elements: one extent term past the cap.
var storageCapDir = filepath.Join("..", "..", "..", "testdata", "wire", "tables", "storage-cap")

// storageCapListWire is a form-1 file whose root holds count empty Big bodies
// as its items list, the shape of list_at.bin and list_over.bin.
func storageCapListWire(u *ir.Unit, count uint64) []byte {
	items := ir.TableFieldWireId(u.Tables["Root"].Fields[0])
	list := binary.AppendUvarint([]byte{byte(ir.TableKindTable)}, count)
	for range count {
		list = append(list, 1, 0)
	}
	wire := []byte{1, 1, byte(ir.TableKindArray)}
	wire = binary.AppendUvarint(wire, uint64(len(list)))
	wire = append(wire, list...)
	wire = append(wire, 0)
	wire = binary.LittleEndian.AppendUint64(wire, items)
	return binary.LittleEndian.AppendUint64(wire, 1)
}

// storageCapSizes is the measure each at vector owes: the root's record and
// each node record rounded to the region alignment, the list extent, and 16
// bytes of attribution per record and for the root.
func storageCapSizes(u *ir.Unit) map[string]int64 {
	round := func(n, a int64) int64 { return (n + a - 1) / a * a }
	ra := ir.TableRegionAlign(u)
	root, big := ir.RecordLayout(u, u.Tables["Root"]), ir.RecordLayout(u, u.Tables["Big"])
	return map[string]int64{
		"list_at":  round(round(root.Size, ra)+round(0, big.Align)+8191*big.Size, ra) + 16,
		"nodes_at": round(root.Size, ra) + 8191*round(big.Size, ra) + 8192*16,
	}
}

// storageCapProbeMain prints, for each vector, load_measure's size and
// reason, and for the over vectors the load's root, the values it built and
// the strict read's reason (load_error). The term rows call measure_field on
// one list term of a 16-byte element at 2^40-16 (it reads, ending at the
// cap) and at 2^40-15 (refused), as the C++ reference's term test does.
const storageCapProbeMain = `
fn measure(name: const u8[:], strict: bool) {
    let data = input(str(name, ".bin"));
    let n, why = schema_rt::load_measure(conformance_unit(), "Root", data);
    var line = str(name, " ", n, " ", schema_rt::load_refusal_name(why), "_");
    if strict {
        // the strict read runs on the over vectors alone: an at vector
        // would build a terabyte
        var vals: schema_rt::LoadVal[>..] = [];
        var nodes: schema_rt::LoadNode[>..] = [];
        var report = schema_rt::LoadReport { .. };
        let root = schema_rt::load_table(conformance_unit(), "Root", data, vals, nodes, report);
        line = str(line, " ", root, " ", vals.len, " ", schema_rt::load_refusal_name(schema_rt::load_error(report, root)), "_");
    }
    write_stdout(str(line, "\n"));
}

fn term_with(name: const u8[:], from: i64, vals: schema_rt::LoadVal[>..]&, nodes: schema_rt::LoadNode[>..]&, report: schema_rt::LoadReport&, store: schema_rt::RetainStore&) {
    let list = input("term.bin");
    var c = schema_rt::LoadCtx { data: list, unit: conformance_unit(), vals: vals, nodes: nodes, report: report, ids_at: 0, ids_n: 0, numbered: false, good: false, root_tab: -1, root_val: -1, cap: false, node_n: 0, retain: store, retaining: false };
    let d = schema_rt::FieldDesc { shape: schema_rt::ShapeArr, ek: 4, array: schema_rt::ArrayList, es: 16, ea: 4, .. };
    var scan = schema_rt::MeasureScan { at: from };
    let why = schema_rt::measure_field(c, scan, d, schema_rt::Rd { off: 0, end: 6 });
    write_stdout(str(name, " ", scan.at, " ", schema_rt::load_refusal_name(why), "_\n"));
}

fn term(name: const u8[:], from: i64) {
    var vals: schema_rt::LoadVal[>..] = [];
    var nodes: schema_rt::LoadNode[>..] = [];
    var report = schema_rt::LoadReport { .. };
    var bytes: u8[>..<] = [];
    var recs: schema_rt::RetainRec[>..<] = [];
    var ids: u64[>..<] = [];
    var none = schema_rt::RetainStore { capacity: 0, id_capacity: 0, used: 0, bytes: bytes, recs: recs, ids: ids };
    term_with(name, from, vals, nodes, report, none);
}

fn main() {
    measure("list_at", false);
    measure("nodes_at", false);
    measure("list_over", true);
    measure("nodes_over", true);
    measure("list_term", true);
    term("term_at", 1099511627776 - 16);
    term("term_over", 1099511627776 - 15);
}
`

// TestGooseStorageCapRefusedAtMeasure holds the storage cap at load on the
// shared vectors: load_measure answers each at vector's exact size, under
// 2^40, and refuses each over vector storage_over_cap, as the load does
// (load_error's strict reason, the report's own) before it builds a value.
// It runs under --debug, so a signed overflow aborts it.
func TestGooseStorageCapRefusedAtMeasure(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile(filepath.Join(storageCapDir, "Cap.schema"))
	if err != nil {
		t.Fatal(err)
	}
	u := probeUnit(t, string(src))
	files := probeFiles(t, u, nil)
	for _, k := range []string{"list_at", "list_over", "nodes_at", "nodes_over"} {
		data, err := os.ReadFile(filepath.Join(storageCapDir, k+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		files[k+".bin"] = data
	}
	if string(storageCapListWire(u, 8191)) != string(files["list_at.bin"]) {
		t.Fatal("storageCapListWire does not write list_at.bin")
	}
	files["list_term.bin"] = storageCapListWire(u, 8193)
	// one i32 list term: kind 4, count 1, one element
	files["term.bin"] = []byte{4, 1, 0, 0, 0, 0}
	files["main.goose"] = []byte(probePrelude + storageCapProbeMain)
	sizes := storageCapSizes(u)
	for k, n := range sizes {
		if n <= 0 || n > 1<<40 {
			t.Fatalf("%s: %d is not under the cap", k, n)
		}
	}
	want := strings.Join([]string{
		fmt.Sprintf("list_at %d _", sizes["list_at"]),
		fmt.Sprintf("nodes_at %d _", sizes["nodes_at"]),
		"list_over -1 storage_over_cap_ -1 0 storage_over_cap_",
		"nodes_over -1 storage_over_cap_ -1 0 storage_over_cap_",
		"list_term -1 storage_over_cap_ -1 0 storage_over_cap_",
		"term_at 1099511627776 _",
		fmt.Sprintf("term_over %d storage_over_cap_", int64(1)<<40-12),
	}, "\n")
	got := strings.TrimSpace(string(gooserun.RunDebug(t, t.TempDir(), "main.goose", files)))
	if got != want {
		t.Fatalf("the storage cap answers\n%s\nwant\n%s", got, want)
	}
}
