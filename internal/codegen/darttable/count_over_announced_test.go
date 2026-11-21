package darttable

import (
	"errors"
	"fmt"
	"math/big"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// A COUNT ABOVE THE ANNOUNCED MAXIMUM REFUSES the read on the message form,
// on the named path and on the skip path, for an array and for an enum-keyed
// array (docs/SPEC-TABLES.md §3.3: a count is read before any body; the
// reference's array, keyed and skip, internal/tablewire/messagedecode.go). The
// count's bits can spell more than max - min, and the count is judged before
// any element.
//
// Every case is checked by its VERDICT alone, taken first from the Go
// reference on the same bytes: refused, or the value (docs/rulings.json
// refusal-verdict-only, SPEC-TABLES §6.5 and §13.10). Why a read refused is
// per-language diagnostic detail, never compared: the reason each side names
// (count_over_bound, out_of_range) is printed on a failure and nothing more.
// Every refusal has an accepted twin on the same entry.
const countOverAnnouncedSchema = `package probe

enum Color { Red, Green, Blue }

fixed table Root
{
	arr    [2..4]uint8
	counts [Color]uint8
}
`

// Vocabulary slots of countOverAnnouncedVocabulary, one based.
const (
	coArr       = 1 // arr announced [..5]uint8: three count bits, so 6 and 7 spell past max
	coKeyed     = 2 // counts announced over two slots: two count bits, so 3 spells past max
	coSkipArr   = 3 // an undeclared [..5]uint8
	coSkipKeyed = 4 // an undeclared u8 keyed array over two slots
	coSkipU8    = 5 // an undeclared u8 ranged 6 bits over 200
	coEntries   = 5
)

func countOverAnnouncedVocabulary(t *testing.T, u *ir.Unit) []ir.TableVocabularyEntry {
	t.Helper()
	root := u.Tables["Root"]
	id := func(name string) uint64 {
		for _, f := range root.Fields {
			if f.Name == name {
				return ir.TableFieldEntry(f).Id
			}
		}
		t.Fatalf("no field %s", name)
		return 0
	}
	plain := ir.TableMessageShape{}
	array := ir.TableMessageShape{Min: 0, Max: 5, Elem: ir.TableKindU8, Inner: &plain}
	keyed := ir.TableMessageShape{Max: 2, Elem: ir.TableKindU8, Inner: &plain}
	entries := []ir.TableVocabularyEntry{
		{Id: id("arr"), Kind: ir.TableKindArray, Shape: array},
		{Id: id("counts"), Kind: ir.TableKindKeyed, Shape: keyed},
		{Id: 0xc001, Kind: ir.TableKindArray, Shape: array},
		{Id: 0xc002, Kind: ir.TableKindKeyed, Shape: keyed},
		{Id: 0xc003, Kind: ir.TableKindU8, Shape: ir.TableMessageShape{Packing: ir.TableMessageRanged, Bits: 6, Base: big.NewInt(200)}},
	}
	if len(entries) != coEntries {
		t.Fatalf("vocabulary has %d entries, want %d", len(entries), coEntries)
	}
	return entries
}

type countOverCase struct {
	name    string
	wire    []byte
	refuses bool   // the verdict: refused, or a load of one value
	check   string // a Dart expression over `value` that holds on a load
}

func countOverAnnouncedCases() []countOverCase {
	refBits := ir.TableMessageRefBits(coEntries)
	body := func() *review1121Bits { return &review1121Bits{} }
	ref := func(b *review1121Bits, slot int) *review1121Bits { return b.put(uint64(slot), refBits) }
	var cases []countOverCase
	add := func(name string, refuses bool, check string, b *review1121Bits) {
		cases = append(cases, countOverCase{name: name, wire: b.batch(refBits), refuses: refuses, check: check})
	}
	const refuse, load = true, false
	add("named_array_past_announced_max", refuse, "", ref(body(), coArr).put(6, 3))
	add("named_array_top_count_bits", refuse, "", ref(body(), coArr).put(7, 3))
	add("named_array_past_reader_bound", refuse, "", ref(body(), coArr).put(5, 3).align().bytes(1, 2, 3, 4, 5))
	add("named_array_at_reader_bound_reads", load, "value.arrCount == 4 && value.arr[3] == 4", ref(body(), coArr).put(4, 3).align().bytes(1, 2, 3, 4))
	add("named_keyed_past_announced_max", refuse, "", ref(body(), coKeyed).put(3, 2))
	add("named_keyed_empty_reads", load, "true", ref(body(), coKeyed).put(0, 2))
	add("skipped_array_past_announced_max", refuse, "", ref(body(), coSkipArr).put(6, 3))
	add("skipped_array_at_announced_max_reads", load, "true", ref(body(), coSkipArr).put(5, 3).align().bytes(1, 2, 3, 4, 5))
	add("skipped_keyed_past_announced_max", refuse, "", ref(body(), coSkipKeyed).put(3, 2))
	add("skipped_keyed_empty_reads", load, "true", ref(body(), coSkipKeyed).put(0, 2))
	// The skip path's own refusal: a ranged value past its kind refuses.
	add("skipped_ranged_past_kind", refuse, "", ref(body(), coSkipU8).put(63, 6))
	add("skipped_ranged_at_kind_top_reads", load, "true", ref(body(), coSkipU8).put(55, 6))
	return cases
}

func countOverAnnouncedProbe(ann []byte, cases []countOverCase) string {
	var b strings.Builder
	fmt.Fprintf(&b, "  final ann = Uint8List.fromList(%s);\n", dartBytes(ann))
	b.WriteString(`  final vocab = h.TableVocabulary(entries: List<h.TableMessageEntry>.generate(256, (i) => h.TableMessageEntry(0, 0)));
  final annRep = h.TableReport();
  check(h.announceRead(vocab, ann, annRep) && vocab.announced, 'the announcement is refused: ' + annRep.reason);
  var failed = false;
  void report(String name, bool pass, int count, h.TableReport rep) {
    if (pass) {
      print('PASS $name');
    } else {
      print('FAIL $name: count=$count refused=${rep.refused} reason=${rep.reason}');
      failed = true;
    }
  }
`)
	for i, tc := range cases {
		fmt.Fprintf(&b, "  {\n    final value = h.Root();\n    h.initRoot(value);\n")
		fmt.Fprintf(&b, "    final rep = h.TableReport();\n")
		fmt.Fprintf(&b, "    final count = r.rootLoadMessages(<h.Root>[value], Uint8List.fromList(%s), vocab, rep);\n", dartBytes(tc.wire))
		if !tc.refuses {
			fmt.Fprintf(&b, "    report(%q, count == 1 && !rep.refused && (%s), count, rep);\n", tc.name, tc.check)
		} else {
			// the verdict alone: rep.reason is printed on a FAIL, never compared
			fmt.Fprintf(&b, "    report(%q, count == 0 && rep.refused, count, rep);\n", tc.name)
		}
		fmt.Fprintf(&b, "  } // %d\n", i)
	}
	b.WriteString("  check(!failed, 'the count-over-announced reads did not hold');\n")
	return b.String()
}

// TestDartMessageCountOverAnnounced holds the emitted Dart batch read to the
// reference's verdict for a count above the announced maximum.
func TestDartMessageCountOverAnnounced(t *testing.T) {
	t.Parallel()
	countOverAnnouncedRun(t, nil)
}

// TestDartMessageCountOverAnnouncedReasonIsDiagnostic renames every reason the
// emitted batch read refuses with (count_over_bound, out_of_range) in a
// scratch copy and requires every case to stay green: a reason-only
// difference from the reference is per-language detail, never a failure
// (docs/rulings.json refusal-verdict-only).
func TestDartMessageCountOverAnnouncedReasonIsDiagnostic(t *testing.T) {
	t.Parallel()
	countOverAnnouncedRun(t, func(files map[string][]byte) {
		renamed := 0
		for name, src := range files {
			text := string(src)
			for _, reason := range []string{"count_over_bound", "out_of_range"} {
				from := "Refuse('" + reason + "')"
				renamed += strings.Count(text, from)
				text = strings.ReplaceAll(text, from, "Refuse('renamed_by_the_plant')")
			}
			files[name] = []byte(text)
		}
		if renamed == 0 {
			t.Fatal("the plant renamed no refusal: the emitted batch read no longer spells Refuse('count_over_bound')")
		}
	})
}

// countOverAnnouncedRun runs every case against the Go reference's verdict,
// then through the emitted Dart batch read, edited by edit when it is not nil.
func countOverAnnouncedRun(t *testing.T, edit func(map[string][]byte)) {
	t.Helper()
	u := unitFrom(t, countOverAnnouncedSchema)
	entries := countOverAnnouncedVocabulary(t, u)
	// ANOTHER BUILD VERSION: the undeclared entries are fields only a writer
	// of another build sends (SPEC-TABLES §3.3 (b)).
	ann := depthBoundaryEncodeAnnouncement(ir.BuildVersion(u)^1, ir.TableVocabularyBytes(entries))
	cases := countOverAnnouncedCases()

	m := tabletext.NewModel(u)
	var vocab tablewire.Vocabulary
	var annRep tabletext.Report
	if err := vocab.AnnounceRead(ann, &annRep); err != nil {
		t.Fatalf("the Go reference refused the announcement: %v", err)
	}
	for _, tc := range cases {
		inst := m.New(m.Lookup("Root"))
		var g tabletext.Report
		n, _, err := tablewire.DecodeMessages(m, []*tabletext.Instance{inst}, tc.wire, &vocab, &g)
		var refusal *tablewire.Refusal
		refused := false
		switch {
		case errors.As(err, &refusal):
			refused = true
		case err != nil:
			// not a refusal: the reference failed, and the case cannot stand on it
			t.Fatalf("%s: the Go reference failed without refusing: %v", tc.name, err)
		case n != 1 || g.Refused:
			refused = true
		}
		if refused != tc.refuses {
			t.Fatalf("%s: the Go reference's verdict is refused=%t, the case says refused=%t", tc.name, refused, tc.refuses)
		}
	}

	out, err := runBatchProbe(t, u, "Root", countOverAnnouncedProbe(ann, cases), edit)
	if err != nil {
		t.Fatalf("the Dart count-over-announced reads did not hold: %v\n%s", err, out)
	}
	for _, tc := range cases {
		if !strings.Contains(out, "PASS "+tc.name+"\n") {
			t.Errorf("missing PASS for %s", tc.name)
		}
	}
	if t.Failed() {
		t.Logf("probe output:\n%s", out)
	}
}
