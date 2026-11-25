// R14 — EVERY COMPILED ENTRY'S REACH IS IN THE WRITER'S COORDINATES: a union
// tag grown 1 -> 8 reads the writer's tag at the WRITER's width and compiles
// whole.
//
// THE LAW (docs/FIXED-FORM-ALGORITHM.md §5.2 EMIT kind 15): when a union's tag
// widens between writer and reader, the union's None `const` LANDS the
// reader's my_tag bytes but reads the writer's raw tag at THE WRITER'S TAG
// WIDTH, their_tag bytes, never my_tag; its source reach is src + their_tag,
// inside the writer's record. A reach at the reader's width would read the
// arm's payload into the tag and refuse a lawful file
// layout_record_too_large. A const that reads no record byte (an arm's tag
// const) has no source reach of its own.
//
// WHERE THE DART LEG KEEPS IT: the runtime's plan compiler
// (internal/codegen/darttable/fixedruntime.go, emitted into every unit's
// <Pkg>Fixed.dart): TableFixedCompiler.compileEntry case 15 pushes the None
// const with the writer's tag width in meta >>> 8, TableFixedCompiler.srcEnd
// takes its reach as src + (meta >>> 8), push holds every reach to the
// writer's declared record (plan.record), and tableFixedRun's const op reads
// the raw tag at meta >>> 8 to refuse a tag past the writer's arms
// out_of_range (§4.6).
//
// THE VECTOR (a forged lock layout: 1 -> 8 cannot come from a real schema, whose
// tag width follows its arm count). Layout block = u32LE entry count, then
// 17-byte entries: id u64LE, kind u8, size u32LE, children u32LE. Kind 13
// table, 15 union (size = tag + widest arm), 8 a u32.
//   THEIRS (writer): [1,13,5,1] [7,15,5,1] [9,8,4,0] — a 5-byte record, one
//   union of tag 1 over one u32.
//   MINE(tag) (reader): [1,13,tag+4,1] [7,15,tag+4,1] [9,8,4,0]; the reader's
//   storage holds the tag at image 0 (tag bytes) and the u32 at image `tag`.
//
// The over-reach refusal itself (an entry that really reaches past the
// writer's record) is the compiler's defence in depth, held by
// internal/codegen/darttable/fixedroadmap_t03_test.go (dart/R14).
//
// run: dart --enable-asserts run test/conformance/dart/rows/R14.dart
//   (from the repository root, after build/tables-generated-dart/.stamp)
// exit: 0 = every assertion passed, 1 = one or more failed

import 'dart:io';
import 'dart:typed_data';

import '../../../../build/tables-generated-dart/g1/Tblg1Fixed.dart' as rt;

int failures = 0;

void check(bool ok, String what) {
  stdout.writeln('${ok ? "ok" : "FAIL"} - R14: $what');
  if (!ok) {
    failures++;
  }
}

Uint8List block(List<List<int>> entries) {
  final out = Uint8List(4 + 17 * entries.length);
  final v = ByteData.sublistView(out);
  v.setUint32(0, entries.length, Endian.little);
  for (var i = 0; i < entries.length; i++) {
    final at = 4 + 17 * i;
    final e = entries[i];
    v.setUint64(at, e[0], Endian.little);
    v.setUint8(at + 8, e[1]);
    v.setUint32(at + 9, e[2], Endian.little);
    v.setUint32(at + 13, e[3], Endian.little);
  }
  return out;
}

final Uint8List theirs = block([
  [1, 13, 5, 1],
  [7, 15, 5, 1],
  [9, 8, 4, 0],
]);

Uint8List mine(int tag) => block([
  [1, 13, tag + 4, 1],
  [7, 15, tag + 4, 1],
  [9, 8, 4, 0],
]);

