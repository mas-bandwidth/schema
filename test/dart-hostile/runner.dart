// THE DART PORT RUNS THE SHARED HOSTILE CORPUS (docs/HOSTILE-CORPUS.md).
//
// One raw input per manifest line, the port's generated reader underneath, and
// the Go engine's committed verdict as the oracle. The corpus is read as files:
// no network, no clock, no outside service. The runner prints one PASS count,
// one FIX-CARD line per carried disagreement, and fails on a disagreement no
// FIX-CARD names (or a FIX-CARD whose case has gone green), so the carried list
// cannot rot into silence.
//
//   dart run test/dart-hostile/runner.dart <corpus-dir> [known-findings.tsv]
//   dart run test/dart-hostile/runner.dart --show <corpus-dir> <case>
//
// The working directory is the repository root. The generated units are the
// ones make/schema-dart-runs-hostile-corpus.mk writes under
// build/dart-hostile/generated (its imports below are relative to this file).
//
// A carrier maps to the port's public reader:
//   text              <root>FromJson(text, report)   (<Package>Wire.dart)
//   wire              <root>WireLoad(bytes, report)  (<Package>Wire.dart)
//   message           announceRead(own announcement), then <root>LoadMessages
//                     into one value (capacity 1)
//   message_novocab   <root>LoadMessages with no announcement read
//   announce          announceRead(vocabulary, input, report)
//   announce_second   announceRead twice on one vocabulary
//   announce_bytes    announceRead with maxBytes 8
//   announce_entries  announceRead with maxEntries 1
//
// An accepted text read's value is digested as the engine's is: SHA-256 of the
// port's text writer with its one trailing newline removed (the engine's
// canonical text, internal/tabletext.Model.Write, carries none). A wire or
// message accept carries no digest in the manifest. A read that throws, or a
// report that says malformed, is the corpus's `damaged`; a refusal is named by
// the port's own reason.
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import '../../build/dart-hostile/generated/tabledemo/TabledemoWire.dart' as tabledemo;
import '../../build/dart-hostile/generated/tabledemo/TabledemoFixed.dart' as tabledemo_fixed;
import '../../build/dart-hostile/generated/messagedemo/MessagedemoWire.dart' as messagedemo;
import '../../build/dart-hostile/generated/messagedemo/MessagedemoFixed.dart' as messagedemo_fixed;
import '../../build/dart-hostile/generated/messagedemo/MessagesFixed.dart' as messages;
import '../../build/dart-hostile/generated/blobdemo/BlobdemoWire.dart' as blobdemo;
import '../../build/dart-hostile/generated/graphdemo/GraphdemoWire.dart' as graphdemo;
import '../../build/dart-hostile/generated/scalardemo/ScalardemoWire.dart' as scalardemo;
import '../../build/dart-hostile/generated/streamdemo/StreamdemoWire.dart' as streamdemo;
import '../../build/dart-hostile/generated/mapdemo/MapdemoWire.dart' as mapdemo;
import '../../build/dart-hostile/generated/listdemo/ListdemoWire.dart' as listdemo;
import '../../build/dart-hostile/generated/probe/probeWire.dart' as probe;

// ---- the answer a case gets ----

final class Answer {
  final String verdict; // accept | refuse | error
  final String detail; // the digest, the reason, or what went wrong
  const Answer(this.verdict, this.detail);
  @override
  String toString() => '$verdict $detail';
}

const Answer _damaged = Answer('refuse', 'damaged');

// a case the runner has no reader bound for is the runner's defect, never an
// answer: it stops the run rather than scoring as a disagreement
final class _Unbound implements Exception {
  final String message;
  const _Unbound(this.message);
  @override
  String toString() => message;
}

// `--show <case>` prints the port's canonical text for one case, to diff by
// hand against the engine's when a FIX-CARD is being worked.
var _show = false;

// ---- the text carrier ----

