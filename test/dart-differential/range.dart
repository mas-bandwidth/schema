// THE ABOVE-2^53 RUNTIME LANE OF THE DART VM / dart2js DIFFERENTIAL.
//
// webints.dart settles the values a JavaScript double carries exactly (up to
// 2^53-1). This driver is the other half: a 64-bit INTEGER above 2^53 cannot
// be represented by a dart2js int, so the generated codec must REFUSE it by
// name instead of silently writing or returning the rounded value. The VM's
// int is exact to 2^63 and keeps carrying the same values.
//
// The target runs this on the VM and through dart2js under node, and holds
// each runtime to its specified side: the VM prints the exact values, the web
// prints the refusal named `TableWire: 64-bit value is outside the exact web
// int range (2^53)`. The table writer/reader below is the generated
// RangedUnsigned unit; putU64/readU64 are the generated codec helpers its
// writer and reader call.
import 'dart:typed_data';

import '../../build/schema-dart-vm-dart2js-differential/gen-websafe/examples/TabledemoWire.dart';

const int _kAbove = 9007199254740994; // 2^53 + 2, an exact JavaScript double

void main() {
  // THE WRITER, through the generated table writer: an unbounded 64-bit
  // payload above the exact web-int range.
  final over = RangedUnsigned();
  over.counts = <int>[_kAbove];
  try {
    rangedUnsignedWireSave(over);
    print('range-table-writer=exact');
  } on StateError catch (e) {
    print('range-table-writer=refused:${e.message}');
  }

  // THE READER, through the generated readU64: 0x0020000000000001 is 2^53+1,
  // which a JavaScript double cannot hold; the VM reads it back exactly.
  final raw = Uint8List.fromList(<int>[1, 0, 0, 0, 0, 0, 0x20, 0]);
  try {
    final v = readU64(raw, 0);
    print('range-reader=exact:$v');
  } on StateError catch (e) {
    print('range-reader=refused:${e.message}');
  }

  // THE SAFE LANE: 2^53-1 still rides the generated writer and reader on both
  // runtimes, unclamped, so the refusal is not a blanket one; 2^53 itself is
  // exact in a JavaScript double and also reads back unchanged.
  final w = TableWireWriter(WireState());
  w.putU64(9007199254740991);
  print('range-safe=${readU64(Uint8List.fromList(w.buf), 0)}');
  final edge = TableWireWriter(WireState());
  edge.putU64(9007199254740992); // 2^53, the boundary
  print('range-edge=${readU64(Uint8List.fromList(edge.buf), 0)}');
}
