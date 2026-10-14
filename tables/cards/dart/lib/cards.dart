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

import 'sha256.dart';

import '../../../../build/tables-generated-dart/cards/Card.dart';
import '../../../../build/tables-generated-dart/cards/CardsWire.dart';

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
  Uint8List _wire;
  Card _card;
  TableWireReport _report;

  Record._(this._wire, this._card, this._report);

  // _publish swaps the owned wire, card and report on complete success only;
  // a refused edit leaves every field untouched (docs/SPEC-TABLES.md §6.2).
  void _publish(Uint8List wire, Card card, TableWireReport report) {
    _wire = wire;
    _card = card;
    _report = report;
  }

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

// ---- L3: the retained upgrade and the strict read (tables/cards/upgrade.go) ----

// maxRetainBytes is the customer ceiling on the retained-record byte store:
// fixed retention storage of 1 << 20 bytes per call, never grown
// (docs/SPEC-TABLES.md §6.6).
const int maxRetainBytes = 1 << 20;

// maxRetainIds is the customer ceiling on the retained-id entry store: 4096
// ids per call, never grown (docs/SPEC-TABLES.md §6.6).
const int maxRetainIds = 4096;

// UpgradeResult is a successful upgrade: the new wire and the accumulated
// report (tables/cards/conformance/FORMAT.md).
class UpgradeResult {
  final Uint8List wire;
  final TableWireReport report;
  UpgradeResult(this.wire, this.report);
}

// upgrade rewrites wire through the generated retaining path so every field
// the current declaration cannot name is preserved at the end of its own body
// (§6.6; tla/CardVersioning.tla: NoFieldLost, BodyUnchanged,
// RefusalKeepsOriginal). Unknown, Retained and Widened alone are not loss.
UpgradeResult upgrade(Uint8List wire) =>
    upgradeWithStorage(wire, maxRetainBytes, maxRetainIds);

// upgradeWithStorage is upgrade with caller-owned retention storage, so a
// capacity one id short can be shown to refuse with retain-lost
// (tla/CardVersioning.tla: RefusalKeepsOriginal).
UpgradeResult upgradeWithStorage(
  Uint8List wire,
  int retainBytes,
  int retainIds,
) {
  if (retainBytes < 0 || retainIds < 0) {
    throw CardException('cards: upgrade: negative retention capacity');
  }
  final input = Uint8List.fromList(wire);
  final report = TableWireReport();
  if (cardWireMeasure(input) < 0) {
    cardWireLoad(input, report);
    throw CardException('cards: upgrade: wire is not a card');
  }
  final retain = TableRetain(retainBytes, List<int>.filled(retainIds, 0));
  final card = cardWireLoadRetain(input, retain, report);
  if (report.refused) {
    throw CardException('cards: upgrade: wire is not a card');
  }
  if (report.malformed) {
    throw CardException('cards: upgrade: wire is malformed');
  }
  checkCard(card);
  final out = cardWireSaveRetain(card, retain, report);
  _lossErr(report);
  return UpgradeResult(out, report);
}

// _lossErr names the report events that make a rewrite unsafe (§6.6):
// RetainLost, KindMismatch, Clamped or Malformed refuse with no output.
void _lossErr(TableWireReport report) {
  if (report.retainLost > 0) {
    throw CardException(
      'cards: upgrade: ${report.retainLost} retained field(s) lost, refusing to replace',
    );
  }
  if (report.kindMismatch > 0) {
    throw CardException(
      'cards: upgrade: ${report.kindMismatch} kind mismatch(es), refusing to replace',
    );
  }
  if (report.clamped > 0) {
    throw CardException(
      'cards: upgrade: ${report.clamped} clamped value(s), refusing to replace',
    );
  }
  if (report.malformed) {
    throw CardException('cards: upgrade: malformed wire, refusing to replace');
  }
}

// readStrict reads wire and refuses any data the current declaration cannot
// support: Unknown, KindMismatch, Clamped, Malformed and every load failure
// (docs/SPEC-TABLES.md §4).
Record readStrict(Uint8List wire) {
  final rec = load(wire);
  final r = rec.report;
  if (r.unknown > 0) {
    throw CardException(
      'cards: strict: ${r.unknown} unknown field(s), unsupported data',
    );
  }
  if (r.kindMismatch > 0) {
    throw CardException(
      'cards: strict: ${r.kindMismatch} kind mismatch(es), unsupported data',
    );
  }
  if (r.clamped > 0) {
    throw CardException(
      'cards: strict: ${r.clamped} clamped value(s), unsupported data',
    );
  }
  if (r.malformed) {
    throw CardException('cards: strict: malformed wire, unsupported data');
  }
  return rec;
}

