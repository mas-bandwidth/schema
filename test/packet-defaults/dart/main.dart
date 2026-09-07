import 'dart:io';
import 'dart:typed_data';

import '../../../build/packet-defaults/dart/defaults/Defaults.dart' as d;
import '../../../build/packet-defaults/dart/plain/Plain.dart' as p;

void check(bool ok, String name) {
  if (!ok) throw StateError(name);
}

Object shape(Object value) {
  if (value is d.Sample)
    return [
      value.name,
      value.nameLength,
      value.token,
      value.tokenLength,
      value.caps,
      value.emptyName,
      value.emptyNameLength,
      value.emptyToken,
      value.emptyTokenLength,
      value.emptyCaps,
    ];
  if (value is d.Batch)
    return [value.head, value.items, value.counted, value.countedCount];
  if (value is d.ZeroCount) return [value.items, value.itemsCount];
  if (value is d.Conditional) return [value.enabled, value.value];
  if (value is d.Choice) return [value.type, value.sample, value.conditional];
  if (value is d.Prefix)
    return [value.name, value.nameLength, value.token, value.tokenLength];
  if (value is d.WideMask) return [value.high, value.all];
  if (value is d.SplitMask) return [value.lead, value.mask, value.tail];
  if (value is num || value is bool || value is Iterable) return value;
  throw StateError('No storage comparison for ${value.runtimeType}');
}

bool equal(Object a, Object b) {
  final x = shape(a), y = shape(b);
  if (x is Iterable && y is Iterable) {
    final aa = x.toList(), bb = y.toList();
    if (aa.length != bb.length) return false;
    for (var i = 0; i < aa.length; i++) {
      if (!equal(aa[i] as Object, bb[i] as Object)) return false;
    }
    return true;
  }
  return x == y;
}

d.Sample zero() {
  final s = d.Sample();
  s.name.fillRange(0, 6, 0);
  s.nameLength = 0;
  s.token.fillRange(0, 4, 0);
  s.tokenLength = 0;
  s.caps = 0;
  s.emptyName.fillRange(0, 3, 0);
  s.emptyNameLength = 0;
  s.emptyToken.fillRange(0, 2, 0);
  s.emptyTokenLength = 0;
  s.emptyCaps = 0;
  return s;
}

d.Sample sample() {
  final s = zero();
  s.name.setAll(0, [195, 169, 240, 144, 128, 128]);
  s.nameLength = 6;
  s.token.setAll(0, [92, 110, 92, 116]);
  s.tokenLength = 4;
  s.caps = 5;
  return s;
}

d.Sample short() {
  final s = zero();
  s.name[0] = 65;
  s.nameLength = 1;
  s.token[1] = 255;
  s.tokenLength = 2;
  s.caps = 2;
  return s;
}

d.Sample dirty() {
  final s = zero();
  s.name.fillRange(0, 6, 100);
  s.nameLength = 6;
  s.token.fillRange(0, 4, 161);
  s.tokenLength = 4;
  s.caps = 7;
  s.emptyName.fillRange(0, 3, 111);
  s.emptyNameLength = 3;
  s.emptyToken.fillRange(0, 2, 177);
  s.emptyTokenLength = 2;
  s.emptyCaps = 7;
  return s;
}

void copy(d.Sample to, d.Sample from) {
  to.name.setAll(0, from.name);
  to.nameLength = from.nameLength;
  to.token.setAll(0, from.token);
  to.tokenLength = from.tokenLength;
  to.caps = from.caps;
  to.emptyName.setAll(0, from.emptyName);
  to.emptyNameLength = from.emptyNameLength;
  to.emptyToken.setAll(0, from.emptyToken);
  to.emptyTokenLength = from.emptyTokenLength;
  to.emptyCaps = from.emptyCaps;
}

d.Sample overlay(d.Sample initial, d.Sample sent) {
  initial.name.setRange(0, sent.nameLength, sent.name);
  initial.nameLength = sent.nameLength;
  initial.token.setRange(0, sent.tokenLength, sent.token);
  initial.tokenLength = sent.tokenLength;
  initial.caps = sent.caps;
  initial.emptyNameLength = 0;
  initial.emptyTokenLength = 0;
  initial.emptyCaps = 0;
  return initial;
}

