// THE DART CONFORMANCE DRIVER (test/conformance/README.md).
//
// The twin of test/conformance/cpp/main.cpp and of the C# one, and it is
// deliberately the same shape: one process per surface, every expectation in
// the data, nothing literal here.
//
//   driver <manifest> list
//   driver <manifest> <surface> <outdir>
//
// The working directory is the repository root, which is the contract: every
// path in the manifest is repo-relative, so this driver never resolves one.
import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import '../../../build/tables-generated-dart/block/BlockdemoBlock.dart';
import '../../../build/tables-generated-dart/block/PaddedBlock.dart' as blk;
import '../../../build/tables-generated-dart/block/RenderBlock.dart' as blk;
import '../../../build/tables-generated-dart/cards/CardsWire.dart';
import '../../../build/tables-generated-dart/pointers/GraphdemoCook.dart';
import '../../../build/tables-generated-dart/pointers/GraphCook.dart' as ck;
import '../../../build/tables-generated-dart/pointers/GraphdemoWire.dart' as graphdemo;
import '../../../build/tables-generated-dart/blobs/BlobdemoWire.dart' as blobdemo;
import '../../../build/tables-generated-dart/blobs/AssetsCook.dart' as assets;
import '../../../build/tables-generated-dart/lists/ListdemoWire.dart' as listdemo;
import '../../../build/tables-generated-dart/maps/MapdemoWire.dart' as mapdemo;
import '../../../build/tables-generated-dart/messages/MessagedemoWire.dart' as messagedemo;
import '../../../build/tables-generated-dart/backend/BackenddemoWire.dart' as backenddemo;
import '../../../build/tables-generated-dart/stream/StreamdemoWire.dart' as streamdemo;
import '../../../build/tables-generated-dart/examples/TabledemoWire.dart' as tabledemo;
import '../../../build/tables-generated-dart/vocab/VocabdemoWire.dart' as vocabdemo;
import '../../../build/tables-generated-dart/vocab9/Vocab9demoWire.dart' as vocab9demo;
import '../../../build/tables-generated-dart-scalars/scalars/ScalardemoWire.dart' as scalardemo;
import '../../../build/tables-generated-dart/wide/WideWire.dart' as widedemo;
import '../../../build/tables-generated-dart/v1/Tblv1Wire.dart' as tblv1;
import '../../../build/tables-generated-dart/v2/Tblv2Wire.dart' as tblv2;
import '../../../build/tables-generated-dart/w1/Tblw1Wire.dart' as tblw1;
import '../../../build/tables-generated-dart/w2/Tblw2Wire.dart' as tblw2;
import '../../../build/tables-generated-dart/r1/Tblr1Wire.dart' as tblr1;
import '../../../build/tables-generated-dart/r2/Tblr2Wire.dart' as tblr2;
import '../../../build/tables-generated-dart/backend/BackenddemoFixed.dart' as backenddemo_fixed;
import '../../../build/tables-generated-dart/backend/BackendFixed.dart' as backend;
import '../../../build/tables-generated-dart/vocab/VocabdemoFixed.dart' as vocabdemo_fixed;
import '../../../build/tables-generated-dart/vocab/VocabFixed.dart' as vocab;
import '../../../build/tables-generated-dart/vocab9/Vocab9demoFixed.dart' as vocab9demo_fixed;
import '../../../build/tables-generated-dart/vocab9/Vocab9Fixed.dart' as vocab9;
import '../../../build/tables-generated-dart/examples/TabledemoFixed.dart' as tabledemo_fixed;
import '../../../build/tables-generated-dart/examples/TablesFixed.dart' as tables_fixed;
import '../../../build/tables-generated-dart/examples/NestedFixed.dart' as nested_fixed;
import '../../../build/tables-generated-dart/examples/PackFixed.dart' as pack_fixed;
import '../../../build/tables-generated-dart/examples/KeyedFixed.dart' as keyed_fixed;
import '../../../build/tables-generated-dart/v1/Tblv1Fixed.dart' as tblv1_fixed;
import '../../../build/tables-generated-dart/v1/V1Fixed.dart' as v1_fixed;
import '../../../build/tables-generated-dart/v2/Tblv2Fixed.dart' as tblv2_fixed;
import '../../../build/tables-generated-dart/v2/V2Fixed.dart' as v2_fixed;
import '../../../build/tables-generated-dart/p1/Tblp1Fixed.dart' as tblp1_fixed;
import '../../../build/tables-generated-dart/p1/P1Fixed.dart' as p1_fixed;
import '../../../build/tables-generated-dart/p3/Tblp3Fixed.dart' as tblp3_fixed;
import '../../../build/tables-generated-dart/p3/P3Fixed.dart' as p3_fixed;
import '../../../build/tables-generated-dart/messages/MessagedemoFixed.dart' as messagedemo_fixed;
import '../../../build/tables-generated-dart/messages/MessagesFixed.dart' as messages_fixed;
import '../../../build/tables-generated-dart/m1/Tblm1Fixed.dart' as tblm1_fixed;
import '../../../build/tables-generated-dart/m1/M1Fixed.dart' as m1_fixed;
import '../../../build/tables-generated-dart/m2/Tblm2Fixed.dart' as tblm2_fixed;
import '../../../build/tables-generated-dart/m2/M2Fixed.dart' as m2_fixed;
import '../../../build/tables-generated-dart/wide/WideFixed.dart' as widedemo_fixed;
import '../../../build/tables-generated-dart/wide/CaptionFixed.dart' as caption_fixed;
import '../../../build/tables-generated-dart-scalars/scalars/ScalardemoFixed.dart' as scalardemo_fixed;
import '../../../build/tables-generated-dart-scalars/scalars/ScalarsFixed.dart' as scalars_fixed;
import '../../../build/tables-generated-dart-scalars/scalars/Int128.dart' as scalars_i128;
import '../../../build/tables-generated-dart-scalars/scalars2/Scalardemo2Fixed.dart' as scalardemo2_fixed;
import '../../../build/tables-generated-dart-scalars/scalars2/Scalars2Fixed.dart' as scalars2_fixed;
import '../../../build/tables-generated-dart/r1/Tblr1Fixed.dart' as tblr1_fixed;
import '../../../build/tables-generated-dart/r1/R1Fixed.dart' as r1_fixed;
import '../../../build/tables-generated-dart/r2/Tblr2Fixed.dart' as tblr2_fixed;
import '../../../build/tables-generated-dart/r2/R2Fixed.dart' as r2_fixed;
import '../../../build/tables-generated-dart/a2/Tbla2Fixed.dart' as tbla2_fixed;
import '../../../build/tables-generated-dart/a2/A2Fixed.dart' as a2_fixed;
import '../../../build/tables-generated-dart/k1/Tblk1Fixed.dart' as tblk1_fixed;
import '../../../build/tables-generated-dart/k1/K1Fixed.dart' as k1_fixed;
import '../../../build/tables-generated-dart/k2/Tblk2Fixed.dart' as tblk2_fixed;
import '../../../build/tables-generated-dart/k2/K2Fixed.dart' as k2_fixed;
import '../../../build/tables-generated-dart/g1/Tblg1Wire.dart' as tblg1;
import '../../../build/tables-generated-dart/m1/Tblm1Wire.dart' as tblm1;
import '../../../build/tables-generated-dart/m2/Tblm2Wire.dart' as tblm2;
import '../../../build/tables-generated-dart/p1/Tblp1Wire.dart' as tblp1;
import '../../../build/tables-generated-dart/p3/Tblp3Wire.dart' as tblp3;
import '../../../build/tables-generated-dart/p2/Tblp2Wire.dart' as tblp2;
import '../../../build/tables-generated-dart/k1/Tblk1Wire.dart' as tblk1;
import '../../../build/tables-generated-dart/k2/Tblk2Wire.dart' as tblk2;
import '../../../build/tables-generated-dart/a2/Tbla2Wire.dart' as tbla2;
import '../../../build/tables-generated-dart-scalars/scalars2/Scalardemo2Wire.dart' as scalardemo2;
import '../../../build/tables-generated-dart/examples/TablesCook.dart' as tablescook;
import '../../../build/tables-generated-dart/examples/NestedCook.dart' as nestedcook;
import '../../../build/tables-generated-dart/examples/KeyedCook.dart' as keyedcook;
import '../../../build/tables-generated-dart/examples/WideCook.dart' as widecook;
import '../../../build/tables-generated-dart/v1/V1Cook.dart' as v1cook;
import '../../../build/tables-generated-dart/v2/V2Cook.dart' as v2cook;
import '../../../build/tables-generated-dart/p1/P1Cook.dart' as p1cook;
import '../../../build/tables-generated-dart/p3/P3Cook.dart' as p3cook;
import '../../../build/tables-generated-dart/m1/M1Cook.dart' as m1cook;
import '../../../build/tables-generated-dart/m2/M2Cook.dart' as m2cook;
import '../../../build/tables-generated-dart/stream/StreamCook.dart' as streamcook;
import '../../../build/tables-generated-dart/messages/MessagesCook.dart' as messagescook;
import '../../../build/tables-generated-dart-scalars/scalars/ScalarsCook.dart' as scalarscook;
import '../../../build/tables-generated-dart/lists/SaveCook.dart' as lsavecook;
import '../../../build/tables-generated-dart/lists/SharedCook.dart' as lsharedcook;
import '../../../build/tables-generated-dart/lists/HoldersCook.dart' as lholderscook;
import '../../../build/tables-generated-dart/lists/MigrateCook.dart' as lmigratecook;
import '../../../build/tables-generated-dart/maps/CellsCook.dart' as mcellscook;
import '../../../build/tables-generated-dart/maps/ChunksCook.dart' as mchunkscook;
import '../../../build/tables-generated-dart/maps/CrewsCook.dart' as mcrewscook;
import '../../../build/tables-generated-dart/maps/DepthCook.dart' as mdepthcook;
import '../../../build/tables-generated-dart/maps/DocsCook.dart' as mdocscook;
import '../../../build/tables-generated-dart/maps/FleetCook.dart' as mfleetcook;
import '../../../build/tables-generated-dart/maps/PairsCook.dart' as mpairscook;
import '../../../build/tables-generated-dart/maps/RunsCook.dart' as mrunscook;
import '../../../build/tables-generated-dart/maps/SlotsCook.dart' as mslotscook;
import '../../../build/tables-generated-dart/maps/SpansCook.dart' as mspanscook;
import '../../../build/tables-generated-dart/maps/TextCook.dart' as mtextcook;
import '../../../build/tables-generated-dart/maps/TrailsCook.dart' as mtrailscook;
import '../../../build/tables-generated-dart/widelist/WidelistdemoWire.dart' as widelist;
import '../../../build/tables-generated-dart/widelist/WidesCook.dart' as widelistcook;
import '../../../build/tables-generated-dart/rt1/Tblrt1Wire.dart' as tblrt1;
import '../../../build/tables-generated-dart/features/FeaturewitnessesWire.dart' as feature;
import '../../../build/tables-generated-dart/features/FeaturesCook.dart' as featurecook;

// Decode JSON bytes before entering a generated String reader. Dart's UTF-8
// decoder skips one leading BOM, matching the Go reader's whole-text boundary.
String? readJsonTextFile(String path) {
  final bytes = File(path).readAsBytesSync();
  try {
    return utf8.decode(bytes, allowMalformed: false);
  } on FormatException {
    return null;
  }
}

// ---- the manifest, exactly as testdata/conformance/tables/FORMAT.md states it

final List<List<String>> lines = [];

void readManifest(String path) {
  for (final raw in File(path).readAsLinesSync()) {
    final text = raw.trim();
    if (text.isEmpty || text.startsWith('#')) {
      continue;
    }
    lines.add(text.split(RegExp(r'[ \t]+')));
  }
}

Iterable<List<String>> kind(String want) => lines.where((f) => f[0] == want);

Uint8List blockBuffer(Uint8List image, int extent, int pointer) {
  // A CLAIM SHORTER THAN THE IMAGE IS A TRUNCATION, so the buffer is the claim
  // in that direction too and only what fits is copied.
  final claim = extent < 0 ? image.length : extent;
  final base = pointer < 0 ? 0 : pointer;
  final buffer = Uint8List(base + claim);
  final copy = claim < image.length ? claim : image.length;
  buffer.setRange(base, base + copy, image);
  return buffer;
}

String openBlock(
  String name,
  Uint8List image,
  int extent,
  int pointer, {
  bool reasons = false,
}) {
  final claim = extent < 0 ? image.length : extent;
  // no buffer at all: the null the reason surface hands the reader as is
  final buffer = pointer < 0 ? null : blockBuffer(image, extent, pointer);
  final base = pointer < 0 ? 0 : pointer;
  final (Object?, Object) opened = name.startsWith('block_render')
      ? blk.RenderFrameBlock.openWithReason(buffer, base, claim)
      : name.startsWith('block_padded')
      ? blk.PaddedFrameBlock.openWithReason(buffer, base, claim)
      : throw StateError('driver: no block named $name');
  if (reasons) {
    // THE REASON BESIDE THE FALSE (docs/SPEC-TABLES.md §19.2): the enum's own
    // word, `ok` when it opened (test/conformance/README.md, block-reason)
    return '${opened.$2}\n';
  }
  return opened.$1 == null ? 'refuse\n' : 'open\n';
}

int surfaceBlock(String outDir) {
  for (final f in kind('block')) {
    final image = File(f[3]).readAsBytesSync();
    File('$outDir/${f[1]}').writeAsStringSync(openBlock(f[1], image, -1, 0));
  }
  return 0;
}

// foreign returns the image with its MAGIC word — the eight bytes at offset 0
// — reversed, which is what that word looks like to a reader of the other byte
// order (docs/SPEC-TABLES.md §19.1, §7.1).
//
// It makes the file foreign to WHOEVER READS IT rather than to a particular
// host: whatever this build's order is, the magic it now reads is not this
// build's, so the refusal lands on the magic check every open puts first. That
// is the only shape a cross-endian expectation can take without depending on
// the host it runs on.
Uint8List foreign(Uint8List image) {
  final out = Uint8List.fromList(image);
  if (out.length >= 8) {
    for (var i = 0; i < 4; i++) {
      final swap = out[i];
      out[i] = out[7 - i];
      out[7 - i] = swap;
    }
  }
  return out;
}

int surfaceBlockForeign(String outDir) {
  for (final f in kind('block')) {
    final image = foreign(File(f[3]).readAsBytesSync());
    File('$outDir/${f[1]}').writeAsStringSync(openBlock(f[1], image, -1, 0));
  }
  return 0;
}

int surfaceForgery(String outDir) {
  for (final f in kind('forgery')) {
    if (f[2] != 'block') {
      continue; // the cook's battery is its own
    }
    final image = File(f[4]).readAsBytesSync();
    final extent = int.parse(f[5]);
    final pointer = f[6] == 'null' ? -1 : int.parse(f[6]);
    File('$outDir/${f[1]}')
        .writeAsStringSync(openBlock(f[3], image, extent, pointer));
  }
  return 0;
}

// ---- the BLOCK ROW DUMP (testdata/conformance/tables/FORMAT.md) ----
//
// The twin of the C++ leg's walk, and like it, written against §8's descriptors
// and NOTHING ELSE: no generated row cursor, no field named in this file. That
// is the claim §19.2 makes for the descriptors, and a walk that reached for a
// cursor would be proving something else. A FLOAT is its IEEE-754 bit pattern,
// because a block row is a byte-identical projection and its bits are the fact.

void dumpScalar(StringBuffer into, ByteData view, int at, int kind, int width) {
  switch (kind) {
    case 1:
      into.write(view.getUint8(at) != 0 ? 'true' : 'false');
      return;
    case 10:
      into.write(
        '0x${view.getUint32(at, Endian.little).toRadixString(16).padLeft(8, '0')}',
      );
      return;
    case 11:
      into.write('0x${hex64(view.getUint64(at, Endian.little))}');
      return;
    case 2:
    case 3:
    case 4:
    case 5:
      final v = width == 1
          ? view.getInt8(at)
          : width == 2
          ? view.getInt16(at, Endian.little)
          : width == 4
          ? view.getInt32(at, Endian.little)
          : view.getInt64(at, Endian.little);
      into.write(v.toString());
      return;
    default:
      final v = width == 1
          ? view.getUint8(at)
          : width == 2
          ? view.getUint16(at, Endian.little)
          : width == 4
          ? view.getUint32(at, Endian.little)
          : view.getUint64(at, Endian.little);
      into.write(unsignedText(v));
      return;
  }
}

// a u64 bit pattern held in a signed int renders as its unsigned magnitude
String unsignedText(int v) =>
    v >= 0 ? v.toString() : BigInt.from(v).toUnsigned(64).toString();

String hex64(int v) {
  final high = (v >> 32) & 0xffffffff;
  final low = v & 0xffffffff;
  return high.toRadixString(16).padLeft(8, '0') +
      low.toRadixString(16).padLeft(8, '0');
}

void dumpText(StringBuffer into, Uint8List bytes, int at, int used) {
  if (used < 0) {
    used = 0;
  }
  into.write('"');
  for (var i = 0; i < used; i++) {
    final c = bytes[at + i];
    if (c >= 0x20 && c < 0x7f && c != 0x22 && c != 0x5c) {
      into.writeCharCode(c);
    } else {
      into.write('\\x${c.toRadixString(16).padLeft(2, '0')}');
    }
  }
  into.write('" len=$used');
}

String dumpJoin(String prefix, String name) =>
    prefix.isEmpty ? name : '$prefix.$name';

bool dumpRecord(
  StringBuffer into,
  Uint8List bytes,
  ByteData view,
  int storage,
  TableBlockInfo? info,
  String path,
) {
  if (info == null) {
    stderr.writeln('driver: a descriptor names no record');
    return false;
  }
  for (final f in info.fields) {
    if (f.outOfLine) {
      continue;
    }
    final name = dumpJoin(path, f.name);
    if (f.counted) {
      final used = view.getInt32(storage + f.countOffset, Endian.little);
      if (used < 0 || used > f.arrayBound) {
        stderr.writeln(
          'driver: ${info.name}.${f.name} carries a used length of $used, '
          'outside [ 0, ${f.arrayBound} ]',
        );
        return false;
      }
      into.write('  $name = ');
      dumpText(into, bytes, storage + f.offset, used);
      into.write('\n');
    } else {
      final slots = f.isArray ? f.arrayBound : 1;
      for (var s = 0; s < slots; s++) {
        final at = f.isArray ? '$name[$s]' : name;
        final value = storage + f.offset + s * f.elemSize;
        if (f.element != null) {
          if (!dumpRecord(into, bytes, view, value, f.element, at)) {
            return false;
          }
        } else {
          into.write('  $at = ');
          dumpScalar(into, view, value, f.kind, f.elemSize);
          into.write('\n');
        }
      }
    }
    if (f.optional) {
      final present = view.getUint8(storage + f.presentOffset) != 0;
      into.write('  $name#present = ${present ? 'true' : 'false'}\n');
    }
  }
  return true;
}

bool dumpBlock(
  StringBuffer into,
  Uint8List bytes,
  ByteData view,
  int base,
  TableBlockInfo info,
) {
  into.write('projection ${info.name} @0\n');
  if (!dumpRecord(into, bytes, view, base, info, '')) {
    return false;
  }
  for (final f in info.fields) {
    if (!f.outOfLine) {
      continue;
    }
    final offsetOf = view.getUint64(base + f.offsetOfOffset, Endian.little);
    final count = view.getUint32(base + f.countOffset, Endian.little);
    final stride = view.getUint32(base + f.strideOffset, Endian.little);
    final row = f.element;
    if (row == null) {
      stderr.writeln('driver: ${f.name} names no element');
      return false;
    }
    into.write(
      'array ${f.name} ${row.name} @$offsetOf count=$count stride=$stride\n',
    );
    for (var r = 0; r < count; r++) {
      final at = offsetOf + r * stride;
      into.write('row $r @$at\n');
      if (!dumpRecord(into, bytes, view, base + at, row, '')) {
        return false;
      }
    }
  }
  return true;
}

int surfaceBlockDump(String outDir) {
  for (final f in kind('block')) {
    final image = File(f[3]).readAsBytesSync();
    final buffer = blockBuffer(image, -1, 0);
    final view = ByteData.view(buffer.buffer);
    final into = StringBuffer();
    final bool ok;
    if (f[1].startsWith('block_render')) {
      final block = blk.RenderFrameBlock.open(buffer, 0, image.length);
      ok =
          block != null &&
          dumpBlock(into, buffer, view, 0, blk.RenderFrameBlock.type);
    } else if (f[1].startsWith('block_padded')) {
      final block = blk.PaddedFrameBlock.open(buffer, 0, image.length);
      ok =
          block != null &&
          dumpBlock(into, buffer, view, 0, blk.PaddedFrameBlock.type);
    } else {
      stderr.writeln('driver: no block named ${f[1]}');
      return 1;
    }
    if (!ok) {
      return 1;
    }
    File('$outDir/${f[1]}').writeAsStringSync(into.toString());
  }
  return 0;
}

// ---- the COOKED form (docs/SPEC-TABLES.md §7) ----
//
// cookAlignment is the alignment the header NAMES, which is where a forged
// buffer has to be placed for the base-alignment check to mean anything. A
// forged word that is not an alignment at all puts the buffer at the format's
// own floor instead.
int cookAlignment(Uint8List source) {
  if (source.length < 48) {
    return 8;
  }
  final view = ByteData.view(
    source.buffer,
    source.offsetInBytes,
    source.lengthInBytes,
  );
  final a = view.getUint64(40, Endian.little);
  if (a < 1 || a > 64 || a & (a - 1) != 0) {
    return 8;
  }
  return a;
}

// place is the forgery contract, exactly (testdata/conformance/tables/FORMAT.md):
// the buffer is EXACTLY the extent the caller claims, its base `lead` bytes past
// an aligned offset, what fits copied in and the rest zero. A Dart base is
// (buffer, offset), so the alignment the reader checks is the offset's.
(Uint8List, int, int) place(
  Uint8List data,
  int extent,
  int lead,
  int alignment,
) {
  final bytes = extent < 0 ? data.length : extent;
  final base = lead;
  final buffer = Uint8List(base + bytes);
  final copy = bytes < data.length ? bytes : data.length;
  buffer.setRange(base, base + copy, data);
  return (buffer, base, bytes);
}

Object? openCookAt(String root, Uint8List? buffer, int base, int bytes) =>
    openCookAtReason(root, buffer, base, bytes).$1;

// openCookAtReason is the cook's Open with the FIRST failing clause beside the
// null (docs/SPEC-TABLES.md §7), the reference leg's word for it.
(Object?, Object) openCookAtReason(
  String root,
  Uint8List? buffer,
  int base,
  int bytes,
) {
  switch (root) {
    case 'Scene':
      return ck.SceneCook.openWithReason(buffer, base, bytes);
    case 'Depot':
      return ck.DepotCook.openWithReason(buffer, base, bytes);
    case 'Album':
      return ck.AlbumCook.openWithReason(buffer, base, bytes);
    case 'TreeNode':
      return ck.TreeNodeCook.openWithReason(buffer, base, bytes);
    case 'ListNode':
      return ck.ListNodeCook.openWithReason(buffer, base, bytes);
  }
  throw StateError('driver: no cook root named $root');
}

TableCookInfo cookType(String root) {
  switch (root) {
    case 'Scene':
      return ck.SceneCook.type;
    case 'Depot':
      return ck.DepotCook.type;
    case 'Album':
      return ck.AlbumCook.type;
    case 'TreeNode':
      return ck.TreeNodeCook.type;
    case 'ListNode':
      return ck.ListNodeCook.type;
  }
  throw StateError('driver: no cook root named $root');
}

// the region a cook was opened over: its first byte and its data length, read
// back through the one accessor pair every root's cook carries
(int, int) cookRegion(Object cook) {
  if (cook is ck.SceneCook) {
    return (cook.region, cook.length);
  }
  if (cook is ck.DepotCook) {
    return (cook.region, cook.length);
  }
  if (cook is ck.AlbumCook) {
    return (cook.region, cook.length);
  }
  if (cook is ck.TreeNodeCook) {
    return (cook.region, cook.length);
  }
  if (cook is ck.ListNodeCook) {
    return (cook.region, cook.length);
  }
  throw StateError('driver: not a cook');
}

String openCookForged(
  String root,
  Uint8List data,
  int extent,
  int lead,
  bool nil, {
  bool reasons = false,
}) {
  final (buffer, base, bytes) = place(data, extent, lead, cookAlignment(data));
  final opened = openCookAtReason(root, nil ? null : buffer, base, bytes);
  if (reasons) {
    // THE REASON BESIDE THE NULL (docs/SPEC-TABLES.md §7): the enum's own
    // word, `ok` when it opened (test/conformance/README.md, cook-reason)
    return '${opened.$2}\n';
  }
  return opened.$1 == null ? 'refuse\n' : 'open\n';
}

// forgeryLine finds the `forgery` row a `refusal` row names: the refusal
// carries the forgery's name and kind, and the bytes, extent and base are the
// forgery's own (testdata/conformance/tables/FORMAT.md).
List<String> forgeryLine(String name) =>
    kind('forgery').firstWhere((f) => f[1] == name);

// cook-reason and block-reason: one word per `refusal` row of the kind, the
// first failing clause as the reference leg's TableRefuseReason spells it
// (docs/SPEC-TABLES.md §7, §19.2; internal/codegen/gotable/refusal.go).
int surfaceReason(String outDir, String want) {
  for (final r in kind('refusal')) {
    if (r[2] != want) {
      continue;
    }
    final f = forgeryLine(r[1]);
    final data = File(f[4]).readAsBytesSync();
    final extent = int.parse(f[5]);
    final pointer = f[6] == 'null' ? -1 : int.parse(f[6]);
    final answer = want == 'cook'
        ? openCookForged(
            f[3],
            data,
            extent,
            pointer < 0 ? 0 : pointer,
            pointer < 0,
            reasons: true,
          )
        : openBlock(f[3], data, extent, pointer, reasons: true);
    File('$outDir/${r[1]}').writeAsStringSync(answer);
  }
  return 0;
}

int surfaceCookForgery(String outDir) {
  for (final f in kind('forgery')) {
    if (f[2] != 'cook') {
      continue; // the block's battery is its own
    }
    final data = File(f[4]).readAsBytesSync();
    final extent = int.parse(f[5]);
    final pointer = f[6] == 'null' ? -1 : int.parse(f[6]);
    File('$outDir/${f[1]}').writeAsStringSync(
      openCookForged(
        f[3],
        data,
        extent,
        pointer < 0 ? 0 : pointer,
        pointer < 0,
      ),
    );
  }
  return 0;
}

int surfaceCookForeign(String outDir) {
  for (final f in kind('cook')) {
    final data = foreign(File(f[4]).readAsBytesSync());
    File('$outDir/${f[1]}')
        .writeAsStringSync(openCookForged(f[3], data, -1, 0, false));
  }
  return 0;
}

// ---- THE COOK'S NODE DUMP (testdata/conformance/tables/FORMAT.md) ----
//
// Every node this side reaches through its OWN derefs, written as canonical
// text so the C++ leg's walk and this one are byte-compared. It is GENERIC over
// the cook descriptors, which is the whole point of them: a pointer slot is
// eight bytes at `offset` holding the SIGNED SELF-RELATIVE delta of §6.3, and a
// delta of zero is null. A by-value nesting is not a node; it is storage inside
// one, and the walk descends through it to reach the pointer slots inside.

class CookWalk {
  final Uint8List bytes;
  final ByteData view;
  final int region;
  final int length;
  final List<int> offsets = <int>[];
  final List<TableCookInfo> types = <TableCookInfo>[];
  final StringBuffer dump = StringBuffer();

  CookWalk(this.bytes, this.view, this.region, this.length);

  int find(int offset) {
    for (var i = 0; i < offsets.length; i++) {
      if (offsets[i] == offset) {
        return i;
      }
    }
    return -1;
  }

  void node(int offset, TableCookInfo type, int depth) {
    if (depth > 4096) {
      throw StateError(
        'the walk nested past any depth a region can hold — '
        'a cycle the deref did not close',
      );
    }
    final at = find(offset);
    if (at >= 0) {
      if (!identical(types[at], type)) {
        throw StateError(
          'two references name the node at offset $offset as two different '
          'tables: ${types[at].name} and ${type.name}',
        );
      }
      // one node, one visit: sharing and a back-reference are the same fact
      return;
    }
    if (offset > length || type.size > length - offset) {
      throw StateError(
        'the node at offset $offset (${type.name}, size ${type.size}) does not '
        'fit inside the region\'s $length bytes',
      );
    }
    final index = offsets.length;
    offsets.add(offset);
    types.add(type);
    dump.write('node $index ${type.name} @$offset\n');
    storage(region + offset, type, '', depth);
  }

  void emit(String path, String value) {
    dump.write('  $path = $value\n');
  }

  void storage(int at, TableCookInfo type, String path, int depth) {
    for (final f in type.fields) {
      final name = path.isEmpty ? f.name : '$path.${f.name}';

      // every COUNT COMPANION, against its declared bound, and a NEGATIVE one
      // refuses too — an extent is never negative, and a walker handed one
      // indexes backwards out of the region (§7.4's pass two)
      var used = -1;
      if (f.countOffset >= 0) {
        used = view.getInt32(at + f.countOffset, Endian.little);
        if (used < 0 || used > f.arrayBound) {
          throw StateError(
            '${type.name}.${f.name} carries a count companion of $used, '
            'outside [ 0, ${f.arrayBound} ]',
          );
        }
      }

      if (f.isPointer) {
        final slot = at + f.offset;
        final delta = view.getInt64(slot, Endian.little);
        if (delta == 0) {
          emit(name, 'null'); // NULL IN A REGION IS A DELTA OF ZERO (§6.3)
          continue;
        }
        final target = slot + delta;
        if (target < region || target >= region + length) {
          throw StateError(
            '${type.name}.${f.name} resolves outside the region — a delta of '
            '$delta from a slot at ${slot - region}',
          );
        }
        final record = f.info;
        if (record == null) {
          throw StateError(
            '${type.name}.${f.name} is a pointer whose descriptor names no record',
          );
        }
        emit(name, '-> @${target - region}');
        node(target - region, record, depth + 1);
        continue;
      }

      switch (f.storage) {
        case TableCookStorage.string:
        case TableCookStorage.bytes:
          final into = StringBuffer();
          dumpText(into, bytes, at + f.offset, used);
          emit(name, into.toString());
        case TableCookStorage.record:
          // a nested record — by value, or every slot of an array of them. A
          // COUNTED array writes all N slots (§7.2), and a slot past the live
          // count holds the value-initialized element, whose pointer slots are
          // zero: walking all of them is what the check does too.
          final slots = f.isArray ? f.arrayBound : 1;
          for (var s = 0; s < slots; s++) {
            final element = f.isArray ? '$name[$s]' : name;
            storage(at + f.offset + s * f.elemSize, f.info!, element, depth);
          }
        default:
          final slots = f.isArray ? f.arrayBound : 1;
          for (var s = 0; s < slots; s++) {
            final element = f.isArray ? '$name[$s]' : name;
            emit(
              element,
              cookScalar(at + f.offset + s * f.elemSize, f.storage, f.elemSize),
            );
          }
      }

      if (f.countOffset >= 0 &&
          f.storage != TableCookStorage.string &&
          f.storage != TableCookStorage.bytes) {
        emit('$name#count', used.toString());
      }
      if (f.presentOffset >= 0) {
        emit(
          '$name#present',
          view.getUint8(at + f.presentOffset) != 0 ? 'true' : 'false',
        );
      }
    }
  }

  String cookScalar(int at, int storage, int width) {
    switch (storage) {
      case TableCookStorage.boolean:
        return view.getUint8(at) != 0 ? 'true' : 'false';
      case TableCookStorage.float:
        // Nothing in the pointered corpus is a float, and a canonical
        // cross-language spelling of one is a decision this gate should not
        // make in passing. The day a float arrives, the gate says so rather
        // than drifting.
        throw StateError(
          'the dump met a float, whose canonical cross-language spelling this '
          'gate does not fix',
        );
      case TableCookStorage.signed:
        switch (width) {
          case 1:
            return view.getInt8(at).toString();
          case 2:
            return view.getInt16(at, Endian.little).toString();
          case 4:
            return view.getInt32(at, Endian.little).toString();
          default:
            return view.getInt64(at, Endian.little).toString();
        }
      default:
        switch (width) {
          case 1:
            return view.getUint8(at).toString();
          case 2:
            return view.getUint16(at, Endian.little).toString();
          case 4:
            return view.getUint32(at, Endian.little).toString();
          default:
            return unsignedText(view.getUint64(at, Endian.little));
        }
    }
  }
}

