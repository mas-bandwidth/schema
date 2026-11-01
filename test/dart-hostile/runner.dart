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
// build/dart-hostile/generated, and the text, wire, wire_retain and packet
// bindings are build/dart-hostile/dispatch.dart, which
// test/dart-hostile/dispatch_gen.dart writes from the manifest (its imports
// below are relative to this file).
//
// A carrier maps to the port's public reader:
//   text              <root>FromJson(text, report)   (<Package>Wire.dart)
//   wire              <root>WireLoad(bytes, report)  (<Package>Wire.dart)
//   wire_retain       <root>WireLoadRetain(bytes, retain, report)
//   message           announceRead(own announcement), then <root>LoadMessages
//                     into one value (capacity 1)
//   message_novocab   <root>LoadMessages with no announcement read
//   announce          announceRead(vocabulary, input, report)
//   announce_second   announceRead twice on one vocabulary
//   announce_bytes    announceRead with maxBytes 8
//   announce_entries  announceRead with maxEntries 1
//   packet            read<Type>(value, bytes, 8 * length), then write<Type>
//                     re-writes the decoded value; the digest is SHA-256 of
//                     those bytes
//
// An accepted text or wire read's value is digested as the engine's is:
// SHA-256 of the port's text writer with its one trailing newline removed (the
// engine's canonical text, internal/tabletext.Model.Write, carries none). A
// read that throws, or a report that says malformed, is the corpus's
// `damaged`; a refusal is named by the port's own reason.
//
// THE RUNNER RULES (docs/HOSTILE-CORPUS.md). Every case runs in its own isolate
// (the VM's default isolate stack) under a time budget (5 s, or
// HOSTILE_CASE_BUDGET_MS). An uncaught error, a stack overflow among them, is
// that case's answer and the run goes on; an isolate still running at its
// budget is killed and the case is over budget. After an accepted wire,
// wire_retain, text or message case the runner also measures, saves and writes
// the text of the decoded value; a throw there is the case's answer too, except
// a writer's named refusal (a StateError whose message opens with its reason,
// `blob_shared: ...`). Cases run one at a time: the budget is wall-clock, and
// parallel cases sharing the bench would charge each other's time.
import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:isolate';
import 'dart:typed_data';

import '../../build/dart-hostile/dispatch.dart' as dispatch;
import '../../build/dart-hostile/generated/tabledemo/TabledemoFixed.dart' as tabledemo_fixed;
import '../../build/dart-hostile/generated/messagedemo/MessagedemoWire.dart' as messagedemo;
import '../../build/dart-hostile/generated/messagedemo/MessagedemoFixed.dart' as messagedemo_fixed;
import '../../build/dart-hostile/generated/messagedemo/MessagesFixed.dart' as messages;

// ---- the answer a case gets ----

final class Answer {
  final String verdict; // accept | refuse | error
  final String detail; // the digest, the reason, or what went wrong
  const Answer(this.verdict, this.detail);
  @override
  String toString() => '$verdict $detail';
}

const Answer _damaged = Answer('refuse', 'damaged');

Answer _unbound(String what) => Answer('error', 'unbound: no $what reader bound');

// `--show <case>` prints the port's canonical text for one case, to diff by
// hand against the engine's when a FIX-CARD is being worked.
var _show = false;

Answer _accepted(String written) {
  if (_show) stdout.write(written);
  final canonical = written.endsWith('\n') ? written.substring(0, written.length - 1) : written;
  return Answer('accept', sha256Hex(utf8.encode(canonical)));
}

// A writer's named refusal: a StateError whose message opens with its reason.
final _namedRefusal = RegExp(r'^[a-z][a-z0-9_]*: ');

// Measure and Save after an accept: a throw is the case's answer, except a
// writer's named refusal.
Answer _after(Answer verdict, String step, void Function() exercise) {
  if (verdict.verdict != 'accept') return verdict;
  try {
    exercise();
  } on StateError catch (e) {
    if (!_namedRefusal.hasMatch(e.message)) {
      return Answer('error', 'after accept, $step raised ${e.toString().split('\n').first}');
    }
  } catch (e) {
    return Answer('error', 'after accept, $step raised ${e.toString().split('\n').first}');
  }
  return verdict;
}

// ---- the text carrier ----

Answer _text(String schema, String table, Uint8List payload) {
  final String text;
  try {
    // a text read takes a Dart String: bytes that are not UTF-8 are not text
    text = utf8.decode(payload);
  } on FormatException {
    return _damaged;
  }
  final dispatch.Loaded? loaded;
  final String written;
  try {
    loaded = dispatch.textLoad(schema, table, text);
    if (loaded == null) return _unbound('text');
    written = loaded.written();
  } catch (_) {
    return _damaged;
  }
  return _after(_accepted(written), 'WireSave/WireMeasure', loaded.exercise);
}

