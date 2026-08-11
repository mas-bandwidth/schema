// Table-wire emission for Go (notes/table-wire.md): <Base>Table.go per unit
// file plus TableRuntime.go — TableWrite/TableRead per `type`, plain byte
// code with no serialize dependency. Field identity is the name-hash id;
// readers prefill declared defaults then overlay, skip unknown ids, skip
// kind mismatches, clamp out-of-range values, and count every event. The
// bytes are pinned against the C++ writer by the cross-language goldens.
//
// This is what lets the Go backend open the same Config.bin/Assets.bin the
// game server reads — one file, native typed readers in every language.
package golang

import (
	"fmt"
	"go/format"
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
		emitted := 0
		for _, d := range f.Decls {
			if st, ok := d.(*ir.Struct); ok {
				if !closure[st.Name] {
					continue
				}
				g.emitTableWrite(st)
				g.emitTableRead(st)
				emitted++
			}
		}
		if emitted == 0 {
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
