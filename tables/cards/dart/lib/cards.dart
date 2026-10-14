// cards.dart — the Dart owning facade for a typed work card (docs/SPEC-TABLES.md
// §2). It is the Dart spelling of the Go facade tables/cards/cards.go, parse.go
// and render.go: a Record that keeps its own defensive copy of the wire, checked
// writes that refuse before publishing, the two customer ceilings the schema
// deliberately leaves unbounded (§2.5, §2.9), and the brief text parsed once and
// rendered many times. The generated Card codec (CardsWire.dart) answers the wire
// and text forms; this facade is the thin owning layer a customer imports.
//
// The Go facade's calls, in Dart's own spelling:
//   Load(wire)            -> load(Uint8List) -> Record
//   FromJSON(text)        -> fromJson(String) -> Record
//   ToJSON(record)        -> toJson(Record) -> String
//   (r *Record) Wire()    -> Record.wire() -> Uint8List
//   Parse(text)           -> parse(String) -> Record
//   Render(record)        -> render(Record) -> String
// A Go error is a thrown CardException here; a Go Report rides on the Record.

import 'dart:convert';
import 'dart:typed_data';

import '../../../../build/tables-generated-dart/cards/Card.dart';
import '../../../../build/tables-generated-dart/cards/CardsWire.dart';
import 'sha256.dart';

// The generated types ride through the facade, the way the Go facade re-exports
// `type Report = TableReport`: a customer imports cards.dart and gets the Card,
// the enums and the report in one place.
export '../../../../build/tables-generated-dart/cards/Card.dart';
export '../../../../build/tables-generated-dart/cards/CardsWire.dart';

// MaxBodyBytes is the customer ceiling on one card's opaque body. The schema
// declares body *string with no bound (docs/SPEC-TABLES.md §2.5); this facade
// refuses a larger body before any bytes are published.
const int maxBodyBytes = 64 << 10;

// MaxListEntries is the customer ceiling on each of a card's collection fields
// (paths/new/shared/needs/extra/consumers), which the schema leaves unbounded
// (docs/SPEC-TABLES.md §2.9).
const int maxListEntries = 512;

// CardException is the facade's error: a refused read or write, carrying the
// reason text a driver maps to a refusal class (tables/cards/conformance/FORMAT.md).
class CardException implements Exception {
  final String message;
  CardException(this.message);
  @override
  String toString() => message;
}

// Record owns a private defensive copy of the wire and the loaded Card. No
// shallow copy of the caller's bytes escapes: the wire field is the caller's
// bytes copied, and wire() returns a fresh copy (docs/SPEC-TABLES.md §6.2).
class Record {
  final Uint8List _wire;
  final Card _card;
  final TableWireReport _report;

  Record._(this._wire, this._card, this._report);

  // wire returns a defensive copy of the record's wire bytes.
  Uint8List wire() => Uint8List.fromList(_wire);

  // card is the loaded card; the record owns it for its whole life.
  Card get card => _card;

  // report is the tolerant read report the load filled.
  TableWireReport get report => _report;
}

// load reads wire into a Record using the generated tolerant load (§6.2).
// Failures and malformed wire throw; tolerant events ride in the returned
// Record's report. The ORIGINAL wire is copied, never rewritten, so an old
// input is preserved byte for byte.
Record load(Uint8List wire) {
  final report = TableWireReport();
  final n = cardWireMeasure(wire);
  if (n < 0) {
    cardWireLoad(wire, report);
    throw CardException('cards: wire is not a card');
  }
  final card = cardWireLoad(wire, report);
  if (report.malformed || report.refused) {
    throw CardException('cards: wire is malformed');
  }
  checkCard(card);
  return Record._(Uint8List.fromList(wire), card, report);
}

// fromJson builds a Record from JSON text through the generated text read and
// the generated save (§16). Unknown, mismatched, clamped and malformed inputs
// are refused, as are the customer bounds. Bytes are published only on complete
// success; a refused write leaves no destination and mutates nothing.
Record fromJson(String text) {
  final report = TableTextReport();
  Card card;
  try {
    final root = jsonDecode(text);
    if (root is! Map<String, dynamic>) {
      throw CardException('cards: JSON is malformed');
    }
    card = cardFromJson(text, report);
  } on FormatException {
    throw CardException('cards: JSON is malformed');
  }
  if (report.unknown > 0 || report.kindMismatch > 0 || report.clamped > 0) {
    throw CardException(
      'cards: JSON has loss events (unknown=${report.unknown} '
      'kindMismatch=${report.kindMismatch} clamped=${report.clamped}): '
      'refused, never truncated',
    );
  }
  checkCard(card);
  final wire = cardWireSave(card);
  return Record._(wire, card, TableWireReport());
}

