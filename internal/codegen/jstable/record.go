// The BLITTABLE RECORD, read side, in JavaScript — the one spelling both
// accelerators share (docs/SPEC-TABLES.md §7, §19.3).
//
// A cooked record IS the blittable row, so the two accelerators are laid out
// from ONE model and this file emits ONE object per record: `<Name>Row`, a
// frozen object of GENERATED ACCESSORS that read each field at the offset the
// compiler settled, out of a DataView the caller already holds.
//
// THERE IS NO STRUCT HERE, and that is the reading tier's whole shape. C++
// declares the record and C# mirrors it with generated padding fields; a
// JavaScript object has no layout to declare, so the offsets ARE the record
// and every read names one. `size` and `align` are carried beside them so a
// consumer, and the layout check, can say what a row costs without a sizeof.
//
// EVERY MULTI-BYTE READ NAMES ITS BYTE ORDER — the `true` at each DataView
// call — so this side reads little-endian because the form is little-endian,
// never because the host is. A file of the other order is refused by its
// magic, at Open, before one field is read.
package jstable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// recordName is the module-scope spelling of one record's accessor object.
// `Row` is a CLAIMED suffix (docs/SPEC-TABLES.md §11), so no declaration in the
// unit can take it.
func recordName(name string) string { return name + "Row" }

// jsF32Reader and jsF64Reader are the ONE module-level float read per width
// every float accessor of a unit calls: the block home defines them
// (blockRuntime), or the cook home where the unit has no block form
// (cookRuntime), and every other module that reads a float imports them from
// there. jsNonFiniteFloat is the latch's query beside them.
const (
	jsF32Reader      = "TableBlockReadF32"
	jsF64Reader      = "TableBlockReadF64"
	jsNonFiniteFloat = "TableBlockNonFiniteFloat"
)

// jsFloatReaders are the runtime float symbols a module imports from its home
// when its own accessors call them.
var jsFloatReaders = []string{jsF32Reader, jsF64Reader}

// jsF32ReaderSource is the readers' text, emitted ONCE per unit.
//
// A TABLE FLOAT IS FINITE (docs/SPEC-TABLES.md §3.3; Glenn, 2026-10-27,
// "tables go finite-only too, same rule as packets"). A block or cook open is
// O(1) and walks no field (§7, §13.4), so it is THE ACCESSOR that meets a
// non-finite float, and it refuses one: the raw bits are tested before they
// become a number, the field's <Owner>.<field> name is LATCHED ON THE VIEW
// (the first name wins) and the accessor answers +0, never the value. The
// latch is a WeakMap keyed by the DataView, so a finite read touches no
// property of the view, changes no shape and allocates nothing; only the
// refusal writes it. TableBlockNonFiniteFloat(view) answers the latched name,
// "" while every float the view's accessors read was finite.
//
// The readers were once an arrow built and run at EVERY read, and a closure
// created per call is a heap allocation: 246 bytes per iteration on the block
// walk, against docs/PORTING.md M1 (the read path allocates nothing) and the
// gate I9 holds it to (`make tables-js-alloc`). As module-level functions they
// allocate nothing. The double comes back out of the call, which is the
// per-language allowance JavaScript takes here for performance (docs/PORTING.md
// M3; Glenn, 2026-10-15), and the allocation gate proves the cost.
func jsF32ReaderSource() string {
	return `// THE FLOAT READS for every float accessor of the unit (docs/SPEC-TABLES.md
// §3.3; docs/PORTING.md M1, M3): the raw bits are tested in place, a NaN or an
// infinity is refused as non_finite_float and LATCHED ON THE VIEW with the
// field named, and the accessor answers +0 for it, never the value. A finite
// read allocates nothing: the refusal is a statement and the +0 a literal, so
// the reader's answer stays an unboxed double (a call's answer in that branch
// boxed every finite read, 44 bytes a walk on make tables-js-alloc).
// Module-scope bindings, claimed in internal/tablenames.
const TableBlockLatch = new WeakMap();
function TableBlockRefuseFloat(view, field) {
  if (!TableBlockLatch.has(view)) { TableBlockLatch.set(view, field); }
}
// the <Owner>.<field> of the first non-finite float an accessor met over view,
// "" when there was none
export function ` + jsNonFiniteFloat + `(view) {
  const field = TableBlockLatch.get(view);
  return field === undefined ? "" : field;
}
export function ` + jsF32Reader + `(view, at, field) {
  if ((view.getUint32(at, true) & 0x7f800000) === 0x7f800000) { TableBlockRefuseFloat(view, field); return 0; }
  return view.getFloat32(at, true);
}
export function ` + jsF64Reader + `(view, at, field) {
  if ((view.getUint32(at + 4, true) & 0x7ff00000) === 0x7ff00000) { TableBlockRefuseFloat(view, field); return 0; }
  return view.getFloat64(at, true);
}

`
}