int surfaceCook(String outDir) {
  for (final f in kind('cook')) {
    final data = File(f[4]).readAsBytesSync();
    final (buffer, base, bytes) = place(data, -1, 0, cookAlignment(data));
    final cook = openCookAt(f[3], buffer, base, bytes);
    if (cook == null) {
      stderr.writeln('driver: ${f[1]} does not open');
      return 1;
    }
    final (region, length) = cookRegion(cook);
    final view = ByteData.view(buffer.buffer);
    final walk = CookWalk(buffer, view, region, length);
    walk.node(0, cookType(f[3]), 0);
    File('$outDir/${f[1]}').writeAsStringSync(walk.dump.toString());
  }
  return 0;
}

// A row this leg cannot carry writes <case>.absent (test/conformance/README.md).
// Every instance root answers wire: its form-1 load re-saved byte for byte,
// the same pair json-read and json-write already exercise.
// graphdemo, blobdemo and listdemo answer wire, json-read, json-write and
// report for the roots the variable class carries. mapdemo, messagedemo,
// scalars and widedemo answer the same four for every root: maps, unions and
// their arrays, keyed and fixed arrays, optionals, flags, wstring and the wide
// scalars on form 1 and in the text (docs/SPEC-TABLES.md §2.3, §2.6, §2.8, §3,
// §15, §16). The V, W and R evolution units answer wire and report on form 1;
// W1/W2 also exercise default-bearing text round trips.
// Other unsupported rows stay absent. no-text rows are not asked for a text.
bool noTextRow(List<String> f) => f.length > 5 && f[5] == 'no-text';

void spillAbsent(String outDir, String name) {
  File('$outDir/$name.absent').writeAsBytesSync(<int>[]);
}

String _reportLine(int unknown, int kind, int widened, int clamped, int duplicate, bool malformed, bool refused) {
  final m = malformed ? 'true' : 'false';
  final v = refused ? 'refused' : 'read';
  return '$unknown,$kind,$widened,$clamped,$duplicate,$m,$v\n';
}

void _w2fUtf8(String s, Uint8List dst, void Function(int) setLen) {
  final b = utf8.encode(s);
  final n = b.length <= dst.length ? b.length : dst.length;
  dst.setRange(0, n, b);
  setLen(n);
}

String _f2wUtf8(Uint8List src, int len) =>
    utf8.decode(src.sublist(0, len), allowMalformed: true);

void _w2fBytes(String s, Uint8List dst, void Function(int) setLen) {
  final b = latin1.encode(s);
  final n = b.length <= dst.length ? b.length : dst.length;
  dst.setRange(0, n, b);
  setLen(n);
}

String _f2wBytes(Uint8List src, int len) =>
    latin1.decode(src.sublist(0, len));

void _w2fWide(String s, Uint16List dst, void Function(int) setLen) {
  final cu = s.codeUnits;
  final n = cu.length <= dst.length ? cu.length : dst.length;
  dst.setRange(0, n, cu);
  setLen(n);
}

String _f2wWide(Uint16List src, int len) =>
    String.fromCharCodes(src.sublist(0, len));

scalars_i128.Int128 _w2fInt128(BigInt b) {
  final u = b.toUnsigned(128);
  return scalars_i128.Int128((u >> 64).toSigned(64).toInt(), u.toSigned(64).toInt());
}

BigInt _f2wInt128(scalars_i128.Int128 x) =>
    ((BigInt.from(x.hi).toUnsigned(64) << 64) | BigInt.from(x.lo).toUnsigned(64)).toSigned(128);

scalars_i128.UInt128 _w2fUInt128(BigInt b) {
  final u = b.toUnsigned(128);
  return scalars_i128.UInt128((u >> 64).toSigned(64).toInt(), u.toSigned(64).toInt());
}

BigInt _f2wUInt128(scalars_i128.UInt128 x) =>
    (BigInt.from(x.hi).toUnsigned(64) << 64) | BigInt.from(x.lo).toUnsigned(64);

void _w2f_scalars_Pose(scalardemo.Pose w, dynamic f) {
  f.x = w.x.toSigned(64).toInt();
  f.y = w.y.toSigned(64).toInt();
  f.heading = w.heading.toSigned(64).toInt();
}

void _f2w_scalars_Pose(dynamic f, scalardemo.Pose w) {
  w.x = BigInt.from(f.x as int);
  w.y = BigInt.from(f.y as int);
  w.heading = BigInt.from(f.heading as int).toUnsigned(64);
}

void _w2f_scalars_SimState(scalardemo.SimState w, scalardemo_fixed.SimState f) {
  f.tilt = w.tilt.toSigned(64).toInt();
  f.angle = w.angle.toSigned(64).toInt();
  f.position = w.position.toSigned(64).toInt();
  f.reach = _w2fInt128(w.reach);
  f.ticks = w.ticks.toSigned(64).toInt();
  f.ratio = w.ratio.toSigned(64).toInt();
  f.speed = w.speed.toSigned(64).toInt();
  f.span = w.span.toSigned(64).toInt();
  f.mass = _w2fUInt128(w.mass);
  f.frames = w.frames.toSigned(64).toInt();
  f.flux = _w2fInt128(w.flux);
  f.energy = _w2fInt128(w.energy);
  f.entityId = _w2fUInt128(w.entityId);
  f.scale = w.scale.toSigned(64).toInt();
  for (var i = 0; i < 3; i++) f.samples[i] = w.samples[i].toSigned(64).toInt();
  f.weightsCount = w.weights.length;
  for (var i = 0; i < w.weights.length; i++) f.weights[i] = w.weights[i].toSigned(64).toInt();
  for (var i = 0; i < 3; i++) f.axes[i] = w.axes[i].toSigned(64).toInt();
  f.seedsCount = w.seeds.length;
  for (var i = 0; i < w.seeds.length; i++) f.seeds[i] = _w2fUInt128(w.seeds[i]);
  _w2f_scalars_Pose(w.pose, f.pose);
  f.spawnPresent = w.hasSpawn;
  _w2f_scalars_Pose(w.spawn, f.spawn);
}

void _f2w_scalars_SimState(scalardemo_fixed.SimState f, scalardemo.SimState w) {
  w.tilt = BigInt.from(f.tilt);
  w.angle = BigInt.from(f.angle);
  w.position = BigInt.from(f.position);
  w.reach = _f2wInt128(f.reach);
  w.ticks = BigInt.from(f.ticks);
  w.ratio = BigInt.from(f.ratio).toUnsigned(64);
  w.speed = BigInt.from(f.speed).toUnsigned(64);
  w.span = BigInt.from(f.span).toUnsigned(64);
  w.mass = _f2wUInt128(f.mass);
  w.frames = BigInt.from(f.frames).toUnsigned(64);
  w.flux = _f2wInt128(f.flux);
  w.energy = _f2wInt128(f.energy);
  w.entityId = _f2wUInt128(f.entityId);
  w.scale = BigInt.from(f.scale);
  for (var i = 0; i < 3; i++) w.samples[i] = BigInt.from(f.samples[i]);
  w.weights = List<BigInt>.generate(f.weightsCount, (i) => BigInt.from(f.weights[i]).toUnsigned(64));
  for (var i = 0; i < 3; i++) w.axes[i] = BigInt.from(f.axes[i]);
  w.seeds = List<BigInt>.generate(f.seedsCount, (i) => _f2wUInt128(f.seeds[i]));
  _f2w_scalars_Pose(f.pose, w.pose);
  w.hasSpawn = f.spawnPresent;
  _f2w_scalars_Pose(f.spawn, w.spawn);
}

Uint8List? _fixedResave<T, P, R>(
  Uint8List wire,
  T Function() create,
  P Function() newPlan,
  R Function() newReport,
  bool Function(R) refused,
  int Function(List<T>, int, Uint8List, int, P, R) load,
  int Function(int) measure,
  int Function(List<T>, int, Uint8List) save,
) {
  final values = <T>[create()];
  final report = newReport();
  if (load(values, 1, wire, wire.length, newPlan(), report) != 1 || refused(report)) {
    return null;
  }
  final out = Uint8List(measure(1));
  if (save(values, 1, out) < 0) return null;
  return out;
}

Uint8List? _fixedSaveOne<T>(
  T value,
  int Function(int) measure,
  int Function(List<T>, int, Uint8List) save,
) {
  final out = Uint8List(measure(1));
  if (save(<T>[value], 1, out) < 0) return null;
  return out;
}

T? _fixedLoadOne<T, P, R>(
  Uint8List wire,
  T Function() create,
  P Function() newPlan,
  R Function() newReport,
  bool Function(R) refused,
  int Function(List<T>, int, Uint8List, int, P, R) load,
) {
  final values = <T>[create()];
  final report = newReport();
  if (load(values, 1, wire, wire.length, newPlan(), report) != 1 || refused(report)) {
    return null;
  }
  return values[0];
}

String _fixedReportLine<T, P, R>(
  Uint8List wire,
  T Function() create,
  P Function() newPlan,
  R Function() newReport,
  (int, int, bool) Function(R) status,
  int Function(List<T>, int, Uint8List, int, P, R) load,
) {
  final values = <T>[create()];
  final report = newReport();
  final n = load(values, 1, wire, wire.length, newPlan(), report);
  final (unknown, widened, refused) = status(report);
  final bad = n != 1 || refused;
  return _reportLine(unknown, 0, widened, 0, 0, bad, bad);
}

