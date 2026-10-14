package tablewire

import (
	"bytes"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// A RETAINING READ IS BOUNDED BY THE STORE, NOT BY THE ANNOUNCED COUNT
// (docs/SPEC-TABLES.md §3.3, §6.6). A peer's announcement is untrusted, and one
// unknown entry announcing an array with `min = max = 0xFFFFFFFF` over an
// element that rides no bits used to buy about 4 GiB of transcoding for a few
// bytes of wire: `msgElements` looped `range n` and `msgPayload` appended a LEB
// byte an element before `keep` ever checked the store's capacity. A nested
// array whose inner run is itself zero-width grows the record the same way, ten
// bytes an element.
//
// The walk is counted by the resolver's own test instrument and never by a
// clock: a walk past 1<<20 elements is the defect, whatever the machine's
// speed. The budget is the store's remaining capacity less the nine bytes
// `keep` charges, so the green walk stops far below it.
const retainZeroWidthUnit = `package retainzerowidth

fixed table Probe
{
    x uint8
}
`

func retainZeroWidthModel(t *testing.T) *tabletext.Model {
	t.Helper()
	f, perrs := parser.Parse("RetainZeroWidth.schema", []byte(retainZeroWidthUnit))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "RetainZeroWidth.schema", Name: "RetainZeroWidth.schema", Base: "RetainZeroWidth",
		Bytes: []byte(retainZeroWidthUnit), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	return tabletext.NewModel(u)
}

// retainZeroWidthBatch builds the one-body batch the cases read: the batch's
// count byte, one body carrying a reference to the single announced entry, the
// entry's own count bits where its shape states any, and the body's zero
// reference. It is the message form's own layout, spelled by hand because the
// entry is one no model declares.
func retainZeroWidthBatch(v *Vocabulary, shape ir.TableMessageShape, count uint64) []byte {
	var w bitWriter
	w.put(0, 8)           // M - 1, one body
	w.put(1, v.RefBits()) // the body's one field: a reference to slot 1
	if bits := ir.TableMessageCountBits(shape); bits > 0 {
		w.put(count, bits)
	}
	w.put(0, v.RefBits()) // the body's terminator
	w.align()
	return append([]byte{ir.TableWireMessageForm}, w.b...)
}

func TestRetainReadOfZeroWidthElementRunIsBounded(t *testing.T) {
	t.Parallel()
	m := retainZeroWidthModel(t)
	wide := ir.TableMessageListMax
	cases := []struct {
		name     string
		shape    ir.TableMessageShape
		count    uint64
		retained bool
	}{
		{
			// THE CARD'S FIRST SHAPE: the count rides in zero bits
			name:  "zero-width element run",
			shape: ir.TableMessageShape{Min: wide, Max: wide, Elem: ir.TableKindNoPayload, Inner: &ir.TableMessageShape{}},
		},
		{
			// AND THE NESTED ONE: a counted outer run whose every element is a
			// three-wide inner run of a zero-width element, so ten bytes an
			// element against the announced count
			name: "nested zero-width element run",
			shape: ir.TableMessageShape{Min: 0, Max: 1 << 20, Elem: ir.TableKindArray,
				Inner: &ir.TableMessageShape{Min: 3, Max: 3, Elem: ir.TableKindNoPayload, Inner: &ir.TableMessageShape{}}},
			count: 1 << 20,
		},
		{
			// A LEGAL RECORD FITS AND IS KEPT BYTE FOR BYTE AS BEFORE
			name:     "a legal run is retained",
			shape:    ir.TableMessageShape{Min: 3, Max: 3, Elem: ir.TableKindNoPayload, Inner: &ir.TableMessageShape{}},
			retained: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			entry := ir.TableVocabularyEntry{Id: 0x5a5a5a5a00000001, Kind: ir.TableKindArray, Shape: c.shape}
			v := &Vocabulary{entries: []ir.TableVocabularyEntry{entry}, announced: true}
			data := retainZeroWidthBatch(v, c.shape, c.count)

			value := m.New(m.Lookup("Probe"))
			store := &Retain{Capacity: 4096, IdCapacity: 32}
			var report tabletext.Report
			var walked uint64
			msgWalkProbe = &walked
			defer func() { msgWalkProbe = nil }()

			n, ok, err := DecodeRetainMessages(m, []*tabletext.Instance{value}, data, v, []*Retain{store}, &report)
			if n != 1 || !ok || err != nil || report.Malformed {
				t.Fatalf("read: %d %v %v %+v", n, ok, err, report)
			}
			if walked >= msgWalkProbeLimit {
				t.Fatalf("the transcoding walk did not stop at the budget: %d elements", walked)
			}
			if bound := uint64(store.Capacity) + 1; walked > bound {
				t.Fatalf("the walk took %d elements, past the %d the store could hold", walked, bound)
			}
			if report.Unknown != 1 || report.RetainLost != 1-boolToInt(c.retained) || report.Retained != boolToInt(c.retained) {
				t.Fatalf("counters: Unknown %d Retained %d RetainLost %d, want one unknown and retained=%v",
					report.Unknown, report.Retained, report.RetainLost, c.retained)
			}
			if !c.retained {
				return
			}
			// THE SAME RECORD, READ INTO A STORE THAT CANNOT DROP IT, IS THE
			// SAME BYTES: the budget must not change a record that fits
			// (docs/SPEC-TABLES.md §6.6).
			big := &Retain{Capacity: 1 << 20, IdCapacity: 32}
			other := m.New(m.Lookup("Probe"))
			var wideReport tabletext.Report
			if _, ok, err := DecodeRetainMessages(m, []*tabletext.Instance{other}, data, v, []*Retain{big}, &wideReport); !ok || err != nil || wideReport.Retained != 1 || wideReport.RetainLost != 0 {
				t.Fatalf("wide read: %v %v %+v", ok, err, wideReport)
			}
			var saveA, saveB tabletext.Report
			got, err := EncodeRetain(m, value, store, &saveA)
			if err != nil {
				t.Fatal(err)
			}
			want, err := EncodeRetain(m, other, big, &saveB)
			if err != nil || !bytes.Equal(got, want) {
				t.Fatalf("the budget changed a record that fits: %v", err)
			}
		})
	}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
