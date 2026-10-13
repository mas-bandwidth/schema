package rusttable

// The COOKED FORM's WRITE side (docs/SPEC-TABLES.md §7.6): the Rust spelling of
// Go's <T>CookMeasure, <T>CookMeasureReason and <T>CookFrom. Measure before
// write; the bytes are `schema cook`'s in both byte orders; a buffer short of
// the measure writes nothing and returns false; CookMeasureReason answers
// TableRefuseInvalidValue the way gotable/refusal.go does.

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

const cookWriteRuntime = `
// ---- the cooked form, the WRITE side (docs/SPEC-TABLES.md §7.6) ----
//
// THE BYTE ORDER IS THE TARGET'S, NOT THE HOST'S. A cook is produced in the
// byte order of the build that will read it (§7), so the fixing happens here
// and never at Open.

#[repr(u8)]
#[derive(Clone, Copy, PartialEq, Eq, Debug)]
pub enum TableByteOrder {
    Little = 1,
    Big = 2,
}

// TableRefuseReason is the typed refusal shared with Go's TableRefuseReason
// (gotable/refusal.go). CookMeasureReason answers TableRefuseInvalidValue when
// the value cannot be cooked (§7.6).
#[repr(u8)]
#[derive(Clone, Copy, PartialEq, Eq, Debug)]
pub enum TableRefuseReason {
    TableRefuseOk = 0,
    TableRefuseNotACook,
    TableRefuseForeignOrder,
    TableRefuseWrongBuildVersion,
    TableRefuseReservedNotZero,
    TableRefuseBadAlignment,
    TableRefuseTruncated,
    TableRefuseUnalignedBase,
    TableRefuseBadLayout,
    TableRefuseUnknownForm,
    TableRefuseCountOverLength,
    TableRefuseCountOverExtentCap,
    TableRefuseBlobOverSizeCap,
    TableRefuseDataCycle,
    TableRefuseInvalidValue,
    TableRefuseAllocationFailed,
    TableRefuseWireDamaged,
}

impl TableRefuseReason {
    pub fn as_str(self) -> &'static str {
        match self {
            TableRefuseReason::TableRefuseOk => "ok",
            TableRefuseReason::TableRefuseNotACook => "not_a_cook",
            TableRefuseReason::TableRefuseForeignOrder => "foreign_order",
            TableRefuseReason::TableRefuseWrongBuildVersion => "wrong_build_version",
            TableRefuseReason::TableRefuseReservedNotZero => "reserved_not_zero",
            TableRefuseReason::TableRefuseBadAlignment => "bad_alignment",
            TableRefuseReason::TableRefuseTruncated => "truncated",
            TableRefuseReason::TableRefuseUnalignedBase => "unaligned_base",
            TableRefuseReason::TableRefuseBadLayout => "bad_layout",
            TableRefuseReason::TableRefuseUnknownForm => "unknown_form",
            TableRefuseReason::TableRefuseCountOverLength => "count_over_length",
            TableRefuseReason::TableRefuseCountOverExtentCap => "count_over_extent_cap",
            TableRefuseReason::TableRefuseBlobOverSizeCap => "blob_over_size_cap",
            TableRefuseReason::TableRefuseDataCycle => "data_cycle",
            TableRefuseReason::TableRefuseInvalidValue => "invalid_value",
            TableRefuseReason::TableRefuseAllocationFailed => "allocation_failed",
            TableRefuseReason::TableRefuseWireDamaged => "wire_damaged",
        }
    }
}

/// One store, width as an argument (docs/SPEC-TABLES.md §7.6). A measure passes
/// an empty buffer and the store writes nothing.
pub fn cook_put(buf: &mut [u8], at: i64, value: u64, width: u32, order: TableByteOrder) {
    if buf.is_empty() || at < 0 {
        return;
    }
    let at = at as usize;
    let width = width as usize;
    if at.saturating_add(width) > buf.len() {
        return;
    }
    for i in 0..width {
        let shift = if order == TableByteOrder::Big { width - 1 - i } else { i };
        buf[at + i] = (value >> (shift * 8)) as u8;
    }
}

pub fn cook_align(v: i64, a: i64) -> i64 {
    if a <= 1 {
        return v;
    }
    (v + a - 1) & !(a - 1)
}
`

func (g *gen) emitCookWrite(roots []*ir.Struct) {
	var tables []*ir.Struct
	for _, st := range roots {
		if st.IsTable && g.cookable(st.Name) {
			tables = append(tables, st)
		}
	}
	if len(tables) == 0 {
		return
	}
	g.pf("// ---- the cooked form, the WRITE side (docs/SPEC-TABLES.md §7.6) ----\n\n")
	g.pf("#[allow(unused_imports)]\nuse std::rc::Rc;\n\n")
	seen := map[string]bool{}
	for _, st := range roots {
		if g.cookable(st.Name) && g.declaredHere(st.Name) {
			g.emitCookRecordWriters(st, seen)
		}
	}
	for _, st := range tables {
		g.emitCookRecordWriters(st, seen)
	}
	for _, st := range tables {
		g.emitCookSurface(st)
	}
}

