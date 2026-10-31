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

Uint8List? wireResave(String unit, String root, Uint8List wire) {
  switch ('$unit $root') {
    case 'tblg1 Guarded':
      return tblg1.guardedWireSave(tblg1.guardedWireLoad(wire));
    case 'tblv1 Cfg':
      return tblv1.cfgWireSave(tblv1.cfgWireLoad(wire));
    case 'tblv2 Cfg':
      return tblv2.cfgWireSave(tblv2.cfgWireLoad(wire));
    case 'tblw1 Fleet':
      return tblw1.fleetWireSave(tblw1.fleetWireLoad(wire));
    case 'tblw2 Fleet':
      return tblw2.fleetWireSave(tblw2.fleetWireLoad(wire));
    case 'tblr1 Cfg':
      return tblr1.cfgWireSave(tblr1.cfgWireLoad(wire));
    case 'tblr2 Cfg':
      return tblr2.cfgWireSave(tblr2.cfgWireLoad(wire));
    case 'graphdemo Scene':
      return graphdemo.sceneWireSave(graphdemo.sceneWireLoad(wire));
    case 'blobdemo Catalog':
      return blobdemo.catalogWireSave(blobdemo.catalogWireLoad(wire));
    case 'listdemo Save':
      return listdemo.saveWireSave(listdemo.saveWireLoad(wire));
    case 'listdemo Album':
      return listdemo.albumWireSave(listdemo.albumWireLoad(wire));
    case 'listdemo Sheet':
      return listdemo.sheetWireSave(listdemo.sheetWireLoad(wire));
    case 'listdemo Unbounded':
      return listdemo.unboundedWireSave(listdemo.unboundedWireLoad(wire));
    case 'mapdemo Cells':
      return mapdemo.cellsWireSave(mapdemo.cellsWireLoad(wire));
    case 'mapdemo Chunks':
      return mapdemo.chunksWireSave(mapdemo.chunksWireLoad(wire));
    case 'mapdemo Crews':
      return mapdemo.crewsWireSave(mapdemo.crewsWireLoad(wire));
    case 'mapdemo Depth':
      return mapdemo.depthWireSave(mapdemo.depthWireLoad(wire));
    case 'mapdemo Docs':
      return mapdemo.docsWireSave(mapdemo.docsWireLoad(wire));
    case 'mapdemo Fleet':
      return mapdemo.fleetWireSave(mapdemo.fleetWireLoad(wire));
    case 'mapdemo Pairs':
      return mapdemo.pairsWireSave(mapdemo.pairsWireLoad(wire));
    case 'mapdemo Runs':
      return mapdemo.runsWireSave(mapdemo.runsWireLoad(wire));
    case 'mapdemo Slots':
      return mapdemo.slotsWireSave(mapdemo.slotsWireLoad(wire));
    case 'mapdemo Spans':
      return mapdemo.spansWireSave(mapdemo.spansWireLoad(wire));
    case 'mapdemo Text':
      return mapdemo.textWireSave(mapdemo.textWireLoad(wire));
    case 'mapdemo Trails':
      return mapdemo.trailsWireSave(mapdemo.trailsWireLoad(wire));
    case 'messagedemo ToolMessage':
      return messagedemo.toolMessageWireSave(messagedemo.toolMessageWireLoad(wire));
    case 'scalars SimState':
      return scalardemo.simStateWireSave(scalardemo.simStateWireLoad(wire));
    case 'widedemo Caption':
      return widedemo.captionWireSave(widedemo.captionWireLoad(wire));
    case 'widedemo Stamp':
      return widedemo.stampWireSave(widedemo.stampWireLoad(wire));
    case 'listdemo Army':
      return listdemo.armyWireSave(listdemo.armyWireLoad(wire));
    case 'listdemo Mixed':
      return listdemo.mixedWireSave(listdemo.mixedWireLoad(wire));
    case 'streamdemo Feed':
      return streamdemo.feedWireSave(streamdemo.feedWireLoad(wire));
    case 'tabledemo ArchiveConfig':
      return tabledemo.archiveConfigWireSave(tabledemo.archiveConfigWireLoad(wire));
    case 'tabledemo KeyedConfig':
      return tabledemo.keyedConfigWireSave(tabledemo.keyedConfigWireLoad(wire));
    case 'tabledemo WideBlob':
      return tabledemo.wideBlobWireSave(tabledemo.wideBlobWireLoad(wire));
    case 'tblm1 Msg':
      return tblm1.msgWireSave(tblm1.msgWireLoad(wire));
    case 'tblm2 Msg':
      return tblm2.msgWireSave(tblm2.msgWireLoad(wire));
    case 'tblp1 Chain':
      return tblp1.chainWireSave(tblp1.chainWireLoad(wire));
    case 'tblp3 Chain':
      return tblp3.chainWireSave(tblp3.chainWireLoad(wire));
    case 'vocabdemo Wide00':
      return vocabdemo.wide00WireSave(vocabdemo.wide00WireLoad(wire));
    case 'vocabdemo Wide09':
      return vocabdemo.wide09WireSave(vocabdemo.wide09WireLoad(wire));
    case 'vocab9demo Wide00':
      return vocab9demo.wide00WireSave(vocab9demo.wide00WireLoad(wire));
    case 'vocab9demo Wide19':
      return vocab9demo.wide19WireSave(vocab9demo.wide19WireLoad(wire));
    case 'backenddemo LoginRequest':
      return backenddemo.loginRequestWireSave(backenddemo.loginRequestWireLoad(wire));
    case 'backenddemo MatchResult':
      return backenddemo.matchResultWireSave(backenddemo.matchResultWireLoad(wire));
    case 'backenddemo StorePurchase':
      return backenddemo.storePurchaseWireSave(backenddemo.storePurchaseWireLoad(wire));
    case 'tabledemo LoadoutConfig':
      return tabledemo.loadoutConfigWireSave(tabledemo.loadoutConfigWireLoad(wire));
    case 'tabledemo ProfileConfig':
      return tabledemo.profileConfigWireSave(tabledemo.profileConfigWireLoad(wire));
    case 'tabledemo RootConfig':
      return tabledemo.rootConfigWireSave(tabledemo.rootConfigWireLoad(wire));
  }
  return null;
}