// MY destination rows, five lanes per entry of mine (TableFixedLane.dstLanes):
// the root at 0, the union's tag at 0 (dst and aux, as the generator lays a
// union), the u32 arm at `tag` inside the union.
Int32List dst(int tag) => Int32List.fromList([
  0, 0, 0, 0, 0, //
  0, 0, 0, 0, 0, //
  tag, 0, 0, 0, 0, //
]);

class Compiled {
  Compiled(this.made, this.plan, this.report);
  final int made;
  final rt.TableFixedPlan plan;
  final rt.TableFixedReport report;
}

Compiled compile(int tag) {
  final plan = rt.TableFixedPlan(256, tag + 4, 1024);
  final report = rt.TableFixedReport();
  final parsed = plan.theirs.parse(
    ByteData.sublistView(theirs),
    0,
    theirs.length,
  );
  check(
    parsed,
    'THEIRS parses: ${rt.TableFixedRefusal.name(plan.theirs.refusal)}',
  );
  final m = mine(tag);
  final made = rt.TableFixedCompiler.compile(
    plan,
    m,
    ByteData.sublistView(m),
    dst(tag),
    Int32List(0),
    0,
    report,
  );
  return Compiled(made, plan, report);
}

// [op, src, dst, size, aux, guard, arg, meta] of every entry.
List<List<int>> entries(rt.TableFixedPlan plan, int count) {
  final out = <List<int>>[];
  for (var i = 0; i < count; i++) {
    final b = i * rt.TableFixedLane.lanes;
    final e = plan.entries;
    out.add([
      e[b + rt.TableFixedLane.op],
      e[b + rt.TableFixedLane.src],
      e[b + rt.TableFixedLane.dst],
      e[b + rt.TableFixedLane.size],
      e[b + rt.TableFixedLane.aux],
      e[b + rt.TableFixedLane.guard],
      e[b + rt.TableFixedLane.arg],
      e[b + rt.TableFixedLane.meta],
    ]);
  }
  return out;
}

// RUN the compiled plan over one writer record into a fresh image.
(int, Uint8List) run(Compiled c, List<int> record) {
  final src = Uint8List.fromList(record);
  final plan = c.plan;
  plan.image.fillRange(0, plan.image.length, 0xEE);
  rt.tableFixedFillRun(
    plan.fill,
    plan.fillCount,
    Uint8List(plan.image.length),
    plan.image,
  );
  // A RUN THAT THROWS (a read past the writer's record) is a named FAIL of
  // this row, never an unhandled exception.
  int why;
  try {
    why = rt.tableFixedRun(
      plan.entries,
      c.made,
      src,
      ByteData.sublistView(src),
      0,
      plan.image,
      plan.imageView,
      plan.remap,
      plan.conv,
      rt.TableFixedReport(),
    );
  } on Error catch (e) {
    check(false, 'the run over $record does not throw: $e');
    why = -1;
  }
  return (why, Uint8List.fromList(plan.image));
}

