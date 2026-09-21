// test/dart-tables/accessor_descriptor.dart
//
// The Dart half of the J1 technique (docs/PORTING.md, schema#421): the
// generated ACCESSORS and the generated DESCRIPTORS are two independent
// derivations of one layout, and a reading tier that only walks the descriptors
// (fuzz.dart) could read the descriptors twice and never know. This reads both
// ways and requires agreement.
//
// Every named field is PLANT-AND-READ on a fresh copy of its fixture (never the
// loaded bytes, and after open has already returned, so no check re-runs): the
// record is zeroed, the descriptor's own bytes are planted 0xFF and the
// accessor must change, then a non-overlapping neighbour window is planted and
// the accessor must NOT change. A scalar additionally gets a straight two-way
// value read. A pointer slot is held on its OFFSET only: the emitter's pointer
// accessor reads one unsigned byte where the descriptor names eight signed
// (block.go:360-362, block.go:413-437), so the width is not held here yet.
//
// Every finding is printed and none aborts the run. A summary names the count
// and the process exits nonzero on any finding, or on fewer than twelve fields
// checked.
import 'dart:io';
import 'dart:typed_data';

import '../../build/tables-generated-dart/block/BlockdemoBlock.dart';
import '../../build/tables-generated-dart/block/RenderBlock.dart' as blk;
import '../../build/tables-generated-dart/pointers/GraphdemoCook.dart';
import '../../build/tables-generated-dart/pointers/GraphCook.dart' as ck;

int findings = 0;
int checked = 0;
int records = 0;
int uncheckedStrings = 0;

void report(String what) {
  findings++;
  stdout.writeln(what);
}

// The two message families the negative controls grep for.
String family(bool isPointer) => isPointer
    ? "the slot accessor's offset is not the descriptor's"
    : 'the accessor and the descriptor disagree';

class Opened {
  final int base;
  final num Function() accessor;
  final num Function() viaDescriptor;
  Opened(this.base, this.accessor, this.viaDescriptor);
}

class Probe {
  final String display;
  final bool isPointer;
  final int offset;
  final int size;
  final int recordSize;
  final Opened? Function(Uint8List copy) open;

  Probe({
    required this.display,
    required this.isPointer,
    required this.offset,
    required this.size,
    required this.recordSize,
    required this.open,
  });
}

void checkProbe(Probe p, Uint8List source) {
  checked++;
  if (p.offset + p.size > p.recordSize) {
    report('${p.display}: ${family(p.isPointer)} — the descriptor\'s range '
        '[${p.offset}, ${p.offset + p.size}) does not lie inside the '
        '${p.recordSize}-byte record');
    return;
  }

  final copy = Uint8List.fromList(source);
  final opened = p.open(copy);
  if (opened == null) {
    report('${p.display}: ${family(p.isPointer)} — the fixture did not open');
    return;
  }
  final base = opened.base;

  // the straight two-way value read, where the descriptor's width and
  // signedness are unambiguous (scalars only).
  if (!p.isPointer) {
    final a = opened.accessor();
    final d = opened.viaDescriptor();
    if (a != d) {
      report('${p.display}: ${family(false)} — the accessor answered $a '
          'but the descriptor names $d');
    }
  }

  // zero the whole record, then plant the descriptor's own bytes.
  for (var i = base; i < base + p.recordSize; i++) {
    copy[i] = 0;
  }
  final zero = opened.accessor();

  for (var i = base + p.offset; i < base + p.offset + p.size; i++) {
    copy[i] = 0xFF;
  }
  final own = opened.accessor();
  if (own == zero) {
    report('${p.display}: ${family(p.isPointer)} — the accessor did not see '
        'the bytes the descriptor names '
        '([${p.offset}, ${p.offset + p.size}) planted 0xFF, accessor answered $own)');
  }

  // restore, then plant a NEIGHBOUR window that does not overlap the field.
  for (var i = base + p.offset; i < base + p.offset + p.size; i++) {
    copy[i] = 0;
  }
  final int nbStart;
  if (p.offset + p.size + p.size <= p.recordSize) {
    nbStart = p.offset + p.size;
  } else {
    nbStart = p.offset - p.size;
  }
  for (var i = base + nbStart; i < base + nbStart + p.size; i++) {
    copy[i] = 0xFF;
  }
  final nb = opened.accessor();
  if (nb != zero) {
    report('${p.display}: ${family(p.isPointer)} — the accessor saw bytes '
        'OUTSIDE the ones the descriptor names '
        '([$nbStart, ${nbStart + p.size}) planted 0xFF, '
        '[${p.offset}, ${p.offset + p.size}) zero, accessor answered $nb)');
  }
}