Uint8List write<T>(T value, int Function(T, ByteData) encode) {
  final data = Uint8List(4096);
  final n = encode(value, ByteData.sublistView(data));
  check(n > 0, 'write');
  return data.sublist(0, n);
}

void read<T extends Object>(
  Uint8List bytes,
  int bits,
  T initial,
  T want,
  bool Function(T, ByteData, int) decode,
) {
  check(decode(initial, ByteData.sublistView(bytes), bits), 'exact-bit read');
  check(equal(initial, want), 'read values and backing storage');
}

void golden<T extends Object>(
  String dir,
  String name,
  T value,
  T Function() initial,
  T Function() want,
  int Function(T, ByteData) encode,
  bool Function(T, ByteData, int) decode,
  int Function(T) measure,
) {
  final bytes = File('$dir/$name.bin').readAsBytesSync();
  final bits = int.parse(File('$dir/$name.bits').readAsStringSync().trim());
  check(
    equal(write(value, encode), bytes) && measure(value) == bits,
    'C++ byte/bit pin $name',
  );
  read(bytes, bits, initial(), want(), decode);
  check(
    !decode(initial(), ByteData.sublistView(bytes), bits - 1),
    'one-bit-short refusal',
  );
}

d.Batch reusedBatch() {
  final b = d.Batch();
  copy(b.counted[1], dirty());
  copy(b.counted[2], short());
  return b;
}

d.ZeroCount reusedZero(int count) {
  final z = d.ZeroCount();
  z.itemsCount = count;
  copy(z.items[0], dirty());
  copy(z.items[1], short());
  return z;
}

d.Choice sampleChoice() {
  final c = d.Choice();
  c.type = d.ChoiceType.sample;
  return c;
}

