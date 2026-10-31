package jstable

// Cook write (docs/SPEC-TABLES.md §7.6). The header words are the block
// prologue's (§19.1): magic, build version, byte order, data length,
// attribution length, alignment, and the two reserved words left zero.
// Measure before write. A buffer shorter than the measure is not touched,
// and CookFrom answers false. CookMeasureReason's refusal is the reference
// spelling invalid_value, a variable cycle is data_cycle, and a NaN or an
// infinity is non_finite_float (docs/SPEC-TABLES.md §3.3).

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/js"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func (g *fixedModule) emitFixedCookWrite() {
	if !g.home {
		return
	}
	layouts := cookLayouts(g.unit, false)
	if len(layouts) == 0 {
		return
	}
	g.pf("%s", cookWriteOpen(g.unit, layouts, false))
}

func (g *fixedModule) emitFixedCookExports(st *ir.Struct) {
	if cookFixedBlocked(g.unit, st) {
		return
	}
	if !g.home {
		g.needHome("TableCookWrite")
	}
	// A fixed record's cooked size is the same for every value, so the two
	// measures read none and take none: a caller's value argument is ignored.
	g.pf("export function %sCookMeasure() {\n", st.Name)
	g.pf("  // Measure before write (docs/SPEC-TABLES.md §7.6). The header words are §19.1's.\n")
	g.pf("  return TableCookWrite.fixedMeasure(%q);\n}\n", st.Name)
	g.pf("export function %sCookMeasureReason() {\n", st.Name)
	g.pf("  // Measure before write (docs/SPEC-TABLES.md §7.6). The header words are §19.1's.\n")
	g.pf("  return [TableCookWrite.fixedMeasure(%q), null];\n}\n", st.Name)
	g.pf("export function %sCookFrom(value, buffer, order) {\n", st.Name)
	g.pf("  // Measure before write (docs/SPEC-TABLES.md §7.6). The header words are §19.1's.\n")
	g.pf("  return TableCookWrite.fixedFrom(%q, value, buffer, order);\n}\n\n", st.Name)
}

func (g *variableGen) emitVariableCookWrite(roots []*ir.Struct) {
	layouts := cookLayouts(g.unit, true)
	if len(layouts) == 0 {
		return
	}
	g.pf("%s", cookWriteOpen(g.unit, layouts, true))
	for _, st := range roots {
		if cookVariableBlocked(g.unit, st) {
			continue
		}
		g.pf("export function %sCookMeasure(value) {\n", st.Name)
		g.pf("  // Measure before write (docs/SPEC-TABLES.md §7.6). The header words are §19.1's.\n")
		g.pf("  return TableCookWrite.variableMeasure(%q, value);\n}\n", st.Name)
		g.pf("export function %sCookMeasureReason(value) {\n", st.Name)
		g.pf("  // Measure before write (docs/SPEC-TABLES.md §7.6). The header words are §19.1's.\n")
		g.pf("  return TableCookWrite.variableReason(%q, value);\n}\n", st.Name)
		g.pf("export function %sCookFrom(value, buffer, order) {\n", st.Name)
		g.pf("  // Measure before write (docs/SPEC-TABLES.md §7.6). The header words are §19.1's.\n")
		g.pf("  return TableCookWrite.variableFrom(%q, value, buffer, order);\n}\n\n", st.Name)
	}
}

func cookWriteOpen(u *ir.Unit, layouts []*ir.Struct, variable bool) string {
	var b strings.Builder
	b.WriteString("// Cook write (docs/SPEC-TABLES.md §7.6). The header words are §19.1's.\n")
	b.WriteString("export const TableCookWrite = (() => {\n")
	fmt.Fprintf(&b, "  const build = %s;\n", jsId(ir.BuildVersion(u)))
	b.WriteString("  const layouts = {\n")
	for _, st := range layouts {
		b.WriteString(cookStructLiteral(u, st, variable))
		b.WriteString(",\n")
	}
	b.WriteString("  };\n")
	b.WriteString(cookEngineJS(variable))
	b.WriteString("})();\n\n")
	return b.String()
}

func cookLayouts(u *ir.Unit, variable bool) []*ir.Struct {
	seen := map[string]bool{}
	var out []*ir.Struct
	for _, f := range u.Files {
		var roots []*ir.Struct
		if variable {
			roots = variableRoots(u, f.Tables)
		} else {
			roots = fixedRoots(u, f.Tables)
		}
		for _, st := range roots {
			if variable {
				if !variableCarried(u, st.Name) || cookVariableBlocked(u, st) {
					continue
				}
			} else if cookFixedBlocked(u, st) {
				continue
			}
			cookCollect(st, seen, &out)
		}
	}
	return out
}

// variableCarried reports whether the variable wire emits a codec for the
// table. A root with no codec has nothing for CookFrom to read.
func variableCarried(u *ir.Unit, name string) bool {
	return newVariableClass(u).carried(name)
}

func cookCollect(st *ir.Struct, seen map[string]bool, out *[]*ir.Struct) {
	if st == nil || seen[st.Name] {
		return
	}
	seen[st.Name] = true
	for _, f := range st.Fields {
		cookCollectField(f, seen, out)
	}
	*out = append(*out, st)
}

func cookCollectField(f *ir.Field, seen map[string]bool, out *[]*ir.Struct) {
	if f != nil && f.IsMap() {
		// a map's generated { key, value } entry is laid like any record (§2.8)
		cookCollect(f.MapEntry, seen, out)
		return
	}
	if f == nil || f.Type.Kind != ir.TNamed {
		return
	}
	switch r := f.Type.Ref.(type) {
	case *ir.Struct:
		cookCollect(r, seen, out)
	case *ir.Union:
		ukey := "union:" + r.Name
		if seen[ukey] {
			return
		}
		seen[ukey] = true
		for _, v := range r.Variants {
			if v.Ref != nil {
				cookCollect(v.Ref, seen, out)
			}
			if v.F != nil {
				cookCollectField(v.F, seen, out)
			}
		}
	}
}

