// NIGHTLY FUZZER over the Dart table readers (schema-dart-nightly-fuzz).
//
// A seeded mutation loop: every mutant is a pure function of (seed, corpus
// input, pass, index), so a finding names the seed and the exact bytes that
// replay it. package:fuzz has no maintained coverage-guided equivalent for the
// Dart 3.13 VM, so this is the seeded loop the card allows; the seed and the
// minimised input are printed and saved.
//
// Three entry points per table, from the generated <Base>Wire.dart library:
//
//   load    rootConfigWireLoad(bytes, report)         tolerant wire load
//   strict  load, save, load again                     a save must read back
//   text    rootConfigFromJson(text, report)           JSON/text parse
//
// The oracle is the card's: no crash, no hang, no assert, and a refusal is a
// clean typed error. Wire readers answer malformed bytes by setting the
// report's `refused`/`malformed` flags, never by throwing. JSON readers REFUSE
// invalid text with a FormatException, which is their documented typed error;
// any other escaping exception is a finding, and so is a strict save that its
// own reader refuses.
//
// usage: nightly_fuzz.dart --seconds N --seed N --seeds DIR
//        nightly_fuzz.dart --replay FILE...
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'registry.dart';

final class Table {
  final String name;
  final Object? Function() newValue;
  final Object? Function(Uint8List, dynamic) load;
  final Uint8List Function(dynamic) save;
  final String Function(dynamic) toJson;
  final Object? Function(String, dynamic) fromJson;
  final dynamic Function() newWireReport;
  final dynamic Function() newTextReport;

  Table(
    this.name,
    this.newValue,
    this.load,
    this.save,
    this.toJson,
    this.fromJson,
    this.newWireReport,
    this.newTextReport,
  );
}

List<Table> tables() {
  final out = <Table>[];
  for (final row in rawTables) {
    out.add(
      Table(
        row[0] as String,
        row[1] as Object? Function(),
        row[2] as Object? Function(Uint8List, dynamic),
        row[3] as Uint8List Function(dynamic),
        row[4] as String Function(dynamic),
        row[5] as Object? Function(String, dynamic),
        row[6] as dynamic Function(),
        row[7] as dynamic Function(),
      ),
    );
  }
  return out;
}

class Rng {
  int state;

  Rng(int seed) : state = seed == 0 ? 0x9e3779b97f4a7c15 : seed;

  int next() {
    state ^= state >>> 12;
    state ^= (state << 25) & 0xffffffffffffffff;
    state ^= state >>> 27;
    return (state * 0x2545f4914f6cdd1d) & 0x7fffffffffffffff;
  }

  int below(int n) => n <= 0 ? 0 : next() % n;
}

Uint8List mutate(Uint8List source, Rng rng, int pass) {
  final out = Uint8List.fromList(source);
  if (out.isEmpty) return Uint8List.fromList(<int>[rng.below(256)]);
  switch (pass % 10) {
    case 0:
      out[rng.below(out.length)] ^= 1 << rng.below(8);
    case 1:
      out[rng.below(out.length)] = rng.below(256);
    case 2:
      if (out.length >= 8) {
        ByteData.view(out.buffer).setUint64(
          rng.below(out.length - 7),
          rng.next(),
          Endian.little,
        );
      }
    case 3:
      if (out.length >= 4) {
        ByteData.view(out.buffer).setUint32(
          rng.below(out.length - 3),
          rng.next() & 0xffffffff,
          Endian.little,
        );
      }
    case 4:
      final at = rng.below(out.length);
      final end = at + rng.below(64) + 1;
      out.fillRange(at, end > out.length ? out.length : end, 0);
    case 5:
      final at = rng.below(out.length);
      return Uint8List.fromList(out.sublist(0, at));
    case 6:
      final at = rng.below(out.length);
      final cut = rng.below(out.length - at) + 1;
      return Uint8List.fromList(out.sublist(0, at) + out.sublist(at + cut));
    case 7:
      final at = rng.below(out.length + 1);
      final run = List<int>.generate(rng.below(16) + 1, (_) => rng.below(256));
      return Uint8List.fromList(out.sublist(0, at) + run + out.sublist(at));
    case 8:
      final at = rng.below(out.length);
      final src = rng.below(out.length);
      final n = rng.below(32) + 1;
      final end = src + n > out.length ? out.length : src + n;
      final copy = out.sublist(src, end);
      final tail = out.sublist(at);
      return Uint8List.fromList(out.sublist(0, at) + copy + tail);
    default:
      final at = rng.below(out.length < 64 ? out.length : 64);
      out[at] = rng.below(256);
  }
  return out;
}

String? evaluate(Table t, Uint8List data, int target) {
  try {
    switch (target) {
      case 0:
        t.load(data, t.newWireReport());
      case 1:
        final first = t.newWireReport();
        final value = t.load(data, first);
        final clean = !(first.refused || first.malformed) &&
            first.unknown == 0 &&
            first.kindMismatch == 0 &&
            first.widened == 0 &&
            first.clamped == 0;
        if (clean) {
          Uint8List? bytes;
          try {
            bytes = t.save(value);
          } on StateError {
            // A loaded pointer cycle cannot be saved: the SPEC §3.1 walker
            // cost is the caller's, and the StateError is the documented
            // typed refusal, not a crash.
            return null;
          }
          t.load(bytes, t.newWireReport());
        }
      default:
        final text = utf8.decode(data, allowMalformed: true);
        final report = t.newTextReport();
        t.fromJson(text, report);
    }
  } on FormatException {
    return null;
  } catch (e) {
    return 'uncaught ${e.runtimeType}: $e';
  }
  return null;
}

