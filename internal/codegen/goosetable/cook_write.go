// THE COOK'S WRITE SIDE (docs/SPEC-TABLES.md §7.6) for Goose, from the TYPED
// records: every FIXED root's <Name>CookMeasure and <Name>Cook, and, in the
// cook's package module, every closure record's <Row>CookPut — the record written PIECE BY
// PIECE at the offsets ir.RecordLayout gives, in the byte order the call
// names, never copied as a struct (§7.6: a swap has to know where every
// scalar begins, and every byte no field covers is zero). Beside each record's
// put is <Row>CookFromLoaded, the typed copy out of the table wire's flat
// value table (runtime/wire_load.goose, THE LOADED VALUE), so a wire a load
// landed cooks through the same typed record a program builds.
//
// The writer is the Go reference's fixed writer (internal/codegen/gotable,
// tableCookFixed) field for field: a counted array writes all N slots, a
// string writes its used bytes and leaves its tail zero, an optional writes
// its presence and then its value, a union writes its tag and the SET arm at
// the arm offset; a count outside its bound, a union tag past the arms, a
// deprecated enum variant and a non-finite float are refusals, and a refusal
// writes nothing — the value is walked once dry, then the extent is zeroed
// and walked again.
//
// A ROOT THE TYPED WRITER DOES NOT CARRY — a POINTERED root (a variable
// closure: pointers, lists, maps, blobs, unbounded text), or a fixed one whose
// closure holds a kind the typed writer has no spelling for — cooks a REGION
// numbered by the depth-first first-visit walk of §3.1 (§7.6), and that
// writer is runtime/cook_write.goose's: one walk over the table wire's FLAT
// VALUE TABLE, which is the Goose value of a variable table, reading the
// unit's layout as data. This file emits that data once per unit, into the
// cook's package module — CookRegionLayout, its tables and unions parallel to
// TableUnit's (tablewire.go) — and each such root's <Name>CookMeasure and
// <Name>Cook over a loaded value.
package goosetable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// cookWritable reports whether a record's typed writer is emitted: every
// field, through nested records and union arms, is a by-value kind the fixed
// writer carries. why names the first that is not.
func cookWritable(u *ir.Unit, st *ir.Struct, seen map[string]bool) (ok bool, why string) {
	if seen[st.Name] {
		return true, ""
	}
	seen[st.Name] = true
	for _, f := range st.Fields {
		if ok, why := cookWritableField(u, f, seen); !ok {
			return false, st.Name + "." + f.Name + ": " + why
		}
	}
	return true, ""
}

func cookWritableField(u *ir.Unit, f *ir.Field, seen map[string]bool) (bool, string) {
	t := f.Type
	switch {
	case t.Pointer:
		return false, "a pointer (a region root's numbering)"
	case f.IsMap() || f.Array == ir.ArrayList:
		return false, "a container (a region root's numbering)"
	case t.TextUnbounded():
		return false, "unbounded text (a blob node)"
	}
	if (t.Kind == ir.TString || t.Kind == ir.TBytes) && (f.Array != ir.ArrayNone || f.KeyEnum != "") {
		return false, "an array of text"
	}
	switch t.Kind {
	case ir.TBool, ir.TFloat32, ir.TFloat64, ir.TBits, ir.TString, ir.TBytes, ir.TInt, ir.TFixed:
		return true, ""
	case ir.TNamed:
		switch r := t.Ref.(type) {
		case *ir.Enum, *ir.Flags:
			return true, ""
		case *ir.Struct:
			return cookWritable(u, r, seen)
		case *ir.Union:
			for _, v := range r.Variants {
				if v.Void() {
					continue
				}
				if ok, why := cookWritableField(u, v.F, seen); !ok {
					return false, "arm " + v.Name + ": " + why
				}
			}
			return true, ""
		}
	}
	return false, "a kind the fixed writer does not carry"
}

// cookWriter emits one record's put and its typed copy.
type cookWriter struct {
	g   *goose.Gen
	u   *ir.Unit
	put strings.Builder
	cp  strings.Builder
	tmp int
}

func (w *cookWriter) pl(ind int, format string, args ...any) {
	w.put.WriteString(strings.Repeat("    ", ind))
	fmt.Fprintf(&w.put, format, args...)
	w.put.WriteByte('\n')
}

func (w *cookWriter) cl(ind int, format string, args ...any) {
	w.cp.WriteString(strings.Repeat("    ", ind))
	fmt.Fprintf(&w.cp, format, args...)
	w.cp.WriteByte('\n')
}

func (w *cookWriter) fresh(prefix string) string {
	w.tmp++
	return fmt.Sprintf("%s%d", prefix, w.tmp)
}

// elemTypeOf is the Goose type of one storage element of a field: its slot
// type with any trailing array bound removed.
func elemTypeOf(slot string) string {
	if before, _, ok := strings.Cut(slot, "["); ok {
		return before
	}
	return slot
}