void main() {
  // ---- 1. THE CELL: THEIRS against MINE(8) compiles WHOLE ------------------
  final c = compile(8);
  check(
    c.made > 0,
    'the grown tag (1 -> 8) compiles whole: compile answers ${c.made} '
    '(-3 is layout_record_too_large)',
  );
  check(!c.plan.tooLarge, 'no entry reaches past the writer\'s record of 5');
  check(
    !c.plan.overflow && !c.plan.unlawful && !c.plan.optionalIntoBare,
    'nothing else refused the pair',
  );

  if (c.made > 0) {
    final es = entries(c.plan, c.made);

    // ---- 2. THE None CONST: src 0, lands 8, reads the writer's 1-byte tag ---
    final none = es.where(
      (e) =>
          e[0] == rt.TableFixedOp.constant && e[4] == 0 && (e[7] & 0xff) != 0,
    );
    check(none.length == 1, 'exactly one union None const in the plan');
    if (none.length == 1) {
      final e = none.first;
      check(
        e[1] == 0,
        'the None const reads the writer\'s tag at src 0, got ${e[1]}',
      );
      check(e[3] == 8, 'the None const LANDS my 8-byte tag, got size ${e[3]}');
      check(
        (e[7] >>> 8) == 1,
        'the None const reads the WRITER\'s 1-byte tag, never 8: got ${e[7] >>> 8}',
      );
      check(
        (e[7] & 0xff) == 1,
        'it carries the writer\'s arm count 1, got ${e[7] & 0xff}',
      );
      check(e[5] == rt.tableFixedNoGuard, 'the None const is unguarded');
      final reach = rt.TableFixedCompiler.srcEnd(
        e[0],
        e[1],
        e[3],
        e[4],
        e[5],
        1,
        e[7],
      );
      check(
        reach == 1 && reach <= 5,
        'its reach is 0 + 1 = 1 of the writer\'s 5, got $reach',
      );
    }
    check(
      es.any(
        (e) =>
            e[0] == rt.TableFixedOp.copy && e[1] == 1 && e[2] == 8 && e[3] == 4,
      ),
      'the u32 arm is a copy of 4 bytes from src 1 (past the writer\'s 1-byte '
      'tag) to image 8 (past my 8-byte tag)',
    );

    // ---- 3. RUN THE PLAN -----------------------------------------------------
    {
      final (why, image) = run(c, [1, 0x44, 0x33, 0x22, 0x11]);
      final iv = ByteData.sublistView(image);
      check(
        why == rt.TableFixedRefusal.none,
        'arm 1 reads with no refusal, got ${rt.TableFixedRefusal.name(why)}',
      );
      check(
        iv.getUint32(0, Endian.little) == 1 &&
            iv.getUint32(4, Endian.little) == 0,
        'my 8-byte tag lands exactly 1, high bytes zero: got lo=${iv.getUint32(0, Endian.little)} '
        'hi=${iv.getUint32(4, Endian.little)}',
      );
      check(
        iv.getUint32(8, Endian.little) == 0x11223344,
        'the u32 lands whole past my tag: got 0x${iv.getUint32(8, Endian.little).toRadixString(16)}',
      );
    }
    {
      final (why, image) = run(c, [0, 0, 0, 0, 0]);
      final iv = ByteData.sublistView(image);
      check(
        why == rt.TableFixedRefusal.none &&
            iv.getUint32(0, Endian.little) == 0 &&
            iv.getUint32(4, Endian.little) == 0,
        'tag 0 reads as None in my 8-byte tag (${rt.TableFixedRefusal.name(why)})',
      );
    }
    {
      final (why, _) = run(c, [2, 0, 0, 0, 0]);
      check(
        why == rt.TableFixedRefusal.outOfRange,
        'a forged writer tag 2 (past one arm) refuses out_of_range, got '
        '${rt.TableFixedRefusal.name(why)}',
      );
    }
  }

  // ---- 4. THE TWINS: MINE(1) and MINE(2) compile whole -----------------------
  for (final tag in [1, 2]) {
    final t = compile(tag);
    check(
      t.made > 0 && !t.plan.tooLarge,
      'the twin THEIRS vs MINE($tag) compiles whole (${t.made})',
    );
  }

  // ---- 5. THE LINEAGE LANE a generated unit builds at load is READY ---------
  for (final tag in [8, 2]) {
    final known = [
      rt.TableFixedKnownLayout(0x3333333344444444, theirs, theirs.length, 5),
    ];
    final lanes = rt.tableFixedLineagePlans(
      known,
      mine(tag),
      dst(tag),
      Int32List(0),
      0,
      tag + 4,
      0x1111111122222222,
    );
    final l = lanes[0];
    check(
      l.why == rt.TableFixedRefusal.none && l.count > 0,
      'the lineage lane for MINE($tag) is ready: why='
      '${rt.TableFixedRefusal.name(l.why)} count=${l.count}',
    );
  }

  exit(failures == 0 ? 0 : 1);
}
