// R1.dart — COMPILE lays the lineage down as static data at build time,
// oldest first and the current layout last, from the lock.
//
// docs/FIXED-FORM-ALGORITHM.md §5.2: the lineage from the lock, oldest first,
// the current layout last, is static data the BUILD lays down. Nothing parses
// a stranger's layout at run time; the reader matches the file's header hash
// against this list and compares the bytes it already holds.
//
// The generated Fixed code carries three pieces of lineage static data:
//   <table>FixedKnown       — the list of TableFixedKnownLayout entries
//   <table>FixedFloor       — 1 + highest retired index, 0 when none
//   <table>FixedLineagePlans — one plan per entry, built from the lock's bytes
//
// This test asserts that these exist for every fixed table in the generated
// examples unit, and that for tables with a single entry (no lock), that
// entry's hash matches the table's own declared hash — which is the trivial
// case of "oldest first, current last" when there is only one layout.
//
// Run: dart run test/conformance/dart/rows/R1.dart

import 'dart:io';
import 'dart:typed_data';

import '../../../../build/tables-generated-dart/examples/TabledemoFixed.dart';
import '../../../../build/tables-generated-dart/examples/TablesFixed.dart'
    as tables;

int _fails = 0;

void check(bool ok, String what) {
  if (!ok) {
    stderr.writeln('FAIL: $what');
    _fails++;
  }
}

bool _bytesEqual(Uint8List a, Uint8List b) {
  if (a.length != b.length) return false;
  for (var i = 0; i < a.length; i++) {
    if (a[i] != b[i]) return false;
  }
  return true;
}

// verifyLineageStaticData asserts that a table's generated code carries the
// three pieces of lineage static data (§5.2), and that for a single-entry
// lineage (no lock) the one entry's hash equals the table's own hash —
// which is the "oldest first, current last" law when there is only one entry.
void verify(
  String tableName,
  List<TableFixedKnownLayout> known,
  int floor,
  List<TableFixedLineagePlan> plans,
  int ownHash,
  int bodyBytes,
  int layoutBytes,
  Uint8List layout,
) {
  // THE LINEAGE IS STATIC DATA: a list the build laid down, not something
  // computed at run time (§5.2, §5.9 #3).
  check(
    known.isNotEmpty,
    '$tableName: FixedKnown is empty — COMPILE did not lay the lineage down',
  );
  check(
    plans.isNotEmpty,
    '$tableName: FixedLineagePlans is empty — COMPILE did not compile plans',
  );

  // OLDEST FIRST, CURRENT LAST: when the lineage has one entry (no lock),
  // that entry IS the current layout, and it must match the table's own hash.
  if (known.length == 1) {
    final entry = known[0];
    check(
      entry.hash == ownHash,
      '$tableName: the single lineage entry\'s hash '
      '0x${entry.hash.toRadixString(16)} '
      'does not match the table\'s own hash 0x${ownHash.toRadixString(16)} '
      '— the build\'s own layout is not last in a one-entry lineage',
    );
    check(
      entry.recordBytes == 8 + bodyBytes,
      '$tableName: record_bytes ${entry.recordBytes} is not 8 + body '
      '($bodyBytes) — §5.2 record_bytes is THE WHOLE RECORD',
    );
    check(
      entry.layoutBytes == layoutBytes,
      '$tableName: layout_bytes ${entry.layoutBytes} != $layoutBytes',
    );
    final layoutMatches =
        entry.layoutBytes == layoutBytes && _bytesEqual(entry.layout, layout);
    check(
      layoutMatches,
      '$tableName: the lineage entry\'s layout differs from the table\'s own '
      '— the static data does not match the build\'s layout',
    );
  }

  // THE FLOOR: 1 + highest retired index, 0 when none is (§5.2).
  check(floor >= 0, '$tableName: FixedFloor is negative ($floor)');

  // ONE PLAN PER ENTRY (§5.9 #3).
  check(
    plans.length == known.length,
    '$tableName: ${plans.length} plans but ${known.length} known — '
    'one plan per lineage entry',
  );
}

void main() {
  // RootConfig
  verify(
    'RootConfig',
    tables.rootConfigFixedKnown,
    tables.rootConfigFixedFloor,
    tables.rootConfigFixedLineagePlans,
    tables.rootConfigFixedHash,
    tables.rootConfigFixedBodyBytes,
    tables.rootConfigFixedLayoutBytes,
    tables.rootConfigFixedLayout,
  );

  // WeaponConfig
  verify(
    'WeaponConfig',
    tables.weaponConfigFixedKnown,
    tables.weaponConfigFixedFloor,
    tables.weaponConfigFixedLineagePlans,
    tables.weaponConfigFixedHash,
    tables.weaponConfigFixedBodyBytes,
    tables.weaponConfigFixedLayoutBytes,
    tables.weaponConfigFixedLayout,
  );

  // LoadoutConfig
  verify(
    'LoadoutConfig',
    tables.loadoutConfigFixedKnown,
    tables.loadoutConfigFixedFloor,
    tables.loadoutConfigFixedLineagePlans,
    tables.loadoutConfigFixedHash,
    tables.loadoutConfigFixedBodyBytes,
    tables.loadoutConfigFixedLayoutBytes,
    tables.loadoutConfigFixedLayout,
  );

  // ProfileConfig
  verify(
    'ProfileConfig',
    tables.profileConfigFixedKnown,
    tables.profileConfigFixedFloor,
    tables.profileConfigFixedLineagePlans,
    tables.profileConfigFixedHash,
    tables.profileConfigFixedBodyBytes,
    tables.profileConfigFixedLayoutBytes,
    tables.profileConfigFixedLayout,
  );

  if (_fails == 0) {
    stdout.writeln(
      'ok: lineage static data present and consistent for all fixed tables',
    );
  } else {
    stdout.writeln('$_fails assertion(s) failed');
  }
  exit(_fails == 0 ? 0 : 1);
}
