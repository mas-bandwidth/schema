// The value bridge joins the already-carried typed JSON records to the
// table wire's flat representation. It moves storage, never reimplements a
// JSON or wire grammar. Pointered closures retain their explicit absence.
package goosetable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

type valueEmit struct {
	*jsonEmit
	tmp int
}

func (e *valueEmit) fresh() string { e.tmp++; return fmt.Sprintf("x%d", e.tmp) }

func (e *valueEmit) emitStruct(st *ir.Struct, root bool) {
	n, typ := st.Name, e.structType(st)
	e.g.Import("wire_value")
	e.declare("_"+n+"ValuePack", "_"+n+"ValueUnpack")
	e.p(0, "fn _%sValuePack(v: const %s&, unit: schema_rt::LoadUnit, data: u8[>..]&, vals: schema_rt::LoadVal[>..]&) -> i64 {", n, typ)
	e.p(1, "let tab = schema_rt::value_table(unit, %q);", n)
	e.p(1, "if tab < 0 { return -1; }")
	e.p(1, "let rec = schema_rt::value_reserve(vals, %d);", len(st.Fields)+1)
	e.p(1, "vals[rec] = schema_rt::LoadVal { state: schema_rt::ValPresent, shape: schema_rt::ShapeTab, made: true, tab: tab, first: rec + 1, count: %d, .. };", len(st.Fields))
	for i, f := range st.Fields {
		e.field(1, f, fieldPlace("v", f), fmt.Sprintf("_%sFields[%d]", n, i), fmt.Sprintf("rec + %d", i+1), true)
	}
	e.p(1, "return rec;")
	e.p(0, "}\n")
	e.p(0, "fn _%sValueUnpack(v: %s&, data: const u8[:], vals: const schema_rt::LoadVal[:], rec: i64) {", n, typ)
	e.p(1, "v = %s { .. };", typ)
	for i, f := range st.Fields {
		s := e.fresh()
		e.p(1, "let %s = schema_rt::value_child(vals, rec, %d);", s, i)
		e.field(1, f, fieldPlace("v", f), fmt.Sprintf("_%sFields[%d]", n, i), s, false)
	}
	e.p(0, "}\n")
	if !root {
		return
	}
	e.declare(n+"ToLoaded", n+"FromLoaded")
	e.p(0, "// %sToLoaded appends a form-1 value tree; -1 restores both caller stores.", n)
	e.p(0, "fn %sToLoaded(v: const %s&, unit: schema_rt::LoadUnit, data: u8[>..]&, vals: schema_rt::LoadVal[>..]&) -> i64 {", n, typ)
	e.p(1, "let bytes = data.len; let slots = vals.len;")
	e.p(1, "let rec = _%sValuePack(v, unit, data, vals);", n)
	e.p(1, "if rec < 0 { data.resize(bytes); vals.resize(slots); }")
	e.p(1, "return rec;")
	e.p(0, "}\n")
	e.p(0, "// %sFromLoaded converts a successful load of this root; absent fields keep their defaults.", n)
	e.p(0, "fn %sFromLoaded(v: %s&, unit: schema_rt::LoadUnit, data: const u8[:], vals: const schema_rt::LoadVal[:], rec: i64) -> bool {", n, typ)
	e.p(1, "v = %s { .. };", typ)
	e.p(1, "if !schema_rt::value_rode(vals, rec) || !vals[rec].made || vals[rec].tab < 0 || vals[rec].tab >= unit.tables.len { return false; }")
	e.p(1, "if unit.tables[vals[rec].tab].name != %q { return false; }", n)
	e.p(1, "_%sValueUnpack(v, data, vals, rec);", n)
	e.p(1, "return true;")
	e.p(0, "}\n")
}