func cookFixedBlocked(u *ir.Unit, st *ir.Struct) bool {
	_ = u
	return cookWriteWalk(st, map[string]bool{}, false)
}

func cookVariableBlocked(u *ir.Unit, st *ir.Struct) bool {
	_ = u
	return cookWriteWalk(st, map[string]bool{}, true)
}

func cookWriteWalk(st *ir.Struct, seen map[string]bool, variable bool) bool {
	if st == nil || seen[st.Name] {
		return false
	}
	seen[st.Name] = true
	for _, f := range st.Fields {
		if cookFieldBlocked(f, variable) {
			return true
		}
		if variable && f.IsMap() {
			if cookWriteWalk(f.MapEntry, seen, variable) {
				return true
			}
			continue
		}
		if f.Type.Kind != ir.TNamed {
			continue
		}
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			if cookWriteWalk(r, seen, variable) {
				return true
			}
		case *ir.Union:
			ukey := "union:" + r.Name
			if seen[ukey] {
				continue
			}
			seen[ukey] = true
			for _, v := range r.Variants {
				if v.F != nil && cookFieldBlocked(v.F, variable) {
					return true
				}
				if v.Ref != nil && cookWriteWalk(v.Ref, seen, variable) {
					return true
				}
			}
		}
	}
	return false
}

// cookFieldBlocked is the construct this cook writer does not place. On the
// variable form (docs/SPEC-TABLES.md §7.6), every construct the variable wire
// carries is placed, a map's entries and a list's elements in the holder's
// node extent (docs/PORTING.md M19); on the fixed form, only the initial fixed
// subset is placed.
func cookFieldBlocked(f *ir.Field, variable bool) bool {
	if f == nil {
		return true
	}
	if variable {
		switch {
		case f.IsMap():
			// the entry is walked as a record (cookWriteWalk)
			return false
		case f.Type.Kind == ir.TMap:
			return true
		case f.Type.Kind == ir.TWString && (f.Type.Pointer || f.Type.Unbounded || f.Array != ir.ArrayNone):
			return true
		case f.IsList() && (f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TWString):
			return true
		case f.Array != ir.ArrayNone && f.Array != ir.ArrayList && (f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TWString):
			return true
		}
		if f.Type.Kind == ir.TNamed {
			switch r := f.Type.Ref.(type) {
			case *ir.Enum, *ir.Flags, *ir.Struct:
				return false
			case *ir.Union:
				for _, v := range r.Variants {
					if v.F != nil && cookFieldBlocked(v.F, true) {
						return true
					}
				}
				return false
			default:
				return true
			}
		}
		switch f.Type.Kind {
		case ir.TBool, ir.TInt, ir.TFixed, ir.TFloat32, ir.TFloat64, ir.TString, ir.TWString, ir.TBytes, ir.TBits:
			return false
		}
		return true
	}
	switch {
	case f.Type.Optional, f.IsMap(), f.IsList(), f.KeyEnum != "", f.Guard != "":
		return true
	case f.Type.Kind == ir.TFixed, f.Type.Kind == ir.TWString, f.Type.Kind == ir.TMap:
		return true
	case f.Type.Kind == ir.TInt && f.Type.Width >= 128:
		return true
	case f.Array != ir.ArrayNone && (f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TWString):
		return true
	case !variable && f.Type.Pointer:
		return true
	}
	if f.Type.Kind == ir.TNamed {
		switch f.Type.Ref.(type) {
		case *ir.Enum, *ir.Flags, *ir.Union, *ir.Struct:
			return false
		default:
			return true
		}
	}
	switch f.Type.Kind {
	case ir.TBool, ir.TInt, ir.TFloat32, ir.TFloat64, ir.TString, ir.TBytes, ir.TBits:
		return false
	}
	return true
}

func cookAlignUp(n, a int64) int64 {
	if a <= 1 {
		return n
	}
	return (n + a - 1) / a * a
}

func cookStructLiteral(u *ir.Unit, st *ir.Struct, variable bool) string {
	ml := ir.RecordLayout(u, st)
	alignCook := max(int64(8), ml.Align)
	data := cookAlignUp(ml.Size, alignCook)
	var b strings.Builder
	fmt.Fprintf(&b, "    %s: {\n", strconv.Quote(st.Name))
	fmt.Fprintf(&b, "      size: %d, align: %d, data: %d, measure: %d, alignCook: %d, typeId: %s",
		ml.Size, ml.Align, data, 64+data+16, alignCook, jsId(ir.TableTypeId(st.WireName())))
	if variable {
		fmt.Fprintf(&b, ", make: () => new %s()", st.Name)
		if cookRecordHasExtent(st) {
			// THE NODE EXTENT (docs/SPEC-TABLES.md §2.8, §6.2; docs/PORTING.md
			// M19): the node aligns to ir.CookNodeAlign and its extent begins at
			// ir.CookExtentStart, the one rule the reference and the tool share
			fmt.Fprintf(&b, ", ext: true, nodeAlign: %d, extentStart: %d",
				ir.CookNodeAlign(u, st), ir.CookExtentStart(u, st))
		}
	}
	b.WriteString(",\n      fields: [\n")
	for i := range ml.Fields {
		fl := &ml.Fields[i]
		fmt.Fprintf(&b, "        %s,\n", cookFieldLiteral(u, fl.Field, fl.Offset, variable))
	}
	b.WriteString("      ],\n    }")
	return b.String()
}

