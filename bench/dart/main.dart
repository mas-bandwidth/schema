// schema bench — the Dart runner: a conforming run.sh leg per
// bench/README.md's runner contract, measuring the generated monomorphic
// Dart codecs over the four bench/corpus/Bench.schema shapes.
//
// CONTRACT (BENCH-STANDARD.md): fixed iteration counts identical to every
// other runner's rows for these benches; 1 discarded warmup run then 7
// measured runs per (bench, path) — or exactly one measured run under
// --round K, where the interleaved driver aggregates across rounds (§2.4);
// per-iteration LCG variation on the write path; 64 rotating variant read
// buffers; median/min/max/spread over the measured runs; CSV v2 rows on
// stdout under --csv, human table on stderr.
//
// WHAT THE ROWS MEASURE: the generated codec — schema's Dart backend emits
// self-contained monomorphic write/read functions with the bitpacker
// inlined and zero runtime dependencies, so the generated code IS the Dart
// serialize path. The rows carry family=gen: a ratio against another
// language's family=rt row (the serialize runtime API called by hand) is a
// subject difference, not a language difference, and the tools refuse it.
// Peak-style numbers from this runner's earlier serialize-family form
// (tight per-shape loops, best-of-five) are NOT comparable to these rows —
// different measurement contract, and the statistic alone moves the number.
//
// GOLDEN GATED (§1.5): before any timing, every measured shape's pinned
// instance is written and byte-compared against the C++-pinned
// testdata/wire golden, and all 64 LCG variant buffers are decoded back
// with every field verified. A runner that mismatches REFUSES to bench.
// corpus_id is FNV-1a-64 over the goldens this run actually loaded (§1.6).
//
// The timed form is the AOT executable (run.sh builds it) — the number
// that ships. JIT (`dart main.dart`) runs the same contract for iteration.
//
//   dart compile exe bench/dart/main.dart -o build/schema_bench_dart
//   cd bench/dart && ../../build/schema_bench_dart [--csv] [--round K] [--quick]
//
// --quick: bench_mixed only, 3 measured runs — run.sh's iteration
// instrument, never the certification instrument.
import 'dart:io';
import 'dart:typed_data';

import '../../generated/bench/dart/Bench.dart';

const int numVariants = 64;

// §1.2/§2.1: fixed per-benchmark iteration counts, identical across every
// language's rows for these benches, recorded in the iters column
const int packetIters = 32000000;
const int intsIters = 40000000;
const int bitsIters = 48000000;
const int mixedIters = 40000000;

bool csv = false;
bool quick = false;
int numRuns = 7;

final Stopwatch _clock = Stopwatch()..start();

double now() => _clock.elapsedMicroseconds * 1e-6;

// the g_sink of the C bench: computed values flow here, and the bench
// publishes it at exit under an env var the compiler cannot rule out, so no
// loop's work can be deleted
int sink = 0;

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

const int lcgMul = 0x5851F42D4C957F2D;
const int lcgAdd = 0x14057B7EF767814F;

int rng = 1;

void lcgSeed() {
  rng = 1;
}

void lcgStep() {
  rng = rng * lcgMul + lcgAdd;
}

// the low 32 bits of (rng >> s), for s in [0,63]
int shr(int s) => (rng >>> s) & 0xffffffff;

/* --------------------------------------------------------------------------
   pinned instances and vary functions — the §1.3 shapes, field mappings
   verbatim from the family benches
   -------------------------------------------------------------------------- */

void initBenchPacket(BenchPacket p) {
  p.a = -37;
  p.b = 12345;
  p.c = 987654;
  p.bits7 = 97;
  p.bits13 = 5000;
  p.bits23 = 1234567;
  p.flag = true;
  p.x = 1.5;
  p.y = -3.25;
  p.z = 100.125;
  p.big = 0x123456789abcdef0;
  for (var i = 0; i < 17; i++) {
    p.blob[i] = (i * 31) & 0xff;
  }
}

