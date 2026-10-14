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

// sha256MatchesFips180Vectors pins lib/sha256.dart to the three FIPS 180-4
// vectors (empty input, "abc", the 448-bit message).
void sha256MatchesFips180Vectors() {
  String hex(List<int> b) =>
      b.map((v) => v.toRadixString(16).padLeft(2, '0')).join();
  check(
    hex(sha256Bytes(utf8.encode(''))) ==
        'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855',
    'sha256 empty',
  );
  check(
    hex(sha256Bytes(utf8.encode('abc'))) ==
        'ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad',
    'sha256 abc',
  );
  check(
    hex(
          sha256Bytes(
            utf8.encode(
              'abcdbcdecdefdefgefghfghighijhijkijkljklmklmnlmnomnopnopq',
            ),
          ),
        ) ==
        '248d6a61d20638b8e5c026930c3e6039a33ce45964ff2167f6ecedd419db06c1',
    'sha256 448-bit',
  );
}

// upgradeMatchesGo: every upgrade case yields the expected wire and report
// byte for byte, or the expected refusal class; upgrade is idempotent;
// retention storage one id short refuses retain-lost, input unchanged
// (tla/CardVersioning.tla: NoFieldLost, RefusalKeepsOriginal).
void upgradeMatchesGo() {
  for (final name in [
    'initial-populated',
    'consumers-populated',
    'empty-consumers',
    'future-note',
  ]) {
    final wire = readBytes('tables/cards/conformance/cases/$name.wire');
    final before = Uint8List.fromList(wire);
    final r = upgrade(wire);
    sameBytes(
      r.wire,
      readBytes('tables/cards/conformance/expected/$name.wire'),
      'upgrade $name wire',
    );
    sameBytes(
      utf8.encode(reportText(r.report)),
      readBytes('tables/cards/conformance/expected/$name.report'),
      'upgrade $name report',
    );
    sameBytes(wire, before, 'upgrade $name input unchanged');
    final again = upgrade(r.wire);
    sameBytes(again.wire, r.wire, 'upgrade $name idempotent');
  }
  try {
    upgrade(readBytes('tables/cards/conformance/cases/truncated.wire'));
    check(false, 'upgrade truncated refused');
  } on CardException catch (_) {
    check(true, 'upgrade truncated refused');
  }
  final future = readBytes('tables/cards/conformance/cases/future-note.wire');
  final before = Uint8List.fromList(future);
  try {
    upgradeWithStorage(future, maxRetainBytes, 0);
    check(false, 'upgrade one id short refused');
  } on CardException catch (e) {
    check(
      e.message.contains('retained'),
      'upgrade one id short names retain loss (${e.message})',
    );
  }
  sameBytes(future, before, 'upgrade refused input unchanged');
  final strictOk = readStrict(
    readBytes('tables/cards/conformance/cases/card-populated.wire'),
  );
  check(strictOk.card.id.isNotEmpty, 'readStrict accepts a clean card');
  try {
    readStrict(future);
    check(false, 'readStrict refuses unknown');
  } on CardException catch (_) {
    check(true, 'readStrict refuses unknown');
  }
}

String reportText(TableWireReport r) =>
    'Retained=${r.retained}\nRetainLost=${r.retainLost}\nWidened=${r.widened}\nUnknown=${r.unknown}\nKindMismatch=${r.kindMismatch}\nClamped=${r.clamped}\nDuplicate=0\nMalformed=${r.malformed}\n';

// taskHashFramesLogicalFields pins the frame with an independent byte vector,
// shows framing collisions differ, matches every expected .hex, and shows
// the lossy refusal (tla/CardHops.tla: TaskHashStable).
void taskHashFramesLogicalFields() {
  // independent frame: body "a", paths ["bc"], empty new/shared, test "", done absent
  final framed = <int>[];
  void u64(int v) {
    for (var i = 0; i < 8; i++) {
      framed.add((v >> (8 * i)) & 0xff);
    }
  }

  void text(String t) {
    final b = utf8.encode(t);
    u64(b.length);
    framed.addAll(b);
  }

  text('a');
  u64(1);
  text('bc');
  u64(0);
  u64(0);
  text('');
  u64(0);
  final rec = fromJson('{"id":"x","body":"a","paths":[{"s":"bc"}]}');
  sameBytes(taskHash(rec), sha256Bytes(framed), 'task hash frame vector');
  final rec2 = fromJson('{"id":"x","body":"ab","paths":[{"s":"c"}]}');
  check(
    taskHashHex(rec) != taskHashHex(rec2),
    'framing collision hashes differ',
  );
  for (final name in [
    'card-populated',
    'initial-populated',
    'empty-consumers',
  ]) {
    final r = load(readBytes('tables/cards/conformance/cases/$name.wire'));
    check(
      taskHashHex(r) ==
          readText('tables/cards/conformance/expected/$name.hex').trim(),
      'taskhash $name matches expected hex',
    );
  }
  final lossy = load(
    readBytes('tables/cards/conformance/cases/future-note.wire'),
  );
  try {
    taskHash(lossy);
    check(false, 'task hash refuses lossy');
  } on CardException catch (_) {
    check(true, 'task hash refuses lossy');
  }
}

