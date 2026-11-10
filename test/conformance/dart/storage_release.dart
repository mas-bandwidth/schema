// The storage-bound refusal in a release build (docs/SPEC.md §5.1): a counted
// array whose count is 9 indexes past the storage it describes, and this run
// has asserts off, so a refusal here cannot be the debug assert.

import 'dart:io';
import 'dart:typed_data';

import 'StorageCook.dart';
import 'StoragereleaseFixed.dart';

void main() {
  final value = Root();
  value.itemsCount = 9; // the declared bound is 8

  if (RootCook.cook(value, Uint8List(256), Endian.little)) {
    stdout.writeln('FAIL storage release: cook wrote a count past its storage');
    exit(1);
  }

  stdout.writeln('storage release gate: a count past its storage is refused with asserts off');
}