String? _textRead(String schema, String table, String text) {
  final key = '$schema $table';
  switch (key) {
    case 'tables/examples RootConfig':
      return tabledemo.rootConfigToJson(tabledemo.rootConfigFromJson(text, tabledemo.TableTextReport()));
    case 'tables/examples PackConfig':
      return tabledemo.packConfigToJson(tabledemo.packConfigFromJson(text, tabledemo.TableTextReport()));
    case 'tables/examples GlobalSettings':
      return tabledemo.globalSettingsToJson(tabledemo.globalSettingsFromJson(text, tabledemo.TableTextReport()));
    case 'tables/examples ShipEntry':
      return tabledemo.shipEntryToJson(tabledemo.shipEntryFromJson(text, tabledemo.TableTextReport()));
    case 'tables/messages ToolMessage':
      return messagedemo.toolMessageToJson(messagedemo.toolMessageFromJson(text, messagedemo.TableTextReport()));
    case 'tables/blobs Catalog':
      return blobdemo.catalogToJson(blobdemo.catalogFromJson(text, blobdemo.TableTextReport()));
    case 'tables/pointers Scene':
      return graphdemo.sceneToJson(graphdemo.sceneFromJson(text, graphdemo.TableTextReport()));
    case 'tables/scalars SimState':
      return scalardemo.simStateToJson(scalardemo.simStateFromJson(text, scalardemo.TableTextReport()));
    case 'tables/stream Feed':
      return streamdemo.feedToJson(streamdemo.feedFromJson(text, streamdemo.TableTextReport()));
    case 'tables/maps Text':
      return mapdemo.textToJson(mapdemo.textFromJson(text, mapdemo.TableTextReport()));
    case 'tables/lists Army':
      return listdemo.armyToJson(listdemo.armyFromJson(text, listdemo.TableTextReport()));
  }
  throw _Unbound('no text reader bound for $key');
}

Answer _text(String schema, String table, Uint8List payload) {
  final String text;
  try {
    // a text read takes a Dart String: bytes that are not UTF-8 are not text
    text = utf8.decode(payload);
  } on FormatException {
    return _damaged;
  }
  final String written;
  try {
    written = _textRead(schema, table, text)!;
  } on _Unbound {
    rethrow;
  } catch (_) {
    return _damaged;
  }
  return _accepted(written);
}

// ---- the wire carrier ----

Answer _accepted(String written) {
  if (_show) stdout.write(written);
  final canonical = written.endsWith('\n') ? written.substring(0, written.length - 1) : written;
  return Answer('accept', sha256Hex(utf8.encode(canonical)));
}

Answer _wireReport(bool malformed, bool refused, String reason, String Function() written) {
  if (refused) return Answer('refuse', reason);
  if (malformed) return _damaged;
  return _accepted(written());
}

Answer _wire(String schema, String table, Uint8List payload) {
  switch ('$schema $table') {
    case 'tables/examples ShipEntry':
      final r = tabledemo.TableWireReport();
      final v = tabledemo.shipEntryWireLoad(payload, r);
      return _wireReport(r.malformed, r.refused, r.reason, () => tabledemo.shipEntryToJson(v));
    case 'schemas/probe.schema Root':
      final r = probe.TableWireReport();
      final v = probe.rootWireLoad(payload, r);
      return _wireReport(r.malformed, r.refused, r.reason, () => probe.rootToJson(v));
  }
  throw _Unbound('no wire reader bound for $schema $table');
}

// ---- the message carriers (docs/SPEC-TABLES.md §3.3) ----

// The message form decodes into the fixed class, which has no text writer of
// its own. The value is digested the way the Elixir runner does it: a body that
// set no field decodes to the table's defaults, which the form-1 text read of
// `{}` states; the runner proves that by comparing the decoded value's fixed
// record with a freshly initialized one, byte for byte, and never assumes it.
Answer _messageAccepted(messagedemo_fixed.ToolMessage value) {
  final fresh = messagedemo_fixed.ToolMessage();
  messagedemo_fixed.initToolMessage(fresh);
  final a = Uint8List(messages.toolMessageFixedMeasure(1));
  final b = Uint8List(messages.toolMessageFixedMeasure(1));
  messages.toolMessageFixedSave(<messagedemo_fixed.ToolMessage>[value], 1, a);
  messages.toolMessageFixedSave(<messagedemo_fixed.ToolMessage>[fresh], 1, b);
  for (var i = 0; i < a.length; i++) {
    if (a[i] != b[i]) return const Answer('error', 'the decoded body set a field: no text writer to digest it');
  }
  return _accepted(messagedemo.toolMessageToJson(messagedemo.toolMessageFromJson('{}')));
}

messagedemo_fixed.TableVocabulary _messageVocabulary() => messagedemo_fixed.TableVocabulary(
    entries: List<messagedemo_fixed.TableMessageEntry>.generate(4096, (_) => messagedemo_fixed.TableMessageEntry(0, 0)));