func cookFieldLiteral(u *ir.Unit, f *ir.Field, offset int64, variable bool) string {
	if variable && f.Type.Optional {
		// An optional field places its presence companion at the last piece
		// (docs/SPEC-TABLES.md §19.3, §20.2).
		pieces := ir.FieldPieces(u, f, offset)
		presentAt := pieces[len(pieces)-1].Offset
		plain := *f
		plain.Type.Optional = false
		lit := cookFieldLiteral(u, &plain, offset, variable)
		return strings.TrimSuffix(lit, "}") + fmt.Sprintf(",presentAt:%d}", presentAt)
	}
	name := f.Name
	if !variable {
		name = js.MemberName(f.Name)
	}
	qn := strconv.Quote(name)
	if f.Type.Kind == ir.TNamed && f.Array == ir.ArrayNone && f.KeyEnum == "" && !f.Type.Pointer {
		if un, ok := f.Type.Ref.(*ir.Union); ok {
			return cookUnionLiteral(u, un, qn, offset, variable)
		}
	}
	pieces := ir.FieldPieces(u, f, offset)
	if variable && f.IsMap() {
		// a MAP's sixteen-byte holder; its entries ride in the node extent,
		// ascending by key (§2.8)
		ml := ir.RecordLayout(u, f.MapEntry)
		ks := 0
		if ir.MapKeyField(f).Type.Kind == ir.TString {
			ks = 1
		}
		return fmt.Sprintf("{k:\"map\",n:%s,at:%d,size:%d,align:%d,t:%s,ks:%d}",
			qn, pieces[0].Offset, ml.Size, max(ml.Align, 1), strconv.Quote(f.MapEntry.Name), ks)
	}
	if variable && f.IsList() {
		// a LIST's sixteen-byte holder; its elements ride in the node extent (§2.9)
		size, align := ir.ListElementLayout(u, f)
		return fmt.Sprintf("{k:\"list\",n:%s,at:%d,size:%d,align:%d,elem:%s}",
			qn, pieces[0].Offset, size, max(align, 1), cookElemLiteral(u, f, variable))
	}
	if variable && f.Type.Kind == ir.TWString && f.Array == ir.ArrayNone {
		// char16_t[N+1]: the used CODE UNITS, each a two-byte scalar in the
		// cook's byte order, then a zero tail, and the unit count beside it
		return fmt.Sprintf("{k:\"wstr\",n:%s,at:%d,lenAt:%d,cap:%d}", qn, pieces[0].Offset, pieces[1].Offset, f.Type.Size)
	}
	if f.KeyEnum != "" || f.Array == ir.ArrayFixed || f.Array == ir.ArrayCounted {
		stride := cookElemStride(u, f)
		countAt := int64(-1)
		countFrom := "bound"
		if f.Array == ir.ArrayCounted {
			countAt = pieces[1].Offset
			if variable {
				countFrom = "len"
			} else {
				countFrom = "count"
			}
		}
		elem := cookElemLiteral(u, f, variable)
		return fmt.Sprintf("{k:\"arr\",n:%s,at:%d,countAt:%d,countFrom:%q,bound:%d,stride:%d,elem:%s}",
			qn, pieces[0].Offset, countAt, countFrom, f.ArrayBound, stride, elem)
	}
	if !f.Type.Pointer && (f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes) {
		k := "str"
		if f.Type.Kind == ir.TBytes {
			k = "bytes"
		}
		if variable {
			if f.Type.Kind == ir.TString {
				k = "vstr"
			} else {
				k = "vbytes"
			}
		}
		return fmt.Sprintf("{k:%q,n:%s,at:%d,lenAt:%d,cap:%d}", k, qn, pieces[0].Offset, pieces[1].Offset, f.Type.Size)
	}
	if variable && f.Type.Pointer && f.Array == ir.ArrayNone && f.Type.Blob() {
		k := "bblob"
		if f.Type.Kind == ir.TString {
			k = "sblob"
		}
		return fmt.Sprintf("{k:%q,n:%s,at:%d}", k, qn, pieces[0].Offset)
	}
	if variable && f.Type.Pointer && f.Array == ir.ArrayNone {
		return fmt.Sprintf("{k:\"ptr\",n:%s,at:%d}", qn, pieces[0].Offset)
	}
	at := offset
	if len(pieces) > 0 {
		at = pieces[0].Offset
	}
	return cookScalarLiteral(u, f, qn, at, variable)
}

func cookElemStride(u *ir.Unit, f *ir.Field) int64 {
	single := *f
	single.Array = ir.ArrayNone
	single.ArrayBound = 0
	single.KeyEnum = ""
	single.KeyEnumRef = nil
	single.Type.Optional = false
	pieces := ir.FieldPieces(u, &single, 0)
	var end int64
	for _, p := range pieces {
		if p.Offset+p.Size > end {
			end = p.Offset + p.Size
		}
	}
	return end
}

func cookElemLiteral(u *ir.Unit, f *ir.Field, variable bool) string {
	single := *f
	single.Array = ir.ArrayNone
	single.ArrayBound = 0
	single.KeyEnum = ""
	single.KeyEnumRef = nil
	single.Type.Optional = false
	return cookFieldLiteral(u, &single, 0, variable)
}

func cookUnionLiteral(u *ir.Unit, un *ir.Union, qn string, offset int64, variable bool) string {
	_, _, tag, armAt := ir.UnionLayout(u, un)
	arms := make([]string, len(un.Variants))
	for i, v := range un.Variants {
		if v.F == nil {
			if variable {
				arms[i] = fmt.Sprintf("{v:1,an:%s}", strconv.Quote(v.Name))
			} else {
				arms[i] = "{v:1}"
			}
			continue
		}
		f := *v.F
		if variable {
			f.Name = "value"
		} else {
			f.Name = v.Name
		}
		armLit := cookFieldLiteral(u, &f, 0, variable)
		if variable {
			armLit = strings.TrimSuffix(armLit, "}") + fmt.Sprintf(",an:%s}", strconv.Quote(v.Name))
		}
		arms[i] = armLit
	}
	vFlag := ""
	if variable {
		vFlag = ",var:1"
	}
	return fmt.Sprintf("{k:\"union\",n:%s,at:%d,tagW:%d,armAt:%d,arms:%d%s,arm:[%s]}",
		qn, offset, tag, armAt, len(un.Variants), vFlag, strings.Join(arms, ","))
}