func (e *valueEmit) emitUnion(un *ir.Union) {
	n, typ := un.Name, e.unionType(un)
	e.g.Import("wire_value")
	e.declare("_"+n+"ValuePack", "_"+n+"ValueUnpack")
	e.p(0, "fn _%sValuePack(v: const %s&, unit: schema_rt::LoadUnit, data: u8[>..]&, vals: schema_rt::LoadVal[>..]&) -> i64 {", n, typ)
	e.p(1, "if v.Type > %d { return -1; }", len(un.Variants))
	e.p(1, "let rec = schema_rt::value_reserve(vals, 2);")
	e.p(1, "vals[rec] = schema_rt::LoadVal { state: schema_rt::ValPresent, shape: schema_rt::ShapeUnion, made: true, tab: schema_rt::value_union(unit, %q), bits: schema_rt::Int128 { lo: v.Type, hi: 0 }, first: rec + 1, count: 1, .. };", n)
	for i, v := range un.Variants {
		if v.Void() {
			continue
		}
		f := armField(v)
		e.p(1, "if v.Type == %d {", i+1)
		e.field(2, f, fieldPlace("v", f), fmt.Sprintf("_%sArms[%d]", n, i), "rec + 1", true)
		e.p(1, "}")
	}
	e.p(1, "return rec;")
	e.p(0, "}\n")
	e.p(0, "fn _%sValueUnpack(v: %s&, data: const u8[:], vals: const schema_rt::LoadVal[:], rec: i64) {", n, typ)
	e.p(1, "v = %s { .. };", typ)
	e.p(1, "if !schema_rt::value_rode(vals, rec) { return; }")
	if un.StorageBits == 64 {
		e.p(1, "v.Type = vals[rec].bits.lo;")
	} else {
		e.p(1, "v.Type = vals[rec].bits.lo as! %sType;", n)
	}
	e.p(1, "let payload = schema_rt::value_child(vals, rec, 0);")
	for i, v := range un.Variants {
		if v.Void() {
			continue
		}
		f := armField(v)
		e.p(1, "if v.Type == %d {", i+1)
		e.field(2, f, fieldPlace("v", f), fmt.Sprintf("_%sArms[%d]", n, i), "payload", false)
		e.p(1, "}")
	}
	e.p(0, "}\n")
}

func (e *valueEmit) field(ind int, f *ir.Field, pl place, desc, s string, pack bool) {
	if pack {
		if f.Type.Optional {
			e.p(ind, "if %sPresent {", pl.val)
			ind++
		}
		e.p(ind, "vals[%s] = schema_rt::LoadVal { state: schema_rt::ValPresent, shape: %s.shape, .. };", s, desc)
	} else {
		if f.Type.Optional {
			e.p(ind, "%sPresent = schema_rt::value_rode(vals, %s);", pl.val, s)
		}
		e.p(ind, "if schema_rt::value_rode(vals, %s) {", s)
		ind++
	}
	if f.Array != ir.ArrayNone || f.KeyEnum != "" {
		n, first, i, elem := e.fresh(), e.fresh(), e.fresh(), e.fresh()
		e.p(ind, "let %s = %s.elem;", elem, desc)
		if pack {
			e.p(ind, "guard %s else { return -1; }", elem)
		} else {
			e.p(ind, "guard %s else { return; }", elem)
		}
		if pack {
			if f.Array == ir.ArrayCounted {
				e.p(ind, "let %s = %sCount as i64;", n, pl.val)
				e.p(ind, "if %s < %d || %s > %d { return -1; }", n, f.ArrayMin, n, slotCount(f))
			} else {
				e.p(ind, "let %s: i64 = %d;", n, slotCount(f))
			}
			e.p(ind, "let %s = schema_rt::value_reserve(vals, %s);", first, n)
			e.p(ind, "vals[%s].made = true; vals[%s].first = %s; vals[%s].count = %s; vals[%s].len = %s;", s, s, first, s, n, s, n)
			e.p(ind, "for %s in %s {", i, n)
			el := elemOf(f)
			e.field(ind+1, el, pl.at(i), elem, first+" + "+i, true)
		} else {
			if f.Array == ir.ArrayCounted {
				e.p(ind, "%sCount = (if vals[%s].len < %d { vals[%s].len } else { %d }) as! i32;", pl.val, s, slotCount(f), s, slotCount(f))
			}
			e.p(ind, "for %s in %d {", i, slotCount(f))
			e.p(ind+1, "let %s = schema_rt::value_child(vals, %s, %s);", first, s, i)
			e.field(ind+1, elemOf(f), pl.at(i), elem, first, false)
		}
		e.p(ind, "}")
	} else {
		e.scalar(ind, f, pl, desc, s, pack)
	}
	if !pack {
		ind--
		e.p(ind, "}")
	} else if f.Type.Optional {
		ind--
		e.p(ind, "}")
	}
}

