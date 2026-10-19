// THE RUNTIME SIDE OF THE DART VM / dart2js DIFFERENTIAL.
//
// This driver runs over a WEB-SAFE build of the generated corpus: every
// schema-internal 64-bit literal in a scratch copy of the generated tree is
// folded to its low 31 bits, so dart2js can accept the tree. The SAME scratch
// copy is run on the Dart VM and compiled by dart2js and run under node; the
// target diffs the two transcripts. A schema-internal value above 2^53 is not
// representable in a web int and is refused by the compile arm instead (see
// the make file).
//
// THE RUNTIME VALUES ARE THE LENS. Every u64, the counts and the signed int64
// fields carry values in 2^32..2^53 (and negatives of them): exact in a JS
// double, but past the 32-bit bitwise operators dart2js uses. The codec must
// carry them identically on both runtimes; the old putU64/readU64 shrank
// 4294967301 (2^32+5) to 5 and _signExtend turned every negative int32 into
// zero under dart2js. The VM-only reference (vmref.dart) covers the >2^53
// values the compile arm refuses.
import 'dart:typed_data';

import '../../build/schema-dart-vm-dart2js-differential/gen-websafe/examples/TabledemoWire.dart';
import '../../build/schema-dart-vm-dart2js-differential/gen-websafe/a2/Tbla2Wire.dart'
    as a2;

String hex(List<int> bytes) =>
    bytes.map((b) => b.toRadixString(16).padLeft(2, '0')).join();

