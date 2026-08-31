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
import '../../generated/bench/dart/Int128.dart';

const int numVariants = 64;

// §1.2/§2.1: fixed per-benchmark iteration counts, identical across every
// language's rows for these benches, recorded in the iters column
const int packetIters = 32000000;
const int intsIters = 40000000;
const int bitsIters = 48000000;
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

int sinkOfBenchPacket(BenchPacket d) {
  var s = d.a +
      d.b +
      d.c +
      d.bits7 +
      d.bits13 +
      d.bits23 +
      (d.flag ? 1 : 0) +
      d.x.toInt() +
      d.y.toInt() +
      d.z.toInt() +
      d.big;
  for (var i = 0; i < 17; i++) {
    s += d.blob[i];
  }
  return s;
}

int sinkOfBenchInts(BenchInts d) =>
    d.f0 + d.f1 + d.f2 + d.f3 + d.f4 + d.f5 + d.f6 + d.f7 + d.f8 + d.f9;

int sinkOfBenchBits(BenchBits d) =>
    d.b7 + d.b13 + d.b23 + d.b3 + d.b32 + d.b11 + d.b19 + d.b48;

// §2.7 full-struct observation over the canonical shape: every decoded field
// folds in — array elements one by one over the decoded extent, booleans as
// 0/1, doubles truncated, the 128-bit values as both halves, the string and
// byte block byte-summed over their used lengths.
int sinkOfBenchMixed(BenchMixed d) {
  var s = d.sequence +
      d.ackSequence +
      d.ackBits +
      d.sessionId +
      d.clientId +
      d.nonce +
      d.worldTime +
      d.frameTick +
      d.serverTime +
      d.entitiesCount +
      d.statsCount +
      d.gameEvent.type +
      d.playerNameLength +
      d.payloadLength +
      d.aimX.toInt() +
      d.aimY.toInt() +
      d.aimZ.toInt() +
      d.recoil.toInt() +
      d.drift.toInt() +
      d.wideKey.hi +
      d.wideKey.lo +
      d.flux.hi +
      d.flux.lo +
      d.ping +
      d.crcHint +
      (d.hasExtra ? 1 : 0) +
      d.extra +
      d.idleTicks;
  for (var i = 0; i < d.entitiesCount; i++) {
    final e = d.entities[i];
    s += e.entityId +
        e.posX +
        e.posY +
        e.posZ +
        e.yaw +
        e.pitch +
        e.velX +
        e.velY +
        e.velZ +
        e.health +
        e.weapon +
        e.damage +
        (e.moving ? 1 : 0) +
        (e.firing ? 1 : 0);
  }
  for (var i = 0; i < d.statsCount; i++) {
    s += d.stats[i].statId + d.stats[i].delta;
  }
  final h = d.gameEvent.hit;
  s += h.targetId + h.damage + h.hitKind + (h.crit ? 1 : 0);
  for (var i = 0; i < 4; i++) {
    s += d.loadout[i];
  }
  for (var i = 0; i < d.playerNameLength; i++) {
    s += d.playerName[i];
  }
  for (var i = 0; i < d.payloadLength; i++) {
    s += d.payload[i];
  }
  return s;
}

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

// BenchMixed — THE canonical benchmark shape (issue #184). The pin is
// test/bench/main.cpp's, transcribed exactly; STRUCTURE fields (the two array
// counts, the two used lengths, the union tag, the `if` gate) are set here and
// never touched by varyBenchMixed, so bytes/op is constant (§2.7).
final playerNamePin = Uint8List.fromList('Rowan_01'.codeUnits);
final payloadPin = Uint8List.fromList(
  [0xde, 0xad, 0xbe, 0xef, 0x01, 0x02, 0x03, 0x04],
);

