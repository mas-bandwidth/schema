
// ScalarDefaultsValidData.dart
//
// Scalar and enum defaults: valid-data write/read acceptance.
//
// A default-constructed fixed table must serialize to the same bytes as
// a table instance where all fields are at their default values.
//
// Run: dart run test/conformance/dart/rows/ScalarDefaultsValidData.dart

import 'dart:io';
import 'dart:typed_data';

import '../../../../build/tables-generated-dart/examples/TabledemoFixed.dart' as home;
import '../../../../build/tables-generated-dart/examples/TablesFixed.dart' as tables;
import '../../../../build/tables-generated-dart/examples/Tables.dart' as base_tables;

int _fails = 0;

void check(bool ok, String what) {
  if (ok) {
    stdout.writeln('ok - $what');
  } else {
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

void main() {
  // 1. Check that a default-constructed object has the correct default values.
  final defaultProfile = home.ProfileConfig();
  final defaultLoadout = defaultProfile.loadout;
  check(defaultLoadout.grade == base_tables.Grade.silver, 'loadout.grade is Grade.Silver');

  final defaultWeapon = home.WeaponConfig();
  check(defaultWeapon.damage == 21.0, 'weapon.damage is 21.0');
  check(defaultWeapon.speed == 500.0, 'weapon.speed is 500.0');
  check(defaultWeapon.penetration == 1, 'weapon.penetration is 1');

  // 2. Write/Read acceptance
  // Serialize a default-constructed object to bytes.
  final defaultRoot = home.RootConfig();
  final defaultBytes = Uint8List(tables.rootConfigFixedMeasure(1));
  tables.rootConfigFixedSave([defaultRoot], 1, defaultBytes);

  // Read from the serialized bytes and check if it is equal to a default-constructed object.
  final loadedRoots = [home.RootConfig()];
  final report = home.TableFixedReport();
  final plan = tables.rootConfigFixedNewPlan();
  final numLoaded = tables.rootConfigFixedLoad(loadedRoots, 1, defaultBytes, defaultBytes.length, plan, report);

  check(numLoaded == 1, 'loaded one record from serialized default object');

  final loadedRoot = loadedRoots[0];
  final loadedBytes = Uint8List(tables.rootConfigFixedMeasure(1));
  tables.rootConfigFixedSave([loadedRoot], 1, loadedBytes);

  check(_bytesEqual(defaultBytes, loadedBytes), 'read/write of default object is consistent');

  if (_fails > 0) {
    exit(1);
  }
}
