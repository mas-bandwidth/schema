// Table-wire emission for Go (notes/table-wire.md): <Base>Table.go per unit
// file plus TableRuntime.go — TableWrite/TableRead per `type`, plain byte
// code with no serialize dependency. Field identity is the name-hash id;
// readers prefill declared defaults then overlay, skip unknown ids, skip
// kind mismatches, clamp out-of-range values, and count every event. The
// bytes are pinned against the C++ writer by the cross-language goldens.
//
// This is what lets the Go backend open the same Config.bin/Assets.bin the
// game server reads — one file, native typed readers in every language.
//
// Every closure type also carries reflection: TableTypeX() returns a static
// field descriptor (name, wire id/kind, bounds, ranges, enum names, branch
// guards — the C++ TableType<X>() surface), and where C++ exposes storage
// offsets, Go emits typed accessors instead: TableGetX/TableSetX read and
// write fields by name with the read side's exact clamping. This is what
// runtime config editors and generic tooling bind against.
package golang

import (
	"fmt"
	"go/format"
	"math/big"
	"strings"

	"github.com/mas-bandwidth/schema/internal/ir"
	"github.com/mas-bandwidth/schema/internal/pack"
)

// table-wire kinds — mirror internal/pack/table.go
const (
	tkBool   = 1
	tkI8     = 2
	tkI16    = 3
	tkI32    = 4
	tkI64    = 5
	tkU8     = 6
	tkU16    = 7
	tkU32    = 8
	tkU64    = 9
	tkF32    = 10
	tkF64    = 11
	tkString = 12
	tkTable  = 13
	tkArray  = 14
)

func tableScalarKind(f *ir.Field) int {
	switch f.Type.Kind {
	case ir.TBool:
		return tkBool
	case ir.TInt:
		if f.Type.Signed {
			switch f.Type.Width {
			case 8:
				return tkI8
			case 16:
				return tkI16
			case 32:
				return tkI32
			default:
				return tkI64
			}
		}
		switch f.Type.Width {
		case 8:
			return tkU8
		case 16:
			return tkU16
		case 32:
			return tkU32
		default:
			return tkU64
		}
	case ir.TBits:
		switch {
		case f.Type.Width <= 8:
			return tkU8
		case f.Type.Width <= 16:
			return tkU16
		case f.Type.Width <= 32:
			return tkU32
		default:
			return tkU64
		}
	case ir.TFloat32:
		return tkF32
	case ir.TFloat64:
		return tkF64
	case ir.TString:
		return tkString
	case ir.TBytes:
		return tkArray
	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Enum:
			switch ref.StorageBits {
			case 8:
				return tkU8
			case 16:
				return tkU16
			case 32:
				return tkU32
			default:
				return tkU64
			}
		case *ir.Flags:
			return tkU64
		case *ir.Struct:
			return tkTable
		}
	}
	return 0
}

func tableKindWidth(kind int) int {
	switch kind {
	case tkBool, tkI8, tkU8:
		return 1
	case tkI16, tkU16:
		return 2
	case tkI32, tkU32, tkF32:
		return 4
	case tkI64, tkU64, tkF64:
		return 8
	}
	return 0
}

// tableWireInt is the unsigned wire-width integer type a kind travels as.
func tableWireInt(kind int) string {
	return fmt.Sprintf("uint%d", tableKindWidth(kind)*8)
}

// tableStorageInt is the generated storage type a scalar field decodes into.
func tableStorageInt(f *ir.Field, kind int) string {
	switch f.Type.Kind {
	case ir.TInt:
		return goInt2(f.Type.Signed, f.Type.Width)
	case ir.TBits:
		if f.Type.Width <= 32 {
			return "uint32"
		}
		return "uint64"
	case ir.TNamed:
		return f.Type.Name
	}
	if kind >= tkI8 && kind <= tkI64 {
		return fmt.Sprintf("int%d", tableKindWidth(kind)*8)
	}
	return tableWireInt(kind)
}

type tableGen struct {
	unit      *ir.Unit
	file      *ir.File
	body      strings.Builder
	needsMath bool
	indent    string // extra per-line indent while emitting inside a branch guard
}

func (g *tableGen) pf(format string, args ...any) {
	s := fmt.Sprintf(format, args...)
	if g.indent != "" && s != "" {
		trailing := strings.HasSuffix(s, "\n")
		if trailing {
			s = s[:len(s)-1]
		}
		s = g.indent + strings.ReplaceAll(s, "\n", "\n"+g.indent)
		if trailing {
			s += "\n"
		}
	}
	g.body.WriteString(s)
}

// tableGuardExprs composes each guarded field's branch condition from the
// wire tree so the writer keeps untaken-branch fields off the wire.
func tableGuardExprs(st *ir.Struct) map[string]string {
	guards := map[string]string{}
	var walk func(items []ir.Item, cond string)
	walk = func(items []ir.Item, cond string) {
		for _, item := range items {
			switch item := item.(type) {
			case *ir.FieldItem:
				if cond != "" {
					guards[item.F.Name] = cond
				}
			case *ir.Branch:
				name := ir.GoExportName(item.Cond)
				pos, neg := "value."+name, "!value."+name
				if item.Neg {
					pos, neg = neg, pos
				}
				and := func(a, b string) string {
					if a == "" {
						return b
					}
					return a + " && " + b
				}
				walk(item.Then, and(cond, pos))
				walk(item.Else, and(cond, neg))
			}
		}
	}
	walk(st.Items, "")
	return guards
}

// structHasDefaults mirrors gen.hasDefaults: whether New<X> exists.
func structHasDefaults(st *ir.Struct) bool {
	seen := map[string]bool{}
	var walk func(st *ir.Struct) bool
	walk = func(st *ir.Struct) bool {
		if seen[st.Name] {
			return false
		}
		seen[st.Name] = true
		for _, f := range st.Fields {
			if f.HasDefault {
				return true
			}
			if f.Type.Kind == ir.TNamed {
				if inner, ok := f.Type.Ref.(*ir.Struct); ok && walk(inner) {
					return true
				}
			}
		}
		return false
	}
	return walk(st)
}