func cookScalarLiteral(u *ir.Unit, f *ir.Field, qn string, at int64, variable bool) string {
	switch f.Type.Kind {
	case ir.TBool:
		return fmt.Sprintf("{k:\"bool\",n:%s,at:%d}", qn, at)
	case ir.TFloat32:
		return fmt.Sprintf("{k:\"f32\",n:%s,at:%d}", qn, at)
	case ir.TFloat64:
		return fmt.Sprintf("{k:\"f64\",n:%s,at:%d}", qn, at)
	case ir.TInt, ir.TFixed, ir.TBits:
		w := int64(f.Type.Width) / 8
		sg := 0
		if f.Type.Kind == ir.TBits {
			if f.Type.Width <= 32 {
				w = 4
			} else {
				w = 8
			}
		} else if f.Type.Signed {
			sg = 1
		}
		return fmt.Sprintf("{k:\"int\",n:%s,at:%d,w:%d,sg:%d}", qn, at, w, sg)
	case ir.TNamed:
		switch r := f.Type.Ref.(type) {
		case *ir.Enum:
			w := int64(ir.StorageBitsFor(r.Max)) / 8
			if variable {
				vs := make([]string, len(r.Variants))
				for i, name := range r.Variants {
					vs[i] = strconv.Quote(name)
				}
				return fmt.Sprintf("{k:\"venum\",n:%s,at:%d,w:%d,max:%d,vs:[%s]}", qn, at, w, r.Max, strings.Join(vs, ","))
			}
			return fmt.Sprintf("{k:\"enum\",n:%s,at:%d,w:%d,max:%d}", qn, at, w, r.Max)
		case *ir.Flags:
			return fmt.Sprintf("{k:\"int\",n:%s,at:%d,w:8,sg:0}", qn, at)
		case *ir.Struct:
			return fmt.Sprintf("{k:\"rec\",n:%s,at:%d,t:%s}", qn, at, strconv.Quote(r.Name))
		}
	}
	_ = u
	return fmt.Sprintf("{k:\"int\",n:%s,at:%d,w:1,sg:0}", qn, at)
}

func cookEngineJS(variable bool) string {
	var b strings.Builder
	b.WriteString(cookEngineShared)
	b.WriteString(cookWriteTextJS(variable))
	b.WriteString(cookEngineTail)
	if variable {
		b.WriteString(cookEngineVariable)
		b.WriteString("  return { variableMeasure, variableReason, variableFrom };\n")
	} else {
		b.WriteString(cookEngineFixed)
		b.WriteString("  return { fixedMeasure, fixedFrom };\n")
	}
	return b.String()
}

func cookWriteTextJS(variable bool) string {
	var b strings.Builder
	b.WriteString("  function writeText(view, base, fd, parent, le) {\n")
	b.WriteString("    let bytes, n;\n")
	if variable {
		b.WriteString("    if (fd.k === \"vstr\") {\n")
		b.WriteString("      if (typeof parent[fd.n] !== \"string\") { return false; }\n")
		b.WriteString("      bytes = TableVariableUtf8Encode(parent[fd.n]);\n")
		b.WriteString("      n = bytes.length;\n")
		b.WriteString("    } else if (fd.k === \"vbytes\") {\n")
		b.WriteString("      bytes = parent[fd.n];\n")
		b.WriteString("      if (!(bytes instanceof Uint8Array)) { return false; }\n")
		b.WriteString("      n = bytes.length;\n")
		b.WriteString("    } else {\n")
	}
	b.WriteString("    bytes = parent[fd.n];\n")
	b.WriteString("    n = parent[fd.n + \"Length\"];\n")
	b.WriteString("    if (!(bytes instanceof Uint8Array)) { return false; }\n")
	if variable {
		b.WriteString("    }\n")
	}
	b.WriteString("    if (!Number.isInteger(n) || n < 0 || n > fd.cap || bytes.length < n) { return false; }\n")
	b.WriteString("    if (view !== null && n > 0) {\n")
	b.WriteString("      const src = bytes instanceof Uint8Array ? bytes.subarray(0, n) : bytes.slice(0, n);\n")
	b.WriteString("      new Uint8Array(view.buffer, view.byteOffset + base + fd.at, n).set(src);\n")
	b.WriteString("    }\n")
	b.WriteString("    return putWidth(view, base + fd.lenAt, 4, n, true, le);\n")
	b.WriteString("  }\n")
	return b.String()
}

