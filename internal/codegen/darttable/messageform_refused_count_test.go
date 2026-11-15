package darttable

// THE MESSAGE FORM'S REFUSED-BATCH COUNT ON THE DART LEG (docs/SPEC-TABLES.md
// §3.3): "A refused batch returns a count of `0`, as every refused batch does;
// a caller that needs room reads the wire's `M` itself." A batch whose `M`
// names more bodies than the caller's storage holds is refused before any body
// is decoded, and the returned count is 0, never the wire's `M`.

import (
	"strings"
	"testing"
)

const refusedBatchSchema = `package probe

fixed table Root {
    a int32
    b bool
    c uint8
}
`

// refusedBatchBody saves ten valid bodies, reads them into storage for eight,
// and reads the same buffer whole into storage for ten. The short storage is
// pre-filled so a write of any body shows; the refusal decodes nothing, so all
// eight keep their values (docs/SPEC-TABLES.md §3.3).
const refusedBatchBody = `
  final vocab = h.TableVocabulary(entries:
      List<h.TableMessageEntry>.generate(4096, (i) => h.TableMessageEntry(0, 0)));
  check(h.announceRead(vocab, h.tableAnnouncement, h.TableReport()), 'announce');
  final bodies = <h.Root>[for (var i = 0; i < 10; i++) (h.Root()..a = i)];
  final m = r.rootMeasureMessages(bodies, h.TableReport());
  check(m > 0, 'measure ten bodies: $m');
  final buf = Uint8List(m);
  final saveRep = h.TableReport();
  check(r.rootSaveMessages(bodies, buf, saveRep) == m, 'save ten bodies: ${saveRep.reason}');
  final short = <h.Root>[for (var i = 0; i < 8; i++) (h.Root()..a = 100 + i)];
  final shortRep = h.TableReport();
  final shortCount = r.rootLoadMessages(short, buf, vocab, shortRep);
  check(shortCount == 0 && shortRep.refused && shortRep.reason == 'batch_too_large',
      'ten into eight: count=$shortCount refused=${shortRep.refused} reason=${shortRep.reason}');
  for (var i = 0; i < 8; i++) {
    check(short[i].a == 100 + i, 'the refused batch wrote body $i: a=${short[i].a}');
  }
  final whole = <h.Root>[for (var i = 0; i < 10; i++) h.Root()];
  final wholeRep = h.TableReport();
  final wholeCount = r.rootLoadMessages(whole, buf, vocab, wholeRep);
  check(wholeCount == 10 && !wholeRep.refused && wholeRep.reason == '',
      'ten into ten: count=$wholeCount refused=${wholeRep.refused} reason=${wholeRep.reason}');
  for (var i = 0; i < 10; i++) {
    check(whole[i].a == i, 'body $i: a=${whole[i].a}');
  }
`

// TestDartMessageRefusedBatchCountsZero pins §3.3's refused-batch count on the
// emitted Dart: a batch naming more bodies than the caller's storage holds
// answers 0 and writes no body, while the same batch into storage for all ten
// reads whole.
func TestDartMessageRefusedBatchCountsZero(t *testing.T) {
	t.Parallel()

	dartBinary(t)
	u := unitFrom(t, refusedBatchSchema)
	out, err := runBatchProbe(t, u, "Root", refusedBatchBody)
	if err != nil {
		t.Fatalf("a refused batch did not return 0: %v\n%s", err, out)
	}
}

// TestDartMessageRefusedBatchCountsZeroNegativeControl puts the old return back
// in the emitted source in memory and requires the case above to go red.
func TestDartMessageRefusedBatchCountsZeroNegativeControl(t *testing.T) {
	t.Parallel()

	dartBinary(t)
	u := unitFrom(t, refusedBatchSchema)
	out, err := runBatchProbe(t, u, "Root", refusedBatchBody)
	if err != nil {
		t.Fatalf("the control's unmodified source is not green: %v\n%s", err, out)
	}

	const fixed = "report.Refuse('batch_too_large');\n    return 0;"
	const old = "report.Refuse('batch_too_large');\n    return count;"
	out, err = runBatchProbe(t, u, "Root", refusedBatchBody, func(files map[string][]byte) {
		name := fixedTableBase(u, "Root") + "Fixed.dart"
		src, ok := files[name]
		if !ok {
			t.Fatalf("the emitted root library %s is missing", name)
		}
		if n := strings.Count(string(src), fixed); n != 1 {
			t.Fatalf("the emitted refusal %q occurs %d times in %s, want once", fixed, n, name)
		}
		files[name] = []byte(strings.Replace(string(src), fixed, old, 1))
	})
	if err == nil {
		t.Fatalf("the old return passed TestDartMessageRefusedBatchCountsZero; output:\n%s", out)
	}
	if !strings.Contains(out, "ten into eight") {
		t.Fatalf("the old return did not fail the refused-batch assertion: %v\n%s", err, out)
	}
}
