// the tables bench — the Dart runner, THE VARIABLE FORM (form byte 1).
//
// The Dart twin of bench/tables/cpp/table_main.cpp's `bench_table` arm, the
// reference implementation: ONE representative table (`TableMixed`, the root
// of bench/corpus/BenchTable.schema) written and loaded on the tolerant wire
// (docs/SPEC-TABLES.md §3), over the board's own corpus — the pinned instance
// testdata/wire/bench_table.bin and its 64 rotating variants
// bench/corpus/variants/bench_table.variants.bin (bench/tables/README.md).
//
// WHY THIS IS NOT bench/tables/dart/table_main.dart. That runner is the FIXED
// form's leg for bench/paired and answers to the PAIRED corpus's id and its
// generated unit (the packet and fixed-table schemas). The variable form's
// corpus is a different corpus under a different id (§1.6) and a different
// generated unit (BenchTable.schema), exactly as the C, C++, C# and Go legs
// keep them: two passes, two boards. The fixed runner is untouched, so no
// paired row moves.
//
// THE FORM THAT IS TIMED IS THE AOT EXECUTABLE (`dart compile exe`), the form
// that ships, as bench/tables/dart/leg builds it:
//
//   dart compile exe bench/tables/dart/table_form1_main.dart -o build/schema_tables_bench_dart
//   build/schema_tables_bench_dart [--gate] [--csv] [--round K] [--iterations N]
//
// THE GATES RUN BEFORE THE CLOCK, and a failing gate emits NO rows (§1.5):
//
//   1. variant 0 IS the pinned instance, byte for byte;
//   2. every one of the 64 variants loads clean (nothing unknown, mismatched,
//      clamped, duplicate or malformed) and re-saves to the SAME bytes;
//   3. two complete rotations load and re-save byte-identically, including
//      the last-to-first transition. The Dart load builds its result object
//      afresh, so there is no caller-owned target to reuse and no caller reset
//      to omit; the rotation proves no state leaks between loads regardless.
//
// THE DART COSTS THIS LEG PAYS, which C and C++ do not: Save returns a fresh
// Uint8List and Load a fresh object graph (a nested object per entity, a list
// per bounded array, a String per text field), so write and round_trip both
// carry allocation, and round_trip carries the object projection. That is the
// language's value model and the shipped API, so it is the number.
//
// THE SINK. Dart has no empty-asm memory clobber, so the write arm folds one
// written byte per call, at a rotating offset, into a sink published at exit
// under an environment variable the compiler cannot rule out. The read arm
// needs none: its output IS the re-save's input.
//
// THIS FILE IS SHAPE-BLIND: it names the generated type and its generated
// Save/Load at those call sites and nothing else (`make shape-gate`;
// bench/tables/dart/SHAPE-GATE.allow).
//
// Output: a human table on stderr; with --csv, CSV v2 rows on stdout. Run from
// the repository root.
import 'dart:io';
import 'dart:typed_data';

import '../../../generated/bench/tables/dart/BenchTableWire.dart'
    show TableMixed, TableWireReport, tableMixedWireLoad, tableMixedWireSave;

const int maxNumRuns = 7; // median of 7 (N >= 5), after 1 warmup run
const int numVariants = 64;
// Dart's variable-form Save and Load allocate a fresh buffer and object graph
// per call (about 10 thousand records a second on a fleet bench), so the
// standard 400000 would take eleven minutes per sitting. 32000 keeps a full
// seven-run measurement near a minute. The count is a column of every row.
const int defaultIterations = 32000;

int gNumRuns = maxNumRuns; // --round K drops this to 1 (§2.4)
int gIterations = 0;
bool gCsv = false;
bool gGate = false;
String gWireDir = 'testdata/wire';
String gVariantDir = 'bench/corpus/variants';
bool failed = false;

final Stopwatch _clock = Stopwatch()..start();
double now() => _clock.elapsedMicroseconds * 1e-6;

// CSV v2 (BENCH-STANDARD.md §5.1): family `table`, linkage aot, checks
// contract (the codec's reader validates the wire contract in every build),
// opt default, inline unknown — the fixed runner's columns, for the same
// reasons. No `codec` column: one codec.
const String csvSuffix = 'aot,contract,default,unknown';
final List<String> gCsvRows = [];
final Map<String, Uint8List> gGoldensLoaded = {};
int gSink = 0;

// §1.6: FNV-1a-64 over the goldens THIS RUN LOADED, in sorted basename order:
// the basename bytes, a 0x00 byte, then the contents.
String corpusId() {
  var h = 0xcbf29ce484222325;
  void mix(int byte) {
    h = (h ^ byte) * 0x100000001b3;
  }

  final names = gGoldensLoaded.keys.toList()..sort();
  for (final name in names) {
    for (final b in name.codeUnits) {
      mix(b);
    }
    mix(0);
    for (final b in gGoldensLoaded[name]!) {
      mix(b);
    }
  }
  final hi = (h >>> 32) & 0xffffffff;
  final lo = h & 0xffffffff;
  return hi.toRadixString(16).padLeft(8, '0') +
      lo.toRadixString(16).padLeft(8, '0');
}

