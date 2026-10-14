// R19.dart — dart/R19: "a clean NEW-READS-OLD of an appended field, variant,
// arm, flag or keyed slot moves no counter at all" (docs/FIXED-FORM-ALGORITHM.md
// §5.4 line 947).
//
// THE LAW: "a clean NEW-READS-OLD of an appended field, variant, arm, flag or
// keyed slot | none | every counter stays at zero: an append the reader knows
// is not an event."
//
// This test generates VOLD_/VNEW_ Dart code at test time (the pairs live at
// test/tables/VOLD_<row>.schema / VNEW_<row>.schema), injects the VOLD layout
// into VNEW's lineage, writes a probe that saves with VOLD and loads with VNEW,
// and asserts that every counter stays at zero. Five rows, one per append kind:
//
//   field_append            — an appended FIELD (COPY op)
//   enum_append             — an appended VARIANT (ORDINAL op)
//   union_append            — an appended ARM (CONST op)
//   flags_append            — an appended FLAG (ordinal op)
//   keyed_array_enum_append — an appended KEYED SLOT (ordinal op)
//
// Run: dart run test/conformance/dart/rows/R19.dart
// Exit 0 green, exit 1 red, one printed line per assertion.
import 'dart:io';

int _failures = 0;

void check(bool ok, String what) {
  if (!ok) {
    stderr.writeln('FAIL: $what');
    _failures++;
  }
}

// ---- code generation --------------------------------------------------------

(String, String) _generatePair(String row) {
  final base = 'build/r19/$row';
  if (Directory(base).existsSync()) {
    Directory(base).deleteSync(recursive: true);
  }
  final voldDir = '$base/vold';
  final vnewDir = '$base/vnew';
  Directory(voldDir).createSync(recursive: true);
  Directory(vnewDir).createSync(recursive: true);

  var r = Process.runSync('./bin/schema', [
    'generate', '--lang', 'dart', '--out', voldDir,
    'test/tables/VOLD_$row.schema',
  ]);
  if (r.exitCode != 0) {
    check(false, '$row: generate VOLD_ failed: ${r.stderr}');
    return ('', '');
  }
  r = Process.runSync('./bin/schema', [
    'generate', '--lang', 'dart', '--out', vnewDir,
    'test/tables/VNEW_$row.schema',
  ]);
  if (r.exitCode != 0) {
    check(false, '$row: generate VNEW_ failed: ${r.stderr}');
    return ('', '');
  }
  return (voldDir, vnewDir);
}

// ---- file discovery ---------------------------------------------------------

String _findFixedFile(String dir) {
  final files = Directory(dir).listSync().whereType<File>()
    .where((f) => f.path.endsWith('Fixed.dart') && !f.path.contains('Tbl'))
    .toList();
  if (files.length != 1) {
    throw StateError('Expected 1 *Fixed.dart in $dir, got ${files.length}');
  }
  return files.first.path;
}

/// Get just the filename from a path.
String _fname(String path) => path.split(Platform.pathSeparator).last;

// ---- lineage injection ------------------------------------------------------

/// Extract the TableFixedKnownLayout(...) entry text from a generated file.
String _extractKnownEntry(String fixedFile) {
  final content = File(fixedFile).readAsStringSync();
  final knownStart = content.indexOf('lineageFixedKnown = <TableFixedKnownLayout>[');
  if (knownStart < 0) throw StateError('No lineageFixedKnown in $fixedFile');
  final entryStart = content.indexOf('TableFixedKnownLayout(', knownStart);
  if (entryStart < 0) throw StateError('No TableFixedKnownLayout in $fixedFile');
  var depth = 0;
  var i = entryStart;
  while (i < content.length) {
    if (content[i] == '(') depth++;
    else if (content[i] == ')') {
      depth--;
      if (depth == 0) { i++; break; }
    }
    i++;
  }
  return content.substring(entryStart, i);
}

/// Inject VOLD_'s known entry into VNEW_'s generated Fixed file.
void _injectLineage(String vnewFixedFile, String voldEntry) {
  var content = File(vnewFixedFile).readAsStringSync();
  final marker = 'lineageFixedKnown = <TableFixedKnownLayout>[';
  final markerIdx = content.indexOf(marker);
  if (markerIdx < 0) throw StateError('No lineageFixedKnown in $vnewFixedFile');
  final bracketIdx = content.indexOf('[', markerIdx);
  final firstEntryIdx = content.indexOf('TableFixedKnownLayout(', bracketIdx);
  if (firstEntryIdx < 0) throw StateError('No first entry in $vnewFixedFile');
  // Insert VOLD_'s entry before VNEW_'s entry.
  // The content before firstEntryIdx ends with "[\n  " (the opening bracket
  // and the indent before VNEW_'s entry). We insert "  $voldEntry,\n" to add
  // VOLD_'s entry with proper indentation.
  content = content.substring(0, firstEntryIdx) +
      '  $voldEntry,\n' +
      content.substring(firstEntryIdx);
  File(vnewFixedFile).writeAsStringSync(content);
}