Uint8List? jsonReadSave(String unit, String root, String text) {
  switch ('$unit $root') {
    case 'tblw1 Fleet':
      return tblw1.fleetWireSave(tblw1.fleetFromJson(text));
    case 'tblw2 Fleet':
      return tblw2.fleetWireSave(tblw2.fleetFromJson(text));
    case 'graphdemo Scene':
      return graphdemo.sceneWireSave(graphdemo.sceneFromJson(text));
    case 'blobdemo Catalog':
      return blobdemo.catalogWireSave(blobdemo.catalogFromJson(text));
    case 'listdemo Save':
      return listdemo.saveWireSave(listdemo.saveFromJson(text));
    case 'listdemo Album':
      return listdemo.albumWireSave(listdemo.albumFromJson(text));
    case 'listdemo Sheet':
      return listdemo.sheetWireSave(listdemo.sheetFromJson(text));
    case 'listdemo Unbounded':
      return listdemo.unboundedWireSave(listdemo.unboundedFromJson(text));
    case 'mapdemo Cells':
      return mapdemo.cellsWireSave(mapdemo.cellsFromJson(text));
    case 'mapdemo Chunks':
      return mapdemo.chunksWireSave(mapdemo.chunksFromJson(text));
    case 'mapdemo Crews':
      return mapdemo.crewsWireSave(mapdemo.crewsFromJson(text));
    case 'mapdemo Depth':
      return mapdemo.depthWireSave(mapdemo.depthFromJson(text));
    case 'mapdemo Docs':
      return mapdemo.docsWireSave(mapdemo.docsFromJson(text));
    case 'mapdemo Fleet':
      return mapdemo.fleetWireSave(mapdemo.fleetFromJson(text));
    case 'mapdemo Pairs':
      return mapdemo.pairsWireSave(mapdemo.pairsFromJson(text));
    case 'mapdemo Runs':
      return mapdemo.runsWireSave(mapdemo.runsFromJson(text));
    case 'mapdemo Slots':
      return mapdemo.slotsWireSave(mapdemo.slotsFromJson(text));
    case 'mapdemo Spans':
      return mapdemo.spansWireSave(mapdemo.spansFromJson(text));
    case 'mapdemo Text':
      return mapdemo.textWireSave(mapdemo.textFromJson(text));
    case 'mapdemo Trails':
      return mapdemo.trailsWireSave(mapdemo.trailsFromJson(text));
    case 'messagedemo ToolMessage':
      return messagedemo.toolMessageWireSave(messagedemo.toolMessageFromJson(text));
    case 'scalars SimState':
      return scalardemo.simStateWireSave(scalardemo.simStateFromJson(text));
    case 'widedemo Caption':
      return widedemo.captionWireSave(widedemo.captionFromJson(text));
    case 'widedemo Stamp':
      return widedemo.stampWireSave(widedemo.stampFromJson(text));
    case 'listdemo Army':
      return listdemo.armyWireSave(listdemo.armyFromJson(text));
    case 'listdemo Mixed':
      return listdemo.mixedWireSave(listdemo.mixedFromJson(text));
    case 'streamdemo Feed':
      return streamdemo.feedWireSave(streamdemo.feedFromJson(text));
    case 'tabledemo ArchiveConfig':
      return tabledemo.archiveConfigWireSave(tabledemo.archiveConfigFromJson(text));
    case 'tabledemo KeyedConfig':
      return tabledemo.keyedConfigWireSave(tabledemo.keyedConfigFromJson(text));
    case 'tabledemo WideBlob':
      return tabledemo.wideBlobWireSave(tabledemo.wideBlobFromJson(text));
    case 'tblg1 Guarded':
      return tblg1.guardedWireSave(tblg1.guardedFromJson(text));
    case 'tblm1 Msg':
      return tblm1.msgWireSave(tblm1.msgFromJson(text));
    case 'tblm2 Msg':
      return tblm2.msgWireSave(tblm2.msgFromJson(text));
    case 'tblp1 Chain':
      return tblp1.chainWireSave(tblp1.chainFromJson(text));
    case 'tblp3 Chain':
      return tblp3.chainWireSave(tblp3.chainFromJson(text));
    case 'tblr1 Cfg':
      return tblr1.cfgWireSave(tblr1.cfgFromJson(text));
    case 'tblr2 Cfg':
      return tblr2.cfgWireSave(tblr2.cfgFromJson(text));
    case 'tblv1 Cfg':
      return tblv1.cfgWireSave(tblv1.cfgFromJson(text));
    case 'tblv2 Cfg':
      return tblv2.cfgWireSave(tblv2.cfgFromJson(text));
    case 'vocabdemo Wide00':
      return vocabdemo.wide00WireSave(vocabdemo.wide00FromJson(text));
    case 'vocabdemo Wide09':
      return vocabdemo.wide09WireSave(vocabdemo.wide09FromJson(text));
    case 'vocab9demo Wide00':
      return vocab9demo.wide00WireSave(vocab9demo.wide00FromJson(text));
    case 'vocab9demo Wide19':
      return vocab9demo.wide19WireSave(vocab9demo.wide19FromJson(text));
    case 'backenddemo LoginRequest':
      return backenddemo.loginRequestWireSave(backenddemo.loginRequestFromJson(text));
    case 'backenddemo MatchResult':
      return backenddemo.matchResultWireSave(backenddemo.matchResultFromJson(text));
    case 'backenddemo StorePurchase':
      return backenddemo.storePurchaseWireSave(backenddemo.storePurchaseFromJson(text));
    case 'tabledemo LoadoutConfig':
      return tabledemo.loadoutConfigWireSave(tabledemo.loadoutConfigFromJson(text));
    case 'tabledemo ProfileConfig':
      return tabledemo.profileConfigWireSave(tabledemo.profileConfigFromJson(text));
    case 'tabledemo RootConfig':
      return tabledemo.rootConfigWireSave(tabledemo.rootConfigFromJson(text));
  }
  return null;
}

String? jsonWriteText(String unit, String root, Uint8List wire) {
  switch ('$unit $root') {
    case 'graphdemo ListNode':
      return graphdemo.listNodeToJson(graphdemo.listNodeWireLoad(wire));
    case 'listdemo Bounded':
      return listdemo.boundedToJson(listdemo.boundedWireLoad(wire));
    case 'listdemo Ints':
      return listdemo.intsToJson(listdemo.intsWireLoad(wire));
    case 'listdemo Squad':
      return listdemo.squadToJson(listdemo.squadWireLoad(wire));
    case 'tblw1 Fleet':
      return tblw1.fleetToJson(tblw1.fleetWireLoad(wire));
    case 'tblw2 Fleet':
      return tblw2.fleetToJson(tblw2.fleetWireLoad(wire));
    case 'graphdemo Scene':
      return graphdemo.sceneToJson(graphdemo.sceneWireLoad(wire));
    case 'blobdemo Catalog':
      return blobdemo.catalogToJson(blobdemo.catalogWireLoad(wire));
    case 'listdemo Save':
      return listdemo.saveToJson(listdemo.saveWireLoad(wire));
    case 'listdemo Album':
      return listdemo.albumToJson(listdemo.albumWireLoad(wire));
    case 'listdemo Sheet':
      return listdemo.sheetToJson(listdemo.sheetWireLoad(wire));
    case 'listdemo Unbounded':
      return listdemo.unboundedToJson(listdemo.unboundedWireLoad(wire));
    case 'mapdemo Cells':
      return mapdemo.cellsToJson(mapdemo.cellsWireLoad(wire));
    case 'mapdemo Chunks':
      return mapdemo.chunksToJson(mapdemo.chunksWireLoad(wire));
    case 'mapdemo Crews':
      return mapdemo.crewsToJson(mapdemo.crewsWireLoad(wire));
    case 'mapdemo Depth':
      return mapdemo.depthToJson(mapdemo.depthWireLoad(wire));
    case 'mapdemo Docs':
      return mapdemo.docsToJson(mapdemo.docsWireLoad(wire));
    case 'mapdemo Fleet':
      return mapdemo.fleetToJson(mapdemo.fleetWireLoad(wire));
    case 'mapdemo Pairs':
      return mapdemo.pairsToJson(mapdemo.pairsWireLoad(wire));
    case 'mapdemo Runs':
      return mapdemo.runsToJson(mapdemo.runsWireLoad(wire));
    case 'mapdemo Slots':
      return mapdemo.slotsToJson(mapdemo.slotsWireLoad(wire));
    case 'mapdemo Spans':
      return mapdemo.spansToJson(mapdemo.spansWireLoad(wire));
    case 'mapdemo Text':
      return mapdemo.textToJson(mapdemo.textWireLoad(wire));
    case 'mapdemo Trails':
      return mapdemo.trailsToJson(mapdemo.trailsWireLoad(wire));
    case 'messagedemo ToolMessage':
      return messagedemo.toolMessageToJson(messagedemo.toolMessageWireLoad(wire));
    case 'scalars SimState':
      return scalardemo.simStateToJson(scalardemo.simStateWireLoad(wire));
    case 'widedemo Caption':
      return widedemo.captionToJson(widedemo.captionWireLoad(wire));
    case 'widedemo Stamp':
      return widedemo.stampToJson(widedemo.stampWireLoad(wire));
    case 'listdemo Army':
      return listdemo.armyToJson(listdemo.armyWireLoad(wire));
    case 'listdemo Mixed':
      return listdemo.mixedToJson(listdemo.mixedWireLoad(wire));
    case 'streamdemo Feed':
      return streamdemo.feedToJson(streamdemo.feedWireLoad(wire));
    case 'tabledemo ArchiveConfig':
      return tabledemo.archiveConfigToJson(tabledemo.archiveConfigWireLoad(wire));
    case 'tabledemo KeyedConfig':
      return tabledemo.keyedConfigToJson(tabledemo.keyedConfigWireLoad(wire));
    case 'tabledemo WideBlob':
      return tabledemo.wideBlobToJson(tabledemo.wideBlobWireLoad(wire));
    case 'tblg1 Guarded':
      return tblg1.guardedToJson(tblg1.guardedWireLoad(wire));
    case 'tblm1 Msg':
      return tblm1.msgToJson(tblm1.msgWireLoad(wire));
    case 'tblm2 Msg':
      return tblm2.msgToJson(tblm2.msgWireLoad(wire));
    case 'tblp1 Chain':
      return tblp1.chainToJson(tblp1.chainWireLoad(wire));
    case 'tblp3 Chain':
      return tblp3.chainToJson(tblp3.chainWireLoad(wire));
    case 'tblr1 Cfg':
      return tblr1.cfgToJson(tblr1.cfgWireLoad(wire));
    case 'tblr2 Cfg':
      return tblr2.cfgToJson(tblr2.cfgWireLoad(wire));
    case 'tblv1 Cfg':
      return tblv1.cfgToJson(tblv1.cfgWireLoad(wire));
    case 'tblv2 Cfg':
      return tblv2.cfgToJson(tblv2.cfgWireLoad(wire));
    case 'vocabdemo Wide00':
      return vocabdemo.wide00ToJson(vocabdemo.wide00WireLoad(wire));
    case 'vocabdemo Wide09':
      return vocabdemo.wide09ToJson(vocabdemo.wide09WireLoad(wire));
    case 'vocab9demo Wide00':
      return vocab9demo.wide00ToJson(vocab9demo.wide00WireLoad(wire));
    case 'vocab9demo Wide19':
      return vocab9demo.wide19ToJson(vocab9demo.wide19WireLoad(wire));
    case 'backenddemo LoginRequest':
      return backenddemo.loginRequestToJson(backenddemo.loginRequestWireLoad(wire));
    case 'backenddemo MatchResult':
      return backenddemo.matchResultToJson(backenddemo.matchResultWireLoad(wire));
    case 'backenddemo StorePurchase':
      return backenddemo.storePurchaseToJson(backenddemo.storePurchaseWireLoad(wire));
    case 'tabledemo LoadoutConfig':
      return tabledemo.loadoutConfigToJson(tabledemo.loadoutConfigWireLoad(wire));
    case 'tabledemo ProfileConfig':
      return tabledemo.profileConfigToJson(tabledemo.profileConfigWireLoad(wire));
    case 'tabledemo RootConfig':
      return tabledemo.rootConfigToJson(tabledemo.rootConfigWireLoad(wire));
  }
  return null;
}

