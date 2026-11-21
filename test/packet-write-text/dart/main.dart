import 'dart:io';
import 'dart:typed_data';

import '../../../build/packet-write-text/dart/gen/PacketText.dart';

int failures = 0;

void fail(String what) {
  stdout.writeln('FAILED: $what');
  failures++;
}

Uint8List parseHex(String text) {
  if (text == '-') return Uint8List(0);
  final fields = text.split(RegExp(r'\s+'));
  final bytes = Uint8List(fields.length);
  for (var i = 0; i < fields.length; i++) {
    bytes[i] = int.parse(fields[i], radix: 16);
  }
  return bytes;
}

Text value(Uint8List raw) {
  final value = Text();
  value.content.setRange(0, raw.length, raw);
  value.contentLength = raw.length;
  return value;
}

bool same(Text value, Uint8List raw) {
  if (value.contentLength != raw.length) return false;
  for (var i = 0; i < raw.length; i++) {
    if (value.content[i] != raw[i]) return false;
  }
  return true;
}

void main(List<String> args) {
  final lines = File(args[0]).readAsLinesSync();
  var ran = 0;
  for (var line in lines) {
    line = line.trim();
    if (line.isEmpty || line.startsWith('#')) continue;
    ran++;
    final parts = line.split('|');
    final name = parts[0].trim();
    final raw = parseHex(parts[1].trim());
    final verdict = parts[2].trim();
    if (verdict != 'written' && verdict != 'refused') {
      throw StateError(
        'cases: $name: verdict "$verdict" is neither written nor refused',
      );
    }
    final sent = value(raw);
    if (verdict == 'refused') {
      try {
        final data = Uint8List(textMaxBytes);
        writeText(sent, ByteData.sublistView(data));
        fail('$name write accepted');
      } on AssertionError {
        // the debug assert is the refusal
      }
      continue;
    }
    final data = Uint8List(textMaxBytes);
    final written = writeText(sent, ByteData.sublistView(data));
    final back = Text();
    final view = ByteData.sublistView(data);
    if (!readText(back, view, written * 8)) {
      fail('$name read refused');
      continue;
    }
    if (!same(back, raw)) {
      fail('$name read back differs');
    }
  }
  // One unit per shared case: tools/mustrun holds the count to the one the
  // recipe registers, so an emptied or shortened cases.txt is red.
  stdout.writeln('packet-write-text Dart: ran $ran, failed $failures');
  if (failures != 0 || ran == 0) {
    exit(1);
  }
}