// tableDefaultExpr renders the Go expression a scalar field's default
// compares against on the write side — identical values to New<X>/zero.
func tableDefaultExpr(f *ir.Field) string {
	switch f.Type.Kind {
	case ir.TBool:
		if f.HasDefault && f.DefBool {
			return "true"
		}
		return "false"
	case ir.TFloat32, ir.TFloat64:
		if f.HasDefault {
			return formatFloat(f.DefFloat)
		}
		return "0"
	case ir.TInt, ir.TBits:
		if f.HasDefault && f.DefInt != nil {
			return f.DefInt.String()
		}
		return "0"
	case ir.TNamed:
		switch f.Type.Ref.(type) {
		case *ir.Enum:
			if f.HasDefault && f.DefVariant != "" {
				return f.Type.Name + f.DefVariant
			}
			return f.Type.Name + "None"
		case *ir.Flags:
			return "0"
		}
	}
	return "0"
}

func (g *tableGen) emitTableWrite(st *ir.Struct) {
	g.pf("// TableWrite%s appends value's table-wire encoding and returns the buffer.\n", st.Name)
	g.pf("func TableWrite%s(value *%s) []byte {\n", st.Name, st.Name)
	g.pf("\tw := &tableWriter{}\n")
	g.pf("\ttableWrite%s(w, value)\n", st.Name)
	g.pf("\treturn w.buf\n}\n\n")

	g.pf("func tableWrite%s(w *tableWriter, value *%s) {\n", st.Name, st.Name)
	guards := tableGuardExprs(st)
	for _, f := range st.Fields {
		if cond, guarded := guards[f.Name]; guarded {
			g.pf("\tif %s {\n", cond)
			g.indent = "\t"
			g.emitTableWriteField(f)
			g.indent = ""
			g.pf("\t}\n")
			continue
		}
		g.emitTableWriteField(f)
	}
	g.pf("\tw.u16(0) // terminator\n")
	g.pf("}\n\n")
}

func (g *tableGen) emitTableWriteField(f *ir.Field) {
	id := pack.FieldId(f.Name)
	kind := tableScalarKind(f)
	name := ir.GoExportName(f.Name)
	switch {
	case f.Type.Kind == ir.TString:
		g.pf("\tif value.%sLength > 0 {\n", name)
		g.pf("\t\tw.u16(0x%04x) // %s\n", id, f.Name)
		g.pf("\t\tw.u8(%d)\n", tkString)
		g.pf("\t\tw.u16(uint16(value.%sLength))\n", name)
		g.pf("\t\tw.raw(value.%s[:value.%sLength])\n\t}\n", name, name)
	case f.Type.Kind == ir.TBytes:
		g.pf("\tif value.%sLength > 0 {\n", name)
		g.pf("\t\tw.u16(0x%04x) // %s\n", id, f.Name)
		g.pf("\t\tw.u8(%d)\n", tkArray)
		g.pf("\t\tw.u32(uint32(3 + value.%sLength))\n", name)
		g.pf("\t\tw.u8(%d)\n", tkU8)
		g.pf("\t\tw.u16(uint16(value.%sLength))\n", name)
		g.pf("\t\tw.raw(value.%s[:value.%sLength])\n\t}\n", name, name)
	case f.Array == ir.ArrayCounted:
		g.pf("\tif value.%sCount > 0 {\n", name)
		g.pf("\t\tw.u16(0x%04x) // %s\n", id, f.Name)
		g.pf("\t\tw.u8(%d)\n", tkArray)
		g.pf("\t\tlenAt := len(w.buf)\n")
		g.pf("\t\tw.u32(0)\n")
		g.pf("\t\tw.u8(%d)\n", kind)
		g.pf("\t\tw.u16(uint16(value.%sCount))\n", name)
		g.pf("\t\tfor i := int32(0); i < value.%sCount; i++ {\n", name)
		g.emitTableWriteElement(f, kind, fmt.Sprintf("value.%s[i]", name), "\t\t\t")
		g.pf("\t\t}\n")
		g.pf("\t\tw.patch32(lenAt, uint32(len(w.buf)-lenAt-4))\n\t}\n")
	case f.Array == ir.ArrayFixed && kind == tkTable:
		// fixed arrays of tables always ride — parity with the C++ writer
		g.pf("\t{\n")
		g.pf("\t\tw.u16(0x%04x) // %s (fixed [%d])\n", id, f.Name, f.ArrayBound)
		g.pf("\t\tw.u8(%d)\n", tkArray)
		g.pf("\t\tlenAt := len(w.buf)\n")
		g.pf("\t\tw.u32(0)\n")
		g.pf("\t\tw.u8(%d)\n", kind)
		g.pf("\t\tw.u16(%d)\n", f.ArrayBound)
		g.pf("\t\tfor i := 0; i < %d; i++ {\n", f.ArrayBound)
		g.emitTableWriteElement(f, kind, fmt.Sprintf("value.%s[i]", name), "\t\t\t")
		g.pf("\t\t}\n")
		g.pf("\t\tw.patch32(lenAt, uint32(len(w.buf)-lenAt-4))\n\t}\n")
	case f.Array == ir.ArrayFixed:
		// fixed arrays are positional; an all-default array elides entirely
		g.pf("\t{\n")
		g.pf("\t\tallDefault := true\n")
		g.pf("\t\tfor i := 0; i < %d; i++ {\n", f.ArrayBound)
		g.pf("\t\t\tif value.%s[i] != %s {\n\t\t\t\tallDefault = false\n\t\t\t\tbreak\n\t\t\t}\n\t\t}\n", name, tableDefaultExpr(f))
		g.pf("\t\tif !allDefault {\n")
		g.pf("\t\t\tw.u16(0x%04x) // %s (fixed [%d])\n", id, f.Name, f.ArrayBound)
		g.pf("\t\t\tw.u8(%d)\n", tkArray)
		g.pf("\t\t\tlenAt := len(w.buf)\n")
		g.pf("\t\t\tw.u32(0)\n")
		g.pf("\t\t\tw.u8(%d)\n", kind)
		g.pf("\t\t\tw.u16(%d)\n", f.ArrayBound)
		g.pf("\t\t\tfor i := 0; i < %d; i++ {\n", f.ArrayBound)
		g.emitTableWriteElement(f, kind, fmt.Sprintf("value.%s[i]", name), "\t\t\t\t")
		g.pf("\t\t\t}\n")
		g.pf("\t\t\tw.patch32(lenAt, uint32(len(w.buf)-lenAt-4))\n\t\t}\n\t}\n")
	case kind == tkTable:
		g.pf("\t{\n")
		g.pf("\t\tfieldAt := len(w.buf)\n")
		g.pf("\t\tw.u16(0x%04x) // %s\n", id, f.Name)
		g.pf("\t\tw.u8(%d)\n", tkTable)
		g.pf("\t\tlenAt := len(w.buf)\n")
		g.pf("\t\tw.u32(0)\n")
		g.pf("\t\ttableWrite%s(w, &value.%s)\n", f.Type.Name, name)
		g.pf("\t\tif len(w.buf)-lenAt-4 <= 2 {\n")
		g.pf("\t\t\tw.buf = w.buf[:fieldAt] // all-default nested elides\n")
		g.pf("\t\t} else {\n")
		g.pf("\t\t\tw.patch32(lenAt, uint32(len(w.buf)-lenAt-4))\n\t\t}\n\t}\n")
	default:
		g.pf("\tif value.%s != %s {\n", name, tableDefaultExpr(f))
		g.pf("\t\tw.u16(0x%04x) // %s\n", id, f.Name)
		g.pf("\t\tw.u8(%d)\n", kind)
		g.emitTableWriteElement(f, kind, "value."+name, "\t\t")
		g.pf("\t}\n")
	}
}

