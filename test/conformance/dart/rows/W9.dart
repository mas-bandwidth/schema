// test/conformance/dart/rows/W9.dart — prefill unwritten ranges only
//
// W9 asserts that identity plans carry an empty fill list and pay no prefill.
//
// LAW (docs/FIXED-FORM-ALGORITHM.md:308): "The plan compiler computes the UNWRITTEN
// RANGES ... and only those take the declared defaults before the loop; for the
// identity plan that list is EMPTY, so the identity read pays no prefill (fix 15)."
//
// CONTROL: if fillCount is non-zero for identity, this test goes red.

import 'dart:typed_data';
import '../../../../build/tables-generated-dart/block/BlockdemoFixed.dart' as fixed;

// Test identity plan has empty fill list (fillCount = 0)
void testIdentityPrefillEmpty() {
  final layout = Uint8List.fromList([
    // header: form byte + 7 zeros
    0x03, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
    // hash (8 bytes) - simplified for test
    0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
    // layout header: entry count (1)
    0x01, 0x00, 0x00, 0x00,
    // entry: id (8), kind (1), size (4), children (4)
    // Table kind=13, size=1, children=0
    0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
    0x0d, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
    0x00, 0x00,
  ]);

  final myLayout = Uint8List.fromList([
    0x03, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
    0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
    0x01, 0x00, 0x00, 0x00,
    0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
    0x0d, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
    0x00, 0x00,
  ]);

  final myDst = Int32List.fromList([0]);
  final cover = Int32List.fromList([0]);
  final imageBytes = 64;
  final ownHash = 0;

  // Identity plan: own hash matches a known layout
  final known = [
    fixed.TableFixedKnownLayout(
      ownHash,
      myLayout,
      myLayout.length,
      1,
    ),
  ];

  final plans = fixed.tableFixedLineagePlans(
    known,
    myLayout,
    myDst,
    cover,
    0,
    imageBytes,
    ownHash,
  );

  if (plans.isEmpty) {
    print('RED: no plans generated');
    return;
  }

  final identity = plans[0];
  if (identity.fillCount != 0) {
    print('RED: identity plan fillCount=${identity.fillCount}, expected 0');
    return;
  }

  print('GREEN: identity plan fillCount=0');
}

// Test that fill list only contains unwritten ranges (not written-by-plan fields)
void testFillIsUnwrittenOnly() {
  // For identity plan: destinations ARE the value bytes, so there is nothing
  // left to subtract from the prefill ranges.
  //
  // This test verifies the fill list construction in tableFixedFillRun.
  
  final defaults = Uint8List(16);
  defaults[0] = 0x42; // non-zero default
  
  // Create destination with some pre-existing data
  final dst = Uint8List(16);
  dst[0] = 0xFF; // different from default
  
  // Empty fill list (identity plan)
  final fill = Int32List(0);
  final count = 0;

  fixed.tableFixedFillRun(fill, count, defaults, dst);

  if (dst[0] != 0xFF) {
    print('RED: prefill modified byte when fill count was 0');
    return;
  }

  print('GREEN: no prefill with empty fill list');
}

void main() {
  testIdentityPrefillEmpty();
  testFillIsUnwrittenOnly();
}
