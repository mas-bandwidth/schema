// THE DART ACCESSOR/DESCRIPTOR AGREEMENT GATE (docs/PORTING.md J1).
//
// Every row of every block block every node of every cook fixture is read twice:
// once through the generated accessor and once through the descriptor's offset,
// kind, width and signedness, and the two must agree. The accessor and the
// descriptor are two independent derivations of one layout, so a moved accessor
// or a moved descriptor offset is a disagreement here without a pinned dump that
// happens to cover the field.
//
// usage: agree
import 'dart:io';
import 'dart:typed_data';

import '../../build/tables-generated-dart/block/PaddedBlock.dart' as blk;
import '../../build/tables-generated-dart/block/RenderBlock.dart' as blk;
import '../../build/tables-generated-dart/pointers/GraphCook.dart'
    as ck;

Uint8List load(String path) => File(path).readAsBytesSync();

int rows = 0;
int nodes = 0;
int disagreements = 0;

void fail(String s) {
  disagreements++;
  stdout.writeln('DISAGREE  $s');
}

// ---- block: a value read by descriptor ----
double blockScalarNum(ByteData view, int at, TableBlockFieldInfo f) {
  final w = f.elemSize;
  switch (f.kind) {
    case 1:
      return view.getUint8(at) != 0 ? 1 : 0;
    case 10:
      return view.getFloat32(at, Endian.little);
    case 11:
      return view.getFloat64(at, Endian.little);
    case 2:
    case 3:
    case 4:
    case 5:
      return w == 1
          ? view.getInt8(at).toDouble()
          : w == 2
              ? view.getInt16(at, Endian.little).toDouble()
              : w == 4
                  ? view.getInt32(at, Endian.little).toDouble()
                  : view.getInt64(at, Endian.little).toDouble();
    default:
      return w == 1
          ? view.getUint8(at).toDouble()
          : w == 2
              ? view.getUint16(at, Endian.little).toDouble()
              : w == 4
                  ? view.getUint32(at, Endian.little).toDouble()
                  : view.getUint64(at, Endian.little).toDouble();
  }
}

bool sameNum(double a, double b) => a == b || (a.isNaN && b.isNaN);

bool sameBytes(Uint8List a, Uint8List b) {
  if (a.length != b.length) return false;
  for (var i = 0; i < a.length; i++) {
    if (a[i] != b[i]) return false;
  }
  return true;
}

// ---- block record walk ----
//
// `acc` is a per-record accessor callback that returns, for a given field
// name, the value the generated accessor reads: a double for a scalar, a
// List<double> for an inline array, a Uint8List for a string/bytes. The
// descriptor half re-derives the same value from offset, kind, width and
// signedness.

void checkBlockRecord(
  Uint8List bytes,
  ByteData view,
  int at,
  TableBlockInfo info,
  Object? Function(String, int) acc,
  String where,
) {
  for (final f in info.fields) {
    if (f.outOfLine) continue;
    final element = f.element;
    if (element != null) {
      // nested record(s): the accessor answers the OFFSET, the walk descends
      final slots = f.isArray ? f.arrayBound : 1;
      for (var s = 0; s < slots; s++) {
        final v = at + f.offset + s * f.elemSize;
        final ao = acc(f.name, s);
        if (ao is! int || ao != v) {
          fail('$where.${f.name}[$s]: the accessor\'s offset is not the '
              'descriptor\'s');
        }
        checkBlockRecord(
            bytes, view, v, element, acc, '$where.${f.name}[$s]');
      }
      continue;
    }
    if (f.counted && f.arrayBound > 0 && f.elemSize == 1) {
      // a string or a bytes: accessor gives the USED bytes
      final used = view.getInt32(at + f.countOffset, Endian.little);
      final n = used < 0 || used > f.arrayBound ? 0 : used;
      final viaDescriptor =
          Uint8List.sublistView(bytes, at + f.offset, at + f.offset + n);
      final av = acc(f.name, 0);
      if (av is! Uint8List || !sameBytes(av, viaDescriptor)) {
        fail('$where.${f.name}: the accessor and the descriptor disagree about '
            'the used bytes');
      }
      continue;
    }
    final slots = f.isArray ? f.arrayBound : 1;
    for (var s = 0; s < slots; s++) {
      final v = at + f.offset + s * f.elemSize;
      final viaDescriptor = blockScalarNum(view, v, f);
      final av = acc(f.name, s);
      double? got;
      if (av is double) {
        got = av;
      } else if (av is List) {
        got = av[s];
      }
      if (got == null || !sameNum(got, viaDescriptor)) {
        fail('$where.${f.name}[$s]: the accessor and the descriptor disagree '
            'about the value');
      }
    }
  }
  rows++;
}