func (g *gen) variableEmitted(name string) bool {
	if g.vc == nil {
		g.vc = newVariableClass(g.unit)
	}
	return g.vc.emitted[name]
}

// cookRustValue reports a record whose cook writer reads the Rust variable
// struct rather than the blittable Row: a carried variable table, or a
// non-table struct the packet emitter spelled.
func (g *gen) packetStructNeeded(name string) bool {
	for n := range ir.VariableTables(g.unit) {
		if !g.variableEmitted(n) {
			continue
		}
		if structReached(g.unit.Tables[n], name, map[string]bool{}) {
			return true
		}
	}
	return false
}

func structReached(st *ir.Struct, name string, seen map[string]bool) bool {
	if st == nil || seen[st.Name] {
		return false
	}
	seen[st.Name] = true
	for _, f := range st.Fields {
		ref, ok := f.Type.Ref.(*ir.Struct)
		if !ok || f.Type.Pointer || f.Type.Blob() {
			continue
		}
		if ref.Name == name || structReached(ref, name, seen) {
			return true
		}
	}
	return false
}

func (g *gen) cookRustValue(st *ir.Struct) bool {
	if !st.IsTable {
		return false
	}
	return g.variableEmitted(st.Name)
}

func (g *gen) declaredHere(name string) bool {
	for _, st := range g.file.Tables {
		if st.Name == name {
			return true
		}
	}
	for _, d := range g.file.Decls {
		if st, ok := d.(*ir.Struct); ok && st.Name == name {
			return true
		}
	}
	return false
}

func (g *gen) emitCookRecordWriters(st *ir.Struct, seen map[string]bool) {
	if seen[st.Name] || !g.cookable(st.Name) {
		return
	}
	seen[st.Name] = true
	for _, f := range st.Fields {
		if f.Type.Kind != ir.TNamed || f.Type.Pointer || f.Type.Blob() {
			continue
		}
		if ref, ok := f.Type.Ref.(*ir.Struct); ok && g.declaredHere(ref.Name) {
			g.emitCookRecordWriters(ref, seen)
		}
	}
	if !g.declaredHere(st.Name) {
		return
	}
	// The Row writer is what a fixed cook and a Row-nested field call. An
	// emitted variable table also gets the Rust-struct writer the graph walk
	// uses. Both, so a sibling file's Row cook can name the Row writer.
	g.emitRowCookRecord(st)
	if !st.IsTable {
		if g.packetStructNeeded(st.Name) {
			g.emitPacketCookRecord(st)
		}
		return
	}
	if g.cookRustValue(st) {
		g.emitRustCookRecord(st)
	}
}

func (g *gen) emitCookSurface(st *ir.Struct) {
	n := st.Name
	snake := ir.RustSnake(n)
	if g.cookRustValue(st) {
		g.pf("// %sCookMeasure / %sCookMeasureReason / %sCookFrom (docs/SPEC-TABLES.md §7.6).\n", n, n, n)
		g.pf("// A buffer short of the measure writes nothing and returns false.\n")
		g.pf("pub fn %s(value: &%s) -> i64 {\n", fn(n, "cook_measure"), n)
		g.pf("    %s(value).0\n}\n\n", fn(n, "cook_measure_reason"))
		g.pf("pub fn %s(value: &%s) -> (i64, TableRefuseReason) {\n", fn(n, "cook_measure_reason"), n)
		g.pf("    match %s(value, &mut [], TableByteOrder::Little, true) {\n", fn(n, "cook_write"))
		g.pf("        Ok(n) => (n, TableRefuseReason::TableRefuseOk),\n")
		g.pf("        Err(r) => (-1, r),\n")
		g.pf("    }\n}\n\n")
		g.pf("pub fn %s(value: &%s, buffer: &mut [u8], order: TableByteOrder) -> bool {\n", fn(n, "cook_from"), n)
		g.pf("    let need = %s(value);\n", fn(n, "cook_measure"))
		g.pf("    if need < 0 || (buffer.len() as i64) < need {\n        return false;\n    }\n")
		g.pf("    %s(value, buffer, order, false).is_ok()\n}\n\n", fn(n, "cook_write"))
		g.emitVariableCookWrite(st)
		return
	}
	layout := ir.RecordLayout(g.unit, st)
	align := max(int64(8), layout.Align)
	size := (layout.Size + align - 1) & -align
	total := 64 + size + 16
	g.pf("// %sCookMeasure / %sCookMeasureReason / %sCookFrom (docs/SPEC-TABLES.md §7.6).\n", n, n, n)
	g.pf("// A fixed table is one node: header, record, one directory entry.\n")
	g.pf("pub fn %s(_value: &%sRow) -> i64 { %d }\n\n", fn(n, "cook_measure"), n, total)
	g.pf("pub fn %s(value: &%sRow) -> (i64, TableRefuseReason) {\n", fn(n, "cook_measure_reason"), n)
	g.pf("    (%s(value), TableRefuseReason::TableRefuseOk)\n}\n\n", fn(n, "cook_measure"))
	g.pf("pub fn %s(value: &%sRow, buffer: &mut [u8], order: TableByteOrder) -> bool {\n", fn(n, "cook_from"), n)
	g.pf("    let need = %s(value);\n", fn(n, "cook_measure"))
	g.pf("    if (buffer.len() as i64) < need {\n        return false;\n    }\n")
	g.pf("    if order != TableByteOrder::Little && order != TableByteOrder::Big {\n        return false;\n    }\n")
	g.pf("    buffer[..need as usize].fill(0);\n")
	g.pf("    cook_put(buffer, 0, 0x4b4f4f434d484353, 8, order);\n")
	g.pf("    cook_put(buffer, 8, BUILD_VERSION, 8, order);\n")
	g.pf("    cook_put(buffer, 16, order as u64, 8, order);\n")
	g.pf("    cook_put(buffer, 24, %d, 8, order);\n", size)
	g.pf("    cook_put(buffer, 32, 16, 8, order);\n")
	g.pf("    cook_put(buffer, 40, %d, 8, order);\n", align)
	g.pf("    if %s(value, buffer, 64, order).is_err() {\n        return false;\n    }\n", fn(n, "row_cook_record"))
	g.pf("    cook_put(buffer, 64 + %d, 0, 8, order);\n", size)
	g.pf("    cook_put(buffer, 64 + %d + 8, 0x%016x, 8, order);\n", size, ir.TableTypeId(st.WireName()))
	g.pf("    let _ = %q;\n    true\n}\n\n", snake)
}

