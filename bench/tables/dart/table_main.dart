// the tables bench — the Dart runner, THE FIXED FORM ONLY.
//
// A port of bench/tables/cpp/table_main.cpp (the reference implementation)
// against the generated Dart table codec. It measures ONE thing: the paired
// bench's sixty-four logical records written and read in the FIXED FORM,
// form byte 3 (docs/SPEC-TABLES.md §3.4), through the generated codec.
//
// WHY THERE IS NO TOLERANT HALF HERE: Dart has NO FORM-1 TABLE WIRE (schema#514).
// The backend carries form 3 and only form 3, so there is no `BenchMixedSave`/`Load`
// on the tolerant wire for this leg to gate or to time.
// What it CAN do with the tolerant corpus, it does — see `checkTolerantCorpus` below:
// the framing arithmetic is checkable without a decoder, and the bytes are what
// the corpus id is made of.
//
// Output: a human table on stderr; with --csv, CSV v2 rows on stdout.

import 'dart:io';
import 'dart:typed_data';

import '../../../generated/bench/paired/dart/BenchFixed.dart';
import '../../../generated/bench/paired/dart/FixedTableFixed.dart';

const int maxNumRuns = 7;
const int numVariants = 64;
const int defaultIterations = 400000;

int numRuns = maxNumRuns;
int gIterations = 0;
bool gCsv = false;
bool gGate = false;
bool gQuick = false;
bool gIndexed = false;
String gWireDir = 'testdata/wire';
String gVariantDir = 'bench/paired/corpus';
bool failed = false;

// ---- CSV v2 (BENCH-STANDARD.md §5.1) ----
// family `table` (§1.9).
// linkage aot — compiled into a standalone whole-program AOT binary.
// checks contract — wire-contract validation unconditional, caller-error asserts dormant.
// opt default — dart compile exe optimization level.
// inline unknown.
const String csvSuffix = 'aot,contract,default,unknown';
final List<String> gCsvRows = [];
final Map<String, Uint8List> gGoldensLoaded = {};

int gSink = 0;
int gProbe = 0;

final Stopwatch _clock = Stopwatch()..start();
double now() => _clock.elapsedMicroseconds * 1e-6;

void fail(String name, String what) {
  stderr.write('FAILED: $name: $what\n');
  failed = true;
}

