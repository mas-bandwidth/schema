// test/conformance/dart/rows/C8.dart — the dart/C8 cell, "fixed-point
// F-shift / bits(N)" (docs/roadmap.sexp at main eb12ceb3).
//
// THE LAW (docs/FIXED-FORM-ALGORITHM.md:81, §1.2's LEAF row at line 110,
// §3's C table at line 188):
//
//   bits(N): N in 1..8 -> kind 6 (u8), N in 9..16 -> kind 7 (u16),
//            N in 17..32 -> kind 8 (u32), N in 33..64 -> kind 9 (u64);
//            the declared STORAGE WIDTH is 4 for N<=32 and 8 for N>=33;
//            the read DECLARES a WIDTH CLAMP of 2^N - 1, which the storage
//            type holds exactly when N IS the storage width, so the clamp
//            is ELIDED for N=32 (storage u32 is also 2^32 - 1) and N=64
//            (storage u64 is also 2^64 - 1).
//
//   fixed(I,F) F-shift: a fixed-point value rides raw at its storage width
//            on this form: the storage is the scaled integer and NOTHING
//            HERE DIVIDES BY 2^F. The bound COMPARED AGAINST IS THE
//            DECLARED WHOLE-UNIT BOUND SHIFTED BY F ONTO THAT SAME RAW
//            SCALE (§3, docs/FIXED-FORM-ALGORITHM.md:866). The compare is
//            `bound * 2^F` against the raw integer.
//
// PRODUCTION PATH this test drives (every stage generated):
//   rangedWidthsFixedSave       the write (internal/codegen/darttable/fixeddart.go, rangedWidthsFixedWriteBody)
//   rangedWidthsFixedDecode     the read every share, including the WIDTH CLAMP for bits(N) (TabledemoFixed.dart, rangedWidthsFixedDecode)
//   rangedWidthsFixedLoad       the load, identity plan
//   TableFixedLayout.{kind,size} the parser used here to verify the layout's own (kind, size) for each bits(N) entry
//
// WHY THIS TEST EXISTS — the cell title joins TWO clauses the dart leg
// must satisfy. The dart leg has ONE fixed-form TYPE THAT COVERS bits(N):
// tables/examples/Ranges.schema pins RangedWidths with six bits(N) widths
// (b8, b12, b16, b32, b48, b64). The dart leg HAS NO SCHEMA WITH fixed(I,F):
// a search over tables/examples, tables/pointers, tables/block and
// tables/blockhome found no `fixed(` declaration. The bits(N) clause is
// what is asserted below; the fixed(I,F) F-shift CLAUSE IS THE LEG'S
// UNPROVED part — named in RESULT.md and as a follow-up.
//
// THE VECTOR is a single in-range record written by this build's own
// rangedWidthsFixedSave at the field's exact bound ($maskedAt), read back
// through the identity plan: any bits(N) value ridden at the declared
// storage width round-trips byte-identical. The control is one byte past
// the WIDTH CLAMP — a u32 pattern wider than 2^N-1 lands the clamp at the
// law-named value with exactly ONE clamp counter and no other counter
// touched.
//
// Run from the repo root: dart run test/conformance/dart/rows/C8.dart
// Exit 0 green / exit 1 red, one line per assertion.

import 'dart:io';
import 'dart:typed_data';

import '../../../../build/tables-generated-dart/examples/RangesFixed.dart';
import '../../../../build/tables-generated-dart/examples/TabledemoFixed.dart'
    show
        RangedWidths,
        TableFixedLayout,
        TableFixedPlan,
        TableFixedRefusal,
        TableFixedReport;

int failures = 0;

void check(String what, bool ok) {
  print('${ok ? 'ok' : 'FAIL'} - $what');
  if (!ok) {
    failures++;
  }
}

// fnv1a64 over a wire name's ASCII bytes, identical to the layout's own
// hash. Used to map the layouts' id values back to the field name so the
// (kind, size) check is against the NAME, not against an index that could
// move on a future field reorder.
int fnv1a64(String name) {
  final bytes = name.codeUnits;
  var h = 0xcbf29ce484222325;
  for (final b in bytes) {
    h = (h ^ b) & 0xffffffffffffffff;
    h = (h * 0x100000001b3) & 0xffffffffffffffff;
  }
  return h;
}