// emitCookWrite writes <Row>CookPut and <Row>CookFromLoaded for every record
// of the closure the fixed writer carries, into the cook's package module.
func emitCookWrite(g *goose.Gen, u *ir.Unit, records []*ir.Struct) {
	rt := goose.RuntimeNamespace
	g.Pf("// ---- the typed write side (docs/SPEC-TABLES.md §7.6) ----\n")
	g.Pf("// <Row>CookPut writes one TYPED record at `at`, piece by piece, in the\n")
	g.Pf("// order `big` names; an empty `b` is the dry run that only validates.\n")
	g.Pf("// <Row>CookFromLoaded copies a record of the table wire's flat value\n")
	g.Pf("// table into the typed record: a field that did not ride keeps the\n")
	g.Pf("// default its constructor gave it.\n")
	for _, st := range records {
		if ok, why := cookWritable(u, st, map[string]bool{}); !ok {
			g.Pf("// %s: no typed writer — %s\n", st.Name, why)
			continue
		}
		typ := g.TypeRef(st.Name, st.IsTable)
		ml := ir.RecordLayout(u, st)
		w := &cookWriter{g: g, u: u}
		for i, fl := range ml.Fields {
			w.field(fl.Field, "v."+ir.GoExportName(fl.Field.Name), "v."+ir.GoExportName(fl.Field.Name), fl.Offset, i, "rec")
		}
		g.Declare(st.Name + "CookPut")
		g.Pf("fn %sCookPut(v: const %s&, b: u8[:], at: i64, big: bool) -> bool {\n%s    return true;\n}\n", st.Name, typ, w.put.String())
		g.Declare(st.Name + "CookFromLoaded")
		g.Pf("fn %sCookFromLoaded(v: %s&, data: const u8[:], vals: const %s::LoadVal[:], rec: i64) {\n", st.Name, typ, rt)
		g.Pf("    v = %s { .. };\n%s}\n", typ, w.cp.String())
	}
	g.Pf("\n")
	emitCookRegionLayout(g, u)
}

// field writes one field of a record whose cook storage starts at `at +
// offset`: src is the field's value slot in the typed record (companions are
// src + Length / Count / Present), dst the same slot as an assignable path
// for the copy, ordinal the field's position in the loaded record.
func (w *cookWriter) field(f *ir.Field, src, dst string, offset int64, ordinal int, rec string) {
	rt := goose.RuntimeNamespace
	pieces := ir.FieldPieces(w.u, f, offset)
	t := f.Type
	slot := w.fresh("sl")
	w.cl(1, "let %s = %s::cook_loaded_field(vals, %s, %d);", slot, rt, rec, ordinal)
	ride := fmt.Sprintf("%s::cook_loaded_rode(vals, %s)", rt, slot)
	if t.Optional {
		present := pieces[len(pieces)-1].Offset
		w.pl(1, "%s::cook_put(b, at + %d, if %sPresent { 1 } else { 0 }, 1, big);", rt, present, src)
		w.cl(1, "%sPresent = %s;", dst, ride)
	}
	switch {
	case t.Kind == ir.TString || t.Kind == ir.TBytes:
		bound := t.Size
		w.pl(1, "if %sLength < 0 || %sLength > %d { return false; }", src, src, bound)
		w.pl(1, "%s::cook_put(b, at + %d, %sLength as! u64, 4, big);", rt, pieces[1].Offset, src)
		w.pl(1, "%s::cook_put_bytes(b, at + %d, %s[0..%sLength as! i64]);", rt, pieces[0].Offset, src, src)
		w.cl(1, "if %s { %sLength = %s::cook_loaded_text(data, vals, %s, %s); }", ride, dst, rt, slot, dst)
	case f.Array != ir.ArrayNone || f.KeyEnum != "":
		facts := ir.BlockFieldOf(w.u, f, offset, false)
		if f.Array == ir.ArrayCounted {
			w.pl(1, "if %sCount < 0 || %sCount > %d { return false; }", src, src, f.ArrayBound)
			w.pl(1, "%s::cook_put(b, at + %d, %sCount as! u64, 4, big);", rt, facts.CountOffset, src)
			w.cl(1, "if %s { %sCount = %s::cook_loaded_count(vals, %s, %d); }", ride, dst, rt, slot, f.ArrayBound)
		}
		i := w.fresh("idx")
		w.pl(1, "for %s in %d {", i, f.ArrayBound)
		w.cl(1, "for %s in %d {", i, f.ArrayBound)
		es := w.fresh("el")
		w.cl(2, "let %s = %s::cook_loaded_elem(vals, %s, %s);", es, rt, slot, i)
		w.value(f, src+"["+i+"]", dst+"["+i+"]", fmt.Sprintf("%d + %s * %d", pieces[0].Offset, i, facts.ElemSize), es, 2)
		w.pl(1, "}")
		w.cl(1, "}")
	default:
		w.value(f, src, dst, fmt.Sprint(pieces[0].Offset), slot, 1)
	}
}

