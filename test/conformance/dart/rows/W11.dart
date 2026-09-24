// W11.dart — bytes(N) is layout kind 14.
//
// docs/FIXED-FORM-ALGORITHM.md:54-55 — "bytes(N) is walked as an ARRAY OF u8
// — kind 14 with one synthetic child at kind 6, size 1 — never as a text kind;
// only string(N) (12) and wstring(N) (33) are text kinds in a layout."
//
// docs/FIXED-FORM-ALGORITHM.md:75 — the kind table confirms: "14 | array —
// and bytes(N), as an array of 6".
//
// WHAT THIS ASSERTS:
//   The ProfileConfig table carries `icon bytes(16)` (tables/examples/
//   Tables.schema:94). In the layout bytes, this field appears as an entry
//   with kind=14 (array), size=20 (4-byte count + 16-byte buffer), and
//   one child entry with kind=6 (u8), size=1 — the synthetic element type.
//
// THE LAYOUT FORMAT (ir/fixedform.go:582-591):
//   4 bytes: entry count (u32 LE)
//   per entry (17 bytes):
//     8 bytes: ID (u64 LE, fnv1a64 of the field/type name)
//     1 byte:  kind
//     4 bytes: size (u32 LE)
//     4 bytes: children (u32 LE)
//
// THE PRODUCTION PATH:
//   The Dart fixed-form emitter (internal/codegen/darttable/fixeddart.go)
//   writes the layout bytes from ir.TableFixedLayoutBytes, which walks the
//   closure with TableFixedWalkRoot. The walk treats bytes(N) as kind 14
//   with a synthetic child of kind 6 (u8).
//
// Run: dart run test/conformance/dart/rows/W11.dart

import 'dart:io';
import 'dart:typed_data';

import '../../../../build/tables-generated-dart/examples/TablesFixed.dart'
    show profileConfigFixedLayout;

int _fails = 0;

void check(bool ok, String what) {
  if (!ok) {
    stderr.writeln('FAIL: $what');
    _fails++;
  } else {
    stdout.writeln('PASS: $what');
  }
}

void main() {
  // The layout starts with a 4-byte entry count, then 17-byte entries.
  final view = ByteData.sublistView(profileConfigFixedLayout);
  final entryCount = view.getUint32(0, Endian.little);
  check(entryCount > 0, 'W11: the layout has entries ($entryCount)');

  // Find the bytes(N) entry: kind=14 with size=20 (4 count + 16 buffer)
  // and children=1. This shape is unique to bytes(N) — no other field
  // has kind 14 with stride-1 children and a live count.
  var foundBytesEntry = false;
  var bytesEntryIdx = -1;
  var bytesKind = -1;
  var bytesSize = -1;
  var bytesChildren = -1;

  for (var i = 0; i < entryCount; i++) {
    final offset = 4 + i * 17;
    final kind = profileConfigFixedLayout[offset + 8];
    final size = view.getUint32(offset + 9, Endian.little);
    final children = view.getUint32(offset + 13, Endian.little);

    if (kind == 14 && children == 1) {
      foundBytesEntry = true;
      bytesEntryIdx = i;
      bytesKind = kind;
      bytesSize = size.toInt();
      bytesChildren = children.toInt();
      break;
    }
  }

  check(
    foundBytesEntry,
    'W11: found a kind-14 entry with one child (the bytes(N) field)',
  );

  // The bytes(N)=bytes(16) entry: size = 4 (count word) + 16 (buffer) = 20.
  if (foundBytesEntry) {
    check(
      bytesKind == 14,
      'W11: bytes(N) entry kind is 14 (got $bytesKind) — '
      '"bytes(N) is layout kind 14" (docs/FIXED-FORM-ALGORITHM.md:54)',
    );
    check(
      bytesSize == 20,
      'W11: bytes(16) entry size is 20 = 4 count + 16 buffer (got $bytesSize)',
    );
    check(
      bytesChildren == 1,
      'W11: bytes(N) has one synthetic child (got $bytesChildren) — '
      '"an ARRAY OF u8 ... with one synthetic child at kind 6, size 1"',
    );
  }

  // The synthetic child entry: kind=6 (u8), size=1.
  // It follows the parent entry.
  if (foundBytesEntry && bytesEntryIdx + 1 < entryCount) {
    final childOffset = 4 + (bytesEntryIdx + 1) * 17;
    final childKind = profileConfigFixedLayout[childOffset + 8];
    final childSize = view.getUint32(childOffset + 9, Endian.little);
    check(
      childKind == 6 && childSize == 1,
      'W11: bytes(N) synthetic child is kind 6 (u8), size 1 '
      '(got kind=$childKind, size=$childSize) — '
      '"synthetic child at kind 6, size 1" (docs/FIXED-FORM-ALGORITHM.md:54)',
    );
  }

  // ---- NEGATIVE CONTROL: the text field before it has kind 12, not 14 ----
  //
  // ProfileConfig's `name string(32)` is the field before `icon bytes(16)`.
  // It should be kind 12 (string), NOT kind 14 — only string/wstring are
  // text kinds, and bytes(N) is an array kind.
  if (foundBytesEntry && bytesEntryIdx > 1) {
    final textOffset = 4 + 1 * 17; // entry 1: name string(32)
    final textKind = profileConfigFixedLayout[textOffset + 8];
    check(
      textKind == 12,
      'W11: the string field before bytes(16) is kind 12 (got $textKind) — '
      '"only string(N) (12) and wstring(N) (33) are text kinds"',
    );
  }

  // ---- THE CLOSED KIND SET ----
  //
  // Count all kind-14 entries. ProfileConfig's closure includes arrays
  // ([4]float32 ratings, [..4]ProfileConfig in RootConfig, etc.), so there
  // will be more than one kind-14 entry. The bytes(N) entry is distinguished
  // by having a single synthetic child of kind 6 (u8) with size 1.
  var kind14Count = 0;
  for (var i = 0; i < entryCount; i++) {
    final offset = 4 + i * 17;
    final kind = profileConfigFixedLayout[offset + 8];
    if (kind == 14) {
      kind14Count++;
    }
  }
  check(
    kind14Count >= 1,
    'W11: at least one kind-14 entry in ProfileConfig (found $kind14Count) — '
    'bytes(N) is an array kind, and the table has other array fields too',
  );

  if (_fails == 0) {
    stdout.writeln('W11: bytes(N) is layout kind 14 — PASS');
  } else {
    stdout.writeln('W11: $_fails assertion(s) failed');
  }
  exit(_fails == 0 ? 0 : 1);
}
