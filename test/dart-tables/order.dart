// THE ORDER-WORD GATE over the Dart accelerators (docs/PORTING.md I7,
// docs/SPEC-TABLES.md §7.1, §19.1).
//
// The conformance harness's `cook-foreign` and `block-foreign` surfaces reverse
// a file's MAGIC, which is the cross-endian refusal every leg can answer on any
// host. The half they do NOT hold is a file whose magic is INTACT and whose
// prologue order word records the other order — exactly the file a reader
// leaning on one check would open.
//
// Dart reads every word through an explicit `Endian.little`, so a Dart reader's
// order is the READER's rather than the host's and there is no native path for a
// big-endian file to take. This gate proves the order word is refused BESIDE the
// magic and not only through it: it opens a same-order block and cook (both must
// open), then hands each the same bytes with its prologue word set to the other
// order (both must refuse).
//
//   usage: dart test/dart-tables/order.dart
//
// Run from the repository root, which is where the Makefile runs it.
import 'dart:io';
import 'dart:typed_data';

import '../../build/tables-generated-dart/block/RenderBlock.dart' as blk;
import '../../build/tables-generated-dart/pointers/GraphCook.dart' as ck;

int failures = 0;

void check(bool ok, String what) {
  if (!ok) {
    stdout.writeln('FAILED: $what');
    failures++;
  }
}

// withWord is a fresh image with the eight-byte word at `at` set to `value`,
// little-endian, exactly where a same-build writer put the order word.
Uint8List withWord(Uint8List source, int at, int value) {
  final copy = Uint8List.fromList(source);
  ByteData.view(copy.buffer).setUint64(at, value, Endian.little);
  return copy;
}

void main() {
  final block = File('testdata/wire/tables/block_render.bin').readAsBytesSync();
  check(
    blk.RenderFrameBlock.open(Uint8List.fromList(block), 0, block.length) !=
        null,
    "the block of this build's byte order does not open",
  );
  check(
    blk.RenderFrameBlock.open(withWord(block, 16, 2), 0, block.length) == null,
    'a block whose magic is intact and whose prologue order word records the '
    'other order opened',
  );

  final cook = File('build/cook-fuzz/Scene.cook').readAsBytesSync();
  check(
    ck.SceneCook.open(Uint8List.fromList(cook), 0, cook.length) != null,
    "the cook of this build's byte order does not open",
  );
  check(
    ck.SceneCook.open(withWord(cook, 16, 2), 0, cook.length) == null,
    'a cook whose magic is intact and whose header order word records the '
    'other order opened',
  );

  stdout.writeln('dart tables order: 4 checks, $failures failure(s)');
  exit(failures == 0 ? 0 : 1);
}
