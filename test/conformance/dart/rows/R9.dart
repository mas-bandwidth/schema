// R9 — a known hash with a different layout length or bytes → layout_malformed.
//
// The law (docs/FIXED-FORM-ALGORITHM.md:864): "The seven §1.1 malformations
// under a KNOWN hash all come back as one name, layout_malformed."
//
// This test constructs two minimal form-3 files against the Meta table from
// the graphdemo unit:
//   1. A file whose header hash is metaFixedHash (known) but whose layout
//      BYTES differ — the reader MUST refuse layout_malformed.
//   2. A file whose header hash is metaFixedHash (known) but whose layout
//      LENGTH differs — the reader MUST refuse layout_malformed.
//
// Exit 0 green / exit 1 red, one printed line per assertion.
// Run as: dart run test/conformance/dart/rows/R9.dart

import 'dart:io';
import 'dart:typed_data';

import '../../../../build/tables-generated-dart/pointers/GraphFixed.dart'
    show
        metaFixedHash,
        metaFixedLayout,
        metaFixedLayoutBytes,
        metaFixedLoad,
        metaFixedRecordBytes;
import '../../../../build/tables-generated-dart/pointers/GraphdemoFixed.dart'
    show Meta, TableFixedPlan, TableFixedRefusal, TableFixedReport;

int pass = 0;
int fail = 0;

void expect(String label, bool condition, String reason) {
  if (condition) {
    print('PASS $label');
    pass++;
  } else {
    print('FAIL $label: $reason');
    fail++;
  }
}

// Build a minimal form-3 file for the Meta table.
// [header (16 bytes)] [layout length (4 bytes)] [layout bytes] [records...]
//
// The header is: form byte (1), 7 reserved zeros (7), layout hash (8).
Uint8List buildForm3File({
  required int layoutHash,
  required Uint8List layout,
  int recordCount = 1,
}) {
  final headerBytes = 16;
  final layoutLenBytes = 4;
  final totalLayout = headerBytes + layoutLenBytes + layout.length;
  final total = totalLayout + recordCount * metaFixedRecordBytes;
  final bytes = Uint8List(total);
  final view = ByteData.sublistView(bytes);

  // form byte 3
  bytes[0] = 3;
  // bytes 1..7 reserved zero (already zero)
  // layout hash at offset 8 (little-endian u64)
  view.setUint64(8, layoutHash, Endian.little);
  // layout length at offset 16 (little-endian u32)
  view.setUint32(16, layout.length, Endian.little);
  // layout bytes at offset 20
  bytes.setRange(20, 20 + layout.length, layout);

  // records: each is 8-byte record hash (same as layout hash) + 16-byte body
  final recordStart = totalLayout;
  for (var r = 0; r < recordCount; r++) {
    final at = recordStart + r * metaFixedRecordBytes;
    view.setUint64(at, layoutHash, Endian.little); // record hash = layout hash
    // body is 16 zero bytes (Meta: build=u32, tag=string(8) both zero)
  }
  return bytes;
}

void main() {
  // ---- case 1: known hash, DIFFERENT layout BYTES (same length) ----
  final layoutBytesDiff = Uint8List.fromList(List<int>.generate(
    metaFixedLayoutBytes,
    (i) => i == 0 ? (metaFixedLayout[0] ^ 0xFF) : metaFixedLayout[i],
  ));
  final file1 = buildForm3File(
    layoutHash: metaFixedHash,
    layout: layoutBytesDiff,
  );
  final report1 = TableFixedReport();
  final plan1 = TableFixedPlan(4096, 16, 4096);
  final values1 = List<Meta>.filled(1, Meta());
  final result1 = metaFixedLoad(
    values1,
    1,
    file1,
    file1.length,
    plan1,
    report1,
  );
  expect(
    'known hash, different layout bytes → layout_malformed',
    result1 == -1 &&
        report1.refused == TableFixedRefusal.layoutMalformed,
    'result=$result1 refused=${TableFixedRefusal.name(report1.refused)}',
  );

  // ---- case 2: known hash, DIFFERENT layout LENGTH (one byte shorter) ----
  final layoutShort = Uint8List.fromList(
    metaFixedLayout.sublist(0, metaFixedLayoutBytes - 1),
  );
  final file2 = buildForm3File(
    layoutHash: metaFixedHash,
    layout: layoutShort,
  );
  final report2 = TableFixedReport();
  final plan2 = TableFixedPlan(4096, 16, 4096);
  final values2 = List<Meta>.filled(1, Meta());
  final result2 = metaFixedLoad(
    values2,
    1,
    file2,
    file2.length,
    plan2,
    report2,
  );
  expect(
    'known hash, different layout length → layout_malformed',
    result2 == -1 &&
        report2.refused == TableFixedRefusal.layoutMalformed,
    'result=$result2 refused=${TableFixedRefusal.name(report2.refused)}',
  );

  // ---- sanity: known hash, CORRECT layout bytes → opens ----
  final file3 = buildForm3File(
    layoutHash: metaFixedHash,
    layout: metaFixedLayout,
  );
  final report3 = TableFixedReport();
  final plan3 = TableFixedPlan(4096, 16, 4096);
  final values3 = List<Meta>.filled(1, Meta());
  final result3 = metaFixedLoad(
    values3,
    1,
    file3,
    file3.length,
    plan3,
    report3,
  );
  expect(
    'known hash, correct layout bytes → opens (sanity)',
    result3 == 1 && report3.refused == TableFixedRefusal.none,
    'result=$result3 refused=${TableFixedRefusal.name(report3.refused)}',
  );

  if (fail > 0) {
    print('$pass passed, $fail failed');
    exit(1);
  } else {
    print('$pass passed, $fail failed');
    exit(0);
  }
}
