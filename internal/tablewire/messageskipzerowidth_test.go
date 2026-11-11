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
// kind 32 elements because the arithmetic run did not take them. A NESTED
// ELEMENT SHAPE IS REFUSED AT THE ANNOUNCEMENT, so a list of lists never
// reaches the skip walk at all.
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
		name     string
		shape    ir.TableMessageShape
		announce bool
	}{
		{"elem 0", ir.TableMessageShape{Min: wide, Max: wide, Elem: 0, Inner: &ir.TableMessageShape{}}, false},
		{"elem 32", ir.TableMessageShape{Min: wide, Max: wide, Elem: ir.TableKindNoPayload, Inner: &ir.TableMessageShape{}}, false},
		{"nested elem 14 over a zero-width inner array", ir.TableMessageShape{Min: wide, Max: wide, Elem: ir.TableKindArray,
			Inner: &ir.TableMessageShape{Min: 3, Max: 3, Elem: 0, Inner: &ir.TableMessageShape{}}}, true},
		{"nested elem 14 over an empty inner array", ir.TableMessageShape{Min: wide, Max: wide, Elem: ir.TableKindArray,
			Inner: &ir.TableMessageShape{Min: 0, Max: 0, Elem: ir.TableKindBool, Inner: &ir.TableMessageShape{}}}, true},
		// AND A NESTED SHAPE IS REFUSED AT THE ANNOUNCEMENT: a count the bits
		// back is still a list of lists, which no declaration produces
		// A WALKED ZERO-PROGRESS ELEMENT IS DAMAGE EVEN WHEN THE BATCH
		// HAS ENOUGH PADDING BITS FOR THE REMAINING COUNT (§3.3).
		{"nested elem 14, a count the bits back", ir.TableMessageShape{Min: 3, Max: 3, Elem: ir.TableKindArray,
			Inner: &ir.TableMessageShape{Min: 2, Max: 2, Elem: 0, Inner: &ir.TableMessageShape{}}}, true},
	}
	m := zeroWidthModel(t)
	for _, c := range cases {
		entry := ir.TableVocabularyEntry{Id: 0x5a5a5a5a00000001, Kind: ir.TableKindArray, Shape: c.shape}
		encoded := entry.Encode(nil)
		_, _, accepted := ir.DecodeShape(encoded[9:], entry.Kind)
		if c.announce {
			if accepted {
				t.Fatalf("%s: the announcement accepted a nested element shape", c.name)
			}
			continue
		}
		if !accepted {
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
		if !ok || report.Refused || report.Unknown != 1 {
			t.Errorf("%s: the entry was not skipped: ok=%v report %+v", c.name, ok, report)
		}
		// AND THE PUBLIC READ AGREES, the same bytes through DecodeMessage
		var whole tabletext.Report
		got, err := DecodeMessage(m, m.New(m.Lookup("Probe")), data, v, &whole)
		if err != nil || !got || whole.Refused {
			t.Errorf("%s: DecodeMessage did not read the batch: ok=%v err=%v report %+v", c.name, got, err, whole)
		}
	}
}

func TestMessageSkipRefusesElementWithNoProgress(t *testing.T) {
	t.Parallel()
	const limit = 1 << 20
	m := zeroWidthModel(t)
	cases := []struct {
		name  string
		shape ir.TableMessageShape
		bits  []uint64
		want  bool
	}{
		{
			name: "walked nested empty arrays refuse",
			shape: ir.TableMessageShape{Min: 2, Max: 2, Elem: ir.TableKindArray,
				Inner: &ir.TableMessageShape{Min: 0, Max: 0, Elem: ir.TableKindBool, Inner: &ir.TableMessageShape{}}},
			want: false,
		},
		{
			name: "walked nested arrays with payload skip",
			shape: ir.TableMessageShape{Min: 2, Max: 2, Elem: ir.TableKindArray,
				Inner: &ir.TableMessageShape{Min: 1, Max: 1, Elem: ir.TableKindBool, Inner: &ir.TableMessageShape{}}},
			bits: []uint64{0, 0},
			want: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			entry := ir.TableVocabularyEntry{Id: 0x5a5a5a5a00000001, Kind: ir.TableKindArray, Shape: c.shape}
			encoded := entry.Encode(nil)
			if _, _, ok := ir.DecodeShape(encoded[9:], entry.Kind); !ok {
				t.Fatal("announcement refused the shape")
			}
			v := &Vocabulary{entries: []ir.TableVocabularyEntry{entry}, announced: true}
			var w bitWriter
			w.put(0, 8) // M - 1
			w.put(1, v.RefBits())
			for _, bit := range c.bits {
				w.put(bit, 1)
			}
			w.put(0, v.RefBits())
			w.align()
			data := append([]byte{ir.TableWireMessageForm}, w.b...)

			r := newBitReader(data[1:])
			if _, ok := r.get(8); !ok {
				t.Fatal("batch count did not read")
			}
			var report tabletext.Report
			steps := &skipSteps{limit: limit}
			d := &bitDecoder{m: m, v: v, report: &report, r: r, refBits: v.RefBits(), steps: steps}
			ok := d.body(m.New(m.Lookup("Probe")))
			if ok != c.want {
				t.Errorf("skip result = %v, want %v; failure=%v report=%+v", ok, c.want, d.fail, report)
			}
			if c.want && (report.Refused || report.Unknown != 1) {
				t.Errorf("well-formed skipped entry was refused: %+v", report)
			}
			if !c.want && (d.fail == nil || d.fail.err == nil) {
				t.Errorf("zero-progress element did not record damage: report=%+v", report)
			}
			if steps.n >= limit {
				t.Errorf("skip walked %d elements", steps.n)
			}
		})
	}
}