func (g *tableGen) emitTableWriteElement(f *ir.Field, kind int, expr, ind string) {
	switch kind {
	case tkBool:
		g.pf("%sif %s {\n%s\tw.u8(1)\n%s} else {\n%s\tw.u8(0)\n%s}\n", ind, expr, ind, ind, ind, ind)
	case tkF32:
		g.needsMath = true
		g.pf("%sw.u32(math.Float32bits(%s))\n", ind, expr)
	case tkF64:
		g.needsMath = true
		g.pf("%sw.u64(math.Float64bits(%s))\n", ind, expr)
	case tkTable:
		g.pf("%selemLenAt := len(w.buf)\n", ind)
		g.pf("%sw.u32(0)\n", ind)
		g.pf("%stableWrite%s(w, &%s)\n", ind, f.Type.Name, expr)
		g.pf("%sw.patch32(elemLenAt, uint32(len(w.buf)-elemLenAt-4))\n", ind)
	default:
		width := tableKindWidth(kind)
		g.pf("%sw.u%d(%s(%s))\n", ind, width*8, tableWireInt(kind), expr)
	}
}

func (g *tableGen) emitTableRead(st *ir.Struct) {
	g.pf("// TableRead%s decodes a table-wire buffer under the permissive contract:\n", st.Name)
	g.pf("// declared defaults prefill, known fields overlay, unknown ids and kind\n")
	g.pf("// mismatches skip and count, out-of-range values clamp and count. false\n")
	g.pf("// means malformed — the partial decode up to that point is kept.\n")
	g.pf("func TableRead%s(data []byte, value *%s, report *TableReport) bool {\n", st.Name, st.Name)
	g.pf("\tr := &tableReader{buf: data, rep: report}\n")
	g.pf("\treturn tableRead%s(r, value)\n}\n\n", st.Name)

	g.pf("func tableRead%s(r *tableReader, value *%s) bool {\n", st.Name, st.Name)
	if structHasDefaults(st) {
		g.pf("\t*value = New%s() // prefill declared defaults, then overlay\n", st.Name)
	} else {
		g.pf("\t*value = %s{} // prefill (all-zero defaults), then overlay\n", st.Name)
	}
	g.pf("\tfor {\n")
	g.pf("\t\tif !r.has(2) {\n\t\t\tr.rep.Malformed = true\n\t\t\treturn false\n\t\t}\n")
	g.pf("\t\tfieldId := r.get16()\n")
	g.pf("\t\tif fieldId == 0 {\n\t\t\treturn true\n\t\t}\n")
	g.pf("\t\tif !r.has(1) {\n\t\t\tr.rep.Malformed = true\n\t\t\treturn false\n\t\t}\n")
	g.pf("\t\tkind := r.get8()\n")
	g.pf("\t\tswitch fieldId {\n")
	for _, f := range st.Fields {
		id := pack.FieldId(f.Name)
		kind := tableScalarKind(f)
		wireKind := kind
		if f.Array != ir.ArrayNone || f.Type.Kind == ir.TBytes {
			wireKind = tkArray
		}
		if f.Type.Kind == ir.TBytes {
			kind = tkU8 // bytes travel as an array of u8 elements
		}
		g.pf("\t\tcase 0x%04x: // %s\n", id, f.Name)
		g.pf("\t\t\tif kind != %d {\n", wireKind)
		g.pf("\t\t\t\tr.rep.KindMismatch++\n")
		g.pf("\t\t\t\tif !r.skip(kind) {\n\t\t\t\t\tr.rep.Malformed = true\n\t\t\t\t\treturn false\n\t\t\t\t}\n")
		g.pf("\t\t\t\tbreak\n\t\t\t}\n")
		g.emitTableReadField(f, kind)
	}
	g.pf("\t\tdefault:\n")
	g.pf("\t\t\tr.rep.Unknown++\n")
	g.pf("\t\t\tif !r.skip(kind) {\n\t\t\t\tr.rep.Malformed = true\n\t\t\t\treturn false\n\t\t\t}\n")
	g.pf("\t\t}\n\t}\n}\n\n")
}

