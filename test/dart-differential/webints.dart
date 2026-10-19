// THE RUNTIME SIDE OF THE DART VM / dart2js DIFFERENTIAL.
//
// This driver runs over a WEB-SAFE build of the generated corpus: every
// schema-internal 64-bit literal in a scratch copy of the generated tree is
// folded to its low 31 bits, so dart2js can represent it and every bitwise
// operator the codec uses is exact under both runtimes. The SAME scratch copy
// is run on the Dart VM and compiled by dart2js and run under node; the target
// diffs the two transcripts. A value above 2^53 is not representable in this
// build and is refused by the compile arm instead (see the make file).
//
// Nothing here is above 2^31: a value the JS port can hold exactly and shift
// without 32-bit operator wraparound is the only thing this lane can compare.
// The VM-only reference (vmref.dart) covers the exact >2^53 values.
import '../../build/schema-dart-vm-dart2js-differential/gen-websafe/examples/TabledemoWire.dart';

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
  u.u64Span = 4294967295;
  u.u64Low = 4294967294;
  u.u64High = 1;
  u.u64Inside = 2147483648;
  u.counts = <int>[0, 1, 4294967295, 2147483648];

  final w = RangedWidths();
  w.b8 = 255;
  w.b16 = 65535;
  w.b32 = 4294967295;
  w.b64 = 4294967295;
  w.b12 = 4095;
  w.b48 = 4294967295;

  final uwire = rangedUnsignedWireSave(u);
  final wwire = rangedWidthsWireSave(w);

  final ub = rangedUnsignedWireLoad(uwire);
  final wb = rangedWidthsWireLoad(wwire);

  print('u8=${ub.u8Span},${ub.u8Low},${ub.u8High},${ub.u8Inside}');
  print('u16=${ub.u16Span},${ub.u16Low},${ub.u16High},${ub.u16Inside}');
  print('u32=${ub.u32Span},${ub.u32Low},${ub.u32High},${ub.u32Inside}');
  print('u64=${ub.u64Span},${ub.u64Low},${ub.u64High},${ub.u64Inside}');
  print('counts=${ub.counts.join(",")}');
  print('widths=${wb.b8},${wb.b16},${wb.b32},${wb.b64},${wb.b12},${wb.b48}');
  print('uwire=${hex(uwire)}');
  print('wwire=${hex(wwire)}');

  // THE PLANT: only the negative control sets DART_DIFF_PLANT=1. It is a
  // run-time value above 2^53, so the VM prints 2^53+3 exactly and dart2js
  // prints the rounded 2^53+2. The target must see the two transcripts differ
  // and name the plant; nothing sets this outside the control.
  if (const bool.fromEnvironment('DART_DIFF_PLANT')) {
    print('plant=${int.parse('9007199254740993') + 2}');
  }
}
