// W8 — bytes(N) takes the array row (the dart leg, fixed form, form byte 3).
//
// THE LAW (docs/FIXED-FORM-ALGORITHM.md:262, §4.1, fix 10): "A `bytes(N)` takes
// the ARRAY's row, not a text field's — the count to `aux` and the elements to
// `dst`", where a text field's row is the other way round — dst the length,
// aux the buffer. `bytes(N)` rides as an array of u8 on this wire (§3.4, kind
// 14), so under the text convention a plan compiled from a bytes(N)'s row
// hands the compiler a count destination that is the BUFFER'S FIRST FOUR BYTES
// and an element destination that is the LENGTH FIELD. The write side rides
// with it (§3.1, docs/FIXED-FORM-ALGORITHM.md:202): text and bytes write
// `length` units, never all Max.
//
// THE PRODUCTION PATH this test drives, end to end, every stage generated:
//
//   profileConfigFixedSave      the write: the template, then the stores
//                               (internal/codegen/darttable/fixeddart.go,
//                               emitWriteBody/emitWritePayload)
//   TableFixedCompiler.compile  the plan compiler reading MY dst rows
//                               (internal/codegen/darttable/fixedruntime.go,
//                               compileEntry, case 14 — the row this law is)
//   tableFixedRun               the one read loop over the compiled plan
//   profileConfigFixedDecode    the decode every path shares
//
// THE VECTOR is built here, from the law, because testdata/conformance/tables
// carries no fixed-form vector for this cell (its fixtures are the block, cook
// and json surfaces): one record whose bytes(16) field `icon` carries FOUR
// live bytes — the writer writes `length` units, never all Max, so the field's
// slack past the live byte stays the template's zeros. The offsets come from
// the declaration (tables/examples/Tables.schema, ProfileConfig): `name
// string(32)` takes 4 + 32 = 36 bytes, so icon's LENGTH is at body offset 36
// and its BUFFER at 40 — the same offsets profileConfigFixedDecode reads (at +
// 36, at + 40) and the identity plan's text entry carries (dst 36, aux 40).
//
// THE CONTROL is the old row, built by swapping the two columns back on a
// copy of this build's own rows and compiling the same plan from the same
// layout — the twin of test/tables/fixedform_main.cpp bytes_row_case, which
// is the C++ reference's assertion of this cell. The row is found by its shape
// and not by its index, so the control does not quietly stop pointing at it
// the day a field moves.
//
//   dart run test/conformance/dart/rows/W8.dart      (from the repo root)
//
// Exit 0 green, 1 red; one line per assertion.

import 'dart:io';
import 'dart:typed_data';

import '../../../../build/tables-generated-dart/examples/TablesFixed.dart'
    as tables;
import '../../../../build/tables-generated-dart/examples/TabledemoFixed.dart'
    as home;

int failures = 0;

void check(bool ok, String what) {
  stdout.writeln('${ok ? "ok" : "FAIL"} - $what');
  if (!ok) {
    failures++;
  }
}

// THE VECTOR, from the law: four live bytes, length 4, never all Max.
final List<int> live = <int>[0xDE, 0xAD, 0xBE, 0xEF];
const int iconLengthAt = 36; // 4 + 32: name string(32) before icon
const int iconBufferAt = 40;

