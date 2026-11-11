package tablewire

import (
	"math/big"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// A MESSAGE-FORM INTEGER IS HELD TO THIS READER'S OWN BOUND (docs/SPEC-TABLES.md
// §3.3): the announced base plus offset decodes to a value, and where the field
// declares no range the bound is the domain of its declared kind.
const messageIntDomainUnit = `package intdomain

fixed table Probe
{
    a int8
    b int32
    c uint8
}
`

func messageIntDomainModel(t *testing.T) *tabletext.Model {
	t.Helper()
	f, perrs := parser.Parse("IntDomain.schema", []byte(messageIntDomainUnit))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "IntDomain.schema", Name: "IntDomain.schema", Base: "IntDomain", Bytes: []byte(messageIntDomainUnit), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	return tabletext.NewModel(u)
}

func messageIntDomainField(t *testing.T, st *ir.Struct, name string) *ir.Field {
	t.Helper()
	for _, f := range st.Fields {
		if f.Name == name {
			return f
		}
	}
	t.Fatalf("field %s is not declared", name)
	return nil
}

// messageIntDomainBody writes one field's reference and payload, then the
// body's terminating zero reference and the batch's pad.
func messageIntDomainBody(v *Vocabulary, entry ir.TableVocabularyEntry, raw uint64) []byte {
	var w bitWriter
	w.put(0, 8) // the batch's count, M - 1
	w.put(1, v.RefBits())
	w.put(raw, int(ir.TableMessageValueBits(entry.Kind, entry.Shape)))
	w.put(0, v.RefBits())
	w.align()
	return append([]byte{ir.TableWireMessageForm}, w.b...)
}

func TestMessageIntegerHeldToReaderKind(t *testing.T) {
	t.Parallel()
	m := messageIntDomainModel(t)
	st := m.Lookup("Probe")
	a := messageIntDomainField(t, st, "a")
	b := messageIntDomainField(t, st, "b")
	c := messageIntDomainField(t, st, "c")

	hostile := []struct {
		name  string
		field *ir.Field
		base  int64
		bits  int64
		raw   uint64
	}{
		{"int8 above its domain", a, 100, 8, 255},
		{"uint8 above its domain", c, 250, 8, 255},
		{"int32 above its domain", b, 1<<31 - 1, 32, 1<<32 - 1},
	}
	for _, tc := range hostile {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			entry := ir.TableVocabularyEntry{
				Id:   ir.TableFieldWireId(tc.field),
				Kind: uint8(ir.TableWireScalarKind(tc.field)),
				Shape: ir.TableMessageShape{
					Packing: ir.TableMessageRanged,
					Bits:    tc.bits,
					Base:    big.NewInt(tc.base),
				},
			}
			v := &Vocabulary{entries: []ir.TableVocabularyEntry{entry}, announced: true}
			inst := m.New(st)
			var report tabletext.Report
			ok, err := DecodeMessage(m, inst, messageIntDomainBody(v, entry, tc.raw), v, &report)
			if ok || err == nil || !report.Refused || RefusalReason(err) != ReasonOutOfRange {
				t.Fatalf("a value outside the reader kind's domain was accepted: ok=%v err=%v report=%+v", ok, err, report)
			}
			if got := inst.Fields[entrySlot(st, tc.field)].Cell; got.I != 0 || got.U != 0 {
				t.Fatalf("a refused value was substituted: cell=%+v", got)
			}
		})
	}

	ends := []struct {
		name  string
		field *ir.Field
		raw   uint64
		want  int64
	}{
		{"int8 minimum", a, 1 << 7, -128},
		{"int8 maximum", a, 1<<7 - 1, 127},
		{"int32 minimum", b, 1 << 31, -1 << 31},
		{"int32 maximum", b, 1<<31 - 1, 1<<31 - 1},
		{"uint8 minimum", c, 0, 0},
		{"uint8 maximum", c, 255, 255},
	}
	for _, tc := range ends {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			entry := ir.TableVocabularyEntry{
				Id:   ir.TableFieldWireId(tc.field),
				Kind: uint8(ir.TableWireScalarKind(tc.field)),
			}
			v := &Vocabulary{entries: []ir.TableVocabularyEntry{entry}, announced: true}
			inst := m.New(st)
			var report tabletext.Report
			ok, err := DecodeMessage(m, inst, messageIntDomainBody(v, entry, tc.raw), v, &report)
			if !ok || err != nil || report.Refused {
				t.Fatalf("a value at the reader kind's end was refused: ok=%v err=%v report=%+v", ok, err, report)
			}
			if got := inst.Fields[entrySlot(st, tc.field)].Cell.I; got != tc.want {
				t.Fatalf("read %d, want %d", got, tc.want)
			}
		})
	}

	t.Run("a lawful widening", func(t *testing.T) {
		t.Parallel()
		entry := ir.TableVocabularyEntry{
			Id:   ir.TableFieldWireId(b),
			Kind: uint8(ir.TableKindI8),
		}
		v := &Vocabulary{entries: []ir.TableVocabularyEntry{entry}, announced: true}
		inst := m.New(st)
		var report tabletext.Report
		ok, err := DecodeMessage(m, inst, messageIntDomainBody(v, entry, 1<<7), v, &report)
		if !ok || err != nil || report.Refused || report.Widened != 1 {
			t.Fatalf("a narrower sender kind did not widen into a wider reader kind: ok=%v err=%v report=%+v", ok, err, report)
		}
		if got := inst.Fields[entrySlot(st, b)].Cell.I; got != -128 {
			t.Fatalf("read %d, want -128", got)
		}
	})
}

func entrySlot(st *ir.Struct, f *ir.Field) int {
	for i := range st.Fields {
		if st.Fields[i] == f {
			return i
		}
	}
	return -1
}