// value writes ONE element of a field (the field itself when it holds one):
// src/dst its storage, at its cook offset relative to `at`, s its loaded slot.
func (w *cookWriter) value(f *ir.Field, src, dst, at string, s string, ind int) {
	rt := goose.RuntimeNamespace
	t := f.Type
	ride := fmt.Sprintf("%s::cook_loaded_rode(vals, %s)", rt, s)
	bits := fmt.Sprintf("%s::cook_loaded_bits(vals, %s)", rt, s)
	// scalar writes an integer slot: a signed one moves to u64 by `as!`, an
	// unsigned one widens implicitly, and the copy narrows only below 64 bits
	// (Goose warns on a redundant cast, and the port is held to zero warnings)
	scalar := func(width int64, signed bool, typ string) {
		expr := src
		if signed {
			expr += " as! u64"
		}
		w.pl(ind, "%s::cook_put(b, at + %s, %s, %d, big);", rt, at, expr, width)
		if typ == "u64" || width == 8 && !signed {
			w.cl(ind, "if %s { %s = %s; }", ride, dst, bits)
		} else {
			w.cl(ind, "if %s { %s = %s as! %s; }", ride, dst, bits, typ)
		}
	}
	switch t.Kind {
	case ir.TBool:
		w.pl(ind, "%s::cook_put(b, at + %s, if %s { 1 } else { 0 }, 1, big);", rt, at, src)
		w.cl(ind, "if %s { %s = %s != 0; }", ride, dst, bits)
	case ir.TFloat32:
		x := w.fresh("xb")
		w.pl(ind, "let %s = %s::accel_f32_bits(%s);", x, rt, src)
		w.pl(ind, "if (%s & 0x7F800000) == 0x7F800000 { return false; }", x)
		w.pl(ind, "%s::cook_put(b, at + %s, %s, 4, big);", rt, at, x)
		w.cl(ind, "if %s { %s = %s::accel_bits_f32(%s); }", ride, dst, rt, bits)
	case ir.TFloat64:
		x := w.fresh("xb")
		w.pl(ind, "let %s = %s::accel_f64_bits(%s);", x, rt, src)
		w.pl(ind, "if (%s & 0x7FF0000000000000) == 0x7FF0000000000000 { return false; }", x)
		w.pl(ind, "%s::cook_put(b, at + %s, %s, 8, big);", rt, at, x)
		w.cl(ind, "if %s { %s = %s::accel_bits_f64(%s); }", ride, dst, rt, bits)
	case ir.TInt, ir.TFixed, ir.TBits:
		typ := elemTypeOf(w.g.Slots(f)[0].Type)
		width := int64(t.Width) / 8
		if width == 16 {
			w.pl(ind, "%s::cook_put_wide(b, at + %s, %s, big);", rt, at, src)
			w.cl(ind, "if %s { %s = %s::cook_loaded_wide(vals, %s); }", ride, dst, rt, s)
			break
		}
		if t.Kind == ir.TBits {
			width = 4
			if t.Width > 32 {
				width = 8
			}
		}
		scalar(width, strings.HasPrefix(typ, "i"), typ)
	case ir.TNamed:
		switch r := t.Ref.(type) {
		case *ir.Enum:
			typ := w.g.TypeRef(r.Name, false)
			width := int64(ir.StorageBitsFor(r.Max)) / 8
			for i := range r.Variants {
				if i < len(r.VariantTags) && hasTag(r.VariantTags[i], "deprecated") {
					w.pl(ind, "if %s == %d { return false; } // %s is deprecated (§2.15)", src, i+1, r.Variants[i])
				}
			}
			scalar(width, false, typ)
		case *ir.Flags:
			scalar(8, false, w.g.TypeRef(r.Name, false))
		case *ir.Struct:
			rTyp := w.g.TypeRef(r.Name, r.IsTable)
			switch {
			case f.Type.Optional && f.Array == ir.ArrayNone && f.KeyEnum == "":
				d, ed, ev := w.fresh("df"), w.fresh("ed"), w.fresh("ev")
				w.pl(ind, "if %sPresent { if !%sCookPut(%s, b, at + %s, big) { return false; } } else { var %s = %s { .. }; let %s: u8[1] = [0]; let %s: %s::LoadVal[1] = [%s::LoadVal { .. }]; %sCookFromLoaded(%s, %s[0..0], %s[0..0], -1); if !%sCookPut(%s, b, at + %s, big) { return false; } }", src, r.Name, src, at, d, rTyp, ed, ev, rt, rt, r.Name, d, ed, ev, r.Name, d, at)
			case f.Array == ir.ArrayCounted:
				lb := strings.LastIndexByte(src, '[')
				arr, idx := src[:lb], strings.TrimSuffix(src[lb+1:], "]")
				d, ed, ev := w.fresh("df"), w.fresh("ed"), w.fresh("ev")
				w.pl(ind, "if %s < %sCount as! i64 { if !%sCookPut(%s, b, at + %s, big) { return false; } } else { var %s = %s { .. }; let %s: u8[1] = [0]; let %s: %s::LoadVal[1] = [%s::LoadVal { .. }]; %sCookFromLoaded(%s, %s[0..0], %s[0..0], -1); if !%sCookPut(%s, b, at + %s, big) { return false; } }", idx, arr, r.Name, src, at, d, rTyp, ed, ev, rt, rt, r.Name, d, ed, ev, r.Name, d, at)
			default:
				w.pl(ind, "if !%sCookPut(%s, b, at + %s, big) { return false; }", r.Name, src, at)
			}
			w.cl(ind, "%sCookFromLoaded(%s, data, vals, %s);", r.Name, dst, s)
		case *ir.Union:
			w.union(r, src, dst, at, s, ind)
		}
	}
}

