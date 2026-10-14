// cards_test.dart — the Dart card facade's test runner (docs/SPEC-TABLES.md §2,
// §6.2, §16). A plain `main` with one check per named test and a nonzero exit on
// any failure; no package:test dependency. The three tests are the ones the
// card's DONE-WHEN names:
//
//   cardRoundTripsThePopulatedFixture — Load/ToJSON/FromJSON/Wire reproduce the
//     committed fixture byte for byte, and the record's wire is a defensive copy.
//   cardBoundsRefuseBeforePublishing — the customer ceilings (MaxBodyBytes 65536,
//     MaxListEntries 512) refuse before any bytes are published.
//   parseRenderMatchesGo — Parse and Render match the Go leg's expected bytes on
//     the shared corpus, and the stranded-key case refuses with the key.
//   cardEnumNoneRoundTrips — an enum field at its zero value spells "None" in
//     JSON and reads back as zero with no loss event (SPEC-TABLES.md §16.2).
//
// Run from the repository root, which is where the Makefile runs it.

import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import '../lib/cards.dart';
import '../lib/sha256.dart';

var failed = false;

void check(bool ok, String what) {
  if (!ok) {
    print('FAILED: $what');
    failed = true;
  }
}

void sameBytes(List<int> got, List<int> want, String what) {
  if (got.length != want.length) {
    check(false, '$what: ${got.length} bytes, expected ${want.length}');
    return;
  }
  for (var i = 0; i < want.length; i++) {
    if (got[i] != want[i]) {
      check(
        false,
        '$what: first byte differing at $i (got ${got[i]}, want ${want[i]})',
      );
      return;
    }
  }
}

Uint8List readBytes(String path) => File(path).readAsBytesSync();

String readText(String path) => File(path).readAsStringSync();

// cardRoundTripsThePopulatedFixture loads tables/cards/fuzz/card-populated.wire,
// compares every field with card-populated.json, the body byte for byte, and
// proves ToJSON and FromJSON reproduce both files exactly; it also mutates the
// caller's input and a Wire result and shows the record unchanged.
void cardRoundTripsThePopulatedFixture() {
  final wire = readBytes('tables/cards/fuzz/card-populated.wire');
  final json = readText('tables/cards/fuzz/card-populated.json');

  final rec = load(wire);
  final card = rec.card;

  // every field against the JSON, and the body byte for byte.
  final obj = jsonDecode(json) as Map<String, dynamic>;
  check(card.id == obj['id'], 'id round-trips');
  check(card.sha == obj['sha'], 'sha round-trips');
  check(card.tier == Tier.flash, 'tier round-trips');
  check(card.repo == obj['repo'], 'repo round-trips');
  check(card.base == obj['_base'], 'base round-trips');
  check(card.kind == obj['kind'], 'kind round-trips');
  check(card.priority == Priority.critical, 'priority round-trips');
  check(card.stream == obj['stream'], 'stream round-trips');
  check(
    card.paths.length == 2 &&
        card.paths[0].s == 'path/one' &&
        card.paths[1].s == 'path/two',
    'paths round-trip',
  );
  check(
    card.new_.length == 1 && card.new_[0].s == 'new/path',
    'new round-trips',
  );
  check(
    card.shared.length == 1 && card.shared[0].s == 'shared/path',
    'shared round-trips',
  );
  check(
    card.needs.length == 2 &&
        card.needs[0].s == 'need-one' &&
        card.needs[1].s == 'need-two',
    'needs round-trip',
  );
  check(card.test == obj['test'], 'test round-trips');
  check(card.done == obj['done_when'], 'done_when round-trips');
  check(card.deadlineMinutes == 45, 'deadline_minutes round-trips');
  check(card.source == obj['source'], 'source round-trips');
  check(card.epoch == 7, 'epoch round-trips');
  check(card.column == Column.review, 'column round-trips');
  check(card.holder == obj['holder'], 'holder round-trips');
  check(card.attempt == 1, 'attempt round-trips');
  check(card.head == obj['head'], 'head round-trips');
  check(card.branch == obj['branch'], 'branch round-trips');
  check(card.body == obj['body'], 'body round-trips');
  sameBytes(
    utf8.encode(card.body!),
    utf8.encode(obj['body'] as String),
    'body byte for byte',
  );

  // ToJSON reproduces the JSON file exactly.
  sameBytes(
    utf8.encode(toJson(rec)),
    utf8.encode(json),
    'ToJSON reproduces card-populated.json',
  );

  // FromJSON reproduces the wire file exactly.
  final rec2 = fromJson(json);
  sameBytes(rec2.wire(), wire, 'FromJSON reproduces card-populated.wire');

  // FromJSON refuses a JSON text that is not an object, as the Go facade does.
  try {
    fromJson('[]');
    check(false, 'FromJSON refuses a non-object JSON text');
  } on CardException catch (e) {
    check(
      e.message.contains('malformed'),
      'FromJSON refuses a non-object JSON text as malformed (${e.message})',
    );
  }

  // the record owns a defensive copy: mutating the caller's input and a Wire
  // result leaves the record unchanged.
  final caller = Uint8List.fromList(wire);
  final rec3 = load(caller);
  caller[0] ^= 0xff;
  sameBytes(
    rec3.wire(),
    wire,
    'record unchanged after the caller mutates its input',
  );

  final w = rec3.wire();
  w[0] ^= 0xff;
  sameBytes(
    rec3.wire(),
    wire,
    'record unchanged after a Wire result is mutated',
  );
}

