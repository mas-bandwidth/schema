// CARD: schema-cell-dart-signed-integers — signed-integers/dart/valid-data
// LAW: docs/SPEC-TABLES.md:6411 — "A FIXED-TABLE RECORD IS AN EIGHT-BYTE HASH
// OF THE WRITER'S LAYOUT, AND THEN THE VALUES IN DECLARED ORDER, EVERY FIELD
// AT ITS BOUND."; docs/FIXED-FORM-ALGORITHM.md:14 — "SLE(w, p) ... the signed
// integer in the w bytes at p, little-endian"; :171 — `int8`..`uint64` ride
// "the declared storage width"; :1682 — "Read a file and save it back; the
// bytes must be identical".
// CELL: signed-integers/dart/valid-data (docs/roadmap.sexp:4318)
// TEST: valid-data write/read acceptance for the signed widths int8, int16,
// int32 and int64 in a §3.4 fixed-table record. The vector is CONSTRUCTED
// FROM THE LAW (testdata/conformance/tables holds no signed-integers case):
// fixed table RangedSigned (tables/examples/Ranges.schema) carries all four
// widths, its layout puts int8 at body +0..3, int16 at +4/6/8/10, int32 at
// +12/16/20/24, int64 at +28/36/44/52 and a [..4]int16 count at +60 with
// elements from +64 — declared order, every field at its declared storage
// width, little-endian two's complement (SLE). Every value below is VALID
// data inside the field's declared range: the span fields declare the full
// storage range (so INT*_MIN/MAX ride), and the low/high/inside fields sit
// ON their declared bounds or inside them, which land exact and count
// nothing. The signedness discriminators are -1 at every width (must ride
// 0xFF* and come back -1, never 255/65535/...) and INT*_MIN.

import 'dart:typed_data';
import 'dart:io';

import '../../../../build/tables-generated-dart/examples/TabledemoFixed.dart'
    as home;
import '../../../../build/tables-generated-dart/examples/RangesFixed.dart'
    as rt;

/// int64's storage minimum. Dart's decimal literals must fit a signed 64-bit
/// int, so INT64_MIN is spelled as max minus one.
const int int64Min = -9223372036854775807 - 1;
const int int64Max = 9223372036854775807;

