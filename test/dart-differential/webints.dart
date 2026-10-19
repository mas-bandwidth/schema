// THE VM SIDE OF THE DART VM / dart2js DIFFERENTIAL.
//
// The same generated codec source is run twice: once on the Dart VM, whose int
// is an exact 64-bit integer, and once compiled by dart2js and run under node,
// whose int is a 53-bit double with 32-bit bitwise operators. Every line this
// program prints is a value the generated reader or writer produced, printed
// through BigInt so the VM's exact answer is visible.
//
// The values above 2^53 are built with int.parse, not literals: dart2js
// refuses to compile an unrepresentable int literal, and the point here is the
// value that reaches the generated code at run time.
import 'dart:io';

import '../../build/schema-dart-vm-dart2js-differential/gen/examples/TabledemoWire.dart';

String hex(List<int> bytes) =>
    bytes.map((b) => b.toRadixString(16).padLeft(2, '0')).join();

void main() {
  final unsigned = RangedUnsigned();
  unsigned.u64Span = int.parse('9007199254740993'); // 2^53 + 1
  unsigned.u64High = int.parse('9007199254740993');

  final signed = RangedSigned();
  signed.i64High = int.parse('9223372036854775807'); // int64 max

  final uwire = rangedUnsignedWireSave(unsigned);
  final swire = rangedSignedWireSave(signed);

  final uback = rangedUnsignedWireLoad(uwire);
  final sback = rangedSignedWireLoad(swire);

  print('u64span=${BigInt.from(unsigned.u64Span).toUnsigned(64)}');
  print('u64high=${BigInt.from(unsigned.u64High).toUnsigned(64)}');
  print('i64high=${BigInt.from(signed.i64High)}');
  print('u64span_wire=${hex(uwire)}');
  print('i64high_wire=${hex(swire)}');
  print('u64span_read=${BigInt.from(uback.u64Span).toUnsigned(64)}');
  print('i64high_read=${BigInt.from(sback.i64High)}');

  // The VM is the reference: these are the values the codec must carry.
  final want = [
    'u64span=9007199254740993',
    'u64high=9007199254740993',
    'i64high=9223372036854775807',
    'u64span_read=9007199254740993',
    'i64high_read=9223372036854775807',
  ];
  final got = [
    'u64span=${BigInt.from(unsigned.u64Span).toUnsigned(64)}',
    'u64high=${BigInt.from(unsigned.u64High).toUnsigned(64)}',
    'i64high=${BigInt.from(signed.i64High)}',
    'u64span_read=${BigInt.from(uback.u64Span).toUnsigned(64)}',
    'i64high_read=${BigInt.from(sback.i64High)}',
  ];
  for (var i = 0; i < want.length; i++) {
    if (got[i] != want[i]) {
      stderr.writeln('VM REFERENCE WRONG: ${got[i]} want ${want[i]}');
      exit(1);
    }
  }
}
