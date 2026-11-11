package tablewire

import (
	"math"
	"math/big"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestMessageSkipChecksPayload holds docs/SPEC-TABLES.md §3.3: §4's "Skipped
// is not unread" applies on the message form too, so an entry this reader does
// not declare is skipped by its announced shape AND held to the conditions the
// wire and the announcement decide: a keyed slot names a no-payload entry and
// does not repeat, text is well-formed, floats are finite, a quantized index is
// at or below its count, a count is within [min,max], a ranged integer is in
// its kind's announced range, and a nested body names no id twice and no
// reserved id.
func TestMessageSkipChecksPayload(t *testing.T) {
	t.Parallel()
	m := covModel(t, `package skipchecks
fixed table Probe { x uint8 }
`)
	root := m.Lookup("Probe")
	id := func(n byte) uint64 { return 0x5a5a5a5a00000000 | uint64(n) }
	empty := func() *ir.TableMessageShape { return &ir.TableMessageShape{} }
	name := ir.TableVocabularyEntry{Id: id(2)}
	noPayload := ir.TableVocabularyEntry{Id: id(2), Kind: ir.TableKindNoPayload}
	stringEntry := ir.TableVocabularyEntry{Id: id(3), Kind: ir.TableKindString, Shape: ir.TableMessageShape{Max: 4}}
	quantized := ir.TableMessageShape{Packing: ir.TableMessageQuantized, QMin: 0, QMax: 1, QRes: 0.1}
	quantizedCount, _, quantizedOK := ir.TableMessageQuantization(quantized)
	if !quantizedOK {
		t.Fatal("the quantized shape does not derive")
	}
	quantized.Bits = int64(ir.TableMessageBitsRequired(0, int64(quantizedCount)))
	ranged := ir.TableMessageShape{Packing: ir.TableMessageRanged, Bits: 8, Base: big.NewInt(-100)}

	cases := []struct {
		name    string
		entries []ir.TableVocabularyEntry
		write   func(*bitWriter, int)
		bad     bool
	}{
		{
			name: "a keyed key naming an entry of the wrong sort",
			entries: []ir.TableVocabularyEntry{
				{Id: id(1), Kind: ir.TableKindKeyed, Shape: ir.TableMessageShape{Max: 2, Elem: ir.TableKindNoPayload, Inner: empty()}},
				name, stringEntry,
			},
			write: func(w *bitWriter, refBits int) {
				w.put(1, 2)
				w.put(3, refBits)
			},
			bad: true,
		},
		{
			name: "a keyed slot twice in one array",
			entries: []ir.TableVocabularyEntry{
				{Id: id(1), Kind: ir.TableKindKeyed, Shape: ir.TableMessageShape{Max: 2, Elem: ir.TableKindNoPayload, Inner: empty()}},
				name,
			},
			write: func(w *bitWriter, refBits int) {
				w.put(2, 2)
				w.put(2, refBits)
				w.put(2, refBits)
			},
			bad: true,
		},
		{
			name: "a lawful keyed array",
			entries: []ir.TableVocabularyEntry{
				{Id: id(1), Kind: ir.TableKindKeyed, Shape: ir.TableMessageShape{Max: 2, Elem: ir.TableKindNoPayload, Inner: empty()}},
				name,
			},
			write: func(w *bitWriter, refBits int) {
				w.put(1, 2)
				w.put(2, refBits)
			},
		},
		{
			name:    "a string that is not well-formed text",
			entries: []ir.TableVocabularyEntry{{Id: id(1), Kind: ir.TableKindString, Shape: ir.TableMessageShape{Max: 8}}},
			write: func(w *bitWriter, _ int) {
				w.put(1, 4)
				w.align()
				w.bytes([]byte{0xff})
			},
			bad: true,
		},
		{
			name:    "a string carrying a zero byte",
			entries: []ir.TableVocabularyEntry{{Id: id(1), Kind: ir.TableKindString, Shape: ir.TableMessageShape{Max: 8}}},
			write: func(w *bitWriter, _ int) {
				w.put(3, 4)
				w.align()
				w.bytes([]byte{'a', 0, 'b'})
			},
			bad: true,
		},
		{
			name:    "a lawful string",
			entries: []ir.TableVocabularyEntry{{Id: id(1), Kind: ir.TableKindString, Shape: ir.TableMessageShape{Max: 8}}},
			write: func(w *bitWriter, _ int) {
				w.put(3, 4)
				w.align()
				w.bytes([]byte("abc"))
			},
		},
		{
			name:    "a non-finite f32",
			entries: []ir.TableVocabularyEntry{{Id: id(1), Kind: ir.TableKindF32}},
			write: func(w *bitWriter, _ int) {
				w.put(uint64(math.Float32bits(float32(math.NaN()))), 32)
			},
			bad: true,
		},
		{
			name:    "a lawful f32",
			entries: []ir.TableVocabularyEntry{{Id: id(1), Kind: ir.TableKindF32}},
			write: func(w *bitWriter, _ int) {
				w.put(uint64(math.Float32bits(1.5)), 32)
			},
		},
		{
			name:    "a quantized index above count",
			entries: []ir.TableVocabularyEntry{{Id: id(1), Kind: ir.TableKindF32, Shape: quantized}},
			write: func(w *bitWriter, _ int) {
				w.put(uint64(quantizedCount+1), int(quantized.Bits))
			},
			bad: true,
		},
		{
			name:    "a lawful quantized index",
			entries: []ir.TableVocabularyEntry{{Id: id(1), Kind: ir.TableKindF32, Shape: quantized}},
			write: func(w *bitWriter, _ int) {
				w.put(uint64(quantizedCount/2), int(quantized.Bits))
			},
		},
		{
			name:    "a non-finite f64",
			entries: []ir.TableVocabularyEntry{{Id: id(1), Kind: ir.TableKindF64}},
			write: func(w *bitWriter, _ int) {
				w.put(math.Float64bits(math.Inf(1)), 64)
			},
			bad: true,
		},
		{
			name:    "a lawful f64",
			entries: []ir.TableVocabularyEntry{{Id: id(1), Kind: ir.TableKindF64}},
			write: func(w *bitWriter, _ int) {
				w.put(math.Float64bits(2.5), 64)
			},
		},
		{
			name: "an array count above its announced maximum",
			entries: []ir.TableVocabularyEntry{
				{Id: id(1), Kind: ir.TableKindArray, Shape: ir.TableMessageShape{Min: 0, Max: 2, Elem: ir.TableKindNoPayload, Inner: empty()}},
			},
			write: func(w *bitWriter, _ int) {
				w.put(3, 2)
			},
			bad: true,
		},
		{
			name: "a lawful array count",
			entries: []ir.TableVocabularyEntry{
				{Id: id(1), Kind: ir.TableKindArray, Shape: ir.TableMessageShape{Min: 0, Max: 2, Elem: ir.TableKindNoPayload, Inner: empty()}},
			},
			write: func(w *bitWriter, _ int) {
				w.put(2, 2)
			},
		},
		{
			name: "a keyed count above its announced maximum",
			entries: []ir.TableVocabularyEntry{
				{Id: id(1), Kind: ir.TableKindKeyed, Shape: ir.TableMessageShape{Max: 2, Elem: ir.TableKindNoPayload, Inner: empty()}},
				name,
				{Id: id(3)},
				{Id: id(4)},
			},
			write: func(w *bitWriter, refBits int) {
				w.put(3, 2)
				w.put(2, refBits)
				w.put(3, refBits)
				w.put(4, refBits)
			},
			bad: true,
		},
		{
			name: "a lawful keyed count",
			entries: []ir.TableVocabularyEntry{
				{Id: id(1), Kind: ir.TableKindKeyed, Shape: ir.TableMessageShape{Max: 2, Elem: ir.TableKindNoPayload, Inner: empty()}},
				name,
				{Id: id(3)},
			},
			write: func(w *bitWriter, refBits int) {
				w.put(2, 2)
				w.put(2, refBits)
				w.put(3, refBits)
			},
		},
		{
			name:    "a ranged offset above the sender's maximum",
			entries: []ir.TableVocabularyEntry{{Id: id(1), Kind: ir.TableKindI8, Shape: ranged}},
			write: func(w *bitWriter, _ int) {
				w.put(255, 8)
			},
			bad: true,
		},
		{
			name:    "a lawful ranged offset",
			entries: []ir.TableVocabularyEntry{{Id: id(1), Kind: ir.TableKindI8, Shape: ranged}},
			write: func(w *bitWriter, _ int) {
				w.put(227, 8)
			},
		},
		{
			name: "a nested body naming a field id twice",
			entries: []ir.TableVocabularyEntry{
				{Id: id(1), Kind: ir.TableKindTable},
				noPayload,
				{Id: id(3), Kind: ir.TableKindNoPayload},
			},
			write: func(w *bitWriter, refBits int) {
				w.put(2, refBits)
				w.put(2, refBits)
				w.put(0, refBits)
			},
			bad: true,
		},
		{
			name: "a nested body naming a reserved id",
			entries: []ir.TableVocabularyEntry{
				{Id: id(1), Kind: ir.TableKindTable},
				{Id: ir.TableMessageVocabularyWireId, Kind: ir.TableKindNoPayload},
			},
			write: func(w *bitWriter, refBits int) {
				w.put(2, refBits)
				w.put(0, refBits)
			},
			bad: true,
		},
		{
			name: "a lawful nested body",
			entries: []ir.TableVocabularyEntry{
				{Id: id(1), Kind: ir.TableKindTable},
				noPayload,
				{Id: id(3), Kind: ir.TableKindNoPayload},
			},
			write: func(w *bitWriter, refBits int) {
				w.put(2, refBits)
				w.put(3, refBits)
				w.put(0, refBits)
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			v := &Vocabulary{entries: c.entries, announced: true}
			refBits := v.RefBits()
			var w bitWriter
			w.put(0, 8) // M - 1
			w.put(1, refBits)
			c.write(&w, refBits)
			w.put(0, refBits)
			w.align()
			data := append([]byte{ir.TableWireMessageForm}, w.b...)

			var report tabletext.Report
			ok, err := DecodeMessage(m, m.New(root), data, v, &report)
			if c.bad {
				if ok || err == nil || !report.Refused {
					t.Fatalf("the damaged payload was accepted: ok=%v err=%v report=%+v", ok, err, report)
				}
				return
			}
			if !ok || err != nil || report.Refused || report.Unknown != 1 {
				t.Fatalf("the lawful payload was not skipped and counted: ok=%v err=%v report=%+v", ok, err, report)
			}
		})
	}
}