// ---- L4: the task hash and atomic field edits (tables/cards/edit.go) ----

// _refuseLossy names the read events that make a record unsafe to hash or
// rewrite (§4, §6.6; tla/CardHops.tla: EditDropsUnknown reversed witness).
void _refuseLossy(TableWireReport rep) {
  if (rep.unknown > 0 ||
      rep.kindMismatch > 0 ||
      rep.clamped > 0 ||
      rep.retainLost > 0 ||
      rep.malformed) {
    throw CardException(
      'cards: record has loss events (unknown=${rep.unknown} kindMismatch=${rep.kindMismatch} clamped=${rep.clamped} retainLost=${rep.retainLost} malformed=${rep.malformed}): refused, never hashed or rewritten',
    );
  }
}

void _hashU64(List<int> out, int v) {
  for (var i = 0; i < 8; i++) {
    out.add((v >> (8 * i)) & 0xff);
  }
}

void _hashText(List<int> out, String text) {
  final b = utf8.encode(text);
  _hashU64(out, b.length);
  out.addAll(b);
}

void _hashList(List<int> out, List<String> items) {
  _hashU64(out, items.length);
  for (final e in items) {
    _hashText(out, e);
  }
}

// taskHash returns the SHA-256 digest of a record's task: body, paths, new,
// shared, test and done in that order, each text as an unsigned 64-bit
// little-endian byte length then its bytes, each list as a count then each
// length-prefixed text; needs, tier, priority and machine fields excluded
// (docs/SPEC-TABLES.md §5; tla/CardHops.tla: TaskHashStable). A nil or lossy
// record is refused with no hash.
Uint8List taskHash(Record record) {
  _refuseLossy(record.report);
  final c = record.card;
  final framed = <int>[];
  _hashText(framed, c.body ?? '');
  _hashList(framed, c.paths.map((e) => e.s).toList());
  _hashList(framed, c.new_.map((e) => e.s).toList());
  _hashList(framed, c.shared.map((e) => e.s).toList());
  _hashText(framed, c.test);
  _hashText(framed, c.done ?? '');
  return sha256Bytes(framed);
}

// taskHashHex is taskHash as 64 lowercase hex digits.
String taskHashHex(Record record) =>
    taskHash(record).map((v) => v.toRadixString(16).padLeft(2, '0')).join();

// taskHashHexWithNeeds is the hash-negative-control frame: taskHash's logical
// frame with the needs list inserted in schema declaration order, between
// shared and test, so a driver that hashes needs — which the task hash excludes
// (docs/SPEC-TABLES.md §5; tla/CardHops.tla: TaskHashStable) — produces a
// different but still valid 64-digit digest. It is reached only through the
// CARDS_DART_SABOTAGE_HASH test switch in bin/driver.dart, never by a customer.
String taskHashHexWithNeeds(Record record) {
  _refuseLossy(record.report);
  final c = record.card;
  final framed = <int>[];
  _hashText(framed, c.body ?? '');
  _hashList(framed, c.paths.map((e) => e.s).toList());
  _hashList(framed, c.new_.map((e) => e.s).toList());
  _hashList(framed, c.shared.map((e) => e.s).toList());
  _hashList(framed, c.needs.map((e) => e.s).toList());
  _hashText(framed, c.test);
  _hashText(framed, c.done ?? '');
  return sha256Bytes(framed)
      .map((v) => v.toRadixString(16).padLeft(2, '0'))
      .join();
}

void _validateStrings(String name, List<String> values, int bound) {
  for (final v in values) {
    if (utf8.encode(v).length > bound) {
      throw CardException(
        'cards: $name element is ${utf8.encode(v).length} bytes, past its $bound-byte bound',
      );
    }
    if (v.contains('\u0000')) {
      throw CardException(
        'cards: $name has an element that is not well-formed UTF-8 without NUL',
      );
    }
  }
}

// _edit is the shared atomic rewrite: refuse a lossy record, reload it fresh,
// mutate, validate the whole rebuilt card, save, and publish only on complete
// success (§6.2; tla/CardHops.tla: TaskHashStable, EditDropsUnknown).
void _edit(Record r, void Function(Card) mutate) {
  _refuseLossy(r.report);
  final fresh = load(r.wire());
  _refuseLossy(fresh.report);
  mutate(fresh.card);
  checkCard(fresh.card);
  final wire = cardWireSave(fresh.card);
  r._publish(wire, fresh.card, TableWireReport());
}

