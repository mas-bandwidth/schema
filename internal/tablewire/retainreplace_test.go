package tablewire_test

import (
	"encoding/binary"
	"fmt"
	"strings"
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

// retainReplacedListSenderUnit is the endpoint's own declaration: `Inner` carries a
// field the reader below cannot name, so the arm at index `k` of the first
// `items` occurrence retains a record the reader's own build never sees. The
// message form has no byte-framed length to damage, so the first occurrence is
// well framed and the second occurrence alone replaces it (§3.3, §6.6).
const retainReplacedListSenderUnit = `package readq

fixed table Inner
{
    value  int32 = 0 | min = 0, max = 1000
    future int32
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

// retainReplacedListMessageBatch writes the same two-occurrence shape in the
// MESSAGE form: the first `items` carries `n` elements with the arm at index
// `k` naming a body that holds the reader's unknown `future`, and the second
// carries `n2` elements and replaces the first whole.
func retainReplacedListMessageBatch(t *testing.T, k, n2 int) (*tablewire.Vocabulary, []byte) {
	t.Helper()
	sender := listModel(t, retainReplacedListSenderUnit)
	const n = 16
	arm := func(i int) string {
		if i == k {
			return `{"alpha":{"value":1,"future":7}}`
		}
		return `{"beta":0}`
	}
	first := make([]string, n)
	for i := range first {
		first[i] = arm(i)
	}
	second := make([]string, n2)
	for i := range second {
		second[i] = `{"beta":0}`
	}
	source := place(t, sender, "Node", fmt.Sprintf(`{"items":[%s]}`, strings.Join(first, ",")))
	next := place(t, sender, "Node", fmt.Sprintf(`{"items":[%s]}`, strings.Join(second, ",")))
	source.Fields = append(source.Fields, next.Fields[0])
	batch, err := tablewire.EncodeMessages(sender, []*tabletext.Instance{source})
	if err != nil {
		t.Fatal(err)
	}
	vocabulary := &tablewire.Vocabulary{}
	if err := vocabulary.AnnounceRead(ir.TableAnnouncement(sender.Unit), &tabletext.Report{}); err != nil {
		t.Fatal(err)
	}
	return vocabulary, batch
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
				t.Fatalf("load: retained=%d, want 1: the arm's unknown field is retained", report.Retained)
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
	// THE MESSAGE FORM'S TWIN (§3.3, §6.6): the same replacement commits at the
	// array's count and align, and the arm record held under an index the new
	// occurrence never reaches dies there. Without forgetFieldArms the earlier
	// index keeps its handle and the save reports retain_lost.
	for _, tc := range cases {
		t.Run("message-"+tc.name, func(t *testing.T) {
			t.Parallel()
			vocabulary, batch := retainReplacedListMessageBatch(t, tc.k, tc.n2)
			inst := m.New(m.Lookup("Node"))
			retain := tablewire.Retain{Capacity: 8192, IdCapacity: 1024}
			var report tabletext.Report
			n, ok, err := tablewire.DecodeRetainMessages(m, []*tabletext.Instance{inst}, batch, vocabulary, []*tablewire.Retain{&retain}, &report)
			if err != nil {
				t.Fatal(err)
			}
			if n != 1 || !ok || report.Malformed {
				t.Fatalf("load: n=%d ok=%v report %+v", n, ok, report)
			}
			if report.Retained != 1 {
				t.Fatalf("load: retained=%d, want 1: the arm's unknown field should still be retained", report.Retained)
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