Uint8List? _wireResaveOne<T, R>(
  Uint8List wire,
  R Function() newReport,
  (bool, String) Function(R) status,
  T Function(Uint8List, R) load,
  Uint8List Function(T) save,
) {
  final r = newReport();
  final v = load(wire, r);
  final (refused, refusal) = status(r);
  if (refused) {
    if (refusal.startsWith('blob_shared')) {
      throw StateError(refusal);
    }
    return null;
  }
  return save(v);
}
void _w2f_backend_PlayerRow(backenddemo.PlayerRow w, backenddemo_fixed.PlayerRow f) {
  f.playerId = w.playerId;
  f.score = w.score;
  f.placement = w.placement;
}
void _f2w_backend_PlayerRow(backenddemo_fixed.PlayerRow f, backenddemo.PlayerRow w) {
  w.playerId = f.playerId;
  w.score = f.score;
  w.placement = f.placement;
}
void _w2f_backend_LoginRequest(backenddemo.LoginRequest w, backenddemo_fixed.LoginRequest f) {
  f.playerId = w.playerId;
  _w2fBytes(w.sessionToken, f.sessionToken, (n) => f.sessionTokenLength = n);
  f.clientBuild = w.clientBuild;
  f.region = w.region;
}
void _f2w_backend_LoginRequest(backenddemo_fixed.LoginRequest f, backenddemo.LoginRequest w) {
  w.playerId = f.playerId;
  w.sessionToken = _f2wBytes(f.sessionToken, f.sessionTokenLength);
  w.clientBuild = f.clientBuild;
  w.region = f.region;
}
void _w2f_backend_MatchResult(backenddemo.MatchResult w, backenddemo_fixed.MatchResult f) {
  f.matchId = w.matchId;
  for (var i = 0; i < f.players.length; i++) _w2f_backend_PlayerRow(w.players[i], f.players[i]);
}
void _f2w_backend_MatchResult(backenddemo_fixed.MatchResult f, backenddemo.MatchResult w) {
  w.matchId = f.matchId;
  for (var i = 0; i < f.players.length; i++) _f2w_backend_PlayerRow(f.players[i], w.players[i]);
}
void _w2f_backend_StorePurchase(backenddemo.StorePurchase w, backenddemo_fixed.StorePurchase f) {
  f.playerId = w.playerId;
  _w2fUtf8(w.sku, f.sku, (n) => f.skuLength = n);
  f.quantity = w.quantity;
  f.priceMinor = w.priceMinor;
  f.currency = w.currency;
}
void _f2w_backend_StorePurchase(backenddemo_fixed.StorePurchase f, backenddemo.StorePurchase w) {
  w.playerId = f.playerId;
  w.sku = _f2wUtf8(f.sku, f.skuLength);
  w.quantity = f.quantity;
  w.priceMinor = f.priceMinor;
  w.currency = f.currency;
}
void _w2f_examples_TeamConfig(tabledemo.TeamConfig w, tabledemo_fixed.TeamConfig f) {
  f.spawnCount = w.spawnCount;
  _w2fUtf8(w.banner, f.banner, (n) => f.bannerLength = n);
}
void _f2w_examples_TeamConfig(tabledemo_fixed.TeamConfig f, tabledemo.TeamConfig w) {
  w.spawnCount = f.spawnCount;
  w.banner = _f2wUtf8(f.banner, f.bannerLength);
}
void _w2f_examples_GunnerConfig(tabledemo.GunnerConfig w, tabledemo_fixed.GunnerConfig f) {
  f.reaction = w.reaction;
  f.tracking = w.tracking;
}
void _f2w_examples_GunnerConfig(tabledemo_fixed.GunnerConfig f, tabledemo.GunnerConfig w) {
  w.reaction = f.reaction;
  w.tracking = f.tracking;
}
void _w2f_examples_TurretConfig(tabledemo.TurretConfig w, tabledemo_fixed.TurretConfig f) {
  f.damage = w.damage;
  f.cooldown = w.cooldown;
  f.gunnerPresent = w.hasGunner;
  _w2f_examples_GunnerConfig(w.gunner, f.gunner);
}
void _f2w_examples_TurretConfig(tabledemo_fixed.TurretConfig f, tabledemo.TurretConfig w) {
  w.damage = f.damage;
  w.cooldown = f.cooldown;
  w.hasGunner = f.gunnerPresent;
  _f2w_examples_GunnerConfig(f.gunner, w.gunner);
}
void _w2f_examples_HullConfig(tabledemo.HullConfig w, tabledemo_fixed.HullConfig f) {
  f.health = w.health;
  f.mass = w.mass;
  for (var i = 0; i < f.turrets.length; i++) _w2f_examples_TurretConfig(w.turrets[i], f.turrets[i]);
}
void _f2w_examples_HullConfig(tabledemo_fixed.HullConfig f, tabledemo.HullConfig w) {
  w.health = f.health;
  w.mass = f.mass;
  for (var i = 0; i < f.turrets.length; i++) _f2w_examples_TurretConfig(f.turrets[i], w.turrets[i]);
}
void _w2f_examples_ScoreBoard(tabledemo.ScoreBoard w, dynamic f) {
  for (var i = 0; i < f.perTeam.length; i++) f.perTeam[i] = w.perTeam[i];
}
void _f2w_examples_ScoreBoard(dynamic f, tabledemo.ScoreBoard w) {
  for (var i = 0; i < f.perTeam.length; i++) w.perTeam[i] = f.perTeam[i];
}
void _w2f_examples_KeyedConfig(tabledemo.KeyedConfig w, tabledemo_fixed.KeyedConfig f) {
  for (var i = 0; i < f.teams.length; i++) _w2f_examples_TeamConfig(w.teams[i], f.teams[i]);
  for (var i = 0; i < f.hulls.length; i++) _w2f_examples_HullConfig(w.hulls[i], f.hulls[i]);
  _w2f_examples_ScoreBoard(w.scores, f.scores);
}
void _f2w_examples_KeyedConfig(tabledemo_fixed.KeyedConfig f, tabledemo.KeyedConfig w) {
  for (var i = 0; i < f.teams.length; i++) _f2w_examples_TeamConfig(f.teams[i], w.teams[i]);
  for (var i = 0; i < f.hulls.length; i++) _f2w_examples_HullConfig(f.hulls[i], w.hulls[i]);
  _f2w_examples_ScoreBoard(f.scores, w.scores);
}
void _w2f_examples_Buff(tabledemo.Buff w, dynamic f) {
  f.multiplier = w.multiplier;
}
void _f2w_examples_Buff(dynamic f, tabledemo.Buff w) {
  w.multiplier = f.multiplier;
}
void _w2f_examples_Debuff(tabledemo.Debuff w, dynamic f) {
  f.amount = w.amount;
}
void _f2w_examples_Debuff(dynamic f, tabledemo.Debuff w) {
  w.amount = f.amount;
}
void _w2f_examples_Effect(tabledemo.Effect w, dynamic f) {
  f.type = (w.armTag as dynamic);
  _w2f_examples_Buff(w.buff, f.buff);
  _w2f_examples_Debuff(w.debuff, f.debuff);
}
void _f2w_examples_Effect(dynamic f, tabledemo.Effect w) {
  w.armTag = (f.type as int);
  _f2w_examples_Buff(f.buff, w.buff);
  _f2w_examples_Debuff(f.debuff, w.debuff);
}
void _w2f_examples_Attachment(tabledemo.Attachment w, dynamic f) {
  f.slot = w.slot;
  f.power = w.power;
}
void _f2w_examples_Attachment(dynamic f, tabledemo.Attachment w) {
  w.slot = f.slot;
  w.power = f.power;
}
void _w2f_examples_WeaponConfig(tabledemo.WeaponConfig w, tabledemo_fixed.WeaponConfig f) {
  f.damage = w.damage;
  f.speed = w.speed;
  f.penetration = w.penetration;
  f.channel = w.channel;
  f.homing = w.homing;
  _w2f_examples_Effect(w.effect, f.effect);
}
void _f2w_examples_WeaponConfig(tabledemo_fixed.WeaponConfig f, tabledemo.WeaponConfig w) {
  w.damage = f.damage;
  w.speed = f.speed;
  w.penetration = f.penetration;
  w.channel = f.channel;
  w.homing = f.homing;
  _f2w_examples_Effect(f.effect, w.effect);
}
void _w2f_examples_LoadoutConfig(tabledemo.LoadoutConfig w, tabledemo_fixed.LoadoutConfig f) {
  f.grade = (w.grade as dynamic);
  f.gradesCount = w.grades.length;
  for (var i = 0; i < w.grades.length; i++) f.grades[i] = (w.grades[i] as int);
  for (var i = 0; i < f.podium.length; i++) f.podium[i] = (w.podium[i] as int);
  f.perks = w.perks;
  _w2f_examples_WeaponConfig(w.primary, f.primary);
  for (var i = 0; i < f.backups.length; i++) _w2f_examples_WeaponConfig(w.backups[i], f.backups[i]);
  f.attachmentsCount = w.attachments.length;
  for (var i = 0; i < w.attachments.length; i++) _w2f_examples_Attachment(w.attachments[i], f.attachments[i]);
}
void _f2w_examples_LoadoutConfig(tabledemo_fixed.LoadoutConfig f, tabledemo.LoadoutConfig w) {
  w.grade = (f.grade as dynamic);
  w.grades = List.generate(f.gradesCount, (i) => (f.grades[i] as dynamic));
  for (var i = 0; i < f.podium.length; i++) w.podium[i] = (f.podium[i] as dynamic);
  w.perks = f.perks;
  _f2w_examples_WeaponConfig(f.primary, w.primary);
  for (var i = 0; i < f.backups.length; i++) _f2w_examples_WeaponConfig(f.backups[i], w.backups[i]);
  w.attachments = List<tabledemo.Attachment>.generate(f.attachmentsCount, (i) { final e = tabledemo.Attachment(); _f2w_examples_Attachment(f.attachments[i], e); return e; });
}
void _w2f_examples_ProfileConfig(tabledemo.ProfileConfig w, tabledemo_fixed.ProfileConfig f) {
  _w2fUtf8(w.name, f.name, (n) => f.nameLength = n);
  _w2fBytes(w.icon, f.icon, (n) => f.iconLength = n);
  f.experience = w.experience;
  f.tilt = w.tilt;
  f.heading = w.heading;
  f.timestamp = w.timestamp;
  f.badge = w.badge;
  f.port = w.port;
  f.epoch = w.epoch;
  f.precision = w.precision;
  for (var i = 0; i < f.ratings.length; i++) f.ratings[i] = w.ratings[i];
  f.hasLoadout = w.hasLoadout;
  _w2f_examples_LoadoutConfig(w.loadout, f.loadout);
}
void _f2w_examples_ProfileConfig(tabledemo_fixed.ProfileConfig f, tabledemo.ProfileConfig w) {
  w.name = _f2wUtf8(f.name, f.nameLength);
  w.icon = _f2wBytes(f.icon, f.iconLength);
  w.experience = f.experience;
  w.tilt = f.tilt;
  w.heading = f.heading;
  w.timestamp = f.timestamp;
  w.badge = f.badge;
  w.port = f.port;
  w.epoch = f.epoch;
  w.precision = f.precision;
  for (var i = 0; i < f.ratings.length; i++) w.ratings[i] = f.ratings[i];
  w.hasLoadout = f.hasLoadout;
  _f2w_examples_LoadoutConfig(f.loadout, w.loadout);
}
void _w2f_examples_RootConfig(tabledemo.RootConfig w, tabledemo_fixed.RootConfig f) {
  _w2fUtf8(w.versionNote, f.versionNote, (n) => f.versionNoteLength = n);
  f.weaponsCount = w.weapons.length;
  for (var i = 0; i < w.weapons.length; i++) _w2f_examples_WeaponConfig(w.weapons[i], f.weapons[i]);
  f.profilesCount = w.profiles.length;
  for (var i = 0; i < w.profiles.length; i++) _w2f_examples_ProfileConfig(w.profiles[i], f.profiles[i]);
}
void _f2w_examples_RootConfig(tabledemo_fixed.RootConfig f, tabledemo.RootConfig w) {
  w.versionNote = _f2wUtf8(f.versionNote, f.versionNoteLength);
  w.weapons = List<tabledemo.WeaponConfig>.generate(f.weaponsCount, (i) { final e = tabledemo.WeaponConfig(); _f2w_examples_WeaponConfig(f.weapons[i], e); return e; });
  w.profiles = List<tabledemo.ProfileConfig>.generate(f.profilesCount, (i) { final e = tabledemo.ProfileConfig(); _f2w_examples_ProfileConfig(f.profiles[i], e); return e; });
}
void _w2f_examples_ArchiveConfig(tabledemo.ArchiveConfig w, tabledemo_fixed.ArchiveConfig f) {
  _w2f_examples_RootConfig(w.root, f.root);
  f.count = w.count;
}
void _f2w_examples_ArchiveConfig(tabledemo_fixed.ArchiveConfig f, tabledemo.ArchiveConfig w) {
  _f2w_examples_RootConfig(f.root, w.root);
  w.count = f.count;
}
void _w2f_examples_GunnerSettings(tabledemo.GunnerSettings w, tabledemo_fixed.GunnerSettings f) {
  f.reaction = w.reaction;
  f.tracking = w.tracking;
  _w2fUtf8(w.callsign, f.callsign, (n) => f.callsignLength = n);
}
void _f2w_examples_GunnerSettings(tabledemo_fixed.GunnerSettings f, tabledemo.GunnerSettings w) {
  w.reaction = f.reaction;
  w.tracking = f.tracking;
  w.callsign = _f2wUtf8(f.callsign, f.callsignLength);
}
void _w2f_examples_ShipEntry(tabledemo.ShipEntry w, tabledemo_fixed.ShipEntry f) {
  _w2fUtf8(w.displayName, f.displayName, (n) => f.displayNameLength = n);
  f.health = w.health;
  f.mass = w.mass;
  f.hardpointsCount = w.hardpoints.length;
  for (var i = 0; i < w.hardpoints.length; i++) f.hardpoints[i] = w.hardpoints[i];
  f.gunnerPresent = w.hasGunner;
  _w2f_examples_GunnerSettings(w.gunner, f.gunner);
}
void _f2w_examples_ShipEntry(tabledemo_fixed.ShipEntry f, tabledemo.ShipEntry w) {
  w.displayName = _f2wUtf8(f.displayName, f.displayNameLength);
  w.health = f.health;
  w.mass = f.mass;
  w.hardpoints = List<int>.generate(f.hardpointsCount, (i) => f.hardpoints[i]);
  w.hasGunner = f.gunnerPresent;
  _f2w_examples_GunnerSettings(f.gunner, w.gunner);
}
void _w2f_examples_GlobalSettings(tabledemo.GlobalSettings w, tabledemo_fixed.GlobalSettings f) {
  f.tickRate = w.tickRate;
  f.difficulty = w.difficulty;
  _w2fUtf8(w.buildNote, f.buildNote, (n) => f.buildNoteLength = n);
  for (var i = 0; i < f.spawnDelays.length; i++) f.spawnDelays[i] = w.spawnDelays[i];
}
void _f2w_examples_GlobalSettings(tabledemo_fixed.GlobalSettings f, tabledemo.GlobalSettings w) {
  w.tickRate = f.tickRate;
  w.difficulty = f.difficulty;
  w.buildNote = _f2wUtf8(f.buildNote, f.buildNoteLength);
  for (var i = 0; i < f.spawnDelays.length; i++) w.spawnDelays[i] = f.spawnDelays[i];
}
void _w2f_examples_PackConfig(tabledemo.PackConfig w, tabledemo_fixed.PackConfig f) {
  f.version = w.version;
  _w2f_examples_GlobalSettings(w.settings, f.settings);
  for (var i = 0; i < f.ships.length; i++) _w2f_examples_ShipEntry(w.ships[i], f.ships[i]);
  for (var i = 0; i < f.thresholds.length; i++) f.thresholds[i] = w.thresholds[i];
  f.reservesCount = w.reserves.length;
  for (var i = 0; i < w.reserves.length; i++) _w2f_examples_ShipEntry(w.reserves[i], f.reserves[i]);
}
void _f2w_examples_PackConfig(tabledemo_fixed.PackConfig f, tabledemo.PackConfig w) {
  w.version = f.version;
  _f2w_examples_GlobalSettings(f.settings, w.settings);
  for (var i = 0; i < f.ships.length; i++) _f2w_examples_ShipEntry(f.ships[i], w.ships[i]);
  for (var i = 0; i < f.thresholds.length; i++) w.thresholds[i] = f.thresholds[i];
  w.reserves = List<tabledemo.ShipEntry>.generate(f.reservesCount, (i) { final e = tabledemo.ShipEntry(); _f2w_examples_ShipEntry(f.reserves[i], e); return e; });
}
void _w2f_m1_Open(tblm1.Open w, tblm1_fixed.Open f) {
  _w2fUtf8(w.path, f.path, (n) => f.pathLength = n);
}
void _f2w_m1_Open(tblm1_fixed.Open f, tblm1.Open w) {
  w.path = _f2wUtf8(f.path, f.pathLength);
}
void _w2f_m1_Save(tblm1.Save w, tblm1_fixed.Save f) {
  _w2fUtf8(w.path, f.path, (n) => f.pathLength = n);
  f.force = w.force;
}
void _f2w_m1_Save(tblm1_fixed.Save f, tblm1.Save w) {
  w.path = _f2wUtf8(f.path, f.pathLength);
  w.force = f.force;
}
void _w2f_m1_Quit(tblm1.Quit w, tblm1_fixed.Quit f) {
  f.code = w.code;
}
void _f2w_m1_Quit(tblm1_fixed.Quit f, tblm1.Quit w) {
  w.code = f.code;
}
void _w2f_m1_Body(tblm1.Body w, tblm1_fixed.Body f) {
  f.type = (w.armTag as dynamic);
  _w2f_m1_Open(w.open, f.open);
  _w2f_m1_Save(w.save, f.save);
  _w2f_m1_Quit(w.quit, f.quit);
}
void _f2w_m1_Body(tblm1_fixed.Body f, tblm1.Body w) {
  w.armTag = (f.type as int);
  _f2w_m1_Open(f.open, w.open);
  _f2w_m1_Save(f.save, w.save);
  _f2w_m1_Quit(f.quit, w.quit);
}
void _w2f_m1_Msg(tblm1.Msg w, tblm1_fixed.Msg f) {
  f.seq = w.seq;
  _w2f_m1_Body(w.body, f.body);
}
void _f2w_m1_Msg(tblm1_fixed.Msg f, tblm1.Msg w) {
  w.seq = f.seq;
  _f2w_m1_Body(f.body, w.body);
}
void _w2f_m2_Open(tblm2.Open w, tblm2_fixed.Open f) {
  _w2fUtf8(w.path, f.path, (n) => f.pathLength = n);
  f.line = w.line;
}
void _f2w_m2_Open(tblm2_fixed.Open f, tblm2.Open w) {
  w.path = _f2wUtf8(f.path, f.pathLength);
  w.line = f.line;
}
void _w2f_m2_Close(tblm2.Close w, tblm2_fixed.Close f) {
  _w2fUtf8(w.path, f.path, (n) => f.pathLength = n);
}
void _f2w_m2_Close(tblm2_fixed.Close f, tblm2.Close w) {
  w.path = _f2wUtf8(f.path, f.pathLength);
}
void _w2f_m2_Quit(tblm2.Quit w, tblm2_fixed.Quit f) {
  f.code = w.code;
}
void _f2w_m2_Quit(tblm2_fixed.Quit f, tblm2.Quit w) {
  w.code = f.code;
}
void _w2f_m2_Body(tblm2.Body w, tblm2_fixed.Body f) {
  f.type = (w.armTag as dynamic);
  _w2f_m2_Close(w.close, f.close);
  _w2f_m2_Open(w.open, f.open);
  _w2f_m2_Quit(w.quit, f.quit);
}
void _f2w_m2_Body(tblm2_fixed.Body f, tblm2.Body w) {
  w.armTag = (f.type as int);
  _f2w_m2_Close(f.close, w.close);
  _f2w_m2_Open(f.open, w.open);
  _f2w_m2_Quit(f.quit, w.quit);
}
void _w2f_m2_Msg(tblm2.Msg w, tblm2_fixed.Msg f) {
  f.seq = w.seq;
  _w2f_m2_Body(w.body, f.body);
}
void _f2w_m2_Msg(tblm2_fixed.Msg f, tblm2.Msg w) {
  w.seq = f.seq;
  _f2w_m2_Body(f.body, w.body);
}
void _w2f_messages_Cursor(messagedemo.Cursor w, dynamic f) {
  f.line = w.line;
  f.column = w.column;
}
void _f2w_messages_Cursor(dynamic f, messagedemo.Cursor w) {
  w.line = f.line;
  w.column = f.column;
}
void _w2f_messages_Ping(messagedemo.Ping w, dynamic f) {
  f.nonce = w.nonce;
}
void _f2w_messages_Ping(dynamic f, messagedemo.Ping w) {
  w.nonce = f.nonce;
}
void _w2f_messages_User(messagedemo.User w, messagedemo_fixed.User f) {
  _w2fUtf8(w.name, f.name, (n) => f.nameLength = n);
}
void _f2w_messages_User(messagedemo_fixed.User f, messagedemo.User w) {
  w.name = _f2wUtf8(f.name, f.nameLength);
}
void _w2f_messages_Script(messagedemo.Script w, messagedemo_fixed.Script f) {
  _w2fUtf8(w.path, f.path, (n) => f.pathLength = n);
  f.line = w.line;
}
void _f2w_messages_Script(messagedemo_fixed.Script f, messagedemo.Script w) {
  w.path = _f2wUtf8(f.path, f.pathLength);
  w.line = f.line;
}
void _w2f_messages_Origin(messagedemo.Origin w, messagedemo_fixed.Origin f) {
  f.type = (w.armTag as dynamic);
  _w2f_messages_User(w.user, f.user);
  _w2f_messages_Script(w.script, f.script);
  f.pid = w.pid;
  _w2fUtf8(w.note, f.note, (n) => f.noteLength = n);
}
void _f2w_messages_Origin(messagedemo_fixed.Origin f, messagedemo.Origin w) {
  w.armTag = (f.type as int);
  _f2w_messages_User(f.user, w.user);
  _f2w_messages_Script(f.script, w.script);
  w.pid = f.pid;
  w.note = _f2wUtf8(f.note, f.noteLength);
}
void _w2f_messages_Selection(messagedemo.Selection w, messagedemo_fixed.Selection f) {
  _w2f_messages_Cursor(w.start, f.start);
  _w2f_messages_Cursor(w.stop, f.stop);
}
void _f2w_messages_Selection(messagedemo_fixed.Selection f, messagedemo.Selection w) {
  _f2w_messages_Cursor(f.start, w.start);
  _f2w_messages_Cursor(f.stop, w.stop);
}
void _w2f_messages_InsertText(messagedemo.InsertText w, messagedemo_fixed.InsertText f) {
  _w2f_messages_Cursor(w.at, f.at);
  _w2fUtf8(w.text, f.text, (n) => f.textLength = n);
  _w2f_messages_Origin(w.origin, f.origin);
  f.originsCount = w.origins.length;
  for (var i = 0; i < w.origins.length; i++) _w2f_messages_Origin(w.origins[i], f.origins[i]);
  f.modesPresent = w.hasModes;
  f.modesCount = w.modes.length;
  for (var i = 0; i < w.modes.length; i++) f.modes[i] = (w.modes[i] as int);
}
void _f2w_messages_InsertText(messagedemo_fixed.InsertText f, messagedemo.InsertText w) {
  _f2w_messages_Cursor(f.at, w.at);
  w.text = _f2wUtf8(f.text, f.textLength);
  _f2w_messages_Origin(f.origin, w.origin);
  w.origins = List<messagedemo.Origin>.generate(f.originsCount, (i) { final e = messagedemo.Origin(); _f2w_messages_Origin(f.origins[i], e); return e; });
  w.hasModes = f.modesPresent;
  w.modes = List.generate(f.modesCount, (i) => (f.modes[i] as dynamic));
}
void _w2f_messages_RemoveText(messagedemo.RemoveText w, messagedemo_fixed.RemoveText f) {
  _w2f_messages_Selection(w.span, f.span);
}
void _f2w_messages_RemoveText(messagedemo_fixed.RemoveText f, messagedemo.RemoveText w) {
  _f2w_messages_Selection(f.span, w.span);
}
void _w2f_messages_EditBody(messagedemo.EditBody w, messagedemo_fixed.EditBody f) {
  f.type = (w.armTag as dynamic);
  _w2f_messages_InsertText(w.insert, f.insert);
  _w2f_messages_RemoveText(w.remove, f.remove);
  f.tally = w.tally;
  f.marksCount = w.marks.length;
  for (var i = 0; i < w.marks.length; i++) f.marks[i] = w.marks[i];
  _w2fBytes(w.blob, f.blob, (n) => f.blobLength = n);
  f.mode = (w.mode as dynamic);
}
void _f2w_messages_EditBody(messagedemo_fixed.EditBody f, messagedemo.EditBody w) {
  w.armTag = (f.type as int);
  _f2w_messages_InsertText(f.insert, w.insert);
  _f2w_messages_RemoveText(f.remove, w.remove);
  w.tally = f.tally;
  w.marks = List<int>.generate(f.marksCount, (i) => f.marks[i]);
  w.blob = _f2wBytes(f.blob, f.blobLength);
  w.mode = (f.mode as dynamic);
}
void _w2f_messages_Edit(messagedemo.Edit w, messagedemo_fixed.Edit f) {
  f.revision = w.revision;
  _w2f_messages_EditBody(w.body, f.body);
}
void _f2w_messages_Edit(messagedemo_fixed.Edit f, messagedemo.Edit w) {
  w.revision = f.revision;
  _f2w_messages_EditBody(f.body, w.body);
}
void _w2f_messages_OpenDocument(messagedemo.OpenDocument w, messagedemo_fixed.OpenDocument f) {
  _w2fUtf8(w.path, f.path, (n) => f.pathLength = n);
  f.mode = (w.mode as dynamic);
  _w2f_messages_Cursor(w.cursor, f.cursor);
}
void _f2w_messages_OpenDocument(messagedemo_fixed.OpenDocument f, messagedemo.OpenDocument w) {
  w.path = _f2wUtf8(f.path, f.pathLength);
  w.mode = (f.mode as dynamic);
  _f2w_messages_Cursor(f.cursor, w.cursor);
}
void _w2f_messages_SaveDocument(messagedemo.SaveDocument w, messagedemo_fixed.SaveDocument f) {
  _w2fUtf8(w.path, f.path, (n) => f.pathLength = n);
  f.force = w.force;
}
void _f2w_messages_SaveDocument(messagedemo_fixed.SaveDocument f, messagedemo.SaveDocument w) {
  w.path = _f2wUtf8(f.path, f.pathLength);
  w.force = f.force;
}
void _w2f_messages_Transaction(messagedemo.Transaction w, messagedemo_fixed.Transaction f) {
  _w2fUtf8(w.reason, f.reason, (n) => f.reasonLength = n);
  f.editsCount = w.edits.length;
  for (var i = 0; i < w.edits.length; i++) _w2f_messages_Edit(w.edits[i], f.edits[i]);
  for (var i = 0; i < f.pending.length; i++) _w2f_messages_EditBody(w.pending[i], f.pending[i]);
  f.checkpointsPresent = w.hasCheckpoints;
  for (var i = 0; i < f.checkpoints.length; i++) f.checkpoints[i] = w.checkpoints[i];
  f.snapshotsPresent = w.hasSnapshots;
  for (var i = 0; i < f.snapshots.length; i++) _w2f_messages_Selection(w.snapshots[i], f.snapshots[i]);
}
void _f2w_messages_Transaction(messagedemo_fixed.Transaction f, messagedemo.Transaction w) {
  w.reason = _f2wUtf8(f.reason, f.reasonLength);
  w.edits = List<messagedemo.Edit>.generate(f.editsCount, (i) { final e = messagedemo.Edit(); _f2w_messages_Edit(f.edits[i], e); return e; });
  for (var i = 0; i < f.pending.length; i++) _f2w_messages_EditBody(f.pending[i], w.pending[i]);
  w.hasCheckpoints = f.checkpointsPresent;
  for (var i = 0; i < f.checkpoints.length; i++) w.checkpoints[i] = f.checkpoints[i];
  w.hasSnapshots = f.snapshotsPresent;
  for (var i = 0; i < f.snapshots.length; i++) _f2w_messages_Selection(f.snapshots[i], w.snapshots[i]);
}
void _w2f_messages_ToolBody(messagedemo.ToolBody w, messagedemo_fixed.ToolBody f) {
  f.type = (w.armTag as dynamic);
  _w2f_messages_OpenDocument(w.open, f.open);
  _w2f_messages_SaveDocument(w.save, f.save);
  _w2f_messages_Transaction(w.transact, f.transact);
  _w2f_messages_Ping(w.ping, f.ping);
  f.caps = w.caps;
  for (var i = 0; i < f.spans.length; i++) f.spans[i] = w.spans[i];
  _w2f_messages_Origin(w.origin, f.origin);
}
void _f2w_messages_ToolBody(messagedemo_fixed.ToolBody f, messagedemo.ToolBody w) {
  w.armTag = (f.type as int);
  _f2w_messages_OpenDocument(f.open, w.open);
  _f2w_messages_SaveDocument(f.save, w.save);
  _f2w_messages_Transaction(f.transact, w.transact);
  _f2w_messages_Ping(f.ping, w.ping);
  w.caps = f.caps;
  for (var i = 0; i < f.spans.length; i++) w.spans[i] = f.spans[i];
  _f2w_messages_Origin(f.origin, w.origin);
}
void _w2f_messages_ToolMessage(messagedemo.ToolMessage w, messagedemo_fixed.ToolMessage f) {
  f.sequence = w.sequence;
  _w2f_messages_ToolBody(w.body, f.body);
  f.historyCount = w.history.length;
  for (var i = 0; i < w.history.length; i++) _w2f_messages_ToolBody(w.history[i], f.history[i]);
  f.tracePresent = w.hasTrace;
  f.traceCount = w.trace.length;
  for (var i = 0; i < w.trace.length; i++) _w2f_messages_Script(w.trace[i], f.trace[i]);
  f.marksPresent = w.hasMarks;
  for (var i = 0; i < f.marks.length; i++) f.marks[i] = (w.marks[i] as int);
}
void _f2w_messages_ToolMessage(messagedemo_fixed.ToolMessage f, messagedemo.ToolMessage w) {
  w.sequence = f.sequence;
  _f2w_messages_ToolBody(f.body, w.body);
  w.history = List<messagedemo.ToolBody>.generate(f.historyCount, (i) { final e = messagedemo.ToolBody(); _f2w_messages_ToolBody(f.history[i], e); return e; });
  w.hasTrace = f.tracePresent;
  w.trace = List<messagedemo.Script>.generate(f.traceCount, (i) { final e = messagedemo.Script(); _f2w_messages_Script(f.trace[i], e); return e; });
  w.hasMarks = f.marksPresent;
  for (var i = 0; i < f.marks.length; i++) w.marks[i] = (f.marks[i] as dynamic);
}
void _w2f_p1_Link(tblp1.Link w, tblp1_fixed.Link f) {
  f.value = w.value;
  _w2fUtf8(w.tag, f.tag, (n) => f.tagLength = n);
}
void _f2w_p1_Link(tblp1_fixed.Link f, tblp1.Link w) {
  w.value = f.value;
  w.tag = _f2wUtf8(f.tag, f.tagLength);
}
void _w2f_p1_Chain(tblp1.Chain w, tblp1_fixed.Chain f) {
  _w2fUtf8(w.name, f.name, (n) => f.nameLength = n);
  _w2f_p1_Link(w.link, f.link);
}
void _f2w_p1_Chain(tblp1_fixed.Chain f, tblp1.Chain w) {
  w.name = _f2wUtf8(f.name, f.nameLength);
  _f2w_p1_Link(f.link, w.link);
}
void _w2f_p3_Link(tblp3.Link w, tblp3_fixed.Link f) {
  f.value = w.value;
  _w2fUtf8(w.tag, f.tag, (n) => f.tagLength = n);
}
void _f2w_p3_Link(tblp3_fixed.Link f, tblp3.Link w) {
  w.value = f.value;
  w.tag = _f2wUtf8(f.tag, f.tagLength);
}
void _w2f_p3_Chain(tblp3.Chain w, tblp3_fixed.Chain f) {
  _w2fUtf8(w.name, f.name, (n) => f.nameLength = n);
  f.linkPresent = w.hasLink;
  _w2f_p3_Link(w.link, f.link);
}
void _f2w_p3_Chain(tblp3_fixed.Chain f, tblp3.Chain w) {
  w.name = _f2wUtf8(f.name, f.nameLength);
  w.hasLink = f.linkPresent;
  _f2w_p3_Link(f.link, w.link);
}
void _w2f_r1_Boost(tblr1.Boost w, dynamic f) {
  f.power = w.power;
}
void _f2w_r1_Boost(dynamic f, tblr1.Boost w) {
  w.power = f.power;
}
void _w2f_r1_Ward(tblr1.Ward w, dynamic f) {
  f.charge = w.charge;
}
void _f2w_r1_Ward(dynamic f, tblr1.Ward w) {
  w.charge = f.charge;
}
void _w2f_r1_Buff(tblr1.Buff w, dynamic f) {
  f.multiplier = w.multiplier;
}
void _f2w_r1_Buff(dynamic f, tblr1.Buff w) {
  w.multiplier = f.multiplier;
}
void _w2f_r1_Effect(tblr1.Effect w, tblr1_fixed.Effect f) {
  f.type = (w.armTag as dynamic);
  _w2f_r1_Boost(w.boost, f.boost);
  _w2f_r1_Ward(w.ward, f.ward);
  f.amount = w.amount;
}
void _f2w_r1_Effect(tblr1_fixed.Effect f, tblr1.Effect w) {
  w.armTag = (f.type as int);
  _f2w_r1_Boost(f.boost, w.boost);
  _f2w_r1_Ward(f.ward, w.ward);
  w.amount = f.amount;
}
void _w2f_r1_Cfg(tblr1.Cfg w, tblr1_fixed.Cfg f) {
  f.grade = (w.grade as dynamic);
  _w2f_r1_Effect(w.effect, f.effect);
  _w2f_r1_Buff(w.buff, f.buff);
  f.gradesCount = w.grades.length;
  for (var i = 0; i < w.grades.length; i++) f.grades[i] = (w.grades[i] as int);
  for (var i = 0; i < f.tally.length; i++) f.tally[i] = w.tally[i];
}
void _f2w_r1_Cfg(tblr1_fixed.Cfg f, tblr1.Cfg w) {
  w.grade = (f.grade as dynamic);
  _f2w_r1_Effect(f.effect, w.effect);
  _f2w_r1_Buff(f.buff, w.buff);
  w.grades = List.generate(f.gradesCount, (i) => (f.grades[i] as dynamic));
  for (var i = 0; i < f.tally.length; i++) w.tally[i] = f.tally[i];
}
void _w2f_r2_Boost(tblr2.Boost w, dynamic f) {
  f.power = w.power;
}
void _f2w_r2_Boost(dynamic f, tblr2.Boost w) {
  w.power = f.power;
}
void _w2f_r2_Ward(tblr2.Ward w, dynamic f) {
  f.charge = w.charge;
}
void _f2w_r2_Ward(dynamic f, tblr2.Ward w) {
  w.charge = f.charge;
}
void _w2f_r2_Buff(tblr2.Buff w, dynamic f) {
  f.mult = w.mult;
}
void _f2w_r2_Buff(dynamic f, tblr2.Buff w) {
  w.mult = f.mult;
}
void _w2f_r2_Effect(tblr2.Effect w, tblr2_fixed.Effect f) {
  f.type = (w.armTag as dynamic);
  _w2f_r2_Boost(w.boost, f.boost);
  _w2f_r2_Ward(w.shield, f.shield);
  f.amount = w.amount;
}
void _f2w_r2_Effect(tblr2_fixed.Effect f, tblr2.Effect w) {
  w.armTag = (f.type as int);
  _f2w_r2_Boost(f.boost, w.boost);
  _f2w_r2_Ward(f.shield, w.shield);
  w.amount = f.amount;
}
void _w2f_r2_Cfg(tblr2.Cfg w, tblr2_fixed.Cfg f) {
  f.grade = (w.grade as dynamic);
  _w2f_r2_Effect(w.effect, f.effect);
  _w2f_r2_Buff(w.buff, f.buff);
  f.gradesCount = w.grades.length;
  for (var i = 0; i < w.grades.length; i++) f.grades[i] = (w.grades[i] as int);
  for (var i = 0; i < f.tally.length; i++) f.tally[i] = w.tally[i];
}
void _f2w_r2_Cfg(tblr2_fixed.Cfg f, tblr2.Cfg w) {
  w.grade = (f.grade as dynamic);
  _f2w_r2_Effect(f.effect, w.effect);
  _f2w_r2_Buff(f.buff, w.buff);
  w.grades = List.generate(f.gradesCount, (i) => (f.grades[i] as dynamic));
  for (var i = 0; i < f.tally.length; i++) w.tally[i] = f.tally[i];
}
void _w2f_v1_Boost(tblv1.Boost w, dynamic f) {
  f.power = w.power;
}
void _f2w_v1_Boost(dynamic f, tblv1.Boost w) {
  w.power = f.power;
}
void _w2f_v1_Ward(tblv1.Ward w, dynamic f) {
  f.charge = w.charge;
}
void _f2w_v1_Ward(dynamic f, tblv1.Ward w) {
  w.charge = f.charge;
}
void _w2f_v1_Effect(tblv1.Effect w, dynamic f) {
  f.type = (w.armTag as dynamic);
  _w2f_v1_Boost(w.boost, f.boost);
  _w2f_v1_Ward(w.ward, f.ward);
}
void _f2w_v1_Effect(dynamic f, tblv1.Effect w) {
  w.armTag = (f.type as int);
  _f2w_v1_Boost(f.boost, w.boost);
  _f2w_v1_Ward(f.ward, w.ward);
}
void _w2f_v1_Inner(tblv1.Inner w, dynamic f) {
  f.factor = w.factor;
}
void _f2w_v1_Inner(dynamic f, tblv1.Inner w) {
  w.factor = f.factor;
}
void _w2f_v1_Cell(tblv1.Cell w, tblv1_fixed.Cell f) {
  f.power = w.power;
  _w2fUtf8(w.label, f.label, (n) => f.labelLength = n);
}
void _f2w_v1_Cell(tblv1_fixed.Cell f, tblv1.Cell w) {
  w.power = f.power;
  w.label = _f2wUtf8(f.label, f.labelLength);
}
void _w2f_v1_Cfg(tblv1.Cfg w, tblv1_fixed.Cfg f) {
  f.a = w.a;
  f.b = w.b;
  f.mode = (w.mode as dynamic);
  _w2fUtf8(w.name, f.name, (n) => f.nameLength = n);
  _w2f_v1_Inner(w.inner, f.inner);
  f.itemsCount = w.items.length;
  for (var i = 0; i < w.items.length; i++) f.items[i] = w.items[i];
  f.grade = (w.grade as dynamic);
  f.gradesCount = w.grades.length;
  for (var i = 0; i < w.grades.length; i++) f.grades[i] = (w.grades[i] as int);
  for (var i = 0; i < f.podium.length; i++) f.podium[i] = (w.podium[i] as int);
  f.slotsCount = w.slots.length;
  for (var i = 0; i < w.slots.length; i++) f.slots[i] = w.slots[i];
  for (var i = 0; i < f.tally.length; i++) f.tally[i] = w.tally[i];
  _w2f_v1_Effect(w.effect, f.effect);
  for (var i = 0; i < f.bank.length; i++) _w2f_v1_Cell(w.bank[i], f.bank[i]);
  for (var i = 0; i < f.tokens.length; i++) f.tokens[i] = w.tokens[i];
  for (var i = 0; i < f.ranks.length; i++) f.ranks[i] = (w.ranks[i] as int);
  for (var i = 0; i < f.ledger.length; i++) f.ledger[i] = w.ledger[i];
  f.extraPresent = w.hasExtra;
  _w2f_v1_Inner(w.extra, f.extra);
  f.tierPresent = w.hasTier;
  f.tier = w.tier;
  f.markPresent = w.hasMark;
  f.mark = (w.mark as dynamic);
}
void _f2w_v1_Cfg(tblv1_fixed.Cfg f, tblv1.Cfg w) {
  w.a = f.a;
  w.b = f.b;
  w.mode = (f.mode as dynamic);
  w.name = _f2wUtf8(f.name, f.nameLength);
  _f2w_v1_Inner(f.inner, w.inner);
  w.items = List<int>.generate(f.itemsCount, (i) => f.items[i]);
  w.grade = (f.grade as dynamic);
  w.grades = List.generate(f.gradesCount, (i) => (f.grades[i] as dynamic));
  for (var i = 0; i < f.podium.length; i++) w.podium[i] = (f.podium[i] as dynamic);
  w.slots = List<int>.generate(f.slotsCount, (i) => f.slots[i]);
  for (var i = 0; i < f.tally.length; i++) w.tally[i] = f.tally[i];
  _f2w_v1_Effect(f.effect, w.effect);
  for (var i = 0; i < f.bank.length; i++) _f2w_v1_Cell(f.bank[i], w.bank[i]);
  for (var i = 0; i < f.tokens.length; i++) w.tokens[i] = f.tokens[i];
  for (var i = 0; i < f.ranks.length; i++) w.ranks[i] = (f.ranks[i] as dynamic);
  for (var i = 0; i < f.ledger.length; i++) w.ledger[i] = f.ledger[i];
  w.hasExtra = f.extraPresent;
  _f2w_v1_Inner(f.extra, w.extra);
  w.hasTier = f.tierPresent;
  w.tier = f.tier;
  w.hasMark = f.markPresent;
  w.mark = (f.mark as dynamic);
}
void _w2f_v2_Boost(tblv2.Boost w, dynamic f) {
  f.power = w.power;
}
void _f2w_v2_Boost(dynamic f, tblv2.Boost w) {
  w.power = f.power;
}
void _w2f_v2_Hex(tblv2.Hex w, dynamic f) {
  f.level = w.level;
}
void _f2w_v2_Hex(dynamic f, tblv2.Hex w) {
  w.level = f.level;
}
void _w2f_v2_Ward(tblv2.Ward w, dynamic f) {
  f.charge = w.charge;
}
void _f2w_v2_Ward(dynamic f, tblv2.Ward w) {
  w.charge = f.charge;
}
void _w2f_v2_Effect(tblv2.Effect w, dynamic f) {
  f.type = (w.armTag as dynamic);
  _w2f_v2_Boost(w.boost, f.boost);
  _w2f_v2_Hex(w.hex, f.hex);
  _w2f_v2_Ward(w.ward, f.ward);
}
void _f2w_v2_Effect(dynamic f, tblv2.Effect w) {
  w.armTag = (f.type as int);
  _f2w_v2_Boost(f.boost, w.boost);
  _f2w_v2_Hex(f.hex, w.hex);
  _f2w_v2_Ward(f.ward, w.ward);
}
void _w2f_v2_Inner(tblv2.Inner w, dynamic f) {
  f.factor = w.factor;
  f.gain = w.gain;
}
void _f2w_v2_Inner(dynamic f, tblv2.Inner w) {
  w.factor = f.factor;
  w.gain = f.gain;
}
void _w2f_v2_Cell(tblv2.Cell w, tblv2_fixed.Cell f) {
  f.power = w.power;
  _w2fUtf8(w.label, f.label, (n) => f.labelLength = n);
}
void _f2w_v2_Cell(tblv2_fixed.Cell f, tblv2.Cell w) {
  w.power = f.power;
  w.label = _f2wUtf8(f.label, f.labelLength);
}
void _w2f_v2_Cfg(tblv2.Cfg w, tblv2_fixed.Cfg f) {
  f.a = w.a;
  f.c = w.c;
  f.mode = (w.mode as dynamic);
  _w2fUtf8(w.title, f.title, (n) => f.titleLength = n);
  _w2f_v2_Inner(w.inner, f.inner);
  f.itemsCount = w.items.length;
  for (var i = 0; i < w.items.length; i++) f.items[i] = w.items[i];
  f.grade = (w.grade as dynamic);
  f.gradesCount = w.grades.length;
  for (var i = 0; i < w.grades.length; i++) f.grades[i] = (w.grades[i] as int);
  for (var i = 0; i < f.podium.length; i++) f.podium[i] = (w.podium[i] as int);
  f.slotsCount = w.slots.length;
  for (var i = 0; i < w.slots.length; i++) f.slots[i] = w.slots[i];
  for (var i = 0; i < f.tally.length; i++) f.tally[i] = w.tally[i];
  _w2f_v2_Effect(w.effect, f.effect);
  for (var i = 0; i < f.bank.length; i++) _w2f_v2_Cell(w.bank[i], f.bank[i]);
  for (var i = 0; i < f.tokens.length; i++) f.tokens[i] = w.tokens[i];
  for (var i = 0; i < f.ranks.length; i++) f.ranks[i] = (w.ranks[i] as int);
  for (var i = 0; i < f.ledger.length; i++) f.ledger[i] = w.ledger[i];
  f.extraPresent = w.hasExtra;
  _w2f_v2_Inner(w.extra, f.extra);
  f.tierPresent = w.hasTier;
  f.tier = w.tier;
  f.markPresent = w.hasMark;
  f.mark = (w.mark as dynamic);
}
void _f2w_v2_Cfg(tblv2_fixed.Cfg f, tblv2.Cfg w) {
  w.a = f.a;
  w.c = f.c;
  w.mode = (f.mode as dynamic);
  w.title = _f2wUtf8(f.title, f.titleLength);
  _f2w_v2_Inner(f.inner, w.inner);
  w.items = List<int>.generate(f.itemsCount, (i) => f.items[i]);
  w.grade = (f.grade as dynamic);
  w.grades = List.generate(f.gradesCount, (i) => (f.grades[i] as dynamic));
  for (var i = 0; i < f.podium.length; i++) w.podium[i] = (f.podium[i] as dynamic);
  w.slots = List<int>.generate(f.slotsCount, (i) => f.slots[i]);
  for (var i = 0; i < f.tally.length; i++) w.tally[i] = f.tally[i];
  _f2w_v2_Effect(f.effect, w.effect);
  for (var i = 0; i < f.bank.length; i++) _f2w_v2_Cell(f.bank[i], w.bank[i]);
  for (var i = 0; i < f.tokens.length; i++) w.tokens[i] = f.tokens[i];
  for (var i = 0; i < f.ranks.length; i++) w.ranks[i] = (f.ranks[i] as dynamic);
  for (var i = 0; i < f.ledger.length; i++) w.ledger[i] = f.ledger[i];
  w.hasExtra = f.extraPresent;
  _f2w_v2_Inner(f.extra, w.extra);
  w.hasTier = f.tierPresent;
  w.tier = f.tier;
  w.hasMark = f.markPresent;
  w.mark = (f.mark as dynamic);
}
void _w2f_vocab_Wide00(vocabdemo.Wide00 w, vocabdemo_fixed.Wide00 f) {
  f.field0000 = w.field0000;
  f.field0001 = w.field0001;
  f.field0002 = w.field0002;
  f.field0003 = w.field0003;
  f.field0004 = w.field0004;
  f.field0005 = w.field0005;
  f.field0006 = w.field0006;
  f.field0007 = w.field0007;
  f.field0008 = w.field0008;
  f.field0009 = w.field0009;
  f.field0010 = w.field0010;
  f.field0011 = w.field0011;
  f.field0012 = w.field0012;
}
void _f2w_vocab_Wide00(vocabdemo_fixed.Wide00 f, vocabdemo.Wide00 w) {
  w.field0000 = f.field0000;
  w.field0001 = f.field0001;
  w.field0002 = f.field0002;
  w.field0003 = f.field0003;
  w.field0004 = f.field0004;
  w.field0005 = f.field0005;
  w.field0006 = f.field0006;
  w.field0007 = f.field0007;
  w.field0008 = f.field0008;
  w.field0009 = f.field0009;
  w.field0010 = f.field0010;
  w.field0011 = f.field0011;
  w.field0012 = f.field0012;
}
void _w2f_vocab_Wide09(vocabdemo.Wide09 w, vocabdemo_fixed.Wide09 f) {
  f.field0900 = w.field0900;
  f.field0901 = w.field0901;
  f.field0902 = w.field0902;
  f.field0903 = w.field0903;
  f.field0904 = w.field0904;
  f.field0905 = w.field0905;
  f.field0906 = w.field0906;
  f.field0907 = w.field0907;
  f.field0908 = w.field0908;
  f.field0909 = w.field0909;
  f.field0910 = w.field0910;
  f.field0911 = w.field0911;
  f.field0912 = w.field0912;
}
void _f2w_vocab_Wide09(vocabdemo_fixed.Wide09 f, vocabdemo.Wide09 w) {
  w.field0900 = f.field0900;
  w.field0901 = f.field0901;
  w.field0902 = f.field0902;
  w.field0903 = f.field0903;
  w.field0904 = f.field0904;
  w.field0905 = f.field0905;
  w.field0906 = f.field0906;
  w.field0907 = f.field0907;
  w.field0908 = f.field0908;
  w.field0909 = f.field0909;
  w.field0910 = f.field0910;
  w.field0911 = f.field0911;
  w.field0912 = f.field0912;
}
void _w2f_vocab9_Wide00(vocab9demo.Wide00 w, vocab9demo_fixed.Wide00 f) {
  f.field0000 = w.field0000;
  f.field0001 = w.field0001;
  f.field0002 = w.field0002;
  f.field0003 = w.field0003;
  f.field0004 = w.field0004;
  f.field0005 = w.field0005;
  f.field0006 = w.field0006;
  f.field0007 = w.field0007;
  f.field0008 = w.field0008;
  f.field0009 = w.field0009;
  f.field0010 = w.field0010;
  f.field0011 = w.field0011;
  f.field0012 = w.field0012;
}
void _f2w_vocab9_Wide00(vocab9demo_fixed.Wide00 f, vocab9demo.Wide00 w) {
  w.field0000 = f.field0000;
  w.field0001 = f.field0001;
  w.field0002 = f.field0002;
  w.field0003 = f.field0003;
  w.field0004 = f.field0004;
  w.field0005 = f.field0005;
  w.field0006 = f.field0006;
  w.field0007 = f.field0007;
  w.field0008 = f.field0008;
  w.field0009 = f.field0009;
  w.field0010 = f.field0010;
  w.field0011 = f.field0011;
  w.field0012 = f.field0012;
}
void _w2f_vocab9_Wide19(vocab9demo.Wide19 w, vocab9demo_fixed.Wide19 f) {
  f.field1900 = w.field1900;
  f.field1901 = w.field1901;
  f.field1902 = w.field1902;
  f.field1903 = w.field1903;
  f.field1904 = w.field1904;
  f.field1905 = w.field1905;
  f.field1906 = w.field1906;
  f.field1907 = w.field1907;
  f.field1908 = w.field1908;
  f.field1909 = w.field1909;
  f.field1910 = w.field1910;
  f.field1911 = w.field1911;
  f.field1912 = w.field1912;
}
void _f2w_vocab9_Wide19(vocab9demo_fixed.Wide19 f, vocab9demo.Wide19 w) {
  w.field1900 = f.field1900;
  w.field1901 = f.field1901;
  w.field1902 = f.field1902;
  w.field1903 = f.field1903;
  w.field1904 = f.field1904;
  w.field1905 = f.field1905;
  w.field1906 = f.field1906;
  w.field1907 = f.field1907;
  w.field1908 = f.field1908;
  w.field1909 = f.field1909;
  w.field1910 = f.field1910;
  w.field1911 = f.field1911;
  w.field1912 = f.field1912;
}
void _w2f_wide_Line(widedemo.Line w, dynamic f) {
  _w2fWide(w.text, f.text, (n) => f.textLength = n);
}
void _f2w_wide_Line(dynamic f, widedemo.Line w) {
  w.text = _f2wWide(f.text, f.textLength);
}
void _w2f_wide_Body(widedemo.Body w, widedemo_fixed.Body f) {
  f.type = (w.armTag as dynamic);
  _w2fWide(w.wide, f.wide, (n) => f.wideLength = n);
  _w2fUtf8(w.narrow, f.narrow, (n) => f.narrowLength = n);
  f.tally = w.tally;
}
void _f2w_wide_Body(widedemo_fixed.Body f, widedemo.Body w) {
  w.armTag = (f.type as int);
  w.wide = _f2wWide(f.wide, f.wideLength);
  w.narrow = _f2wUtf8(f.narrow, f.narrowLength);
  w.tally = f.tally;
}
void _w2f_wide_Caption(widedemo.Caption w, widedemo_fixed.Caption f) {
  _w2fWide(w.title, f.title, (n) => f.titleLength = n);
  _w2f_wide_Line(w.line, f.line);
  f.linesCount = w.lines.length;
  for (var i = 0; i < w.lines.length; i++) _w2f_wide_Line(w.lines[i], f.lines[i]);
  _w2f_wide_Body(w.body, f.body);
}
void _f2w_wide_Caption(widedemo_fixed.Caption f, widedemo.Caption w) {
  w.title = _f2wWide(f.title, f.titleLength);
  _f2w_wide_Line(f.line, w.line);
  w.lines = List<widedemo.Line>.generate(f.linesCount, (i) { final e = widedemo.Line(); _f2w_wide_Line(f.lines[i], e); return e; });
  _f2w_wide_Body(f.body, w.body);
}
void _w2f_wide_Stamp(widedemo.Stamp w, widedemo_fixed.Stamp f) {
  _w2fWide(w.label, f.label, (n) => f.labelLength = n);
  f.seq = w.seq;
}
void _f2w_wide_Stamp(widedemo_fixed.Stamp f, widedemo.Stamp w) {
  w.label = _f2wWide(f.label, f.labelLength);
  w.seq = f.seq;
}

