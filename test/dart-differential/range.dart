// THE ABOVE-2^53 RUNTIME LANE OF THE DART VM / dart2js DIFFERENTIAL.
//
// webints.dart settles the values a JavaScript double carries exactly (up to
// 2^53-1). This driver is the other half: a 64-bit INTEGER outside +-(2^53-1)
// cannot be represented by a dart2js int, so the generated codec must REFUSE
// it by name instead of silently writing or returning the rounded value. The
// VM's int is exact to 2^63 and keeps carrying the same values.
//
// The target runs this on the VM and through dart2js under node, and holds
// each runtime to its specified side: the VM prints the exact values, the web
// prints the refusal named `TableWire: 64-bit value is outside the exact web
// int range (2^53)`. The table writer/reader below is the generated
// RangedUnsigned unit; putU64/readU64 are the generated codec helpers its
// writer and reader call.
import 'dart:typed_data';

import '../../build/schema-dart-vm-dart2js-differential/gen-websafe/examples/TabledemoWire.dart';

const int _kBound = 9007199254740991; // 2^53 - 1

String tryWrite(int v) {
  final r = RangedUnsigned();
  r.counts = <int>[v];
  try {
    rangedUnsignedWireSave(r);
    return 'exact';
  } on StateError catch (e) {
    return 'refused:${e.message}';
  }
}

void main() {
  // THE WRITER, through the generated table writer, over the three boundary
  // values: 2^53-1 (inside the exact web range, exact on both runtimes), and
  // 2^53 and 2^53+1 (outside it; exact on the VM, refused on the web).
  final low = tryWrite(_kBound);
  final edge = tryWrite(9007199254740992); // 2^53
  final over = tryWrite(int.parse('9007199254740993')); // 2^53 + 1
  print('range-writer-low=$low');
  print('range-writer-edge=$edge');
  print('range-writer-over=$over');

  // THE READER, through the generated readU64, over the boundary patterns:
  // 2^53 (0x0020000000000000), 2^53+1 (0x0020000000000001) and -2^53
  // (0xFFE0000000000000). The VM reads them exactly; the web refuses them.
  String tryRead(List<int> bytes) {
    try {
      return 'exact:${readU64(Uint8List.fromList(bytes), 0)}';
    } on StateError catch (e) {
      return 'refused:${e.message}';
    }
  }

  print('range-reader-edge=${tryRead([0, 0, 0, 0, 0, 0, 0x20, 0])}');
  print('range-reader-over=${tryRead([1, 0, 0, 0, 0, 0, 0x20, 0])}');
  print('range-reader-neg=${tryRead([0, 0, 0, 0, 0, 0, 0xE0, 0xFF])}');

  // THE SAFE LANE: 2^53-1 still rides the generated writer and reader on both
  // runtimes, unclamped, so the refusal is not a blanket one.
  final w = TableWireWriter(WireState());
  w.putU64(_kBound);
  print('range-safe=${readU64(Uint8List.fromList(w.buf), 0)}');
}