void main() {
  final u = RangedUnsigned();
  u.u8Span = 255;
  u.u8Low = 254;
  u.u16Span = 65535;
  u.u16Low = 65534;
  u.u32Span = 4294967295;
  u.u32Low = 4294967294;
  u.u64Span = 9007199254740991;
  u.u64Low = 4294967301;
  u.u64High = 4503599627370496;
  u.u64Inside = 8589934593;
  u.counts = <int>[0, 1, 4294967301, 9007199254740991];

  final s = RangedSigned();
  s.i8Span = -128;
  s.i8Low = -127;
  s.i16Span = -32768;
  s.i16Low = -32767;
  s.i32Span = -2147483648;
  s.i32Low = -2147483647;
  s.i64Span = 9007199254740991;
  s.i64Low = -9007199254740991;
  s.i64High = -4294967301;
  s.i64Inside = 4294967301;

  final w = RangedWidths();
  w.b8 = 255;
  w.b16 = 65535;
  w.b32 = 4294967295;
  w.b64 = 9007199254740991;
  w.b12 = 4095;
  w.b48 = 281474976710655;

  final uwire = rangedUnsignedWireSave(u);
  final swire = rangedSignedWireSave(s);
  final wwire = rangedWidthsWireSave(w);

  final urep = TableWireReport();
  final srep = TableWireReport();
  final wrep = TableWireReport();
  final ub = rangedUnsignedWireLoad(uwire, urep);
  final sb = rangedSignedWireLoad(swire, srep);
  final wb = rangedWidthsWireLoad(wwire, wrep);

  print('u8=${ub.u8Span},${ub.u8Low},${ub.u8High},${ub.u8Inside}');
  print('u16=${ub.u16Span},${ub.u16Low},${ub.u16High},${ub.u16Inside}');
  print('u32=${ub.u32Span},${ub.u32Low},${ub.u32High},${ub.u32Inside}');
  print('u64=${ub.u64Span},${ub.u64Low},${ub.u64High},${ub.u64Inside}');
  print('counts=${ub.counts.join(",")}');
  print('i8=${sb.i8Span},${sb.i8Low},${sb.i8High},${sb.i8Inside}');
  print('i16=${sb.i16Span},${sb.i16Low},${sb.i16High},${sb.i16Inside}');
  print('i32=${sb.i32Span},${sb.i32Low},${sb.i32High},${sb.i32Inside}');
  print('i64=${sb.i64Span},${sb.i64Low},${sb.i64High},${sb.i64Inside}');
  print('widths=${wb.b8},${wb.b16},${wb.b32},${wb.b64},${wb.b12},${wb.b48}');
  print('ureport=clamped:${urep.clamped},malformed:${urep.malformed},kindMismatch:${urep.kindMismatch},widened:${urep.widened}');
  print('sreport=clamped:${srep.clamped},malformed:${srep.malformed},kindMismatch:${srep.kindMismatch},widened:${srep.widened}');
  print('wreport=clamped:${wrep.clamped},malformed:${wrep.malformed},kindMismatch:${wrep.kindMismatch},widened:${wrep.widened}');
  print('uwire=${hex(uwire)}');
  print('swire=${hex(swire)}');
  print('wwire=${hex(wwire)}');

  // THE INT64 LANE BOUNDS. The web-safe copy folds a declared range bound to
  // +-(2^53-1), not to its low 31 bits, so the u64/int64 values in 2^32..2^53
  // assigned above are inside their declared range and must read back exactly.
  // The transcript says so, and the make target requires this line to be
  // `ok`, so a fold that clamps the lane cannot pass by agreeing with itself.
  final bad = <String>[];
  void same(String name, int want, int got) {
    if (want != got) bad.add('$name=$got want $want');
  }

  same('u64Span', 9007199254740991, ub.u64Span);
  same('u64Low', 4294967301, ub.u64Low);
  same('u64High', 4503599627370496, ub.u64High);
  same('u64Inside', 8589934593, ub.u64Inside);
  same('count[2]', 4294967301, ub.counts[2]);
  same('count[3]', 9007199254740991, ub.counts[3]);
  same('i64Span', 9007199254740991, sb.i64Span);
  same('i64Low', -9007199254740991, sb.i64Low);
  same('i64High', -4294967301, sb.i64High);
  same('i64Inside', 4294967301, sb.i64Inside);
  same('b64', 9007199254740991, wb.b64);
  same('uClamped', 0, urep.clamped);
  same('sClamped', 0, srep.clamped);
  same('wClamped', 0, wrep.clamped);
  // THE UNION INT64 ARM (schema-dart-u64-codec-exact-on-web). test/tables/
  // A2.schema's union Value has `a int64`; its read is the integer arm that
  // wireunion.go emits. At BASE that arm assembled bytes 4..7 with shifts of
  // 32..56, which dart2js computes with its 32-bit operator: 2^32+5 read back
  // as 5 on the web while the VM read 2^32+5.
  for (final want in <int>[4294967301, 1099511627779, 9007199254740991, -4294967301]) {
    final root = a2.Root()..id = 7..tail = 9;
    root.value.armTag = 1;
    root.value.a = want;
    final wire = a2.rootWireSave(root);
    final rep = a2.TableWireReport();
    final back = a2.rootWireLoad(wire, rep);
    print('union[$want]=${back.value.armTag},${back.value.a},refused:${rep.refused},malformed:${rep.malformed} wire=${hex(wire)}');
    same('union[$want]', want, back.value.a);
    same('union[$want].tail', 9, back.tail);
  }

  // THE WEB REFUSAL. The same wire with the arm's eight bytes replaced by
  // 2^62+5, a pattern no web int holds: the VM must read it exactly and the
  // web must refuse it by name (report.refused, the arm left at its default),
  // never round it. The line is the same on both runtimes only when each
  // keeps its half of the rule.
  {
    final root = a2.Root()..id = 7..tail = 9;
    root.value.armTag = 1;
    root.value.a = 9007199254740991;
    final wire = a2.rootWireSave(root);
    final idx = hex(wire).indexOf('ffffffffffff1f00');
    final at = idx >= 0 && idx.isEven ? idx ~/ 2 : 0;
    final high = <int>[5, 0, 0, 0, 0, 0, 0, 0x40];
    for (var i = 0; i < 8; i++) {
      wire[at + i] = high[i];
    }
    final rep = a2.TableWireReport();
    final back = a2.rootWireLoad(wire, rep);
    final web = identical(0, 0.0);
    final kept = web
        ? rep.refused && back.value.a == 0 && !rep.malformed
        : !rep.refused && back.value.a == int.parse('4611686018427387909');
    print('unionRefused=${at > 0 && kept ? 'ok' : 'BAD(at=$at,refused=${rep.refused},a=${back.value.a})'}');
    same('unionRefusedTail', 9, back.tail);
  }

  // THE LEB128 LENGTH (readLeb). The arm's payload length re-encoded as the
  // five-byte LEB of 2^32+8: no buffer holds that, so both runtimes must call
  // the arm malformed. At BASE readLeb shifted the fifth group by 28, which
  // dart2js computes with its 32-bit operator, so the web read a length of 8,
  // took the payload and loaded the arm the VM refused.
  {
    final root = a2.Root()..id = 7..tail = 9;
    root.value.armTag = 1;
    root.value.a = 9007199254740991;
    final h = hex(a2.rootWireSave(root));
    final idx = h.indexOf('0508ffffffffffff1f00');
    final at = idx >= 0 && idx.isEven ? idx ~/ 2 + 1 : 0;
    if (at == 0) bad.add('lebHigh found no arm length to re-encode');
    final bytes = <int>[];
    for (var i = 0; i < h.length; i += 2) {
      bytes.add(int.parse(h.substring(i, i + 2), radix: 16));
    }
    if (at > 0) bytes.replaceRange(at, at + 1, <int>[0x88, 0x80, 0x80, 0x80, 0x10]);
    final rep = a2.TableWireReport();
    final back = a2.rootWireLoad(Uint8List.fromList(bytes), rep);
    print('lebHigh=at:$at,arm:${back.value.armTag},a:${back.value.a},malformed:${rep.malformed}');
    if (!rep.malformed || back.value.a != 0) bad.add('lebHigh read a length of 2^32+8 as a smaller one');
  }

  print('roundtrip=${bad.isEmpty ? 'ok' : 'BAD:${bad.join(";")}'}');

  // THE PLANT: only the negative control sets DART_DIFF_PLANT=1. It is a
  // run-time value above 2^53, so the VM prints 2^53+3 exactly and dart2js
  // prints the rounded 2^53+2. The target must see the two transcripts differ
  // and name the plant; nothing sets this outside the control.
  if (const bool.fromEnvironment('DART_DIFF_PLANT')) {
    print('plant=${int.parse('9007199254740993') + 2}');
  }
}
