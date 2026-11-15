import 'dart:io';
import 'dart:typed_data';

import '../../../build/packet-text/dart/Narrow.dart' as d;

String hex(Uint8List bytes) => bytes.isEmpty
    ? '-'
    : bytes.map((b) => b.toRadixString(16).padLeft(2, '0')).join();

// readBits is the bits read the shared corpus is compared on: it decodes value
// from the front of wire and returns the bits the object consumed, or -1, with
// no end check (the end check is packet-end-dart's). Dart's one-packet read
// returns a verdict only and checks the end, so the bits read is the fewest
// bits b at which the one-packet read accepts the first b bits of wire with
// every later bit cleared: the object reads only bits below b, and below its
// own length it runs out of bits.
(int, d.Narrow) readBits(Uint8List wire) {
  for (var bits = 0; bits <= wire.length * 8; bits++) {
    final bytes = (bits + 7) >>> 3;
    final prefix = Uint8List(bytes + 8)..setRange(0, bytes, wire);
    if ((bits & 7) != 0) {
      prefix[bytes - 1] &= (1 << (bits & 7)) - 1;
    }
    final value = d.Narrow();
    if (d.readNarrow(value, ByteData.sublistView(prefix), bits)) {
      return (bits, value);
    }
  }
  return (-1, d.Narrow());
}

void main() {
  String? line;
  while ((line = stdin.readLineSync()) != null) {
    final raw = line! == '-' ? '' : line;
    final wire = Uint8List(raw.length ~/ 2);
    for (var i = 0; i < wire.length; i++) {
      wire[i] = int.parse(raw.substring(i * 2, i * 2 + 2), radix: 16);
    }
    final (consumed, value) = readBits(wire);
    if (consumed < 0) {
      stdout.writeln('REFUSE');
      continue;
    }
    final encoded = Uint8List(256);
    final count = d.writeNarrow(value, ByteData.sublistView(encoded));
    final canonical = encoded.sublist(0, count);
    // There is no measure function (SPEC §6.1): the written bit length is
    // what the bits read consumes of the canonical bytes, which lies in
    // write's last byte. The one-packet read accepts the canonical packet at
    // that exact bound and refuses it one bit less.
    final (written, _) = readBits(canonical);
    final view = ByteData.sublistView(encoded);
    if (written < 0 ||
        written > count * 8 ||
        (count > 0 && written <= count * 8 - 8) ||
        !d.readNarrow(d.Narrow(), view, written) ||
        (written > 0 && d.readNarrow(d.Narrow(), view, written - 1))) {
      throw StateError('exact bit bound');
    }
    stdout.writeln(
      'OK $consumed ${hex(value.text.sublist(0, value.textLength))} '
      '$written ${hex(canonical)}',
    );
  }
}