const cookEngineShared = `  // A TABLE FLOAT IS FINITE (docs/SPEC-TABLES.md §3.3): the cook writer
  // refuses a NaN or an infinity, a float32 one past the float32 range
  // included, and names it to the measure's reason as non_finite_float
  function refuseFloat(ctx) {
    if (ctx !== null) { ctx.nonFinite = true; }
    return false;
  }
  function fit(v, width, signed) {
    let b;
    if (typeof v === "bigint") { b = v; }
    else if (typeof v === "number" && Number.isInteger(v)) { b = BigInt(v); }
    else { return null; }
    if (width === 16) {
      if (signed) {
        if (b < -(1n << 127n) || b > (1n << 127n) - 1n) { return null; }
      } else if (b < 0n || b > (1n << 128n) - 1n) { return null; }
      return b;
    }
    if (width === 8) {
      if (signed) {
        if (b < -0x8000000000000000n || b > 0x7fffffffffffffffn) { return null; }
      } else if (b < 0n || b > 0xffffffffffffffffn) { return null; }
      return b;
    }
    const n = Number(b);
    let lo = 0, hi = 255;
    if (width === 2) { hi = 65535; }
    else if (width === 4) { hi = 4294967295; }
    if (signed) {
      hi = width === 1 ? 127 : width === 2 ? 32767 : 2147483647;
      lo = width === 1 ? -128 : width === 2 ? -32768 : -2147483648;
    }
    if (n < lo || n > hi) { return null; }
    return b;
  }
  function put64(view, at, v, le) {
    if (view !== null) { view.setBigUint64(at, v, le); }
  }
  function putWidth(view, at, width, v, signed, le) {
    const bits = fit(v, width, signed);
    if (bits === null) { return false; }
    if (view === null) { return true; }
    if (width === 16) {
      const lo = BigInt.asUintN(64, bits);
      const hi = BigInt.asUintN(64, bits >> 64n);
      view.setBigUint64(at, le ? lo : hi, le);
      view.setBigUint64(at + 8, le ? hi : lo, le);
      return true;
    }
    if (width === 8) {
      if (signed) { view.setBigInt64(at, bits, le); }
      else { view.setBigUint64(at, bits, le); }
      return true;
    }
    const n = Number(bits);
    if (width === 1) { if (signed) { view.setInt8(at, n); } else { view.setUint8(at, n); } }
    else if (width === 2) { if (signed) { view.setInt16(at, n, le); } else { view.setUint16(at, n, le); } }
    else { if (signed) { view.setInt32(at, n, le); } else { view.setUint32(at, n, le); } }
    return true;
  }
  function writeStruct(view, base, layout, value, le, ctx) {
    if (layout === undefined || value === null || typeof value !== "object") { return false; }
    for (let i = 0; i < layout.fields.length; i++) {
      if (!writeField(view, base, layout.fields[i], value, le, ctx)) { return false; }
    }
    return true;
  }
  function writeField(view, base, fd, parent, le, ctx) {
    if (parent === null || typeof parent !== "object") { return false; }
    if (fd.presentAt !== undefined) {
      if (parent[fd.n] === null) {
        if (view !== null) { view.setUint8(base + fd.presentAt, 0); }
        if (fd.k === "rec") {
          return writeStruct(view, base + fd.at, layouts[fd.t], layouts[fd.t].make(), le, ctx);
        }
        if (fd.k === "arr") {
          if (fd.countAt >= 0 && !putWidth(view, base + fd.countAt, 4, 0, true, le)) { return false; }
          for (let i = 0; i < fd.bound; i++) {
            if (!writeValue(view, base + fd.at + i * fd.stride, fd.elem, holeValue(fd.elem), le, ctx)) { return false; }
          }
        }
        return true;
      }
      if (view !== null) { view.setUint8(base + fd.presentAt, 1); }
    }
    if (fd.k === "arr") { return writeArray(view, base, fd, parent, le, ctx); }
    // a LIST's or a MAP's holder is written by the extent's carve, which
    // places the array it points at (§2.8, §2.9)
    if (fd.k === "list" || fd.k === "map") { return true; }
    if (fd.k === "wstr") { return writeWide(view, base, fd, parent, le); }
    if (fd.k === "str" || fd.k === "bytes" || fd.k === "vstr" || fd.k === "vbytes") { return writeText(view, base, fd, parent, le); }
    if (fd.k === "union") { return writeUnion(view, base + fd.at, fd, parent[fd.n], le, ctx); }
    if (fd.k === "ptr") { return writeRef(view, base + fd.at, ptrIndex(ctx, parent[fd.n]), le, ctx); }
    if (fd.k === "sblob" || fd.k === "bblob") { return writeRef(view, base + fd.at, blobIndex(ctx, parent, fd, parent[fd.n]), le, ctx); }
    return writeValue(view, base + fd.at, fd, parent[fd.n], le, ctx);
  }
  function writeValue(view, at, fd, value, le, ctx) {
    switch (fd.k) {
      case "bool":
        if (typeof value !== "boolean") { return false; }
        if (view !== null) { view.setUint8(at, value ? 1 : 0); }
        return true;
      case "f32":
        if (typeof value !== "number") { return false; }
        if (!Number.isFinite(Math.fround(value))) { return refuseFloat(ctx); }
        if (view !== null) { view.setFloat32(at, value, le); }
        return true;
      case "f64":
        if (typeof value !== "number") { return false; }
        if (!Number.isFinite(value)) { return refuseFloat(ctx); }
        if (view !== null) { view.setFloat64(at, value, le); }
        return true;
      case "int":
        return putWidth(view, at, fd.w, value, fd.sg === 1, le);
      case "enum":
        if (!Number.isInteger(value) || value < 0 || value > fd.max) { return false; }
        return putWidth(view, at, fd.w, value, false, le);
      case "venum": {
        let ord = 0;
        if (value !== null && value !== undefined && value !== "None") {
          const i = fd.vs.indexOf(value);
          if (i < 0) { return false; }
          ord = i + 1;
        }
        if (ord > fd.max) { return false; }
        return putWidth(view, at, fd.w, ord, false, le);
      }
      case "rec":
        return writeStruct(view, at, layouts[fd.t], value, le, ctx);
      case "ptr":
        return writeRef(view, at, ptrIndex(ctx, value), le, ctx);
      case "union":
        return writeUnion(view, at, fd, value, le, ctx);
      case "list": case "map":
        return true;
      default:
        return false;
    }
  }
  // char16_t[N+1]: a JavaScript string IS its UTF-16 code units, each written
  // as a two-byte scalar in the cook's byte order, the unit count beside them
  function writeWide(view, base, fd, parent, le) {
    const s = parent[fd.n];
    if (typeof s !== "string" || s.length > fd.cap) { return false; }
    if (view !== null) {
      for (let i = 0; i < s.length; i++) { view.setUint16(base + fd.at + 2 * i, s.charCodeAt(i), le); }
    }
    return putWidth(view, base + fd.lenAt, 4, s.length, true, le);
  }
  function holeValue(fd) {
    switch (fd.k) {
      case "bool": return false;
      case "int": return fd.w >= 8 ? 0n : 0;
      case "f32": case "f64": case "enum": return 0;
      case "venum": case "ptr": case "sblob": case "bblob": return null;
      case "union": return fd.var ? null : undefined;
      case "rec": return layouts[fd.t].make();
      default: return undefined;
    }
  }
  function writeArray(view, base, fd, parent, le, ctx) {
    const arr = parent[fd.n];
    let count;
    if (fd.countFrom === "bound") { count = fd.bound; }
    else if (fd.countFrom === "len") {
      if (!Array.isArray(arr)) { return false; }
      count = arr.length;
    } else { count = parent[fd.n + "Count"]; }
    if (!Number.isInteger(count) || count < 0 || count > fd.bound) { return false; }
    if (fd.countAt >= 0 && !putWidth(view, base + fd.countAt, 4, count, true, le)) { return false; }
    for (let i = 0; i < fd.bound; i++) {
      let el;
      if (i < count || fd.countFrom !== "len") {
        if (arr == null || arr[i] === undefined) { return false; }
        el = arr[i];
      } else { el = holeValue(fd.elem); }
      if (!writeValue(view, base + fd.at + i * fd.stride, fd.elem, el, le, ctx)) { return false; }
    }
    return true;
  }
  function writeUnion(view, at, fd, value, le, ctx) {
    if (fd.var) {
      if (value === null) {
        return putWidth(view, at, fd.tagW, 0, false, le);
      }
      if (typeof value !== "object") { return false; }
      let tag = 0;
      for (let i = 0; i < fd.arm.length; i++) {
        if (fd.arm[i].an === value.arm) { tag = i + 1; break; }
      }
      if (tag === 0) { return false; }
      if (!putWidth(view, at, fd.tagW, tag, false, le)) { return false; }
      const arm = fd.arm[tag - 1];
      if (arm.v) { return true; }
      return writeField(view, at + fd.armAt, arm, value, le, ctx);
    }
    if (value === null || typeof value !== "object") { return false; }
    const tag = value.type;
    if (!Number.isInteger(tag) || tag < 0 || tag > fd.arms) { return false; }
    if (!putWidth(view, at, fd.tagW, tag, false, le)) { return false; }
    if (tag === 0) { return true; }
    const arm = fd.arm[tag - 1];
    if (arm.v) { return true; }
    return writeField(view, at + fd.armAt, arm, value, le, ctx);
  }
  function writeRef(view, at, idx, le, ctx) {
    if (idx < 0) { return false; }
    if (idx === 0) {
      if (view !== null) { view.setBigInt64(at, 0n, le); }
      return true;
    }
    if (ctx === null || ctx.entries[idx - 1] === undefined) { return false; }
    if (view !== null) {
      const delta = 64n + BigInt(ctx.entries[idx - 1].offset) - BigInt(at);
      view.setBigInt64(at, delta, le);
    }
    return true;
  }
  function ptrIndex(ctx, val) {
    if (val === null || val === undefined) { return 0; }
    if (ctx === null) { return -1; }
    const i = ctx.nodes.index.get(val);
    return i === undefined ? -1 : i;
  }
  function blobIndex(ctx, holder, fd, val) {
    if (val === null || val === undefined) { return 0; }
    if (ctx === null) { return -1; }
    if (fd.k === "sblob" && typeof val === "string") {
      const m = ctx.nodes.slots.get(holder);
      if (m === undefined || !m.has(fd.n)) { return -1; }
      return m.get(fd.n);
    }
    const i = ctx.nodes.index.get(val);
    return i === undefined ? -1 : i;
  }
`

