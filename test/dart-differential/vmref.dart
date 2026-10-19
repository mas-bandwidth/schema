// THE VM REFERENCE SIDE OF THE DART VM / dart2js DIFFERENTIAL.
//
// The generated tree is compiled for the Dart VM exactly as emitted, and this
// driver carries the values dart2js cannot represent (above 2^53) through the
// generated writer and reader. The VM must reproduce every one exactly. That
// is the reference the web-safe lane's transcript is held against, and it is
// why the differential fails when the VM stops carrying 64-bit ints exactly.
import 'dart:io';

import '../../build/schema-dart-vm-dart2js-differential/gen/examples/TabledemoWire.dart';

void main() {
  final unsigned = RangedUnsigned();
  unsigned.u64Span = int.parse('9007199254740993'); // 2^53 + 1
  unsigned.u64High = int.parse('9007199254740993');
  unsigned.u64Inside = int.parse('9223372036854775807'); // int64 max
  unsigned.counts = <int>[
    -1, // uint64 max, the 64-bit two's-complement spelling
    -2, // uint64 max - 1
  ];

  final signed = RangedSigned();
  signed.i64High = int.parse('9223372036854775807');
  signed.i64Span = int.parse('-9223372036854775808');
  signed.edges = <int>[-32768, 32767];

  final uback = rangedUnsignedWireLoad(rangedUnsignedWireSave(unsigned));
  final sback = rangedSignedWireLoad(rangedSignedWireSave(signed));

  final want = <String, int>{
    'u64span': unsigned.u64Span,
    'u64high': unsigned.u64High,
    'u64inside': unsigned.u64Inside,
    'count0': unsigned.counts[0],
    'count1': unsigned.counts[1],
    'i64high': signed.i64High,
    'i64span': signed.i64Span,
    'edge0': signed.edges[0],
    'edge1': signed.edges[1],
  };
  final got = <String, int>{
    'u64span': uback.u64Span,
    'u64high': uback.u64High,
    'u64inside': uback.u64Inside,
    'count0': uback.counts[0],
    'count1': uback.counts[1],
    'i64high': sback.i64High,
    'i64span': sback.i64Span,
    'edge0': sback.edges[0],
    'edge1': sback.edges[1],
  };

  var bad = 0;
  for (final key in want.keys) {
    final w = BigInt.from(want[key]!).toUnsigned(64);
    final g = BigInt.from(got[key]!).toUnsigned(64);
    print('$key=$g');
    if (w != g) {
      stderr.writeln('VM REFERENCE WRONG: $key=$g want $w');
      bad++;
    }
  }
  if (bad != 0) {
    exit(1);
  }
}