func (g *tableGen) emitTableReadField(f *ir.Field, kind int) {
	ind := "\t\t\t"
	name := ir.GoExportName(f.Name)
	switch {
	case f.Type.Kind == ir.TString:
		g.pf("%sif !r.has(2) {\n%s\tr.rep.Malformed = true\n%s\treturn false\n%s}\n", ind, ind, ind, ind)
		g.pf("%slength := int(r.get16())\n", ind)
		g.pf("%sif !r.has(length) {\n%s\tr.rep.Malformed = true\n%s\treturn false\n%s}\n", ind, ind, ind, ind)
		g.pf("%skeep := length\n", ind)
		g.pf("%sif keep > %d {\n%s\tkeep = %d\n%s\tr.rep.Clamped++\n%s}\n", ind, f.Type.Size, ind, f.Type.Size, ind, ind)
		g.pf("%scopy(value.%s[:keep], r.buf[r.off:r.off+keep])\n", ind, name)
		g.pf("%svalue.%sLength = int32(keep)\n", ind, name)
		g.pf("%sr.off += length\n", ind)
	case f.Array != ir.ArrayNone || f.Type.Kind == ir.TBytes:
		bound := f.ArrayBound
		if f.Type.Kind == ir.TBytes {
			bound = f.Type.Size
		}
		g.pf("%sif !r.has(4) {\n%s\tr.rep.Malformed = true\n%s\treturn false\n%s}\n", ind, ind, ind, ind)
		g.pf("%sbodyLen := int(r.get32())\n", ind)
		g.pf("%sif !r.has(bodyLen) {\n%s\tr.rep.Malformed = true\n%s\treturn false\n%s}\n", ind, ind, ind, ind)
		g.pf("%sbodyEnd := r.off + bodyLen\n", ind)
		g.pf("%sif bodyLen >= 3 {\n", ind)
		g.pf("%s\telemKind := r.get8()\n", ind)
		g.pf("%s\tcount := int(r.get16())\n", ind)
		g.pf("%s\tif elemKind != %d {\n%s\t\tr.rep.KindMismatch++\n%s\t\tr.off = bodyEnd\n%s\t\tbreak\n%s\t}\n", ind, kind, ind, ind, ind, ind)
		g.pf("%s\tkeep := count\n", ind)
		g.pf("%s\tif keep > %d {\n%s\t\tkeep = %d\n%s\t\tr.rep.Clamped++\n%s\t}\n", ind, bound, ind, bound, ind, ind)
		g.pf("%s\tfor i := 0; i < keep; i++ {\n", ind)
		g.emitTableReadElement(f, kind, ind+"\t\t")
		g.pf("%s\t}\n", ind)
		if f.Type.Kind == ir.TBytes {
			g.pf("%s\tvalue.%sLength = int32(keep)\n", ind, name)
		} else if f.Array == ir.ArrayCounted {
			g.pf("%s\tvalue.%sCount = int32(keep)\n", ind, name)
		}
		g.pf("%s}\n", ind)
		g.pf("%sr.off = bodyEnd // excess elements and slack skip via the length\n", ind)
	case kind == tkTable:
		g.pf("%sif !r.has(4) {\n%s\tr.rep.Malformed = true\n%s\treturn false\n%s}\n", ind, ind, ind, ind)
		g.pf("%sbodyLen := int(r.get32())\n", ind)
		g.pf("%sif !r.has(bodyLen) {\n%s\tr.rep.Malformed = true\n%s\treturn false\n%s}\n", ind, ind, ind, ind)
		g.pf("%ssub := &tableReader{buf: r.buf[r.off : r.off+bodyLen], rep: r.rep}\n", ind)
		g.pf("%stableRead%s(sub, &value.%s)\n", ind, f.Type.Name, name)
		g.pf("%sr.off += bodyLen\n", ind)
	default:
		g.emitTableReadScalarInto(f, kind, "value."+name, ind)
	}
}

func (g *tableGen) emitTableReadElement(f *ir.Field, kind int, ind string) {
	name := ir.GoExportName(f.Name)
	switch kind {
	case tkTable:
		g.pf("%sif !r.has(4) {\n%s\tr.rep.Malformed = true\n%s\treturn false\n%s}\n", ind, ind, ind, ind)
		g.pf("%selemLen := int(r.get32())\n", ind)
		g.pf("%sif !r.has(elemLen) {\n%s\tr.rep.Malformed = true\n%s\treturn false\n%s}\n", ind, ind, ind, ind)
		g.pf("%ssub := &tableReader{buf: r.buf[r.off : r.off+elemLen], rep: r.rep}\n", ind)
		g.pf("%stableRead%s(sub, &value.%s[i])\n", ind, f.Type.Name, name)
		g.pf("%sr.off += elemLen\n", ind)
	default:
		g.emitTableReadScalarInto(f, kind, fmt.Sprintf("value.%s[i]", name), ind)
	}
}

// emitTableReadScalarInto decodes one fixed-width scalar into a storage
// lvalue, with range clamps where the schema declares them.
func (g *tableGen) emitTableReadScalarInto(f *ir.Field, kind int, lvalue, ind string) {
	width := tableKindWidth(kind)
	g.pf("%sif !r.has(%d) {\n%s\tr.rep.Malformed = true\n%s\treturn false\n%s}\n", ind, width, ind, ind, ind)
	switch kind {
	case tkBool:
		g.pf("%s%s = r.get8() != 0\n", ind, lvalue)
	case tkF32:
		g.needsMath = true
		if f.HasFloatRange {
			g.pf("%sdecoded := math.Float32frombits(r.get32())\n", ind)
			g.pf("%sif decoded < %s {\n%s\tdecoded = %s\n%s\tr.rep.Clamped++\n%s} else if decoded > %s {\n%s\tdecoded = %s\n%s\tr.rep.Clamped++\n%s}\n",
				ind, formatFloat(f.FMin), ind, formatFloat(f.FMin), ind, ind, formatFloat(f.FMax), ind, formatFloat(f.FMax), ind, ind)
			g.pf("%s%s = decoded\n", ind, lvalue)
			return
		}
		g.pf("%s%s = math.Float32frombits(r.get32())\n", ind, lvalue)
	case tkF64:
		g.needsMath = true
		if f.HasFloatRange {
			g.pf("%sdecoded := math.Float64frombits(r.get64())\n", ind)
			g.pf("%sif decoded < %s {\n%s\tdecoded = %s\n%s\tr.rep.Clamped++\n%s} else if decoded > %s {\n%s\tdecoded = %s\n%s\tr.rep.Clamped++\n%s}\n",
				ind, formatFloat(f.FMin), ind, formatFloat(f.FMin), ind, ind, formatFloat(f.FMax), ind, formatFloat(f.FMax), ind, ind)
			g.pf("%s%s = decoded\n", ind, lvalue)
			return
		}
		g.pf("%s%s = math.Float64frombits(r.get64())\n", ind, lvalue)
	default:
		if enum, isEnum := f.Type.Ref.(*ir.Enum); f.Type.Kind == ir.TNamed && isEnum {
			g.pf("%sraw := r.get%d()\n", ind, width*8)
			g.pf("%sif raw > %d {\n%s\traw = 0 // out-of-set -> None\n%s\tr.rep.Clamped++\n%s}\n", ind, enum.Max, ind, ind, ind)
			g.pf("%s%s = %s(raw)\n", ind, lvalue, f.Type.Name)
			return
		}
		if _, isFlags := f.Type.Ref.(*ir.Flags); f.Type.Kind == ir.TNamed && isFlags {
			g.pf("%s%s = %s(r.get%d())\n", ind, lvalue, f.Type.Name, width*8)
			return
		}
		storage := tableStorageInt(f, kind)
		signed := f.Type.Kind == ir.TInt && f.Type.Signed
		if signed {
			g.pf("%sdecoded := int%d(r.get%d())\n", ind, width*8, width*8)
		} else {
			g.pf("%sdecoded := r.get%d()\n", ind, width*8)
		}
		if f.HasIntRange {
			g.pf("%sif decoded < %s {\n%s\tdecoded = %s\n%s\tr.rep.Clamped++\n%s} else if decoded > %s {\n%s\tdecoded = %s\n%s\tr.rep.Clamped++\n%s}\n",
				ind, f.IntMin.String(), ind, f.IntMin.String(), ind, ind, f.IntMax.String(), ind, f.IntMax.String(), ind, ind)
		}
		if f.Type.Kind == ir.TBits && f.Type.Width < width*8 {
			maxv := (uint64(1) << f.Type.Width) - 1
			g.pf("%sif decoded > %d {\n%s\tdecoded = %d // bits(%d) width clamp\n%s\tr.rep.Clamped++\n%s}\n", ind, maxv, ind, maxv, f.Type.Width, ind, ind)
		}
		g.pf("%s%s = %s(decoded)\n", ind, lvalue, storage)
	}
}