// cardBoundsRefuseBeforePublishing proves the customer ceilings refuse before
// any bytes are published: 65536 body bytes accepted, 65537 refused naming body
// and 65536; 512 entries accepted and 513 refused naming the field, for each
// collection (docs/SPEC-TABLES.md §2.5, §2.9).
void cardBoundsRefuseBeforePublishing() {
  // body: 65536 accepted, 65537 refused.
  final okBody = '{"id":"x","body":"${'y' * 65536}"}';
  fromJson(okBody);
  check(true, '65536 body bytes accepted');

  final overBody = '{"id":"x","body":"${'y' * 65537}"}';
  try {
    fromJson(overBody);
    check(false, '65537 body bytes refused');
  } on CardException catch (e) {
    check(
      e.message.contains('body') && e.message.contains('65536'),
      '65537 body bytes refused naming body and 65536 (${e.message})',
    );
  }

  // each collection: 512 accepted, 513 refused naming the field.
  final collections = <String, String>{
    'paths': '{"s":"p"}',
    '_new': '{"s":"n"}',
    'shared': '{"s":"s"}',
    'needs': '{"s":"d"}',
    'extra': '{"key":"k"}',
    'consumers': '{"kind":"k"}',
  };
  collections.forEach((field, elem) {
    final ok = '{"id":"x","$field":[${List.filled(512, elem).join(',')}]}';
    fromJson(ok);
    check(true, '$field 512 entries accepted');

    final over = '{"id":"x","$field":[${List.filled(513, elem).join(',')}]}';
    try {
      fromJson(over);
      check(false, '$field 513 entries refused');
    } on CardException catch (e) {
      check(
        e.message.contains(field) && e.message.contains('512'),
        '$field 513 entries refused naming the field and 512 (${e.message})',
      );
    }
  });
}

