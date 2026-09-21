// P3.dart — hostile bytes, sweep + sanitizer for the Dart leg.
// Cells dart/P3: the assertion that proves the Dart leg refuses hostile
// bytes on the fixed-form reader for table Chain with its ?Link optional.
//
// Run: dart run test/conformance/dart/rows/P3.dart
// Working directory: the repository root.
//
// The law: a reader never throws on hostile bytes — it always answers by
// name. docs/SPEC-TABLES.md §3.4: "Buffers are caller-owned Uint8Lists and
// nothing throws."
//
// This test checks:
// 1. A valid fixed-form Chain with optional Link present reads GREEN.
// 2. A valid fixed-form Chain with optional Link absent reads GREEN.
// 3. A hostile file (wrong form byte) is refused by name.
// 4. A hostile file (wrong header hash) is refused by name.
// 5. A hostile file (corrupted reserved bytes) is refused malformed.
// 6. A hostile record hash mismatch is refused no_layout.
//
// All six assertions name the production call site: chainFixedLoad in
// P3Fixed.dart, which is called by the conformance driver's block-dump
// surface via RenderFrameBlock.open / PaddedFrameBlock.open. The reader
// answers by name and never throws.

import 'dart:io';
import 'dart:typed_data';

import '../../../../build/tables-generated-dart/p3/Tblp3Fixed.dart' as fixed;
import '../../../../build/tables-generated-dart/p3/P3Fixed.dart' as p3;

int green = 0;
int red = 0;

void check(bool ok, String label) {
  if (ok) {
    green++;
  } else {
    stderr.writeln('FAIL: $label');
    red++;
  }
}

void main() {
  final plan = p3.chainFixedNewPlan();
  final report = fixed.TableFixedReport();

  // ---- 1. Valid file: Chain with optional Link present ----
  {
    final chain = fixed.Chain();
    chain.nameLength = 4;
    chain.name[0] = 0x74; // 't'
    chain.name[1] = 0x69; // 'i'
    chain.name[2] = 0x70; // 'p'
    chain.name[3] = 0x00;
    chain.linkPresent = true;
    chain.link.value = 42;
    chain.link.tagLength = 3;
    chain.link.tag[0] = 0x74; // 't'
    chain.link.tag[1] = 0x61; // 'a'
    chain.link.tag[2] = 0x67; // 'g'

    final bytes = p3.chainFixedMeasure(1);
    final buf = Uint8List(bytes);
    final wrote = p3.chainFixedSave([chain], 1, buf);
    check(wrote == buf.length, 'save present: $wrote == ${buf.length}');

    final got = fixed.Chain();
    report.reset();
    final loaded =
        p3.chainFixedLoad([got], 1, buf, buf.length, plan, report);
    check(loaded == 1, 'load present: $loaded');
    check(report.refused == fixed.TableFixedRefusal.none, 'no refusal');
    check(!report.malformed, 'not malformed');
    check(got.linkPresent, 'link present true');
    check(got.link.value == 42, 'link value 42');
    check(got.nameLength == 4, 'name length 4');
  }

  // ---- 2. Valid file: Chain with optional Link absent ----
  {
    final chain = fixed.Chain();
    chain.nameLength = 4;
    chain.name[0] = 0x74;
    chain.name[1] = 0x69;
    chain.name[2] = 0x70;
    chain.linkPresent = false;

    final bytes = p3.chainFixedMeasure(1);
    final buf = Uint8List(bytes);
    p3.chainFixedSave([chain], 1, buf);

    final got = fixed.Chain();
    report.reset();
    final loaded =
        p3.chainFixedLoad([got], 1, buf, buf.length, plan, report);
    check(loaded == 1, 'load absent');
    check(!got.linkPresent, 'link present false');
    check(got.link.value == 0, 'link absent value default 0');
  }

  // ---- 3. Hostile: wrong form byte (previous_form) ----
  {
    final chain = fixed.Chain();
    chain.nameLength = 4;
    final bytes = p3.chainFixedMeasure(1);
    final buf = Uint8List(bytes);
    p3.chainFixedSave([chain], 1, buf);
    buf[0] = 1; // form byte 1 — previous form

    final got = fixed.Chain();
    report.reset();
    final loaded =
        p3.chainFixedLoad([got], 1, buf, buf.length, plan, report);
    check(loaded == -1, 'wrong form byte refuses');
    check(report.refused == fixed.TableFixedRefusal.previousForm,
        'refusal is previous_form');
  }

  // ---- 4. Hostile: wrong header hash (layout_newer) ----
  {
    final chain = fixed.Chain();
    chain.nameLength = 4;
    final bytes = p3.chainFixedMeasure(1);
    final buf = Uint8List(bytes);
    p3.chainFixedSave([chain], 1, buf);
    buf[8] ^= 0xff; // corrupt the header hash

    final got = fixed.Chain();
    report.reset();
    final loaded =
        p3.chainFixedLoad([got], 1, buf, buf.length, plan, report);
    check(loaded == -1, 'wrong header hash refuses');
    check(report.refused == fixed.TableFixedRefusal.layoutNewer,
        'refusal is layout_newer');
  }

  // ---- 5. Hostile: corrupted reserved byte (malformed) ----
  {
    final chain = fixed.Chain();
    chain.nameLength = 4;
    final bytes = p3.chainFixedMeasure(1);
    final buf = Uint8List(bytes);
    p3.chainFixedSave([chain], 1, buf);
    buf[1] = 1; // first reserved byte non-zero

    final got = fixed.Chain();
    report.reset();
    final loaded =
        p3.chainFixedLoad([got], 1, buf, buf.length, plan, report);
    check(loaded == -1, 'corrupted reserved byte refuses');
    check(report.malformed, 'malformed');
  }

  // ---- 6. Hostile: record hash mismatch (no_layout) ----
  {
    final chain = fixed.Chain();
    chain.nameLength = 4;
    final bytes = p3.chainFixedMeasure(2);
    final buf = Uint8List(bytes);
    p3.chainFixedSave([chain], 1, buf);
    // Forge the second record's hash to zero
    final rec2 = p3.chainFixedHeaderBytes + p3.chainFixedRecordBytes;
    buf.fillRange(rec2, rec2 + 8, 0);

    final plan2 = p3.chainFixedNewPlan();
    report.reset();
    final loaded = p3.chainFixedLoad(
        [fixed.Chain(), fixed.Chain()], 2, buf, buf.length, plan2, report);
    check(loaded == -1, 'record hash mismatch refuses');
    check(report.refused == fixed.TableFixedRefusal.noLayout,
        'refusal is no_layout');
  }

  // ---- Summary ----
  if (red > 0) {
    stderr.writeln('FAIL: $red of ${red + green} assertions failed');
  }
  stdout.writeln('P3: $green green, $red red');
  exit(red == 0 ? 0 : 1);
}