Uint8List? wireResave(String unit, String root, Uint8List wire) {
  switch ('$unit $root') {
    case 'featurewitnesses Root':
      return _wireResaveOne(wire, feature.TableWireReport.new, (rep) => (rep.refused, rep.refusal), feature.rootWireLoad, feature.rootWireSave);
    case 'featurewitnesses ArrayRoot':
      return _wireResaveOne(wire, feature.TableWireReport.new, (rep) => (rep.refused, rep.refusal), feature.arrayRootWireLoad, feature.arrayRootWireSave);
    case 'tblg1 Guarded':
      return _wireResaveOne(wire, tblg1.TableWireReport.new, (rep) => (rep.refused, rep.refusal), tblg1.guardedWireLoad, tblg1.guardedWireSave);
    case 'tblw1 Fleet':
      return _wireResaveOne(wire, tblw1.TableWireReport.new, (rep) => (rep.refused, rep.refusal), tblw1.fleetWireLoad, tblw1.fleetWireSave);
    case 'tblw2 Fleet':
      return _wireResaveOne(wire, tblw2.TableWireReport.new, (rep) => (rep.refused, rep.refusal), tblw2.fleetWireLoad, tblw2.fleetWireSave);
    case 'graphdemo Scene':
      return _wireResaveOne(wire, graphdemo.TableWireReport.new, (rep) => (rep.refused, rep.refusal), graphdemo.sceneWireLoad, graphdemo.sceneWireSave);
    case 'blobdemo Catalog':
      return _wireResaveOne(wire, blobdemo.TableWireReport.new, (rep) => (rep.refused, rep.refusal), blobdemo.catalogWireLoad, blobdemo.catalogWireSave);
    case 'listdemo Save':
      return _wireResaveOne(wire, listdemo.TableWireReport.new, (rep) => (rep.refused, rep.refusal), listdemo.saveWireLoad, listdemo.saveWireSave);
    case 'listdemo Album':
      return _wireResaveOne(wire, listdemo.TableWireReport.new, (rep) => (rep.refused, rep.refusal), listdemo.albumWireLoad, listdemo.albumWireSave);
    case 'listdemo Sheet':
      return _wireResaveOne(wire, listdemo.TableWireReport.new, (rep) => (rep.refused, rep.refusal), listdemo.sheetWireLoad, listdemo.sheetWireSave);
    case 'listdemo Unbounded':
      return _wireResaveOne(wire, listdemo.TableWireReport.new, (rep) => (rep.refused, rep.refusal), listdemo.unboundedWireLoad, listdemo.unboundedWireSave);
    case 'mapdemo Cells':
      return _wireResaveOne(wire, mapdemo.TableWireReport.new, (rep) => (rep.refused, rep.refusal), mapdemo.cellsWireLoad, mapdemo.cellsWireSave);
    case 'mapdemo Chunks':
      return _wireResaveOne(wire, mapdemo.TableWireReport.new, (rep) => (rep.refused, rep.refusal), mapdemo.chunksWireLoad, mapdemo.chunksWireSave);
    case 'mapdemo Crews':
      return _wireResaveOne(wire, mapdemo.TableWireReport.new, (rep) => (rep.refused, rep.refusal), mapdemo.crewsWireLoad, mapdemo.crewsWireSave);
    case 'mapdemo Depth':
      return _wireResaveOne(wire, mapdemo.TableWireReport.new, (rep) => (rep.refused, rep.refusal), mapdemo.depthWireLoad, mapdemo.depthWireSave);
    case 'mapdemo Docs':
      return _wireResaveOne(wire, mapdemo.TableWireReport.new, (rep) => (rep.refused, rep.refusal), mapdemo.docsWireLoad, mapdemo.docsWireSave);
    case 'mapdemo Fleet':
      return _wireResaveOne(wire, mapdemo.TableWireReport.new, (rep) => (rep.refused, rep.refusal), mapdemo.fleetWireLoad, mapdemo.fleetWireSave);
    case 'mapdemo Pairs':
      return _wireResaveOne(wire, mapdemo.TableWireReport.new, (rep) => (rep.refused, rep.refusal), mapdemo.pairsWireLoad, mapdemo.pairsWireSave);
    case 'mapdemo Runs':
      return _wireResaveOne(wire, mapdemo.TableWireReport.new, (rep) => (rep.refused, rep.refusal), mapdemo.runsWireLoad, mapdemo.runsWireSave);
    case 'mapdemo Slots':
      return _wireResaveOne(wire, mapdemo.TableWireReport.new, (rep) => (rep.refused, rep.refusal), mapdemo.slotsWireLoad, mapdemo.slotsWireSave);
    case 'mapdemo Spans':
      return _wireResaveOne(wire, mapdemo.TableWireReport.new, (rep) => (rep.refused, rep.refusal), mapdemo.spansWireLoad, mapdemo.spansWireSave);
    case 'mapdemo Text':
      return _wireResaveOne(wire, mapdemo.TableWireReport.new, (rep) => (rep.refused, rep.refusal), mapdemo.textWireLoad, mapdemo.textWireSave);
    case 'mapdemo Trails':
      return _wireResaveOne(wire, mapdemo.TableWireReport.new, (rep) => (rep.refused, rep.refusal), mapdemo.trailsWireLoad, mapdemo.trailsWireSave);
    case 'widelist Wide':
      return _wireResaveOne(wire, widelist.TableWireReport.new, (rep) => (rep.refused, rep.refusal), widelist.wideWireLoad, widelist.wideWireSave);
    case 'listdemo Army':
      return _wireResaveOne(wire, listdemo.TableWireReport.new, (rep) => (rep.refused, rep.refusal), listdemo.armyWireLoad, listdemo.armyWireSave);
    case 'listdemo Mixed':
      return _wireResaveOne(wire, listdemo.TableWireReport.new, (rep) => (rep.refused, rep.refusal), listdemo.mixedWireLoad, listdemo.mixedWireSave);
    case 'streamdemo Feed':
      return _wireResaveOne(wire, streamdemo.TableWireReport.new, (rep) => (rep.refused, rep.refusal), streamdemo.feedWireLoad, streamdemo.feedWireSave);
    case 'tabledemo WideBlob':
      return _wireResaveOne(wire, tabledemo.TableWireReport.new, (rep) => (rep.refused, rep.refusal), tabledemo.wideBlobWireLoad, tabledemo.wideBlobWireSave);
    case 'graphdemo ListNode':
      return _wireResaveOne(wire, graphdemo.TableWireReport.new, (rep) => (rep.refused, rep.refusal), graphdemo.listNodeWireLoad, graphdemo.listNodeWireSave);
    case 'listdemo Bounded':
      return _wireResaveOne(wire, listdemo.TableWireReport.new, (rep) => (rep.refused, rep.refusal), listdemo.boundedWireLoad, listdemo.boundedWireSave);
    case 'listdemo Ints':
      return _wireResaveOne(wire, listdemo.TableWireReport.new, (rep) => (rep.refused, rep.refusal), listdemo.intsWireLoad, listdemo.intsWireSave);
    case 'listdemo Squad':
      return _wireResaveOne(wire, listdemo.TableWireReport.new, (rep) => (rep.refused, rep.refusal), listdemo.squadWireLoad, listdemo.squadWireSave);
    case 'tblp2 Chain':
      return _wireResaveOne(wire, tblp2.TableWireReport.new, (rep) => (rep.refused, rep.refusal), tblp2.chainWireLoad, tblp2.chainWireSave);
    case 'backenddemo LoginRequest':
      if (wire.isNotEmpty && wire[0] == 1) {
        final rep = backenddemo.TableWireReport();
        final w = backenddemo.loginRequestWireLoad(wire, rep);
        if (rep.refused) return null;
        final f = backenddemo_fixed.LoginRequest();
        _w2f_backend_LoginRequest(w, f);
        return _fixedSaveOne(f, backend.loginRequestFixedMeasure, backend.loginRequestFixedSave);
      }
      return _fixedResave(wire, backenddemo_fixed.LoginRequest.new, backend.loginRequestFixedNewPlan, backenddemo_fixed.TableFixedReport.new, (rep) => rep.refused != 0, backend.loginRequestFixedLoad, backend.loginRequestFixedMeasure, backend.loginRequestFixedSave);
    case 'backenddemo MatchResult':
      if (wire.isNotEmpty && wire[0] == 1) {
        final rep = backenddemo.TableWireReport();
        final w = backenddemo.matchResultWireLoad(wire, rep);
        if (rep.refused) return null;
        final f = backenddemo_fixed.MatchResult();
        _w2f_backend_MatchResult(w, f);
        return _fixedSaveOne(f, backend.matchResultFixedMeasure, backend.matchResultFixedSave);
      }
      return _fixedResave(wire, backenddemo_fixed.MatchResult.new, backend.matchResultFixedNewPlan, backenddemo_fixed.TableFixedReport.new, (rep) => rep.refused != 0, backend.matchResultFixedLoad, backend.matchResultFixedMeasure, backend.matchResultFixedSave);
    case 'backenddemo StorePurchase':
      if (wire.isNotEmpty && wire[0] == 1) {
        final rep = backenddemo.TableWireReport();
        final w = backenddemo.storePurchaseWireLoad(wire, rep);
        if (rep.refused) return null;
        final f = backenddemo_fixed.StorePurchase();
        _w2f_backend_StorePurchase(w, f);
        return _fixedSaveOne(f, backend.storePurchaseFixedMeasure, backend.storePurchaseFixedSave);
      }
      return _fixedResave(wire, backenddemo_fixed.StorePurchase.new, backend.storePurchaseFixedNewPlan, backenddemo_fixed.TableFixedReport.new, (rep) => rep.refused != 0, backend.storePurchaseFixedLoad, backend.storePurchaseFixedMeasure, backend.storePurchaseFixedSave);
    case 'messagedemo ToolMessage':
      if (wire.isNotEmpty && wire[0] == 1) {
        final rep = messagedemo.TableWireReport();
        final w = messagedemo.toolMessageWireLoad(wire, rep);
        if (rep.refused) return null;
        final f = messagedemo_fixed.ToolMessage();
        _w2f_messages_ToolMessage(w, f);
        return _fixedSaveOne(f, messages_fixed.toolMessageFixedMeasure, messages_fixed.toolMessageFixedSave);
      }
      return _fixedResave(wire, messagedemo_fixed.ToolMessage.new, messages_fixed.toolMessageFixedNewPlan, messagedemo_fixed.TableFixedReport.new, (rep) => rep.refused != 0, messages_fixed.toolMessageFixedLoad, messages_fixed.toolMessageFixedMeasure, messages_fixed.toolMessageFixedSave);
    case 'scalars SimState':
      if (wire.isNotEmpty && wire[0] == 1) {
        final rep = scalardemo.TableWireReport();
        final w = scalardemo.simStateWireLoad(wire, rep);
        if (rep.refused) return null;
        final f = scalardemo_fixed.SimState();
        _w2f_scalars_SimState(w, f);
        return _fixedSaveOne(f, scalars_fixed.simStateFixedMeasure, scalars_fixed.simStateFixedSave);
      }
      return _fixedResave(wire, scalardemo_fixed.SimState.new, scalars_fixed.simStateFixedNewPlan, scalardemo_fixed.TableFixedReport.new, (rep) => rep.refused != 0, scalars_fixed.simStateFixedLoad, scalars_fixed.simStateFixedMeasure, scalars_fixed.simStateFixedSave);
    case 'tabledemo ArchiveConfig':
      if (wire.isNotEmpty && wire[0] == 1) {
        final rep = tabledemo.TableWireReport();
        final w = tabledemo.archiveConfigWireLoad(wire, rep);
        if (rep.refused) return null;
        final f = tabledemo_fixed.ArchiveConfig();
        _w2f_examples_ArchiveConfig(w, f);
        return _fixedSaveOne(f, nested_fixed.archiveConfigFixedMeasure, nested_fixed.archiveConfigFixedSave);
      }
      return _fixedResave(wire, tabledemo_fixed.ArchiveConfig.new, nested_fixed.archiveConfigFixedNewPlan, tabledemo_fixed.TableFixedReport.new, (rep) => rep.refused != 0, nested_fixed.archiveConfigFixedLoad, nested_fixed.archiveConfigFixedMeasure, nested_fixed.archiveConfigFixedSave);
    case 'tabledemo KeyedConfig':
      if (wire.isNotEmpty && wire[0] == 1) {
        final rep = tabledemo.TableWireReport();
        final w = tabledemo.keyedConfigWireLoad(wire, rep);
        if (rep.refused) return null;
        final f = tabledemo_fixed.KeyedConfig();
        _w2f_examples_KeyedConfig(w, f);
        return _fixedSaveOne(f, keyed_fixed.keyedConfigFixedMeasure, keyed_fixed.keyedConfigFixedSave);
      }
      return _fixedResave(wire, tabledemo_fixed.KeyedConfig.new, keyed_fixed.keyedConfigFixedNewPlan, tabledemo_fixed.TableFixedReport.new, (rep) => rep.refused != 0, keyed_fixed.keyedConfigFixedLoad, keyed_fixed.keyedConfigFixedMeasure, keyed_fixed.keyedConfigFixedSave);
    case 'tabledemo LoadoutConfig':
      if (wire.isNotEmpty && wire[0] == 1) {
        final rep = tabledemo.TableWireReport();
        final w = tabledemo.loadoutConfigWireLoad(wire, rep);
        if (rep.refused) return null;
        final f = tabledemo_fixed.LoadoutConfig();
        _w2f_examples_LoadoutConfig(w, f);
        return _fixedSaveOne(f, tables_fixed.loadoutConfigFixedMeasure, tables_fixed.loadoutConfigFixedSave);
      }
      return _fixedResave(wire, tabledemo_fixed.LoadoutConfig.new, tables_fixed.loadoutConfigFixedNewPlan, tabledemo_fixed.TableFixedReport.new, (rep) => rep.refused != 0, tables_fixed.loadoutConfigFixedLoad, tables_fixed.loadoutConfigFixedMeasure, tables_fixed.loadoutConfigFixedSave);
    case 'tabledemo PackConfig':
      if (wire.isNotEmpty && wire[0] == 1) {
        final rep = tabledemo.TableWireReport();
        final w = tabledemo.packConfigWireLoad(wire, rep);
        if (rep.refused) return null;
        final f = tabledemo_fixed.PackConfig();
        _w2f_examples_PackConfig(w, f);
        return _fixedSaveOne(f, pack_fixed.packConfigFixedMeasure, pack_fixed.packConfigFixedSave);
      }
      return _fixedResave(wire, tabledemo_fixed.PackConfig.new, pack_fixed.packConfigFixedNewPlan, tabledemo_fixed.TableFixedReport.new, (rep) => rep.refused != 0, pack_fixed.packConfigFixedLoad, pack_fixed.packConfigFixedMeasure, pack_fixed.packConfigFixedSave);
    case 'tabledemo ProfileConfig':
      if (wire.isNotEmpty && wire[0] == 1) {
        final rep = tabledemo.TableWireReport();
        final w = tabledemo.profileConfigWireLoad(wire, rep);
        if (rep.refused) return null;
        final f = tabledemo_fixed.ProfileConfig();
        _w2f_examples_ProfileConfig(w, f);
        return _fixedSaveOne(f, tables_fixed.profileConfigFixedMeasure, tables_fixed.profileConfigFixedSave);
      }
      return _fixedResave(wire, tabledemo_fixed.ProfileConfig.new, tables_fixed.profileConfigFixedNewPlan, tabledemo_fixed.TableFixedReport.new, (rep) => rep.refused != 0, tables_fixed.profileConfigFixedLoad, tables_fixed.profileConfigFixedMeasure, tables_fixed.profileConfigFixedSave);
    case 'tabledemo RootConfig':
      if (wire.isNotEmpty && wire[0] == 1) {
        final rep = tabledemo.TableWireReport();
        final w = tabledemo.rootConfigWireLoad(wire, rep);
        if (rep.refused) return null;
        final f = tabledemo_fixed.RootConfig();
        _w2f_examples_RootConfig(w, f);
        return _fixedSaveOne(f, tables_fixed.rootConfigFixedMeasure, tables_fixed.rootConfigFixedSave);
      }
      return _fixedResave(wire, tabledemo_fixed.RootConfig.new, tables_fixed.rootConfigFixedNewPlan, tabledemo_fixed.TableFixedReport.new, (rep) => rep.refused != 0, tables_fixed.rootConfigFixedLoad, tables_fixed.rootConfigFixedMeasure, tables_fixed.rootConfigFixedSave);
    case 'tblm1 Msg':
      if (wire.isNotEmpty && wire[0] == 1) {
        final rep = tblm1.TableWireReport();
        final w = tblm1.msgWireLoad(wire, rep);
        if (rep.refused) return null;
        final f = tblm1_fixed.Msg();
        _w2f_m1_Msg(w, f);
        return _fixedSaveOne(f, m1_fixed.msgFixedMeasure, m1_fixed.msgFixedSave);
      }
      return _fixedResave(wire, tblm1_fixed.Msg.new, m1_fixed.msgFixedNewPlan, tblm1_fixed.TableFixedReport.new, (rep) => rep.refused != 0, m1_fixed.msgFixedLoad, m1_fixed.msgFixedMeasure, m1_fixed.msgFixedSave);
    case 'tblm2 Msg':
      if (wire.isNotEmpty && wire[0] == 1) {
        final rep = tblm2.TableWireReport();
        final w = tblm2.msgWireLoad(wire, rep);
        if (rep.refused) return null;
        final f = tblm2_fixed.Msg();
        _w2f_m2_Msg(w, f);
        return _fixedSaveOne(f, m2_fixed.msgFixedMeasure, m2_fixed.msgFixedSave);
      }
      return _fixedResave(wire, tblm2_fixed.Msg.new, m2_fixed.msgFixedNewPlan, tblm2_fixed.TableFixedReport.new, (rep) => rep.refused != 0, m2_fixed.msgFixedLoad, m2_fixed.msgFixedMeasure, m2_fixed.msgFixedSave);
    case 'tblp1 Chain':
      if (wire.isNotEmpty && wire[0] == 1) {
        final rep = tblp1.TableWireReport();
        final w = tblp1.chainWireLoad(wire, rep);
        if (rep.refused) return null;
        final f = tblp1_fixed.Chain();
        _w2f_p1_Chain(w, f);
        return _fixedSaveOne(f, p1_fixed.chainFixedMeasure, p1_fixed.chainFixedSave);
      }
      return _fixedResave(wire, tblp1_fixed.Chain.new, p1_fixed.chainFixedNewPlan, tblp1_fixed.TableFixedReport.new, (rep) => rep.refused != 0, p1_fixed.chainFixedLoad, p1_fixed.chainFixedMeasure, p1_fixed.chainFixedSave);
    case 'tblp3 Chain':
      if (wire.isNotEmpty && wire[0] == 1) {
        final rep = tblp3.TableWireReport();
        final w = tblp3.chainWireLoad(wire, rep);
        if (rep.refused) return null;
        final f = tblp3_fixed.Chain();
        _w2f_p3_Chain(w, f);
        return _fixedSaveOne(f, p3_fixed.chainFixedMeasure, p3_fixed.chainFixedSave);
      }
      return _fixedResave(wire, tblp3_fixed.Chain.new, p3_fixed.chainFixedNewPlan, tblp3_fixed.TableFixedReport.new, (rep) => rep.refused != 0, p3_fixed.chainFixedLoad, p3_fixed.chainFixedMeasure, p3_fixed.chainFixedSave);
    case 'tblr1 Cfg':
      if (wire.isNotEmpty && wire[0] == 1) {
        final rep = tblr1.TableWireReport();
        final w = tblr1.cfgWireLoad(wire, rep);
        if (rep.refused) return null;
        final f = tblr1_fixed.Cfg();
        _w2f_r1_Cfg(w, f);
        return _fixedSaveOne(f, r1_fixed.cfgFixedMeasure, r1_fixed.cfgFixedSave);
      }
      return _fixedResave(wire, tblr1_fixed.Cfg.new, r1_fixed.cfgFixedNewPlan, tblr1_fixed.TableFixedReport.new, (rep) => rep.refused != 0, r1_fixed.cfgFixedLoad, r1_fixed.cfgFixedMeasure, r1_fixed.cfgFixedSave);
    case 'tblr2 Cfg':
      if (wire.isNotEmpty && wire[0] == 1) {
        final rep = tblr2.TableWireReport();
        final w = tblr2.cfgWireLoad(wire, rep);
        if (rep.refused) return null;
        final f = tblr2_fixed.Cfg();
        _w2f_r2_Cfg(w, f);
        return _fixedSaveOne(f, r2_fixed.cfgFixedMeasure, r2_fixed.cfgFixedSave);
      }
      return _fixedResave(wire, tblr2_fixed.Cfg.new, r2_fixed.cfgFixedNewPlan, tblr2_fixed.TableFixedReport.new, (rep) => rep.refused != 0, r2_fixed.cfgFixedLoad, r2_fixed.cfgFixedMeasure, r2_fixed.cfgFixedSave);
    case 'tblv1 Cfg':
      if (wire.isNotEmpty && wire[0] == 1) {
        final rep = tblv1.TableWireReport();
        final w = tblv1.cfgWireLoad(wire, rep);
        if (rep.refused) return null;
        final f = tblv1_fixed.Cfg();
        _w2f_v1_Cfg(w, f);
        return _fixedSaveOne(f, v1_fixed.cfgFixedMeasure, v1_fixed.cfgFixedSave);
      }
      return _fixedResave(wire, tblv1_fixed.Cfg.new, v1_fixed.cfgFixedNewPlan, tblv1_fixed.TableFixedReport.new, (rep) => rep.refused != 0, v1_fixed.cfgFixedLoad, v1_fixed.cfgFixedMeasure, v1_fixed.cfgFixedSave);
    case 'tblv2 Cfg':
      if (wire.isNotEmpty && wire[0] == 1) {
        final rep = tblv2.TableWireReport();
        final w = tblv2.cfgWireLoad(wire, rep);
        if (rep.refused) return null;
        final f = tblv2_fixed.Cfg();
        _w2f_v2_Cfg(w, f);
        return _fixedSaveOne(f, v2_fixed.cfgFixedMeasure, v2_fixed.cfgFixedSave);
      }
      return _fixedResave(wire, tblv2_fixed.Cfg.new, v2_fixed.cfgFixedNewPlan, tblv2_fixed.TableFixedReport.new, (rep) => rep.refused != 0, v2_fixed.cfgFixedLoad, v2_fixed.cfgFixedMeasure, v2_fixed.cfgFixedSave);
    case 'vocab9demo Wide00':
      if (wire.isNotEmpty && wire[0] == 1) {
        final rep = vocab9demo.TableWireReport();
        final w = vocab9demo.wide00WireLoad(wire, rep);
        if (rep.refused) return null;
        final f = vocab9demo_fixed.Wide00();
        _w2f_vocab9_Wide00(w, f);
        return _fixedSaveOne(f, vocab9.wide00FixedMeasure, vocab9.wide00FixedSave);
      }
      return _fixedResave(wire, vocab9demo_fixed.Wide00.new, vocab9.wide00FixedNewPlan, vocab9demo_fixed.TableFixedReport.new, (rep) => rep.refused != 0, vocab9.wide00FixedLoad, vocab9.wide00FixedMeasure, vocab9.wide00FixedSave);
    case 'vocab9demo Wide19':
      if (wire.isNotEmpty && wire[0] == 1) {
        final rep = vocab9demo.TableWireReport();
        final w = vocab9demo.wide19WireLoad(wire, rep);
        if (rep.refused) return null;
        final f = vocab9demo_fixed.Wide19();
        _w2f_vocab9_Wide19(w, f);
        return _fixedSaveOne(f, vocab9.wide19FixedMeasure, vocab9.wide19FixedSave);
      }
      return _fixedResave(wire, vocab9demo_fixed.Wide19.new, vocab9.wide19FixedNewPlan, vocab9demo_fixed.TableFixedReport.new, (rep) => rep.refused != 0, vocab9.wide19FixedLoad, vocab9.wide19FixedMeasure, vocab9.wide19FixedSave);
    case 'vocabdemo Wide00':
      if (wire.isNotEmpty && wire[0] == 1) {
        final rep = vocabdemo.TableWireReport();
        final w = vocabdemo.wide00WireLoad(wire, rep);
        if (rep.refused) return null;
        final f = vocabdemo_fixed.Wide00();
        _w2f_vocab_Wide00(w, f);
        return _fixedSaveOne(f, vocab.wide00FixedMeasure, vocab.wide00FixedSave);
      }
      return _fixedResave(wire, vocabdemo_fixed.Wide00.new, vocab.wide00FixedNewPlan, vocabdemo_fixed.TableFixedReport.new, (rep) => rep.refused != 0, vocab.wide00FixedLoad, vocab.wide00FixedMeasure, vocab.wide00FixedSave);
    case 'vocabdemo Wide09':
      if (wire.isNotEmpty && wire[0] == 1) {
        final rep = vocabdemo.TableWireReport();
        final w = vocabdemo.wide09WireLoad(wire, rep);
        if (rep.refused) return null;
        final f = vocabdemo_fixed.Wide09();
        _w2f_vocab_Wide09(w, f);
        return _fixedSaveOne(f, vocab.wide09FixedMeasure, vocab.wide09FixedSave);
      }
      return _fixedResave(wire, vocabdemo_fixed.Wide09.new, vocab.wide09FixedNewPlan, vocabdemo_fixed.TableFixedReport.new, (rep) => rep.refused != 0, vocab.wide09FixedLoad, vocab.wide09FixedMeasure, vocab.wide09FixedSave);
    case 'widedemo Caption':
      if (wire.isNotEmpty && wire[0] == 1) {
        final rep = widedemo.TableWireReport();
        final w = widedemo.captionWireLoad(wire, rep);
        if (rep.refused) return null;
        final f = widedemo_fixed.Caption();
        _w2f_wide_Caption(w, f);
        return _fixedSaveOne(f, caption_fixed.captionFixedMeasure, caption_fixed.captionFixedSave);
      }
      return _fixedResave(wire, widedemo_fixed.Caption.new, caption_fixed.captionFixedNewPlan, widedemo_fixed.TableFixedReport.new, (rep) => rep.refused != 0, caption_fixed.captionFixedLoad, caption_fixed.captionFixedMeasure, caption_fixed.captionFixedSave);
    case 'widedemo Stamp':
      if (wire.isNotEmpty && wire[0] == 1) {
        final rep = widedemo.TableWireReport();
        final w = widedemo.stampWireLoad(wire, rep);
        if (rep.refused) return null;
        final f = widedemo_fixed.Stamp();
        _w2f_wide_Stamp(w, f);
        return _fixedSaveOne(f, caption_fixed.stampFixedMeasure, caption_fixed.stampFixedSave);
      }
      return _fixedResave(wire, widedemo_fixed.Stamp.new, caption_fixed.stampFixedNewPlan, widedemo_fixed.TableFixedReport.new, (rep) => rep.refused != 0, caption_fixed.stampFixedLoad, caption_fixed.stampFixedMeasure, caption_fixed.stampFixedSave);
  }
  return null;
}

