// The Dart leg of test/packet-void (SPEC §4.8): answers every case of
// testdata/wire/packet-void/cases.txt, one "case <name> ok|FAILED: <why>"
// line each, for tools/casegate. The pins are the Go oracle's bytes, never
// this writer's. A thrown error (an --enable-asserts contract in the
// generated writer, a range error) is caught per case, so every case still
// gets its line. Dart's writer reports bytes, not bits; its framing read
// reports the bits consumed, which is checked against the bits column.
import 'dart:io';
import 'dart:typed_data';

import '../../../build/packet-void/dart/Void.dart' as v;

class Failed implements Exception {
  Failed(this.why);
  final String why;
}

// A case's value, one field set by its type.
class Value {
  Value(this.type);
  final String type;
  final mixed = v.Mixed();
  final late = v.Late();
  final tags = v.Tags();
  final single = v.Single();
  final empty = v.Empty();
  final frame = v.Frame();
  final batch = v.Batch();
}

const types = ['Mixed', 'Late', 'Tags', 'Single', 'Empty', 'Frame', 'Batch'];

// The payload arm holds its declared defaults (entries empty, marker 5).
void selectPayload(v.Mixed m) {
  m.type = v.MixedType.payload;
  v.initDefaultArm(m.payload);
}

// The value of each accept case, by name; null for a name this driver does
// not know, which is FAILED and never written with another case's value.
Value? build(String name) {
  switch (name) {
    case 'mixed-none':
      return Value('Mixed')..mixed.type = v.MixedType.none;
    case 'mixed-ping':
      return Value('Mixed')..mixed.type = v.MixedType.ping;
    case 'mixed-payload':
      final value = Value('Mixed');
      selectPayload(value.mixed);
      return value;
    case 'late-payload':
      final value = Value('Late');
      value.late.type = v.LateType.payload;
      v.initDefaultArm(value.late.payload);
      return value;
    case 'late-pong':
      return Value('Late')..late.type = v.LateType.pong;
    case 'tags-none':
      return Value('Tags')..tags.type = v.TagsType.none;
    case 'tags-ping':
      return Value('Tags')..tags.type = v.TagsType.ping;
    case 'tags-pong':
      return Value('Tags')..tags.type = v.TagsType.pong;
    case 'single-none':
      return Value('Single')..single.type = v.SingleType.none;
    case 'single-ack':
      return Value('Single')..single.type = v.SingleType.ack;
    case 'empty-none':
      return Value('Empty')..empty.type = v.EmptyType.none;
    case 'frame-ping':
      final value = Value('Frame');
      value.frame.lead = 5;
      value.frame.choice.type = v.MixedType.ping;
      value.frame.tail = 6;
      return value;
    case 'batch-empty':
      return Value('Batch')..batch.valuesCount = 0;
    case 'batch-ping':
      final value = Value('Batch');
      value.batch.valuesCount = 1;
      value.batch.values[0].type = v.MixedType.ping;
      return value;
    case 'batch-ping-payload':
      final value = Value('Batch');
      value.batch.valuesCount = 2;
      value.batch.values[0].type = v.MixedType.ping;
      selectPayload(value.batch.values[1]);
      return value;
  }
  return null;
}

// Every read lands on storage dirtied first: a selected payload must be
// constructed with its declared defaults, whatever the storage held.
void dirtyMixed(v.Mixed m) {
  m.type = v.MixedType.payload;
  m.payload.entriesCount = 2;
  m.payload.entries[0].retries = 99;
  m.payload.entries[1].retries = 99;
  m.payload.marker = 1;
}

Value dirty(String type) {
  final value = Value(type);
  dirtyMixed(value.mixed);
  value.late.type = v.LateType.payload;
  value.late.payload.entriesCount = 2;
  value.late.payload.entries[0].retries = 99;
  value.late.payload.entries[1].retries = 99;
  value.late.payload.marker = 1;
  value.tags.type = v.TagsType.pong;
  value.single.type = v.SingleType.ack;
  value.frame.lead = 1;
  value.frame.tail = 1;
  dirtyMixed(value.frame.choice);
  value.batch.valuesCount = 2;
  dirtyMixed(value.batch.values[0]);
  dirtyMixed(value.batch.values[1]);
  return value;
}

