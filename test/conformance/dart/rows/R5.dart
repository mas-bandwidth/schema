// R5.dart — the definitions digest carries every range, every float range
// with its resolution, every bits(N), fixed(I,F) and flags type the closure
// states, so two writers that differ only in those facts never share a hash.
//
// docs/SPEC-TABLES.md §3.4, THE DEFINITIONS DIGEST (current scheme): the
// 26-byte prefix, then ROWS `tag u8, path length u32, path, payload length
// u32, payload` over the closure walked depth first in declared order, EVERY
// occurrence of a named table, union or flags walked at its own path (no
// deduplication by name):
//   'C'  a counted array's minimum count, u64
//   'R'  a ranged integer or fixed-point field: leaf kind u32, signed u8,
//        fractional bits u32, then min and max as canonical integers
//   'D'  a ranged float: leaf kind u32, min and max as float64 bits; then
//   'Q'  its resolution as float64 bits (0 when none was declared)
//   'B'  bits(N): N as u32
//   'X'  fixed(I,F) / ufixed(I,F): I u32, F u32, signed u8
//   'F'  a flags type: wire bit count u32, flag count u32, each flag's name id
// No reader-limit row exists (docs/FIXED-FORM-ALGORITHM.md §5.2). (This row's
// header once quoted a retired scheme: i64 min/max, an 'L' limit row and
// flags deduplicated by name.)
//
// WHAT THIS ASSERTS (each against fnv1a64(layout ++ prefix), the hash a root
// with NO rows would carry, so a pass says rows were folded in):
//   1. RangedSigned (12 integer ranges across four widths) has a hash that
//      differs from fnv1a64(layout ++ v2 prefix) — ranges fold into the digest.
//   2. RootConfig (closure includes LoadoutConfig → Perks flags) has a hash
//      that differs from fnv1a64(layout ++ v2 prefix) — the flags type is in the digest.
//   3. Two tables with the same layout shape but different ranges produce
//      different hashes (RangedSigned vs RangedUnsigned).
//   4. PaddedRow, with no rows, hashes exactly fnv1a64(layout ++ prefix).
//
// THE PRODUCTION PATH:
//   ir.TableFixedDefinitionsDigest (ir/fixedform_digest_v2.go) emits the
//   prefix and rows; the emitter folds them into the hash constant after the
//   layout bytes. No runtime computes either.
//
// Run: dart run test/conformance/dart/rows/R5.dart

import 'dart:io';
import 'dart:typed_data';

import '../../../../build/tables-generated-dart/block/PaddedFixed.dart'
    show paddedRowFixedHash, paddedRowFixedLayout;
import '../../../../build/tables-generated-dart/examples/RangesFixed.dart'
    show
        rangedSignedFixedHash,
        rangedSignedFixedLayout,
        rangedUnsignedFixedHash,
        rangedUnsignedFixedLayout;
import '../../../../build/tables-generated-dart/examples/TablesFixed.dart'
    show rootConfigFixedHash, rootConfigFixedLayout;

int _fails = 0;

void check(bool ok, String what) {
  if (!ok) {
    stderr.writeln('FAIL: $what');
    _fails++;
  } else {
    stdout.writeln('PASS: $what');
  }
}

int fnv1a64(List<int> bytes) {
  var h = BigInt.parse('cbf29ce484222325', radix: 16);
  final prime = BigInt.parse('100000001b3', radix: 16);
  final mask = (BigInt.one << 64) - BigInt.one;
  for (final b in bytes) {
    h = (h ^ BigInt.from(b)) & mask;
    h = (h * prime) & mask;
  }
  return h.toSigned(64).toInt();
}

String hex64(int v) =>
    (v & 0xFFFFFFFFFFFFFFFF).toRadixString(16).padLeft(16, '0');


// THE PROSPECTIVE V2 IDENTITY PREFIX (docs/FIXED-FORM-ALGORITHM.md §5.2,
// "PROSPECTIVE V2 DEFINITIONS DIGEST"): every non-nil fixed root begins its
// digest with these 26 bytes, "\0schema-fixed-definitions\x02", even when it has
// no rows. So a root with no digest rows hashes fnv1a64(layout ++ prefix), and
// a root whose ranges, resolutions, bits, fixed splits or flags ARE in the
// digest hashes something else. (This row once compared against
// fnv1a64(layout) alone, the legacy layout-only key, which WireLaw 3 retired
// for a root's own identity; against it every v2 hash differs, so the
// "digest folded in" checks below compare with the prefix included.)
final List<int> v2Prefix = <int>[0x00, ...'schema-fixed-definitions'.codeUnits, 0x02];

