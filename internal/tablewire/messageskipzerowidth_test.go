package tablewire

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// A RUN OF ZERO-WIDTH ELEMENTS IS NOT A LOOP (docs/SPEC-TABLES.md §3.3). A
// endpoint's announcement is untrusted, and one unknown entry announcing an array
// with `min = max = 0xFFFFFFFF` over an element that rides no bits used to buy
// about 2^32 iterations of the skip walk for a few bytes of wire: kind 0 and
// kind 32 elements because the arithmetic run did not take them, and a nested
// array over a zero-width inner array because nothing bounds a walked element
// that consumes nothing.
//
// The work is counted by the decoder's own step counter and never by a clock:
// a walk past 1<<20 elements is the defect, whatever the machine's speed.
const zeroWidthUnit = `package zerowidth

fixed table Probe
{
    x uint8
}
`

func zeroWidthModel(t *testing.T) *tabletext.Model {
	t.Helper()
	f, perrs := parser.Parse("ZeroWidth.schema", []byte(zeroWidthUnit))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "ZeroWidth.schema", Name: "ZeroWidth.schema", Base: "ZeroWidth", Bytes: []byte(zeroWidthUnit), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	return tabletext.NewModel(u)
}

func TestMessageSkipOfZeroWidthElementsIsBounded(t *testing.T) {
	t.Parallel()
	const limit = 1 << 20
	wide := ir.TableMessageListMax
	cases := []struct {
		name      string
		shape     ir.TableMessageShape
		malformed bool
	}{
		{"elem 0", ir.TableMessageShape{Min: wide, Max: wide, Elem: 0, Inner: &ir.TableMessageShape{}}, false},
		{"elem 32", ir.TableMessageShape{Min: wide, Max: wide, Elem: ir.TableKindNoPayload, Inner: &ir.TableMessageShape{}}, false},
		{"nested elem 14 over a zero-width inner array", ir.TableMessageShape{Min: wide, Max: wide, Elem: ir.TableKindArray,
			Inner: &ir.TableMessageShape{Min: 3, Max: 3, Elem: 0, Inner: &ir.TableMessageShape{}}}, true},
		{"nested elem 14 over an empty inner array", ir.TableMessageShape{Min: wide, Max: wide, Elem: ir.TableKindArray,
			Inner: &ir.TableMessageShape{Min: 0, Max: 0, Elem: ir.TableKindBool, Inner: &ir.TableMessageShape{}}}, true},
		// AND A WELL-FORMED NESTED RUN STILL SKIPS: a count the batch's bits
		// back is the shape a conforming writer produces, and it reads as before
		{"nested elem 14, a count the bits back", ir.TableMessageShape{Min: 3, Max: 3, Elem: ir.TableKindArray,
			Inner: &ir.TableMessageShape{Min: 2, Max: 2, Elem: 0, Inner: &ir.TableMessageShape{}}}, false},
	}
	m := zeroWidthModel(t)
	for _, c := range cases {
		entry := ir.TableVocabularyEntry{Id: 0x5a5a5a5a00000001, Kind: ir.TableKindArray, Shape: c.shape}
		// THE ANNOUNCEMENT ACCEPTS THE SHAPE, which is what makes it hostile
		// rather than impossible: the decode below meets it unrefused
		encoded := entry.Encode(nil)
		if _, _, ok := ir.DecodeShape(encoded[9:], entry.Kind); !ok {
			t.Fatalf("%s: the announcement refuses the shape, so the case tests nothing", c.name)
		}
		v := &Vocabulary{entries: []ir.TableVocabularyEntry{entry}, announced: true}
		// one body: the unknown entry's reference, no count (min = max), the
		// body's zero reference, then the batch's pad
		var w bitWriter
		w.put(0, 8) // M - 1
		w.put(1, v.RefBits())
		w.put(0, v.RefBits())
		w.align()
		data := append([]byte{ir.TableWireMessageForm}, w.b...)

		r := newBitReader(data[1:])
		if _, ok := r.get(8); !ok {
			t.Fatalf("%s: the batch count did not read", c.name)
		}
		var report tabletext.Report
		steps := &skipSteps{limit: limit}
		d := &bitDecoder{m: m, v: v, report: &report, r: r, refBits: v.RefBits(), steps: steps}
		ok := d.body(m.New(m.Lookup("Probe")))
		if steps.n >= limit {
			t.Errorf("%s: the skip walked %d elements of a %d-byte batch before the counter stopped it", c.name, steps.n, len(data))
			continue
		}
		if bound := uint64(len(data)*8) + 1; steps.n > bound {
			t.Errorf("%s: the skip walked %d elements, more than the %d bits the batch holds", c.name, steps.n, bound)
		}
		if c.malformed {
			if ok || !report.Malformed {
				t.Errorf("%s: a nested run of zero-width elements was not refused as malformed: ok=%v report %+v", c.name, ok, report)
			}
			continue
		}
		if !ok || report.Malformed || report.Unknown != 1 {
			t.Errorf("%s: the entry was not skipped: ok=%v report %+v", c.name, ok, report)
		}
		// AND THE PUBLIC READ AGREES, the same bytes through DecodeMessage
		var whole tabletext.Report
		got, err := DecodeMessage(m, m.New(m.Lookup("Probe")), data, v, &whole)
		if err != nil || !got || whole.Malformed {
			t.Errorf("%s: DecodeMessage did not read the batch: ok=%v err=%v report %+v", c.name, got, err, whole)
		}
	}
}
