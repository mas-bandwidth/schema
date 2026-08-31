// schema bench — the Dart runner: a conforming run.sh leg per
// bench/README.md's runner contract, measuring the generated monomorphic
// Dart codecs over bench/corpus/Bench.schema's BenchMixed.
//
// CONTRACT (BENCH-STANDARD.md): fixed iteration counts identical to every
// other runner's rows for these benches; 1 discarded warmup run then 7
// measured runs per (bench, path) — or exactly one measured run under
// --round K, where the interleaved driver aggregates across rounds (§2.4);
// 64 rotating variant buffers on both paths; median/min/max/spread over
// the measured runs; CSV v2 rows on stdout under --csv, human table on
// stderr.
//
// WHAT THE ROWS MEASURE: the generated codec — schema's Dart backend emits
// self-contained monomorphic write/read functions with the bitpacker
// inlined and zero runtime dependencies, so the generated code IS the Dart
// serialize path. The rows carry family=gen — the estate's one benchmark
// subject (schema#196).
// Peak-style numbers from this runner's earlier serialize-family form
// (tight per-shape loops, best-of-five) are NOT comparable to these rows —
// different measurement contract, and the statistic alone moves the number.
//
// GOLDEN GATED (§1.5): before any timing, variant 0 of the committed
// variant corpus is byte-compared against the C++-pinned testdata/wire
// golden, and every variant is decoded and re-encoded byte-identically.
// A runner that mismatches REFUSES to bench.
// corpus_id is FNV-1a-64 over the goldens this run actually loaded (§1.6).
//
// The timed form is the AOT executable (run.sh builds it) — the number
// that ships. JIT (`dart main.dart`) runs the same contract for iteration.
//
//   dart compile exe bench/dart/main.dart -o build/schema_bench_dart
//   cd bench/dart && ../../build/schema_bench_dart [--csv] [--round K] [--quick]
//
// --quick: 3 measured runs instead of 7 — run.sh's iteration instrument,
// never the certification instrument. bench_mixed is the whole leg either
// way.
import 'dart:io';
import 'dart:typed_data';

import '../../generated/bench/dart/Bench.dart';

const int numVariants = 64;

// §1.2/§2.1: fixed per-benchmark iteration counts, identical across every
// language's rows for these benches, recorded in the iters column
const int mixedIters = 4000000;

bool csv = false;
bool quick = false;
int numRuns = 7;

final Stopwatch _clock = Stopwatch()..start();

double now() => _clock.elapsedMicroseconds * 1e-6;

// the g_sink of the C bench: computed values flow here, and the bench
// publishes it at exit under an env var the compiler cannot rule out, so no
// loop's work can be deleted
int sink = 0;

/* --------------------------------------------------------------------------
   read-side sink discipline (#175, equalized to the cpp/c reference): every
   read loop observes the FULL decoded struct per iteration. The C/C++ legs
   get this for free from an empty-asm memory clobber over the whole struct;
   Dart has no zero-cost clobber, so the leg's idiom is a per-iteration sum
   of every decoded field — doubles truncated via toInt(), booleans as 0/1,
   byte arrays element-by-element. The sink adds are real work the clobber
   languages do not pay; the published number is an upper bound on the
   observation cost.
   -------------------------------------------------------------------------- */

Never gateFail(String row, String what) {
  stderr.write('GOLDEN GATE FAILED: $row $what\nreporting nothing.\n');
  exit(1);
}

/* --------------------------------------------------------------------------
   CSV v2 (§5.1): rows collected and flushed with the corpus_id of the
   goldens this run loaded; the per-runner constants — family gen (the rows
   measure generated code), linkage aot (codec compiled with the caller into
   one whole-program AOT executable, no library boundary), checks contract
   (caller-error asserts dormant in the AOT/product form, wire-contract
   validation unconditional in the reader), opt default (Dart has no level),
   inline unknown (the §4 verdict pass has no dart branch).
   -------------------------------------------------------------------------- */

