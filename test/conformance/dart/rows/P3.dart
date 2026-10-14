// test/conformance/dart/rows/P3.dart — row dart/P3, "hostile bytes, sweep +
// sanitizer" (docs/roadmap.sexp:3956; audit schema#898, matrix schema#876).
//
// THE LAW. docs/FIXED-FORM-ALGORITHM.md:1686, the fixed form's proof table
// (§7 proof 5), and its ruling in docs/SPEC-TABLES.md §3.4
// (docs/SPEC-TABLES.md:7138):
//
//   "a byte-flip fuzz over the whole file, under a sanitizer, and it is not
//    optional — every offset is arithmetic over sizes a stranger wrote down,
//    so every byte, one bit at a time, is answered one of three ways and never
//    a fourth: a refusal by name, a `malformed` read, or a read that lands
//    values."
//
// and docs/SPEC-TABLES.md §3.4's first sentence
// (docs/SPEC-TABLES.md:7135):
//
//   "Buffers are caller-owned Uint8Lists and nothing throws."
//
// THE SANITIZER. Dart has no ASan, so the memory-safety oracle is the
// language's own bounds check: every read the generated fixed-form reader
// makes is through a ByteData / Uint8List, and a read past the caller's buffer
// THROWS. "No exception escaped the reader" therefore IS "no read left the
// buffer" — a reader that lets one out has stopped refusing and has not landed
// values, and that escaping exception is the fourth answer this row says never
// happens. It is the same substitution the JS row records (its DataView throws)
// and the Elixir row records (its escaping exception class is the oracle).
//
// THE SWEEP. A valid fixed-form Chain file — the P3.schema optional side:
// string(16) name and ?Link { int32 value | min = 0, max = 1000; string(8)
// tag } — is written through the production chainFixedSave, then EVERY byte is
// flipped one bit at a time. Each mutant is read through the production
// chainFixedLoad (a fresh plan and report per mutant) and must answer exactly
// one of the three ways, never a fourth:
//
//   * report.refused != none  — a refusal BY NAME, or
//   * report.malformed        — a malformed read, or
//   * loaded == 1             — a read that LANDS VALUES.
//
// Any thrown exception, and any answer outside those three, is the fourth
// answer and a failure. The sweep counts its three answers and asserts each
// occurred, so a datapath that quietly stopped reaching the reader cannot pass.
//
// THE PRODUCTION PATH. build/tables-generated-dart/p3/, laid down by
// `make build/conformance-dart`: P3Fixed.dart's chainFixedMeasure,
// chainFixedSave, chainFixedLoad and chainFixedNewPlan, over Tblp3Fixed.dart's
// Chain/Link value classes, the known-layout table and the one read loop. The
// same emitter output the conformance driver reaches the generated tables
// through. The vector here is CONSTRUCTED from the law (the tree carries no
// fixed-form P3 bytes under testdata/conformance/tables), and the file is the
// whole file the law says to sweep: the form byte, the reserved bytes, the
// header hash, the layout length and layout bytes, and the record.
//
// THE CONTROL (recorded in RESULT.md). Breaking one constant of this law in
// the generated code — the layoutBytes bound that keeps a hostile layout
// length from walking the layout compare past the buffer — lets a mutant's
// read leave the buffer and throw, and the `escaped == 0` assertion goes RED.
// Restored, the row is GREEN again.
//
// Run from the repository root:  dart run test/conformance/dart/rows/P3.dart
// Exit 0 = green, exit 1 = red. One printed line per assertion.

import 'dart:io';
import 'dart:typed_data';

import '../../../../build/tables-generated-dart/p3/P3Fixed.dart' as p3;
import '../../../../build/tables-generated-dart/p3/Tblp3Fixed.dart'
    as fixed;

int green = 0;
int red = 0;

void check(bool ok, String what) {
  stdout.writeln('${ok ? 'ok  ' : 'FAIL'} $what');
  if (ok) {
    green++;
  } else {
    red++;
  }
}

void putText(Uint8List buffer, String value) {
  buffer.setRange(0, value.length, value.codeUnits);
}

/// Build THE LAWFUL FILE the sweep mutates: one Chain with the optional Link
/// present, so every region of the file the law names is non-trivial — a name
/// short of its bound (its slack must still be zeros), a present byte, a value
/// at 42 inside 0..1000, and a tag short of its bound.
Uint8List lawfulFile() {
  final chain = fixed.Chain();
  putText(chain.name, 'p3-row');
  chain.nameLength = 6;
  chain.linkPresent = true;
  chain.link.value = 42;
  putText(chain.link.tag, 'tag');
  chain.link.tagLength = 3;

  final bytes = Uint8List(p3.chainFixedMeasure(1));
  final wrote = p3.chainFixedSave(<fixed.Chain>[chain], 1, bytes);
  if (wrote != bytes.length) {
    stderr.writeln('FAIL: chainFixedSave wrote $wrote of ${bytes.length}');
    red++;
  }
  return bytes;
}