// toJson writes the record's known-field JSON text (§16). It is the known-field
// view, not a lossless archival rewrite.
String toJson(Record record) {
  return cardToJson(record.card);
}

// render emits the admitted brief text: RESULT first, the mapped keys in a fixed
// documented order, extras in stored order, then the body bytes verbatim (§6.2,
// §16). It is a known-field view, never an archival/machine-state rewrite.
String render(Record record) {
  final card = record.card;
  final out = StringBuffer();
  out.write('RESULT: ');
  out.write(card.id);
  out.write(' sha=');
  out.write(card.sha);
  out.write(' tier: ');
  out.write(tierString(card.tier));
  out.write('\n');

  emitStr(out, 'REPO', card.repo);
  emitStr(out, 'BASE', card.base);
  emitStr(out, 'KIND', card.kind);
  final prio = priorityString(card.priority);
  if (prio != '') {
    out.write('PRIORITY: ');
    out.write(prio);
    out.write('\n');
  }
  emitList(out, 'PATHS', card.paths.map((e) => e.s).toList());
  emitList(out, 'NEW', card.new_.map((e) => e.s).toList());
  emitList(out, 'SHARED', card.shared.map((e) => e.s).toList());
  emitList(out, 'DEPENDS-ON', card.needs.map((e) => e.s).toList());
  emitStr(out, 'TEST', card.test);
  emitPtr(out, 'DONE-WHEN', card.done);
  emitDeadline(out, card.deadlineMinutes);
  emitPtr(out, 'SOURCE', card.source);

  for (final e in card.extra) {
    out.write(e.key);
    out.write(': ');
    if (e.value != null) {
      out.write(e.value);
    }
    out.write('\n');
  }

  if (card.body != null) {
    out.write(card.body);
  }
  return out.toString();
}

// ---- the task hash (tables/cards/edit.go, TaskHash) ----

// taskHash returns the digest of a record's TASK: body, paths, new, shared,
// test and done, in that order (docs/SPEC-TABLES.md §5). It refuses a record
// whose read reported loss (unknown, mismatched, clamped, retain-lost or
// malformed), because the "task" such a digest would cover is incomplete; the
// attempt-bound rule keeps an apparently valid digest from covering silent
// loss (tla/CardHops.tla TaskHashStable).
Uint8List taskHash(Record record) {
  refuseLossy(record.report);
  final card = record.card;
  final h = <int>[];
  hashText(h, card.body ?? '');
  hashPathTextList(h, card.paths);
  hashPathTextList(h, card.new_);
  hashPathTextList(h, card.shared);
  hashText(h, card.test);
  hashText(h, card.done ?? '');
  return Uint8List.fromList(digestOf(h));
}

// refuseLossy names the read events that make a record unsafe to hash or
// rewrite (docs/SPEC-TABLES.md §4, §6.6): a silent loss means the task the
// digest would cover is incomplete.
void refuseLossy(TableWireReport report) {
  if (report.unknown > 0 ||
      report.kindMismatch > 0 ||
      report.clamped > 0 ||
      report.retainLost > 0 ||
      report.malformed) {
    throw CardException(
      'cards: record has loss events (unknown=${report.unknown} '
      'kindMismatch=${report.kindMismatch} clamped=${report.clamped} '
      'retainLost=${report.retainLost} malformed=${report.malformed}): '
      'refused, never hashed or rewritten',
    );
  }
}

// hashU64 appends an unsigned 64-bit little-endian value (docs/SPEC-TABLES.md
// §5: each frame length and list count is fixed-width little-endian).
void hashU64(List<int> out, int v) {
  for (var i = 0; i < 8; i++) {
    out.add((v >>> (8 * i)) & 0xff);
  }
}

// hashText appends one length-prefixed UTF-8 text, so text and element
// boundaries cannot collide (docs/SPEC-TABLES.md §5).
void hashText(List<int> out, String text) {
  final bytes = utf8.encode(text);
  hashU64(out, bytes.length);
  out.addAll(bytes);
}

