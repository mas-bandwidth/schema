package darttable

import (
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
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
	value := m.New(m.Lookup(row.root))
	_, _, err := tablewire.DecodeMessages(m, []*tabletext.Instance{value}, loginMutant, vocabulary, &oracle)
	if err != nil {
		t.Fatalf("the Go oracle erred on the mutant: %v", err)
	}
	out, err := runBatchProbe(t, u, row.root, messageClampedBody(announce, oracle.Clamped))
	if err != nil {
		t.Fatalf("the clamped message count did not run green: %v\n%s", err, out)
	}
}

// loginMutant is the D11 vector: a form-2 batch carrying one LoginRequest body
// whose session_token length overruns the declared bytes(32) bound.
const loginMutantHex = "02004200000000000008c01002030a11181f262d343b424950575e656c737a81888f969da4abb2b9c0c7ced5dc44f28800403101"

var loginMutant = mustHex(loginMutantHex)

func mustHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

// messageClampedBody loads the mutant with the generated Dart batch message load
// and asserts the report's clamped count matches the Go oracle's on the same
// bytes (docs/SPEC-TABLES.md §3.3): the want is the Go oracle's count, never a
// literal.
func messageClampedBody(announce []byte, want int) string {
	return fmt.Sprintf(`
  final ann = Uint8List.fromList(%s);
  final msg = Uint8List.fromList(%s);
  final vocab = h.TableVocabulary(entries: List<h.TableMessageEntry>.generate(4096, (i) => h.TableMessageEntry(0, 0)));
  final annRep = h.TableReport();
  check(h.announceRead(vocab, ann, annRep), 'the connection announcement is refused: ' + annRep.reason);
  final values = <h.LoginRequest>[h.LoginRequest()];
  final rep = h.TableReport();
  final count = r.loginRequestLoadMessages(values, msg, vocab, rep);
  check(rep.clamped == %d, 'clamped=${rep.clamped} want %d (docs/SPEC-TABLES.md §3.3)');
`, dartBytes(announce), dartBytes(loginMutant), want, want)
}
