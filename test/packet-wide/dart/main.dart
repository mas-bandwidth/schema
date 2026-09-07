import 'dart:io';
import 'dart:typed_data';

import '../../../build/packet-wide/dart/WideText.dart' as d;
import '../../../build/packet-wide/dart/shapes/Shapes.dart' as p;

void check(bool ok, String message) {
  if (!ok) throw StateError(message);
}

String hex(Uint8List bytes) =>
    bytes.map((b) => b.toRadixString(16).padLeft(2, '0')).join();

void replay<T>(
  Uint8List wire,
  T value,
  bool Function(T, ByteData, int) read,
  int Function(T, ByteData) write,
  int Function(T) measure,
  Uint16List Function(T) text,
  int Function(T) length,
) {
  final view = ByteData.sublistView(wire);
  text(value).fillRange(0, text(value).length, 0x7f7f);
  if (!read(value, view, wire.length * 8)) {
    stdout.writeln('REFUSE');
    return;
  }
  final bits = measure(value);
  check(read(value, view, bits), 'exact bit read');
  final encoded = Uint8List(256);
  final count = write(value, ByteData.sublistView(encoded));
  check(count == (bits + 7) ~/ 8, 'writer byte count');
  final payload = text(value)
      .take(length(value))
      .map((u) => u.toRadixString(16).padLeft(4, '0'))
      .join();
  check(!read(value, view, bits - 1), 'one bit short read');
  stdout.writeln(
    'OK $bits ${payload.isEmpty ? '-' : payload} $bits ${hex(encoded.sublist(0, count))}',
  );
}

void contracts() {
  final wire = ByteData(1024);
  final v = d.WideSeven();
  for (final n in [-1, 8, 1]) {
    v.textLength = n;
    var refused = false;
    try {
      d.writeWideSeven(v, wire);
    } on ArgumentError {
      refused = true;
    }
    check(refused, 'writer bounds/null');
  }
  v.text[0] = 0xd800;
  d.writeWideSeven(v, wire);
  check(!d.readWideSeven(v, wire, 35), 'unpaired high');
  v.text[0] = 0xffff;
  d.writeWideSeven(v, wire);
  v.text.fillRange(0, 7, 0x7f7f);
  check(
    d.readWideSeven(v, wire, 35) &&
        v.text[0] == 0xffff &&
        v.text[1] == 0x7f7f &&
        v.text[6] == 0x7f7f,
    'tail',
  );
  final c = p.Conditional()
    ..enabled = true
    ..textLength = 2;
  c.text.setAll(0, [0xd800, 0xdc00]);
  p.writeConditional(c, wire);
  check(
    p.measureConditional(c) == 68 && wire.getUint8(0) & 15 == 5,
    'unaligned groups',
  );
  final co = p.Conditional();
  check(
    p.readConditional(co, wire, 68) &&
        co.textLength == 2 &&
        co.text[1] == 0xdc00,
    'conditional',
  );
  c.enabled = false;
  p.writeConditional(c, wire);
  check(
    p.readConditional(co, wire, 1) &&
        co.textLength == 0 &&
        co.text[0] == 0 &&
        co.text[1] == 0,
    'branch zero',
  );
  final choice = p.Choice()..type = p.ChoiceType.text;
  choice.text.valueLength = 1;
  choice.text.value[0] = 0xffff;
  p.writeChoice(choice, wire);
  var bits = p.measureChoice(choice);
  final out = p.Choice();
  for (var i = 0; i < 2; i++) {
    out.text.value[3] = 0x7f7f;
    check(
      p.readChoice(out, wire, bits) &&
          out.text.valueLength == 1 &&
          out.text.value[0] == 0xffff &&
          out.text.value[3] == 0,
      'union reset',
    );
  }
  final box = p.Box();
  check(box.countedCount == 1, 'born count');
  box.items[0].valueLength = 1;
  box.items[0].value[0] = 0xffff;
  box.counted[0].valueLength = 1;
  box.counted[0].value[0] = 65;
  box.choice.type = p.ChoiceType.text;
  p.writeBox(box, wire);
  bits = p.measureBox(box);
  final bo = p.Box();
  bo.counted[1].value[0] = 0x7f7f;
  check(
    p.readBox(bo, wire, bits) &&
        bo.items[0].value[0] == 0xffff &&
        bo.countedCount == 1 &&
        bo.counted[0].value[0] == 65 &&
        bo.counted[1].value[0] == 0x7f7f &&
        bo.choice.type == p.ChoiceType.text,
    'composition',
  );
  check(!p.readBox(bo, wire, bits - 1), 'composition truncation');
}

void main(List<String> args) {
  if (args.isNotEmpty) {
    contracts();
    return;
  }
  String? line;
  while ((line = stdin.readLineSync()) != null) {
    final parts = line!.split(' ');
    final raw = parts[1] == '-' ? '' : parts[1];
    final wire = Uint8List(raw.length ~/ 2);
    for (var i = 0; i < wire.length; i++) {
      wire[i] = int.parse(raw.substring(i * 2, i * 2 + 2), radix: 16);
    }
    if (parts[0] == '7') {
      replay(
        wire,
        d.WideSeven(),
        d.readWideSeven,
        d.writeWideSeven,
        d.measureWideSeven,
        (v) => v.text,
        (v) => v.textLength,
      );
    } else if (parts[0] == '4') {
      replay(
        wire,
        d.WideFour(),
        d.readWideFour,
        d.writeWideFour,
        d.measureWideFour,
        (v) => v.text,
        (v) => v.textLength,
      );
    } else {
      throw StateError('unexpected bound');
    }
  }
}