// union writes the tag, then the SET arm at the arm offset (§7.2): an
// unselected arm's bytes stay the zero the extent was cleared to.
func (w *cookWriter) union(un *ir.Union, src, dst, at, s string, ind int) {
	rt := goose.RuntimeNamespace
	_, _, tagSize, armOffset := ir.UnionLayout(w.u, un)
	tag := w.g.TypeRef(un.Name, w.u.TableUnions[un.Name] != nil) + "Type"
	w.pl(ind, "if %s.Type > %d { return false; }", src, len(un.Variants))
	w.pl(ind, "%s::cook_put(b, at + %s, %s.Type, %d, big);", rt, at, src, tagSize)
	w.cl(ind, "if %s::cook_loaded_rode(vals, %s) {", rt, s)
	if un.StorageBits == 64 {
		w.cl(ind+1, "%s.Type = %s::cook_loaded_bits(vals, %s);", dst, rt, s)
	} else {
		w.cl(ind+1, "%s.Type = %s::cook_loaded_bits(vals, %s) as! %s;", dst, rt, s, tag)
	}
	w.cl(ind, "}")
	payload := w.fresh("pl")
	w.cl(ind, "let %s = %s::cook_loaded_payload(vals, %s);", payload, rt, s)
	for i, v := range un.Variants {
		if v.Void() {
			continue
		}
		arm := ir.GoExportName(v.Name)
		w.pl(ind, "if %s.Type == %d {", src, i+1)
		w.cl(ind, "if %s.Type == %d {", dst, i+1)
		af := *v.F
		af.Name = v.Name
		pieces := ir.FieldPieces(w.u, &af, 0)
		off := fmt.Sprintf("%s + %d", at, armOffset+pieces[0].Offset)
		switch {
		case af.Type.Kind == ir.TString || af.Type.Kind == ir.TBytes:
			a := src + "." + arm
			w.pl(ind+1, "if %sLength < 0 || %sLength > %d { return false; }", a, a, af.Type.Size)
			w.pl(ind+1, "%s::cook_put(b, at + %s + %d, %sLength as! u64, 4, big);", rt, at, armOffset+pieces[1].Offset, a)
			w.pl(ind+1, "%s::cook_put_bytes(b, at + %s, %s[0..%sLength as! i64]);", rt, off, a, a)
			w.cl(ind+1, "if %s::cook_loaded_rode(vals, %s) { %s.%sLength = %s::cook_loaded_text(data, vals, %s, %s.%s); }", rt, payload, dst, arm, rt, payload, dst, arm)
		case af.Array != ir.ArrayNone || af.KeyEnum != "":
			a, d := src+"."+arm, dst+"."+arm
			facts := ir.BlockFieldOf(w.u, &af, 0, false)
			if af.Type.Optional {
				present := pieces[len(pieces)-1].Offset
				w.pl(ind+1, "%s::cook_put(b, at + %s + %d, if %sPresent { 1 } else { 0 }, 1, big);", rt, at, armOffset+present, a)
				w.cl(ind+1, "%sPresent = %s::cook_loaded_rode(vals, %s);", d, rt, payload)
			}
			if af.Array == ir.ArrayCounted {
				w.pl(ind+1, "if %sCount < 0 || %sCount > %d { return false; }", a, a, af.ArrayBound)
				w.pl(ind+1, "%s::cook_put(b, at + %s + %d, %sCount as! u64, 4, big);", rt, at, armOffset+facts.CountOffset, a)
				w.cl(ind+1, "if %s::cook_loaded_rode(vals, %s) { %sCount = %s::cook_loaded_count(vals, %s, %d); }", rt, payload, d, rt, payload, af.ArrayBound)
			}
			i := w.fresh("idx")
			w.pl(ind+1, "for %s in %d {", i, af.ArrayBound)
			w.cl(ind+1, "for %s in %d {", i, af.ArrayBound)
			es := w.fresh("el")
			w.cl(ind+2, "let %s = %s::cook_loaded_elem(vals, %s, %s);", es, rt, payload, i)
			w.value(&af, a+"["+i+"]", d+"["+i+"]", fmt.Sprintf("%s + %d + %s * %d", at, armOffset+pieces[0].Offset, i, facts.ElemSize), es, ind+2)
			w.pl(ind+1, "}")
			w.cl(ind+1, "}")
		case af.Type.Optional:
			a, d := src+"."+arm, dst+"."+arm
			present := pieces[len(pieces)-1].Offset
			w.pl(ind+1, "%s::cook_put(b, at + %s + %d, if %sPresent { 1 } else { 0 }, 1, big);", rt, at, armOffset+present, a)
			w.cl(ind+1, "%sPresent = %s::cook_loaded_rode(vals, %s);", d, rt, payload)
			w.value(&af, a, d, fmt.Sprintf("%s + %d", at, armOffset+pieces[0].Offset), payload, ind+1)
		default:
			w.value(&af, src+"."+arm, dst+"."+arm, fmt.Sprintf("%s + %d", at, armOffset+pieces[0].Offset), payload, ind+1)
		}
		w.pl(ind, "}")
		w.cl(ind, "}")
	}
}

