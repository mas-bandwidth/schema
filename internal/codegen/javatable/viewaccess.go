package javatable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE WALKABLE COLUMNS of a Java field descriptor (docs/SPEC-TABLES.md §8.1):
// where a field lives, its element size and capacity, its count and presence
// companions, the nested table's descriptor, and — because a Java field has no
// address — the cached accessor pair that reads and writes it. §8.1: "a
// language whose fields have no address carries the pair that reads and writes
// the field instead — a getter and a setter over one element, the object a
// nested value is stored as, the byte buffer a string(N) is, and the counted
// and presence companions." The accessors are lambdas built once in the
// descriptor's class initialization and cached with it, so a walk allocates
// nothing a typed read would not (a boxed list element is the storage itself).
//
// The BYTE columns (offset, elementSize, countOffset, presentOffset) are the
// field's place in the COOKED record (docs/SPEC-TABLES.md §7.2,
// ir.RecordLayout): the one byte layout a Java table has, the one its
// <Name>Row accessors read and the one every other target's descriptor names.
// The accessor columns reach the object storage, <Name>Variable.Value, which is
// the storage the variable form and the text form (§16) load into and save
// from. A declaration that has no Value class (a `type` no table closure
// reaches, §8.2) carries the byte columns and null accessors.

// fieldPlace is a field's cooked-record columns (docs/SPEC-TABLES.md §8.1),
// spelled as the Go leg spells them (internal/codegen/gotable/codecs.go
// emitTableFieldDescriptor): -1 where a companion does not exist.
type fieldPlace struct {
	isArray, counted                                bool
	offset, elementSize, countOffset, presentOffset int64
}

func cookedPlace(u *ir.Unit, st *ir.Struct, f *ir.Field) fieldPlace {
	p := fieldPlace{countOffset: -1, presentOffset: -1}
	fl := ir.RecordLayout(u, st).FieldByName(f.Name)
	if fl == nil {
		return p
	}
	pieces := ir.FieldPieces(u, f, fl.Offset)
	text := !f.Type.Pointer && (f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TString || f.Type.Kind == ir.TWString)
	p.isArray = f.IsMap() || f.Array != ir.ArrayNone || (f.Type.Kind == ir.TBytes && !f.Type.Pointer) || f.KeyEnum != ""
	p.counted = f.Array == ir.ArrayCounted || text
	p.offset = pieces[0].Offset
	p.elementSize = pieces[0].Size
	switch {
	case f.IsMap():
		p.elementSize = ir.RecordLayout(u, f.MapEntry).Size
	case f.Array == ir.ArrayList:
		elem := variableElement(f)
		if ref, ok := f.Type.Ref.(*ir.Struct); ok && !f.Type.Pointer {
			p.elementSize = ir.RecordLayout(u, ref).Size
		} else {
			p.elementSize = ir.FieldPieces(u, elem, 0)[0].Size
		}
	case f.Type.Kind == ir.TBytes && !f.Type.Pointer && f.Array == ir.ArrayNone:
		p.elementSize = 1
	case p.isArray && f.ArrayBound > 0:
		p.elementSize = pieces[0].Size / f.ArrayBound
	}
	if p.counted && len(pieces) > 1 {
		p.countOffset = pieces[1].Offset
	}
	if f.IsMap() || f.Array == ir.ArrayList {
		// a list's and a map's count rides its sixteen-byte slot (§2.8, §2.9)
		p.counted = true
		p.countOffset = p.offset + 8
	}
	if f.Type.Optional {
		p.presentOffset = pieces[len(pieces)-1].Offset
	}
	return p
}

// fieldCapacity is a field's declared bound (docs/SPEC-TABLES.md §8.1, Go's
// ArrayBound): a fixed, bounded or enum-keyed array's capacity, a string's,
// wide string's or byte buffer's maximum length; 0 for a scalar, a blob, an
// unbounded list and a map, which declare none.
func fieldCapacity(f *ir.Field) int64 {
	switch {
	case f.IsMap(), f.Array == ir.ArrayList:
		return 0
	case f.Array != ir.ArrayNone || f.KeyEnum != "":
		return f.ArrayBound
	case f.Type.Pointer:
		return 0
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TWString, f.Type.Kind == ir.TBytes:
		return f.Type.Size
	}
	return 0
}

// fieldLane is how a field's ELEMENT is read and written generically: "long"
// for every integer-valued element (bool as 0/1, an enum's value, a flags
// mask, bits(N), every integer and fixed of at most 64 bits), "double" for a
// float, "object" for everything held by reference (a string, a byte buffer, a
// nested record, a union, a 128-bit value). storage is the Java type the long
// and double lanes cast to on a write.
func fieldLane(ef *ir.Field) (lane, storage string, unsigned bool) {
	if ef.Type.Pointer {
		return "object", "", false
	}
	if variableWide(ef) {
		switch t := variableWideType(ef); t {
		case "int", "long":
			return "long", t, false
		}
		return "object", "", false
	}
	switch ef.Type.Kind {
	case ir.TBool:
		return "long", "boolean", false
	case ir.TInt, ir.TBits:
		if ef.Type.Width <= 32 {
			return "long", "int", ef.Type.Kind == ir.TBits || !ef.Type.Signed
		}
		return "long", "long", false
	case ir.TFloat32:
		return "double", "float", false
	case ir.TFloat64:
		return "double", "double", false
	case ir.TNamed:
		switch ef.Type.Ref.(type) {
		case *ir.Enum:
			return "long", "int", false
		case *ir.Flags:
			return "long", "long", false
		}
	}
	return "object", "", false
}