String? loadReport(String unit, String root, Uint8List wire) {
  switch ('$unit $root') {
    case 'graphdemo ListNode':
      final r = graphdemo.TableWireReport();
      graphdemo.listNodeWireLoad(wire, r);
      return _reportLine(r.unknown, r.kindMismatch, r.widened, r.clamped, r.duplicate, r.malformed, r.refused);
    case 'listdemo Army':
      final r = listdemo.TableWireReport();
      listdemo.armyWireLoad(wire, r);
      return _reportLine(r.unknown, r.kindMismatch, r.widened, r.clamped, r.duplicate, r.malformed, r.refused);
    case 'listdemo Bounded':
      final r = listdemo.TableWireReport();
      listdemo.boundedWireLoad(wire, r);
      return _reportLine(r.unknown, r.kindMismatch, r.widened, r.clamped, r.duplicate, r.malformed, r.refused);
    case 'listdemo Ints':
      final r = listdemo.TableWireReport();
      listdemo.intsWireLoad(wire, r);
      return _reportLine(r.unknown, r.kindMismatch, r.widened, r.clamped, r.duplicate, r.malformed, r.refused);
    case 'listdemo Squad':
      final r = listdemo.TableWireReport();
      listdemo.squadWireLoad(wire, r);
      return _reportLine(r.unknown, r.kindMismatch, r.widened, r.clamped, r.duplicate, r.malformed, r.refused);
    case 'tblv1 Cfg':
      final r = tblv1.TableWireReport();
      tblv1.cfgWireLoad(wire, r);
      return _reportLine(r.unknown, r.kindMismatch, r.widened, r.clamped, r.duplicate, r.malformed, r.refused);
    case 'tblv2 Cfg':
      final r = tblv2.TableWireReport();
      tblv2.cfgWireLoad(wire, r);
      return _reportLine(r.unknown, r.kindMismatch, r.widened, r.clamped, r.duplicate, r.malformed, r.refused);
    case 'tblw1 Fleet':
      final r = tblw1.TableWireReport();
      tblw1.fleetWireLoad(wire, r);
      return _reportLine(r.unknown, r.kindMismatch, r.widened, r.clamped, r.duplicate, r.malformed, r.refused);
    case 'tblw2 Fleet':
      final r = tblw2.TableWireReport();
      tblw2.fleetWireLoad(wire, r);
      return _reportLine(r.unknown, r.kindMismatch, r.widened, r.clamped, r.duplicate, r.malformed, r.refused);
    case 'tblr1 Cfg':
      final r = tblr1.TableWireReport();
      tblr1.cfgWireLoad(wire, r);
      return _reportLine(r.unknown, r.kindMismatch, r.widened, r.clamped, r.duplicate, r.malformed, r.refused);
    case 'tblr2 Cfg':
      final r = tblr2.TableWireReport();
      tblr2.cfgWireLoad(wire, r);
      return _reportLine(r.unknown, r.kindMismatch, r.widened, r.clamped, r.duplicate, r.malformed, r.refused);
    case 'graphdemo Scene':
      final r = graphdemo.TableWireReport();
      graphdemo.sceneWireLoad(wire, r);
      return _reportLine(r.unknown, r.kindMismatch, r.widened, r.clamped, r.duplicate, r.malformed, r.refused);
    case 'blobdemo Catalog':
      final r = blobdemo.TableWireReport();
      blobdemo.catalogWireLoad(wire, r);
      return _reportLine(r.unknown, r.kindMismatch, r.widened, r.clamped, r.duplicate, r.malformed, r.refused);
    case 'listdemo Save':
      final r = listdemo.TableWireReport();
      listdemo.saveWireLoad(wire, r);
      return _reportLine(r.unknown, r.kindMismatch, r.widened, r.clamped, r.duplicate, r.malformed, r.refused);
    case 'listdemo Album':
      final r = listdemo.TableWireReport();
      listdemo.albumWireLoad(wire, r);
      return _reportLine(r.unknown, r.kindMismatch, r.widened, r.clamped, r.duplicate, r.malformed, r.refused);
    case 'listdemo Sheet':
      final r = listdemo.TableWireReport();
      listdemo.sheetWireLoad(wire, r);
      return _reportLine(r.unknown, r.kindMismatch, r.widened, r.clamped, r.duplicate, r.malformed, r.refused);
    case 'listdemo Unbounded':
      final r = listdemo.TableWireReport();
      listdemo.unboundedWireLoad(wire, r);
      return _reportLine(r.unknown, r.kindMismatch, r.widened, r.clamped, r.duplicate, r.malformed, r.refused);
    case 'mapdemo Cells':
      final r = mapdemo.TableWireReport();
      mapdemo.cellsWireLoad(wire, r);
      return _reportLine(r.unknown, r.kindMismatch, r.widened, r.clamped, r.duplicate, r.malformed, r.refused);
    case 'mapdemo Chunks':
      final r = mapdemo.TableWireReport();
      mapdemo.chunksWireLoad(wire, r);
      return _reportLine(r.unknown, r.kindMismatch, r.widened, r.clamped, r.duplicate, r.malformed, r.refused);
    case 'mapdemo Crews':
      final r = mapdemo.TableWireReport();
      mapdemo.crewsWireLoad(wire, r);
      return _reportLine(r.unknown, r.kindMismatch, r.widened, r.clamped, r.duplicate, r.malformed, r.refused);
    case 'mapdemo Depth':
      final r = mapdemo.TableWireReport();
      mapdemo.depthWireLoad(wire, r);
      return _reportLine(r.unknown, r.kindMismatch, r.widened, r.clamped, r.duplicate, r.malformed, r.refused);
    case 'mapdemo Docs':
      final r = mapdemo.TableWireReport();
      mapdemo.docsWireLoad(wire, r);
      return _reportLine(r.unknown, r.kindMismatch, r.widened, r.clamped, r.duplicate, r.malformed, r.refused);
    case 'mapdemo Fleet':
      final r = mapdemo.TableWireReport();
      mapdemo.fleetWireLoad(wire, r);
      return _reportLine(r.unknown, r.kindMismatch, r.widened, r.clamped, r.duplicate, r.malformed, r.refused);
    case 'mapdemo Pairs':
      final r = mapdemo.TableWireReport();
      mapdemo.pairsWireLoad(wire, r);
      return _reportLine(r.unknown, r.kindMismatch, r.widened, r.clamped, r.duplicate, r.malformed, r.refused);
    case 'mapdemo Runs':
      final r = mapdemo.TableWireReport();
      mapdemo.runsWireLoad(wire, r);
      return _reportLine(r.unknown, r.kindMismatch, r.widened, r.clamped, r.duplicate, r.malformed, r.refused);
    case 'mapdemo Slots':
      final r = mapdemo.TableWireReport();
      mapdemo.slotsWireLoad(wire, r);
      return _reportLine(r.unknown, r.kindMismatch, r.widened, r.clamped, r.duplicate, r.malformed, r.refused);
    case 'mapdemo Spans':
      final r = mapdemo.TableWireReport();
      mapdemo.spansWireLoad(wire, r);
      return _reportLine(r.unknown, r.kindMismatch, r.widened, r.clamped, r.duplicate, r.malformed, r.refused);
    case 'mapdemo Text':
      final r = mapdemo.TableWireReport();
      mapdemo.textWireLoad(wire, r);
      return _reportLine(r.unknown, r.kindMismatch, r.widened, r.clamped, r.duplicate, r.malformed, r.refused);
    case 'mapdemo Trails':
      final r = mapdemo.TableWireReport();
      mapdemo.trailsWireLoad(wire, r);
      return _reportLine(r.unknown, r.kindMismatch, r.widened, r.clamped, r.duplicate, r.malformed, r.refused);
    case 'messagedemo ToolMessage':
      final r = messagedemo.TableWireReport();
      messagedemo.toolMessageWireLoad(wire, r);
      return _reportLine(r.unknown, r.kindMismatch, r.widened, r.clamped, r.duplicate, r.malformed, r.refused);
    case 'scalars SimState':
      final r = scalardemo.TableWireReport();
      scalardemo.simStateWireLoad(wire, r);
      return _reportLine(r.unknown, r.kindMismatch, r.widened, r.clamped, r.duplicate, r.malformed, r.refused);
    case 'widedemo Caption':
      final r = widedemo.TableWireReport();
      widedemo.captionWireLoad(wire, r);
      return _reportLine(r.unknown, r.kindMismatch, r.widened, r.clamped, r.duplicate, r.malformed, r.refused);
    case 'widedemo Stamp':
      final r = widedemo.TableWireReport();
      widedemo.stampWireLoad(wire, r);
      return _reportLine(r.unknown, r.kindMismatch, r.widened, r.clamped, r.duplicate, r.malformed, r.refused);
    // THE P SET's presence evolution and the M SET's arm evolution
    // (docs/SPEC-TABLES.md §4, §5): a value, an optional and a pointer reading
    // one another, and a union whose arms moved, each read tolerantly
    case 'tblp1 Chain':
      final r = tblp1.TableWireReport();
      tblp1.chainWireLoad(wire, r);
      return _reportLine(r.unknown, r.kindMismatch, r.widened, r.clamped, r.duplicate, r.malformed, r.refused);
    case 'tblp3 Chain':
      final r = tblp3.TableWireReport();
      tblp3.chainWireLoad(wire, r);
      return _reportLine(r.unknown, r.kindMismatch, r.widened, r.clamped, r.duplicate, r.malformed, r.refused);
    // the stream unit's parts: an element kind, a count past its bound and
    // an index out of range, each read tolerantly (§4)
    case 'streamdemo Feed':
      final r = streamdemo.TableWireReport();
      streamdemo.feedWireLoad(wire, r);
      return _reportLine(r.unknown, r.kindMismatch, r.widened, r.clamped, r.duplicate, r.malformed, r.refused);
    case 'tblp2 Chain':
      final r = tblp2.TableWireReport();
      tblp2.chainWireLoad(wire, r);
      return _reportLine(r.unknown, r.kindMismatch, r.widened, r.clamped, r.duplicate, r.malformed, r.refused);
    // the 64-bit id-table pair and the arm retypes (§3, §5): K1 and K2 read
    // each other's escapes, A2 reads A1's arms moved, retyped and unknown
    case 'tblk1 Root':
      final r = tblk1.TableWireReport();
      tblk1.rootWireLoad(wire, r);
      return _reportLine(r.unknown, r.kindMismatch, r.widened, r.clamped, r.duplicate, r.malformed, r.refused);
    case 'tblk2 Root':
      final r = tblk2.TableWireReport();
      tblk2.rootWireLoad(wire, r);
      return _reportLine(r.unknown, r.kindMismatch, r.widened, r.clamped, r.duplicate, r.malformed, r.refused);
    case 'tbla2 Root':
      final r = tbla2.TableWireReport();
      tbla2.rootWireLoad(wire, r);
      return _reportLine(r.unknown, r.kindMismatch, r.widened, r.clamped, r.duplicate, r.malformed, r.refused);
    case 'tblscalars2 SimState':
      final r = scalardemo2.TableWireReport();
      scalardemo2.simStateWireLoad(wire, r);
      return _reportLine(r.unknown, r.kindMismatch, r.widened, r.clamped, r.duplicate, r.malformed, r.refused);
    case 'tblm1 Msg':
      final r = tblm1.TableWireReport();
      tblm1.msgWireLoad(wire, r);
      return _reportLine(r.unknown, r.kindMismatch, r.widened, r.clamped, r.duplicate, r.malformed, r.refused);
    case 'tblm2 Msg':
      final r = tblm2.TableWireReport();
      tblm2.msgWireLoad(wire, r);
      return _reportLine(r.unknown, r.kindMismatch, r.widened, r.clamped, r.duplicate, r.malformed, r.refused);
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
) {
  switch ('$unit $root') {
    case 'graphdemo Scene':
      final r = graphdemo.TableWireReport();
      final value = graphdemo.sceneWireLoad(wire, r);
      if (r.malformed || r.refused) return (-1, (_, _) => false);
      return (
        ck.SceneCook.cookMeasure(value),
        (out, order) => ck.SceneCook.cook(value, out, order),
      );
    case 'blobdemo Catalog':
      final r = blobdemo.TableWireReport();
      final value = blobdemo.catalogWireLoad(wire, r);
      if (r.malformed || r.refused) return (-1, (_, _) => false);
      return (
        assets.CatalogCook.cookMeasure(value),
        (out, order) => assets.CatalogCook.cook(value, out, order),
      );
    case 'tabledemo ArchiveConfig':
      final r = tabledemo.TableWireReport();
      final value = tabledemo.archiveConfigWireLoad(wire, r);
      if (r.malformed || r.refused) return (-1, (_, _) => false);
      return (
        nestedcook.ArchiveConfigCookWire.cookMeasure(value),
        (out, order) => nestedcook.ArchiveConfigCookWire.cook(value, out, order),
      );
    case 'tabledemo KeyedConfig':
      final r = tabledemo.TableWireReport();
      final value = tabledemo.keyedConfigWireLoad(wire, r);
      if (r.malformed || r.refused) return (-1, (_, _) => false);
      return (
        keyedcook.KeyedConfigCookWire.cookMeasure(value),
        (out, order) => keyedcook.KeyedConfigCookWire.cook(value, out, order),
      );
    case 'tabledemo LoadoutConfig':
      final r = tabledemo.TableWireReport();
      final value = tabledemo.loadoutConfigWireLoad(wire, r);
      if (r.malformed || r.refused) return (-1, (_, _) => false);
      return (
        tablescook.LoadoutConfigCookWire.cookMeasure(value),
        (out, order) => tablescook.LoadoutConfigCookWire.cook(value, out, order),
      );
    case 'tabledemo ProfileConfig':
      final r = tabledemo.TableWireReport();
      final value = tabledemo.profileConfigWireLoad(wire, r);
      if (r.malformed || r.refused) return (-1, (_, _) => false);
      return (
        tablescook.ProfileConfigCookWire.cookMeasure(value),
        (out, order) => tablescook.ProfileConfigCookWire.cook(value, out, order),
      );
    case 'tabledemo RootConfig':
      final r = tabledemo.TableWireReport();
      final value = tabledemo.rootConfigWireLoad(wire, r);
      if (r.malformed || r.refused) return (-1, (_, _) => false);
      return (
        tablescook.RootConfigCookWire.cookMeasure(value),
        (out, order) => tablescook.RootConfigCookWire.cook(value, out, order),
      );
    case 'tabledemo WideBlob':
      final r = tabledemo.TableWireReport();
      final value = tabledemo.wideBlobWireLoad(wire, r);
      if (r.malformed || r.refused) return (-1, (_, _) => false);
      return (
        widecook.WideBlobCookWire.cookMeasure(value),
        (out, order) => widecook.WideBlobCookWire.cook(value, out, order),
      );
    case 'tblv1 Cfg':
      final r = tblv1.TableWireReport();
      final value = tblv1.cfgWireLoad(wire, r);
      if (r.malformed || r.refused) return (-1, (_, _) => false);
      return (
        v1cook.CfgCookWire.cookMeasure(value),
        (out, order) => v1cook.CfgCookWire.cook(value, out, order),
      );
    case 'tblv2 Cfg':
      final r = tblv2.TableWireReport();
      final value = tblv2.cfgWireLoad(wire, r);
      if (r.malformed || r.refused) return (-1, (_, _) => false);
      return (
        v2cook.CfgCookWire.cookMeasure(value),
        (out, order) => v2cook.CfgCookWire.cook(value, out, order),
      );
    case 'tblp1 Chain':
      final r = tblp1.TableWireReport();
      final value = tblp1.chainWireLoad(wire, r);
      if (r.malformed || r.refused) return (-1, (_, _) => false);
      return (
        p1cook.ChainCookWire.cookMeasure(value),
        (out, order) => p1cook.ChainCookWire.cook(value, out, order),
      );
    case 'tblp3 Chain':
      final r = tblp3.TableWireReport();
      final value = tblp3.chainWireLoad(wire, r);
      if (r.malformed || r.refused) return (-1, (_, _) => false);
      return (
        p3cook.ChainCookWire.cookMeasure(value),
        (out, order) => p3cook.ChainCookWire.cook(value, out, order),
      );
    case 'tblm1 Msg':
      final r = tblm1.TableWireReport();
      final value = tblm1.msgWireLoad(wire, r);
      if (r.malformed || r.refused) return (-1, (_, _) => false);
      return (
        m1cook.MsgCookWire.cookMeasure(value),
        (out, order) => m1cook.MsgCookWire.cook(value, out, order),
      );
    case 'tblm2 Msg':
      final r = tblm2.TableWireReport();
      final value = tblm2.msgWireLoad(wire, r);
      if (r.malformed || r.refused) return (-1, (_, _) => false);
      return (
        m2cook.MsgCookWire.cookMeasure(value),
        (out, order) => m2cook.MsgCookWire.cook(value, out, order),
      );
    case 'messagedemo ToolMessage':
      final r = messagedemo.TableWireReport();
      final value = messagedemo.toolMessageWireLoad(wire, r);
      if (r.malformed || r.refused) return (-1, (_, _) => false);
      return (
        messagescook.ToolMessageCookWire.cookMeasure(value),
        (out, order) =>
            messagescook.ToolMessageCookWire.cook(value, out, order),
      );
    case 'scalars SimState':
      final r = scalardemo.TableWireReport();
      final value = scalardemo.simStateWireLoad(wire, r);
      if (r.malformed || r.refused) return (-1, (_, _) => false);
      return (
        scalarscook.SimStateCookWire.cookMeasure(value),
        (out, order) => scalarscook.SimStateCookWire.cook(value, out, order),
      );
    case 'streamdemo Feed':
      final r = streamdemo.TableWireReport();
      final value = streamdemo.feedWireLoad(wire, r);
      if (r.malformed || r.refused) return (-1, (_, _) => false);
      return (
        streamcook.FeedCook.cookMeasure(value),
        (out, order) => streamcook.FeedCook.cook(value, out, order),
      );
    case 'listdemo Save':
      final r = listdemo.TableWireReport();
      final value = listdemo.saveWireLoad(wire, r);
      if (r.malformed || r.refused) return (-1, (_, _) => false);
      return (
        lsavecook.SaveCook.cookMeasure(value),
        (out, order) => lsavecook.SaveCook.cook(value, out, order),
      );
    case 'listdemo Mixed':
      final r = listdemo.TableWireReport();
      final value = listdemo.mixedWireLoad(wire, r);
      if (r.malformed || r.refused) return (-1, (_, _) => false);
      return (
        lsavecook.MixedCook.cookMeasure(value),
        (out, order) => lsavecook.MixedCook.cook(value, out, order),
      );
    case 'listdemo Album':
      final r = listdemo.TableWireReport();
      final value = listdemo.albumWireLoad(wire, r);
      if (r.malformed || r.refused) return (-1, (_, _) => false);
      return (
        lsharedcook.AlbumCook.cookMeasure(value),
        (out, order) => lsharedcook.AlbumCook.cook(value, out, order),
      );
    case 'listdemo Sheet':
      final r = listdemo.TableWireReport();
      final value = listdemo.sheetWireLoad(wire, r);
      if (r.malformed || r.refused) return (-1, (_, _) => false);
      return (
        lholderscook.SheetCook.cookMeasure(value),
        (out, order) => lholderscook.SheetCook.cook(value, out, order),
      );
    case 'listdemo Army':
      final r = listdemo.TableWireReport();
      final value = listdemo.armyWireLoad(wire, r);
      if (r.malformed || r.refused) return (-1, (_, _) => false);
      return (
        lholderscook.ArmyCook.cookMeasure(value),
        (out, order) => lholderscook.ArmyCook.cook(value, out, order),
      );
    case 'listdemo Unbounded':
      final r = listdemo.TableWireReport();
      final value = listdemo.unboundedWireLoad(wire, r);
      if (r.malformed || r.refused) return (-1, (_, _) => false);
      return (
        lmigratecook.UnboundedCook.cookMeasure(value),
        (out, order) => lmigratecook.UnboundedCook.cook(value, out, order),
      );
    case 'mapdemo Cells':
      final r = mapdemo.TableWireReport();
      final value = mapdemo.cellsWireLoad(wire, r);
      if (r.malformed || r.refused) return (-1, (_, _) => false);
      return (
        mcellscook.CellsCook.cookMeasure(value),
        (out, order) => mcellscook.CellsCook.cook(value, out, order),
      );
    case 'mapdemo Chunks':
      final r = mapdemo.TableWireReport();
      final value = mapdemo.chunksWireLoad(wire, r);
      if (r.malformed || r.refused) return (-1, (_, _) => false);
      return (
        mchunkscook.ChunksCook.cookMeasure(value),
        (out, order) => mchunkscook.ChunksCook.cook(value, out, order),
      );
    case 'mapdemo Crews':
      final r = mapdemo.TableWireReport();
      final value = mapdemo.crewsWireLoad(wire, r);
      if (r.malformed || r.refused) return (-1, (_, _) => false);
      return (
        mcrewscook.CrewsCook.cookMeasure(value),
        (out, order) => mcrewscook.CrewsCook.cook(value, out, order),
      );
    case 'mapdemo Depth':
      final r = mapdemo.TableWireReport();
      final value = mapdemo.depthWireLoad(wire, r);
      if (r.malformed || r.refused) return (-1, (_, _) => false);
      return (
        mdepthcook.DepthCook.cookMeasure(value),
        (out, order) => mdepthcook.DepthCook.cook(value, out, order),
      );
    case 'mapdemo Docs':
      final r = mapdemo.TableWireReport();
      final value = mapdemo.docsWireLoad(wire, r);
      if (r.malformed || r.refused) return (-1, (_, _) => false);
      return (
        mdocscook.DocsCook.cookMeasure(value),
        (out, order) => mdocscook.DocsCook.cook(value, out, order),
      );
    case 'mapdemo Fleet':
      final r = mapdemo.TableWireReport();
      final value = mapdemo.fleetWireLoad(wire, r);
      if (r.malformed || r.refused) return (-1, (_, _) => false);
      return (
        mfleetcook.FleetCook.cookMeasure(value),
        (out, order) => mfleetcook.FleetCook.cook(value, out, order),
      );
    case 'mapdemo Pairs':
      final r = mapdemo.TableWireReport();
      final value = mapdemo.pairsWireLoad(wire, r);
      if (r.malformed || r.refused) return (-1, (_, _) => false);
      return (
        mpairscook.PairsCook.cookMeasure(value),
        (out, order) => mpairscook.PairsCook.cook(value, out, order),
      );
    case 'mapdemo Runs':
      final r = mapdemo.TableWireReport();
      final value = mapdemo.runsWireLoad(wire, r);
      if (r.malformed || r.refused) return (-1, (_, _) => false);
      return (
        mrunscook.RunsCook.cookMeasure(value),
        (out, order) => mrunscook.RunsCook.cook(value, out, order),
      );
    case 'mapdemo Slots':
      final r = mapdemo.TableWireReport();
      final value = mapdemo.slotsWireLoad(wire, r);
      if (r.malformed || r.refused) return (-1, (_, _) => false);
      return (
        mslotscook.SlotsCook.cookMeasure(value),
        (out, order) => mslotscook.SlotsCook.cook(value, out, order),
      );
    case 'mapdemo Spans':
      final r = mapdemo.TableWireReport();
      final value = mapdemo.spansWireLoad(wire, r);
      if (r.malformed || r.refused) return (-1, (_, _) => false);
      return (
        mspanscook.SpansCook.cookMeasure(value),
        (out, order) => mspanscook.SpansCook.cook(value, out, order),
      );
    case 'mapdemo Text':
      final r = mapdemo.TableWireReport();
      final value = mapdemo.textWireLoad(wire, r);
      if (r.malformed || r.refused) return (-1, (_, _) => false);
      return (
        mtextcook.TextCook.cookMeasure(value),
        (out, order) => mtextcook.TextCook.cook(value, out, order),
      );
    case 'mapdemo Trails':
      final r = mapdemo.TableWireReport();
      final value = mapdemo.trailsWireLoad(wire, r);
      if (r.malformed || r.refused) return (-1, (_, _) => false);
      return (
        mtrailscook.TrailsCook.cookMeasure(value),
        (out, order) => mtrailscook.TrailsCook.cook(value, out, order),
      );
    case 'widelist Wide':
      final r = widelist.TableWireReport();
      final value = widelist.wideWireLoad(wire, r);
      if (r.malformed || r.refused) return (-1, (_, _) => false);
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
    final writer = cookWriter(
      instance[2],
      instance[3],
      File(instance[4]).readAsBytesSync(),
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
    final saved = jsonReadSave(f[2], f[3], File(textPath).readAsStringSync());
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
  return 0;
}

// §16.3 and §17.5: a hostile tree reports the actual tolerant text-read
// counters, or refuses a malformed text. A missing codec is explicit.
String? jsonHostileReport(String unit, String root, String text) {
  dynamic r;
  try {
    switch ('$unit $root') {
      case 'tabledemo RootConfig':
        r = tabledemo.TableTextReport();
        tabledemo.rootConfigFromJson(text, r);
        break;
      case 'tabledemo PackConfig':
        r = tabledemo.TableTextReport();
        tabledemo.packConfigFromJson(text, r);
        break;
      case 'blobdemo Catalog':
        r = blobdemo.TableTextReport();
        blobdemo.catalogFromJson(text, r);
        break;
      case 'graphdemo Scene':
        r = graphdemo.TableTextReport();
        graphdemo.sceneFromJson(text, r);
        break;
      case 'mapdemo Text':
        r = mapdemo.TableTextReport();
        mapdemo.textFromJson(text, r);
        break;
      case 'listdemo Army':
        r = listdemo.TableTextReport();
        listdemo.armyFromJson(text, r);
        break;
      case 'messagedemo ToolMessage':
        r = messagedemo.TableTextReport();
        messagedemo.toolMessageFromJson(text, r);
        break;
      case 'scalars SimState':
        r = scalardemo.TableTextReport();
        scalardemo.simStateFromJson(text, r);
        break;
      case 'streamdemo Feed':
        r = streamdemo.TableTextReport();
        streamdemo.feedFromJson(text, r);
        break;
      default:
        return null;
    }
  } catch (_) {
    return 'refused\n';
  }
  return '${r.unknown},${r.kindMismatch},0,${r.clamped},${r.duplicate},false,read\n';
}

int surfaceJsonHostile(String outDir) {
  for (final f in kind('json-hostile')) {
    final text = utf8.decode(File('${f[4]}/${f[3]}.json').readAsBytesSync(), allowMalformed: true);
    final verdict = jsonHostileReport(f[2], f[3], text);
    if (verdict == null) {
      spillAbsent(outDir, f[1]);
      continue;
    }
    File('$outDir/${f[1]}').writeAsStringSync(verdict);
  }
  return 0;
}

// ---- retain and retain-save (docs/SPEC-TABLES.md §6.6): RT1's Node loaded
// with retention at the row's two capacities, measured and saved back. `retain`
// prints `<retained>,<retain_lost>,<unknown> <save lost>`, `retain-save` the
// saved bytes. `short` is ONE BYTE SHORT OF THE LAST RECORD: a roomy load says
// what this port's own records cost, and the second load gets one byte less
// (test/conformance/README.md). The id list's capacity is a count.
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
      if (report.malformed || report.refused) {
        stderr.writeln('driver: ${f[1]}: capacity probe refused');
        return 1;
      }
      store = tblrt1.TableRetain(store.used - 1 < 0 ? 0 : store.used - 1, ids);
      report = tblrt1.TableWireReport();
    }
    final root = tblrt1.nodeWireLoadRetain(wire, store, report);
    if (report.malformed || report.refused) {
      stderr.writeln('driver: ${f[1]}: retaining load failed');
      return 1;
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
      File('$outDir/${f[1]}').writeAsStringSync(
        '${report.retained},${report.retainLost},${report.unknown} '
        '${save.retainLost}\n',
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
    final line = loadReport(f[2], f[3], wire);
    if (line == null) {
      spillAbsent(outDir, f[1]);
      continue;
    }
    final noValue = f.length > 5 && f[5] == 'no-value';
    final fields = line.trimRight().split(',');
    final damaged = fields.length == 7 && fields[5] == 'true';
    if (fields.last == 'refused' || damaged || noValue) {
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

int surfaceReport(String outDir) {
  for (final f in kind('report')) {
    final line = loadReport(f[2], f[3], File(f[4]).readAsBytesSync());
    if (line == null) {
      spillAbsent(outDir, f[1]);
      continue;
    }
    File('$outDir/${f[1]}').writeAsStringSync(line);
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
    // `count, ok := load(...)` and sets `rep.malformed = rep.malformed || !ok
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
      clamped: rep.clamped as int,
      malformed: (rep.malformed as bool) || !ok && !(rep.refused as bool),
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
    damaged: (r) => r.refused || r.malformed,
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
    damaged: (r) => r.refused || r.malformed,
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
    damaged: (r) => r.refused || r.malformed,
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
    damaged: (r) => r.refused || r.malformed,
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
    damaged: (r) => r.refused || r.malformed,
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
    damaged: (r) => r.refused || r.malformed,
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
    damaged: (r) => r.refused || r.malformed,
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
    damaged: (r) => r.refused || r.malformed,
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
    return _fuzzReplyFor(report as dynamic, m >= 0, m, _refusable(() => save(value)));
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
    return _fuzzReplyFor(report as dynamic, m >= 0, m, _refusable(() => save(value, retain, report)));
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
    kindMismatch: r.kindMismatch as int,
    widened: r.widened as int,
    clamped: r.clamped as int,
    duplicate: r.duplicate as int,
    malformed: r.malformed as bool,
    refused: r.refused as bool,
    measure: measure,
    retained: r.retained as int,
    retainLost: r.retainLost as int,
    saved: saved,
  );
}

_FuzzRoot? fuzzRoot(String unit, String root) {
  switch ('$unit $root') {
    case 'cards Card':
      return _FuzzRoot(
          _plainCodec(TableWireReport.new, cardWireMeasure, cardWireLoad, cardWireSave),
          retain: _retainCodec(TableRetain.new, TableWireReport.new, cardWireMeasure, cardWireLoadRetain, cardWireSaveRetain));
    case 'backenddemo LoginRequest':
      return _FuzzRoot(
          _plainCodec(backenddemo.TableWireReport.new, backenddemo.loginRequestWireMeasure, backenddemo.loginRequestWireLoad, backenddemo.loginRequestWireSave),
          retain: _retainCodec(backenddemo.TableRetain.new, backenddemo.TableWireReport.new, backenddemo.loginRequestWireMeasure, backenddemo.loginRequestWireLoadRetain, backenddemo.loginRequestWireSaveRetain));
    case 'backenddemo MatchResult':
      return _FuzzRoot(
          _plainCodec(backenddemo.TableWireReport.new, backenddemo.matchResultWireMeasure, backenddemo.matchResultWireLoad, backenddemo.matchResultWireSave),
          retain: _retainCodec(backenddemo.TableRetain.new, backenddemo.TableWireReport.new, backenddemo.matchResultWireMeasure, backenddemo.matchResultWireLoadRetain, backenddemo.matchResultWireSaveRetain));
    case 'backenddemo StorePurchase':
      return _FuzzRoot(
          _plainCodec(backenddemo.TableWireReport.new, backenddemo.storePurchaseWireMeasure, backenddemo.storePurchaseWireLoad, backenddemo.storePurchaseWireSave),
          retain: _retainCodec(backenddemo.TableRetain.new, backenddemo.TableWireReport.new, backenddemo.storePurchaseWireMeasure, backenddemo.storePurchaseWireLoadRetain, backenddemo.storePurchaseWireSaveRetain));
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
          _plainCodec(messagedemo.TableWireReport.new, messagedemo.toolMessageWireMeasure, messagedemo.toolMessageWireLoad, messagedemo.toolMessageWireSave),
          retain: _retainCodec(messagedemo.TableRetain.new, messagedemo.TableWireReport.new, messagedemo.toolMessageWireMeasure, messagedemo.toolMessageWireLoadRetain, messagedemo.toolMessageWireSaveRetain));
    case 'scalars SimState':
      return _FuzzRoot(
          _plainCodec(scalardemo.TableWireReport.new, scalardemo.simStateWireMeasure, scalardemo.simStateWireLoad, scalardemo.simStateWireSave),
          retain: _retainCodec(scalardemo.TableRetain.new, scalardemo.TableWireReport.new, scalardemo.simStateWireMeasure, scalardemo.simStateWireLoadRetain, scalardemo.simStateWireSaveRetain));
    case 'streamdemo Feed':
      return _FuzzRoot(
          _plainCodec(streamdemo.TableWireReport.new, streamdemo.feedWireMeasure, streamdemo.feedWireLoad, streamdemo.feedWireSave),
          retain: _retainCodec(streamdemo.TableRetain.new, streamdemo.TableWireReport.new, streamdemo.feedWireMeasure, streamdemo.feedWireLoadRetain, streamdemo.feedWireSaveRetain));
    case 'tabledemo ArchiveConfig':
      return _FuzzRoot(
          _plainCodec(tabledemo.TableWireReport.new, tabledemo.archiveConfigWireMeasure, tabledemo.archiveConfigWireLoad, tabledemo.archiveConfigWireSave),
          retain: _retainCodec(tabledemo.TableRetain.new, tabledemo.TableWireReport.new, tabledemo.archiveConfigWireMeasure, tabledemo.archiveConfigWireLoadRetain, tabledemo.archiveConfigWireSaveRetain));
    case 'tabledemo KeyedConfig':
      return _FuzzRoot(
          _plainCodec(tabledemo.TableWireReport.new, tabledemo.keyedConfigWireMeasure, tabledemo.keyedConfigWireLoad, tabledemo.keyedConfigWireSave),
          retain: _retainCodec(tabledemo.TableRetain.new, tabledemo.TableWireReport.new, tabledemo.keyedConfigWireMeasure, tabledemo.keyedConfigWireLoadRetain, tabledemo.keyedConfigWireSaveRetain));
    case 'tabledemo LoadoutConfig':
      return _FuzzRoot(
          _plainCodec(tabledemo.TableWireReport.new, tabledemo.loadoutConfigWireMeasure, tabledemo.loadoutConfigWireLoad, tabledemo.loadoutConfigWireSave),
          retain: _retainCodec(tabledemo.TableRetain.new, tabledemo.TableWireReport.new, tabledemo.loadoutConfigWireMeasure, tabledemo.loadoutConfigWireLoadRetain, tabledemo.loadoutConfigWireSaveRetain));
    case 'tabledemo ProfileConfig':
      return _FuzzRoot(
          _plainCodec(tabledemo.TableWireReport.new, tabledemo.profileConfigWireMeasure, tabledemo.profileConfigWireLoad, tabledemo.profileConfigWireSave),
          retain: _retainCodec(tabledemo.TableRetain.new, tabledemo.TableWireReport.new, tabledemo.profileConfigWireMeasure, tabledemo.profileConfigWireLoadRetain, tabledemo.profileConfigWireSaveRetain));
    case 'tabledemo RootConfig':
      return _FuzzRoot(
          _plainCodec(tabledemo.TableWireReport.new, tabledemo.rootConfigWireMeasure, tabledemo.rootConfigWireLoad, tabledemo.rootConfigWireSave),
          retain: _retainCodec(tabledemo.TableRetain.new, tabledemo.TableWireReport.new, tabledemo.rootConfigWireMeasure, tabledemo.rootConfigWireLoadRetain, tabledemo.rootConfigWireSaveRetain));
    case 'tabledemo WideBlob':
      return _FuzzRoot(
          _plainCodec(tabledemo.TableWireReport.new, tabledemo.wideBlobWireMeasure, tabledemo.wideBlobWireLoad, tabledemo.wideBlobWireSave),
          retain: _retainCodec(tabledemo.TableRetain.new, tabledemo.TableWireReport.new, tabledemo.wideBlobWireMeasure, tabledemo.wideBlobWireLoadRetain, tabledemo.wideBlobWireSaveRetain));
    case 'tbla2 Root':
      return _FuzzRoot(
          _plainCodec(tbla2.TableWireReport.new, tbla2.rootWireMeasure, tbla2.rootWireLoad, tbla2.rootWireSave),
          retain: _retainCodec(tbla2.TableRetain.new, tbla2.TableWireReport.new, tbla2.rootWireMeasure, tbla2.rootWireLoadRetain, tbla2.rootWireSaveRetain));
    case 'tblg1 Guarded':
      return _FuzzRoot(
          _plainCodec(tblg1.TableWireReport.new, tblg1.guardedWireMeasure, tblg1.guardedWireLoad, tblg1.guardedWireSave),
          retain: _retainCodec(tblg1.TableRetain.new, tblg1.TableWireReport.new, tblg1.guardedWireMeasure, tblg1.guardedWireLoadRetain, tblg1.guardedWireSaveRetain));
    case 'tblk1 Root':
      return _FuzzRoot(
          _plainCodec(tblk1.TableWireReport.new, tblk1.rootWireMeasure, tblk1.rootWireLoad, tblk1.rootWireSave),
          retain: _retainCodec(tblk1.TableRetain.new, tblk1.TableWireReport.new, tblk1.rootWireMeasure, tblk1.rootWireLoadRetain, tblk1.rootWireSaveRetain));
    case 'tblk2 Root':
      return _FuzzRoot(
          _plainCodec(tblk2.TableWireReport.new, tblk2.rootWireMeasure, tblk2.rootWireLoad, tblk2.rootWireSave),
          retain: _retainCodec(tblk2.TableRetain.new, tblk2.TableWireReport.new, tblk2.rootWireMeasure, tblk2.rootWireLoadRetain, tblk2.rootWireSaveRetain));
    case 'tblm1 Msg':
      return _FuzzRoot(
          _plainCodec(tblm1.TableWireReport.new, tblm1.msgWireMeasure, tblm1.msgWireLoad, tblm1.msgWireSave),
          retain: _retainCodec(tblm1.TableRetain.new, tblm1.TableWireReport.new, tblm1.msgWireMeasure, tblm1.msgWireLoadRetain, tblm1.msgWireSaveRetain));
    case 'tblm2 Msg':
      return _FuzzRoot(
          _plainCodec(tblm2.TableWireReport.new, tblm2.msgWireMeasure, tblm2.msgWireLoad, tblm2.msgWireSave),
          retain: _retainCodec(tblm2.TableRetain.new, tblm2.TableWireReport.new, tblm2.msgWireMeasure, tblm2.msgWireLoadRetain, tblm2.msgWireSaveRetain));
    case 'tblp1 Chain':
      return _FuzzRoot(
          _plainCodec(tblp1.TableWireReport.new, tblp1.chainWireMeasure, tblp1.chainWireLoad, tblp1.chainWireSave),
          retain: _retainCodec(tblp1.TableRetain.new, tblp1.TableWireReport.new, tblp1.chainWireMeasure, tblp1.chainWireLoadRetain, tblp1.chainWireSaveRetain));
    case 'tblp2 Chain':
      return _FuzzRoot(
          _plainCodec(tblp2.TableWireReport.new, tblp2.chainWireMeasure, tblp2.chainWireLoad, tblp2.chainWireSave),
          retain: _retainCodec(tblp2.TableRetain.new, tblp2.TableWireReport.new, tblp2.chainWireMeasure, tblp2.chainWireLoadRetain, tblp2.chainWireSaveRetain));
    case 'tblp3 Chain':
      return _FuzzRoot(
          _plainCodec(tblp3.TableWireReport.new, tblp3.chainWireMeasure, tblp3.chainWireLoad, tblp3.chainWireSave),
          retain: _retainCodec(tblp3.TableRetain.new, tblp3.TableWireReport.new, tblp3.chainWireMeasure, tblp3.chainWireLoadRetain, tblp3.chainWireSaveRetain));
    case 'tblr1 Cfg':
      return _FuzzRoot(
          _plainCodec(tblr1.TableWireReport.new, tblr1.cfgWireMeasure, tblr1.cfgWireLoad, tblr1.cfgWireSave),
          retain: _retainCodec(tblr1.TableRetain.new, tblr1.TableWireReport.new, tblr1.cfgWireMeasure, tblr1.cfgWireLoadRetain, tblr1.cfgWireSaveRetain));
    case 'tblr2 Cfg':
      return _FuzzRoot(
          _plainCodec(tblr2.TableWireReport.new, tblr2.cfgWireMeasure, tblr2.cfgWireLoad, tblr2.cfgWireSave),
          retain: _retainCodec(tblr2.TableRetain.new, tblr2.TableWireReport.new, tblr2.cfgWireMeasure, tblr2.cfgWireLoadRetain, tblr2.cfgWireSaveRetain));
    case 'tblscalars2 SimState':
      return _FuzzRoot(
          _plainCodec(scalardemo2.TableWireReport.new, scalardemo2.simStateWireMeasure, scalardemo2.simStateWireLoad, scalardemo2.simStateWireSave),
          retain: _retainCodec(scalardemo2.TableRetain.new, scalardemo2.TableWireReport.new, scalardemo2.simStateWireMeasure, scalardemo2.simStateWireLoadRetain, scalardemo2.simStateWireSaveRetain));
    case 'tblv1 Cfg':
      return _FuzzRoot(
          _plainCodec(tblv1.TableWireReport.new, tblv1.cfgWireMeasure, tblv1.cfgWireLoad, tblv1.cfgWireSave),
          retain: _retainCodec(tblv1.TableRetain.new, tblv1.TableWireReport.new, tblv1.cfgWireMeasure, tblv1.cfgWireLoadRetain, tblv1.cfgWireSaveRetain));
    case 'tblv2 Cfg':
      return _FuzzRoot(
          _plainCodec(tblv2.TableWireReport.new, tblv2.cfgWireMeasure, tblv2.cfgWireLoad, tblv2.cfgWireSave),
          retain: _retainCodec(tblv2.TableRetain.new, tblv2.TableWireReport.new, tblv2.cfgWireMeasure, tblv2.cfgWireLoadRetain, tblv2.cfgWireSaveRetain));
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
          _plainCodec(vocab9demo.TableWireReport.new, vocab9demo.wide00WireMeasure, vocab9demo.wide00WireLoad, vocab9demo.wide00WireSave),
          retain: _retainCodec(vocab9demo.TableRetain.new, vocab9demo.TableWireReport.new, vocab9demo.wide00WireMeasure, vocab9demo.wide00WireLoadRetain, vocab9demo.wide00WireSaveRetain));
    case 'vocab9demo Wide19':
      return _FuzzRoot(
          _plainCodec(vocab9demo.TableWireReport.new, vocab9demo.wide19WireMeasure, vocab9demo.wide19WireLoad, vocab9demo.wide19WireSave),
          retain: _retainCodec(vocab9demo.TableRetain.new, vocab9demo.TableWireReport.new, vocab9demo.wide19WireMeasure, vocab9demo.wide19WireLoadRetain, vocab9demo.wide19WireSaveRetain));
    case 'vocabdemo Wide00':
      return _FuzzRoot(
          _plainCodec(vocabdemo.TableWireReport.new, vocabdemo.wide00WireMeasure, vocabdemo.wide00WireLoad, vocabdemo.wide00WireSave),
          retain: _retainCodec(vocabdemo.TableRetain.new, vocabdemo.TableWireReport.new, vocabdemo.wide00WireMeasure, vocabdemo.wide00WireLoadRetain, vocabdemo.wide00WireSaveRetain));
    case 'vocabdemo Wide09':
      return _FuzzRoot(
          _plainCodec(vocabdemo.TableWireReport.new, vocabdemo.wide09WireMeasure, vocabdemo.wide09WireLoad, vocabdemo.wide09WireSave),
          retain: _retainCodec(vocabdemo.TableRetain.new, vocabdemo.TableWireReport.new, vocabdemo.wide09WireMeasure, vocabdemo.wide09WireLoadRetain, vocabdemo.wide09WireSaveRetain));
    case 'widedemo Caption':
      return _FuzzRoot(
          _plainCodec(widedemo.TableWireReport.new, widedemo.captionWireMeasure, widedemo.captionWireLoad, widedemo.captionWireSave),
          retain: _retainCodec(widedemo.TableRetain.new, widedemo.TableWireReport.new, widedemo.captionWireMeasure, widedemo.captionWireLoadRetain, widedemo.captionWireSaveRetain));
    case 'widedemo Stamp':
      return _FuzzRoot(
          _plainCodec(widedemo.TableWireReport.new, widedemo.stampWireMeasure, widedemo.stampWireLoad, widedemo.stampWireSave),
          retain: _retainCodec(widedemo.TableRetain.new, widedemo.TableWireReport.new, widedemo.stampWireMeasure, widedemo.stampWireLoadRetain, widedemo.stampWireSaveRetain));
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