void main() {
  // ---- THE ROW ------------------------------------------------------------
  //
  // The five lanes of a dst row: dst, stride, aux, counted, arg. The bytes row
  // is found by its SHAPE — stride one and a live count is a bytes(N) and
  // nothing else that also carries the bytes flavour — and not by its index.
  final rows =
      tables.profileConfigFixedDst.length ~/ home.TableFixedLane.dstLanes;
  var bytesRow = -1;
  var bytesRows = 0;
  var textRow = -1;
  var textRows = 0;
  for (var r = 0; r < rows; r++) {
    final b = r * home.TableFixedLane.dstLanes;
    final counted =
        tables.profileConfigFixedDst[b + home.TableFixedLane.dstCounted];
    final stride =
        tables.profileConfigFixedDst[b + home.TableFixedLane.dstStride];
    final arg = tables.profileConfigFixedDst[b + home.TableFixedLane.dstArg];
    if (stride == 1 && counted != 0 && arg == home.TableFixedOp.textBytes) {
      bytesRow = r;
      bytesRows++;
    }
    // a utf8 TEXT row: the flavour 1, no live count, no stride
    if (stride == 0 && counted == 0 && arg == 1) {
      textRow = r;
      textRows++;
    }
  }
  check(
    bytesRows == 1,
    'W8: the bytes(N) row is the one row with stride one, a live count and the '
    'bytes flavour (found $bytesRows)',
  );
  check(
    textRows == 1,
    'W8: the utf8 text row is the one row with no count, no stride and the '
    'utf8 flavour (found $textRows)',
  );

  // THE ROW ITSELF: the count to `aux` (the length field at 36) and the
  // elements to `dst` (the buffer at 40) — the ARRAY's row. The identity plan
  // lands this field with the TEXT op (length 36, units 40) and reads neither
  // column; the compile row is what a stranger's plan is walked through.
  if (bytesRow >= 0) {
    final b = bytesRow * home.TableFixedLane.dstLanes;
    check(
      tables.profileConfigFixedDst[b + home.TableFixedLane.dstAux] ==
          iconLengthAt,
      'W8: the bytes(N) row sends the count to aux — the length field at '
      '$iconLengthAt (got ${tables.profileConfigFixedDst[b + home.TableFixedLane.dstAux]})',
    );
    check(
      tables.profileConfigFixedDst[b + home.TableFixedLane.dstOffset] ==
          iconBufferAt,
      'W8: the bytes(N) row sends the elements to dst — the buffer at '
      '$iconBufferAt (got ${tables.profileConfigFixedDst[b + home.TableFixedLane.dstOffset]})',
    );
  }
  // ...AND A TEXT FIELD'S ROW IS THE OTHER WAY ROUND: dst the length (0),
  // aux the buffer (4) — name string(32) is the field before icon.
  if (textRow >= 0) {
    final b = textRow * home.TableFixedLane.dstLanes;
    check(
      tables.profileConfigFixedDst[b + home.TableFixedLane.dstOffset] == 0 &&
          tables.profileConfigFixedDst[b + home.TableFixedLane.dstAux] == 4,
      'W8: a text field\'s row is the other way round — dst the length (0), '
      'aux the buffer (4)',
    );
  }

  // ---- THE WRITE, THEN THE READ THROUGH THE COMPILED PLAN -----------------
  //
  // The record: icon carries four live bytes; the writer puts the length word
  // at 36 and those four bytes at 40, and nothing past them.
  final one = home.ProfileConfig();
  one.icon.setRange(0, live.length, live);
  one.iconLength = live.length;
  final file = Uint8List(tables.profileConfigFixedMeasure(1));
  check(
    tables.profileConfigFixedSave(<home.ProfileConfig>[one], 1, file) ==
        file.length,
    'W8: the production writer saves the record',
  );
  final bodyAt = tables.profileConfigFixedHeaderBytes + 8;
  final fileView = ByteData.sublistView(file);
  check(
    fileView.getInt32(bodyAt + iconLengthAt, Endian.little) == live.length,
    'W8: the length word rides in front, at $iconLengthAt',
  );
  var liveOk = true;
  for (var i = 0; i < live.length; i++) {
    liveOk = liveOk && file[bodyAt + iconBufferAt + i] == live[i];
  }
  check(liveOk, 'W8: the live bytes ride at the buffer, at $iconBufferAt');

  // THE PLAN, compiled from this build's own layout against its own rows —
  // the shape a peer's plan is compiled through, with `theirs` the layout on
  // the wire and `mine` this reader's own rows.
  final plan = tables.profileConfigFixedNewPlan();
  check(
    plan.theirs.parse(
      ByteData.sublistView(tables.profileConfigFixedLayout),
      0,
      tables.profileConfigFixedLayout.length,
    ),
    'W8: the layout parses into the plan',
  );
  final made = home.TableFixedCompiler.compile(
    plan,
    tables.profileConfigFixedLayout,
    ByteData.sublistView(tables.profileConfigFixedLayout),
    tables.profileConfigFixedDst,
    tables.profileConfigFixedCover,
    tables.profileConfigFixedCoverCount,
    home.TableFixedReport(),
  );
  check(made > 0, 'W8: the plan compiles ($made entries)');
  home.tableFixedFillRun(
    plan.fill,
    plan.fillCount,
    tables.profileConfigFixedPrefill,
    plan.image,
  );
  final rc = home.TableFixedReport();
  home.tableFixedRun(
    plan.entries,
    made,
    file,
    fileView,
    bodyAt,
    plan.image,
    plan.imageView,
    plan.remap,
    plan.conv,
    rc,
  );
  final back = home.ProfileConfig();
  home.profileConfigFixedDecode(back, plan.image, plan.imageView, 0, rc);
  check(
    back.iconLength == live.length && bytesRide(back.icon),
    'W8 GREEN: the array row lands the buffer in the buffer and the length in '
    'the length — icon reads ${liveText(back.icon, back.iconLength)}',
  );

  // ---- THE NEGATIVE CONTROL: THE TEXT ROW, AS IT WAS ----------------------
  //
  // Swap the two columns back on a COPY of the rows — dst the buffer's offset
  // becomes the length's, and aux the other way — and compile the same plan
  // from the same layout. The count then lands in the buffer's first four
  // bytes and the elements over the length field, and the value must NOT come
  // back: what reports the law is the value that comes back, not a refusal.
  final swapped = Int32List.fromList(tables.profileConfigFixedDst);
  if (bytesRow >= 0) {
    final b = bytesRow * home.TableFixedLane.dstLanes;
    final d = swapped[b + home.TableFixedLane.dstOffset];
    swapped[b + home.TableFixedLane.dstOffset] =
        swapped[b + home.TableFixedLane.dstAux];
    swapped[b + home.TableFixedLane.dstAux] = d;
  }
  final wrongPlan = tables.profileConfigFixedNewPlan();
  check(
    wrongPlan.theirs.parse(
      ByteData.sublistView(tables.profileConfigFixedLayout),
      0,
      tables.profileConfigFixedLayout.length,
    ),
    'W8: the layout parses into the control plan',
  );
  final made2 = home.TableFixedCompiler.compile(
    wrongPlan,
    tables.profileConfigFixedLayout,
    ByteData.sublistView(tables.profileConfigFixedLayout),
    swapped,
    tables.profileConfigFixedCover,
    tables.profileConfigFixedCoverCount,
    home.TableFixedReport(),
  );
  check(
    made2 > 0,
    'W8: the plan compiles either way — the rows are not what refuses '
    '($made2 entries)',
  );
  home.tableFixedFillRun(
    wrongPlan.fill,
    wrongPlan.fillCount,
    tables.profileConfigFixedPrefill,
    wrongPlan.image,
  );
  final rc2 = home.TableFixedReport();
  home.tableFixedRun(
    wrongPlan.entries,
    made2,
    file,
    fileView,
    bodyAt,
    wrongPlan.image,
    wrongPlan.imageView,
    wrongPlan.remap,
    wrongPlan.conv,
    rc2,
  );
  final wrong = home.ProfileConfig();
  home.profileConfigFixedDecode(
    wrong,
    wrongPlan.image,
    wrongPlan.imageView,
    0,
    rc2,
  );
  final right2 = wrong.iconLength == live.length && bytesRide(wrong.icon);
  check(
    !right2,
    'W8 NEGATIVE CONTROL: the text row really does write the count into the '
    'buffer — icon reads ${liveText(wrong.icon, wrong.iconLength)}',
  );

  exit(failures == 0 ? 0 : 1);
}

bool bytesRide(Uint8List icon) {
  for (var i = 0; i < live.length; i++) {
    if (icon[i] != live[i]) {
      return false;
    }
  }
  return true;
}

String liveText(Uint8List icon, int used) {
  final parts = <String>['len $used'];
  for (var i = 0; i < used && i < 4; i++) {
    parts.add('0x${icon[i].toRadixString(16)}');
  }
  return parts.join(' ');
}
