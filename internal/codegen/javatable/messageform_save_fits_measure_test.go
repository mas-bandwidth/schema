package javatable

// TestJavaMessageSaveFitsItsMeasure pins the MESSAGE FORM's core invariant
// (docs/SPEC-TABLES.md §3.3, and §3.2 for the keyed unit): a message must SAVE
// into a buffer of EXACTLY its measured size — produce exactly those bytes,
// equal to the Go reference's — and refuse a buffer one byte smaller.
//
// The probe loads the Go reference's batch, measures it, saves it into a
// buffer of exactly measured() bytes and compares byte for byte, then saves
// into measured()-1 bytes and requires refusal (-1). Every fixed-table root of
// the keyed unit (tables/examples) and of the message unit (tables/messages)
// is exercised at its default value and at a seeded value.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

var messageFitSeeds = map[string]map[string]string{
	"tables/examples": {
		"TeamConfig":   `{"spawn_count": 5}`,
		"GunnerConfig": `{"tracking": true}`,
		"TurretConfig": `{"damage": 12.0}`,
		"HullConfig":   `{"health": 200.0}`,
		"KeyedConfig":  `{"teams": {"Red": {"spawn_count": 5}}}`,
	},
	"tables/messages": {
		"ToolMessage": `{"sequence": 9}`,
	},
}

func TestJavaMessageSaveFitsItsMeasure(t *testing.T) {
	t.Parallel()
	javaTools(t)
	root := "../../.."
	for _, dir := range []string{"tables/examples", "tables/messages"} {
		dir := dir
		t.Run(dir, func(t *testing.T) {
			t.Parallel()
			u := loadUnitDir(t, root, dir)
			m := tabletext.NewModel(u)
			announce := ir.TableAnnouncement(u)
			var body strings.Builder
			var ids []string
			for _, st := range fixedRoots(u) {
				name := st.Name
				// DEFAULT value: every field at its declared default, which is
				// the case that elides an all-default nested table (§3.3).
				defInst := m.New(m.Lookup(name))
				wire, err := tablewire.EncodeMessages(m, []*tabletext.Instance{defInst})
				if err != nil {
					t.Fatalf("%s default: oracle encode: %v", name, err)
				}
				id := name + "_default"
				body.WriteString(messageFitProbeBlock(id, name, announce, wire))
				ids = append(ids, id)
				if seed, ok := messageFitSeeds[dir][name]; ok {
					seedInst := m.New(m.Lookup(name))
					var rep tabletext.Report
					if !m.Read(seedInst, []byte(seed), &rep) || rep != (tabletext.Report{}) {
						t.Fatalf("%s seeded: oracle did not read the seed cleanly: %+v", name, rep)
					}
					wire, err = tablewire.EncodeMessages(m, []*tabletext.Instance{seedInst})
					if err != nil {
						t.Fatalf("%s seeded: oracle encode: %v", name, err)
					}
					id = name + "_seeded"
					body.WriteString(messageFitProbeBlock(id, name, announce, wire))
					ids = append(ids, id)
				}
			}
			if len(ids) == 0 {
				t.Fatal("no fixed-table message root found in " + dir)
			}
			out := runMessageBatchProbe(t, u, body.String())
			for _, id := range ids {
				if line := messageBatchLine(out, id); line != "BATCH "+id+" ok" {
					t.Errorf("%s/%s: %s", dir, id, line)
				}
			}
		})
	}
}

// messageFitProbeBlock emits one check: measure the loaded batch, save into a
// buffer of exactly measured() bytes (must succeed, equal the reference, and
// return measured), and save into measured()-1 bytes (must be refused, -1).
func messageFitProbeBlock(id, root string, announce, wire []byte) string {
	cls := ir.GoExportName(root)
	return fmt.Sprintf(`
        {
            final byte[] ann = fromHex(%q);
            final byte[] msg = fromHex(%q);
            final TableVocabulary vocab = new TableVocabulary();
            vocab.reset(4096);
            final boolean announced = TableVocabulary.announceRead(vocab, ann, new TableVocabulary.Report());
            final %sFixed.Value[] values = new %sFixed.Value[1];
            values[0] = new %sFixed.Value();
            final int count = %sMessage.loadMessages(values, values.length, vocab, msg, new TableVocabulary.Report());
            final TableVocabulary.Report mrep = new TableVocabulary.Report();
            final int measured = %sMessage.measureMessages(values, count, mrep);
            final byte[] exact = new byte[measured];
            java.util.Arrays.fill(exact, (byte) 0xa5);
            final TableVocabulary.Report srep = new TableVocabulary.Report();
            final int wrote = %sMessage.saveMessages(values, count, exact, srep);
            final boolean alike = java.util.Arrays.equals(exact, msg);
            final byte[] small = new byte[Math.max(0, measured - 1)];
            final int refused = %sMessage.saveMessages(values, count, small, new TableVocabulary.Report());
            say(%q, announced && count == 1 && measured == msg.length && wrote == measured && alike && refused == -1,
                "announced=" + announced + " count=" + count + " measured=" + measured + " msg=" + msg.length
                + " wrote=" + wrote + " alike=" + alike + " refused=" + refused);
        }
`, hexOf(announce), hexOf(wire),
		cls, cls, cls, cls, cls, cls, cls, id)
}
