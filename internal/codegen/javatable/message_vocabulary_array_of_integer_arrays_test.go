package javatable

// message_vocabulary_array_of_integer_arrays — an array of arrays over an
// inner integer is refused at the announcement (docs/SPEC-TABLES.md §3.3): no
// declaration produces a list of lists, so the engine refuses the shape before
// the inner integer's packing byte is read. The Java vocabulary reader gives
// the same verdict on the same bytes, whether or not that packing byte is
// present.
//
// The absent-inner-shape row is the one that bites: a reader that stops at the
// inner element kind accepts the truncated entry the engine refuses.

import (
	"fmt"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestJavaMessageVocabularyArrayOfIntegerArrays(t *testing.T) {
	t.Parallel()
	javaToolsRequired(t)

	u := unitFrom(t, `package nestedarray

fixed table Root
{
    a int32
}
`)

	entry := ir.TableVocabularyEntry{
		Id:   0x1111111111111111,
		Kind: ir.TableKindArray,
		Shape: ir.TableMessageShape{
			Min: 0, Max: 2, Elem: ir.TableKindArray,
			Inner: &ir.TableMessageShape{
				Min: 0, Max: 2, Elem: ir.TableKindI32,
				Inner: &ir.TableMessageShape{Packing: ir.TableMessageRaw},
			},
		},
	}
	wellFormed := ir.TableVocabularyBytes([]ir.TableVocabularyEntry{entry})
	// The inner int32's shape is the packing byte the engine's DecodeShape
	// reads. Without it the vocabulary ends at the inner element kind, which
	// the Java reader used to take as a whole entry.
	innerShapeAbsent := wellFormed[:len(wellFormed)-1]

	rows := []struct {
		name  string
		vocab []byte
	}{
		{name: "well formed", vocab: wellFormed},
		{name: "inner int32 shape absent", vocab: innerShapeAbsent},
	}

	hexes := make([]string, len(rows))
	for i, row := range rows {
		data := announcement(append(versionField(0x1122334455667788), vocabField(6, row.vocab)...))
		// STEP ONE: the engine on the same bytes is the reference, and it
		// refuses an array whose element is itself an array.
		var v tablewire.Vocabulary
		var report tabletext.Report
		if err := v.AnnounceRead(data, &report); err == nil || v.Announced() {
			t.Fatalf("%s: the engine accepted an array-of-arrays announcement: %+v", row.name, report)
		}
		hexes[i] = hexOf(data)
	}

	body := fmt.Sprintf(`
        final String[] annHex = { %q, %q };
        for (int i = 0; i < annHex.length; i++) {
            final TableVocabulary vocab = new TableVocabulary();
            vocab.reset(16);
            final TableVocabulary.Report rep = new TableVocabulary.Report();
            final boolean read = TableVocabulary.announceRead(vocab, fromHex(annHex[i]), rep);
            say("nested_" + i, !read && vocab.count == 0,
                "read=" + read + " count=" + vocab.count
                + " damaged=" + rep.damaged + " refused=" + rep.refused);
        }
`, hexes[0], hexes[1])

	out := runMessageBatchProbe(t, u, body)
	for i, row := range rows {
		id := fmt.Sprintf("nested_%d", i)
		if line := messageBatchLine(out, id); line != "BATCH "+id+" ok" {
			t.Errorf("%s: %s", row.name, line)
		}
	}
}