Answer _message(String table, Uint8List payload, {required bool announced}) {
  if (table != 'ToolMessage') throw _Unbound('no message reader bound for $table');
  final vocab = _messageVocabulary();
  if (announced) {
    final own = Uint8List(messagedemo_fixed.announceMeasure());
    messagedemo_fixed.announce(own);
    if (!messagedemo_fixed.announceRead(vocab, own, messagedemo_fixed.TableReport())) {
      return const Answer('error', 'the unit could not read its own announcement');
    }
  }
  final value = messagedemo_fixed.ToolMessage();
  messagedemo_fixed.initToolMessage(value);
  final r = messagedemo_fixed.TableReport();
  final count = messages.toolMessageLoadMessages(<messagedemo_fixed.ToolMessage>[value], payload, vocab, r);
  if (r.refused) return Answer('refuse', r.reason);
  if (r.malformed || count != 1) return _damaged;
  return _messageAccepted(value);
}

Answer _announce(String carrier, Uint8List payload) {
  final vocab = tabledemo_fixed.TableVocabulary(
      entries: List<tabledemo_fixed.TableMessageEntry>.generate(4096, (_) => tabledemo_fixed.TableMessageEntry(0, 0)));
  switch (carrier) {
    case 'announce_bytes':
      vocab.maxBytes = 8;
    case 'announce_entries':
      vocab.maxEntries = 1;
    case 'announce_second':
      tabledemo_fixed.announceRead(vocab, payload, tabledemo_fixed.TableReport());
  }
  final r = tabledemo_fixed.TableReport();
  final ok = tabledemo_fixed.announceRead(vocab, payload, r);
  if (r.refused) return Answer('refuse', r.reason);
  if (r.malformed || !ok) return _damaged;
  return const Answer('accept', '-');
}

Answer run(String carrier, String schema, String table, Uint8List payload) {
  switch (carrier) {
    case 'text':
      return _text(schema, table, payload);
    case 'wire':
      return _wire(schema, table, payload);
    case 'message':
      return _message(table, payload, announced: true);
    case 'message_novocab':
      return _message(table, payload, announced: false);
    case 'announce':
    case 'announce_second':
    case 'announce_bytes':
    case 'announce_entries':
      return _announce(carrier, payload);
  }
  throw _Unbound('unknown carrier $carrier');
}

// ---- SHA-256 (FIPS 180-4), so the runner needs no package ----

const List<int> _k = <int>[
  0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
  0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
  0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
  0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
  0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
  0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
  0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
  0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2,
];

int _rotr(int x, int n) => ((x >> n) | (x << (32 - n))) & 0xffffffff;

String sha256Hex(List<int> message) {
  final h = <int>[0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a, 0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19];
  final bitLength = message.length * 8;
  final padded = <int>[...message, 0x80];
  while (padded.length % 64 != 56) {
    padded.add(0);
  }
  for (var i = 7; i >= 0; i--) {
    padded.add((bitLength >> (i * 8)) & 0xff);
  }
  final w = List<int>.filled(64, 0);
  for (var chunk = 0; chunk < padded.length; chunk += 64) {
    for (var i = 0; i < 16; i++) {
      final j = chunk + i * 4;
      w[i] = (padded[j] << 24) | (padded[j + 1] << 16) | (padded[j + 2] << 8) | padded[j + 3];
    }
    for (var i = 16; i < 64; i++) {
      final s0 = _rotr(w[i - 15], 7) ^ _rotr(w[i - 15], 18) ^ (w[i - 15] >> 3);
      final s1 = _rotr(w[i - 2], 17) ^ _rotr(w[i - 2], 19) ^ (w[i - 2] >> 10);
      w[i] = (w[i - 16] + s0 + w[i - 7] + s1) & 0xffffffff;
    }
    var a = h[0], b = h[1], c = h[2], d = h[3], e = h[4], f = h[5], g = h[6], hh = h[7];
    for (var i = 0; i < 64; i++) {
      final s1 = _rotr(e, 6) ^ _rotr(e, 11) ^ _rotr(e, 25);
      final ch = (e & f) ^ ((~e & 0xffffffff) & g);
      final t1 = (hh + s1 + ch + _k[i] + w[i]) & 0xffffffff;
      final s0 = _rotr(a, 2) ^ _rotr(a, 13) ^ _rotr(a, 22);
      final maj = (a & b) ^ (a & c) ^ (b & c);
      final t2 = (s0 + maj) & 0xffffffff;
      hh = g;
      g = f;
      f = e;
      e = (d + t1) & 0xffffffff;
      d = c;
      c = b;
      b = a;
      a = (t1 + t2) & 0xffffffff;
    }
    h[0] = (h[0] + a) & 0xffffffff;
    h[1] = (h[1] + b) & 0xffffffff;
    h[2] = (h[2] + c) & 0xffffffff;
    h[3] = (h[3] + d) & 0xffffffff;
    h[4] = (h[4] + e) & 0xffffffff;
    h[5] = (h[5] + f) & 0xffffffff;
    h[6] = (h[6] + g) & 0xffffffff;
    h[7] = (h[7] + hh) & 0xffffffff;
  }
  return h.map((v) => v.toRadixString(16).padLeft(8, '0')).join();
}

