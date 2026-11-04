package rusttable

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/memceiling"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// storageCapHugeSchema holds the overflow witnesses: Huge's storage is
// 2^40-64 bytes, so 2^24+1 of them sum past 2^64 and wrap to a total under the
// cap. A measure that summed before it checked would accept them.
const storageCapHugeSchema = `package hugecheck
fixed table Big { a [16777216]int64 }
fixed table Huge { b [8191]Big
    c [16777208]int64 }
table Root { items []Huge
    next *Huge }
`

// storageCapOverflowWire writes a form-1 file whose Root holds count empty
// Huge bodies, as the items list or as node-table records.
func storageCapOverflowWire(u *ir.Unit, nodes bool, count uint64) []byte {
	var ids []uint64
	ref := func(id uint64) []byte {
		for i, v := range ids {
			if v == id {
				return binary.AppendUvarint(nil, uint64(i+1))
			}
		}
		ids = append(ids, id)
		return binary.AppendUvarint(nil, uint64(len(ids)))
	}
	framed := func(body []byte) []byte {
		return append(binary.AppendUvarint(nil, uint64(len(body))), body...)
	}
	var root []byte
	if nodes {
		table := ref(math.MaxUint64)
		huge := ref(ir.TableWireId(u.Tables["Huge"].WireName()))
		records := binary.AppendUvarint(nil, count)
		for range count {
			records = append(records, huge...)
			records = append(records, 1, 0)
		}
		root = append(append(table, byte(ir.TableKindString)), framed(records)...)
	} else {
		items := ref(ir.TableFieldWireId(u.Tables["Root"].Fields[0]))
		list := binary.AppendUvarint([]byte{byte(ir.TableKindTable)}, count)
		for range count {
			list = append(list, 1, 0)
		}
		root = append(append(items, byte(ir.TableKindArray)), framed(list)...)
	}
	wire := append([]byte{1}, root...)
	wire = append(wire, 0)
	for _, id := range ids {
		wire = binary.LittleEndian.AppendUint64(wire, id)
	}
	return binary.LittleEndian.AppendUint64(wire, uint64(len(ids)))
}

// TestStorageCapRefusedAtMeasureRust holds the storage cap at load
// (docs/SPEC-TABLES.md §11, §6.5) on the shared vectors in
// testdata/wire/tables/storage-cap: Big's storage is 2^27 bytes, so 8191 empty
// Big bodies measure under the 2^40-byte cap and 8192 measure over it, as the
// items list and as node records. root_load_measure_reason answers a size at
// or under the cap for an at vector and (-1, StorageOverCap) for an over
// vector; root_load and root_load_retain refuse an over vector by name before
// any Big is built, keeping the report's damage history and retaining
// nothing. A load of an at vector would build about a TiB, so the probe never
// runs one.
func TestStorageCapRefusedAtMeasureRust(t *testing.T) {
	slowtest.Gate(t, "rustc (the generated storage-cap probe)")
	memceiling.Guard(t, memceiling.Default)
	t.Parallel()
	dir, err := filepath.Abs("../../../testdata/wire/tables/storage-cap")
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile(filepath.Join(dir, "Cap.schema"))
	if err != nil {
		t.Fatal(err)
	}
	var body strings.Builder
	body.WriteString(storageCapProbePrologue)
	for _, v := range []struct {
		file string
		over bool
	}{{"list_at.bin", false}, {"list_over.bin", true}, {"nodes_at.bin", false}, {"nodes_over.bin", true}} {
		writeStorageCapCase(&body, filepath.Join(dir, v.file), v.file, v.over, true)
	}
	body.WriteString("    if failures != 0 { std::process::exit(1); }\n")
	out, err := runRustDiscardProbe(t, unitFrom(t, string(src)), body.String())
	if err != nil {
		t.Fatalf("storage-cap probe: %v\n%s", err, out)
	}
}

// TestStorageCapTermsRefusedBeforeTheyWrapRust: each list extent term and
// each node record is checked against the cap before the sum can wrap. 2^24+1
// Huge elements or records wrap an unchecked int64 sum to a total under the
// cap, so the measure must refuse them as storage_over_cap.
func TestStorageCapTermsRefusedBeforeTheyWrapRust(t *testing.T) {
	slowtest.Gate(t, "rustc (the generated storage-cap overflow probe)")
	memceiling.Guard(t, memceiling.Default)
	t.Parallel()
	u := unitFrom(t, storageCapHugeSchema)
	dir := t.TempDir()
	var body strings.Builder
	body.WriteString(storageCapProbePrologue)
	for _, nodes := range []bool{false, true} {
		name := "list_wrap.bin"
		if nodes {
			name = "nodes_wrap.bin"
		}
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, storageCapOverflowWire(u, nodes, 1<<24+1), 0o644); err != nil {
			t.Fatal(err)
		}
		writeStorageCapCase(&body, path, name, true, false)
	}
	body.WriteString("    if failures != 0 { std::process::exit(1); }\n")
	out, err := runRustDiscardProbe(t, u, body.String())
	if err != nil {
		t.Fatalf("storage-cap overflow probe: %v\n%s", err, out)
	}
}

const storageCapProbePrologue = `
    let mut failures = 0;
    macro_rules! check {
        ($ok:expr, $name:expr) => { if !$ok { eprintln!("FAIL: {}", $name); failures += 1; } };
    }
    const CAP: i64 = 1 << 40;
`

// writeStorageCapCase emits one vector's checks: the measure for every
// vector, and the plain and retaining loads for an over vector when load is
// set.
func writeStorageCapCase(body *strings.Builder, path, name string, over, load bool) {
	fmt.Fprintf(body, "    {\n        let wire = std::fs::read(%q).unwrap();\n        let (size, why) = root_load_measure_reason(&wire);\n", path)
	if !over {
		fmt.Fprintf(body, `        check!(size > 0 && size <= CAP && why == TableReason::None && root_load_measure(&wire) == size, format!("%[1]s measure {} {:?}", size, why));
    }
`, name)
		return
	}
	fmt.Fprintf(body, `        check!(size == -1 && why == TableReason::StorageOverCap && root_load_measure(&wire) == -1, format!("%[1]s measure {} {:?}", size, why));
`, name)
	if load {
		fmt.Fprintf(body, `        for retaining in [false, true] {
            for old_damage in [false, true] {
                let mut report = TableReport { malformed: old_damage, unknown: 7, ..Default::default() };
                let mut store = TableRetain { bytes: Vec::with_capacity(4096), ids: vec![123] };
                store.bytes.push(99);
                let value = if retaining { root_load_retain(&wire, &mut store, &mut report) } else { root_load(&wire, &mut report) };
                check!(value.is_none(), "%[1]s load built a value");
                check!(report.refused && report.reason == TableReason::StorageOverCap && report.reason.name() == "storage_over_cap", format!("%[1]s load reason {:?}", report.reason));
                check!(report.malformed == old_damage && report.unknown == 7, "%[1]s load history");
                check!(!retaining || (store.bytes.is_empty() && store.ids.is_empty()), "%[1]s retained state");
            }
        }
`, name)
	}
	body.WriteString("    }\n")
}
