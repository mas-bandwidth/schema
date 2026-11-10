package darttable

import (
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestMessageClampedCountDart pins that the Dart message report counts a
// clamped string exactly as tablewire.DecodeMessages does (docs/SPEC-TABLES.md
// §3.3): the wire fuzzer cause D11 found that a string clamped on load went
// uncounted where the Go reference counts it. The vector is the wire-fuzz
// mutant over backenddemo.LoginRequest, bit pass #84, run seed 24845619678:
// an over-long session_token that clamps to its declared bytes(32) bound. The
// Go oracle counts one clamped on those bytes; the Dart load must report the
// same.
func TestMessageClampedCountDart(t *testing.T) {
	t.Parallel()
	var row *batchRow
	for i := range batchManifest(t) {
		r := &batchManifest(t)[i]
		if r.name == "login_full" {
			row = r
			break
		}
	}
	if row == nil {
		t.Fatal("no login_full message row in the manifest")
	}
	u := batchUnit(t, row.path)
	st := findTable(t, u, row.root)
	if !st.IsTable || !st.FixedDeclared {
		t.Skipf("§3.3 is a fixed table's; %s is a variable table", row.root)
	}
	// The Go oracle answers the same bytes: decode the mutant with
	// tablewire.DecodeMessages and read its clamped count, so the Dart report
	// is compared to the reference, never to a literal.
	m := tabletext.NewModel(u)
	vocabulary := &tablewire.Vocabulary{}
	var oracle tabletext.Report
	announce := tablewire.Announce(m.Unit)
	if err := vocabulary.AnnounceRead(announce, &oracle); err != nil {
		t.Fatalf("the Go oracle refused the announcement: %v", err)
	}
	// STRICT VERDICT (docs/plans/strict-only.md decision 2): a fixed-string
	// payload past its bound refuses the whole batch read; the Go oracle
	// refuses the same bytes.
	loginMutant := loginOverBoundMutant(t, m.Unit)
	value := m.New(m.Lookup(row.root))
	_, _, err := tablewire.DecodeMessages(m, []*tabletext.Instance{value}, loginMutant, vocabulary, &oracle)
	if err == nil && !oracle.Refused {
		t.Fatalf("the Go oracle must refuse the mutant: report %+v", oracle)
	}
	out, err := runBatchProbe(t, u, row.root, messageClampedBody(announce, loginMutant))
	if err != nil {
		t.Fatalf("the clamped message count did not run green: %v\n%s", err, out)
	}
}

// loginOverBoundMutant is the D11 vector: a form-2 batch carrying one
// LoginRequest body whose session_token length overruns the declared
// bytes(32) bound by one, spelled against the unit's own announcement (its
// slot and its reference width), then the payload that length names, the
// body's terminator and the pad.
func loginOverBoundMutant(t *testing.T, u *ir.Unit) []byte {
	t.Helper()
	vocabulary := tablewire.MessageVocabulary(u)
	refBits := ir.TableMessageRefBits(len(vocabulary))
	slot, token := 0, ir.TableVocabularyEntry{}
	for i, e := range vocabulary {
		if e.Id == ir.TableWireId("session_token") {
			slot, token = i+1, e
		}
	}
	if slot == 0 {
		t.Fatal("the announcement carries no session_token")
	}
	over := uint64(token.Shape.Max) + 1
	countBits := ir.TableMessageCountBits(token.Shape)
	if over >= 1<<uint(countBits) {
		t.Fatalf("a %d-bit count cannot spell %d", countBits, over)
	}
	out := []byte{ir.TableWireMessageForm, 0}
	at := 16
	put := func(value uint64, width int) {
		for i := range width {
			if at/8 == len(out) {
				out = append(out, 0)
			}
			if value>>uint(i)&1 == 1 {
				out[at/8] |= 1 << uint(at%8)
			}
			at++
		}
	}
	put(uint64(slot), refBits)
	put(over, countBits)
	for at%8 != 0 {
		put(0, 1)
	}
	for range over {
		put(0x41, 8)
	}
	put(0, refBits)
	return out
}

func mustHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

// messageClampedBody loads the mutant with the generated Dart batch message
// load and asserts the strict refusal: no message lands and the report
// refuses length_over_bound (docs/plans/strict-only.md decision 2).
func messageClampedBody(announce, loginMutant []byte) string {
	return fmt.Sprintf(`
  final ann = Uint8List.fromList(%s);
  final msg = Uint8List.fromList(%s);
  final vocab = h.TableVocabulary(entries: List<h.TableMessageEntry>.generate(4096, (i) => h.TableMessageEntry(0, 0)));
  final annRep = h.TableReport();
  check(h.announceRead(vocab, ann, annRep), 'the connection announcement is refused: ' + annRep.reason);
  final values = <h.LoginRequest>[h.LoginRequest()];
  final rep = h.TableReport();
  final count = r.loginRequestLoadMessages(values, msg, vocab, rep);
  check(count == 0 && rep.refused && rep.reason == 'length_over_bound',
      'over-bound batch body refused: count=$count reason=${rep.reason}');
`, dartBytes(announce), dartBytes(loginMutant))
}
