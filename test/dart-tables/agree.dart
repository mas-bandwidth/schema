// THE DART ACCESSOR/DESCRIPTOR AGREEMENT GATE (docs/PORTING.md J1).
//
// Every row and every node of the block and cook fixtures is read twice: once
// through the generated accessor and once through the descriptor's offset,
// kind, width and signedness, and the two must agree. A moved accessor or a
// moved descriptor offset is a disagreement here without a pinned dump that
// happens to cover the field, because the two halves are independent
// derivations of one layout.
//
// usage: agree
import 'dart:io';
import 'dart:typed_data';

import '../../build/tables-generated-dart/block/PaddedBlock.dart' as blk;
import '../../build/tables-generated-dart/block/RenderBlock.dart' as blk;
import '../../build/tables-generated-dart/pointers/GraphCook.dart'
    as graphck;
import '../../build/tables-generated-dart/pointers/MarksCook.dart'
    as marksck;
import '../../build/tables-generated-dart/pointers/PartsCook.dart'
    as partsck;

Uint8List load(String path) => File(path).readAsBytesSync();

class Disagree {
  final String where;
  Disagree(this.where);
  @override
  String toString() => 'the accessor and the descriptor disagree at $where';
}

int rows = 0;
int nodes = 0;

void fail(String s) {
  stdout.writeln('DISAGREE  $s');
}

// ---- block: scalar read by descriptor ----
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

void main(List<String> args) {
  stdout.writeln('dart tables agree: NOT YET WIRED, $rows rows, $nodes nodes');
}