// hashPathTextList appends one list as a count then each length-prefixed
// element (docs/SPEC-TABLES.md §5).
void hashPathTextList(List<int> out, List<PathText> list) {
  hashU64(out, list.length);
  for (final e in list) {
    hashText(out, e.s);
  }
}

// checkCard is the checked-write helper: the customer bounds and the
// well-formedness of the opaque body and every collection element, enforced
// before bytes are published (docs/SPEC-TABLES.md §2.5, §2.9).
void checkCard(Card card) {
  if (card.body != null) {
    final body = card.body!;
    if (utf8.encode(body).length > maxBodyBytes) {
      throw CardException(
        'cards: body is ${utf8.encode(body).length} bytes, past the '
        '$maxBodyBytes-byte ceiling',
      );
    }
    if (body.contains('\u0000')) {
      throw CardException('cards: body is not well-formed UTF-8 without NUL');
    }
  }
  checkPathList(card.paths, 'paths');
  checkPathList(card.new_, '_new');
  checkPathList(card.shared, 'shared');
  checkIDList(card.needs, 'needs');
  checkExtras(card.extra);
  checkConsumers(card.consumers);
}

void checkPathList(List<PathText> list, String name) {
  if (list.length > maxListEntries) {
    throw CardException(
      'cards: $name has ${list.length} entries, past the '
      '$maxListEntries-entry ceiling',
    );
  }
  for (final e in list) {
    if (e.s.contains('\u0000')) {
      throw CardException(
        'cards: $name has an element that is not well-formed UTF-8 without NUL',
      );
    }
  }
}

void checkIDList(List<CardID> list, String name) {
  if (list.length > maxListEntries) {
    throw CardException(
      'cards: $name has ${list.length} entries, past the '
      '$maxListEntries-entry ceiling',
    );
  }
  for (final e in list) {
    if (e.s.contains('\u0000')) {
      throw CardException(
        'cards: $name has an element that is not well-formed UTF-8 without NUL',
      );
    }
  }
}

// checkExtras enforces the extras rules before any facade write publishes: the
// 512-entry ceiling shared with the other collections, the admitted header key
// grammar, no CR or LF in a key or value, well-formed UTF-8 keys and values
// without NUL, and no key duplicating a mapped header name (HEADER MAPPING,
// docs/SPEC-TABLES.md §2.9).
void checkExtras(List<KeyValue> list) {
  if (list.length > maxListEntries) {
    throw CardException(
      'cards: extra has ${list.length} entries, past the '
      '$maxListEntries-entry ceiling',
    );
  }
  for (final e in list) {
    final k = e.key;
    if (k.contains('\u0000')) {
      throw CardException(
        'cards: extra has a key that is not well-formed UTF-8 without NUL',
      );
    }
    if (!headerKeyRE.hasMatch(k)) {
      throw CardException(
        'cards: extra key "$k" is not an admitted header key',
      );
    }
    if (k.contains('\r') || k.contains('\n')) {
      throw CardException('cards: extra has a key with a CR or LF');
    }
    if (reservedHeaderKeys.contains(k)) {
      throw CardException(
        'cards: extra key "$k" duplicates a mapped header key',
      );
    }
    if (e.value != null) {
      final v = e.value!;
      if (v.contains('\u0000')) {
        throw CardException(
          'cards: extra has a value that is not well-formed UTF-8 without NUL',
        );
      }
      if (v.contains('\r') || v.contains('\n')) {
        throw CardException('cards: extra has a value with a CR or LF');
      }
    }
  }
}

// checkConsumers enforces the consumers collection ceiling and the
// well-formedness of every consumer string, using the same rules as the other
// collections (docs/SPEC-TABLES.md §2.9).
void checkConsumers(List<Consumer> list) {
  if (list.length > maxListEntries) {
    throw CardException(
      'cards: consumers has ${list.length} entries, past the '
      '$maxListEntries-entry ceiling',
    );
  }
  for (final e in list) {
    for (final f in [e.kind, e.who, e.route, e.model, e.outcome, e.line]) {
      if (f.contains('\u0000')) {
        throw CardException(
          'cards: consumers has a field that is not well-formed UTF-8 without NUL',
        );
      }
    }
  }
}

