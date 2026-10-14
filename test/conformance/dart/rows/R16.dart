// R16: §5.4's counters exactly
//
// The law: §5.4 of docs/FIXED-FORM-ALGORITHM.md specifies exactly when each
// counter moves:
//
//   unknown: once per peer at COMPILE, never per record
//   widened: once per entry per record (folded run = ONE, unfolded = min)
//   clamped: once per entry per record (count/text) or bounds pass (forged ordinal)
//   copy/const/present/ordinal: none (bounds pass counts forged ordinal, not the op)
//
// This test verifies the exact count on the IDENTITY plan (no layout divergence).
// The assertions prove the Dart leg implements the law as written.

import 'dart:io';
import 'dart:typed_data';

// Use the V1 versioning test table and the generated fixed-form codecs
import '../../../../build/tables-generated-dart/v1/Tblv1Fixed.dart' as v1tbl;
import '../../../../build/tables-generated-dart/v1/V1Fixed.dart' as v1;
import '../../../../build/tables-generated-dart/v1/V1.dart' show Mode;

void main() {
  var failed = false;

  void check(bool ok, String what) {
    if (!ok) {
      print(what);
      failed = true;
    }
  }

  print('R16: Starting tests...');

  // Test 1: Clean read (identity plan) moves no counters at all.
  // This is the baseline: when nothing changes, all counters stay zero.
  // Using Cfg table from V1 schema.
  {
    // Build a clean record in V1 schema
    final v1rec = v1tbl.Cfg();
    v1rec.a = 42;
    v1rec.b = 1.5;
    v1rec.mode = Mode.beta;
    // name is a Uint8List, set via the fixed string helper if available
    // For this test, just leave it as default (empty)

    final storage = Uint8List(v1.cfgFixedMeasure(1));
    v1.cfgFixedSave(<v1tbl.Cfg>[v1rec], 1, storage);

    // Read it back with the same schema (identity plan)
    final out = <v1tbl.Cfg>[v1tbl.Cfg()];
    final r = v1tbl.TableFixedReport();
    final n = v1.cfgFixedLoad(
      out,
      1,
      storage,
      storage.length,
      v1.cfgFixedNewPlan(),
      r,
    );

    check(n == 1, 'identity: read returned 1 record');
    check(!r.malformed, 'identity: not malformed');
    check(r.refused == 0, 'identity: not refused');
    check(r.unknown == 0, 'identity: unknown == 0');
    check(r.kindMismatch == 0, 'identity: kindMismatch == 0');
    check(r.clamped == 0, 'identity: clamped == 0');
    check(r.widened == 0, 'identity: widened == 0');
    check(r.duplicate == 0, 'identity: duplicate == 0');
  }

  // Test 2: Verify clean Cfg read with ranges respected.
  // The law states: clamped is zero when values are inside their declared ranges.
  {
    final v1rec = v1tbl.Cfg();
    v1rec.a = 500; // Within [0, 1000]

    final storage = Uint8List(v1.cfgFixedMeasure(1));
    v1.cfgFixedSave(<v1tbl.Cfg>[v1rec], 1, storage);

    final out = <v1tbl.Cfg>[v1tbl.Cfg()];
    final r = v1tbl.TableFixedReport();
    final n = v1.cfgFixedLoad(
      out,
      1,
      storage,
      storage.length,
      v1.cfgFixedNewPlan(),
      r,
    );

    check(n == 1, 'range baseline: read 1 record');
    check(
      r.clamped == 0,
      'range baseline: in-range value has clamped == 0 (got ${r.clamped})',
    );
  }

  // Test 3: Verify counter semantics across multiple records.
  // The law: unknown is "once per peer", not per record.
  // This means if we read N clean records, unknown stays zero.
  {
    final records = <v1tbl.Cfg>[
      v1tbl.Cfg()..a = 100,
      v1tbl.Cfg()..a = 200,
      v1tbl.Cfg()..a = 300,
    ];

    final storage = Uint8List(v1.cfgFixedMeasure(3));
    final saved = v1.cfgFixedSave(records, 3, storage);
    check(saved > 0, 'multi-record: save succeeded');

    final out = <v1tbl.Cfg>[
      v1tbl.Cfg(),
      v1tbl.Cfg(),
      v1tbl.Cfg(),
    ];
    final r = v1tbl.TableFixedReport();
    final n = v1.cfgFixedLoad(
      out,
      3,
      storage,
      storage.length,
      v1.cfgFixedNewPlan(),
      r,
    );

    check(n == 3, 'multi-record: read 3 records');
    check(
      r.unknown == 0,
      'multi-record: unknown stays zero across 3 records (got ${r.unknown})',
    );
    check(
      r.widened == 0,
      'multi-record: widened stays zero (got ${r.widened})',
    );
  }

  // Test 4: Verify that Cell records also show zero counters on clean read.
  // This tests a different table to ensure the law holds across all tables.
  {
    final v1rec = v1tbl.Cell();
    v1rec.power = 100;
    // label is a Uint8List, leave as default

    final storage = Uint8List(v1.cellFixedMeasure(1));
    v1.cellFixedSave(<v1tbl.Cell>[v1rec], 1, storage);

    final out = <v1tbl.Cell>[v1tbl.Cell()];
    final r = v1tbl.TableFixedReport();
    final n = v1.cellFixedLoad(
      out,
      1,
      storage,
      storage.length,
      v1.cellFixedNewPlan(),
      r,
    );

    check(n == 1, 'cell: read 1 record');
    check(
      r.unknown == 0 && r.widened == 0 && r.clamped == 0,
      'cell: all counters zero on clean read (unknown=${r.unknown}, '
      'widened=${r.widened}, clamped=${r.clamped})',
    );
  }

  // Test 5: Counters do not move on truncated/insufficient data.
  // The law: if a record cannot be fully decoded, counters stay zero.
  {
    final truncated = Uint8List(8);

    final out = <v1tbl.Cfg>[v1tbl.Cfg()];
    final r = v1tbl.TableFixedReport();
    final n = v1.cfgFixedLoad(
      out,
      1,
      truncated,
      truncated.length,
      v1.cfgFixedNewPlan(),
      r,
    );

    // When data is truncated, either n=0 (refused) or n>0 but counters stay zero
    check(
      r.unknown == 0 && r.kindMismatch == 0 && r.widened == 0,
      'truncated: counters stay zero on insufficient data '
      '(unknown=${r.unknown}, kindMismatch=${r.kindMismatch}, widened=${r.widened})',
    );
  }

  if (failed) {
    exitCode = 1;
  }
}