// write is the public writer: the bytes written.
int write(Value value, ByteData out) {
  switch (value.type) {
    case 'Mixed':
      return v.writeMixed(value.mixed, out);
    case 'Late':
      return v.writeLate(value.late, out);
    case 'Tags':
      return v.writeTags(value.tags, out);
    case 'Single':
      return v.writeSingle(value.single, out);
    case 'Empty':
      return v.writeEmpty(value.empty, out);
    case 'Frame':
      return v.writeFrame(value.frame, out);
    case 'Batch':
      return v.writeBatch(value.batch, out);
  }
  throw Failed('unknown type ${value.type}');
}

// read is the public one-packet read: false refuses the wire.
bool read(Value value, ByteData pin, int numBits) {
  switch (value.type) {
    case 'Mixed':
      return v.readMixed(value.mixed, pin, numBits);
    case 'Late':
      return v.readLate(value.late, pin, numBits);
    case 'Tags':
      return v.readTags(value.tags, pin, numBits);
    case 'Single':
      return v.readSingle(value.single, pin, numBits);
    case 'Empty':
      return v.readEmpty(value.empty, pin, numBits);
    case 'Frame':
      return v.readFrame(value.frame, pin, numBits);
    case 'Batch':
      return v.readBatch(value.batch, pin, numBits);
  }
  throw Failed('unknown type ${value.type}');
}

// readBits is the framing read: the bits the object consumed.
int readBits(Value value, ByteData pin, int numBits) {
  switch (value.type) {
    case 'Mixed':
      return v.readMixedBits(value.mixed, pin, numBits, 0);
    case 'Late':
      return v.readLateBits(value.late, pin, numBits, 0);
    case 'Tags':
      return v.readTagsBits(value.tags, pin, numBits, 0);
    case 'Single':
      return v.readSingleBits(value.single, pin, numBits, 0);
    case 'Empty':
      return v.readEmptyBits(value.empty, pin, numBits, 0);
    case 'Frame':
      return v.readFrameBits(value.frame, pin, numBits, 0);
    case 'Batch':
      return v.readBatchBits(value.batch, pin, numBits, 0);
  }
  throw Failed('unknown type ${value.type}');
}

bool mixedEqual(v.Mixed got, v.Mixed want) {
  if (got.type != want.type) return false;
  if (want.type != v.MixedType.payload)
    return true; // unselected arms are unspecified
  // every backing entry, used or not, holds what the arm's construction gave
  // it: a read constructs the arm before decoding
  return got.payload.entriesCount == want.payload.entriesCount &&
      got.payload.marker == want.payload.marker &&
      got.payload.entries[0].retries == want.payload.entries[0].retries &&
      got.payload.entries[1].retries == want.payload.entries[1].retries;
}

bool valueEqual(Value got, Value want) {
  switch (want.type) {
    case 'Mixed':
      return mixedEqual(got.mixed, want.mixed);
    case 'Late':
      if (got.late.type != want.late.type) return false;
      if (want.late.type != v.LateType.payload) return true;
      return got.late.payload.entriesCount == want.late.payload.entriesCount &&
          got.late.payload.marker == want.late.payload.marker &&
          got.late.payload.entries[0].retries ==
              want.late.payload.entries[0].retries &&
          got.late.payload.entries[1].retries ==
              want.late.payload.entries[1].retries;
    case 'Tags':
      return got.tags.type == want.tags.type;
    case 'Single':
      return got.single.type == want.single.type;
    case 'Empty':
      return got.empty.type == want.empty.type;
    case 'Frame':
      return got.frame.lead == want.frame.lead &&
          got.frame.tail == want.frame.tail &&
          mixedEqual(got.frame.choice, want.frame.choice);
    case 'Batch':
      if (got.batch.valuesCount != want.batch.valuesCount) return false;
      for (var i = 0; i < want.batch.valuesCount; i++) {
        if (!mixedEqual(got.batch.values[i], want.batch.values[i])) {
          return false;
        }
      }
      return true;
  }
  return false;
}

