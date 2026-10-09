// R18 — a clamp that cannot fire is not emitted, and nothing moves.
//
// The law (docs/FIXED-FORM-ALGORITHM.md:950, §5.4): an ordinal whose extent
// FILLS its storage width — 255 variants in a byte, 65535 in two — has no
// read-side clamp, because `> extent` against a value of that very width is
// a comparison no value satisfies. The elided check never clamped, so the
// counter it would have moved never moved either. The same rule already
// applies to a ranged scalar whose declared end sits on its width's limit,
// and to a bits(N) width clamp where N is the storage width. A port that
// emits the tautology instead is still conforming; this leg drops it, and
// this test pins the emission as well as the silence.
//
// THE CORPUS. tables/examples/Ranges.schema is the law's own scalar data:
// every integer width four ways — both bounds at the limits (`*_span`),
// each limit alone (`*_low`, `*_high`), and one value off each end
// (`*_inside`) — plus the bits widths, of which b32/b64 fill their storage
// exactly. The ordinal case's own schema, test/tables/VOLD_enum_width.schema
// (255 variants, a one-byte ordinal whose extent IS the storage maximum),
// has no Dart backend emission — make/dart.mk generates only
// examples/pointers/block/v1/v2/p1/p3 — so no 255-ordinal decode exists to
// inspect on this leg. The test therefore proves the ordinal half the way
// the corpus allows: every byte- or half-word-loaded `> N` clamp the
// emitter DID write can fire (N strictly below the load width's maximum),
// the widest corpus enum lands its top variant with no counter moving, and
// a forged ordinal past the top lands None and counts exactly one.
//
// Exit 0 green / exit 1 red, one printed line per assertion.
// Run as: dart run test/conformance/dart/rows/R18.dart

import 'dart:io';
import 'dart:typed_data';

import '../../../../build/tables-generated-dart/examples/TabledemoFixed.dart'
    as tbd;
import '../../../../build/tables-generated-dart/examples/RangesFixed.dart'
    as rng;
import '../../../../build/tables-generated-dart/examples/PackFixed.dart'
    as pack;
import '../../../../build/tables-generated-dart/examples/Pack.dart'
    show Difficulty;

int pass = 0;
int fail = 0;

void expect(String label, bool condition, String reason) {
  if (condition) {
    print('PASS $label');
    pass++;
  } else {
    print('FAIL $label: $reason');
    fail++;
  }
}

// The generated source under test, resolved against this file's own location
// so the row runs from any working directory.
String generatedSource(String leaf) {
  final uri = Platform.script.resolve(
    '../../../../build/tables-generated-dart/examples/$leaf',
  );
  return File.fromUri(uri).readAsStringSync();
}

// The body of `void name(`: from its opening line to the first closing brace
// at column zero. Decode bodies nest only indented blocks, so that brace is
// the function's own end.
String functionBody(String text, String name) {
  final start = text.indexOf('void $name(');
  if (start < 0) {
    return '';
  }
  final end = text.indexOf('\n}\n', start);
  if (end < 0) {
    return '';
  }
  return text.substring(start, end);
}

