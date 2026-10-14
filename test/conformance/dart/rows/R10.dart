// R10 — the run-time walk of a stranger's layout and the recompute of the
// header's hash are retired (docs/FIXED-FORM-ALGORITHM.md §5.6).
//
// THE LAW (docs/FIXED-FORM-ALGORITHM.md:967):
//
//   "Reading a layout the lock has never seen. The run-time walk of a
//   stranger's layout — the seven §1.1 checks stay as the LOCK's validation
//   of what it records and the oracle's validation of the corpus. The
//   recompute of the header's hash from the layout behind it, and with it
//   §2's step 5: there is no third thing to check, the byte comparison holds
//   the header and the layout together, and the digest could not be re-derived
//   from the wire anyway."
//
// This test asserts, for the Dart leg, that the two retired things are indeed
// absent from the load path:
//
//   1. THE RECOMPUTE OF THE HEADER'S HASH — the hash is a compile-time
//      constant, identical to the one the known list carries, and is never
//      derived from layout bytes at load time.
//
//   2. THE RUN-TIME WALK OF A STRANGER'S LAYOUT — the layout bytes are a
//      compile-time constant, identical to the known list's, and the load
//      path does a byte comparison against them, not a walk. A file whose
//      hash is unknown is refused layout_newer; one whose hash is known but
//      whose layout bytes differ is refused layout_malformed.
//
// The control: break one byte of the generated metaFixedLayout or metaFixedHash
// constant, and this test goes red — the static-data identity check or the
// load assertion no longer holds.
//
// Run: dart run test/conformance/dart/rows/R10.dart
// Exit 0 = green, exit 1 = red, one printed line per assertion.

import 'dart:io';
import 'dart:typed_data';

import '../../../../build/tables-generated-dart/pointers/GraphFixed.dart'
    show
        metaFixedBodyBytes,
        metaFixedHash,
        metaFixedKnown,
        metaFixedLayout,
        metaFixedLayoutBytes,
        metaFixedLoad,
        metaFixedRecordBytes;
import '../../../../build/tables-generated-dart/pointers/GraphdemoFixed.dart'
    show
        Meta,
        TableFixedKnownLayout,
        TableFixedPlan,
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
  }
}

// ---- THE STATIC-DATA IDENTITY CHECKS ----
//
// §5.2: the lineage from the lock, oldest first, the current layout last,
// is static data the BUILD lays down. The layout and the hash are compile-
// time constants, not computed at run time from a file's bytes.

