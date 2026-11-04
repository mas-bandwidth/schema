package lobstertable

import (
	"bytes"
	"encoding/binary"
	"maps"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// SPEC-TABLES §2.8: an entry with no key field has the key's default. The
// entry remains present even when its key and value both elide. Its default
// still participates in the strict ascending-key check.
func TestLobsterMapDefaultKey(t *testing.T) {
	u := lobsterUnitOf(t, "package mapdefault\ntable Integers { entries map[uint8]int32 }\ntable Strings { entries map[string(8)]int32 }\n")
	wire := func(entries []byte, count byte, withKey bool) []byte {
		payload := append([]byte{13, count}, entries...)
		b := append([]byte{1, 1, 14, byte(len(payload))}, payload...)
		b = append(b, 0)
		b = binary.LittleEndian.AppendUint64(b, ir.TableFieldWireId(u.Tables["Integers"].Fields[0]))
		ids := uint64(1)
		if withKey {
			b = binary.LittleEndian.AppendUint64(b, ir.MapKeyWireId)
			ids++
		}
		return binary.LittleEndian.AppendUint64(b, ids)
	}
	good := wire([]byte{1, 0}, 1, false)
	duplicate := wire([]byte{1, 0, 1, 0}, 2, false)
	descending := wire([]byte{4, 2, 6, 2, 0, 1, 0}, 2, true)
	m := tabletext.NewModel(u)
	for _, name := range []string{"Integers", "Strings"} {
		inst := m.New(m.Lookup(name))
		var report tabletext.Report
		if ok, err := tablewire.Decode(m, inst, good, &report); !ok || err != nil {
			t.Fatalf("%s: default-key control refused: %v %+v", name, err, report)
		}
		if back, err := tablewire.Encode(m, inst); err != nil || !bytes.Equal(back, good) {
			t.Fatalf("%s: default-key canonical bytes %x, want %x (%v)", name, back, good, err)
		}
		if ok, _ := tablewire.Decode(m, m.New(m.Lookup(name)), duplicate, &report); ok {
			t.Fatalf("%s: repeated default key accepted", name)
		}
	}
	var report tabletext.Report
	if ok, _ := tablewire.Decode(m, m.New(m.Lookup("Integers")), descending, &report); ok {
		t.Fatal("descending key followed by default key accepted")
	}
	t.Logf("minimal default-key wire (%d bytes): %x", len(good), good)
	files, err := lobster.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	const program = `import .ProbeTable
import .schema_save
import .retain_unknown
import .region_measure
let good = read_file("good")
let duplicate = read_file("duplicate")
let descending = read_file("descending")
assert good and duplicate and descending
for(["Integers", "Strings"]) name:
    let rep = schema_rt.LoadReport {}
    let rec = schema_rt.load(name, good, rep)
    assert rec
    assert length(rec.fields[0].entries) == 1
    let entry = rec.fields[0].entries[0].tab
    assert entry and entry.fields[0].cell.i == 0 and entry.fields[0].cell.s == "" and entry.fields[1].cell.i == 0
    let saved, why = schema_rt.save_reasoned(rec)
    assert why == "" and saved == good
    schema_rt.release(rec)
    let measure, whole, refused = schema_rt.load_measure(name, good)
    assert measure >= 0 and whole and not refused
    let store = schema_rt.Retain { capacity: 1024, id_capacity: 32 }
    let kept = schema_rt.load_retain(name, good, store, rep)
    assert kept and schema_rt.save_retain(kept, store, rep) == good
    schema_rt.release(kept)
    assert not schema_rt.load(name, duplicate, rep)
    let _n, _whole, bad = schema_rt.load_measure(name, duplicate)
    assert bad
assert not schema_rt.load("Integers", descending, schema_rt.LoadReport {})
print("PASS")
`
	out, _ := lobsterEvolutionRun(t, files, program, map[string][]byte{"good": good, "duplicate": duplicate, "descending": descending})
	if strings.TrimSpace(out) != "PASS" {
		t.Fatalf("default map key: %s", out)
	}
}
