// R22 — THE CLOSURE RULE, the dart leg's cell (docs/roadmap.sexp dart/R22).
//
// THE LAW (docs/FIXED-FORM-ALGORITHM.md §5.5, lines 961-963):
//
//   `closure(T)` is `T`, every `table` or `type` it reaches by value, every
//   enum, union, flags and constant any of them names, recursively. Every
//   table or type in it is declared `fixed`; a pointer, map or unbounded
//   array in it is a compile refusal; `T` is never in its own closure.
//
// docs/SPEC-TABLES.md §2.2 states the same refusal ("the compiler REFUSES in
// its by-value closure anything that would make a body variable size, naming
// the field and the table") and §3.4 the walk's edges ("`BY-VALUE CLOSURE`
// reaches through every by-value edge there is"; "a nested `fixed table`
// stops the walk, because its own closure was checked at its own
// declaration").
//
// FOR THE DART LEG the compile refusal IS the dart toolchain's: the leg's
// production entrypoint is `./bin/schema generate --lang dart` (the command
// every build target here runs — make/dart.mk), which walks the closure in
// ir/table.go `FixedClosureBreaks` through internal/check/check_fixed.go and
// refuses BY NAME. So this test drives that actual path — one subprocess per
// case, exit status and refusal text asserted, and the out directory
// required to stay EMPTY (a refusal emits nothing) — and then reads the
// GENERATED DART the way the driver reads it
// (test/conformance/dart/main.dart:15: a relative import into
// build/tables-generated-dart/), walking a fixed unit's block-descriptor
// graph: every table or type reached by value is itself fixed, no construct
// the law refuses survives into emitted code, and the graph is a tree —
// T is never in its own closure.
//
// Standalone: `dart run test/conformance/dart/rows/R22.dart` from the repo
// root. Exit 0 green, 1 red, one printed line per assertion. Depends on no
// other rows/ file, edits no tracked file (scratch lives in build/, which
// git ignores), and needs the leg's built state the driver needs:
// bin/schema and build/tables-generated-dart/ (the PREFLIGHT builds both).
import 'dart:io';

import '../../../../build/tables-generated-dart/block/BlockdemoBlock.dart';
import '../../../../build/tables-generated-dart/block/PaddedBlock.dart'
    as padded;

var failures = 0;

void check(bool ok, String line) {
  failures += ok ? 0 : 1;
  stdout.writeln('${ok ? 'ok' : 'FAIL'} R22 $line');
}

// refuse runs the leg's production entrypoint over one schema and asserts
// the law's refusal: a non-zero exit, the construct NAMED in the output, and
// no dart emitted. `phrase` is the clause of the refusal that says WHICH
// construct broke the closure.
void refuse(String name, String schemaText, String phrase) {
  final scratch = Directory('build/r22-rows/$name');
  if (scratch.existsSync()) {
    scratch.deleteSync(recursive: true);
  }
  scratch.createSync(recursive: true);
  final schema = File('build/r22-rows/$name/T.schema');
  schema.writeAsStringSync('package r22probe\n\n$schemaText\n');
  final out = 'build/r22-rows/$name/out';
  final run = Process.runSync('./bin/schema', [
    'generate',
    '--lang',
    'dart',
    '--out',
    out,
    schema.path,
  ]);
  final said = '${run.stdout}${run.stderr}';
  if (run.exitCode == 0) {
    check(
      false,
      '$name: the source was accepted — the closure rule is gone '
      '($schemaText)',
    );
    return;
  }
  if (!said.contains(phrase)) {
    check(
      false,
      '$name: refused, but not by the law\'s own words — '
      'wanted "$phrase", got: ${said.trim()}',
    );
    return;
  }
  final written = Directory(out).existsSync()
      ? Directory(out).listSync().whereType<File>().length
      : 0;
  check(
    written == 0,
    '$name: refused by name and nothing emitted '
    '("$phrase")',
  );
}

