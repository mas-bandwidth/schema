// schema bench — the Dart runner (minimal form): the generated monomorphic
// codecs over the four family `rt` shapes (BENCH-STANDARD.md §1.3), measured
// with serialize.dart's own bench methodology so the rows read side by side
// with that library's — same LCG, same vary-function field mappings, same
// iteration counts, same 64 read variants, same best-of-five discipline,
// same units and reporting format (serialize.dart bench/bench.dart is the
// reference for all of it). This is the issue #155 prediction instrument:
// generated constant-width monomorphic code vs the library's stream rows.
//
// GOLDEN GATED (§1.5): before any row is timed, every pinned corpus
// instance is written and byte-compared against the C++-pinned
// testdata/wire golden, and all 64 LCG variant buffers of every shape are
// decoded back with every field verified. A runner that mismatches REFUSES
// to bench.
//
//   dart compile exe bench/dart/main.dart -o build/schema_bench_dart
//   cd bench/dart && ../../build/schema_bench_dart          # AOT — the number that ships
//   cd bench/dart && dart main.dart                          # JIT — the iteration number
//
// Iteration counts are overridable, the library bench's own env names:
//   BENCH_STREAM_PACKETS=100000 dart main.dart
//
// Full run.sh/CSV-preamble integration per bench/README.md is a named
// follow-on; this runner carries the golden gate, the methodology and the
// rows.
import 'dart:io';
import 'dart:typed_data';

import '../../generated/bench/dart/Bench.dart';

const int numTrials = 5;
const int numVariants = 64;

int envInt(String name, int fallback) {
  final raw = Platform.environment[name];
  if (raw == null) {
    return fallback;
  }
  final value = int.tryParse(raw);
  if (value == null || value < 1) {
    stderr.write('$name must be a positive integer\n');
    exit(1);
  }
  return value;
}

final int streamNumPackets = envInt('BENCH_STREAM_PACKETS', 1000000);

bool csv = false;

final Stopwatch _clock = Stopwatch()..start();

double now() => _clock.elapsedMicroseconds * 1e-6;

// the g_sink of the C bench: computed values flow here, and the bench
// publishes it at exit under an env var the compiler cannot rule out, so no
// loop's work can be deleted
int sink = 0;

final class _Result {
  final String row;
  final String op;
  final String units;
  final double value;
  _Result(this.row, this.op, this.units, this.value);
}

final List<_Result> results = [];

void report(String row, String op, String units, double value) {
  results.add(_Result(row, op, units, value));
}

void printRow(String line) {
  if (!csv) {
    stdout.write(line);
  }
}

Never gateFail(String row, String what) {
  stderr.write('GOLDEN GATE FAILED: $row $what\nreporting nothing.\n');
  exit(1);
}

/* --------------------------------------------------------------------------
   the C bench's uint64 LCG, direct: Dart's int is 64 bits and wraps two's
   complement, which IS arithmetic mod 2^64 (serialize.dart bench, verbatim)
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
  final goldenBytes = File('../../testdata/wire/$goldenName.bin')
      .readAsBytesSync();
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
   the timed rows: write and read (and measure, where the family prints it),
   best of five trials — serialize.dart bench/bench.dart's loop, with the
   generated monomorphic functions in place of the stream calls
   -------------------------------------------------------------------------- */

