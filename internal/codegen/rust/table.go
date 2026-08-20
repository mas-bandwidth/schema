package rust

// The TABLE wire backend for Rust — the evolution-tolerant encoding described
// in WIRES.md, previously implemented only for C++, C# and Go.
//
// The wire is byte-for-byte the same one the other three emit; this file is a
// port of the emit rules, not a new format. Every constant, every field order
// and every skip rule mirrors internal/codegen/golang/table.go, which is the
// closest structural analogue (both emit their own runtime rather than calling
// into a hand-written one). Where they differ it is Rust surface only: snake
// case, slices instead of pointer+length, Option instead of nil.
//
// Two things here are Rust-specific and load-bearing:
//
//   - Storage gets #[repr(C)]. The table wire promises relocatable storage —
//     memcpy-able, memory-mappable, shareable across processes. Rust's default
//     representation guarantees no such thing: the compiler may reorder fields
//     and the layout is not stable between builds. Copy alone is not enough,
//     and a struct that is merely Copy would honour the promise by accident
//     until the day it did not.
//
//   - The reflection descriptors are 'static data, not constructed at call
//     time, so TableType* costs nothing per instance — the same property the
//     C++ flat arrays have.

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/mas-bandwidth/schema/ir"
)

// table-wire kinds — mirror internal/pack/table.go.
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

// tableWireRust is the unsigned wire-width Rust integer a kind travels as.
func tableWireRust(kind int) string {
	return fmt.Sprintf("u%d", tableKindWidth(kind)*8)
}

// tableSignedRust is the signed Rust integer of a kind's width.
func tableSignedRust(kind int) string {
	return fmt.Sprintf("i%d", tableKindWidth(kind)*8)
}