List<int> withV2Prefix(List<int> layout) => <int>[...layout, ...v2Prefix];

void main() {
  // ---- LAW: every range is in the digest (§5.2, bill §13) ----
  //
  // RangedSigned carries twelve integer ranges across four widths (int8,
  // int16, int32, int64), each with different bounds. Its digest is non-empty
  // because of these 'R' entries, so the hash must differ from
  // fnv1a64(layout ++ v2 prefix).
  final rangedSignedLayoutOnly = fnv1a64(withV2Prefix(rangedSignedFixedLayout));
  check(
    rangedSignedLayoutOnly != rangedSignedFixedHash,
    'R5: RangedSigned has 12 integer ranges — digest is non-empty, '
    'so fnv1a64(layout ++ v2 prefix) != hash '
    '(0x${hex64(rangedSignedLayoutOnly)} != 0x${hex64(rangedSignedFixedHash)})',
  );

  // RangedUnsigned carries twelve unsigned integer ranges — different bounds
  // from RangedSigned. Its hash must also differ from fnv1a64(layout ++ v2 prefix).
  final rangedUnsignedLayoutOnly = fnv1a64(withV2Prefix(rangedUnsignedFixedLayout));
  check(
    rangedUnsignedLayoutOnly != rangedUnsignedFixedHash,
    'R5: RangedUnsigned has 12 unsigned integer ranges — '
    'fnv1a64(layout ++ v2 prefix) != hash '
    '(0x${hex64(rangedUnsignedLayoutOnly)} != 0x${hex64(rangedUnsignedFixedHash)})',
  );

  // ---- LAW: ranges distinguish tables with the same layout shape ----
  //
  // RangedSigned and RangedUnsigned have the same layout structure (same
  // number of entries, same scalar kinds, same sizes) but different range
  // bounds. Their hashes must differ because the digest 'R' entries differ.
  check(
    rangedSignedFixedHash != rangedUnsignedFixedHash,
    'R5: two tables with the same layout shape but different ranges '
    'produce different hashes '
    '(signed 0x${hex64(rangedSignedFixedHash)} != '
    'unsigned 0x${hex64(rangedUnsignedFixedHash)})',
  );

  // ---- LAW: a flags type is in the digest (row F at its wire path) ----
  //
  // RootConfig's closure reaches LoadoutConfig, which carries `perks Perks`.
  // Perks is a flags type (flags Perks { Shielded, Cloaked, Turbo }).
  // The flags type contributes an 'F' entry to the digest, so the hash
  // must differ from fnv1a64(layout ++ v2 prefix).
  final rootConfigLayoutOnly = fnv1a64(withV2Prefix(rootConfigFixedLayout));
  check(
    rootConfigLayoutOnly != rootConfigFixedHash,
    'R5: RootConfig\'s closure includes LoadoutConfig → Perks (flags type) — '
    'the flags digest entry moves the hash '
    '(0x${hex64(rootConfigLayoutOnly)} != 0x${hex64(rootConfigFixedHash)})',
  );

  // ---- CONTROL: a table with no digest entries matches fnv1a64(layout ++ v2 prefix) ----
  //
  // PaddedRow has no ranges, no flags, no bits, no fixed, no limits —
  // its digest is the v2 prefix alone, so the hash equals fnv1a64(layout ++ v2 prefix).
  final paddedLayoutOnly = fnv1a64(withV2Prefix(paddedRowFixedLayout));
  check(
    paddedLayoutOnly == paddedRowFixedHash,
    'R5: PaddedRow has no digest entries — fnv1a64(layout ++ v2 prefix) == hash '
    '(0x${hex64(paddedLayoutOnly)} == 0x${hex64(paddedRowFixedHash)})',
  );

  if (_fails == 0) {
    stdout.writeln(
      'R5: digest carries ranges and flags — PASS',
    );
  } else {
    stdout.writeln('R5: $_fails assertion(s) failed');
  }
  exit(_fails == 0 ? 0 : 1);
}