// tableGuardStrings composes each guarded field's branch condition WITHOUT
// the value. prefix — the reflection descriptor's machine-usable guard
// ("at_rest", "!at_rest", composed with " && " for nesting), matching the
// C++ descriptors byte for byte.
func tableGuardStrings(st *ir.Struct) map[string]string {
	guards := map[string]string{}
	var walk func(items []ir.Item, cond string)
	walk = func(items []ir.Item, cond string) {
		for _, item := range items {
			switch item := item.(type) {
			case *ir.FieldItem:
				if cond != "" {
					guards[item.F.Name] = cond
				}
			case *ir.Branch:
				pos, neg := item.Cond, "!"+item.Cond
				if item.Neg {
					pos, neg = neg, pos
				}
				and := func(a, b string) string {
					if a == "" {
						return b
					}
					return a + " && " + b
				}
				walk(item.Then, and(cond, pos))
				walk(item.Else, and(cond, neg))
			}
		}
	}
	walk(st.Items, "")
	return guards
}

// tableFieldTypeName renders a field's schema-facing type name for the
// descriptor ("float32", "bits(9)", "ShipType", "GunnerSettings").
func tableFieldTypeName(f *ir.Field) string {
	switch f.Type.Kind {
	case ir.TBool:
		return "bool"
	case ir.TInt:
		prefix := "int"
		if !f.Type.Signed {
			prefix = "uint"
		}
		return fmt.Sprintf("%s%d", prefix, f.Type.Width)
	case ir.TBits:
		return fmt.Sprintf("bits(%d)", f.Type.Width)
	case ir.TFloat32:
		return "float32"
	case ir.TFloat64:
		return "float64"
	case ir.TString:
		return "string"
	case ir.TBytes:
		return "bytes"
	case ir.TNamed:
		return f.Type.Name
	}
	return "?"
}

// bigToFloat64 narrows a declared integer bound to the descriptor's float64
// range field (precision past 2^53 is documented as lost).
func bigToFloat64(v *big.Int) float64 {
	f, _ := new(big.Float).SetInt(v).Float64()
	return f
}

// tableStorageType is the generated Go storage type of a settable scalar
// field — the "matching type" TableSetX accepts beside int64/uint64/float64.
func tableStorageType(f *ir.Field) string {
	switch f.Type.Kind {
	case ir.TInt:
		return goInt2(f.Type.Signed, f.Type.Width)
	case ir.TBits:
		if f.Type.Width <= 32 {
			return "uint32"
		}
		return "uint64"
	case ir.TFloat32:
		return "float32"
	case ir.TFloat64:
		return "float64"
	case ir.TNamed:
		return f.Type.Name
	}
	return ""
}

// emitTableDescriptor emits X's descriptor: a package-level var carrying the
// name, an init() that fills in the fields, and the TableTypeX accessor.
//
// The var-plus-init split is the cycle-safety choice: every tableTypeX var
// exists (with its identity pointer) before ANY init() runs, so descriptor
// links between types — in either declaration order, even mutually — always
// resolve, where a single composite var literal could create an
// initialization cycle the compiler refuses. And because init() completes
// before main, descriptors are immutable by the time any goroutine can look:
// concurrent first use is safe with no sync.Once and no lazy state.
func (g *tableGen) emitTableDescriptor(st *ir.Struct) {
	varName := "tableType" + st.Name
	g.pf("var %s = &TableTypeInfo{Name: %q}\n\n", varName, st.Name)
	if len(st.Fields) > 0 {
		guards := tableGuardStrings(st)
		g.pf("func init() {\n")
		g.pf("\t%s.Fields = []TableFieldInfo{\n", varName)
		for _, f := range st.Fields {
			g.pf("\t\t{%s},\n", strings.Join(g.tableDescriptorParts(f, guards[f.Name]), ", "))
		}
		g.pf("\t}\n}\n\n")
	}
	g.pf("// TableType%s returns %s's reflection descriptor — field names, wire\n", st.Name, st.Name)
	g.pf("// ids/kinds, bounds, ranges, enum names and branch guards. Pair it with\n")
	g.pf("// TableGet%s/TableSet%s to walk, print, diff or edit values generically.\n", st.Name, st.Name)
	g.pf("func TableType%s() *TableTypeInfo { return %s }\n\n", st.Name, varName)
}

