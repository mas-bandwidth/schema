package rusttable

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// DAMAGE ENDS THE BODY IT IS MET IN (docs/SPEC-TABLES.md §4): a damaged child
// body ends there, the enclosing body steps over it by its L and reads on, and
// the later non-finite float it reaches refuses the read as non_finite_float.
func TestRustDamagedChildReadsOnToLaterFloat(t *testing.T) {
	t.Skip("layer 4 (rust): leg tolerates what the strict read refuses, re-pin as a verdict: docs/plans/strict-only.md")
	slowtest.Gate(t, "rustc (damaged nested-body diagnostic ordering)")
	keyedRequireRustc(t)
	u := unitFrom(t, `package damaged
table Leaf { x float32 }
enum Key { A, B }
union Choice { leaf Leaf }
table Root {
 rows []Leaf
 items [2]Leaf
 counted [..2]Leaf
 keyed [Key]Leaf
 optional ?Leaf
 nested Leaf
 choice Choice
 entries map[uint32]Leaf
 later float32
}
`)
	m := tabletext.NewModel(u)
	var probe string
	for _, shape := range []string{"rows", "items", "counted", "keyed", "optional", "nested", "choice", "entries"} {
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
		field := func(name string, kind byte, payload []byte) []byte {
			return append(append(ref(ir.TableWireId(name)), kind), payload...)
		}
		length := func(body []byte) []byte { return append(binary.AppendUvarint(nil, uint64(len(body))), body...) }
		// The first x is finite; a final x reference has no kind or payload.
		bad := append(field("x", 10, []byte{0, 0, 128, 63}), ref(ir.TableWireId("x"))...)
		nan := append(field("x", 10, []byte{0, 0, 192, 127}), 0)
		var body []byte
		switch shape {
		case "rows", "items", "counted":
			payload := append([]byte{13, 2}, length(bad)...)
			payload = append(payload, length(nan)...)
			body = field(shape, 14, length(payload))
		case "keyed":
			payload := append([]byte{13, 2}, ref(ir.TableWireId("A"))...)
			payload = append(payload, length(bad)...)
			payload = append(payload, ref(ir.TableWireId("B"))...)
			payload = append(payload, length(nan)...)
			body = field(shape, 16, length(payload))
		case "choice":
			payload := append(ref(ir.TableWireId("leaf")), 13)
			payload = append(payload, length(bad)...)
			body = field(shape, 15, payload)
			body = append(body, field("later", 10, []byte{0, 0, 192, 127})...)
		case "entries":
			entry := field("key", 8, []byte{1, 0, 0, 0})
			entry = append(entry, field("value", 13, length(bad))...)
			entry = append(entry, 0)
			payload := append([]byte{13, 1}, length(entry)...)
			body = field(shape, 14, length(payload))
			body = append(body, field("later", 10, []byte{0, 0, 192, 127})...)
		default:
			body = field(shape, 13, length(bad))
			body = append(body, field("later", 10, []byte{0, 0, 192, 127})...)
		}
		wire := append(append([]byte{1}, body...), 0)
		for _, id := range ids {
			wire = binary.LittleEndian.AppendUint64(wire, id)
		}
		wire = binary.LittleEndian.AppendUint64(wire, uint64(len(ids)))
		var report tabletext.Report
		ok, err := tablewire.DecodeRegion(m, m.New(u.Tables["Root"]), wire, &report)
		if _, refused := errors.AsType[*tablewire.NonFiniteRefusal](err); ok || !refused {
			t.Fatalf("%s oracle: %v %v %+v", shape, ok, err, report)
		}
		probe += fmt.Sprintf(`for retaining in [false,true] {let wire=vec!%s;let mut report=TableReport::default();let mut store=TableRetain{bytes:Vec::with_capacity(4096),ids:Vec::with_capacity(4096)};let result=if retaining {root_load_retain(&wire,&mut store,&mut report)} else {root_load(&wire,&mut report)};assert!(result.is_none() && report.refused && !report.non_finite_float.is_empty(),"%s retaining={} {:?}",retaining,report);assert!(store.bytes.is_empty() && store.ids.is_empty());}`, rustByteVector(wire), shape)
	}
	if out, err := rustConstructProbe(t, u, probe); err != nil {
		t.Fatalf("native: %v\n%s", err, out)
	}
}

// The pinned list_damaged_child_reads_on vector: a damaged first placement ends
// there, and the list reads on to a later placement carrying non-finite bits,
// which refuses the read.
func TestRustDamagedListFuzzRegression(t *testing.T) {
	slowtest.Gate(t, "rustc (preserved damaged-list fuzz input)")
	keyedRequireRustc(t)
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	u := generateUnit(t, repo, []string{"tables/lists"})
	wire, err := os.ReadFile(filepath.Join(repo, "testdata/wire/tables/fuzz-vectors/list_damaged_child_reads_on.bin"))
	if err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`for retaining in [false,true] {let wire=vec!%s;let mut report=TableReport::default();let mut store=TableRetain{bytes:Vec::with_capacity(4096),ids:Vec::with_capacity(4096)};let result=if retaining {save_load_retain(&wire,&mut store,&mut report)} else {save_load(&wire,&mut report)};assert!(result.is_none() && report.refused && !report.non_finite_float.is_empty(),"retaining={} {:?}",retaining,report);assert!(store.bytes.is_empty() && store.ids.is_empty());}`, rustByteVector(wire))
	if out, err := rustConstructProbe(t, u, body); err != nil {
		t.Fatalf("native: %v\n%s", err, out)
	}
}
