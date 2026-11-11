package tablewire

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE SKIP WALK IS BOUNDED AT DEPTH 128 (docs/SPEC-TABLES.md §3.3): an entry
// this reader cannot name is skipped through its announced shape, and a body
// that nests an unknown table inside itself is refused past 128 levels rather
// than walked to the batch's end.
const messageSkipDepthUnit = `package skipdepth

fixed table Probe
{
    x uint8
}
`

func messageSkipDepthModel(t *testing.T) *tabletext.Model {
	t.Helper()
	f, perrs := parser.Parse("SkipDepth.schema", []byte(messageSkipDepthUnit))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "SkipDepth.schema", Name: "SkipDepth.schema", Base: "SkipDepth", Bytes: []byte(messageSkipDepthUnit), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	return tabletext.NewModel(u)
}

func TestMessageSkipDepthIsBounded(t *testing.T) {
	t.Parallel()
	m := messageSkipDepthModel(t)
	entry := ir.TableVocabularyEntry{Id: 0x5a5a5a5a00000002, Kind: ir.TableKindTable}
	v := &Vocabulary{entries: []ir.TableVocabularyEntry{entry}, announced: true}
	refBits := v.RefBits()
	cases := []struct {
		name   string
		levels int
		want   bool
	}{
		{"128 levels", 128, true},
		{"129 levels", 129, false},
		{"1000 levels", 1000, false},
		{"10000 levels", 10000, false},
	}
	for _, c := range cases {
		// one body that names the unknown entry at each level, then closes
		// every body it opened
		var w bitWriter
		w.put(0, 8) // M - 1
		for range c.levels {
			w.put(1, refBits)
		}
		for range c.levels + 1 {
			w.put(0, refBits)
		}
		w.align()
		data := append([]byte{ir.TableWireMessageForm}, w.b...)

		var report tabletext.Report
		r := newBitReader(data[1:])
		if _, ok := r.get(8); !ok {
			t.Fatalf("%s: the batch count did not read", c.name)
		}
		d := &bitDecoder{m: m, v: v, report: &report, r: r, refBits: refBits, fail: &readFail{}}
		ok := d.body(m.New(m.Lookup("Probe")))
		if ok != c.want {
			t.Errorf("%s: the skip verdict is %v, want %v", c.name, ok, c.want)
		}
		if c.want {
			if d.fail.err != nil || report.Refused {
				t.Errorf("%s: the batch was refused: err=%v report %+v", c.name, d.fail.err, report)
			}
		} else if d.fail.err == nil {
			t.Errorf("%s: the batch was not refused", c.name)
		}

		// AND THE PUBLIC READ AGREES, the same bytes through DecodeMessage
		var whole tabletext.Report
		got, err := DecodeMessage(m, m.New(m.Lookup("Probe")), data, v, &whole)
		if got != c.want {
			t.Errorf("%s: DecodeMessage verdict is %v, want %v", c.name, got, c.want)
		}
		if c.want && err != nil {
			t.Errorf("%s: DecodeMessage refused the batch: %v", c.name, err)
		}
		if !c.want && err == nil {
			t.Errorf("%s: DecodeMessage did not refuse the batch", c.name)
		}
	}
}
