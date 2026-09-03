// The unit's shared Dart BLOCK runtime (docs/SPEC-TABLES.md §19, §20): the magic,
// this build's byte order, the build version, and the reflection descriptor
// classes every block's own descriptors are built from.
//
// Emitted once per unit into <Package>Block.dart, which every other
// <Base>Block.dart of the unit imports.
package darttable

import "fmt"

func blockRuntimeSource(buildVersion uint64) string {
	return fmt.Sprintf(blockRuntimeTemplate, buildVersion)
}

const blockRuntimeTemplate = `
// THE BUILD VERSION (docs/SPEC-TABLES.md §20): one digest over every fact the bytes
// this build produces depend on — the type wire's protocol id, every table's
// layout keyed by wire id, every table's meaning (defaults, ranges, enum and
// union vocabularies, keyed the same way), and the build's byte order. It is
// the number a block carries and the number open compares.
//
// There are TWO ids in the design and they are not interchangeable: the
// PROTOCOL ID is the type wire's and nothing else, and the BUILD VERSION is
// what everything cooked or blocked is keyed by. A table edit moves this and
// never the protocol id; a type edit moves both.
const int tableBuildVersion = 0x%016x;

// The block's magic (docs/SPEC-TABLES.md §19.1), read BYTEWISE: it is the one field
// read without assuming the order the rest of the block is in. A consumer that
// reads back the byte-swapped value has found a foreign byte order and refuses;
// one that reads back anything else has not found a block at all.
const int tableBlockMagic = 0x4b4c42414d484353;

// This build's byte order, as the prologue carries it: 1 little, 2 big. Dart's
// typed data is read at an EXPLICIT endianness everywhere in this file, so the
// reader has no native order of its own — the constant says what the producer's
// prologue must say, and a block of the other order is refused rather than
// fixed up.
const int tableBlockByteOrder = 1;

// The prologue's first three words, read bytewise so no assumption about order
// rides ahead of the check that establishes it.
int tableBlockRead64(ByteData view, int at) {
  var value = 0;
  for (var i = 7; i >= 0; i--) {
    value = (value << 8) | view.getUint8(at + i);
  }
  return value;
}

// ---- the block form's reflection (docs/SPEC-TABLES.md §8, §19.2) ----
//
// One record's layout as DATA — the whole mechanism behind the block form's
// read side, and what retires a hand-kept mirror. A block-form table's own
// descriptor describes its PROJECTION; the element descriptor of each
// out-of-line array describes that array's ROW, and so on down.

final class TableBlockFieldInfo {
  final String name;
  // the field's offset in the record this descriptor describes, and its size
  final int offset;
  final int size;
  final int kind; // the table-wire kind, as TableFieldInfo carries it
  // an out-of-line array: the triple's three members are live
  final bool outOfLine;
  final int offsetOfOffset; // the triple's offsetOf member, or -1

  // The COUNT COMPANION, one column doing one job in both spellings: the
  // triple's count member for an out-of-line array, the int32 used length of a
  // string or a bytes inline, -1 when the field has none.
  final int countOffset;
  final int strideOffset; // the triple's stride member, or -1
  // THIS BUILD's pitch, to assert against — never to index with (§19.2)
  final int stride;

  // ---- what a GENERIC ROW WALK needs, in the vocabulary TableFieldInfo
  // already uses (docs/SPEC-TABLES.md §8.1), so ONE walker reads a cooked node and
  // a block row without learning a second one. Where the field starts is the
  // pair above; this is everything after it.
  // inline storage of arrayBound slots at elemSize (bytes included)
  final bool isArray;
  final bool counted; // countOffset names a used-length companion
  final bool optional; // presentOffset names a bool presence companion
  // inline slots, or a string's declared maximum; 0 for a plain scalar
  final int arrayBound;
  // ONE slot's size; the field's own when it holds one value
  final int elemSize;
  final int presentOffset; // the presence companion, or -1

  // the ELEMENT's or the nested record's own layout. null when the field is a
  // scalar. Following it is how a walker DESCENDS: an out-of-line array's rows,
  // and a nested record's fields, are both reached through this one column.
  final TableBlockInfo? element;

  const TableBlockFieldInfo({
    required this.name,
    required this.offset,
    required this.size,
    required this.kind,
    required this.outOfLine,
    required this.offsetOfOffset,
    required this.countOffset,
    required this.strideOffset,
    required this.stride,
    required this.isArray,
    required this.counted,
    required this.optional,
    required this.arrayBound,
    required this.elemSize,
    required this.presentOffset,
    required this.element,
  });
}

final class TableBlockInfo {
  final String name;
  final int buildVersion; // the unit's (docs/SPEC-TABLES.md §20)
  final int size; // the record's own size: a projection's, or a row's
  final int align;
  final List<TableBlockFieldInfo> fields;

  const TableBlockInfo({
    required this.name,
    required this.buildVersion,
    required this.size,
    required this.align,
    required this.fields,
  });

  int get numFields => fields.length;
}
`