Uint8List? jsonReadSave(String unit, String root, String text) {
  switch ('$unit $root') {
    case 'featurewitnesses Root':
      final rep = feature.TableTextReport();
      final v = feature.rootFromJson(text, rep);
      if (rep.refused) return null;
      return feature.rootWireSave(v);
    case 'featurewitnesses ArrayRoot':
      final rep = feature.TableTextReport();
      final v = feature.arrayRootFromJson(text, rep);
      if (rep.refused) return null;
      return feature.arrayRootWireSave(v);
    case 'tblg1 Guarded':
      final rep = tblg1.TableTextReport();
      final v = tblg1.guardedFromJson(text, rep);
      if (rep.refused) return null;
      return tblg1.guardedWireSave(v);
    case 'tblw1 Fleet':
      final rep = tblw1.TableTextReport();
      final v = tblw1.fleetFromJson(text, rep);
      if (rep.refused) return null;
      return tblw1.fleetWireSave(v);
    case 'tblw2 Fleet':
      final rep = tblw2.TableTextReport();
      final v = tblw2.fleetFromJson(text, rep);
      if (rep.refused) return null;
      return tblw2.fleetWireSave(v);
    case 'graphdemo Scene':
      final rep = graphdemo.TableTextReport();
      final v = graphdemo.sceneFromJson(text, rep);
      if (rep.refused) return null;
      return graphdemo.sceneWireSave(v);
    case 'blobdemo Catalog':
      final rep = blobdemo.TableTextReport();
      final v = blobdemo.catalogFromJson(text, rep);
      if (rep.refused) return null;
      return blobdemo.catalogWireSave(v);
    case 'listdemo Save':
      final rep = listdemo.TableTextReport();
      final v = listdemo.saveFromJson(text, rep);
      if (rep.refused) return null;
      return listdemo.saveWireSave(v);
    case 'listdemo Album':
      final rep = listdemo.TableTextReport();
      final v = listdemo.albumFromJson(text, rep);
      if (rep.refused) return null;
      return listdemo.albumWireSave(v);
    case 'listdemo Sheet':
      final rep = listdemo.TableTextReport();
      final v = listdemo.sheetFromJson(text, rep);
      if (rep.refused) return null;
      return listdemo.sheetWireSave(v);
    case 'listdemo Unbounded':
      final rep = listdemo.TableTextReport();
      final v = listdemo.unboundedFromJson(text, rep);
      if (rep.refused) return null;
      return listdemo.unboundedWireSave(v);
    case 'mapdemo Cells':
      final rep = mapdemo.TableTextReport();
      final v = mapdemo.cellsFromJson(text, rep);
      if (rep.refused) return null;
      return mapdemo.cellsWireSave(v);
    case 'mapdemo Chunks':
      final rep = mapdemo.TableTextReport();
      final v = mapdemo.chunksFromJson(text, rep);
      if (rep.refused) return null;
      return mapdemo.chunksWireSave(v);
    case 'mapdemo Crews':
      final rep = mapdemo.TableTextReport();
      final v = mapdemo.crewsFromJson(text, rep);
      if (rep.refused) return null;
      return mapdemo.crewsWireSave(v);
    case 'mapdemo Depth':
      final rep = mapdemo.TableTextReport();
      final v = mapdemo.depthFromJson(text, rep);
      if (rep.refused) return null;
      return mapdemo.depthWireSave(v);
    case 'mapdemo Docs':
      final rep = mapdemo.TableTextReport();
      final v = mapdemo.docsFromJson(text, rep);
      if (rep.refused) return null;
      return mapdemo.docsWireSave(v);
    case 'mapdemo Fleet':
      final rep = mapdemo.TableTextReport();
      final v = mapdemo.fleetFromJson(text, rep);
      if (rep.refused) return null;
      return mapdemo.fleetWireSave(v);
    case 'mapdemo Pairs':
      final rep = mapdemo.TableTextReport();
      final v = mapdemo.pairsFromJson(text, rep);
      if (rep.refused) return null;
      return mapdemo.pairsWireSave(v);
    case 'mapdemo Runs':
      final rep = mapdemo.TableTextReport();
      final v = mapdemo.runsFromJson(text, rep);
      if (rep.refused) return null;
      return mapdemo.runsWireSave(v);
    case 'mapdemo Slots':
      final rep = mapdemo.TableTextReport();
      final v = mapdemo.slotsFromJson(text, rep);
      if (rep.refused) return null;
      return mapdemo.slotsWireSave(v);
    case 'mapdemo Spans':
      final rep = mapdemo.TableTextReport();
      final v = mapdemo.spansFromJson(text, rep);
      if (rep.refused) return null;
      return mapdemo.spansWireSave(v);
    case 'mapdemo Text':
      final rep = mapdemo.TableTextReport();
      final v = mapdemo.textFromJson(text, rep);
      if (rep.refused) return null;
      return mapdemo.textWireSave(v);
    case 'mapdemo Trails':
      final rep = mapdemo.TableTextReport();
      final v = mapdemo.trailsFromJson(text, rep);
      if (rep.refused) return null;
      return mapdemo.trailsWireSave(v);
    case 'widelist Wide':
      final rep = widelist.TableTextReport();
      final v = widelist.wideFromJson(text, rep);
      if (rep.refused) return null;
      return widelist.wideWireSave(v);
    case 'listdemo Army':
      final rep = listdemo.TableTextReport();
      final v = listdemo.armyFromJson(text, rep);
      if (rep.refused) return null;
      return listdemo.armyWireSave(v);
    case 'listdemo Mixed':
      final rep = listdemo.TableTextReport();
      final v = listdemo.mixedFromJson(text, rep);
      if (rep.refused) return null;
      return listdemo.mixedWireSave(v);
    case 'streamdemo Feed':
      final rep = streamdemo.TableTextReport();
      final v = streamdemo.feedFromJson(text, rep);
      if (rep.refused) return null;
      return streamdemo.feedWireSave(v);
    case 'tabledemo WideBlob':
      final rep = tabledemo.TableTextReport();
      final v = tabledemo.wideBlobFromJson(text, rep);
      if (rep.refused) return null;
      return tabledemo.wideBlobWireSave(v);
    case 'graphdemo ListNode':
      final rep = graphdemo.TableTextReport();
      final v = graphdemo.listNodeFromJson(text, rep);
      if (rep.refused) return null;
      return graphdemo.listNodeWireSave(v);
    case 'listdemo Bounded':
      final rep = listdemo.TableTextReport();
      final v = listdemo.boundedFromJson(text, rep);
      if (rep.refused) return null;
      return listdemo.boundedWireSave(v);
    case 'listdemo Ints':
      final rep = listdemo.TableTextReport();
      final v = listdemo.intsFromJson(text, rep);
      if (rep.refused) return null;
      return listdemo.intsWireSave(v);
    case 'listdemo Squad':
      final rep = listdemo.TableTextReport();
      final v = listdemo.squadFromJson(text, rep);
      if (rep.refused) return null;
      return listdemo.squadWireSave(v);
    case 'tblp2 Chain':
      final rep = tblp2.TableTextReport();
      final v = tblp2.chainFromJson(text, rep);
      if (rep.refused) return null;
      return tblp2.chainWireSave(v);
    case 'backenddemo LoginRequest':
      final rep = backenddemo.TableTextReport();
      final w = backenddemo.loginRequestFromJson(text, rep);
      if (rep.refused) return null;
      final f = backenddemo_fixed.LoginRequest();
      _w2f_backend_LoginRequest(w, f);
      return _fixedSaveOne(f, backend.loginRequestFixedMeasure, backend.loginRequestFixedSave);
    case 'backenddemo MatchResult':
      final rep = backenddemo.TableTextReport();
      final w = backenddemo.matchResultFromJson(text, rep);
      if (rep.refused) return null;
      final f = backenddemo_fixed.MatchResult();
      _w2f_backend_MatchResult(w, f);
      return _fixedSaveOne(f, backend.matchResultFixedMeasure, backend.matchResultFixedSave);
    case 'backenddemo StorePurchase':
      final rep = backenddemo.TableTextReport();
      final w = backenddemo.storePurchaseFromJson(text, rep);
      if (rep.refused) return null;
      final f = backenddemo_fixed.StorePurchase();
      _w2f_backend_StorePurchase(w, f);
      return _fixedSaveOne(f, backend.storePurchaseFixedMeasure, backend.storePurchaseFixedSave);
    case 'messagedemo ToolMessage':
      final rep = messagedemo.TableTextReport();
      final w = messagedemo.toolMessageFromJson(text, rep);
      if (rep.refused) return null;
      final f = messagedemo_fixed.ToolMessage();
      _w2f_messages_ToolMessage(w, f);
      return _fixedSaveOne(f, messages_fixed.toolMessageFixedMeasure, messages_fixed.toolMessageFixedSave);
    case 'scalars SimState':
      final rep = scalardemo.TableTextReport();
      final w = scalardemo.simStateFromJson(text, rep);
      if (rep.refused) return null;
      final f = scalardemo_fixed.SimState();
      _w2f_scalars_SimState(w, f);
      return _fixedSaveOne(f, scalars_fixed.simStateFixedMeasure, scalars_fixed.simStateFixedSave);
    case 'tabledemo ArchiveConfig':
      final rep = tabledemo.TableTextReport();
      final w = tabledemo.archiveConfigFromJson(text, rep);
      if (rep.refused) return null;
      final f = tabledemo_fixed.ArchiveConfig();
      _w2f_examples_ArchiveConfig(w, f);
      return _fixedSaveOne(f, nested_fixed.archiveConfigFixedMeasure, nested_fixed.archiveConfigFixedSave);
    case 'tabledemo KeyedConfig':
      final rep = tabledemo.TableTextReport();
      final w = tabledemo.keyedConfigFromJson(text, rep);
      if (rep.refused) return null;
      final f = tabledemo_fixed.KeyedConfig();
      _w2f_examples_KeyedConfig(w, f);
      return _fixedSaveOne(f, keyed_fixed.keyedConfigFixedMeasure, keyed_fixed.keyedConfigFixedSave);
    case 'tabledemo LoadoutConfig':
      final rep = tabledemo.TableTextReport();
      final w = tabledemo.loadoutConfigFromJson(text, rep);
      if (rep.refused) return null;
      final f = tabledemo_fixed.LoadoutConfig();
      _w2f_examples_LoadoutConfig(w, f);
      return _fixedSaveOne(f, tables_fixed.loadoutConfigFixedMeasure, tables_fixed.loadoutConfigFixedSave);
    case 'tabledemo PackConfig':
      final rep = tabledemo.TableTextReport();
      final w = tabledemo.packConfigFromJson(text, rep);
      if (rep.refused) return null;
      final f = tabledemo_fixed.PackConfig();
      _w2f_examples_PackConfig(w, f);
      return _fixedSaveOne(f, pack_fixed.packConfigFixedMeasure, pack_fixed.packConfigFixedSave);
    case 'tabledemo ProfileConfig':
      final rep = tabledemo.TableTextReport();
      final w = tabledemo.profileConfigFromJson(text, rep);
      if (rep.refused) return null;
      final f = tabledemo_fixed.ProfileConfig();
      _w2f_examples_ProfileConfig(w, f);
      return _fixedSaveOne(f, tables_fixed.profileConfigFixedMeasure, tables_fixed.profileConfigFixedSave);
    case 'tabledemo RootConfig':
      final rep = tabledemo.TableTextReport();
      final w = tabledemo.rootConfigFromJson(text, rep);
      if (rep.refused) return null;
      final f = tabledemo_fixed.RootConfig();
      _w2f_examples_RootConfig(w, f);
      return _fixedSaveOne(f, tables_fixed.rootConfigFixedMeasure, tables_fixed.rootConfigFixedSave);
    case 'tblm1 Msg':
      final rep = tblm1.TableTextReport();
      final w = tblm1.msgFromJson(text, rep);
      if (rep.refused) return null;
      final f = tblm1_fixed.Msg();
      _w2f_m1_Msg(w, f);
      return _fixedSaveOne(f, m1_fixed.msgFixedMeasure, m1_fixed.msgFixedSave);
    case 'tblm2 Msg':
      final rep = tblm2.TableTextReport();
      final w = tblm2.msgFromJson(text, rep);
      if (rep.refused) return null;
      final f = tblm2_fixed.Msg();
      _w2f_m2_Msg(w, f);
      return _fixedSaveOne(f, m2_fixed.msgFixedMeasure, m2_fixed.msgFixedSave);
    case 'tblp1 Chain':
      final rep = tblp1.TableTextReport();
      final w = tblp1.chainFromJson(text, rep);
      if (rep.refused) return null;
      final f = tblp1_fixed.Chain();
      _w2f_p1_Chain(w, f);
      return _fixedSaveOne(f, p1_fixed.chainFixedMeasure, p1_fixed.chainFixedSave);
    case 'tblp3 Chain':
      final rep = tblp3.TableTextReport();
      final w = tblp3.chainFromJson(text, rep);
      if (rep.refused) return null;
      final f = tblp3_fixed.Chain();
      _w2f_p3_Chain(w, f);
      return _fixedSaveOne(f, p3_fixed.chainFixedMeasure, p3_fixed.chainFixedSave);
    case 'tblr1 Cfg':
      final rep = tblr1.TableTextReport();
      final w = tblr1.cfgFromJson(text, rep);
      if (rep.refused) return null;
      final f = tblr1_fixed.Cfg();
      _w2f_r1_Cfg(w, f);
      return _fixedSaveOne(f, r1_fixed.cfgFixedMeasure, r1_fixed.cfgFixedSave);
    case 'tblr2 Cfg':
      final rep = tblr2.TableTextReport();
      final w = tblr2.cfgFromJson(text, rep);
      if (rep.refused) return null;
      final f = tblr2_fixed.Cfg();
      _w2f_r2_Cfg(w, f);
      return _fixedSaveOne(f, r2_fixed.cfgFixedMeasure, r2_fixed.cfgFixedSave);
    case 'tblv1 Cfg':
      final rep = tblv1.TableTextReport();
      final w = tblv1.cfgFromJson(text, rep);
      if (rep.refused) return null;
      final f = tblv1_fixed.Cfg();
      _w2f_v1_Cfg(w, f);
      return _fixedSaveOne(f, v1_fixed.cfgFixedMeasure, v1_fixed.cfgFixedSave);
    case 'tblv2 Cfg':
      final rep = tblv2.TableTextReport();
      final w = tblv2.cfgFromJson(text, rep);
      if (rep.refused) return null;
      final f = tblv2_fixed.Cfg();
      _w2f_v2_Cfg(w, f);
      return _fixedSaveOne(f, v2_fixed.cfgFixedMeasure, v2_fixed.cfgFixedSave);
    case 'vocab9demo Wide00':
      final rep = vocab9demo.TableTextReport();
      final w = vocab9demo.wide00FromJson(text, rep);
      if (rep.refused) return null;
      final f = vocab9demo_fixed.Wide00();
      _w2f_vocab9_Wide00(w, f);
      return _fixedSaveOne(f, vocab9.wide00FixedMeasure, vocab9.wide00FixedSave);
    case 'vocab9demo Wide19':
      final rep = vocab9demo.TableTextReport();
      final w = vocab9demo.wide19FromJson(text, rep);
      if (rep.refused) return null;
      final f = vocab9demo_fixed.Wide19();
      _w2f_vocab9_Wide19(w, f);
      return _fixedSaveOne(f, vocab9.wide19FixedMeasure, vocab9.wide19FixedSave);
    case 'vocabdemo Wide00':
      final rep = vocabdemo.TableTextReport();
      final w = vocabdemo.wide00FromJson(text, rep);
      if (rep.refused) return null;
      final f = vocabdemo_fixed.Wide00();
      _w2f_vocab_Wide00(w, f);
      return _fixedSaveOne(f, vocab.wide00FixedMeasure, vocab.wide00FixedSave);
    case 'vocabdemo Wide09':
      final rep = vocabdemo.TableTextReport();
      final w = vocabdemo.wide09FromJson(text, rep);
      if (rep.refused) return null;
      final f = vocabdemo_fixed.Wide09();
      _w2f_vocab_Wide09(w, f);
      return _fixedSaveOne(f, vocab.wide09FixedMeasure, vocab.wide09FixedSave);
    case 'widedemo Caption':
      final rep = widedemo.TableTextReport();
      final w = widedemo.captionFromJson(text, rep);
      if (rep.refused) return null;
      final f = widedemo_fixed.Caption();
      _w2f_wide_Caption(w, f);
      return _fixedSaveOne(f, caption_fixed.captionFixedMeasure, caption_fixed.captionFixedSave);
    case 'widedemo Stamp':
      final rep = widedemo.TableTextReport();
      final w = widedemo.stampFromJson(text, rep);
      if (rep.refused) return null;
      final f = widedemo_fixed.Stamp();
      _w2f_wide_Stamp(w, f);
      return _fixedSaveOne(f, caption_fixed.stampFixedMeasure, caption_fixed.stampFixedSave);
  }
  return null;
}

String? jsonWriteText(String unit, String root, Uint8List wire) {
  switch ('$unit $root') {
    case 'featurewitnesses Root':
      final rep = feature.TableWireReport();
      final v = feature.rootWireLoad(wire, rep);
      if (rep.refused) return null;
      return feature.rootToJson(v);
    case 'featurewitnesses ArrayRoot':
      final rep = feature.TableWireReport();
      final v = feature.arrayRootWireLoad(wire, rep);
      if (rep.refused) return null;
      return feature.arrayRootToJson(v);
    case 'tblg1 Guarded':
      final rep = tblg1.TableWireReport();
      final v = tblg1.guardedWireLoad(wire, rep);
      if (rep.refused) return null;
      return tblg1.guardedToJson(v);
    case 'tblw1 Fleet':
      final rep = tblw1.TableWireReport();
      final v = tblw1.fleetWireLoad(wire, rep);
      if (rep.refused) return null;
      return tblw1.fleetToJson(v);
    case 'tblw2 Fleet':
      final rep = tblw2.TableWireReport();
      final v = tblw2.fleetWireLoad(wire, rep);
      if (rep.refused) return null;
      return tblw2.fleetToJson(v);
    case 'graphdemo Scene':
      final rep = graphdemo.TableWireReport();
      final v = graphdemo.sceneWireLoad(wire, rep);
      if (rep.refused) return null;
      return graphdemo.sceneToJson(v);
    case 'blobdemo Catalog':
      final rep = blobdemo.TableWireReport();
      final v = blobdemo.catalogWireLoad(wire, rep);
      if (rep.refused) return null;
      return blobdemo.catalogToJson(v);
    case 'listdemo Save':
      final rep = listdemo.TableWireReport();
      final v = listdemo.saveWireLoad(wire, rep);
      if (rep.refused) return null;
      return listdemo.saveToJson(v);
    case 'listdemo Album':
      final rep = listdemo.TableWireReport();
      final v = listdemo.albumWireLoad(wire, rep);
      if (rep.refused) return null;
      return listdemo.albumToJson(v);
    case 'listdemo Sheet':
      final rep = listdemo.TableWireReport();
      final v = listdemo.sheetWireLoad(wire, rep);
      if (rep.refused) return null;
      return listdemo.sheetToJson(v);
    case 'listdemo Unbounded':
      final rep = listdemo.TableWireReport();
      final v = listdemo.unboundedWireLoad(wire, rep);
      if (rep.refused) return null;
      return listdemo.unboundedToJson(v);
    case 'mapdemo Cells':
      final rep = mapdemo.TableWireReport();
      final v = mapdemo.cellsWireLoad(wire, rep);
      if (rep.refused) return null;
      return mapdemo.cellsToJson(v);
    case 'mapdemo Chunks':
      final rep = mapdemo.TableWireReport();
      final v = mapdemo.chunksWireLoad(wire, rep);
      if (rep.refused) return null;
      return mapdemo.chunksToJson(v);
    case 'mapdemo Crews':
      final rep = mapdemo.TableWireReport();
      final v = mapdemo.crewsWireLoad(wire, rep);
      if (rep.refused) return null;
      return mapdemo.crewsToJson(v);
    case 'mapdemo Depth':
      final rep = mapdemo.TableWireReport();
      final v = mapdemo.depthWireLoad(wire, rep);
      if (rep.refused) return null;
      return mapdemo.depthToJson(v);
    case 'mapdemo Docs':
      final rep = mapdemo.TableWireReport();
      final v = mapdemo.docsWireLoad(wire, rep);
      if (rep.refused) return null;
      return mapdemo.docsToJson(v);
    case 'mapdemo Fleet':
      final rep = mapdemo.TableWireReport();
      final v = mapdemo.fleetWireLoad(wire, rep);
      if (rep.refused) return null;
      return mapdemo.fleetToJson(v);
    case 'mapdemo Pairs':
      final rep = mapdemo.TableWireReport();
      final v = mapdemo.pairsWireLoad(wire, rep);
      if (rep.refused) return null;
      return mapdemo.pairsToJson(v);
    case 'mapdemo Runs':
      final rep = mapdemo.TableWireReport();
      final v = mapdemo.runsWireLoad(wire, rep);
      if (rep.refused) return null;
      return mapdemo.runsToJson(v);
    case 'mapdemo Slots':
      final rep = mapdemo.TableWireReport();
      final v = mapdemo.slotsWireLoad(wire, rep);
      if (rep.refused) return null;
      return mapdemo.slotsToJson(v);
    case 'mapdemo Spans':
      final rep = mapdemo.TableWireReport();
      final v = mapdemo.spansWireLoad(wire, rep);
      if (rep.refused) return null;
      return mapdemo.spansToJson(v);
    case 'mapdemo Text':
      final rep = mapdemo.TableWireReport();
      final v = mapdemo.textWireLoad(wire, rep);
      if (rep.refused) return null;
      return mapdemo.textToJson(v);
    case 'mapdemo Trails':
      final rep = mapdemo.TableWireReport();
      final v = mapdemo.trailsWireLoad(wire, rep);
      if (rep.refused) return null;
      return mapdemo.trailsToJson(v);
    case 'widelist Wide':
      final rep = widelist.TableWireReport();
      final v = widelist.wideWireLoad(wire, rep);
      if (rep.refused) return null;
      return widelist.wideToJson(v);
    case 'listdemo Army':
      final rep = listdemo.TableWireReport();
      final v = listdemo.armyWireLoad(wire, rep);
      if (rep.refused) return null;
      return listdemo.armyToJson(v);
    case 'listdemo Mixed':
      final rep = listdemo.TableWireReport();
      final v = listdemo.mixedWireLoad(wire, rep);
      if (rep.refused) return null;
      return listdemo.mixedToJson(v);
    case 'streamdemo Feed':
      final rep = streamdemo.TableWireReport();
      final v = streamdemo.feedWireLoad(wire, rep);
      if (rep.refused) return null;
      return streamdemo.feedToJson(v);
    case 'tabledemo WideBlob':
      final rep = tabledemo.TableWireReport();
      final v = tabledemo.wideBlobWireLoad(wire, rep);
      if (rep.refused) return null;
      return tabledemo.wideBlobToJson(v);
    case 'graphdemo ListNode':
      final rep = graphdemo.TableWireReport();
      final v = graphdemo.listNodeWireLoad(wire, rep);
      if (rep.refused) return null;
      return graphdemo.listNodeToJson(v);
    case 'listdemo Bounded':
      final rep = listdemo.TableWireReport();
      final v = listdemo.boundedWireLoad(wire, rep);
      if (rep.refused) return null;
      return listdemo.boundedToJson(v);
    case 'listdemo Ints':
      final rep = listdemo.TableWireReport();
      final v = listdemo.intsWireLoad(wire, rep);
      if (rep.refused) return null;
      return listdemo.intsToJson(v);
    case 'listdemo Squad':
      final rep = listdemo.TableWireReport();
      final v = listdemo.squadWireLoad(wire, rep);
      if (rep.refused) return null;
      return listdemo.squadToJson(v);
    case 'tblp2 Chain':
      final rep = tblp2.TableWireReport();
      final v = tblp2.chainWireLoad(wire, rep);
      if (rep.refused) return null;
      return tblp2.chainToJson(v);
    case 'backenddemo LoginRequest':
      if (wire.isNotEmpty && wire[0] == 1) {
        final rep = backenddemo.TableWireReport();
        final w = backenddemo.loginRequestWireLoad(wire, rep);
        if (rep.refused) return null;
        return backenddemo.loginRequestToJson(w);
      }
      final f = _fixedLoadOne(wire, backenddemo_fixed.LoginRequest.new, backend.loginRequestFixedNewPlan, backenddemo_fixed.TableFixedReport.new, (rep) => rep.refused != 0, backend.loginRequestFixedLoad);
      if (f == null) return null;
      final w = backenddemo.LoginRequest();
      _f2w_backend_LoginRequest(f, w);
      return backenddemo.loginRequestToJson(w);
    case 'backenddemo MatchResult':
      if (wire.isNotEmpty && wire[0] == 1) {
        final rep = backenddemo.TableWireReport();
        final w = backenddemo.matchResultWireLoad(wire, rep);
        if (rep.refused) return null;
        return backenddemo.matchResultToJson(w);
      }
      final f = _fixedLoadOne(wire, backenddemo_fixed.MatchResult.new, backend.matchResultFixedNewPlan, backenddemo_fixed.TableFixedReport.new, (rep) => rep.refused != 0, backend.matchResultFixedLoad);
      if (f == null) return null;
      final w = backenddemo.MatchResult();
      _f2w_backend_MatchResult(f, w);
      return backenddemo.matchResultToJson(w);
    case 'backenddemo StorePurchase':
      if (wire.isNotEmpty && wire[0] == 1) {
        final rep = backenddemo.TableWireReport();
        final w = backenddemo.storePurchaseWireLoad(wire, rep);
        if (rep.refused) return null;
        return backenddemo.storePurchaseToJson(w);
      }
      final f = _fixedLoadOne(wire, backenddemo_fixed.StorePurchase.new, backend.storePurchaseFixedNewPlan, backenddemo_fixed.TableFixedReport.new, (rep) => rep.refused != 0, backend.storePurchaseFixedLoad);
      if (f == null) return null;
      final w = backenddemo.StorePurchase();
      _f2w_backend_StorePurchase(f, w);
      return backenddemo.storePurchaseToJson(w);
    case 'messagedemo ToolMessage':
      if (wire.isNotEmpty && wire[0] == 1) {
        final rep = messagedemo.TableWireReport();
        final w = messagedemo.toolMessageWireLoad(wire, rep);
        if (rep.refused) return null;
        return messagedemo.toolMessageToJson(w);
      }
      final f = _fixedLoadOne(wire, messagedemo_fixed.ToolMessage.new, messages_fixed.toolMessageFixedNewPlan, messagedemo_fixed.TableFixedReport.new, (rep) => rep.refused != 0, messages_fixed.toolMessageFixedLoad);
      if (f == null) return null;
      final w = messagedemo.ToolMessage();
      _f2w_messages_ToolMessage(f, w);
      return messagedemo.toolMessageToJson(w);
    case 'scalars SimState':
      if (wire.isNotEmpty && wire[0] == 1) {
        final rep = scalardemo.TableWireReport();
        final w = scalardemo.simStateWireLoad(wire, rep);
        if (rep.refused) return null;
        return scalardemo.simStateToJson(w);
      }
      final f = _fixedLoadOne(wire, scalardemo_fixed.SimState.new, scalars_fixed.simStateFixedNewPlan, scalardemo_fixed.TableFixedReport.new, (rep) => rep.refused != 0, scalars_fixed.simStateFixedLoad);
      if (f == null) return null;
      final w = scalardemo.SimState();
      _f2w_scalars_SimState(f, w);
      return scalardemo.simStateToJson(w);
    case 'tabledemo ArchiveConfig':
      if (wire.isNotEmpty && wire[0] == 1) {
        final rep = tabledemo.TableWireReport();
        final w = tabledemo.archiveConfigWireLoad(wire, rep);
        if (rep.refused) return null;
        return tabledemo.archiveConfigToJson(w);
      }
      final f = _fixedLoadOne(wire, tabledemo_fixed.ArchiveConfig.new, nested_fixed.archiveConfigFixedNewPlan, tabledemo_fixed.TableFixedReport.new, (rep) => rep.refused != 0, nested_fixed.archiveConfigFixedLoad);
      if (f == null) return null;
      final w = tabledemo.ArchiveConfig();
      _f2w_examples_ArchiveConfig(f, w);
      return tabledemo.archiveConfigToJson(w);
    case 'tabledemo KeyedConfig':
      if (wire.isNotEmpty && wire[0] == 1) {
        final rep = tabledemo.TableWireReport();
        final w = tabledemo.keyedConfigWireLoad(wire, rep);
        if (rep.refused) return null;
        return tabledemo.keyedConfigToJson(w);
      }
      final f = _fixedLoadOne(wire, tabledemo_fixed.KeyedConfig.new, keyed_fixed.keyedConfigFixedNewPlan, tabledemo_fixed.TableFixedReport.new, (rep) => rep.refused != 0, keyed_fixed.keyedConfigFixedLoad);
      if (f == null) return null;
      final w = tabledemo.KeyedConfig();
      _f2w_examples_KeyedConfig(f, w);
      return tabledemo.keyedConfigToJson(w);
    case 'tabledemo LoadoutConfig':
      if (wire.isNotEmpty && wire[0] == 1) {
        final rep = tabledemo.TableWireReport();
        final w = tabledemo.loadoutConfigWireLoad(wire, rep);
        if (rep.refused) return null;
        return tabledemo.loadoutConfigToJson(w);
      }
      final f = _fixedLoadOne(wire, tabledemo_fixed.LoadoutConfig.new, tables_fixed.loadoutConfigFixedNewPlan, tabledemo_fixed.TableFixedReport.new, (rep) => rep.refused != 0, tables_fixed.loadoutConfigFixedLoad);
      if (f == null) return null;
      final w = tabledemo.LoadoutConfig();
      _f2w_examples_LoadoutConfig(f, w);
      return tabledemo.loadoutConfigToJson(w);
    case 'tabledemo PackConfig':
      if (wire.isNotEmpty && wire[0] == 1) {
        final rep = tabledemo.TableWireReport();
        final w = tabledemo.packConfigWireLoad(wire, rep);
        if (rep.refused) return null;
        return tabledemo.packConfigToJson(w);
      }
      final f = _fixedLoadOne(wire, tabledemo_fixed.PackConfig.new, pack_fixed.packConfigFixedNewPlan, tabledemo_fixed.TableFixedReport.new, (rep) => rep.refused != 0, pack_fixed.packConfigFixedLoad);
      if (f == null) return null;
      final w = tabledemo.PackConfig();
      _f2w_examples_PackConfig(f, w);
      return tabledemo.packConfigToJson(w);
    case 'tabledemo ProfileConfig':
      if (wire.isNotEmpty && wire[0] == 1) {
        final rep = tabledemo.TableWireReport();
        final w = tabledemo.profileConfigWireLoad(wire, rep);
        if (rep.refused) return null;
        return tabledemo.profileConfigToJson(w);
      }
      final f = _fixedLoadOne(wire, tabledemo_fixed.ProfileConfig.new, tables_fixed.profileConfigFixedNewPlan, tabledemo_fixed.TableFixedReport.new, (rep) => rep.refused != 0, tables_fixed.profileConfigFixedLoad);
      if (f == null) return null;
      final w = tabledemo.ProfileConfig();
      _f2w_examples_ProfileConfig(f, w);
      return tabledemo.profileConfigToJson(w);
    case 'tabledemo RootConfig':
      if (wire.isNotEmpty && wire[0] == 1) {
        final rep = tabledemo.TableWireReport();
        final w = tabledemo.rootConfigWireLoad(wire, rep);
        if (rep.refused) return null;
        return tabledemo.rootConfigToJson(w);
      }
      final f = _fixedLoadOne(wire, tabledemo_fixed.RootConfig.new, tables_fixed.rootConfigFixedNewPlan, tabledemo_fixed.TableFixedReport.new, (rep) => rep.refused != 0, tables_fixed.rootConfigFixedLoad);
      if (f == null) return null;
      final w = tabledemo.RootConfig();
      _f2w_examples_RootConfig(f, w);
      return tabledemo.rootConfigToJson(w);
    case 'tblm1 Msg':
      if (wire.isNotEmpty && wire[0] == 1) {
        final rep = tblm1.TableWireReport();
        final w = tblm1.msgWireLoad(wire, rep);
        if (rep.refused) return null;
        return tblm1.msgToJson(w);
      }
      final f = _fixedLoadOne(wire, tblm1_fixed.Msg.new, m1_fixed.msgFixedNewPlan, tblm1_fixed.TableFixedReport.new, (rep) => rep.refused != 0, m1_fixed.msgFixedLoad);
      if (f == null) return null;
      final w = tblm1.Msg();
      _f2w_m1_Msg(f, w);
      return tblm1.msgToJson(w);
    case 'tblm2 Msg':
      if (wire.isNotEmpty && wire[0] == 1) {
        final rep = tblm2.TableWireReport();
        final w = tblm2.msgWireLoad(wire, rep);
        if (rep.refused) return null;
        return tblm2.msgToJson(w);
      }
      final f = _fixedLoadOne(wire, tblm2_fixed.Msg.new, m2_fixed.msgFixedNewPlan, tblm2_fixed.TableFixedReport.new, (rep) => rep.refused != 0, m2_fixed.msgFixedLoad);
      if (f == null) return null;
      final w = tblm2.Msg();
      _f2w_m2_Msg(f, w);
      return tblm2.msgToJson(w);
    case 'tblp1 Chain':
      if (wire.isNotEmpty && wire[0] == 1) {
        final rep = tblp1.TableWireReport();
        final w = tblp1.chainWireLoad(wire, rep);
        if (rep.refused) return null;
        return tblp1.chainToJson(w);
      }
      final f = _fixedLoadOne(wire, tblp1_fixed.Chain.new, p1_fixed.chainFixedNewPlan, tblp1_fixed.TableFixedReport.new, (rep) => rep.refused != 0, p1_fixed.chainFixedLoad);
      if (f == null) return null;
      final w = tblp1.Chain();
      _f2w_p1_Chain(f, w);
      return tblp1.chainToJson(w);
    case 'tblp3 Chain':
      if (wire.isNotEmpty && wire[0] == 1) {
        final rep = tblp3.TableWireReport();
        final w = tblp3.chainWireLoad(wire, rep);
        if (rep.refused) return null;
        return tblp3.chainToJson(w);
      }
      final f = _fixedLoadOne(wire, tblp3_fixed.Chain.new, p3_fixed.chainFixedNewPlan, tblp3_fixed.TableFixedReport.new, (rep) => rep.refused != 0, p3_fixed.chainFixedLoad);
      if (f == null) return null;
      final w = tblp3.Chain();
      _f2w_p3_Chain(f, w);
      return tblp3.chainToJson(w);
    case 'tblr1 Cfg':
      if (wire.isNotEmpty && wire[0] == 1) {
        final rep = tblr1.TableWireReport();
        final w = tblr1.cfgWireLoad(wire, rep);
        if (rep.refused) return null;
        return tblr1.cfgToJson(w);
      }
      final f = _fixedLoadOne(wire, tblr1_fixed.Cfg.new, r1_fixed.cfgFixedNewPlan, tblr1_fixed.TableFixedReport.new, (rep) => rep.refused != 0, r1_fixed.cfgFixedLoad);
      if (f == null) return null;
      final w = tblr1.Cfg();
      _f2w_r1_Cfg(f, w);
      return tblr1.cfgToJson(w);
    case 'tblr2 Cfg':
      if (wire.isNotEmpty && wire[0] == 1) {
        final rep = tblr2.TableWireReport();
        final w = tblr2.cfgWireLoad(wire, rep);
        if (rep.refused) return null;
        return tblr2.cfgToJson(w);
      }
      final f = _fixedLoadOne(wire, tblr2_fixed.Cfg.new, r2_fixed.cfgFixedNewPlan, tblr2_fixed.TableFixedReport.new, (rep) => rep.refused != 0, r2_fixed.cfgFixedLoad);
      if (f == null) return null;
      final w = tblr2.Cfg();
      _f2w_r2_Cfg(f, w);
      return tblr2.cfgToJson(w);
    case 'tblv1 Cfg':
      if (wire.isNotEmpty && wire[0] == 1) {
        final rep = tblv1.TableWireReport();
        final w = tblv1.cfgWireLoad(wire, rep);
        if (rep.refused) return null;
        return tblv1.cfgToJson(w);
      }
      final f = _fixedLoadOne(wire, tblv1_fixed.Cfg.new, v1_fixed.cfgFixedNewPlan, tblv1_fixed.TableFixedReport.new, (rep) => rep.refused != 0, v1_fixed.cfgFixedLoad);
      if (f == null) return null;
      final w = tblv1.Cfg();
      _f2w_v1_Cfg(f, w);
      return tblv1.cfgToJson(w);
    case 'tblv2 Cfg':
      if (wire.isNotEmpty && wire[0] == 1) {
        final rep = tblv2.TableWireReport();
        final w = tblv2.cfgWireLoad(wire, rep);
        if (rep.refused) return null;
        return tblv2.cfgToJson(w);
      }
      final f = _fixedLoadOne(wire, tblv2_fixed.Cfg.new, v2_fixed.cfgFixedNewPlan, tblv2_fixed.TableFixedReport.new, (rep) => rep.refused != 0, v2_fixed.cfgFixedLoad);
      if (f == null) return null;
      final w = tblv2.Cfg();
      _f2w_v2_Cfg(f, w);
      return tblv2.cfgToJson(w);
    case 'vocab9demo Wide00':
      if (wire.isNotEmpty && wire[0] == 1) {
        final rep = vocab9demo.TableWireReport();
        final w = vocab9demo.wide00WireLoad(wire, rep);
        if (rep.refused) return null;
        return vocab9demo.wide00ToJson(w);
      }
      final f = _fixedLoadOne(wire, vocab9demo_fixed.Wide00.new, vocab9.wide00FixedNewPlan, vocab9demo_fixed.TableFixedReport.new, (rep) => rep.refused != 0, vocab9.wide00FixedLoad);
      if (f == null) return null;
      final w = vocab9demo.Wide00();
      _f2w_vocab9_Wide00(f, w);
      return vocab9demo.wide00ToJson(w);
    case 'vocab9demo Wide19':
      if (wire.isNotEmpty && wire[0] == 1) {
        final rep = vocab9demo.TableWireReport();
        final w = vocab9demo.wide19WireLoad(wire, rep);
        if (rep.refused) return null;
        return vocab9demo.wide19ToJson(w);
      }
      final f = _fixedLoadOne(wire, vocab9demo_fixed.Wide19.new, vocab9.wide19FixedNewPlan, vocab9demo_fixed.TableFixedReport.new, (rep) => rep.refused != 0, vocab9.wide19FixedLoad);
      if (f == null) return null;
      final w = vocab9demo.Wide19();
      _f2w_vocab9_Wide19(f, w);
      return vocab9demo.wide19ToJson(w);
    case 'vocabdemo Wide00':
      if (wire.isNotEmpty && wire[0] == 1) {
        final rep = vocabdemo.TableWireReport();
        final w = vocabdemo.wide00WireLoad(wire, rep);
        if (rep.refused) return null;
        return vocabdemo.wide00ToJson(w);
      }
      final f = _fixedLoadOne(wire, vocabdemo_fixed.Wide00.new, vocab.wide00FixedNewPlan, vocabdemo_fixed.TableFixedReport.new, (rep) => rep.refused != 0, vocab.wide00FixedLoad);
      if (f == null) return null;
      final w = vocabdemo.Wide00();
      _f2w_vocab_Wide00(f, w);
      return vocabdemo.wide00ToJson(w);
    case 'vocabdemo Wide09':
      if (wire.isNotEmpty && wire[0] == 1) {
        final rep = vocabdemo.TableWireReport();
        final w = vocabdemo.wide09WireLoad(wire, rep);
        if (rep.refused) return null;
        return vocabdemo.wide09ToJson(w);
      }
      final f = _fixedLoadOne(wire, vocabdemo_fixed.Wide09.new, vocab.wide09FixedNewPlan, vocabdemo_fixed.TableFixedReport.new, (rep) => rep.refused != 0, vocab.wide09FixedLoad);
      if (f == null) return null;
      final w = vocabdemo.Wide09();
      _f2w_vocab_Wide09(f, w);
      return vocabdemo.wide09ToJson(w);
    case 'widedemo Caption':
      if (wire.isNotEmpty && wire[0] == 1) {
        final rep = widedemo.TableWireReport();
        final w = widedemo.captionWireLoad(wire, rep);
        if (rep.refused) return null;
        return widedemo.captionToJson(w);
      }
      final f = _fixedLoadOne(wire, widedemo_fixed.Caption.new, caption_fixed.captionFixedNewPlan, widedemo_fixed.TableFixedReport.new, (rep) => rep.refused != 0, caption_fixed.captionFixedLoad);
      if (f == null) return null;
      final w = widedemo.Caption();
      _f2w_wide_Caption(f, w);
      return widedemo.captionToJson(w);
    case 'widedemo Stamp':
      if (wire.isNotEmpty && wire[0] == 1) {
        final rep = widedemo.TableWireReport();
        final w = widedemo.stampWireLoad(wire, rep);
        if (rep.refused) return null;
        return widedemo.stampToJson(w);
      }
      final f = _fixedLoadOne(wire, widedemo_fixed.Stamp.new, caption_fixed.stampFixedNewPlan, widedemo_fixed.TableFixedReport.new, (rep) => rep.refused != 0, caption_fixed.stampFixedLoad);
      if (f == null) return null;
      final w = widedemo.Stamp();
      _f2w_wide_Stamp(f, w);
      return widedemo.stampToJson(w);
  }
  return null;
}