// ---- probe generation -------------------------------------------------------

String _buildProbe(String row, String voldFixedFile, String vnewFixedFile,
    String voldDeclFile, String vnewDeclFile, String body) {
  final voldFixed = _fname(voldFixedFile);
  final vnewFixed = _fname(vnewFixedFile);
  final voldDecl = _fname(voldDeclFile);
  final vnewDecl = _fname(vnewDeclFile);
  return '''// GENERATED PROBE — R19.dart $row
import 'dart:io';
import 'dart:typed_data';
import 'vold/$voldDecl' as voldDecl;
import 'vold/$voldFixed' as vold;
import 'vnew/$vnewDecl' as vnewDecl;
import 'vnew/$vnewFixed' as vnew;

void check(bool ok, String what) {
  if (!ok) { stderr.writeln('FAIL: \$what'); exitCode = 1; }
}

String why(vnew.TableFixedReport r) =>
    'refused=\${r.refused} malformed=\${r.malformed} '
    'unknown=\${r.unknown} kind=\${r.kindMismatch} clamped=\${r.clamped} '
    'widened=\${r.widened} duplicate=\${r.duplicate}';

void main() {
$body
  if (exitCode == 0) stdout.writeln('ok: $row clean NEW-READS-OLD');
}
''';
}

// ---- probe bodies -----------------------------------------------------------

String _fieldAppendProbe() => '''
  final old = vold.Lineage();
  vold.initLineage(old);
  old.x = 11; old.y = 22; old.z = 33;
  final buf = Uint8List(vold.lineageFixedMeasure(1));
  vold.lineageFixedSave(<vold.Lineage>[old], 1, buf);
  final back = <vnew.Lineage>[vnew.Lineage()];
  vnew.initLineage(back[0]);
  final plan = vnew.lineageFixedNewPlan();
  final report = vnew.TableFixedReport();
  final n = vnew.lineageFixedLoad(back, 1, buf, buf.length, plan, report);
  check(n == 1, 'field_append: n=\$n \${why(report)}');
  check(back[0].x == 11 && back[0].y == 22 && back[0].z == 33,
      'field_append: old values did not land');
  check(back[0].w == 77, 'field_append: appended field not its default: \${back[0].w}');
  check(report.unknown == 0 && report.kindMismatch == 0 &&
        report.clamped == 0 && report.widened == 0 && report.duplicate == 0,
      'field_append: counters moved: \${why(report)}');
''';

String _enumAppendProbe() => '''
  final old = vold.Lineage();
  vold.initLineage(old);
  old.tier = voldDecl.Tier.gold; old.seq = 9;
  final buf = Uint8List(vold.lineageFixedMeasure(1));
  vold.lineageFixedSave(<vold.Lineage>[old], 1, buf);
  final back = <vnew.Lineage>[vnew.Lineage()];
  vnew.initLineage(back[0]);
  final plan = vnew.lineageFixedNewPlan();
  final report = vnew.TableFixedReport();
  final n = vnew.lineageFixedLoad(back, 1, buf, buf.length, plan, report);
  check(n == 1, 'enum_append: n=\$n \${why(report)}');
  check(back[0].tier == vnewDecl.Tier.gold,
      'enum_append: Gold did not land: \${back[0].tier}');
  check(back[0].seq == 9, 'enum_append: seq did not land: \${back[0].seq}');
  check(report.unknown == 0 && report.kindMismatch == 0 &&
        report.clamped == 0 && report.widened == 0 && report.duplicate == 0,
      'enum_append: counters moved: \${why(report)}');
''';

String _unionAppendProbe() => '''
  final old = vold.Lineage();
  vold.initLineage(old);
  old.pick.type = voldDecl.PickType.alpha;
  old.pick.alpha.m = 7;
  old.seq = 5;
  final buf = Uint8List(vold.lineageFixedMeasure(1));
  vold.lineageFixedSave(<vold.Lineage>[old], 1, buf);
  final back = <vnew.Lineage>[vnew.Lineage()];
  vnew.initLineage(back[0]);
  final plan = vnew.lineageFixedNewPlan();
  final report = vnew.TableFixedReport();
  final n = vnew.lineageFixedLoad(back, 1, buf, buf.length, plan, report);
  check(n == 1, 'union_append: n=\$n \${why(report)}');
  check(back[0].pick.type == vnewDecl.PickType.alpha,
      'union_append: tag did not land: \${back[0].pick.type}');
  check(back[0].pick.alpha.m == 7,
      'union_append: payload did not land: \${back[0].pick.alpha.m}');
  check(back[0].seq == 5, 'union_append: seq did not land: \${back[0].seq}');
  check(report.unknown == 0 && report.kindMismatch == 0 &&
        report.clamped == 0 && report.widened == 0 && report.duplicate == 0,
      'union_append: counters moved: \${why(report)}');
''';