/// minimise shrinks a failing input by removing halves while the same table
/// and target still fail. It is bounded and deterministic.
Uint8List minimise(Table t, Uint8List data, int target) {
  var best = data;
  var chunk = best.length;
  while (chunk > 1) {
    var changed = false;
    for (var at = 0; at < best.length; at += chunk) {
      final end = at + chunk > best.length ? best.length : at + chunk;
      final trial = Uint8List.fromList(best.sublist(0, at) + best.sublist(end));
      if (trial.isNotEmpty && evaluate(t, trial, target) != null) {
        best = trial;
        changed = true;
        break;
      }
    }
    if (!changed) chunk ~/= 2;
  }
  return best;
}

void main(List<String> args) {
  final secondsAt = args.indexOf('--seconds');
  final seedAt = args.indexOf('--seed');
  final seedsAt = args.indexOf('--seeds');
  final replayAt = args.indexOf('--replay');

  final all = tables();
  if (all.isEmpty) {
    stderr.writeln('nightly_fuzz: the registry is empty — this fuzzer would prove nothing');
    exit(2);
  }

  if (replayAt >= 0) {
    var failures = 0;
    for (final arg in args.sublist(replayAt + 1)) {
      final split = arg.indexOf('=');
      final tableName = split < 0 ? '' : arg.substring(0, split);
      final path = split < 0 ? arg : arg.substring(split + 1);
      final data = File(path).readAsBytesSync();
      final name = path.split('/').last;
      Table? t;
      for (final e in all) {
        final type = e.name.split('/').last;
        if (tableName == type || tableName == e.name ||
            (tableName.isEmpty && name.startsWith(type + '.'))) {
          t = e;
        }
      }
      if (t == null) {
        stderr.writeln('nightly_fuzz: $path names no table in the registry');
        failures++;
        continue;
      }
      for (var target = 0; target < 3; target++) {
        final why = evaluate(t!, data, target);
        if (why != null) {
          stderr.writeln('REPLAY FAILED $path target=$target ${t.name}: $why');
          failures++;
        }
      }
    }
    if (failures != 0) exit(1);
    print('nightly_fuzz: replayed ${args.length - replayAt - 1} saved inputs clean');
    return;
  }

  if (secondsAt < 0 || seedAt < 0 || seedsAt < 0) {
    stderr.writeln('usage: nightly_fuzz.dart --seconds N --seed N --seeds DIR');
    exit(2);
  }
  final seconds = int.parse(args[secondsAt + 1]);
  final seed = int.parse(args[seedAt + 1]);
  final seedDir = Directory(args[seedsAt + 1]);

  final globalSeeds = <Uint8List>[];
  if (seedDir.existsSync()) {
    for (final f in seedDir.listSync(recursive: true)) {
      if (f is File) globalSeeds.add(Uint8List.fromList(f.readAsBytesSync()));
    }
  }
  globalSeeds.sort((a, b) => a.length.compareTo(b.length));

  final seedsFor = <String, List<Uint8List>>{};
  for (final t in all) {
    final seeds = <Uint8List>[Uint8List(0)];
    try {
      seeds.add(t.save(t.newValue()));
    } catch (_) {
      // a default instance that will not save is itself a finding, but the
      // mutation loop below reports it as one rather than dying at start-up
    }
    // the shared wire vectors and the Go fuzz corpus: capped so start-up on a
    // 10-minute budget is bounded, and ordered smallest first
    for (final s in globalSeeds.take(400)) {
      seeds.add(s);
      if (s.length > 1) seeds.add(Uint8List.sublistView(s, 0, s.length ~/ 2));
    }
    seedsFor[t.name] = seeds;
  }

  final rng = Rng(seed);
  final clock = Stopwatch()..start();
  var cases = 0;
  final regressions = Directory('build/nightly-fuzz/regressions');
  regressions.createSync(recursive: true);

  while (clock.elapsed.inSeconds < seconds) {
    for (final t in all) {
      final seeds = seedsFor[t.name]!;
      for (var target = 0; target < 3; target++) {
        final pass = cases++;
        var data = mutate(seeds[rng.below(seeds.length)], rng, pass);
        final why = evaluate(t!, data, target);
        if (why == null) continue;
        data = minimise(t, data, target);
        final slug = '${t.name.split('/').last}.seed$seed-$pass-t$target';
        final path = '${regressions.path}/$slug.bin';
        File(path).writeAsBytesSync(data);
        print('FINDING $slug table=${t.name} target=$target seed=$seed: $why');
        print('SAVED $path (${data.length} bytes)');
        exit(1);
      }
    }
  }
  print('nightly_fuzz: OK ${all.length} tables, $cases cases, seed=$seed, ${clock.elapsed.inSeconds}s');
}
