// Table-wire emission for C# (notes/table-wire.md): <Base>Table.cs per unit
// file plus TableRuntime.cs — TableWrite/TableRead per `type`, plain byte
// code with no serialize dependency, using System only — Unity/IL2CPP-safe
// (no reflection, no unsafe, no LINQ). Field identity is the name-hash id;
// readers prefill declared defaults then overlay, skip unknown ids, skip
// kind mismatches, clamp out-of-range values, and count every event. The
// bytes are pinned against the C++ writer by the cross-language goldens.
//
// This is what lets Unity write UserSettings and read the same
// Config.bin/Assets.bin the game server reads — one file, native typed
// readers in every language.
//
// Every closure type also carries reflection: TableTypeX() returns a static
// field descriptor (name, wire id/kind, bounds, ranges, enum names, branch
// guards — the C++ TableType<X>() surface), data-oriented per SPEC §4.11:
// one flat TableFieldInfo[] of contiguous structs per type, separate from
// table data, walked sequentially, zero per-instance weight. Where C++
// exposes storage offsets, C# emits typed accessors instead —
// TableGetX/TableSetX read and write fields by name through the TableValue
// variant struct, no boxing on the numeric and bool paths. This is what
// runtime config editors (Unity) and generic tooling bind against.
package csharp

import (
	"fmt"
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

// tableWireUint is the unsigned C# type a kind travels as on the wire.
func tableWireUint(kind int) string {
	return csUint(tableKindWidth(kind) * 8)
}

// tableStorageType is the generated C# storage type a scalar field decodes
// into (enums and flags are special-cased before this is consulted).
func tableStorageType(f *ir.Field, kind int) string {
	switch f.Type.Kind {
	case ir.TInt:
		if f.Type.Signed {
			return csInt(f.Type.Width)
		}
		return csUint(f.Type.Width)
	case ir.TBits:
		if f.Type.Width <= 32 {
			return "uint"
		}
		return "ulong"
	}
	if kind >= tkI8 && kind <= tkI64 {
		return csInt(tableKindWidth(kind) * 8)
	}
	return tableWireUint(kind)
}

// tableIntLit renders an integer literal safely for C#: 64-bit values carry
// their L/UL suffix (a bare decimal past int range types long, which cannot
// compare against ulong), and long.MinValue is written directly — C# admits
// -9223372036854775808 as a long literal by special rule, suffix-free.
func tableIntLit(v *big.Int, signed bool, widthBytes int) string {
	s := v.String()
	if widthBytes < 8 {
		return s
	}
	if !signed {
		return s + "UL"
	}
	if s == "-9223372036854775808" {
		return s
	}
	return s + "L"
}

type tableGen struct {
	unit   *ir.Unit
	file   *ir.File
	body   strings.Builder
	indent string // extra per-line indent while emitting inside a branch guard
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

// pfMalformed emits the framing check: fewer than n bytes left means a
// structurally broken buffer — report and stop, keeping the partial decode.
func (g *tableGen) pfMalformed(ind string, n any) {
	g.pf("%sif (!r.Has(%v))\n%s{\n%s    r.Report.Malformed = true;\n%s    return false;\n%s}\n", ind, n, ind, ind, ind, ind)
}

// pfClamp emits the clamp-and-count pair over the local `decoded`.
func (g *tableGen) pfClamp(ind, lo, hi, note string) {
	g.pf("%sif (decoded < %s)\n%s{\n%s    decoded = %s;%s\n%s    r.Report.Clamped++;\n%s}\n", ind, lo, ind, ind, lo, note, ind, ind)
	g.pf("%selse if (decoded > %s)\n%s{\n%s    decoded = %s;%s\n%s    r.Report.Clamped++;\n%s}\n", ind, hi, ind, ind, hi, note, ind, ind)
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

// tableDefaultExpr renders the C# expression a scalar field's default
// compares against on the write side — identical values to the class
// initializers the reader's `new X()` prefill installs.
func tableDefaultExpr(f *ir.Field) string {
	switch f.Type.Kind {
	case ir.TBool:
		if f.HasDefault && f.DefBool {
			return "true"
		}
		return "false"
	case ir.TFloat32:
		if f.HasDefault {
			return formatFloat32(f.DefFloat)
		}
		return "0.0f"
	case ir.TFloat64:
		if f.HasDefault {
			return formatFloat(f.DefFloat)
		}
		return "0.0"
	case ir.TInt, ir.TBits:
		if f.HasDefault && f.DefInt != nil {
			signed := f.Type.Kind == ir.TInt && f.Type.Signed
			width := 4
			if f.Type.Width > 32 {
				width = 8
			}
			return tableIntLit(f.DefInt, signed, width)
		}
		return "0"
	case ir.TNamed:
		switch f.Type.Ref.(type) {
		case *ir.Enum:
			if f.HasDefault && f.DefVariant != "" {
				return f.Type.Name + "." + f.DefVariant
			}
			return f.Type.Name + ".None"
		case *ir.Flags:
			return "0"
		}
	}
	return "0"
}

func (g *tableGen) emitTableWrite(st *ir.Struct) {
	g.pf("// TableWrite%s returns value's table-wire encoding as a fresh buffer.\n", st.Name)
	g.pf("public static byte[] TableWrite%s(%s value)\n{\n", st.Name, st.Name)
	g.pf("    TableWriter w = new TableWriter();\n")
	g.pf("    TableWrite%s(w, value);\n", st.Name)
	g.pf("    return w.ToArray();\n}\n\n")

	g.pf("// AppendTable%s appends value's table-wire encoding to w — the\n", st.Name)
	g.pf("// zero-allocation write path: hold one TableWriter, Clear() it between\n")
	g.pf("// uses, and steady-state writes never allocate once the buffer has grown.\n")
	g.pf("// The wire is w.Buf[0 .. w.Len), byte-identical to TableWrite%s.\n", st.Name)
	g.pf("public static void AppendTable%s(TableWriter w, %s value)\n{\n", st.Name, st.Name)
	g.pf("    TableWrite%s(w, value);\n}\n\n", st.Name)

	g.pf("private static void TableWrite%s(TableWriter w, %s value)\n{\n", st.Name, st.Name)
	guards := tableGuardExprs(st)
	for _, f := range st.Fields {
		if cond, guarded := guards[f.Name]; guarded {
			g.pf("    if (%s)\n    {\n", cond)
			g.indent = "    "
			g.emitTableWriteField(f)
			g.indent = ""
			g.pf("    }\n")
			continue
		}
		g.emitTableWriteField(f)
	}
	g.pf("    w.U16(0); // terminator\n")
	g.pf("}\n\n")
}

func (g *tableGen) emitTableWriteField(f *ir.Field) {
	id := pack.FieldId(f.Name)
	kind := tableScalarKind(f)
	name := ir.GoExportName(f.Name)
	switch {
	case f.Type.Kind == ir.TString:
		g.pf("    if (value.%sLength > 0)\n    {\n", name)
		g.pf("        w.U16(0x%04x); // %s\n", id, f.Name)
		g.pf("        w.U8(%d);\n", tkString)
		g.pf("        w.U16((ushort)value.%sLength);\n", name)
		g.pf("        w.Raw(value.%s, value.%sLength);\n    }\n", name, name)
	case f.Type.Kind == ir.TBytes:
		g.pf("    if (value.%sLength > 0)\n    {\n", name)
		g.pf("        w.U16(0x%04x); // %s\n", id, f.Name)
		g.pf("        w.U8(%d);\n", tkArray)
		g.pf("        w.U32((uint)(3 + value.%sLength));\n", name)
		g.pf("        w.U8(%d);\n", tkU8)
		g.pf("        w.U16((ushort)value.%sLength);\n", name)
		g.pf("        w.Raw(value.%s, value.%sLength);\n    }\n", name, name)
	case f.Array == ir.ArrayCounted:
		g.pf("    if (value.%sCount > 0)\n    {\n", name)
		g.pf("        w.U16(0x%04x); // %s\n", id, f.Name)
		g.pf("        w.U8(%d);\n", tkArray)
		g.pf("        int lenAt = w.Len;\n")
		g.pf("        w.U32(0);\n")
		g.pf("        w.U8(%d);\n", kind)
		g.pf("        w.U16((ushort)value.%sCount);\n", name)
		g.pf("        for (int i = 0; i < value.%sCount; i++)\n        {\n", name)
		g.emitTableWriteElement(f, kind, fmt.Sprintf("value.%s[i]", name), "            ")
		g.pf("        }\n")
		g.pf("        w.Patch32(lenAt, (uint)(w.Len - lenAt - 4));\n    }\n")
	case f.Array == ir.ArrayFixed && kind == tkTable:
		// fixed arrays of tables always ride — parity with the C++ writer
		g.pf("    {\n")
		g.pf("        w.U16(0x%04x); // %s (fixed [%d])\n", id, f.Name, f.ArrayBound)
		g.pf("        w.U8(%d);\n", tkArray)
		g.pf("        int lenAt = w.Len;\n")
		g.pf("        w.U32(0);\n")
		g.pf("        w.U8(%d);\n", kind)
		g.pf("        w.U16(%d);\n", f.ArrayBound)
		g.pf("        for (int i = 0; i < %d; i++)\n        {\n", f.ArrayBound)
		g.emitTableWriteElement(f, kind, fmt.Sprintf("value.%s[i]", name), "            ")
		g.pf("        }\n")
		g.pf("        w.Patch32(lenAt, (uint)(w.Len - lenAt - 4));\n    }\n")
	case f.Array == ir.ArrayFixed:
		// fixed arrays are positional; an all-default array elides entirely
		g.pf("    {\n")
		g.pf("        bool allDefault = true;\n")
		g.pf("        for (int i = 0; i < %d; i++)\n        {\n", f.ArrayBound)
		g.pf("            if (value.%s[i] != %s)\n            {\n                allDefault = false;\n                break;\n            }\n        }\n", name, tableDefaultExpr(f))
		g.pf("        if (!allDefault)\n        {\n")
		g.pf("            w.U16(0x%04x); // %s (fixed [%d])\n", id, f.Name, f.ArrayBound)
		g.pf("            w.U8(%d);\n", tkArray)
		g.pf("            int lenAt = w.Len;\n")
		g.pf("            w.U32(0);\n")
		g.pf("            w.U8(%d);\n", kind)
		g.pf("            w.U16(%d);\n", f.ArrayBound)
		g.pf("            for (int i = 0; i < %d; i++)\n            {\n", f.ArrayBound)
		g.emitTableWriteElement(f, kind, fmt.Sprintf("value.%s[i]", name), "                ")
		g.pf("            }\n")
		g.pf("            w.Patch32(lenAt, (uint)(w.Len - lenAt - 4));\n        }\n    }\n")
	case kind == tkTable:
		g.pf("    {\n")
		g.pf("        int fieldAt = w.Len;\n")
		g.pf("        w.U16(0x%04x); // %s\n", id, f.Name)
		g.pf("        w.U8(%d);\n", tkTable)
		g.pf("        int lenAt = w.Len;\n")
		g.pf("        w.U32(0);\n")
		g.pf("        TableWrite%s(w, value.%s);\n", f.Type.Name, name)
		g.pf("        if (w.Len - lenAt - 4 <= 2)\n        {\n")
		g.pf("            w.Len = fieldAt; // all-default nested elides\n")
		g.pf("        }\n        else\n        {\n")
		g.pf("            w.Patch32(lenAt, (uint)(w.Len - lenAt - 4));\n        }\n    }\n")
	default:
		g.pf("    if (value.%s != %s)\n    {\n", name, tableDefaultExpr(f))
		g.pf("        w.U16(0x%04x); // %s\n", id, f.Name)
		g.pf("        w.U8(%d);\n", kind)
		g.emitTableWriteElement(f, kind, "value."+name, "        ")
		g.pf("    }\n")
	}
}

func (g *tableGen) emitTableWriteElement(f *ir.Field, kind int, expr, ind string) {
	switch kind {
	case tkBool:
		g.pf("%sif (%s)\n%s{\n%s    w.U8(1);\n%s}\n%selse\n%s{\n%s    w.U8(0);\n%s}\n", ind, expr, ind, ind, ind, ind, ind, ind, ind)
	case tkF32:
		g.pf("%sw.F32(%s);\n", ind, expr)
	case tkF64:
		g.pf("%sw.F64(%s);\n", ind, expr)
	case tkTable:
		g.pf("%sint elemLenAt = w.Len;\n", ind)
		g.pf("%sw.U32(0);\n", ind)
		g.pf("%sTableWrite%s(w, %s);\n", ind, f.Type.Name, expr)
		g.pf("%sw.Patch32(elemLenAt, (uint)(w.Len - elemLenAt - 4));\n", ind)
	default:
		width := tableKindWidth(kind)
		g.pf("%sw.U%d((%s)%s);\n", ind, width*8, tableWireUint(kind), expr)
	}
}

func (g *tableGen) emitTableRead(st *ir.Struct) {
	g.pf("// TableRead%s decodes a table-wire buffer under the permissive contract:\n", st.Name)
	g.pf("// declared defaults prefill, known fields overlay, unknown ids and kind\n")
	g.pf("// mismatches skip and count, out-of-range values clamp and count. false\n")
	g.pf("// means malformed — the partial decode up to that point is kept.\n")
	g.pf("public static bool TableRead%s(byte[] data, ref %s value, TableReport report)\n{\n", st.Name, st.Name)
	g.pf("    TableReader r = new TableReader(data, 0, data.Length, report);\n")
	g.pf("    return TableRead%s(r, ref value);\n}\n\n", st.Name)

	g.pf("private static bool TableRead%s(TableReader r, ref %s value)\n{\n", st.Name, st.Name)
	g.pf("    value = new %s(); // prefill declared defaults, then overlay\n", st.Name)
	g.pf("    while (true)\n    {\n")
	g.pfMalformed("        ", 2)
	g.pf("        ushort fieldId = r.Get16();\n")
	g.pf("        if (fieldId == 0)\n        {\n            return true;\n        }\n")
	g.pfMalformed("        ", 1)
	g.pf("        byte kind = r.Get8();\n")
	g.pf("        switch (fieldId)\n        {\n")
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
		g.pf("            case 0x%04x: // %s\n            {\n", id, f.Name)
		g.pf("                if (kind != %d)\n                {\n", wireKind)
		g.pf("                    r.Report.KindMismatch++;\n")
		g.pf("                    if (!r.Skip(kind))\n                    {\n                        r.Report.Malformed = true;\n                        return false;\n                    }\n")
		g.pf("                    break;\n                }\n")
		g.emitTableReadField(f, kind)
		g.pf("                break;\n            }\n")
	}
	g.pf("            default:\n            {\n")
	g.pf("                r.Report.Unknown++;\n")
	g.pf("                if (!r.Skip(kind))\n                {\n                    r.Report.Malformed = true;\n                    return false;\n                }\n")
	g.pf("                break;\n            }\n")
	g.pf("        }\n    }\n}\n\n")
}

func (g *tableGen) emitTableReadField(f *ir.Field, kind int) {
	ind := "                "
	name := ir.GoExportName(f.Name)
	switch {
	case f.Type.Kind == ir.TString:
		g.pfMalformed(ind, 2)
		g.pf("%sint length = r.Get16();\n", ind)
		g.pfMalformed(ind, "length")
		g.pf("%sint keep = length;\n", ind)
		g.pf("%sif (keep > %d)\n%s{\n%s    keep = %d;\n%s    r.Report.Clamped++;\n%s}\n", ind, f.Type.Size, ind, ind, f.Type.Size, ind, ind)
		g.pf("%sr.CopyOut(value.%s, keep);\n", ind, name)
		g.pf("%svalue.%sLength = keep;\n", ind, name)
		g.pf("%sr.Off += length;\n", ind)
	case f.Array != ir.ArrayNone || f.Type.Kind == ir.TBytes:
		bound := f.ArrayBound
		if f.Type.Kind == ir.TBytes {
			bound = f.Type.Size
		}
		g.pfMalformed(ind, 4)
		g.pf("%sint bodyLen = (int)r.Get32();\n", ind)
		g.pfMalformed(ind, "bodyLen")
		g.pf("%sint bodyEnd = r.Off + bodyLen;\n", ind)
		g.pf("%sif (bodyLen >= 3)\n%s{\n", ind, ind)
		g.pf("%s    byte elemKind = r.Get8();\n", ind)
		g.pf("%s    int count = r.Get16();\n", ind)
		g.pf("%s    if (elemKind != %d)\n%s    {\n%s        r.Report.KindMismatch++;\n%s        r.Off = bodyEnd;\n%s        break;\n%s    }\n", ind, kind, ind, ind, ind, ind, ind)
		g.pf("%s    int keep = count;\n", ind)
		g.pf("%s    if (keep > %d)\n%s    {\n%s        keep = %d;\n%s        r.Report.Clamped++;\n%s    }\n", ind, bound, ind, ind, bound, ind, ind)
		g.pf("%s    for (int i = 0; i < keep; i++)\n%s    {\n", ind, ind)
		g.emitTableReadElement(f, kind, ind+"        ")
		g.pf("%s    }\n", ind)
		if f.Type.Kind == ir.TBytes {
			g.pf("%s    value.%sLength = keep;\n", ind, name)
		} else if f.Array == ir.ArrayCounted {
			g.pf("%s    value.%sCount = keep;\n", ind, name)
		}
		g.pf("%s}\n", ind)
		g.pf("%sr.Off = bodyEnd; // excess elements and slack skip via the length\n", ind)
	case kind == tkTable:
		g.pfMalformed(ind, 4)
		g.pf("%sint bodyLen = (int)r.Get32();\n", ind)
		g.pfMalformed(ind, "bodyLen")
		g.pf("%sTableReader sub = new TableReader(r.Buf, r.Off, r.Off + bodyLen, r.Report);\n", ind)
		g.pf("%sTableRead%s(sub, ref value.%s);\n", ind, f.Type.Name, name)
		g.pf("%sr.Off += bodyLen;\n", ind)
	default:
		g.emitTableReadScalarInto(f, kind, "value."+name, ind)
	}
}

func (g *tableGen) emitTableReadElement(f *ir.Field, kind int, ind string) {
	name := ir.GoExportName(f.Name)
	switch kind {
	case tkTable:
		g.pfMalformed(ind, 4)
		g.pf("%sint elemLen = (int)r.Get32();\n", ind)
		g.pfMalformed(ind, "elemLen")
		g.pf("%sTableReader sub = new TableReader(r.Buf, r.Off, r.Off + elemLen, r.Report);\n", ind)
		g.pf("%sTableRead%s(sub, ref value.%s[i]);\n", ind, f.Type.Name, name)
		g.pf("%sr.Off += elemLen;\n", ind)
	default:
		g.emitTableReadScalarInto(f, kind, fmt.Sprintf("value.%s[i]", name), ind)
	}
}

// emitTableReadScalarInto decodes one fixed-width scalar into a storage
// lvalue, with range clamps where the schema declares them.
func (g *tableGen) emitTableReadScalarInto(f *ir.Field, kind int, lvalue, ind string) {
	width := tableKindWidth(kind)
	g.pfMalformed(ind, width)
	switch kind {
	case tkBool:
		g.pf("%s%s = r.Get8() != 0;\n", ind, lvalue)
	case tkF32:
		if f.HasFloatRange {
			g.pf("%sfloat decoded = r.GetF32();\n", ind)
			g.pfClamp(ind, formatFloat32(f.FMin), formatFloat32(f.FMax), "")
			g.pf("%s%s = decoded;\n", ind, lvalue)
			return
		}
		g.pf("%s%s = r.GetF32();\n", ind, lvalue)
	case tkF64:
		if f.HasFloatRange {
			g.pf("%sdouble decoded = r.GetF64();\n", ind)
			g.pfClamp(ind, formatFloat(f.FMin), formatFloat(f.FMax), "")
			g.pf("%s%s = decoded;\n", ind, lvalue)
			return
		}
		g.pf("%s%s = r.GetF64();\n", ind, lvalue)
	default:
		if enum, isEnum := f.Type.Ref.(*ir.Enum); f.Type.Kind == ir.TNamed && isEnum {
			maxLit := fmt.Sprintf("%d", enum.Max)
			if width == 8 {
				maxLit += "UL"
			}
			g.pf("%s%s raw = r.Get%d();\n", ind, tableWireUint(kind), width*8)
			g.pf("%sif (raw > %s)\n%s{\n%s    raw = 0; // out-of-set -> None\n%s    r.Report.Clamped++;\n%s}\n", ind, maxLit, ind, ind, ind, ind)
			g.pf("%s%s = (%s)raw;\n", ind, lvalue, f.Type.Name)
			return
		}
		if _, isFlags := f.Type.Ref.(*ir.Flags); f.Type.Kind == ir.TNamed && isFlags {
			g.pf("%s%s = r.Get%d();\n", ind, lvalue, width*8)
			return
		}
		storage := tableStorageType(f, kind)
		signed := f.Type.Kind == ir.TInt && f.Type.Signed
		if signed {
			g.pf("%s%s decoded = (%s)r.Get%d();\n", ind, csInt(width*8), csInt(width*8), width*8)
		} else {
			g.pf("%s%s decoded = r.Get%d();\n", ind, csUint(width*8), width*8)
		}
		if f.HasIntRange {
			lo := tableIntLit(f.IntMin, signed, width)
			hi := tableIntLit(f.IntMax, signed, width)
			g.pfClamp(ind, lo, hi, "")
		}
		if f.Type.Kind == ir.TBits && f.Type.Width < width*8 {
			maxv := (uint64(1) << f.Type.Width) - 1
			maxLit := fmt.Sprintf("%d", maxv)
			if width == 8 {
				maxLit += "UL"
			}
			g.pf("%sif (decoded > %s)\n%s{\n%s    decoded = %s; // bits(%d) width clamp\n%s    r.Report.Clamped++;\n%s}\n", ind, maxLit, ind, ind, maxLit, f.Type.Width, ind, ind)
		}
		g.pf("%s%s = (%s)decoded;\n", ind, lvalue, storage)
	}
}

// tableGuardStrings composes each guarded field's branch condition WITHOUT
// the value. prefix — the reflection descriptor's machine-usable guard
// ("at_rest", "!at_rest", composed with " && " for nesting), matching the
// C++ and Go descriptors byte for byte.
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

// bigToFloat64 narrows a declared integer bound to the descriptor's double
// range field (precision past 2^53 is documented as lost).
func bigToFloat64(v *big.Int) float64 {
	f, _ := new(big.Float).SetInt(v).Float64()
	return f
}

// emitTableDescriptor emits TableTypeX() — the descriptor accessor over one
// flat TableFieldInfo[] (SPEC §4.11: contiguous structs, sequential walks,
// zero per-instance weight), built lazily on first use: C# gives partial
// classes NO cross-file static initializer order, so an eager initializer
// could observe a not-yet-built nested descriptor — the accessors compose
// in dependency order instead (the closure is acyclic). A double-build
// under a race is benign: equivalent data, last write wins.
func (g *tableGen) emitTableDescriptor(st *ir.Struct) {
	guards := tableGuardStrings(st)
	g.pf("private static TableTypeInfo _tableType%s;\n\n", st.Name)
	g.pf("// TableType%s returns %s's reflection descriptor — field names, wire\n", st.Name, st.Name)
	g.pf("// ids/kinds, bounds, ranges, enum names and branch guards. Pair it with\n")
	g.pf("// TableGet%s/TableSet%s to walk, print, diff or edit values generically\n", st.Name, st.Name)
	g.pf("// (walk the flat Fields array with `ref readonly var f = ref fields[i];`).\n")
	g.pf("public static TableTypeInfo TableType%s()\n{\n", st.Name)
	g.pf("    if (_tableType%s != null)\n    {\n        return _tableType%s;\n    }\n", st.Name, st.Name)
	g.pf("    TableFieldInfo[] fields = new TableFieldInfo[%d];\n", len(st.Fields))
	for i, f := range st.Fields {
		g.pf("    fields[%d] = new TableFieldInfo { %s };\n", i, strings.Join(g.tableDescriptorParts(f, guards[f.Name]), ", "))
	}
	g.pf("    TableTypeInfo info = new TableTypeInfo();\n")
	g.pf("    info.Name = \"%s\";\n", st.Name)
	g.pf("    info.Fields = fields;\n")
	g.pf("    _tableType%s = info;\n", st.Name)
	g.pf("    return info;\n}\n\n")
}

// tableDescriptorParts renders one field's TableFieldInfo initializer —
// default-valued members stay unwritten (the struct's zero carries them),
// except EnumMax whose not-an-enum value is -1 and Guard whose unguarded
// value is "" (a C# 9 struct cannot default a field to it), so both always
// ride.
func (g *tableGen) tableDescriptorParts(f *ir.Field, guard string) []string {
	kind := tableScalarKind(f)
	if f.Type.Kind == ir.TBytes {
		kind = tkU8 // bytes surface as an array of u8 elements
	}
	parts := []string{
		fmt.Sprintf("Name = %q", f.Name),
		fmt.Sprintf("TypeName = %q", tableFieldTypeName(f)),
		fmt.Sprintf("Id = 0x%04x", pack.FieldId(f.Name)),
		fmt.Sprintf("Kind = %d", kind),
	}
	if f.Array != ir.ArrayNone || f.Type.Kind == ir.TBytes {
		parts = append(parts, "IsArray = true")
	}
	if f.Array == ir.ArrayCounted || f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TString {
		parts = append(parts, "Counted = true")
	}
	switch {
	case f.Array != ir.ArrayNone:
		parts = append(parts, fmt.Sprintf("ArrayBound = %d", f.ArrayBound))
	case f.Type.Kind == ir.TBytes, f.Type.Kind == ir.TString:
		parts = append(parts, fmt.Sprintf("ArrayBound = %d", f.Type.Size))
	}
	if _, isStruct := f.Type.Ref.(*ir.Struct); f.Type.Kind == ir.TNamed && isStruct {
		parts = append(parts, "Table = TableType"+f.Type.Name+"()")
	}
	if f.HasIntRange {
		parts = append(parts, "HasRange = true",
			"RangeMin = "+formatFloat(bigToFloat64(f.IntMin)),
			"RangeMax = "+formatFloat(bigToFloat64(f.IntMax)))
	} else if f.HasFloatRange {
		parts = append(parts, "HasRange = true",
			"RangeMin = "+formatFloat(f.FMin),
			"RangeMax = "+formatFloat(f.FMax))
	}
	if enum, isEnum := f.Type.Ref.(*ir.Enum); f.Type.Kind == ir.TNamed && isEnum {
		parts = append(parts, fmt.Sprintf("EnumMax = %d", enum.Max),
			"EnumName = EnumName"+f.Type.Name)
	} else {
		parts = append(parts, "EnumMax = -1")
	}
	parts = append(parts, fmt.Sprintf("Guard = %q", guard))
	return parts
}

// emitTableGet emits TableGetX — the read half of the accessor pair that
// stands in for C++'s storage offsets.
func (g *tableGen) emitTableGet(st *ir.Struct) {
	g.pf("// TableGet%s reads the named field from value into a TableValue — no\n", st.Name)
	g.pf("// boxing (SPEC §4.11): scalars normalize (signed -> I, unsigned/bits -> U,\n")
	g.pf("// floats -> F, bools -> B), enums and flags -> U, strings decode into S\n")
	g.pf("// (the one allocating path), nested tables put the member reference in\n")
	g.pf("// Obj, arrays put the member array in Obj and the used element count in\n")
	g.pf("// Count (fixed arrays: the full bound). Unknown field names return false.\n")
	g.pf("public static bool TableGet%s(%s value, string field, out TableValue result)\n{\n", st.Name, st.Name)
	if len(st.Fields) > 0 {
		g.pf("    switch (field)\n    {\n")
		for _, f := range st.Fields {
			name := ir.GoExportName(f.Name)
			var init string
			switch {
			case f.Type.Kind == ir.TString:
				init = fmt.Sprintf("Kind = TableValueKind.String, S = System.Text.Encoding.UTF8.GetString(value.%s, 0, value.%sLength)", name, name)
			case f.Type.Kind == ir.TBytes:
				init = fmt.Sprintf("Kind = TableValueKind.Array, Obj = value.%s, Count = value.%sLength", name, name)
			case f.Array == ir.ArrayCounted:
				init = fmt.Sprintf("Kind = TableValueKind.Array, Obj = value.%s, Count = value.%sCount", name, name)
			case f.Array == ir.ArrayFixed:
				init = fmt.Sprintf("Kind = TableValueKind.Array, Obj = value.%s, Count = value.%s.Length", name, name)
			case f.Type.Kind == ir.TBool:
				init = fmt.Sprintf("Kind = TableValueKind.Bool, B = value.%s", name)
			case f.Type.Kind == ir.TFloat32, f.Type.Kind == ir.TFloat64:
				init = fmt.Sprintf("Kind = TableValueKind.Float, F = value.%s", name)
			case f.Type.Kind == ir.TInt && f.Type.Signed:
				init = fmt.Sprintf("Kind = TableValueKind.Int, I = value.%s", name)
			case f.Type.Kind == ir.TInt, f.Type.Kind == ir.TBits:
				init = fmt.Sprintf("Kind = TableValueKind.Uint, U = value.%s", name)
			default: // TNamed
				switch f.Type.Ref.(type) {
				case *ir.Enum:
					init = fmt.Sprintf("Kind = TableValueKind.Uint, U = (ulong)value.%s", name)
				case *ir.Flags:
					init = fmt.Sprintf("Kind = TableValueKind.Uint, U = value.%s", name)
				case *ir.Struct:
					init = fmt.Sprintf("Kind = TableValueKind.Table, Obj = value.%s", name)
				}
			}
			g.pf("        case %q:\n        {\n", f.Name)
			g.pf("            result = new TableValue { %s };\n", init)
			g.pf("            return true;\n        }\n")
		}
		g.pf("    }\n")
	}
	g.pf("    result = new TableValue();\n    return false;\n}\n\n")
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
	g.pf("// enums, flags, bools and strings only. Numerics accept the Int, Uint and\n")
	g.pf("// Float kinds; out-of-range values CLAMP exactly as the table-wire read\n")
	g.pf("// side does (declared ranges, bits width, enum out-of-set -> None), and\n")
	g.pf("// strings truncate to the declared max. Unknown fields, nested tables and\n")
	g.pf("// arrays return false.\n")
	g.pf("public static bool TableSet%s(%s value, string field, in TableValue v)\n{\n", st.Name, st.Name)
	settable := 0
	for _, f := range st.Fields {
		if tableSettable(f) {
			settable++
		}
	}
	if settable == 0 {
		g.pf("    // no directly-settable fields: nested tables and arrays edit through\n")
		g.pf("    // their own descriptors and accessors\n")
		g.pf("    return false;\n}\n\n")
		return
	}
	g.pf("    switch (field)\n    {\n")
	for _, f := range st.Fields {
		if tableSettable(f) {
			g.emitTableSetField(f)
		}
	}
	g.pf("    }\n    return false;\n}\n\n")
}

func (g *tableGen) emitTableSetField(f *ir.Field) {
	ind := "            "
	name := ir.GoExportName(f.Name)
	g.pf("        case %q:\n        {\n", f.Name)
	switch f.Type.Kind {
	case ir.TBool:
		g.pf("%sif (v.Kind != TableValueKind.Bool)\n%s{\n%s    return false;\n%s}\n", ind, ind, ind, ind)
		g.pf("%svalue.%s = v.B;\n%sreturn true;\n", ind, name, ind)
	case ir.TString:
		g.pf("%sif (v.Kind != TableValueKind.String || v.S == null)\n%s{\n%s    return false;\n%s}\n", ind, ind, ind, ind)
		g.pf("%sbyte[] encoded = System.Text.Encoding.UTF8.GetBytes(v.S);\n", ind)
		g.pf("%sint keep = encoded.Length;\n", ind)
		g.pf("%sif (keep > %d)\n%s{\n%s    keep = %d; // truncate to the declared max, as the read side does\n%s}\n", ind, f.Type.Size, ind, ind, f.Type.Size, ind)
		g.pf("%sSystem.Array.Copy(encoded, value.%s, keep);\n", ind, name)
		g.pf("%svalue.%sLength = keep;\n%sreturn true;\n", ind, name, ind)
	case ir.TFloat32, ir.TFloat64:
		g.pf("%sdouble n;\n%sif (!v.AsDouble(out n))\n%s{\n%s    return false;\n%s}\n", ind, ind, ind, ind, ind)
		if f.HasFloatRange {
			g.emitTableSetClamp(ind, formatFloat(f.FMin), formatFloat(f.FMax), "")
		}
		if f.Type.Kind == ir.TFloat32 {
			g.pf("%svalue.%s = (float)n;\n%sreturn true;\n", ind, name, ind)
		} else {
			g.pf("%svalue.%s = n;\n%sreturn true;\n", ind, name, ind)
		}
	case ir.TInt:
		if f.Type.Signed {
			g.pf("%slong n;\n%sif (!v.AsLong(out n))\n%s{\n%s    return false;\n%s}\n", ind, ind, ind, ind, ind)
			if f.HasIntRange {
				g.emitTableSetClamp(ind, tableIntLit(f.IntMin, true, 8), tableIntLit(f.IntMax, true, 8), "")
			}
			g.emitTableSetAssign(ind, name, csInt(f.Type.Width), "long")
		} else {
			g.pf("%sulong n;\n%sif (!v.AsUlong(out n))\n%s{\n%s    return false;\n%s}\n", ind, ind, ind, ind, ind)
			if f.HasIntRange {
				min := ""
				if f.IntMin.Sign() > 0 {
					min = tableIntLit(f.IntMin, false, 8)
				}
				g.emitTableSetClamp(ind, min, tableIntLit(f.IntMax, false, 8), "")
			}
			g.emitTableSetAssign(ind, name, csUint(f.Type.Width), "ulong")
		}
	case ir.TBits:
		g.pf("%sulong n;\n%sif (!v.AsUlong(out n))\n%s{\n%s    return false;\n%s}\n", ind, ind, ind, ind, ind)
		storage := "uint"
		storageBits := 32
		if f.Type.Width > 32 {
			storage = "ulong"
			storageBits = 64
		}
		if f.Type.Width < storageBits {
			maxv := (uint64(1) << f.Type.Width) - 1
			g.emitTableSetClamp(ind, "", fmt.Sprintf("%dUL", maxv), fmt.Sprintf(" // bits(%d) width clamp", f.Type.Width))
		}
		g.emitTableSetAssign(ind, name, storage, "ulong")
	case ir.TNamed:
		g.pf("%sulong n;\n%sif (!v.AsUlong(out n))\n%s{\n%s    return false;\n%s}\n", ind, ind, ind, ind, ind)
		if enum, isEnum := f.Type.Ref.(*ir.Enum); isEnum {
			g.pf("%sif (n > %dUL)\n%s{\n%s    n = 0; // out-of-set -> None, as the read side does\n%s}\n", ind, enum.Max, ind, ind, ind)
			g.pf("%svalue.%s = (%s)n;\n%sreturn true;\n", ind, name, f.Type.Name, ind)
		} else { // flags: plain ulong storage, no clamp — as the read side
			g.pf("%svalue.%s = n;\n%sreturn true;\n", ind, name, ind)
		}
	}
	g.pf("        }\n")
}

// emitTableSetClamp clamps n to [min, max]; an empty min means the low side
// cannot underflow (unsigned with a non-positive declared min).
func (g *tableGen) emitTableSetClamp(ind, min, max, note string) {
	if min != "" {
		g.pf("%sif (n < %s)\n%s{\n%s    n = %s;\n%s}\n", ind, min, ind, ind, min, ind)
		g.pf("%selse if (n > %s)\n%s{\n%s    n = %s;%s\n%s}\n", ind, max, ind, ind, max, note, ind)
		return
	}
	g.pf("%sif (n > %s)\n%s{\n%s    n = %s;%s\n%s}\n", ind, max, ind, ind, max, note, ind)
}

// emitTableSetAssign writes the normalized n into the storage member, casting
// only where the storage type is narrower than the normalization domain.
func (g *tableGen) emitTableSetAssign(ind, name, storage, normType string) {
	if storage == normType {
		g.pf("%svalue.%s = n;\n%sreturn true;\n", ind, name, ind)
		return
	}
	g.pf("%svalue.%s = (%s)n;\n%sreturn true;\n", ind, name, storage, ind)
}

// tableRuntime is TableRuntime.cs: the report type plus the byte-level
// writer/reader the generated table functions share.
func tableRuntime(ns string) string {
	// block namespace: Unity's compiler is C# 9 (no file-scoped namespaces)
	header := `// Generated by the schema compiler. DO NOT EDIT.
// The TABLE wire runtime (evolution-tolerant, notes/table-wire.md): plain
// little-endian byte code over byte[], using System only — no serialize
// dependency, no reflection, no unsafe, no LINQ (Unity/IL2CPP-safe).

using System;

`
	body := `// TableReport counts the permissive read contract's events: how far the data
// diverged from this build's schema, without anything crashing or rejecting.
public sealed class TableReport
{
    public int Unknown;      // fields this schema does not declare (newer data)
    public int KindMismatch; // fields whose wire kind changed (skipped, defaults kept)
    public int Clamped;      // values pulled into declared ranges / sets / bounds
    public bool Malformed;   // structurally broken buffer; partial decode was kept
}

// ---- reflection (tables only, notes/table-wire.md) ----
//
// Static field descriptors for every type in the table closure: name, wire
// id/kind, bounds, ranges, enum names and branch guards — enough to walk,
// print, diff, edit or bind any table value at runtime with no schema files
// and no System.Reflection. Data-oriented by ruling (SPEC §4.11): reflection
// data stays SEPARATE from table data — TableFieldInfo is a struct and each
// type's descriptor is ONE flat TableFieldInfo[], contiguous value elements
// walked sequentially, built once, shared by every instance, zero
// per-instance weight. Walk without copies:
//
//     TableFieldInfo[] fields = Schema.TableTypeX().Fields;
//     for (int i = 0; i < fields.Length; i++)
//     {
//         ref readonly var f = ref fields[i];
//         // f.Name, f.Kind, f.Guard, ...
//     }
//
// Where C++ hands out storage offsets, C# hands out generated accessors:
// Schema.TableGetX/TableSetX read and write fields by name through the
// TableValue variant below — no boxing on the numeric and bool paths.

// TableFieldInfo describes one field of a table-closure type.
public struct TableFieldInfo
{
    public string Name;                  // schema field name, e.g. "health"
    public string TypeName;              // schema type name, e.g. "float32", "ShipType"
    public ushort Id;                    // table-wire field id (name hash)
    public byte Kind;                    // table-wire kind; for arrays/bytes, the ELEMENT kind
    public bool IsArray;                 // fixed or counted array (bytes included)
    public bool Counted;                 // a Count/Length companion exists (counted arrays, strings, bytes)
    public int ArrayBound;               // array capacity / string max length; 0 for plain scalars
    public TableTypeInfo Table;          // nested table's descriptor, or null
    public bool HasRange;                // a declared [min, max] (int or float)
    public double RangeMin;              // NOTE: int64 ranges beyond 2^53 lose precision here
    public double RangeMax;
    public long EnumMax;                 // enums: highest valid wire value (None = 0 always valid); else -1
    public Func<ulong, string> EnumName; // enums: value -> name; else null
    public string Guard;                 // branch guard, e.g. "at_rest" or "!at_rest"; "" if unguarded
}

// TableTypeInfo is one closure type's descriptor: the schema type name over
// the flat field array. One instance per type — Schema.TableTypeX() returns
// it, and nested-field descriptors link the same instances.
public sealed class TableTypeInfo
{
    public string Name; // schema type name
    public TableFieldInfo[] Fields;
}

// TableValueKind tags which TableValue channel is live.
public enum TableValueKind : byte
{
    None = 0,
    Bool = 1,   // -> B
    Int = 2,    // signed integers -> I
    Uint = 3,   // unsigned integers, bits, enums, flags -> U
    Float = 4,  // float32/float64 -> F
    String = 5, // -> S
    Table = 6,  // nested table reference -> Obj
    Array = 7,  // array reference -> Obj, used element count -> Count
}

// TableValue is the variant the generated TableGetX/TableSetX accessors move
// values through — a plain struct: numerics and bools travel in value fields
// (no boxing, no allocation), strings in S (System.String must allocate),
// nested tables and arrays as references in Obj. Counted arrays pair the raw
// member array in Obj with the used element count in Count — reference plus
// int, no ArraySegment box, no copy.
public struct TableValue
{
    public TableValueKind Kind;
    public bool B;
    public long I;
    public ulong U;
    public double F;
    public string S;
    public object Obj;
    public int Count;

    // From* build the editor-path values TableSetX consumes — plain struct
    // returns, no allocation.
    public static TableValue FromBool(bool value)
    {
        TableValue result = new TableValue();
        result.Kind = TableValueKind.Bool;
        result.B = value;
        return result;
    }

    public static TableValue FromInt(long value)
    {
        TableValue result = new TableValue();
        result.Kind = TableValueKind.Int;
        result.I = value;
        return result;
    }

    public static TableValue FromUint(ulong value)
    {
        TableValue result = new TableValue();
        result.Kind = TableValueKind.Uint;
        result.U = value;
        return result;
    }

    public static TableValue FromFloat(double value)
    {
        TableValue result = new TableValue();
        result.Kind = TableValueKind.Float;
        result.F = value;
        return result;
    }

    public static TableValue FromString(string value)
    {
        TableValue result = new TableValue();
        result.Kind = TableValueKind.String;
        result.S = value;
        return result;
    }

    // As* normalize the three numeric kinds into one domain for the generated
    // set path (plain casts — the declared-range clamps follow per field);
    // false = not a numeric. readonly members: no defensive copies through
    // the accessors' in parameters.
    public readonly bool AsLong(out long result)
    {
        switch (Kind)
        {
            case TableValueKind.Int:
                result = I;
                return true;
            case TableValueKind.Uint:
                result = (long)U;
                return true;
            case TableValueKind.Float:
                result = (long)F;
                return true;
        }
        result = 0;
        return false;
    }

    public readonly bool AsUlong(out ulong result)
    {
        switch (Kind)
        {
            case TableValueKind.Int:
                result = (ulong)I;
                return true;
            case TableValueKind.Uint:
                result = U;
                return true;
            case TableValueKind.Float:
                result = (ulong)F;
                return true;
        }
        result = 0;
        return false;
    }

    public readonly bool AsDouble(out double result)
    {
        switch (Kind)
        {
            case TableValueKind.Int:
                result = I;
                return true;
            case TableValueKind.Uint:
                result = U;
                return true;
            case TableValueKind.Float:
                result = F;
                return true;
        }
        result = 0.0;
        return false;
    }
}

// TableWriter is the generated writers' growable little-endian buffer —
// array doubling, floats via BitConverter's bit casts (byte-identical
// little-endian on every platform the runtime targets).
// TableWriter is the reusable table-wire write buffer. AppendTableX writes
// into it; the wire is Buf[0 .. Len). Hold one, Clear() between uses, and
// steady-state writes never allocate once the buffer has grown — the Unity
// per-frame path. TableWriteX is the allocating convenience on top.
public sealed class TableWriter
{
    public byte[] Buf = new byte[256];
    public int Len;

    // Clear resets the length and keeps the grown capacity.
    public void Clear()
    {
        Len = 0;
    }

    private void Reserve(int bytes)
    {
        if (Len + bytes <= Buf.Length)
        {
            return;
        }
        int capacity = Buf.Length * 2;
        while (capacity < Len + bytes)
        {
            capacity *= 2;
        }
        Array.Resize(ref Buf, capacity);
    }

    public void U8(byte v)
    {
        Reserve(1);
        Buf[Len] = v;
        Len += 1;
    }

    public void U16(ushort v)
    {
        Reserve(2);
        Buf[Len] = (byte)v;
        Buf[Len + 1] = (byte)(v >> 8);
        Len += 2;
    }

    public void U32(uint v)
    {
        Reserve(4);
        Buf[Len] = (byte)v;
        Buf[Len + 1] = (byte)(v >> 8);
        Buf[Len + 2] = (byte)(v >> 16);
        Buf[Len + 3] = (byte)(v >> 24);
        Len += 4;
    }

    public void U64(ulong v)
    {
        U32((uint)v);
        U32((uint)(v >> 32));
    }

    public void F32(float v)
    {
        U32((uint)BitConverter.SingleToInt32Bits(v));
    }

    public void F64(double v)
    {
        U64((ulong)BitConverter.DoubleToInt64Bits(v));
    }

    public void Raw(byte[] data, int count)
    {
        Reserve(count);
        Array.Copy(data, 0, Buf, Len, count);
        Len += count;
    }

    public void Patch32(int at, uint v)
    {
        Buf[at] = (byte)v;
        Buf[at + 1] = (byte)(v >> 8);
        Buf[at + 2] = (byte)(v >> 16);
        Buf[at + 3] = (byte)(v >> 24);
    }

    public byte[] ToArray()
    {
        byte[] wire = new byte[Len];
        Array.Copy(Buf, wire, Len);
        return wire;
    }
}

// TableReader walks one table value inside [Off, End) — nested tables get a
// sub-reader over the same buffer, bounded by their length prefix.
internal sealed class TableReader
{
    public byte[] Buf;
    public int Off;
    public int End;
    public TableReport Report;

    public TableReader(byte[] buf, int off, int end, TableReport report)
    {
        Buf = buf;
        Off = off;
        End = end;
        Report = report;
    }

    public bool Has(int bytes)
    {
        return bytes >= 0 && bytes <= End - Off;
    }

    public byte Get8()
    {
        byte v = Buf[Off];
        Off += 1;
        return v;
    }

    public ushort Get16()
    {
        ushort v = (ushort)(Buf[Off] | (Buf[Off + 1] << 8));
        Off += 2;
        return v;
    }

    public uint Get32()
    {
        uint v = (uint)(Buf[Off] | (Buf[Off + 1] << 8) | (Buf[Off + 2] << 16) | (Buf[Off + 3] << 24));
        Off += 4;
        return v;
    }

    public ulong Get64()
    {
        ulong lo = Get32();
        ulong hi = Get32();
        return lo | (hi << 32);
    }

    public float GetF32()
    {
        return BitConverter.Int32BitsToSingle((int)Get32());
    }

    public double GetF64()
    {
        return BitConverter.Int64BitsToDouble((long)Get64());
    }

    // CopyOut copies the next count bytes without advancing — the caller
    // advances by the full wire length, past any truncated excess.
    public void CopyOut(byte[] dst, int count)
    {
        Array.Copy(Buf, Off, dst, 0, count);
    }

    // Skip advances past one field payload of the given kind — how unknown
    // and kind-changed fields stay harmless.
    public bool Skip(byte kind)
    {
        switch (kind)
        {
            case 1: // bool
            case 2: // i8
            case 6: // u8
            {
                if (!Has(1))
                {
                    return false;
                }
                Off += 1;
                return true;
            }
            case 3: // i16
            case 7: // u16
            {
                if (!Has(2))
                {
                    return false;
                }
                Off += 2;
                return true;
            }
            case 4: // i32
            case 8: // u32
            case 10: // f32
            {
                if (!Has(4))
                {
                    return false;
                }
                Off += 4;
                return true;
            }
            case 5: // i64
            case 9: // u64
            case 11: // f64
            {
                if (!Has(8))
                {
                    return false;
                }
                Off += 8;
                return true;
            }
            case 12: // string: u16 length + bytes
            {
                if (!Has(2))
                {
                    return false;
                }
                int stringLen = Get16();
                if (!Has(stringLen))
                {
                    return false;
                }
                Off += stringLen;
                return true;
            }
            case 13: // table: u32 length + body
            case 14: // array: u32 length + body
            {
                if (!Has(4))
                {
                    return false;
                }
                int bodyLen = (int)Get32();
                if (!Has(bodyLen))
                {
                    return false;
                }
                Off += bodyLen;
                return true;
            }
        }
        return false; // unknown kind: cannot know its length
    }
}
`
	return header + "namespace " + ns + "\n{\n" + indent4(body) + "\n}\n"
}

// GenerateTable emits <Base>Table.cs for every unit file, plus
// TableRuntime.cs. The caller (Generate) has already refused units the C#
// backend cannot carry (int128/uint128, fixed).
func GenerateTable(u *ir.Unit) (map[string][]byte, error) {
	if err := pack.CheckTableIds(u); err != nil {
		return nil, err
	}
	for _, f := range u.Files {
		if f.Base == "TableRuntime" {
			return nil, fmt.Errorf("schema file TableRuntime collides with the generated C# table runtime file; rename it")
		}
	}
	ns := capitalize(u.Package)
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
			g.pf("// One flat TableFieldInfo[] per closure type (SPEC §4.11: contiguous\n")
			g.pf("// structs, sequential walks, zero per-instance weight), built lazily on\n")
			g.pf("// first use: C# gives partial classes no cross-file static initializer\n")
			g.pf("// order, so an eager initializer could observe a not-yet-built nested\n")
			g.pf("// descriptor — the accessors compose in dependency order instead (the\n")
			g.pf("// closure is acyclic). A double-build under a race is benign: equivalent\n")
			g.pf("// data, last write wins.\n\n")
			for _, st := range members {
				g.emitTableDescriptor(st)
				g.emitTableGet(st)
				g.emitTableSet(st)
			}
		} else {
			g.pf("    // no tables declared or referenced in this file — codecs are emitted\n")
			g.pf("    // for the table closure only (`table` declarations and what they reach)\n")
		}
		var h strings.Builder
		fmt.Fprintf(&h, "// Generated by the schema compiler from %s.schema. DO NOT EDIT.\n", f.Base)
		fmt.Fprintf(&h, "// package %s — protocol id 0x%016x\n", u.Package, u.ProtocolId)
		h.WriteString("// The TABLE wire (evolution-tolerant, notes/table-wire.md): plain byte\n")
		h.WriteString("// code, no serialize dependency — Unity/IL2CPP-safe.\n\n")
		// block namespace: Unity's compiler is C# 9 (no file-scoped namespaces)
		fmt.Fprintf(&h, "namespace %s\n{\n", ns)
		if g.body.Len() > 0 {
			var body strings.Builder
			body.WriteString("\n// The table codecs live on the same partial Schema class as the rest of\n")
			body.WriteString("// the generated surface (SPEC §6.1 naming) — one slice per generated file.\n")
			body.WriteString("public static partial class Schema\n{\n")
			body.WriteString(indent4(g.body.String()))
			body.WriteString("}\n")
			h.WriteString(indent4(body.String()))
			h.WriteString("\n")
		}
		h.WriteString("}\n")
		out[f.Base+"Table.cs"] = []byte(h.String())
	}
	out["TableRuntime.cs"] = []byte(tableRuntime(ns))
	return out, nil
}
