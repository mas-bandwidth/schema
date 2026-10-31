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
// A POINTERED ROOT (a variable closure: pointers, lists, maps, blobs) cooks
// a REGION, numbered by the depth-first first-visit walk of §3.1 (§7.6). The
// Goose typed record holds its pointers as references into storage the load
// or the builder owns, and that numbering is not carried here: a pointered
// root gets no writer, which the cook-write surface answers as a named absence
// (test/conformance/goose/accel.goose).
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
	if (t.Kind == ir.TString || t.Kind == ir.TBytes) && f.Array != ir.ArrayNone {
		return false, "an array of text"
	}
	switch t.Kind {
	case ir.TBool, ir.TFloat32, ir.TFloat64, ir.TBits, ir.TString, ir.TBytes:
		return true, ""
	case ir.TInt:
		if t.Width > 64 {
			return false, "a 128-bit integer"
		}
		return true, ""
	case ir.TNamed:
		switch r := t.Ref.(type) {
		case *ir.Enum, *ir.Flags:
			return true, ""
		case *ir.Struct:
			return cookWritable(u, r, seen)
		case *ir.Union:
			if f.Array != ir.ArrayNone {
				return false, "an array of unions"
			}
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
	if i := strings.Index(slot, "["); i >= 0 {
		return slot[:i]
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
		g.Pf("    if rec < 0 || !vals[rec].made { return; }\n%s}\n", w.cp.String())
	}
	g.Pf("\n")
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
	case ir.TInt, ir.TBits:
		typ := elemTypeOf(w.g.Slots(f)[0].Type)
		width := int64(t.Width) / 8
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
			w.g.TypeRef(r.Name, r.IsTable)
			w.pl(ind, "if !%sCookPut(%s, b, at + %s, big) { return false; }", r.Name, src, at)
			w.cl(ind, "if %s { %sCookFromLoaded(%s, data, vals, %s); }", ride, r.Name, dst, s)
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
		case af.Array != ir.ArrayNone || af.Type.Optional:
			// an arm of an array or an optional: the fixed writer's arm
			// spelling of a field line, which this corpus does not carry
			w.pl(ind+1, "return false; // arm %s: an array or optional arm has no typed writer yet", v.Name)
		default:
			w.value(&af, src+"."+arm, dst+"."+arm, fmt.Sprintf("%s + %d", at, armOffset+pieces[0].Offset), payload, ind+1)
		}
		w.pl(ind, "}")
		w.cl(ind, "}")
	}
}

// emitCookRoot is one FIXED table's write surface (§7.6): the measure, a
// layout constant, and the write — the header, the record at the data part's
// base, and the one-entry attribution part.
func emitCookRoot(g *goose.Gen, u *ir.Unit, st *ir.Struct) {
	if !st.IsTable || st.IsMapEntry() || ir.VariableTables(u)[st.Name] {
		return
	}
	if ok, _ := cookWritable(u, st, map[string]bool{}); !ok {
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
