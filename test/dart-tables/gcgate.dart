// THE DART TABLES ALLOCATION GATE, measured rather than grepped.
//
// The claim is the port's biggest: **the wire path allocates nothing per
// record**. A scan over emitted source for allocating SPELLINGS cannot hold a
// claim about behavior — a bare list literal, `List.filled` and
// `Uint8List.fromList` all walked through one — so the gate is a measurement
// with a planted allocation that turns it red.
//
// THE INSTRUMENT IS THE VM'S OWN NEW-SPACE SCAVENGE COUNT. Run under
// `--verbose_gc`, the VM prints one line per collection to stderr; a loop that
// allocates nothing triggers none, however long it runs. It has a TRUE ZERO
// FLOOR — the property the claim needs — it shares nothing with the code under
// test, it needs no service protocol and no accumulator the VM resets, and it
// works under AOT and under the JIT alike.
//
//   usage: gcgate <phase> <iterations> [case]
//   phases: idle | load | measure | save | wire | text
//           plant-report | plant-bytes   (the negative controls)
//
// The steady phase is bracketed by two marker lines on STDERR — the stream the
// GC lines are on, so their order is the VM's own — and the count is the
// Scavenge lines between them: everything before the first marker is the
// corpus loading, the warm pass and, under the JIT, the optimizer's own
// compilation, none of which is the loop's. `make tables-dart-alloc` does the
// counting, under the JIT and under `dart compile aot-snapshot` both.
//
// The one line on stdout is a checksum, so nothing the loop does can be
// optimized away.
import 'dart:io';
import 'dart:typed_data';

import '../../build/tables-generated-dart/examples/TabledemoTable.dart';
import 'corpus.dart';

// THE PLANTS' SINK: a ring the planted objects are stored into at a moving
// index and read back at the end, so no compiler can sink the allocation. A
// single overwritten variable is not enough — the AOT compiler sees a store
// that the next iteration kills and removes the object with it.
final List<Object?> plantRing = List<Object?>.filled(64, null);
int checksum = 0;

void main(List<String> args) {
  if (args.length < 2) {
    stderr.writeln('usage: gcgate <phase> <iterations> [case]');
    exit(2);
  }
  final phase = args[0];
  final n = int.parse(args[1]);
  var cases = corpus();
  if (args.length > 2) {
    cases = cases.where((c) => c.name == args[2]).toList();
    if (cases.isEmpty) {
      stderr.writeln('gcgate: no case named ${args[2]}');
      exit(2);
    }
  }
  const phases = {
    'idle',
    'load',
    'measure',
    'save',
    'wire',
    'text',
    'plant-report',
    'plant-bytes',
  };
  if (!phases.contains(phase)) {
    stderr.writeln('gcgate: no phase named $phase');
    exit(2);
  }

  // A WARM PASS FIRST, outside the count: under the JIT the optimizer's own
  // compilation allocates, and it belongs to the warm-up rather than to the
  // loop. Under AOT there is nothing to warm and this costs the passes. The
  // text phase warms briefly: a text round trip is a hundred times a wire one,
  // and the phase is a price rather than a gate.
  final warm = phase == 'text' ? 50 : 2000;
  for (var i = 0; i < warm; i++) {
    for (var k = 0; k < cases.length; k++) {
      final c = cases[k];
      report.clear();
      reader.attach(c.wire, report);
      c.loadBody(reader);
      checksum += c.measure();
      writer.attach(c.scratch);
      c.saveBody(writer);
      checksum += writer.offset;
      if (phase == 'text') {
        c.fromJson(c.text, report);
        checksum += c.toJsonMeasure();
        checksum += c.toJson(c.scratch);
      }
    }
  }

  // THE PHASE'S OWN LOOP RUNS ONCE BEFORE THE MARKER, so that under the JIT the
  // loop is compiled — the optimizer's on-stack replacement of a loop that has
  // never run, and the code objects it makes, are the warm-up's and not the
  // measurement's. Under AOT it is one more pass.
  steady(phase, 2000, cases);

  stderr.writeln('gcgate: steady phase begins');
  steady(phase, n, cases);
  stderr.writeln('gcgate: steady phase ends');
  stdout.writeln(
    'gcgate $phase $n ${checksum & 0xffff} ${plantRing[7] == null ? 0 : 1}',
  );
}

// steady is the measured loop: n passes over the corpus, one phase.
void steady(String phase, int n, List<Case> cases) {
  for (var i = 0; i < n; i++) {
    // INDEXED, not for-in: a for-in over a list is an iterator object per
    // outer iteration in code the optimizer has not reached yet, and the
    // gate must not count the instrument's own allocation
    for (var k = 0; k < cases.length; k++) {
      final c = cases[k];
      switch (phase) {
        case 'idle':
          checksum += c.wire[0];
        case 'load':
          report.clear();
          reader.attach(c.wire, report);
          c.loadBody(reader);
          checksum += reader.offset;
        case 'measure':
          checksum += c.measure();
        case 'save':
          writer.attach(c.scratch);
          c.saveBody(writer);
          checksum += writer.offset;
        case 'wire':
          report.clear();
          reader.attach(c.wire, report);
          c.loadBody(reader);
          checksum += c.measure();
          writer.attach(c.scratch);
          c.saveBody(writer);
          checksum += writer.offset;
        case 'text':
          report.clear();
          c.fromJson(c.text, report);
          checksum += c.toJsonMeasure();
          checksum += c.toJson(c.scratch);
        case 'plant-report':
          // THE NEGATIVE CONTROL: one object per record, of a class the code
          // under test could plausibly construct
          report.clear();
          reader.attach(c.wire, report);
          c.loadBody(reader);
          checksum += reader.offset;
          final planted = TableReport();
          plantRing[i & 63] = planted;
          checksum += planted.unknown;
        case 'plant-bytes':
          // and the other: eight bytes per record
          report.clear();
          reader.attach(c.wire, report);
          c.loadBody(reader);
          checksum += reader.offset;
          final planted = Uint8List(8);
          plantRing[i & 63] = planted;
          checksum += planted.length;
      }
    }
  }
}
