// The packet Write of a bare intN / uintN outside its declared width (SPEC
// §5.1 row 11), the Dart leg.
//
// Reads the shared cases beside the schema and writes every one of them. A
// refused case must fire the writer's debug assert (the checked twin); a
// written case must write and read back exactly in both twins. The release
// twin trusts the caller by design, so it does not require the assert.
//
// A Dart int is 64 bits, and a uint64 is carried as its two's complement
// bit pattern, so a 64-bit width is the storage itself: no Dart value of an
// I64 or a U64 lies outside its width, and a refused 64-bit case has no
// value to write. Every narrower width is held in that 64-bit int, so each
// of its refused cases is written.
//
// Prints OK and exits 0; every failure is one FAILED line, then exit 1.
import 'dart:io';
import 'dart:typed_data';

import '../../../build/packet-write-bareint/dart/BareInt.dart';

var failed = false;

void check(bool ok, String what) {
  if (!ok) {
    print('FAILED: $what');
    failed = true;
  }
}

final bool assertsEnabled = () {
  var on = false;
  assert(on = true);
  return on;
}();

// One generated type: its width, its signedness, and a write and a read
// round trip through the generated functions.
final class Leg {
  Leg(this.width, this.signed, this.write, this.read);
  final int width;
  final bool signed;
  final int Function(int value, ByteData view) write;
  final int? Function(ByteData view, int numBits) read;
}

final legs = <String, Leg>{
  'I8': Leg(8, true, (v, view) => writeI8(I8()..value = v, view), (view, n) {
    final back = I8();
    return readI8(back, view, n) ? back.value : null;
  }),
  'I16': Leg(16, true, (v, view) => writeI16(I16()..value = v, view), (
    view,
    n,
  ) {
    final back = I16();
    return readI16(back, view, n) ? back.value : null;
  }),
  'I32': Leg(32, true, (v, view) => writeI32(I32()..value = v, view), (
    view,
    n,
  ) {
    final back = I32();
    return readI32(back, view, n) ? back.value : null;
  }),
  'I64': Leg(64, true, (v, view) => writeI64(I64()..value = v, view), (
    view,
    n,
  ) {
    final back = I64();
    return readI64(back, view, n) ? back.value : null;
  }),
  'U8': Leg(8, false, (v, view) => writeU8(U8()..value = v, view), (view, n) {
    final back = U8();
    return readU8(back, view, n) ? back.value : null;
  }),
  'U16': Leg(16, false, (v, view) => writeU16(U16()..value = v, view), (
    view,
    n,
  ) {
    final back = U16();
    return readU16(back, view, n) ? back.value : null;
  }),
  'U32': Leg(32, false, (v, view) => writeU32(U32()..value = v, view), (
    view,
    n,
  ) {
    final back = U32();
    return readU32(back, view, n) ? back.value : null;
  }),
  'U64': Leg(64, false, (v, view) => writeU64(U64()..value = v, view), (
    view,
    n,
  ) {
    final back = U64();
    return readU64(back, view, n) ? back.value : null;
  }),
};

void main(List<String> args) {
  final lines = File(args[0]).readAsLinesSync();
  var cases = 0;
  for (final line in lines) {
    final text = line.trim();
    if (text.isEmpty || text.startsWith('#')) {
      continue;
    }
    final fields = text.split(RegExp(r'\s+'));
    final name = fields[0];
    final leg = legs[fields[1]];
    if (leg == null) {
      check(false, 'packet-write-bareint $name: unknown type ${fields[1]}');
      continue;
    }
    final value = BigInt.parse(fields[2]);
    final refused = fields[3] == 'refused';
    cases++;

    if (refused && leg.width == 64) {
      // the storage is the width: no Dart int holds this value apart from a
      // lawful one, so there is nothing to write
      continue;
    }
    // the Dart storage: the value itself, or a uint64's bit pattern
    final stored = value.toSigned(64).toInt();
    final view = ByteData(8);

    var fired = false;
    var written = 0;
    try {
      written = leg.write(stored, view);
    } on AssertionError {
      fired = true;
    }

    if (refused) {
      if (assertsEnabled) {
        check(
          fired,
          'packet-write-bareint $name: out-of-width value was written',
        );
      }
      continue;
    }

    if (fired) {
      check(false, 'packet-write-bareint $name: lawful value was refused');
      continue;
    }
    final back = leg.read(view, written * 8);
    check(
      back != null,
      'packet-write-bareint $name: the writer bytes read back',
    );
    if (back != null) {
      check(back == stored, 'packet-write-bareint $name: read back $back');
    }
  }

  if (cases == 0) {
    check(false, 'packet-write-bareint: no cases read');
  }
  if (failed) {
    exit(1);
  }
  print('packet-write-bareint Dart: every shared case has its verdict');
}
