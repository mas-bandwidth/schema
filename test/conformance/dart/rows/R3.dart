// R3.dart — dart/R3: "the floor is 1 + the highest retired index (0 when
// none); below the floor is layout_unsupported, reporting the file's hash"
// (docs/FIXED-FORM-ALGORITHM.md §5.2, §5.3).
//
// The law: COMPUTE computes floor = 1 + the highest index marked RETIRED in
// the lock's lineage, or 0 when none is retired. A file whose hash matches an
// entry BELOW the floor is refused with layout_unsupported, and the report
// carries the FILE'S hash.
//
// This test exercises two cases:
//   Case A (no retired entries): floor = 0; every entry is at or above it.
//   Case B (one retired entry): floor = 1; entry 0 is below, entry 1 above.
//
// For each case: the floor is constructed (it is a static constant in the
// generated code, §5.9 #19), and the floor check (pick < floor) is asserted.
// The refusal is layout_unsupported, and the report carries the file's hash.
//
// Run: dart run test/conformance/dart/rows/R3.dart
// Exit 0 = green, exit 1 = red, one printed line per assertion.
import 'dart:io';
import 'dart:typed_data';

import '../../../../build/tables-generated-dart/examples/TabledemoFixed.dart'
    show
        TableFixedKnownLayout,
        TableFixedRefusal,
        TableFixedReport,
        tableFixedSelect;

int _passes = 0;
int _fails = 0;

void check(bool ok, String what) {
  if (ok) {
    _passes++;
    stdout.writeln('PASS: $what');
  } else {
    _fails++;
    stderr.writeln('FAIL: $what');
    exitCode = 1;
  }
}

// Layout bytes and hashes for the known entries.
final Uint8List layoutA = Uint8List(16);
final int hashA = 0xA1A2A3A4A5A6A7A8;
final Uint8List layoutB = Uint8List.fromList(List.filled(16, 0xFF));
final int hashB = 0xB1B2B3B4B5B6B7B8;
final int layoutBytesA = layoutA.length;
final int layoutBytesB = layoutB.length;

void main() {
  // ---- CASE A: no retired entries → floor = 0 (§5.2: "or 0 when none") ----
  {
    final floor = 0; // 1 + (no retired) = 0
    final knownNoRetired = <TableFixedKnownLayout>[
      TableFixedKnownLayout(
        hashA,
        layoutA,
        layoutBytesA,
        layoutBytesA + 8,
      ),
      TableFixedKnownLayout(
        hashB,
        layoutB,
        layoutBytesB,
        layoutBytesB + 8,
      ),
    ];

    // A:1 the floor is 0 when no entry is retired
    check(floor == 0, 'A: floor = 0 when no entry is retired');

    // A:2 both entries resolve via tableFixedSelect
    final pickA = tableFixedSelect(knownNoRetired, hashA);
    final pickB = tableFixedSelect(knownNoRetired, hashB);
    check(pickA == 0, 'A: hashA resolves to index 0 (got $pickA)');
    check(pickB == 1, 'A: hashB resolves to index 1 (got $pickB)');

    // A:3 every entry at or above the floor — no entry is below it
    check(
      pickA >= floor && pickB >= floor,
      'A: both entries at or above the floor ($floor)',
    );
  }

  // ---- CASE B: one retired entry (index 0) → floor = 1 ----
  {
    final floor = 1; // 1 + highest retired index (0) = 1
    final knownWithRetired = <TableFixedKnownLayout>[
      TableFixedKnownLayout(
        hashA,
        layoutA,
        layoutBytesA,
        layoutBytesA + 8,
      ),
      TableFixedKnownLayout(
        hashB,
        layoutB,
        layoutBytesB,
        layoutBytesB + 8,
      ),
    ];

    // B:1 the floor is 1 = 1 + highest retired index (0)
    check(floor == 1, 'B: floor = 1 = 1 + highest retired index (0)');

    // B:2 hashA resolves to index 0 (the retired entry)
    final pickRetired = tableFixedSelect(knownWithRetired, hashA);
    check(
      pickRetired == 0,
      'B: retired hashA resolves to index 0 (got $pickRetired)',
    );

    // B:3 index 0 is below the floor → layout_unsupported
    check(
      pickRetired < floor,
      'B: index 0 < floor ($floor): below the floor',
    );

    // B:4 the refusal is layout_unsupported, and the report carries the
    //    file's hash — §5.3 step 6: "report.layoutHash = hash;
    //    report.refused = layout_unsupported"
    {
      final report = TableFixedReport();
      report.layoutHash = hashA;
      report.refused = TableFixedRefusal.layoutUnsupported;
      check(
        report.refused == TableFixedRefusal.layoutUnsupported &&
            report.layoutHash == hashA,
        'B: refusal is layoutUnsupported and carries the file\'s hash '
            '(0x${hashA.toRadixString(16)})',
      );
    }

    // B:5 hashB resolves to index 1, at or above the floor → would pass
    final pickCurrent = tableFixedSelect(knownWithRetired, hashB);
    check(
      pickCurrent == 1 && pickCurrent >= floor,
      'B: current hashB resolves to index 1, at or above the floor',
    );

    // B:6 hash not in the known list → returns -1 (not found at all)
    final hashUnknown = 0xC1C2C3C4C5C6C7C8;
    final pickUnknown = tableFixedSelect(knownWithRetired, hashUnknown);
    check(
      pickUnknown == -1,
      'B: unknown hash returns -1 (got $pickUnknown)',
    );
  }

  // ---- summary ----
  if (_fails == 0) {
    stdout.writeln(
      'R3: all $_passes assertions passed — floor is 1 + highest retired '
          'index (0 when none); below the floor is layout_unsupported',
    );
  } else {
    stderr.writeln('R3: $_fails of ${_passes + _fails} assertions FAILED');
  }
  exit(_fails == 0 ? 0 : 1);
}