// A block field this gate reads: out-of-line triples and nested records are
// structural (a triple or a cursor, not a scalar accessor), and a string or
// `bytes` is DELIBERATELY UNCHECKED (its descriptor offset names the bytes
// while the emitted accessor is a `…Length` getter at countOffset). Anything
// else must be named in the accessor table, or it is a finding.
void enforceBlock(String record, TableBlockInfo info, Set<String> named) {
  records++;
  for (final f in info.fields) {
    if (f.outOfLine) continue;
    if (f.element != null) continue;
    if (f.counted && f.countOffset >= 0) {
      uncheckedStrings++;
      continue;
    }
    if (!named.contains(f.name)) {
      report('$record.${f.name}: the gate names no accessor for this field');
    }
  }
}

void enforceCook(String record, TableCookInfo info, Set<String> named) {
  records++;
  for (final f in info.fields) {
    if (f.isPointer) {
      if (!named.contains(f.name)) {
        report('$record.${f.name}: the gate names no accessor for this field');
      }
      continue;
    }
    if (f.storage == TableCookStorage.string ||
        f.storage == TableCookStorage.bytes) {
      uncheckedStrings++;
      continue;
    }
    if (f.storage == TableCookStorage.record) continue;
    if (!named.contains(f.name)) {
      report('$record.${f.name}: the gate names no accessor for this field');
    }
  }
}

// The descriptor's own width and signedness, for the fields this gate names.
num blockBlind(ByteData view, int at, TableBlockFieldInfo f) {
  if (f.kind == 10) return view.getFloat32(at, Endian.little);
  if (f.kind == 11) return view.getFloat64(at, Endian.little);
  final signed = f.kind >= 2 && f.kind <= 5;
  switch (f.size) {
    case 1:
      return signed ? view.getInt8(at) : view.getUint8(at);
    case 2:
      return signed ? view.getInt16(at, Endian.little) : view.getUint16(at, Endian.little);
    case 4:
      return signed ? view.getInt32(at, Endian.little) : view.getUint32(at, Endian.little);
    default:
      return signed ? view.getInt64(at, Endian.little) : view.getUint64(at, Endian.little);
  }
}

num cookBlind(ByteData view, int at, TableCookFieldInfo f) {
  if (f.storage == TableCookStorage.float) {
    return f.elemSize == 4
        ? view.getFloat32(at, Endian.little)
        : view.getFloat64(at, Endian.little);
  }
  if (f.storage == TableCookStorage.signed) {
    switch (f.elemSize) {
      case 1:
        return view.getInt8(at);
      case 2:
        return view.getInt16(at, Endian.little);
      case 4:
        return view.getInt32(at, Endian.little);
      default:
        return view.getInt64(at, Endian.little);
    }
  }
  switch (f.elemSize) {
    case 1:
      return view.getUint8(at);
    case 2:
      return view.getUint16(at, Endian.little);
    case 4:
      return view.getUint32(at, Endian.little);
    default:
      return view.getUint64(at, Endian.little);
  }
}

