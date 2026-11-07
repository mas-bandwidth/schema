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
    final encoded = Uint8List(256);
    final count = d.writeNarrow(value, ByteData.sublistView(encoded));
    // There is no measure function (SPEC §6.1): the exact bit length is the
    // fewest bits the reader accepts, which lies in write's last byte.
    var bits = count == 0 ? 0 : count * 8 - 7;
    while (bits <= count * 8 && !d.readNarrow(d.Narrow(), view, bits)) {
      bits++;
    }
    if (bits > count * 8 ||
        (bits > 0 && d.readNarrow(d.Narrow(), view, bits - 1))) {
      throw StateError('exact bit bound');
    }
    stdout.writeln(
      'OK $bits ${hex(value.text.sublist(0, value.textLength))} '
      '$bits ${hex(encoded.sublist(0, count))}',
    );
  }
}