void varyBenchPacket(BenchPacket p) {
  lcgStep();
  p.a = (shr(8) & 63) - 32;
  p.b = shr(16) & 65535;
  p.c = (shr(24) & 0xfffff) - 500000;
  p.bits7 = rng & 127;
  p.bits13 = shr(3) & 8191;
  p.bits23 = shr(5) & 8388607;
  p.flag = (rng & 1) != 0;
  p.x = (rng & 0xffff).toDouble(); // exact in float32
  p.big = rng; // the full 64 bits, direct
  p.blob[0] = shr(32) & 0xff;
}

bool checkBenchPacket(BenchPacket e, BenchPacket d) {
  if (e.a != d.a ||
      e.b != d.b ||
      e.c != d.c ||
      e.bits7 != d.bits7 ||
      e.bits13 != d.bits13 ||
      e.bits23 != d.bits23 ||
      e.flag != d.flag ||
      e.x != d.x ||
      e.y != d.y ||
      e.z != d.z ||
      e.big != d.big) {
    return false;
  }
  for (var i = 0; i < 17; i++) {
    if (e.blob[i] != d.blob[i]) {
      return false;
    }
  }
  return true;
}

void initBenchInts(BenchInts p) {
  p.f0 = -37;
  p.f1 = 12345;
  p.f2 = 987654;
  p.f3 = 2;
  p.f4 = -15;
  p.f5 = 777;
  p.f6 = -2048;
  p.f7 = 200;
  p.f8 = -543210;
  p.f9 = 99;
}

void varyBenchInts(BenchInts f) {
  lcgStep();
  f.f0 = (shr(8) & 63) - 32;
  f.f1 = shr(16) & 65535;
  f.f2 = (shr(24) & 0xfffff) - 500000;
  f.f3 = shr(2) & 3;
  f.f4 = (shr(11) & 15) - 8;
  f.f5 = shr(22) & 511;
  f.f6 = (shr(33) & 2047) - 1024;
  f.f7 = shr(40) & 255;
  f.f8 = (shr(30) & 0xfffff) - 500000;
  f.f9 = shr(57) & 63;
}

bool checkBenchInts(BenchInts e, BenchInts d) =>
    e.f0 == d.f0 &&
    e.f1 == d.f1 &&
    e.f2 == d.f2 &&
    e.f3 == d.f3 &&
    e.f4 == d.f4 &&
    e.f5 == d.f5 &&
    e.f6 == d.f6 &&
    e.f7 == d.f7 &&
    e.f8 == d.f8 &&
    e.f9 == d.f9;

void initBenchBits(BenchBits p) {
  p.b7 = 97;
  p.b13 = 5000;
  p.b23 = 1234567;
  p.b3 = 5;
  p.b32 = 0xdeadbeef;
  p.b11 = 1024;
  p.b19 = 333333;
  p.b48 = 0xfedcba987654;
}

void varyBenchBits(BenchBits f) {
  lcgStep();
  f.b7 = rng & 127;
  f.b13 = shr(3) & 8191;
  f.b23 = shr(5) & 8388607;
  f.b3 = shr(29) & 7;
  f.b32 = shr(16);
  f.b11 = shr(37) & 2047;
  f.b19 = shr(44) & 524287;
  // the 48-bit field: low dword + 16-bit remainder, composed — the same
  // bits the family benches send as two lanes
  f.b48 = (rng & 0xffffffff) | ((shr(32) & 0xffff) << 32);
}

bool checkBenchBits(BenchBits e, BenchBits d) =>
    e.b7 == d.b7 &&
    e.b13 == d.b13 &&
    e.b23 == d.b23 &&
    e.b3 == d.b3 &&
    e.b32 == d.b32 &&
    e.b11 == d.b11 &&
    e.b19 == d.b19 &&
    e.b48 == d.b48;

void initBenchMixed(BenchMixed p) {
  p.sequence = 52428;
  p.ackBits = 0xa5a5a5a5;
  p.entityId = 2049;
  p.posX = -16384;
  p.posY = 16383;
  p.posZ = -1;
  p.yaw = 511;
  p.moving = true;
  p.firing = false;
  p.timestamp = 0x123456789abc;
  p.weapon = 15;
}

