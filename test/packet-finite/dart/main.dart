// Packet floats are finite (SPEC §4.3, serialize format 2.0), the Dart leg.
//
// The writer refuses a NaN (any sign, any payload) and both infinities at
// every float shape a packet carries: the bare float32 and float64 fields, an
// element of a fixed float32 array and of a counted float64 array, and the
// compressed float32. Write contracts are asserts, so the write half runs in
// the checked twin only (dart --enable-asserts); the release twin trusts by
// design. The reader refuses an all-ones exponent on the raw bits as
// non_finite_float, from crafted bytes, at every one of those slots, in both
// twins. -0 and the subnormals ride bit exact both ways.
//
// Prints OK and exits 0; every failure is one FAILED line, then exit 1.
import 'dart:io';
import 'dart:typed_data';

import '../../../build/packet-finite/dart/Finite.dart';

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

final ByteData _scratch = ByteData(8);

double f32FromBits(int bits) {
  _scratch.setUint32(0, bits, Endian.little);
  return _scratch.getFloat32(0, Endian.little);
}

int f32Bits(double value) {
  _scratch.setFloat32(0, value, Endian.little);
  return _scratch.getUint32(0, Endian.little);
}

double f64FromBits(int bits) {
  _scratch.setUint64(0, bits, Endian.little);
  return _scratch.getFloat64(0, Endian.little);
}

int f64Bits(double value) {
  _scratch.setFloat64(0, value, Endian.little);
  return _scratch.getUint64(0, Endian.little);
}

String hex(int bits) => '0x${bits.toUnsigned(64).toRadixString(16)}';

// The wire layout of Floats, LSB-first bit offsets (SPEC §4.3): narrow at 0,
// wide at 32 (low dword first), narrows[0..2] at 96/128/160, the count of
// wides in 3 bits at 192, wides[i] at 195 + 64 i, then the quantized step
// index in 11 bits. With four wides that is 462 bits.
const int narrowAt = 0;
const int wideAt = 32;
int narrowsAt(int i) => 96 + 32 * i;
int widesAt(int i) => 195 + 64 * i;
const int fullBits = 462;

int getBits(Uint8List bytes, int offset, int bits) {
  var value = 0;
  for (var i = 0; i < bits; i++) {
    final at = offset + i;
    value |= ((bytes[at >>> 3] >>> (at & 7)) & 1) << i;
  }
  return value;
}

void putBits(Uint8List bytes, int offset, int bits, int value) {
  for (var i = 0; i < bits; i++) {
    final at = offset + i;
    final mask = 1 << (at & 7);
    if ((value >>> i) & 1 != 0) {
      bytes[at >>> 3] |= mask;
    } else {
      bytes[at >>> 3] &= ~mask & 0xff;
    }
  }
}

Floats baseline() {
  final value = Floats();
  value.narrow = 1.5;
  value.wide = -2.25;
  value.narrows[0] = 0.5;
  value.narrows[1] = -0.75;
  value.narrows[2] = 3.0;
  value.widesCount = 4;
  value.wides[0] = 1.0;
  value.wides[1] = 2.0;
  value.wides[2] = -4.0;
  value.wides[3] = 8.0;
  value.quantized = 1.25;
  return value;
}

Uint8List write(Floats value) {
  final buffer = ByteData(floatsMaxBytes);
  final n = writeFloats(value, buffer);
  return buffer.buffer.asUint8List(0, n);
}

bool read(Uint8List bytes, Floats into) {
  final view = ByteData(floatsMaxBytes);
  view.buffer.asUint8List().setRange(0, bytes.length, bytes);
  return readFloats(into, view, fullBits);
}

// THE WRITE HALF: every non-finite value at every slot fires the contract.
final List<(String, double)> nonFinite = [
  ('NaN', double.nan),
  ('-NaN', -double.nan),
  ('payload NaN', f64FromBits(0x7ff0000000000123)),
  ('signalling NaN', f64FromBits(0x7ff0000000000001)),
  ('-payload NaN', f64FromBits(0xfff8000000abcdef)),
  ('+inf', double.infinity),
  ('-inf', double.negativeInfinity),
];

final List<(String, void Function(Floats, double))> writeSlots = [
  ('narrow (float32 field)', (v, x) => v.narrow = x),
  ('wide (float64 field)', (v, x) => v.wide = x),
  ('narrows[1] (float32 array element)', (v, x) => v.narrows[1] = x),
  ('wides[2] (float64 array element)', (v, x) => v.wides[2] = x),
  ('quantized (compressed float32)', (v, x) => v.quantized = x),
];

