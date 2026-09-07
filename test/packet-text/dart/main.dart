import 'dart:io';
import 'dart:typed_data';

import '../../../build/packet-text/dart/Narrow.dart' as d;

String hex(Uint8List bytes) => bytes.isEmpty
    ? '-'
    : bytes.map((b) => b.toRadixString(16).padLeft(2, '0')).join();

void main() {
  String? line;
  while ((line = stdin.readLineSync()) != null) {
    final raw = line! == '-' ? '' : line;
    final wire = Uint8List(raw.length ~/ 2);
    for (var i = 0; i < wire.length; i++) {
      wire[i] = int.parse(raw.substring(i * 2, i * 2 + 2), radix: 16);
    }
    final padded = Uint8List(wire.length + 8)..setAll(0, wire);
    final view = ByteData.sublistView(padded);
    final value = d.Narrow();
    if (!d.readNarrow(value, view, wire.length * 8)) {
      stdout.writeln('REFUSE');
      continue;
    }
    final bits = d.measureNarrow(value);
    if (!d.readNarrow(d.Narrow(), view, bits) ||
        d.readNarrow(d.Narrow(), view, bits - 1)) {
      throw StateError('exact bit bound');
    }
    final encoded = Uint8List(256);
    final count = d.writeNarrow(value, ByteData.sublistView(encoded));
    if (count != (bits + 7) ~/ 8) throw StateError('writer byte count');
    stdout.writeln(
      'OK $bits ${hex(value.text.sublist(0, value.textLength))} '
      '$bits ${hex(encoded.sublist(0, count))}',
    );
  }
}
