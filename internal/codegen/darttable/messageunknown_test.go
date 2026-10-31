package darttable

import (
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// THE DART MESSAGE LOAD COUNTS UNKNOWN FIELDS AS GO DOES (docs/SPEC-TABLES.md
// §3.3): the Go reference's `body` walk (internal/tablewire/messagedecode.go)
// counts an announced entry the root does not name as ONE `unknown` and STEPS
// ITS PAYLOAD by the entry's announced shape (`skip`), continuing the body; only
// a reference naming no announced entry, or a reserved id, is damage. The Dart
// emitter once treated every non-field reference as damage and ended the body
// at the first unknown, so a body carrying two unknowns counted one
// (dart-fix-message-unknown-count, cause D5 of the Dart wire fuzz).

// messageUnknownRow is one wire-fuzz mutant of a form-2 message whose unknown
// count diverged between the legs: the manifest message row whose connection
// announces the vocabulary, the root, and the mutated wire, restored from the
// receipts of dart-fix-message-fuzz-arm-checks-load-result w4 (`xxd -r -p`).
// wantGo pins the reference's own count on the bytes, so a regression of the
// reference cannot carry the Dart leg with it.
type messageUnknownRow struct {
	name    string
	row     string
	root    string
	wantGo  int
	wireHex string
	// annHex, when set, is the announcement the receipt was made under, which
	// the manifest row no longer carries: a mutant's references are slots
	// of THAT vocabulary, so it is read under the connection that made it.
	annHex string
}

// backendConnWideTail is backenddemo's announcement as the wire fuzz run of
// the LoginRequest receipts saw it: its tail carried a third blob id,
// fnv1a64("wstring"), so every table name sat one slot higher than today's
// (docs/SPEC-TABLES.md §3.3). A current reader takes it as an ordinary
// announcement whose extra name it does not know.
const backendConnWideTail = "010109ae8544080a63d016020ec10206be02e5e9b563d0a9b8cf0f38c48d74ffe34ac109000dfa34688b0f86260e00200600f9080bbd37d565b80800dd240af523a655c71e266814ba79e336f30900298b551875ace3980e0a0a0df524f6e7fb630ec104011100204a75304802a398060104011a4de85c199a42820c201897806bf58b76a30601070140882aff5c8845a7080016d40a899bfba5751e07ec28db19b1ee6100c7f87b8e19177dd900546a0a98196582ea00cae8a8d71977ed5a00b01eb5b5072b200900cf5a96b5075ffb0800243cd2e88bdf380b0059bed5b5070f460900d2298fe1b65dc7030dc47867d77ccd519b0dc2f098556fe9a80b0dffffffffffffffff00e44f1c4f47c02e2f0058fcaffad8e04b7000c7d47b26b09d295f0035e6b3c00f528820003fa5b6b0dc9eac16003f4cef5a0c54e9670024c3f5f6a941b2230075c23f6a372e36180000fefffffffffffffffdffffffffffffff0200000000000000"

const wide09Mutant = "0200760100000077020000007803000000ac79040000007a050000007b060000007c070000007d080000007e090000007f0a000000800b000000810c000000820d00000000"

var messageUnknownRows = []messageUnknownRow{
	// backenddemo.LoginRequest (corpus seed login_full_message), message fuzz
	// pass reference #1, mutant 60, run seed 24845619678: the reference
	// counts 2 unknown, the Dart leg counted 1 (the 1-vs-2 case).
	{name: "LoginRequest_mutant60_unknown_1_vs_2", row: "login_full", root: "LoginRequest", wantGo: 2,
		wireHex: "02006100000000000008c00002030a11181f262d343b424950575e656c737a81888f969da4abb2b9c0c7ced5dc44f28800403101", annHex: backendConnWideTail},
	// backenddemo.LoginRequest on the wire-fuzz random pass's first
	// decoded-value save divergence (pass bit #390, run seed 24845619678):
	// the reference counts 0 unknown and both legs mark the body malformed,
	// so the unknown count agrees even though the save does not (the save
	// divergence is another cause, named in the card's report).
	{name: "LoginRequest_save_divergence_malformed", row: "login_full", root: "LoginRequest", wantGo: 0,
		wireHex: "02004200000000000008c00002030a11181f262d343b424950575e656c737a81888f969da4abb2b9c0c7ced5dc44f28800003101", annHex: backendConnWideTail},
	// The vocabdemo.Wide09 receipt (vocab_conn's announcement).
	{name: "Wide09_example", row: "vocab_wide", root: "Wide09", wantGo: 0, wireHex: wide09Mutant},
	// The same bytes against the connection's other message root, whose
	// fields are not the slots the mutant names.
	{name: "Wide00_same_bytes", row: "vocab_low", root: "Wide00", wantGo: 3, wireHex: wide09Mutant},
}

// goMessageUnknown is the Go reference message decode's answer on the bytes:
// tablewire.DecodeMessages under the connection's announcement.
func goMessageUnknown(t *testing.T, row batchRow, root string, wire []byte) (unknown int, malformed bool) {
	t.Helper()
	m := tabletext.NewModel(batchUnit(t, row.path))
	var v tablewire.Vocabulary
	var annRep tabletext.Report
	if err := v.AnnounceRead(row.announce, &annRep); err != nil || !v.Announced() {
		t.Fatalf("%s: the reference refuses the connection announcement: %v", row.name, err)
	}
	st := m.Unit.Tables[root]
	if st == nil || !st.FixedDeclared {
		t.Fatalf("%s is not a fixed root of %s", root, row.path)
	}
	var rep tabletext.Report
	if _, _, err := tablewire.DecodeMessages(m, []*tabletext.Instance{m.New(st)}, wire, &v, &rep); err != nil {
		t.Fatalf("%s: the reference refuses the mutant: %v", root, err)
	}
	return rep.Unknown, rep.Malformed
}

// TestMessageUnknownCountDart loads each mutant by the generated Dart batch
// message load and by the Go reference message decode, under the same
// announcement, and asserts the two `unknown` counts are equal
// (docs/SPEC-TABLES.md §3.3).
func TestMessageUnknownCountDart(t *testing.T) {
	t.Parallel()
	rows := map[string]batchRow{}
	for _, row := range batchManifest(t) {
		rows[row.name] = row
	}
	for _, c := range messageUnknownRows {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			row, ok := rows[c.row]
			if !ok {
				t.Fatalf("the manifest carries no message row %s", c.row)
			}
			wire, err := hex.DecodeString(c.wireHex)
			if err != nil {
				t.Fatal(err)
			}
			if c.annHex != "" {
				if row.announce, err = hex.DecodeString(c.annHex); err != nil {
					t.Fatal(err)
				}
			}
			goUnknown, goMalformed := goMessageUnknown(t, row, c.root, wire)
			if goUnknown != c.wantGo {
				t.Fatalf("%s: the Go reference counts unknown=%d on the mutant, the receipt says %d", c.root, goUnknown, c.wantGo)
			}
			u := batchUnit(t, row.path)
			st := findTable(t, u, c.root)
			lower := lowerFirst(c.root)
			body := fmt.Sprintf(`
  final ann = Uint8List.fromList(%s);
  final msg = Uint8List.fromList(%s);
  final vocab = h.TableVocabulary(entries: List<h.TableMessageEntry>.generate(4096, (i) => h.TableMessageEntry(0, 0)));
  final annRep = h.TableReport();
  check(h.announceRead(vocab, ann, annRep), 'the connection announcement is refused: ' + annRep.reason);
  final v = h.%s();
  h.init%s(v);
  final values = <h.%s>[v];
  final rep = h.TableReport();
  r.%sLoadMessages(values, msg, vocab, rep);
  print('unknown=${rep.unknown} malformed=${rep.malformed}');
`, dartBytes(row.announce), dartBytes(wire), st.Name, st.Name, st.Name, lower)
			out, err := runBatchProbe(t, u, c.root, body)
			if err != nil {
				t.Fatalf("%s: the Dart probe did not run: %v\n%s", c.root, err, out)
			}
			dartUnknown := -1
			for line := range strings.SplitSeq(out, "\n") {
				if rest, ok := strings.CutPrefix(line, "unknown="); ok {
					n, _, _ := strings.Cut(rest, " ")
					dartUnknown, _ = strconv.Atoi(n)
				}
			}
			if dartUnknown != goUnknown {
				t.Errorf("%s: Dart counts unknown=%d, the Go reference counts %d (Go malformed=%v); Dart said %q",
					c.root, dartUnknown, goUnknown, goMalformed, strings.TrimSpace(out))
			}
		})
	}
}
