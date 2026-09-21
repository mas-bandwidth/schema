// THE DART CELL C13 — PRESENT FLAG BYTE != 0 (schema matrix #876).
//
// The law (docs/FIXED-FORM-ALGORITHM.md §4.5, line 337):
//   "a `bool` or a PRESENT FLAG | lands as **`byte != 0`**, normalised to the
//   language's own true. `0x02` is not a bool a reader stores verbatim (fix 2)"
//
// The production path is the generated block reader, exactly the one the
// conformance driver's `block-dump` surface walks: `PaddedFrameBlock.open`
// (build/tables-generated-dart/block/PaddedBlock.dart) over
// testdata/wire/tables/block_padded.bin — the image of tables/block/Padded.schema
// — then a PaddedRow row through the generated cursor, and the optional
// field's generated accessor `counterPresent`. PaddedRow.counter is the one
// optional in the block corpus (§19.3): its present flag is the byte at
// row+60, and the accessor normalises it with `!= 0`.
//
// The fixture's present bytes are 0x00 and 0x01, which is what a valid image
// holds. The law's distinctive byte is 0x02 — a present flag that is not the
// language's true value must still read as true, never verbatim — so this test
// starts from the fixture and flips one row's present byte to 0x02 (and to
// 0xff), asserting the SAME generated accessor answers true for both, and
// false for a clear 0x00. If the accessor compared `== 1` the 0x02 byte would
// read false and this test goes red.

import 'dart:io';
import 'dart:typed_data';

import '../../../../build/tables-generated-dart/block/PaddedBlock.dart' as blk;

var failed = false;

void check(bool ok, String what) {
  if (!ok) {
    print('FAILED: $what');
    failed = true;
  }
}

int exitCode() => failed ? 1 : 0;

void main() {
  final image = File('testdata/wire/tables/block_padded.bin').readAsBytesSync();

  final block = blk.PaddedFrameBlock.open(image, 0, image.length);
  check(block != null, 'block_padded.bin opens under PaddedFrameBlock.open');
  if (block == null) {
    exit(exitCode());
  }

  final row = block.rowsCursor();
  block.rowsAt(0, row);
  check(!row.counterPresent, 'row 0: a clear present byte 0x00 reads false');
  block.rowsAt(1, row);
  check(row.counterPresent, 'row 1: a set present byte 0x01 reads true');

  // The law's byte: 0x02 is not this language's true, and a reader that
  // compares `== 1` or stores the byte verbatim answers false here. The block
  // prologue does not check row contents, so a mutated copy still opens.
  final present = row.at + 60; // the generated offset of counter's flag
  for (final byte in <int>[0x02, 0xff]) {
    final mutated = Uint8List.fromList(image);
    mutated[present] = byte;
    final again = blk.PaddedFrameBlock.open(mutated, 0, mutated.length);
    check(again != null, 'a block with a 0x${byte.toRadixString(16)} present '
        'byte still opens');
    if (again != null) {
      final row2 = again.rowsCursor();
      again.rowsAt(1, row2);
      check(
        row2.counterPresent,
        'a present flag of 0x${byte.toRadixString(16)} normalises to true '
        '(byte != 0)',
      );
    }
  }

  exit(exitCode());
}