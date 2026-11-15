// The packet end check on the Dart leg (SPEC §5, packet-canonical-end).
//
// The generated one-packet read runs the end check once, after the top-level
// object: the bits consumed, rounded up to a whole byte, must equal the
// packet's byte length, and the final byte's unused bits must be zero.
//
// Reads the three shared vectors under testdata/wire/packet-end and holds the
// verdicts: the exact packet is accepted as value 3, the same packet plus a
// whole byte is refused, and the same value with the final byte's unused bits
// set is refused.
//
// Prints OK and exits 0; every failure is one FAILED line, then exit 1.
import 'dart:io';
import 'dart:typed_data';

import '../../../build/packet-end/dart/End.dart';

var failed = false;

void check(bool ok, String what) {
  if (!ok) {
    print('FAILED: $what');
    failed = true;
  }
}

void vector(String dir, String name, {required bool accepted, int? value}) {
  final data = File('$dir/$name.bin').readAsBytesSync();
  final out = End();
  final read = readEnd(out, ByteData.sublistView(data), data.length * 8);
  if (accepted) {
    check(read, 'packet-end $name refused');
    if (read) {
      check(out.value == value, 'packet-end $name value');
    }
  } else {
    check(!read, 'packet-end $name accepted');
  }
}

void main(List<String> args) {
  final dir = args[0];
  vector(dir, 'exact', accepted: true, value: 3);
  vector(dir, 'trailing', accepted: false);
  vector(dir, 'padding', accepted: false);
  if (failed) {
    exit(1);
  }
  print('packet-end Dart: exact accepted as 3; trailing and padding refused');
}