void flushCsv() {
  if (!gCsv) {
    return;
  }
  if (failed) {
    stderr.write('refusing to emit CSV rows from a failing run\n');
    return;
  }
  final id = corpusId();
  final out = StringBuffer(
    'lang,bench,path,iters,bytes_per_op,runs,median_msgs_per_sec,min_msgs_per_sec,'
    'max_msgs_per_sec,median_mb_per_sec,spread_pct,corpus_id,family,linkage,checks,opt,inline\n',
  );
  for (final row in gCsvRows) {
    out.write('$row,$id,table,$csvSuffix\n');
  }
  stdout.write(out.toString());
}

void fail(String name, String what) {
  stderr.write('FAILED: $name: $what\n');
  failed = true;
}

Uint8List? readCorpus(String dir, String basename) {
  final path = '$dir/$basename';
  Uint8List bytes;
  try {
    bytes = File(path).readAsBytesSync();
  } on FileSystemException {
    stderr.write(
      'missing corpus $path — run from the schema repo root '
      '(or pass --wire-dir/--variant-dir)\n',
    );
    return null;
  }
  gGoldensLoaded[basename] = bytes;
  return bytes;
}

bool bytesEqual(Uint8List a, Uint8List b) {
  if (a.length != b.length) {
    return false;
  }
  for (var i = 0; i < a.length; i++) {
    if (a[i] != b[i]) {
      return false;
    }
  }
  return true;
}

final class Stats {
  final double median;
  final double min;
  final double max;
  final double spread;
  Stats(this.median, this.min, this.max, this.spread);
}

Stats stats(List<double> rates) {
  final sorted = List<double>.from(rates)..sort();
  final n = sorted.length;
  final median = sorted[n ~/ 2];
  return Stats(
    median,
    sorted.first,
    sorted.last,
    (sorted.last - sorted.first) / median * 100.0,
  );
}

void report(String path, int iters, double bytesPerOp, Stats s) {
  const bench = 'bench_table';
  final mbps = s.median * bytesPerOp / (1024.0 * 1024.0);
  stderr.write(
    '${bench.padRight(18)} ${path.padRight(11)} '
    '${(s.median / 1e6).toStringAsFixed(3).padLeft(10)} M msg/s '
    '${mbps.toStringAsFixed(1).padLeft(10)} MB/s   '
    '(min ${(s.min / 1e6).toStringAsFixed(3)}, '
    'max ${(s.max / 1e6).toStringAsFixed(3)}, '
    'spread ${s.spread.toStringAsFixed(1)}%)\n',
  );
  if (gCsv) {
    gCsvRows.add(
      'dart,$bench,$path,$iters,$bytesPerOp,$gNumRuns,'
      '${s.median.toStringAsFixed(0)},${s.min.toStringAsFixed(0)},'
      '${s.max.toStringAsFixed(0)},'
      '${mbps.toStringAsFixed(2)},${s.spread.toStringAsFixed(2)}',
    );
  }
}

bool loadsClean(TableWireReport r) =>
    r.unknown == 0 &&
    r.kindMismatch == 0 &&
    r.clamped == 0 &&
    r.duplicate == 0 &&
    !r.malformed &&
    !r.refused;

