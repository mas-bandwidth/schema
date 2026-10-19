package darttable

import (
	"encoding/hex"
	"fmt"
	"testing"
)

// THE DART MESSAGE LOAD COUNTS UNKNOWN FIELDS AS GO DOES (docs/SPEC-TABLES.md
// §3.3): a form-2 message carrying unknown field references is counted by the
// Dart batch message load exactly as the Go reference message decode counts it.
// The reference's `body` walk counts an announced entry this root does not name
// as ONE `unknown` and STEPS ITS PAYLOAD, continuing the body; a reference
// naming no announced entry, or a reserved id, is damage. The Dart emitter once
// treated every non-field reference as damage and terminated the body at the
// first unknown, so a message carrying two unknowns counted one
// (dart-fix-message-unknown-count, cause D5 of the Dart wire fuzz).

// messageUnknownRow is one fuzz mutation of a form-2 message whose unknown count
// diverged: the root, the Go oracle's `unknown` count on the same bytes, and
// the mutated wire. Restored from the receipts of
// dart-fix-message-fuzz-arm-checks-load-result w4 with `xxd -r -p`.
type messageUnknownRow struct {
	root   string
	want   int
	wireHex string
}

var messageUnknownRows = []messageUnknownRow{
	// backenddemo.LoginRequest (corpus seed login_full_message), message fuzz
	// pass reference #1, mutant 60, run seed 24845619678: oracle report=2
	// unknown; the Dart leg reported 1.
	{root: "LoginRequest", want: 2,
		wireHex: "02006100000000000008c00002030a11181f262d343b424950575e656c737a81888f969da4abb2b9c0c7ced5dc44f28800403101"},
	// vocabdemo.Wide09, same comparison: oracle unknown=2.
	{root: "Wide09", want: 2,
		wireHex: "0200760100000077020000007803000000ac79040000007a050000007b060000007c070000007d080000007e090000007f0a000000800b000000810c000000820d00000000"},
}

// TestMessageUnknownCountDart loads each representative mutation by the
// generated Dart batch message load and asserts the report's `unknown` count
// equals the Go reference message decode's on the same bytes
// (docs/SPEC-TABLES.md §3.3).
func TestMessageUnknownCountDart(t *testing.T) {
	t.Parallel()
	rows := batchManifest(t)
	paths := map[string]string{}
	for _, row := range rows {
		paths[row.root] = row.path
	}
	for _, m := range messageUnknownRows {
		m := m
		t.Run(m.root, func(t *testing.T) {
			t.Parallel()
			path, ok := paths[m.root]
			if !ok {
				t.Fatalf("no manifest message row for root %s", m.root)
			}
			u := batchUnit(t, path)
			st := findTable(t, u, m.root)
			if !st.IsTable || !st.FixedDeclared {
				t.Fatalf("%s is not a fixed root", m.root)
			}
			lower := lowerFirst(m.root)
			body := fmt.Sprintf(`
  final msg = Uint8List.fromList(%s);
  final vocab = h.TableVocabulary(entries: List<h.TableMessageEntry>.generate(4096, (i) => h.TableMessageEntry(0, 0)));
  check(h.announceRead(vocab, h.tableAnnouncement, h.TableReport()), 'announce');
  final v = h.%s();
  h.init%s(v);
  final values = <h.%s>[v];
  final rep = h.TableReport();
  final count = r.%sLoadMessages(values, msg, vocab, rep);
  check(rep.unknown == %d, 'unknown ' + rep.unknown.toString() + ' want %d');
  print('unknown=${rep.unknown} count=$count');
`, dartBytes(mustHex(m.wireHex)), st.Name, st.Name, st.Name, lower, m.want, m.want)
			out, err := runBatchProbe(t, u, m.root, body)
			if err != nil {
				t.Fatalf("%s: the unknown-count probe did not run green: %v\n%s", m.root, err, out)
			}
		})
	}
}

func mustHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}
