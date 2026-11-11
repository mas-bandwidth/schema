package rusttable

// TestRustMessageBodyRepeatedFieldRefuses holds the message form's body to the
// rule that a field id, known or unknown, named twice in one body is damage,
// and that a keyed slot named twice in one keyed array is damage
// (docs/SPEC-TABLES.md §3.2, §3.3). The Go engine (internal/tablewire) decodes
// the same bytes in this test and its verdict is the oracle's: a repeated id
// is wire_damaged, and the same body with the repeat removed loads the first
// value.
//
// A FUNCTIONAL TEST: it compiles and runs Rust with rustc. It skips by name
// when rustc is absent and SCHEMA_REQUIRE_CORPUS is unset, and fails by name
// when SCHEMA_REQUIRE_CORPUS=1, as make tables-rust-message-batch runs it. The
// Go half reads only the generator's output and the oracle: no clock, no
// socket, no network.

import (
	"fmt"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const rustRepeatedIdSrc = `package repeated

enum Slot { A, B, C }

fixed table Root
{
    ticks uint32
    by    [Slot]uint32
}
`

// rustRepeatedIdStranger is the same reader with one field it cannot name, so
// a body under the stranger's announcement carries an id this build skips.
const rustRepeatedIdStrangerSrc = `package repeated

enum Slot { A, B, C }

fixed table Root
{
    ticks uint32
    by    [Slot]uint32
    extra uint32
}
`

func TestRustMessageBodyRepeatedFieldRefuses(t *testing.T) {
	t.Parallel()
	messageBatchRustc(t)

	u := unitFrom(t, rustRepeatedIdSrc)
	m := tabletext.NewModel(u)
	root := m.Lookup("Root")
	voc := repeatedIdVocabulary(t, u)

	// A BODY THAT NAMES ONE FIELD TWICE, 5 THEN 9 (the rule and the defect).
	once := repeatedIdEncode(t, m, "Root", `{"ticks":5}`)
	second := repeatedIdEncode(t, m, "Root", `{"ticks":9}`)
	onceSpots := tablewire.MessageSpots(m, root, once, &voc)
	secondSpots := tablewire.MessageSpots(m, root, second, &voc)
	onceEnd := repeatedIdTerminator(t, onceSpots)
	secondEnd := repeatedIdTerminator(t, secondSpots)
	twice := repeatedIdBatch(
		repeatedIdSpan{once, 16, 8 + onceEnd.Off},
		repeatedIdSpan{second, 16, 8 + secondEnd.Off + secondEnd.Width},
	)

	// THE SAME BODY WITHOUT THE SECOND NAMING LOADS THE FIRST VALUE.
	repeatedIdEngineCase(t, m, "Root", "once", once, voc, true, 5)
	// THE REPEAT REFUSES.
	repeatedIdEngineCase(t, m, "Root", "twice", twice, voc, false, 0)

	// AN UNKNOWN FIELD NAMED TWICE REFUSES: the stranger's announcement names
	// an id this build cannot name, and the body carries it twice.
	stranger := unitFrom(t, rustRepeatedIdStrangerSrc)
	ms := tabletext.NewModel(stranger)
	svoc := repeatedIdVocabulary(t, stranger)
	unknown := repeatedIdEncode(t, ms, "Root", `{"extra":7}`)
	unknownSpots := tablewire.MessageSpots(ms, ms.Lookup("Root"), unknown, &svoc)
	unknownEnd := repeatedIdTerminator(t, unknownSpots)
	unknownTwice := repeatedIdBatch(
		repeatedIdSpan{unknown, 16, 8 + unknownEnd.Off},
		repeatedIdSpan{unknown, 16, 8 + unknownEnd.Off},
		repeatedIdSpan{unknown, 8 + unknownEnd.Off, 8 + unknownEnd.Off + unknownEnd.Width},
	)
	repeatedIdEngineCase(t, m, "Root", "unknown_twice", unknownTwice, svoc, false, 0)

	// A KEYED SLOT TWICE IN ONE KEYED ARRAY REFUSES: the second key's
	// reference is overwritten with the first's.
	keyed := repeatedIdEncode(t, m, "Root", `{"by":{"A":1,"B":2}}`)
	keyedSpots := tablewire.MessageSpots(m, root, keyed, &voc)
	if len(keyedSpots) < 4 {
		t.Fatalf("the oracle's scan found %d references in a keyed body, want the field, its two keys and the terminator: %+v", len(keyedSpots), keyedSpots)
	}
	keyedTwice := append([]byte(nil), keyed...)
	repeatedIdSetBits(keyedTwice, 8+keyedSpots[2].Off, keyedSpots[2].Width, keyedSpots[1].Value)
	repeatedIdEngineCase(t, m, "Root", "keyed_twice", keyedTwice, voc, false, 0)

	announce := tablewire.Announce(stranger)
	body := repeatedIdRustCase("once", "TABLE_ANNOUNCEMENT", "ONCE", once, true, 5) +
		repeatedIdRustCase("twice", "TABLE_ANNOUNCEMENT", "TWICE", twice, false, 0) +
		repeatedIdRustCase("unknown_twice", "STRANGER_ANNOUNCEMENT", "UNKNOWN_TWICE", unknownTwice, false, 0) +
		repeatedIdRustCase("keyed_twice", "TABLE_ANNOUNCEMENT", "KEYED_TWICE", keyedTwice, false, 0)
	head := fmt.Sprintf("    const STRANGER_ANNOUNCEMENT: [u8; %d] = [%s];\n", len(announce), rustBytes(announce)) +
		fmt.Sprintf("    const ONCE: [u8; %d] = [%s];\n", len(once), rustBytes(once)) +
		fmt.Sprintf("    const TWICE: [u8; %d] = [%s];\n", len(twice), rustBytes(twice)) +
		fmt.Sprintf("    const UNKNOWN_TWICE: [u8; %d] = [%s];\n", len(unknownTwice), rustBytes(unknownTwice)) +
		fmt.Sprintf("    const KEYED_TWICE: [u8; %d] = [%s];\n", len(keyedTwice), rustBytes(keyedTwice))
	out := runMessageBatchProbe(t, u, head+body)
	for _, id := range []string{"once", "twice", "unknown_twice", "keyed_twice"} {
		if line := messageBatchLine(out, id); line != "BATCH "+id+" ok" {
			t.Errorf("%s: the Rust body loader does not hold the repeated-id rule: %s", id, line)
		}
	}
}

func repeatedIdVocabulary(t *testing.T, u *ir.Unit) tablewire.Vocabulary {
	t.Helper()
	var v tablewire.Vocabulary
	var rep tabletext.Report
	if err := v.AnnounceRead(tablewire.Announce(u), &rep); err != nil {
		t.Fatalf("the oracle refused the announcement: %v", err)
	}
	return v
}

func repeatedIdEncode(t *testing.T, m *tabletext.Model, root, text string) []byte {
	t.Helper()
	inst := m.New(m.Lookup(root))
	var rep tabletext.Report
	if !m.Read(inst, []byte(text), &rep) || rep != (tabletext.Report{}) {
		t.Fatalf("the oracle did not read %s cleanly: %+v", text, rep)
	}
	wire, err := tablewire.EncodeMessage(m, inst)
	if err != nil {
		t.Fatalf("the oracle did not encode %s: %v", text, err)
	}
	return wire
}

// repeatedIdEngineCase decodes one body with the engine and requires the
// verdict the rule names; an accepted body must hold the first value.
func repeatedIdEngineCase(t *testing.T, m *tabletext.Model, root, what string, data []byte, v tablewire.Vocabulary, wantOK bool, wantTicks uint64) {
	t.Helper()
	inst := m.New(m.Lookup(root))
	vv := v
	var rep tabletext.Report
	_, ok, err := tablewire.DecodeMessages(m, []*tabletext.Instance{inst}, data, &vv, &rep)
	if ok != wantOK {
		t.Fatalf("%s: the oracle answered ok=%v, want %v: err=%v report=%+v", what, ok, wantOK, err, rep)
	}
	if !wantOK {
		if tablewire.RefusalReason(err) != tablewire.ReasonWireDamaged {
			t.Fatalf("%s: the oracle refused %q, want %q: %v", what, tablewire.RefusalReason(err), tablewire.ReasonWireDamaged, err)
		}
		return
	}
	if err != nil {
		t.Fatalf("%s: the oracle accepted with err=%v", what, err)
	}
	i, found := inst.FieldIndexByKey("ticks")
	if !found {
		t.Fatalf("%s: the oracle's root carries no ticks field", what)
	}
	if got := inst.Fields[i].Cell.U; got != wantTicks {
		t.Fatalf("%s: the oracle read ticks=%d, want %d", what, got, wantTicks)
	}
}

// repeatedIdRustCase is one probe block: announce, load, and print one line.
func repeatedIdRustCase(id, announceConst, wireConst string, data []byte, wantOK bool, wantTicks uint64) string {
	return fmt.Sprintf(`
    {
        let mut storage = vec![TableMessageEntry::default(); 4096];
        let mut vocab = TableVocabulary::new(&mut storage);
        let mut arep = TableReport::default();
        let announced = announce_read(&mut vocab, &%s, &mut arep);
        let mut values = vec![RootRow::default(); 2];
        let mut lrep = TableReport::default();
        let (count, ok) = root_load_messages(&mut values, &vocab, &%s, &mut lrep);
        let want_ok = %t;
        let same = if want_ok {
            ok && count == 1 && !lrep.damaged && !lrep.refused && values[0].ticks == %d
        } else {
            !ok && count == 0 && lrep.damaged && !lrep.refused
        };
        say(%q, announced && same,
            format!("announced={announced} ok={ok} count={count} ticks={} report={lrep:?}", values[0].ticks));
    }
`, announceConst, wireConst, wantOK, wantTicks, id)
}

// repeatedIdTerminator is the zero reference that closes a body.
func repeatedIdTerminator(t *testing.T, spots []tablewire.BitSpot) tablewire.BitSpot {
	t.Helper()
	if len(spots) == 0 || spots[len(spots)-1].Value != 0 {
		t.Fatalf("the oracle's scan did not find the body's terminator: %+v", spots)
	}
	return spots[len(spots)-1]
}

type repeatedIdSpan struct {
	data     []byte
	from, to int
}

// repeatedIdBatch is a batch of one body from bit spans, zero-padded once.
func repeatedIdBatch(spans ...repeatedIdSpan) []byte {
	total := 0
	for _, s := range spans {
		total += s.to - s.from
	}
	out := make([]byte, 2+(total+7)/8+1)
	out[0] = ir.TableWireMessageForm
	out[1] = 0
	at := 16
	for _, s := range spans {
		for i := s.from; i < s.to; i++ {
			if (s.data[i/8]>>uint(i%8))&1 != 0 {
				out[at/8] |= 1 << uint(at%8)
			}
			at++
		}
	}
	return out[:(at+7)/8]
}

func repeatedIdSetBits(buf []byte, off, width int, value uint64) {
	for i := 0; i < width; i++ {
		if (value>>uint(i))&1 != 0 {
			buf[(off+i)/8] |= 1 << uint((off+i)%8)
		} else {
			buf[(off+i)/8] &^= 1 << uint((off+i)%8)
		}
	}
}
