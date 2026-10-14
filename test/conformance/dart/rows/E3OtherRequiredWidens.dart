// E3OtherRequiredWidens.dart — All other required widen-ladder cases
//
// LAW: docs/FIXED-FORM-ALGORITHM.md:742 — "A WIDEN'S SIGN IS THE LADDER'S, and
// the SAME-KIND widen has none." Across the ladder (te.kind != me.kind and
// TableFixedWidens holds) the source sign-extends when the WRITER's kind is
// signed (i8..i64 or a signed fixed(I,F)) and zero-extends otherwise. Within
// one kind (te.kind == me.kind, te.size < me.size, me.size <= 8) the source
// zero-extends. A grown enum ordinal also zero-extends.
//
// CELL: dart/E3/other-required-widens
//
// PRODUCTION ENTRYPOINT: tableFixedRun in the generated Dart fixed-form
// runtime (build/tables-generated-dart/pointers/GraphdemoFixed.dart), called
// by every <table>FixedLoad when a record's hash selects a compiled plan from
// the lineage. The widen/widenFloat ops are the only place a narrower source
// becomes a wider destination.
//
// RUN: dart run test/conformance/dart/rows/E3OtherRequiredWidens.dart

import 'dart:io';
import 'dart:typed_data';

import '../../../../build/tables-generated-dart/pointers/GraphdemoFixed.dart'
    show
        TableFixedOp,
        TableFixedReport,
        tableFixedNoGuard,
        tableFixedRun;

void main() {
  var failed = false;

  void check(bool ok, String msg) {
    if (!ok) {
      print('FAIL: $msg');
      failed = true;
    }
  }

  // A single-entry plan is nine int32 lanes. We use it to isolate each widen
  // case from the compiler, so the test is about the runtime's interpretation
  // of the plan and nothing else.
  Int32List plan(
    int op,
    int src,
    int dst,
    int size,
    int meta,
  ) {
    return Int32List.fromList(<int>[
      op, // op
      src, // src offset in the source record
      dst, // dst offset in the image
      size, // source size in bytes
      0, // aux
      tableFixedNoGuard, // guard
      0, // arg
      meta, // meta: wide width | (signed ? 0x100 : 0)
      1, // argW
    ]);
  }

  // Run one widen entry over a source buffer that starts at offset 0.
  // Returns the widened destination bytes starting at dst.
  Uint8List runWiden(
    Int32List entries,
    int entryCount,
    Uint8List source,
    int dst,
    int dstSize,
  ) {
    final image = Uint8List(dst + dstSize + 8);
    final report = TableFixedReport();
    tableFixedRun(
      entries,
      entryCount,
      source,
      ByteData.sublistView(source),
      0,
      image,
      ByteData.sublistView(image),
      Int32List(0),
      ByteData(16),
      report,
    );
    check(report.widened == 1, 'widened counter increments once');
    return image.sublist(dst, dst + dstSize);
  }

  // ---- LADDER SIGNED WIDEN: i16 -> i32 ----
  // The writer's kind is signed, so the widen sign-extends.
  // Source -1 as int16 is 0xff 0xff; widened to int32 it must be
  // 0xff 0xff 0xff 0xff.
  {
    final source = Uint8List.fromList(<int>[0xff, 0xff]); // int16 -1
    final got = runWiden(
      plan(TableFixedOp.widen, 0, 0, 2, 4 | 0x100),
      1,
      source,
      0,
      4,
    );
    check(
      got[0] == 0xff &&
          got[1] == 0xff &&
          got[2] == 0xff &&
          got[3] == 0xff,
      'ladder signed widen i16->i32 sign-extends -1 '
      '(got 0x${got.map((b) => b.toRadixString(16).padLeft(2, '0')).join()})',
    );
  }

  // ---- LADDER UNSIGNED WIDEN: u16 -> u32 ----
  // The writer's kind is unsigned, so the widen zero-extends.
  // Source 0x00 0x80 (uint16 32768 LE); widened to uint32 LE it must be
  // 0x00 0x80 0x00 0x00, not 0x00 0x80 0xff 0xff.
  {
    final source = Uint8List.fromList(<int>[0x00, 0x80]); // uint16 32768 LE
    final got = runWiden(
      plan(TableFixedOp.widen, 0, 0, 2, 4), // signed bit clear
      1,
      source,
      0,
      4,
    );
    check(
      got[0] == 0x00 &&
          got[1] == 0x80 &&
          got[2] == 0x00 &&
          got[3] == 0x00,
      'ladder unsigned widen u16->u32 zero-extends high bit '
      '(got 0x${got.map((b) => b.toRadixString(16).padLeft(2, '0')).join()})',
    );
  }

  // ---- SAME-KIND WIDEN: bits(8) -> bits(16) ----
  // te.kind == me.kind (both unsigned integers carrying bits), so the widen
  // has no sign and zero-extends. Source 0xff widened to two bytes must be
  // 0xff 0x00, not 0xff 0xff.
  {
    final source = Uint8List.fromList(<int>[0xff]); // bits(8) value
    final got = runWiden(
      plan(TableFixedOp.widen, 0, 0, 1, 2), // signed bit clear
      1,
      source,
      0,
      2,
    );
    check(
      got[0] == 0xff && got[1] == 0x00,
      'same-kind widen bits(8)->bits(16) zero-extends '
      '(got 0x${got.map((b) => b.toRadixString(16).padLeft(2, '0')).join()})',
    );
  }

  // ---- GROWN ENUM ORDINAL: u8 -> u16 ----
  // An enum's ordinal grows wider but stays the same kind (30 -> 30), so it
  // zero-extends. Source ordinal 0xff widened to two bytes must be 0xff 0x00.
  {
    final source = Uint8List.fromList(<int>[0xff]); // ordinal value
    final got = runWiden(
      plan(TableFixedOp.widen, 0, 0, 1, 2), // signed bit clear
      1,
      source,
      0,
      2,
    );
    check(
      got[0] == 0xff && got[1] == 0x00,
      'grown enum ordinal u8->u16 zero-extends '
      '(got 0x${got.map((b) => b.toRadixString(16).padLeft(2, '0')).join()})',
    );
  }

  // ---- FLOAT WIDEN: f32 -> f64 ----
  // widenFloat is bit-exact: every f32 value lands as the same f64.
  {
    const original = 3.1415927;
    final scratch = ByteData(4);
    scratch.setFloat32(0, original, Endian.little);
    final source = Uint8List.fromList(<int>[
      scratch.getUint8(0),
      scratch.getUint8(1),
      scratch.getUint8(2),
      scratch.getUint8(3),
    ]);
    final got = runWiden(
      plan(TableFixedOp.widenFloat, 0, 0, 4, 0),
      1,
      source,
      0,
      8,
    );
    final value = ByteData.sublistView(got).getFloat64(0, Endian.little);
    final expected = scratch.getFloat32(0, Endian.little);
    check(
      value == expected,
      'float widen f32->f64 lands bit-exact '
      '(got $value, expected $expected)',
    );
  }

  if (failed) {
    exit(1);
  }
  print('E3OtherRequiredWidens: all other required widen-ladder cases — PASS');
}
