package darttable

// THE MESSAGE FORM'S MALFORMED OVERFLAG (docs/SPEC-TABLES.md §3.3).
//
// Form-2 messages the Go oracle reads clean (malformed=0) are answered
// malformed=1 by Dart, on every message root. The Dart batch message load
// (messageform_batch.go) is compared with the Go reference message decode
// (internal/tablewire/messagedecode.go) on each representative mutant of the
// 36,000-mutant Go-vs-Dart comparison, and the malformed flag and the report
// counters must agree. The cause is a body reference naming a slot this root
// does not declare: Go counts the unknown, steps past its payload and keeps
// reading (messagedecode.go body: `Unknown++` then `skip(entry)` then
// `continue`); the Dart emitter's `default:` case damages the batch and dies.
// This test pins each representative mutant in that direction, and pins the
// step's one exception: a reference naming a RESERVED id stays malformed.

import (
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func mustMessageHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

type malformedOverflagCase struct {
	name     string
	unitPath string
	root     string
	hex      string
}

var malformedOverflagCases = []malformedOverflagCase{
	{"vocabdemo.Wide00", "tables/vocab", "Wide00", "0200810500000000"},
	{"vocab9demo.Wide00", "tables/vocab9", "Wide00", "0200810a0000000000"},
	{"vocab9demo.Wide19", "tables/vocab9", "Wide19", "0200f80200000008340000000000"},
	{"backenddemo.LoginRequest", "tables/backend", "LoginRequest", "02004600000000000008c00002030a11181f262d343b424950575e656c737a81888f969da4abb2b9c0c7ced5dc44f28800403101"},
}

// nodeRefCases are one root per unit: the vocabulary, and so the node-table
// slot, is the unit's.
var nodeRefCases = []malformedOverflagCase{
	{"vocabdemo.Wide00", "tables/vocab", "Wide00", ""},
	{"vocab9demo.Wide00", "tables/vocab9", "Wide00", ""},
	{"backenddemo.LoginRequest", "tables/backend", "LoginRequest", ""},
}

// TestMessageMalformedOverflagDart replays each representative mutant of this
// cause against the Go reference decode and the generated Dart batch message
// load, and requires the two to agree: the same count, the same malformed
// flag, the same refusal verdict and the same unknown counter. Every subtest
// opens with t.Parallel().
func TestMessageMalformedOverflagDart(t *testing.T) {
	t.Parallel()
	for _, c := range malformedOverflagCases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			agreeOnMessageMutant(t, c.unitPath, c.root, mustMessageHex(t, c.hex))
		})
	}
	// THE STEP OVER AN UNKNOWN SLOT STOPS AT A RESERVED ID (§3.1, §3.3): every
	// vocabulary names the node table (ir.TableVocabulary), and a body
	// reference to it is malformed with no unknown counted, because the Go
	// reference body walk's reserved-id arm runs before its unknown-slot rule.
	// A step that took it for one more unknown kind-0 slot reads the body
	// clean.
	for _, c := range nodeRefCases {
		t.Run(c.name+".node_ref", func(t *testing.T) {
			t.Parallel()
			agreeOnMessageMutant(t, c.unitPath, c.root, nodeRefMutant(t, batchUnit(t, c.unitPath)))
		})
	}
}

// nodeRefMutant is a batch of one body whose first reference names the
// vocabulary's node-table entry, then the zero reference and the zero pad
// (references ride least significant bit first, as the bit reader reads them).
func nodeRefMutant(t *testing.T, u *ir.Unit) []byte {
	t.Helper()
	entries := ir.TableVocabulary(u)
	slot := uint64(0)
	for i, e := range entries {
		if e.Id == ir.TableNodeWireId {
			slot = uint64(i + 1)
		}
	}
	if slot == 0 {
		t.Fatal("the vocabulary names no node-table entry")
	}
	width := ir.TableMessageRefBits(len(entries))
	bits := make([]bool, 2*width)
	for i := range width {
		bits[i] = slot>>uint(i)&1 == 1
	}
	out := []byte{2, 0}
	for i := 0; i < len(bits); i += 8 {
		var b byte
		for j := 0; j < 8 && i+j < len(bits); j++ {
			if bits[i+j] {
				b |= 1 << uint(j)
			}
		}
		out = append(out, b)
	}
	return out
}

// agreeOnMessageMutant loads one mutant through the Go message decode and the
// generated Dart batch message load and requires the same count, refusal,
// malformed flag and unknown counter.
func agreeOnMessageMutant(t *testing.T, unitPath, root string, mutant []byte) {
	t.Helper()
	u := batchUnit(t, unitPath)
	m := tabletext.NewModel(u)
	def := m.Lookup(root)
	if def == nil {
		t.Fatalf("unit %s declares no table %s", unitPath, root)
	}
	vocab := &tablewire.Vocabulary{}
	if err := vocab.AnnounceRead(ir.TableAnnouncement(u), &tabletext.Report{}); err != nil {
		t.Fatalf("announce: %v", err)
	}
	bodies, _ := tablewire.MessageCount(mutant, vocab)
	if bodies < 1 {
		bodies = 1
	}
	batch := make([]*tabletext.Instance, bodies)
	for i := range batch {
		batch[i] = m.New(def)
	}
	var rep tabletext.Report
	countGo, ok, derr := tablewire.DecodeMessages(m, batch, mutant, vocab, &rep)
	refused := tablewire.Refused(derr)
	if derr != nil && !refused {
		t.Fatalf("the oracle refused the root itself: %v", derr)
	}
	malformedGo := rep.Malformed || (!ok && !refused)

	body := fmt.Sprintf(`
  final vocab = h.TableVocabulary(entries: List<h.TableMessageEntry>.generate(4096, (i) => h.TableMessageEntry(0, 0)));
  check(h.announceRead(vocab, h.tableAnnouncement, h.TableReport()), 'announce');
  final values = <h.%s>[for (var i = 0; i < 256; i++) h.%s()];
  final rep = h.TableReport();
  final count = r.%sLoadMessages(values, Uint8List.fromList(%s), vocab, rep);
  check(count == %d, 'count: got $count want %d');
  check(rep.refused == %v, 'refused: got ${rep.refused} want %v');
  check(rep.malformed == %v, 'malformed: got ${rep.malformed} want %v');
  check(rep.unknown == %d, 'unknown: got ${rep.unknown} want %d');
`, root, root, lowerFirst(root), dartBytes(mutant),
		countGo, countGo, refused, refused, malformedGo, malformedGo, rep.Unknown, rep.Unknown)
	out, err := runBatchProbe(t, u, root, body)
	if err != nil {
		t.Fatalf("%s %s: the Dart load did not agree with the Go oracle (count=%d malformed=%v unknown=%d) on %x: %v\n%s",
			unitPath, root, countGo, malformedGo, rep.Unknown, mutant, err, out)
	}
}
