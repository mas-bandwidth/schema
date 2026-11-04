package darttable

// A REFUSAL WRITES NOTHING (docs/SPEC.md §5.1), and A COUNT OR A LENGTH PAST
// ITS STORAGE IS REFUSED IN EVERY BUILD. The fixed form's save and the message
// form's SaveMessages answer -1 for a value whose count or length would index
// past its own storage, and for a NaN, in the RELEASE build (no asserts), and
// leave the caller's buffer exactly as it was: every probe fills the buffer
// with a poison byte, saves a refused batch, and requires every byte of it
// still poison. The refused value is the batch's SECOND body, behind a first
// body that would write, and its fault sits behind a field of its own that
// would write too, so a writer that stores as it walks is caught.

import (
	"strings"
	"testing"
)

const noPartialSchema = `package probe

type Item
{
    n int32
}

union Pick
{
    item Item
}

fixed table Root
{
    first int32
    name  string(4)
    items [..3]Item
    pick  Pick
    ratio float32
}
`

const noPartialBody = `
  Uint8List poisoned(int n) => Uint8List(n)..fillRange(0, n, 0xA5);
  bool intact(Uint8List b) => b.every((x) => x == 0xA5);
  h.Root good() {
    final v = h.Root();
    h.initRoot(v);
    v.first = 5;
    v.name.setRange(0, 2, [0x68, 0x69]);
    v.nameLength = 2;
    v.itemsCount = 1;
    v.items[0].n = 9;
    v.ratio = 0.5;
    return v;
  }
  // the message form writes a non-positive count or length as an absent
  // field and indexes nothing, so the negative ones are the fixed form's
  final faults = <String, void Function(h.Root)>{
    'a count past its storage': (v) => v.itemsCount = 4,
    'a length past its storage': (v) => v.nameLength = 5,
    'a NaN': (v) => v.ratio = double.nan,
  };
  final fixedFaults = <String, void Function(h.Root)>{
    ...faults,
    'a negative count': (v) => v.itemsCount = -1,
    'a negative length': (v) => v.nameLength = -1,
  };
  // a union's tag is its enum, so no tag outside its arms reaches a writer
  final messageFaults = faults;
  // the unrefused batch writes, so the probe's buffers are big enough
  final fine = <h.Root>[good(), good()];
  check(r.rootFixedSave(fine, 2, Uint8List(r.rootFixedMeasure(2))) == r.rootFixedMeasure(2),
      'the fixed save of a lawful batch did not write');
  final fineNeed = r.rootMeasureMessages(fine, h.TableReport());
  check(fineNeed > 2 && r.rootSaveMessages(fine, Uint8List(fineNeed), h.TableReport()) == fineNeed,
      'SaveMessages of a lawful batch did not write');
  fixedFaults.forEach((what, fault) {
    final batch = <h.Root>[good(), good()];
    fault(batch[1]);
    final bytes = poisoned(r.rootFixedMeasure(2) + 64);
    check(r.rootFixedSave(batch, 2, bytes) == -1, 'the fixed save wrote $what');
    check(intact(bytes), 'the fixed save refused $what after storing bytes');
  });
  {
    final bytes = poisoned(r.rootFixedMeasure(3) + 64);
    check(r.rootFixedSave(<h.Root>[good(), good()], 3, bytes) == -1,
        'the fixed save read a batch past its length');
    check(intact(bytes), 'the fixed save refused a batch past its length after storing bytes');
  }
  messageFaults.forEach((what, fault) {
    final batch = <h.Root>[good(), good()];
    fault(batch[1]);
    final bytes = poisoned(fineNeed + 64);
    check(r.rootSaveMessages(batch, bytes, h.TableReport()) == -1, 'SaveMessages wrote $what');
    check(intact(bytes), 'SaveMessages refused $what after storing bytes');
  });
  {
    final bytes = poisoned(fineNeed - 1);
    check(r.rootSaveMessages(fine, bytes, h.TableReport()) == -1,
        'SaveMessages wrote into a buffer one byte short');
    check(intact(bytes), 'SaveMessages refused a short buffer after storing bytes');
  }
`

func TestWriteRefusalWritesNothingDart(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, noPartialSchema)
	out, err := runBatchProbeBuild(t, u, "Root", noPartialBody, false)
	if err != nil || !strings.Contains(out, "ok") {
		t.Fatalf("a refused save stored bytes in the release build: %v\n%s", err, out)
	}
}