void benchShape<P>(
  String row,
  String label,
  _GatedShape<P> gated,
  void Function(P) init,
  void Function(P) vary,
  int Function(P, ByteData) write,
  bool Function(P, ByteData, int) read,
  int Function(P) sinkOf, {
  int Function(P)? measure,
  bool mbRow = false,
}) {
  final packet = gated.packet;
  final decoded = gated.decoded;
  final variants = gated.variants;
  final numBits = gated.bytesPerPacket * 8;
  final buffer = Uint8List(256);
  final view = ByteData.sublistView(buffer);

  var bestWrite = double.infinity;
  var bestRead = double.infinity;
  var bestMeasure = double.infinity;

  for (var trial = 0; trial < numTrials; trial++) {
    init(packet);
    lcgSeed();

    var start = now();
    for (var i = 0; i < streamNumPackets; i++) {
      vary(packet);
      sink = (sink + write(packet, view)) & 0xffffffff;
    }
    var elapsed = now() - start;
    if (elapsed < bestWrite) {
      bestWrite = elapsed;
    }

    start = now();
    for (var i = 0; i < streamNumPackets; i++) {
      if (!read(decoded, variants[i & (numVariants - 1)], numBits)) {
        exit(1);
      }
      sink = (sink + sinkOf(decoded)) & 0xffffffff;
    }
    elapsed = now() - start;
    if (elapsed < bestRead) {
      bestRead = elapsed;
    }

    if (measure != null) {
      start = now();
      for (var i = 0; i < streamNumPackets; i++) {
        vary(packet);
        sink = (sink + measure(packet)) & 0xffffffff;
      }
      elapsed = now() - start;
      if (elapsed < bestMeasure) {
        bestMeasure = elapsed;
      }
    }
  }

  final totalMB = (gated.bytesPerPacket * streamNumPackets) / (1024 * 1024);
  final packets = streamNumPackets / 1000000;

  if (mbRow) {
    report(row, 'write', 'MB/s', totalMB / bestWrite);
    report(row, 'write', 'Mpackets/s', packets / bestWrite);
    report(row, 'read', 'MB/s', totalMB / bestRead);
    report(row, 'read', 'Mpackets/s', packets / bestRead);
    printRow(
      '$label write: ${(totalMB / bestWrite).toStringAsFixed(1).padLeft(8)} MB/s  (${(packets / bestWrite).toStringAsFixed(1)} M packets/s)\n',
    );
    printRow(
      '$label read:  ${(totalMB / bestRead).toStringAsFixed(1).padLeft(8)} MB/s  (${(packets / bestRead).toStringAsFixed(1)} M packets/s)\n',
    );
  } else {
    report(row, 'write', 'Mpackets/s', packets / bestWrite);
    report(row, 'read', 'Mpackets/s', packets / bestRead);
    printRow(
      '$label write: ${(packets / bestWrite).toStringAsFixed(1).padLeft(6)} M packets/s   read: ${(packets / bestRead).toStringAsFixed(1).padLeft(6)} M packets/s\n',
    );
  }
  if (measure != null) {
    report(row, 'measure', 'Mpackets/s', packets / bestMeasure);
    printRow(
      '$label measure: ${(packets / bestMeasure).toStringAsFixed(1).padLeft(6)} M packets/s (generation-time folded)\n',
    );
  }
}

/* ------------------------------------------------------------------------- */

void main(List<String> arguments) {
  csv = arguments.contains('--csv');

  // every row's golden gate runs before any row is timed: a runner that
  // fails its goldens reports nothing at all (§1.5)
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

  printRow('\n[schema bench — generated Dart]\n\n');

  // the stream-comparable row: the same 12-op packet serialize.dart's
  // stream rows measure, through the generated monomorphic codec
  benchShape(
    'bench_packet',
    'packet (generated):',
    gatedPacket,
    initBenchPacket,
    varyBenchPacket,
    writeBenchPacket,
    readBenchPacket,
    (BenchPacket d) => d.b,
    measure: measureBenchPacket,
    mbRow: true,
  );

  printRow('\n');

  benchShape(
    'bench_ints',
    'int packet   (generated):',
    gatedInts,
    initBenchInts,
    varyBenchInts,
    writeBenchInts,
    readBenchInts,
    (BenchInts d) => d.f0,
  );
  benchShape(
    'bench_bits',
    'bits packet  (generated):',
    gatedBits,
    initBenchBits,
    varyBenchBits,
    writeBenchBits,
    readBenchBits,
    (BenchBits d) => d.b7,
  );
  benchShape(
    'bench_mixed',
    'mixed packet (generated):',
    gatedMixed,
    initBenchMixed,
    varyBenchMixed,
    writeBenchMixed,
    readBenchMixed,
    (BenchMixed d) => d.sequence,
  );

  printRow('\n');

  if (csv) {
    final buffer = StringBuffer('row,op,units,value\n');
    for (final r in results) {
      buffer.write(
        '${r.row},${r.op},${r.units},${r.value.toStringAsFixed(4)}\n',
      );
    }
    stdout.write(buffer.toString());
  }

  // the g_sink escape: the compiler cannot prove the env var absent, so the
  // accumulated sink is observable and no loop's work can be deleted
  if (Platform.environment['SERIALIZE_BENCH_SINK'] != null) {
    stderr.write('sink: $sink\n');
  }
}