// emitCookRoot is one table's write surface (§7.6). A fixed root the typed
// writer carries gets the measure, a layout constant, and the write — the
// header, the record at the data part's base, and the one-entry attribution
// part; every other root gets the region writer's (emitCookRegionRoot).
func emitCookRoot(g *goose.Gen, u *ir.Unit, st *ir.Struct) {
	if !st.IsTable || st.IsMapEntry() || !st.CarriesForm("cook") {
		return
	}
	if cookRegionRoot(u, st) {
		emitCookRegionRoot(g, u, st)
		return
	}
	rt := goose.RuntimeNamespace
	n := st.Name
	ml := ir.RecordLayout(u, st)
	align := max(int64(8), ml.Align)
	data := (ml.Size + align - 1) / align * align
	total := 64 + data + 16
	typ := g.TypeRef(n, st.IsTable)
	g.Declare(n + "CookMeasure")
	g.Declare(n + "Cook")
	g.Pf("// %sCookMeasure is the whole cooked file's length, a layout constant: the\n", n)
	g.Pf("// header, the record rounded to the region's alignment, one attribution\n")
	g.Pf("// entry (docs/SPEC-TABLES.md §7.6).\n")
	g.Pf("fn %sCookMeasure() -> i64 { return %d; }\n", n, total)
	g.Pf("// %sCook writes the cook of one typed value into out[0..%sCookMeasure()]\n", n, n)
	g.Pf("// in the order `big` names, and nothing at all on a short capacity or a\n")
	g.Pf("// value no cook carries. The bytes are `schema cook`'s, byte for byte.\n")
	g.Pf("fn %sCook(v: const %s&, out: u8[:], big: bool) -> bool {\n", n, typ)
	g.Pf("    if out.len < %d { return false; }\n", total)
	g.Pf("    if !%sCookPut(v, out[0..0], 64, big) { return false; }\n", n)
	g.Pf("    %s::cook_begin(out[0..%d], BuildVersion, %d, 1, %d, big);\n", rt, total, data, align)
	g.Pf("    if !%sCookPut(v, out, 64, big) { return false; }\n", n)
	g.Pf("    %s::cook_put(out, %d, 0, 8, big);\n", rt, 64+data)
	g.Pf("    %s::cook_put(out, %d, 0x%016x, 8, big);\n", rt, 64+data+8, ir.TableWireId(st.WireName()))
	g.Pf("    return true;\n")
	g.Pf("}\n\n")
}

// cookRegionRoot reports a table whose write side is the region writer: one
// that declares the cook among its forms (§2.12) and is a pointered root, or
// a fixed one the typed writer does not carry.
func cookRegionRoot(u *ir.Unit, st *ir.Struct) bool {
	if !st.IsTable || st.IsMapEntry() || !st.CarriesForm("cook") {
		return false
	}
	if ir.VariableTables(u)[st.Name] {
		return true
	}
	ok, _ := cookWritable(u, st, map[string]bool{})
	return !ok
}