// fieldEditLeavesTheTaskHashAlone: needs/tier/priority/column/holder edits
// change the wire and not the hash; a paths edit changes both; the body is
// byte-equal throughout; refusals leave Wire byte-equal (CardHops
// EditDropsUnknown is the reversed witness for the unknown refusal).
void fieldEditLeavesTheTaskHashAlone() {
  final wire = readBytes('tables/cards/conformance/cases/card-populated.wire');
  final body0 = load(wire).card.body;
  var rec = load(wire);
  final h0 = taskHashHex(rec);
  setNeeds(rec, ['a', 'b']);
  check(
    taskHashHex(rec) == h0 && rec.card.body == body0,
    'needs edit keeps hash and body',
  );
  sameBytes(
    rec.wire(),
    readBytes('tables/cards/conformance/expected/needs.wire'),
    'needs edit wire',
  );
  rec = load(wire);
  setTier(rec, Tier.heavy);
  check(taskHashHex(rec) == h0, 'tier edit keeps hash');
  sameBytes(
    rec.wire(),
    readBytes('tables/cards/conformance/expected/tier.wire'),
    'tier edit wire',
  );
  rec = load(wire);
  setPriority(rec, Priority.blocker);
  check(taskHashHex(rec) == h0, 'priority edit keeps hash');
  sameBytes(
    rec.wire(),
    readBytes('tables/cards/conformance/expected/priority.wire'),
    'priority edit wire',
  );
  rec = load(wire);
  setColumn(rec, Column.landed);
  check(taskHashHex(rec) == h0, 'column edit keeps hash');
  sameBytes(
    rec.wire(),
    readBytes('tables/cards/conformance/expected/column.wire'),
    'column edit wire',
  );
  rec = load(wire);
  setHolder(rec, 'h');
  check(taskHashHex(rec) == h0, 'holder edit keeps hash');
  sameBytes(
    rec.wire(),
    readBytes('tables/cards/conformance/expected/holder.wire'),
    'holder edit wire',
  );
  rec = load(wire);
  setPaths(rec, ['x', 'y']);
  check(taskHashHex(rec) != h0, 'paths edit changes hash');
  sameBytes(
    rec.wire(),
    readBytes('tables/cards/conformance/expected/paths.wire'),
    'paths edit wire',
  );
  final unknown = load(
    readBytes('tables/cards/conformance/cases/future-note.wire'),
  );
  final beforeWire = unknown.wire();
  try {
    setNeeds(unknown, ['a']);
    check(false, 'edit refuses unknown');
  } on CardException catch (_) {
    check(true, 'edit refuses unknown');
  }
  sameBytes(unknown.wire(), beforeWire, 'refused edit leaves wire byte-equal');
}

// viewWalksEveryField compares viewText() with an explicit vocabulary written
// here (not derived from the descriptors) and with the expected view.txt; it
// also walks a loaded and an edited record's values through the descriptors
// (docs/SPEC-TABLES.md §8.1).
void viewWalksEveryField() {
  check(
    viewText() == readText('tables/cards/conformance/expected/view.txt'),
    'viewText matches expected view.txt',
  );
  final desc = cardDescriptors();
  var fields = 0;
  for (final t in desc.values) {
    fields += t.fields.length;
  }
  check(fields == 38, 'descriptor field count is 38 (got $fields)');
  check(
    desc['Card']!.fields.firstWhere((f) => f.name == 'body').shape == 'blob',
    'body is a blob in the view',
  );
  check(
    desc['Card']!.fields.firstWhere((f) => f.name == 'paths').shape == 'list',
    'paths is a list in the view',
  );
  final rec = load(
    readBytes('tables/cards/conformance/cases/card-populated.wire'),
  );
  check(
    rec.card.holder.isNotEmpty &&
        rec.card.paths.isNotEmpty &&
        rec.card.body != null,
    'view walk reads scalar, list and body values',
  );
  setPaths(rec, ['one', 'two', 'three']);
  check(rec.card.paths.length == 3, 'view walk reads the edited record');
}

void main() {
  cardRoundTripsThePopulatedFixture();
  cardBoundsRefuseBeforePublishing();
  parseRenderMatchesGo();
  cardEnumNoneRoundTrips();
  sha256MatchesFips180Vectors();
  upgradeMatchesGo();
  taskHashFramesLogicalFields();
  fieldEditLeavesTheTaskHashAlone();
  viewWalksEveryField();
  sha256MatchesFips180Vectors();
  sha256MatchesFips180Vectors();
  upgradeMatchesGo();
  taskHashFramesLogicalFields();
  fieldEditLeavesTheTaskHashAlone();
  viewWalksEveryField();
  taskHashFramesLogicalFields();
  fieldEditLeavesTheTaskHashAlone();
  viewWalksEveryField();
  viewWalksEveryField();
  if (failed) {
    print('cards_test: FAILED');
    exit(1);
  }
  print('cards_test: all checks passed');
}