String? loadReport(String unit, String root, Uint8List wire, {bool framing = false}) {
  switch ('$unit $root') {
    case 'featurewitnesses Root':
      final rep = feature.TableWireReport();
      feature.rootWireLoad(wire, rep);
      return _reportLine(rep.unknown, 0, rep.widened, 0, 0, rep.refused, rep.refused);
    case 'featurewitnesses ArrayRoot':
      final rep = feature.TableWireReport();
      feature.arrayRootWireLoad(wire, rep);
      return _reportLine(rep.unknown, 0, rep.widened, 0, 0, rep.refused, rep.refused);
    case 'tblg1 Guarded':
      final rep = tblg1.TableWireReport();
      tblg1.guardedWireLoad(wire, rep);
      return _reportLine(rep.unknown, 0, rep.widened, 0, 0, rep.refused, rep.refused);
    case 'tblw1 Fleet':
      final rep = tblw1.TableWireReport();
      tblw1.fleetWireLoad(wire, rep);
      return _reportLine(rep.unknown, 0, rep.widened, 0, 0, rep.refused, rep.refused);
    case 'tblw2 Fleet':
      final rep = tblw2.TableWireReport();
      tblw2.fleetWireLoad(wire, rep);
      return _reportLine(rep.unknown, 0, rep.widened, 0, 0, rep.refused, rep.refused);
    case 'graphdemo Scene':
      final rep = graphdemo.TableWireReport();
      graphdemo.sceneWireLoad(wire, rep);
      return _reportLine(rep.unknown, 0, rep.widened, 0, 0, rep.refused, rep.refused);
    case 'blobdemo Catalog':
      final rep = blobdemo.TableWireReport();
      blobdemo.catalogWireLoad(wire, rep);
      return _reportLine(rep.unknown, 0, rep.widened, 0, 0, rep.refused, rep.refused);
    case 'listdemo Save':
      final rep = listdemo.TableWireReport();
      listdemo.saveWireLoad(wire, rep);
      return _reportLine(rep.unknown, 0, rep.widened, 0, 0, rep.refused, rep.refused);
    case 'listdemo Album':
      final rep = listdemo.TableWireReport();
      listdemo.albumWireLoad(wire, rep);
      return _reportLine(rep.unknown, 0, rep.widened, 0, 0, rep.refused, rep.refused);
    case 'listdemo Sheet':
      final rep = listdemo.TableWireReport();
      listdemo.sheetWireLoad(wire, rep);
      return _reportLine(rep.unknown, 0, rep.widened, 0, 0, rep.refused, rep.refused);
    case 'listdemo Unbounded':
      final rep = listdemo.TableWireReport();
      listdemo.unboundedWireLoad(wire, rep);
      return _reportLine(rep.unknown, 0, rep.widened, 0, 0, rep.refused, rep.refused);
    case 'mapdemo Cells':
      final rep = mapdemo.TableWireReport();
      mapdemo.cellsWireLoad(wire, rep);
      return _reportLine(rep.unknown, 0, rep.widened, 0, 0, rep.refused, rep.refused);
    case 'mapdemo Chunks':
      final rep = mapdemo.TableWireReport();
      mapdemo.chunksWireLoad(wire, rep);
      return _reportLine(rep.unknown, 0, rep.widened, 0, 0, rep.refused, rep.refused);
    case 'mapdemo Crews':
      final rep = mapdemo.TableWireReport();
      mapdemo.crewsWireLoad(wire, rep);
      return _reportLine(rep.unknown, 0, rep.widened, 0, 0, rep.refused, rep.refused);
    case 'mapdemo Depth':
      final rep = mapdemo.TableWireReport();
      mapdemo.depthWireLoad(wire, rep);
      return _reportLine(rep.unknown, 0, rep.widened, 0, 0, rep.refused, rep.refused);
    case 'mapdemo Docs':
      final rep = mapdemo.TableWireReport();
      mapdemo.docsWireLoad(wire, rep);
      return _reportLine(rep.unknown, 0, rep.widened, 0, 0, rep.refused, rep.refused);
    case 'mapdemo Fleet':
      final rep = mapdemo.TableWireReport();
      mapdemo.fleetWireLoad(wire, rep);
      return _reportLine(rep.unknown, 0, rep.widened, 0, 0, rep.refused, rep.refused);
    case 'mapdemo Pairs':
      final rep = mapdemo.TableWireReport();
      mapdemo.pairsWireLoad(wire, rep);
      return _reportLine(rep.unknown, 0, rep.widened, 0, 0, rep.refused, rep.refused);
    case 'mapdemo Runs':
      final rep = mapdemo.TableWireReport();
      mapdemo.runsWireLoad(wire, rep);
      return _reportLine(rep.unknown, 0, rep.widened, 0, 0, rep.refused, rep.refused);
    case 'mapdemo Slots':
      final rep = mapdemo.TableWireReport();
      mapdemo.slotsWireLoad(wire, rep);
      return _reportLine(rep.unknown, 0, rep.widened, 0, 0, rep.refused, rep.refused);
    case 'mapdemo Spans':
      final rep = mapdemo.TableWireReport();
      mapdemo.spansWireLoad(wire, rep);
      return _reportLine(rep.unknown, 0, rep.widened, 0, 0, rep.refused, rep.refused);
    case 'mapdemo Text':
      final rep = mapdemo.TableWireReport();
      mapdemo.textWireLoad(wire, rep);
      return _reportLine(rep.unknown, 0, rep.widened, 0, 0, rep.refused, rep.refused);
    case 'mapdemo Trails':
      final rep = mapdemo.TableWireReport();
      mapdemo.trailsWireLoad(wire, rep);
      return _reportLine(rep.unknown, 0, rep.widened, 0, 0, rep.refused, rep.refused);
    case 'widelist Wide':
      final rep = widelist.TableWireReport();
      widelist.wideWireLoad(wire, rep);
      return _reportLine(rep.unknown, 0, rep.widened, 0, 0, rep.refused, rep.refused);
    case 'listdemo Army':
      final rep = listdemo.TableWireReport();
      listdemo.armyWireLoad(wire, rep);
      return _reportLine(rep.unknown, 0, rep.widened, 0, 0, rep.refused, rep.refused);
    case 'listdemo Mixed':
      final rep = listdemo.TableWireReport();
      listdemo.mixedWireLoad(wire, rep);
      return _reportLine(rep.unknown, 0, rep.widened, 0, 0, rep.refused, rep.refused);
    case 'streamdemo Feed':
      final rep = streamdemo.TableWireReport();
      streamdemo.feedWireLoad(wire, rep);
      return _reportLine(rep.unknown, 0, rep.widened, 0, 0, rep.refused, rep.refused);
    case 'tabledemo WideBlob':
      return _reportLine(0, 0, 0, 0, 0, true, true);
    case 'graphdemo ListNode':
      final rep = graphdemo.TableWireReport();
      graphdemo.listNodeWireLoad(wire, rep);
      return _reportLine(rep.unknown, 0, rep.widened, 0, 0, rep.refused, rep.refused);
    case 'listdemo Bounded':
      final rep = listdemo.TableWireReport();
      listdemo.boundedWireLoad(wire, rep);
      return _reportLine(rep.unknown, 0, rep.widened, 0, 0, rep.refused, rep.refused);
    case 'listdemo Ints':
      final rep = listdemo.TableWireReport();
      listdemo.intsWireLoad(wire, rep);
      return _reportLine(rep.unknown, 0, rep.widened, 0, 0, rep.refused, rep.refused);
    case 'listdemo Squad':
      final rep = listdemo.TableWireReport();
      listdemo.squadWireLoad(wire, rep);
      return _reportLine(rep.unknown, 0, rep.widened, 0, 0, rep.refused, rep.refused);
    case 'tblp2 Chain':
      final rep = tblp2.TableWireReport();
      tblp2.chainWireLoad(wire, rep);
      return _reportLine(rep.unknown, 0, rep.widened, 0, 0, rep.refused, rep.refused);
    case 'backenddemo LoginRequest':
      return _fixedReportLine(wire, backenddemo_fixed.LoginRequest.new, backend.loginRequestFixedNewPlan, backenddemo_fixed.TableFixedReport.new, (rep) => (rep.unknown, rep.widened, rep.refused != 0), backend.loginRequestFixedLoad);
    case 'backenddemo MatchResult':
      return _fixedReportLine(wire, backenddemo_fixed.MatchResult.new, backend.matchResultFixedNewPlan, backenddemo_fixed.TableFixedReport.new, (rep) => (rep.unknown, rep.widened, rep.refused != 0), backend.matchResultFixedLoad);
    case 'backenddemo StorePurchase':
      return _fixedReportLine(wire, backenddemo_fixed.StorePurchase.new, backend.storePurchaseFixedNewPlan, backenddemo_fixed.TableFixedReport.new, (rep) => (rep.unknown, rep.widened, rep.refused != 0), backend.storePurchaseFixedLoad);
    case 'messagedemo ToolMessage':
      return _fixedReportLine(wire, messagedemo_fixed.ToolMessage.new, messages_fixed.toolMessageFixedNewPlan, messagedemo_fixed.TableFixedReport.new, (rep) => (rep.unknown, rep.widened, rep.refused != 0), messages_fixed.toolMessageFixedLoad);
    case 'scalars SimState':
      return _fixedReportLine(wire, scalardemo_fixed.SimState.new, scalars_fixed.simStateFixedNewPlan, scalardemo_fixed.TableFixedReport.new, (rep) => (rep.unknown, rep.widened, rep.refused != 0), scalars_fixed.simStateFixedLoad);
    case 'tabledemo ArchiveConfig':
      return _fixedReportLine(wire, tabledemo_fixed.ArchiveConfig.new, nested_fixed.archiveConfigFixedNewPlan, tabledemo_fixed.TableFixedReport.new, (rep) => (rep.unknown, rep.widened, rep.refused != 0), nested_fixed.archiveConfigFixedLoad);
    case 'tabledemo KeyedConfig':
      return _fixedReportLine(wire, tabledemo_fixed.KeyedConfig.new, keyed_fixed.keyedConfigFixedNewPlan, tabledemo_fixed.TableFixedReport.new, (rep) => (rep.unknown, rep.widened, rep.refused != 0), keyed_fixed.keyedConfigFixedLoad);
    case 'tabledemo LoadoutConfig':
      return _fixedReportLine(wire, tabledemo_fixed.LoadoutConfig.new, tables_fixed.loadoutConfigFixedNewPlan, tabledemo_fixed.TableFixedReport.new, (rep) => (rep.unknown, rep.widened, rep.refused != 0), tables_fixed.loadoutConfigFixedLoad);
    case 'tabledemo PackConfig':
      return _fixedReportLine(wire, tabledemo_fixed.PackConfig.new, pack_fixed.packConfigFixedNewPlan, tabledemo_fixed.TableFixedReport.new, (rep) => (rep.unknown, rep.widened, rep.refused != 0), pack_fixed.packConfigFixedLoad);
    case 'tabledemo ProfileConfig':
      return _fixedReportLine(wire, tabledemo_fixed.ProfileConfig.new, tables_fixed.profileConfigFixedNewPlan, tabledemo_fixed.TableFixedReport.new, (rep) => (rep.unknown, rep.widened, rep.refused != 0), tables_fixed.profileConfigFixedLoad);
    case 'tabledemo RootConfig':
      return _fixedReportLine(wire, tabledemo_fixed.RootConfig.new, tables_fixed.rootConfigFixedNewPlan, tabledemo_fixed.TableFixedReport.new, (rep) => (rep.unknown, rep.widened, rep.refused != 0), tables_fixed.rootConfigFixedLoad);
    case 'tblm1 Msg':
      return _fixedReportLine(wire, tblm1_fixed.Msg.new, m1_fixed.msgFixedNewPlan, tblm1_fixed.TableFixedReport.new, (rep) => (rep.unknown, rep.widened, rep.refused != 0), m1_fixed.msgFixedLoad);
    case 'tblm2 Msg':
      return _fixedReportLine(wire, tblm2_fixed.Msg.new, m2_fixed.msgFixedNewPlan, tblm2_fixed.TableFixedReport.new, (rep) => (rep.unknown, rep.widened, rep.refused != 0), m2_fixed.msgFixedLoad);
    case 'tblp1 Chain':
      return _fixedReportLine(wire, tblp1_fixed.Chain.new, p1_fixed.chainFixedNewPlan, tblp1_fixed.TableFixedReport.new, (rep) => (rep.unknown, rep.widened, rep.refused != 0), p1_fixed.chainFixedLoad);
    case 'tblp3 Chain':
      return _fixedReportLine(wire, tblp3_fixed.Chain.new, p3_fixed.chainFixedNewPlan, tblp3_fixed.TableFixedReport.new, (rep) => (rep.unknown, rep.widened, rep.refused != 0), p3_fixed.chainFixedLoad);
    case 'tblr1 Cfg':
      if (framing && (wire.isEmpty || wire[0] != 3)) {
        final rep = tblr1.TableWireReport();
        tblr1.cfgWireLoad(wire, rep);
        final scan = tblr1.TableWireReader(wire, wire.length)..report = rep;
        if (!rep.refused && scan.open() == 0) {
          scan.scanNodeTable();
          if (scan.wireUnreachedNode()) rep.refuse('unreached_node');
        }
        return _reportLine(rep.unknown, 0, rep.widened, 0, 0, rep.refused, rep.refused);
      }
      return _fixedReportLine(wire, tblr1_fixed.Cfg.new, r1_fixed.cfgFixedNewPlan, tblr1_fixed.TableFixedReport.new, (rep) => (rep.unknown, rep.widened, rep.refused != 0), r1_fixed.cfgFixedLoad);
    case 'tblr2 Cfg':
      return _fixedReportLine(wire, tblr2_fixed.Cfg.new, r2_fixed.cfgFixedNewPlan, tblr2_fixed.TableFixedReport.new, (rep) => (rep.unknown, rep.widened, rep.refused != 0), r2_fixed.cfgFixedLoad);
    case 'tblv1 Cfg':
      return _fixedReportLine(wire, tblv1_fixed.Cfg.new, v1_fixed.cfgFixedNewPlan, tblv1_fixed.TableFixedReport.new, (rep) => (rep.unknown, rep.widened, rep.refused != 0), v1_fixed.cfgFixedLoad);
    case 'tblv2 Cfg':
      return _fixedReportLine(wire, tblv2_fixed.Cfg.new, v2_fixed.cfgFixedNewPlan, tblv2_fixed.TableFixedReport.new, (rep) => (rep.unknown, rep.widened, rep.refused != 0), v2_fixed.cfgFixedLoad);
    case 'vocab9demo Wide00':
      return _fixedReportLine(wire, vocab9demo_fixed.Wide00.new, vocab9.wide00FixedNewPlan, vocab9demo_fixed.TableFixedReport.new, (rep) => (rep.unknown, rep.widened, rep.refused != 0), vocab9.wide00FixedLoad);
    case 'vocab9demo Wide19':
      return _fixedReportLine(wire, vocab9demo_fixed.Wide19.new, vocab9.wide19FixedNewPlan, vocab9demo_fixed.TableFixedReport.new, (rep) => (rep.unknown, rep.widened, rep.refused != 0), vocab9.wide19FixedLoad);
    case 'vocabdemo Wide00':
      return _fixedReportLine(wire, vocabdemo_fixed.Wide00.new, vocab.wide00FixedNewPlan, vocabdemo_fixed.TableFixedReport.new, (rep) => (rep.unknown, rep.widened, rep.refused != 0), vocab.wide00FixedLoad);
    case 'vocabdemo Wide09':
      return _fixedReportLine(wire, vocabdemo_fixed.Wide09.new, vocab.wide09FixedNewPlan, vocabdemo_fixed.TableFixedReport.new, (rep) => (rep.unknown, rep.widened, rep.refused != 0), vocab.wide09FixedLoad);
    case 'widedemo Caption':
      return _fixedReportLine(wire, widedemo_fixed.Caption.new, caption_fixed.captionFixedNewPlan, widedemo_fixed.TableFixedReport.new, (rep) => (rep.unknown, rep.widened, rep.refused != 0), caption_fixed.captionFixedLoad);
    case 'widedemo Stamp':
      return _fixedReportLine(wire, widedemo_fixed.Stamp.new, caption_fixed.stampFixedNewPlan, widedemo_fixed.TableFixedReport.new, (rep) => (rep.unknown, rep.widened, rep.refused != 0), caption_fixed.stampFixedLoad);
    case 'tbla2 Root':
      return _fixedReportLine(wire, tbla2_fixed.Root.new, a2_fixed.rootFixedNewPlan, tbla2_fixed.TableFixedReport.new, (rep) => (rep.unknown, rep.widened, rep.refused != 0), a2_fixed.rootFixedLoad);
    case 'tblk1 Root':
      return _fixedReportLine(wire, tblk1_fixed.Root.new, k1_fixed.rootFixedNewPlan, tblk1_fixed.TableFixedReport.new, (rep) => (rep.unknown, rep.widened, rep.refused != 0), k1_fixed.rootFixedLoad);
    case 'tblk2 Root':
      return _fixedReportLine(wire, tblk2_fixed.Root.new, k2_fixed.rootFixedNewPlan, tblk2_fixed.TableFixedReport.new, (rep) => (rep.unknown, rep.widened, rep.refused != 0), k2_fixed.rootFixedLoad);
    case 'tblscalars2 SimState':
      return _fixedReportLine(wire, scalardemo2_fixed.SimState.new, scalars2_fixed.simStateFixedNewPlan, scalardemo2_fixed.TableFixedReport.new, (rep) => (rep.unknown, rep.widened, rep.refused != 0), scalars2_fixed.simStateFixedLoad);
  }
  return null;
}

// ---- cook-write (docs/SPEC-TABLES.md §7.6): the runtime WRITES the cooked
// file, and the harness compares it with the two `schema cook` wrote. Each
// row names an instance: its wire is loaded, measured, refused at one byte
// short (which must write nothing) and cooked in each byte order.
//
// A row whose instance this leg cannot load writes <case>.absent with the
// reason, by unit (test/conformance/README.md).
// Every unit's rows are answered today; the map stays as the one place a
// future absence is named.
const Map<String, String> cookWriteAbsent = <String, String>{};

// cookWriter loads one instance and answers its measured extent and the
// writer for one byte order, or null when this leg has no codec for it.
(int, bool Function(Uint8List, Endian))? cookWriter(
  String unit,
  String root,
  Uint8List wire,
  String? jsonText,
  String shareSrc,
  String shareAlias,
) {
  switch ('$unit $root') {
    case 'featurewitnesses Root':
      final r = feature.TableWireReport();
      final tr = feature.TableTextReport();
      final value = jsonText != null ? feature.rootFromJson(jsonText, tr) : feature.rootWireLoad(wire, r);
      if (r.refused || tr.refused) return (-1, (_, _) => false);
      return(featurecook.RootCook.cookMeasure(value),(out,order)=>featurecook.RootCook.cook(value,out,order));
    case 'featurewitnesses ArrayRoot':
      final r = feature.TableWireReport();
      final tr = feature.TableTextReport();
      final value = jsonText != null ? feature.arrayRootFromJson(jsonText, tr) : feature.arrayRootWireLoad(wire, r);
      if (r.refused || tr.refused) return (-1, (_, _) => false);
      return(featurecook.ArrayRootCook.cookMeasure(value),(out,order)=>featurecook.ArrayRootCook.cook(value,out,order));

    case 'graphdemo Scene':
      final r = graphdemo.TableWireReport();
      final tr = graphdemo.TableTextReport();
      final value = jsonText != null ? graphdemo.sceneFromJson(jsonText, tr) : graphdemo.sceneWireLoad(wire, r);
      if (r.refused || tr.refused) return (-1, (_, _) => false);
      return (
        ck.SceneCook.cookMeasure(value),
        (out, order) => ck.SceneCook.cook(value, out, order),
      );
    case 'blobdemo Catalog':
      final r = blobdemo.TableWireReport();
      final tr = blobdemo.TableTextReport();
      final value = jsonText != null ? blobdemo.catalogFromJson(jsonText, tr) : blobdemo.catalogWireLoad(wire, r);
      if (r.refused || tr.refused) return (-1, (_, _) => false);
      if (shareSrc == 'thumb' && shareAlias == 'alias') value.alias = value.thumb;
      return (
        assets.CatalogCook.cookMeasure(value),
        (out, order) => assets.CatalogCook.cook(value, out, order),
      );
    case 'tabledemo ArchiveConfig':
      final r = tabledemo.TableWireReport();
      final tr = tabledemo.TableTextReport();
      final value = jsonText != null ? tabledemo.archiveConfigFromJson(jsonText, tr) : tabledemo.archiveConfigWireLoad(wire, r);
      if (r.refused || tr.refused) return (-1, (_, _) => false);
      return (
        nestedcook.ArchiveConfigCookWire.cookMeasure(value),
        (out, order) => nestedcook.ArchiveConfigCookWire.cook(value, out, order),
      );
    case 'tabledemo KeyedConfig':
      final r = tabledemo.TableWireReport();
      final tr = tabledemo.TableTextReport();
      final value = jsonText != null ? tabledemo.keyedConfigFromJson(jsonText, tr) : tabledemo.keyedConfigWireLoad(wire, r);
      if (r.refused || tr.refused) return (-1, (_, _) => false);
      return (
        keyedcook.KeyedConfigCookWire.cookMeasure(value),
        (out, order) => keyedcook.KeyedConfigCookWire.cook(value, out, order),
      );
    case 'tabledemo LoadoutConfig':
      final r = tabledemo.TableWireReport();
      final tr = tabledemo.TableTextReport();
      final value = jsonText != null ? tabledemo.loadoutConfigFromJson(jsonText, tr) : tabledemo.loadoutConfigWireLoad(wire, r);
      if (r.refused || tr.refused) return (-1, (_, _) => false);
      return (
        tablescook.LoadoutConfigCookWire.cookMeasure(value),
        (out, order) => tablescook.LoadoutConfigCookWire.cook(value, out, order),
      );
    case 'tabledemo ProfileConfig':
      final r = tabledemo.TableWireReport();
      final tr = tabledemo.TableTextReport();
      final value = jsonText != null ? tabledemo.profileConfigFromJson(jsonText, tr) : tabledemo.profileConfigWireLoad(wire, r);
      if (r.refused || tr.refused) return (-1, (_, _) => false);
      return (
        tablescook.ProfileConfigCookWire.cookMeasure(value),
        (out, order) => tablescook.ProfileConfigCookWire.cook(value, out, order),
      );
    case 'tabledemo RootConfig':
      final r = tabledemo.TableWireReport();
      final tr = tabledemo.TableTextReport();
      final value = jsonText != null ? tabledemo.rootConfigFromJson(jsonText, tr) : tabledemo.rootConfigWireLoad(wire, r);
      if (r.refused || tr.refused) return (-1, (_, _) => false);
      return (
        tablescook.RootConfigCookWire.cookMeasure(value),
        (out, order) => tablescook.RootConfigCookWire.cook(value, out, order),
      );
    case 'tabledemo WideBlob':
      final r = tabledemo.TableWireReport();
      final tr = tabledemo.TableTextReport();
      final value = jsonText != null ? tabledemo.wideBlobFromJson(jsonText, tr) : tabledemo.wideBlobWireLoad(wire, r);
      if (r.refused || tr.refused) return (-1, (_, _) => false);
      return (
        widecook.WideBlobCookWire.cookMeasure(value),
        (out, order) => widecook.WideBlobCookWire.cook(value, out, order),
      );
    case 'tblv1 Cfg':
      final r = tblv1.TableWireReport();
      final tr = tblv1.TableTextReport();
      final value = jsonText != null ? tblv1.cfgFromJson(jsonText, tr) : tblv1.cfgWireLoad(wire, r);
      if (r.refused || tr.refused) return (-1, (_, _) => false);
      return (
        v1cook.CfgCookWire.cookMeasure(value),
        (out, order) => v1cook.CfgCookWire.cook(value, out, order),
      );
    case 'tblv2 Cfg':
      final r = tblv2.TableWireReport();
      final tr = tblv2.TableTextReport();
      final value = jsonText != null ? tblv2.cfgFromJson(jsonText, tr) : tblv2.cfgWireLoad(wire, r);
      if (r.refused || tr.refused) return (-1, (_, _) => false);
      return (
        v2cook.CfgCookWire.cookMeasure(value),
        (out, order) => v2cook.CfgCookWire.cook(value, out, order),
      );
    case 'tblp1 Chain':
      final r = tblp1.TableWireReport();
      final tr = tblp1.TableTextReport();
      final value = jsonText != null ? tblp1.chainFromJson(jsonText, tr) : tblp1.chainWireLoad(wire, r);
      if (r.refused || tr.refused) return (-1, (_, _) => false);
      return (
        p1cook.ChainCookWire.cookMeasure(value),
        (out, order) => p1cook.ChainCookWire.cook(value, out, order),
      );
    case 'tblp3 Chain':
      final r = tblp3.TableWireReport();
      final tr = tblp3.TableTextReport();
      final value = jsonText != null ? tblp3.chainFromJson(jsonText, tr) : tblp3.chainWireLoad(wire, r);
      if (r.refused || tr.refused) return (-1, (_, _) => false);
      return (
        p3cook.ChainCookWire.cookMeasure(value),
        (out, order) => p3cook.ChainCookWire.cook(value, out, order),
      );
    case 'tblm1 Msg':
      final r = tblm1.TableWireReport();
      final tr = tblm1.TableTextReport();
      final value = jsonText != null ? tblm1.msgFromJson(jsonText, tr) : tblm1.msgWireLoad(wire, r);
      if (r.refused || tr.refused) return (-1, (_, _) => false);
      return (
        m1cook.MsgCookWire.cookMeasure(value),
        (out, order) => m1cook.MsgCookWire.cook(value, out, order),
      );
    case 'tblm2 Msg':
      final r = tblm2.TableWireReport();
      final tr = tblm2.TableTextReport();
      final value = jsonText != null ? tblm2.msgFromJson(jsonText, tr) : tblm2.msgWireLoad(wire, r);
      if (r.refused || tr.refused) return (-1, (_, _) => false);
      return (
        m2cook.MsgCookWire.cookMeasure(value),
        (out, order) => m2cook.MsgCookWire.cook(value, out, order),
      );
    case 'messagedemo ToolMessage':
      final r = messagedemo.TableWireReport();
      final tr = messagedemo.TableTextReport();
      final value = jsonText != null ? messagedemo.toolMessageFromJson(jsonText, tr) : messagedemo.toolMessageWireLoad(wire, r);
      if (r.refused || tr.refused) return (-1, (_, _) => false);
      return (
        messagescook.ToolMessageCookWire.cookMeasure(value),
        (out, order) =>
            messagescook.ToolMessageCookWire.cook(value, out, order),
      );
    case 'scalars SimState':
      final r = scalardemo.TableWireReport();
      final tr = scalardemo.TableTextReport();
      final value = jsonText != null ? scalardemo.simStateFromJson(jsonText, tr) : scalardemo.simStateWireLoad(wire, r);
      if (r.refused || tr.refused) return (-1, (_, _) => false);
      return (
        scalarscook.SimStateCookWire.cookMeasure(value),
        (out, order) => scalarscook.SimStateCookWire.cook(value, out, order),
      );
    case 'streamdemo Feed':
      final r = streamdemo.TableWireReport();
      final tr = streamdemo.TableTextReport();
      final value = jsonText != null ? streamdemo.feedFromJson(jsonText, tr) : streamdemo.feedWireLoad(wire, r);
      if (r.refused || tr.refused) return (-1, (_, _) => false);
      return (
        streamcook.FeedCook.cookMeasure(value),
        (out, order) => streamcook.FeedCook.cook(value, out, order),
      );
    case 'listdemo Save':
      final r = listdemo.TableWireReport();
      final tr = listdemo.TableTextReport();
      final value = jsonText != null ? listdemo.saveFromJson(jsonText, tr) : listdemo.saveWireLoad(wire, r);
      if (r.refused || tr.refused) return (-1, (_, _) => false);
      return (
        lsavecook.SaveCook.cookMeasure(value),
        (out, order) => lsavecook.SaveCook.cook(value, out, order),
      );
    case 'listdemo Mixed':
      final r = listdemo.TableWireReport();
      final tr = listdemo.TableTextReport();
      final value = jsonText != null ? listdemo.mixedFromJson(jsonText, tr) : listdemo.mixedWireLoad(wire, r);
      if (r.refused || tr.refused) return (-1, (_, _) => false);
      return (
        lsavecook.MixedCook.cookMeasure(value),
        (out, order) => lsavecook.MixedCook.cook(value, out, order),
      );
    case 'listdemo Album':
      final r = listdemo.TableWireReport();
      final tr = listdemo.TableTextReport();
      final value = jsonText != null ? listdemo.albumFromJson(jsonText, tr) : listdemo.albumWireLoad(wire, r);
      if (r.refused || tr.refused) return (-1, (_, _) => false);
      return (
        lsharedcook.AlbumCook.cookMeasure(value),
        (out, order) => lsharedcook.AlbumCook.cook(value, out, order),
      );
    case 'listdemo Sheet':
      final r = listdemo.TableWireReport();
      final tr = listdemo.TableTextReport();
      final value = jsonText != null ? listdemo.sheetFromJson(jsonText, tr) : listdemo.sheetWireLoad(wire, r);
      if (r.refused || tr.refused) return (-1, (_, _) => false);
      return (
        lholderscook.SheetCook.cookMeasure(value),
        (out, order) => lholderscook.SheetCook.cook(value, out, order),
      );
    case 'listdemo Army':
      final r = listdemo.TableWireReport();
      final tr = listdemo.TableTextReport();
      final value = jsonText != null ? listdemo.armyFromJson(jsonText, tr) : listdemo.armyWireLoad(wire, r);
      if (r.refused || tr.refused) return (-1, (_, _) => false);
      return (
        lholderscook.ArmyCook.cookMeasure(value),
        (out, order) => lholderscook.ArmyCook.cook(value, out, order),
      );
    case 'listdemo Unbounded':
      final r = listdemo.TableWireReport();
      final tr = listdemo.TableTextReport();
      final value = jsonText != null ? listdemo.unboundedFromJson(jsonText, tr) : listdemo.unboundedWireLoad(wire, r);
      if (r.refused || tr.refused) return (-1, (_, _) => false);
      return (
        lmigratecook.UnboundedCook.cookMeasure(value),
        (out, order) => lmigratecook.UnboundedCook.cook(value, out, order),
      );
    case 'mapdemo Cells':
      final r = mapdemo.TableWireReport();
      final tr = mapdemo.TableTextReport();
      final value = jsonText != null ? mapdemo.cellsFromJson(jsonText, tr) : mapdemo.cellsWireLoad(wire, r);
      if (r.refused || tr.refused) return (-1, (_, _) => false);
      return (
        mcellscook.CellsCook.cookMeasure(value),
        (out, order) => mcellscook.CellsCook.cook(value, out, order),
      );
    case 'mapdemo Chunks':
      final r = mapdemo.TableWireReport();
      final tr = mapdemo.TableTextReport();
      final value = jsonText != null ? mapdemo.chunksFromJson(jsonText, tr) : mapdemo.chunksWireLoad(wire, r);
      if (r.refused || tr.refused) return (-1, (_, _) => false);
      return (
        mchunkscook.ChunksCook.cookMeasure(value),
        (out, order) => mchunkscook.ChunksCook.cook(value, out, order),
      );
    case 'mapdemo Crews':
      final r = mapdemo.TableWireReport();
      final tr = mapdemo.TableTextReport();
      final value = jsonText != null ? mapdemo.crewsFromJson(jsonText, tr) : mapdemo.crewsWireLoad(wire, r);
      if (r.refused || tr.refused) return (-1, (_, _) => false);
      return (
        mcrewscook.CrewsCook.cookMeasure(value),
        (out, order) => mcrewscook.CrewsCook.cook(value, out, order),
      );
    case 'mapdemo Depth':
      final r = mapdemo.TableWireReport();
      final tr = mapdemo.TableTextReport();
      final value = jsonText != null ? mapdemo.depthFromJson(jsonText, tr) : mapdemo.depthWireLoad(wire, r);
      if (r.refused || tr.refused) return (-1, (_, _) => false);
      return (
        mdepthcook.DepthCook.cookMeasure(value),
        (out, order) => mdepthcook.DepthCook.cook(value, out, order),
      );
    case 'mapdemo Docs':
      final r = mapdemo.TableWireReport();
      final tr = mapdemo.TableTextReport();
      final value = jsonText != null ? mapdemo.docsFromJson(jsonText, tr) : mapdemo.docsWireLoad(wire, r);
      if (r.refused || tr.refused) return (-1, (_, _) => false);
      return (
        mdocscook.DocsCook.cookMeasure(value),
        (out, order) => mdocscook.DocsCook.cook(value, out, order),
      );
    case 'mapdemo Fleet':
      final r = mapdemo.TableWireReport();
      final tr = mapdemo.TableTextReport();
      final value = jsonText != null ? mapdemo.fleetFromJson(jsonText, tr) : mapdemo.fleetWireLoad(wire, r);
      if (r.refused || tr.refused) return (-1, (_, _) => false);
      return (
        mfleetcook.FleetCook.cookMeasure(value),
        (out, order) => mfleetcook.FleetCook.cook(value, out, order),
      );
    case 'mapdemo Pairs':
      final r = mapdemo.TableWireReport();
      final tr = mapdemo.TableTextReport();
      final value = jsonText != null ? mapdemo.pairsFromJson(jsonText, tr) : mapdemo.pairsWireLoad(wire, r);
      if (r.refused || tr.refused) return (-1, (_, _) => false);
      return (
        mpairscook.PairsCook.cookMeasure(value),
        (out, order) => mpairscook.PairsCook.cook(value, out, order),
      );
    case 'mapdemo Runs':
      final r = mapdemo.TableWireReport();
      final tr = mapdemo.TableTextReport();
      final value = jsonText != null ? mapdemo.runsFromJson(jsonText, tr) : mapdemo.runsWireLoad(wire, r);
      if (r.refused || tr.refused) return (-1, (_, _) => false);
      return (
        mrunscook.RunsCook.cookMeasure(value),
        (out, order) => mrunscook.RunsCook.cook(value, out, order),
      );
    case 'mapdemo Slots':
      final r = mapdemo.TableWireReport();
      final tr = mapdemo.TableTextReport();
      final value = jsonText != null ? mapdemo.slotsFromJson(jsonText, tr) : mapdemo.slotsWireLoad(wire, r);
      if (r.refused || tr.refused) return (-1, (_, _) => false);
      return (
        mslotscook.SlotsCook.cookMeasure(value),
        (out, order) => mslotscook.SlotsCook.cook(value, out, order),
      );
    case 'mapdemo Spans':
      final r = mapdemo.TableWireReport();
      final tr = mapdemo.TableTextReport();
      final value = jsonText != null ? mapdemo.spansFromJson(jsonText, tr) : mapdemo.spansWireLoad(wire, r);
      if (r.refused || tr.refused) return (-1, (_, _) => false);
      return (
        mspanscook.SpansCook.cookMeasure(value),
        (out, order) => mspanscook.SpansCook.cook(value, out, order),
      );
    case 'mapdemo Text':
      final r = mapdemo.TableWireReport();
      final tr = mapdemo.TableTextReport();
      final value = jsonText != null ? mapdemo.textFromJson(jsonText, tr) : mapdemo.textWireLoad(wire, r);
      if (r.refused || tr.refused) return (-1, (_, _) => false);
      return (
        mtextcook.TextCook.cookMeasure(value),
        (out, order) => mtextcook.TextCook.cook(value, out, order),
      );
    case 'mapdemo Trails':
      final r = mapdemo.TableWireReport();
      final tr = mapdemo.TableTextReport();
      final value = jsonText != null ? mapdemo.trailsFromJson(jsonText, tr) : mapdemo.trailsWireLoad(wire, r);
      if (r.refused || tr.refused) return (-1, (_, _) => false);
      return (
        mtrailscook.TrailsCook.cookMeasure(value),
        (out, order) => mtrailscook.TrailsCook.cook(value, out, order),
      );
    case 'widelist Wide':
      final r = widelist.TableWireReport();
      final tr = widelist.TableTextReport();
      final value = jsonText != null ? widelist.wideFromJson(jsonText, tr) : widelist.wideWireLoad(wire, r);
      if (r.refused || tr.refused) return (-1, (_, _) => false);
      return (
        widelistcook.WideCook.cookMeasure(value),
        (out, order) => widelistcook.WideCook.cook(value, out, order),
      );
  }
  return null;
}

