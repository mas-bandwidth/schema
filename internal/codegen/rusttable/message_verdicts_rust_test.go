package rusttable

// Two Rust message-form verdicts, held to the Go oracle (internal/tablewire):
//
//   - A value outside its declared range, a bits(N) value past its width,
//     is refused whole with no value landed (docs/SPEC-TABLES.md §3.3). Only
//     the verdict is held to the oracle; which reason each names is
//     per-language detail (docs/rulings.json refusal-verdict-only).
//   - A counted array [Min..N]T of struct or union elements elides exactly
//     when the oracle's messageElides does: count 0, or count Min with every
//     element at its default (a struct whose body is its terminator alone, a
//     union at None). The message read refuses the same spelling stored.
//     A text list is a variable-table construct and cannot be declared in a
//     fixed table, so the message form has no text-element cell.
//
// A FUNCTIONAL TEST: it compiles and runs Rust with rustc. No clock, no
// socket, no network.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

const verdictRangeReaderSrc = `package verdict

fixed table Root
{
    ticks uint32
    level uint8 | min = 0, max = 100
    temp  float32 | min = -1, max = 1, resolution = 0.01
    flags bits(4)
}
`

// verdictRangeWriterSrc is the record at another build: no range, a plain
// float, and a wider bits field, so each can spell a value
// the reader's declaration does not hold.
const verdictRangeWriterSrc = `package verdict

fixed table Root
{
    ticks uint32
    level uint8
    temp  float32
    flags bits(8)
}
`

func TestRustMessageOutOfRangeRefusedByName(t *testing.T) {
	t.Parallel()
	messageBatchRustc(t)

	u := unitFrom(t, verdictRangeReaderSrc)
	m := tabletext.NewModel(u)
	writer := unitFrom(t, verdictRangeWriterSrc)
	mw := tabletext.NewModel(writer)
	wvoc := repeatedIdVocabulary(t, writer)
	ann := tablewire.Announce(writer)

	inRange := repeatedIdEncode(t, mw, "Root", `{"ticks":5,"level":50,"temp":0.5,"flags":3}`)
	repeatedIdEngineCase(t, m, "Root", "in_range", inRange, wvoc, true, 5)
	cases := []struct{ id, text string }{
		{"level_past_max", `{"ticks":5,"level":150}`},
		{"temp_past_max", `{"ticks":5,"temp":2.0}`},
		{"flags_past_width", `{"ticks":5,"flags":200}`},
	}
	head := fmt.Sprintf("    const ANN: [u8; %d] = [%s];\n", len(ann), rustBytes(ann)) +
		fmt.Sprintf("    const IN_RANGE: [u8; %d] = [%s];\n", len(inRange), rustBytes(inRange))
	body := repeatedIdRustCase("in_range", "ANN", "IN_RANGE", inRange, true, 5)
	for i, c := range cases {
		wire := repeatedIdEncode(t, mw, "Root", c.text)
		inst := m.New(m.Lookup("Root"))
		vv := wvoc
		var rep tabletext.Report
		// the oracle's VERDICT (docs/rulings.json refusal-verdict-only):
		// refused; why is its own diagnostic, never Rust's
		_, ok, err := tablewire.DecodeMessages(m, []*tabletext.Instance{inst}, wire, &vv, &rep)
		if ok || !tablewire.ReadRefusal(err) {
			t.Fatalf("%s: the oracle answered ok=%v err=%v, want a refusal", c.id, ok, err)
		}
		t.Logf("%s: the oracle refused %s", c.id, tablewire.RefusalReason(err))
		name := fmt.Sprintf("W%d", i)
		head += fmt.Sprintf("    const %s: [u8; %d] = [%s];\n", name, len(wire), rustBytes(wire))
		body += fmt.Sprintf(`
    {
        let mut storage = vec![TableMessageEntry::default(); 64];
        let mut vocab = TableVocabulary::new(&mut storage);
        let announced = announce_read(&mut vocab, &ANN, &mut TableReport::default());
        let mut values = vec![RootRow::default(); 2];
        values[0].ticks = 9;
        let mut lrep = TableReport::default();
        let (count, ok) = root_load_messages(&mut values, &vocab, &%s, &mut lrep);
        say(%q, announced && !ok && count == 0 && (lrep.refused || lrep.damaged)
            && values[0].ticks == root_message_default().ticks,
            format!("announced={announced} ok={ok} count={count} ticks={} report={lrep:?}", values[0].ticks));
    }
`, name, c.id)
	}
	out := runMessageBatchProbe(t, u, head+body)
	if line := messageBatchLine(out, "in_range"); line != "BATCH in_range ok" {
		t.Errorf("in_range: %s", line)
	}
	for _, c := range cases {
		if line := messageBatchLine(out, c.id); line != "BATCH "+c.id+" ok" {
			t.Errorf("%s: the Rust message read does not refuse the out-of-range value: %s", c.id, line)
		}
	}
}

const verdictCountedSrc = `package counted

type Inner
{
    a int32 = 3
}

union U
{
    x int32
    y int32
}

fixed table Root
{
    ticks uint32
    ps    [2..3]Inner
    us    [2..3]U
}
`