void varyBenchMixed(BenchMixed f) {
  lcgStep();
  f.sequence = shr(8) & 65535;
  f.ackBits = shr(16);
  f.entityId = rng & 4095;
  f.posX = (shr(20) & 32767) - 16384;
  f.posY = (shr(25) & 32767) - 16384;
  f.posZ = (shr(30) & 32767) - 16384;
  f.yaw = shr(3) & 511;
  f.moving = (rng & 1) != 0;
  f.firing = (rng & 2) != 0;
  f.timestamp = (rng & 0xffffffff) | ((shr(32) & 0xffff) << 32);
  f.weapon = shr(60) & 15;
}

bool checkBenchMixed(BenchMixed e, BenchMixed d) =>
    e.sequence == d.sequence &&
    e.ackBits == d.ackBits &&
    e.entityId == d.entityId &&
    e.posX == d.posX &&
    e.posY == d.posY &&
    e.posZ == d.posZ &&
    e.yaw == d.yaw &&
    e.moving == d.moving &&
    e.firing == d.firing &&
    e.timestamp == d.timestamp &&
    e.weapon == d.weapon;

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

final class _GatedShape<P> {
  final P packet;
  final P decoded;
  final List<ByteData> variants;
  final int bytesPerPacket;
  _GatedShape(this.packet, this.decoded, this.variants, this.bytesPerPacket);
}

_GatedShape<P> gateShape<P>(
  String row,
  String goldenName,
  P packet,
  P decoded,
  void Function(P) init,
  void Function(P) vary,
  int Function(P, ByteData) write,
  bool Function(P, ByteData, int) read,
  bool Function(P, P) checkFields,
) {
  // the pinned corpus instance against the C++-pinned golden
  final buffer = Uint8List(256);
  final view = ByteData.sublistView(buffer);
  init(packet);
  final n = write(packet, view);
  final goldenFile = File('../../testdata/wire/$goldenName.bin');
  if (!goldenFile.existsSync()) {
    stderr.write(
      'missing wire golden testdata/wire/$goldenName.bin — run from bench/dart\n',
    );
    exit(1);
  }
  final goldenBytes = goldenFile.readAsBytesSync();
  goldensLoaded['$goldenName.bin'] = goldenBytes;
  if (!bytesEqual(Uint8List.sublistView(buffer, 0, n), goldenBytes)) {
    gateFail(row, 'pinned instance vs testdata/wire/$goldenName.bin');
  }

  // 64 LCG variants, written with the same sequence the write loop uses,
  // each decoded back and field-verified
  init(packet);
  lcgSeed();
  final variants = <ByteData>[];
  var bytesPerPacket = -1;
  for (var v = 0; v < numVariants; v++) {
    vary(packet);
    final vbuf = Uint8List(256);
    final nv = write(packet, ByteData.sublistView(vbuf));
    if (bytesPerPacket == -1) {
      bytesPerPacket = nv;
    } else if (nv != bytesPerPacket) {
      gateFail(row, 'variant $v size $nv != $bytesPerPacket');
    }
    variants.add(ByteData.sublistView(vbuf, 0, nv));
  }
  init(packet);
  lcgSeed();
  for (var v = 0; v < numVariants; v++) {
    vary(packet);
    if (!read(decoded, variants[v], bytesPerPacket * 8)) {
      gateFail(row, 'variant $v read verdict');
    }
    if (!checkFields(packet, decoded)) {
      gateFail(row, 'variant $v field mismatch');
    }
  }
  return _GatedShape(packet, decoded, variants, bytesPerPacket);
}

/* --------------------------------------------------------------------------
   the timed rows: per (bench, path), 1 discarded warmup run then numRuns
   measured runs — the write loop varies every packet through the LCG, the
   read loop rotates the 64 gated variant buffers, and every loop's work
   flows into the sink.
   -------------------------------------------------------------------------- */