void initBenchMixed(BenchMixed p) {
  p.sequence = 52428;
  p.ackSequence = 12345;
  p.ackBits = 0xa5a5a5a5;
  p.sessionId = 0x123456789abcdef0;
  p.clientId = 0xdeadbeef;
  p.nonce = 0xfedcba9876543210;
  p.worldTime = -987654321000;
  p.frameTick = 0x123456789abc;
  p.serverTime = 12345678;
  p.entitiesCount = 8;
  for (var i = 0; i < 8; i++) {
    final e = p.entities[i];
    e.entityId = 2049 + i * 17;
    e.posX = -16383 + i * 4096;
    e.posY = 16383 - i * 4096;
    e.posZ = -1 + i * 2048;
    e.yaw = 511 - i * 64;
    e.pitch = i * 73;
    e.velX = -2048 + i * 512;
    e.velY = 2047 - i * 512;
    e.velZ = -1024 + i * 256;
    e.health = 1000 - i * 100;
    e.weapon = 1 + i;
    e.damage = 0x5a + i;
    e.moving = i % 2 == 0;
    e.firing = i % 3 == 0;
  }
  p.statsCount = 80;
  for (var i = 0; i < 80; i++) {
    p.stats[i].statId = (i * 3) % 256;
    p.stats[i].delta = -512 + (i * 13) % 1024;
  }
  p.gameEvent.type = MixedEventType.hit;
  p.gameEvent.hit.targetId = 4095;
  p.gameEvent.hit.damage = 4095;
  p.gameEvent.hit.hitKind = 7;
  p.gameEvent.hit.crit = true;
  p.loadout.setAll(0, [0x11, 0x22, 0x33, 0x44]);
  p.playerName.setAll(0, playerNamePin);
  p.playerNameLength = 8;
  p.payload.setAll(0, payloadPin);
  p.payloadLength = 8;
  p.aimX = 0.5;
  p.aimY = -0.25;
  p.aimZ = 0.75;
  p.recoil = 1.5;
  p.drift = -3.25;
  p.wideKey = const UInt128(0x0123456789abcdef, 0xfedcba9876543210);
  p.flux = const Int128(0x800000000, 7); // 2^99 + 7
  p.ping = 12345;
  p.crcHint = 0xabcdef;
  p.hasExtra = true;
  p.extra = 200;
}

// The LCG field mapping, identical in every runner. VALUE fields only:
// every count, used length, union tag and branch gate is STRUCTURE (§2.7).
// All 8 entities vary; the 80 stats vary delta (statId stays pinned).
void varyBenchMixed(BenchMixed f) {
  lcgStep();
  f.sequence = shr(8) & 65535;
  f.ackSequence = shr(24) & 65535;
  f.ackBits = shr(16);
  f.sessionId = rng;
  f.clientId = shr(32);
  f.nonce = rng ^ 0xa5a5a5a5a5a5a5a5;
  f.worldTime = ((rng >>> 12) & 0xfffffffff) - 34359738368;
  f.frameTick = rng & 0xffffffffffff;
  f.serverTime = shr(20) & 0x7fffff;
  for (var i = 0; i < 8; i++) {
    final e = f.entities[i];
    e.entityId = (rng >>> i) & 4095;
    e.posX = ((rng >>> (i + 4)) & 16383) - 8192;
    e.posY = ((rng >>> (i + 12)) & 16383) - 8192;
    e.health = (rng >>> (i + 20)) & 511;
    e.weapon = (rng >>> (i + 40)) & 15;
    e.damage = (rng >>> (i + 28)) & 255;
    e.moving = ((rng >>> i) & 1) != 0;
  }
  for (var i = 0; i < 80; i++) {
    f.stats[i].delta = ((rng >>> (i & 31)) & 1023) - 512;
  }
  f.gameEvent.hit.targetId = shr(6) & 4095;
  f.gameEvent.hit.damage = shr(18) & 4095;
  f.gameEvent.hit.hitKind = shr(30) & 7;
  f.gameEvent.hit.crit = (rng & 4) != 0;
  f.loadout[0] = shr(56) & 255;
  f.playerName[7] = 65 + (shr(50) & 15);
  f.payload[0] = shr(48) & 255;
  f.aimX = (shr(2) & 255) * (1 / 256) - 0.5;
  f.aimY = (shr(10) & 255) * (1 / 256) - 0.5;
  f.aimZ = (shr(18) & 255) * (1 / 256) - 0.5;
  f.recoil = (rng & 0xffff).toDouble();
  f.drift = ((rng >>> 8) & 0xffffff) * 0.5;
  f.wideKey = UInt128(rng >>> 1, rng);
  f.flux = Int128(0, rng >>> 16);
  f.ping = shr(40) & 0x7fff;
  f.crcHint = shr(24) & 0xffffff;
  f.extra = shr(52) & 255;
}