// ---- the brief text, parsed once (tables/cards/parse.go) ----

// headerLineRE is the admitted header grammar: a key of one letter then
// letters/digits/hyphens, a colon, optional whitespace, and the rest as the
// value. Anchored at the start, so leading whitespace is not a key.
final RegExp headerLineRE = RegExp(r'^([A-Za-z][A-Za-z0-9-]*):\s*(.*)$');

// headerKeyRE is the admitted key grammar alone: one letter then
// letters/digits/hyphens, the same key rule headerLineRE applies.
final RegExp headerKeyRE = RegExp(r'^[A-Za-z][A-Za-z0-9-]*$');

// deadlineRE is the customer spelling DEADLINE accepts: "finish within N
// minutes." with a checked conversion into deadline_minutes.
final RegExp deadlineRE = RegExp(r'^finish within ([0-9]+) minutes\.$');

// strandedKeys is the customer's exact case-sensitive set refused once prose
// has begun (nova-sprint pkg/swarm/lintheader.go cardHeaderKeys). Other
// KEY-shaped body lines, such as NOTE:, are opaque and preserved.
const Set<String> strandedKeys = {
  'KIND',
  'PATHS',
  'TEST',
  'LEGS',
  'SOURCE',
  'START',
  'STOP',
  'DEPENDS-ON',
  'PRIORITY',
  'NEW',
  'SHARED',
  'REPO',
  'BASE',
  'tier',
  'RESULT',
};

// reservedHeaderKeys is the mapped header vocabulary, including RESULT and the
// agreeing tier line. An extra must not duplicate one of these names.
const Set<String> reservedHeaderKeys = {
  'RESULT',
  'tier',
  'REPO',
  'BASE',
  'KIND',
  'PRIORITY',
  'PATHS',
  'NEW',
  'SHARED',
  'DEPENDS-ON',
  'TEST',
  'DONE-WHEN',
  'DEADLINE',
  'SOURCE',
};

// kv is one extra header entry, kept in arrival order.
class Kv {
  final String key;
  final String value;
  Kv(this.key, this.value);
}

// parsedCard is the intermediate, temporary result of reading the header lines
// before any generated storage exists.
class ParsedCard {
  String id = '';
  String sha = '';
  int tier = 0;
  String repo = '';
  String base = '';
  String kind = '';
  int priority = 0;
  String stream = '';
  List<String> paths = <String>[];
  List<String> news = <String>[];
  List<String> shared = <String>[];
  List<String> needs = <String>[];
  String test = '';
  String doneWhen = '';
  String source = '';
  int deadline = 0;
  List<Kv> extras = <Kv>[];
}

// lineRef is one physical line: its content without the LF/CRLF terminator and
// the character offset just past its terminator (where the body could begin).
class LineRef {
  final String content;
  final int termEnd;
  final int num;
  LineRef(this.content, this.termEnd, this.num);
}

// parse reads admitted brief text and publishes a typed Record only on complete
// checked success (docs/SPEC-TABLES.md §2, §6.2). The body is the exact suffix
// immediately after the last header line's terminator; header layout may
// canonicalize, body bytes never do.
Record parse(String text) {
  if (text.contains('\u0000')) {
    throw CardException('cards: brief contains a NUL byte');
  }

  final lines = splitLines(text);
  if (lines.isEmpty) {
    throw CardException('cards: brief has no RESULT line');
  }

  final m = matchHeader(lines[0].content);
  if (m == null || m.key != 'RESULT') {
    throw CardException('cards: line 1 is not a RESULT line');
  }
  final p = ParsedCard();
  applyResultOn(p, m.value);

  var lastHeaderEnd = lines[0].termEnd;
  final seen = <String, int>{'RESULT': 1};
  var prose = false;
  for (final ln in lines.skip(1)) {
    if (isBlank(ln.content)) {
      continue;
    }
    final m2 = matchHeader(ln.content);
    if (m2 == null) {
      prose = true;
      continue;
    }
    if (prose) {
      if (strandedKeys.contains(m2.key)) {
        throw CardException(
          'cards: stranded header key "${m2.key}" on line ${ln.num}',
        );
      }
      continue;
    }
    applyHeader(p, m2.key, m2.value, ln.num, seen);
    lastHeaderEnd = ln.termEnd;
  }

  final body = text.substring(lastHeaderEnd);
  return buildOn(p, body);
}

