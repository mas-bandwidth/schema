package luautable_test

import (
	"encoding/hex"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v3/internal/tablewire"
	"github.com/mas-bandwidth/schema/v3/ir"
)

// luauMsgBits is the batch body's bit writer: least significant bit first.
type luauMsgBits struct {
	out []byte
	n   int
}

func (w *luauMsgBits) put(v uint64, bits int) {
	for i := range bits {
		if w.n%8 == 0 {
			w.out = append(w.out, 0)
		}
		if i < 64 && v>>uint(i)&1 != 0 {
			w.out[len(w.out)-1] |= 1 << uint(w.n%8)
		}
		w.n++
	}
}

// TestLuauMessageRangedValueHeldToTheAnnouncedKind pins the message form's
// cross-leg rules (1) and (2) of the 2026-11-21 review on the Luau table
// runtime (docs/SPEC-TABLES.md §3.3). A ranged integer read by name is base
// plus offset held to the announced kind's domain, never wrapped: a sum past
// it is refused, for 8, 64 and 128-bit kinds and where the reader is wider
// than the announced kind. The skip path holds the same sum and calls it
// damage. The lawful twin of every case reads. The reader and writer
// schemas, the probe, every case and its batch bytes are the committed corpus
// testdata/review1121/message-ranged.
func TestLuauMessageRangedValueHeldToTheAnnouncedKind(t *testing.T) {
	t.Parallel()
	corpus := filepath.Join(luauReview1121, "message-ranged")
	rows := review1121Rows(t, filepath.Join(corpus, "cases.tsv"), 5)
	reader := review1121Unit(t, filepath.Join(corpus, "reader", "Probe.schema"))
	writer := review1121Unit(t, filepath.Join(corpus, "writer", "Probe.schema"))
	dir := review1121Modules(t, reader, filepath.Join(corpus, "probe.luau"))

	// the committed batch is a body holding the field at its offset in its
	// three bits (the high half of a 128-bit kind's offset is zero there
	// too), then the terminator: rebuilt here under the writer's vocabulary,
	// so a fixture that drifts from its schema is refused
	vocab := tablewire.MessageVocabulary(writer)
	refBits := ir.TableMessageRefBits(len(vocab))
	field := map[string]*ir.Field{}
	for _, f := range writer.Tables["Msg"].Fields {
		field[f.Name] = f
	}
	slot := func(name string) uint64 {
		e := ir.TableFieldEntry(field[name])
		for i, v := range vocab {
			if v.Key() == e.Key() {
				return uint64(i + 1)
			}
		}
		t.Fatalf("field %s outside the writer's vocabulary", name)
		return 0
	}
	ranged := func(name string, offset uint64) string {
		f := field[name]
		if f == nil {
			t.Fatalf("field %s is not the writer's", name)
		}
		e := ir.TableFieldEntry(f)
		if bits := ir.TableMessageValueBits(e.Kind, e.Shape); bits != 3 {
			t.Fatalf("field %s rides %d bits, want 3", name, bits)
		}
		w := &luauMsgBits{}
		w.put(slot(name), refBits)
		w.put(offset, 3)
		w.put(0, refBits) // the body's terminator
		return hex.EncodeToString(append([]byte{2, 0}, w.out...))
	}
	read, refused := 0, 0
	for _, r := range rows {
		offset, err := strconv.ParseUint(r[2], 10, 3)
		if err != nil {
			t.Fatalf("%s: offset %s: %v", r[0], r[2], err)
		}
		if b := ranged(r[1], offset); b != r[3] {
			t.Errorf("%s: field %s at offset %d is the batch %s, the corpus commits %s", r[0], r[1], offset, b, r[3])
		}
		if r[4] == "read" {
			read++
		} else {
			refused++
		}
	}
	if read == 0 || refused == 0 {
		t.Fatalf("the corpus holds %d read and %d refused rows: each is proven beside the other", read, refused)
	}
	raw := luaurun.Run(t, dir, "main.luau", map[string][]byte{
		"announce": tablewire.Announce(writer),
		"cases":    review1121CaseLines(rows),
	})
	got := review1121Verdicts(t, raw)
	for _, r := range rows {
		if got[r[0]] != r[4] {
			t.Errorf("%s: got %q, want %q", r[0], got[r[0]], r[4])
		}
	}
}
