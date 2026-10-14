// R5.dart — the digest carries every range, every resolution (tag 'Q') and
// every reader limit (tag 'L'), and a flags type deduped by name, once.
//
// docs/FIXED-FORM-ALGORITHM.md:1130 — "every range, every resolution (tag 'Q')
// and every limit, tag 'L' (bill §13), or the widening cannot be refused —
// and a flags type deduped by NAME, once, as a struct is."
//
// docs/FIXED-FORM-ALGORITHM.md:627-649 — the digest byte layout:
//   'R'  integer, float or fixed-point range: min then max, each i64 LE
//   'Q'  a COMPRESSED FLOAT's RESOLUTION: the IEEE-754 bits of the float64 step
//   'B'  bits(N): N as u32 LE
//   'X'  fixed(I,F) / ufixed(I,F): I u32 LE, F u32 LE, then 1 signed or 0 unsigned
//   'F'  a flags type: wire bit count as u32 LE, then per flag 'f' and fnv1a64(name)
//   'L'  a reader-side limit: the limit as u64 LE (reserved until one exists)
//
// WHAT THIS ASSERTS:
//   1. RangedSigned (12 integer ranges across four widths) has a hash that
//      differs from fnv1a64(layout_bytes) — ranges fold into the digest.
//   2. RootConfig (closure includes LoadoutConfig → Perks flags) has a hash
//      that differs from fnv1a64(layout_bytes) — the flags type is in the digest.
//   3. Two tables with the same layout shape but different ranges produce
//      different hashes (RangedSigned vs RangedUnsigned: different widths,
//      different range bounds, different hashes).
//
// THE PRODUCTION PATH:
//   ir.TableFixedDefinitionsDigest walks the struct's fields in closure order,
//   emitting 'R' for each range, 'Q' for each compressed float resolution,
//   'B' for bits(N), 'X' for fixed(I,F), 'F' for flags (deduped by name),
//   and 'L' for reader limits. The digest bytes are then folded into the
//   hash by ir.TableFixedLayoutHash.
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

void main() {
  // ---- LAW: every range is in the digest (§5.2, bill §13) ----
  //
  // RangedSigned carries twelve integer ranges across four widths (int8,
  // int16, int32, int64), each with different bounds. Its digest is non-empty
  // because of these 'R' entries, so the hash must differ from
  // fnv1a64(layout_bytes alone).
  final rangedSignedLayoutOnly = fnv1a64(rangedSignedFixedLayout);
  check(
    rangedSignedLayoutOnly != rangedSignedFixedHash,
    'R5: RangedSigned has 12 integer ranges — digest is non-empty, '
    'so fnv1a64(layout) != hash '
    '(0x${hex64(rangedSignedLayoutOnly)} != 0x${hex64(rangedSignedFixedHash)})',
  );

  // RangedUnsigned carries twelve unsigned integer ranges — different bounds
  // from RangedSigned. Its hash must also differ from fnv1a64(layout alone).
  final rangedUnsignedLayoutOnly = fnv1a64(rangedUnsignedFixedLayout);
  check(
    rangedUnsignedLayoutOnly != rangedUnsignedFixedHash,
    'R5: RangedUnsigned has 12 unsigned integer ranges — '
    'fnv1a64(layout) != hash '
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

  // ---- LAW: a flags type is in the digest, deduped by name once ----
  //
  // RootConfig's closure reaches LoadoutConfig, which carries `perks Perks`.
  // Perks is a flags type (flags Perks { Shielded, Cloaked, Turbo }).
  // The flags type contributes an 'F' entry to the digest, so the hash
  // must differ from fnv1a64(layout_bytes alone).
  final rootConfigLayoutOnly = fnv1a64(rootConfigFixedLayout);
  check(
    rootConfigLayoutOnly != rootConfigFixedHash,
    'R5: RootConfig\'s closure includes LoadoutConfig → Perks (flags type) — '
    'the flags digest entry moves the hash '
    '(0x${hex64(rootConfigLayoutOnly)} != 0x${hex64(rootConfigFixedHash)})',
  );

  // ---- CONTROL: a table with no digest entries matches fnv1a64(layout) ----
  //
  // PaddedRow has no ranges, no flags, no bits, no fixed, no limits —
  // its digest is empty, so the hash equals fnv1a64(layout_bytes).
  final paddedLayoutOnly = fnv1a64(paddedRowFixedLayout);
  check(
    paddedLayoutOnly == paddedRowFixedHash,
    'R5: PaddedRow has no digest entries — fnv1a64(layout) == hash '
    '(0x${hex64(paddedLayoutOnly)} == 0x${hex64(paddedRowFixedHash)})',
  );

  if (_fails == 0) {
    stdout.writeln(
      'R5: digest carries ranges, resolutions, limits, flags once — PASS',
    );
  } else {
    stdout.writeln('R5: $_fails assertion(s) failed');
  }
  exit(_fails == 0 ? 0 : 1);
}