func (g *gen) emitRowCookRecord(st *ir.Struct) {
	g.pf("#[allow(dead_code, unused_variables, clippy::needless_pass_by_ref_mut)]\n")
	g.pf("pub fn %s(value: &%sRow, buf: &mut [u8], base: i64, order: TableByteOrder) -> core::result::Result<(), TableRefuseReason> {\n", fn(st.Name, "row_cook_record"), st.Name)
	layout := ir.RecordLayout(g.unit, st)
	for _, fl := range layout.Fields {
		g.emitRowFieldWrite(fl, "value", "base")
	}
	g.pf("    Ok(())\n}\n\n")
}

func (g *gen) emitRowFieldWrite(fl ir.FieldLayout, val, base string) {
	f := fl.Field
	pieces := ir.FieldPieces(g.unit, f, fl.Offset)
	if len(pieces) == 0 {
		return
	}
	name := f.Name
	if f.Type.Optional {
		p := pieces[len(pieces)-1]
		g.pf("    cook_put(buf, %s + %d, if %s.%s_present { 1 } else { 0 }, 1, order);\n", base, p.Offset, val, name)
	}
	switch {
	case f.IsList() || f.IsMap():
		g.pf("    return Err(TableRefuseReason::TableRefuseInvalidValue); // a list or a map is not a fixed row (§7.6)\n")
	case f.Type.Pointer || f.Type.Blob():
		g.pf("    cook_put(buf, %s + %d, %s.%s as u64, 8, order);\n", base, pieces[0].Offset, val, name)
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
		g.pf("    {\n")
		g.pf("        let used = %s.%s_length;\n", val, name)
		g.pf("        if used < 0 || used as i64 > %d {\n            return Err(TableRefuseReason::TableRefuseInvalidValue);\n        }\n", f.Type.Size)
		g.pf("        if !buf.is_empty() {\n")
		g.pf("            let at = (%s + %d) as usize;\n", base, pieces[0].Offset)
		g.pf("            let n = used as usize;\n")
		g.pf("            buf[at..at + n].copy_from_slice(&%s.%s[..n]);\n", val, name)
		g.pf("        }\n")
		g.pf("        cook_put(buf, %s + %d, used as u64, 4, order);\n", base, pieces[1].Offset)
		g.pf("    }\n")
	case f.Type.Kind == ir.TWString:
		g.pf("    return Err(TableRefuseReason::TableRefuseInvalidValue);\n")
	case f.KeyEnum != "" || f.Array == ir.ArrayFixed || f.Array == ir.ArrayCounted:
		step := rowElemStep(g.unit, f)
		bound := f.ArrayBound
		if f.KeyEnum != "" {
			bound = f.ArrayBound
		}
		g.pf("    for i in 0..%d {\n", bound)
		g.emitRowElemWrite(f, fmt.Sprintf("&%s.%s[i]", val, name), fmt.Sprintf("%s + %d + i as i64 * %d", base, pieces[0].Offset, step), "        ")
		g.pf("    }\n")
		if f.Array == ir.ArrayCounted && f.KeyEnum == "" {
			g.pf("    if %s.%s_count < 0 || %s.%s_count > %d {\n        return Err(TableRefuseReason::TableRefuseInvalidValue);\n    }\n", val, name, val, name, bound)
			g.pf("    cook_put(buf, %s + %d, %s.%s_count as u64, 4, order);\n", base, pieces[1].Offset, val, name)
		}
	default:
		g.emitRowElemWrite(f, "&"+val+"."+name, fmt.Sprintf("%s + %d", base, pieces[0].Offset), "    ")
	}
}

