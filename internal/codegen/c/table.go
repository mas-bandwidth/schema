package c

// The TABLE wire for C — the evolution-tolerant encoding in WIRES.md.
//
// The bytes are the same ones the other four emit; this is a port of the emit
// rules, not a new format. Where C differs from Rust (the closest analogue,
// since both emit their own runtime rather than calling a hand-written one):
//
//   - no Option, so a nil nested descriptor is a NULL pointer
//   - no methods, so the runtime is free functions over explicit structs
//   - no slices, so a descriptor is a pointer plus a count
//   - the reader assembles its window rather than over-reading, matching
//     serialize.c's own read contract

import (
	"fmt"
	"math"
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

type tableGen struct {
	unit   *ir.Unit
	file   *ir.File
	body   strings.Builder
	errs   []error
	indent string
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

// tableGuardExprs composes each guarded field's branch condition, so the
// writer keeps untaken-branch fields off the wire.
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
				pos, neg := "value->"+item.Cond, "!value->"+item.Cond
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

// tableGuardStrings renders the guard in SCHEMA terms for the descriptor.
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

// ---- write ----

func (g *tableGen) emitTableWrite(st *ir.Struct) {
	lower := snake(st.Name)
	g.pf("/* Appends %s's table-wire encoding to the writer. */\n", st.Name)
	g.pf("static SCHEMA_UNUSED void table_write_%s_into( table_writer_t * w, const %s * value )\n{\n", lower, st.Name)
	if len(st.Fields) == 0 {
		// A fieldless table writes only its terminator; without this the
		// value parameter is unused and every -Werror consumer breaks
		// (found by FuzzGeneratedCompiles, the clang -fsyntax-only leg).
		g.pf("    (void) value; /* no fields: nothing but the terminator */\n")
	}
	guards := tableGuardExprs(st)
	for _, f := range st.Fields {
		if cond, guarded := guards[f.Name]; guarded {
			g.pf("    if ( %s )\n    {\n", cond)
			g.indent = "    "
			g.emitTableWriteField(f)
			g.indent = ""
			g.pf("    }\n")
			continue
		}
		g.emitTableWriteField(f)
	}
	g.pf("    table_w_u16( w, 0 ); /* terminator */\n")
	g.pf("}\n\n")

	g.pf("/* Writes %s's table-wire encoding into buffer, returning the byte count,\n", st.Name)
	g.pf("   or -1 if the buffer was too small. Ask %s_TABLE_MAX_BYTES for a size\n", screaming(st.Name))
	g.pf("   that always fits. */\n")
	g.pf("static SCHEMA_UNUSED int table_write_%s( void * buffer, int bytes, const %s * value )\n{\n", lower, st.Name)
	g.pf("    table_writer_t w;\n")
	g.pf("    w.data = (serialize_uint8_t *) buffer;\n    w.capacity = bytes;\n    w.length = 0;\n    w.overflow = 0;\n")
	g.pf("    table_write_%s_into( &w, value );\n", lower)
	g.pf("    return w.overflow ? -1 : w.length;\n}\n\n")
}

func (g *tableGen) emitTableWriteField(f *ir.Field) {
	id := ir.FieldId(f.Name)
	kind := tableScalarKind(f)
	name := f.Name
	switch {
	case f.Type.Kind == ir.TString:
		g.pf("    if ( value->%s_length > 0 )\n    {\n", name)
		g.pf("        table_w_u16( w, 0x%04x ); /* %s */\n", id, f.Name)
		g.pf("        table_w_u8( w, %d );\n", tkString)
		g.pf("        table_w_u16( w, (serialize_uint16_t) value->%s_length );\n", name)
		g.pf("        table_w_raw( w, (const serialize_uint8_t *) value->%s, (int) value->%s_length );\n    }\n", name, name)
	case f.Type.Kind == ir.TBytes:
		g.pf("    if ( value->%s_length > 0 )\n    {\n", name)
		g.pf("        table_w_u16( w, 0x%04x ); /* %s */\n", id, f.Name)
		g.pf("        table_w_u8( w, %d );\n", tkArray)
		g.pf("        table_w_u32( w, (serialize_uint32_t) ( 3 + value->%s_length ) );\n", name)
		g.pf("        table_w_u8( w, %d );\n", tkU8)
		g.pf("        table_w_u16( w, (serialize_uint16_t) value->%s_length );\n", name)
		g.pf("        table_w_raw( w, value->%s, (int) value->%s_length );\n    }\n", name, name)
	case f.Array == ir.ArrayCounted:
		g.pf("    if ( value->%s_count > 0 )\n    {\n", name)
		g.pf("        int len_at;\n        int32_t i;\n")
		g.pf("        table_w_u16( w, 0x%04x ); /* %s */\n", id, f.Name)
		g.pf("        table_w_u8( w, %d );\n", tkArray)
		g.pf("        len_at = w->length;\n")
		g.pf("        table_w_u32( w, 0 );\n")
		g.pf("        table_w_u8( w, %d );\n", kind)
		g.pf("        table_w_u16( w, (serialize_uint16_t) value->%s_count );\n", name)
		g.pf("        for ( i = 0; i < value->%s_count; i++ )\n        {\n", name)
		g.emitTableWriteElement(f, kind, fmt.Sprintf("value->%s[i]", name), "            ")
		g.pf("        }\n")
		g.pf("        table_w_patch32( w, len_at, (serialize_uint32_t) ( w->length - len_at - 4 ) );\n    }\n")
	case f.Array == ir.ArrayFixed && kind == tkTable:
		g.pf("    {\n        int len_at;\n        int32_t i;\n")
		g.pf("        table_w_u16( w, 0x%04x ); /* %s (fixed [%d]) */\n", id, f.Name, f.ArrayBound)
		g.pf("        table_w_u8( w, %d );\n", tkArray)
		g.pf("        len_at = w->length;\n")
		g.pf("        table_w_u32( w, 0 );\n")
		g.pf("        table_w_u8( w, %d );\n", kind)
		g.pf("        table_w_u16( w, %d );\n", f.ArrayBound)
		g.pf("        for ( i = 0; i < %d; i++ )\n        {\n", f.ArrayBound)
		g.emitTableWriteElement(f, kind, fmt.Sprintf("value->%s[i]", name), "            ")
		g.pf("        }\n")
		g.pf("        table_w_patch32( w, len_at, (serialize_uint32_t) ( w->length - len_at - 4 ) );\n    }\n")
	case f.Array == ir.ArrayFixed:
		g.pf("    {\n        int all_default = 1;\n        int32_t i;\n")
		g.pf("        for ( i = 0; i < %d; i++ )\n        {\n", f.ArrayBound)
		g.pf("            if ( value->%s[i] != %s ) { all_default = 0; break; }\n        }\n", name, g.tableDefaultExpr(f))
		g.pf("        if ( !all_default )\n        {\n")
		g.pf("            int len_at;\n")
		g.pf("            table_w_u16( w, 0x%04x ); /* %s (fixed [%d]) */\n", id, f.Name, f.ArrayBound)
		g.pf("            table_w_u8( w, %d );\n", tkArray)
		g.pf("            len_at = w->length;\n")
		g.pf("            table_w_u32( w, 0 );\n")
		g.pf("            table_w_u8( w, %d );\n", kind)
		g.pf("            table_w_u16( w, %d );\n", f.ArrayBound)
		g.pf("            for ( i = 0; i < %d; i++ )\n            {\n", f.ArrayBound)
		g.emitTableWriteElement(f, kind, fmt.Sprintf("value->%s[i]", name), "                ")
		g.pf("            }\n")
		g.pf("            table_w_patch32( w, len_at, (serialize_uint32_t) ( w->length - len_at - 4 ) );\n        }\n    }\n")
	case kind == tkTable:
		g.pf("    {\n        int field_at = w->length;\n        int len_at;\n")
		g.pf("        table_w_u16( w, 0x%04x ); /* %s */\n", id, f.Name)
		g.pf("        table_w_u8( w, %d );\n", tkTable)
		g.pf("        len_at = w->length;\n")
		g.pf("        table_w_u32( w, 0 );\n")
		g.pf("        table_write_%s_into( w, &value->%s );\n", snake(f.Type.Name), name)
		g.pf("        if ( w->length - len_at - 4 <= 2 )\n        {\n")
		g.pf("            w->length = field_at; /* all-default nested elides */\n        }\n")
		g.pf("        else\n        {\n")
		g.pf("            table_w_patch32( w, len_at, (serialize_uint32_t) ( w->length - len_at - 4 ) );\n        }\n    }\n")
	default:
		g.pf("    if ( value->%s != %s )\n    {\n", name, g.tableDefaultExpr(f))
		g.pf("        table_w_u16( w, 0x%04x ); /* %s */\n", id, f.Name)
		g.pf("        table_w_u8( w, %d );\n", kind)
		g.emitTableWriteElement(f, kind, "value->"+name, "        ")
		g.pf("    }\n")
	}
}

func (g *tableGen) emitTableWriteElement(f *ir.Field, kind int, expr, ind string) {
	switch kind {
	case tkBool:
		g.pf("%stable_w_u8( w, %s ? 1 : 0 );\n", ind, expr)
	case tkF32:
		g.pf("%s{\n%s    serialize_uint32_t bits;\n%s    memcpy( &bits, &%s, 4 );\n", ind, ind, ind, expr)
		g.pf("%s    table_w_u32( w, bits );\n%s}\n", ind, ind)
	case tkF64:
		g.pf("%s{\n%s    serialize_uint64_t bits;\n%s    memcpy( &bits, &%s, 8 );\n", ind, ind, ind, expr)
		g.pf("%s    table_w_u64( w, bits );\n%s}\n", ind, ind)
	case tkTable:
		g.pf("%s{\n%s    int elem_len_at = w->length;\n", ind, ind)
		g.pf("%s    table_w_u32( w, 0 );\n", ind)
		g.pf("%s    table_write_%s_into( w, &%s );\n", ind, snake(f.Type.Name), expr)
		g.pf("%s    table_w_patch32( w, elem_len_at, (serialize_uint32_t) ( w->length - elem_len_at - 4 ) );\n%s}\n", ind, ind)
	default:
		switch tableKindWidth(kind) {
		case 1:
			g.pf("%stable_w_u8( w, (serialize_uint8_t) %s );\n", ind, expr)
		case 2:
			g.pf("%stable_w_u16( w, (serialize_uint16_t) %s );\n", ind, expr)
		case 4:
			g.pf("%stable_w_u32( w, (serialize_uint32_t) %s );\n", ind, expr)
		default:
			g.pf("%stable_w_u64( w, (serialize_uint64_t) %s );\n", ind, expr)
		}
	}
}

// tableDefaultExpr is the value a scalar compares against on the write side —
// identical to new_X()/zero.
func (g *tableGen) tableDefaultExpr(f *ir.Field) string {
	switch f.Type.Kind {
	case ir.TBool:
		if f.HasDefault && f.DefBool {
			return "1"
		}
		return "0"
	case ir.TFloat32, ir.TFloat64:
		if f.HasDefault {
			return formatFloat(f.DefFloat)
		}
		return "0"
	case ir.TInt, ir.TBits:
		if f.HasDefault && f.DefInt != nil {
			// through the extreme guards: INT64_MIN and above-INT64_MAX
			// defaults have no bare decimal spelling (issue #95)
			return intLit(f.DefInt, "")
		}
		return "0"
	case ir.TNamed:
		switch f.Type.Ref.(type) {
		case *ir.Enum:
			if f.HasDefault && f.DefVariant != "" {
				return screaming(f.Type.Name) + "_" + screaming(f.DefVariant)
			}
			return screaming(f.Type.Name) + "_NONE"
		case *ir.Flags:
			return "0"
		}
	}
	return "0"
}

var _ = big.NewInt

// ---- read ----

func (g *tableGen) emitTableRead(st *ir.Struct) {
	lower := snake(st.Name)
	g.pf("/* Decodes a table-wire buffer under the permissive contract: declared\n")
	g.pf("   defaults prefill, known fields overlay, unknown ids and kind mismatches\n")
	g.pf("   skip and count, out-of-range values clamp and count. 0 means malformed —\n")
	g.pf("   the partial decode up to that point is kept. */\n")
	g.pf("static SCHEMA_UNUSED int table_read_%s_from( table_reader_t * r, %s * value, table_report_t * report )\n{\n", lower, st.Name)
	if structHasDefaults(st) {
		g.pf("    *value = new_%s(); /* prefill declared defaults, then overlay */\n", lower)
	} else {
		g.pf("    memset( value, 0, sizeof( *value ) ); /* prefill (all-zero), then overlay */\n")
	}
	g.pf("    for ( ;; )\n    {\n")
	g.pf("        serialize_uint16_t field_id;\n        serialize_uint8_t kind;\n")
	g.pf("        if ( !table_r_has( r, 2 ) ) { report->malformed = 1; return 0; }\n")
	g.pf("        field_id = table_r_u16( r );\n")
	g.pf("        if ( field_id == 0 ) { return 1; }\n")
	g.pf("        if ( !table_r_has( r, 1 ) ) { report->malformed = 1; return 0; }\n")
	g.pf("        kind = table_r_u8( r );\n")
	g.pf("        switch ( field_id )\n        {\n")
	for _, f := range st.Fields {
		id := ir.FieldId(f.Name)
		kind := tableScalarKind(f)
		wireKind := kind
		if f.Array != ir.ArrayNone || f.Type.Kind == ir.TBytes {
			wireKind = tkArray
		}
		if f.Type.Kind == ir.TBytes {
			kind = tkU8
		}
		g.pf("            case 0x%04x: /* %s */\n            {\n", id, f.Name)
		g.pf("                if ( kind != %d )\n                {\n", wireKind)
		g.pf("                    report->kind_mismatch++;\n")
		g.pf("                    if ( !table_r_skip( r, kind ) ) { report->malformed = 1; return 0; }\n")
		g.pf("                }\n                else\n                {\n")
		g.emitTableReadField(f, kind)
		g.pf("                }\n                break;\n            }\n")
	}
	g.pf("            default:\n            {\n")
	g.pf("                report->unknown++;\n")
	g.pf("                if ( !table_r_skip( r, kind ) ) { report->malformed = 1; return 0; }\n")
	g.pf("                break;\n            }\n")
	g.pf("        }\n    }\n}\n\n")

	g.pf("/* Decodes a table-wire buffer into value. */\n")
	g.pf("static SCHEMA_UNUSED int table_read_%s( const void * buffer, int bytes, %s * value, table_report_t * report )\n{\n", lower, st.Name)
	g.pf("    table_reader_t r;\n")
	g.pf("    r.data = (const serialize_uint8_t *) buffer;\n    r.length = bytes;\n    r.off = 0;\n")
	g.pf("    return table_read_%s_from( &r, value, report );\n}\n\n", lower)
}

func (g *tableGen) emitTableReadField(f *ir.Field, kind int) {
	ind := "                    "
	name := f.Name
	switch {
	case f.Type.Kind == ir.TString:
		g.pf("%sint length;\n%sint keep;\n", ind, ind)
		g.pf("%sif ( !table_r_has( r, 2 ) ) { report->malformed = 1; return 0; }\n", ind)
		g.pf("%slength = (int) table_r_u16( r );\n", ind)
		g.pf("%sif ( !table_r_has( r, length ) ) { report->malformed = 1; return 0; }\n", ind)
		g.pf("%skeep = length;\n", ind)
		g.pf("%sif ( keep > %d ) { keep = %d; report->clamped++; }\n", ind, f.Type.Size, f.Type.Size)
		g.pf("%smemcpy( value->%s, r->data + r->off, (size_t) keep );\n", ind, name)
		g.pf("%svalue->%s[keep] = 0;\n", ind, name)
		g.pf("%svalue->%s_length = (int32_t) keep;\n", ind, name)
		g.pf("%sr->off += length;\n", ind)
	case f.Array != ir.ArrayNone || f.Type.Kind == ir.TBytes:
		bound := f.ArrayBound
		if f.Type.Kind == ir.TBytes {
			bound = f.Type.Size
		}
		g.pf("%sint body_len;\n%sint body_end;\n", ind, ind)
		g.pf("%sif ( !table_r_has( r, 4 ) ) { report->malformed = 1; return 0; }\n", ind)
		g.pf("%sbody_len = (int) table_r_u32( r );\n", ind)
		g.pf("%sif ( !table_r_has( r, body_len ) ) { report->malformed = 1; return 0; }\n", ind)
		g.pf("%sbody_end = r->off + body_len;\n", ind)
		g.pf("%sif ( body_len >= 3 )\n%s{\n", ind, ind)
		g.pf("%s    serialize_uint8_t elem_kind = table_r_u8( r );\n", ind)
		g.pf("%s    int count = (int) table_r_u16( r );\n", ind)
		g.pf("%s    if ( elem_kind != %d )\n%s    {\n%s        report->kind_mismatch++;\n%s    }\n", ind, kind, ind, ind, ind)
		g.pf("%s    else\n%s    {\n", ind, ind)
		g.pf("%s        int keep = count;\n%s        int i;\n", ind, ind)
		g.pf("%s        if ( keep > %d ) { keep = %d; report->clamped++; }\n", ind, bound, bound)
		g.pf("%s        for ( i = 0; i < keep; i++ )\n%s        {\n", ind, ind)
		g.emitTableReadElement(f, kind, ind+"            ")
		g.pf("%s        }\n", ind)
		if f.Type.Kind == ir.TBytes {
			g.pf("%s        value->%s_length = (int32_t) keep;\n", ind, name)
		} else if f.Array == ir.ArrayCounted {
			g.pf("%s        value->%s_count = (int32_t) keep;\n", ind, name)
		}
		g.pf("%s    }\n%s}\n", ind, ind)
		g.pf("%sr->off = body_end; /* excess elements and slack skip via the length */\n", ind)
	case kind == tkTable:
		g.pf("%sint body_len;\n", ind)
		g.pf("%sif ( !table_r_has( r, 4 ) ) { report->malformed = 1; return 0; }\n", ind)
		g.pf("%sbody_len = (int) table_r_u32( r );\n", ind)
		g.pf("%sif ( !table_r_has( r, body_len ) ) { report->malformed = 1; return 0; }\n", ind)
		g.pf("%s{\n%s    table_reader_t sub;\n", ind, ind)
		g.pf("%s    sub.data = r->data + r->off;\n%s    sub.length = body_len;\n%s    sub.off = 0;\n", ind, ind, ind)
		g.pf("%s    table_read_%s_from( &sub, &value->%s, report );\n", ind, snake(f.Type.Name), name)
		g.pf("%s}\n%sr->off += body_len;\n", ind, ind)
	default:
		g.emitTableReadScalarInto(f, kind, "value->"+name, ind)
	}
}

func (g *tableGen) emitTableReadElement(f *ir.Field, kind int, ind string) {
	name := f.Name
	switch kind {
	case tkTable:
		g.pf("%sif ( !table_r_has( r, 4 ) ) { report->malformed = 1; return 0; }\n", ind)
		g.pf("%s{\n%s    int elem_len = (int) table_r_u32( r );\n", ind, ind)
		g.pf("%s    table_reader_t sub;\n", ind)
		g.pf("%s    if ( !table_r_has( r, elem_len ) ) { report->malformed = 1; return 0; }\n", ind)
		g.pf("%s    sub.data = r->data + r->off;\n%s    sub.length = elem_len;\n%s    sub.off = 0;\n", ind, ind, ind)
		g.pf("%s    table_read_%s_from( &sub, &value->%s[i], report );\n", ind, snake(f.Type.Name), name)
		g.pf("%s    r->off += elem_len;\n%s}\n", ind, ind)
	default:
		g.emitTableReadScalarInto(f, kind, fmt.Sprintf("value->%s[i]", name), ind)
	}
}

func (g *tableGen) emitTableReadScalarInto(f *ir.Field, kind int, lvalue, ind string) {
	width := tableKindWidth(kind)
	g.pf("%sif ( !table_r_has( r, %d ) ) { report->malformed = 1; return 0; }\n", ind, width)
	switch kind {
	case tkBool:
		g.pf("%s%s = table_r_u8( r ) != 0;\n", ind, lvalue)
		return
	case tkF32:
		g.pf("%s{\n%s    serialize_uint32_t bits = table_r_u32( r );\n%s    float raw;\n%s    memcpy( &raw, &bits, 4 );\n", ind, ind, ind, ind)
		g.emitClampFloat(f, "raw", lvalue, ind+"    ", "float")
		g.pf("%s}\n", ind)
		return
	case tkF64:
		g.pf("%s{\n%s    serialize_uint64_t bits = table_r_u64( r );\n%s    double raw;\n%s    memcpy( &raw, &bits, 8 );\n", ind, ind, ind, ind)
		g.emitClampFloat(f, "raw", lvalue, ind+"    ", "double")
		g.pf("%s}\n", ind)
		return
	}
	get := map[int]string{1: "table_r_u8( r )", 2: "table_r_u16( r )", 4: "table_r_u32( r )", 8: "table_r_u64( r )"}[width]
	signed := kind >= tkI8 && kind <= tkI64
	g.pf("%s{\n", ind)
	if signed {
		g.pf("%s    serialize_int%d_t raw = (serialize_int%d_t) %s;\n", ind, width*8, width*8, get)
	} else {
		g.pf("%s    serialize_uint%d_t raw = %s;\n", ind, width*8, get)
	}
	g.emitClampInt(f, kind, "raw", lvalue, ind+"    ")
	g.pf("%s}\n", ind)
}

// emitClampInt applies the declared range or enum variant set before storing,
// counting a clamp in the report — the permissive contract.
func (g *tableGen) emitClampInt(f *ir.Field, kind int, src, lvalue, ind string) {
	storage := g.tableStorage(f, kind)

	if f.Type.Kind == ir.TNamed {
		if e, isEnum := f.Type.Ref.(*ir.Enum); isEnum {
			g.pf("%s%s v = %s;\n", ind, storage, src)
			g.pf("%sif ( (serialize_uint64_t) v > %d ) { v = 0; report->clamped++; }\n", ind, e.Max)
			g.pf("%s%s = v;\n", ind, lvalue)
			return
		}
		if _, isFlags := f.Type.Ref.(*ir.Flags); isFlags {
			g.pf("%s%s = (%s) %s;\n", ind, lvalue, storage, src)
			return
		}
	}
	if !f.HasIntRange || f.IntMin == nil || f.IntMax == nil {
		g.pf("%s%s = (%s) %s;\n", ind, lvalue, storage, src)
		return
	}
	// The clamp carrier is int64 — except for an unsigned 64-bit field, which
	// clamps in ITS OWN domain: its bounds can live above INT64_MAX (no LL
	// spelling holds them, issue #95), and the int64 detour would fold large
	// decoded values negative — the C++ reader clamps unsigned 64-bit values
	// as uint64_t, and the two readers must agree. Each half is emitted only
	// when it CAN be false for the carrier: a bound at the carrier's own
	// extreme makes the compare tautological, which -Wtype-limits rejects and
	// the C legs build with -Werror (the same elision the wire-side range
	// assert applies).
	carrier, suffix := "serialize_int64_t", "LL"
	loNeeded := !f.IntMin.IsInt64() || f.IntMin.Int64() > math.MinInt64
	hiNeeded := !f.IntMax.IsInt64() || f.IntMax.Int64() < math.MaxInt64
	if kind == tkU64 {
		carrier, suffix = "serialize_uint64_t", "ULL"
		maxU64 := new(big.Int).SetUint64(^uint64(0))
		loNeeded = f.IntMin.Sign() > 0
		hiNeeded = f.IntMax.Cmp(maxU64) < 0
	}
	if !loNeeded && !hiNeeded {
		g.pf("%s%s = (%s) %s;\n", ind, lvalue, storage, src)
		return
	}
	g.pf("%s{\n%s    %s v = (%s) %s;\n", ind, ind, carrier, carrier, src)
	if loNeeded {
		lo := intLit(f.IntMin, suffix)
		g.pf("%s    if ( v < %s ) { v = %s; report->clamped++; }\n", ind, lo, lo)
	}
	if hiNeeded {
		hi := intLit(f.IntMax, suffix)
		g.pf("%s    if ( v > %s ) { v = %s; report->clamped++; }\n", ind, hi, hi)
	}
	g.pf("%s    %s = (%s) v;\n%s}\n", ind, lvalue, storage, ind)
}

func (g *tableGen) emitClampFloat(f *ir.Field, src, lvalue, ind, ty string) {
	if !f.HasFloatRange {
		g.pf("%s%s = %s;\n", ind, lvalue, src)
		return
	}
	g.pf("%sif ( %s < (%s) %s ) { %s = (%s) %s; report->clamped++; }\n",
		ind, src, ty, formatFloat(f.FMin), src, ty, formatFloat(f.FMin))
	g.pf("%sif ( %s > (%s) %s ) { %s = (%s) %s; report->clamped++; }\n",
		ind, src, ty, formatFloat(f.FMax), src, ty, formatFloat(f.FMax))
	g.pf("%s%s = %s;\n", ind, lvalue, src)
}

// tableStorage is the generated storage type a scalar decodes into.
func (g *tableGen) tableStorage(f *ir.Field, kind int) string {
	switch f.Type.Kind {
	case ir.TInt:
		return cInt(f.Type.Signed, f.Type.Width)
	case ir.TBits:
		if f.Type.Width <= 32 {
			return "uint32_t"
		}
		return "uint64_t"
	case ir.TFloat32:
		return "float"
	case ir.TFloat64:
		return "double"
	case ir.TBool:
		return "int"
	case ir.TNamed:
		return f.Type.Name
	}
	if kind >= tkI8 && kind <= tkI64 {
		return fmt.Sprintf("int%d_t", tableKindWidth(kind)*8)
	}
	return fmt.Sprintf("uint%d_t", tableKindWidth(kind)*8)
}

// ---- reflection descriptors ----

func (g *tableGen) emitTableDescriptor(st *ir.Struct) {
	upper := screaming(st.Name)
	guards := tableGuardStrings(st)

	g.pf("static const table_field_info_t TABLE_TYPE_%s_FIELDS[] = {\n", upper)
	for _, f := range st.Fields {
		g.pf("    { %s },\n", strings.Join(g.tableDescriptorParts(f, guards[f.Name]), ", "))
	}
	if len(st.Fields) == 0 {
		g.pf("    { 0 } /* C forbids an empty initializer */\n")
	}
	g.pf("};\n\n")

	g.pf("static const table_type_info_t TABLE_TYPE_%s = {\n", upper)
	g.pf("    %q,\n", st.Name)
	g.pf("    TABLE_TYPE_%s_FIELDS,\n", upper)
	g.pf("    %d\n};\n\n", len(st.Fields))

	g.pf("/* Returns %s's reflection descriptor — field names, wire ids/kinds,\n", st.Name)
	g.pf("   bounds, ranges, enum names and branch guards. Static data: no\n")
	g.pf("   per-instance cost and no lazy initialization. */\n")
	g.pf("static SCHEMA_UNUSED const table_type_info_t * table_type_%s( void ) { return &TABLE_TYPE_%s; }\n\n",
		snake(st.Name), upper)
}

func (g *tableGen) tableDescriptorParts(f *ir.Field, guard string) []string {
	kind := tableScalarKind(f)
	if f.Type.Kind == ir.TBytes {
		kind = tkU8
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

	table := "NULL"
	if _, isStruct := f.Type.Ref.(*ir.Struct); f.Type.Kind == ir.TNamed && isStruct {
		table = "&TABLE_TYPE_" + screaming(f.Type.Name)
	}

	hasRange, rangeMin, rangeMax := "0", "0.0", "0.0"
	if f.HasIntRange && f.IntMin != nil && f.IntMax != nil {
		hasRange = "1"
		rangeMin = formatFloat(bigToFloat(f.IntMin))
		rangeMax = formatFloat(bigToFloat(f.IntMax))
	} else if f.HasFloatRange {
		hasRange = "1"
		rangeMin = formatFloat(f.FMin)
		rangeMax = formatFloat(f.FMax)
	}

	enumMax, enumName := "-1", "NULL"
	if enum, isEnum := f.Type.Ref.(*ir.Enum); f.Type.Kind == ir.TNamed && isEnum {
		enumMax = fmt.Sprintf("%d", enum.Max)
		enumName = "enum_name_" + snake(f.Type.Name) + "_dyn"
	}

	return []string{
		fmt.Sprintf("%q", f.Name),
		fmt.Sprintf("%q", tableFieldTypeName(f)),
		fmt.Sprintf("0x%04x", ir.FieldId(f.Name)),
		fmt.Sprintf("%d", kind),
		boolLit(isArray),
		boolLit(counted),
		fmt.Sprintf("%d", arrayBound),
		table,
		hasRange,
		rangeMin,
		rangeMax,
		enumMax,
		enumName,
		fmt.Sprintf("%q", guard),
	}
}

func boolLit(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

func bigToFloat(v *big.Int) float64 {
	f, _ := new(big.Float).SetInt(v).Float64()
	return f
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

// ---- entry point ----

// GenerateTable emits <Base>Table.h per file that declares or reaches a table,
// plus the shared TableRuntime.h.
func GenerateTable(u *ir.Unit) (map[string][]byte, error) {
	if err := ir.CheckTableIds(u); err != nil {
		return nil, err
	}
	for _, f := range u.Files {
		if f.Base == "TableRuntime" {
			return nil, fmt.Errorf("schema file TableRuntime collides with the generated C table runtime header; rename it")
		}
	}

	closure := ir.TableClosure(u)
	out := map[string][]byte{}
	var errs []error

	// Only a file with table-closure members emits a Table.h, so the include
	// list has to be filtered — a dependency that declares no table has no
	// header to include, and naming it is a build failure in the consumer.
	hasTable := map[string]bool{}
	for _, f := range u.Files {
		for _, d := range f.Decls {
			if st, ok := d.(*ir.Struct); ok && closure[st.Name] {
				hasTable[f.Base] = true
				break
			}
		}
	}

	for _, f := range u.Files {
		var members []*ir.Struct
		// emission order, not declaration order: table_write_a_into calls
		// table_write_b_into, and C99 has no implicit declarations
		for _, d := range ir.EmissionOrder(f) {
			if st, ok := d.(*ir.Struct); ok && closure[st.Name] {
				members = append(members, st)
			}
		}
		if len(members) == 0 {
			continue
		}

		g := &tableGen{unit: u, file: f}
		for _, st := range members {
			g.emitTableWrite(st)
			g.emitTableRead(st)
		}
		g.pf("/* ---- reflection descriptors (tables only) ---- */\n\n")
		for _, st := range members {
			g.emitTableDescriptor(st)
		}
		errs = append(errs, g.errs...)

		var h strings.Builder
		fmt.Fprintf(&h, "/* Generated by the schema compiler from %s.schema. DO NOT EDIT.\n", f.Base)
		h.WriteString("   SPDX-License-Identifier: NONE — this generated output is yours, under terms of\n")
		h.WriteString("   your choice. See the LICENSE exception in the schema compiler; the compiler is\n")
		h.WriteString("   AGPL-3.0, its output is not.\n")
		fmt.Fprintf(&h, "   package %s — protocol id 0x%016x\n", u.Package, u.ProtocolId)
		h.WriteString("   The TABLE wire (evolution-tolerant). */\n\n")
		guard := "SCHEMA_" + screaming(u.Package) + "_" + screaming(f.Base) + "TABLE_H"
		fmt.Fprintf(&h, "#ifndef %s\n#define %s\n\n", guard, guard)
		fmt.Fprintf(&h, "#include \"%s.h\"\n", f.Base)
		h.WriteString("#include \"TableRuntime.h\"\n")
		for _, dep := range sortedDeps(ir.FileDeps(u)[f.Base]) {
			if hasTable[dep] {
				fmt.Fprintf(&h, "#include \"%sTable.h\"\n", dep)
			}
		}
		h.WriteString("\n#ifdef __cplusplus\nextern \"C\" {\n#endif\n\n")
		h.WriteString(g.body.String())
		h.WriteString("#ifdef __cplusplus\n}\n#endif\n\n")
		fmt.Fprintf(&h, "#endif /* %s */\n", guard)
		out[f.Base+"Table.h"] = []byte(h.String())
	}

	if len(errs) > 0 {
		var b strings.Builder
		for i, e := range errs {
			if i > 0 {
				b.WriteString("\n")
			}
			b.WriteString(e.Error())
		}
		return nil, fmt.Errorf("%s", b.String())
	}

	if len(out) > 0 {
		out["TableRuntime.h"] = []byte(tableRuntime())
	}
	return out, nil
}

// tableRuntime is the generated table-wire runtime: the byte-level writer and
// reader, the report, the descriptor types, and the skip rule that makes
// unknown and kind-changed fields harmless.
//
// Emitted rather than kept in serialize.c because the table wire does no
// bit-level work — it is plain little-endian bytes, and a dependency for that
// would buy nothing.
func tableRuntime() string {
	return `/* Generated by the schema compiler. DO NOT EDIT.
   SPDX-License-Identifier: NONE — this generated output is yours, under terms of
   your choice. See the LICENSE exception in the schema compiler; the compiler is
   AGPL-3.0, its output is not.
   The TABLE wire runtime (evolution-tolerant). */

#ifndef SCHEMA_TABLE_RUNTIME_H
#define SCHEMA_TABLE_RUNTIME_H

#include <stdint.h>
#include <string.h>
#include "serialize.h"

#ifndef SCHEMA_UNUSED
#if defined(__GNUC__) || defined(__clang__)
#define SCHEMA_UNUSED __attribute__((unused))
#else
#define SCHEMA_UNUSED
#endif
#endif

#ifdef __cplusplus
extern "C" {
#endif

/* Counts the permissive read contract's events: how far the data diverged from
   this build's schema, without anything crashing or rejecting. */
typedef struct table_report_t
{
    int32_t unknown;        /* fields this schema does not declare (newer data) */
    int32_t kind_mismatch;  /* fields whose wire kind changed (skipped, defaults kept) */
    int32_t clamped;        /* values pulled into declared ranges / sets / bounds */
    int malformed;          /* structurally broken buffer; partial decode was kept */
} table_report_t;

/* ---- reflection ----

   Static field descriptors for every type in the table closure: name, wire
   id/kind, bounds, ranges, enum names and branch guards — enough to walk,
   print, diff, edit or bind any table value at runtime with no schema files. */

struct table_type_info_t;

typedef struct table_field_info_t
{
    const char * name;        /* schema field name */
    const char * type_name;   /* schema type name */
    uint16_t id;              /* table-wire field id (name hash) */
    uint8_t kind;             /* table-wire kind; for arrays/bytes, the ELEMENT kind */
    int is_array;             /* fixed or counted array (bytes included) */
    int counted;              /* a count/length companion exists */
    int64_t array_bound;      /* array capacity / string max length; 0 for scalars */
    const struct table_type_info_t * table;  /* nested table's descriptor, or NULL */
    int has_range;            /* a declared [min, max] */
    double range_min;         /* NOTE: int64 ranges beyond 2^53 lose precision here */
    double range_max;
    int64_t enum_max;         /* enums: highest valid wire value; else -1 */
    const char * ( * enum_name )( uint64_t );  /* enums: value -> name; else NULL */
    const char * guard;       /* branch guard, e.g. "at_rest"; "" if unguarded */
} table_field_info_t;

typedef struct table_type_info_t
{
    const char * name;
    const table_field_info_t * fields;
    int field_count;
} table_type_info_t;

/* ---- the byte-level writer ----

   Bounded: a write past capacity sets overflow and is dropped, so a caller can
   check once at the end rather than after every field. */

typedef struct table_writer_t
{
    serialize_uint8_t * data;
    int capacity;
    int length;
    int overflow;
} table_writer_t;

static SCHEMA_UNUSED void table_w_raw( table_writer_t * w, const serialize_uint8_t * bytes, int count )
{
    if ( w->overflow || w->length + count > w->capacity )
    {
        w->overflow = 1;
        return;
    }
    memcpy( w->data + w->length, bytes, (size_t) count );
    w->length += count;
}

static SCHEMA_UNUSED void table_w_u8( table_writer_t * w, serialize_uint8_t v )
{
    table_w_raw( w, &v, 1 );
}

static SCHEMA_UNUSED void table_w_u16( table_writer_t * w, serialize_uint16_t v )
{
    serialize_uint8_t b[2];
    b[0] = (serialize_uint8_t) ( v & 0xFF );
    b[1] = (serialize_uint8_t) ( ( v >> 8 ) & 0xFF );
    table_w_raw( w, b, 2 );
}

static SCHEMA_UNUSED void table_w_u32( table_writer_t * w, serialize_uint32_t v )
{
    serialize_uint8_t b[4];
    int i;
    for ( i = 0; i < 4; i++ )
    {
        b[i] = (serialize_uint8_t) ( ( v >> ( i * 8 ) ) & 0xFF );
    }
    table_w_raw( w, b, 4 );
}

static SCHEMA_UNUSED void table_w_u64( table_writer_t * w, serialize_uint64_t v )
{
    serialize_uint8_t b[8];
    int i;
    for ( i = 0; i < 8; i++ )
    {
        b[i] = (serialize_uint8_t) ( ( v >> ( i * 8 ) ) & 0xFF );
    }
    table_w_raw( w, b, 8 );
}

static SCHEMA_UNUSED void table_w_patch32( table_writer_t * w, int off, serialize_uint32_t v )
{
    int i;
    if ( w->overflow || off + 4 > w->length )
    {
        return;
    }
    for ( i = 0; i < 4; i++ )
    {
        w->data[off + i] = (serialize_uint8_t) ( ( v >> ( i * 8 ) ) & 0xFF );
    }
}

/* ---- the byte-level reader ---- */

typedef struct table_reader_t
{
    const serialize_uint8_t * data;
    int length;
    int off;
} table_reader_t;

static SCHEMA_UNUSED int table_r_has( const table_reader_t * r, int n )
{
    return n >= 0 && r->off + n <= r->length;
}

static SCHEMA_UNUSED serialize_uint8_t table_r_u8( table_reader_t * r )
{
    serialize_uint8_t v = r->data[r->off];
    r->off += 1;
    return v;
}

static SCHEMA_UNUSED serialize_uint16_t table_r_u16( table_reader_t * r )
{
    serialize_uint16_t v = (serialize_uint16_t) ( (serialize_uint16_t) r->data[r->off] |
                           ( (serialize_uint16_t) r->data[r->off + 1] << 8 ) );
    r->off += 2;
    return v;
}

static SCHEMA_UNUSED serialize_uint32_t table_r_u32( table_reader_t * r )
{
    serialize_uint32_t v = 0;
    int i;
    for ( i = 0; i < 4; i++ )
    {
        v |= ( (serialize_uint32_t) r->data[r->off + i] ) << ( i * 8 );
    }
    r->off += 4;
    return v;
}

static SCHEMA_UNUSED serialize_uint64_t table_r_u64( table_reader_t * r )
{
    serialize_uint64_t v = 0;
    int i;
    for ( i = 0; i < 8; i++ )
    {
        v |= ( (serialize_uint64_t) r->data[r->off + i] ) << ( i * 8 );
    }
    r->off += 8;
    return v;
}

/* Advances past one field payload of the given kind — how unknown and
   kind-changed fields stay harmless. */
static SCHEMA_UNUSED int table_r_skip( table_reader_t * r, serialize_uint8_t kind )
{
    switch ( kind )
    {
        case 1: case 2: case 6:               /* bool, i8, u8 */
            if ( !table_r_has( r, 1 ) ) return 0;
            r->off += 1;
            return 1;
        case 3: case 7:                       /* i16, u16 */
            if ( !table_r_has( r, 2 ) ) return 0;
            r->off += 2;
            return 1;
        case 4: case 8: case 10:              /* i32, u32, f32 */
            if ( !table_r_has( r, 4 ) ) return 0;
            r->off += 4;
            return 1;
        case 5: case 9: case 11:              /* i64, u64, f64 */
            if ( !table_r_has( r, 8 ) ) return 0;
            r->off += 8;
            return 1;
        case 12:                              /* string: u16 length then bytes */
        {
            int n;
            if ( !table_r_has( r, 2 ) ) return 0;
            n = (int) table_r_u16( r );
            if ( !table_r_has( r, n ) ) return 0;
            r->off += n;
            return 1;
        }
        case 13: case 14:                     /* table / array: u32 body then body */
        {
            int n;
            if ( !table_r_has( r, 4 ) ) return 0;
            n = (int) table_r_u32( r );
            if ( !table_r_has( r, n ) ) return 0;
            r->off += n;
            return 1;
        }
        default:
            return 0;
    }
}

#ifdef __cplusplus
}
#endif

#endif /* SCHEMA_TABLE_RUNTIME_H */
`
}