const String csvSuffix = 'gen,aot,contract,default,unknown';
final List<String> csvRows = [];
final Map<String, Uint8List> goldensLoaded = {};

String corpusId() {
  const int mask = 0xFFFFFFFFFFFFFFFF;
  var h = 0xcbf29ce484222325;
  void mix(int byte) {
    h = ((h ^ byte) * 0x100000001b3) & mask;
  }

  final names = goldensLoaded.keys.toList()..sort();
  for (final name in names) {
    for (final b in name.codeUnits) {
      mix(b);
    }
    mix(0);
    for (final b in goldensLoaded[name]!) {
      mix(b);
    }
  }
  // native Dart ints are signed 64-bit and toRadixString would print a
  // negative id: format as two logical-shifted 32-bit halves instead
  final hi = (h >>> 32) & 0xffffffff;
  final lo = h & 0xffffffff;
  return hi.toRadixString(16).padLeft(8, '0') + lo.toRadixString(16).padLeft(8, '0');
}

void report(String bench, String path, int iters, int bytesPerOp, List<double> rates) {
  final sorted = List<double>.from(rates)..sort();
  final median = sorted[sorted.length ~/ 2];
  final min = sorted.first;
  final max = sorted.last;
  final spread = (max - min) / median * 100.0;
  final mbps = median * bytesPerOp / (1024.0 * 1024.0);
  stderr.write(
    '${bench.padRight(18)} ${path.padRight(5)} '
    '${(median / 1e6).toStringAsFixed(2).padLeft(10)} M msg/s '
    '${mbps.toStringAsFixed(1).padLeft(10)} MB/s   '
    '(min ${(min / 1e6).toStringAsFixed(2)}, max ${(max / 1e6).toStringAsFixed(2)}, '
    'spread ${spread.toStringAsFixed(1)}%)\n',
  );
  if (csv) {
    csvRows.add(
      'dart,$bench,$path,$iters,$bytesPerOp,${rates.length},'
      '${median.toStringAsFixed(0)},${min.toStringAsFixed(0)},${max.toStringAsFixed(0)},'
      '${mbps.toStringAsFixed(2)},${spread.toStringAsFixed(2)}',
    );
  }
}

void flushCsv() {
  if (!csv) {
    return;
  }
  final id = corpusId();
  final out = StringBuffer(
    'lang,bench,path,iters,bytes_per_op,runs,median_msgs_per_sec,min_msgs_per_sec,'
    'max_msgs_per_sec,median_mb_per_sec,spread_pct,corpus_id,family,linkage,checks,opt,inline\n',
  );
  for (final row in csvRows) {
    out.write('$row,$id,$csvSuffix\n');
  }
  stdout.write(out.toString());
}

/* --------------------------------------------------------------------------
   the C bench's uint64 LCG, direct: Dart's int is 64 bits and wraps two's
   complement, which IS arithmetic mod 2^64
   -------------------------------------------------------------------------- */

/* --------------------------------------------------------------------------
   the golden gate (§1.5), shared by every shape: the PINNED instance's
   bytes must equal the C++-pinned testdata/wire golden, and all 64 variant
   buffers must decode back field-perfect. A mismatch refuses to bench.
   -------------------------------------------------------------------------- */

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

/* --------------------------------------------------------------------------
   the DATA-DRIVEN driver for bench_mixed (issue #191).

   THE PROPERTY: nothing below names a field of the shape it measures. Shape
   knowledge lives in the committed variant DATA (bench/corpus/variants,
   emitted by bench/tools/variantgen) and in the generated codec, and nowhere
   else — so this driver cannot drift from another language's driver in what
   it measures, which is the whole reason the design exists. If a change here
   ever needs a field name, the design has failed and that is the finding.

   It is the ONLY driver in this runner: bench_mixed is the whole leg, and
   no hand-written pin, vary, field-check or sink function survives here.
   -------------------------------------------------------------------------- */