void main(List<String> args) {
  final dir = args[0];
  check(equal(d.Sample(), sample()), 'packet-default constructor bytes');
  final reused = dirty();
  final originalName = reused.name, originalToken = reused.token;
  d.initSample(reused);
  check(equal(reused, sample()), 'Init restores defaults');
  check(
    identical(reused.name, originalName) &&
        identical(reused.token, originalToken),
    'Init retains buffers',
  );
  d.zeroSample(reused);
  check(equal(reused, zero()), 'Zero differs from Init');
  final empty = d.EmptyOnly();
  check(
    equal(
      [
        empty.name,
        empty.nameLength,
        empty.token,
        empty.tokenLength,
        empty.caps,
      ],
      [
        [0, 0],
        0,
        [0],
        0,
        0,
      ],
    ),
    'empty defaults',
  );
  final prefix = d.Prefix();
  check(
    equal(shape(prefix), [
      [195, 169, 0, 0, 0],
      2,
      [92, 110, 0, 0, 0],
      2,
    ]),
    'short default tails',
  );
  prefix.name.fillRange(0, 5, 255);
  prefix.token.fillRange(0, 5, 255);
  d.initPrefix(prefix);
  check(equal(prefix, d.Prefix()), 'Init clears stale tails');
  final wide = d.WideMask();
  check(
    wide.high == 0x8000000000000000 && wide.all == 0xffffffffffffffff,
    'bit63 and all64 default masks',
  );
  final wideBytes = Uint8List(16)..fillRange(8, 16, 255);
  wideBytes[7] = 128;
  check(
    equal(write(wide, d.writeWideMask), wideBytes) &&
        d.measureWideMask(wide) == 128,
    'independent 64-bit wire',
  );
  read(
    wideBytes,
    128,
    d.WideMask()
      ..high = 0
      ..all = 0,
    wide,
    d.readWideMask,
  );
  final split = d.SplitMask()
    ..lead = 5
    ..mask = 1 << 32
    ..tail = 2;
  final splitBytes = Uint8List.fromList([5, 0, 0, 0, 40]);
  check(
    equal(write(split, d.writeSplitMask), splitBytes) &&
        d.measureSplitMask(split) == 38,
    'independent 33-bit wire',
  );
  read(splitBytes, 38, d.SplitMask(), split, d.readSplitMask);
  final plain = p.Sample()
    ..nameLength = 6
    ..tokenLength = 4
    ..caps = 5;
  plain.name.setAll(0, sample().name);
  plain.token.setAll(0, sample().token);
  check(
    equal(write(plain, p.writeSample), write(d.Sample(), d.writeSample)) &&
        p.measureSample(plain) == d.measureSample(d.Sample()),
    'defaultless twin',
  );
  golden(
    dir,
    'sample-defaults',
    d.Sample(),
    zero,
    sample,
    d.writeSample,
    d.readSample,
    d.measureSample,
  );
  final batch = d.Batch();
  check(
    batch.countedCount == 1 && equal(batch.head, sample()),
    'nested defaults and born count',
  );
  for (final s in [...batch.items, ...batch.counted]) {
    check(equal(s, sample()), 'all backing elements');
  }
  golden(
    dir,
    'batch-defaults',
    batch,
    reusedBatch,
    reusedBatch,
    d.writeBatch,
    d.readBatch,
    d.measureBatch,
  );
  final z = d.ZeroCount();
  check(
    z.itemsCount == 0 && equal(z.items, [sample(), sample()]),
    'zero-count defaults',
  );
  golden(
    dir,
    'zero-count',
    z,
    () => reusedZero(2),
    () => reusedZero(0),
    d.writeZeroCount,
    d.readZeroCount,
    d.measureZeroCount,
  );
  check(
    d.Conditional().enabled && equal(d.Conditional().value, sample()),
    'conditional defaults',
  );
  golden(
    dir,
    'conditional-on',
    d.Conditional(),
    d.Conditional.new,
    d.Conditional.new,
    d.writeConditional,
    d.readConditional,
    d.measureConditional,
  );
  final off = d.Conditional()..enabled = false;
  d.Conditional offWant() {
    final c = d.Conditional()..enabled = false;
    copy(c.value, zero());
    return c;
  }

  golden(
    dir,
    'conditional-off',
    off,
    d.Conditional.new,
    offWant,
    d.writeConditional,
    d.readConditional,
    d.measureConditional,
  );
  golden(
    dir,
    'choice-sample',
    sampleChoice(),
    () => d.Choice()..type = d.ChoiceType.conditional,
    sampleChoice,
    d.writeChoice,
    d.readChoice,
    d.measureChoice,
  );
  golden(
    dir,
    'sample-short',
    short(),
    dirty,
    () => overlay(dirty(), short()),
    d.writeSample,
    d.readSample,
    d.measureSample,
  );
  golden(
    dir,
    'sample-empty',
    zero(),
    dirty,
    () => overlay(dirty(), zero()),
    d.writeSample,
    d.readSample,
    d.measureSample,
  );
  for (final sent in [short(), zero()]) {
    final value = sampleChoice();
    copy(value.sample, sent);
    final wire = write(value, d.writeChoice);
    final output = d.Choice()..type = d.ChoiceType.conditional;
    final selected = output.sample, buffer = output.sample.name;
    final want = sampleChoice();
    copy(want.sample, overlay(sample(), sent));
    for (var attempt = 0; attempt < 2; attempt++) {
      copy(selected, dirty());
      read(wire, d.measureChoice(value), output, want, d.readChoice);
      check(
        identical(output.sample, selected) && identical(selected.name, buffer),
        'selection retains storage',
      );
    }
  }
  final offChoice = d.Choice()..type = d.ChoiceType.conditional;
  offChoice.conditional.enabled = false;
  final wire = write(offChoice, d.writeChoice),
      output = sampleChoice(),
      want = d.Choice()..type = d.ChoiceType.conditional;
  want.conditional.enabled = false;
  copy(want.conditional.value, zero());
  for (var attempt = 0; attempt < 2; attempt++) {
    output.conditional.enabled = true;
    copy(output.conditional.value, dirty());
    read(wire, d.measureChoice(offChoice), output, want, d.readChoice);
  }
  print(
    'packet defaults Dart: constructors, eight C++ goldens and reused storage OK',
  );
}
