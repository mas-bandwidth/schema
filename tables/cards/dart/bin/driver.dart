// driver.dart — the Dart leg of the card conformance set
// (tables/cards/conformance/FORMAT.md). It answers the driver contract
// test/conformance/README.md states — `<driver> <manifest> list` and
// `<driver> <manifest> <surface> <outdir>` — over the five surfaces this card
// owns (load, save, render, parse, bounds), through the facade in
// tables/cards/dart/lib/cards.dart. The upgrade, edit, taskhash and view
// surfaces belong to a-dart-card-upgrades-edits-and-walks-the-shared-corpus and
// are answered absent (exit 2). It is the concrete side of tla/CardHops.tla:
// every language's driver must answer the same bytes.
//
// A test-only environment switch, CARDS_DART_SABOTAGE=1, changes one rendered
// byte so the negative control can prove the render surface goes red while the
// other four stay green.

import 'dart:convert';
import 'dart:io';

import '../lib/cards.dart';

const List<String> surfaces = [
  'load',
  'save',
  'render',
  'parse',
  'bounds',
  'upgrade',
  'edit',
  'taskhash',
  'view',
];

class Case {
  final String surface;
  final String name;
  final String input;
  final List<String> edits;
  Case(this.surface, this.name, this.input, this.edits);
}

List<Case> readManifest(String path) {
  final out = <Case>[];
  for (final raw in File(path).readAsLinesSync()) {
    final line = raw.trim();
    if (line.isEmpty || line.startsWith('#')) {
      continue;
    }
    final f = line.split(RegExp(r'[ \t]+'));
    if (f.length < 3) {
      stderr.writeln('driver: malformed manifest line "$line"');
      exit(1);
    }
    out.add(Case(f[0], f[1], f[2], f.length > 3 ? f.sublist(3) : <String>[]));
  }
  return out;
}

void writeFile(String outdir, String name, String ext, List<int> data) {
  File('$outdir/$name.$ext').writeAsBytesSync(data);
}

void refuse(String outdir, String name, String cls) {
  writeFile(outdir, name, 'refused', '$cls\n'.codeUnits);
}

// refusalClass maps a facade error to one class of the closed list FORMAT.md
// fixes, never free text, so every language can spell the same refusal.
String refusalClass(Object err) {
  final msg = err.toString();
  if (msg.contains('past the 65536-byte ceiling')) {
    return 'body-bound 65536';
  }
  if (msg.contains('past the 512-entry ceiling')) {
    return 'list-bound ${fieldName(msg)} 512';
  }
  if (msg.contains('stranded header key')) {
    return 'stranded-key ${quotedWord(msg)}';
  }
  if (msg.contains('retained') && msg.contains('lost')) {
    return 'retain-lost';
  }
  if (msg.contains('unknown') && msg.contains('loss events')) {
    return 'unknown-present';
  }
  if (msg.contains('unknown field')) {
    return 'unknown-present';
  }
  return 'malformed';
}

// fieldName reads the collection name from a checkCard ceiling error, whose
// text is "cards: <field> has N entries, past the 512-entry ceiling ...".
String fieldName(String msg) {
  final rest = msg.replaceFirst('cards: ', '');
  return rest.split(' has ').first;
}

// quotedWord reads the first double-quoted token of an error, the stranded
// header key a parse refusal names.
String quotedWord(String msg) {
  final i = msg.indexOf('"');
  if (i < 0) {
    return '';
  }
  final j = msg.indexOf('"', i + 1);
  if (j < 0) {
    return '';
  }
  return msg.substring(i + 1, j);
}

// writeReport writes the read report, one Name=value line per counter of the
// Go TableReport in declaration order (tables/cards/conformance/FORMAT.md).
void writeReport(String outdir, String name, TableWireReport r) {
  final b = StringBuffer();
  b.writeln('Retained=${r.retained}');
  b.writeln('RetainLost=${r.retainLost}');
  b.writeln('Widened=${r.widened}');
  b.writeln('Unknown=${r.unknown}');
  b.writeln('KindMismatch=${r.kindMismatch}');
  b.writeln('Clamped=${r.clamped}');
  b.writeln('Duplicate=0');
  b.writeln('Malformed=${r.malformed}');
  writeFile(outdir, name, 'report', b.toString().codeUnits);
}