/// VD: SIGNED INTEGERS 8/16/32/64 — valid-data write/read acceptance (§3.4
/// record, int8..int64).
///
/// Write a RangedSigned record through the fixed-form Save with every width
/// at a storage extreme or a signedness-discriminating value; assert the wire
/// bytes carry each width's signed little-endian two's-complement pattern;
/// read it back through the fixed-form Load and assert every field lands
/// exact and the read counts nothing; save the loaded record again and assert
/// the bytes are identical to the first write.
void main() {
  var failed = false;

  void check(bool ok, String msg) {
    if (ok) {
      print('VD: PASS $msg');
    } else {
      print('VD: FAIL $msg');
      failed = true;
    }
  }

  // ---- THE VALUE: every signed width, all four lanes ----
  final v = home.RangedSigned();
  v.i8Span = -128; // INT8_MIN, valid: i8_span declares [-128, 127]
  v.i8Low = 126; // i8_low's declared max, valid
  v.i8High = -127; // i8_high's declared min, valid
  v.i8Inside = -1; // the signedness discriminator
  v.i16Span = -32768; // INT16_MIN
  v.i16Low = 32766;
  v.i16High = -32767;
  v.i16Inside = -1;
  v.i32Span = -2147483648; // INT32_MIN
  v.i32Low = 2147483646;
  v.i32High = -2147483647;
  v.i32Inside = -1;
  v.i64Span = int64Min; // INT64_MIN
  v.i64Low = 9223372036854775806;
  v.i64High = -9223372036854775807;
  v.i64Inside = -1;
  v.edgesCount = 4; // [..4]int16, elements from body + 64
  v.edges[0] = 32767;
  v.edges[1] = -32768;
  v.edges[2] = -1;
  v.edges[3] = 1;

  // ---- WRITE (§3.4: the eight-byte hash, then the values in declared
  // order, every field at its bound) ----
  final file = Uint8List(rt.rangedSignedFixedMeasure(1));
  final saved = rt.rangedSignedFixedSave(<home.RangedSigned>[v], 1, file);
  check(
    saved == rt.rangedSignedFixedMeasure(1),
    'save answers its byte count ($saved = ${rt.rangedSignedFixedMeasure(1)})',
  );
  check(
    file[0] == home.tableFixedForm,
    "the file's form byte is 3 (§3.4, got ${file[0]})",
  );
  final body = rt.rangedSignedFixedHeaderBytes + 8;
  final view = ByteData.sublistView(file);
  check(
    view.getUint64(rt.rangedSignedFixedHeaderBytes, Endian.little) ==
        rt.rangedSignedFixedHash,
    'the record opens with the eight-byte layout hash',
  );

  // THE WIRE, derived from the layout: int8 at +0, int16 at +4, int32 at
  // +12, int64 at +28, elements at +64 — SLE, little-endian two's complement.
  void wantBytes(int at, List<int> want, String what) {
    var ok = true;
    for (var i = 0; i < want.length; i++) {
      if (file[body + at + i] != want[i]) {
        ok = false;
      }
    }
    check(
      ok,
      '$what (want ${want.map((b) => '0x${b.toRadixString(16).padLeft(2, '0')}').join(' ')}, '
      'got ${[for (var i = 0; i < want.length; i++) file[body + at + i]].map((b) => '0x${b.toRadixString(16).padLeft(2, '0')}').join(' ')})',
    );
  }

  wantBytes(0, [0x80], 'int8 -128 rides its two-complement byte');
  wantBytes(3, [0xFF], 'int8 -1 rides 0xFF, never 0x01');
  wantBytes(4, [0x00, 0x80], 'int16 -32768 rides 00 80 little-endian');
  wantBytes(10, [0xFF, 0xFF], 'int16 -1 rides FF FF');
  wantBytes(12, [0x00, 0x00, 0x00, 0x80], 'int32 INT32_MIN rides 00 00 00 80');
  wantBytes(24, [0xFF, 0xFF, 0xFF, 0xFF], 'int32 -1 rides FF FF FF FF');
  wantBytes(28, [
    0x00,
    0x00,
    0x00,
    0x00,
    0x00,
    0x00,
    0x00,
    0x80,
  ], 'int64 INT64_MIN rides 00*7 80');
  wantBytes(52, [
    0xFF,
    0xFF,
    0xFF,
    0xFF,
    0xFF,
    0xFF,
    0xFF,
    0xFF,
  ], 'int64 -1 rides FF*8');
  wantBytes(64, [
    0xFF,
    0x7F,
    0x00,
    0x80,
    0xFF,
    0xFF,
    0x01,
    0x00,
  ], 'int16 elements 32767,-32768,-1,1 ride LE two-complement');

  // ---- READ (§3.4: prefill and ONE loop over ONE plan) ----
  final back = <home.RangedSigned>[home.RangedSigned()];
  final r = home.TableFixedReport();
  final read = rt.rangedSignedFixedLoad(
    back,
    1,
    file,
    file.length,
    rt.rangedSignedFixedNewPlan(),
    r,
  );
  check(read == 1, 'the record reads back (n=$read)');
  check(
    !r.malformed && r.refused == home.TableFixedRefusal.none,
    'valid data is no refusal (malformed=${r.malformed} refused=${r.refused})',
  );
  check(r.clamped == 0, 'valid data moves no clamp (clamped=${r.clamped})');
  check(
    r.widened == 0,
    'same-width data moves no widen (widened=${r.widened})',
  );

  final b = back[0];
  check(b.i8Span == -128, 'int8: INT8_MIN lands exact (got ${b.i8Span})');
  check(b.i8Low == 126, 'int8: 126 at the declared max lands exact');
  check(b.i8High == -127, 'int8: -127 at the declared min lands exact');
  check(b.i8Inside == -1, 'int8: -1 comes back signed -1, not 255');
  check(b.i16Span == -32768, 'int16: INT16_MIN lands exact (got ${b.i16Span})');
  check(b.i16Low == 32766, 'int16: 32766 at the declared max lands exact');
  check(b.i16High == -32767, 'int16: -32767 at the declared min lands exact');
  check(b.i16Inside == -1, 'int16: -1 comes back signed -1, not 65535');
  check(
    b.i32Span == -2147483648,
    'int32: INT32_MIN lands exact (got ${b.i32Span})',
  );
  check(
    b.i32Low == 2147483646,
    'int32: 2147483646 at the declared max lands exact',
  );
  check(
    b.i32High == -2147483647,
    'int32: -2147483647 at the declared min lands exact',
  );
  check(b.i32Inside == -1, 'int32: -1 comes back signed -1, not 4294967295');
  check(
    b.i64Span == int64Min,
    'int64: INT64_MIN lands exact (got ${b.i64Span})',
  );
  check(
    b.i64Low == 9223372036854775806,
    'int64: 9223372036854775806 at the declared max lands exact',
  );
  check(
    b.i64High == -9223372036854775807,
    'int64: -9223372036854775807 at the declared min lands exact',
  );
  check(
    b.i64Inside == -1,
    'int64: -1 comes back signed -1, not 18446744073709551615',
  );
  check(b.edgesCount == 4, 'the [..4]int16 count lands 4');
  check(
    b.edges[0] == 32767 &&
        b.edges[1] == -32768 &&
        b.edges[2] == -1 &&
        b.edges[3] == 1,
    'int16 elements land exact, -1 signed and -32768 whole',
  );

  // ---- READ AND SAVE AGAIN (docs/FIXED-FORM-ALGORITHM.md:1682: the bytes
  // must be identical) ----
  final again = Uint8List(rt.rangedSignedFixedMeasure(1));
  final saved2 = rt.rangedSignedFixedSave(back, 1, again);
  check(
    saved2 == again.length,
    'the loaded record saves again ($saved2 = ${again.length})',
  );
  var same = true;
  for (var i = 0; i < file.length; i++) {
    if (file[i] != again[i]) {
      same = false;
    }
  }
  check(same, 'read-and-save-again is byte-identical to the first write');

  if (failed) {
    exit(1);
  }
  print('VD: signed integers 8/16/32/64 valid-data write/read — PASS');
}
