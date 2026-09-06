// The Dart cross-language wire test: the generated Dart libraries write the
// SAME pinned instances the C++ test pins in testdata/wire/*.bin and
// byte-compare against those files — cross-language wire identity is the
// §7.2 gate this leg carries. Plus round-trips through the Dart reader, the
// §5 branch-zeroing checks, the specified-defaults checks, the measure
// functions held to the written wire, the refusal vectors (reject, never
// clamp — and never throw on hostile bytes), and the bench-corpus pins
// (bench_*, real_packet) the C++ bench pinner authored.
//
// Prints OK and exits 0, exactly like its C++/Go/JS twins. Run from
// test/dart (the Makefile does): the wire goldens are at
// ../../testdata/wire. Both modes run in CI: JIT with --enable-asserts (the
// checked twin — writer-contract asserts fire) and AOT release (the
// production twin, issue #155's target — asserts compiled out). The wire
// must be identical in both, and the goldens prove it.
import 'dart:io';
import 'dart:typed_data';

import '../../generated/bench/dart/Bench.dart' as bench;
import '../../generated/bench/dart/Int128.dart';
import '../../generated/bench/dart/realworld/RealWorld.dart' as rw;
import '../../generated/dart/Clauses.dart';
import '../../generated/dart/Degenerate.dart';
import '../../generated/dart/Enums.dart';
import '../../generated/dart/Joins.dart';
import '../../generated/dart/Types.dart';
import '../../generated/dart/Wire.dart';

var failed = false;

void check(bool ok, String what) {
  if (!ok) {
    print('FAILED: $what');
    failed = true;
  }
}

// asserts on = the checked twin (JIT --enable-asserts); off = release AOT.
final bool assertsEnabled = () {
  var on = false;
  assert(on = true);
  return on;
}();

