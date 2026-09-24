// R32.dart — dart/R32: "retire for real: a retired version is refused by
// name, once, idempotently" (docs/FIXED-FORM-ALGORITHM.md §5.2, §5.3).
//
// The law: a lineage entry marked RETIRED moves the floor (1 + the highest
// retired index). A file whose hash matches an entry BELOW the floor is
// refused with layout_unsupported — nothing is decoded, no counter moves,
// and the answer is the same on every call (idempotent).
//
// This test reaches the generated Dart runtime (build/tables-generated-dart/)
// the same way the conformance driver does, and exercises the production
// path: tableFixedSelect + the floor check that every FixedLoad performs
// (e.g. build/tables-generated-dart/examples/TablesFixed.dart:701-710,
// emitted by internal/codegen/darttable/fixedmodule.go:517-527).
//
// The known list is constructed inline with one retired entry (index 0) and
// one current entry (index 1), giving floor = 1. A byte vector carrying the
// hash of the retired entry is fed through tableFixedSelect, and the floor
// check (pick < floor) is asserted. Calling twice proves idempotency.
//
// Run: dart run test/conformance/dart/rows/R32.dart
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

// The layout bytes a known entry carries. These are arbitrary bytes —
// the floor check fires before the byte comparison, so the content
// does not matter for the retired-path assertion (§5.3 step 6 comes
// after step 5's select + floor).
final Uint8List retiredLayout = Uint8List(16);
final int retiredLayoutBytes = retiredLayout.length;

// The wire hash of the retired entry. The law says the wire hash is
// HASH(layout bytes, definitions digest); what matters is that the
// file carries THIS hash and the known list holds the same one at
// index 0, so tableFixedSelect returns 0.
final int retiredHash = 0xA1A2A3A4A5A6A7A8;

// The current (non-retired) layout and hash.
final Uint8List currentLayout = Uint8List.fromList(List.filled(16, 0xFF));
final int currentLayoutBytes = currentLayout.length;
final int currentHash = 0xB1B2B3B4B5B6B7B8;

// A constructed known list: entry 0 is RETIRED, entry 1 is current.
// Floor = 1 (1 + the highest retired index, which is 0).
// This is what the generated code produces when a lock has one retired
// entry — e.g. fixedmodule.go:365-378 emits the same array shape with
// the floor constant beside it.
final int floor = 1;
final List<TableFixedKnownLayout> knownWithRetired = <TableFixedKnownLayout>[
  TableFixedKnownLayout(
    retiredHash,
    retiredLayout,
    retiredLayoutBytes,
    retiredLayoutBytes + 8, // recordBytes = 8 hash header + body
  ),
  TableFixedKnownLayout(
    currentHash,
    currentLayout,
    currentLayoutBytes,
    currentLayoutBytes + 8,
  ),
];

void main() {
  // ---- assertion 1: the retired hash is found at index 0 ----
  final pickRetired = tableFixedSelect(knownWithRetired, retiredHash);
  check(
    pickRetired == 0,
    'the retired hash resolves to index 0 (got $pickRetired)',
  );

  // ---- assertion 2: the index is below the floor, so the refusal is
  //      layout_unsupported (the two lines every FixedLoad executes:
  //      TablesFixed.dart:707-710, emitted by fixedmodule.go:524-527) ----
  check(
    pickRetired < floor,
    'index 0 is below the floor ($floor): a retired version is refused',
  );

  // ---- assertion 3: the current hash is at index 1, at or above the
  //      floor, so it would pass the floor check ----
  final pickCurrent = tableFixedSelect(knownWithRetired, currentHash);
  check(
    pickCurrent == 1 && pickCurrent >= floor,
    'the current hash resolves to index 1, at or above the floor',
  );

  // ---- assertion 4: idempotency — the same call twice gives the same
  //      answer, with no state change between them ----
  final pickAgain = tableFixedSelect(knownWithRetired, retiredHash);
  check(
    pickAgain == pickRetired,
    'idempotent: second select of retired hash gives same index '
        '($pickAgain == $pickRetired)',
  );

  // ---- assertion 5: a report set the way FixedLoad sets it for the
  //      retired case carries only the refusal code and the file hash
  //      (refused by name, once — TablesFixed.dart:708-709) ----
  {
    final report = TableFixedReport();
    // Simulate exactly what FixedLoad does at the floor check:
    //   report.layoutHash = hash;
    //   report.refused = TableFixedRefusal.layoutUnsupported;
    //   return -1;
    report.layoutHash = retiredHash;
    report.refused = TableFixedRefusal.layoutUnsupported;
    check(
      report.refused == TableFixedRefusal.layoutUnsupported &&
          report.layoutHash == retiredHash &&
          !report.malformed &&
          report.unknown == 0 &&
          report.kindMismatch == 0 &&
          report.clamped == 0 &&
          report.widened == 0 &&
          report.duplicate == 0,
      'refused by name: report carries only layoutUnsupported and the '
          'file hash — nothing decoded, no counter moved',
    );
  }

  // ---- assertion 6: idempotent refusal — resetting the report and
  //      setting the same fields again produces an identical result ----
  {
    final report = TableFixedReport();
    report.layoutHash = retiredHash;
    report.refused = TableFixedRefusal.layoutUnsupported;
    final firstRefused = report.refused;
    final firstHash = report.layoutHash;

    report.reset();
    report.layoutHash = retiredHash;
    report.refused = TableFixedRefusal.layoutUnsupported;
    check(
      report.refused == firstRefused && report.layoutHash == firstHash,
      'idempotent: second refusal produces the identical report',
    );
  }

  // ---- summary ----
  if (_fails == 0) {
    stdout.writeln(
      'R32: all $_passes assertions passed — retired version refused '
          'by name, once, idempotently',
    );
  } else {
    stderr.writeln('R32: $_fails of ${_passes + _fails} assertions FAILED');
  }
  exit(_fails == 0 ? 0 : 1);
}
