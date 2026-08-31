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
import '../../generated/dart/Enums.dart';
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

    expectAssert(() {
      final bad = InputPacket();
      bad.inputsCount = 17; // above [0, MaxInputsPerPacket]
      writeInputPacket(bad, writeView);
    }, 'an out-of-range array count must trip the writer contract');

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

  if (failed) {
    exitCode = 1;
    return;
  }
  print('OK');
}
