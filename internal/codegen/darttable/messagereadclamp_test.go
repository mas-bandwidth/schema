package darttable

import (
	"fmt"
	"os"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestMessageReadClampsDeclaredRangeDart pins that the Dart message read clamps
// a decoded value to the reader's own declaration and counts it, exactly as
// the Go oracle does (internal/tablewire/messagedecode.go, `scalar`;
// docs/SPEC-TABLES.md §3.3, §4). A ranged field rides at bits_required(0, max - min), so
// an offset above max - min decodes to a value above max; the generated Dart
// payload the base and stored with no range check and no count, so Dart landed
// 11..16 where Go lands 10 and counted 0 where Go counts 1. The vector is the
// coordinator's wire-fuzz mutant over backenddemo.MatchResult (bit 402 flipped),
// the same bytes the finder traced: loaded through the generated Dart batch
// message load and through tablewire.DecodeMessages, the six counters and body
// one's saved bytes must agree.
//
// The same test carries the constructed StorePurchase vector and a synthetic
// counted-array vector, so the card's TEST line runs every clamp the message
// read owes. No message root declares a signed ranged field with a minimum below
// zero: backenddemo's score, placement and quantity, messagedemo's pid and tally,
// and graphdemo's build, quality, depth, version, seq and hits all declare
// min = 0, and the wide roots carry no range at all. There is therefore no
// below-min signed vector to construct; the below-min branch is the same clamp
// the above-max vectors reach, read on the other side.
func TestMessageReadClampsDeclaredRangeDart(t *testing.T) {
	t.Parallel()

	t.Run("MatchResult", func(t *testing.T) {
		t.Parallel()
		row := findMessageRow(t, "match_full")
		u := batchUnit(t, row.path)
		st := findTable(t, u, row.root)
		if !st.IsTable || !st.FixedDeclared {
			t.Skipf("§3.3 is a fixed table's; %s is a variable table", row.root)
		}
		checkMessageReadClamp(t, row.root, u, matchReadClampMutant(t, u))
	})

	// The constructed second vector: a store block whose quantity field's offset
	// rides above its declared max (99). The mutant is built by the test from the
	// Go encoder — a StorePurchase with quantity 120 is written, whose offset 119
	// the oracle then clamps back to 99 and counts — so the vector is never a
	// literal and pins both the clamp and the count on a second root with a
	// ranged field (StorePurchase's `quantity uint8 = 1 | min = 1, max = 99`).
	t.Run("StorePurchase", func(t *testing.T) {
		t.Parallel()
		row := findMessageRow(t, "store_full")
		u := batchUnit(t, row.path)
		st := findTable(t, u, row.root)
		if !st.IsTable || !st.FixedDeclared {
			t.Skipf("§3.3 is a fixed table's; %s is a variable table", row.root)
		}
		m := tabletext.NewModel(u)
		checkMessageReadClamp(t, row.root, u, storePurchaseClampMutant(t, m))
	})

	// The counted-array vector: a count above the reader's own bound keeps the
	// bound, counts one `clamped`, reads and drops the surplus elements, and
	// lands the KEPT count in the reader's storage so a save writes what the Go
	// oracle's array does (`fv.Count = int(kept)`).
	t.Run("CountedArray", func(t *testing.T) {
		t.Parallel()
		u, mutant := countedArrayClampMutant(t)
		checkMessageReadClamp(t, "ClampBox", u, mutant)
	})
}

// countedArrayClampSchema is a synthetic fixed unit with one counted array of
// table elements whose wire count can exceed the declared bound of two, and a
// field after it so a surplus element that is not consumed moves the read.
const countedArrayClampSchema = `package probe

fixed table ClampItem
{
    n uint8 = 0 | min = 0, max = 10
}

fixed table ClampBox
{
    items [..2]ClampItem
    trailer uint8 = 0
}
`

// countedArrayClampMutant builds a batch whose items count is three, one above
// the bound, from the Go encoder. The encoder writes what the instance holds;
// the reader's own bound is what clamps, exactly as the oracle's array does.
func countedArrayClampMutant(t *testing.T) (*ir.Unit, []byte) {
	t.Helper()
	u := unitFrom(t, countedArrayClampSchema)
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup("ClampBox"))
	var items *tabletext.Field
	for i := range inst.Fields {
		if inst.Fields[i].Def.Name == "items" {
			items = &inst.Fields[i]
		}
	}
	if items == nil {
		t.Fatal("the synthetic box declares no items field")
	}
	items.Count = 3
	for len(items.Elems) < items.Count {
		items.Elems = append(items.Elems, tabletext.Cell{})
	}
	for i := 0; i < items.Count; i++ {
		items.Elems[i].Tab = m.New(m.Lookup("ClampItem"))
		for j := range items.Elems[i].Tab.Fields {
			if items.Elems[i].Tab.Fields[j].Def.Name == "n" {
				items.Elems[i].Tab.Fields[j].Cell.I = int64(i + 1)
				items.Elems[i].Tab.Fields[j].Cell.U = uint64(i + 1)
			}
		}
	}
	for i := range inst.Fields {
		if inst.Fields[i].Def.Name == "trailer" {
			inst.Fields[i].Cell.I = 9
			inst.Fields[i].Cell.U = 9
		}
	}
	out, err := tablewire.EncodeMessage(m, inst)
	if err != nil {
		t.Fatalf("the Go encoder refused the counted-array mutant: %v", err)
	}
	return u, out
}