// setNeeds replaces the needs list atomically (§2.9, §6.2).
void setNeeds(Record r, List<String> needs) {
  _validateStrings('needs', needs, 64);
  if (needs.length > maxListEntries) {
    throw CardException(
      'cards: needs has ${needs.length} entries, past the $maxListEntries-entry ceiling',
    );
  }
  _edit(r, (c) => c.needs = needs.map((e) => CardID()..s = e).toList());
}

// setPaths replaces the paths list atomically (§2.9, §6.2).
void setPaths(Record r, List<String> paths) {
  _validateStrings('paths', paths, 192);
  if (paths.length > maxListEntries) {
    throw CardException(
      'cards: paths has ${paths.length} entries, past the $maxListEntries-entry ceiling',
    );
  }
  _edit(r, (c) => c.paths = paths.map((e) => PathText()..s = e).toList());
}

// setTier changes the tier atomically; only a declared variant is accepted (§8).
void setTier(Record r, int tier) {
  if (tier < 0 || tier > Tier.max) {
    throw CardException('cards: tier $tier is not a declared variant');
  }
  _edit(r, (c) => c.tier = tier);
}

// setPriority changes the priority atomically (§8).
void setPriority(Record r, int priority) {
  if (priority < 0 || priority > Priority.max) {
    throw CardException('cards: priority $priority is not a declared variant');
  }
  _edit(r, (c) => c.priority = priority);
}

// setColumn changes the column atomically (§8).
void setColumn(Record r, int column) {
  if (column < 0 || column > Column.max) {
    throw CardException('cards: column $column is not a declared variant');
  }
  _edit(r, (c) => c.column = column);
}

// setHolder changes the holder atomically (§6.1).
void setHolder(Record r, String holder) {
  _validateStrings('holder', [holder], 32);
  _edit(r, (c) => c.holder = holder);
}

// ---- the view (docs/SPEC-TABLES.md §8.1) ----

// _viewKinds pins the schema element type of every descriptor field, keyed by
// table and field; viewWalksEveryField pins the same vocabulary independently
// in the test, so a declaration change without a vocabulary update fails.
const Map<String, String> _viewKinds = {
  'Card.id': 'string',
  'Card.sha': 'string',
  'Card.tier': 'Tier',
  'Card.repo': 'string',
  'Card._base': 'string',
  'Card.kind': 'string',
  'Card.priority': 'Priority',
  'Card.stream': 'string',
  'Card.paths': 'PathText',
  'Card._new': 'PathText',
  'Card.shared': 'PathText',
  'Card.needs': 'CardID',
  'Card.test': 'string',
  'Card.done': 'string',
  'Card.deadline_minutes': 'int32',
  'Card.source': 'string',
  'Card.epoch': 'int32',
  'Card.column': 'Column',
  'Card.holder': 'string',
  'Card.attempt': 'int32',
  'Card.head': 'string',
  'Card.branch': 'string',
  'Card.body': 'string',
  'Card.extra': 'KeyValue',
  'Card.consumers': 'Consumer',
  'CardID.s': 'string',
  'Consumer.kind': 'string',
  'Consumer.who': 'string',
  'Consumer.route': 'string',
  'Consumer.model': 'string',
  'Consumer.started': 'int64',
  'Consumer.ended': 'int64',
  'Consumer.outcome': 'string',
  'Consumer.line': 'string',
  'Consumer.cost_micro_usd': 'int64',
  'KeyValue.key': 'string',
  'KeyValue.value': 'string',
  'PathText.s': 'string',
};

// viewText writes the generated view: one line per field of every table,
// tables sorted by name, fields in declaration order, from cardDescriptors()
// (docs/SPEC-TABLES.md §8.1; tables/cards/conformance/FORMAT.md).
String viewText() {
  final desc = cardDescriptors();
  final tables = desc.keys.toList()..sort();
  final out = StringBuffer();
  for (final t in tables) {
    for (final f in desc[t]!.fields) {
      final kind = _viewKinds['$t.${f.name}'];
      if (kind == null) {
        throw CardException('cards: view has no kind for $t.${f.name}');
      }
      out.writeln('$t ${f.name} $kind ${f.shape} ${f.bound ?? '-'}');
    }
  }
  return out.toString();
}