// emitCookRegionRoot is one region root's write surface (§7.6) over a LOADED
// value — the flat value table, the node table and the input the views read,
// as schema_rt::load_table builds them: the measure, and the write.
func emitCookRegionRoot(g *goose.Gen, u *ir.Unit, st *ir.Struct) {
	rt := goose.RuntimeNamespace
	n := st.Name
	g.Import(cookWriteModule)
	g.Import(UnitModule(u))
	g.Declare(n + "CookMeasure")
	g.Declare(n + "Cook")
	g.Pf("// %sCookMeasure is the whole cooked file's length for one loaded value —\n", n)
	g.Pf("// `vals`, `nodes` and the `data` their views read, root record `root`, as\n")
	g.Pf("// schema_rt::load_table builds them — or -1 for a value no cook carries. It\n")
	g.Pf("// depends on the value: the numbering of §3.1 and every extent (§7.6).\n")
	g.Pf("fn %sCookMeasure(data: const u8[:], vals: const %s::LoadVal[:], nodes: const %s::LoadNode[:], root: i64) -> i64 {\n", n, rt, rt)
	g.Pf("    return %s::cook_region_measure(%s, CookRegionLayout, BuildVersion, %sCookRegionTable, data, vals, nodes, root);\n", rt, UnitName, n)
	g.Pf("}\n")
	g.Pf("// %sCook writes the cook of one loaded value into out[0..%sCookMeasure(..)]\n", n, n)
	g.Pf("// in the order `big` names, and nothing at all on a short capacity or a\n")
	g.Pf("// value no cook carries. The bytes are `schema cook`'s, byte for byte.\n")
	g.Pf("fn %sCook(data: const u8[:], vals: const %s::LoadVal[:], nodes: const %s::LoadNode[:], root: i64, out: u8[:], big: bool) -> bool {\n", n, rt, rt)
	g.Pf("    return %s::cook_region_write(%s, CookRegionLayout, BuildVersion, %sCookRegionTable, data, vals, nodes, root, out, big, false) >= 0;\n", rt, UnitName, n)
	g.Pf("}\n\n")
}

// cookWriteModule is the region writer's runtime module.
const cookWriteModule = "cook_write"

// emitCookRegionLayout writes the unit's cook layout as data into the cook's
// package module, for a unit with a region root: one schema_rt::CookTab per
// TableUnit table and one schema_rt::CookUni per TableUnit union, in the
// registry's own order (unitDecls), and each region root's place in them.
func emitCookRegionLayout(g *goose.Gen, u *ir.Unit) {
	var roots []*ir.Struct
	for _, f := range u.Files {
		for _, st := range f.Tables {
			if cookRegionRoot(u, st) {
				roots = append(roots, st)
			}
		}
	}
	if len(roots) == 0 {
		return
	}
	rt := goose.RuntimeNamespace
	g.Import(cookWriteModule)
	structs, unions := unitDecls(u)
	tabIndex := map[string]int{}
	for i, st := range structs {
		tabIndex[st.Name] = i
	}
	uniIndex := map[string]int{}
	for i, un := range unions {
		uniIndex[un.Name] = i
	}
	cl := &cookLayout{u: u, tab: tabIndex, uni: uniIndex}
	g.Pf("// ---- the region writer's layout (docs/SPEC-TABLES.md §7.2, §7.6) ----\n")
	g.Pf("// Every record and union of TableUnit, in its order, at the layout\n")
	g.Pf("// ir.RecordLayout computes: schema_rt::cook_region_write reads it as data.\n")
	var tabs, unis []string
	for i, st := range structs {
		ml := ir.RecordLayout(u, st)
		var cols []string
		for _, f := range st.Fields {
			fl := ml.FieldByName(f.Name)
			cols = append(cols, cl.col(f, fl.Offset))
		}
		sym := fmt.Sprintf("_CookRegionCols%d", i)
		g.Declare(sym)
		g.Pf("let %s: %s::CookCol[%d] = [\n", sym, rt, max(len(cols), 1))
		if len(cols) == 0 {
			cols = append(cols, rt+"::CookCol { .. }")
		}
		g.Pf("    %s,\n];\n", strings.Join(cols, ",\n    "))
		tabs = append(tabs, fmt.Sprintf("%s::CookTab { record: %d, align: %d, ext: %t, cols: %s[0..%d] }",
			rt, cookRecordBytes(u, st), cookNodeAlign(u, st), cookRecordHasExtent(u, st), sym, len(st.Fields)))
	}
	for i, un := range unions {
		_, _, tag, arm := ir.UnionLayout(u, un)
		var cols []string
		for _, v := range un.Variants {
			if v.Void() {
				cols = append(cols, rt+"::CookCol { .. }")
				continue
			}
			af := *v.F
			af.Name = v.Name
			cols = append(cols, cl.col(&af, 0))
		}
		sym := fmt.Sprintf("_CookRegionArms%d", i)
		g.Declare(sym)
		g.Pf("let %s: %s::CookCol[%d] = [\n", sym, rt, max(len(cols), 1))
		if len(cols) == 0 {
			cols = append(cols, rt+"::CookCol { .. }")
		}
		g.Pf("    %s,\n];\n", strings.Join(cols, ",\n    "))
		unis = append(unis, fmt.Sprintf("%s::CookUni { tag: %d, arm: %d, cols: %s[0..%d] }", rt, tag, arm, sym, len(un.Variants)))
	}
	var l lit
	if len(tabs) > 0 {
		g.Declare("_CookRegionTabs")
		g.Pf("let _CookRegionTabs: %s::CookTab[%d] = [\n    %s,\n];\n", rt, len(tabs), strings.Join(tabs, ",\n    "))
		l.add("tabs", "_CookRegionTabs")
	}
	if len(unis) > 0 {
		g.Declare("_CookRegionUnis")
		g.Pf("let _CookRegionUnis: %s::CookUni[%d] = [\n    %s,\n];\n", rt, len(unis), strings.Join(unis, ",\n    "))
		l.add("unis", "_CookRegionUnis")
	}
	l.add("text_align", fmt.Sprint(ir.TableRegionAlign(u)))
	g.Declare("CookRegionLayout")
	g.Pf("let CookRegionLayout: %s::CookUnitLayout = %s;\n", rt, l.render("CookUnitLayout"))
	g.Pf("// each region root's table in TableUnit\n")
	for _, st := range roots {
		g.Declare(st.Name + "CookRegionTable")
		g.Pf("let %sCookRegionTable: i64 = %d;\n", st.Name, tabIndex[st.Name])
	}
	g.Pf("\n")
}

