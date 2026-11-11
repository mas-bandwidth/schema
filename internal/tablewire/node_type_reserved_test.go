package tablewire

import (
	"encoding/binary"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// A node record's TYPE reference resolves to an id (docs/SPEC-TABLES.md §3.1).
// The three ids the language holds back are damage anywhere but their own
// transport, so a record whose type reference names one refuses the whole load
// (§3.3, §5). The two blob type ids are a blob record's lawful type and read
// as before (§2.5).
const reservedTypeUnit = `package reservedtype

table Child
{
    n int32
}

table Root
{
    head *Child
    data *bytes
    text *string
}
`

// reservedTypeWire encodes one Root value.
func reservedTypeWire(t *testing.T, m *tabletext.Model, value string) []byte {
	t.Helper()
	wire, err := Encode(m, covPlace(t, m, "Root", value))
	if err != nil {
		t.Fatal(err)
	}
	return wire
}

// reservedTypeFile is a Root whose head names one empty Child record, with the
// record's TYPE reference in the id-table slot the writer gave it. The
// node-table id is carried once, so a reserved id in the TYPE position is not
// also a repeated entry.
func reservedTypeFile(typeId uint64) []byte {
	ids := []uint64{ir.TableWireId("head"), ir.TableNodeWireId, typeId}
	typeRef := byte(3)
	if typeId == ir.TableNodeWireId {
		ids = ids[:2]
		typeRef = 2
	}
	// node_count 1, the record's TYPE reference, L = 1, the empty table body
	payload := []byte{1, typeRef, 1, 0}
	body := []byte{1, uint8(ir.TableKindPointer), 2, 2, uint8(ir.TableKindString), byte(len(payload))}
	body = append(body, payload...)
	body = append(body, 0)
	out := append([]byte{ir.TableWireForm}, body...)
	for _, id := range ids {
		out = binary.LittleEndian.AppendUint64(out, id)
	}
	return binary.LittleEndian.AppendUint64(out, uint64(len(ids)))
}

func reservedTypeRead(t *testing.T, m *tabletext.Model, wire []byte) (bool, tabletext.Report, error) {
	t.Helper()
	var report tabletext.Report
	ok, err := Decode(m, m.New(m.Lookup("Root")), wire, &report)
	return ok, report, err
}

// TestNodeTypeReservedIdRefused holds a node record whose TYPE reference
// resolves to a reserved id to the rule that refuses it, and the two blob type
// ids and an ordinary table id to the reads they keep (docs/SPEC-TABLES.md
// §3.1, §3.3, §5).
func TestNodeTypeReservedIdRefused(t *testing.T) {
	t.Parallel()
	m := covModel(t, reservedTypeUnit)

	for _, control := range []struct {
		name, value string
	}{
		{"an ordinary node record", `{"head":{"n":7}}`},
		{"a bytes blob record", `{"data":"AQID"}`},
		{"a string blob record", `{"text":"hello"}`},
	} {
		ok, report, err := reservedTypeRead(t, m, reservedTypeWire(t, m, control.value))
		if !ok || err != nil || report.Refused {
			t.Errorf("%s did not read: ok=%t err=%v report=%+v", control.name, ok, err, report)
		}
	}

	for _, reserved := range []struct {
		name string
		id   uint64
	}{
		{"the node-table id", ir.TableNodeWireId},
		{"the build-version id", ir.TableBuildVersionWireId},
		{"the vocabulary id", ir.TableMessageVocabularyWireId},
	} {
		ok, report, err := reservedTypeRead(t, m, reservedTypeFile(reserved.id))
		if ok || !ReadRefusal(err) || !report.Refused {
			t.Errorf("a node record whose type reference resolves to %s read: ok=%t err=%v report=%+v", reserved.name, ok, err, report)
		}
	}
}