void benchShape<P>(
  String row,
  _GatedShape<P> gated,
  int iters,
  void Function(P) vary,
  int Function(P, ByteData) write,
  bool Function(P, ByteData, int) read,
  int Function(P) sinkOf,
) {
  final packet = gated.packet;
  final decoded = gated.decoded;
  final variants = gated.variants;
  final numBits = gated.bytesPerPacket * 8;
  final buffer = Uint8List(256);
  final view = ByteData.sublistView(buffer);

  final writeRates = <double>[];
  for (var run = -1; run < numRuns; run++) {
    final start = now();
    for (var i = 0; i < iters; i++) {
      vary(packet);
      sink = (sink + write(packet, view)) & 0xffffffff;
    }
    final elapsed = now() - start;
    if (run >= 0) {
      writeRates.add(iters / elapsed);
    }
  }

  final readRates = <double>[];
  for (var run = -1; run < numRuns; run++) {
    final start = now();
    for (var i = 0; i < iters; i++) {
      if (!read(decoded, variants[i & (numVariants - 1)], numBits)) {
        exit(1);
      }
      sink = (sink + sinkOf(decoded)) & 0xffffffff;
    }
    final elapsed = now() - start;
    if (run >= 0) {
      readRates.add(iters / elapsed);
    }
  }

  report(row, 'write', iters, gated.bytesPerPacket, writeRates);
  report(row, 'read', iters, gated.bytesPerPacket, readRates);
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
  final gatedMixed = gateShape(
    'bench_mixed',
    'bench_mixed',
    BenchMixed(),
    BenchMixed(),
    initBenchMixed,
    varyBenchMixed,
    writeBenchMixed,
    readBenchMixed,
    checkBenchMixed,
  );

  if (quick) {
    benchShape(
      'bench_mixed',
      gatedMixed,
      mixedIters,
      varyBenchMixed,
      writeBenchMixed,
      readBenchMixed,
      (BenchMixed d) => d.sequence,
    );
  } else {
    final gatedPacket = gateShape(
      'bench_packet',
      'bench_packet',
      BenchPacket(),
      BenchPacket(),
      initBenchPacket,
      varyBenchPacket,
      writeBenchPacket,
      readBenchPacket,
      checkBenchPacket,
    );
    final gatedInts = gateShape(
      'bench_ints',
      'bench_ints',
      BenchInts(),
      BenchInts(),
      initBenchInts,
      varyBenchInts,
      writeBenchInts,
      readBenchInts,
      checkBenchInts,
    );
    final gatedBits = gateShape(
      'bench_bits',
      'bench_bits',
      BenchBits(),
      BenchBits(),
      initBenchBits,
      varyBenchBits,
      writeBenchBits,
      readBenchBits,
      checkBenchBits,
    );

    benchShape(
      'bench_packet',
      gatedPacket,
      packetIters,
      varyBenchPacket,
      writeBenchPacket,
      readBenchPacket,
      (BenchPacket d) => d.b,
    );
    benchShape(
      'bench_ints',
      gatedInts,
      intsIters,
      varyBenchInts,
      writeBenchInts,
      readBenchInts,
      (BenchInts d) => d.f0,
    );
    benchShape(
      'bench_bits',
      gatedBits,
      bitsIters,
      varyBenchBits,
      writeBenchBits,
      readBenchBits,
      (BenchBits d) => d.b7,
    );
    benchShape(
      'bench_mixed',
      gatedMixed,
      mixedIters,
      varyBenchMixed,
      writeBenchMixed,
      readBenchMixed,
      (BenchMixed d) => d.sequence,
    );
  }

  flushCsv();
  stderr.write('OK (corpus_id ${corpusId()})\n');

  // the g_sink escape: the compiler cannot prove the env var absent, so the
  // accumulated sink is observable and no loop's work can be deleted
  if (Platform.environment['SERIALIZE_BENCH_SINK'] != null) {
    stderr.write('sink: $sink\n');
  }
}