// jsScalarRead renders the DataView call that reads one value of a record
// field, by the WIRE KIND the descriptors carry and the STORAGE width the
// layout model gives it — the same pair the canonical row dump reads with, so
// the generated accessor and a reflective walk cannot disagree about a byte.
//
// A float reads through the unit's module-level reader for its width
// (jsF32ReaderSource), which refuses a non-finite one and latches `field`, the
// <Owner>.<field> name, on the view; nothing is built at the call site.
func jsScalarRead(kind int, elemSize int64, at, field string) string {
	switch kind {
	case tkBool:
		return fmt.Sprintf("view.getUint8(%s) !== 0", at)
	case tkF32:
		return fmt.Sprintf("%s(view, %s, %s)", jsF32Reader, at, jsStr(field))
	case tkF64:
		return fmt.Sprintf("%s(view, %s, %s)", jsF64Reader, at, jsStr(field))
	case tkI8, tkI16, tkI32, tkI64:
		switch elemSize {
		case 1:
			return fmt.Sprintf("view.getInt8(%s)", at)
		case 2:
			return fmt.Sprintf("view.getInt16(%s, true)", at)
		case 4:
			return fmt.Sprintf("view.getInt32(%s, true)", at)
		default:
			return fmt.Sprintf("view.getBigInt64(%s, true)", at)
		}
	}
	switch elemSize {
	case 1:
		return fmt.Sprintf("view.getUint8(%s)", at)
	case 2:
		return fmt.Sprintf("view.getUint16(%s, true)", at)
	case 4:
		return fmt.Sprintf("view.getUint32(%s, true)", at)
	default:
		return fmt.Sprintf("view.getBigUint64(%s, true)", at)
	}
}

// isFlagsField reports a field of a `flags` type: eight bytes of mask in every
// record, read as a BigInt by its value accessor and tested bit by bit,
// without one, by the `<Member>Has` accessor beside it.
func isFlagsField(f *ir.Field) bool {
	if f.Type.Kind != ir.TNamed {
		return false
	}
	_, isFlags := f.Type.Ref.(*ir.Flags)
	return isFlags
}

// jsFlagBitRead renders a `flags` mask's ONE-BIT test over the record bytes at
// `at`, as a Number expression that allocates nothing: the bit is the
// variant's ordinal — the currency FlagName<Flags>(bit) takes — and the test
// reads the one 32-bit word that holds it. A BigInt read of the whole mask
// would be one allocation per row on the hottest path the block form has.
func jsFlagBitRead(at string) string {
	return fmt.Sprintf("bit < 32 ? ((view.getUint32(%s, true) >>> bit) & 1) === 1 : ((view.getUint32((%s) + 4, true) >>> (bit - 32)) & 1) === 1", at, at)
}

// recordRefs is what one record's accessors reference from elsewhere in the
// unit — the element records it descends into.
type recordRefs map[string]bool