const cookEngineTail = ``

const cookEngineFixed = `  function fixedMeasure(name) {
    const layout = layouts[name];
    return layout === undefined ? -1 : layout.measure;
  }
  function fixedFrom(name, value, buffer, order) {
    const layout = layouts[name];
    if (layout === undefined || value === null || typeof value !== "object") { return false; }
    if (order !== 1 && order !== 2) { return false; }
    if (!(buffer instanceof Uint8Array) || buffer.byteLength < layout.measure) { return false; }
    const le = order === 1;
    if (!writeStruct(null, 64, layout, value, le, null)) { return false; }
    buffer.fill(0, 0, layout.measure);
    const view = new DataView(buffer.buffer, buffer.byteOffset, buffer.byteLength);
    put64(view, 0, 0x4b4f4f434d484353n, le);
    put64(view, 8, build, le);
    put64(view, 16, BigInt(order), le);
    put64(view, 24, BigInt(layout.data), le);
    put64(view, 32, 16n, le);
    put64(view, 40, BigInt(layout.alignCook), le);
    if (!writeStruct(view, 64, layout, value, le, null)) { return false; }
    put64(view, 64 + layout.data, 0n, le);
    put64(view, 64 + layout.data + 8, layout.typeId, le);
    return true;
  }
`

// alignUp is the variable engine's alone: a fixed cook lays one record at
// a constant offset and aligns nothing.
const cookEngineVariable = `  function alignUp(n, a) {
    if (a <= 1) { return n; }
    return Math.floor((n + a - 1) / a) * a;
  }
  // ---- THE NODE EXTENT (docs/SPEC-TABLES.md §2.8, §2.9, §7.2, §7.6;
  // docs/PORTING.md M19; the layout tla/NodeExtent.tla models and
  // internal/tablecook/extent.go writes). A record's lists and maps lay their
  // arrays after the record's own storage: field by field in declaration
  // order, each holder's WHOLE array at the running offset rounded to its
  // element's alignment, then, element by element, the arrays those elements
  // hold by value — PRE-ORDER. The holder takes the self-relative delta to its
  // array (zero when empty) and the count. With view null the same walk
  // measures: one walk, two halves, so they cannot drift. at is the value's
  // own offset, base the extent's, and cur.at the running offset inside it.
  function carveRecord(view, at, base, cur, value, layout, le, ctx) {
    if (layout === undefined || !layout.ext) { return true; }
    if (value === null || typeof value !== "object") { return false; }
    for (let i = 0; i < layout.fields.length; i++) {
      const fd = layout.fields[i];
      if (fd.k === "ptr" || fd.k === "sblob" || fd.k === "bblob") { continue; }
      const v = value[fd.n];
      if (fd.presentAt !== undefined && v === null) { continue; }
      if (!carveValue(view, at + fd.at, base, cur, fd, v, le, ctx)) { return false; }
    }
    return true;
  }
  function carveValue(view, at, base, cur, fd, v, le, ctx) {
    switch (fd.k) {
      case "list": case "map":
        return carveHolder(view, at, base, cur, fd, v, le, ctx);
      case "rec":
        return carveRecord(view, at, base, cur, v, layouts[fd.t], le, ctx);
      case "arr": {
        const e = fd.elem;
        if (e.k !== "rec" && e.k !== "union" && e.k !== "list" && e.k !== "map") { return true; }
        if (e.k === "rec" && !layouts[e.t].ext) { return true; }
        // a slot past a counted array's live elements holds the
        // value-initialized element, which holds no list and no map
        const n = fd.countFrom === "len" ? (Array.isArray(v) ? v.length : 0) : fd.bound;
        for (let i = 0; i < n; i++) {
          if (!carveValue(view, at + i * fd.stride, base, cur, e, v[i], le, ctx)) { return false; }
        }
        return true;
      }
      case "union": {
        if (!fd.var || v === null || v === undefined || typeof v !== "object") { return true; }
        for (let i = 0; i < fd.arm.length; i++) {
          const arm = fd.arm[i];
          if (arm.an !== v.arm) { continue; }
          if (arm.v || arm.k === "ptr" || arm.k === "sblob" || arm.k === "bblob") { return true; }
          return carveValue(view, at + fd.armAt + arm.at, base, cur, arm, v.value, le, ctx);
        }
        return true;
      }
      default:
        return true;
    }
  }
  // A holder's elements in the order its array lays them: a list's as it holds
  // them, a map's entries ASCENDING BY KEY (§2.8). THE WRITER HOLDS THE SORT:
  // TableVariableMapEntries orders the entries a Map holds in whatever order,
  // so a Map built out of key order cooks the same bytes, and under the
  // cook's entry cache the numbering, the measure and the write all see the
  // same entry objects.
  function holderElements(fd, v) {
    if (fd.k === "map") {
      if (!(v instanceof Map)) { return null; }
      return TableVariableMapEntries({ c: layouts[fd.t], ks: fd.ks === 1 }, v);
    }
    return Array.isArray(v) ? v : null;
  }
  function carveHolder(view, slot, base, cur, fd, v, le, ctx) {
    const elems = holderElements(fd, v);
    if (elems === null) { return false; }
    const n = elems.length;
    cur.at = alignUp(cur.at, fd.align);
    const array = base + cur.at;
    cur.at += n * fd.size;
    if (view !== null) {
      view.setBigInt64(slot, n > 0 ? BigInt(array - slot) : 0n, le);
      view.setInt32(slot + 8, n, le);
    }
    if (fd.k === "map") {
      const entry = layouts[fd.t];
      for (let i = 0; i < n; i++) {
        if (!writeStruct(view, array + i * fd.size, entry, elems[i], le, ctx)) { return false; }
      }
      for (let i = 0; i < n; i++) {
        if (!carveRecord(view, array + i * fd.size, base, cur, elems[i], entry, le, ctx)) { return false; }
      }
      return true;
    }
    for (let i = 0; i < n; i++) {
      if (!writeValue(view, array + i * fd.size, fd.elem, elems[i], le, ctx)) { return false; }
    }
    for (let i = 0; i < n; i++) {
      if (!carveValue(view, array + i * fd.size, base, cur, fd.elem, elems[i], le, ctx)) { return false; }
    }
    return true;
  }
  // the save's entry cache (TableVariableEntryCache), held for one whole
  // measure or write so every walk sees one sorted entry object per map
  function withEntries(f) {
    const outer = TableVariableEntryCache;
    TableVariableEntryCache = new Map();
    try { return f(); } finally { TableVariableEntryCache = outer; }
  }
  function blobSize(entry) {
    return 8 + entry.len + (entry.string ? 1 : 0);
  }
  function region(name, value) {
    if (value === null || typeof value !== "object") { return { reason: "invalid_value" }; }
    const codec = TableVariableCodecs[name];
    const layout = layouts[name];
    if (codec === undefined || layout === undefined) { return { reason: "invalid_value" }; }
    const nodes = TableVariableNumber(codec, value, false);
    if (nodes === null) { return { reason: "data_cycle" }; }
    const entries = [{ typeId: layout.typeId, v: value, blob: false, layout, offset: 0 }];
    for (const n of nodes.list) {
      if (n.codec === undefined) {
        if (n.body == null) { return { reason: "invalid_value" }; }
        entries.push({ typeId: n.typeId, blob: true, body: n.body, string: n.typeId === TableStringBlobId, len: n.body.length, offset: 0 });
      } else {
        const child = layouts[n.codec.name];
        if (child === undefined) { return { reason: "invalid_value" }; }
        entries.push({ typeId: child.typeId, v: n.v, blob: false, layout: child, offset: 0 });
      }
    }
    const ctx = { entries, nodes, nonFinite: false };
    // a record node that holds a list or a map is its storage rounded to
    // ir.CookExtentStart, then its extent, at ir.CookNodeAlign (§2.8, §6.2)
    for (let i = 0; i < entries.length; i++) {
      const e = entries[i];
      e.extent = 0;
      if (!e.blob && e.layout.ext) {
        const cur = { at: 0 };
        if (!carveRecord(null, 0, 0, cur, e.v, e.layout, true, ctx)) { return { reason: ctx.nonFinite ? "non_finite_float" : "invalid_value" }; }
        e.extent = cur.at;
      }
    }
    let data = 0, align = 8;
    for (let i = 0; i < entries.length; i++) {
      const e = entries[i];
      const alignment = e.blob ? 8 : (e.layout.ext ? e.layout.nodeAlign : e.layout.align);
      const size = e.blob ? blobSize(e) : (e.layout.ext ? e.layout.extentStart + e.extent : e.layout.size);
      if (alignment > align) { align = alignment; }
      data = alignUp(data, alignment);
      e.offset = data;
      data += size;
    }
    data = alignUp(data, align);
    for (let i = 0; i < entries.length; i++) {
      const e = entries[i];
      if (!e.blob && !writeStruct(null, 64 + e.offset, e.layout, e.v, true, ctx)) { return { reason: ctx.nonFinite ? "non_finite_float" : "invalid_value" }; }
    }
    return { reason: null, total: 64 + data + entries.length * 16, data, align, entries, ctx };
  }
  function variableMeasure(name, value) {
    return withEntries(() => {
      const reg = region(name, value);
      return reg.reason === null ? reg.total : -1;
    });
  }
  function variableReason(name, value) {
    return withEntries(() => {
      const reg = region(name, value);
      if (reg.reason !== null) { return [-1, reg.reason]; }
      return [reg.total, null];
    });
  }
  function variableFrom(name, value, buffer, order) {
    return withEntries(() => cookFrom(name, value, buffer, order));
  }
  function cookFrom(name, value, buffer, order) {
    if (order !== 1 && order !== 2) { return false; }
    const reg = region(name, value);
    if (reg.reason !== null) { return false; }
    if (!(buffer instanceof Uint8Array) || buffer.byteLength < reg.total) { return false; }
    buffer.fill(0, 0, reg.total);
    const view = new DataView(buffer.buffer, buffer.byteOffset, buffer.byteLength);
    const le = order === 1;
    put64(view, 0, 0x4b4f4f434d484353n, le);
    put64(view, 8, build, le);
    put64(view, 16, BigInt(order), le);
    put64(view, 24, BigInt(reg.data), le);
    put64(view, 32, BigInt(reg.entries.length * 16), le);
    put64(view, 40, BigInt(reg.align), le);
    for (let i = 0; i < reg.entries.length; i++) {
      const e = reg.entries[i];
      const at = 64 + e.offset;
      if (e.blob) {
        view.setUint32(at, e.len, le);
        if (e.len > 0) {
          const src = e.body instanceof Uint8Array ? e.body : Uint8Array.from(e.body);
          new Uint8Array(view.buffer, view.byteOffset + at + 8, e.len).set(src);
        }
      } else {
        if (!writeStruct(view, at, e.layout, e.v, le, reg.ctx)) { return false; }
        if (e.layout.ext && !carveRecord(view, at, at + e.layout.extentStart, { at: 0 }, e.v, e.layout, le, reg.ctx)) { return false; }
      }
    }
    const dir = 64 + reg.data;
    for (let i = 0; i < reg.entries.length; i++) {
      const e = reg.entries[i];
      put64(view, dir + i * 16, BigInt(e.offset), le);
      put64(view, dir + i * 16 + 8, e.typeId, le);
    }
    return true;
  }
`