// The corpus is the board's historical fixed-stride one: 64 equal records.
void benchTable(String name, int baseIters) {
  final iters = gIterations != 0 ? gIterations : baseIters;
  final packed = readCorpus(gVariantDir, '$name.variants.bin');
  final pinned = readCorpus(gWireDir, '$name.bin');
  if (packed == null ||
      pinned == null ||
      packed.isEmpty ||
      packed.length % numVariants != 0) {
    failed = true;
    return;
  }
  final record = packed.length ~/ numVariants;
  final variants = <Uint8List>[
    for (var k = 0; k < numVariants; k++)
      Uint8List.fromList(
        Uint8List.sublistView(packed, k * record, (k + 1) * record),
      ),
  ];

  // gate 1 (§1.5): variant 0 IS the pinned instance.
  if (!bytesEqual(pinned, variants[0])) {
    fail(name, 'WIRE GOLDEN MISMATCH: variant 0 is not the pinned instance');
    return;
  }

  // gate 2: every variant loads clean, re-saves, and comes back
  // byte-identical at the same length — before any clock starts.
  final instances = <TableMixed>[];
  for (var k = 0; k < numVariants; k++) {
    final r = TableWireReport();
    final v = tableMixedWireLoad(variants[k], r);
    if (!loadsClean(r)) {
      fail(name, 'load of variant $k failed (${r.reason})');
      return;
    }
    if (!bytesEqual(tableMixedWireSave(v), variants[k])) {
      fail(
        name,
        'variant round-trip bytes differ — refusing to bench a codec that '
        'does not reproduce the corpus',
      );
      return;
    }
    instances.add(v);
  }

  // gate 3: two complete rotations, including the last-to-first transition.
  for (var k = 0; k < 2 * numVariants; k++) {
    final index = k & (numVariants - 1);
    final r = TableWireReport();
    final v = tableMixedWireLoad(variants[index], r);
    if (!loadsClean(r) || !bytesEqual(tableMixedWireSave(v), variants[index])) {
      fail(name, 'rotation round-trip bytes differ');
      return;
    }
  }

  stderr.write(
    '$name: gates passed before the clock — golden, load, round-trip, '
    'rotation (4 of 4)\n',
  );
  if (gGate) {
    return;
  }

  final bytesPerOp = record.toDouble();
  final writeRates = List<double>.filled(gNumRuns, 0);
  final roundTripRates = List<double>.filled(gNumRuns, 0);

  // WRITE: save the 64 pre-loaded instances round-robin (§2.7 variation); the
  // sink reads one written byte back at a rotating offset.
  for (var run = -1; run < gNumRuns; run++) {
    final start = now();
    for (var i = 0; i < iters; i++) {
      final wrote = tableMixedWireSave(instances[i & (numVariants - 1)]);
      if (wrote.length != record) {
        fail(name, 'save failed in loop');
        return;
      }
      gSink = gSink + wrote.length + wrote[i % record];
    }
    final elapsed = now() - start;
    if (run >= 0) {
      writeRates[run] = iters / elapsed;
    }
  }

  // ROUND-TRIP: load a variant buffer, then re-save what came out. The load
  // needs no sink of its own: its output IS the save's input.
  final report_ = TableWireReport();
  for (var run = -1; run < gNumRuns; run++) {
    final start = now();
    for (var i = 0; i < iters; i++) {
      final v = tableMixedWireLoad(variants[i & (numVariants - 1)], report_);
      final wrote = tableMixedWireSave(v);
      if (wrote.length != record) {
        fail(name, 're-save failed in loop');
        return;
      }
      gSink = gSink + wrote.length + wrote[i % record];
    }
    final elapsed = now() - start;
    if (run >= 0) {
      roundTripRates[run] = iters / elapsed;
    }
  }
  if (!loadsClean(report_)) {
    fail(name, 'a load in the round-trip loop was not clean');
    return;
  }

  final w = stats(writeRates);
  final rt = stats(roundTripRates);
  report('write', iters, bytesPerOp, w);
  report('round_trip', iters, bytesPerOp, rt);

  // READ is DERIVED, never measured (§2.9): informational, not a CSV row.
  final readTime = 1.0 / rt.median - 1.0 / w.median;
  if (readTime > 0) {
    stderr.write(
      '${name.padRight(18)} ${'read'.padRight(11)} '
      '${(1e-6 / readTime).toStringAsFixed(3).padLeft(10)} M msg/s   '
      '(DERIVED: round-trip minus write, informational — not a measured row)\n',
    );
  }
}

Never usage() {
  stderr.write(
    'usage: table-form1-dart [--gate] [--csv] [--round K] [--iterations N] '
    '[--wire-dir <dir>] [--variant-dir <dir>]\n',
  );
  exit(1);
}

void main(List<String> args) {
  for (var i = 0; i < args.length; i++) {
    final a = args[i];
    if (a == '--gate') {
      gGate = true;
    } else if (a == '--csv') {
      gCsv = true;
    } else if (a == '--wire-dir' && i + 1 < args.length) {
      gWireDir = args[++i];
    } else if (a == '--variant-dir' && i + 1 < args.length) {
      gVariantDir = args[++i];
    } else if (a == '--iterations' && i + 1 < args.length) {
      final n = int.tryParse(args[++i]);
      if (n == null || n <= 0 || n > 2147483584 || n % numVariants != 0) {
        stderr.write(
          '--iterations requires a positive multiple of 64 up to 2147483584\n',
        );
        exit(1);
      }
      gIterations = n;
    } else if (a == '--round' && i + 1 < args.length) {
      // §2.4: one warmup + one measured run; K only identifies the round.
      final k = int.tryParse(args[++i]);
      if (k == null || k < 0) {
        stderr.write(
          "--round takes a non-negative integer, got '${args[i]}'\n",
        );
        exit(1);
      }
      gNumRuns = 1;
    } else {
      usage();
    }
  }

  stderr.write(
    'schema tables bench (dart ${Platform.version.split(' ').first}, '
    'the VARIABLE FORM)\n',
  );

  benchTable('bench_table', defaultIterations);

  flushCsv();

  if (failed) {
    stderr.write('TABLES BENCH FAILED (corpus_id ${corpusId()})\n');
    exit(1);
  }
  stderr.write(
    'OK (corpus_id ${corpusId()}, sink ${gSink == 0 ? '0' : 'nonzero'})\n',
  );
  if (Platform.environment['SERIALIZE_BENCH_SINK'] != null) {
    stderr.write('sink: $gSink\n');
  }
}