// emitRecordObject writes one record's accessor object into b, and records
// every other record it names in refs.
//
// The accessors are the READING TIER's whole surface, and their signatures are
// uniform on purpose: a value accessor takes (view, at[, index]) and answers
// the value; a nested record's takes (at[, index]) and answers the BYTE OFFSET
// of that record, which is what its own accessors then take; a string's or a
// bytes' takes (bytes, at) and answers a VIEW over the used bytes, which
// copies nothing.
//
// `fields` beside them is the same set reached by the field's schema name, in
// one uniform shape — (bytes, view, at, index) — so a generic reader and the
// named accessors can be held to each other by a test rather than by reading.
func emitRecordObject(b *strings.Builder, u *ir.Unit, name string, ml *ir.MemberLayout, refs recordRefs) {
	pf := func(format string, args ...any) { fmt.Fprintf(b, format, args...) }
	pf("// %s — one record, read where it lies. `Row` is a CLAIMED suffix\n", name)
	pf("// (docs/SPEC-TABLES.md §11), so no declaration in the unit can take it.\n")
	pf("export const %s = (() => {\n", recordName(name))
	pf("  const Size = %d;\n", ml.Size)
	pf("  const Align = %d;\n\n", ml.Align)
	var uniform []string
	for _, fl := range ml.Fields {
		f := fl.Field
		member := ir.GoExportName(f.Name)
		facts := ir.BlockFieldOf(u, f, fl.Offset, false)
		pieces := ir.FieldPieces(u, f, fl.Offset)
		kind := tableScalarKind(f)
		if f.Type.Kind == ir.TBytes {
			kind = tkU8
		}
		if f.IsList() || f.IsMap() {
			emitRowHolder(pf, u, name, f, pieces[0].Offset)
			uniform = append(uniform, fmt.Sprintf("%q: (bytes, view, at) => %sDelta(view, at)", f.Name, member))
			continue
		}
		switch {
		case f.Type.Pointer:
			// A *T SLOT IS EIGHT BYTES (docs/SPEC-TABLES.md §6.3, §7.2), holding
			// the SIGNED SELF-RELATIVE delta from the slot's own address, and
			// NULL IS ZERO. The slot's OFFSET is the fact a deref needs, so it
			// is what the accessor hands back beside the delta.
			pf("  function %sSlot(at) { return at + %d; }\n", member, fl.Offset)
			pf("  function %sDelta(view, at) { return view.getBigInt64(at + %d, true); }\n", member, fl.Offset)
			uniform = append(uniform, fmt.Sprintf("%q: (bytes, view, at) => %sDelta(view, at)", f.Name, member))
		case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
			pf("  function %sLength(view, at) { return view.getInt32(at + %d, true); }\n", member, pieces[1].Offset)
			pf("  function %s(bytes, view, at) {\n", member)
			pf("    let used = %sLength(view, at);\n", member)
			pf("    if (!(used >= 0) || used > %d) { used = 0; } // a companion outside its bound is cook-check's refusal, not a read\n", facts.ArrayBound)
			pf("    return bytes.subarray(at + %d, at + %d + used); // a VIEW over the region: no copy\n", fl.Offset, fl.Offset)
			pf("  }\n")
			uniform = append(uniform, fmt.Sprintf("%q: (bytes, view, at) => %s(bytes, view, at)", f.Name, member))
		case isClassRef(f.Type) && f.Type.Kind == ir.TNamed:
			if ref, ok := f.Type.Ref.(*ir.Struct); ok {
				refs[ref.Name] = true
			}
			elem := facts.ElemSize
			if elem == 0 {
				elem = fl.Size
			}
			pf("  function %sAt(at, i = 0) { return at + %d + i * %d; }\n", member, fl.Offset, elem)
			uniform = append(uniform, fmt.Sprintf("%q: (bytes, view, at, i) => %sAt(at, i)", f.Name, member))
		default:
			elem := facts.ElemSize
			if elem == 0 {
				elem = fl.Size
			}
			pf("  function %s(view, at, i = 0) { return %s; }\n", member,
				jsScalarRead(kind, elem, fmt.Sprintf("at + %d + i * %d", fl.Offset, elem), name+"."+f.Name))
			if isFlagsField(f) {
				// the mask's accessor answers a BigInt, one allocation per read;
				// this one answers whether ONE bit is set and allocates nothing
				pf("  function %sHas(view, at, bit, i = 0) { return %s; }\n", member,
					jsFlagBitRead(fmt.Sprintf("at + %d + i * %d", fl.Offset, elem)))
			}
			uniform = append(uniform, fmt.Sprintf("%q: (bytes, view, at, i) => %s(view, at, i)", f.Name, member))
		}
		if facts.Counted && f.Type.Kind != ir.TString && f.Type.Kind != ir.TBytes {
			pf("  function %sCount(view, at) { return view.getInt32(at + %d, true); }\n", member, facts.CountOffset)
		}
		if facts.Optional {
			pf("  function %sPresent(view, at) { return view.getUint8(at + %d) !== 0; }\n", member, facts.PresentOffset)
		}
	}
	pf("\n  return Object.freeze({\n")
	pf("    Size, Align,\n")
	pf("    // the same accessors by the field's SCHEMA name, in one uniform\n")
	pf("    // shape — (bytes, view, at, index) — so a generic reader and the\n")
	pf("    // named accessors above can be held to each other by a test.\n")
	pf("    Fields: Object.freeze({ %s }),\n", strings.Join(uniform, ", "))
	pf("    %s\n", strings.Join(recordMembers(u, ml), ", "))
	pf("  });\n})();\n\n")
}

