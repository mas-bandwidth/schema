// test/conformance/dart/rows/P3.dart — row dart/P3, "hostile bytes, sweep +
// sanitizer" (docs/roadmap.sexp:3956; audit schema#898, matrix schema#876).
//
// THE LAW. docs/FIXED-FORM-ALGORITHM.md §7 proof 5 (the fixed form's proof
// table), today's text:
//
//   "a byte-flip fuzz over the whole file, under a sanitizer, and it is not
//    optional — every offset is arithmetic over sizes a stranger wrote down,
//    so every byte, one bit at a time, is answered one of two ways and never
//    a third: a refusal by one name, or a read that lands values."
//
// There is no `malformed` read on this form: every framing and layout damage
// is a refusal by name (§5.3's refusal table). AND REFUSE IS TOTAL: a refusal
// answers -1, moves no counter and leaves every row handed in at its declared
// defaults (§5.3, SPEC-TABLES §6.5), so a refusal that returns a count or
// leaves a partly landed row is the third answer too.
//
// and docs/SPEC-TABLES.md §3.4's first sentence:
//
//   "Buffers are caller-owned Uint8Lists and nothing throws."
//
// THE SANITIZER. Dart has no ASan, so the memory-safety oracle is the
// language's own bounds check: every read the generated fixed-form reader
// makes is through a ByteData / Uint8List, and a read past the caller's buffer
// THROWS. "No exception escaped the reader" therefore IS "no read left the
// buffer" — a reader that lets one out has stopped refusing and has not landed
// values, and that escaping exception is the third answer this row says never
// happens. It is the same substitution the JS row records (its DataView throws)
// and the Elixir row records (its escaping exception class is the oracle).
//
// THE SWEEP. A valid fixed-form Chain file — the P3.schema optional side:
// string(16) name and ?Link { int32 value | min = 0, max = 1000; string(8)
// tag } — is written through the production chainFixedSave, then EVERY byte is
// flipped one bit at a time. Each mutant is read through the production
// chainFixedLoad (a fresh plan and report per mutant, and a row handed in
// holding NON-default values) and must answer exactly one of the two ways,
// never a third:
//
//   * report.refused != none, loaded == -1, no counter moved, and the row
//     handed in reset to its declared defaults — a refusal BY NAME, or
//   * report.refused == none and loaded == 1 — a read that LANDS VALUES.
//
// Any thrown exception, and any answer outside those two, is the third answer
// and a failure. The sweep counts its answers and asserts each occurred, so a
// datapath that quietly stopped reaching the reader cannot pass; and it pins
// the NAME per region of the header: every flip of a reserved byte refuses as
// framing damage (the Dart leg's wire_damaged, the spec's reserved_not_zero),
// every flip of the header hash refuses layout_newer (the spec's
// layout_unknown: a hash no lineage entry holds), and every flip of the layout
// length or a layout byte refuses layout_damaged (the spec's
// layout_malformed).
//
// THE PRODUCTION PATH. build/tables-generated-dart/p3/, laid down by
// `make build/conformance-dart`: P3Fixed.dart's chainFixedMeasure,
// chainFixedSave, chainFixedLoad and chainFixedNewPlan, over Tblp3Fixed.dart's
// Chain/Link value classes, the known-layout table and the one read loop. The
// vector here is CONSTRUCTED from the law (the tree carries no fixed-form P3
// bytes under testdata/conformance/tables), and the file is the whole file the
// law says to sweep: the form byte, the reserved bytes, the header hash, the
// layout length and layout bytes, and the record.
//
// THE CONTROLS. (1) Breaking BOTH layout-length bounds in the generated
// chainFixedLoad — the `20 + L > bytes` refusal and the `L !=
// known.layoutBytes` refusal — lets a hostile layout length walk the layout
// compare past the arrays: mutants throw RangeError, and the `escaped == 0`
// assertion goes RED (either bound alone still holds the sweep: the other
// catches every single-bit length flip). (2) Dropping the refusal's reset of
// the rows handed in (`initChain(values[j])` in refuse()) turns every
// refusal into a third answer, and the totality assertions go RED. Restored,
// the row is GREEN again.
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
/// and returns one word for which of the two answers it got, or a third
/// description so a failure can name it. The row handed in holds NON-default
/// values, so a refusal that does not reset it to its declared defaults is
/// caught.
String answer(Uint8List mutant) {
  final held = fixed.Chain();
  putText(held.name, 'held');
  held.nameLength = 4;
  held.linkPresent = true;
  held.link.value = 7;
  final values = <fixed.Chain>[held];
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
    final v = values[0];
    var nameZero = true;
    for (final b in v.name) {
      nameZero = nameZero && b == 0;
    }
    final total = loaded == -1 &&
        report.unknown == 0 &&
        report.widened == 0 &&
        v.nameLength == 0 &&
        nameZero &&
        !v.linkPresent &&
        v.link.value == fixed.Link().value;
    if (total) {
      return 'refusal:${fixed.TableFixedRefusal.name(report.refused)}';
    }
    return 'third(refused=${fixed.TableFixedRefusal.name(report.refused)} '
        'loaded=$loaded unknown=${report.unknown} widened=${report.widened} '
        'nameLength=${v.nameLength} linkPresent=${v.linkPresent} '
        'link.value=${v.link.value}: refusal not total)';
  }
  if (loaded == 1) {
    return 'landed';
  }
  return 'third(loaded=$loaded refused=none)';
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
      report.refused == fixed.TableFixedRefusal.none &&
          report.unknown == 0 &&
          report.widened == 0,
      'lawful file: a clean read moves no refusal and no counter',
    );
    check(back[0].linkPresent, 'lawful file: the optional Link is present');
    check(back[0].link.value == 42, 'lawful file: link.value lands 42');
    check(back[0].nameLength == 6, 'lawful file: nameLength lands 6');
    check(source[0] == fixed.tableFixedForm, 'lawful file: form byte 3');
  }

  // THE HEADER'S REGIONS, from the file's own layout length.
  final layoutAt = fixed.TableFixedLimits.layoutAt;
  final hashAt = fixed.TableFixedLimits.hashAt;
  final layoutLen = ByteData.sublistView(source)
      .getUint32(fixed.TableFixedLimits.headerBytes, Endian.little);
  final layoutEnd = layoutAt + layoutLen;

  // ---- THE SWEEP: every byte, one bit at a time, over the whole file
  var refusals = 0;
  var landed = 0;
  var escaped = 0;
  var third = 0;
  var firstEscape = '';
  var firstThird = '';
  // per region: mutants, and mutants refused by the region's own name
  var reservedN = 0, reservedNamed = 0;
  var hashN = 0, hashNamed = 0;
  var layoutN = 0, layoutNamed = 0;
  var firstMisnamed = '';
  final total = source.length * 8;

  for (var i = 0; i < source.length; i++) {
    for (var b = 0; b < 8; b++) {
      final mutant = Uint8List.fromList(source);
      mutant[i] ^= 1 << b;
      String? want;
      if (i >= 1 && i < hashAt) {
        want = 'refusal:wire_damaged';
        reservedN++;
      } else if (i >= hashAt && i < fixed.TableFixedLimits.headerBytes) {
        want = 'refusal:layout_newer';
        hashN++;
      } else if (i >= fixed.TableFixedLimits.headerBytes && i < layoutEnd) {
        want = 'refusal:layout_damaged';
        layoutN++;
      }
      try {
        final a = answer(mutant);
        if (want != null) {
          if (a == want) {
            if (i < hashAt) {
              reservedNamed++;
            } else if (i < fixed.TableFixedLimits.headerBytes) {
              hashNamed++;
            } else {
              layoutNamed++;
            }
          } else if (firstMisnamed.isEmpty) {
            firstMisnamed = 'byte $i bit $b: $a, want $want';
          }
        }
        if (a == 'landed') {
          landed++;
        } else if (a.startsWith('refusal:')) {
          refusals++;
        } else {
          third++;
          if (firstThird.isEmpty) {
            firstThird = 'byte $i bit $b: $a';
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
    third == 0,
    'never a third answer: every mutant was a total refusal by name or a '
    'read that lands values — $third otherwise'
    '${firstThird.isEmpty ? '' : ' (first: $firstThird)'}',
  );
  check(
    refusals + landed + escaped + third == total,
    'the sweep answers every mutant exactly once: '
    '${refusals + landed + escaped + third} of $total',
  );
  check(
    reservedN == 56 && reservedNamed == reservedN,
    'every reserved-byte flip refuses wire_damaged (the spec\'s '
    'reserved_not_zero): $reservedNamed of $reservedN'
    '${firstMisnamed.isEmpty ? '' : ' (first misnamed: $firstMisnamed)'}',
  );
  check(
    hashN == 64 && hashNamed == hashN,
    'every header-hash flip refuses layout_newer (the spec\'s '
    'layout_unknown): $hashNamed of $hashN',
  );
  check(
    layoutN == (4 + layoutLen) * 8 && layoutNamed == layoutN,
    'every layout-length or layout-byte flip refuses layout_damaged (the '
    'spec\'s layout_malformed): $layoutNamed of $layoutN',
  );
  check(
    refusals > 0,
    'a refusal by name occurs under the sweep: $refusals mutants',
  );
  check(
    landed > 0,
    'a read that lands values occurs under the sweep: $landed mutants '
    '(the record body)',
  );

  stdout.writeln(
    'P3: ${source.length} bytes, $total single-bit mutants — '
    '$refusals refused, $landed landed, $escaped escaped, $third third — '
    '${red == 0 ? 'GREEN' : 'RED'}',
  );
  exit(red == 0 ? 0 : 1);
}