// findMessageRow returns the manifest row named `name`.
func findMessageRow(t *testing.T, name string) *batchRow {
	t.Helper()
	for i := range batchManifest(t) {
		r := &batchManifest(t)[i]
		if r.name == name {
			return r
		}
	}
	t.Fatalf("no %s message row in the manifest", name)
	return nil
}

// storePurchaseClampMutant builds a store message whose quantity offset rides
// above the declared max, using the Go encoder as the authority: quantity 120
// is written with its wired offset 119, which the reader's own range then
// clamps. The serialized bytes are the mutant the Dart batch load must answer
// identically.
func storePurchaseClampMutant(t *testing.T, m *tabletext.Model) []byte {
	t.Helper()
	root := "StorePurchase"
	inst := m.New(m.Lookup(root))
	if f, ok := inst.FieldByKey("player_id"); ok {
		f.Cell.I = 9007199254740993
	}
	if f, ok := inst.FieldByKey("sku"); ok {
		f.Cell.Str = []byte("abcdefghijklmnopqrstu")
		f.Count = 21
	}
	if f, ok := inst.FieldByKey("quantity"); ok {
		f.Cell.I = 120
		f.Cell.U = 120
	}
	if f, ok := inst.FieldByKey("price_minor"); ok {
		f.Cell.I = 499
	}
	if f, ok := inst.FieldByKey("currency"); ok {
		f.Cell.I = 2
	}
	out, err := tablewire.EncodeMessage(m, inst)
	if err != nil {
		t.Fatalf("the Go encoder refused the StorePurchase mutant: %v", err)
	}
	return out
}

// checkMessageReadClamp answers one mutant with the Go oracle and with the
// generated Dart batch message load, and holds the six counters and body one's
// saved bytes equal.
func checkMessageReadClamp(t *testing.T, root string, u *ir.Unit, mutant []byte) {
	t.Helper()
	m := tabletext.NewModel(u)
	vocabulary := &tablewire.Vocabulary{}
	var oracle tabletext.Report
	announce := tablewire.Announce(m.Unit)
	if err := vocabulary.AnnounceRead(announce, &oracle); err != nil {
		t.Fatalf("the Go oracle refused the announcement: %v", err)
	}
	value := m.New(m.Lookup(root))
	_, ok, err := tablewire.DecodeMessages(m, []*tabletext.Instance{value}, mutant, vocabulary, &oracle)
	if err != nil {
		t.Fatalf("the Go oracle erred on the mutant: %v", err)
	}
	if !ok {
		t.Fatalf("the Go oracle did not decode the mutant whole")
	}
	if oracle.Clamped == 0 {
		t.Fatalf("the oracle counts no clamp on the pinned mutant; the vector is wrong")
	}
	saved, err := tablewire.EncodeMessage(m, value)
	if err != nil {
		t.Fatalf("the oracle could not re-encode body one: %v", err)
	}
	out, err := runBatchProbe(t, u, root, messageReadClampBody(root, announce, oracle, saved, mutant))
	if err != nil {
		t.Fatalf("the clamped message read did not run green: %v\n%s", err, out)
	}
}