void writeRefusals() {
  if (!assertsEnabled) {
    return; // the release writer trusts by design
  }
  for (final (slot, set) in writeSlots) {
    for (final (name, x) in nonFinite) {
      final value = baseline();
      set(value, x);
      var fired = false;
      try {
        write(value);
      } on AssertionError {
        fired = true;
      }
      check(fired, 'non_finite_float write refusal: $name at $slot');
    }
  }
  // a double past float32's range rounds to an infinity: not a float32 value
  final value = baseline();
  value.narrow = 1e300;
  var fired = false;
  try {
    write(value);
  } on AssertionError {
    fired = true;
  }
  check(fired, 'non_finite_float write refusal: 1e300 at narrow (float32)');
}

// THE READ HALF: crafted bytes carrying an all-ones exponent are refused.
const List<int> f32Patterns = [
  0x7f800000, // +inf
  0xff800000, // -inf
  0x7fc00000, // quiet NaN
  0xffc00000, // -quiet NaN
  0x7f800001, // signalling NaN, lowest payload
  0x7fffffff, // NaN, every payload bit
  0xff812345, // -NaN, a payload
];

const List<int> f64Patterns = [
  0x7ff0000000000000, // +inf
  0xfff0000000000000, // -inf
  0x7ff8000000000000, // quiet NaN
  0xfff8000000000000, // -quiet NaN
  0x7ff0000000000001, // signalling NaN, lowest payload
  0x7fffffffffffffff, // NaN, every payload bit
  0xfff00000deadbeef, // -NaN, a payload
];

void readRefusals() {
  final clean = write(baseline());
  check(clean.length * 8 >= fullBits, 'baseline wire is $fullBits bits');
  check(read(clean, Floats()), 'baseline wire reads');
  final f32Slots = <(String, int)>[
    ('narrow (float32 field)', narrowAt),
    for (var i = 0; i < 3; i++)
      ('narrows[$i] (float32 array element)', narrowsAt(i)),
  ];
  final f64Slots = <(String, int)>[
    ('wide (float64 field)', wideAt),
    for (var i = 0; i < 4; i++)
      ('wides[$i] (float64 array element)', widesAt(i)),
  ];
  for (final (slot, at) in f32Slots) {
    for (final bits in f32Patterns) {
      final bytes = Uint8List.fromList(clean);
      putBits(bytes, at, 32, bits);
      check(
        !read(bytes, Floats()),
        'non_finite_float read refusal: ${hex(bits)} at $slot',
      );
    }
    // the largest finite float32 beside them is a value, read exactly
    final bytes = Uint8List.fromList(clean);
    putBits(bytes, at, 32, 0x7f7fffff);
    final into = Floats();
    check(read(bytes, into), 'finite float32 0x7f7fffff at $slot reads');
  }
  for (final (slot, at) in f64Slots) {
    for (final bits in f64Patterns) {
      final bytes = Uint8List.fromList(clean);
      putBits(bytes, at, 64, bits);
      check(
        !read(bytes, Floats()),
        'non_finite_float read refusal: ${hex(bits)} at $slot',
      );
    }
    final bytes = Uint8List.fromList(clean);
    putBits(bytes, at, 64, 0x7fefffffffffffff);
    check(
      read(bytes, Floats()),
      'finite float64 0x7fefffffffffffff at $slot reads',
    );
  }
}

// -0 AND THE SUBNORMALS: bit exact on the wire and back.
void bitExact() {
  final cases = <(String, int, int)>[
    ('-0', 0x80000000, 0x8000000000000000),
    ('smallest subnormal', 0x00000001, 0x0000000000000001),
    ('-largest subnormal', 0x807fffff, 0x800fffffffffffff),
  ];
  for (final (name, b32, b64) in cases) {
    final value = baseline();
    value.narrow = f32FromBits(b32);
    value.narrows[2] = f32FromBits(b32);
    value.wide = f64FromBits(b64);
    value.wides[1] = f64FromBits(b64);
    final bytes = write(value);
    check(getBits(bytes, narrowAt, 32) == b32, '$name narrow wire bits');
    check(
      getBits(bytes, narrowsAt(2), 32) == b32,
      '$name narrows[2] wire bits',
    );
    check(getBits(bytes, wideAt, 64) == b64, '$name wide wire bits');
    check(getBits(bytes, widesAt(1), 64) == b64, '$name wides[1] wire bits');
    final back = Floats();
    check(read(bytes, back), '$name reads');
    check(f32Bits(back.narrow) == b32, '$name narrow read bits');
    check(f32Bits(back.narrows[2]) == b32, '$name narrows[2] read bits');
    check(f64Bits(back.wide) == b64, '$name wide read bits');
    check(f64Bits(back.wides[1]) == b64, '$name wides[1] read bits');
  }
}

void main() {
  writeRefusals();
  readRefusals();
  bitExact();
  if (failed) {
    exit(1);
  }
  print('OK (asserts ${assertsEnabled ? 'on' : 'off'})');
}