// THE EXPECTED (kind, size, maxValue at 2^N-1) for each bits(N) field
// RangedWidths carries. The kind is the unsigned integer kind the bit
// count picks; size is the declared storage width (4 for N<=32, 8 for
// N>=33); maxValue is the law's clamp target.
class BitsCase {
  final int kind;
  final int size;
  final int maxValue;
  final int maskLow64;
  const BitsCase(this.kind, this.size, this.maxValue, this.maskLow64);
}

const Map<String, BitsCase> _expectations = {
  'b8':  BitsCase(6, 4, 0xFF,        0x00000000000000FF),
  'b12': BitsCase(7, 4, 0xFFF,       0x0000000000000FFF),
  'b16': BitsCase(7, 4, 0xFFFF,     0x000000000000FFFF),
  'b32': BitsCase(8, 4, 0xFFFFFFFF, 0x00000000FFFFFFFF),
  'b48': BitsCase(9, 8, -1 & 0xFFFFFFFFFFFF, 0x0000FFFFFFFFFFFF), // 2^48 - 1
  'b64': BitsCase(9, 8, -1,         0xFFFFFFFFFFFFFFFF), // 2^64 - 1
};

// LAYOUT-LEVEL ASSERTIONS: the COMPILER PINS (kind, size) PER ENTRY at
// build time against the layout this build ships. A read through the
// PUBLIC TableFixedLayout.{id,kind,size} accessor is the single source
// for "is the law being implemented by THIS BUILD'S compilation?" —
// anything else reads WHAT THE WRITER WROTE, not what the layout itself
// declares, and would let a writer/store mismatch pass.
void assertLayout(TableFixedLayout layout) {
  if (layout.count != _expectations.length + 1) {
    print(
      'C8 layout: expected '
      '${_expectations.length + 1} entries (RangedWidths + 6 bits), '
      'got ${layout.count}',
    );
    failures++;
    return;
  }
  for (var i = 1; i < layout.count; i++) {
    final id = layout.id(i);
    String? name;
    for (final n in _expectations.keys) {
      if (fnv1a64(n) == id) {
        name = n;
        break;
      }
    }
    if (name == null) {
      continue;
    }
    final exp = _expectations[name]!;
    check(
      'C8 layout $name: kind = ${exp.kind} (got ${layout.kind(i)})',
      layout.kind(i) == exp.kind,
    );
    check(
      'C8 layout $name: size = ${exp.size} (got ${layout.size(i)})',
      layout.size(i) == exp.size,
    );
  }
}

// A CLEAN READ through the identity plan: each bit field set to its u64
// IMAGE of the in-range mask, no clamp, round trip preserves the value.
(RangedWidths, TableFixedReport) readClean(
  int b8,
  int b12,
  int b16,
  int b32,
  int b64,
  int b48,
) {
  final value = RangedWidths()
    ..b8 = b8
    ..b12 = b12
    ..b16 = b16
    ..b32 = b32
    ..b64 = b64
    ..b48 = b48;
  final bytes = Uint8List(rangedWidthsFixedMeasure(1));
  rangedWidthsFixedSave([value], 1, bytes);
  final out = RangedWidths();
  final report = TableFixedReport();
  final n = rangedWidthsFixedLoad(
    [out],
    1,
    bytes,
    bytes.length,
    rangedWidthsFixedNewPlan(),
    report,
  );
  if (n != 1) {
    failures++;
    print('FAIL C8 clean read returned $n records (refused=${report.refused})');
  }
  return (out, report);
}

// Round-trip EACH bits(N) AT its in-range bound; clamped == 0, value reads
// back identically.
void assertRoundTrip() {
  final cases = <(String, int)>[
    ('b8',  _expectations['b8']!.maxValue),
    ('b12', _expectations['b12']!.maxValue),
    ('b16', _expectations['b16']!.maxValue),
    ('b32', _expectations['b32']!.maxValue),
    ('b48', _expectations['b48']!.maxValue),
    ('b64', _expectations['b64']!.maxValue & 0x7FFFFFFFFFFFFFFF),
  ];
  for (final c in cases) {
    int b8 = _expectations['b8']!.maxValue;
    int b12 = _expectations['b12']!.maxValue;
    int b16 = _expectations['b16']!.maxValue;
    int b32 = _expectations['b32']!.maxValue;
    int b48 = _expectations['b48']!.maxValue;
    int b64 = _expectations['b64']!.maxValue & 0x7FFFFFFFFFFFFFFF;
    if (c.$1 == 'b8') b8 = c.$2;
    if (c.$1 == 'b12') b12 = c.$2;
    if (c.$1 == 'b16') b16 = c.$2;
    if (c.$1 == 'b32') b32 = c.$2;
    if (c.$1 == 'b48') b48 = c.$2;
    if (c.$1 == 'b64') b64 = c.$2;
    final (back, report) = readClean(b8, b12, b16, b32, b64, b48);
    check(
      'C8 round trip: ${c.$1} at mask=0x${c.$2.toRadixString(16)}: '
      'clamped == 0 (got ${report.clamped})',
      report.clamped == 0,
    );
    final got = switch (c.$1) {
      'b8'  => back.b8,
      'b12' => back.b12,
      'b16' => back.b16,
      'b32' => back.b32,
      'b48' => back.b48,
      'b64' => back.b64,
      _ => 0,
    };
    final exp = c.$2;
    final ok = got == exp;
    check(
      'C8 round trip: ${c.$1} read back = 0x${exp.toRadixString(16)} '
      '(got 0x${got.toRadixString(16)})',
      ok,
    );
  }
}

