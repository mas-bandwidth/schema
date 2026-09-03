// THE DART TABLES SOAK, gated on CORRECTNESS under reuse.
//
// Every record in the corpus round-trips through the wire and through the text
// for the whole run, byte-compared against its golden, into storage that is
// reused every iteration — which is the property a reused instance can
// actually break: a stale count, a buffer not cleared, a limit not restored.
// A round trip that stops reproducing its golden throws, and the run refuses.
//
//   usage: soak <seconds>
//
// THE ALLOCATION FLOOR IS NOT THIS FILE'S TO HOLD. It is gated beside this
// soak by `make tables-dart-alloc` (test/dart-tables/gcgate.dart): a
// measurement of the VM's own scavenge count over a steady phase, held at zero
// under AOT and printed under the JIT, with a planted allocation that turns it
// red. The two share one corpus (corpus.dart) and one shape — a caller-owned
// reader, writer and report — so what the soak proves correct is what the gate
// proves allocation-free.
//
// SOAK_SABOTAGE=1 corrupts one byte of one re-saved record, and the byte
// comparison must refuse it: `make tables-dart-soak-negative-control`.
import 'dart:io';

import 'corpus.dart';

bool sabotage = false;

// ONE WIRE ITERATION: load the golden into the caller's value, measure it, save
// it back into the caller's scratch, and byte-compare. Nothing here allocates.
void wirePass(List<Case> corpus) {
  for (final c in corpus) {
    report.clear();
    reader.attach(c.wire, report);
    if (!c.loadBody(reader)) {
      throw StateError('${c.name}: the wire golden does not load');
    }
    final size = c.measure();
    if (size < 0 || size > c.scratch.length) {
      throw StateError('${c.name}: measures $size');
    }
    writer.attach(c.scratch);
    if (!c.saveBody(writer)) {
      throw StateError('${c.name}: the save refuses a value it just read');
    }
    if (sabotage) {
      // THE NEGATIVE CONTROL: one byte of one re-saved record, which the
      // comparison below must refuse
      c.scratch[0] ^= 1;
    }
    if (writer.offset != size ||
        !same(c.wire, c.scratch, size) ||
        size != c.wire.length) {
      throw StateError(
        '${c.name}: the round trip does not reproduce the golden',
      );
    }
  }
}

// ONE TEXT ITERATION: read the golden text into the value and write it back,
// both ways byte-compared. This is the phase whose floor is not zero: the §16
// walk's allocations are per FLOAT and per NUMBER TOKEN, and the page prices
// them.
void textPass(List<Case> corpus) {
  for (final c in corpus) {
    report.clear();
    if (!c.fromJson(c.text, report)) {
      throw StateError('${c.name}: the golden text does not read');
    }
    final size = c.toJsonMeasure();
    if (size != c.text.length) {
      throw StateError(
        '${c.name}: measures $size against a text of ${c.text.length}',
      );
    }
    if (c.toJson(c.scratch) != size || !same(c.text, c.scratch, size)) {
      throw StateError(
        '${c.name}: the text written back is not the golden text',
      );
    }
  }
}

void main(List<String> args) {
  sabotage = Platform.environment['SOAK_SABOTAGE'] == '1';
  final seconds = args.isEmpty ? 5 : int.parse(args[0]);
  final cases = corpus();

  final clock = Stopwatch()..start();
  var iterations = 0;
  try {
    while (clock.elapsedMilliseconds < seconds * 1000) {
      wirePass(cases);
      textPass(cases);
      iterations++;
    }
  } on StateError catch (e) {
    stdout.write('\nSOAK FAILED: $e\n');
    exit(1);
  }
  final records = cases.length;
  stdout.write('\nDART TABLES SOAK\n');
  stdout.write(
    '  ${seconds}s, $iterations iterations over $records records — '
    '${iterations * records} wire round trips and as many text round trips, '
    'every one byte-compared\n',
  );
  stdout.write('the soak holds: every round trip reproduced its golden\n');
  exit(0);
}