func (e *valueEmit) scalar(ind int, f *ir.Field, pl place, desc, s string, pack bool) {
	t := f.Type
	switch t.Kind {
	case ir.TString, ir.TBytes:
		if pack {
			e.p(ind, "if !schema_rt::value_put_text(data, vals, %s, %s, %s) { return -1; }", s, pl.val, pl.length)
		} else {
			e.p(ind, "%s = schema_rt::value_get_text(data, vals, %s, %s);", pl.length, s, pl.val)
		}
		return
	case ir.TNamed:
		switch r := t.Ref.(type) {
		case *ir.Struct:
			e.structType(r)
			e.record(ind, r.Name, pl.val, s, pack)
			return
		case *ir.Union:
			e.unionType(r)
			e.record(ind, r.Name, pl.val, s, pack)
			return
		}
	}
	bits := fmt.Sprintf("vals[%s].bits.lo", s)
	if pack {
		lo, hi := pl.val, "0"
		switch t.Kind {
		case ir.TBool:
			lo = "if " + pl.val + " { 1 } else { 0 }"
		case ir.TFloat32:
			lo = "schema_rt::f32_bits(" + pl.val + ")"
		case ir.TFloat64:
			lo = "schema_rt::f64_bits(" + pl.val + ")"
		case ir.TInt:
			if strings.HasPrefix(elemTypeOf(e.g.Slots(f)[0].Type), "i") {
				lo = pl.val + " as! u64"
				hi = "if " + pl.val + " < 0 { 0xFFFFFFFFFFFFFFFF } else { 0 }"
			}
		}
		e.p(ind, "vals[%s].bits = schema_rt::Int128 { lo: %s, hi: %s };", s, lo, hi)
	} else {
		value := bits
		switch t.Kind {
		case ir.TBool:
			value = bits + " != 0"
		case ir.TFloat32:
			value = "schema_rt::fixed_bits_f32(" + bits + " as! u32)"
		case ir.TFloat64:
			value = "schema_rt::fixed_bits_f64(" + bits + ")"
		default:
			typ := elemTypeOf(e.g.Slots(f)[0].Type)
			wide := typ == "u64"
			if r, ok := t.Ref.(*ir.Flags); ok {
				typ = e.g.TypeRef(r.Name, false)
				wide = true
			}
			if r, ok := t.Ref.(*ir.Enum); ok {
				typ = e.g.TypeRef(r.Name, false)
				wide = ir.StorageBitsFor(r.Max) == 64
			}
			if !wide {
				value = bits + " as! " + typ
			}
		}
		e.p(ind, "%s = %s;", pl.val, value)
	}
}
func (e *valueEmit) record(ind int, name, pl, s string, pack bool) {
	if pack {
		x := e.fresh()
		e.p(ind, "let %s = _%sValuePack(%s, unit, data, vals);", x, name, pl)
		e.p(ind, "if %s < 0 { return -1; }", x)
		e.p(ind, "vals[%s] = vals[%s];", s, x)
	} else {
		e.p(ind, "_%sValueUnpack(%s, data, vals, %s);", name, pl, s)
	}
}