// parseRenderMatchesGo proves Parse and Render match the Go leg's expected bytes
// on the shared corpus: every parse case parses to the expected wire, the
// rejected case refuses with the stranded key, and every render case renders to
// the expected text byte for byte (tables/cards/conformance).
void parseRenderMatchesGo() {
  // parse: normalized-valid parses to the expected wire.
  final text = readText('tables/cards/conformance/cases/normalized-valid.md');
  final rec = parse(text);
  sameBytes(
    rec.wire(),
    readBytes('tables/cards/conformance/expected/normalized-valid.wire'),
    'normalized-valid parses to the expected wire',
  );

  // parse: rejected-legacy refuses with the stranded key PRIORITY.
  final legacy = readText('tables/cards/conformance/cases/rejected-legacy.md');
  try {
    parse(legacy);
    check(false, 'rejected-legacy refuses with the stranded key');
  } on CardException catch (e) {
    check(
      e.message.contains('stranded header key') &&
          e.message.contains('PRIORITY'),
      'rejected-legacy refuses with the stranded key PRIORITY (${e.message})',
    );
  }

  // render: every render case renders to the expected text byte for byte.
  final renders = <String, String>{
    'card-populated': 'card-populated',
    'initial-populated': 'initial-populated',
    'consumers-populated': 'consumers-populated',
    'empty-consumers': 'empty-consumers',
  };
  renders.forEach((name, _) {
    final wire = readBytes('tables/cards/conformance/cases/$name.wire');
    final rec2 = load(wire);
    sameBytes(
      utf8.encode(render(rec2)),
      readBytes('tables/cards/conformance/expected/$name.txt'),
      '$name renders to the expected text',
    );
  });
}

// cardEnumNoneRoundTrips proves a card whose enum fields sit at their zero
// value round-trips through the JSON text form: ToJSON spells the value
// "None" instead of refusing, and FromJSON reads "None" back as zero with no
// loss event, so the facade publishes it (docs/SPEC-TABLES.md §16.2).
void cardEnumNoneRoundTrips() {
  final rec = fromJson('{"id":"x"}');
  check(rec.card.priority == 0, 'default priority is zero');
  final text = toJson(rec);
  check(
    text.contains('"priority": "None"'),
    'ToJSON spells enum zero as None ($text)',
  );

  final rec2 = fromJson('{"id":"x","priority":"None","tier":"None"}');
  check(rec2.card.priority == 0, 'FromJSON reads None as zero');
  check(rec2.card.tier == 0, 'FromJSON reads tier None as zero');
}

String hex(List<int> bytes) {
  final b = StringBuffer();
  for (final x in bytes) {
    b.write(x.toRadixString(16).padLeft(2, '0'));
  }
  return b.toString();
}

// sha256MatchesFips180Vectors pins the facade's SHA-256 to the three published
// FIPS 180-4 vectors (empty input, "abc", and the 448-bit message), the
// independent ground truth a correct implementation must reproduce.
void sha256MatchesFips180Vectors() {
  check(
    hex(digestOf(utf8.encode(''))) ==
        'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855',
    'SHA-256 of the empty string',
  );
  check(
    hex(digestOf(utf8.encode('abc'))) ==
        'ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad',
    'SHA-256 of "abc"',
  );
  check(
    hex(
          digestOf(
            utf8.encode(
              'abcdbcdecdefdefgefghfghighijhijkijkljklmklmnlmnomnopnopq',
            ),
          ),
        ) ==
        '248d6a61d20638b8e5c026930c3e6039a33ce45964ff2167f6ecedd419db06c1',
    'SHA-256 of the 448-bit message',
  );
}