String _flagsAppendProbe() => '''
  final old = vold.Lineage();
  vold.initLineage(old);
  old.caps = voldDecl.capsJump; old.seq = 5;
  final buf = Uint8List(vold.lineageFixedMeasure(1));
  vold.lineageFixedSave(<vold.Lineage>[old], 1, buf);
  final back = <vnew.Lineage>[vnew.Lineage()];
  vnew.initLineage(back[0]);
  final plan = vnew.lineageFixedNewPlan();
  final report = vnew.TableFixedReport();
  final n = vnew.lineageFixedLoad(back, 1, buf, buf.length, plan, report);
  check(n == 1, 'flags_append: n=\$n \${why(report)}');
  check(report.unknown == 0 && report.kindMismatch == 0 &&
        report.clamped == 0 && report.widened == 0 && report.duplicate == 0,
      'flags_append: counters moved: \${why(report)}');
''';

String _keyedArrayEnumAppendProbe() => '''
  final old = vold.Lineage();
  vold.initLineage(old);
  old.seq = 0;
  final buf = Uint8List(vold.lineageFixedMeasure(1));
  vold.lineageFixedSave(<vold.Lineage>[old], 1, buf);
  final back = <vnew.Lineage>[vnew.Lineage()];
  vnew.initLineage(back[0]);
  final plan = vnew.lineageFixedNewPlan();
  final report = vnew.TableFixedReport();
  final n = vnew.lineageFixedLoad(back, 1, buf, buf.length, plan, report);
  check(n == 1, 'keyed_array_enum_append: n=\$n \${why(report)}');
  check(back[0].slots[3].n == 7,
      'keyed_array_enum_append: appended slot not element default: \${back[0].slots[3].n}');
  check(report.unknown == 0 && report.kindMismatch == 0 &&
        report.clamped == 0 && report.widened == 0 && report.duplicate == 0,
      'keyed_array_enum_append: counters moved: \${why(report)}');
''';

// ---- main --------------------------------------------------------------------

void main() {
  // Each row: (schema row name, probe body, needs non-Fixed import)
  final rows = <(String, String)>[
    ('field_append', _fieldAppendProbe()),
    ('enum_append', _enumAppendProbe()),
    ('union_append', _unionAppendProbe()),
    ('flags_append', _flagsAppendProbe()),
    ('keyed_array_enum_append', _keyedArrayEnumAppendProbe()),
  ];

  for (final (row, body) in rows) {
    _runAppendTest(row, body);
  }

  if (_failures > 0) {
    stderr.writeln('R19: $_failures assertion(s) failed');
    exit(1);
  }
  stdout.writeln('R19: all 5 append kinds — clean NEW-READS-OLD moves no counter');
  exit(0);
}

void _runAppendTest(String row, String body) {
  final (voldDir, vnewDir) = _generatePair(row);
  if (voldDir.isEmpty) return;

  final voldFixed = _findFixedFile(voldDir);
  final vnewFixed = _findFixedFile(vnewDir);

  // Find the non-Fixed dart file (for enum/union/flags types).
  final voldDeclFile = voldFixed.replaceAll('Fixed.dart', '.dart');
  final vnewDeclFile = vnewFixed.replaceAll('Fixed.dart', '.dart');

  // Inject VOLD_'s layout into VNEW_'s lineage.
  final voldEntry = _extractKnownEntry(voldFixed);
  _injectLineage(vnewFixed, voldEntry);

  // Write and run probe.
  final probe = _buildProbe(row, voldFixed, vnewFixed, voldDeclFile, vnewDeclFile, body);
  final probePath = 'build/r19/$row/probe.dart';
  File(probePath).writeAsStringSync(probe);

  final compileResult = Process.runSync('dart', [
    'compile', 'exe', '-o', 'build/r19/$row/probe', probePath,
  ]);
  if (compileResult.exitCode != 0) {
    check(false, '$row: probe compile failed:\n${compileResult.stderr}');
    return;
  }

  final runResult = Process.runSync('build/r19/$row/probe', []);
  if (runResult.exitCode != 0) {
    check(false, '$row: probe failed:\n${runResult.stdout}${runResult.stderr}');
  } else {
    final output = '${runResult.stdout}'.trim();
    stdout.writeln(output);
  }
}
