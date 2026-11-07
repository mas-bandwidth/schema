package darttable

// A RANGED int64 WHOSE MINIMUM LIES NEAR -2^63 (docs/SPEC-TABLES.md §3.3).
//
// The announcement's signed range base travels as a zigzag LEB128
// (ir.DecodeShape, tableMessageUnzigzag), so a minimum near -2^63 names a
// uint64 above 2^63. The Dart int is signed and bit-transparent: the same
// value reads back negative, and the reader must not mistake that bit pattern
// for a failed read or a hostile width. The Go oracle accepts the shape and
// decodes the body; the generated Dart must answer the same verdict, the same
// counters and the same saved bytes.
//
// The widths are ir.TableMessageBitsRequired's: a minimum near the floor and a
// maximum of 100 span a value near 2^64, and a uint64 field whose max is above
// 2^63 spans it on the unsigned side. Every want below is the Go oracle's,
// never a literal.

import (
	"fmt"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// announceInt64Case is one probe: a fixed root Box with one ranged integer
// field x, and the value the body one carries.
type announceInt64Case struct {
	name   string
	schema string
	i      int64
	u      uint64
}

const announceInt64NearFloorSchema = `package probe

fixed table Box
{
    x int64 = 0 | min = -9223372036854775000, max = 100
}
`

const announceInt64FloorSchema = `package probe

fixed table Box
{
    x int64 = 0 | min = -9223372036854775808, max = 100
}
`

const announceInt64Uint64Schema = `package probe

fixed table Box
{
    x uint64 = 0 | min = 0, max = 18446744073709551000
}
`

// TestMessageAnnounceInt64MinNearFloorDart pins the announcement read, the
// shape decode and the batch read for a ranged 64-bit field whose range spans
// the signed boundary (docs/SPEC-TABLES.md §3.3). Each case is decoded by the
// Go oracle and by the generated Dart, and the announcement verdict, the six
// counters and body one's saved bytes must agree.
func TestMessageAnnounceInt64MinNearFloorDart(t *testing.T) {
	t.Parallel()

	wide := uint64(1)<<63 + 7
	cases := []announceInt64Case{
		{name: "int64_min_near_floor", schema: announceInt64NearFloorSchema, i: 5000, u: 5000},
		{name: "int64_min_exact_floor", schema: announceInt64FloorSchema, i: 5000, u: 5000},
		{name: "int64_at_min", schema: announceInt64FloorSchema, i: -9223372036854775808, u: 1 << 63},
		{name: "int64_at_max", schema: announceInt64FloorSchema, i: 100, u: 100},
		{name: "int64_past_max", schema: announceInt64FloorSchema, i: 101, u: 101},
		{name: "uint64_max_above_2_63", schema: announceInt64Uint64Schema, i: int64(wide), u: wide},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			checkAnnounceInt64Case(t, c)
		})
	}
}

// checkAnnounceInt64Case answers one probe with the Go oracle and with the
// generated Dart announcement read and batch load, and holds the verdict, the
// counters and body one's saved bytes equal.
func checkAnnounceInt64Case(t *testing.T, c announceInt64Case) {
	t.Helper()
	u := unitFrom(t, c.schema)
	st := findTable(t, u, "Box")
	if !st.IsTable || !st.FixedDeclared {
		t.Fatalf("§3.3 is a fixed table's; Box is a variable table")
	}
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup("Box"))
	f, found := inst.FieldByKey("x")
	if !found {
		t.Fatal("the probe's Box declares no x field")
	}
	f.Cell.I = c.i
	f.Cell.U = c.u
	body, err := tablewire.EncodeMessage(m, inst)
	if err != nil {
		t.Fatalf("the Go encoder refused the probe body: %v", err)
	}

	announce := tablewire.Announce(u)
	var announceReport tabletext.Report
	vocabulary := &tablewire.Vocabulary{}
	if err := vocabulary.AnnounceRead(announce, &announceReport); err != nil {
		t.Fatalf("the Go oracle refused the announcement: %v", err)
	}
	var loadReport tabletext.Report
	value := m.New(m.Lookup("Box"))
	count, ok, err := tablewire.DecodeMessages(m, []*tabletext.Instance{value}, body, vocabulary, &loadReport)
	if err != nil {
		t.Fatalf("the Go oracle erred on the probe body: %v", err)
	}
	if !ok || count != 1 {
		t.Fatalf("the Go oracle decoded %d bodies, ok=%v; the probe is one body", count, ok)
	}
	saved, err := tablewire.EncodeMessage(m, value)
	if err != nil {
		t.Fatalf("the Go oracle could not re-encode body one: %v", err)
	}

	out, err := runBatchProbe(t, u, "Box", announceInt64Body(announce, body, loadReport, saved))
	if err != nil {
		t.Fatalf("the message read did not run green: %v\n%s", err, out)
	}
}

// announceInt64Body drives the generated Dart announcement read and the batch
// load of one body, then compares the counters and body one's saved bytes
// against the Go oracle's on the same bytes (docs/SPEC-TABLES.md §3.3, §4).
func announceInt64Body(announce, body []byte, rep tabletext.Report, saved []byte) string {
	return fmt.Sprintf(`
  final ann = Uint8List.fromList(%s);
  final msg = Uint8List.fromList(%s);
  final vocab = h.TableVocabulary(entries: List<h.TableMessageEntry>.generate(4096, (i) => h.TableMessageEntry(0, 0)));
  final annRep = h.TableReport();
  final accepted = h.announceRead(vocab, ann, annRep);
  check(accepted, 'the connection announcement is refused: ' + annRep.reason);
  final value = h.Box();
  h.initBox(value);
  final values = <h.Box>[value];
  final loadRep = h.TableReport();
  final count = r.boxLoadMessages(values, msg, vocab, loadRep);
  check(count == 1 && !loadRep.refused && !loadRep.malformed,
      'LoadMessages count=$count refused=${loadRep.refused} malformed=${loadRep.malformed}');
  check(loadRep.unknown == %d && loadRep.kindMismatch == %d && loadRep.widened == %d &&
      loadRep.clamped == %d && loadRep.duplicate == %d && loadRep.malformed == false,
      'six counters unknown=${loadRep.unknown} kindMismatch=${loadRep.kindMismatch} widened=${loadRep.widened} clamped=${loadRep.clamped} duplicate=${loadRep.duplicate} malformed=${loadRep.malformed}');
  final measureRep = h.TableReport();
  final measure = r.boxMeasureMessages(values, measureRep);
  final buf = Uint8List(measure);
  final saveRep = h.TableReport();
  final wrote = r.boxSaveMessages(values, buf, saveRep);
  check(wrote == %d, 'SaveMessages=$wrote want %d');
  final want = Uint8List.fromList(%s);
  var same = true;
  for (var i = 0; i < want.length; i++) {
    if (buf[i] != want[i]) { same = false; }
  }
  check(same, 'saved bytes of body one differ from the oracle');
`, dartBytes(announce), dartBytes(body),
		rep.Unknown, rep.KindMismatch, rep.Widened, rep.Clamped, rep.Duplicate,
		len(saved), len(saved), dartBytes(saved))
}
