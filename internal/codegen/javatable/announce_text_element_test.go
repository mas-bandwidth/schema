package javatable

// announce_text_element — an announced array or keyed entry over element kind
// 12 is a shape like any other (docs/SPEC-TABLES.md §3.3): a string element's
// shape is the `max` its kind names, the announcement that carries it is read,
// and a skip steps such an element one at a time rather than by width.
//
// The reference is the engine (internal/tablewire.Vocabulary.AnnounceRead and
// DecodeMessages) on the same bytes. The Java leg must give the same verdict
// and land the same values.

import (
	"fmt"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// textElementAnnouncement wraps a vocabulary in the form-1 announcement the
// reserved ids frame (§3.3).
func textElementAnnouncement(entries []ir.TableVocabularyEntry) []byte {
	const version = 0x1122334455667788
	return announcement(append(versionField(version), vocabField(6, ir.TableVocabularyBytes(entries))...))
}

// textElementBitWriter is the batch's low-bit-first bit stream, spelled the
// way the wire and both readers spell it (docs/SPEC-TABLES.md §3.3).
type textElementBitWriter struct {
	buf  []byte
	bits int
}

func (w *textElementBitWriter) put(v uint64, n int) {
	for i := 0; i < n; i++ {
		if w.bits%8 == 0 {
			w.buf = append(w.buf, 0)
		}
		if (v>>uint(i))&1 == 1 {
			w.buf[w.bits/8] |= 1 << uint(w.bits%8)
		}
		w.bits++
	}
}

func (w *textElementBitWriter) align() {
	for w.bits%8 != 0 {
		w.put(0, 1)
	}
}

func TestJavaAnnounceReadsTextElement(t *testing.T) {
	t.Parallel()
	javaToolsRequired(t)

	u := unitFrom(t, `package textannounce

fixed table Root
{
    a int32
}
`)
	root := u.Tables["Root"]
	if root == nil || len(root.Fields) != 1 {
		t.Fatalf("the probe unit does not carry the expected root")
	}
	known := ir.TableFieldEntry(root.Fields[0])

	stringShape := func(max int64) *ir.TableMessageShape {
		return &ir.TableMessageShape{Max: max}
	}
	// An array over element kind 12, a keyed entry over element kind 12, and
	// an array of arrays whose inner element is kind 12: each is an announced
	// shape like any other and each carries that element's own `max` (§3.3).
	textEntries := []ir.TableVocabularyEntry{
		{
			Id:    0x1111111111111111,
			Kind:  ir.TableKindArray,
			Shape: ir.TableMessageShape{Min: 0, Max: 4, Elem: ir.TableKindString, Inner: stringShape(8)},
		},
		{
			Id:    0x2222222222222222,
			Kind:  ir.TableKindKeyed,
			Shape: ir.TableMessageShape{Max: 4, Elem: ir.TableKindString, Inner: stringShape(8)},
		},
		{
			Id:   0x3333333333333333,
			Kind: ir.TableKindArray,
			Shape: ir.TableMessageShape{
				Min: 0, Max: 2, Elem: ir.TableKindArray,
				Inner: &ir.TableMessageShape{Min: 0, Max: 2, Elem: ir.TableKindString, Inner: stringShape(8)},
			},
		},
	}
	textAnnounce := textElementAnnouncement(textEntries)

	// STEP ONE: the engine is the reference on these same bytes. A unit whose
	// vocabulary carries these shapes announces and reads them (§3.3).
	{
		var v tablewire.Vocabulary
		var report tabletext.Report
		if err := v.AnnounceRead(textAnnounce, &report); err != nil {
			t.Fatalf("the engine refused the text-element announcement: %v (%+v)", err, report)
		}
		if !v.Announced() || report.Refused {
			t.Fatalf("the engine did not read the text-element announcement: %+v", report)
		}
		if got := len(v.Entries()); got != len(textEntries) {
			t.Fatalf("the engine read %d entries, want %d", got, len(textEntries))
		}
	}

	// A batch whose body names an entry the reader cannot name, two string
	// elements long, then a known field: the unknown is counted once and the
	// known field lands on the same value the engine decodes.
	unknown := textEntries[0]
	entries := []ir.TableVocabularyEntry{unknown, known}
	batchAnnounce := textElementAnnouncement(entries)
	refBits := ir.TableMessageRefBits(len(entries))
	countBits := ir.TableMessageBitsRequired(0, unknown.Shape.Max)
	stringBits := ir.TableMessageBitsRequired(0, unknown.Shape.Inner.Max)

	w := &textElementBitWriter{}
	w.put(1, refBits) // the unknown text list
	w.put(2, countBits)
	for _, text := range []string{"hi", "yo"} {
		w.put(uint64(len(text)), stringBits)
		w.align()
		for _, c := range []byte(text) {
			w.put(uint64(c), 8)
		}
	}
	w.put(2, refBits) // the known field
	w.put(0x12345678, 32)
	w.put(0, refBits) // the body's zero reference
	w.align()
	batch := append([]byte{ir.TableWireMessageForm, 0}, w.buf...)

	// The engine's decode of the same batch: one body, one unknown, the known
	// field's value.
	{
		m := tabletext.NewModel(u)
		insts := []*tabletext.Instance{m.New(m.Lookup("Root"))}
		var v tablewire.Vocabulary
		var report tabletext.Report
		if err := v.AnnounceRead(batchAnnounce, &report); err != nil {
			t.Fatalf("the engine refused the batch announcement: %v", err)
		}
		var decodeReport tabletext.Report
		count, ok, err := tablewire.DecodeMessages(m, insts, batch, &v, &decodeReport)
		if err != nil || !ok || count != 1 {
			t.Fatalf("the engine did not decode the text-element batch: count=%d ok=%t err=%v report=%+v", count, ok, err, decodeReport)
		}
		if decodeReport.Unknown != 1 || decodeReport.Refused {
			t.Fatalf("the engine's decode counted %d unknowns (refused=%t), want 1", decodeReport.Unknown, decodeReport.Refused)
		}
		if got := insts[0].Fields[0].Cell.I; got != 0x12345678 {
			t.Fatalf("the engine decoded %s = %d, want %d", root.Fields[0].Name, got, 0x12345678)
		}
	}

	body := fmt.Sprintf(`
        {
            final byte[] ann = fromHex(%q);
            final TableVocabulary vocab = new TableVocabulary();
            vocab.reset(16);
            final TableVocabulary.Report rep = new TableVocabulary.Report();
            final boolean read = TableVocabulary.announceRead(vocab, ann, rep);
            final boolean shapes = vocab.count == 3
                && vocab.entries[0].kind == 14 && vocab.entries[0].element != null
                && vocab.entries[0].element.kind == 12 && vocab.entries[0].element.max == 8
                && vocab.entries[0].shape.max == 4
                && vocab.entries[1].kind == 16 && vocab.entries[1].element != null
                && vocab.entries[1].element.kind == 12 && vocab.entries[1].element.max == 8
                && vocab.entries[1].shape.max == 4
                && vocab.entries[2].kind == 14 && vocab.entries[2].element != null
                && vocab.entries[2].element.kind == 14 && vocab.entries[2].element.max == 2
                && vocab.entries[2].shape.max == 2;
            say("text_announce", read && !rep.damaged && !rep.refused && shapes,
                "read=" + read + " damaged=" + rep.damaged + " refused=" + rep.refused
                + " count=" + vocab.count);
        }
        {
            final byte[] ann = fromHex(%q);
            final byte[] batch = fromHex(%q);
            final TableVocabulary vocab = new TableVocabulary();
            vocab.reset(16);
            final TableVocabulary.Report arep = new TableVocabulary.Report();
            final boolean announced = TableVocabulary.announceRead(vocab, ann, arep);
            final RootFixed.Value[] values = new RootFixed.Value[] { new RootFixed.Value() };
            final TableVocabulary.Report rep = new TableVocabulary.Report();
            final int count = RootMessage.loadMessages(values, values.length, vocab, batch, rep);
            say("text_batch", announced && !arep.damaged && !arep.refused
                && !rep.damaged && !rep.refused && count == 1 && rep.unknown == 1
                && values[0].a == 0x12345678,
                "announced=" + announced + " count=" + count + " unknown=" + rep.unknown
                + " damaged=" + rep.damaged + " refused=" + rep.refused
                + " a=" + values[0].a);
        }
`, hexOf(textAnnounce), hexOf(batchAnnounce), hexOf(batch))

	out := runMessageBatchProbe(t, u, body)
	for _, id := range []string{"text_announce", "text_batch"} {
		if line := messageBatchLine(out, id); line != "BATCH "+id+" ok" {
			t.Fatalf("%s: %s", id, line)
		}
	}
}
