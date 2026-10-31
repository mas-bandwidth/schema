package javatable

// messageform_unknown_reference — form 2 (message) load keeps the fields past an
// unknown reference (docs/SPEC-TABLES.md §3.3): a body whose refs meet an
// announced entry the reader cannot name skips it whole and keeps reading, so
// the fields that follow still land. The Go oracle (internal/tablewire) is the
// reference; the Java leg must answer the same report and re-encode the same
// value.
//
// The mutant is the wire-fuzz pass `reference #143` over backenddemo.MatchResult
// (seed 24845619678): one bit in the last player's run is flipped, so that
// player's body ends early and the trailing bits re-read as a second players
// occurrence whose entries the reader cannot name. The oracle decodes the
// players array in place, so the already-loaded fields survive the later
// malformed read; before the fix the Java load reset the array at the second
// occurrence and left it at its defaults (20 saved bytes) where the oracle kept
// every field (132 saved bytes).

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// mutantMatchUnknownRef is the wire-fuzz mutant of match_full_message.bin that
// isolates cause C9 (an unknown reference inside a MatchResult body). The
// batch is damaged, and DAMAGE DISCARDS THE WHOLE BATCH (docs/SPEC-TABLES.md
// §4, §3.3): the Java leg owes the refusal, malformed, and no counter or
// re-saved body is compared.
const mutantMatchUnknownRef = "02004640404040404040c021a00f00000000000020d20400217d00000000000000f928900420a80f000000000000206c05120184f50100000000000024b7403280c03e000000000000801818480810da07000000000000902903490182fb00000000000000026a203140781f00000000000040da0d240708f00300000000000088ce810401217e00000000000000383c90240000"

func TestJavaFixMessageUnknownReferenceRefused(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnitDir(t, root, "tables/backend")

	v := &tablewire.Vocabulary{}
	if err := v.AnnounceRead(ir.TableAnnouncement(u), &tabletext.Report{}); err != nil {
		t.Fatalf("announce: %v", err)
	}
	mutant := mustHex(t, mutantMatchUnknownRef)
	bodies, _ := tablewire.MessageCount(mutant, v)
	batch := make([]*tabletext.Instance, bodies)
	for i := range batch {
		batch[i] = tabletext.NewModel(u).New(u.Tables["MatchResult"])
	}
	rep := &tabletext.Report{}
	if _, _, derr := tablewire.DecodeMessages(tabletext.NewModel(u), batch, mutant, v, rep); derr != nil || !rep.Malformed {
		t.Fatalf("oracle does not read the batch as damaged: %v %+v", derr, rep)
	}

	ann, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "tables", "backend_conn.bin"))
	if err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`
        {
            final byte[] ann = fromHex(%q);
            final byte[] msg = fromHex(%q);
            final TableVocabulary vocab = new TableVocabulary();
            vocab.reset(4096);
            final TableVocabulary.Report arep = new TableVocabulary.Report();
            final boolean announced = TableVocabulary.announceRead(vocab, ann, arep);
            final MatchResultFixed.Value[] values = new MatchResultFixed.Value[TableMessage.batchMax];
            for (int i = 0; i < values.length; i++) { values[i] = new MatchResultFixed.Value(); }
            final TableVocabulary.Report lrep = new TableVocabulary.Report();
            final int count = MatchResultMessage.loadMessages(values, values.length, vocab, msg, lrep);
            final int cc = count < 1 ? 1 : count;
            final TableVocabulary.Report mrep = new TableVocabulary.Report();
            final int measured = MatchResultMessage.measureMessages(values, cc, mrep);
            final byte[] buf = new byte[Math.max(measured, 0)];
            java.util.Arrays.fill(buf, (byte) 0xa5);
            final TableVocabulary.Report srep = new TableVocabulary.Report();
            final int wrote = MatchResultMessage.saveMessages(values, cc, buf, srep);
            if (!announced) { fail("announce failed"); }
            System.out.println("C9 unknown=" + lrep.unknown + " kind_mismatch=" + lrep.kindMismatch
                + " widened=" + lrep.widened + " clamped=" + lrep.clamped
                + " malformed=" + lrep.malformed
                + " save=" + (wrote < 0 ? "failed" : hex(java.util.Arrays.copyOf(buf, wrote))));
        }
`, hexOf(ann), mutantMatchUnknownRef)
	out := runMessageBatchProbe(t, u, body)
	line := ""
	for l := range strings.SplitSeq(out, "\n") {
		if strings.HasPrefix(l, "C9 unknown=") {
			line = l
			break
		}
	}
	if line == "" {
		t.Fatalf("no C9 report line in the probe output:\n%s", out)
	}
	if !strings.Contains(line, " malformed=true ") {
		t.Errorf("Java did not refuse a damaged batch: %s", line)
	}
}

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b := make([]byte, len(s)/2)
	for i := range b {
		var v int
		if _, err := fmt.Sscanf(s[2*i:2*i+2], "%02x", &v); err != nil {
			t.Fatalf("hex: %v", err)
		}
		b[i] = byte(v)
	}
	return b
}