// matchReadClampMutant is the coordinator's vector rebuilt on the current
// wire: the backenddemo match_full value with the first player's placement
// written at offset 13, which decodes to 14, above its declared max of 10 (a
// placement rides bits_required(0, 9), four bits, so 13 is spellable). The Go
// oracle counts one `clamped`; Dart before its fix landed the out-of-range
// value uncounted.
func matchReadClampMutant(t *testing.T, u *ir.Unit) []byte {
	t.Helper()
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup("MatchResult"))
	file, err := os.ReadFile("../../../testdata/wire/tables/match_full.bin")
	if err != nil {
		t.Fatal(err)
	}
	var rep tabletext.Report
	if _, err := tablewire.Decode(m, inst, file, &rep); err != nil || !rep.Silent() {
		t.Fatalf("match_full did not read: %v %+v", err, rep)
	}
	players, ok := inst.FieldByKey("players")
	if !ok || len(players.Elems) == 0 || players.Elems[0].Tab == nil {
		t.Fatal("match_full holds no first player")
	}
	placement, ok := players.Elems[0].Tab.FieldByKey("placement")
	if !ok {
		t.Fatal("a player holds no placement")
	}
	placement.Cell.I, placement.Cell.U = 14, 14
	out, err := tablewire.EncodeMessage(m, inst)
	if err != nil {
		t.Fatalf("the Go encoder refused the MatchResult mutant: %v", err)
	}
	return out
}

// messageReadClampBody loads the mutant with the generated Dart batch message
// load and asserts the report's six counters and body one's saved bytes match
// the Go oracle's on the same bytes (docs/SPEC-TABLES.md §3.3, §4): each want
// is the Go oracle's, never a literal.
func messageReadClampBody(root string, announce []byte, rep tabletext.Report, saved, mutant []byte) string {
	lower := lowerFirst(root)
	return fmt.Sprintf(`
  final ann = Uint8List.fromList(%s);
  final msg = Uint8List.fromList(%s);
  final vocab = h.TableVocabulary(entries: List<h.TableMessageEntry>.generate(4096, (i) => h.TableMessageEntry(0, 0)));
  final annRep = h.TableReport();
  check(h.announceRead(vocab, ann, annRep), 'the connection announcement is refused: ' + annRep.reason);
  final value = h.%s();
  h.init%s(value);
  final values = <h.%s>[value];
  final rep = h.TableReport();
  final count = r.%sLoadMessages(values, msg, vocab, rep);
  check(count == 1 && !rep.refused && !rep.malformed,
      'LoadMessages count=$count refused=${rep.refused} malformed=${rep.malformed}');
  check(rep.unknown == %d && rep.kindMismatch == %d && rep.widened == %d &&
      rep.clamped == %d && rep.duplicate == %d,
      'six counters unknown=${rep.unknown} kindMismatch=${rep.kindMismatch} widened=${rep.widened} clamped=${rep.clamped} duplicate=${rep.duplicate}');
  final measureRep = h.TableReport();
  final measure = r.%sMeasureMessages(values, measureRep);
  final buf = Uint8List(measure);
  final saveRep = h.TableReport();
  final wrote = r.%sSaveMessages(values, buf, saveRep);
  check(wrote == %d, 'SaveMessages=$wrote want %d');
  final want = Uint8List.fromList(%s);
  var same = true;
  for (var i = 0; i < want.length; i++) {
    if (buf[i] != want[i]) { same = false; }
  }
  check(same, 'saved bytes of body one differ from the oracle');
`, dartBytes(announce), dartBytes(mutant),
		root, root, root, lower,
		rep.Unknown, rep.KindMismatch, rep.Widened, rep.Clamped, rep.Duplicate,
		lower, lower, len(saved), len(saved), dartBytes(saved))
}