Uint8List load(String dir, String name) {
  try {
    return File('$dir/$name.bin').readAsBytesSync();
  } on FileSystemException catch (e) {
    throw Failed('cannot open $dir/$name.bin: ${e.message}');
  }
}

Uint8List written(Value value) {
  final out = Uint8List(64);
  final bytes = write(value, ByteData.sublistView(out));
  return Uint8List.sublistView(out, 0, bytes);
}

bool sameBytes(Uint8List a, Uint8List b) {
  if (a.length != b.length) return false;
  for (var i = 0; i < a.length; i++) {
    if (a[i] != b[i]) return false;
  }
  return true;
}

void acceptCase(String dir, String name, String type, String bitsColumn) {
  final want = build(name);
  if (want == null) throw Failed('unknown accept case');
  if (want.type != type) {
    throw Failed('case type $type, the driver builds another');
  }
  final bits = int.tryParse(bitsColumn);
  if (bits == null) throw Failed('bits column $bitsColumn');
  final pin = load(dir, name);
  final out = written(want);
  if (out.length != (bits + 7) >> 3) {
    throw Failed('write has ${out.length} bytes, want ${(bits + 7) >> 3}');
  }
  if (!sameBytes(out, pin)) throw Failed('write bytes differ from the pin');
  final view = ByteData.sublistView(pin);
  final got = dirty(type);
  if (!read(got, view, pin.length * 8)) throw Failed('read refused');
  if (!valueEqual(got, want)) throw Failed('read value differs');
  if (!sameBytes(written(got), pin)) {
    throw Failed('the read value writes other bytes than the pin');
  }
  final consumed = readBits(dirty(type), view, pin.length * 8);
  if (consumed != bits) throw Failed('read has $consumed bits, want $bits');
}

void refuseCase(String dir, String name, String type) {
  if (!types.contains(type)) throw Failed('unknown type $type');
  final pin = load(dir, name);
  if (read(dirty(type), ByteData.sublistView(pin), pin.length * 8)) {
    throw Failed('read accepted');
  }
}

// True under --enable-asserts: the generated writer's contract checks are
// Dart asserts, so only then does a write refuse. Decided by the language's
// own assertion switch, never by a flag the test passes.
bool assertsOn() {
  var on = false;
  assert(on = true);
  return on;
}

// A text tag and a payload of another type cannot be built: the tag is an int
// and the payload a final DefaultArm, and the analyzer refuses them.
const notRepresentable = ['mixed-write-payload-type', 'mixed-write-tag-text'];

// The change a refuse-write case names, on the type's default value; null for
// a case this driver does not know. A Dart tag is an int, so a negative tag
// and tag 3 are representable.
Value? buildRefuseWrite(String name) {
  switch (name) {
    case 'mixed-write-tag-negative':
      return Value('Mixed')..mixed.type = v.MixedType(-1);
    case 'mixed-write-tag-3':
      return Value('Mixed')..mixed.type = v.MixedType(3);
    case 'mixed-write-marker-8':
      final value = Value('Mixed');
      selectPayload(value.mixed);
      value.mixed.payload.marker = 8;
      return value;
    case 'tags-write-tag-3':
      return Value('Tags')..tags.type = v.TagsType(3);
    case 'empty-write-tag-1':
      return Value('Empty')..empty.type = v.EmptyType(1);
  }
  return null;
}