// tableStorageRust is the generated storage type a scalar field decodes into.
func (g *tableGen) tableStorageRust(f *ir.Field, kind int) string {
	switch f.Type.Kind {
	case ir.TInt:
		if f.Type.Signed {
			return fmt.Sprintf("i%d", f.Type.Width)
		}
		return fmt.Sprintf("u%d", f.Type.Width)
	case ir.TBits:
		if f.Type.Width <= 32 {
			return "u32"
		}
		return "u64"
	case ir.TFloat32:
		return "f32"
	case ir.TFloat64:
		return "f64"
	case ir.TBool:
		return "bool"
	case ir.TNamed:
		return f.Type.Name
	}
	if kind >= tkI8 && kind <= tkI64 {
		return tableSignedRust(kind)
	}
	return tableWireRust(kind)
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

// tableGuardExprs composes each guarded field's branch condition from the wire
// tree, so the writer keeps untaken-branch fields off the wire exactly as the
// other three backends do.
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
				name := item.Cond
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

// ---- write ----

func (g *tableGen) emitTableWrite(st *ir.Struct) {
	lower := ir.RustSnake(st.Name)
	g.pf("/// Returns `value`'s table-wire encoding in a fresh buffer.\n")
	g.pf("pub fn table_write_%s(value: &%s) -> Vec<u8> {\n", lower, st.Name)
	g.pf("    append_table_%s(Vec::new(), value)\n}\n\n", lower)

	g.pf("/// Appends `value`'s table-wire encoding to `dst` and returns the extended\n")
	g.pf("/// buffer — the zero-allocation write path: hand it a buffer you reuse\n")
	g.pf("/// (`v.clear()` then pass `v`) and steady-state writes never touch the heap.\n")
	g.pf("pub fn append_table_%s(dst: Vec<u8>, value: &%s) -> Vec<u8> {\n", lower, st.Name)
	g.pf("    let mut w = TableWriter { buf: dst };\n")
	g.pf("    table_write_%s_into(&mut w, value);\n", lower)
	g.pf("    w.buf\n}\n\n")

	g.pf("fn table_write_%s_into(w: &mut TableWriter, value: &%s) {\n", lower, st.Name)
	guards := tableGuardExprs(st)
	for _, f := range st.Fields {
		if cond, guarded := guards[f.Name]; guarded {
			g.pf("    if %s {\n", cond)
			g.indent = "    "
			g.emitTableWriteField(f)
			g.indent = ""
			g.pf("    }\n")
			continue
		}
		g.emitTableWriteField(f)
	}
	g.pf("    w.u16(0); // terminator\n")
	g.pf("}\n\n")
}

func (g *tableGen) emitTableWriteField(f *ir.Field) {
	id := ir.FieldId(f.Name)
	kind := tableScalarKind(f)
	name := f.Name
	switch {
	case f.Type.Kind == ir.TString:
		g.pf("    if value.%s_length > 0 {\n", name)
		g.pf("        w.u16(0x%04x); // %s\n", id, f.Name)
		g.pf("        w.u8(%d);\n", tkString)
		g.pf("        w.u16(value.%s_length as u16);\n", name)
		g.pf("        w.raw(&value.%s[..value.%s_length as usize]);\n    }\n", name, name)
	case f.Type.Kind == ir.TBytes:
		g.pf("    if value.%s_length > 0 {\n", name)
		g.pf("        w.u16(0x%04x); // %s\n", id, f.Name)
		g.pf("        w.u8(%d);\n", tkArray)
		g.pf("        w.u32(3 + value.%s_length as u32);\n", name)
		g.pf("        w.u8(%d);\n", tkU8)
		g.pf("        w.u16(value.%s_length as u16);\n", name)
		g.pf("        w.raw(&value.%s[..value.%s_length as usize]);\n    }\n", name, name)
	case f.Array == ir.ArrayCounted:
		g.pf("    if value.%s_count > 0 {\n", name)
		g.pf("        w.u16(0x%04x); // %s\n", id, f.Name)
		g.pf("        w.u8(%d);\n", tkArray)
		g.pf("        let len_at = w.buf.len();\n")
		g.pf("        w.u32(0);\n")
		g.pf("        w.u8(%d);\n", kind)
		g.pf("        w.u16(value.%s_count as u16);\n", name)
		g.pf("        for i in 0..value.%s_count as usize {\n", name)
		g.emitTableWriteElement(f, kind, fmt.Sprintf("value.%s[i]", name), "            ")
		g.pf("        }\n")
		g.pf("        let body = (w.buf.len() - len_at - 4) as u32;\n")
		g.pf("        w.patch32(len_at, body);\n    }\n")
	case f.Array == ir.ArrayFixed && kind == tkTable:
		// fixed arrays of tables always ride — parity with the C++ writer
		g.pf("    {\n")
		g.pf("        w.u16(0x%04x); // %s (fixed [%d])\n", id, f.Name, f.ArrayBound)
		g.pf("        w.u8(%d);\n", tkArray)
		g.pf("        let len_at = w.buf.len();\n")
		g.pf("        w.u32(0);\n")
		g.pf("        w.u8(%d);\n", kind)
		g.pf("        w.u16(%d);\n", f.ArrayBound)
		g.pf("        for i in 0..%d {\n", f.ArrayBound)
		g.emitTableWriteElement(f, kind, fmt.Sprintf("value.%s[i]", name), "            ")
		g.pf("        }\n")
		g.pf("        let body = (w.buf.len() - len_at - 4) as u32;\n")
		g.pf("        w.patch32(len_at, body);\n    }\n")
	case f.Array == ir.ArrayFixed:
		// fixed arrays are positional; an all-default array elides entirely
		g.pf("    {\n")
		g.pf("        let mut all_default = true;\n")
		g.pf("        for i in 0..%d {\n", f.ArrayBound)
		g.pf("            if value.%s[i] != %s { all_default = false; break; }\n", name, g.tableDefaultExpr(f))
		g.pf("        }\n")
		g.pf("        if !all_default {\n")
		g.pf("            w.u16(0x%04x); // %s (fixed [%d])\n", id, f.Name, f.ArrayBound)
		g.pf("            w.u8(%d);\n", tkArray)
		g.pf("            let len_at = w.buf.len();\n")
		g.pf("            w.u32(0);\n")
		g.pf("            w.u8(%d);\n", kind)
		g.pf("            w.u16(%d);\n", f.ArrayBound)
		g.pf("            for i in 0..%d {\n", f.ArrayBound)
		g.emitTableWriteElement(f, kind, fmt.Sprintf("value.%s[i]", name), "                ")
		g.pf("            }\n")
		g.pf("            let body = (w.buf.len() - len_at - 4) as u32;\n")
		g.pf("            w.patch32(len_at, body);\n        }\n    }\n")
	case kind == tkTable:
		g.pf("    {\n")
		g.pf("        let field_at = w.buf.len();\n")
		g.pf("        w.u16(0x%04x); // %s\n", id, f.Name)
		g.pf("        w.u8(%d);\n", tkTable)
		g.pf("        let len_at = w.buf.len();\n")
		g.pf("        w.u32(0);\n")
		g.pf("        table_write_%s_into(w, &value.%s);\n", ir.RustSnake(f.Type.Name), name)
		g.pf("        if w.buf.len() - len_at - 4 <= 2 {\n")
		g.pf("            w.buf.truncate(field_at); // all-default nested elides\n")
		g.pf("        } else {\n")
		g.pf("            let body = (w.buf.len() - len_at - 4) as u32;\n")
		g.pf("            w.patch32(len_at, body);\n        }\n    }\n")
	default:
		// a scalar at its default does not ride — the reader prefills defaults,
		// so omitting it is both smaller and exactly what the other three do
		g.pf("    if value.%s != %s {\n", name, g.tableDefaultExpr(f))
		g.pf("        w.u16(0x%04x); // %s\n", id, f.Name)
		g.pf("        w.u8(%d);\n", kind)
		g.emitTableWriteElement(f, kind, "value."+name, "        ")
		g.pf("    }\n")
	}
}

func (g *tableGen) emitTableWriteElement(f *ir.Field, kind int, expr, ind string) {
	switch kind {
	case tkBool:
		g.pf("%sw.u8(if %s { 1 } else { 0 });\n", ind, expr)
	case tkF32:
		g.pf("%sw.u32(f32::to_bits(%s));\n", ind, expr)
	case tkF64:
		g.pf("%sw.u64(f64::to_bits(%s));\n", ind, expr)
	case tkTable:
		g.pf("%s{\n", ind)
		g.pf("%s    let len_at = w.buf.len();\n", ind)
		g.pf("%s    w.u32(0);\n", ind)
		g.pf("%s    table_write_%s_into(w, &%s);\n", ind, ir.RustSnake(f.Type.Name), expr)
		g.pf("%s    let body = (w.buf.len() - len_at - 4) as u32;\n", ind)
		g.pf("%s    w.patch32(len_at, body);\n", ind)
		g.pf("%s}\n", ind)
	default:
		cast := tableWireRust(kind)
		// enums are repr'd integers; flags are already u64
		src := expr
		if f.Type.Kind == ir.TNamed {
			if _, isEnum := f.Type.Ref.(*ir.Enum); isEnum {
				src = expr + ".0" // enums are #[repr(transparent)] newtypes
			}
		}
		_ = cast
		switch tableKindWidth(kind) {
		case 1:
			g.pf("%sw.u8(%s as u8);\n", ind, src)
		case 2:
			g.pf("%sw.u16(%s as u16);\n", ind, src)
		case 4:
			g.pf("%sw.u32(%s as u32);\n", ind, src)
		default:
			g.pf("%sw.u64(%s as u64);\n", ind, src)
		}
	}
}

// ---- read ----

func (g *tableGen) emitTableRead(st *ir.Struct) {
	lower := ir.RustSnake(st.Name)
	g.pf("/// Decodes a table-wire buffer under the permissive contract: declared\n")
	g.pf("/// defaults prefill, known fields overlay, unknown ids and kind mismatches\n")
	g.pf("/// skip and count, out-of-range values clamp and count. `false` means\n")
	g.pf("/// malformed — the partial decode up to that point is kept.\n")
	g.pf("pub fn table_read_%s(data: &[u8], value: &mut %s, report: &mut TableReport) -> bool {\n", lower, st.Name)
	g.pf("    let mut r = TableReader { buf: data, off: 0 };\n")
	g.pf("    table_read_%s_from(&mut r, value, report)\n}\n\n", lower)

	g.pf("fn table_read_%s_from(r: &mut TableReader, value: &mut %s, report: &mut TableReport) -> bool {\n", lower, st.Name)
	if structHasDefaults(st) {
		g.pf("    *value = %s::new(); // prefill declared defaults, then overlay\n", st.Name)
	} else {
		g.pf("    *value = %s::default(); // prefill (all-zero defaults), then overlay\n", st.Name)
	}
	g.pf("    loop {\n")
	g.pf("        if !r.has(2) { report.malformed = true; return false; }\n")
	g.pf("        let field_id = r.get16();\n")
	g.pf("        if field_id == 0 { return true; }\n")
	g.pf("        if !r.has(1) { report.malformed = true; return false; }\n")
	g.pf("        let kind = r.get8();\n")
	g.pf("        match field_id {\n")
	for _, f := range st.Fields {
		id := ir.FieldId(f.Name)
		kind := tableScalarKind(f)
		wireKind := kind
		if f.Array != ir.ArrayNone || f.Type.Kind == ir.TBytes {
			wireKind = tkArray
		}
		if f.Type.Kind == ir.TBytes {
			kind = tkU8 // bytes travel as an array of u8 elements
		}
		g.pf("            0x%04x => { // %s\n", id, f.Name)
		g.pf("                if kind != %d {\n", wireKind)
		g.pf("                    report.kind_mismatch += 1;\n")
		g.pf("                    if !r.skip(kind) { report.malformed = true; return false; }\n")
		g.pf("                } else {\n")
		g.emitTableReadField(f, kind)
		g.pf("                }\n")
		g.pf("            }\n")
	}
	g.pf("            _ => {\n")
	g.pf("                report.unknown += 1;\n")
	g.pf("                if !r.skip(kind) { report.malformed = true; return false; }\n")
	g.pf("            }\n")
	g.pf("        }\n    }\n}\n\n")
}

func (g *tableGen) emitTableReadField(f *ir.Field, kind int) {
	ind := "                    "
	name := f.Name
	switch {
	case f.Type.Kind == ir.TString:
		g.pf("%sif !r.has(2) { report.malformed = true; return false; }\n", ind)
		g.pf("%slet length = r.get16() as usize;\n", ind)
		g.pf("%sif !r.has(length) { report.malformed = true; return false; }\n", ind)
		g.pf("%slet mut keep = length;\n", ind)
		g.pf("%sif keep > %d { keep = %d; report.clamped += 1; }\n", ind, f.Type.Size, f.Type.Size)
		g.pf("%svalue.%s[..keep].copy_from_slice(&r.buf[r.off..r.off + keep]);\n", ind, name)
		g.pf("%svalue.%s_length = keep as i32;\n", ind, name)
		g.pf("%sr.off += length;\n", ind)
	case f.Array != ir.ArrayNone || f.Type.Kind == ir.TBytes:
		bound := f.ArrayBound
		if f.Type.Kind == ir.TBytes {
			bound = f.Type.Size
		}
		g.pf("%sif !r.has(4) { report.malformed = true; return false; }\n", ind)
		g.pf("%slet body_len = r.get32() as usize;\n", ind)
		g.pf("%sif !r.has(body_len) { report.malformed = true; return false; }\n", ind)
		g.pf("%slet body_end = r.off + body_len;\n", ind)
		g.pf("%sif body_len >= 3 {\n", ind)
		g.pf("%s    let elem_kind = r.get8();\n", ind)
		g.pf("%s    let count = r.get16() as usize;\n", ind)
		g.pf("%s    if elem_kind != %d {\n", ind, kind)
		g.pf("%s        report.kind_mismatch += 1;\n", ind)
		g.pf("%s    } else {\n", ind)
		g.pf("%s        let mut keep = count;\n", ind)
		g.pf("%s        if keep > %d { keep = %d; report.clamped += 1; }\n", ind, bound, bound)
		g.pf("%s        for i in 0..keep {\n", ind)
		g.emitTableReadElement(f, kind, ind+"            ")
		g.pf("%s        }\n", ind)
		if f.Type.Kind == ir.TBytes {
			g.pf("%s        value.%s_length = keep as i32;\n", ind, name)
		} else if f.Array == ir.ArrayCounted {
			g.pf("%s        value.%s_count = keep as i32;\n", ind, name)
		}
		g.pf("%s    }\n", ind)
		g.pf("%s}\n", ind)
		g.pf("%sr.off = body_end; // excess elements and slack skip via the length\n", ind)
	case kind == tkTable:
		g.pf("%sif !r.has(4) { report.malformed = true; return false; }\n", ind)
		g.pf("%slet body_len = r.get32() as usize;\n", ind)
		g.pf("%sif !r.has(body_len) { report.malformed = true; return false; }\n", ind)
		g.pf("%s{\n", ind)
		g.pf("%s    let mut sub = TableReader { buf: &r.buf[r.off..r.off + body_len], off: 0 };\n", ind)
		g.pf("%s    table_read_%s_from(&mut sub, &mut value.%s, report);\n", ind, ir.RustSnake(f.Type.Name), name)
		g.pf("%s}\n", ind)
		g.pf("%sr.off += body_len;\n", ind)
	default:
		g.emitTableReadScalarInto(f, kind, "value."+name, ind)
	}
}

func (g *tableGen) emitTableReadElement(f *ir.Field, kind int, ind string) {
	name := f.Name
	switch kind {
	case tkTable:
		g.pf("%sif !r.has(4) { report.malformed = true; return false; }\n", ind)
		g.pf("%slet elem_len = r.get32() as usize;\n", ind)
		g.pf("%sif !r.has(elem_len) { report.malformed = true; return false; }\n", ind)
		g.pf("%s{\n", ind)
		g.pf("%s    let mut sub = TableReader { buf: &r.buf[r.off..r.off + elem_len], off: 0 };\n", ind)
		g.pf("%s    table_read_%s_from(&mut sub, &mut value.%s[i], report);\n", ind, ir.RustSnake(f.Type.Name), name)
		g.pf("%s}\n", ind)
		g.pf("%sr.off += elem_len;\n", ind)
	default:
		g.emitTableReadScalarInto(f, kind, fmt.Sprintf("value.%s[i]", name), ind)
	}
}

func (g *tableGen) emitTableReadScalarInto(f *ir.Field, kind int, lvalue, ind string) {
	width := tableKindWidth(kind)
	g.pf("%sif !r.has(%d) { report.malformed = true; return false; }\n", ind, width)
	switch kind {
	case tkBool:
		g.pf("%s%s = r.get8() != 0;\n", ind, lvalue)
		return
	case tkF32:
		g.pf("%slet raw = f32::from_bits(r.get32());\n", ind)
		g.emitClampFloat(f, "raw", lvalue, ind, "f32")
		return
	case tkF64:
		g.pf("%slet raw = f64::from_bits(r.get64());\n", ind)
		g.emitClampFloat(f, "raw", lvalue, ind, "f64")
		return
	}

	get := map[int]string{1: "r.get8()", 2: "r.get16()", 4: "r.get32()", 8: "r.get64()"}[width]
	signed := kind >= tkI8 && kind <= tkI64
	if signed {
		g.pf("%slet raw = %s as %s;\n", ind, get, tableSignedRust(kind))
	} else {
		g.pf("%slet raw = %s;\n", ind, get)
	}
	g.emitClampInt(f, kind, "raw", lvalue, ind)
}

// emitClampInt applies the declared range (or enum variant set) before storing,
// counting a clamp in the report — the permissive contract.
func (g *tableGen) emitClampInt(f *ir.Field, kind int, src, lvalue, ind string) {
	storage := g.tableStorageRust(f, kind)

	if f.Type.Kind == ir.TNamed {
		if e, isEnum := f.Type.Ref.(*ir.Enum); isEnum {
			// Enum.Max is the top WIRE value — the variant count, or the
			// [max = K] widening. Values above it are not wire-legal, so they
			// clamp to None (0) and count, matching the other three backends.
			g.pf("%slet mut v = %s;\n", ind, src)
			g.pf("%sif v as u64 > %d { v = 0; report.clamped += 1; }\n", ind, e.Max)
			g.pf("%s%s = %s(v as %s);\n", ind, lvalue, f.Type.Name, enumStorageRust(e))
			return
		}
		if _, isFlags := f.Type.Ref.(*ir.Flags); isFlags {
			g.pf("%s%s = %s as %s;\n", ind, lvalue, src, storage)
			return
		}
	}

	lo, hi, has := fieldRange(f)
	if !has {
		g.pf("%s%s = %s as %s;\n", ind, lvalue, src, storage)
		return
	}
	g.pf("%slet mut v = %s;\n", ind, src)
	g.pf("%sif (v as i128) < %s { v = %s as _; report.clamped += 1; }\n", ind, lo, lo)
	g.pf("%sif (v as i128) > %s { v = %s as _; report.clamped += 1; }\n", ind, hi, hi)
	g.pf("%s%s = v as %s;\n", ind, lvalue, storage)
}

func (g *tableGen) emitClampFloat(f *ir.Field, src, lvalue, ind, ty string) {
	lo, hi, has := fieldRangeFloat(f)
	if !has {
		g.pf("%s%s = %s;\n", ind, lvalue, src)
		return
	}
	g.pf("%slet mut v = %s;\n", ind, src)
	g.pf("%sif v < %s as %s { v = %s as %s; report.clamped += 1; }\n", ind, lo, ty, lo, ty)
	g.pf("%sif v > %s as %s { v = %s as %s; report.clamped += 1; }\n", ind, hi, ty, hi, ty)
	g.pf("%s%s = v;\n", ind, lvalue)
}

// fieldRange renders the declared integer bounds as i128 literals, or reports
// that the field is unbounded.
func fieldRange(f *ir.Field) (string, string, bool) {
	if !f.HasIntRange || f.IntMin == nil || f.IntMax == nil {
		return "", "", false
	}
	return bigLit(f.IntMin), bigLit(f.IntMax), true
}

func fieldRangeFloat(f *ir.Field) (string, string, bool) {
	if !f.HasFloatRange {
		return "", "", false
	}
	return fmt.Sprintf("%v", f.FMin), fmt.Sprintf("%v", f.FMax), true
}

func bigLit(v *big.Int) string {
	return v.String() + "i128"
}

// enumStorageRust is the newtype's inner integer — smallest unsigned fitting Max.
func enumStorageRust(e *ir.Enum) string {
	return fmt.Sprintf("u%d", e.StorageBits)
}

// ---- the runtime module ----

// tableRuntime is the generated table-wire runtime: the byte-level writer and
// reader, the report, and the skip rule that makes unknown and kind-changed
// fields harmless. It is emitted rather than kept in serialize.rs because the
// table wire has no bit-level work — it is plain little-endian bytes, and a
// dependency for that would buy nothing.
func tableRuntime() string {
	return `// Generated by the schema compiler. DO NOT EDIT.
// SPDX-License-Identifier: NONE — this generated output is yours, under terms of
// your choice. See the LICENSE exception in the schema compiler; the compiler is
// AGPL-3.0, its output is not.
// The TABLE wire runtime (evolution-tolerant, notes/table-wire.md).

/// Counts the permissive read contract's events: how far the data diverged from
/// this build's schema, without anything crashing or rejecting.
#[derive(Clone, Copy, PartialEq, Eq, Debug, Default)]
pub struct TableReport {
    /// fields this schema does not declare (newer data)
    pub unknown: i32,
    /// fields whose wire kind changed (skipped, defaults kept)
    pub kind_mismatch: i32,
    /// values pulled into declared ranges / sets / bounds
    pub clamped: i32,
    /// structurally broken buffer; partial decode was kept
    pub malformed: bool,
}

// ---- reflection (tables only, notes/table-wire.md) ----
//
// Static field descriptors for every type in the table closure: name, wire
// id/kind, bounds, ranges, enum names and branch guards — enough to walk,
// print, diff, edit or bind any table value at runtime with no schema files.
// table_type_*() returns a type's descriptor.

/// Describes one field of a table-closure type.
#[derive(Clone, Copy, Debug)]
pub struct TableFieldInfo {
    /// schema field name, e.g. "health"
    pub name: &'static str,
    /// schema type name, e.g. "float32", "ShipType"
    pub type_name: &'static str,
    /// table-wire field id (name hash)
    pub id: u16,
    /// table-wire kind; for arrays/bytes, the ELEMENT kind
    pub kind: u8,
    /// fixed or counted array (bytes included)
    pub is_array: bool,
    /// a count/length companion exists (counted arrays, strings, bytes)
    pub counted: bool,
    /// array capacity / string max length; 0 for plain scalars
    pub array_bound: i64,
    /// nested table's descriptor
    pub table: Option<&'static TableTypeInfo>,
    /// a declared [min, max] (int or float)
    pub has_range: bool,
    /// NOTE: i64 ranges beyond 2^53 lose precision here
    pub range_min: f64,
    pub range_max: f64,
    /// enums: highest valid wire value (None = 0 always valid); else -1
    pub enum_max: i64,
    /// enums: value -> name
    pub enum_name: Option<fn(u64) -> &'static str>,
    /// branch guard, e.g. "at_rest" or "!at_rest"; "" if unguarded
    pub guard: &'static str,
}

/// One closure type's descriptor.
#[derive(Clone, Copy, Debug)]
pub struct TableTypeInfo {
    /// schema type name
    pub name: &'static str,
    pub fields: &'static [TableFieldInfo],
}

pub(crate) struct TableWriter {
    pub(crate) buf: Vec<u8>,
}

impl TableWriter {
    #[inline]
    pub(crate) fn u8(&mut self, v: u8) {
        self.buf.push(v);
    }
    #[inline]
    pub(crate) fn u16(&mut self, v: u16) {
        self.buf.extend_from_slice(&v.to_le_bytes());
    }
    #[inline]
    pub(crate) fn u32(&mut self, v: u32) {
        self.buf.extend_from_slice(&v.to_le_bytes());
    }
    #[inline]
    pub(crate) fn u64(&mut self, v: u64) {
        self.buf.extend_from_slice(&v.to_le_bytes());
    }
    #[inline]
    pub(crate) fn raw(&mut self, b: &[u8]) {
        self.buf.extend_from_slice(b);
    }
    #[inline]
    pub(crate) fn patch32(&mut self, off: usize, v: u32) {
        self.buf[off..off + 4].copy_from_slice(&v.to_le_bytes());
    }
}

pub(crate) struct TableReader<'a> {
    pub(crate) buf: &'a [u8],
    pub(crate) off: usize,
}

impl<'a> TableReader<'a> {
    #[inline]
    pub(crate) fn has(&self, n: usize) -> bool {
        self.off + n <= self.buf.len()
    }
    #[inline]
    pub(crate) fn get8(&mut self) -> u8 {
        let v = self.buf[self.off];
        self.off += 1;
        v
    }
    #[inline]
    pub(crate) fn get16(&mut self) -> u16 {
        let v = u16::from_le_bytes([self.buf[self.off], self.buf[self.off + 1]]);
        self.off += 2;
        v
    }
    #[inline]
    pub(crate) fn get32(&mut self) -> u32 {
        let mut b = [0u8; 4];
        b.copy_from_slice(&self.buf[self.off..self.off + 4]);
        self.off += 4;
        u32::from_le_bytes(b)
    }
    #[inline]
    pub(crate) fn get64(&mut self) -> u64 {
        let mut b = [0u8; 8];
        b.copy_from_slice(&self.buf[self.off..self.off + 8]);
        self.off += 8;
        u64::from_le_bytes(b)
    }

    /// Advances past one field payload of the given kind — how unknown and
    /// kind-changed fields stay harmless.
    pub(crate) fn skip(&mut self, kind: u8) -> bool {
        match kind {
            1 | 2 | 6 => {
                // bool, i8, u8
                if !self.has(1) {
                    return false;
                }
                self.off += 1;
                true
            }
            3 | 7 => {
                // i16, u16
                if !self.has(2) {
                    return false;
                }
                self.off += 2;
                true
            }
            4 | 8 | 10 => {
                // i32, u32, f32
                if !self.has(4) {
                    return false;
                }
                self.off += 4;
                true
            }
            5 | 9 | 11 => {
                // i64, u64, f64
                if !self.has(8) {
                    return false;
                }
                self.off += 8;
                true
            }
            12 => {
                // string: u16 length then bytes
                if !self.has(2) {
                    return false;
                }
                let n = self.get16() as usize;
                if !self.has(n) {
                    return false;
                }
                self.off += n;
                true
            }
            13 | 14 => {
                // table / array: u32 body length then the body
                if !self.has(4) {
                    return false;
                }
                let n = self.get32() as usize;
                if !self.has(n) {
                    return false;
                }
                self.off += n;
                true
            }
            _ => false,
        }
    }
}
`
}

// GenerateTable emits the table-wire modules: one <base>_table.rs per schema
// file that declares or reaches a table, plus the shared table_runtime.rs.
//
// Rust modules are the lowercased basename (rust.Generate's rule), so the
// table module for Types.schema is types_table.rs — a suffix rather than a
// separate case, which keeps the module index in lib.rs mechanical.
func GenerateTable(u *ir.Unit) (map[string][]byte, error) {
	if err := ir.CheckTableIds(u); err != nil {
		return nil, err
	}
	for _, f := range u.Files {
		if strings.EqualFold(f.Base, "TableRuntime") {
			return nil, fmt.Errorf("schema file %s collides with the generated Rust table runtime module (table_runtime.rs); rename it", f.Base)
		}
		// <base>_table.rs is this emitter's output name, so a schema file that
		// already lowers to that name would be overwritten silently
		for _, other := range u.Files {
			if strings.EqualFold(f.Base, other.Base+"Table") {
				return nil, fmt.Errorf("schema files %s and %s collide — the Rust emitter writes %s_table.rs as %s's table codec; rename one file",
					other.Base, f.Base, strings.ToLower(other.Base), other.Base)
			}
		}
	}

	closure := ir.TableClosure(u)
	out := map[string][]byte{}
	for _, f := range u.Files {
		var members []*ir.Struct
		for _, d := range f.Decls {
			if st, ok := d.(*ir.Struct); ok && closure[st.Name] {
				members = append(members, st)
			}
		}
		if len(members) == 0 {
			continue // codecs are emitted for the table closure only
		}

		g := &tableGen{unit: u, file: f}
		for _, st := range members {
			g.emitTableWrite(st)
			g.emitTableRead(st)
		}
		g.pf("// ---- reflection descriptors (tables only, notes/table-wire.md) ----\n")
		g.pf("//\n")
		g.pf("// Descriptors are 'static data, so table_type_* costs nothing per instance\n")
		g.pf("// and needs no lazy initialization: Rust resolves the cross-type links at\n")
		g.pf("// compile time, which is the one place this is simpler than the C#/Go\n")
		g.pf("// backends, where the same links are wired at startup.\n\n")
		for _, st := range members {
			g.emitTableDescriptor(st)
		}

		var h strings.Builder
		fmt.Fprintf(&h, "// Generated by the schema compiler from %s.schema. DO NOT EDIT.\n// SPDX-License-Identifier: NONE — this generated output is yours, under terms of\n// your choice. See the LICENSE exception in the schema compiler; the compiler is\n// AGPL-3.0, its output is not.\n", f.Base)
		fmt.Fprintf(&h, "// package %s — protocol id 0x%016x\n", u.Package, u.ProtocolId)
		h.WriteString("// The TABLE wire (evolution-tolerant, notes/table-wire.md).\n\n")
		h.WriteString("#![allow(clippy::needless_range_loop)]\n\n")
		h.WriteString("use crate::*;\n\n")
		h.WriteString(g.body.String())
		out[strings.ToLower(f.Base)+"_table.rs"] = []byte(h.String())
	}

	if len(out) > 0 {
		out["table_runtime.rs"] = []byte(tableRuntime())
	}
	return out, nil
}

// ---- reflection descriptors ----

func (g *tableGen) emitTableDescriptor(st *ir.Struct) {
	snake := ir.RustSnake(st.Name)
	upper := strings.ToUpper(snake)
	guards := tableGuardStrings(st)

	g.pf("static TABLE_TYPE_%s_FIELDS: &[TableFieldInfo] = &[\n", upper)
	for _, f := range st.Fields {
		g.pf("    TableFieldInfo {\n")
		for _, part := range g.tableDescriptorParts(f, guards[f.Name]) {
			g.pf("        %s,\n", part)
		}
		g.pf("    },\n")
	}
	g.pf("];\n\n")

	g.pf("static TABLE_TYPE_%s: TableTypeInfo = TableTypeInfo {\n", upper)
	g.pf("    name: %q,\n", st.Name)
	g.pf("    fields: TABLE_TYPE_%s_FIELDS,\n", upper)
	g.pf("};\n\n")

	g.pf("/// Returns `%s`'s reflection descriptor — field names, wire ids/kinds,\n", st.Name)
	g.pf("/// bounds, ranges, enum names and branch guards. Static data: no\n")
	g.pf("/// per-instance cost and no lazy initialization.\n")
	g.pf("pub fn table_type_%s() -> &'static TableTypeInfo { &TABLE_TYPE_%s }\n\n", snake, upper)
}

// tableDescriptorParts renders one field's TableFieldInfo literal. Rust has no
// zero-value elision for struct literals, so unlike the Go emitter every member
// is written explicitly.
func (g *tableGen) tableDescriptorParts(f *ir.Field, guard string) []string {
	kind := tableScalarKind(f)
	if f.Type.Kind == ir.TBytes {
		kind = tkU8 // bytes surface as an array of u8 elements
	}

	isArray := f.Array != ir.ArrayNone || f.Type.Kind == ir.TBytes
	counted := f.Array == ir.ArrayCounted || f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TString

	arrayBound := int64(0)
	switch {
	case f.Array != ir.ArrayNone:
		arrayBound = f.ArrayBound
	case f.Type.Kind == ir.TBytes, f.Type.Kind == ir.TString:
		arrayBound = f.Type.Size
	}

	table := "None"
	if _, isStruct := f.Type.Ref.(*ir.Struct); f.Type.Kind == ir.TNamed && isStruct {
		table = fmt.Sprintf("Some(&TABLE_TYPE_%s)", strings.ToUpper(ir.RustSnake(f.Type.Name)))
	}

	hasRange, rangeMin, rangeMax := "false", "0.0", "0.0"
	if f.HasIntRange {
		hasRange = "true"
		rangeMin = formatFloatRust(bigToFloat64(f.IntMin))
		rangeMax = formatFloatRust(bigToFloat64(f.IntMax))
	} else if f.HasFloatRange {
		hasRange = "true"
		rangeMin = formatFloatRust(f.FMin)
		rangeMax = formatFloatRust(f.FMax)
	}

	enumMax, enumName := "-1", "None"
	if enum, isEnum := f.Type.Ref.(*ir.Enum); f.Type.Kind == ir.TNamed && isEnum {
		enumMax = fmt.Sprintf("%d", enum.Max)
		enumName = fmt.Sprintf("Some(enum_name_%s_dyn)", ir.RustSnake(f.Type.Name))
	}

	return []string{
		fmt.Sprintf("name: %q", f.Name),
		fmt.Sprintf("type_name: %q", tableFieldTypeName(f)),
		fmt.Sprintf("id: 0x%04x", ir.FieldId(f.Name)),
		fmt.Sprintf("kind: %d", kind),
		fmt.Sprintf("is_array: %t", isArray),
		fmt.Sprintf("counted: %t", counted),
		fmt.Sprintf("array_bound: %d", arrayBound),
		fmt.Sprintf("table: %s", table),
		fmt.Sprintf("has_range: %s", hasRange),
		fmt.Sprintf("range_min: %s", rangeMin),
		fmt.Sprintf("range_max: %s", rangeMax),
		fmt.Sprintf("enum_max: %s", enumMax),
		fmt.Sprintf("enum_name: %s", enumName),
		fmt.Sprintf("guard: %q", guard),
	}
}

// tableGuardStrings renders each guarded field's branch condition in SCHEMA
// terms ("at_rest" / "!at_rest"), which is what the descriptor exposes — the
// generated-code form lives in tableGuardExprs.
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

func tableFieldTypeName(f *ir.Field) string {
	switch f.Type.Kind {
	case ir.TBool:
		return "bool"
	case ir.TInt:
		if f.Type.Signed {
			return fmt.Sprintf("int%d", f.Type.Width)
		}
		return fmt.Sprintf("uint%d", f.Type.Width)
	case ir.TBits:
		return fmt.Sprintf("bits%d", f.Type.Width)
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

func bigToFloat64(v *big.Int) float64 {
	f, _ := new(big.Float).SetInt(v).Float64()
	return f
}

// formatFloatRust renders a float64 as a Rust literal that always carries a
// decimal point, so an integral bound is still an f64 and not an inference
// error at the use site.
func formatFloatRust(v float64) string {
	s := fmt.Sprintf("%v", v)
	if !strings.ContainsAny(s, ".eE") && !strings.Contains(s, "inf") && !strings.Contains(s, "NaN") {
		s += ".0"
	}
	switch {
	case strings.Contains(s, "+Inf"):
		return "f64::INFINITY"
	case strings.Contains(s, "-Inf"):
		return "f64::NEG_INFINITY"
	case strings.Contains(s, "NaN"):
		return "f64::NAN"
	}
	return s
}

// tableDefaultExpr renders the Rust expression a scalar field's default
// compares against on the write side — identical values to new()/zero.
func (g *tableGen) tableDefaultExpr(f *ir.Field) string {
	switch f.Type.Kind {
	case ir.TBool:
		if f.HasDefault && f.DefBool {
			return "true"
		}
		return "false"
	case ir.TFloat32, ir.TFloat64:
		if f.HasDefault {
			return formatFloatRust(f.DefFloat)
		}
		return "0.0"
	case ir.TInt, ir.TBits:
		if f.HasDefault && f.DefInt != nil {
			return f.DefInt.String()
		}
		return "0"
	case ir.TNamed:
		switch f.Type.Ref.(type) {
		case *ir.Enum:
			if f.HasDefault && f.DefVariant != "" {
				return f.Type.Name + "::" + ir.RustConstName(f.DefVariant)
			}
			return f.Type.Name + "::NONE"
		case *ir.Flags:
			return "0"
		}
	}
	return "0"
}