void main() {
  final fixed = generatedSource('TabledemoFixed.dart');

  // ---- A. NOT EMITTED: a bound on its storage limit writes no comparison --
  final signedBody = functionBody(fixed, 'rangedSignedFixedDecode');
  final unsignedBody = functionBody(fixed, 'rangedUnsignedFixedDecode');
  final widthsBody = functionBody(fixed, 'rangedWidthsFixedDecode');
  expect(
    'emission: decode bodies found',
    signedBody.isNotEmpty &&
        unsignedBody.isNotEmpty &&
        widthsBody.isNotEmpty,
    'missing rangedSignedFixedDecode=${signedBody.isEmpty} '
    'rangedUnsignedFixedDecode=${unsignedBody.isEmpty} '
    'rangedWidthsFixedDecode=${widthsBody.isEmpty}',
  );

  // A span field's load line is `value.xSpan = view.get...(at...);` — no
  // angle bracket. Any `<` or `>` on a line naming a span field is a clamp
  // the law forbids.
  bool fieldHasComparison(String body, String field) {
    for (final line in body.split('\n')) {
      if (line.contains(field) && (line.contains('<') || line.contains('>'))) {
        return true;
      }
    }
    return false;
  }

  for (final field in <String>[
    'i8Span',
    'i16Span',
    'i32Span',
    'i64Span',
  ]) {
    expect(
      'emission: $field carries no comparison',
      signedBody.isNotEmpty && !fieldHasComparison(signedBody, field),
      '$field is fully spanned [-2^(w-1), 2^(w-1)-1]; '
      'both ends sit on the storage limit',
    );
  }
  for (final field in <String>[
    'u8Span',
    'u16Span',
    'u32Span',
    'u64Span',
  ]) {
    expect(
      'emission: $field carries no comparison',
      unsignedBody.isNotEmpty && !fieldHasComparison(unsignedBody, field),
      '$field is fully spanned [0, 2^w-1]; '
      'both ends sit on the storage limit',
    );
  }
  for (final field in <String>['b32', 'b64']) {
    expect(
      'emission: $field carries no width clamp',
      widthsBody.isNotEmpty && !fieldHasComparison(widthsBody, field),
      '$field fills the storage its wire kind gives it, '
      'so 2^N - 1 cannot fire',
    );
  }

  // The scan is not vacuous: the neighbours one step inside the limits DO
  // carry exactly their one live end, and the narrow bits widths clamp.
  for (final probe in <String>[
    'value.i8Low > 126',
    'value.i8High < -127',
    'value.i16Low > 32766',
    'value.i16High < -32767',
    'value.u8Low > 254',
    'value.u8High < 1',
    'value.u16Low > 65534',
    'value.u32High < 1',
    'value.b8 > 255',
    'value.b12 > 4095',
  ]) {
    expect(
      'emission: live end still written ($probe)',
      fixed.contains(probe),
      'the one-sided neighbour of a span field must keep its live clamp',
    );
  }

  // Every byte- or half-word-loaded `> N` clamp can fire: N is strictly
  // below what the load width holds (255 for a byte, 65535 for two). This
  // covers the ordinal clamps (`value.grade > 3` over getUint8) and any
  // byte-loaded ranged clamp alike; a 255-variant enum would need
  // `> 255` here, which no value satisfies.
  final ordinalClamps = <RegExp>[
    RegExp(
      r'value\.(\w+) = view\.getUint(8|16)\([^;]*\);\s*\n\s*if \(value\.\1 > (\d+)\)',
    ),
    RegExp(
      r'value\.(\w+)\[i\] = view\.getUint(8|16)\([^;]*\);\s*\n\s*if \(value\.\1\[i\] > (\d+)\)',
    ),
  ];
  var clampCount = 0;
  var tautology = '';
  for (final re in ordinalClamps) {
    for (final m in re.allMatches(fixed)) {
      clampCount++;
      final bits = int.parse(m.group(2)!);
      final bound = int.parse(m.group(3)!);
      final max = (1 << bits) - 1;
      if (bound >= max) {
        tautology = '${m.group(1)} > $bound over $bits bits';
      }
    }
  }
  expect(
    'emission: a corpus ordinal clamp is seen',
    clampCount > 0,
    'no getUint8/16 load followed by `> N` found at all',
  );
  expect(
    'emission: no emitted byte/half-word clamp is a tautology',
    clampCount > 0 && tautology.isEmpty,
    tautology.isEmpty ? 'no `> N` clamp seen' : 'cannot fire: $tautology',
  );
  expect(
    'emission: the corpus enum keeps its live clamp (grade > 3)',
    fixed.contains('value.grade > 3'),
    'Grade has 3 variants in one byte; `> 3` can fire and must stay',
  );

  // ---- B. NOTHING MOVES: storage-limit extremes land exact, clamped == 0 --
  {
    final v = tbd.RangedUnsigned()
      ..u8Span = 255
      ..u16Span = 65535
      ..u32Span = 4294967295
      ..u64Span = 0xffffffffffffffff
      ..countsCount = 1;
    v.counts[0] = 0xffffffffffffffff;
    final file = Uint8List(rng.rangedUnsignedFixedMeasure(1));
    expect(
      'unsigned extremes: save',
      rng.rangedUnsignedFixedSave(<tbd.RangedUnsigned>[v], 1, file) ==
          file.length,
      'save refused the storage maxima',
    );
    final back = <tbd.RangedUnsigned>[tbd.RangedUnsigned()];
    final r = tbd.TableFixedReport();
    final n = rng.rangedUnsignedFixedLoad(
      back,
      1,
      file,
      file.length,
      rng.rangedUnsignedFixedNewPlan(),
      r,
    );
    expect(
      'unsigned extremes: storage maxima land exact, nothing moves',
      n == 1 &&
          back[0].u8Span == 255 &&
          back[0].u16Span == 65535 &&
          back[0].u32Span == 4294967295 &&
          back[0].u64Span == 0xffffffffffffffff &&
          back[0].counts[0] == 0xffffffffffffffff &&
          r.clamped == 0 &&
          !r.malformed &&
          r.refused == tbd.TableFixedRefusal.none,
      'n=$n u8Span=${back[0].u8Span} u16Span=${back[0].u16Span} '
      'u32Span=${back[0].u32Span} u64Span=${back[0].u64Span} '
      'clamped=${r.clamped} refused=${r.refused}',
    );
  }
  {
    final lo = tbd.RangedSigned()
      ..i8Span = -128
      ..i16Span = -32768
      ..i32Span = -2147483648
      ..i64Span = -9223372036854775808
      ..edgesCount = 4;
    lo.edges[0] = -32768;
    lo.edges[1] = 0;
    lo.edges[2] = 32767;
    lo.edges[3] = -1;
    final hi = tbd.RangedSigned()
      ..i8Span = 127
      ..i16Span = 32767
      ..i32Span = 2147483647
      ..i64Span = 9223372036854775807
      ..edgesCount = 4;
    hi.edges[0] = 32767;
    hi.edges[1] = -32768;
    hi.edges[2] = 0;
    hi.edges[3] = 1;
    final file = Uint8List(rng.rangedSignedFixedMeasure(2));
    expect(
      'signed extremes: save',
      rng.rangedSignedFixedSave(<tbd.RangedSigned>[lo, hi], 2, file) ==
          file.length,
      'save refused the storage limits',
    );
    final back = <tbd.RangedSigned>[tbd.RangedSigned(), tbd.RangedSigned()];
    final r = tbd.TableFixedReport();
    final n = rng.rangedSignedFixedLoad(
      back,
      2,
      file,
      file.length,
      rng.rangedSignedFixedNewPlan(),
      r,
    );
    expect(
      'signed extremes: storage limits land exact, nothing moves',
      n == 2 &&
          back[0].i8Span == -128 &&
          back[0].i16Span == -32768 &&
          back[0].i32Span == -2147483648 &&
          back[0].i64Span == -9223372036854775808 &&
          back[1].i8Span == 127 &&
          back[1].i16Span == 32767 &&
          back[1].i32Span == 2147483647 &&
          back[1].i64Span == 9223372036854775807 &&
          back[0].edges[0] == -32768 &&
          back[0].edges[2] == 32767 &&
          r.clamped == 0 &&
          !r.malformed &&
          r.refused == tbd.TableFixedRefusal.none,
      'n=$n i8Span=${back[0].i8Span}/${back[1].i8Span} '
      'clamped=${r.clamped} refused=${r.refused}',
    );
  }
  {
    final v = tbd.RangedWidths()
      ..b8 = 255
      ..b16 = 65535
      ..b32 = 0xFFFFFFFF
      ..b64 = 0xffffffffffffffff
      ..b12 = 4095
      ..b48 = 281474976710655;
    final file = Uint8List(rng.rangedWidthsFixedMeasure(1));
    expect(
      'widths extremes: save',
      rng.rangedWidthsFixedSave(<tbd.RangedWidths>[v], 1, file) ==
          file.length,
      'save refused the width maxima',
    );
    final back = <tbd.RangedWidths>[tbd.RangedWidths()];
    final r = tbd.TableFixedReport();
    final n = rng.rangedWidthsFixedLoad(
      back,
      1,
      file,
      file.length,
      rng.rangedWidthsFixedNewPlan(),
      r,
    );
    expect(
      'widths extremes: full-width ends land exact, nothing moves',
      n == 1 &&
          back[0].b8 == 255 &&
          back[0].b16 == 65535 &&
          back[0].b32 == 0xFFFFFFFF &&
          back[0].b64 == 0xffffffffffffffff &&
          back[0].b12 == 4095 &&
          back[0].b48 == 281474976710655 &&
          r.clamped == 0 &&
          !r.malformed &&
          r.refused == tbd.TableFixedRefusal.none,
      'n=$n b32=${back[0].b32} b64=${back[0].b64} '
      'clamped=${r.clamped} refused=${r.refused}',
    );
  }
  {
    // The widest corpus enum's top variant: Difficulty.hard = 3 of 3 in one
    // byte. The `> 3` clamp exists but cannot fire on a legal value.
    final v = tbd.GlobalSettings()..difficulty = Difficulty.hard;
    final file = Uint8List(pack.globalSettingsFixedMeasure(1));
    expect(
      'enum top: save',
      pack.globalSettingsFixedSave(<tbd.GlobalSettings>[v], 1, file) ==
          file.length,
      'save refused the top variant',
    );
    final back = <tbd.GlobalSettings>[tbd.GlobalSettings()];
    final r = tbd.TableFixedReport();
    final n = pack.globalSettingsFixedLoad(
      back,
      1,
      file,
      file.length,
      pack.globalSettingsFixedNewPlan(),
      r,
    );
    expect(
      'enum top: the top variant lands, nothing moves',
      n == 1 &&
          back[0].difficulty == Difficulty.hard &&
          r.clamped == 0 &&
          !r.malformed &&
          r.refused == tbd.TableFixedRefusal.none,
      'n=$n difficulty=${back[0].difficulty} '
      'clamped=${r.clamped} refused=${r.refused}',
    );
  }

  // ---- C. THE PASS RUNS AT ALL: one step past a live end clamps and ----
  // counts exactly one. Each forgery pokes one vector byte behind an
  // untouched hash and layout, so the read stays on the identity plan.
  {
    final v = tbd.RangedUnsigned();
    final file = Uint8List(rng.rangedUnsignedFixedMeasure(1));
    rng.rangedUnsignedFixedSave(<tbd.RangedUnsigned>[v], 1, file);
    final bodyAt = rng.rangedUnsignedFixedHeaderBytes + 8;
    file[bodyAt + 1] = 255; // u8Low, one past its max of 254
    final back = <tbd.RangedUnsigned>[tbd.RangedUnsigned()];
    final r = tbd.TableFixedReport();
    final n = rng.rangedUnsignedFixedLoad(
      back,
      1,
      file,
      file.length,
      rng.rangedUnsignedFixedNewPlan(),
      r,
    );
    expect(
      'control: u8Low=255 clamps to 254 and counts one',
      n == 1 && back[0].u8Low == 254 && r.clamped == 1,
      'n=$n u8Low=${back[0].u8Low} clamped=${r.clamped}',
    );
  }
  {
    final v = tbd.RangedSigned();
    final file = Uint8List(rng.rangedSignedFixedMeasure(1));
    rng.rangedSignedFixedSave(<tbd.RangedSigned>[v], 1, file);
    final bodyAt = rng.rangedSignedFixedHeaderBytes + 8;
    ByteData.sublistView(file).setInt8(bodyAt + 2, -128); // i8High < -127
    final back = <tbd.RangedSigned>[tbd.RangedSigned()];
    final r = tbd.TableFixedReport();
    final n = rng.rangedSignedFixedLoad(
      back,
      1,
      file,
      file.length,
      rng.rangedSignedFixedNewPlan(),
      r,
    );
    expect(
      'control: i8High=-128 clamps to -127 and counts one',
      n == 1 && back[0].i8High == -127 && r.clamped == 1,
      'n=$n i8High=${back[0].i8High} clamped=${r.clamped}',
    );
  }
  {
    final v = tbd.RangedWidths();
    final file = Uint8List(rng.rangedWidthsFixedMeasure(1));
    rng.rangedWidthsFixedSave(<tbd.RangedWidths>[v], 1, file);
    final bodyAt = rng.rangedWidthsFixedHeaderBytes + 8;
    ByteData.sublistView(
      file,
    ).setUint32(bodyAt + 20, 4096, Endian.little); // b12, one past 4095
    final back = <tbd.RangedWidths>[tbd.RangedWidths()];
    final r = tbd.TableFixedReport();
    final n = rng.rangedWidthsFixedLoad(
      back,
      1,
      file,
      file.length,
      rng.rangedWidthsFixedNewPlan(),
      r,
    );
    expect(
      'control: b12=4096 clamps to 4095 and counts one',
      n == 1 && back[0].b12 == 4095 && r.clamped == 1,
      'n=$n b12=${back[0].b12} clamped=${r.clamped}',
    );
  }
  {
    // A forged ordinal past the top variant lands None and counts one
    // (§5.4: the bounds pass counts a forged ordinal remapped to None).
    final v = tbd.GlobalSettings();
    final file = Uint8List(pack.globalSettingsFixedMeasure(1));
    pack.globalSettingsFixedSave(<tbd.GlobalSettings>[v], 1, file);
    final bodyAt = pack.globalSettingsFixedHeaderBytes + 8;
    file[bodyAt + 4] = 9; // difficulty, past the top variant 3
    final back = <tbd.GlobalSettings>[tbd.GlobalSettings()];
    final r = tbd.TableFixedReport();
    final n = pack.globalSettingsFixedLoad(
      back,
      1,
      file,
      file.length,
      pack.globalSettingsFixedNewPlan(),
      r,
    );
    expect(
      'control: forged ordinal 9 lands None and counts one',
      n == 1 && back[0].difficulty == Difficulty.none && r.clamped == 1,
      'n=$n difficulty=${back[0].difficulty} clamped=${r.clamped}',
    );
  }

  if (fail > 0) {
    print('$pass passed, $fail failed');
    exit(1);
  } else {
    print('$pass passed, $fail failed');
    exit(0);
  }
}