func (g *gen) emitRowElemWrite(f *ir.Field, expr, at, ind string) {
	if f.Type.Kind == ir.TNamed {
		if ref, ok := f.Type.Ref.(*ir.Struct); ok {
			g.pf("%s%s(%s, buf, %s, order)?;\n", ind, fn(ref.Name, "row_cook_record"), expr, at)
			return
		}
		if ref, ok := f.Type.Ref.(*ir.Enum); ok {
			w := int64(ir.StorageBitsFor(ref.Max)) / 8
			g.pf("%scook_put(buf, %s, *%s as u64, %d, order);\n", ind, at, expr, w)
			return
		}
		if _, ok := f.Type.Ref.(*ir.Flags); ok {
			g.pf("%scook_put(buf, %s, *%s as u64, 8, order);\n", ind, at, expr)
			return
		}
	}
	switch f.Type.Kind {
	case ir.TBool:
		g.pf("%scook_put(buf, %s, if *%s { 1 } else { 0 }, 1, order);\n", ind, at, expr)
	case ir.TFloat32:
		g.pf("%scook_put(buf, %s, %s.to_bits() as u64, 4, order);\n", ind, at, expr)
	case ir.TFloat64:
		g.pf("%scook_put(buf, %s, %s.to_bits(), 8, order);\n", ind, at, expr)
	default:
		w := scalarWidth(f)
		g.pf("%scook_put(buf, %s, *%s as u64, %d, order);\n", ind, at, expr, w)
	}
}

func (g *gen) emitPacketCookRecord(st *ir.Struct) {
	g.pf("#[allow(dead_code, unused_variables)]\n")
	g.pf("pub fn %s(value: &%s, buf: &mut [u8], base: i64, order: TableByteOrder) -> core::result::Result<(), TableRefuseReason> {\n", fn(st.Name, "cook_record"), st.Name)
	layout := ir.RecordLayout(g.unit, st)
	for _, fl := range layout.Fields {
		g.emitRowFieldWrite(fl, "value", "base")
	}
	g.pf("    Ok(())\n}\n\n")
}

func (g *gen) emitRustCookRecord(st *ir.Struct) {
	g.pf("#[allow(dead_code, unused_variables, clippy::needless_pass_by_ref_mut, clippy::too_many_arguments)]\n")
	g.pf("pub fn %s(value: &%s, buf: &mut [u8], base: i64, order: TableByteOrder, g: &TableNodeGraph, offsets: &[i64], extent_base: i64, extent_at: &mut i64) -> core::result::Result<(), TableRefuseReason> {\n", fn(st.Name, "cook_record"), st.Name)
	layout := ir.RecordLayout(g.unit, st)
	for _, fl := range layout.Fields {
		g.emitRustFieldWrite(fl, "value", "base")
	}
	g.pf("    Ok(())\n}\n\n")
	g.pf("#[allow(dead_code)]\n")
	g.pf("pub fn %s(value: &%s) -> core::result::Result<i64, TableRefuseReason> {\n", fn(st.Name, "cook_extent"), st.Name)
	g.pf("    let _ = value;\n")
	g.pf("    let mut at: i64 = 0;\n")
	g.emitRustExtent(st, "value", "at")
	g.pf("    Ok(at)\n}\n\n")
}

func (g *gen) emitRustExtent(st *ir.Struct, val, at string) {
	for _, f := range st.Fields {
		if f.IsList() {
			size, align := ir.ListElementLayout(g.unit, f)
			g.pf("    %s = cook_align(%s, %d);\n", at, at, align)
			g.pf("    %s += %s.%s.len() as i64 * %d;\n", at, val, ir.RustSnake(f.Name), size)
			if rustElemHasExtent(g.unit, f) {
				g.pf("    for e in &%s.%s {\n", val, ir.RustSnake(f.Name))
				g.pf("        %s += %s(e)?;\n", at, fn(f.Type.Name, "cook_extent"))
				g.pf("    }\n")
			}
			continue
		}
		if f.Type.Pointer || f.Type.Blob() || f.Type.Kind != ir.TNamed {
			continue
		}
		ref, ok := f.Type.Ref.(*ir.Struct)
		if !ok || !recordHasCookExtent(g.unit, ref) {
			continue
		}
		x := val + "." + ir.RustSnake(f.Name)
		switch {
		case f.Array != ir.ArrayNone:
			g.pf("    for e in &%s {\n        %s += %s(e)?;\n    }\n", x, at, fn(ref.Name, "cook_extent"))
		default:
			g.pf("    %s += %s(&%s)?;\n", at, fn(ref.Name, "cook_extent"), x)
		}
	}
}

