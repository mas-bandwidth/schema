// The unit's shared Dart COOK runtime (docs/SPEC-TABLES.md §7): the magic, this
// build's byte order, the reference answers, and the reflection descriptor
// classes every root's own descriptors are built from.
//
// Emitted once per unit into <Package>Cook.dart, which every other
// <Base>Cook.dart of the unit imports. The BUILD VERSION rides in the BLOCK
// runtime home when the unit has one; a unit whose tables all have unions has
// no block form to carry it, so it is emitted here instead — exactly one
// accelerator defines it (docs/SPEC-TABLES.md §20.7).
package darttable

import "fmt"

func cookRuntimeSource(buildVersion uint64, withBuildVersion bool) string {
	s := ""
	if withBuildVersion {
		s = fmt.Sprintf(cookBuildVersionTemplate, buildVersion)
	}
	return s + cookRuntimeCore
}

const cookBuildVersionTemplate = `
// THE BUILD VERSION (docs/SPEC-TABLES.md §20): one digest over every fact the bytes
// this build produces depend on. It rides here because this unit has no block
// form to carry it — exactly one accelerator defines it (§20.7).
const int tableBuildVersion = 0x%016x;
`

const cookRuntimeCore = `
// The cooked file's magic (docs/SPEC-TABLES.md §7.1), read BYTEWISE: it is what
// establishes the byte order every other header word is written in. A cook of
// the other order reads back this constant byte-reversed and refuses there,
// rather than reaching a fix-up pass this design does not have.
const int tableCookMagic = 0x4b4f4f434d484353;

// This build's byte order, as the header's own word records it: 1 little,
// 2 big.
const int tableCookByteOrder = 1;

// The ceiling on the header's alignment word (§7): the same sixty-four a
// block's base takes, past which the derived data offset would no longer be the
// 64 every unit this language can declare produces.
const int tableCookMaxAlign = 64;

// The header's words, read bytewise so no assumption about order rides ahead of
// the check that establishes it.
int tableCookRead64(ByteData view, int at) {
  var value = 0;
  for (var i = 7; i >= 0; i--) {
    value = (value << 8) | view.getUint8(at + i);
  }
  return value;
}

// What a deref answers when it is not an offset (§6.3). NULL IS A DELTA OF
// ZERO, and outside is Dart's own addition: C++ needs no bound because a
// pointer past the region is the caller's problem at the dereference, and in
// Dart the read after it would be an escaping RangeError — which is not a
// refusal.
abstract final class TableCookRef {
  static const int none = -1;
  static const int outside = -2;
}

// What a cooked SLOT holds, which is not always what the WIRE carries: an ENUM
// slot holds the ORDINAL at the enum's own derived storage width (§7.2), where
// the wire carries the variant-name hash. So the descriptors name the storage
// rather than reuse a wire kind, and a walker reads a slot with the width
// elemSize gives and the signedness this names.
abstract final class TableCookStorage {
  static const int signed = 0;
  static const int unsigned = 1;
  static const int boolean = 2;
  static const int float = 3;
  static const int string = 4;
  static const int bytes = 5;
  static const int record = 6;
  static const int reference = 7;
}

// ---- the cook's reflection (docs/SPEC-TABLES.md §7.5) ----

final class TableCookFieldInfo {
  final String name;
  // where the field's storage begins in the record, and how much of it there is
  final int offset;
  final int size;
  // ONE slot's size; the field's own when it holds one value
  final int elemSize;
  // arrayBound slots at elemSize, laid end to end from offset
  final bool isArray;
  // the slots, or a string's or a bytes' declared maximum; 1 for a scalar
  final int arrayBound;
  // an eight-byte SIGNED self-relative reference slot (§6.3)
  final bool isPointer;
  // the int32 used-length companion, or -1
  final int countOffset;
  // the bool presence companion of a ?T, or -1
  final int presentOffset;
  final int storage; // one of TableCookStorage's
  // the record this field names — a by-value nesting, an array of them, or the
  // target of a reference. null when the field is a scalar. Following it is how
  // a walker DESCENDS.
  //
  // It is a FUNCTION and not the descriptor itself, and the cycle it breaks is
  // in the DESCRIPTOR GRAPH rather than in any region: a record's field column
  // can name its OWN record — ListNode.next names ListNode — so a value here
  // would be a constant that depends on itself. A tear-off of a static method
  // is still a compile-time constant, so the graph stays const and the cycle
  // costs one call at the edge.
  //
  // A COOKED REGION IS NEVER CYCLIC. schema cook refuses a cyclic wire by
  // name (docs/SPEC-TABLES.md §3.1, §6.2), so the nodes a walk meets form a DAG;
  // it is the types that point at themselves, not the nodes.
  final TableCookInfo Function()? record;

  const TableCookFieldInfo({
    required this.name,
    required this.offset,
    required this.size,
    required this.elemSize,
    required this.isArray,
    required this.arrayBound,
    required this.isPointer,
    required this.countOffset,
    required this.presentOffset,
    required this.storage,
    required this.record,
  });

  // the record this field names, resolved.
  TableCookInfo? get info => record == null ? null : record!();
}

final class TableCookInfo {
  final String name;
  final int size;
  final int align;
  final List<TableCookFieldInfo> fields;

  const TableCookInfo({
    required this.name,
    required this.size,
    required this.align,
    required this.fields,
  });

  int get numFields => fields.length;
}
`