// recordMembers names every accessor one record's object re-exports.
func recordMembers(u *ir.Unit, ml *ir.MemberLayout) []string {
	var out []string
	for _, fl := range ml.Fields {
		f := fl.Field
		member := ir.GoExportName(f.Name)
		facts := ir.BlockFieldOf(u, f, fl.Offset, false)
		if f.IsList() || f.IsMap() {
			out = append(out, member+"Slot", member+"Delta", member+"Count", member+"Stride", member+"At")
			if rowFindKey(f) != nil {
				out = append(out, member+"Find")
			}
			continue
		}
		switch {
		case f.Type.Pointer:
			out = append(out, member+"Slot", member+"Delta")
		case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
			out = append(out, member, member+"Length")
		case isClassRef(f.Type) && f.Type.Kind == ir.TNamed:
			out = append(out, member+"At")
		default:
			out = append(out, member)
			if isFlagsField(f) {
				out = append(out, member+"Has")
			}
		}
		if facts.Counted && f.Type.Kind != ir.TString && f.Type.Kind != ir.TBytes {
			out = append(out, member+"Count")
		}
		if facts.Optional {
			out = append(out, member+"Present")
		}
	}
	return out
}

// emitRowHolder is a LIST's or a MAP's sixteen-byte holder (docs/SPEC-TABLES.md
// §2.8, §2.9, §7.2; docs/PORTING.md M19): the slot holds the SIGNED
// SELF-RELATIVE delta to the element array in the holder's node extent (zero
// for an empty one) and the int32 count beside it. `<F>At(view, at, i)` is the
// i-th element's offset IN PLACE: the slot's own offset plus the delta plus i
// strides, one add and one multiply, with nothing parsed and nothing copied.
// The delta is read as its two 32-bit halves, so the span allocates nothing
// (a cook's references stay below 2^53, the region's own bound). A MAP adds
// `<F>Find`, a binary search over the ascending entries where they lie.
func emitRowHolder(pf func(string, ...any), u *ir.Unit, owner string, f *ir.Field, slot int64) {
	member := ir.GoExportName(f.Name)
	var stride int64
	if f.IsMap() {
		stride = ir.RecordLayout(u, f.MapEntry).Size
	} else {
		stride, _ = ir.ListElementLayout(u, f)
	}
	pf("  // %s: the holder's slot, its delta and its count; %sAt is element i where it lies (§2.8, §2.9)\n",
		ir.FieldTypeSpelling(f), member)
	pf("  function %sSlot(at) { return at + %d; }\n", member, slot)
	pf("  function %sDelta(view, at) { return view.getBigInt64(at + %d, true); }\n", member, slot)
	pf("  function %sCount(view, at) { const n = view.getInt32(at + %d, true); return n < 0 ? 0 : n; }\n", member, slot+8)
	pf("  const %sStride = %d;\n", member, stride)
	pf("  function %sAt(view, at, i = 0) {\n", member)
	pf("    return at + %d + view.getInt32(at + %d, true) * 4294967296 + view.getUint32(at + %d, true) + i * %d;\n",
		slot, slot+4, slot, stride)
	pf("  }\n")
	key := rowFindKey(f)
	if key == nil {
		return
	}
	entry := ir.RecordLayout(u, f.MapEntry)
	var keyAt int64
	for _, efl := range entry.Fields {
		if efl.Field == key {
			keyAt = efl.Offset
		}
	}
	pieces := ir.FieldPieces(u, key, keyAt)
	pf("  // the entry whose key is `key`, where the entries lie, or -1: a binary search over\n")
	pf("  // the ascending array (§2.8), floor(log2 n) + 1 key compares and no allocation.\n")
	if key.Type.Kind == ir.TString {
		pf("  // `key` is the key's UTF-8 bytes, ordered as bytes, unsigned, a prefix first.\n")
		pf("  function %sFind(bytes, view, at, key) {\n", member)
		pf("    const base = %sAt(view, at, 0);\n", member)
		pf("    let low = 0, high = %sCount(view, at);\n", member)
		pf("    while (low < high) {\n")
		pf("      const mid = (low + high) >>> 1;\n")
		pf("      const e = base + mid * %d;\n", stride)
		pf("      let used = view.getInt32(e + %d, true);\n", pieces[1].Offset)
		pf("      if (!(used >= 0) || used > %d) { used = 0; }\n", key.Type.Size)
		pf("      const m = used < key.length ? used : key.length;\n")
		pf("      let order = 0;\n")
		pf("      for (let i = 0; i < m && order === 0; i++) { order = bytes[e + %d + i] - key[i]; }\n", pieces[0].Offset)
		pf("      if (order === 0) { order = used - key.length; }\n")
		pf("      if (order === 0) { return e; }\n")
		pf("      if (order < 0) { low = mid + 1; } else { high = mid; }\n")
		pf("    }\n")
		pf("    return -1;\n")
		pf("  }\n")
		return
	}
	width := pieces[0].Size
	want := "Number(key)"
	if width == 8 {
		want = `typeof key === "bigint" ? key : BigInt(key)`
	}
	pf("  // `key` is the integer, ordered by value (signed for a signed kind).\n")
	pf("  function %sFind(view, at, key) {\n", member)
	pf("    const want = %s;\n", want)
	pf("    const base = %sAt(view, at, 0);\n", member)
	pf("    let low = 0, high = %sCount(view, at);\n", member)
	pf("    while (low < high) {\n")
	pf("      const mid = (low + high) >>> 1;\n")
	pf("      const e = base + mid * %d;\n", stride)
	pf("      const k = %s;\n", jsScalarRead(tableScalarKind(key), width, fmt.Sprintf("e + %d", pieces[0].Offset), owner+"."+f.Name))
	pf("      if (k === want) { return e; }\n")
	pf("      if (k < want) { low = mid + 1; } else { high = mid; }\n")
	pf("    }\n")
	pf("    return -1;\n")
	pf("  }\n")
}

// rowFindKey is a map's key when the cooked entry array can be searched in
// place: a bounded string(N), compared as bytes, or an integer up to 64 bits.
// A list has none.
func rowFindKey(f *ir.Field) *ir.Field {
	if !f.IsMap() {
		return nil
	}
	key := ir.MapKeyField(f)
	switch {
	case key.Type.Kind == ir.TString && !key.Type.Pointer && !key.Type.Unbounded && key.Type.Size > 0:
		return key
	case key.Type.Kind == ir.TInt && key.Type.Width <= 64:
		return key
	}
	return nil
}
