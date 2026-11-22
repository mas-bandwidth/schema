package rusttable

// Two Rust message-form verdicts, held to the Go oracle (internal/tablewire):
//
//   - A value read by name outside its declared range, a bits(N) value past
//     its width is refused BY
//     NAME as out_of_range (docs/SPEC-TABLES.md §3.3), not as wire damage.
//   - A counted array [Min..N]T of struct or union elements elides exactly
//     when the oracle's messageElides does: count 0, or count Min with every
//     element at its default (a struct whose body is its terminator alone, a
//     union at None). The message read refuses the same spelling stored.
//     A text list is a variable-table construct and cannot be declared in a
//     fixed table, so the message form has no text-element cell.
//
// The schemas, values and verdicts are the committed corpus
// testdata/message-verdicts (range/ and counted/), one row per case, each
// refusal beside its lawful control.
//
// A FUNCTIONAL TEST: it compiles and runs Rust with rustc. No clock, no
// socket, no network.

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// verdictCorpusDir is the committed message-verdict corpus.
const verdictCorpusDir = "testdata/message-verdicts"

// verdictSchema reads one committed schema of the corpus.
func verdictSchema(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(verdictCorpusDir, rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// verdictRows reads one committed cases.tsv of the corpus: every row that is
// not blank or a # comment, split on tabs, each with exactly want fields.
func verdictRows(t *testing.T, rel string, want int) [][]string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(verdictCorpusDir, rel))
	if err != nil {
		t.Fatal(err)
	}
	var rows [][]string
	for n, line := range strings.Split(string(b), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != want {
			t.Fatalf("%s line %d: %d fields, want %d", rel, n+1, len(f), want)
		}
		rows = append(rows, f)
	}
	if len(rows) == 0 {
		t.Fatalf("%s: no cases", rel)
	}
	return rows
}

// verdictTicks is a row's ticks column: the first field a read must hold.
func verdictTicks(t *testing.T, id, s string) uint64 {
	t.Helper()
	v, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		t.Fatalf("%s: ticks %q: %v", id, s, err)
	}
	return v
}

func TestRustMessageOutOfRangeRefusedByName(t *testing.T) {
	t.Parallel()
	messageBatchRustc(t)

	u := unitFrom(t, verdictSchema(t, "range/reader.schema"))
	m := tabletext.NewModel(u)
	writer := unitFrom(t, verdictSchema(t, "range/writer.schema"))
	mw := tabletext.NewModel(writer)
	wvoc := repeatedIdVocabulary(t, writer)
	ann := tablewire.Announce(writer)

	rows := verdictRows(t, "range/cases.tsv", 4)
	head := fmt.Sprintf("    const ANN: [u8; %d] = [%s];\n", len(ann), rustBytes(ann))
	var body strings.Builder
	refusals, reads := 0, 0
	for i, r := range rows {
		id, text, verdict := r[0], r[1], r[2]
		wire := repeatedIdEncode(t, mw, "Root", text)
		name := fmt.Sprintf("W%d", i)
		head += fmt.Sprintf("    const %s: [u8; %d] = [%s];\n", name, len(wire), rustBytes(wire))
		switch verdict {
		case "read":
			reads++
			ticks := verdictTicks(t, id, r[3])
			repeatedIdEngineCase(t, m, "Root", id, wire, wvoc, true, ticks)
			body.WriteString(repeatedIdRustCase(id, "ANN", name, wire, true, ticks))
		case "out_of_range":
			refusals++
			inst := m.New(m.Lookup("Root"))
			vv := wvoc
			var rep tabletext.Report
			if _, ok, err := tablewire.DecodeMessages(m, []*tabletext.Instance{inst}, wire, &vv, &rep); ok || tablewire.RefusalReason(err) != tablewire.ReasonOutOfRange {
				t.Fatalf("%s: the oracle answered ok=%v err=%v, want the out_of_range refusal", id, ok, err)
			}
			fmt.Fprintf(&body, `
    {
        let mut storage = vec![TableMessageEntry::default(); 64];
        let mut vocab = TableVocabulary::new(&mut storage);
        let announced = announce_read(&mut vocab, &ANN, &mut TableReport::default());
        let mut values = vec![RootRow::default(); 2];
        values[0].ticks = 9;
        let mut lrep = TableReport::default();
        let (count, ok) = root_load_messages(&mut values, &vocab, &%s, &mut lrep);
        say(%q, announced && !ok && count == 0 && lrep.refused && !lrep.damaged
            && matches!(lrep.reason, TableReason::OutOfRange) && values[0].ticks == root_message_default().ticks,
            format!("announced={announced} ok={ok} count={count} ticks={} report={lrep:?}", values[0].ticks));
    }
`, name, id)
		default:
			t.Fatalf("range/cases.tsv %s: verdict %q", id, verdict)
		}
	}
	if refusals == 0 || reads == 0 {
		t.Fatalf("range/cases.tsv: %d refusals, %d lawful reads; the corpus owes both", refusals, reads)
	}
	out := runMessageBatchProbe(t, u, head+body.String())
	for _, r := range rows {
		if line := messageBatchLine(out, r[0]); line != "BATCH "+r[0]+" ok" {
			t.Errorf("%s (%s): the Rust message read does not answer as the oracle: %s", r[0], r[2], line)
		}
	}
}