// tableDescriptorParts renders one field's keyed TableFieldInfo literal —
// zero-valued members stay unwritten (Go's zero value carries them), except
// EnumMax whose not-an-enum value is -1 and so always rides.
func (g *tableGen) tableDescriptorParts(f *ir.Field, guard string) []string {
	kind := tableScalarKind(f)
	if f.Type.Kind == ir.TBytes {
		kind = tkU8 // bytes surface as an array of u8 elements
	}
	parts := []string{
		fmt.Sprintf("Name: %q", f.Name),
		fmt.Sprintf("TypeName: %q", tableFieldTypeName(f)),
		fmt.Sprintf("Id: 0x%04x", pack.FieldId(f.Name)),
		fmt.Sprintf("Kind: %d", kind),
	}
	if f.Array != ir.ArrayNone || f.Type.Kind == ir.TBytes {
		parts = append(parts, "IsArray: true")
	}
	if f.Array == ir.ArrayCounted || f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TString {
		parts = append(parts, "Counted: true")
	}
	switch {
	case f.Array != ir.ArrayNone:
		parts = append(parts, fmt.Sprintf("ArrayBound: %d", f.ArrayBound))
	case f.Type.Kind == ir.TBytes, f.Type.Kind == ir.TString:
		parts = append(parts, fmt.Sprintf("ArrayBound: %d", f.Type.Size))
	}
	if _, isStruct := f.Type.Ref.(*ir.Struct); f.Type.Kind == ir.TNamed && isStruct {
		parts = append(parts, "Table: tableType"+f.Type.Name)
	}
	if f.HasIntRange {
		parts = append(parts, "HasRange: true",
			"RangeMin: "+formatFloat(bigToFloat64(f.IntMin)),
			"RangeMax: "+formatFloat(bigToFloat64(f.IntMax)))
	} else if f.HasFloatRange {
		parts = append(parts, "HasRange: true",
			"RangeMin: "+formatFloat(f.FMin),
			"RangeMax: "+formatFloat(f.FMax))
	}
	if enum, isEnum := f.Type.Ref.(*ir.Enum); f.Type.Kind == ir.TNamed && isEnum {
		parts = append(parts, fmt.Sprintf("EnumMax: %d", enum.Max),
			"EnumName: EnumName"+f.Type.Name)
	} else {
		parts = append(parts, "EnumMax: -1")
	}
	if guard != "" {
		parts = append(parts, fmt.Sprintf("Guard: %q", guard))
	}
	return parts
}

// emitTableGet emits TableGetX — the read half of the accessor pair that
// stands in for C++'s storage offsets.
func (g *tableGen) emitTableGet(st *ir.Struct) {
	g.pf("// TableGet%s reads the named field from value: scalars normalize (signed ->\n", st.Name)
	g.pf("// int64, unsigned/bits -> uint64, floats -> float64, bools as-is), enums and\n")
	g.pf("// flags -> uint64, strings -> the used string, nested tables -> a typed\n")
	g.pf("// pointer, fixed arrays -> a pointer to the backing array, counted arrays\n")
	g.pf("// and bytes -> the used slice. Unknown field names return (nil, false).\n")
	g.pf("func TableGet%s(value *%s, field string) (any, bool) {\n", st.Name, st.Name)
	if len(st.Fields) > 0 {
		g.pf("\tswitch field {\n")
		for _, f := range st.Fields {
			name := ir.GoExportName(f.Name)
			g.pf("\tcase %q:\n", f.Name)
			switch {
			case f.Type.Kind == ir.TString:
				g.pf("\t\treturn string(value.%s[:value.%sLength]), true\n", name, name)
			case f.Type.Kind == ir.TBytes:
				g.pf("\t\treturn value.%s[:value.%sLength], true\n", name, name)
			case f.Array == ir.ArrayCounted:
				g.pf("\t\treturn value.%s[:value.%sCount], true\n", name, name)
			case f.Array == ir.ArrayFixed:
				g.pf("\t\treturn &value.%s, true\n", name)
			case f.Type.Kind == ir.TBool:
				g.pf("\t\treturn value.%s, true\n", name)
			case f.Type.Kind == ir.TFloat32, f.Type.Kind == ir.TFloat64:
				g.pf("\t\treturn float64(value.%s), true\n", name)
			case f.Type.Kind == ir.TInt && f.Type.Signed:
				g.pf("\t\treturn int64(value.%s), true\n", name)
			case f.Type.Kind == ir.TInt, f.Type.Kind == ir.TBits:
				g.pf("\t\treturn uint64(value.%s), true\n", name)
			default: // TNamed
				switch f.Type.Ref.(type) {
				case *ir.Enum, *ir.Flags:
					g.pf("\t\treturn uint64(value.%s), true\n", name)
				case *ir.Struct:
					g.pf("\t\treturn &value.%s, true\n", name)
				}
			}
		}
		g.pf("\t}\n")
	}
	g.pf("\treturn nil, false\n}\n\n")
}

// tableSettable reports whether TableSetX carries a field: the editor write
// path is scalars, enums, flags, bools and strings only.
func tableSettable(f *ir.Field) bool {
	if f.Array != ir.ArrayNone || f.Type.Kind == ir.TBytes {
		return false
	}
	if _, isStruct := f.Type.Ref.(*ir.Struct); f.Type.Kind == ir.TNamed && isStruct {
		return false
	}
	return true
}

// emitTableSet emits TableSetX — the write half of the accessor pair. Values
// clamp exactly like the table-wire read side: declared int/float ranges,
// bits width, enum out-of-set -> None, string truncation to the bound.
func (g *tableGen) emitTableSet(st *ir.Struct) {
	g.pf("// TableSet%s writes the named field — the editor write path: scalars,\n", st.Name)
	g.pf("// enums, flags, bools and strings only. Numerics accept the field's own Go\n")
	g.pf("// type plus int64/uint64/float64; out-of-range values CLAMP exactly as the\n")
	g.pf("// table-wire read side does, and strings truncate to the declared max.\n")
	g.pf("// Unknown fields, nested tables and arrays return false.\n")
	g.pf("func TableSet%s(value *%s, field string, v any) bool {\n", st.Name, st.Name)
	settable := 0
	for _, f := range st.Fields {
		if tableSettable(f) {
			settable++
		}
	}
	if settable == 0 {
		g.pf("\t// no directly-settable fields: nested tables and arrays edit through\n")
		g.pf("\t// their own descriptors and accessors\n")
		g.pf("\treturn false\n}\n\n")
		return
	}
	g.pf("\tswitch field {\n")
	for _, f := range st.Fields {
		if tableSettable(f) {
			g.emitTableSetField(f)
		}
	}
	g.pf("\t}\n\treturn false\n}\n\n")
}

