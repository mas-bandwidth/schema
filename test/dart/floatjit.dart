// The JIT float-boxing instrument (issue 404, PORTING M3): does a double
// crossing a float conversion call box under the JIT on the pinned SDK?
//
// A steady loop writes and reads float32 fields (Input: five float32s) and
// float64 fields (Vec3: three float64s) through the generated packet codec
// in generated/dart/Types.dart, in one of three shapes:
//
//   generated   the codec exactly as generated, called from one loop
//   inlined     the same two codecs with the conversion hand-inlined at each
//               assignment (the control: what the codec costs with no call
//               for a double to cross)
//   twocallers  the generated codec called from two different callers in
//               alternation (the inliner's decision depends on the loop
//               around the codec, so one caller is not the whole story)
//   planted-exit  THE BLADE, not a measurement: the generated warm-up, then
//               exit 3 with no completion line, standing in for a measured
//               run that dies part way; make tables-dart-float-jit must
//               refuse it by name before it reads any count
//
// Usage: dart --verbose_gc floatjit.dart <shape> <steady-iterations>
//
// The program runs a fixed warm-up, then <steady-iterations> passes. It
// counts nothing itself: the VM prints one line per scavenge under
// --verbose_gc, and make tables-dart-float-jit runs each shape twice (N and
// 2N steady passes), counting only events between the steady markers and
// requiring zero in each window. Start-up and warm-up are excluded. Prints the
// completion line `floatjit shape=<s> steady=<n> check=<c>` on stdout as its
// last act; the make target reads no count from a run without it. check
// depends on every decoded value so nothing is dead code.
import 'dart:io';
import 'dart:typed_data';

import '../../generated/dart/Types.dart';

// The warm-up is many short calls, not one long one: a single long call gets
// its loop optimized by on-stack replacement only, and OSR code is entered
// only through the back edge, so the steady call would re-enter the pass in
// unoptimized code (which boxes every double) until the pass is compiled
// again. warmupCalls calls get the pass compiled for a normal entry first,
// and the steady window sees only the optimized code.
const int warmupCalls = 2000;
const int warmupChunk = 100;

// The inlined control's scratch: the same overlaid views the generated
// helpers use, so the only difference between the shapes is the call.
final Float32List _f32 = Float32List(2);
final Uint32List _u32 = _f32.buffer.asUint32List();
final Float64List _f64 = _f32.buffer.asFloat64List();
final Uint64List _u64 = _f32.buffer.asUint64List();

final ByteData inputBuf = ByteData(inputMaxBytes);
final ByteData vecBuf = ByteData(vec3MaxBytes);
final Input inA = Input();
final Input inB = Input();
final Vec3 vA = Vec3();
final Vec3 vB = Vec3();

Future<void> main(List<String> args) async {
  if (args.length != 2) {
    stderr.writeln(
      'usage: floatjit.dart generated|inlined|twocallers|planted-exit '
      '<steady>',
    );
    exit(2);
  }
  final shape = args[0];
  final steady = int.parse(args[1]);
  inA
    ..stickX = 0.25
    ..stickY = -0.5
    ..throttle = 1.0
    ..yaw = 3.140625
    ..pitch = -2.0
    ..fire = true;
  vA
    ..x = 1.5
    ..y = -2.25
    ..z = 1e300;
  var check = 0;
  final pass = switch (shape) {
    'generated' => passGenerated,
    'inlined' => passInlined,
    'twocallers' => passTwoCallers,
    'planted-exit' => passPlantedExit,
    _ => null,
  };
  if (pass == null) {
    stderr.writeln('floatjit: unknown shape $shape');
    exit(2);
  }
  // Stdout is blocking on the pinned SDK. Prepare both byte buffers and its
  // synchronous stream consumer before warm-up: encoding the end marker or
  // closing/reopening the consumer inside the window can itself scavenge.
  final windowBegins = Uint8List.fromList(
    'floatjit: steady window begins\n'.codeUnits,
  );
  final windowEnds = Uint8List.fromList(
    'floatjit: steady window ends\n'.codeUnits,
  );
  stderr.add(Uint8List(0));
  for (var i = 0; i < warmupCalls; i++) {
    check += pass(warmupChunk, 0);
  }
  // Blocking writes put the preencoded brackets on the same fd as the VM's
  // --verbose_gc lines, in order. No await, encoding or stream teardown may
  // enter this interval: the instrument must not charge its own allocations
  // to the codec. Flush only after the end marker is already written.
  stderr.add(windowBegins);
  check += pass(steady, 1);
  stderr.add(windowEnds);
  await stderr.flush();
  stdout.writeln('floatjit shape=$shape steady=$steady check=$check');
}