// splitLines returns every physical line with its content and its terminator
// end offset. A trailing LF does not invent a final empty line.
List<LineRef> splitLines(String text) {
  final refs = <LineRef>[];
  var i = 0;
  var n = 1;
  while (i < text.length) {
    final start = i;
    final j = text.indexOf('\n', start);
    var lineEnd = text.length;
    var termEnd = text.length;
    if (j >= 0) {
      lineEnd = j;
      termEnd = j + 1;
    }
    var content = text.substring(start, lineEnd);
    if (content.isNotEmpty && content.endsWith('\r')) {
      content = content.substring(0, content.length - 1);
    }
    refs.add(LineRef(content, termEnd, n));
    n++;
    i = termEnd;
  }
  return refs;
}

// matchHeader recognizes a header key line; null for prose, blank (handled by
// the caller) or any line with leading whitespace.
HeaderMatch? matchHeader(String line) {
  final m = headerLineRE.firstMatch(line);
  if (m == null) {
    return null;
  }
  return HeaderMatch(m.group(1)!, m.group(2)!);
}

class HeaderMatch {
  final String key;
  final String value;
  HeaderMatch(this.key, this.value);
}

// applyResult parses the RESULT value "id sha=<sha> tier: <tier>" and validates
// each part (docs/SPEC-TABLES.md §2, §4).
void applyResultOn(ParsedCard p, String value) {
  final i = value.lastIndexOf(' tier:');
  if (i < 0) {
    throw CardException('cards: RESULT line has no " tier:"');
  }
  final tierStr = value.substring(i + ' tier:'.length).trim();
  final rest = value.substring(0, i);
  final j = rest.lastIndexOf(' sha=');
  if (j < 0) {
    throw CardException('cards: RESULT line has no " sha="');
  }
  final idStr = rest.substring(0, j).trim();
  final shaStr = rest.substring(j + ' sha='.length);
  checkStrBound('id', idStr, 64);
  if (!isHex(shaStr) || (shaStr.length != 12 && shaStr.length != 40)) {
    throw CardException(
      'cards: RESULT sha "$shaStr" is not 12 or 40 hex digits',
    );
  }
  p.id = idStr;
  p.sha = shaStr;
  p.tier = parseTier(tierStr);
}

// applyHeader dispatches one header line to its field, an agreeing tier, or an
// extra, refusing duplicates and CR/LF inside any single-line value.
void applyHeader(
  ParsedCard p,
  String key,
  String value,
  int num,
  Map<String, int> seen,
) {
  if (value.contains('\r') || value.contains('\n')) {
    throw CardException('cards: $key on line $num has a CR or LF in its value');
  }
  if (key == 'tier') {
    if (seen.containsKey('tier')) {
      throw CardException(
        'cards: duplicate header key "$key" on lines ${seen['tier']} and $num',
      );
    }
    seen['tier'] = num;
    final t = parseTier(value.trim());
    if (t != p.tier) {
      throw CardException('cards: tier on line $num disagrees with RESULT');
    }
    return;
  }
  if (key == 'RESULT') {
    throw CardException(
      'cards: duplicate header key "$key" on lines ${seen['RESULT']} and $num',
    );
  }
  if (reservedHeaderKeys.contains(key)) {
    if (seen.containsKey(key)) {
      throw CardException(
        'cards: duplicate header key "$key" on lines ${seen[key]} and $num',
      );
    }
    seen[key] = num;
    assignMapped(p, key, value, num);
    return;
  }
  checkStrBound('extra key', key, 32);
  p.extras.add(Kv(key, value));
}

// assignMapped fills one mapped header field with its checked value.
void assignMapped(ParsedCard p, String key, String value, int num) {
  switch (key) {
    case 'REPO':
      checkStrBound('REPO', value, 96);
      p.repo = value;
      break;
    case 'BASE':
      checkStrBound('BASE', value, 96);
      p.base = value;
      break;
    case 'KIND':
      checkStrBound('KIND', value, 24);
      p.kind = value;
      break;
    case 'PRIORITY':
      p.priority = parsePriority(value.trim());
      break;
    case 'PATHS':
      p.paths = splitListChecked(value, 192, true, 'PATHS', num);
      break;
    case 'NEW':
      p.news = splitListChecked(value, 192, false, 'NEW', num);
      break;
    case 'SHARED':
      p.shared = splitListChecked(value, 192, false, 'SHARED', num);
      break;
    case 'DEPENDS-ON':
      p.needs = splitListChecked(value, 64, false, 'DEPENDS-ON', num);
      break;
    case 'TEST':
      checkStrBound('TEST', value, 192);
      p.test = value;
      break;
    case 'DONE-WHEN':
      p.doneWhen = value;
      break;
    case 'DEADLINE':
      p.deadline = parseDeadline(value);
      break;
    case 'SOURCE':
      p.source = value;
      break;
  }
}