func (g *tableGen) emitTableSetField(f *ir.Field) {
	name := ir.GoExportName(f.Name)
	g.pf("\tcase %q:\n", f.Name)
	switch f.Type.Kind {
	case ir.TBool:
		g.pf("\t\tb, ok := v.(bool)\n\t\tif !ok {\n\t\t\treturn false\n\t\t}\n")
		g.pf("\t\tvalue.%s = b\n\t\treturn true\n", name)
	case ir.TString:
		g.pf("\t\ts, ok := v.(string)\n\t\tif !ok {\n\t\t\treturn false\n\t\t}\n")
		g.pf("\t\tif len(s) > %d {\n\t\t\ts = s[:%d] // truncate to the declared max, as the read side does\n\t\t}\n", f.Type.Size, f.Type.Size)
		g.pf("\t\tcopy(value.%s[:], s)\n", name)
		g.pf("\t\tvalue.%sLength = int32(len(s))\n\t\treturn true\n", name)
	case ir.TFloat32, ir.TFloat64:
		g.emitTableSetNumeric(tableStorageType(f), "float64")
		if f.HasFloatRange {
			g.emitTableSetClamp(formatFloat(f.FMin), formatFloat(f.FMax), "")
		}
		g.emitTableSetAssign(name, tableStorageType(f), "float64")
	case ir.TInt:
		if f.Type.Signed {
			g.emitTableSetNumeric(tableStorageType(f), "int64")
			if f.HasIntRange {
				g.emitTableSetClamp(f.IntMin.String(), f.IntMax.String(), "")
			}
		} else {
			g.emitTableSetNumeric(tableStorageType(f), "uint64")
			if f.HasIntRange {
				min := ""
				if f.IntMin.Sign() > 0 {
					min = f.IntMin.String()
				}
				g.emitTableSetClamp(min, f.IntMax.String(), "")
			}
		}
		g.emitTableSetAssign(name, tableStorageType(f), signedNorm(f.Type.Signed))
	case ir.TBits:
		g.emitTableSetNumeric(tableStorageType(f), "uint64")
		storageBits := 32
		if f.Type.Width > 32 {
			storageBits = 64
		}
		if f.Type.Width < storageBits {
			maxv := (uint64(1) << f.Type.Width) - 1
			g.emitTableSetClamp("", fmt.Sprintf("%d", maxv), fmt.Sprintf(" // bits(%d) width clamp", f.Type.Width))
		}
		g.emitTableSetAssign(name, tableStorageType(f), "uint64")
	case ir.TNamed:
		g.emitTableSetNumeric(f.Type.Name, "uint64")
		if enum, isEnum := f.Type.Ref.(*ir.Enum); isEnum {
			g.pf("\t\tif n > %d {\n\t\t\tn = 0 // out-of-set -> None, as the read side does\n\t\t}\n", enum.Max)
		}
		g.pf("\t\tvalue.%s = %s(n)\n\t\treturn true\n", name, f.Type.Name)
	}
}

func signedNorm(signed bool) string {
	if signed {
		return "int64"
	}
	return "uint64"
}

// emitTableSetNumeric emits the accepted-type switch normalizing v into a
// local n of normType — the field's own storage type first, then the three
// editor numerics, deduplicated where they coincide.
func (g *tableGen) emitTableSetNumeric(matchType, normType string) {
	g.pf("\t\tvar n %s\n", normType)
	g.pf("\t\tswitch t := v.(type) {\n")
	seen := map[string]bool{}
	for _, typ := range []string{matchType, "int64", "uint64", "float64"} {
		if seen[typ] {
			continue
		}
		seen[typ] = true
		if typ == normType {
			g.pf("\t\tcase %s:\n\t\t\tn = t\n", typ)
		} else {
			g.pf("\t\tcase %s:\n\t\t\tn = %s(t)\n", typ, normType)
		}
	}
	g.pf("\t\tdefault:\n\t\t\treturn false\n\t\t}\n")
}

// emitTableSetClamp clamps n to [min, max]; an empty min means the low side
// cannot underflow (unsigned with a non-positive declared min).
func (g *tableGen) emitTableSetClamp(min, max, note string) {
	if min != "" {
		g.pf("\t\tif n < %s {\n\t\t\tn = %s\n\t\t} else if n > %s {\n\t\t\tn = %s%s\n\t\t}\n", min, min, max, max, note)
		return
	}
	g.pf("\t\tif n > %s {\n\t\t\tn = %s%s\n\t\t}\n", max, max, note)
}

func (g *tableGen) emitTableSetAssign(name, storage, normType string) {
	if storage == normType {
		g.pf("\t\tvalue.%s = n\n\t\treturn true\n", name)
		return
	}
	g.pf("\t\tvalue.%s = %s(n)\n\t\treturn true\n", name, storage)
}