int passGenerated(int n, int salt) {
  var check = salt;
  for (var i = 0; i < n; i++) {
    check += writeInput(inA, inputBuf);
    if (!readInput(inB, inputBuf, inputMaxBits)) check++;
    check += writeVec3(vA, vecBuf);
    if (!readVec3(vB, vecBuf, vec3MaxBits)) check++;
    if (inB.yaw != inA.yaw || vB.z != vA.z) check++;
  }
  return check;
}

// The blade: warm up as the generated shape does, then die before the
// completion line, as a measured run killed part way would.
int passPlantedExit(int n, int salt) {
  if (salt == 0) return passGenerated(n, salt);
  exit(3);
}

int passTwoCallers(int n, int salt) {
  var check = salt;
  for (var i = 0; i < n; i++) {
    check += (i & 1) == 0 ? callerOne() : callerTwo();
  }
  return check;
}

int callerOne() {
  var check = writeInput(inA, inputBuf);
  if (!readInput(inB, inputBuf, inputMaxBits)) check++;
  check += writeVec3(vA, vecBuf);
  if (!readVec3(vB, vecBuf, vec3MaxBits)) check++;
  if (inB.pitch != inA.pitch) check++;
  return check;
}

int callerTwo() {
  var check = writeVec3(vB, vecBuf);
  if (!readVec3(vA, vecBuf, vec3MaxBits)) check++;
  check += writeInput(inB, inputBuf);
  if (!readInput(inA, inputBuf, inputMaxBits)) check++;
  if (vA.x != vB.x) check++;
  return check;
}

int passInlined(int n, int salt) {
  var check = salt;
  for (var i = 0; i < n; i++) {
    check += writeInputInlined(inA, inputBuf);
    if (!readInputInlined(inB, inputBuf)) check++;
    check += writeVec3Inlined(vA, vecBuf);
    if (!readVec3Inlined(vB, vecBuf)) check++;
    if (inB.yaw != inA.yaw || vB.z != vA.z) check++;
  }
  return check;
}

// The hand-inlined control. Non-NaN float32 only (the loop's values are
// finite); the NaN branch the generated helper carries is the emitter's
// business, not this instrument's.
int writeInputInlined(Input value, ByteData view) {
  _f32[0] = value.stickX;
  final a = _u32[0];
  _f32[0] = value.stickY;
  final b = _u32[0];
  view.setUint64(0, a | (b << 32), Endian.little);
  _f32[0] = value.throttle;
  final c = _u32[0];
  _f32[0] = value.yaw;
  final d = _u32[0];
  view.setUint64(8, c | (d << 32), Endian.little);
  _f32[0] = value.pitch;
  final e = _u32[0];
  final bits =
      e |
      ((value.fire ? 1 : 0) << 32) |
      ((value.altFire ? 1 : 0) << 33) |
      ((value.boost ? 1 : 0) << 34) |
      ((value.brake ? 1 : 0) << 35) |
      ((value.aim ? 1 : 0) << 36) |
      ((value.lockOn ? 1 : 0) << 37) |
      ((value.zoom ? 1 : 0) << 38) |
      ((value.ping ? 1 : 0) << 39);
  view.setUint64(16, bits, Endian.little);
  return 21;
}

bool readInputInlined(Input value, ByteData view) {
  final w0 = view.getUint64(0, Endian.little);
  final w1 = view.getUint64(8, Endian.little);
  final w2 = view.getUint64(16, Endian.little);
  _u32[0] = w0 & 0xffffffff;
  value.stickX = _f32[0];
  _u32[0] = w0 >>> 32;
  value.stickY = _f32[0];
  _u32[0] = w1 & 0xffffffff;
  value.throttle = _f32[0];
  _u32[0] = w1 >>> 32;
  value.yaw = _f32[0];
  _u32[0] = w2 & 0xffffffff;
  value.pitch = _f32[0];
  value.fire = (w2 >>> 32) & 1 != 0;
  value.altFire = (w2 >>> 33) & 1 != 0;
  value.boost = (w2 >>> 34) & 1 != 0;
  value.brake = (w2 >>> 35) & 1 != 0;
  value.aim = (w2 >>> 36) & 1 != 0;
  value.lockOn = (w2 >>> 37) & 1 != 0;
  value.zoom = (w2 >>> 38) & 1 != 0;
  value.ping = (w2 >>> 39) & 1 != 0;
  return true;
}

int writeVec3Inlined(Vec3 value, ByteData view) {
  _f64[0] = value.x;
  view.setUint64(0, _u64[0], Endian.little);
  _f64[0] = value.y;
  view.setUint64(8, _u64[0], Endian.little);
  _f64[0] = value.z;
  view.setUint64(16, _u64[0], Endian.little);
  return 24;
}

bool readVec3Inlined(Vec3 value, ByteData view) {
  _u64[0] = view.getUint64(0, Endian.little);
  value.x = _f64[0];
  _u64[0] = view.getUint64(8, Endian.little);
  value.y = _f64[0];
  _u64[0] = view.getUint64(16, Endian.little);
  value.z = _f64[0];
  return true;
}
