// C12 — A BOOL OR PRESENT BYTE OTHER THAN 0 OR 1 REFUSES wire_damaged
// (docs/FIXED-FORM-ALGORITHM.md §4.5, the last row of the scatter table):
//
//   "a `bool`, a PRESENT FLAG, or the `present` op: a byte of `0` or `1`
//   lands as the language's own false or true; any other byte refuses
//   `wire_damaged` (SPEC-TABLES §4, §6.5): no writer emits one, and `0x02` is
//   neither stored verbatim nor normalized."
//
// The retired answer (land `byte != 0`, normalised to true) is gone: today's
// law is STRICT, the byte refuses the whole load by name (§5.3 step 11, the
// CHECK pass), the load answers -1, and every row handed in is left at its
// declared defaults (docs/SPEC-TABLES.md §6.5).
//
// THE TREE. The row reads test/tables/HB.schema's `Hostile` record (a bool,
// an optional, and a trailing uint32 bracketing them, the shape the cpp
// leg's C13 reads). No make target generates HB for Dart, so this row
// generates it itself, the way test/conformance/elixir/rows/R16.exs does:
// `bin/schema generate --lang dart` into build/conformance-dart-rows/C12/,
// then a driver written beside it runs under the same `dart --enable-asserts
// run` that runs this file. A failed generate or a failed driver is red,
// with its output.
//
// RUN (from anywhere): dart --enable-asserts run test/conformance/dart/rows/C12.dart
import 'dart:io';

const _driver = r'''
import 'dart:io';
import 'dart:typed_data';

import 'hb/HBFixed.dart';

int failures = 0;

void check(bool ok, String what) {
  if (ok) {
    stdout.writeln('PASS: $what');
  } else {
    stdout.writeln('FAIL: $what');
    failures++;
  }
}

Uint8List lawful() {
  final v = Hostile();
  initHostile(v);
  v.flag = true;
  v.linkPresent = true;
  v.link.n = 11;
  v.trail = 0xBB;
  final bytes = Uint8List(hostileFixedMeasure(1));
  final n = hostileFixedSave([v], 1, bytes);
  check(n == hostileFixedMeasure(1), 'C12: lawful record saves (got $n)');
  return bytes;
}

/// A row handed in DIRTY, so "left at its declared defaults" is measured.
Hostile poisoned() {
  final back = Hostile();
  initHostile(back);
  back.flag = true;
  back.linkPresent = true;
  back.link.n = 0x5A5A5A5A;
  back.trail = 0x5A5A5A5A;
  return back;
}

void main() {
  // body starts at hostileFixedHeaderBytes + the record hash (8 bytes):
  // flag at +0, link's present byte at +1, link.n at +2, trail at +6.
  final bodyAt = hostileFixedHeaderBytes + 8;
  final fresh = Hostile();
  initHostile(fresh);

  // ---- CONTROL: the lawful record reads whole ----
  {
    final bytes = lawful();
    final back = poisoned();
    final report = TableFixedReport();
    final got = hostileFixedLoad(
        [back], 1, bytes, bytes.length, hostileFixedNewPlan(), report);
    check(
      got == 1 && report.refused == TableFixedRefusal.none,
      'CONTROL: the lawful record reads (got $got, '
      'refused=${TableFixedRefusal.name(report.refused)})',
    );
    check(
      back.flag && back.linkPresent && back.link.n == 11 && back.trail == 0xBB,
      'CONTROL: every field lands from the wire (flag=${back.flag} '
      'linkPresent=${back.linkPresent} n=${back.link.n} trail=${back.trail})',
    );
  }

  // ---- a bool byte, then a present byte, other than 0/1 ----
  final cases = <(String, int, int)>[
    ('a bool byte of 0x02', 0, 2),
    ('a bool byte of 0xFF', 0, 0xFF),
    ('a present byte of 0x02', 1, 2),
    ('a present byte of 0xFF', 1, 0xFF),
  ];
  for (final (what, offset, byte) in cases) {
    final bytes = lawful();
    bytes[bodyAt + offset] = byte;
    final back = poisoned();
    final report = TableFixedReport();
    final got = hostileFixedLoad(
        [back], 1, bytes, bytes.length, hostileFixedNewPlan(), report);
    check(got == -1, 'C12: $what refuses the load (got $got)');
    check(
      report.refused == TableFixedRefusal.damaged &&
          TableFixedRefusal.name(report.refused) == 'wire_damaged',
      'C12: $what refuses wire_damaged by name '
      '(got ${TableFixedRefusal.name(report.refused)})',
    );
    check(
      back.flag == fresh.flag &&
          back.linkPresent == fresh.linkPresent &&
          back.link.n == fresh.link.n &&
          back.trail == fresh.trail,
      'C12: $what lands nothing: the row is at its declared defaults '
      '(flag=${back.flag} linkPresent=${back.linkPresent} '
      'n=${back.link.n} trail=${back.trail}; want flag=${fresh.flag} '
      'linkPresent=${fresh.linkPresent} n=${fresh.link.n} '
      'trail=${fresh.trail})',
    );
    check(
      report.unknown == 0 && report.widened == 0,
      'C12: $what moves no counter '
      '(unknown=${report.unknown} widened=${report.widened})',
    );
  }

  if (failures > 0) {
    stdout.writeln('C12: $failures FAILED');
    exit(1);
  }
  stdout.writeln('C12: a bool or present byte other than 0/1 refuses '
      'wire_damaged (Dart)');
}
''';

void main() {
  final repo = Directory.fromUri(Platform.script.resolve('../../../../'));
  final root = repo.uri.toFilePath();
  final work = Directory('${root}build/conformance-dart-rows/C12');
  if (work.existsSync()) {
    work.deleteSync(recursive: true);
  }
  work.createSync(recursive: true);

  final gen = Process.runSync('${root}bin/schema', [
    'generate',
    '--lang',
    'dart',
    '--out',
    '${work.path}/hb',
    '${root}test/tables/HB.schema',
  ]);
  if (gen.exitCode != 0) {
    stdout.writeln('FAIL: C12: bin/schema generate --lang dart HB.schema '
        'exited ${gen.exitCode}\n${gen.stdout}${gen.stderr}');
    exit(1);
  }

  final driver = File('${work.path}/c12_driver.dart');
  driver.writeAsStringSync(_driver);
  final run = Process.runSync(
      Platform.resolvedExecutable, ['--enable-asserts', 'run', driver.path]);
  stdout.write(run.stdout);
  stderr.write(run.stderr);
  if (run.exitCode != 0) {
    stdout.writeln('FAIL: C12: the driver exited ${run.exitCode}');
    exit(1);
  }
}
