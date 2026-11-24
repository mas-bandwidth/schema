// The write-refusal-packet surface on the Dart leg (docs/SPEC.md §5.1): every
// row of the one shared list, test/conformance/write-refusal-packet/rows.txt
// (<name> <verdict> <op> <arg>, keyed here by name), starts from the type's
// default value, changes ONE field, and runs the packet writer. Dart is an assert leg, so the driver runs under
// --enable-asserts and an AssertionError is the refusal; a written twin must
// be accepted and read back. A row this driver has no setter for fails, and so
// does a setter it runs that the file does not name.
import 'dart:io';
import 'dart:typed_data';

import '../../../build/packet-write-refusal/dart/gen/RefusalPacket.dart';

int failures = 0;

void fail(String what) {
  stdout.writeln('FAILED: packet-write-refusal $what');
  failures++;
}

typedef Setter = void Function(PacketWriteRefusal v);

final Map<String, Setter> setters = {
  // write-refusal-packet setters: begin
  'scalar_high': (v) => v.scalar = 11,
  'scalar_low': (v) => v.scalar = -1,
  'scalar_at_max': (v) => v.scalar = 10,
  'scalar_at_min': (v) => v.scalar = 0,
  'bits_wide': (v) => v.narrow = 200,
  'bits_just_wide': (v) => v.narrow = 128,
  'bits_at_max': (v) => v.narrow = 127,
  'enum_high': (v) => v.grade = Grade(4),
  'enum_at_max': (v) => v.grade = Grade.gold,
  'union_tag_high': (v) => v.shot.type = ShotType(3),
  'union_tag_at_max': (v) => v.shot.type = ShotType.missile,
  'count_high': (v) => v.windowCount = 9,
  'count_low': (v) => v.windowCount = 1,
  'count_at_max': (v) => v.windowCount = 8,
  'count_at_min': (v) => v.windowCount = 2,
  'text_long': (v) => v.textLength = 9,
  'text_illformed': (v) {
    v.text[0] = 0xff;
    v.textLength = 1;
  },
  'text_zero': (v) {
    v.text[0] = 0;
    v.textLength = 1;
  },
  'text_at_max': (v) {
    v.text.fillRange(0, v.text.length, 0x61);
    v.textLength = 8;
  },
  'const_wrong': (v) => v.fixedConstant = 6,
  'const_at': (v) => v.fixedConstant = 5,
  'reserved_wrong': (v) => v.reservedZero = 1,
  'reserved_at': (v) => v.reservedZero = 0,
  'flags_wide': (v) => v.perks = 1 << 3,
  'flags_wide_high': (v) => v.perks = 1 << 40,
  'flags_at_max': (v) => v.perks = perkShield | perkBoost | perkCloak,
  // write-refusal-packet setters: end
};

// Cases the leg cannot build. Dart's enum and union tag are extension types
// over int, so every ordinal is buildable and the set is empty.
final Set<String> notRepresentable = {};

void main(List<String> args) {
  final seen = <String>{};
  for (var line in File(args[0]).readAsLinesSync()) {
    line = line.trim();
    if (line.isEmpty || line.startsWith('#')) continue;
    final parts = line.split(RegExp(r'\s+'));
    if (parts.length != 4 || (parts[1] != 'refused' && parts[1] != 'wrote')) {
      fail(': a row is not <name> refused|wrote <op> <arg>: $line');
      continue;
    }
    final name = parts[0];
    final verdict = parts[1];
    if (!seen.add(name)) {
      fail('$name: named twice in rows.txt');
      continue;
    }
    if (notRepresentable.contains(name)) continue;
    final set = setters[name];
    if (set == null) {
      fail('$name: a row in rows.txt this driver has no setter for');
      continue;
    }
    final value = PacketWriteRefusal();
    set(value);
    final data = Uint8List(packetWriteRefusalMaxBytes);
    final view = ByteData.sublistView(data);
    if (verdict == 'refused') {
      try {
        writePacketWriteRefusal(value, view);
        fail('$name: refused value was written');
      } on AssertionError {
        // the debug assert is the refusal
      }
      continue;
    }
    try {
      final written = writePacketWriteRefusal(value, view);
      final back = PacketWriteRefusal();
      if (!readPacketWriteRefusal(back, view, written * 8)) {
        fail('$name: written twin read back refused');
      }
    } on AssertionError {
      fail('$name: the lawful value at the boundary was refused');
    }
  }
  for (final name in setters.keys) {
    if (!seen.contains(name))
      fail('$name: a setter this driver runs that rows.txt does not name');
  }
  if (failures != 0) exit(1);
  stdout.writeln('packet-write-refusal-dart: ${seen.length} rows');
}