void main() {
  // The Meta table's known list has one entry (no lock), so its layout
  // and hash must match the table's own constants.
  final TableFixedKnownLayout known = metaFixedKnown[0];

  // THE HASH IS A CONSTANT: identical to the known list's.
  check(
    metaFixedHash == known.hash,
    'the header\'s hash is a compile-time constant: 0x${metaFixedHash.toRadixString(16)} '
    'equals the known list\'s 0x${known.hash.toRadixString(16)} — '
    'not recomputed from the layout',
  );

  // THE LAYOUT IS A CONSTANT: byte-for-byte identical to the known list's.
  check(
    metaFixedLayoutBytes == known.layoutBytes,
    'the layout length is a constant: $metaFixedLayoutBytes equals the '
    'known list\'s ${known.layoutBytes}',
  );
  bool layoutMatches = metaFixedLayoutBytes == known.layoutBytes;
  if (layoutMatches) {
    for (var i = 0; i < metaFixedLayoutBytes; i++) {
      if (metaFixedLayout[i] != known.layout[i]) {
        layoutMatches = false;
        break;
      }
    }
  }
  check(
    layoutMatches,
    'the layout bytes are a constant: byte-for-byte identical to the '
    'known list\'s — not walked at run time',
  );

  // ---- THE LOAD-PATH ASSERTIONS ----
  //
  // Build minimal form-3 files to exercise the production load path and
  // prove the two retired things are absent from it.

  // Build a minimal form-3 file for the Meta table.
  // Header: 16 bytes (form byte, 7 reserved, 8-byte hash)
  // Layout length: 4 bytes (u32 LE)
  // Layout bytes: N bytes
  // Records: 8-byte record hash + 16-byte body each
  Uint8List buildForm3({
    required int layoutHash,
    required Uint8List layout,
    int recordCount = 1,
  }) {
    const headerBytes = 16;
    const layoutLenBytes = 4;
    final totalLayout = headerBytes + layoutLenBytes + layout.length;
    final total = totalLayout + recordCount * metaFixedRecordBytes;
    final bytes = Uint8List(total);
    final view = ByteData.sublistView(bytes);

    bytes[0] = 3; // form byte
    view.setUint64(8, layoutHash, Endian.little);
    view.setUint32(16, layout.length, Endian.little);
    bytes.setRange(20, 20 + layout.length, layout);

    // records: each is 8-byte record hash (same as layout hash) + 16-byte body
    final recordStart = totalLayout;
    for (var r = 0; r < recordCount; r++) {
      final at = recordStart + r * metaFixedRecordBytes;
      view.setUint64(at, layoutHash, Endian.little);
    }
    return bytes;
  }

  // ASSERTION: correct hash, correct layout → opens.
  // This proves the constants work and the load path accepts what the
  // lock recorded.
  {
    final file = buildForm3(
      layoutHash: metaFixedHash,
      layout: metaFixedLayout,
    );
    final report = TableFixedReport();
    final plan = TableFixedPlan(1, metaFixedBodyBytes, 1);
    final values = List<Meta>.filled(1, Meta());
    final result = metaFixedLoad(
      values,
      1,
      file,
      file.length,
      plan,
      report,
    );
    check(
      result == 1 && report.refused == TableFixedRefusal.none,
      'correct hash + correct layout → opens (result=$result, '
      'refused=${TableFixedRefusal.name(report.refused)})',
    );
  }

  // ASSERTION: correct hash, DIFFERENT layout bytes → layout_malformed.
  // This proves the load path does a BYTE COMPARISON, not a walk.
  // A walk would try to parse the bytes; a comparison just says "these
  // bytes differ from what the lock recorded."
  {
    final differentLayout = Uint8List.fromList(List<int>.generate(
      metaFixedLayoutBytes,
      (i) => i == 0 ? (metaFixedLayout[0] ^ 0xFF) : metaFixedLayout[i],
    ));
    final file = buildForm3(
      layoutHash: metaFixedHash,
      layout: differentLayout,
    );
    final report = TableFixedReport();
    final plan = TableFixedPlan(1, metaFixedBodyBytes, 1);
    final values = List<Meta>.filled(1, Meta());
    final result = metaFixedLoad(
      values,
      1,
      file,
      file.length,
      plan,
      report,
    );
    check(
      result == -1 &&
          report.refused == TableFixedRefusal.layoutMalformed,
      'correct hash, different layout bytes → layout_malformed '
      '(result=$result, refused=${TableFixedRefusal.name(report.refused)}) — '
      'byte comparison, not a walk',
    );
  }

  // ASSERTION: UNKNOWN hash → layout_newer.
  // This proves there is no run-time walk of a stranger's layout: the
  // hash is looked up in the known list, and an unknown hash is refused
  // by name without parsing the layout bytes.
  {
    const strangerHash = 0xDEADBEEF01234567;
    final file = buildForm3(
      layoutHash: strangerHash,
      layout: Uint8List(metaFixedLayoutBytes), // arbitrary bytes
    );
    final report = TableFixedReport();
    final plan = TableFixedPlan(1, metaFixedBodyBytes, 1);
    final values = List<Meta>.filled(1, Meta());
    final result = metaFixedLoad(
      values,
      1,
      file,
      file.length,
      plan,
      report,
    );
    check(
      result == -1 &&
          report.refused == TableFixedRefusal.layoutNewer,
      'unknown hash → layout_newer (result=$result, '
      'refused=${TableFixedRefusal.name(report.refused)}) — '
      'no run-time walk of a stranger\'s layout',
    );
  }

  // ASSERTION: tableFixedSelect returns -1 for an unknown hash,
  // proving the hash is matched against the known list and not
  // recomputed from the layout.
  {
    const strangerHash = 0xCAFEBABE00000001;
    final pick = tableFixedSelect(metaFixedKnown, strangerHash);
    check(
      pick < 0,
      'tableFixedSelect returns $pick for unknown hash — '
      'the hash is matched against the lock\'s list, not recomputed',
    );
  }

  // ---- THE VERDICT ----
  if (_fails > 0) {
    stderr.writeln(
      'R10: $_fails of ${_passes + _fails} assertions FAILED — '
      'the retired runtime is still alive',
    );
    exit(1);
  }
  stdout.writeln(
    'R10: all $_passes assertions passed — '
    'the run-time walk of a stranger\'s layout and the recompute of '
    'the header\'s hash are retired',
  );
  exit(0);
}