// The writer's refusal is its debug assert, an AssertionError under
// --enable-asserts. In the compiled exe the generated writer has no write
// check by design, and the case is answered ok without writing the value.
void refuseWriteCase(String name, String type, String verdict) {
  if (notRepresentable.contains(name)) {
    if (verdict != 'refuse-write') {
      throw Failed(
        'named not representable in Dart, and its verdict is $verdict',
      );
    }
    return;
  }
  final value = buildRefuseWrite(name);
  if (value == null) throw Failed('unknown refuse-write case');
  if (value.type != type) {
    throw Failed('case type $type, the driver builds another');
  }
  if (!assertsOn()) return;
  try {
    write(value, ByteData.sublistView(Uint8List(64)));
  } on AssertionError {
    return;
  }
  throw Failed('write accepted');
}

// The select case: read mixed-ping with the public one-packet read, then
// select the payload arm on that value by setting its tag, as the Dart API
// does: the union keeps its payload object, constructed with its declared
// defaults, and the void arm read left it untouched.
void selectCase(String dir, String name, String type, String bitsColumn) {
  if (name != 'mixed-ping-select-payload' || type != 'Mixed') {
    throw Failed('unknown select case');
  }
  final bits = int.tryParse(bitsColumn);
  if (bits == null) throw Failed('bits column $bitsColumn');
  final from = load(dir, 'mixed-ping');
  final value = Value('Mixed');
  if (!read(value, ByteData.sublistView(from), from.length * 8)) {
    throw Failed('read mixed-ping refused');
  }
  if (value.mixed.type != v.MixedType.ping) {
    throw Failed('read mixed-ping: tag ${value.mixed.type}');
  }
  value.mixed.type = v.MixedType.payload;
  final out = written(value);
  if (out.length != (bits + 7) >> 3) {
    throw Failed('write has ${out.length} bytes, want ${(bits + 7) >> 3}');
  }
  if (!sameBytes(out, load(dir, name))) {
    throw Failed('write bytes differ from the pin');
  }
}

void main(List<String> args) {
  if (args.length != 1) {
    stderr.writeln('usage: main.dart <testdata/wire/packet-void>');
    exit(2);
  }
  // The generated surface beside the wire: tag extents, debug names and
  // maximum sizes. A miss exits non-zero, which tools/casegate holds red.
  if (v.MixedType.max != 2 ||
      v.TagsType.max != 2 ||
      v.SingleType.max != 1 ||
      v.EmptyType.max != 0 ||
      v.enumNameMixedType(v.MixedType.ping) != 'Ping' ||
      v.enumNameTagsType(v.TagsType.pong) != 'Pong' ||
      v.mixedMaxBits != 23 ||
      v.tagsMaxBits != 2 ||
      v.singleMaxBits != 1 ||
      v.emptyMaxBits != 0) {
    print('FAILED: packet-void tag extents, debug names or maximum bits');
    exit(1);
  }
  final dir = args[0];
  final List<String> lines;
  try {
    lines = File('$dir/cases.txt').readAsLinesSync();
  } on FileSystemException catch (e) {
    stderr.writeln('cannot open $dir/cases.txt: ${e.message}');
    exit(2);
  }
  for (final line in lines) {
    final fields = line.trim().split(RegExp(r'\s+'));
    final name = fields[0];
    if (name.isEmpty || name.startsWith('#')) continue;
    String? why;
    try {
      if (fields.length < 4) {
        throw Failed('a case line needs <name> <type> <verdict> <bits>');
      }
      switch (fields[2]) {
        case 'accept':
          acceptCase(dir, name, fields[1], fields[3]);
        case 'refuse':
          refuseCase(dir, name, fields[1]);
        case 'select':
          selectCase(dir, name, fields[1], fields[3]);
        case 'refuse-write':
          refuseWriteCase(name, fields[1], fields[2]);
        default:
          throw Failed('verdict ${fields[2]}');
      }
    } on Failed catch (e) {
      why = e.why;
    } catch (e) {
      why = 'threw $e';
    }
    print(why == null ? 'case $name ok' : 'case $name FAILED: $why');
  }
}