void checkStrBound(String name, String value, int bound) {
  if (value.length > bound) {
    throw CardException(
      'cards: $name is ${value.length} bytes, past the $bound-byte bound',
    );
  }
}

// splitListChecked splits a comma-separated list, trims elements, drops empty
// ones, and enforces the element bound and the entry ceiling (docs/SPEC-TABLES.md
// §2.9). A whole value of "-" or empty is an empty list; PATHS alone also takes
// the customer sentinel "none".
List<String> splitListChecked(
  String value,
  int bound,
  bool noneSentinel,
  String name,
  int num,
) {
  final v = value.trim();
  if (v == '' || v == '-') {
    return <String>[];
  }
  if (noneSentinel && v == 'none') {
    return <String>[];
  }
  final parts = v.split(',');
  final out = <String>[];
  for (var part in parts) {
    part = part.trim();
    if (part == '') {
      continue;
    }
    checkStrBound('$name element', part, bound);
    out.add(part);
  }
  if (out.length > maxListEntries) {
    throw CardException(
      'cards: $name on line $num has ${out.length} entries, past the '
      '$maxListEntries-entry ceiling',
    );
  }
  return out;
}

int parseTier(String s) {
  switch (s) {
    case 'flash':
      return Tier.flash;
    case 'pro':
      return Tier.pro;
    case 'heavy':
      return Tier.heavy;
    case 'frontier':
      return Tier.frontier;
  }
  throw CardException('cards: unknown tier "$s"');
}

int parsePriority(String s) {
  switch (s) {
    case 'normal':
      return Priority.normal;
    case 'high':
      return Priority.high;
    case 'critical':
      return Priority.critical;
    case 'blocker':
      return Priority.blocker;
  }
  throw CardException('cards: unknown priority "$s"');
}

int parseDeadline(String s) {
  final m = deadlineRE.firstMatch(s.trim());
  if (m == null) {
    throw CardException('cards: not of the form "finish within N minutes."');
  }
  final n = int.parse(m.group(1)!);
  if (n < 0 || n > 100000) {
    throw CardException('cards: minutes out of range');
  }
  return n;
}

bool isHex(String s) {
  if (s.isEmpty) {
    return false;
  }
  for (final c in s.codeUnits) {
    final ok =
        (c >= 0x30 && c <= 0x39) ||
        (c >= 0x61 && c <= 0x66) ||
        (c >= 0x41 && c <= 0x46);
    if (!ok) {
      return false;
    }
  }
  return true;
}

bool isBlank(String line) {
  for (final c in line.codeUnits) {
    if (c != 0x20 && c != 0x09 && c != 0x0d) {
      return false;
    }
  }
  return true;
}

// build publishes the parsed card through the generated save and checked write
// (docs/SPEC-TABLES.md §6.2). Bytes are published only on complete success; a
// refused write mutates nothing.
Record buildOn(ParsedCard p, String body) {
  final card = Card();
  card.id = p.id;
  card.sha = p.sha;
  card.tier = p.tier;
  card.repo = p.repo;
  card.base = p.base;
  card.kind = p.kind;
  card.priority = p.priority;
  card.stream = p.stream;
  card.paths = p.paths.map((e) => PathText()..s = e).toList();
  card.new_ = p.news.map((e) => PathText()..s = e).toList();
  card.shared = p.shared.map((e) => PathText()..s = e).toList();
  card.needs = p.needs.map((e) => CardID()..s = e).toList();
  card.test = p.test;
  if (p.doneWhen != '') {
    card.done = p.doneWhen;
  }
  card.deadlineMinutes = p.deadline;
  if (p.source != '') {
    card.source = p.source;
  }
  card.extra = p.extras
      .map(
        (e) => KeyValue()
          ..key = e.key
          ..value = e.value,
      )
      .toList();
  if (body.isNotEmpty) {
    card.body = body;
  }
  checkCard(card);
  final wire = cardWireSave(card);
  return Record._(wire, card, TableWireReport());
}