func (g *gen) emitRustFieldWrite(fl ir.FieldLayout, val, base string) {
	f := fl.Field
	pieces := ir.FieldPieces(g.unit, f, fl.Offset)
	if len(pieces) == 0 {
		return
	}
	x := val + "." + ir.RustSnake(f.Name)
	if f.Type.Optional && !f.Type.Pointer && !f.Type.Blob() {
		p := pieces[len(pieces)-1]
		g.pf("    cook_put(buf, %s + %d, 1, 1, order); // a carried variable field is present (§7.2)\n", base, p.Offset)
	}
	switch {
	case f.IsMap():
		g.pf("    return Err(TableRefuseReason::TableRefuseInvalidValue); // maps are not carried (§2.8)\n")
	case f.IsList():
		g.emitRustList(f, x, fmt.Sprintf("%s + %d", base, pieces[0].Offset))
	case f.Type.Blob():
		g.emitRustBlobRef(f, x, fmt.Sprintf("%s + %d", base, pieces[0].Offset))
	case f.Type.Pointer && f.Array == ir.ArrayNone:
		g.emitRustPtr(f, "&"+x, fmt.Sprintf("%s + %d", base, pieces[0].Offset), "    ")
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
		g.emitRustBuffer(f, x, base, pieces)
	case f.KeyEnum != "" || f.Array == ir.ArrayFixed || f.Array == ir.ArrayCounted:
		g.emitRustArray(f, x, base, pieces)
	default:
		g.emitRustElem(f, "&"+x, fmt.Sprintf("%s + %d", base, pieces[0].Offset), "    ")
	}
}

func (g *gen) emitRustBuffer(f *ir.Field, x, base string, pieces []ir.BlockFieldPiece) {
	g.pf("    {\n")
	if f.Type.Kind == ir.TBytes {
		g.pf("        let raw: &[u8] = &%s;\n", x)
	} else {
		g.pf("        let raw = %s.as_bytes();\n", x)
	}
	g.pf("        if raw.len() > %d {\n            return Err(TableRefuseReason::TableRefuseInvalidValue);\n        }\n", f.Type.Size)
	g.pf("        if !buf.is_empty() {\n")
	g.pf("            let at = (%s + %d) as usize;\n", base, pieces[0].Offset)
	g.pf("            buf[at..at + raw.len()].copy_from_slice(raw);\n")
	g.pf("        }\n")
	g.pf("        cook_put(buf, %s + %d, raw.len() as u64, 4, order);\n", base, pieces[1].Offset)
	g.pf("    }\n")
}

func (g *gen) emitRustArray(f *ir.Field, x, base string, pieces []ir.BlockFieldPiece) {
	step := rowElemStep(g.unit, f)
	bound := f.ArrayBound
	g.pf("    if %s.len() > %d {\n        return Err(TableRefuseReason::TableRefuseInvalidValue);\n    }\n", x, bound)
	g.pf("    for i in 0..%d {\n", bound)
	g.pf("        if i < %s.len() {\n", x)
	g.emitRustElem(f, fmt.Sprintf("&%s[i]", x), fmt.Sprintf("%s + %d + i as i64 * %d", base, pieces[0].Offset, step), "            ")
	g.pf("        }\n")
	g.pf("    }\n")
	if f.Array == ir.ArrayCounted && f.KeyEnum == "" {
		g.pf("    cook_put(buf, %s + %d, %s.len() as u64, 4, order);\n", base, pieces[1].Offset, x)
	}
}

func (g *gen) emitRustList(f *ir.Field, x, slot string) {
	size, align := ir.ListElementLayout(g.unit, f)
	g.pf("    {\n")
	g.pf("        let count = %s.len() as i64;\n", x)
	g.pf("        if count > i32::MAX as i64 {\n            return Err(TableRefuseReason::TableRefuseInvalidValue);\n        }\n")
	g.pf("        *extent_at = cook_align(*extent_at, %d);\n", align)
	g.pf("        let start = *extent_at;\n")
	g.pf("        *extent_at += count * %d;\n", size)
	g.pf("        let array = extent_base + start;\n")
	g.pf("        if count > 0 {\n")
	g.pf("            cook_put(buf, %s, (array - (%s)) as u64, 8, order);\n", slot, slot)
	g.pf("        }\n")
	g.pf("        cook_put(buf, %s + 8, count as u64, 4, order);\n", slot)
	g.pf("        for (i, e) in %s.iter().enumerate() {\n", x)
	g.emitRustElem(f, "e", fmt.Sprintf("array + i as i64 * %d", size), "            ")
	g.pf("        }\n")
	g.pf("    }\n")
}