final class _DataDrivenShape<P> {
  final List<P> instances;
  final P decoded;
  final List<ByteData> variants;
  final int bytesPerPacket;
  _DataDrivenShape(
    this.instances,
    this.decoded,
    this.variants,
    this.bytesPerPacket,
  );
}

// P — the generated message type — is named once at the call site, through a
// factory. A TYPE name is not a field name; the driver knows nothing about
// the shape's contents.
_DataDrivenShape<P> gateDataDriven<P>(
  String row,
  String goldenName,
  P Function() make,
  int Function(P, ByteData) write,
  bool Function(P, ByteData, int) read,
) {
  // The records are fixed-width by construction (§2.7 pins every structure
  // field), so the file needs no index: the record size IS file size /
  // numVariants, and a file that does not divide evenly is a refusal.
  final path = '../../bench/corpus/variants/$row.variants.bin';
  final file = File(path);
  if (!file.existsSync()) {
    stderr.write(
      'missing variant data $path — run `make bench-variants`, and run the '
      'bench from bench/dart\n',
    );
    exit(1);
  }
  final packed = file.readAsBytesSync();
  if (packed.isEmpty || packed.length % numVariants != 0) {
    gateFail(
      row,
      'variant data $path is ${packed.length} bytes, not a multiple of '
      '$numVariants records — refusing to bench data whose stride is not the '
      'record size',
    );
  }
  final record = packed.length ~/ numVariants;
  final variants = <ByteData>[];
  for (var k = 0; k < numVariants; k++) {
    final slot = Uint8List(512);
    slot.setRange(0, record, packed, k * record);
    variants.add(ByteData.sublistView(slot, 0, record));
  }
  // The variant data is corpus (§1.6): it defines the work inside the timed
  // loops, so it rides in corpus_id exactly as the wire goldens do.
  goldensLoaded['$row.variants.bin'] = packed;

  // gate 1 (§1.5): variant 0 IS the pinned instance, so the whole variant
  // file is bound to the wire golden by one byte-compare.
  final goldenFile = File('../../testdata/wire/$goldenName.bin');
  if (!goldenFile.existsSync()) {
    stderr.write(
      'missing wire golden testdata/wire/$goldenName.bin — run from bench/dart\n',
    );
    exit(1);
  }
  final goldenBytes = goldenFile.readAsBytesSync();
  goldensLoaded['$goldenName.bin'] = goldenBytes;
  if (!bytesEqual(Uint8List.sublistView(packed, 0, record), goldenBytes)) {
    gateFail(row, 'variant 0 vs testdata/wire/$goldenName.bin');
  }

  // gate 2: every variant decodes, re-encodes, and comes back byte-identical
  // at the same length. This is stronger than the pinned-instance-only gate
  // the retired hand-written shapes applied — §1.5's named residual (the 64
  // varied buffers
  // length-checked but never value-checked) closes here, for every variant.
  final twin = Uint8List(512);
  final twinView = ByteData.sublistView(twin);
  final instances = <P>[];
  for (var v = 0; v < numVariants; v++) {
    final instance = make();
    if (!read(instance, variants[v], record * 8)) {
      gateFail(row, 'decode of variant $v failed');
    }
    final n = write(instance, twinView);
    if (n != record ||
        !bytesEqual(
          Uint8List.sublistView(twin, 0, n),
          Uint8List.sublistView(packed, v * record, (v + 1) * record),
        )) {
      gateFail(
        row,
        'variant $v round-trip bytes differ — refusing to bench a codec that '
        'does not reproduce the corpus',
      );
    }
    instances.add(instance);
  }
  return _DataDrivenShape(instances, make(), variants, record);
}