bool checkBenchMixed(BenchMixed e, BenchMixed d) {
  if (e.sequence != d.sequence ||
      e.ackSequence != d.ackSequence ||
      e.ackBits != d.ackBits ||
      e.sessionId != d.sessionId ||
      e.clientId != d.clientId ||
      e.nonce != d.nonce ||
      e.worldTime != d.worldTime ||
      e.frameTick != d.frameTick ||
      e.serverTime != d.serverTime ||
      e.entitiesCount != d.entitiesCount ||
      e.statsCount != d.statsCount ||
      e.gameEvent.type != d.gameEvent.type ||
      e.playerNameLength != d.playerNameLength ||
      e.payloadLength != d.payloadLength ||
      e.recoil != d.recoil ||
      e.drift != d.drift ||
      e.wideKey.hi != d.wideKey.hi ||
      e.wideKey.lo != d.wideKey.lo ||
      e.flux.hi != d.flux.hi ||
      e.flux.lo != d.flux.lo ||
      e.ping != d.ping ||
      e.crcHint != d.crcHint ||
      e.hasExtra != d.hasExtra ||
      e.extra != d.extra) {
    return false;
  }
  for (var i = 0; i < e.entitiesCount; i++) {
    final a = e.entities[i];
    final b = d.entities[i];
    if (a.entityId != b.entityId ||
        a.posX != b.posX ||
        a.posY != b.posY ||
        a.posZ != b.posZ ||
        a.yaw != b.yaw ||
        a.pitch != b.pitch ||
        a.velX != b.velX ||
        a.velY != b.velY ||
        a.velZ != b.velZ ||
        a.health != b.health ||
        a.weapon != b.weapon ||
        a.damage != b.damage ||
        a.moving != b.moving ||
        a.firing != b.firing) {
      return false;
    }
  }
  for (var i = 0; i < e.statsCount; i++) {
    if (e.stats[i].statId != d.stats[i].statId ||
        e.stats[i].delta != d.stats[i].delta) {
      return false;
    }
  }
  if (e.gameEvent.hit.targetId != d.gameEvent.hit.targetId ||
      e.gameEvent.hit.damage != d.gameEvent.hit.damage ||
      e.gameEvent.hit.hitKind != d.gameEvent.hit.hitKind ||
      e.gameEvent.hit.crit != d.gameEvent.hit.crit) {
    return false;
  }
  for (var i = 0; i < 4; i++) {
    if (e.loadout[i] != d.loadout[i]) {
      return false;
    }
  }
  for (var i = 0; i < e.playerNameLength; i++) {
    if (e.playerName[i] != d.playerName[i]) {
      return false;
    }
  }
  for (var i = 0; i < e.payloadLength; i++) {
    if (e.payload[i] != d.payload[i]) {
      return false;
    }
  }
  // aimX/Y/Z are COMPRESSED floats: the wire carries a quantized step, so the
  // decoded value is not the value that was written and no equality applies.
  return true;
}

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
  final buffer = Uint8List(512);
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
    final vbuf = Uint8List(512);
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
  final buffer = Uint8List(512);
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
      sinkOfBenchMixed, // full-struct observation (#175)
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
      sinkOfBenchPacket, // full-struct observation (#175)
    );
    benchShape(
      'bench_ints',
      gatedInts,
      intsIters,
      varyBenchInts,
      writeBenchInts,
      readBenchInts,
      sinkOfBenchInts, // full-struct observation (#175)
    );
    benchShape(
      'bench_bits',
      gatedBits,
      bitsIters,
      varyBenchBits,
      writeBenchBits,
      readBenchBits,
      sinkOfBenchBits, // full-struct observation (#175)
    );
    benchShape(
      'bench_mixed',
      gatedMixed,
      mixedIters,
      varyBenchMixed,
      writeBenchMixed,
      readBenchMixed,
      sinkOfBenchMixed, // full-struct observation (#175)
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
