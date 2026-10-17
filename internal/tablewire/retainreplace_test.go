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
// elements, and the arm at index `k` is a body whose unknown field is retained
// but whose framing is damaged (its terminator ends before the last byte of its
// L), so only the arm handle still names that body. The second occurrence
// replaces the first whole, carrying `n2` elements.
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

	for i := 0; i < n; i++ {
		if i == k {
			b.leb(b.ref("alpha"))
			b.u8(uint8(ir.TableKindTable))
			armAt := b.openLen()
			b.field("future", ir.TableKindI32)
			b.body = binary.LittleEndian.AppendUint32(b.body, 7)
			b.end()
			b.body = append(b.body, 0xAA, 0xBB)
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
	for i := 0; i < n2; i++ {
		b.leb(0)
	}
	b.closeLen(array2)

	b.end()
	return b.finish()
}

func TestRetainOracleForgetsEveryArmOfAReplacedList(t *testing.T) {
	t.Parallel()
	m := retainArmOccurrenceModel(t)
	cases := []struct {
		name string
		k    int
		n2   int
	}{
		{"fewer-elements", 10, 4},
		{"equal-elements", 10, 11},
		{"more-elements", 10, 20},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			inst := m.New(m.Lookup("Node"))
			retain := tablewire.Retain{Capacity: 8192, IdCapacity: 1024}
			var report tabletext.Report
			ok, err := tablewire.DecodeRetain(m, inst, retainReplacedListWire(tc.k, tc.n2), &retain, &report)
			if err != nil {
				t.Fatal(err)
			}
			if !ok {
				t.Fatal("the load refused a well-framed file")
			}
			if report.Retained != 1 {
				t.Fatalf("load: retained=%d, want 1: the damaged arm's unknown field should still be retained", report.Retained)
			}
			var save tabletext.Report
			if _, err := tablewire.EncodeRetain(m, inst, &retain, &save); err != nil {
				t.Fatal(err)
			}
			if save.RetainLost != 0 {
				t.Fatalf("save: retain_lost=%d, want 0: the replaced occurrence's earlier arm has no record to lose", save.RetainLost)
			}
		})
	}
}