// ---- the view (docs/SPEC-TABLES.md §8, §8.1, §8.3) ----

// viewTypeName resolves one descriptor row's schema element name (§8.1). The
// generated descriptor carries the wire kind alone, so a list's element table
// and an enum's type name are named here, keyed by the table and field the
// registry lists: the view is the vocabulary a walker reads a record by name
// and kind with.
String viewTypeName(String table, TableFieldInfo f) {
  switch (f.kind) {
    case 1:
      return 'bool';
    case 4:
      return 'int32';
    case 5:
      return 'int64';
    case 12:
    case 17:
      return 'string';
    case 14:
      final e = _listElementType['$table.${f.name}'];
      if (e != null) {
        return e;
      }
      throw CardException('cards: list $table.${f.name} has no element type');
    case 30:
      final e = _enumTypeName['$table.${f.name}'];
      if (e != null) {
        return e;
      }
      throw CardException('cards: enum $table.${f.name} has no type name');
  }
  throw CardException('cards: field $table.${f.name} has kind ${f.kind}');
}

const Map<String, String> _listElementType = {
  'Card.paths': 'PathText',
  'Card._new': 'PathText',
  'Card.shared': 'PathText',
  'Card.needs': 'CardID',
  'Card.extra': 'KeyValue',
  'Card.consumers': 'Consumer',
};

const Map<String, String> _enumTypeName = {
  'Card.tier': 'Tier',
  'Card.priority': 'Priority',
  'Card.column': 'Column',
};

// view walks the generated card unit's cardDescriptors() (TableFieldInfo and
// TableTypeInfo, §8.1) and renders one line per field of every table, tables
// sorted by name and each table's fields in declaration order (§8; FORMAT.md
// "The view"): `<table> <field> <kind> <shape> <bound>`, where bound is the
// declared string length or `-` when the field has none.
String view() {
  final descs = cardDescriptors();
  final names = descs.keys.toList()..sort();
  final out = StringBuffer();
  for (final name in names) {
    for (final f in descs[name]!.fields) {
      var bound = '-';
      if (f.shape == 'value' && f.bound != null) {
        bound = '${f.bound}';
      }
      out.write(name);
      out.write(' ');
      out.write(f.name);
      out.write(' ');
      out.write(viewTypeName(name, f));
      out.write(' ');
      out.write(f.shape);
      out.write(' ');
      out.write(bound);
      out.write('\n');
    }
  }
  return out.toString();
}

// ---- render helpers (tables/cards/render.go) ----

String tierString(int t) {
  switch (t) {
    case Tier.flash:
      return 'flash';
    case Tier.pro:
      return 'pro';
    case Tier.heavy:
      return 'heavy';
    case Tier.frontier:
      return 'frontier';
  }
  throw CardException('cards: tier $t has no header spelling');
}

String priorityString(int p) {
  switch (p) {
    case Priority.none:
      return '';
    case Priority.normal:
      return 'normal';
    case Priority.high:
      return 'high';
    case Priority.critical:
      return 'critical';
    case Priority.blocker:
      return 'blocker';
  }
  throw CardException('cards: priority $p has no header spelling');
}

void emitStr(StringBuffer out, String key, String value) {
  if (value == '') {
    return;
  }
  out.write(key);
  out.write(': ');
  out.write(value);
  out.write('\n');
}

void emitPtr(StringBuffer out, String key, String? value) {
  if (value == null) {
    return;
  }
  out.write(key);
  out.write(': ');
  out.write(value);
  out.write('\n');
}

void emitDeadline(StringBuffer out, int minutes) {
  if (minutes == 0) {
    return;
  }
  out.write('DEADLINE: finish within ');
  out.write('$minutes');
  out.write(' minutes.');
  out.write('\n');
}

void emitList(StringBuffer out, String key, List<String> elems) {
  if (elems.isEmpty) {
    return;
  }
  out.write(key);
  out.write(': ');
  for (var i = 0; i < elems.length; i++) {
    if (i > 0) {
      out.write(', ');
    }
    out.write(elems[i]);
  }
  out.write('\n');
}
