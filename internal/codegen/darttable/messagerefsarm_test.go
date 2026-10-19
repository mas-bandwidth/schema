package darttable

// THE MESSAGE LOAD'S REFUSAL OF ENUM AND UNION REFERENCES (docs/SPEC-TABLES.md
// §3.3): the Dart batch load must refuse a reference that names no name or arm
// entry exactly as the Go reference does
// (internal/tablewire/messagedecode.go:226 `name`, :239 `arm`). A reference
// above the announcement's count, or one naming an entry of the wrong sort (a
// payload-bearing entry where a name is required, a kind-0 entry where an arm
// is required), is DAMAGE — one `malformed` — never an `unknown`. These are
// the remaining-underflag mutants Dart's tallied message fuzz kept diverging
// on: Dart reported (1,0,0,0,0,false) where the Go oracle reported
// (0,0,0,0,0,true).

import (
	"encoding/hex"
	"fmt"
	"regexp"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// refsArmCases are the two representative mutants of the tally fuzz (run seed
// 24845619678, n 20000), hex as remaining-underflag.txt restores them.
var refsArmCases = []struct {
	root   string
	mutant []byte
}{
	{"LoginRequest", mustHex("02004200000000000008c00002030a11181f262d343b424950575e656c737a81888f969da4abb2b9c0c7ced5dc44f28800402102")},
	{"StorePurchase", mustHex("02004200000000000008805201636f696e732e7061636b2e6c617267652e323530308b81990f0000684400")},
}

func mustHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

// refsArmBody drives the mutant through the emitted Dart batch load and prints
// the report's `unknown`, `malformed` and `refused` lanes, the fields the Dart
// message report carries (docs/SPEC-TABLES.md §3.3).
func refsArmBody(root string, mutant []byte) string {
	lower := lowerFirst(root)
	return fmt.Sprintf(`
  final msg = Uint8List.fromList(%s);
  final vocab = h.TableVocabulary(entries: List<h.TableMessageEntry>.generate(4096, (i) => h.TableMessageEntry(0, 0)));
  final annRep = h.TableReport();
  check(h.announceRead(vocab, h.tableAnnouncement, annRep), 'the announcement is refused: ' + annRep.reason);
  final value = h.%s();
  h.init%s(value);
  final values = <h.%s>[value];
  final rep = h.TableReport();
  final count = r.%sLoadMessages(values, msg, vocab, rep);
  print('${rep.unknown} ${rep.malformed} ${rep.refused}');
`, dartBytes(mutant), root, root, root, lower)
}

var refsArmLine = regexp.MustCompile(`(?m)^(\d+) (true|false) (true|false)$`)

// testMessageRefsAndUnknownArmDart loads each mutant through the generated Dart
// batch message load and through the Go reference message decode, and asserts
// the malformed flag and the report counters agree.
func TestMessageRefsAndUnknownArmDart(t *testing.T) {
	t.Parallel()
	u := batchUnit(t, "tables/backend")
	m := tabletext.NewModel(u)
	var vocab tablewire.Vocabulary
	var annRep tabletext.Report
	if err := vocab.AnnounceRead(tablewire.Announce(m.Unit), &annRep); err != nil {
		t.Fatalf("announce the reference vocabulary: %v", err)
	}
	for _, tc := range refsArmCases {
		t.Run(tc.root, func(t *testing.T) {
			st := findTable(t, u, tc.root)
			if !st.IsTable || !st.FixedDeclared {
				t.Skipf("§3.3 is a fixed table's; %s is not", tc.root)
			}
			inst := m.New(m.Lookup(tc.root))
			var g tabletext.Report
			if _, _, err := tablewire.DecodeMessages(m, []*tabletext.Instance{inst}, tc.mutant, &vocab, &g); err != nil {
				t.Fatalf("Go reference decode: %v", err)
			}
			out, err := runBatchProbe(t, u, tc.root, refsArmBody(tc.root, tc.mutant))
			if err != nil {
				t.Fatalf("the emitted Dart batch load did not run green: %v\n%s", err, out)
			}
			line := refsArmLine.FindStringSubmatch(out)
			if line == nil {
				t.Fatalf("no report line in the Dart probe output:\n%s", out)
			}
			dUnknown := 0
			if line[1] == "1" {
				dUnknown = 1
			}
			dMalformed := line[2] == "true"
			dRefused := line[3] == "true"
			if dUnknown != g.Unknown || dMalformed != g.Malformed || dRefused != g.Refused {
				t.Fatalf("%s: Dart report unknown=%d malformed=%v refused=%v, Go reference unknown=%d malformed=%v refused=%v",
					tc.root, dUnknown, dMalformed, dRefused, g.Unknown, g.Malformed, g.Refused)
			}
		})
	}
}