Uint8List? readCorpus(String dir, String basename) {
  var path = '$dir/$basename';
  var file = File(path);
  if (!file.existsSync() && File('$gVariantDir/$basename').existsSync()) {
    file = File('$gVariantDir/$basename');
  }
  if (!file.existsSync()) {
    stderr.write(
      'missing corpus $path — run from the schema repo root (or pass --wire-dir/--variant-dir)\n',
    );
    return null;
  }
  final bytes = file.readAsBytesSync();
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

String corpusId() {
  const int mask = 0xFFFFFFFFFFFFFFFF;
  var h = 0xcbf29ce484222325;
  void mix(int byte) {
    h = ((h ^ byte) * 0x100000001b3) & mask;
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

final class RunStats {
  final double median;
  final double min;
  final double max;
  final double spread;
  RunStats(this.median, this.min, this.max, this.spread);
}

RunStats stats(List<double> rates) {
  final sorted = List<double>.from(rates)..sort();
  final n = sorted.length;
  final median = sorted[n ~/ 2];
  final min = sorted.first;
  final max = sorted.last;
  final spread = (max - min) / median * 100.0;
  return RunStats(median, min, max, spread);
}

void report(
  String bench,
  String path,
  int iters,
  double bytesPerOp,
  RunStats s,
) {
  final mbps = s.median * bytesPerOp / (1024.0 * 1024.0);
  stderr.write(
    '${bench.padRight(18)} ${path.padRight(11)} '
    '${(s.median / 1e6).toStringAsFixed(3).padLeft(10)} M msg/s '
    '${mbps.toStringAsFixed(1).padLeft(10)} MB/s   '
    '(min ${(s.min / 1e6).toStringAsFixed(3)}, max ${(s.max / 1e6).toStringAsFixed(3)}, '
    'spread ${s.spread.toStringAsFixed(1)}%)\n',
  );
  if (gCsv) {
    gCsvRows.add(
      'dart,$bench,$path,$iters,$bytesPerOp,$numRuns,'
      '${s.median.toStringAsFixed(0)},${s.min.toStringAsFixed(0)},${s.max.toStringAsFixed(0)},'
      '${mbps.toStringAsFixed(2)},${s.spread.toStringAsFixed(2)}',
    );
  }
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

bool checkTolerantCorpus(String name, String golden) {
  final packed = readCorpus(gVariantDir, '$name.variants.bin');
  var pinned = readCorpus(gWireDir, '$golden.bin');
  if (packed == null || pinned == null) {
    return false;
  }
  final lengths = List<int>.filled(numVariants, 0);
  if (gIndexed) {
    final index = readCorpus(gVariantDir, '$name.lengths');
    if (index == null || index.length != numVariants * 4) {
      fail(name, 'the length index is not 64 little-endian uint32 lengths');
      return false;
    }
    final view = ByteData.sublistView(index);
    for (var k = 0; k < numVariants; k++) {
      lengths[k] = view.getUint32(k * 4, Endian.little);
    }
  } else {
    if (packed.isEmpty || packed.length % numVariants != 0) {
      fail(name, 'the unindexed corpus is not 64 equal records');
      return false;
    }
    lengths.fillRange(0, numVariants, packed.length ~/ numVariants);
  }

  // If pinned wire in gWireDir differs in size from variant 0, check variantDir
  if (pinned.length != lengths[0] &&
      File('$gVariantDir/$golden.bin').existsSync()) {
    pinned = readCorpus(gVariantDir, '$golden.bin')!;
  }

  var offset = 0;
  for (var k = 0; k < numVariants; k++) {
    if (lengths[k] <= 0 || offset + lengths[k] > packed.length) {
      fail(name, "variant $k's length does not fit the corpus");
      return false;
    }
    offset += lengths[k];
  }
  if (offset != packed.length) {
    fail(name, 'the lengths do not sum to the corpus size');
    return false;
  }
  if (!bytesEqual(pinned, Uint8List.sublistView(packed, 0, lengths[0]))) {
    stderr.write(
      'WIRE GOLDEN MISMATCH: $golden (${pinned.length} golden vs ${lengths[0]} corpus bytes) — '
      'refusing to bench against a corpus whose variant 0 is not the pinned instance\n',
    );
    failed = true;
    return false;
  }
  return true;
}

void benchFixed(String name, int baseIters) {
  final iters = gIterations != 0 ? gIterations : baseIters;
  final file = readCorpus(gVariantDir, 'bench_fixed.bin');
  final vocab = readCorpus(gVariantDir, 'bench_fixed.layout');
  if (file == null || vocab == null || file.isEmpty) {
    failed = true;
    return;
  }
  final double bytesPerOp = file.length / numVariants;
  final twin = Uint8List(file.length);

  // gate 1: THE BLOCK IS THE CORPUS'S BLOCK.
  if (vocab.length != fixedTableFixedLayout.length ||
      !bytesEqual(vocab, fixedTableFixedLayout)) {
    fail(
      name,
      "this build's vocabulary layout is not the corpus's, byte for byte",
    );
    return;
  }

  // The measured shape, named once: FixedTable
  final values = List.generate(numVariants, (_) => FixedTable());
  final out = List.generate(numVariants, (_) => FixedTable());
  final plan = fixedTableFixedNewPlan();
  final outPlan = fixedTableFixedNewPlan();
  final report_ = TableFixedReport();

  // gate 2: the whole file loads clean.
  if (fixedTableFixedLoad(
        values,
        numVariants,
        file,
        file.length,
        plan,
        report_,
      ) !=
      numVariants) {
    fail(name, 'the fixed corpus did not load');
    return;
  }

  // gate 3: writing them back reproduces the file, byte for byte.
  final wrote = fixedTableFixedSave(values, numVariants, twin);
  if (wrote != file.length || !bytesEqual(twin, file)) {
    fail(
      name,
      'round-trip bytes differ — refusing to bench a codec that does not reproduce the corpus',
    );
    return;
  }

  // gate 4: reused storage round-trips too.
  for (var k = 0; k < 2; k++) {
    if (fixedTableFixedLoad(
              out,
              numVariants,
              file,
              file.length,
              outPlan,
              report_,
            ) !=
            numVariants ||
        fixedTableFixedSave(out, numVariants, twin) != file.length ||
        !bytesEqual(twin, file)) {
      fail(name, 'reused target round-trip bytes differ');
      return;
    }
  }

  if (gGate) {
    return;
  }

  final need = fixedTableFixedMeasure(numVariants);
  final buffer = Uint8List(file.length);
  buffer.setRange(0, file.length, file);

  final writeRates = <double>[];
  final roundTripRates = <double>[];

  // WRITE: one call lays down all 64 records; an op is one record.
  for (var run = -1; run < numRuns; run++) {
    final start = now();
    for (var i = 0; i < iters; i += numVariants) {
      final w = fixedTableFixedSave(values, numVariants, buffer);
      if (w != file.length) {
        fail(name, 'save failed in loop');
        return;
      }
      gProbe = (gProbe + 1 < need) ? gProbe + 1 : 0;
      gSink = (gSink + w + buffer[gProbe]) & 0xffffffff;
    }
    final elapsed = now() - start;
    if (run >= 0) {
      writeRates.add(iters / elapsed);
    }
  }

  // ROUND-TRIP: read the file back, then write what came out.
  for (var run = -1; run < numRuns; run++) {
    final start = now();
    for (var i = 0; i < iters; i += numVariants) {
      if (fixedTableFixedLoad(
            out,
            numVariants,
            buffer,
            file.length,
            outPlan,
            report_,
          ) !=
          numVariants) {
        fail(name, 'load failed in loop');
        return;
      }
      final w = fixedTableFixedSave(out, numVariants, twin);
      if (w != file.length) {
        fail(name, 're-save failed in loop');
        return;
      }
      gProbe = (gProbe + 1 < need) ? gProbe + 1 : 0;
      gSink = (gSink + w + twin[gProbe]) & 0xffffffff;
    }
    final elapsed = now() - start;
    if (run >= 0) {
      roundTripRates.add(iters / elapsed);
    }
  }

  final w = stats(writeRates);
  final rt = stats(roundTripRates);
  report(name, 'write', iters, bytesPerOp, w);
  report(name, 'round_trip', iters, bytesPerOp, rt);

  // READ is DERIVED, never measured: round-trip time minus write time.
  final readTime = 1.0 / rt.median - 1.0 / w.median;
  if (readTime > 0) {
    stderr.write(
      '${name.padRight(18)} ${'read'.padRight(11)} '
      '${(1e-6 / readTime).toStringAsFixed(3).padLeft(10)} M msg/s   '
      '(DERIVED: round-trip minus write, informational — not a measured row)\n',
    );
  }
}

void usage() {
  stderr.write(
    'usage: table_main.dart [--indexed] [--gate] [--csv] [--round K] '
    '[--iterations N] [--quick] [--wire-dir <dir>] [--variant-dir <dir>]\n',
  );
  exit(1);
}

void main(List<String> args) {
  for (var i = 0; i < args.length; i++) {
    switch (args[i]) {
      case '--indexed':
        gIndexed = true;
      case '--gate':
        gGate = true;
      case '--csv':
        gCsv = true;
      case '--quick':
        gQuick = true;
      case '--wire-dir':
        if (i + 1 >= args.length) usage();
        gWireDir = args[++i];
      case '--variant-dir':
        if (i + 1 >= args.length) usage();
        gVariantDir = args[++i];
      case '--iterations':
        if (i + 1 >= args.length) usage();
        final n = int.tryParse(args[++i]);
        if (n == null || n <= 0 || n % numVariants != 0) {
          stderr.write(
            '--iterations requires a positive multiple of 64 up to 2147483584\n',
          );
          exit(1);
        }
        gIterations = n;
      case '--round':
        if (i + 1 >= args.length) usage();
        final k = int.tryParse(args[++i]);
        if (k == null || k < 0) {
          stderr.write(
            "--round takes a non-negative integer, got '${args[i]}'\n",
          );
          exit(1);
        }
        numRuns = 1;
      default:
        usage();
    }
  }

  if (gQuick && numRuns == maxNumRuns) {
    numRuns = 3;
  }

  stderr.write('schema tables bench (dart, the FIXED FORM)\n');

  if (checkTolerantCorpus('bench_table', 'bench_table')) {
    benchFixed('bench_fixed', defaultIterations);
  }

  flushCsv();

  if (failed) {
    stderr.write('TABLES BENCH FAILED (corpus_id ${corpusId()})\n');
    exit(1);
  }
  stderr.write('OK (corpus_id ${corpusId()})\n');

  if (Platform.environment['SERIALIZE_BENCH_SINK'] != null) {
    stderr.write('sink: $gSink\n');
  }
}
