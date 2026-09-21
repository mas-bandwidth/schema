// THE DART LEG's F4 cell: layout_malformed, truncated (docs/FIXED-FORM-ALGORITHM.md:144).
//
// A standalone test for the law: `L := LE(4, b+16)`; if `20 + L > bytes`, `REFUSE layout_malformed`.
//
// This test constructs a minimal byte vector that triggers the condition:
// - form byte 3 (fixed form)
// - 7 reserved zero bytes
// - 8-byte hash
// - 4-byte layout length L
// - L bytes of layout
// - but the total file length is < 20 + L, making it truncated

import 'dart:io';
import 'dart:typed_data';

// Import the generated fixed form runtime
import '../../../../build/tables-generated-dart/examples/TabledemoFixed.dart';
import '../../../../build/tables-generated-dart/examples/TablesFixed.dart';

void main() {
  // The smallest layout is 4 bytes (just the entry count of 0)
  // So L = 4, and we need 20 + 4 = 24 bytes total
  // A truncated file has fewer than 24 bytes
  
  // Build a file with:
  // - form byte: 3
  // - 7 reserved zero bytes
  // - hash: 8 bytes (we'll use rootConfigFixedHash)
  // - layout length: 4 bytes (L = 4)
  // - layout: 4 bytes (entry count of 0)
  // Total should be 24 bytes, but we'll make it 23 bytes (truncated by 1)
  
  // Use rootConfigFixedHash as a known hash
  final hash = rootConfigFixedHash;
  
  // Build the header: form byte (3) + 7 zeros + hash (8 bytes) + layout length (4 bytes = 4)
  final header = Uint8List.fromList([
    3, // form byte
    0, 0, 0, 0, 0, 0, 0, // 7 reserved zeros
    ..._uint64ToBytes(hash), // hash at offset 8
    4, 0, 0, 0, // layout length L = 4 at offset 16
  ]);
  
  // The layout should be 4 bytes (entry count of 0)
  final layout = Uint8List.fromList([0, 0, 0, 0]);
  
  // A valid file would be: header (20 bytes) + layout (4 bytes) = 24 bytes
  // A truncated file has only 23 bytes (missing the last byte of layout)
  final truncatedFile = Uint8List.fromList([
    ...header,
    ...layout.sublist(0, 3), // only 3 bytes of layout instead of 4
  ]);
  
  // Now try to load it
  final report = TableFixedReport();
  final values = List<RootConfig>.filled(1, RootConfig());
  final plan = TableFixedPlan(100, 1000, 100);
  
  final count = rootConfigFixedLoad(values, 1, truncatedFile, truncatedFile.length, plan, report);
  
  // Should refuse with layoutMalformed
  if (report.refused == TableFixedRefusal.layoutMalformed) {
    stdout.writeln('GREEN: truncated file (20+L > bytes) refuses layout_malformed');
    exit(0);
  } else {
    stdout.writeln('RED: expected layoutMalformed, got ${TableFixedRefusal.name(report.refused)}');
    exit(1);
  }
}

// Helper to convert a 64-bit unsigned int to little-endian bytes
List<int> _uint64ToBytes(int value) {
  final bytes = Uint8List(8);
  final view = ByteData.sublistView(bytes);
  view.setUint64(0, value, Endian.little);
  return bytes;
}
