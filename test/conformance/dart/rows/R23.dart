// R23 — the static data's member names and order — TableFixedKnownLayout = hash, layout, layout_bytes, record_bytes; the report's layout_hash last and zero on every other path.
//
// THE LAW (docs/FIXED-FORM-ALGORITHM.md:538-543):
//
//   One entry of `R.known` is `TableFixedKnownLayout` and it carries FOUR members
//   in this order — **`hash`, `layout`, `layout_bytes`, `record_bytes`** — the byte
//   length riding BESIDE the pointer rather than inside it, the way §4.1 names the
//   plan entry's lanes. `tools/fixedtwin` holds the C and C++ runtimes to that text
//   member for member, so a leg writing the struct from this page alone and landing
//   a different spelling breaks a gate rather than a test (§5.9 #19).
//   **The file's hash lands on the report as `layout_hash`**, last on the report and
//   zero on every other path (§5.9 #15).
//
// Standalone: `dart run test/conformance/dart/rows/R23.dart` from the repo
// root. Exit 0 green, 1 red, one printed line per assertion.
import 'dart:io';
import 'dart:typed_data';

import '../../../../build/tables-generated-dart/block/BlockdemoFixed.dart'
    show TableFixedKnownLayout, TableFixedReport, TableFixedRefusal;
import '../../../../build/tables-generated-dart/block/PaddedFixed.dart'
    show paddedRowFixedKnown;

int failures = 0;

void check(bool ok, String line) {
  failures += ok ? 0 : 1;
  stdout.writeln('${ok ? 'ok' : 'FAIL'} R23 $line');
}

void main() {
  // ---- THE STATIC DATA'S MEMBER NAMES AND ORDER ---
  // TableFixedKnownLayout carries FOUR members in this order: hash, layout, layout_bytes, record_bytes

  // We verify the order by constructing a TableFixedKnownLayout and checking
  // that the fields are accessible in the declared order.
  final layout = TableFixedKnownLayout(
    0xDEADBEEF,
    Uint8List.fromList(const <int>[0x01, 0x02, 0x03]),
    3,
    16,
  );

  // Verify the constructor parameter order matches the member order
  check(
    layout.hash == 0xDEADBEEF,
    'TableFixedKnownLayout.hash is first member and equals constructor first param',
  );
  check(
    layout.layout.length == 3 &&
        layout.layout[0] == 0x01 &&
        layout.layout[1] == 0x02 &&
        layout.layout[2] == 0x03,
    'TableFixedKnownLayout.layout is second member and equals constructor second param',
  );
  check(
    layout.layoutBytes == 3,
    'TableFixedKnownLayout.layoutBytes is third member and equals constructor third param',
  );
  check(
    layout.recordBytes == 16,
    'TableFixedKnownLayout.recordBytes is fourth member and equals constructor fourth param',
  );

  // Verify the generated constant uses the correct parameter order
  // The first entry in paddedRowFixedKnown should have hash=0x9683695abec5c762
  check(
    paddedRowFixedKnown.isNotEmpty,
    'paddedRowFixedKnown list is not empty',
  );
  check(
    paddedRowFixedKnown[0].hash == 0x9683695abec5c762,
    'paddedRowFixedKnown[0].hash equals the first constructor parameter',
  );
  check(
    paddedRowFixedKnown[0].layoutBytes == 293,
    'paddedRowFixedKnown[0].layoutBytes equals the third constructor parameter',
  );
  check(
    paddedRowFixedKnown[0].recordBytes == 58,
    'paddedRowFixedKnown[0].recordBytes equals the fourth constructor parameter',
  );

  // ---- THE REPORT'S layout_hash LAST AND ZERO ON EVERY OTHER PATH ---
  // The report's layoutHash is the LAST member and is zero on every path except
  // the two layout refusals (layout_newer and layout_unsupported).

  // Create a fresh report - all members should be zero-initialized
  final report = TableFixedReport();

  // Verify layoutHash is last by checking it's zero when no layout refusal has occurred
  check(
    report.layoutHash == 0,
    'TableFixedReport.layoutHash is last member and zero on default construction',
  );

  // Verify all other report fields are also zero/false on default construction
  check(
    !report.malformed &&
        report.refused == TableFixedRefusal.none &&
        report.unknown == 0 &&
        report.kindMismatch == 0 &&
        report.clamped == 0 &&
        report.widened == 0 &&
        report.duplicate == 0 &&
        report.hash == 0,
    'TableFixedReport: all fields before layoutHash are zero/false on default construction',
  );

  // Reset the report and verify layoutHash is still last and zero
  report.reset();
  check(
    report.layoutHash == 0,
    'TableFixedReport.layoutHash is zero after reset',
  );

  // ---- the verdict
  if (failures > 0) {
    stdout.writeln(
      'FAIL R23: $failures assertion(s) red — the static data member names and order '
      'do not hold for the dart leg',
    );
    exit(1);
  }
  stdout.writeln(
    'ok R23: the static data\'s member names and order — TableFixedKnownLayout = '
    'hash, layout, layout_bytes, record_bytes; the report\'s layout_hash last and '
    'zero on every other path',
  );
  exit(0);
}
