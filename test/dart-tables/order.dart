// THE CROSS-ENDIAN ORDER-WORD GATE (docs/SPEC-TABLES.md §19.1, §7.1; issue
// #415; docs/PORTING.md I7). The harness's two foreign surfaces (cook-foreign,
// block-foreign) reverse the MAGIC word, which every leg answers on any host;
// nothing forges the half a reader leaning on one check would open — a file
// whose magic is intact and whose order word says the other order. This gate
// holds that half: a copy of each corpus image with ONLY the order word
// (offset 16 of the prologue/header, 8 bytes) set to the other order's value
// (2) must be refused by the block AND the cook paths, the unmodified image
// must still open, and no exception may escape either call.
//
// Dart's typed data is read at an explicit endianness everywhere
// (internal/codegen/darttable/blockruntime.go), so this reader has no native
// order of its own: the order word is the whole of the byte-order check, and
// it is read bytewise as little-endian, so value 2 is byte 0x02 at offset 16
// followed by zeros.
//
//   usage: order
import 'dart:io';
import 'dart:typed_data';

import '../../build/tables-generated-dart/block/PaddedBlock.dart' as blk;
import '../../build/tables-generated-dart/block/RenderBlock.dart' as blk;
import '../../build/tables-generated-dart/pointers/GraphCook.dart' as ck;

Uint8List load(String path) => File(path).readAsBytesSync();

/// forgeOrderWord returns the image with ONLY the order word set to the other
/// order's value, 2, leaving the magic, the build version and everything else
/// intact. The order word is the eight bytes at offset 16, read bytewise as
/// little-endian, so 2 is L 0x02 at offset 16 and zeros after it.
Uint8List forgeOrderWord(Uint8List source) {
  final out = Uint8List.fromList(source);
  if (out.length >= 24) {
    for (var i = 16; i < 24; i++) {
      out[i] = 0;
    }
    out[16] = 2;
  }
  return out;
}

class Row {
  final String name;
  final String kind; // 'block' or 'cook'
  final Uint8List image;
  final Object? Function(Uint8List) open;

  Row(this.name, this.kind, this.image, this.open);
}

int failures = 0;

void check(Row row) {
  Object? opened;
  try {
    opened = row.open(row.image);
  } catch (e) {
    failures++;
    stdout.write('ORDER FAIL ${row.name}: an exception ESCAPED opening the unmodified image: $e\n');
    return;
  }
  if (opened == null) {
    failures++;
    stdout.write('ORDER FAIL ${row.name}: the unmodified corpus image REFUSED — it must open\n');
    return;
  }
  final forged = forgeOrderWord(row.image);
  try {
    opened = row.open(forged);
  } catch (e) {
    failures++;
    stdout.write('ORDER FAIL ${row.name}: an exception ESCAPED opening the forged image: $e\n');
    return;
  }
  if (opened != null) {
    failures++;
    stdout.write('ORDER FAIL ${row.name}: a forged order word OPENED — it must refuse\n');
    return;
  }
  stdout.write('ORDER ok ${row.name}: unmodified opens and a forged order word refuses\n');
}

void main(List<String> args) {
  final rows = <Row>[
    Row(
      'block RenderFrame',
      'block',
      load('testdata/wire/tables/block_render.bin'),
      (b) => blk.RenderFrameBlock.open(b, 0, b.length),
    ),
    Row(
      'block PaddedFrame',
      'block',
      load('testdata/wire/tables/block_padded.bin'),
      (b) => blk.PaddedFrameBlock.open(b, 0, b.length),
    ),
    Row(
      'cook Scene',
      'cook',
      load('build/cook-fuzz/Scene.cook'),
      (b) => ck.SceneCook.open(b, 0, b.length),
    ),
    Row(
      'cook Depot',
      'cook',
      load('build/cook-fuzz/Depot.cook'),
      (b) => ck.DepotCook.open(b, 0, b.length),
    ),
    Row(
      'cook Album',
      'cook',
      load('build/cook-fuzz/Album.cook'),
      (b) => ck.AlbumCook.open(b, 0, b.length),
    ),
    Row(
      'cook TreeNode',
      'cook',
      load('build/cook-fuzz/TreeNode.cook'),
      (b) => ck.TreeNodeCook.open(b, 0, b.length),
    ),
    Row(
      'cook ListNode',
      'cook',
      load('build/cook-fuzz/ListNode.cook'),
      (b) => ck.ListNodeCook.open(b, 0, b.length),
    ),
  ];

  for (final row in rows) {
    check(row);
  }

  stdout.write(
    'dart tables order: ${rows.length} rows over ${rows.where((r) => r.kind == 'block').length} '
    'blocks and ${rows.where((r) => r.kind == 'cook').length} cooks — $failures finding(s)\n',
  );
  exit(failures == 0 ? 0 : 1);
}