func TestRustMessageCountedBornElidesByElementDefault(t *testing.T) {
	t.Parallel()
	messageBatchRustc(t)

	u := unitFrom(t, verdictSchema(t, "counted/Root.schema"))
	m := tabletext.NewModel(u)
	voc := repeatedIdVocabulary(t, u)
	ann := tablewire.Announce(u)
	root := m.Lookup("Root")

	rows := verdictRows(t, "counted/cases.tsv", 6)
	head := fmt.Sprintf("    const ANN: [u8; %d] = [%s];\n", len(ann), rustBytes(ann))
	var body strings.Builder
	wires := map[string][]byte{}
	consts := map[string]string{}
	var ids []string
	kinds := map[string]int{}
	for i, r := range rows {
		id, kind, source, set, verdict := r[0], r[1], r[2], r[3], r[4]
		kinds[kind]++
		ids = append(ids, id)
		switch kind {
		case "save":
			// The writer: Rust's bytes are the engine's for each value.
			if verdict != "bytes" {
				t.Fatalf("counted/cases.tsv %s: a save row's verdict is bytes, not %q", id, verdict)
			}
			wire := repeatedIdEncode(t, m, "Root", source)
			wires[id] = wire
			name := fmt.Sprintf("S%d", i)
			consts[id] = name
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
`, set, id, name, name)
		case "read":
			// The reader: the live spellings read.
			wire, ok := wires[source]
			if !ok || verdict != "read" {
				t.Fatalf("counted/cases.tsv %s: a read row names an earlier save row and reads (source %q, verdict %q)", id, source, verdict)
			}
			ticks := verdictTicks(t, id, r[5])
			repeatedIdEngineCase(t, m, "Root", id, wire, voc, true, ticks)
			body.WriteString(repeatedIdRustCase(id, "ANN", consts[source], wire, true, ticks))
		case "birth-drop-field", "birth-zero-arm":
			// The birth spelled out, at the writer's own build, is a field
			// stored at its default.
			src, ok := wires[source]
			if !ok || verdict != "wire_damaged" {
				t.Fatalf("counted/cases.tsv %s: a birth row names an earlier save row and is wire_damaged (source %q, verdict %q)", id, source, verdict)
			}
			var wire []byte
			spots := tablewire.MessageSpots(m, root, src, &voc)
			if kind == "birth-drop-field" {
				// element 0's one field (reference, value) removed, so both
				// elements are bodies that are their terminator alone.
				if len(spots) != 6 || spots[3].Value != 0 || spots[4].Value != 0 || spots[5].Value != 0 {
					t.Fatalf("%s: unexpected reference spots in %s: %+v", id, source, spots)
				}
				wire = repeatedIdBatch(
					repeatedIdSpan{src, 16, 8 + spots[2].Off},
					repeatedIdSpan{src, 8 + spots[3].Off, 8 + spots[5].Off + spots[5].Width},
				)
			} else {
				// element 0's arm reference set to 0 and its payload
				// removed, so both elements are None.
				if len(spots) != 5 || spots[3].Value != 0 || spots[4].Value != 0 {
					t.Fatalf("%s: unexpected reference spots in %s: %+v", id, source, spots)
				}
				zero := make([]byte, (spots[2].Width+7)/8+1)
				wire = repeatedIdBatch(
					repeatedIdSpan{src, 16, 8 + spots[2].Off},
					repeatedIdSpan{zero, 0, spots[2].Width},
					repeatedIdSpan{src, 8 + spots[3].Off, 8 + spots[4].Off + spots[4].Width},
				)
			}
			repeatedIdEngineCase(t, m, "Root", id, wire, voc, false, 0)
			name := strings.ToUpper(id)
			head += fmt.Sprintf("    const %s: [u8; %d] = [%s];\n", name, len(wire), rustBytes(wire))
			body.WriteString(repeatedIdRustCase(id, "ANN", name, wire, false, 0))
		default:
			t.Fatalf("counted/cases.tsv %s: kind %q", id, kind)
		}
	}
	if kinds["save"] == 0 || kinds["read"] == 0 || kinds["birth-drop-field"] == 0 || kinds["birth-zero-arm"] == 0 {
		t.Fatalf("counted/cases.tsv: kinds %v; the corpus owes saves, lawful reads and both births", kinds)
	}

	out := runMessageBatchProbe(t, u, head+body.String())
	for _, id := range ids {
		if line := messageBatchLine(out, id); line != "BATCH "+id+" ok" {
			t.Errorf("%s: the Rust counted array does not elide by its elements' defaults: %s", id, line)
		}
	}
}