/// ONE MUTANT, ONE ANSWER. Reads the mutant through the production entrypoint
/// and returns one word for which of the three answers it got, or a fourth
/// description so a failure can name it.
String answer(Uint8List mutant) {
  final values = <fixed.Chain>[fixed.Chain()];
  final report = fixed.TableFixedReport();
  final plan = p3.chainFixedNewPlan();
  final loaded = p3.chainFixedLoad(
    values,
    1,
    mutant,
    mutant.length,
    plan,
    report,
  );
  if (report.refused != fixed.TableFixedRefusal.none) {
    return 'refusal:${fixed.TableFixedRefusal.name(report.refused)}';
  }
  if (report.malformed) {
    return 'malformed';
  }
  if (loaded == 1) {
    return 'landed';
  }
  return 'fourth(loaded=$loaded malformed=${report.malformed} '
      'refused=${report.refused})';
}

void main() {
  final source = lawfulFile();

  // ---- the lawful file opens and lands values (the baseline the sweep needs)
  {
    final back = <fixed.Chain>[fixed.Chain()];
    final report = fixed.TableFixedReport();
    final loaded = p3.chainFixedLoad(
      back,
      1,
      source,
      source.length,
      p3.chainFixedNewPlan(),
      report,
    );
    check(loaded == 1, 'lawful file: chainFixedLoad answers 1 record ($loaded)');
    check(
      !report.malformed && report.refused == fixed.TableFixedRefusal.none,
      'lawful file: a clean read moves no refusal and is not malformed',
    );
    check(back[0].linkPresent, 'lawful file: the optional Link is present');
    check(back[0].link.value == 42, 'lawful file: link.value lands 42');
    check(back[0].nameLength == 6, 'lawful file: nameLength lands 6');
    check(source[0] == fixed.tableFixedForm, 'lawful file: form byte 3');
  }

  // ---- THE SWEEP: every byte, one bit at a time, over the whole file
  var refusals = 0;
  var malformed = 0;
  var landed = 0;
  var escaped = 0;
  var fourth = 0;
  var firstEscape = '';
  var firstFourth = '';
  final total = source.length * 8;

  for (var i = 0; i < source.length; i++) {
    for (var b = 0; b < 8; b++) {
      final mutant = Uint8List.fromList(source);
      mutant[i] ^= 1 << b;
      try {
        final a = answer(mutant);
        if (a == 'malformed') {
          malformed++;
        } else if (a == 'landed') {
          landed++;
        } else if (a.startsWith('refusal:')) {
          refusals++;
        } else {
          fourth++;
          if (firstFourth.isEmpty) {
            firstFourth = 'byte $i bit $b: $a';
          }
        }
      } catch (e) {
        escaped++;
        if (firstEscape.isEmpty) {
          firstEscape = 'byte $i bit $b: $e';
        }
      }
    }
  }

  check(
    escaped == 0,
    'sanitizer: no single-bit mutant made a read leave the buffer — '
    '$escaped exceptions escaped a reader'
    '${firstEscape.isEmpty ? '' : ' (first: $firstEscape)'}',
  );
  check(
    fourth == 0,
    'never a fourth answer: every mutant was a refusal, malformed, or a read '
    'that lands values — $fourth otherwise'
    '${firstFourth.isEmpty ? '' : ' (first: $firstFourth)'}',
  );
  check(
    refusals + malformed + landed + escaped + fourth == total,
    'the sweep answers every mutant exactly once: '
    '${refusals + malformed + landed + escaped + fourth} of $total',
  );
  check(
    refusals > 0,
    'a refusal by name occurs under the sweep: $refusals mutants '
    '(the form byte, the header hash and the record hash)',
  );
  check(
    malformed > 0,
    'a malformed read occurs under the sweep: $malformed mutants '
    '(the reserved bytes and the layout)',
  );
  check(
    landed > 0,
    'a read that lands values occurs under the sweep: $landed mutants '
    '(the record body)',
  );

  stdout.writeln(
    'P3: ${source.length} bytes, $total single-bit mutants — '
    '$refusals refused, $malformed malformed, $landed landed, '
    '$escaped escaped — '
    '${red == 0 ? 'GREEN' : 'RED'}',
  );
  exit(red == 0 ? 0 : 1);
}