// ---- the wire carriers ----

Answer _wire(String schema, String table, Uint8List payload, {required bool retain}) {
  final loaded =
      retain ? dispatch.wireLoadRetain(schema, table, payload) : dispatch.wireLoad(schema, table, payload);
  if (loaded == null) return _unbound(retain ? 'wire_retain' : 'wire');
  if (loaded.refused) return Answer('refuse', loaded.reason);
  if (loaded.malformed) return _damaged;
  return _after(_accepted(loaded.written()), retain ? 'WireMeasureRetain/WireSaveRetain' : 'WireMeasure/WireSave',
      loaded.exercise);
}

// ---- the packet carrier ----

Answer _packet(String schema, String type, Uint8List payload) {
  if (!dispatch.packetBound(schema, type)) return _unbound('packet');
  final written = dispatch.packetRun(schema, type, payload);
  if (written == null) return const Answer('refuse', 'packet_refused');
  return Answer('accept', sha256Hex(written));
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
  final verdict = _accepted(messagedemo.toolMessageToJson(messagedemo.toolMessageFromJson('{}')));
  // Measure and Save the decoded batch back through the message writer.
  return _after(verdict, 'MeasureMessages/SaveMessages', () {
    final values = <messagedemo_fixed.ToolMessage>[value];
    final need = messages.toolMessageMeasureMessages(values, messagedemo_fixed.TableReport());
    messages.toolMessageSaveMessages(values, Uint8List(need < 0 ? 64 : need + 64), messagedemo_fixed.TableReport());
  });
}

messagedemo_fixed.TableVocabulary _messageVocabulary() => messagedemo_fixed.TableVocabulary(
    entries: List<messagedemo_fixed.TableMessageEntry>.generate(4096, (_) => messagedemo_fixed.TableMessageEntry(0, 0)));

Answer _message(String table, Uint8List payload, {required bool announced}) {
  if (table != 'ToolMessage') return _unbound('message');
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
      return _wire(schema, table, payload, retain: false);
    case 'wire_retain':
      return _wire(schema, table, payload, retain: true);
    case 'message':
      return _message(table, payload, announced: true);
    case 'message_novocab':
      return _message(table, payload, announced: false);
    case 'announce':
    case 'announce_second':
    case 'announce_bytes':
    case 'announce_entries':
      return _announce(carrier, payload);
    case 'packet':
      return _packet(schema, table, payload);
  }
  return Answer('error', 'unknown carrier $carrier');
}

// ---- one case in its own isolate ----

// the isolate's entry: [reply, carrier, schema, table, payload]
void _caseMain(List<Object> message) {
  final reply = message[0] as SendPort;
  Answer got;
  try {
    got = run(message[1] as String, message[2] as String, message[3] as String, message[4] as Uint8List);
  } catch (e) {
    // e.g. the text writer refusing a value the read accepted (a NaN)
    got = Answer('error', e.toString().split('\n').first);
  }
  reply.send(<String>[got.verdict, got.detail]);
}

Future<Answer> isolated(String carrier, String schema, String table, Uint8List payload, Duration budget) async {
  final port = ReceivePort();
  final errors = ReceivePort();
  final exits = ReceivePort();
  final done = Completer<Answer>();
  port.listen((m) {
    final l = m as List<Object?>;
    if (!done.isCompleted) done.complete(Answer(l[0] as String, l[1] as String));
  });
  errors.listen((e) {
    final l = e as List<Object?>;
    if (!done.isCompleted) done.complete(Answer('error', 'uncaught ${l[0]}'.split('\n').first));
  });
  exits.listen((_) {
    // the reply, when there is one, is delivered before the exit notice
    Timer.run(() {
      if (!done.isCompleted) done.complete(const Answer('error', 'the case isolate exited without an answer'));
    });
  });
  final isolate = await Isolate.spawn<List<Object>>(_caseMain, <Object>[port.sendPort, carrier, schema, table, payload],
      onError: errors.sendPort, onExit: exits.sendPort, errorsAreFatal: true);
  final timer = Timer(budget, () {
    if (!done.isCompleted) {
      isolate.kill(priority: Isolate.immediate);
      done.complete(Answer('error', 'over budget: still running after ${budget.inMilliseconds} ms'));
    }
  });
  final got = await done.future;
  timer.cancel();
  port.close();
  errors.close();
  exits.close();
  return got;
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

Future<void> main(List<String> args) async {
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
  final budgetEnv = Platform.environment['HOSTILE_CASE_BUDGET_MS'] ?? '';
  final budget = Duration(milliseconds: budgetEnv.isEmpty ? 5000 : int.parse(budgetEnv));
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
    final got = await isolated(carrier, schema, table, payload, budget);
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
  exit(bad.isNotEmpty || stale.isNotEmpty ? 1 : 0);
}