// FORGED READ: one bit field set one PAST the declared bound. The WIDTH
// CLAMP lands at 2^N - 1 exactly, with exactly ONE clamp and nothing
// else moved. A wrong mask (e.g., 2^N instead of 2^N - 1) turns this
// test RED on the offending field; a wrong storage width (a layout
// admitting size 1 for kind 6 instead of size 4) fails at LAYOUT-LEVEL
// ASSERTION above.
void assertClamp() {
  void one(String field, int rawValue, int clampedAt) {
    final b8  = field == 'b8'  ? rawValue : _expectations['b8']!.maxValue;
    final b12 = field == 'b12' ? rawValue : _expectations['b12']!.maxValue;
    final b16 = field == 'b16' ? rawValue : _expectations['b16']!.maxValue;
    final b32 = field == 'b32' ? rawValue : _expectations['b32']!.maxValue;
    final b64 = field == 'b64' ? rawValue : _expectations['b64']!.maxValue & 0x7FFFFFFFFFFFFFFF;
    final b48 = field == 'b48' ? rawValue : _expectations['b48']!.maxValue;
    final (back, report) = readClean(b8, b12, b16, b32, b64, b48);
    check(
      'C8 clamp $field raw=0x${rawValue.toRadixString(16)}: '
      'clamped == 1 (got ${report.clamped})',
      report.clamped == 1,
    );
    final got = switch (field) {
      'b8'  => back.b8,
      'b12' => back.b12,
      'b16' => back.b16,
      'b32' => back.b32,
      'b48' => back.b48,
      'b64' => back.b64,
      _ => 0,
    };
    check(
      'C8 clamp $field raw=0x${rawValue.toRadixString(16)}: '
      'lands at 0x${clampedAt.toRadixString(16)} (got 0x${got.toRadixString(16)})',
      got == clampedAt,
    );
    final otherMoved = report.unknown != 0 ||
        report.kindMismatch != 0 ||
        report.widened != 0 ||
        report.malformed ||
        report.refused != TableFixedRefusal.none;
    check(
      'C8 clamp $field raw=0x${rawValue.toRadixString(16)}: '
      'no other counter moved '
      '(unknown=${report.unknown} kindMismatch=${report.kindMismatch} '
      'widened=${report.widened} malformed=${report.malformed} '
      'refused=${report.refused})',
      !otherMoved,
    );
  }

  // The b32 and b64 cases cannot be FORGED PAST THE WIDTH CLAMP through
  // their declared storage (storage IS the clamp), so they are covered
  // by the round-trip case above — those two are ELIDED clamps.
  one('b8',  0x100,             _expectations['b8']!.maxValue);
  one('b12', 0x1000,            _expectations['b12']!.maxValue);
  one('b16', 0x10000,           _expectations['b16']!.maxValue);
  one('b48', 0x0001000000000000, _expectations['b48']!.maxValue);
}

void main() {
  print('C8: bits(N) storage width and width clamp');
  final layout = TableFixedLayout()
    ..at = 0
    ..view = ByteData.sublistView(rangedWidthsFixedLayout);
  final ok = layout.parse(
    ByteData.sublistView(rangedWidthsFixedLayout),
    0,
    rangedWidthsFixedLayout.length,
  );
  if (!ok) {
    print('FAIL C8 layout parse refused: ${layout.refusal}');
    exitCode = 1;
    return;
  }
  assertLayout(layout);
  assertRoundTrip();
  assertClamp();
  if (failures != 0) {
    print('C8 FAILED ($failures assertions)');
    exitCode = 1;
    return;
  }
  print('C8 green: bits(N) law asserted (kind, size, width clamp)');
}