func (g *gen) emitRustPtr(f *ir.Field, expr, at, ind string) {
	g.pf("%smatch %s {\n", ind, expr)
	g.pf("%s    None => cook_put(buf, %s, 0, 8, order),\n", ind, at)
	g.pf("%s    Some(n) => {\n", ind)
	g.pf("%s        let idx = g.node_index(n);\n", ind)
	g.pf("%s        if idx == 0 || idx as usize > offsets.len() {\n            return Err(TableRefuseReason::TableRefuseInvalidValue);\n        }\n", ind)
	g.pf("%s        let delta = offsets[idx as usize - 1] - (%s);\n", ind, at)
	g.pf("%s        if delta == 0 {\n            return Err(TableRefuseReason::TableRefuseInvalidValue);\n        }\n", ind)
	g.pf("%s        cook_put(buf, %s, delta as u64, 8, order);\n", ind, at)
	g.pf("%s    }\n", ind)
	g.pf("%s}\n", ind)
}

func (g *gen) emitRustBlobRef(f *ir.Field, x, at string) {
	g.pf("    match &%s {\n", x)
	g.pf("        None => cook_put(buf, %s, 0, 8, order),\n", at)
	g.pf("        Some(n) => {\n")
	g.pf("            let key = Rc::as_ptr(n) as *const u8 as usize;\n")
	g.pf("            let idx = g.blob_index(key);\n")
	g.pf("            if idx == 0 || idx as usize > offsets.len() {\n                return Err(TableRefuseReason::TableRefuseInvalidValue);\n            }\n")
	g.pf("            let delta = offsets[idx as usize - 1] - (%s);\n", at)
	g.pf("            if delta == 0 {\n                return Err(TableRefuseReason::TableRefuseInvalidValue);\n            }\n")
	g.pf("            cook_put(buf, %s, delta as u64, 8, order);\n", at)
	g.pf("        }\n")
	g.pf("    }\n")
}

func (g *gen) emitRustElem(f *ir.Field, expr, at, ind string) {
	if f.Type.Pointer {
		g.emitRustPtr(f, expr, at, ind)
		return
	}
	if f.Type.Kind == ir.TNamed {
		if ref, ok := f.Type.Ref.(*ir.Enum); ok {
			w := int64(ir.StorageBitsFor(ref.Max)) / 8
			g.pf("%slet ord: u64 = match *%s {\n", ind, expr)
			g.pf("%s    %s::None => 0,\n", ind, ref.Name)
			for i, v := range ref.Variants {
				g.pf("%s    %s::%s => %d,\n", ind, ref.Name, rustVariant(v), i+1)
			}
			g.pf("%s};\n", ind)
			g.pf("%scook_put(buf, %s, ord, %d, order);\n", ind, at, w)
			return
		}
		if _, ok := f.Type.Ref.(*ir.Flags); ok {
			g.pf("%scook_put(buf, %s, *%s as u64, 8, order);\n", ind, at, expr)
			return
		}
		if ref, ok := f.Type.Ref.(*ir.Struct); ok {
		if g.cookRustValue(ref) {
			g.pf("%s%s(%s, buf, %s, order, g, offsets, extent_base, extent_at)?;\n", ind, fn(ref.Name, "cook_record"), expr, at)
		} else if !ref.IsTable {
			g.pf("%s%s(%s, buf, %s, order)?;\n", ind, fn(ref.Name, "cook_record"), expr, at)
		} else {
			g.pf("%s%s(%s, buf, %s, order)?;\n", ind, fn(ref.Name, "row_cook_record"), expr, at)
		}
			return
		}
	}
	switch f.Type.Kind {
	case ir.TBool:
		g.pf("%scook_put(buf, %s, if *%s { 1 } else { 0 }, 1, order);\n", ind, at, expr)
	case ir.TFloat32:
		g.pf("%scook_put(buf, %s, %s.to_bits() as u64, 4, order);\n", ind, at, expr)
	case ir.TFloat64:
		g.pf("%scook_put(buf, %s, %s.to_bits(), 8, order);\n", ind, at, expr)
	case ir.TString:
		g.pf("%s{\n", ind)
		g.pf("%s    let raw = %s.as_bytes();\n", ind, expr)
		g.pf("%s    if raw.len() > %d {\n        return Err(TableRefuseReason::TableRefuseInvalidValue);\n    }\n", ind, f.Type.Size)
		g.pf("%s    if !buf.is_empty() {\n", ind)
		g.pf("%s        let at = (%s) as usize;\n", ind, at)
		g.pf("%s        buf[at..at + raw.len()].copy_from_slice(raw);\n", ind)
		g.pf("%s    }\n", ind)
		g.pf("%s}\n", ind)
	default:
		w := scalarWidth(f)
		g.pf("%scook_put(buf, %s, *%s as u64, %d, order);\n", ind, at, expr, w)
	}
}