int surfaceCookWrite(String outDir) {
  for (final f in kind('cook-write')) {
    final instance = lines.firstWhere(
      (c) => c[0] == 'instance' && c[1] == f[1],
      orElse: () => const <String>[],
    );
    if (instance.isEmpty) {
      stderr.writeln('driver: cook-write ${f[1]} names no instance');
      return 1;
    }
    String? jsonText;
    var shareSrc = '-';
    var shareAlias = '-';
    if (f.length > 2 && f[2].endsWith('.json')) {
      jsonText = readJsonTextFile(f[2]);
      if (f.length > 3) shareSrc = f[3];
      if (f.length > 4) shareAlias = f[4];
    } else {
      for (final tok in f.skip(2)) {
        if (tok.startsWith('input=')) {
          jsonText = readJsonTextFile(tok.substring(6));
        } else if (tok.startsWith('share=')) {
          final sp = tok.substring(6).split(':');
          if (sp.length == 2) {
            shareSrc = sp[0];
            shareAlias = sp[1];
          }
        }
      }
    }
    final writer = cookWriter(
      instance[2],
      instance[3],
      File(instance[4]).readAsBytesSync(),
      jsonText,
      shareSrc,
      shareAlias,
    );
    if (writer == null) {
      final why =
          cookWriteAbsent[instance[2]] ??
          'no Dart codec for ${instance[2]} ${instance[3]}';
      for (final name in [f[1], '${f[1]}-be']) {
        File('$outDir/$name.absent').writeAsStringSync(why);
      }
      continue;
    }
    final (size, cook) = writer;
    if (size < 1) {
      stderr.writeln('driver: ${f[1]}: cook measure refused');
      return 1;
    }
    for (final order in [Endian.little, Endian.big]) {
      final name = order == Endian.little ? f[1] : '${f[1]}-be';
      final buffer = Uint8List(size)..fillRange(0, size, 0xa5);
      // one byte short is refused, and the refusal writes NOTHING (§7.6)
      if (cook(Uint8List.sublistView(buffer, 0, size - 1), order) ||
          buffer.any((b) => b != 0xa5)) {
        stderr.writeln('driver: $name: a short capacity wrote bytes');
        return 1;
      }
      if (!cook(buffer, order)) {
        stderr.writeln('driver: $name: cook write refused');
        return 1;
      }
      File('$outDir/$name').writeAsBytesSync(buffer);
    }
  }
  return 0;
}

int surfaceWire(String outDir) {
  for (final f in kind('instance')) {
    Uint8List? saved;
    try {
      saved = wireResave(f[2], f[3], File(f[4]).readAsBytesSync());
    } on StateError catch (e) {
      // a blob two slots name is refused by every writer
      // (docs/SPEC-TABLES.md §2.5): the case's answer is that refusal
      if (!e.message.startsWith('blob_shared')) rethrow;
      File('$outDir/${f[1]}.absent').writeAsStringSync('blob_shared');
      continue;
    }
    if (saved == null) {
      spillAbsent(outDir, f[1]);
      continue;
    }
    File('$outDir/${f[1]}').writeAsBytesSync(saved);
  }
  return 0;
}

int surfaceJsonRead(String outDir) {
  for (final f in kind('instance')) {
    if (noTextRow(f)) continue;
    final textPath = 'testdata/conformance/tables/json/${f[1]}.json';
    if (!File(textPath).existsSync()) {
      spillAbsent(outDir, f[1]);
      continue;
    }
    final text = readJsonTextFile(textPath);
    if (text == null) throw FormatException('invalid UTF-8 in $textPath');
    final saved = jsonReadSave(f[2], f[3], text);
    if (saved == null) {
      spillAbsent(outDir, f[1]);
      continue;
    }
    File('$outDir/${f[1]}').writeAsBytesSync(saved);
  }
  return 0;
}

int surfaceJsonWrite(String outDir) {
  for (final f in kind('instance')) {
    if (noTextRow(f)) continue;
    final text = jsonWriteText(f[2], f[3], File(f[4]).readAsBytesSync());
    if (text == null) {
      spillAbsent(outDir, '${f[1]}.json');
      continue;
    }
    File('$outDir/${f[1]}.json').writeAsStringSync(text);
  }
  for (final f in kind('json-write-refusal')) {
    final wire = File(f[4]).readAsBytesSync();
    String? outText;
    try {
      switch ('${f[2]} ${f[3]}') {
        case 'graphdemo Scene':
          final r = graphdemo.TableWireReport();
          final v = graphdemo.sceneWireLoad(wire, r);
          if (r.refused) {
            outText = 'refused\n';
          } else {
            outText = graphdemo.sceneToJson(v);
          }
          break;
        case 'tabledemo ProfileConfig':
          final fx = _fixedLoadOne(wire, tabledemo_fixed.ProfileConfig.new, tables_fixed.profileConfigFixedNewPlan, tabledemo_fixed.TableFixedReport.new, (rep) => rep.refused != 0, tables_fixed.profileConfigFixedLoad);
          if (fx == null) return 1;
          final v = tabledemo.ProfileConfig();
          _f2w_examples_ProfileConfig(fx, v);
          if (f[5] == 'narrow:name') v.name = String.fromCharCodes(const <int>[0x61, 0xd800, 0x62]);
          outText = tabledemo.profileConfigToJson(v);
          break;
        case 'widedemo Stamp':
          final fx = _fixedLoadOne(wire, widedemo_fixed.Stamp.new, caption_fixed.stampFixedNewPlan, widedemo_fixed.TableFixedReport.new, (rep) => rep.refused != 0, caption_fixed.stampFixedLoad);
          if (fx == null) return 1;
          final v = widedemo.Stamp();
          _f2w_wide_Stamp(fx, v);
          if (f[5] == 'wide:label') v.label = String.fromCharCodes(const <int>[0x61, 0xd800, 0x62]);
          outText = widedemo.stampToJson(v);
          break;
      }
    } on StateError {
      outText = 'refused\n';
    }
    if (outText == null) {
      spillAbsent(outDir, '${f[1]}.json');
      continue;
    }
    File('$outDir/${f[1]}.json').writeAsStringSync(outText);
  }
  return 0;
}

// §16.3 and §17.5: a hostile tree reports the actual tolerant text-read
// counters, or refuses a malformed text. A missing codec is explicit.
String? jsonHostileReport(String unit, String root, String text) {
  dynamic r;
  late final String Function() encode;
  try {
    switch ('$unit $root') {
      case 'featurewitnesses Root':
        r = feature.TableTextReport();
        final v = feature.rootFromJson(text, r);
        encode = () => feature.rootToJson(v);
        break;
      case 'featurewitnesses ArrayRoot':
        r = feature.TableTextReport();
        final v = feature.arrayRootFromJson(text, r);
        encode = () => feature.arrayRootToJson(v);
        break;
      case 'tabledemo RootConfig':
        r = tabledemo.TableTextReport();
        final v = tabledemo.rootConfigFromJson(text, r);
        encode = () => tabledemo.rootConfigToJson(v);
        break;
      case 'tabledemo PackConfig':
        r = tabledemo.TableTextReport();
        final v = tabledemo.packConfigFromJson(text, r);
        encode = () => tabledemo.packConfigToJson(v);
        break;
      case 'blobdemo Catalog':
        r = blobdemo.TableTextReport();
        final v = blobdemo.catalogFromJson(text, r);
        encode = () => blobdemo.catalogToJson(v);
        break;
      case 'graphdemo Scene':
        r = graphdemo.TableTextReport();
        final v = graphdemo.sceneFromJson(text, r);
        encode = () => graphdemo.sceneToJson(v);
        break;
      case 'mapdemo Text':
        r = mapdemo.TableTextReport();
        final v = mapdemo.textFromJson(text, r);
        encode = () => mapdemo.textToJson(v);
        break;
      case 'listdemo Army':
        r = listdemo.TableTextReport();
        final v = listdemo.armyFromJson(text, r);
        encode = () => listdemo.armyToJson(v);
        break;
      case 'messagedemo ToolMessage':
        r = messagedemo.TableTextReport();
        final v = messagedemo.toolMessageFromJson(text, r);
        encode = () => messagedemo.toolMessageToJson(v);
        break;
      case 'scalars SimState':
        r = scalardemo.TableTextReport();
        final v = scalardemo.simStateFromJson(text, r);
        encode = () => scalardemo.simStateToJson(v);
        break;
      case 'streamdemo Feed':
        r = streamdemo.TableTextReport();
        final v = streamdemo.feedFromJson(text, r);
        encode = () => streamdemo.feedToJson(v);
        break;
      default:
        return null;
    }
  } on FormatException {
    return 'refused\n';
  }
  if (r.refused as bool) return 'refused\n';
  // the report line, then the §16 text of the value the read left
  return '${r.unknown},${0},0,${0},${0},false,read\n${encode()}';
}

int surfaceJsonHostile(String outDir) {
  for (final f in kind('json-hostile')) {
    final text = readJsonTextFile('${f[4]}/${f[3]}.json');
    if (text == null) {
      File('$outDir/${f[1]}').writeAsStringSync('refused\n');
      continue;
    }
    final verdict = jsonHostileReport(f[2], f[3], text);
    if (verdict == null) {
      spillAbsent(outDir, f[1]);
      continue;
    }
    File('$outDir/${f[1]}').writeAsStringSync(verdict);
    // an accepted hostile read also writes the write-back wire of the value
    // the text read left
    final first = verdict.substring(0, verdict.indexOf('\n')).trimRight();
    if (first.endsWith('read')) {
      final saved = jsonReadSave(f[2], f[3], text);
      if (saved != null) {
        File('$outDir/${f[1]}.wire').writeAsBytesSync(saved);
      }
    }
  }
  return 0;
}

// ---- retain and retain-save (docs/SPEC-TABLES.md §6.6): RT1's Node loaded
// with retention at the row's two capacities, measured and saved back. `retain`
// answers `read\n` plus the §16 text of the known value the read left (or the
// refusal alone), `retain-save` the saved bytes. `short` is ONE BYTE SHORT OF
// THE LAST RECORD: a roomy load says what this port's own records cost, and the
// second load gets one byte less (test/conformance/README.md). The id list's
// capacity is a count.
const int _retainRoomy = 1 << 20;

int surfaceRetain(String outDir, bool saves) {
  for (final f in lines) {
    if (f[0] != 'retain') continue;
    if (saves && f[f.length - 1] == '-') continue;
    if (f[2] != 'tblrt1' || f[3] != 'Node') {
      stderr.writeln('driver: retain ${f[1]} names ${f[2]} ${f[3]}');
      return 1;
    }
    final wire = File(f[4]).readAsBytesSync();
    final ids = List<int>.filled(f[6] == 'full' ? _retainRoomy ~/ 8 : int.parse(f[6]), 0);
    var store = tblrt1.TableRetain(_retainRoomy, ids);
    var report = tblrt1.TableWireReport();
    if (f[5] == 'short') {
      tblrt1.nodeWireLoadRetain(wire, store, report);
      if (report.refused || report.refused) {
        stderr.writeln('driver: ${f[1]}: capacity probe refused');
        return 1;
      }
      store = tblrt1.TableRetain(store.used - 1 < 0 ? 0 : store.used - 1, ids);
      report = tblrt1.TableWireReport();
    }
    final root = tblrt1.nodeWireLoadRetain(wire, store, report);
    if (report.refused) {
      // THE ONE READ'S VERDICT is the answer: a refused retaining read owes
      // the refusal alone, and no bytes (docs/SPEC-TABLES.md §6.6)
      if (!saves) {
        File('$outDir/${f[1]}').writeAsStringSync('refused\n');
      }
      continue;
    }
    final size = tblrt1.nodeWireMeasureRetain(root, store);
    final save = tblrt1.TableWireReport();
    final out = tblrt1.nodeWireSaveRetain(root, store, save);
    if (size < 0 || out.length != size) {
      stderr.writeln('driver: ${f[1]}: retaining measure and save disagree');
      return 1;
    }
    // the save is a function of the value and the store: a second one writes
    // the same bytes and loses the same records
    final second = tblrt1.TableWireReport();
    final again = tblrt1.nodeWireSaveRetain(root, store, second);
    if (!_sameBytes(again, out) || second.retainLost != save.retainLost) {
      stderr.writeln('driver: ${f[1]}: retaining save is not repeatable');
      return 1;
    }
    if (saves) {
      File('$outDir/${f[1]}').writeAsBytesSync(out);
    } else {
      // the verdict, then the §16 text of the known value the read left:
      // retention keeps the unknown records for the save and never changes
      // the value a reader of this build sees
      File('$outDir/${f[1]}').writeAsStringSync(
        'read\n${tblrt1.nodeToJson(root)}',
      );
    }
  }
  return 0;
}

bool _sameBytes(List<int> a, List<int> b) {
  if (a.length != b.length) return false;
  for (var i = 0; i < a.length; i++) {
    if (a[i] != b[i]) return false;
  }
  return true;
}

// THE FRAMING VECTORS (docs/LAYERS/1-framing.md): the read report as the
// `report` surface prints it, then the §16 text of the value the read left,
// unless the row says `no-value` or the read was refused. DAMAGE DISCARDS THE
// WHOLE LOAD (docs/SPEC-TABLES.md §4): a read that reports malformed answers
// the report line alone, and no value is written after it. A derived line is
// `framing <case> <unit> <root> <wire> [no-value]`.
int surfaceFraming(String outDir) {
  for (final f in kind('framing')) {
    final wire = File(f[4]).readAsBytesSync();
    final line = loadReport(f[2], f[3], wire, framing: true);
    if (line == null) {
      spillAbsent(outDir, f[1]);
      continue;
    }
    final noValue = f.length > 5 && f[5] == 'no-value';
    final fields = line.trimRight().split(',');
    final damaged = fields.length == 7 && fields[5] == 'true';
    if (fields.last == 'refused' || damaged) {
      File('$outDir/${f[1]}').writeAsStringSync(line);
      continue;
    }
    // an accepted read writes its write-back wire, whatever it owes in text
    final saved = wireResave(f[2], f[3], wire);
    if (saved == null) {
      spillAbsent(outDir, f[1]);
      continue;
    }
    File('$outDir/${f[1]}.wire').writeAsBytesSync(saved);
    if (noValue) {
      File('$outDir/${f[1]}').writeAsStringSync(line);
      continue;
    }
    final text = jsonWriteText(f[2], f[3], wire);
    if (text == null) {
      spillAbsent(outDir, f[1]);
      continue;
    }
    File('$outDir/${f[1]}').writeAsStringSync(line + text);
  }
  return 0;
}

// The report line, then the §16 text of the value the read left when the
// read reads, as `framing` answers a row that owes its value.
int surfaceReport(String outDir) {
  for (final f in kind('report')) {
    final wire = File(f[4]).readAsBytesSync();
    final line = loadReport(f[2], f[3], wire);
    if (line == null) {
      spillAbsent(outDir, f[1]);
      continue;
    }
    final fields = line.trimRight().split(',');
    final damaged = fields.length == 7 && fields[5] == 'true';
    if (fields.last == 'refused' || damaged) {
      File('$outDir/${f[1]}').writeAsStringSync(line);
      continue;
    }
    final text = jsonWriteText(f[2], f[3], wire);
    final saved = wireResave(f[2], f[3], wire);
    if (text == null || saved == null) {
      // no text or save writer for this root: the line alone, and the value
      // (and its write-back wire) absent
      File('$outDir/${f[1]}').writeAsStringSync(line);
      spillAbsent(outDir, f[1]);
      continue;
    }
    File('$outDir/${f[1]}').writeAsStringSync(line + text);
    // the write-back wire of the accepted read
    File('$outDir/${f[1]}.wire').writeAsBytesSync(saved);
  }
  return 0;
}

// ---- the MESSAGE FORM (docs/SPEC-TABLES.md §3.3) ----
//
// One `message` row reads its connection's announcement into a fresh
// vocabulary, loads the batch against it into 256 bodies of caller storage
// ("LoadMessages reads one buffer into the caller's storage and allocates
// nothing"), and saves the bodies read back into one buffer ("SaveMessages
// takes an array of bodies of ONE root and writes ONE buffer"), sized by
// MeasureMessages. The harness compares those bytes with the message golden,
// as test/conformance/go/messages.go does for the Go leg.

typedef MessageRun = Uint8List? Function(Uint8List announcement, Uint8List wire);

// A MessageFuzz answers one wire-fuzz mutant of a form-2 roster entry with the
// reply wireFuzz writes (docs/SPEC-TABLES.md §3.3, §4.2).
typedef MessageFuzz = List<int> Function(Uint8List wire);

// MessageCodec is one fixed root's batch verbs, bound twice: the `message`
// surface's batch read against a connection's announcement, and the wire
// fuzzer's mutant read against the unit's OWN announcement.
final class MessageCodec {
  const MessageCodec(this.batch, this.fuzz);
  final MessageRun batch;
  final MessageFuzz fuzz;
}

// _ownAnnouncement is the unit's compiled announcement, which the fuzzer's
// vocabulary reads: the roster names the form and never carries one, because
// the vocabulary is a pure function of the build version (§3.3).
Uint8List _ownAnnouncement(int Function() measure, int Function(Uint8List) announce) {
  final out = Uint8List(measure());
  announce(out);
  return out;
}

// _messageRow binds one fixed root's batch verbs. The vocabulary and report
// classes are each unit home's own, so the binding is generic over them.
MessageCodec _messageRow<T, V, R>({
  required T Function() fresh,
  required void Function(T) init,
  required V Function() vocabulary,
  required R Function() report,
  required Uint8List Function() own,
  required bool Function(V, Uint8List, R) announceRead,
  required int Function(List<T>, Uint8List, V, R) load,
  required int Function(List<T>, R) measure,
  required int Function(List<T>, Uint8List, R) save,
  required bool Function(R) damaged,
}) {
  // THE FUZZ ARM (docs/SPEC-TABLES.md §3.3, §4.2), as messageRow in
  // test/conformance/go/messages.go runs it with fuzz=true: the batch is read
  // into the same 256 bodies of caller storage the Go reference reads it into,
  // so a batch of up to 256 bodies is never refused as too large, and the
  // first body is saved back whatever the batch claims. A message root has no
  // LoadMeasure, so the reply carries loaded=1 and measure=-1, as the Go
  // leg's does.
  V? ownVocabulary;
  var ownRefused = false;
  final storage = List<T>.generate(256, (_) => fresh());
  final first = storage.sublist(0, 1);
  MessageFuzz fuzz = (Uint8List wire) {
    final values = storage;
    for (final v in values) {
      init(v);
    }
    final dynamic rep = report();
    if (ownVocabulary == null && !ownRefused) {
      final vocab = vocabulary();
      if (announceRead(vocab, own(), report())) {
        ownVocabulary = vocab;
      } else {
        ownRefused = true;
      }
    }
    final vocab = ownVocabulary;
    // THE LOAD'S ANSWER IS READ, never dropped: LoadMessages answers the
    // bodies it read, -1 on failure (§3.3), and the Go reference takes
    // `count, ok := load(...)` and sets `rep.refused = rep.refused || !ok
    // && !rep.refused` (test/conformance/go/messages.go, messageRow). A load
    // that failed without a refusal is malformed, whatever the report says,
    // so a broken batch is never saved back as if whole.
    var count = -1;
    if (vocab == null) {
      (rep as dynamic).Refuse('no_vocabulary');
    } else {
      count = load(values, wire, vocab, rep as R);
    }
    final ok = count >= 0;
    List<int>? saved;
    final n = measure(first, report());
    if (n >= 0) {
      final out = Uint8List(n);
      if (save(first, out, report()) == n) {
        saved = out;
      }
    }
    return _fuzzReply(
      loaded: true,
      unknown: rep.unknown as int,
      clamped: 0 as int,
      malformed: (rep.refused as bool) || !ok && !(rep.refused as bool),
      refused: rep.refused as bool,
      measure: -1,
      saved: saved,
    );
  };
  return MessageCodec((Uint8List announcement, Uint8List wire) {
    final vocab = vocabulary();
    if (!announceRead(vocab, announcement, report())) {
      return null;
    }
    final values = List<T>.generate(256, (_) {
      final v = fresh();
      init(v);
      return v;
    });
    final rep = report();
    final count = load(values, wire, vocab, rep);
    if (count < 1 || count > 256 || damaged(rep)) {
      return null;
    }
    final bodies = values.sublist(0, count);
    final n = measure(bodies, report());
    if (n < 0) {
      return null;
    }
    final out = Uint8List(n);
    if (save(bodies, out, report()) != n) {
      return null;
    }
    return out;
  }, fuzz);
}

List<E> _entries<E>(E Function() zero) => List<E>.generate(4096, (_) => zero());

final Map<String, MessageCodec> messageRuns = {
  'backenddemo LoginRequest': _messageRow<backenddemo_fixed.LoginRequest, backenddemo_fixed.TableVocabulary, backenddemo_fixed.TableReport>(
    fresh: backenddemo_fixed.LoginRequest.new,
    init: backenddemo_fixed.initLoginRequest,
    vocabulary: () => backenddemo_fixed.TableVocabulary(entries: _entries(() => backenddemo_fixed.TableMessageEntry(0, 0))),
    report: backenddemo_fixed.TableReport.new,
    own: () => _ownAnnouncement(backenddemo_fixed.announceMeasure, backenddemo_fixed.announce),
    announceRead: backenddemo_fixed.announceRead,
    load: backend.loginRequestLoadMessages,
    measure: backend.loginRequestMeasureMessages,
    save: backend.loginRequestSaveMessages,
    damaged: (r) => r.refused || r.refused,
  ),
  'backenddemo MatchResult': _messageRow<backenddemo_fixed.MatchResult, backenddemo_fixed.TableVocabulary, backenddemo_fixed.TableReport>(
    fresh: backenddemo_fixed.MatchResult.new,
    init: backenddemo_fixed.initMatchResult,
    vocabulary: () => backenddemo_fixed.TableVocabulary(entries: _entries(() => backenddemo_fixed.TableMessageEntry(0, 0))),
    report: backenddemo_fixed.TableReport.new,
    own: () => _ownAnnouncement(backenddemo_fixed.announceMeasure, backenddemo_fixed.announce),
    announceRead: backenddemo_fixed.announceRead,
    load: backend.matchResultLoadMessages,
    measure: backend.matchResultMeasureMessages,
    save: backend.matchResultSaveMessages,
    damaged: (r) => r.refused || r.refused,
  ),
  'backenddemo StorePurchase': _messageRow<backenddemo_fixed.StorePurchase, backenddemo_fixed.TableVocabulary, backenddemo_fixed.TableReport>(
    fresh: backenddemo_fixed.StorePurchase.new,
    init: backenddemo_fixed.initStorePurchase,
    vocabulary: () => backenddemo_fixed.TableVocabulary(entries: _entries(() => backenddemo_fixed.TableMessageEntry(0, 0))),
    report: backenddemo_fixed.TableReport.new,
    own: () => _ownAnnouncement(backenddemo_fixed.announceMeasure, backenddemo_fixed.announce),
    announceRead: backenddemo_fixed.announceRead,
    load: backend.storePurchaseLoadMessages,
    measure: backend.storePurchaseMeasureMessages,
    save: backend.storePurchaseSaveMessages,
    damaged: (r) => r.refused || r.refused,
  ),
  'backenddemo Envelope': _messageRow<backenddemo_fixed.Envelope, backenddemo_fixed.TableVocabulary, backenddemo_fixed.TableReport>(
    fresh: backenddemo_fixed.Envelope.new,
    init: backenddemo_fixed.initEnvelope,
    vocabulary: () => backenddemo_fixed.TableVocabulary(entries: _entries(() => backenddemo_fixed.TableMessageEntry(0, 0))),
    report: backenddemo_fixed.TableReport.new,
    own: () => _ownAnnouncement(backenddemo_fixed.announceMeasure, backenddemo_fixed.announce),
    announceRead: backenddemo_fixed.announceRead,
    load: backend.envelopeLoadMessages,
    measure: backend.envelopeMeasureMessages,
    save: backend.envelopeSaveMessages,
    damaged: (r) => r.refused || r.refused,
  ),
  'vocabdemo Wide00': _messageRow<vocabdemo_fixed.Wide00, vocabdemo_fixed.TableVocabulary, vocabdemo_fixed.TableReport>(
    fresh: vocabdemo_fixed.Wide00.new,
    init: vocabdemo_fixed.initWide00,
    vocabulary: () => vocabdemo_fixed.TableVocabulary(entries: _entries(() => vocabdemo_fixed.TableMessageEntry(0, 0))),
    report: vocabdemo_fixed.TableReport.new,
    own: () => _ownAnnouncement(vocabdemo_fixed.announceMeasure, vocabdemo_fixed.announce),
    announceRead: vocabdemo_fixed.announceRead,
    load: vocab.wide00LoadMessages,
    measure: vocab.wide00MeasureMessages,
    save: vocab.wide00SaveMessages,
    damaged: (r) => r.refused || r.refused,
  ),
  'vocabdemo Wide09': _messageRow<vocabdemo_fixed.Wide09, vocabdemo_fixed.TableVocabulary, vocabdemo_fixed.TableReport>(
    fresh: vocabdemo_fixed.Wide09.new,
    init: vocabdemo_fixed.initWide09,
    vocabulary: () => vocabdemo_fixed.TableVocabulary(entries: _entries(() => vocabdemo_fixed.TableMessageEntry(0, 0))),
    report: vocabdemo_fixed.TableReport.new,
    own: () => _ownAnnouncement(vocabdemo_fixed.announceMeasure, vocabdemo_fixed.announce),
    announceRead: vocabdemo_fixed.announceRead,
    load: vocab.wide09LoadMessages,
    measure: vocab.wide09MeasureMessages,
    save: vocab.wide09SaveMessages,
    damaged: (r) => r.refused || r.refused,
  ),
  'vocab9demo Wide00': _messageRow<vocab9demo_fixed.Wide00, vocab9demo_fixed.TableVocabulary, vocab9demo_fixed.TableReport>(
    fresh: vocab9demo_fixed.Wide00.new,
    init: vocab9demo_fixed.initWide00,
    vocabulary: () => vocab9demo_fixed.TableVocabulary(entries: _entries(() => vocab9demo_fixed.TableMessageEntry(0, 0))),
    report: vocab9demo_fixed.TableReport.new,
    own: () => _ownAnnouncement(vocab9demo_fixed.announceMeasure, vocab9demo_fixed.announce),
    announceRead: vocab9demo_fixed.announceRead,
    load: vocab9.wide00LoadMessages,
    measure: vocab9.wide00MeasureMessages,
    save: vocab9.wide00SaveMessages,
    damaged: (r) => r.refused || r.refused,
  ),
  'vocab9demo Wide19': _messageRow<vocab9demo_fixed.Wide19, vocab9demo_fixed.TableVocabulary, vocab9demo_fixed.TableReport>(
    fresh: vocab9demo_fixed.Wide19.new,
    init: vocab9demo_fixed.initWide19,
    vocabulary: () => vocab9demo_fixed.TableVocabulary(entries: _entries(() => vocab9demo_fixed.TableMessageEntry(0, 0))),
    report: vocab9demo_fixed.TableReport.new,
    own: () => _ownAnnouncement(vocab9demo_fixed.announceMeasure, vocab9demo_fixed.announce),
    announceRead: vocab9demo_fixed.announceRead,
    load: vocab9.wide19LoadMessages,
    measure: vocab9.wide19MeasureMessages,
    save: vocab9.wide19SaveMessages,
    damaged: (r) => r.refused || r.refused,
  ),
};