// cookRecordHasExtent reports a record with a list or a map reachable BY VALUE
// (docs/SPEC-TABLES.md §2.8, §2.9): the records whose node is their storage
// PLUS an extent, the tool's recordHasExtent (internal/tablecook/extent.go).
// A pointee and a byte buffer are nodes of their own, so neither is reached.
func cookRecordHasExtent(st *ir.Struct) bool {
	return cookStructExtent(st, map[*ir.Struct]bool{}, map[*ir.Union]bool{})
}

func cookStructExtent(st *ir.Struct, seenS map[*ir.Struct]bool, seenU map[*ir.Union]bool) bool {
	if st == nil || seenS[st] {
		return false
	}
	seenS[st] = true
	for _, f := range st.Fields {
		if cookFieldExtent(f, seenS, seenU) {
			return true
		}
	}
	return false
}

func cookFieldExtent(f *ir.Field, seenS map[*ir.Struct]bool, seenU map[*ir.Union]bool) bool {
	if f.IsMap() || f.IsList() {
		return true
	}
	if f.Type.Pointer || f.Type.Blob() || f.Type.Kind != ir.TNamed {
		return false
	}
	switch ref := f.Type.Ref.(type) {
	case *ir.Struct:
		return cookStructExtent(ref, seenS, seenU)
	case *ir.Union:
		if seenU[ref] {
			return false
		}
		seenU[ref] = true
		for _, v := range ref.Variants {
			if v.F != nil && cookFieldExtent(v.F, seenS, seenU) {
				return true
			}
			if v.Ref != nil && cookStructExtent(v.Ref, seenS, seenU) {
				return true
			}
		}
	}
	return false
}