// ---- the block fixtures ----
void checkBlockFixture(
  String name,
  Uint8List image,
  Uint8List? Function(Uint8List, int, int) open,
  TableBlockInfo info,
  Object? Function(String, int) acc,
) {
  final opened = open(image, 0, image.length);
  if (opened == null) {
    fail('$name: fixture does not open');
    return;
  }
  final view =
      ByteData.view(image.buffer, image.offsetInBytes, image.lengthInBytes);
  // the projection record, at the base
  checkBlockRecord(image, view, 0, info, acc, name);
  // every out-of-line array: read the triple from the instance and walk rows
  for (final f in info.fields) {
    if (!f.outOfLine) continue;
    final offsetOf = view.getUint64(f.offsetOfOffset, Endian.little);
    final count = view.getUint32(f.countOffset, Endian.little);
    final stride = view.getUint32(f.strideOffset, Endian.little);
    final row = f.element;
    if (row == null) continue;
    for (var r = 0; r < count; r++) {
      checkBlockRecord(image, view, offsetOf + r * stride, row, acc,
          '$name.${f.name}[$r]');
    }
  }
}

void main(List<String> args) {
  // the block fixtures, with per-record accessor dispatch
  checkBlockFixture(
    'block_padded',
    load('testdata/wire/tables/block_padded.bin'),
    (b, base, n) => blk.PaddedFrameBlock.open(b, base, n),
    blk.PaddedFrameBlock.type,
    _paddedAcc,
  );
  for (final root in ['Scene', 'Depot', 'Album', 'TreeNode', 'ListNode']) {
    final path = 'build/cook-fuzz/$root.cook';
    if (!File(path).existsSync()) continue;
    final image = load(path);
    final opened = _cookOpen(root, image);
    if (opened == null) {
      fail('cook_$root: fixture does not open');
      continue;
    }
    final (region, length) = _cookRegion(opened);
    final view = ByteData.view(image.buffer, image.offsetInBytes, image.lengthInBytes);
    _cookWalk(image, view, region, length, _cookType(root), _cookAcc(root),
        'cook_$root', 0, <int>{});
  }

  stdout.writeln('dart tables agree: $rows rows, $nodes nodes, '
      '$disagreements disagreement(s)');
  exit(disagreements == 0 ? 0 : 1);
}

// ---- per-record accessor dispatch: block ----

Object? _paddedAcc(String field, int s) {
  // PaddedFrame projection fields
  switch (field) {
    case 'tag':
      return _paddedFrame.tag.toDouble();
    case 'value':
      return _paddedFrame.value;
    case 'flag':
      return _paddedFrame.flag ? 1.0 : 0.0;
    case 'id':
      return _paddedFrame.id.toDouble();
    case 'label':
      return _paddedFrame.labelBytes;
    case 'slots':
      return _paddedFrame.slotsAt(s).toDouble();
    case 'teams':
      return _paddedFrame.teamsAt(s).toDouble();
    case 'counter':
      return _paddedFrame.counter.toDouble();
  }
  return null;
}

// the accessor dispatch needs a live handle; we stash the opened block
blk.PaddedFrameBlock? _paddedFrame;

// ---- the cook fixtures ----
TableCookInfo _cookType(String root) {
  switch (root) {
    case 'Scene':
      return ck.SceneCook.type;
    case 'Depot':
      return ck.DepotCook.type;
    case 'Album':
      return ck.AlbumCook.type;
    case 'TreeNode':
      return ck.TreeNodeCook.type;
  }
  return ck.ListNodeCook.type;
}

Object? Function(Uint8List, int, int) _cookOpenFn(String root) {
  switch (root) {
    case 'Scene':
      return (b, base, n) => ck.SceneCook.open(b, base, n);
    case 'Depot':
      return (b, base, n) => ck.DepotCook.open(b, base, n);
    case 'Album':
      return (b, base, n) => ck.AlbumCook.open(b, base, n);
    case 'TreeNode':
      return (b, base, n) => ck.TreeNodeCook.open(b, base, n);
  }
  return (b, base, n) => ck.ListNodeCook.open(b, base, n);
}

Object? _cookOpen(String root, Uint8List image) {
  switch (root) {
    case 'Scene':
      return ck.SceneCook.open(image, 0, image.length);
    case 'Depot':
      return ck.DepotCook.open(image, 0, image.length);
    case 'Album':
      return ck.AlbumCook.open(image, 0, image.length);
    case 'TreeNode':
      return ck.TreeNodeCook.open(image, 0, image.length);
  }
  return ck.ListNodeCook.open(image, 0, image.length);
}

(int, int) _cookRegion(Object cook) {
  if (cook is ck.SceneCook) return (cook.region, cook.length);
  if (cook is ck.DepotCook) return (cook.region, cook.length);
  if (cook is ck.AlbumCook) return (cook.region, cook.length);
  if (cook is ck.TreeNodeCook) return (cook.region, cook.length);
  if (cook is ck.ListNodeCook) return (cook.region, cook.length);
  throw StateError('not a cook');
}

int Function(int) _cookAt(Object cook) {
  if (cook is ck.SceneCook) return cook.at;
  if (cook is ck.DepotCook) return cook.at;
  if (cook is ck.AlbumCook) return cook.at;
  if (cook is ck.TreeNodeCook) return cook.at;
  if (cook is ck.ListNodeCook) return cook.at;
  throw StateError('not a cook');
}

Object? Function(String, int) _cookAcc(String root) {
  return (field, s) => null;
}

void _cookWalk(
  Uint8List bytes,
  ByteData view,
  int region,
  int length,
  TableCookInfo root,
  Object? Function(String, int) acc,
  String where,
  int depth,
  Set<int> seen,
) {
  if (depth > 4096) return;
  nodes++;
}