func (g *gen) emitVariableCookWrite(st *ir.Struct) {
	n := st.Name
	g.pf("#[allow(clippy::too_many_arguments, unused_variables)]\n")
	g.pf("fn %s(value: &%s, buffer: &mut [u8], order: TableByteOrder, measure: bool) -> core::result::Result<i64, TableRefuseReason> {\n", fn(n, "cook_write"), n)
	g.pf("    if order != TableByteOrder::Little && order != TableByteOrder::Big {\n")
	g.pf("        return Err(TableRefuseReason::TableRefuseInvalidValue);\n")
	g.pf("    }\n")
	g.pf("    let mut graph = TableNodeGraph::new();\n")
	g.pf("    %s(value, &mut graph);\n", fn(n, "number"))
	g.pf("    let mut align: i64 = 8;\n")
	g.pf("    let mut offset: i64 = 0;\n")
	g.pf("    let mut nodes: Vec<(i64, u64, i64, i64)> = Vec::new(); // offset, type id, record, extent\n")
	// root
	g.emitCookNodePush(st, "value", true)
	g.pf("    for rec in &graph.records {\n")
	g.pf("        match rec {\n")
	g.pf("            TableNodeRecord::Blob(type_id, bytes) => {\n")
	g.pf("                offset = cook_align(offset, 8);\n")
	g.pf("                let extra: i64 = if *type_id == STRING_TYPE_ID { 1 } else { 0 };\n")
	g.pf("                let size = 8 + bytes.len() as i64 + extra;\n")
	g.pf("                nodes.push((offset, *type_id, size, 0));\n")
	g.pf("                offset += size;\n")
	g.pf("            }\n")
	g.pf("            TableNodeRecord::Table(type_id, node) => {\n")
	g.pf("                match *type_id {\n")
	for _, target := range g.cookPointerTargets(st) {
		g.pf("                    0x%016x => {\n", ir.TableWireId(target.WireName()))
		g.pf("                        if let Some(v) = node.downcast_ref::<%s>() {\n", target.Name)
		g.emitCookNodePush(target, "v", false)
		g.pf("                        } else {\n                            return Err(TableRefuseReason::TableRefuseInvalidValue);\n                        }\n")
		g.pf("                    }\n")
	}
	g.pf("                    _ => return Err(TableRefuseReason::TableRefuseInvalidValue),\n")
	g.pf("                }\n")
	g.pf("            }\n")
	g.pf("        }\n")
	g.pf("    }\n")
	g.pf("    let data = cook_align(offset, align);\n")
	g.pf("    let data_off = cook_align(64, align);\n")
	g.pf("    let total = data_off + data + nodes.len() as i64 * 16;\n")
	g.pf("    if measure {\n        return Ok(total);\n    }\n")
	g.pf("    if (buffer.len() as i64) < total {\n        return Err(TableRefuseReason::TableRefuseInvalidValue);\n    }\n")
	g.pf("    buffer[..total as usize].fill(0);\n")
	g.pf("    cook_put(buffer, 0, 0x4b4f4f434d484353, 8, order);\n")
	g.pf("    cook_put(buffer, 8, BUILD_VERSION, 8, order);\n")
	g.pf("    cook_put(buffer, 16, order as u64, 8, order);\n")
	g.pf("    cook_put(buffer, 24, data as u64, 8, order);\n")
	g.pf("    cook_put(buffer, 32, nodes.len() as u64 * 16, 8, order);\n")
	g.pf("    cook_put(buffer, 40, align as u64, 8, order);\n")
	g.pf("    let mut offsets = Vec::with_capacity(nodes.len());\n")
	g.pf("    // absolute, so a slot's self-relative delta is target - slot (§6.3)\n")
	g.pf("    for n in &nodes { offsets.push(data_off + n.0); }\n")
	// write root then records
	g.pf("    {\n")
	g.pf("        let (at, _, record, _) = nodes[0];\n")
	g.pf("        let mut extent_at: i64 = 0;\n")
	g.pf("        let extent_base = data_off + at + record;\n")
	g.pf("        %s(value, buffer, data_off + at, order, &graph, &offsets, extent_base, &mut extent_at)?;\n", fn(n, "cook_record"))
	g.pf("        if extent_at != nodes[0].3 {\n            return Err(TableRefuseReason::TableRefuseInvalidValue);\n        }\n")
	g.pf("    }\n")
	g.pf("    let mut ri = 1usize;\n")
	g.pf("    for rec in &graph.records {\n")
	g.pf("        let (at, _, record, extent) = nodes[ri];\n")
	g.pf("        match rec {\n")
	g.pf("            TableNodeRecord::Blob(_, bytes) => {\n")
	g.pf("                cook_put(buffer, data_off + at, bytes.len() as u64, 4, order);\n")
	g.pf("                if !bytes.is_empty() {\n")
	g.pf("                    let dst = (data_off + at + 8) as usize;\n")
	g.pf("                    buffer[dst..dst + bytes.len()].copy_from_slice(bytes);\n")
	g.pf("                }\n")
	g.pf("            }\n")
	g.pf("            TableNodeRecord::Table(type_id, node) => {\n")
	g.pf("                let mut extent_at: i64 = 0;\n")
	g.pf("                let extent_base = data_off + at + record;\n")
	g.pf("                match *type_id {\n")
	for _, target := range g.cookPointerTargets(st) {
		g.pf("                    0x%016x => {\n", ir.TableWireId(target.WireName()))
		g.pf("                        let v = node.downcast_ref::<%s>().ok_or(TableRefuseReason::TableRefuseInvalidValue)?;\n", target.Name)
		g.pf("                        %s(v, buffer, data_off + at, order, &graph, &offsets, extent_base, &mut extent_at)?;\n", fn(target.Name, "cook_record"))
		g.pf("                    }\n")
	}
	g.pf("                    _ => return Err(TableRefuseReason::TableRefuseInvalidValue),\n")
	g.pf("                }\n")
	g.pf("                if extent_at != extent {\n                    return Err(TableRefuseReason::TableRefuseInvalidValue);\n                }\n")
	g.pf("            }\n")
	g.pf("        }\n")
	g.pf("        ri += 1;\n")
	g.pf("    }\n")
	g.pf("    let mut dir = data_off + data;\n")
	g.pf("    for n in &nodes {\n")
	g.pf("        cook_put(buffer, dir, n.0 as u64, 8, order);\n")
	g.pf("        cook_put(buffer, dir + 8, n.1, 8, order);\n")
	g.pf("        dir += 16;\n")
	g.pf("    }\n")
	g.pf("    Ok(total)\n}\n\n")
}

