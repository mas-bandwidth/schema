// The packet Write of a bare intN / uintN outside its declared width (SPEC
// §5.1 row 11), the Dart leg.
//
// Reads the shared cases beside the schema and writes every case a Dart
// field can hold. A refused case must fire the writer's debug assert (the
// checked twin); a written case must write and read back exactly in both
// twins. The release twin trusts the caller by design: it writes the same
// refused values and does not require the assert.
//
// Every generated field here is a 64-bit Dart int, and a uint64 is held as
// its two's complement bit pattern. A case whose value no Dart int of its
// field stands for is not run: it must be named in notRepresentable below,
// with why, and is printed by name. A case that is neither run nor named
// there is a failure, and so is a named case a Dart int can hold.
//
// Prints the counts and exits 0; every failure is one FAILED line, then
// exit 1.
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

// The shared cases no Dart int of their field stands for, by name, with
// why. These are not run; each is printed by name, and the driver fails if
// one of them turns out holdable or a case is neither run nor listed here.
const notRepresentable = <String, String>{
  'int64-above-max': '2^63 is above the largest Dart int (2^63 - 1), and an int64 field is a Dart int',
  'uint64-above-max': '2^64 needs 65 bits, and a uint64 field is a 64-bit Dart int holding its bit pattern',
  'uint64-negative': 'a uint64 field holds a 64-bit pattern, and the pattern of -1 is uint64-max, a lawful value',
};

final BigInt _two64 = BigInt.one << 64;

// The Dart int that stands for value in a field of this leg, or null when
// none does. A uint64 is its bit pattern, so only 0 ..= 2^64 - 1 has one;
// every other field holds the value itself, so it needs a Dart int's range.
int? storage(Leg leg, BigInt value) {
  if (!leg.signed && leg.width == 64) {
    if (value.isNegative || value >= _two64) {
      return null;
    }
    return value.toSigned(64).toInt();
  }
  return value.isValidInt ? value.toInt() : null;
}

void main(List<String> args) {
  final lines = File(args[0]).readAsLinesSync();
  var total = 0;
  var ran = 0;
  var refusedRan = 0;
  var unrepresentable = 0;
  final seen = <String>{};
  for (final line in lines) {
    final text = line.trim();
    if (text.isEmpty || text.startsWith('#')) {
      continue;
    }
    final fields = text.split(RegExp(r'\s+'));
    final name = fields[0];
    total++;
    seen.add(name);
    final leg = legs[fields[1]];
    if (leg == null) {
      check(false, 'packet-write-bareint $name: unknown type ${fields[1]}');
      continue;
    }
    final refused = fields[3] == 'refused';
    // a non-integer value is excluded by the field's Dart type (int) at
    // compile time; it is held to the same named list as a wide one
    final value = BigInt.tryParse(fields[2]);
    final stored = value == null ? null : storage(leg, value);
    final why = notRepresentable[name];

    if (stored == null) {
      if (why == null) {
        check(
          false,
          'packet-write-bareint $name: neither run nor named not representable in Dart',
        );
      } else {
        print('not representable in Dart: $name: $why');
        unrepresentable++;
      }
      continue;
    }
    if (why != null) {
      check(
        false,
        'packet-write-bareint $name: named not representable, but a Dart int holds it',
      );
    }

    final view = ByteData(8);
    var fired = false;
    var written = 0;
    try {
      written = leg.write(stored, view);
    } on AssertionError {
      fired = true;
    }
    ran++;

    if (refused) {
      refusedRan++;
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

  for (final name in notRepresentable.keys) {
    if (!seen.contains(name)) {
      check(
        false,
        'packet-write-bareint $name: named not representable, but not a shared case',
      );
    }
  }
  if (total == 0) {
    check(false, 'packet-write-bareint: no cases read');
  }
  check(
    ran + unrepresentable == total,
    'packet-write-bareint: ran $ran and not representable $unrepresentable '
    'do not add up to the $total shared cases',
  );
  if (failed) {
    exit(1);
  }
  final refusedVerdict = assertsEnabled
      ? 'each fired the assert'
      : 'written unchecked: the release twin trusts the caller';
  print(
    'packet-write-bareint Dart: ran $ran ($refusedRan refused, '
    '$refusedVerdict), not representable in Dart $unrepresentable, '
    'of $total shared cases',
  );
}