// cookLayout renders the columns of one unit's region layout.
type cookLayout struct {
	u   *ir.Unit
	tab map[string]int
	uni map[string]int
}

// col is one field's schema_rt::CookCol, its offsets relative to the record
// whose storage puts the field at `offset` (an arm's: zero).
func (cl *cookLayout) col(f *ir.Field, offset int64) string {
	u := cl.u
	pieces := ir.FieldPieces(u, f, offset)
	if len(pieces) == 0 {
		return goose.RuntimeNamespace + "::CookCol { .. }"
	}
	c := cookCol{lenOff: -1, presentOff: -1, tab: -1, uni: -1}
	c.off, c.size = pieces[0].Offset, pieces[0].Size
	if f.Type.Optional {
		c.presentOff = pieces[len(pieces)-1].Offset
	}
	elem := *f
	elem.Array = ir.ArrayNone
	elem.ArrayBound = 0
	elem.KeyEnum = ""
	elem.KeyEnumRef = nil
	elem.Type.Optional = false
	switch {
	case f.IsMap():
		ml := ir.RecordLayout(u, f.MapEntry)
		c.stride, c.ealign = ml.Size, max(ml.Align, 1)
		c.tab = int64(cl.tab[f.MapEntry.Name])
		c.eext = cookRecordHasExtent(u, f.MapEntry)
	case f.IsList():
		c.stride, c.ealign = ir.ListElementLayout(u, f)
		c.eext = cookFieldHasExtent(u, &elem)
		cl.elem(&c, &elem)
	case f.Type.TextUnbounded():
	case !f.Type.Pointer && f.Array == ir.ArrayNone && (f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes):
		c.lenOff = pieces[1].Offset
	case f.Array != ir.ArrayNone || f.KeyEnum != "":
		c.stride = cookElementBytes(u, f)
		if f.Array == ir.ArrayCounted {
			c.lenOff = pieces[1].Offset
		}
		cl.elem(&c, &elem)
	default:
		cl.elem(&c, &elem)
	}
	c.ext = cookFieldHasExtent(u, f)
	return c.render()
}

// cookCol is a schema_rt::CookCol under construction.
type cookCol struct {
	off, size, lenOff, presentOff, stride, ealign, w, tab, uni int64
	ext, eext                                                  bool
}

// render writes the literal in schema_rt::CookCol's declaration order.
func (c cookCol) render() string {
	var l lit
	l.add("off", fmt.Sprint(c.off))
	l.add("size", fmt.Sprint(c.size))
	l.add("len_off", fmt.Sprint(c.lenOff))
	l.add("present_off", fmt.Sprint(c.presentOff))
	if c.stride != 0 {
		l.add("stride", fmt.Sprint(c.stride))
	}
	if c.ealign != 0 {
		l.add("ealign", fmt.Sprint(c.ealign))
	}
	if c.w != 0 {
		l.add("w", fmt.Sprint(c.w))
	}
	l.add("tab", fmt.Sprint(c.tab))
	l.add("uni", fmt.Sprint(c.uni))
	l.flag("ext", c.ext)
	l.flag("eext", c.eext)
	return l.render("CookCol")
}

