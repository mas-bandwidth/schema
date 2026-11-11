package javatable

// message_zero_width_run — a zero-width element under a wide count is not a
// walk (docs/SPEC-TABLES.md §3.3): the Java message-form skip of an announced
// entry the reader does not name costs work bounded by the batch's bits, and
// gives the engine's verdict. A walked element that consumes no bit finishes
// the rest of its run in one step, and a remaining count the bits left could
// not back is damage, exactly as internal/tablewire's message decode reads it.
//
// The announcement is built by hand the reference's way, so the shape under
// test is the announcement's own: an unbounded array whose element is itself
// an array that rides no bits, once as an empty inner array, once as an empty
// enum-keyed array, and once as a fixed inner array over a zero-width element.
// A small honest count in the same shape still loads with unknown = 1.

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const zeroWidthRunUnit = `package zerowidthrun

fixed table Probe
{
    x uint8
}
`

func TestJavaMessageZeroWidthRunIsBounded(t *testing.T) {
	t.Parallel()
	javaToolsRequired(t)
	u := unitFrom(t, zeroWidthRunUnit)
	m := tabletext.NewModel(u)
	wide := ir.TableMessageListMax
	zero := func() *ir.TableMessageShape { return &ir.TableMessageShape{} }
	rows := []struct {
		name   string
		shape  ir.TableMessageShape
		honest bool
	}{
		{
			name: "an empty inner array",
			shape: ir.TableMessageShape{Min: wide, Max: wide, Elem: ir.TableKindArray,
				Inner: &ir.TableMessageShape{Min: 0, Max: 0, Elem: 0, Inner: zero()}},
		},
		{
			name: "an empty enum-keyed inner array",
			shape: ir.TableMessageShape{Min: wide, Max: wide, Elem: ir.TableKindKeyed,
				Inner: &ir.TableMessageShape{Max: 0, Elem: ir.TableKindBool, Inner: zero()}},
		},
		{
			name: "a fixed inner array over a zero-width element",
			shape: ir.TableMessageShape{Min: wide, Max: wide, Elem: ir.TableKindArray,
				Inner: &ir.TableMessageShape{Min: 3, Max: 3, Elem: 0, Inner: zero()}},
		},
		{
			name: "a small honest count",
			shape: ir.TableMessageShape{Min: 2, Max: 2, Elem: ir.TableKindArray,
				Inner: &ir.TableMessageShape{Min: 0, Max: 0, Elem: 0, Inner: zero()}},
			honest: true,
		},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			entry := ir.TableVocabularyEntry{Id: 0x5a5a5a5a00000001, Kind: ir.TableKindArray, Shape: row.shape}
			ann := announcement(append(versionField(0x5a5a5a5a5a5a5a5a),
				vocabField(6, ir.TableVocabularyBytes([]ir.TableVocabularyEntry{entry}))...))
			batch := []byte{ir.TableWireMessageForm, 0, 0x01}

			v := &tablewire.Vocabulary{}
			var arep tabletext.Report
			if err := v.AnnounceRead(ann, &arep); err != nil {
				t.Fatalf("the engine refused the announcement: %v %+v", err, arep)
			}
			var rep tabletext.Report
			count, ok, err := tablewire.DecodeMessages(m,
				[]*tabletext.Instance{m.New(m.Lookup("Probe"))}, batch, v, &rep)
			engineAccepted := err == nil && ok
			if row.honest {
				if !engineAccepted || count != 1 || rep.Unknown != 1 {
					t.Fatalf("the engine's rule does not accept the honest run: count=%d ok=%v err=%v report=%+v",
						count, ok, err, rep)
				}
			} else if engineAccepted {
				t.Fatalf("the engine's rule accepts a zero-width run: count=%d report=%+v", count, rep)
			}

			body := fmt.Sprintf(`
        {
            final byte[] ann = fromHex(%q);
            final byte[] msg = fromHex(%q);
            final TableVocabulary vocab = new TableVocabulary();
            vocab.reset(4096);
            final TableVocabulary.Report arep = new TableVocabulary.Report();
            final boolean announced = TableVocabulary.announceRead(vocab, ann, arep);
            final ProbeFixed.Value[] values = new ProbeFixed.Value[1];
            values[0] = new ProbeFixed.Value();
            final TableVocabulary.Report lrep = new TableVocabulary.Report();
            final int count = ProbeMessage.loadMessages(values, values.length, vocab, msg, lrep);
            final boolean accepted = count == 1 && !lrep.damaged && !lrep.refused;
            System.out.println("ZWR accepted=" + accepted
                + " unknown=" + lrep.unknown
                + " damaged=" + lrep.damaged
                + " refused=" + lrep.refused
                + " count=" + count
                + " announced=" + announced);
        }
`, hexOf(ann), hexOf(batch))
			out := runMessageBatchProbeWithin(t, u, body, 10*time.Second)
			line := ""
			for l := range strings.SplitSeq(out, "\n") {
				if strings.HasPrefix(l, "ZWR ") {
					line = l
					break
				}
			}
			if line == "" {
				t.Fatalf("no ZWR report line in the probe output:\n%s", out)
			}
			javaAccepted := strings.Contains(line, "accepted=true")
			if javaAccepted != engineAccepted {
				t.Errorf("the Java verdict differs from the engine: Java %s", line)
			}
			if row.honest && !strings.Contains(line, "unknown=1") {
				t.Errorf("the honest run did not count its unknown entry: %s", line)
			}
		})
	}
}