// ---- the manifest and the carried findings ----

List<List<String>> _rows(String path) => File(path)
    .readAsLinesSync()
    .where((l) => l.isNotEmpty && !l.startsWith('#'))
    .map((l) => l.split('\t'))
    .toList();

void main(List<String> args) {
  if (args.length == 3 && args[0] == '--show') {
    for (final m in _rows('${args[1]}/manifest.tsv')) {
      if (m[0] != args[2]) continue;
      _show = true;
      stderr.writeln(run(m[1], m[2], m[3], File('${args[1]}/${m[4]}').readAsBytesSync()));
      return;
    }
    stderr.writeln('no case ${args[2]}');
    exit(2);
  }
  if (args.isEmpty || args.length > 2) {
    stderr.writeln('usage: runner.dart <corpus-dir> [known-findings.tsv]');
    stderr.writeln('       runner.dart --show <corpus-dir> <case>');
    exit(2);
  }
  // the hash is checked before it is trusted: the FIPS 180-2 "abc" vector
  if (sha256Hex(utf8.encode('abc')) != 'ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad') {
    stderr.writeln('the runner\'s SHA-256 fails its own known-answer test');
    exit(1);
  }
  final corpus = args[0];
  final findings = <String, List<String>>{};
  if (args.length == 2 && File(args[1]).existsSync()) {
    for (final f in _rows(args[1])) {
      findings[f[0]] = f;
    }
  }
  final manifest = _rows('$corpus/manifest.tsv');
  var passed = 0, carried = 0;
  final bad = <String>[], stale = <String>[];
  final seen = <String>{};
  for (final m in manifest) {
    final name = m[0], carrier = m[1], schema = m[2], table = m[3], input = m[4];
    final verdict = m[6], reason = m[7], digest = m[8];
    seen.add(name);
    final want = verdict == 'accept' ? Answer('accept', digest == '-' ? '-' : digest) : Answer('refuse', reason);
    final payload = File('$corpus/$input').readAsBytesSync();
    Answer got;
    try {
      got = run(carrier, schema, table, payload);
    } on _Unbound {
      rethrow;
    } catch (e) {
      // e.g. the text writer refusing a value the read accepted (a NaN)
      got = Answer('error', e.toString().split('\n').first);
    }
    final agrees = got.verdict == want.verdict && got.detail == want.detail;
    final finding = findings[name];
    if (agrees) {
      if (finding != null) {
        stdout.writeln('STALE $name: FIX-CARD ${finding[1]} no longer reproduces');
        stale.add(name);
      } else {
        passed++;
      }
    } else if (finding != null) {
      stdout.writeln('FIX-CARD: ${finding[1]}: ${finding[2]} ${finding[3]} (got $got)');
      carried++;
    } else {
      stdout.writeln('FAIL $name carrier=$carrier got=$got want=$want');
      bad.add(name);
    }
  }
  for (final name in findings.keys) {
    if (!seen.contains(name)) {
      stdout.writeln('STALE $name: FIX-CARD names no case the manifest carries');
      stale.add(name);
    }
  }
  stdout.writeln('${manifest.length} cases: $passed pass, $carried carried as FIX-CARD, '
      '${bad.length} unexplained, ${stale.length} stale');
  if (bad.isNotEmpty || stale.isNotEmpty) exit(1);
}