// elem sets the facts of ONE value of a field's type: its scalar storage
// width, or the record or union it is.
func (cl *cookLayout) elem(c *cookCol, f *ir.Field) {
	t := f.Type
	switch {
	case t.Pointer || t.Blob():
		c.w = 8
	case t.Kind == ir.TBool:
		c.w = 1
	case t.Kind == ir.TFloat32:
		c.w = 4
	case t.Kind == ir.TFloat64:
		c.w = 8
	case t.Kind == ir.TInt || t.Kind == ir.TFixed:
		c.w = int64(t.Width) / 8
	case t.Kind == ir.TBits:
		c.w = 4
		if t.Width > 32 {
			c.w = 8
		}
	case t.Kind == ir.TNamed:
		switch r := t.Ref.(type) {
		case *ir.Enum:
			c.w = int64(ir.StorageBitsFor(r.Max)) / 8
		case *ir.Flags:
			c.w = 8
		case *ir.Struct:
			c.tab = int64(cl.tab[r.Name])
		case *ir.Union:
			c.uni = int64(cl.uni[r.Name])
		}
	}
}

// The extent facts are internal/tablecook's (extent.go), the tool's cooker
// this writer must agree with byte for byte.

// cookRecordHasExtent reports a record with a list, a map or an unbounded
// text reachable BY VALUE: its node is its storage plus an extent.
func cookRecordHasExtent(u *ir.Unit, st *ir.Struct) bool {
	if st == nil {
		return false
	}
	for _, f := range st.Fields {
		if f.IsMap() || f.IsList() || f.Type.TextUnbounded() {
			return true
		}
		if f.Type.Pointer || f.Type.Blob() || f.Type.Kind != ir.TNamed {
			continue
		}
		switch ref := f.Type.Ref.(type) {
		case *ir.Struct:
			if cookRecordHasExtent(u, ref) {
				return true
			}
		case *ir.Union:
			if cookUnionHasExtent(u, ref, map[*ir.Union]bool{}) {
				return true
			}
		}
	}
	return false
}

// cookUnionHasExtent reports a union with a container below an arm held by
// value; a pointer arm and a byte buffer arm reach nodes, not the extent.
func cookUnionHasExtent(u *ir.Unit, un *ir.Union, seen map[*ir.Union]bool) bool {
	if un == nil || seen[un] {
		return false
	}
	seen[un] = true
	for _, v := range un.Variants {
		if v.F == nil {
			continue
		}
		if v.F.IsMap() || v.F.IsList() || v.F.Type.TextUnbounded() {
			return true
		}
		if v.F.Type.Pointer || v.F.Type.Blob() || v.F.Type.Kind != ir.TNamed {
			continue
		}
		switch ref := v.F.Type.Ref.(type) {
		case *ir.Struct:
			if cookRecordHasExtent(u, ref) {
				return true
			}
		case *ir.Union:
			if cookUnionHasExtent(u, ref, seen) {
				return true
			}
		}
	}
	return false
}

// cookFieldHasExtent reports a field whose by-value storage can hold a
// container (the array spelling aside).
func cookFieldHasExtent(u *ir.Unit, f *ir.Field) bool {
	if f.Type.TextUnbounded() {
		return true
	}
	if f.Type.Pointer || f.Type.Blob() || f.Type.Kind != ir.TNamed {
		return false
	}
	switch ref := f.Type.Ref.(type) {
	case *ir.Struct:
		return cookRecordHasExtent(u, ref)
	case *ir.Union:
		return cookUnionHasExtent(u, ref, map[*ir.Union]bool{})
	}
	return false
}

// cookRecordBytes is a node's record half: its sizeof, rounded to where its
// extent begins when it can hold one (ir.CookExtentStart).
func cookRecordBytes(u *ir.Unit, st *ir.Struct) int64 {
	if !cookRecordHasExtent(u, st) {
		return ir.RecordLayout(u, st).Size
	}
	return ir.CookExtentStart(u, st)
}

// cookNodeAlign is a node's alignment in the region (ir.CookNodeAlign where
// it can hold an extent).
func cookNodeAlign(u *ir.Unit, st *ir.Struct) int64 {
	if !cookRecordHasExtent(u, st) {
		return ir.RecordLayout(u, st).Align
	}
	return ir.CookNodeAlign(u, st)
}

// cookElementBytes is the stride of one array slot: the element's own
// storage, as ir.FieldPieces sizes it.
func cookElementBytes(u *ir.Unit, f *ir.Field) int64 {
	single := *f
	single.Array = ir.ArrayNone
	single.ArrayBound = 0
	single.KeyEnum = ""
	single.KeyEnumRef = nil
	single.Type.Optional = false
	var end int64
	for _, p := range ir.FieldPieces(u, &single, 0) {
		end = max(end, p.Offset+p.Size)
	}
	return end
}