void one(String surface, Case c, String outdir) {
  switch (surface) {
    case 'load':
      final wire = File(c.input).readAsBytesSync();
      final rec = load(wire);
      final json = toJson(rec);
      writeFile(outdir, c.name, 'json', utf8.encode(json));
      writeReport(outdir, c.name, rec.report);
      break;
    case 'save':
    case 'bounds':
      final text = File(c.input).readAsStringSync();
      try {
        final rec = fromJson(text);
        writeFile(outdir, c.name, 'wire', rec.wire());
      } on CardException catch (e) {
        refuse(outdir, c.name, refusalClass(e));
      }
      break;
    case 'render':
      final wire = File(c.input).readAsBytesSync();
      final rec = load(wire);
      var text = render(rec);
      if (Platform.environment['CARDS_DART_SABOTAGE'] == '1') {
        // the negative control: change one rendered byte.
        if (text.isNotEmpty) {
          final first = text.codeUnitAt(0);
          text =
              String.fromCharCode(first == 0x52 ? 0x53 : 0x52) +
              text.substring(1);
        }
      }
      writeFile(outdir, c.name, 'txt', utf8.encode(text));
      break;
    case 'upgrade':
      final wire = File(c.input).readAsBytesSync();
      try {
        if (Platform.environment['CARDS_DART_SABOTAGE_UPGRADE'] == '1') {
          // the upgrade negative control: an ordinary rewrite drops the
          // retained field, so upgrade alone goes red.
          final rec = load(wire);
          writeFile(outdir, c.name, 'wire', rec.wire());
          writeReport(outdir, c.name, rec.report);
        } else {
          final r = upgrade(wire);
          writeFile(outdir, c.name, 'wire', r.wire);
          writeReport(outdir, c.name, r.report);
        }
      } on CardException catch (e) {
        refuse(outdir, c.name, refusalClass(e));
      }
      break;
    case 'edit':
      final wire = File(c.input).readAsBytesSync();
      try {
        final rec = load(wire);
        applyEdit(rec, c.edits.isEmpty ? '' : c.edits[0]);
        writeFile(outdir, c.name, 'wire', rec.wire());
      } on CardException catch (e) {
        refuse(outdir, c.name, refusalClass(e));
      }
      break;
    case 'taskhash':
      final wire = File(c.input).readAsBytesSync();
      try {
        final rec = load(wire);
        if (Platform.environment['CARDS_DART_SABOTAGE_HASH'] == '1') {
          // the hash negative control: needs rides in the SHA-256 frame, so
          // the digest is a valid 64-digit hex that differs from the task hash
          // (which excludes needs), and taskhash alone goes red
          // (tla/CardHops.tla: TaskHashStable).
          writeFile(
            outdir,
            c.name,
            'hex',
            utf8.encode(taskHashHexWithNeeds(rec)),
          );
        } else {
          writeFile(outdir, c.name, 'hex', utf8.encode(taskHashHex(rec)));
        }
      } on CardException catch (e) {
        refuse(outdir, c.name, refusalClass(e));
      }
      break;
    case 'view':
      writeFile(outdir, 'view', 'txt', utf8.encode(viewText()));
      break;
    case 'parse':
      final text = File(c.input).readAsStringSync();
      try {
        final rec = parse(text);
        writeFile(outdir, c.name, 'wire', rec.wire());
      } on CardException catch (e) {
        refuse(outdir, c.name, refusalClass(e));
      }
      break;
  }
}

// applyEdit applies one manifest edit line (needs=a,b, tier=heavy,
// priority=blocker, paths=x,y, column=review or holder=h) through the Set*
// edit of the same name (tables/cards/conformance/FORMAT.md).
void applyEdit(Record rec, String edit) {
  final i = edit.indexOf('=');
  if (i < 0) {
    throw CardException('cards: malformed edit "$edit"');
  }
  final key = edit.substring(0, i);
  final value = edit.substring(i + 1);
  List<String> list() => value.isEmpty ? <String>[] : value.split(',');
  switch (key) {
    case 'needs':
      setNeeds(rec, list());
      break;
    case 'paths':
      setPaths(rec, list());
      break;
    case 'tier':
      setTier(rec, parseTier(value));
      break;
    case 'priority':
      setPriority(rec, parsePriority(value));
      break;
    case 'column':
      setColumn(rec, parseColumn(value));
      break;
    case 'holder':
      setHolder(rec, value);
      break;
    default:
      throw CardException('cards: unknown edit "$key"');
  }
}

int parseColumn(String s) {
  switch (s) {
    case 'waiting':
      return Column.waiting;
    case 'ready':
      return Column.ready;
    case 'working':
      return Column.working;
    case 'review':
      return Column.review;
    case 'merging':
      return Column.merging;
    case 'landed':
      return Column.landed;
    case 'plan':
      return Column.plan;
    case 'check':
      return Column.check;
  }
  throw CardException('cards: unknown column "$s"');
}

int run(List<String> args) {
  if (args.length < 2) {
    stderr.writeln(
      'usage: driver <manifest> list\n'
      '       driver <manifest> <surface> <outdir>',
    );
    return 2;
  }
  final manifest = args[0];
  final verb = args[1];
  if (verb == 'list') {
    for (final s in surfaces) {
      stdout.writeln(s);
    }
    return 0;
  }
  if (args.length < 3) {
    stderr.writeln('usage: driver <manifest> <surface> <outdir>');
    return 2;
  }
  final surface = verb;
  final outdir = args[2];
  if (!surfaces.contains(surface)) {
    return 2;
  }
  final cases = readManifest(manifest);
  Directory(outdir).createSync(recursive: true);
  for (final c in cases) {
    if (c.surface != surface) {
      continue;
    }
    one(surface, c, outdir);
  }
  return 0;
}

void main(List<String> args) {
  exit(run(args));
}