// accessorColumns is the chained accessor columns of one field of the Value
// class holder (docs/SPEC-TABLES.md §8.1): items (count, resize) for an array,
// the getter and setter of its lane, presence for an optional, and the nested
// table's descriptor.
func accessorColumns(holder string, f *ir.Field) string {
	vf := f
	if f.IsMap() {
		vf = variableStruct(&ir.Struct{Fields: []*ir.Field{f}}).Fields[0]
	}
	m := varMember(vf)
	h := "((" + holder + ") h)." + m
	var b strings.Builder
	if ref, ok := f.Type.Ref.(*ir.Struct); ok && f.Type.Kind == ir.TNamed {
		fmt.Fprintf(&b, ".nested(() -> %sDescriptor.tableType())", ref.Name)
	} else if f.IsMap() {
		fmt.Fprintf(&b, ".nested(() -> %sDescriptor.tableType())", f.MapEntry.Name)
	}
	array := vf.Array != ir.ArrayNone
	ef := vf
	elemType := variableValueType(vf)
	if array {
		ef = variableElement(vf)
		if isStructArray(vf) || isPointerArray(vf) {
			elemType = vf.Type.Name + "Variable.Value"
		} else {
			elemType = variableListElemType(vf)
		}
		resize := fmt.Sprintf("while (v.%s.size() > n) v.%s.remove(v.%s.size() - 1); while (v.%s.size() < n) v.%s.add(%s);",
			m, m, m, m, m, variableDefault(ef))
		if isPointerArray(vf) {
			resize += fmt.Sprintf(" while (v.%sIdx.size() > n) v.%sIdx.remove(v.%sIdx.size() - 1); while (v.%sIdx.size() < n) v.%sIdx.add(0L);",
				m, m, m, m, m)
		}
		if vf.KeyEnum != "" {
			// an enum-keyed array has one slot per variant, always (§2.4)
			fmt.Fprintf(&b, ".items(h -> %s.size(), null)", h)
		} else {
			fmt.Fprintf(&b, ".items(h -> %s.size(), (h, n) -> { %s v = (%s) h; %s })", h, holder, holder, resize)
		}
	}
	read := h
	if array {
		read = h + ".get(i)"
	}
	write := func(x string) string {
		if array {
			return fmt.Sprintf("%s.set(i, %s)", h, x)
		}
		return fmt.Sprintf("%s = %s", h, x)
	}
	switch lane, storage, unsigned := fieldLane(ef); lane {
	case "long":
		get, set := read, write("("+storage+") x")
		switch storage {
		case "boolean":
			get, set = read+" ? 1L : 0L", write("x != 0")
		case "long":
			set = write("x")
		case "int":
			if unsigned {
				get = "Integer.toUnsignedLong(" + read + ")"
			}
		}
		fmt.Fprintf(&b, ".longs((h, i) -> %s, (h, i, x) -> %s)", get, set)
	case "double":
		set := write("(float) x")
		if storage == "double" {
			set = write("x")
		}
		fmt.Fprintf(&b, ".doubles((h, i) -> %s, (h, i, x) -> %s)", read, set)
	default:
		fmt.Fprintf(&b, ".objects((h, i) -> %s, (h, i, x) -> %s)", read, write("("+elemType+") x"))
	}
	if vf.Type.Optional {
		fmt.Fprintf(&b, ".presence(h -> %sPresent, (h, p) -> %sPresent = p)", h, h)
	}
	return b.String()
}

// typeHooks is a Value-backed declaration's create and RESET hooks
// (docs/SPEC-TABLES.md §8.1): reset puts one instance back at its declared
// defaults IN PLACE, one member at a time — a by-value record through its own
// descriptor's reset, an array emptied (an enum-keyed one refilled to one
// default slot per variant) — so its cost grows with the declarations, not
// with the bytes.
func typeHooks(st *ir.Struct) string {
	holder := st.Name + "Variable.Value"
	var b strings.Builder
	fmt.Fprintf(&b, "        t.create = %s::new;\n", holder)
	fmt.Fprintf(&b, "        t.reset = h -> {\n            %s v = (%s) h;\n", holder, holder)
	for _, f := range variableStruct(st).Fields {
		m := varMember(f)
		switch {
		case f.KeyEnum != "":
			fmt.Fprintf(&b, "            v.%s.clear(); for (int i = 0; i < %d; i++) v.%s.add(%s);\n", m, f.ArrayBound, m, variableDefault(variableElement(f)))
			if f.Type.Pointer {
				fmt.Fprintf(&b, "            v.%sIdx.clear(); for (int i = 0; i < %d; i++) v.%sIdx.add(0L);\n", m, f.ArrayBound, m)
			}
		case f.Array != ir.ArrayNone:
			fmt.Fprintf(&b, "            v.%s.clear();\n", m)
			if isPointerArray(f) {
				fmt.Fprintf(&b, "            v.%sIdx.clear();\n", m)
			}
		case isStruct(f) && !f.Type.Pointer:
			fmt.Fprintf(&b, "            %sDescriptor.tableType().reset.accept(v.%s);\n", f.Type.Name, m)
		default:
			fmt.Fprintf(&b, "            v.%s = %s;\n", m, variableDefault(f))
			if f.Type.Blob() || (f.Type.Pointer && isStruct(f)) {
				fmt.Fprintf(&b, "            v.%sIdx = 0L;\n", m)
			}
		}
		if f.Type.Optional {
			fmt.Fprintf(&b, "            v.%sPresent = false;\n", m)
		}
	}
	b.WriteString("        };\n")
	return b.String()
}