func (g *gen) emitCookNodePush(st *ir.Struct, val string, isRoot bool) {
	layout := ir.RecordLayout(g.unit, st)
	rec := layout.Size
	if recordHasCookExtent(g.unit, st) {
		rec = (layout.Size + 7) & ^int64(7)
	}
	ind := "                "
	if isRoot {
		ind = "    "
	}
	g.pf("%soffset = cook_align(offset, %d);\n", ind, layout.Align)
	g.pf("%sif %d > align {\n        align = %d;\n    }\n", ind, layout.Align, layout.Align)
	g.pf("%slet extent = %s(%s)?;\n", ind, fn(st.Name, "cook_extent"), val)
	g.pf("%snodes.push((offset, 0x%016x, %d, extent));\n", ind, ir.TableTypeId(st.WireName()), rec)
	g.pf("%soffset += %d + extent;\n", ind, rec)
}

func (g *gen) cookPointerTargets(root *ir.Struct) []*ir.Struct {
	seen := map[string]bool{}
	targeted := map[string]bool{}
	var out []*ir.Struct
	var walk func(*ir.Struct)
	walk = func(st *ir.Struct) {
		if st == nil || seen[st.Name] {
			return
		}
		seen[st.Name] = true
		for _, f := range st.Fields {
			if f.Type.Blob() {
				continue
			}
			ref, ok := f.Type.Ref.(*ir.Struct)
			if !ok {
				continue
			}
			if f.Type.Pointer && !targeted[ref.Name] {
				targeted[ref.Name] = true
				out = append(out, ref)
			}
			walk(ref)
		}
	}
	walk(root)
	return out
}

func recordHasCookExtent(u *ir.Unit, st *ir.Struct) bool {
	if st == nil {
		return false
	}
	for _, f := range st.Fields {
		if f.IsList() || f.IsMap() {
			return true
		}
		if f.Type.Pointer || f.Type.Blob() || f.Type.Kind != ir.TNamed {
			continue
		}
		if ref, ok := f.Type.Ref.(*ir.Struct); ok && recordHasCookExtent(u, ref) {
			return true
		}
	}
	return false
}

func rustElemHasExtent(u *ir.Unit, f *ir.Field) bool {
	if f.Type.Pointer || f.Type.Blob() || f.Type.Kind != ir.TNamed {
		return false
	}
	ref, ok := f.Type.Ref.(*ir.Struct)
	return ok && recordHasCookExtent(u, ref)
}

func rowElemStep(u *ir.Unit, f *ir.Field) int64 {
	single := *f
	single.Array = ir.ArrayNone
	single.ArrayBound = 0
	single.KeyEnum = ""
	single.Type.Optional = false
	pieces := ir.FieldPieces(u, &single, 0)
	if len(pieces) == 0 {
		return 0
	}
	return pieces[0].Size
}

func scalarWidth(f *ir.Field) int64 {
	switch f.Type.Kind {
	case ir.TBool:
		return 1
	case ir.TFloat32:
		return 4
	case ir.TFloat64:
		return 8
	case ir.TBits:
		if f.Type.Width <= 32 {
			return 4
		}
		return 8
	case ir.TInt, ir.TFixed:
		w := int64(f.Type.Width) / 8
		if w < 1 {
			return 1
		}
		return w
	}
	return 1
}

// silence unused import if a file emits no rust-struct writer that needs strings.
var _ = strings.TrimSpace
