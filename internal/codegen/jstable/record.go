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

// jsScalarRead renders the DataView call that reads one value of a record
// field, by the WIRE KIND the descriptors carry and the STORAGE width the
// layout model gives it — the same pair the canonical row dump reads with, so
// the generated accessor and a reflective walk cannot disagree about a byte.
func jsScalarRead(kind int, elemSize int64, at string) string {
	switch kind {
	case tkBool:
		return fmt.Sprintf("view.getUint8(%s) !== 0", at)
	case tkF32:
		return fmt.Sprintf("view.getFloat32(%s, true)", at)
	case tkF64:
		return fmt.Sprintf("view.getFloat64(%s, true)", at)
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
		switch {
		case f.Type.Pointer:
			// A *T SLOT IS EIGHT BYTES (docs/SPEC-TABLES.md §6.3, §7.2), holding
			// the SIGNED SELF-RELATIVE delta from the slot's own address, and
			// NULL IS ZERO. The slot's OFFSET is the fact a deref needs, so it
			// is what the accessor hands back beside the delta.
			pf("  function %sSlot(at) { return at + %d; }\n", member, fl.Offset)
			pf("  function %sDelta(view, at) { return view.getBigInt64(at + %d, true); }\n", member, fl.Offset)
			uniform = append(uniform, fmt.Sprintf("%q: (bytes, view, at, i) => %sDelta(view, at)", f.Name, member))
		case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
			pf("  function %sLength(view, at) { return view.getInt32(at + %d, true); }\n", member, pieces[1].Offset)
			pf("  function %s(bytes, view, at) {\n", member)
			pf("    let used = %sLength(view, at);\n", member)
			pf("    if (!(used >= 0) || used > %d) { used = 0; } // a companion outside its bound is cook-check's refusal, not a read\n", facts.ArrayBound)
			pf("    return bytes.subarray(at + %d, at + %d + used); // a VIEW over the region: no copy\n", fl.Offset, fl.Offset)
			pf("  }\n")
			uniform = append(uniform, fmt.Sprintf("%q: (bytes, view, at, i) => %s(bytes, view, at)", f.Name, member))
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
				jsScalarRead(kind, elem, fmt.Sprintf("at + %d + i * %d", fl.Offset, elem)))
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
		switch {
		case f.Type.Pointer:
			out = append(out, member+"Slot", member+"Delta")
		case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
			out = append(out, member, member+"Length")
		case isClassRef(f.Type) && f.Type.Kind == ir.TNamed:
			out = append(out, member+"At")
		default:
			out = append(out, member)
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
