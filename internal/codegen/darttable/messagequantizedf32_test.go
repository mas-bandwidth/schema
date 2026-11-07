package darttable

// A QUANTIZED FLOAT32 ON THE MESSAGE FORM (docs/SPEC-TABLES.md §3.3). A float32
// field with a declared range and resolution rides on the wire as its step
// index (packing 2, ir.TableMessageQuantized), never as its IEEE-754 bits. The
// Go reference reads it at the announced width, reconstructs `min + index *
// delta` in float32 arithmetic, rejects an index above `count` as damage, and
// clamps the reconstructed value to the reader's own range
// (internal/tablewire/messagedecode.go, `float32` and `clampFloat`). This test
// pins that the generated Dart batch load answers the same six counters and
// that the value it loads saves back to the Go oracle's bytes.
//
// The Box schema is the coordinator's probe: a fixed root whose one field is a
// compressed float. The vectors cover an on-step value, an off-step value, a
// value above max, a value below min, and each bound. The Go oracle encodes each
// instance, the Dart leg loads those bytes, and both the loaded value and the
// re-saved bytes must be the oracle's: the wants are the oracle's, never a
// literal.

import (
	"fmt"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const quantizedFloat32Schema = `package probe

fixed table Box
{
    x float32 = 0 | min = -1.5, max = 2.5, resolution = 0.5
}
`

func TestMessageQuantizedFloat32ReadDart(t *testing.T) {
	t.Parallel()
	// The toolchain comes through dartBinary, so a bare `go test` skips by name
	// and SCHEMA_REQUIRE_CORPUS=1 makes a missing SDK a failure.
	dartBinary(t)
	for _, tc := range []struct {
		name string
		x    float64
	}{
		{"on_step", 1.0},
		{"off_step", 0.3},
		{"above_max", 7.25},
		{"below_min", -9.0},
		{"at_min", -1.5},
		{"at_max", 2.5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			u := unitFrom(t, quantizedFloat32Schema)
			checkQuantizedFloat32Read(t, u, tc.x)
		})
	}
}

// checkQuantizedFloat32Read answers one Box value with the Go oracle and with
// the generated Dart batch message load, and holds the six counters, the
// loaded field and body one's saved bytes equal.
func checkQuantizedFloat32Read(t *testing.T, u *ir.Unit, x float64) {
	t.Helper()
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup("Box"))
	f, ok := inst.FieldByKey("x")
	if !ok {
		t.Fatal("the synthetic Box declares no x field")
	}
	f.Cell.F = x
	mutant, err := tablewire.EncodeMessage(m, inst)
	if err != nil {
		t.Fatalf("the Go encoder refused x=%v: %v", x, err)
	}

	vocabulary := &tablewire.Vocabulary{}
	var announceReport tabletext.Report
	announce := tablewire.Announce(u)
	if err := vocabulary.AnnounceRead(announce, &announceReport); err != nil {
		t.Fatalf("the Go oracle refused the announcement: %v", err)
	}
	value := m.New(m.Lookup("Box"))
	var report tabletext.Report
	count, ok, err := tablewire.DecodeMessages(m, []*tabletext.Instance{value}, mutant, vocabulary, &report)
	if err != nil {
		t.Fatalf("the Go oracle erred on x=%v: %v", x, err)
	}
	if !ok || count != 1 {
		t.Fatalf("the Go oracle did not decode x=%v whole: count=%d ok=%v", x, count, ok)
	}
	loaded, ok := value.FieldByKey("x")
	if !ok {
		t.Fatal("the oracle's decoded Box declares no x field")
	}
	wantX := float64(float32(loaded.Cell.F))
	saved, err := tablewire.EncodeMessage(m, value)
	if err != nil {
		t.Fatalf("the oracle could not re-encode x=%v: %v", x, err)
	}

	body := quantizedFloat32Body(announce, mutant, report, wantX, saved)
	out, err := runBatchProbe(t, u, "Box", body)
	if err != nil {
		t.Fatalf("the quantized float32 message read did not run green: %v\n%s", err, out)
	}
}

// quantizedFloat32Body loads the encoded Box with the generated Dart batch
// message load and asserts the report's six counters, the loaded x and body
// one's saved bytes match the Go oracle's on the same bytes (docs/SPEC-TABLES.md
// §3.3, §4): each want is the Go oracle's, never a literal.
func quantizedFloat32Body(announce, mutant []byte, rep tabletext.Report, wantX float64, saved []byte) string {
	return fmt.Sprintf(`
  final ann = Uint8List.fromList(%s);
  final msg = Uint8List.fromList(%s);
  final vocab = h.TableVocabulary(entries: List<h.TableMessageEntry>.generate(4096, (i) => h.TableMessageEntry(0, 0)));
  final annRep = h.TableReport();
  check(h.announceRead(vocab, ann, annRep), 'the connection announcement is refused: ' + annRep.reason);
  final value = h.Box();
  h.initBox(value);
  final values = <h.Box>[value];
  final loadRep = h.TableReport();
  final count = r.boxLoadMessages(values, msg, vocab, loadRep);
  check(count == 1 && !loadRep.refused && !loadRep.malformed,
      'LoadMessages count=$count refused=${loadRep.refused} malformed=${loadRep.malformed}');
  check(loadRep.unknown == %d && loadRep.kindMismatch == %d && loadRep.widened == %d &&
      loadRep.clamped == %d && loadRep.duplicate == %d,
      'six counters unknown=${loadRep.unknown} kindMismatch=${loadRep.kindMismatch} widened=${loadRep.widened} clamped=${loadRep.clamped} duplicate=${loadRep.duplicate}');
  check(value.x == %s, 'x=${value.x} want %s');
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
`, dartBytes(announce), dartBytes(mutant),
		rep.Unknown, rep.KindMismatch, rep.Widened, rep.Clamped, rep.Duplicate,
		fixedFloatLit(wantX), fixedFloatLit(wantX),
		len(saved), len(saved), dartBytes(saved))
}
