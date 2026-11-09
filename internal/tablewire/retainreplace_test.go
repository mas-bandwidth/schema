package tablewire_test

import (
	"encoding/binary"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// retainReplacedListWire writes the shape THIS CARD pins: a root `Node` whose
// `items` LIST OF UNIONS occurs TWICE. The first occurrence carries `n`
// elements, and the arm at index `k` is a well-framed body whose unknown field
// is retained. The second occurrence replaces the first whole, carrying `n2`
// elements.
//
// docs/SPEC-TABLES.md §6.6: a later occurrence supersedes the earlier one, so
// an arm the first occurrence built past the second's count dies with it and
// neither counter moves — retain_lost stays 0 at save whatever `n2` is.
func retainReplacedListWire(k, n2 int) []byte {
	b := &wireBuilder{}
	const n = 16

	b.field("items", ir.TableKindArray)
	arrayAt := b.openLen()
	b.u8(uint8(ir.TableKindUnion))
	b.leb(n)

	for i := range n {
		if i == k {
			b.leb(b.ref("alpha"))
			b.u8(uint8(ir.TableKindTable))
			armAt := b.openLen()
			b.field("future", ir.TableKindI32)
			b.body = binary.LittleEndian.AppendUint32(b.body, 7)
			b.end()
			b.closeLen(armAt)
		} else {
			b.leb(0)
		}
	}
	b.closeLen(arrayAt)

	b.field("items", ir.TableKindArray)
	array2 := b.openLen()
	b.u8(uint8(ir.TableKindUnion))
	b.leb(uint64(n2))
	for range n2 {
		b.leb(0)
	}
	b.closeLen(array2)

	b.end()
	return b.finish()
}

func TestRetainOracleRefusesAReplacedList(t *testing.T) {
	t.Parallel()
	m := retainArmOccurrenceModel(t)
	for _, n2 := range []int{4, 11, 20} {
		inst := m.New(m.Lookup("Node"))
		retain := tablewire.Retain{Capacity: 8192, IdCapacity: 1024}
		var report tabletext.Report
		ok, err := tablewire.DecodeRetain(m, inst, retainReplacedListWire(10, n2), &retain, &report)
		if ok || tablewire.RefusalReason(err) != tablewire.ReasonWireDamaged || retain.Used() != 0 {
			t.Fatalf("n2=%d: %v %v %+v", n2, ok, err, report)
		}
	}
}
