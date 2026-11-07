package tablewire_test

import (
	"encoding/binary"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const retainArmOccurrenceUnit = `package readq

fixed table Inner
{
    value int32 = 0 | min = 0, max = 1000
}

union Pick
{
    alpha Inner
    beta  int32
}

table Node
{
    items []Pick
}
`

func retainArmOccurrenceModel(t *testing.T) *tabletext.Model {
	t.Helper()
	f, perrs := parser.Parse("RetainArmOccurrence.schema", []byte(retainArmOccurrenceUnit))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "RetainArmOccurrence.schema", Name: "RetainArmOccurrence.schema", Base: "RetainArmOccurrence",
		Bytes: []byte(retainArmOccurrenceUnit), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	return tabletext.NewModel(u)
}

// retainArmOccurrenceWire writes the shape THIS CARD pins (docs/SPEC-TABLES.md
// §6.6): a root `Node` whose `items` LIST OF UNIONS carries enough elements
// that the slot slice grows and reallocates while it decodes, where element
// zero's union arm is a body whose unknown field is retained, and then the SAME
// `items` field AGAIN, which the spec makes a second occurrence that replaces
// the first whole.
//
// The file is well framed: damage would discard the whole load (§4), so the
// first occurrence's record has to die by the replacement alone. A slice
// append moves the earlier cell while the list decodes, so a lookup keyed by
// the cell's address would miss the arm and the earlier occurrence's record
// would outlive its body, to be counted `retain_lost` at save.
func retainArmOccurrenceWire() []byte {
	b := &wireBuilder{}
	const n = 16

	b.field("items", ir.TableKindArray)
	arrayAt := b.openLen()
	b.u8(uint8(ir.TableKindUnion))
	b.leb(n)

	b.leb(b.ref("alpha"))
	b.u8(uint8(ir.TableKindTable))
	armAt := b.openLen()
	b.field("future", ir.TableKindI32)
	b.body = binary.LittleEndian.AppendUint32(b.body, 7)
	b.end()
	b.closeLen(armAt)

	for i := 1; i < n; i++ {
		b.leb(0)
	}
	b.closeLen(arrayAt)

	b.field("items", ir.TableKindArray)
	array2 := b.openLen()
	b.u8(uint8(ir.TableKindUnion))
	b.leb(n)
	for range n {
		b.leb(0)
	}
	b.closeLen(array2)

	b.end()
	return b.finish()
}

func TestRetainOracleForgetsAnArmByOccurrence(t *testing.T) {
	t.Parallel()
	m := retainArmOccurrenceModel(t)
	inst := m.New(m.Lookup("Node"))
	retain := tablewire.Retain{Capacity: 8192, IdCapacity: 1024}
	var report tabletext.Report
	ok, err := tablewire.DecodeRetain(m, inst, retainArmOccurrenceWire(), &retain, &report)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("the load refused a well-framed file")
	}
	if report.Retained != 1 || report.RetainLost != 0 {
		t.Fatalf("load: retained=%d retain_lost=%d, want 1 / 0", report.Retained, report.RetainLost)
	}
	var save tabletext.Report
	if _, err := tablewire.EncodeRetain(m, inst, &retain, &save); err != nil {
		t.Fatal(err)
	}
	if save.RetainLost != 0 {
		t.Fatalf("save: retain_lost=%d, want 0: the earlier arm's record survived the occurrence that replaced it", save.RetainLost)
	}
}