func TestRustMessageCountedBornElidesByElementDefault(t *testing.T) {
	t.Parallel()
	messageBatchRustc(t)

	u := unitFrom(t, verdictCountedSrc)
	m := tabletext.NewModel(u)
	voc := repeatedIdVocabulary(t, u)
	ann := tablewire.Announce(u)
	root := m.Lookup("Root")

	// The writer: Rust's bytes are the engine's for each value.
	saves := []struct{ id, text, set string }{
		{"ps_born", `{"ticks":5,"ps":[{},{}]}`, "v.ps_count = 2; v.ps[0] = inner_message_default(); v.ps[1] = inner_message_default();"},
		{"ps_live", `{"ticks":5,"ps":[{"a":4},{}]}`, "v.ps_count = 2; v.ps[0] = inner_message_default(); v.ps[1] = inner_message_default(); v.ps[0].a = 4;"},
		{"us_born", `{"ticks":5,"us":[{},{}]}`, "v.us_count = 2;"},
		{"us_arm_zero", `{"ticks":5,"us":[{"x":0},{}]}`, "v.us_count = 2; v.us[0].set_x(0);"},
	}
	head := fmt.Sprintf("    const ANN: [u8; %d] = [%s];\n", len(ann), rustBytes(ann))
	var body strings.Builder
	wires := map[string][]byte{}
	for i, c := range saves {
		wire := repeatedIdEncode(t, m, "Root", c.text)
		wires[c.id] = wire
		name := fmt.Sprintf("S%d", i)
		head += fmt.Sprintf("    const %s: [u8; %d] = [%s];\n", name, len(wire), rustBytes(wire))
		fmt.Fprintf(&body, `
    {
        let mut v = root_message_default();
        v.ticks = 5;
        %s
        let values = vec![v; 1];
        let mut buf = vec![0u8; 64];
        let wrote = root_save_messages(&values, &mut buf, &mut TableReport::default());
        let got = &buf[..wrote.max(0) as usize];
        say(%q, got == &%s[..], format!("wrote={wrote} got={got:02x?} want={:02x?}", %s));
    }
`, c.set, "save_"+c.id, name, name)
	}

	// The reader: the live spellings read, and the birth spelled out, at the
	// writer's own build, is a field stored at its default.
	for _, id := range []string{"ps_live", "us_arm_zero"} {
		repeatedIdEngineCase(t, m, "Root", id, wires[id], voc, true, 5)
		body.WriteString(repeatedIdRustCase("read_"+id, "ANN", constFor(saves, id), wires[id], true, 5))
	}
	// ps: element 0's one field (reference, value) removed, so both elements
	// are bodies that are their terminator alone.
	ps := wires["ps_live"]
	psSpots := tablewire.MessageSpots(m, root, ps, &voc)
	if len(psSpots) != 6 || psSpots[3].Value != 0 || psSpots[4].Value != 0 || psSpots[5].Value != 0 {
		t.Fatalf("ps_live: unexpected reference spots %+v", psSpots)
	}
	psBirth := repeatedIdBatch(
		repeatedIdSpan{ps, 16, 8 + psSpots[2].Off},
		repeatedIdSpan{ps, 8 + psSpots[3].Off, 8 + psSpots[5].Off + psSpots[5].Width},
	)
	// us: element 0's arm reference set to 0 and its payload removed, so both
	// elements are None.
	us := wires["us_arm_zero"]
	usSpots := tablewire.MessageSpots(m, root, us, &voc)
	if len(usSpots) != 5 || usSpots[3].Value != 0 || usSpots[4].Value != 0 {
		t.Fatalf("us_arm_zero: unexpected reference spots %+v", usSpots)
	}
	zero := make([]byte, (usSpots[2].Width+7)/8+1)
	usBirth := repeatedIdBatch(
		repeatedIdSpan{us, 16, 8 + usSpots[2].Off},
		repeatedIdSpan{zero, 0, usSpots[2].Width},
		repeatedIdSpan{us, 8 + usSpots[3].Off, 8 + usSpots[4].Off + usSpots[4].Width},
	)
	for _, c := range []struct {
		id   string
		wire []byte
	}{{"ps_stored_at_birth", psBirth}, {"us_stored_at_birth", usBirth}} {
		repeatedIdEngineCase(t, m, "Root", c.id, c.wire, voc, false, 0)
		name := strings.ToUpper(c.id)
		head += fmt.Sprintf("    const %s: [u8; %d] = [%s];\n", name, len(c.wire), rustBytes(c.wire))
		body.WriteString(repeatedIdRustCase(c.id, "ANN", name, c.wire, false, 0))
	}

	out := runMessageBatchProbe(t, u, head+body.String())
	ids := []string{"read_ps_live", "read_us_arm_zero", "ps_stored_at_birth", "us_stored_at_birth"}
	for _, c := range saves {
		ids = append(ids, "save_"+c.id)
	}
	for _, id := range ids {
		if line := messageBatchLine(out, id); line != "BATCH "+id+" ok" {
			t.Errorf("%s: the Rust counted array does not elide by its elements' defaults: %s", id, line)
		}
	}
}

func constFor(saves []struct{ id, text, set string }, id string) string {
	for i, c := range saves {
		if c.id == id {
			return fmt.Sprintf("S%d", i)
		}
	}
	return "MISSING"
}
