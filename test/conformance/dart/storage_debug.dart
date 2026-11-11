// The storage checks in a debug build (docs/SPEC.md §5.1): Dart is an assert
// leg, so a count or a length past the storage it indexes is the programmer's
// error, and the cook writer's `assert` is how it says so. This run has
// asserts on, and each case must end in an AssertionError, which this driver
// answers as `refused`. A release build carries no such check.

import 'dart:io';
import 'dart:typed_data';

import 'StorageCook.dart';
import 'StoragereleaseFixed.dart';

bool refused(String name, void Function(Root value) set) {
  final value = Root();
  set(value);
  try {
    final ok = RootCook.cook(value, Uint8List(256), Endian.little);
    stdout.writeln('FAIL storage debug $name: cook answered $ok, no assert');
    return false;
  } on AssertionError {
    stdout.writeln('refused: $name');
    return true;
  } catch (e) {
    stdout.writeln('FAIL storage debug $name: ${e.runtimeType}, no assert');
    return false;
  }
}

void main() {
  var ok = true;
  // the declared bound is 8
  ok &= refused('count', (value) => value.itemsCount = 9);
  // string(16)
  ok &= refused('length', (value) => value.nameLength = 17);
  if (!ok) {
    exit(1);
  }
  stdout.writeln(
    'storage debug gate: a count or length past its storage hits the assert',
  );
}