void main() {
  // ---- THE REFUSALS — the law's three clauses, by name, through
  // ---- ./bin/schema generate --lang dart.

  // every TABLE reached by value is itself fixed: a plain `table` in the
  // closure is refused (ir/table.go:231 — "holds the plain table").
  refuse(
    'plain-table-by-value',
    'fixed table T {\n'
        '  q Q\n'
        '}\n'
        '\n'
        'table Q {\n'
        '  x int32\n'
        '}\n',
    'holds the plain table Q by value',
  );

  // a POINTER in the closure is a compile refusal (ir/table.go:224).
  refuse(
    'pointer',
    'fixed table T {\n'
        '  p *P\n'
        '}\n'
        '\n'
        'table P {\n'
        '  x int32\n'
        '}\n',
    'is a pointer',
  );

  // a MAP in the closure is a compile refusal (ir/table.go:218).
  refuse(
    'map',
    'fixed table T {\n'
        '  m map[int32]int32\n'
        '}\n',
    'is a map',
  );

  // an UNBOUNDED ARRAY in the closure is a compile refusal (ir/table.go:220).
  refuse(
    'unbounded-array',
    'fixed table T {\n'
        '  xs []int32\n'
        '}\n',
    'is an unbounded array',
  );

  // a byte buffer is a pointer at a blob node — the closure refuses the
  // unbounded spelling and keeps the bounded one (ir/table.go:222; the
  // corpus's bytes(12) in PaddedFrame compiles, see the walk below).
  refuse(
    'byte-buffer',
    'fixed table T {\n'
        '  data *bytes\n'
        '}\n',
    'is a byte buffer',
  );

  // T IS NEVER IN ITS OWN CLOSURE: holding itself by value is refused —
  // by the composition-cycle gate the assembler runs before the closure walk
  // ("type composition cycle: T -> T"), which is a compile refusal all the
  // same, and it names the cycle.
  refuse(
    'self-by-value',
    'fixed table T {\n'
        '  inner T\n'
        '}\n',
    'type composition cycle: T -> T',
  );

  // ... and through a `type` held by value, one hop out.
  refuse(
    'self-through-type',
    'fixed table T {\n'
        '  p P\n'
        '}\n'
        '\n'
        'type P {\n'
        '  p P\n'
        '}\n',
    'type composition cycle: P -> P',
  );

  // ---- THE POSITIVE HALF — a closure that OBEYS the law generates.
  //
  // An enum, a `type` held by value, a nested FIXED table held by value and
  // a bounded array: every member of T's by-value closure is fixed, so the
  // dart leg emits the unit — the fixed form (form 3) among its files.
  {
    final scratch = Directory('build/r22-rows/all-fixed-closure');
    if (scratch.existsSync()) {
      scratch.deleteSync(recursive: true);
    }
    scratch.createSync(recursive: true);
    File('build/r22-rows/all-fixed-closure/Probe.schema').writeAsStringSync(
      'package r22probe\n'
      '\n'
      'enum Color { Red, Green }\n'
      '\n'
      'type Vec {\n'
      '  x float32\n'
      '  y float32\n'
      '}\n'
      '\n'
      'fixed table Inner {\n'
      '  n int32\n'
      '}\n'
      '\n'
      'fixed table Probe {\n'
      '  c Color\n'
      '  v Vec\n'
      '  xs [4]int32\n'
      '  inner Inner\n'
      '}\n',
    );
    final run = Process.runSync('./bin/schema', [
      'generate',
      '--lang',
      'dart',
      '--out',
      'build/r22-rows/all-fixed-closure/out',
      'build/r22-rows/all-fixed-closure/Probe.schema',
    ]);
    final fixed = run.exitCode == 0
        ? Directory('build/r22-rows/all-fixed-closure/out')
              .listSync()
              .whereType<File>()
              .where((f) => f.path.endsWith('/ProbeFixed.dart'))
              .toList()
        : <File>[];
    check(
      run.exitCode == 0 && fixed.length == 1,
      'all-fixed-closure: an enum, a type, a nested fixed table and a '
      'bounded array generate the unit\'s fixed form ProbeFixed.dart '
      '(exit ${run.exitCode}, ${fixed.length} ProbeFixed.dart)',
    );
  }

  // ---- THE CLOSURE, READ BACK FROM THE GENERATED DART ----
  //
  // The corpus's own fixed unit (tables/block/Padded.schema, reached the way
  // test/conformance/dart/main.dart reaches generated code). PaddedFrame
  // holds [..MaxPaddedRows]PaddedRow BY VALUE — the corpus's own by-value
  // edge — so the walk below crosses a table-reached-by-value and asserts
  // the law holds in what the dart leg EMITTED:
  //   - every record reached by value declares a fixed size and alignment
  //     (it is itself fixed);
  //   - every inline field declares one slot's storage, every counted field
  //     a DECLARED bound — no pointer, no map, no unbounded array survived
  //     into the emitted closure;
  //   - the walk keeps the path and fails if a record is already on it:
  //     T is never in its own closure, so the descriptor graph is a tree.
  void walkRecord(TableBlockInfo info, List<String> path) {
    final where = path.join(' -> ');
    check(
      info.size > 0 && info.align > 0,
      'descriptor ${info.name} ($where): a record reached by value declares '
      'its fixed size ${info.size} and alignment ${info.align}',
    );
    if (path.contains(info.name)) {
      check(false, 'descriptor ${info.name} ($where): is in its own closure');
      return;
    }
    for (final f in info.fields) {
      if (f.outOfLine) {
        // an out-of-line array of a fixed closure: the triple is declared
        // and the bound is IN THE DECLARATION — the count may be the data's
        // nowhere in a fixed body (§2.2).
        final bounded = f.arrayBound > 0;
        check(
          f.offsetOfOffset >= 0 &&
              f.countOffset >= 0 &&
              f.strideOffset >= 0 &&
              f.stride > 0 &&
              bounded,
          'descriptor ${info.name}.${f.name}: an out-of-line array declares '
          'its triple and its bound ${f.arrayBound} — no unbounded array '
          'survived into emitted dart',
        );
        final row = f.element;
        if (row == null) {
          check(false, 'descriptor ${info.name}.${f.name}: names no row');
          continue;
        }
        walkRecord(row, [...path, '${info.name}.${f.name}']);
        continue;
      }
      check(
        f.elemSize > 0,
        'descriptor ${info.name}.${f.name}: an inline field declares its '
        'slot\'s storage (${f.elemSize} byte(s)) — no pointer, no map',
      );
      if (f.counted) {
        check(
          f.arrayBound > 0,
          'descriptor ${info.name}.${f.name}: a counted field declares its '
          'bound ${f.arrayBound} (string(N)/bytes(N) have a size the '
          'declaration states)',
        );
      }
      final nested = f.element;
      if (nested != null) {
        walkRecord(nested, [...path, '${info.name}.${f.name}']);
      }
    }
  }

  walkRecord(padded.PaddedFrameBlock.type, ['PaddedFrameBlock.type']);

  // ---- the verdict
  if (failures > 0) {
    stdout.writeln(
      'FAIL R22: $failures assertion(s) red — the closure rule '
      'does not hold for the dart leg',
    );
    exit(1);
  }
  stdout.writeln(
    'ok R22: the closure rule holds for the dart leg — '
    'every table or type reached by value is itself fixed, a pointer, map '
    'or unbounded array in the closure is a compile refusal, and T is '
    'never in its own closure',
  );
  exit(0);
}