void main() {
  final versionField =
      blk.RenderFrameBlock.type.fields.firstWhere((f) => f.name == 'version');
  final cameraRow = blk.RenderFrameBlock.rowRenderCamera;
  final cameraIdField =
      cameraRow.fields.firstWhere((f) => f.name == 'camera_id');
  final cameraTypeField =
      cameraRow.fields.firstWhere((f) => f.name == 'camera_type');
  final targetObjectIdField =
      cameraRow.fields.firstWhere((f) => f.name == 'target_object_id');
  final fovField = cameraRow.fields.firstWhere((f) => f.name == 'fov');

  final valueField =
      ck.ListNodeCook.type.fields.firstWhere((f) => f.name == 'value');
  final nextField =
      ck.ListNodeCook.type.fields.firstWhere((f) => f.name == 'next');

  final sceneVersionField =
      ck.SceneCook.type.fields.firstWhere((f) => f.name == 'version');
  final headField = ck.SceneCook.type.fields.firstWhere((f) => f.name == 'head');
  final treeField = ck.SceneCook.type.fields.firstWhere((f) => f.name == 'tree');
  final settingsField =
      ck.SceneCook.type.fields.firstWhere((f) => f.name == 'settings');
  final aliasField =
      ck.SceneCook.type.fields.firstWhere((f) => f.name == 'alias');

  enforceBlock('RenderFrame', blk.RenderFrameBlock.type, {'version'});
  enforceBlock('RenderCamera', cameraRow,
      {'camera_id', 'camera_type', 'target_object_id', 'fov'});
  enforceCook('ListNode', ck.ListNodeCook.type, {'value', 'next'});
  enforceCook('Scene', ck.SceneCook.type,
      {'version', 'head', 'tree', 'settings', 'alias'});

  final blockBytes =
      File('testdata/wire/tables/block_render.bin').readAsBytesSync();
  final listNodeBytes = File('build/cook-fuzz/ListNode.cook').readAsBytesSync();
  final sceneBytes = File('build/cook-fuzz/Scene.cook').readAsBytesSync();

  Probe blockScalar(String display, TableBlockFieldInfo f, Opened? Function(Uint8List) open) =>
      Probe(
        display: display,
        isPointer: false,
        offset: f.offset,
        size: f.size,
        recordSize: blk.RenderFrameBlock.projectionSize,
        open: open,
      );

  Probe rowScalar(String display, TableBlockFieldInfo f, Opened? Function(Uint8List) open) =>
      Probe(
        display: display,
        isPointer: false,
        offset: f.offset,
        size: f.size,
        recordSize: cameraRow.size,
        open: open,
      );

  Probe cookScalar(String display, TableCookFieldInfo f, int recordSize, Opened? Function(Uint8List) open) =>
      Probe(
        display: display,
        isPointer: false,
        offset: f.offset,
        size: f.size,
        recordSize: recordSize,
        open: open,
      );

  Probe pointer(String display, TableCookFieldInfo f, int recordSize, Opened? Function(Uint8List) open) =>
      Probe(
        display: display,
        isPointer: true,
        offset: f.offset,
        size: f.size,
        recordSize: recordSize,
        open: open,
      );

  final blockProbes = <Probe>[
    blockScalar('RenderFrame.version', versionField, (copy) {
      final b = blk.RenderFrameBlock.open(copy, 0, copy.length);
      if (b == null) return null;
      return Opened(b.base, () => b.version,
          () => blockBlind(b.view, b.base + versionField.offset, versionField));
    }),
    rowScalar('RenderCamera.cameraId', cameraIdField, (copy) {
      final b = blk.RenderFrameBlock.open(copy, 0, copy.length);
      if (b == null) return null;
      final row = b.camerasCursor();
      b.camerasAt(0, row);
      return Opened(row.at, () => row.cameraId,
          () => blockBlind(row.view, row.at + cameraIdField.offset, cameraIdField));
    }),
    rowScalar('RenderCamera.cameraType', cameraTypeField, (copy) {
      final b = blk.RenderFrameBlock.open(copy, 0, copy.length);
      if (b == null) return null;
      final row = b.camerasCursor();
      b.camerasAt(0, row);
      return Opened(row.at, () => row.cameraType,
          () => blockBlind(row.view, row.at + cameraTypeField.offset, cameraTypeField));
    }),
    rowScalar('RenderCamera.targetObjectId', targetObjectIdField, (copy) {
      final b = blk.RenderFrameBlock.open(copy, 0, copy.length);
      if (b == null) return null;
      final row = b.camerasCursor();
      b.camerasAt(0, row);
      return Opened(
          row.at,
          () => row.targetObjectId,
          () => blockBlind(
              row.view, row.at + targetObjectIdField.offset, targetObjectIdField));
    }),
    rowScalar('RenderCamera.fov', fovField, (copy) {
      final b = blk.RenderFrameBlock.open(copy, 0, copy.length);
      if (b == null) return null;
      final row = b.camerasCursor();
      b.camerasAt(0, row);
      return Opened(row.at, () => row.fov,
          () => blockBlind(row.view, row.at + fovField.offset, fovField));
    }),
  ];

  final listNodeProbes = <Probe>[
    cookScalar('ListNode.value', valueField, ck.ListNodeCook.type.size, (copy) {
      final c = ck.ListNodeCook.open(copy, 0, copy.length);
      if (c == null) return null;
      final row = c.cursor();
      c.root(row);
      return Opened(row.at, () => row.value,
          () => cookBlind(row.view, row.at + valueField.offset, valueField));
    }),
    pointer('ListNode.next', nextField, ck.ListNodeCook.type.size, (copy) {
      final c = ck.ListNodeCook.open(copy, 0, copy.length);
      if (c == null) return null;
      final row = c.cursor();
      c.root(row);
      return Opened(row.at, () => row.next, () => 0);
    }),
  ];

  final sceneProbes = <Probe>[
    cookScalar('Scene.version', sceneVersionField, ck.SceneCook.type.size, (copy) {
      final c = ck.SceneCook.open(copy, 0, copy.length);
      if (c == null) return null;
      final row = c.cursor();
      c.root(row);
      return Opened(row.at, () => row.version,
          () => cookBlind(row.view, row.at + sceneVersionField.offset, sceneVersionField));
    }),
    pointer('Scene.head', headField, ck.SceneCook.type.size, (copy) {
      final c = ck.SceneCook.open(copy, 0, copy.length);
      if (c == null) return null;
      final row = c.cursor();
      c.root(row);
      return Opened(row.at, () => row.head, () => 0);
    }),
    pointer('Scene.tree', treeField, ck.SceneCook.type.size, (copy) {
      final c = ck.SceneCook.open(copy, 0, copy.length);
      if (c == null) return null;
      final row = c.cursor();
      c.root(row);
      return Opened(row.at, () => row.tree, () => 0);
    }),
    pointer('Scene.settings', settingsField, ck.SceneCook.type.size, (copy) {
      final c = ck.SceneCook.open(copy, 0, copy.length);
      if (c == null) return null;
      final row = c.cursor();
      c.root(row);
      return Opened(row.at, () => row.settings, () => 0);
    }),
    pointer('Scene.alias', aliasField, ck.SceneCook.type.size, (copy) {
      final c = ck.SceneCook.open(copy, 0, copy.length);
      if (c == null) return null;
      final row = c.cursor();
      c.root(row);
      return Opened(row.at, () => row.alias, () => 0);
    }),
  ];

  for (final p in blockProbes) {
    checkProbe(p, blockBytes);
  }
  for (final p in listNodeProbes) {
    checkProbe(p, listNodeBytes);
  }
  for (final p in sceneProbes) {
    checkProbe(p, sceneBytes);
  }

  stdout.writeln('dart accessor/descriptor: $checked fields checked over '
      '$records records — $findings finding(s)');
  if (checked < 12) {
    stdout.writeln(
        'dart accessor/descriptor: only $checked fields checked, fewer than twelve — not a pass');
    findings++;
  }
  exit(findings == 0 ? 0 : 1);
}
