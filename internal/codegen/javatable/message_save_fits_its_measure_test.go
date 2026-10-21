package javatable

// message_save_fits_its_measure — a Java message must SAVE into a buffer of
// exactly its MEASURED size, and refuse a buffer one byte smaller (docs/SPEC-
// TABLES.md §3.3). The Go port is the reference for the bytes: every body is
// encoded by internal/tablewire, the generated Java loads those bytes, measures
// them and re-saves them, so the save is checked byte for byte against Go.
//
// The regression this pins: saving a nested table whose fields all ride nothing
// wrote the field reference and the body's zero reference into the stream and
// then truncated them away — a transient write that overflowed a buffer of
// exactly the measured size (the write is discarded, so its overflow must be
// discarded with it). The measure walk never touches a buffer, so it could not
// see the transient overflow; a real save into an exact-size buffer could.

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestJavaMessageSaveFitsItsMeasure(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}

	type unitCase struct {
		dir    string
		tables []string          // all fixed roots, default values
		seeded map[string]string // table -> seed JSON, seeded values
	}
	units := []unitCase{
		{
			dir: "tables/messages",
			tables: []string{
				"User", "Script", "Selection", "InsertText", "RemoveText",
				"Edit", "OpenDocument", "SaveDocument", "Transaction", "ToolMessage",
			},
			seeded: map[string]string{
				"Selection":    `{"start":{"line":1,"column":2},"end":{"line":3,"column":4}}`,
				"InsertText":   `{"at":{"line":1,"column":2},"text":"hi"}`,
				"RemoveText":   `{"span":{"start":{"line":9,"column":8},"end":{"line":7,"column":6}}}`,
				"OpenDocument": `{"path":"a.txt","cursor":{"line":1,"column":2}}`,
				"ToolMessage":  `{"sequence":7}`,
			},
		},
		{
			dir:    "tables/examples",
			tables: []string{"PackConfig"},
			seeded: map[string]string{
				"PackConfig": `{"version":2}`,
			},
		},
	}

	for _, uc := range units {
		t.Run(filepath.Base(uc.dir), func(t *testing.T) {
			t.Parallel()
			u := loadUnitDir(t, root, uc.dir)
			m := tabletext.NewModel(u)
			announce := ir.TableAnnouncement(u)

			var body strings.Builder
			var ids []string
			for _, name := range uc.tables {
				st := m.Lookup(name)
				if st == nil {
					t.Fatalf("unit %s has no table %s", uc.dir, name)
				}
				def := m.New(st)
				defWire, err := tablewire.EncodeMessage(m, def)
				if err != nil {
					t.Fatalf("%s: encode default: %v", name, err)
				}
				id := name + "_default"
				body.WriteString(saveFitsBody(name, id, announce, defWire))
				ids = append(ids, id)

				if seed, ok := uc.seeded[name]; ok {
					seedInst := m.New(st)
					var rep tabletext.Report
					if !m.Read(seedInst, []byte(seed), &rep) || rep != (tabletext.Report{}) {
						t.Fatalf("%s: oracle did not read seed cleanly: %+v", name, rep)
					}
					seedWire, err := tablewire.EncodeMessage(m, seedInst)
					if err != nil {
						t.Fatalf("%s: encode seeded: %v", name, err)
					}
					id := name + "_seeded"
					body.WriteString(saveFitsBody(name, id, announce, seedWire))
					ids = append(ids, id)
				}
			}

			out := runMessageBatchProbe(t, u, body.String())
			for _, id := range ids {
				if line := messageBatchLine(out, id); line != "BATCH "+id+" ok" {
					t.Errorf("%s: %s", uc.dir, line)
				}
			}
		})
	}
}

// saveFitsBody emits a driver body that, for one message table, loads the Go
// reference batch, measures it, re-saves it into a buffer of exactly the
// measured size (must succeed and reproduce Go's bytes) and into a buffer one
// byte smaller (must return -1).
func saveFitsBody(cls, id string, announce, msg []byte) string {
	return fmt.Sprintf(`
        {
            final byte[] ann = fromHex(%q);
            final byte[] msg = fromHex(%q);
            final TableVocabulary vocab = new TableVocabulary();
            vocab.reset(4096);
            final TableVocabulary.Report arep = new TableVocabulary.Report();
            final boolean announced = TableVocabulary.announceRead(vocab, ann, arep);
            final %sFixed.Value[] values = new %sFixed.Value[TableMessage.batchMax];
            for (int i = 0; i < values.length; i++) { values[i] = new %sFixed.Value(); }
            final TableVocabulary.Report lrep = new TableVocabulary.Report();
            final int count = %sMessage.loadMessages(values, values.length, vocab, msg, lrep);
            final TableVocabulary.Report mrep = new TableVocabulary.Report();
            final int measured = %sMessage.measureMessages(values, count, mrep);
            final byte[] bufExact = new byte[measured];
            final TableVocabulary.Report srep = new TableVocabulary.Report();
            final int wroteExact = %sMessage.saveMessages(values, count, bufExact, srep);
            final boolean same = java.util.Arrays.equals(bufExact, msg);
            final byte[] bufShort = new byte[Math.max(0, measured - 1)];
            final int wroteShort = %sMessage.saveMessages(values, count, bufShort, new TableVocabulary.Report());
            say(%q, announced && !lrep.malformed && !lrep.refused && count == 1 && wroteExact == measured && same && wroteShort == -1,
                "announced=" + announced + " measured=" + measured + " wroteExact=" + wroteExact + " wroteShort=" + wroteShort + " same=" + same);
        }
`, hexOf(announce), hexOf(msg), cls, cls, cls, cls, cls, cls, cls, id)
}