// expectAssert runs a writer-contract violation and demands the assert fires
// (checked twin only — the release writer trusts by design).
void expectAssert(void Function() fn, String what) {
  if (!assertsEnabled) {
    return;
  }
  try {
    fn();
    check(false, what);
  } on AssertionError {
    // the contract fired — expected
  }
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

Uint8List golden(String name) =>
    File('../../testdata/wire/$name.bin').readAsBytesSync();

// The bit-exact measure oracle (#163): the C++ reference writer's exact bit
// count, pinned beside the byte golden — a measure error confined to the
// final byte cannot hide behind byte-granularity gates.
int goldenBits(String name) =>
    int.parse(File('../../testdata/wire/$name.bits').readAsStringSync().trim());

// The shared write buffer — a multiple of 8, larger than any pinned shape.
final Uint8List writeBuf = Uint8List(4096);
final ByteData writeView = ByteData.sublistView(writeBuf);

Uint8List written(int bytes) => Uint8List.sublistView(writeBuf, 0, bytes);

// goldenWire writes value, byte-compares against the C++-pinned golden,
// holds measure to the written wire, reads it back, and re-writes to prove
// byte-identical round-trip — the leg's core instrument.
void pin<T>(
  String name,
  T value,
  T out,
  int Function(T, ByteData) write,
  bool Function(T, ByteData, int) read,
  int Function(T) measure,
) {
  final n = write(value, writeView);
  final g = golden(name);
  check(n == g.length, '$name: wrote $n bytes, golden has ${g.length}');
  check(bytesEqual(written(n), g), '$name: Dart bytes == the C++-pinned bytes');
  final bits = measure(value);
  check((bits + 7) >>> 3 == n, '$name: measure $bits bits vs $n bytes written');
  final pinnedBits = goldenBits(name);
  check(
    bits == pinnedBits,
    '$name: measure $bits bits vs the C++ reference\'s $pinnedBits (bit-exact oracle)',
  );
  check(read(out, ByteData.sublistView(g), g.length * 8), '$name: read');
  final n2 = write(out, writeView);
  check(
    n2 == n && bytesEqual(written(n), g),
    '$name: round-trips to identical bytes',
  );
}

// float32 rounding for expected-value computation (the generated codecs
// carry their own private twin).
final Float32List _f32 = Float32List(1);
double fround(double v) {
  _f32[0] = v;
  return _f32[0];
}

Uint8List textBytes(String s) {
  final b = Uint8List(s.length);
  for (var i = 0; i < s.length; i++) {
    b[i] = s.codeUnitAt(i);
  }
  return b;
}

ShipCreate makeShipCreate() {
  final inp = ShipCreate();
  inp.shipType = ShipType.bomber;
  inp.position.x = 1000;
  inp.position.y = -2000;
  inp.position.z = 3000;
  inp.hasFlags = true;
  inp.flags = shipFlagsBoosting | shipFlagsAiming;
  inp.team = Team.blue;
  inp.health = 750;
  inp.thrust = 55;
  return inp;
}

RigidBody makeRigidBody() {
  final inp = RigidBody();
  inp.position.x = 1.5;
  inp.position.y = -2.5;
  inp.position.z = 3.25;
  inp.orientation.x = 0.1;
  inp.orientation.y = 0.2;
  inp.orientation.z = 0.3;
  inp.orientation.w = 0.9;
  inp.atRest = false;
  inp.linearVelocity.x = 10.0;
  inp.linearVelocity.y = 20.0;
  inp.linearVelocity.z = -3.0;
  inp.angularVelocity.x = 0.25;
  inp.angularVelocity.y = 0.5;
  inp.angularVelocity.z = 0.75;
  return inp;
}

InputPacket makeInputPacket() {
  final p = InputPacket();
  p.synchronizeSequence = 7;
  p.currentFrame = 123456789;
  p.startFrame = 123456780;
  p.inputsCount = 2;
  p.inputs[0].throttle = 0.5;
  p.inputs[0].fire = true;
  p.inputs[1].stickX = -0.25;
  p.inputs[1].boost = true;
  return p;
}

TestData testDataInstance() {
  final inp = TestData();
  inp.a = -100;
  inp.b = 100;
  inp.c = 149;
  inp.d = 0x11;
  inp.e = 0x22;
  inp.f = 0x33;
  inp.g = true;
  inp.itemsCount = 3;
  inp.items[0] = 0;
  inp.items[1] = 128;
  inp.items[2] = 255;
  inp.floatValue = fround(3.1415926);
  inp.compressedFloatValue = 2.5;
  inp.doubleValue = 1.0 / 3.0;
  inp.int8Value = -128;
  inp.int16Value = -32768;
  inp.uint8Value = 255;
  inp.uint16Value = 65535;
  inp.uint32Value = 4294967295;
  inp.uint64Value = 0xffffffffffffffff;
  inp.int64Full = 0x8000000000000000; // int64 min, the hex spelling
  inp.int64Range = -999999999999;
  for (var i = 0; i < inp.fixedBytes.length; i++) {
    inp.fixedBytes[i] = (i * 3) & 0xff;
  }
  inp.text.setAll(0, textBytes('the quick brown fox'));
  inp.textLength = 19;
  return inp;
}

void main() {
  // ---- ShipCreate: the bool-gated flags branch, both ways ----
  {
    final inp = makeShipCreate();
    final out = ShipCreate();
    pin(
      'shipcreate_flags',
      inp,
      out,
      writeShipCreate,
      readShipCreate,
      measureShipCreate,
    );
    check(
      out.hasFlags && out.flags == (shipFlagsBoosting | shipFlagsAiming),
      'ShipCreate flags round-trip',
    );

    // untaken branch: flags must read back ZERO (SPEC §5) — into the same
    // out value, so stale flags would be caught
    inp.hasFlags = false;
    final n = writeShipCreate(inp, writeView);
    final wire = Uint8List.fromList(written(n));
    check(
      readShipCreate(out, ByteData.sublistView(wire), n * 8),
      'read ShipCreate no-flags',
    );
    check(
      !out.hasFlags && out.flags == 0,
      'untaken branch reads as zero (SPEC §5)',
    );
    check(
      (measureShipCreate(inp) + 7) >>> 3 == n,
      'measure tracks the untaken branch',
    );
  }

  // ---- RigidBody: the back-reference example, both branch sides ----
  {
    final inp = makeRigidBody();
    pin(
      'rigidbody_moving',
      inp,
      RigidBody(),
      writeRigidBody,
      readRigidBody,
      measureRigidBody,
    );

    inp.atRest = true;
    final out = RigidBody();
    pin(
      'rigidbody_at_rest',
      inp,
      out,
      writeRigidBody,
      readRigidBody,
      measureRigidBody,
    );
    // the at-rest read must ZERO both velocities (SPEC §5) — out was read
    // from the at-rest wire after the writer's value had them set
    check(out.atRest, 'at_rest reads true');
    check(
      out.linearVelocity.x == 0.0 &&
          out.linearVelocity.y == 0.0 &&
          out.linearVelocity.z == 0.0 &&
          out.angularVelocity.x == 0.0 &&
          out.angularVelocity.y == 0.0 &&
          out.angularVelocity.z == 0.0,
      'velocities read as zero under the taken at-rest branch (SPEC §5)',
    );
  }

  // ---- Chat: the string framing == classic serialize_string over N + 1 ----
  {
    final inp = Chat();
    inp.text.setAll(0, textBytes('wire parity'));
    inp.textLength = 11;
    final out = Chat();
    pin('chat', inp, out, writeChat, readChat, measureChat);
    check(
      out.textLength == 11 &&
          bytesEqual(
            Uint8List.sublistView(out.text, 0, 11),
            textBytes('wire parity'),
          ),
      'Chat round-trips',
    );
  }

  // ---- ProbeHeader: const/reserved/align on the wire; corruption rejected ----
  {
    final inp = ProbeHeader();
    inp.version = 5;
    inp.probeId = 0x1122334455667788;
    final n = writeProbeHeader(inp, writeView);
    check(writeBuf[0] == 0xab, 'const(0xAB, 8) leads the wire');
    final out = ProbeHeader();
    pin(
      'probe_header',
      inp,
      out,
      writeProbeHeader,
      readProbeHeader,
      measureProbeHeader,
    );
    check(
      out.version == 5 && out.probeId == 0x1122334455667788,
      'ProbeHeader round-trips',
    );

    final corrupt = Uint8List.fromList(written(n));
    corrupt[0] = 0xac;
    check(
      !readProbeHeader(out, ByteData.sublistView(corrupt), n * 8),
      'a corrupted wire constant is REJECTED (SPEC §4.3)',
    );
  }

  // ---- InputPacket + TestData against their C++ pins ----
  pin(
    'inputpacket',
    makeInputPacket(),
    InputPacket(),
    writeInputPacket,
    readInputPacket,
    measureInputPacket,
  );
  {
    final out = TestData();
    pin(
      'testdata',
      testDataInstance(),
      out,
      writeTestData,
      readTestData,
      measureTestData,
    );
    check(
      out.int64Full == 0x8000000000000000 &&
          out.uint64Value == 0xffffffffffffffff &&
          out.int8Value == -128,
      'TestData extremes round-trip — signed narrows and full-range ints',
    );
  }

  // ---- CompressedProbe: the FMA-boundary vectors (SPEC §7.2 gate 7) ----
  // 0.005 quantizes to 1 under the float32 two-rounding law; -4.8585 over
  // the non-zero-min range quantizes to 142. Same pinned instance as the
  // C++ leg, against the same golden.
  {
    final inp = CompressedProbe();
    inp.boundary = 0.005;
    inp.offset = -4.8585;
    final out = CompressedProbe();
    pin(
      'compressed_probe',
      inp,
      out,
      writeCompressedProbe,
      readCompressedProbe,
      measureCompressedProbe,
    );
    check(
      out.boundary == fround(fround(1 / 1000) * 10),
      'boundary reconstructs integer 1',
    );
    check(
      out.offset == fround(fround(fround(142 / 10000) * 10) - 5),
      'offset reconstructs integer 142',
    );
  }

  // ---- specified defaults: construction carries them; zero* is the zero form ----
  {
    final sample = ProbeSample();
    check(sample.active, 'ProbeSample.active defaults true');
    zeroProbeSample(sample);
    check(
      !sample.active,
      'the §5 zero form stays zero — zero* does not reapply defaults',
    );
    final config = ProbeConfig();
    check(config.retries == -1, 'ProbeConfig.retries defaults -1');
    check(
      config.preferred == Weapon.railgun,
      'ProbeConfig.preferred defaults Railgun',
    );
  }

  // ---- ProbeBits: the full-range uint32/uint64 paths, C++-pinned ----
  {
    final inp = ProbeBits();
    inp.small = 0x1ff;
    inp.boundary = 0x1ffffffff;
    inp.wide = 0xfedcba9876543210;
    inp.sensor = 4294967295;
    inp.nonce = 0xffffffffffffffff;
    final out = ProbeBits();
    pin('probebits', inp, out, writeProbeBits, readProbeBits, measureProbeBits);
    check(
      out.wide == 0xfedcba9876543210 && out.nonce == 0xffffffffffffffff,
      'ProbeBits round-trips — 9/33/64-bit and full-range paths',
    );
  }

  // ---- ProbeCollider: first-class one-of (SPEC §4.8) — C++-pinned wire,
  // round trip, the None arm, an array of unions, and the refusal negative
  // controls ----
  {
    final inp = ProbeCollider();
    check(
      inp.shape.type == ProbeShapeType.none,
      'construction is the empty union',
    );
    check(probeShapeMaxBits == 2 + 16, 'MaxBits is tag + the largest arm');

    inp.armor = 7;
    inp.shape.type = ProbeShapeType.slab;
    inp.shape.slab.width = 42;
    inp.shape.slab.height = 9;
    // inp.backup stays None — the empty arm costs the tag bits only
    inp.extrasCount = 1;
    inp.extras[0].type = ProbeShapeType.ring;
    inp.extras[0].ring.radius = 777;

    final out = ProbeCollider();
    out.backup.type = ProbeShapeType.ring; // dirty — the read must restore None
    pin(
      'probecollider',
      inp,
      out,
      writeProbeCollider,
      readProbeCollider,
      measureProbeCollider,
    );
    check(
      out.armor == 7 &&
          out.shape.type == ProbeShapeType.slab &&
          out.shape.slab.width == 42 &&
          out.shape.slab.height == 9,
      'the selected arm round-trips',
    );
    check(
      out.backup.type == ProbeShapeType.none,
      'the None arm reads back empty',
    );
    check(
      out.extrasCount == 1 &&
          out.extras[0].type == ProbeShapeType.ring &&
          out.extras[0].ring.radius == 777,
      'the union array round-trips',
    );

    // the all-None shape — the wire is far shorter than MaxBits; a reader
    // whose fused bounds counted MaxBitsUnion would refuse this valid wire
    final none = ProbeCollider();
    none.armor = 7;
    final n = writeProbeCollider(none, writeView);
    final noneWire = Uint8List.fromList(written(n));
    check(
      readProbeCollider(out, ByteData.sublistView(noneWire), n * 8),
      'the all-None union wire reads (no MaxBits over-bounding)',
    );
    check(
      (measureProbeCollider(none) + 7) >>> 3 == n,
      'measure prices the selected arm, not MaxBits',
    );

    // NEGATIVE CONTROL — perturb the tag: 2 bits at bit offset 8, range
    // [0, 2]; forcing both bits makes 3 and the read must refuse
    final g = golden('probecollider');
    final corrupt = Uint8List.fromList(g);
    corrupt[1] |= 0x03;
    check(
      !readProbeCollider(out, ByteData.sublistView(corrupt), g.length * 8),
      'an out-of-range union tag is refused (SPEC §4.8)',
    );

    // NEGATIVE CONTROL — corrupt the arm payload: width rides 7 bits at bit
    // offset 10 with range [0, 100]; all seven bits decode 127
    final corrupt2 = Uint8List.fromList(g);
    corrupt2[1] |= 0xfc;
    corrupt2[2] |= 0x01;
    check(
      !readProbeCollider(out, ByteData.sublistView(corrupt2), g.length * 8),
      'a corrupt union arm payload is refused (SPEC §4.8)',
    );

    // the write side validates the tag BEFORE it rides (checked twin)
    expectAssert(() {
      final rogue = ProbeShape();
      rogue.type = 3;
      writeProbeShape(rogue, writeView);
    }, 'an out-of-set union tag must trip the writer contract');
  }

  // ---- ProbeSample: the nested if/else wire, both ways, and §5 zeroing ----
  {
    final inp = ProbeSample(); // active = true
    inp.orientation = 90.0;
    inp.rawDelta = -5;
    inp.bigDelta = -1234567890123;
    inp.weapon = Weapon.laser;
    inp.hasTarget = true;
    inp.targetId = 777;
    inp.idleTicks = 12345; // untaken side on the wire — must read back ZERO
    inp.samplesCount = 1;
    inp.samples[0] = 42;

    final n = writeProbeSample(inp, writeView);
    final wire = Uint8List.fromList(written(n));
    final out = ProbeSample();
    check(
      readProbeSample(out, ByteData.sublistView(wire), n * 8),
      'read ProbeSample active',
    );
    check(
      out.active &&
          out.weapon == Weapon.laser &&
          out.hasTarget &&
          out.targetId == 777,
      'the taken branch round-trips, nested branch included',
    );
    check(out.idleTicks == 0, 'the untaken else side reads as zero (SPEC §5)');
    check(
      out.orientation == 90.0,
      'compressed float round-trips exactly at its resolution',
    );
    check(
      (measureProbeSample(inp) + 7) >>> 3 == n,
      'ProbeSample measure vs written bytes',
    );

    inp.active = false;
    inp.hasTarget = false;
    final n2 = writeProbeSample(inp, writeView);
    final wire2 = Uint8List.fromList(written(n2));
    check(
      readProbeSample(out, ByteData.sublistView(wire2), n2 * 8),
      'read ProbeSample idle',
    );
    check(!out.active && out.idleTicks == 12345, 'the else branch round-trips');
    check(
      out.weapon == Weapon.none && !out.hasTarget && out.targetId == 0,
      'the whole untaken then side reads as zero, nested branch included',
    );
  }

  // ---- ProbeArray: transitive defaults and its C++ pin ----
  {
    final fresh = ProbeArray();
    check(
      fresh.samples[0].active && fresh.samples[1].active,
      'defaults reach through a fixed array',
    );
    check(
      fresh.config.retries == -1 && fresh.config.preferred == Weapon.railgun,
      'defaults reach through a plain member',
    );

    final inp = ProbeArray();
    inp.samples[0].orientation = 90.0;
    inp.samples[0].rawDelta = -5;
    inp.samples[0].bigDelta = -1234567890123;
    inp.samples[0].weapon = Weapon.laser;
    inp.samples[0].hasTarget = true;
    inp.samples[0].targetId = 777;
    inp.samples[0].samplesCount = 1;
    inp.samples[0].samples[0] = 42;
    inp.samples[1].active = false;
    inp.samples[1].orientation = -45.5;
    inp.samples[1].rawDelta = 7;
    inp.samples[1].bigDelta = 99;
    inp.samples[1].idleTicks = 1000;
    inp.samples[1].samplesCount = 2;
    inp.samples[1].samples[0] = 7;
    inp.samples[1].samples[1] = 8;
    inp.config.retries = 3;
    inp.config.preferred = Weapon.missile;

    final out = ProbeArray();
    pin(
      'probearray',
      inp,
      out,
      writeProbeArray,
      readProbeArray,
      measureProbeArray,
    );
    check(
      !out.samples[1].active && out.samples[1].idleTicks == 1000,
      'nested else branch round-trips',
    );
    check(
      out.samples[1].weapon == Weapon.none && !out.samples[1].hasTarget,
      'nested untaken side reads as zero (SPEC §5)',
    );
    check(
      out.config.retries == 3 && out.config.preferred == Weapon.missile,
      'config round-trips',
    );
  }

  // ---- ProbeReport: nested composition, and the widened flags wire ----
  {
    final inp = ProbeReport();
    inp.header.version = 3;
    inp.header.probeId = 0xcafebabe;
    inp.flags = probeFlagsArmed | probeFlagsDamaged;
    inp.echo.testA = 555;
    inp.echo.testB = 1000;

    final n = writeProbeReport(inp, writeView);
    final wire = Uint8List.fromList(written(n));
    final out = ProbeReport();
    check(
      readProbeReport(out, ByteData.sublistView(wire), n * 8),
      'read ProbeReport',
    );
    check(
      out.header.probeId == 0xcafebabe &&
          out.flags == (probeFlagsArmed | probeFlagsDamaged) &&
          out.echo.testA == 555 &&
          out.echo.testB == 1000,
      'ProbeReport round-trips — a named type as an ordinary field',
    );

    // a mask bit above the widened 8-bit wire is refused, not truncated
    expectAssert(() {
      inp.flags = 1 << 9;
      writeProbeReport(inp, writeView);
    }, 'a mask bit above the flags wire width must trip the writer contract');
  }

  // ---- Block: the bytes(N) framing; Heartbeat: the empty body ----
  {
    final inp = Block();
    for (var i = 0; i < 100; i++) {
      inp.data[i] = i;
    }
    inp.dataLength = 100;
    final n = writeBlock(inp, writeView);
    final wire = Uint8List.fromList(written(n));
    final out = Block();
    check(readBlock(out, ByteData.sublistView(wire), n * 8), 'read Block');
    check(
      out.dataLength == 100 &&
          bytesEqual(
            Uint8List.sublistView(out.data, 0, 100),
            Uint8List.sublistView(inp.data, 0, 100),
          ),
      'Block round-trips — bytes(N) framing',
    );
    check(measureBlock(inp) % 8 == 0, 'Block wire ends byte-aligned');

    final hb = Heartbeat();
    check(
      writeHeartbeat(hb, writeView) == 0 &&
          readHeartbeat(hb, writeView, 0) &&
          measureHeartbeat(hb) == 0,
      'Heartbeat — presence is the payload (SPEC §4.6)',
    );
  }

  // ---- the readers agree on what they REJECT, and never throw ----
  {
    // an interior null in a string is content the read refuses
    final chatGolden = golden('chat');
    final corrupt = Uint8List.fromList(chatGolden);
    corrupt[4] = 0; // inside the text bytes (length rides bytes 0-1)
    final out = Chat();
    check(
      !readChat(out, ByteData.sublistView(corrupt), corrupt.length * 8),
      'an interior null is rejected (SPEC §4.7)',
    );

    // a truncated stream is refused by the fused bounds checks
    final truncated = Uint8List.sublistView(chatGolden, 0, 3);
    check(
      !readChat(out, ByteData.sublistView(truncated), truncated.length * 8),
      'truncation is refused, not thrown',
    );

    // a numBits larger than the buffer is refused up front
    check(
      !readChat(out, ByteData.sublistView(truncated), 4096),
      'an oversized numBits is refused up front',
    );

    // a nonzero reserved bit is rejected
    final probeGolden = golden('probe_header');
    final corrupt2 = Uint8List.fromList(probeGolden);
    corrupt2[1] |= 0x08; // the first reserved bit above version's 3
    final out3 = ProbeHeader();
    check(
      !readProbeHeader(
        out3,
        ByteData.sublistView(corrupt2),
        corrupt2.length * 8,
      ),
      'a nonzero reserved bit is rejected (SPEC §4.3)',
    );

    // an out-of-range array count is refused before any element rides —
    // corrupt the count bits INSIDE a complete valid wire (the preamble is
    // 16+64+64 = 144 bits, so the 5-bit count sits at byte 18 bits 0-4)
    final packetGolden = golden('inputpacket');
    final corrupt3 = Uint8List.fromList(packetGolden);
    corrupt3[18] = (corrupt3[18] & ~0x1f) | 17; // count 2 -> 17, over [0, 16]
    final out4 = InputPacket();
    check(
      !readInputPacket(
        out4,
        ByteData.sublistView(corrupt3),
        corrupt3.length * 8,
      ),
      'an out-of-range count is refused before the loop',
    );
  }

  // ---- checked-twin writer contracts (release trusts by design) ----
  {
    expectAssert(() {
      final bad = ShipCreate();
      bad.health = 5000; // above [0, MaxHealth]
      writeShipCreate(bad, writeView);
    }, 'an out-of-range ranged write must trip the writer contract');

    expectAssert(() {
      final bad = Chat();
      bad.textLength = 999; // above string(MaxChatLength)
      writeChat(bad, writeView);
    }, 'an out-of-range string length must trip the writer contract');

    // A COUNT IS NOT A CHECKED-TWIN CONTRACT (SPEC §4.6): the count guards
    // the element loop and the pack subtracts the low bound, so a count
    // outside its wire range is refused in EVERY build — with asserts and
    // without — rather than left to a predicate release removes.
    {
      final bad = InputPacket();
      bad.inputsCount = 17; // above [0, MaxInputsPerPacket]
      check(writeInputPacket(bad, writeView) == -1,
          'a count above its wire range is refused in every build');
    }

    expectAssert(() {
      final bad = ShipCreate();
      bad.shipType = 99; // enum headroom above the wire range
      writeShipCreate(bad, writeView);
    }, 'enum headroom above the wire range must trip the writer contract');
  }

  // ---- flagName / flagNames: per-bit names and the set renderer ----
  {
    check(flagNameShipFlags(0) == 'FiringLaser', 'flagName names bit 0');
    check(flagNameShipFlags(9) == '???', 'flagName is out-of-range safe');
    check(flagNamesShipFlags(0) == '0', 'flagNames renders the empty set as 0');
    check(
      flagNamesShipFlags(shipFlagsFiringLaser | shipFlagsBraking) ==
          'FiringLaser|Braking',
      'flagNames renders the set bits',
    );
    check(
      flagNamesShipFlags(shipFlagsAiming | (1 << 63)) ==
          'Aiming|0x8000000000000000',
      'flagNames renders unknown high bits as hex',
    );
    check(
      enumNameWeapon(Weapon.railgun) == 'Railgun',
      'enumName names a variant',
    );
    check(enumNameWeapon(15) == '???', 'enumName is headroom-safe');
  }

  // ============== THE BENCH CORPUS (BENCH-STANDARD.md §1.5) ==============
  // The same pinned instances test/bench/main.cpp authored into
  // testdata/wire/{bench_*,real_packet}.bin — the oracle gate the Dart bench
  // runner is held to; this leg carries it because the runner imports these
  // exact libraries.
  {
    final packet = bench.BenchPacket();
    packet.a = -37;
    packet.b = 12345;
    packet.c = 987654;
    packet.bits7 = 97;
    packet.bits13 = 5000;
    packet.bits23 = 1234567;
    packet.flag = true;
    packet.x = 1.5;
    packet.y = -3.25;
    packet.z = 100.125;
    packet.big = 0x123456789abcdef0;
    for (var i = 0; i < 17; i++) {
      packet.blob[i] = (i * 31) & 0xff;
    }
    pin(
      'bench_packet',
      packet,
      bench.BenchPacket(),
      bench.writeBenchPacket,
      bench.readBenchPacket,
      bench.measureBenchPacket,
    );
    check(bench.measureBenchPacket(packet) == 392, 'BenchPacket is 392 bits');

    final ints = bench.BenchInts();
    ints.f0 = -37;
    ints.f1 = 12345;
    ints.f2 = 987654;
    ints.f3 = 2;
    ints.f4 = -15;
    ints.f5 = 777;
    ints.f6 = -2048;
    ints.f7 = 200;
    ints.f8 = -543210;
    ints.f9 = 99;
    pin(
      'bench_ints',
      ints,
      bench.BenchInts(),
      bench.writeBenchInts,
      bench.readBenchInts,
      bench.measureBenchInts,
    );

    final bits = bench.BenchBits();
    bits.b7 = 97;
    bits.b13 = 5000;
    bits.b23 = 1234567;
    bits.b3 = 5;
    bits.b32 = 0xdeadbeef;
    bits.b11 = 1024;
    bits.b19 = 333333;
    bits.b48 = 0xfedcba987654;
    pin(
      'bench_bits',
      bits,
      bench.BenchBits(),
      bench.writeBenchBits,
      bench.readBenchBits,
      bench.measureBenchBits,
    );

    // BenchMixed — THE canonical benchmark shape (#184); the pin is
    // test/bench/main.cpp's, transcribed exactly
    final mixed = bench.BenchMixed();
    mixed.sequence = 52428;
    mixed.ackSequence = 12345;
    mixed.ackBits = 0xa5a5a5a5;
    mixed.sessionId = 0x123456789abcdef0;
    mixed.clientId = 0xdeadbeef;
    mixed.nonce = 0xfedcba9876543210;
    mixed.worldTime = -987654321000;
    mixed.frameTick = 0x123456789abc;
    mixed.serverTime = 12345678;
    mixed.entitiesCount = 8;
    for (var i = 0; i < 8; i++) {
      final e = mixed.entities[i];
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
      e.moving = (i % 2) == 0;
      e.firing = (i % 3) == 0;
    }
    mixed.statsCount = 80;
    for (var i = 0; i < 80; i++) {
      mixed.stats[i].statId = (i * 3) % 256;
      mixed.stats[i].delta = -512 + (i * 13) % 1024;
    }
    mixed.gameEvent.type = bench.MixedEventType.hit;
    mixed.gameEvent.hit.targetId = 4095;
    mixed.gameEvent.hit.damage = 4095;
    mixed.gameEvent.hit.hitKind = 7;
    mixed.gameEvent.hit.crit = true;
    mixed.loadout.setAll(0, [0x11, 0x22, 0x33, 0x44]);
    mixed.playerName.setAll(0, 'Rowan_01'.codeUnits);
    mixed.playerNameLength = 8;
    mixed.payload.setAll(0, [0xde, 0xad, 0xbe, 0xef, 0x01, 0x02, 0x03, 0x04]);
    mixed.payloadLength = 8;
    mixed.aimX = 0.5;
    mixed.aimY = -0.25;
    mixed.aimZ = 0.75;
    mixed.recoil = 1.5;
    mixed.drift = -3.25;
    mixed.wideKey = const UInt128(0x0123456789abcdef, 0xfedcba9876543210);
    mixed.flux = const Int128(0x800000000, 7); // 2^99 + 7
    mixed.ping = 12345;
    mixed.crcHint = 0xabcdef;
    mixed.hasExtra = true;
    mixed.extra = 200;
    pin(
      'bench_mixed',
      mixed,
      bench.BenchMixed(),
      bench.writeBenchMixed,
      bench.readBenchMixed,
      bench.measureBenchMixed,
    );

    // RealPacket pins the ALL-DEFAULTS instance: constructed and serialized
    // unmodified — 1629 bits = 204 bytes
    final real = rw.RealPacket();
    pin(
      'real_packet',
      real,
      rw.RealPacket(),
      rw.writeRealPacket,
      rw.readRealPacket,
      rw.measureRealPacket,
    );
    check(rw.measureRealPacket(real) == 1629, 'RealPacket is 1629 bits');
  }

  // ---- Degenerate.schema: the degenerate arrangements (issue #203) ----
  //
  // Twelve shapes in the C++ test's order against the one C++-pinned golden.
  // This emitter writes a whole message into a buffer rather than appending
  // to a bit stream, so the leg CONCATENATES its twelve buffers — which
  // equals what the stream legs write because every type in that file is a
  // whole number of bytes wide. A fixed scalar array whose elements an
  // emitter places TWICE is invisible to a same-language round trip; only
  // this compare against another language's bytes names it.
  {
    final vec2 = Vec2()
      ..x = 1.5
      ..y = -2.25;

    final spanF64 = SpanF64();
    spanF64.values[0] = 3.5;
    spanF64.values[1] = -4.75;

    final spanU64 = SpanU64();
    spanU64.values[0] = 0xdeadbeefcafebabe;
    spanU64.values[1] = 1;

    final spanI64 = SpanI64();
    spanI64.values[0] = -1234567890123;
    spanI64.values[1] = 42;

    final spanOne = SpanOne();
    spanOne.values[0] = 0x0123456789abcdef;

    final spanChunk = SpanChunk();
    spanChunk.values[0] = 0x1111;
    spanChunk.values[1] = 0x2222;
    spanChunk.values[2] = 0x3333;
    spanChunk.values[3] = 0x4444;

    final spanTail = SpanTail()..tail = 0xfeedface;
    spanTail.values[0] = 6.125;
    spanTail.values[1] = -7.0;

    final spanTwice = SpanTwice();
    spanTwice.a[0] = 8.5;
    spanTwice.a[1] = 9.5;
    spanTwice.b[0] = -10.5;
    spanTwice.b[1] = -11.5;

    final trio = Trio()
      ..a = 0xabcde
      ..b = 0x12345
      ..c = 0xfffff;

    final trioSole = TrioSole();
    trioSole.inner
      ..a = 1
      ..b = 2
      ..c = 3;

    final trioFirst = TrioFirst()..trailer = 0xbeef;
    trioFirst.inner
      ..a = 0xaaaaa
      ..b = 0x55555
      ..c = 0xf0f0f;

    final straddle = TrioStraddle()
      ..pad0 = 0x0011223344556677
      ..pad1 = 0x8899aabbccddeeff
      ..pad2 = 0xffffffffffffffff
      ..pad3 = 0
      ..pad4 = 0x123456789abcdef0
      ..pad5 = 0xabcdef;
    straddle.inner
      ..a = 0x11111
      ..b = 0x22222
      ..c = 0x33333;

    // write each message into its own buffer, then concatenate
    final parts = <Uint8List>[];
    int emit<T>(
      T value,
      int Function(T, ByteData) write,
      int Function(T) measure,
      String name,
    ) {
      final buf = Uint8List(256);
      final n = write(value, ByteData.sublistView(buf));
      check((measure(value) + 7) >>> 3 == n, '$name: measure vs bytes written');
      parts.add(Uint8List.sublistView(buf, 0, n));
      return n;
    }

    var total = 0;
    total += emit(vec2, writeVec2, measureVec2, 'Vec2');
    total += emit(spanF64, writeSpanF64, measureSpanF64, 'SpanF64');
    total += emit(spanU64, writeSpanU64, measureSpanU64, 'SpanU64');
    total += emit(spanI64, writeSpanI64, measureSpanI64, 'SpanI64');
    total += emit(spanOne, writeSpanOne, measureSpanOne, 'SpanOne');
    total += emit(spanChunk, writeSpanChunk, measureSpanChunk, 'SpanChunk');
    total += emit(spanTail, writeSpanTail, measureSpanTail, 'SpanTail');
    total += emit(spanTwice, writeSpanTwice, measureSpanTwice, 'SpanTwice');
    total += emit(trio, writeTrio, measureTrio, 'Trio');
    total += emit(trioSole, writeTrioSole, measureTrioSole, 'TrioSole');
    total += emit(trioFirst, writeTrioFirst, measureTrioFirst, 'TrioFirst');
    total += emit(
      straddle,
      writeTrioStraddle,
      measureTrioStraddle,
      'TrioStraddle',
    );
    check(
      total * 8 ==
          128 + 128 + 128 + 128 + 64 + 64 + 160 + 256 + 64 + 64 + 80 + 408,
      'the twelve degenerate shapes ride their declared widths and nothing more',
    );

    final joined = Uint8List(total);
    var at = 0;
    for (final part in parts) {
      joined.setRange(at, at + part.length, part);
      at += part.length;
    }
    final g = golden('degenerate');
    check(
      joined.length == g.length,
      'degenerate: wrote ${joined.length} bytes, golden has ${g.length}',
    );
    check(
      bytesEqual(joined, g),
      'degenerate: Dart bytes == the C++-pinned bytes',
    );

    // and each shape reads back out of its own slice of the golden
    at = 0;
    bool back<T>(T out, int bytes, bool Function(T, ByteData, int) read) {
      final slice = Uint8List(bytes + 8); // read slack
      slice.setRange(0, bytes, g, at);
      at += bytes;
      return read(out, ByteData.sublistView(slice), bytes * 8);
    }

    final rVec2 = Vec2();
    final rSpanF64 = SpanF64();
    final rSpanU64 = SpanU64();
    final rSpanI64 = SpanI64();
    final rSpanOne = SpanOne();
    final rSpanChunk = SpanChunk();
    final rSpanTail = SpanTail();
    final rSpanTwice = SpanTwice();
    final rTrio = Trio();
    final rTrioSole = TrioSole();
    final rTrioFirst = TrioFirst();
    final rStraddle = TrioStraddle();

    check(back(rVec2, 16, readVec2), 'read Vec2');
    check(back(rSpanF64, 16, readSpanF64), 'read SpanF64');
    check(back(rSpanU64, 16, readSpanU64), 'read SpanU64');
    check(back(rSpanI64, 16, readSpanI64), 'read SpanI64');
    check(back(rSpanOne, 8, readSpanOne), 'read SpanOne');
    check(back(rSpanChunk, 8, readSpanChunk), 'read SpanChunk');
    check(back(rSpanTail, 20, readSpanTail), 'read SpanTail');
    check(back(rSpanTwice, 32, readSpanTwice), 'read SpanTwice');
    check(back(rTrio, 8, readTrio), 'read Trio');
    check(back(rTrioSole, 8, readTrioSole), 'read TrioSole');
    check(back(rTrioFirst, 10, readTrioFirst), 'read TrioFirst');
    check(back(rStraddle, 51, readTrioStraddle), 'read TrioStraddle');

    check(rVec2.x == 1.5 && rVec2.y == -2.25, 'Vec2 round-trips');
    check(
      rSpanF64.values[0] == 3.5 && rSpanF64.values[1] == -4.75,
      'SpanF64 round-trips',
    );
    check(
      rSpanU64.values[0] == 0xdeadbeefcafebabe && rSpanU64.values[1] == 1,
      'SpanU64 round-trips',
    );
    check(
      rSpanI64.values[0] == -1234567890123 && rSpanI64.values[1] == 42,
      'SpanI64 round-trips',
    );
    check(rSpanOne.values[0] == 0x0123456789abcdef, 'SpanOne round-trips');
    check(
      rSpanChunk.values[0] == 0x1111 && rSpanChunk.values[3] == 0x4444,
      'SpanChunk round-trips',
    );
    check(
      rSpanTail.values[0] == 6.125 &&
          rSpanTail.values[1] == -7.0 &&
          rSpanTail.tail == 0xfeedface,
      'SpanTail round-trips',
    );
    check(
      rSpanTwice.a[0] == 8.5 && rSpanTwice.b[1] == -11.5,
      'SpanTwice round-trips',
    );
    check(
      rTrio.a == 0xabcde && rTrio.b == 0x12345 && rTrio.c == 0xfffff,
      'Trio round-trips',
    );
    check(
      rTrioSole.inner.a == 1 && rTrioSole.inner.c == 3,
      'TrioSole round-trips',
    );
    check(
      rTrioFirst.inner.a == 0xaaaaa && rTrioFirst.trailer == 0xbeef,
      'TrioFirst round-trips',
    );
    check(
      rStraddle.pad0 == 0x0011223344556677 &&
          rStraddle.pad4 == 0x123456789abcdef0,
      'TrioStraddle pads round-trip',
    );
    check(
      rStraddle.pad5 == 0xabcdef &&
          rStraddle.inner.a == 0x11111 &&
          rStraddle.inner.c == 0x33333,
      "TrioStraddle's nested fields round-trip across the boundary",
    );
  }

  // ---- Clauses.schema / Joins.schema: the mid-byte arrangements ----
  //
  // Degenerate.schema is every-type-a-whole-number-of-bytes by construction,
  // so no clause boundary in it lands mid-byte. These two units are chosen so
  // they do. This emitter writes a whole message into a buffer rather than
  // appending to a bit stream, and here the shapes are NOT byte-aligned — so
  // the golden is deliberately the concatenation of the shapes written alone,
  // which is what every leg can reproduce.
  {
    final parts = <Uint8List>[];

    void emit<T>(
      String name,
      int bits,
      T value,
      int Function(T, ByteData) write,
      int Function(T) measure,
    ) {
      final buf = Uint8List(64);
      final n = write(value, ByteData.sublistView(buf));
      check(
        measure(value) == bits,
        '$name: measure ${measure(value)}, expected $bits bits',
      );
      check(
        n == (bits + 7) >>> 3,
        '$name: wrote $n bytes, expected ${(bits + 7) >>> 3}',
      );
      parts.add(Uint8List.sublistView(buf, 0, n));
    }

    Uint8List joinParts() {
      final total = parts.fold<int>(0, (n, p) => n + p.length);
      final joined = Uint8List(total);
      var at = 0;
      for (final part in parts) {
        joined.setRange(at, at + part.length, part);
        at += part.length;
      }
      return joined;
    }

    // ---- Clauses.schema ----

    for (final c in [0, 1, 3, 4, 5, 7, 12]) {
      final v = W13()..itemsCount = c;
      for (var i = 0; i < c; i++) {
        v.items[i] = 8191 - i * 733;
      }
      emit('W13/$c', 4 + 13 * c, v, writeW13, measureW13);
    }
    for (final c in [0, 1, 2, 3, 4, 9]) {
      final v = W17()..itemsCount = c;
      for (var i = 0; i < c; i++) {
        v.items[i] = 131071 - i * 11117;
      }
      emit('W17/$c', 4 + 17 * c, v, writeW17, measureW17);
    }
    for (final c in [0, 1, 2, 3, 6]) {
      final v = W26()..itemsCount = c;
      for (var i = 0; i < c; i++) {
        v.items[i] = 67108863 - i * 5555555;
      }
      emit('W26/$c', 3 + 26 * c, v, writeW26, measureW26);
    }
    for (final c in [0, 1, 3, 4, 5, 20]) {
      final v = W1()..itemsCount = c;
      for (var i = 0; i < c; i++) {
        v.items[i] = i % 2;
      }
      emit('W1/$c', 5 + c, v, writeW1, measureW1);
    }
    for (var c = 0; c <= 3; c++) {
      final v = W52()..itemsCount = c;
      for (var i = 0; i < c; i++) {
        v.items[i] = 4503599627370495 - i * 123456789;
      }
      emit('W52/$c', 2 + 52 * c, v, writeW52, measureW52);
    }
    for (var c = 0; c <= 3; c++) {
      final v = W50()..itemsCount = c;
      for (var i = 0; i < c; i++) {
        v.items[i] = 1125899906842623 - i * 987654321;
      }
      emit('W50/$c', 2 + 50 * c, v, writeW50, measureW50);
    }
    {
      final v = F13();
      for (var i = 0; i < 7; i++) {
        v.items[i] = 8191 - i * 911;
      }
      emit('F13', 91, v, writeF13, measureF13);
    }
    for (final c in [0, 1, 3, 4, 5, 10]) {
      final v = ArrTri3()..itemsCount = c;
      for (var i = 0; i < c; i++) {
        v.items[i]
          ..a = i % 2
          ..b = i % 4;
      }
      emit('ArrTri3/$c', 4 + 3 * c, v, writeArrTri3, measureArrTri3);
    }
    {
      final v = ArrEleven();
      for (var i = 0; i < 9; i++) {
        v.items[i]
          ..a = i % 8
          ..b = 255 - i * 17;
      }
      emit('ArrEleven', 99, v, writeArrEleven, measureArrEleven);
    }
    // lead 5 + tag 2 + tail 7 — a zero-bit arm behind a tag costs the tag
    for (final tag in [
      EmptyUnionType.none,
      EmptyUnionType.a,
      EmptyUnionType.b,
    ]) {
      final v = HoldsEmptyUnion()
        ..lead = 21
        ..tail = 99;
      v.u.type = tag;
      emit(
        'HoldsEmptyUnion/$tag',
        14,
        v,
        writeHoldsEmptyUnion,
        measureHoldsEmptyUnion,
      );
    }
    // lead 5 + s_length 4 = 9, the align pads 7 to 16; then 8*s bytes,
    // b_length 4, an align pad of 4, 8*b bytes and a 3-bit tail. The 5-bit
    // lead is what puts the align at a non-zero offset.
    final strsS = <List<int>>[<int>[], 'abcdefgh'.codeUnits, 'xyz'.codeUnits];
    final strsB = <List<int>>[
      <int>[],
      <int>[0xf0, 0xf1, 0xf2, 0xf3, 0xf4, 0xf5, 0xf6, 0xf7],
      <int>[1, 2, 3],
    ];
    final strsBits = <int>[27, 155, 75];
    for (var k = 0; k < 3; k++) {
      final v = Strs()
        ..lead = 21
        ..tail = 5
        ..sLength = strsS[k].length
        ..bLength = strsB[k].length;
      v.s.setRange(0, strsS[k].length, strsS[k]);
      v.b.setRange(0, strsB[k].length, strsB[k]);
      emit('Strs/$k', strsBits[k], v, writeStrs, measureStrs);
    }
    for (var c = 0; c <= 4; c++) {
      final v = ArrNested()
        ..lead = 21
        ..tail = 5
        ..itemsCount = c;
      for (var i = 0; i < c; i++) {
        v.items[i]
          ..a = i % 8
          ..b = 200 - i * 7;
      }
      emit('ArrNested/$c', 11 + 11 * c, v, writeArrNested, measureArrNested);
    }
    emit('Sole', 13, Sole()..only = 5555, writeSole, measureSole);

    {
      final joined = joinParts();
      final g = golden('clauses');
      check(
        joined.length == g.length,
        'clauses: wrote ${joined.length} bytes, golden has ${g.length}',
      );
      check(
        bytesEqual(joined, g),
        'clauses: Dart bytes == the C++-pinned bytes',
      );

      // Read each shape back out of its own slice. A clause that decodes a
      // different number of elements than the writer encoded shows up here
      // even where the byte compare above happens to pass.
      var at = 0;
      bool back<T>(T out, int bits, bool Function(T, ByteData, int) read) {
        final bytes = (bits + 7) >>> 3;
        final slice = Uint8List(bytes + 8); // read slack
        slice.setRange(0, bytes, g, at);
        at += bytes;
        return read(out, ByteData.sublistView(slice), bits);
      }

      for (final c in [0, 1, 3, 4, 5, 7, 12]) {
        final r = W13();
        check(back(r, 4 + 13 * c, readW13), 'read W13/$c');
        check(r.itemsCount == c, 'W13/$c count round-trips');
        for (var i = 0; i < c; i++) {
          check(r.items[i] == 8191 - i * 733, 'W13/$c element round-trips');
        }
      }
      for (final c in [0, 1, 2, 3, 4, 9]) {
        final r = W17();
        check(back(r, 4 + 17 * c, readW17), 'read W17/$c');
        check(r.itemsCount == c, 'W17/$c count round-trips');
        for (var i = 0; i < c; i++) {
          check(r.items[i] == 131071 - i * 11117, 'W17/$c element round-trips');
        }
      }
      for (final c in [0, 1, 2, 3, 6]) {
        final r = W26();
        check(back(r, 3 + 26 * c, readW26), 'read W26/$c');
        check(r.itemsCount == c, 'W26/$c count round-trips');
        for (var i = 0; i < c; i++) {
          check(
            r.items[i] == 67108863 - i * 5555555,
            'W26/$c element round-trips',
          );
        }
      }
      for (final c in [0, 1, 3, 4, 5, 20]) {
        final r = W1();
        check(back(r, 5 + c, readW1), 'read W1/$c');
        check(r.itemsCount == c, 'W1/$c count round-trips');
        for (var i = 0; i < c; i++) {
          check(r.items[i] == i % 2, 'W1/$c element round-trips');
        }
      }
      for (var c = 0; c <= 3; c++) {
        final r = W52();
        check(back(r, 2 + 52 * c, readW52), 'read W52/$c');
        check(r.itemsCount == c, 'W52/$c count round-trips');
        for (var i = 0; i < c; i++) {
          check(
            r.items[i] == 4503599627370495 - i * 123456789,
            'W52/$c element round-trips',
          );
        }
      }
      for (var c = 0; c <= 3; c++) {
        final r = W50();
        check(back(r, 2 + 50 * c, readW50), 'read W50/$c');
        check(r.itemsCount == c, 'W50/$c count round-trips');
        for (var i = 0; i < c; i++) {
          check(
            r.items[i] == 1125899906842623 - i * 987654321,
            'W50/$c element round-trips',
          );
        }
      }
      {
        final r = F13();
        check(back(r, 91, readF13), 'read F13');
        for (var i = 0; i < 7; i++) {
          check(r.items[i] == 8191 - i * 911, 'F13 element round-trips');
        }
      }
      for (final c in [0, 1, 3, 4, 5, 10]) {
        final r = ArrTri3();
        check(back(r, 4 + 3 * c, readArrTri3), 'read ArrTri3/$c');
        check(r.itemsCount == c, 'ArrTri3/$c count round-trips');
        for (var i = 0; i < c; i++) {
          check(
            r.items[i].a == i % 2 && r.items[i].b == i % 4,
            'ArrTri3/$c element round-trips',
          );
        }
      }
      {
        final r = ArrEleven();
        check(back(r, 99, readArrEleven), 'read ArrEleven');
        for (var i = 0; i < 9; i++) {
          check(
            r.items[i].a == i % 8 && r.items[i].b == 255 - i * 17,
            'ArrEleven element round-trips',
          );
        }
      }
      for (final tag in [
        EmptyUnionType.none,
        EmptyUnionType.a,
        EmptyUnionType.b,
      ]) {
        final r = HoldsEmptyUnion();
        check(back(r, 14, readHoldsEmptyUnion), 'read HoldsEmptyUnion/$tag');
        check(
          r.lead == 21 && r.tail == 99 && r.u.type == tag,
          'HoldsEmptyUnion/$tag round-trips',
        );
      }
      for (var k = 0; k < 3; k++) {
        final r = Strs();
        check(back(r, strsBits[k], readStrs), 'read Strs/$k');
        check(r.lead == 21 && r.tail == 5, 'Strs/$k lead and tail round-trip');
        check(
          r.sLength == strsS[k].length && r.bLength == strsB[k].length,
          'Strs/$k lengths round-trip',
        );
        for (var i = 0; i < strsS[k].length; i++) {
          check(r.s[i] == strsS[k][i], 'Strs/$k string byte round-trips');
        }
        for (var i = 0; i < strsB[k].length; i++) {
          check(r.b[i] == strsB[k][i], 'Strs/$k bytes byte round-trips');
        }
      }
      for (var c = 0; c <= 4; c++) {
        final r = ArrNested();
        check(back(r, 11 + 11 * c, readArrNested), 'read ArrNested/$c');
        check(
          r.itemsCount == c && r.lead == 21 && r.tail == 5,
          'ArrNested/$c round-trips',
        );
        for (var i = 0; i < c; i++) {
          check(
            r.items[i].a == i % 8 && r.items[i].b == 200 - i * 7,
            'ArrNested/$c element round-trips',
          );
        }
      }
      {
        final r = Sole();
        check(back(r, 13, readSole), 'read Sole');
        check(r.only == 5555, 'Sole round-trips');
      }
      check(at == g.length, 'the clauses reads consume the whole golden');
    }

    // ---- Joins.schema ----
    //
    // Every branch is written on BOTH arms, so no path is pinned by omission.
    // The expected value after a round trip is not the value written: the
    // untaken side reads back as zero (SPEC §5).

    parts.clear();

    for (final f in [false, true]) {
      // the arms agree on WIDTH but not on value, so a join that keeps the
      // wrong arm is a value mismatch and not just a width one
      emit(
        'ArmsAgree/$f',
        24,
        ArmsAgree()
          ..lead = 21
          ..flag = f
          ..a = 1234
          ..b = 1500
          ..tail = 99,
        writeArmsAgree,
        measureArmsAgree,
      );
      emit(
        'ArmsDisagree/$f',
        f ? 24 : 16,
        ArmsDisagree()
          ..lead = 21
          ..flag = f
          ..a = 1234
          ..b = 5
          ..tail = 99,
        writeArmsDisagree,
        measureArmsDisagree,
      );
      emit(
        'ArmEmpty/$f',
        f ? 32 : 13,
        ArmEmpty()
          ..lead = 21
          ..flag = f
          ..a = 456789
          ..tail = 99,
        writeArmEmpty,
        measureArmEmpty,
      );
      final alignStr = ArmAlign()
        ..lead = 21
        ..flag = f
        ..sLength = 4
        ..b = 1000
        ..tail = 99;
      alignStr.s.setRange(0, 4, 'abcd'.codeUnits);
      emit(
        'ArmAlign/$f',
        f ? 55 : 23,
        alignStr,
        writeArmAlign,
        measureArmAlign,
      );
      emit(
        'ArmAlignEmptyStr/$f',
        23,
        ArmAlign()
          ..lead = 21
          ..flag = f
          ..b = 1000
          ..tail = 99,
        writeArmAlign,
        measureArmAlign,
      );
    }
    for (final o in [false, true]) {
      for (final i in [false, true]) {
        emit(
          'ArmsNested/$o$i',
          o ? (i ? 40 : 16) : 23,
          ArmsNested()
            ..lead = 5
            ..outer = o
            ..inner = i
            ..x = 500000000
            ..y = 17
            ..z = 4000
            ..tail = 33,
          writeArmsNested,
          measureArmsNested,
        );
      }
    }
    for (final f in [false, true]) {
      for (var c = 0; c <= 3; c++) {
        final v = ArmArray()
          ..lead = 21
          ..flag = f
          ..itemsCount = c
          ..b = 300
          ..tail = 99;
        for (var i = 0; i < c; i++) {
          v.items[i] = 8191 - i * 777;
        }
        emit(
          'ArmArray/$f/$c',
          f ? 15 + 13 * c : 22,
          v,
          writeArmArray,
          measureArmArray,
        );
      }
    }
    // lead 5 + tag 2 + arm + tail 11 — the arms are 0, 3 and 37 bits
    final unevenArms = <int>[
      UnevenType.none,
      UnevenType.narrow,
      UnevenType.wide,
    ];
    final unevenBits = <int>[18, 21, 55];
    for (var k = 0; k < 3; k++) {
      final v = HoldsUneven()
        ..lead = 21
        ..tail = 1500;
      v.u.type = unevenArms[k];
      if (unevenArms[k] == UnevenType.narrow) v.u.narrow.n = 5;
      if (unevenArms[k] == UnevenType.wide) v.u.wide.w = 123456789012;
      emit(
        'HoldsUneven/$k',
        unevenBits[k],
        v,
        writeHoldsUneven,
        measureHoldsUneven,
      );
    }
    // alternating arms: item i is Narrow (2 + 3) when even, Wide (2 + 37)
    final unevenItemBits = <int>[0, 5, 44, 49];
    for (var c = 0; c <= 3; c++) {
      final v = ArrUneven()
        ..lead = 21
        ..tail = 5
        ..itemsCount = c;
      for (var i = 0; i < c; i++) {
        if (i % 2 == 0) {
          v.items[i].type = UnevenType.narrow;
          v.items[i].narrow.n = i % 8;
        } else {
          v.items[i].type = UnevenType.wide;
          v.items[i].wide.w = 99887766554 + i;
        }
      }
      emit(
        'ArrUneven/$c',
        10 + unevenItemBits[c],
        v,
        writeArrUneven,
        measureArrUneven,
      );
    }
    // lead 5 + count 2 + 13*c + s_length 3, an align to the byte, 8*s, then a
    // 32 + 29 + 19 + 4 static run after the align regains it
    for (var c = 0; c <= 3; c++) {
      for (final sl in [0, 4]) {
        final v = RegainAfterAlign()
          ..lead = 21
          ..itemsCount = c
          ..sLength = sl
          ..p = 0xdeadbeef
          ..q = (1 << 29) - 7
          ..r = (1 << 19) - 3
          ..tail = 9;
        if (sl != 0) v.s.setRange(0, 4, 'wxyz'.codeUnits);
        for (var i = 0; i < c; i++) {
          v.items[i] = 8191 - i * 999;
        }
        final afterAlign = ((5 + 2 + 13 * c + 3 + 7) >>> 3) * 8;
        emit(
          'Regain/$c/$sl',
          afterAlign + 8 * sl + 84,
          v,
          writeRegainAfterAlign,
          measureRegainAfterAlign,
        );
      }
    }

    {
      final joined = joinParts();
      final g = golden('joins');
      check(
        joined.length == g.length,
        'joins: wrote ${joined.length} bytes, golden has ${g.length}',
      );
      check(bytesEqual(joined, g), 'joins: Dart bytes == the C++-pinned bytes');

      var at = 0;
      bool back<T>(T out, int bits, bool Function(T, ByteData, int) read) {
        final bytes = (bits + 7) >>> 3;
        final slice = Uint8List(bytes + 8);
        slice.setRange(0, bytes, g, at);
        at += bytes;
        return read(out, ByteData.sublistView(slice), bits);
      }

      for (final f in [false, true]) {
        final agree = ArmsAgree();
        check(back(agree, 24, readArmsAgree), 'read ArmsAgree/$f');
        check(
          agree.lead == 21 && agree.flag == f && agree.tail == 99,
          'ArmsAgree/$f round-trips',
        );
        check(
          f
              ? (agree.a == 1234 && agree.b == 0)
              : (agree.b == 1500 && agree.a == 0),
          "ArmsAgree/$f's untaken side reads as zero (SPEC §5)",
        );

        final disagree = ArmsDisagree();
        check(
          back(disagree, f ? 24 : 16, readArmsDisagree),
          'read ArmsDisagree/$f',
        );
        check(
          disagree.lead == 21 && disagree.tail == 99,
          'ArmsDisagree/$f round-trips',
        );
        check(
          f
              ? (disagree.a == 1234 && disagree.b == 0)
              : (disagree.b == 5 && disagree.a == 0),
          "ArmsDisagree/$f's untaken side reads as zero",
        );

        final armEmpty = ArmEmpty();
        check(back(armEmpty, f ? 32 : 13, readArmEmpty), 'read ArmEmpty/$f');
        check(
          armEmpty.lead == 21 && armEmpty.tail == 99,
          'ArmEmpty/$f round-trips',
        );
        check(
          armEmpty.a == (f ? 456789 : 0),
          "ArmEmpty/$f's absent arm reads as zero",
        );

        final alignStr = ArmAlign();
        check(back(alignStr, f ? 55 : 23, readArmAlign), 'read ArmAlign/$f');
        check(
          alignStr.lead == 21 && alignStr.tail == 99,
          'ArmAlign/$f round-trips',
        );
        check(
          f
              ? (alignStr.sLength == 4 &&
                    alignStr.s[0] == 0x61 &&
                    alignStr.s[3] == 0x64 &&
                    alignStr.b == 0)
              : (alignStr.b == 1000 && alignStr.sLength == 0),
          "ArmAlign/$f's untaken side reads as zero",
        );

        final alignEmpty = ArmAlign();
        check(back(alignEmpty, 23, readArmAlign), 'read ArmAlignEmptyStr/$f');
        check(
          alignEmpty.lead == 21 && alignEmpty.tail == 99,
          'ArmAlignEmptyStr/$f round-trips',
        );
        check(
          f
              ? (alignEmpty.sLength == 0 && alignEmpty.b == 0)
              : (alignEmpty.b == 1000),
          "ArmAlign/$f's empty string round-trips",
        );
      }
      for (final o in [false, true]) {
        for (final i in [false, true]) {
          final r = ArmsNested();
          check(
            back(r, o ? (i ? 40 : 16) : 23, readArmsNested),
            'read ArmsNested/$o$i',
          );
          check(
            r.lead == 5 && r.tail == 33 && r.outer == o,
            'ArmsNested/$o$i round-trips',
          );
          if (o) {
            check(
              r.inner == i && r.z == 0,
              "ArmsNested/$o$i's outer arm round-trips",
            );
            check(
              i ? (r.x == 500000000 && r.y == 0) : (r.y == 17 && r.x == 0),
              "ArmsNested/$o$i's inner arm round-trips",
            );
          } else {
            check(
              r.z == 4000 && r.x == 0 && r.y == 0,
              "ArmsNested/$o$i's else arm round-trips",
            );
          }
        }
      }
      for (final f in [false, true]) {
        for (var c = 0; c <= 3; c++) {
          final r = ArmArray();
          check(
            back(r, f ? 15 + 13 * c : 22, readArmArray),
            'read ArmArray/$f/$c',
          );
          check(r.lead == 21 && r.tail == 99, 'ArmArray/$f/$c round-trips');
          if (f) {
            check(
              r.itemsCount == c && r.b == 0,
              "ArmArray/$f/$c's array arm round-trips",
            );
            for (var i = 0; i < c; i++) {
              check(
                r.items[i] == 8191 - i * 777,
                'ArmArray/$f/$c element round-trips',
              );
            }
          } else {
            check(
              r.b == 300 && r.itemsCount == 0,
              "ArmArray/$f/$c's scalar arm round-trips",
            );
          }
        }
      }
      for (var k = 0; k < 3; k++) {
        final r = HoldsUneven();
        check(back(r, unevenBits[k], readHoldsUneven), 'read HoldsUneven/$k');
        check(
          r.lead == 21 && r.tail == 1500 && r.u.type == unevenArms[k],
          'HoldsUneven/$k round-trips',
        );
        if (unevenArms[k] == UnevenType.narrow)
          check(r.u.narrow.n == 5, "HoldsUneven's narrow arm round-trips");
        if (unevenArms[k] == UnevenType.wide)
          check(
            r.u.wide.w == 123456789012,
            "HoldsUneven's wide arm round-trips",
          );
      }
      for (var c = 0; c <= 3; c++) {
        final r = ArrUneven();
        check(
          back(r, 10 + unevenItemBits[c], readArrUneven),
          'read ArrUneven/$c',
        );
        check(
          r.itemsCount == c && r.lead == 21 && r.tail == 5,
          'ArrUneven/$c round-trips',
        );
        for (var i = 0; i < c; i++) {
          if (i % 2 == 0) {
            check(
              r.items[i].type == UnevenType.narrow &&
                  r.items[i].narrow.n == i % 8,
              'ArrUneven narrow element round-trips',
            );
          } else {
            check(
              r.items[i].type == UnevenType.wide &&
                  r.items[i].wide.w == 99887766554 + i,
              'ArrUneven wide element round-trips',
            );
          }
        }
      }
      for (var c = 0; c <= 3; c++) {
        for (final sl in [0, 4]) {
          final r = RegainAfterAlign();
          final afterAlign = ((5 + 2 + 13 * c + 3 + 7) >>> 3) * 8;
          check(
            back(r, afterAlign + 8 * sl + 84, readRegainAfterAlign),
            'read Regain/$c/$sl',
          );
          check(
            r.lead == 21 && r.itemsCount == c && r.sLength == sl,
            'Regain/$c/$sl round-trips',
          );
          check(
            r.p == 0xdeadbeef &&
                r.q == (1 << 29) - 7 &&
                r.r == (1 << 19) - 3 &&
                r.tail == 9,
            "Regain/$c/$sl's static run after the align round-trips",
          );
          for (var i = 0; i < c; i++) {
            check(
              r.items[i] == 8191 - i * 999,
              'Regain/$c/$sl element round-trips',
            );
          }
        }
      }
      check(at == g.length, 'the joins reads consume the whole golden');
    }
  }

  if (failed) {
    exitCode = 1;
    return;
  }
  print('OK');
}