// tableRuntime is TableRuntime.go: the report type plus the byte-level
// writer/reader the generated table functions share.
func tableRuntime(pkg string) string {
	return `// Generated by the schema compiler. DO NOT EDIT.
// The TABLE wire runtime (evolution-tolerant, notes/table-wire.md).

package ` + pkg + `

import "encoding/binary"

// TableReport counts the permissive read contract's events: how far the data
// diverged from this build's schema, without anything crashing or rejecting.
type TableReport struct {
	Unknown      int  // fields this schema does not declare (newer data)
	KindMismatch int  // fields whose wire kind changed (skipped, defaults kept)
	Clamped      int  // values pulled into declared ranges / sets / bounds
	Malformed    bool // structurally broken buffer; partial decode was kept
}

// ---- reflection (tables only, notes/table-wire.md) ----
//
// Static field descriptors for every type in the table closure: name, wire
// id/kind, bounds, ranges, enum names and branch guards — enough to walk,
// print, diff, edit or bind any table value at runtime with no schema files.
// TableTypeX() returns X's descriptor. Where C++ exposes storage offsets,
// Go emits typed accessors instead: TableGetX/TableSetX read and write
// fields by name.

// TableFieldInfo describes one field of a table-closure type.
type TableFieldInfo struct {
	Name       string              // schema field name, e.g. "health"
	TypeName   string              // schema type name, e.g. "float32", "ShipType"
	Id         uint16              // table-wire field id (name hash)
	Kind       byte                // table-wire kind; for arrays/bytes, the ELEMENT kind
	IsArray    bool                // fixed or counted array (bytes included)
	Counted    bool                // a Count/Length int32 companion exists (counted arrays, strings, bytes)
	ArrayBound int32               // array capacity / string max length; 0 for plain scalars
	Table      *TableTypeInfo      // nested table's descriptor, or nil
	HasRange   bool                // a declared [min, max] (int or float)
	RangeMin   float64             // NOTE: int64 ranges beyond 2^53 lose precision here
	RangeMax   float64
	EnumMax    int64               // enums: highest valid wire value (None = 0 always valid); else -1
	EnumName   func(uint64) string // enums: value -> name; else nil
	Guard      string              // branch guard, e.g. "at_rest" or "!at_rest"; "" if unguarded
}

// TableTypeInfo is one closure type's descriptor.
type TableTypeInfo struct {
	Name   string // schema type name
	Fields []TableFieldInfo
}

type tableWriter struct{ buf []byte }

func (w *tableWriter) u8(v byte)    { w.buf = append(w.buf, v) }
func (w *tableWriter) u16(v uint16) { w.buf = binary.LittleEndian.AppendUint16(w.buf, v) }
func (w *tableWriter) u32(v uint32) { w.buf = binary.LittleEndian.AppendUint32(w.buf, v) }
func (w *tableWriter) u64(v uint64) { w.buf = binary.LittleEndian.AppendUint64(w.buf, v) }
func (w *tableWriter) raw(b []byte) { w.buf = append(w.buf, b...) }
func (w *tableWriter) patch32(off int, v uint32) {
	binary.LittleEndian.PutUint32(w.buf[off:], v)
}

type tableReader struct {
	buf []byte
	off int
	rep *TableReport
}

func (r *tableReader) has(n int) bool { return n >= 0 && r.off+n <= len(r.buf) }
func (r *tableReader) get8() byte     { v := r.buf[r.off]; r.off++; return v }
func (r *tableReader) get16() uint16 {
	v := binary.LittleEndian.Uint16(r.buf[r.off:])
	r.off += 2
	return v
}
func (r *tableReader) get32() uint32 {
	v := binary.LittleEndian.Uint32(r.buf[r.off:])
	r.off += 4
	return v
}
func (r *tableReader) get64() uint64 {
	v := binary.LittleEndian.Uint64(r.buf[r.off:])
	r.off += 8
	return v
}

// skip advances past one field payload of the given kind — how unknown and
// kind-changed fields stay harmless.
func (r *tableReader) skip(kind byte) bool {
	switch kind {
	case 1, 2, 6: // bool, i8, u8
		if !r.has(1) {
			return false
		}
		r.off++
		return true
	case 3, 7: // i16, u16
		if !r.has(2) {
			return false
		}
		r.off += 2
		return true
	case 4, 8, 10: // i32, u32, f32
		if !r.has(4) {
			return false
		}
		r.off += 4
		return true
	case 5, 9, 11: // i64, u64, f64
		if !r.has(8) {
			return false
		}
		r.off += 8
		return true
	case 12: // string: u16 length + bytes
		if !r.has(2) {
			return false
		}
		n := int(r.get16())
		if !r.has(n) {
			return false
		}
		r.off += n
		return true
	case 13, 14: // table, array: u32 length + body
		if !r.has(4) {
			return false
		}
		n := int(r.get32())
		if !r.has(n) {
			return false
		}
		r.off += n
		return true
	}
	return false // unknown kind: cannot know its length
}
`
}

// GenerateTable emits <Base>Table.go for every unit file, plus
// TableRuntime.go. The caller (Generate) has already refused units the Go
// backend cannot carry (int128/uint128, fixed).
func GenerateTable(u *ir.Unit) (map[string][]byte, error) {
	if err := pack.CheckTableIds(u); err != nil {
		return nil, err
	}
	for _, f := range u.Files {
		if f.Base == "TableRuntime" {
			return nil, fmt.Errorf("schema file TableRuntime collides with the generated Go table runtime file; rename it")
		}
	}
	closure := ir.TableClosure(u)
	out := map[string][]byte{}
	for _, f := range u.Files {
		g := &tableGen{unit: u, file: f}
		var members []*ir.Struct
		for _, d := range f.Decls {
			if st, ok := d.(*ir.Struct); ok && closure[st.Name] {
				members = append(members, st)
			}
		}
		for _, st := range members {
			g.emitTableWrite(st)
			g.emitTableRead(st)
		}
		if len(members) > 0 {
			g.pf("// ---- reflection descriptors (tables only, notes/table-wire.md) ----\n")
			g.pf("//\n")
			g.pf("// Descriptor links are wired in init(), which completes before main: every\n")
			g.pf("// tableTypeX var exists before any init() runs (so cross-type links always\n")
			g.pf("// resolve, whatever the declaration order), and by the time any goroutine\n")
			g.pf("// can look the descriptors are immutable — concurrent first use needs no\n")
			g.pf("// locking and no lazy state.\n\n")
			for _, st := range members {
				g.emitTableDescriptor(st)
				g.emitTableGet(st)
				g.emitTableSet(st)
			}
		} else {
			g.pf("// no tables declared or referenced in this file — codecs are emitted\n")
			g.pf("// for the table closure only (`table` declarations and what they reach)\n")
		}
		var h strings.Builder
		fmt.Fprintf(&h, "// Generated by the schema compiler from %s.schema. DO NOT EDIT.\n", f.Base)
		fmt.Fprintf(&h, "// package %s — protocol id 0x%016x\n", u.Package, u.ProtocolId)
		h.WriteString("// The TABLE wire (evolution-tolerant, notes/table-wire.md).\n\n")
		fmt.Fprintf(&h, "package %s\n\n", u.Package)
		if g.needsMath {
			h.WriteString("import \"math\"\n\n")
		}
		h.WriteString(g.body.String())
		src, err := format.Source([]byte(h.String()))
		if err != nil {
			return nil, fmt.Errorf("generated Go table code for %s does not parse — a compiler bug, not a schema error: %v", f.Path, err)
		}
		out[f.Base+"Table.go"] = src
	}
	runtime, err := format.Source([]byte(tableRuntime(u.Package)))
	if err != nil {
		return nil, fmt.Errorf("Go table runtime does not parse — a compiler bug: %v", err)
	}
	out["TableRuntime.go"] = runtime
	return out, nil
}