// taskHashFramesLogicalFields pins the hash frame (docs/SPEC-TABLES.md §5) with
// an independently written byte vector, shows body "a" with paths ["bc"] and
// body "ab" with paths ["c"] hash differently, matches every expected .hex, and
// shows the lossy refusal (tables/cards/edit.go TaskHash).
void taskHashFramesLogicalFields() {
  final base = fromJson(
    '{"id":"x","body":"a","paths":[{"s":"bc"}],"test":"T","done_when":"d"}',
  );
  check(
    hex(taskHash(base)) ==
        'bc36e53437b6d6a9364abce0cbe34ba3ceb9d985c01adaeed1ff4344a54f95cf',
    'the frame of body "a", paths ["bc"], test "T", done "d"',
  );

  final shifted = fromJson(
    '{"id":"x","body":"ab","paths":[{"s":"c"}],"test":"T","done_when":"d"}',
  );
  check(
    hex(taskHash(shifted)) ==
        '9945767371c71f3f3d6c2171fd5350236f4cdaac4358005d4fa4613318789c72',
    'the frame of body "ab", paths ["c"], test "T", done "d"',
  );
  check(
    hex(taskHash(shifted)) != hex(taskHash(base)),
    'body "a" paths ["bc"] and body "ab" paths ["c"] hash differently',
  );

  for (final name in [
    'card-populated',
    'initial-populated',
    'empty-consumers',
  ]) {
    final rec = load(readBytes('tables/cards/conformance/cases/$name.wire'));
    final want = readText('tables/cards/conformance/expected/$name.hex').trim();
    check(hex(taskHash(rec)) == want, '$name matches its expected .hex');
  }

  final lossy = load(
    readBytes('tables/cards/conformance/cases/future-note.wire'),
  );
  try {
    taskHash(lossy);
    check(false, 'a lossy record is refused');
  } on CardException catch (e) {
    check(
      e.message.contains('loss events'),
      'a lossy record is refused, never hashed (${e.message})',
    );
  }
}

// viewWalksEveryField proves the view is the vocabulary (docs/SPEC-TABLES.md
// §8, §8.1, FORMAT.md "The view"): it compares view() byte for byte with the Go
// leg's expected view.txt, and with an explicit vocabulary written here (not
// read from the descriptors), so a descriptor edit that changes the view is
// caught against independent ground truth.
void viewWalksEveryField() {
  sameBytes(
    utf8.encode(view()),
    readBytes('tables/cards/conformance/expected/view.txt'),
    'view matches the expected view.txt',
  );

  const vocabulary = <String>[
    'Card id string value 64',
    'Card sha string value 40',
    'Card tier Tier value -',
    'Card repo string value 96',
    'Card _base string value 96',
    'Card kind string value 24',
    'Card priority Priority value -',
    'Card stream string value 32',
    'Card paths PathText list -',
    'Card _new PathText list -',
    'Card shared PathText list -',
    'Card needs CardID list -',
    'Card test string value 192',
    'Card done string blob -',
    'Card deadline_minutes int32 value -',
    'Card source string blob -',
    'Card epoch int32 value -',
    'Card column Column value -',
    'Card holder string value 32',
    'Card attempt int32 value -',
    'Card head string value 40',
    'Card branch string value 128',
    'Card body string blob -',
    'Card extra KeyValue list -',
    'Card consumers Consumer list -',
    'CardID s string value 64',
    'Consumer kind string value 16',
    'Consumer who string value 32',
    'Consumer route string value 48',
    'Consumer model string value 64',
    'Consumer started int64 value -',
    'Consumer ended int64 value -',
    'Consumer outcome string value 16',
    'Consumer line string value 200',
    'Consumer cost_micro_usd int64 value -',
    'KeyValue key string value 32',
    'KeyValue value string blob -',
    'PathText s string value 192',
  ];
  final lines = view().split('\n').where((l) => l.isNotEmpty).toList();
  check(
    lines.length == vocabulary.length,
    'the view has ${vocabulary.length} field lines',
  );
  for (var i = 0; i < vocabulary.length && i < lines.length; i++) {
    check(lines[i] == vocabulary[i], 'view line ${i + 1} "${vocabulary[i]}"');
  }
}

void main() {
  sha256MatchesFips180Vectors();
  taskHashFramesLogicalFields();
  viewWalksEveryField();
  cardRoundTripsThePopulatedFixture();
  cardBoundsRefuseBeforePublishing();
  parseRenderMatchesGo();
  cardEnumNoneRoundTrips();
  if (failed) {
    print('cards_test: FAILED');
    exit(1);
  }
  print('cards_test: all checks passed');
}