void benchDataDriven<P>(
  String row,
  _DataDrivenShape<P> gated,
  int iters,
  int Function(P, ByteData) write,
  bool Function(P, ByteData, int) read,
) {
  final instances = gated.instances;
  final decoded = gated.decoded;
  final variants = gated.variants;
  final numBits = gated.bytesPerPacket * 8;
  final buffer = Uint8List(512);
  final view = ByteData.sublistView(buffer);

  // WRITE: encode the 64 pre-decoded instances round-robin. Rotating the
  // instances is what §2.7's per-iteration LCG mutation bought — the encoder
  // never sees the same input twice in a row and cannot precompute scratch
  // words — with none of the per-language mutation code, and with bytes/op
  // constant by construction rather than by assertion.
  final writeRates = <double>[];
  for (var run = -1; run < numRuns; run++) {
    final start = now();
    for (var i = 0; i < iters; i++) {
      sink =
          (sink + write(instances[i & (numVariants - 1)], view)) & 0xffffffff;
    }
    final elapsed = now() - start;
    if (run >= 0) {
      writeRates.add(iters / elapsed);
    }
  }

  // ROUND-TRIP: decode a variant buffer, then re-encode what came out. The
  // decode needs no sink discipline of its own — its output IS the encode's
  // input, so every decoded field is observed by construction, with no
  // per-language fold to audit (§2.7's read-side sink problem dissolved
  // rather than equalized).
  final roundTripRates = <double>[];
  for (var run = -1; run < numRuns; run++) {
    final start = now();
    for (var i = 0; i < iters; i++) {
      if (!read(decoded, variants[i & (numVariants - 1)], numBits)) {
        exit(1);
      }
      sink = (sink + write(decoded, view)) & 0xffffffff;
    }
    final elapsed = now() - start;
    if (run >= 0) {
      roundTripRates.add(iters / elapsed);
    }
  }

  report(row, 'write', iters, gated.bytesPerPacket, writeRates);
  report(row, 'round_trip', iters, gated.bytesPerPacket, roundTripRates);

  // READ is DERIVED, never measured: round-trip time minus write time. It
  // prints for continuity with the read rows the rest of the corpus still
  // reports and is NOT a CSV row — a derived number in the CSV would be
  // divided as if it had been measured.
  final w = (List<double>.from(writeRates)..sort())[writeRates.length ~/ 2];
  final rt =
      (List<double>.from(roundTripRates)..sort())[roundTripRates.length ~/ 2];
  final readTime = 1.0 / rt - 1.0 / w;
  if (readTime > 0) {
    stderr.write(
      '${row.padRight(18)} ${'read'.padRight(5)} '
      '${(1e-6 / readTime).toStringAsFixed(2).padLeft(10)} M msg/s   '
      '(DERIVED: round-trip minus write, informational — not a measured row)\n',
    );
  }
}

/* ------------------------------------------------------------------------- */

void main(List<String> arguments) {
  for (var i = 0; i < arguments.length; i++) {
    switch (arguments[i]) {
      case '--csv':
        csv = true;
      case '--quick':
        quick = true;
      case '--round':
        if (i + 1 >= arguments.length) {
          stderr.write('--round takes a round number\n');
          exit(1);
        }
        i++; // K only identifies the round to the driver
        numRuns = 1;
      default:
        stderr.write('usage: main.dart [--csv] [--round K] [--quick]\n');
        exit(1);
    }
  }
  if (quick && numRuns == 7) {
    numRuns = 3;
  }

  stderr.write(
    'schema bench (dart, generated codecs'
    '${quick ? ', --quick: iteration instrument, not certification' : ''})\n',
  );

  // every measured row's golden gate runs before any row is timed: a runner
  // that fails its goldens reports nothing at all (§1.5)
  final gatedMixed = gateDataDriven<BenchMixed>(
    'bench_mixed',
    'bench_mixed',
    BenchMixed.new,
    writeBenchMixed,
    readBenchMixed,
  );

  benchDataDriven(
    'bench_mixed',
    gatedMixed,
    mixedIters,
    writeBenchMixed,
    readBenchMixed,
  );

  flushCsv();
  stderr.write('OK (corpus_id ${corpusId()})\n');

  // the g_sink escape: the compiler cannot prove the env var absent, so the
  // accumulated sink is observable and no loop's work can be deleted
  if (Platform.environment['SERIALIZE_BENCH_SINK'] != null) {
    stderr.write('sink: $sink\n');
  }
}