int surfaceMessage(String outDir) {
  for (final f in kind('message')) {
    List<String>? conn;
    for (final c in kind('connection')) {
      if (c[1] == f[2]) {
        conn = c;
        break;
      }
    }
    if (conn == null) {
      stderr.writeln('message ${f[1]}: no connection row ${f[2]}');
      return 1;
    }
    final run = messageRuns['${conn[2]} ${f[3]}'];
    if (f[5] == '-') {
      // A named refusal row: the root is a variable table, which carries no
      // message verbs (docs/SPEC-TABLES.md §3.3: the message form is fixed
      // tables only), so the driver answers ABSENT and names the reason.
      if (run != null) {
        stderr.writeln('message ${f[1]}: a variable table carries a message batch');
        return 1;
      }
      File('$outDir/${f[1]}.absent').writeAsStringSync('variable table: the message form is fixed tables only (SPEC-TABLES §3.3)');
      continue;
    }
    if (run == null) {
      File('$outDir/${f[1]}.absent').writeAsStringSync('');
      continue;
    }
    final answer = run.batch(File(conn[4]).readAsBytesSync(), File(f[5]).readAsBytesSync());
    if (answer == null) {
      stderr.writeln('message ${f[1]}: the batch did not load and save');
      return 1;
    }
    File('$outDir/${f[1]}').writeAsBytesSync(answer);
  }
  return 0;
}

const String surfaces =
    // wire, json-read, json-write and report answer the variable class for
    // graphdemo, blobdemo and listdemo (docs/SPEC-TABLES.md §3, §16.7), and
    // cook-write the graphdemo and blobdemo rows (§7.6). retain and
    // retain-save carry RT1's retaining round trip (§6.6). The
    // accelerators and the forgery surfaces stay, and cook-reason and
    // block-reason name each forgery's first failing clause (§7, §19.2). The
    // wire fuzzer's roots are another card's.
    ''
    'wire\nmessage\nretain\nretain-save\njson-read\njson-write\njson-hostile\nreport\nframing\n'
    'cook\ncook-write\ncook-foreign\nblock\nblock-foreign\nblock-dump\n'
    'forgery\ncook-forgery\ncook-reason\nblock-reason\n';

void main(List<String> args) {
  if (args.isNotEmpty && args[0] == 'wire-fuzz') {
    wireFuzz().then((_) => exit(0)).catchError((Object e) {
      stderr.writeln('wire-fuzz: $e');
      exit(1);
    });
    return;
  }
  exit(run(args));
}

// ---- the WIRE-FUZZ driver (test/conformance/harness/wirefuzz.go) ----
//
// The harness owns the mutations and the oracle; this process only binds the
// roster entry to the generated Card codec and returns the report and the
// saved value. The protocol is the binary one the Go leg speaks
// (test/conformance/go/wire_fuzz.go): a roster of (unit, root, form, retain),
// one availability byte a root, then for each mutant an index, a length, the
// wire bytes, and back the loaded flag, the six counters, the verdict, the
// region measure and the saved bytes.

class _ByteSource {
  final List<int> _buf = <int>[];
  final List<Completer<void>> _waiters = <Completer<void>>[];
  bool _done = false;

  _ByteSource() {
    stdin.listen((List<int> chunk) {
      _buf.addAll(chunk);
      _flush();
    }, onDone: () {
      _done = true;
      _flush();
    });
  }

  void _flush() {
    while (_waiters.isNotEmpty) {
      _waiters.removeAt(0).complete();
    }
  }

  Future<Uint8List> read(int n) async {
    while (_buf.length < n && !_done) {
      final c = Completer<void>();
      _waiters.add(c);
      await c.future;
    }
    final take = _buf.length < n ? _buf.length : n;
    final out = Uint8List.fromList(_buf.sublist(0, take));
    _buf.removeRange(0, take);
    return out;
  }
}

void _putU8(List<int> out, int v) => out.add(v & 0xFF);
void _putU32(List<int> out, int v) {
  for (var i = 0; i < 4; i++) {
    out.add((v >> (8 * i)) & 0xFF);
  }
}

void _putU64(List<int> out, int v) {
  for (var i = 0; i < 8; i++) {
    out.add((v >> (8 * i)) & 0xFF);
  }
}

Future<String> _readString(_ByteSource src) async {
  final n = ByteData.sublistView(await src.read(2)).getUint16(0, Endian.little);
  return String.fromCharCodes(await src.read(n));
}

// _fuzzReply is one reply in the harness's layout: loaded, the five counters,
// malformed, the refusal verdict, the measure, the two retention counters,
// and the saved length and bytes (-1 for a save that failed), as
// test/conformance/go/wire_fuzz.go writes it.
List<int> _fuzzReply({
  required bool loaded,
  int unknown = 0,
  int kindMismatch = 0,
  int widened = 0,
  int clamped = 0,
  int duplicate = 0,
  bool malformed = false,
  bool refused = false,
  required int measure,
  int retained = 0,
  int retainLost = 0,
  List<int>? saved,
}) {
  final out = <int>[];
  _putU8(out, loaded ? 1 : 0);
  _putU32(out, unknown);
  _putU32(out, kindMismatch);
  _putU32(out, widened);
  _putU32(out, clamped);
  _putU32(out, duplicate);
  _putU8(out, malformed ? 1 : 0);
  _putU8(out, refused ? 1 : 0);
  _putU64(out, measure);
  _putU32(out, retained);
  _putU32(out, retainLost);
  if (saved == null) {
    _putU64(out, -1);
  } else {
    _putU64(out, saved.length);
    out.addAll(saved);
  }
  return out;
}

// A _FuzzRoot is one file root's binding for the wire-fuzz stream: the
// ordinary Load/Save run and, where the roster asks for it, the retaining
// pair. Every root the wire surface answers is bound by fuzzRoot below, so the
// fuzzer reads the whole corpus, not one table.
class _FuzzRoot {
  final _FuzzRun plain;
  final _FuzzRun? retain;
  const _FuzzRoot(this.plain, {this.retain});
}

typedef _FuzzRun = List<int> Function(Uint8List wire);

_FuzzRun _plainCodec<V, R>(
  R Function() mkReport,
  int Function(Uint8List) measure,
  V Function(Uint8List, [R?]) load,
  Uint8List Function(V) save,
) {
  return (Uint8List wire) {
    final report = mkReport();
    final m = measure(wire);
    final value = load(wire, report);
    final dynamic r=report;
    final loaded=!(r.refused as bool);
    return _fuzzReplyFor(r,loaded,m,loaded ? _refusable(() => save(value)) : null);
  };
}

// The retention arm (docs/SPEC-TABLES.md §6.6): read and write back through
// the retaining pair, with the two caller-owned stores the harness fixes
// (1 MiB of bytes and one id per eight bytes).
_FuzzRun _retainCodec<V, R, T>(
  T Function(int, List<int>) mkRetain,
  R Function() mkReport,
  int Function(Uint8List) measure,
  V Function(Uint8List, T, R) load,
  Uint8List Function(V, T, R) save,
) {
  return (Uint8List wire) {
    final report = mkReport();
    final retain = mkRetain(1048576, List<int>.filled(131072, 0));
    final m = measure(wire);
    final value = load(wire, retain, report);
    final dynamic r=report;
    final loaded=!(r.refused as bool);
    return _fuzzReplyFor(r,loaded,m,loaded ? _refusable(() => save(value, retain, report)) : null);
  };
}

// A save the value cannot honour is a REFUSAL, a StateError by name here (a
// cycle, an arm tag naming no arm): the harness reads it as a save that wrote
// nothing, the -1 in the reply.
List<int>? _refusable(Uint8List Function() save) {
  try {
    return save();
  } on StateError {
    return null;
  }
}

List<int> _fuzzReplyFor(dynamic r, bool loaded, int measure, List<int>? saved) {
  return _fuzzReply(
    loaded: loaded,
    unknown: r.unknown as int,
    kindMismatch: 0 as int,
    widened: r.widened as int,
    clamped: 0 as int,
    duplicate: 0 as int,
    malformed: r.refused as bool,
    // Builder count refusal rides the completion/save-failure channel;
    // the shared builder oracle keeps its ordinary report counters.
    refused: (r.refused as bool) && r.reason != 'count_over_extent_cap',
    measure: measure,
    retained: r.retained as int,
    retainLost: r.retainLost as int,
    saved: saved,
  );
}

_FuzzRoot? fuzzRoot(String unit, String root) {
  switch ('$unit $root') {
    case 'featurewitnesses Root':
      return _FuzzRoot(
        _plainCodec(feature.TableWireReport.new,feature.rootWireMeasure,feature.rootWireLoad,feature.rootWireSave),
        retain:_retainCodec(feature.TableRetain.new,feature.TableWireReport.new,feature.rootWireMeasure,feature.rootWireLoadRetain,feature.rootWireSaveRetain));
    case 'featurewitnesses ArrayRoot':
      return _FuzzRoot(
        _plainCodec(feature.TableWireReport.new,feature.arrayRootWireMeasure,feature.arrayRootWireLoad,feature.arrayRootWireSave),
        retain:_retainCodec(feature.TableRetain.new,feature.TableWireReport.new,feature.arrayRootWireMeasure,feature.arrayRootWireLoadRetain,feature.arrayRootWireSaveRetain));

    case 'cards Card':
      return _FuzzRoot(
          _plainCodec(TableWireReport.new, cardWireMeasure, cardWireLoad, cardWireSave),
          retain: _retainCodec(TableRetain.new, TableWireReport.new, cardWireMeasure, cardWireLoadRetain, cardWireSaveRetain));
    case 'backenddemo LoginRequest':
      return _FuzzRoot(
          _plainCodec(backenddemo.TableWireReport.new, backenddemo.loginRequestWireMeasure, backenddemo.loginRequestWireLoad, backenddemo.loginRequestWireSave));
    case 'backenddemo MatchResult':
      return _FuzzRoot(
          _plainCodec(backenddemo.TableWireReport.new, backenddemo.matchResultWireMeasure, backenddemo.matchResultWireLoad, backenddemo.matchResultWireSave));
    case 'backenddemo StorePurchase':
      return _FuzzRoot(
          _plainCodec(backenddemo.TableWireReport.new, backenddemo.storePurchaseWireMeasure, backenddemo.storePurchaseWireLoad, backenddemo.storePurchaseWireSave));
    case 'blobdemo Catalog':
      return _FuzzRoot(
          _plainCodec(blobdemo.TableWireReport.new, blobdemo.catalogWireMeasure, blobdemo.catalogWireLoad, blobdemo.catalogWireSave),
          retain: _retainCodec(blobdemo.TableRetain.new, blobdemo.TableWireReport.new, blobdemo.catalogWireMeasure, blobdemo.catalogWireLoadRetain, blobdemo.catalogWireSaveRetain));
    case 'graphdemo Scene':
      return _FuzzRoot(
          _plainCodec(graphdemo.TableWireReport.new, graphdemo.sceneWireMeasure, graphdemo.sceneWireLoad, graphdemo.sceneWireSave),
          retain: _retainCodec(graphdemo.TableRetain.new, graphdemo.TableWireReport.new, graphdemo.sceneWireMeasure, graphdemo.sceneWireLoadRetain, graphdemo.sceneWireSaveRetain));
    case 'listdemo Album':
      return _FuzzRoot(
          _plainCodec(listdemo.TableWireReport.new, listdemo.albumWireMeasure, listdemo.albumWireLoad, listdemo.albumWireSave),
          retain: _retainCodec(listdemo.TableRetain.new, listdemo.TableWireReport.new, listdemo.albumWireMeasure, listdemo.albumWireLoadRetain, listdemo.albumWireSaveRetain));
    case 'listdemo Army':
      return _FuzzRoot(
          _plainCodec(listdemo.TableWireReport.new, listdemo.armyWireMeasure, listdemo.armyWireLoad, listdemo.armyWireSave),
          retain: _retainCodec(listdemo.TableRetain.new, listdemo.TableWireReport.new, listdemo.armyWireMeasure, listdemo.armyWireLoadRetain, listdemo.armyWireSaveRetain));
    case 'listdemo Mixed':
      return _FuzzRoot(
          _plainCodec(listdemo.TableWireReport.new, listdemo.mixedWireMeasure, listdemo.mixedWireLoad, listdemo.mixedWireSave),
          retain: _retainCodec(listdemo.TableRetain.new, listdemo.TableWireReport.new, listdemo.mixedWireMeasure, listdemo.mixedWireLoadRetain, listdemo.mixedWireSaveRetain));
    case 'listdemo Save':
      return _FuzzRoot(
          _plainCodec(listdemo.TableWireReport.new, listdemo.saveWireMeasure, listdemo.saveWireLoad, listdemo.saveWireSave),
          retain: _retainCodec(listdemo.TableRetain.new, listdemo.TableWireReport.new, listdemo.saveWireMeasure, listdemo.saveWireLoadRetain, listdemo.saveWireSaveRetain));
    case 'listdemo Sheet':
      return _FuzzRoot(
          _plainCodec(listdemo.TableWireReport.new, listdemo.sheetWireMeasure, listdemo.sheetWireLoad, listdemo.sheetWireSave),
          retain: _retainCodec(listdemo.TableRetain.new, listdemo.TableWireReport.new, listdemo.sheetWireMeasure, listdemo.sheetWireLoadRetain, listdemo.sheetWireSaveRetain));
    case 'listdemo Unbounded':
      return _FuzzRoot(
          _plainCodec(listdemo.TableWireReport.new, listdemo.unboundedWireMeasure, listdemo.unboundedWireLoad, listdemo.unboundedWireSave),
          retain: _retainCodec(listdemo.TableRetain.new, listdemo.TableWireReport.new, listdemo.unboundedWireMeasure, listdemo.unboundedWireLoadRetain, listdemo.unboundedWireSaveRetain));
    case 'mapdemo Cells':
      return _FuzzRoot(
          _plainCodec(mapdemo.TableWireReport.new, mapdemo.cellsWireMeasure, mapdemo.cellsWireLoad, mapdemo.cellsWireSave),
          retain: _retainCodec(mapdemo.TableRetain.new, mapdemo.TableWireReport.new, mapdemo.cellsWireMeasure, mapdemo.cellsWireLoadRetain, mapdemo.cellsWireSaveRetain));
    case 'mapdemo Chunks':
      return _FuzzRoot(
          _plainCodec(mapdemo.TableWireReport.new, mapdemo.chunksWireMeasure, mapdemo.chunksWireLoad, mapdemo.chunksWireSave),
          retain: _retainCodec(mapdemo.TableRetain.new, mapdemo.TableWireReport.new, mapdemo.chunksWireMeasure, mapdemo.chunksWireLoadRetain, mapdemo.chunksWireSaveRetain));
    case 'mapdemo Crews':
      return _FuzzRoot(
          _plainCodec(mapdemo.TableWireReport.new, mapdemo.crewsWireMeasure, mapdemo.crewsWireLoad, mapdemo.crewsWireSave),
          retain: _retainCodec(mapdemo.TableRetain.new, mapdemo.TableWireReport.new, mapdemo.crewsWireMeasure, mapdemo.crewsWireLoadRetain, mapdemo.crewsWireSaveRetain));
    case 'mapdemo Depth':
      return _FuzzRoot(
          _plainCodec(mapdemo.TableWireReport.new, mapdemo.depthWireMeasure, mapdemo.depthWireLoad, mapdemo.depthWireSave),
          retain: _retainCodec(mapdemo.TableRetain.new, mapdemo.TableWireReport.new, mapdemo.depthWireMeasure, mapdemo.depthWireLoadRetain, mapdemo.depthWireSaveRetain));
    case 'mapdemo Docs':
      return _FuzzRoot(
          _plainCodec(mapdemo.TableWireReport.new, mapdemo.docsWireMeasure, mapdemo.docsWireLoad, mapdemo.docsWireSave),
          retain: _retainCodec(mapdemo.TableRetain.new, mapdemo.TableWireReport.new, mapdemo.docsWireMeasure, mapdemo.docsWireLoadRetain, mapdemo.docsWireSaveRetain));
    case 'mapdemo Fleet':
      return _FuzzRoot(
          _plainCodec(mapdemo.TableWireReport.new, mapdemo.fleetWireMeasure, mapdemo.fleetWireLoad, mapdemo.fleetWireSave),
          retain: _retainCodec(mapdemo.TableRetain.new, mapdemo.TableWireReport.new, mapdemo.fleetWireMeasure, mapdemo.fleetWireLoadRetain, mapdemo.fleetWireSaveRetain));
    case 'mapdemo Pairs':
      return _FuzzRoot(
          _plainCodec(mapdemo.TableWireReport.new, mapdemo.pairsWireMeasure, mapdemo.pairsWireLoad, mapdemo.pairsWireSave),
          retain: _retainCodec(mapdemo.TableRetain.new, mapdemo.TableWireReport.new, mapdemo.pairsWireMeasure, mapdemo.pairsWireLoadRetain, mapdemo.pairsWireSaveRetain));
    case 'mapdemo Runs':
      return _FuzzRoot(
          _plainCodec(mapdemo.TableWireReport.new, mapdemo.runsWireMeasure, mapdemo.runsWireLoad, mapdemo.runsWireSave),
          retain: _retainCodec(mapdemo.TableRetain.new, mapdemo.TableWireReport.new, mapdemo.runsWireMeasure, mapdemo.runsWireLoadRetain, mapdemo.runsWireSaveRetain));
    case 'mapdemo Slots':
      return _FuzzRoot(
          _plainCodec(mapdemo.TableWireReport.new, mapdemo.slotsWireMeasure, mapdemo.slotsWireLoad, mapdemo.slotsWireSave),
          retain: _retainCodec(mapdemo.TableRetain.new, mapdemo.TableWireReport.new, mapdemo.slotsWireMeasure, mapdemo.slotsWireLoadRetain, mapdemo.slotsWireSaveRetain));
    case 'mapdemo Spans':
      return _FuzzRoot(
          _plainCodec(mapdemo.TableWireReport.new, mapdemo.spansWireMeasure, mapdemo.spansWireLoad, mapdemo.spansWireSave),
          retain: _retainCodec(mapdemo.TableRetain.new, mapdemo.TableWireReport.new, mapdemo.spansWireMeasure, mapdemo.spansWireLoadRetain, mapdemo.spansWireSaveRetain));
    case 'mapdemo Text':
      return _FuzzRoot(
          _plainCodec(mapdemo.TableWireReport.new, mapdemo.textWireMeasure, mapdemo.textWireLoad, mapdemo.textWireSave),
          retain: _retainCodec(mapdemo.TableRetain.new, mapdemo.TableWireReport.new, mapdemo.textWireMeasure, mapdemo.textWireLoadRetain, mapdemo.textWireSaveRetain));
    case 'mapdemo Trails':
      return _FuzzRoot(
          _plainCodec(mapdemo.TableWireReport.new, mapdemo.trailsWireMeasure, mapdemo.trailsWireLoad, mapdemo.trailsWireSave),
          retain: _retainCodec(mapdemo.TableRetain.new, mapdemo.TableWireReport.new, mapdemo.trailsWireMeasure, mapdemo.trailsWireLoadRetain, mapdemo.trailsWireSaveRetain));
    case 'messagedemo ToolMessage':
      return _FuzzRoot(
          _plainCodec(messagedemo.TableWireReport.new, messagedemo.toolMessageWireMeasure, messagedemo.toolMessageWireLoad, messagedemo.toolMessageWireSave));
    case 'scalars SimState':
      return _FuzzRoot(
          _plainCodec(scalardemo.TableWireReport.new, scalardemo.simStateWireMeasure, scalardemo.simStateWireLoad, scalardemo.simStateWireSave));
    case 'streamdemo Feed':
      return _FuzzRoot(
          _plainCodec(streamdemo.TableWireReport.new, streamdemo.feedWireMeasure, streamdemo.feedWireLoad, streamdemo.feedWireSave),
          retain: _retainCodec(streamdemo.TableRetain.new, streamdemo.TableWireReport.new, streamdemo.feedWireMeasure, streamdemo.feedWireLoadRetain, streamdemo.feedWireSaveRetain));
    case 'tabledemo ArchiveConfig':
      return _FuzzRoot(
          _plainCodec(tabledemo.TableWireReport.new, tabledemo.archiveConfigWireMeasure, tabledemo.archiveConfigWireLoad, tabledemo.archiveConfigWireSave));
    case 'tabledemo KeyedConfig':
      return _FuzzRoot(
          _plainCodec(tabledemo.TableWireReport.new, tabledemo.keyedConfigWireMeasure, tabledemo.keyedConfigWireLoad, tabledemo.keyedConfigWireSave));
    case 'tabledemo LoadoutConfig':
      return _FuzzRoot(
          _plainCodec(tabledemo.TableWireReport.new, tabledemo.loadoutConfigWireMeasure, tabledemo.loadoutConfigWireLoad, tabledemo.loadoutConfigWireSave));
    case 'tabledemo ProfileConfig':
      return _FuzzRoot(
          _plainCodec(tabledemo.TableWireReport.new, tabledemo.profileConfigWireMeasure, tabledemo.profileConfigWireLoad, tabledemo.profileConfigWireSave));
    case 'tabledemo RootConfig':
      return _FuzzRoot(
          _plainCodec(tabledemo.TableWireReport.new, tabledemo.rootConfigWireMeasure, tabledemo.rootConfigWireLoad, tabledemo.rootConfigWireSave));
    case 'tabledemo WideBlob':
      return _FuzzRoot(
          _plainCodec(tabledemo.TableWireReport.new, tabledemo.wideBlobWireMeasure, tabledemo.wideBlobWireLoad, tabledemo.wideBlobWireSave));
    case 'tbla2 Root':
      return _FuzzRoot(
          _plainCodec(tbla2.TableWireReport.new, tbla2.rootWireMeasure, tbla2.rootWireLoad, tbla2.rootWireSave));
    case 'tblg1 Guarded':
      return _FuzzRoot(
          _plainCodec(tblg1.TableWireReport.new, tblg1.guardedWireMeasure, tblg1.guardedWireLoad, tblg1.guardedWireSave),
          retain: _retainCodec(tblg1.TableRetain.new, tblg1.TableWireReport.new, tblg1.guardedWireMeasure, tblg1.guardedWireLoadRetain, tblg1.guardedWireSaveRetain));
    case 'tblk1 Root':
      return _FuzzRoot(
          _plainCodec(tblk1.TableWireReport.new, tblk1.rootWireMeasure, tblk1.rootWireLoad, tblk1.rootWireSave));
    case 'tblk2 Root':
      return _FuzzRoot(
          _plainCodec(tblk2.TableWireReport.new, tblk2.rootWireMeasure, tblk2.rootWireLoad, tblk2.rootWireSave));
    case 'tblm1 Msg':
      return _FuzzRoot(
          _plainCodec(tblm1.TableWireReport.new, tblm1.msgWireMeasure, tblm1.msgWireLoad, tblm1.msgWireSave));
    case 'tblm2 Msg':
      return _FuzzRoot(
          _plainCodec(tblm2.TableWireReport.new, tblm2.msgWireMeasure, tblm2.msgWireLoad, tblm2.msgWireSave));
    case 'tblp1 Chain':
      return _FuzzRoot(
          _plainCodec(tblp1.TableWireReport.new, tblp1.chainWireMeasure, tblp1.chainWireLoad, tblp1.chainWireSave));
    case 'tblp2 Chain':
      return _FuzzRoot(
          _plainCodec(tblp2.TableWireReport.new, tblp2.chainWireMeasure, tblp2.chainWireLoad, tblp2.chainWireSave),
          retain: _retainCodec(tblp2.TableRetain.new, tblp2.TableWireReport.new, tblp2.chainWireMeasure, tblp2.chainWireLoadRetain, tblp2.chainWireSaveRetain));
    case 'tblp3 Chain':
      return _FuzzRoot(
          _plainCodec(tblp3.TableWireReport.new, tblp3.chainWireMeasure, tblp3.chainWireLoad, tblp3.chainWireSave));
    case 'tblr1 Cfg':
      return _FuzzRoot(
          _plainCodec(tblr1.TableWireReport.new, tblr1.cfgWireMeasure, tblr1.cfgWireLoad, tblr1.cfgWireSave));
    case 'tblr2 Cfg':
      return _FuzzRoot(
          _plainCodec(tblr2.TableWireReport.new, tblr2.cfgWireMeasure, tblr2.cfgWireLoad, tblr2.cfgWireSave));
    case 'tblscalars2 SimState':
      return _FuzzRoot(
          _plainCodec(scalardemo2.TableWireReport.new, scalardemo2.simStateWireMeasure, scalardemo2.simStateWireLoad, scalardemo2.simStateWireSave));
    case 'tblv1 Cfg':
      return _FuzzRoot(
          _plainCodec(tblv1.TableWireReport.new, tblv1.cfgWireMeasure, tblv1.cfgWireLoad, tblv1.cfgWireSave));
    case 'tblv2 Cfg':
      return _FuzzRoot(
          _plainCodec(tblv2.TableWireReport.new, tblv2.cfgWireMeasure, tblv2.cfgWireLoad, tblv2.cfgWireSave));
    case 'tblw1 Fleet':
      return _FuzzRoot(
          _plainCodec(tblw1.TableWireReport.new, tblw1.fleetWireMeasure, tblw1.fleetWireLoad, tblw1.fleetWireSave),
          retain: _retainCodec(tblw1.TableRetain.new, tblw1.TableWireReport.new, tblw1.fleetWireMeasure, tblw1.fleetWireLoadRetain, tblw1.fleetWireSaveRetain));
    case 'tblw2 Fleet':
      return _FuzzRoot(
          _plainCodec(tblw2.TableWireReport.new, tblw2.fleetWireMeasure, tblw2.fleetWireLoad, tblw2.fleetWireSave),
          retain: _retainCodec(tblw2.TableRetain.new, tblw2.TableWireReport.new, tblw2.fleetWireMeasure, tblw2.fleetWireLoadRetain, tblw2.fleetWireSaveRetain));
    case 'vocab9demo Wide00':
      return _FuzzRoot(
          _plainCodec(vocab9demo.TableWireReport.new, vocab9demo.wide00WireMeasure, vocab9demo.wide00WireLoad, vocab9demo.wide00WireSave));
    case 'vocab9demo Wide19':
      return _FuzzRoot(
          _plainCodec(vocab9demo.TableWireReport.new, vocab9demo.wide19WireMeasure, vocab9demo.wide19WireLoad, vocab9demo.wide19WireSave));
    case 'vocabdemo Wide00':
      return _FuzzRoot(
          _plainCodec(vocabdemo.TableWireReport.new, vocabdemo.wide00WireMeasure, vocabdemo.wide00WireLoad, vocabdemo.wide00WireSave));
    case 'vocabdemo Wide09':
      return _FuzzRoot(
          _plainCodec(vocabdemo.TableWireReport.new, vocabdemo.wide09WireMeasure, vocabdemo.wide09WireLoad, vocabdemo.wide09WireSave));
    case 'widedemo Caption':
      return _FuzzRoot(
          _plainCodec(widedemo.TableWireReport.new, widedemo.captionWireMeasure, widedemo.captionWireLoad, widedemo.captionWireSave));
    case 'widedemo Stamp':
      return _FuzzRoot(
          _plainCodec(widedemo.TableWireReport.new, widedemo.stampWireMeasure, widedemo.stampWireLoad, widedemo.stampWireSave));
    case 'widelist Wide':
      return _FuzzRoot(
          _plainCodec(widelist.TableWireReport.new, widelist.wideWireMeasure, widelist.wideWireLoad, widelist.wideWireSave),
          retain: _retainCodec(widelist.TableRetain.new, widelist.TableWireReport.new, widelist.wideWireMeasure, widelist.wideWireLoadRetain, widelist.wideWireSaveRetain));
  }
  return null;
}

Future<void> wireFuzz() async {
  final src = _ByteSource();
  final count = ByteData.sublistView(await src.read(4)).getUint32(0, Endian.little);
  final runs = <_FuzzRun?>[];
  final messages = <MessageFuzz?>[];
  final available = <int>[];
  for (var i = 0; i < count; i++) {
    final unit = await _readString(src);
    final root = await _readString(src);
    final form = (await src.read(1))[0];
    final retain = (await src.read(1))[0];
    final bound = form == 1 ? fuzzRoot(unit, root) : null;
    final run = bound == null ? null : (retain == 1 ? bound.retain : bound.plain);
    // a form-2 root rides the message arm (§3.3), as findMessageCodec binds
    // it in test/conformance/go/wire_fuzz.go; a variable table carries no
    // message verbs and so answers absent, as the Go leg's does
    final message = form == 2 && retain == 0 ? messageRuns['$unit $root']?.fuzz : null;
    runs.add(run);
    messages.add(message);
    available.add(run != null || message != null ? 1 : 0);
  }
  stdout.add(available);
  await stdout.flush();
  while (true) {
    final idxBytes = await src.read(4);
    if (idxBytes.length < 4) {
      break; // the harness closed the stream
    }
    final index = ByteData.sublistView(idxBytes).getUint32(0, Endian.little);
    final size = ByteData.sublistView(await src.read(4)).getUint32(0, Endian.little);
    final wire = await src.read(size);
    final message = messages[index];
    if (message != null) {
      stdout.add(message(wire));
    } else {
      stdout.add(runs[index]!(wire));
    }
    await stdout.flush();
  }
}

int run(List<String> args) {
  if (args.length < 2) {
    stderr.writeln(
      'usage: driver <manifest> list\n'
      '       driver <manifest> <surface> <outdir>',
    );
    return 2;
  }
  readManifest(args[0]);
  final surface = args[1];
  if (surface == 'list') {
    stdout.write(surfaces);
    return 0;
  }
  if (args.length < 3) {
    stderr.writeln('usage: driver <manifest> <surface> <outdir>');
    return 2;
  }
  final out = args[2];
  switch (surface) {
    case 'wire':
      return surfaceWire(out);
    case 'message':
      return surfaceMessage(out);
    case 'retain':
      return surfaceRetain(out, false);
    case 'retain-save':
      return surfaceRetain(out, true);
    case 'json-read':
      return surfaceJsonRead(out);
    case 'json-write':
      return surfaceJsonWrite(out);
    case 'json-hostile':
      return surfaceJsonHostile(out);
    case 'report':
      return surfaceReport(out);
    case 'framing':
      return surfaceFraming(out);
    case 'cook':
      return surfaceCook(out);
    case 'cook-write':
      return surfaceCookWrite(out);
    case 'cook-foreign':
      return surfaceCookForeign(out);
    case 'cook-forgery':
      return surfaceCookForgery(out);
    case 'block':
      return surfaceBlock(out);
    case 'block-foreign':
      return surfaceBlockForeign(out);
    case 'block-dump':
      return surfaceBlockDump(out);
    case 'forgery':
      return surfaceForgery(out);
    case 'cook-reason':
      return surfaceReason(out, 'cook');
    case 'block-reason':
      return surfaceReason(out, 'block');
  }
  return 2;
}
