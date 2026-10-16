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
import '../../../build/tables-generated-dart-scalars/scalars/ScalardemoWire.dart' as scalardemo;
import '../../../build/tables-generated-dart/wide/WideWire.dart' as widedemo;
import '../../../build/tables-generated-dart/backend/BackenddemoFixed.dart' as backenddemo;
import '../../../build/tables-generated-dart/backend/BackendFixed.dart' as backend;
import '../../../build/tables-generated-dart/vocab/VocabdemoFixed.dart' as vocabdemo;
import '../../../build/tables-generated-dart/vocab/VocabFixed.dart' as vocab;
import '../../../build/tables-generated-dart/vocab9/Vocab9demoFixed.dart' as vocab9demo;
import '../../../build/tables-generated-dart/vocab9/Vocab9Fixed.dart' as vocab9;


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

String openBlock(String name, Uint8List image, int extent, int pointer) {
  if (pointer < 0) {
    return 'refuse\n'; // no buffer at all
  }
  final claim = extent < 0 ? image.length : extent;
  final buffer = blockBuffer(image, extent, pointer);
  final Object? block = name.startsWith('block_render')
      ? blk.RenderFrameBlock.open(buffer, pointer, claim)
      : name.startsWith('block_padded')
      ? blk.PaddedFrameBlock.open(buffer, pointer, claim)
      : throw StateError('driver: no block named $name');
  return block == null ? 'refuse\n' : 'open\n';
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

Object? openCookAt(String root, Uint8List? buffer, int base, int bytes) {
  switch (root) {
    case 'Scene':
      return ck.SceneCook.open(buffer, base, bytes);
    case 'Depot':
      return ck.DepotCook.open(buffer, base, bytes);
    case 'Album':
      return ck.AlbumCook.open(buffer, base, bytes);
    case 'TreeNode':
      return ck.TreeNodeCook.open(buffer, base, bytes);
    case 'ListNode':
      return ck.ListNodeCook.open(buffer, base, bytes);
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
  bool nil,
) {
  final (buffer, base, bytes) = place(data, extent, lead, cookAlignment(data));
  final opened = openCookAt(root, nil ? null : buffer, base, bytes);
  return opened == null ? 'refuse\n' : 'open\n';
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
// graphdemo, blobdemo and listdemo answer wire, json-read, json-write and
// report for the roots the variable class carries. mapdemo, messagedemo,
// scalars and widedemo answer the same four for every root: maps, unions and
// their arrays, keyed and fixed arrays, optionals, flags, wstring and the wide
// scalars on form 1 and in the text (docs/SPEC-TABLES.md §2.3, §2.6, §2.8, §3,
// §15, §16). Every other unit stays absent. no-text rows are not asked for a
// text.
bool noTextRow(List<String> f) => f.length > 5 && f[5] == 'no-text';

void spillAbsent(String outDir, String name) {
  File('$outDir/$name.absent').writeAsBytesSync(<int>[]);
}

String _reportLine(int unknown, int kind, int widened, int clamped, bool malformed, bool refused) {
  final m = malformed ? 'true' : 'false';
  final v = refused ? 'refused' : 'read';
  return '$unknown,$kind,$widened,$clamped,0,$m,$v\n';
}

Uint8List? wireResave(String unit, String root, Uint8List wire) {
  switch ('$unit $root') {
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
  }
  return null;
}

Uint8List? jsonReadSave(String unit, String root, String text) {
  switch ('$unit $root') {
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
  }
  return null;
}

String? jsonWriteText(String unit, String root, Uint8List wire) {
  switch ('$unit $root') {
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
  }
  return null;
}

String? loadReport(String unit, String root, Uint8List wire) {
  switch ('$unit $root') {
    case 'graphdemo Scene':
      final r = graphdemo.TableWireReport();
      graphdemo.sceneWireLoad(wire, r);
      return _reportLine(r.unknown, r.kindMismatch, r.widened, r.clamped, r.malformed, r.refused);
    case 'blobdemo Catalog':
      final r = blobdemo.TableWireReport();
      blobdemo.catalogWireLoad(wire, r);
      return _reportLine(r.unknown, r.kindMismatch, r.widened, r.clamped, r.malformed, r.refused);
    case 'listdemo Save':
      final r = listdemo.TableWireReport();
      listdemo.saveWireLoad(wire, r);
      return _reportLine(r.unknown, r.kindMismatch, r.widened, r.clamped, r.malformed, r.refused);
    case 'listdemo Album':
      final r = listdemo.TableWireReport();
      listdemo.albumWireLoad(wire, r);
      return _reportLine(r.unknown, r.kindMismatch, r.widened, r.clamped, r.malformed, r.refused);
    case 'listdemo Sheet':
      final r = listdemo.TableWireReport();
      listdemo.sheetWireLoad(wire, r);
      return _reportLine(r.unknown, r.kindMismatch, r.widened, r.clamped, r.malformed, r.refused);
    case 'listdemo Unbounded':
      final r = listdemo.TableWireReport();
      listdemo.unboundedWireLoad(wire, r);
      return _reportLine(r.unknown, r.kindMismatch, r.widened, r.clamped, r.malformed, r.refused);
    case 'mapdemo Cells':
      final r = mapdemo.TableWireReport();
      mapdemo.cellsWireLoad(wire, r);
      return _reportLine(r.unknown, r.kindMismatch, r.widened, r.clamped, r.malformed, r.refused);
    case 'mapdemo Chunks':
      final r = mapdemo.TableWireReport();
      mapdemo.chunksWireLoad(wire, r);
      return _reportLine(r.unknown, r.kindMismatch, r.widened, r.clamped, r.malformed, r.refused);
    case 'mapdemo Crews':
      final r = mapdemo.TableWireReport();
      mapdemo.crewsWireLoad(wire, r);
      return _reportLine(r.unknown, r.kindMismatch, r.widened, r.clamped, r.malformed, r.refused);
    case 'mapdemo Depth':
      final r = mapdemo.TableWireReport();
      mapdemo.depthWireLoad(wire, r);
      return _reportLine(r.unknown, r.kindMismatch, r.widened, r.clamped, r.malformed, r.refused);
    case 'mapdemo Docs':
      final r = mapdemo.TableWireReport();
      mapdemo.docsWireLoad(wire, r);
      return _reportLine(r.unknown, r.kindMismatch, r.widened, r.clamped, r.malformed, r.refused);
    case 'mapdemo Fleet':
      final r = mapdemo.TableWireReport();
      mapdemo.fleetWireLoad(wire, r);
      return _reportLine(r.unknown, r.kindMismatch, r.widened, r.clamped, r.malformed, r.refused);
    case 'mapdemo Pairs':
      final r = mapdemo.TableWireReport();
      mapdemo.pairsWireLoad(wire, r);
      return _reportLine(r.unknown, r.kindMismatch, r.widened, r.clamped, r.malformed, r.refused);
    case 'mapdemo Runs':
      final r = mapdemo.TableWireReport();
      mapdemo.runsWireLoad(wire, r);
      return _reportLine(r.unknown, r.kindMismatch, r.widened, r.clamped, r.malformed, r.refused);
    case 'mapdemo Slots':
      final r = mapdemo.TableWireReport();
      mapdemo.slotsWireLoad(wire, r);
      return _reportLine(r.unknown, r.kindMismatch, r.widened, r.clamped, r.malformed, r.refused);
    case 'mapdemo Spans':
      final r = mapdemo.TableWireReport();
      mapdemo.spansWireLoad(wire, r);
      return _reportLine(r.unknown, r.kindMismatch, r.widened, r.clamped, r.malformed, r.refused);
    case 'mapdemo Text':
      final r = mapdemo.TableWireReport();
      mapdemo.textWireLoad(wire, r);
      return _reportLine(r.unknown, r.kindMismatch, r.widened, r.clamped, r.malformed, r.refused);
    case 'mapdemo Trails':
      final r = mapdemo.TableWireReport();
      mapdemo.trailsWireLoad(wire, r);
      return _reportLine(r.unknown, r.kindMismatch, r.widened, r.clamped, r.malformed, r.refused);
    case 'messagedemo ToolMessage':
      final r = messagedemo.TableWireReport();
      messagedemo.toolMessageWireLoad(wire, r);
      return _reportLine(r.unknown, r.kindMismatch, r.widened, r.clamped, r.malformed, r.refused);
    case 'scalars SimState':
      final r = scalardemo.TableWireReport();
      scalardemo.simStateWireLoad(wire, r);
      return _reportLine(r.unknown, r.kindMismatch, r.widened, r.clamped, r.malformed, r.refused);
    case 'widedemo Caption':
      final r = widedemo.TableWireReport();
      widedemo.captionWireLoad(wire, r);
      return _reportLine(r.unknown, r.kindMismatch, r.widened, r.clamped, r.malformed, r.refused);
    case 'widedemo Stamp':
      final r = widedemo.TableWireReport();
      widedemo.stampWireLoad(wire, r);
      return _reportLine(r.unknown, r.kindMismatch, r.widened, r.clamped, r.malformed, r.refused);
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
const Map<String, String> cookWriteAbsent = <String, String>{
  'tabledemo': _fixedFormOnly,
  'tblv1': _fixedFormOnly,
  'tblv2': _fixedFormOnly,
  'tblp1': _fixedFormOnly,
  'tblp3': _fixedFormOnly,
  'tblm1': _fixedFormOnly,
  'tblm2': _fixedFormOnly,
  'messagedemo':
      'the Dart table backend refuses the unit (a union arm '
      'carrying `was`, docs/SPEC-TABLES.md §5): no Dart value to cook',
  'streamdemo':
      'the Dart table backend refuses the unit (a union a table '
      'closure holds, docs/SPEC-TABLES.md §2.6, §11): no Dart value to cook',
  'scalars':
      'the unit carries the wide kinds, for which the Dart table '
      'backend emits no cook form (docs/SPEC-TABLES.md §3)',
};

const String _fixedFormOnly =
    'the Dart fixed class reads form 3 only and '
    "this instance's wire is form 1, which Dart reads for the variable class "
    'alone (docs/SPEC-TABLES.md §3, §3.4): no Dart value to cook';

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
    final saved = wireResave(f[2], f[3], File(f[4]).readAsBytesSync());
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

// _messageRow binds one fixed root's batch verbs. The vocabulary and report
// classes are each unit home's own, so the binding is generic over them.
MessageRun _messageRow<T, V, R>({
  required T Function() fresh,
  required void Function(T) init,
  required V Function() vocabulary,
  required R Function() report,
  required bool Function(V, Uint8List, R) announceRead,
  required int Function(List<T>, Uint8List, V, R) load,
  required int Function(List<T>, R) measure,
  required int Function(List<T>, Uint8List, R) save,
  required bool Function(R) damaged,
}) {
  return (Uint8List announcement, Uint8List wire) {
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
  };
}

List<E> _entries<E>(E Function() zero) => List<E>.generate(4096, (_) => zero());

final Map<String, MessageRun> messageRuns = {
  'backenddemo LoginRequest': _messageRow<backenddemo.LoginRequest, backenddemo.TableVocabulary, backenddemo.TableReport>(
    fresh: backenddemo.LoginRequest.new,
    init: backenddemo.initLoginRequest,
    vocabulary: () => backenddemo.TableVocabulary(entries: _entries(() => backenddemo.TableMessageEntry(0, 0))),
    report: backenddemo.TableReport.new,
    announceRead: backenddemo.announceRead,
    load: backend.loginRequestLoadMessages,
    measure: backend.loginRequestMeasureMessages,
    save: backend.loginRequestSaveMessages,
    damaged: (r) => r.refused || r.malformed,
  ),
  'backenddemo MatchResult': _messageRow<backenddemo.MatchResult, backenddemo.TableVocabulary, backenddemo.TableReport>(
    fresh: backenddemo.MatchResult.new,
    init: backenddemo.initMatchResult,
    vocabulary: () => backenddemo.TableVocabulary(entries: _entries(() => backenddemo.TableMessageEntry(0, 0))),
    report: backenddemo.TableReport.new,
    announceRead: backenddemo.announceRead,
    load: backend.matchResultLoadMessages,
    measure: backend.matchResultMeasureMessages,
    save: backend.matchResultSaveMessages,
    damaged: (r) => r.refused || r.malformed,
  ),
  'backenddemo StorePurchase': _messageRow<backenddemo.StorePurchase, backenddemo.TableVocabulary, backenddemo.TableReport>(
    fresh: backenddemo.StorePurchase.new,
    init: backenddemo.initStorePurchase,
    vocabulary: () => backenddemo.TableVocabulary(entries: _entries(() => backenddemo.TableMessageEntry(0, 0))),
    report: backenddemo.TableReport.new,
    announceRead: backenddemo.announceRead,
    load: backend.storePurchaseLoadMessages,
    measure: backend.storePurchaseMeasureMessages,
    save: backend.storePurchaseSaveMessages,
    damaged: (r) => r.refused || r.malformed,
  ),
  'backenddemo Envelope': _messageRow<backenddemo.Envelope, backenddemo.TableVocabulary, backenddemo.TableReport>(
    fresh: backenddemo.Envelope.new,
    init: backenddemo.initEnvelope,
    vocabulary: () => backenddemo.TableVocabulary(entries: _entries(() => backenddemo.TableMessageEntry(0, 0))),
    report: backenddemo.TableReport.new,
    announceRead: backenddemo.announceRead,
    load: backend.envelopeLoadMessages,
    measure: backend.envelopeMeasureMessages,
    save: backend.envelopeSaveMessages,
    damaged: (r) => r.refused || r.malformed,
  ),
  'vocabdemo Wide00': _messageRow<vocabdemo.Wide00, vocabdemo.TableVocabulary, vocabdemo.TableReport>(
    fresh: vocabdemo.Wide00.new,
    init: vocabdemo.initWide00,
    vocabulary: () => vocabdemo.TableVocabulary(entries: _entries(() => vocabdemo.TableMessageEntry(0, 0))),
    report: vocabdemo.TableReport.new,
    announceRead: vocabdemo.announceRead,
    load: vocab.wide00LoadMessages,
    measure: vocab.wide00MeasureMessages,
    save: vocab.wide00SaveMessages,
    damaged: (r) => r.refused || r.malformed,
  ),
  'vocabdemo Wide09': _messageRow<vocabdemo.Wide09, vocabdemo.TableVocabulary, vocabdemo.TableReport>(
    fresh: vocabdemo.Wide09.new,
    init: vocabdemo.initWide09,
    vocabulary: () => vocabdemo.TableVocabulary(entries: _entries(() => vocabdemo.TableMessageEntry(0, 0))),
    report: vocabdemo.TableReport.new,
    announceRead: vocabdemo.announceRead,
    load: vocab.wide09LoadMessages,
    measure: vocab.wide09MeasureMessages,
    save: vocab.wide09SaveMessages,
    damaged: (r) => r.refused || r.malformed,
  ),
  'vocab9demo Wide00': _messageRow<vocab9demo.Wide00, vocab9demo.TableVocabulary, vocab9demo.TableReport>(
    fresh: vocab9demo.Wide00.new,
    init: vocab9demo.initWide00,
    vocabulary: () => vocab9demo.TableVocabulary(entries: _entries(() => vocab9demo.TableMessageEntry(0, 0))),
    report: vocab9demo.TableReport.new,
    announceRead: vocab9demo.announceRead,
    load: vocab9.wide00LoadMessages,
    measure: vocab9.wide00MeasureMessages,
    save: vocab9.wide00SaveMessages,
    damaged: (r) => r.refused || r.malformed,
  ),
  'vocab9demo Wide19': _messageRow<vocab9demo.Wide19, vocab9demo.TableVocabulary, vocab9demo.TableReport>(
    fresh: vocab9demo.Wide19.new,
    init: vocab9demo.initWide19,
    vocabulary: () => vocab9demo.TableVocabulary(entries: _entries(() => vocab9demo.TableMessageEntry(0, 0))),
    report: vocab9demo.TableReport.new,
    announceRead: vocab9demo.announceRead,
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
    if (run == null) {
      // A message row whose root is a plain `table` carries no message verbs
      // (docs/SPEC-TABLES.md §3.3: "a plain `table T` may not" ride the
      // form), so the driver answers ABSENT and names the reason.
      File('$outDir/${f[1]}.absent').writeAsStringSync('plain table: §3.3');
      continue;
    }
    final answer = run(File(conn[4]).readAsBytesSync(), File(f[5]).readAsBytesSync());
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
    // cook-write the graphdemo and blobdemo rows (§7.6). The
    // accelerators and the forgery surfaces stay. The wire fuzzer's roots are
    // another card's.
    ''
    'wire\nmessage\njson-read\njson-write\nreport\n'
    'cook\ncook-write\ncook-foreign\nblock\nblock-foreign\nblock-dump\n'
    'forgery\ncook-forgery\n';

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
void _putU16(List<int> out, int v) {
  out.add(v & 0xFF);
  out.add((v >> 8) & 0xFF);
}

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

Future<void> wireFuzz() async {
  final src = _ByteSource();
  final count = ByteData.sublistView(await src.read(4)).getUint32(0, Endian.little);
  final roster = <bool>[];
  final retains = <bool>[];
  final available = <int>[];
  for (var i = 0; i < count; i++) {
    final unit = await _readString(src);
    final root = await _readString(src);
    final form = (await src.read(1))[0];
    final retain = (await src.read(1))[0];
    final ok = unit == 'cards' && root == 'Card' && form == 1;
    roster.add(ok);
    retains.add(ok && retain == 1);
    available.add(ok ? 1 : 0);
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
    final report = TableWireReport();
    var loaded = true;
    var measure = 0;
    var saved = <int>[];
    if (!roster[index]) {
      loaded = false;
      measure = -1;
    } else if (retains[index]) {
      // the retention arm (docs/SPEC-TABLES.md §6.6): read and write back
      // through the retaining pair, with the two caller-owned stores the
      // harness fixes (1 MiB of bytes and one id per eight bytes).
      final retain = TableRetain(1048576, List<int>.filled(131072, 0));
      measure = cardWireMeasure(wire);
      loaded = measure >= 0;
      final card = cardWireLoadRetain(wire, retain, report);
      saved = cardWireSaveRetain(card, retain, report);
    } else {
      measure = cardWireMeasure(wire);
      loaded = measure >= 0;
      final card = cardWireLoad(wire, report);
      saved = cardWireSave(card);
    }
    final out = <int>[];
    _putU8(out, loaded ? 1 : 0);
    _putU32(out, report.unknown);
    _putU32(out, report.kindMismatch);
    _putU32(out, report.widened);
    _putU32(out, report.clamped);
    _putU32(out, 0); // duplicate: the card carries no map
    _putU8(out, report.malformed ? 1 : 0);
    _putU8(out, report.refused ? 1 : 0);
    _putU64(out, measure);
    _putU32(out, report.retained);
    _putU32(out, report.retainLost);
    _putU64(out, saved.length);
    out.addAll(saved);
    stdout.add(out);
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
    case 'json-read':
      return surfaceJsonRead(out);
    case 'json-write':
      return surfaceJsonWrite(out);
    case 'report':
      return surfaceReport(out);
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
  }
  return 2;
}
