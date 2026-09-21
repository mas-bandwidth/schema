// W12: hash includes the 4-byte count (docs/FIXED-FORM-ALGORITHM.md §5.2).
//
// "The layout hash is fnv1a64 over the layout's bytes as written, the 4-byte
// count included." This test takes the LAYOUT BYTES from two generated tables,
// hashes them with fnv1a64 written out LONGHAND — never borrowed from the code
// under test — and:
//
//   1. asserts fnv1a64(layout) matches the runtime's own TableFixedLayout.hashOf
//      (the function the code under test calls), proving they agree;
//   2. asserts fnv1a64(layout[4..]) does NOT match, proving the count word is
//      part of the input — "the count is included" is asserted by nothing if
//      the negative never fires;
//   3. asserts the two layouts have distinct hashes, so the case is not one
//      lucky constant.
//
//   dart run test/conformance/dart/rows/W12.dart

import 'dart:io';
import 'dart:typed_data';

import '../../../../build/dart-fixed/fx1/FX1Fixed.dart' as fx1;
import '../../../../build/dart-fixed/fx1/Tblfx1Fixed.dart' as fx1home;
import '../../../../build/dart-fixed/fx2/FX2Fixed.dart' as fx2;
import '../../../../build/dart-fixed/fx2/Tblfx2Fixed.dart' as fx2home;

var failed = false;

void check(bool ok, String what) {
  if (!ok) {
    print('FAILED: $what');
    failed = true;
  }
}

// fnv1a64 is §5's hash written out LONGHAND in the fixture: a driver that
// hashed with the code it is checking "would agree with whatever that code
// happened to do". Here it is the oracle, not an echo.
int fnv1a64(Uint8List bytes, int at, int length) {
  var h = 0xcbf29ce484222325;
  for (var i = 0; i < length; i++) {
    h = (h ^ bytes[at + i]) * 0x100000001b3;
  }
  return h;
}

void main(List<String> args) {
  final cases = [
    {
      'name': 'FxRoot',
      'layout': fx1.fxRootFixedLayout,
      'runtimeHashOf': fx1home.TableFixedLayout.hashOf(
        fx1.fxRootFixedLayout,
        0,
        fx1.fxRootFixedLayout.length,
      ),
    },
    {
      'name': 'Fx2Root',
      'layout': fx2.fxRootFixedLayout,
      'runtimeHashOf': fx1home.TableFixedLayout.hashOf(
        fx2.fxRootFixedLayout,
        0,
        fx2.fxRootFixedLayout.length,
      ),
    },
  ];

  for (final tc in cases) {
    final name = tc['name'] as String;
    final layout = tc['layout'] as Uint8List;
    final runtimeHash = tc['runtimeHashOf'] as int;

    // The layout's own first four bytes are its u32 entry count.
    final entryCount =
        ByteData.sublistView(layout).getUint32(0, Endian.little);
    check(
      entryCount == (layout.length - 4) ~/ 17,
      '$name: the layout opens with its 4-byte entry count: '
      'got $entryCount, want ${(layout.length - 4) ~/ 17}',
    );

    // THE RULE, BY NAME: fnv1a64 over the layout's bytes as written, the 4-byte
    // count included, must agree with the runtime's own hashOf.
    final oracleHash = fnv1a64(layout, 0, layout.length);
    check(
      oracleHash == runtimeHash,
      '$name: fnv1a64 over the layout\'s bytes as written (the 4-byte count '
      'included) must match the runtime hashOf: oracle 0x${oracleHash.toRadixString(16).padLeft(16, '0')}, '
      'runtime 0x${runtimeHash.toRadixString(16).padLeft(16, '0')}',
    );

    // AND THE COUNT IS IN THE INPUT: dropping it must move the number, or
    // "the count is included" is asserted by nothing.
    final dropped = fnv1a64(layout, 4, layout.length - 4);
    check(
      dropped != runtimeHash,
      '$name: dropping the 4-byte count leaves the hash at '
      '0x${dropped.toRadixString(16).padLeft(16, '0')}, '
      'so the count is not in the input',
    );
  }

  // Two distinct layouts must have distinct hashes.
  check(
    cases[0]['runtimeHashOf'] != cases[1]['runtimeHashOf'],
    'two distinct layouts share the hash; the case is one lucky constant',
  );

  if (failed) {
    print('W12: FAIL');
    exit(1);
  }
  print(
    'W12: hash includes the 4-byte count — '
    '${cases.length} layouts verified, negative control biting',
  );
}
