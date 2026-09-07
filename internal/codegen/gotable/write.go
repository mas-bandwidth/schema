package gotable

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// Measurement uses the same elision and validation walk as writing. A nested
// payload is measured with its parent's first-use vocabulary; writing restores
// that vocabulary to the payload's start, never patches a variable-width L.
func (g *tableGen) emitTableMeasure(st *ir.Struct) {
	n := st.Name
	g.pf("func %sMeasureBody(value *%s, ids *TableIds) int64 {\n", n, n)
	g.pf("\tw := TableWriter{Measuring:true, Ids:ids}\n")
	g.pf("\tif !%sSaveBody(&w, value) { return -1 }; return w.Offset\n}\n\n", n)
	g.pf("// %sMeasure returns the exact file size, or -1 for invalid storage.\n", n)
	g.pf("func %sMeasure(value *%s) int64 {\n", n, n)
	g.pf("\tvar ids TableIds\n\tw := TableWriter{Measuring:true, Ids:&ids}\n\tw.Put8(1)\n")
	g.pf("\tif !%sSaveBody(&w, value) { return -1 }; w.Trailer()\n", n)
	g.pf("\tif w.Overflow || ids.Overflow { return -1 }; return w.Offset\n}\n\n")
}

func (g *tableGen) emitTableWrite(st *ir.Struct) {
	g.pf("// %sSaveBody writes a body under the writer's shared id vocabulary.\n", st.Name)
	g.pf("func %sSaveBody(w *TableWriter, value *%s) bool {\n", st.Name, st.Name)
	guards := tableGuardExprs(st)
	for _, f := range st.Fields {
		cond := guards[f.Name]
		if f.Type.Optional {
			present := "value." + member(f) + "Present"
			if cond == "" {
				cond = present
			} else {
				cond = "(" + cond + ") && " + present
			}
		}
		if cond != "" {
			g.pf("\tif %s {\n", cond)
			g.indent = "\t"
		}
		g.emitWireField(f, f.Type.Optional)
		if cond != "" {
			g.indent = ""
			g.pf("\t}\n")
		}
	}
	g.pf("\tw.Put8(0)\n\treturn !w.Overflow && !w.Ids.Overflow\n}\n\n")
}

func (g *tableGen) emitTableSave(st *ir.Struct) {
	g.pf("// %sSave writes a form-1 file, returning -1 if storage or capacity is invalid.\n", st.Name)
	g.pf("func %sSave(value *%s, buffer []byte) int64 {\n", st.Name, st.Name)
	g.pf("\tvar ids TableIds\n\tw := TableWriter{Buffer:buffer, Ids:&ids}\n\tw.Put8(1)\n")
	g.pf("\tif !%sSaveBody(&w, value) { return -1 }; w.Trailer()\n", st.Name)
	g.pf("\tif w.Overflow || ids.Overflow { return -1 }; return w.Offset\n}\n\n")
}

func (g *tableGen) emitWireHeader(f *ir.Field, kind int, ind string) {
	g.pf("%sw.Id(0x%016x); w.Put8(%d) // %s\n", ind, ir.TableFieldWireId(f), kind, f.Name)
}

func (g *tableGen) emitWireField(f *ir.Field, force bool) {
	name, kind := member(f), ir.TableWireScalarKind(f)
	expr := "value." + name
	switch {
	case f.KeyEnum != "":
		g.emitWireArray(f, force, true)
	case f.Array != ir.ArrayNone || f.Type.Kind == ir.TBytes:
		g.emitWireArray(f, force, false)
	case f.Type.Kind == ir.TString:
		g.pf("\tif %sLength < 0 || %sLength > %d { return false }\n", expr, expr, f.Type.Size)
		cond := expr + "Length > 0"
		if force {
			cond = "true"
		}
		g.pf("\tif %s {\n", cond)
		g.emitWireHeader(f, tkString, "\t\t")
		g.pf("\t\tw.PutLeb(uint64(%sLength)); w.Raw(%s[:%sLength])\n\t}\n", expr, expr, expr)
	case kind == tkTable:
		g.pf("\t{\n\t\tmark := w.Ids.Count\n\t\tref := w.Ids.Ref(0x%016x)\n\t\tstart := w.Ids.Count\n", ir.TableFieldWireId(f))
		g.pf("\t\tbody := %sMeasureBody(&%s, w.Ids)\n\t\tif body < 0 { return false }\n", f.Type.Name, expr)
		cond := "body > 1"
		if force {
			cond = "true"
		}
		g.pf("\t\tif %s {\n", cond)
		g.emitMeasuredFrame("ref", "13", "body", "\t\t\t", func(ind string) {
			g.pf("%sif !%sSaveBody(w, &%s) { return false }\n", ind, f.Type.Name, expr)
		})
		g.pf("\t\t} else { w.Ids.Truncate(mark) }\n\t}\n")
	case kind == tkUnion:
		un := f.Type.Ref.(*ir.Union)
		g.pf("\tswitch %s.Type {\n\tcase %sTypeNone:\n", expr, un.Name)
		for _, v := range un.Variants {
			arm := expr + "." + ir.GoExportName(v.Name)
			g.pf("\tcase %sType%s:\n", un.Name, ir.GoExportName(v.Name))
			g.emitWireHeader(f, tkUnion, "\t\t")
			g.pf("\t\tref := w.Ids.Ref(0x%016x)\n\t\tstart := w.Ids.Count\n", ir.TableWireId(v.WireName()))
			g.pf("\t\tbody := %sMeasureBody(&%s, w.Ids)\n\t\tif body < 0 { return false }\n", v.Type, arm)
			g.emitMeasuredFrame("ref", "13", "body", "\t\t", func(ind string) {
				g.pf("%sif !%sSaveBody(w, &%s) { return false }\n", ind, v.Type, arm)
			})
		}
		g.pf("\tdefault: return false\n\t}\n")
	default:
		cond := expr + " != " + fieldDefaultExpr(f)
		if force {
			cond = "true"
		}
		g.pf("\tif %s {\n", cond)
		g.emitWireHeader(f, kind, "\t\t")
		g.emitWireElement(f, expr, "w", "\t\t", false)
		g.pf("\t}\n")
	}
}

// start is the vocabulary count just before the measured payload, whose ids
// now stand in the vocabulary. Measuring keeps them; saving replays them.
func (g *tableGen) emitMeasuredFrame(ref, kind, size, ind string, write func(string)) {
	g.pf("%sif w.Measuring {\n%s\tw.Advance(tableLebBytes(%s)+1+tableLebBytes(uint64(%s))+%s)\n", ind, ind, ref, size, size)
	g.pf("%s} else {\n%s\tw.Ids.Truncate(start)\n%s\tw.PutLeb(%s); w.Put8(%s); w.PutLeb(uint64(%s))\n", ind, ind, ind, ref, kind, size)
	write(ind + "\t")
	g.pf("%s}\n", ind)
}

func (g *tableGen) emitWireArray(f *ir.Field, force, keyed bool) {
	name := member(f)
	expr := "value." + name
	kind := ir.TableWireScalarKind(f)
	count := fmt.Sprint(f.ArrayBound)
	if f.Type.Kind == ir.TBytes {
		kind = tkU8
		count = expr + "Length"
	} else if f.Array == ir.ArrayCounted {
		count = expr + "Count"
	}
	if keyed {
		count = keyedLoopBound(f)
	}
	if f.Type.Kind == ir.TBytes || f.Array == ir.ArrayCounted {
		bound := f.ArrayBound
		if f.Type.Kind == ir.TBytes {
			bound = f.Type.Size
		}
		g.pf("\tif %s < 0 || %s > %d { return false }\n", count, count, bound)
	}
	g.pf("\t{\n")
	// A scalar fixed array elides only when all elements have their defaults.
	cond := count + " > 0"
	if f.Array == ir.ArrayFixed && kind != tkTable && !keyed {
		g.pf("\t\tallDefault := true\n\t\tfor i := 0; i < int(%s); i++ { if %s[i] != %s { allDefault = false; break } }\n", count, expr, fieldDefaultExpr(f))
		cond = "!allDefault"
	}
	if force || f.Array == ir.ArrayFixed && kind == tkTable {
		cond = "true"
	}
	g.pf("\t\tif %s {\n", cond)
	g.pf("\t\t\tmark := w.Ids.Count\n\t\t\tref := w.Ids.Ref(0x%016x)\n\t\t\tstart := w.Ids.Count\n", ir.TableFieldWireId(f))
	g.pf("\t\t\tbody := TableWriter{Measuring:true, Ids:w.Ids}\n\t\t\tpairs := uint64(0)\n")
	g.pf("\t\t\tfor i := 0; i < int(%s); i++ {\n", count)
	g.emitWireArrayElement(f, expr, kind, keyed, "body", "\t\t\t\t")
	g.pf("\t\t\t\tpairs++\n\t\t\t}\n")
	wireKind := tkArray
	if keyed {
		wireKind = tkKeyed
	}
	if keyed && !force {
		g.pf("\t\t\tif pairs == 0 { w.Ids.Truncate(mark) } else {\n")
	} else {
		g.pf("\t\t\t_ = mark\n\t\t\t{\n")
	}
	g.pf("\t\t\t\tif body.Overflow { return false }\n\t\t\t\tsize := int64(1)+tableLebBytes(pairs)+body.Offset\n")
	g.emitMeasuredFrame("ref", fmt.Sprint(wireKind), "size", "\t\t\t\t", func(ind string) {
		g.pf("%sw.Put8(%d); w.PutLeb(pairs)\n", ind, kind)
		g.pf("%sfor i := 0; i < int(%s); i++ {\n", ind, count)
		g.emitWireArrayElement(f, expr, kind, keyed, "w", ind+"\t")
		g.pf("%s}\n", ind)
	})
	g.pf("\t\t\t}\n\t\t}\n\t}\n")
}

func writerPointer(name string) string {
	if name == "w" {
		return name
	}
	return "&" + name
}

func (g *tableGen) emitWireArrayElement(f *ir.Field, expr string, kind int, keyed bool, writer, ind string) {
	if keyed {
		if kind == tkTable {
			g.pf("%selementMark := %s.Ids.Count\n", ind, writer)
			g.pf("%skeyID, named := %s(i+1).TableEnumId(); if !named { return false }\n", ind, f.KeyEnum)
			g.pf("%skeyRef := %s.Ids.Ref(keyID)\n", ind, writer)
			g.pf("%selementStart := %s.Ids.Count\n", ind, writer)
			g.pf("%sn := %sMeasureBody(&%s[i], %s.Ids); if n < 0 { return false }\n", ind, f.Type.Name, expr, writer)
			g.pf("%sif n == 1 { %s.Ids.Truncate(elementMark); continue }\n", ind, writer)
			g.pf("%s%s.PutLeb(keyRef); %s.PutLeb(uint64(n))\n", ind, writer, writer)
			g.pf("%sif %s.Measuring { %s.Advance(n) } else {\n", ind, writer, writer)
			g.pf("%s\t%s.Ids.Truncate(elementStart)\n%s\tif !%sSaveBody(%s, &%s[i]) { return false }\n%s}\n", ind, writer, ind, f.Type.Name, writerPointer(writer), expr, ind)
			return
		}
		g.pf("%sif %s[i] == %s { continue }\n", ind, expr, fieldDefaultExpr(f))
		g.pf("%skeyID, named := %s(i+1).TableEnumId(); if !named { return false }; %s.Id(keyID)\n", ind, f.KeyEnum, writer)
		if enumRef(f) != nil {
			g.pf("%selementID, named := %s[i].TableEnumId(); if !named { return false }\n", ind, expr)
			g.pf("%selementRef := uint64(0); if %s[i] != %sNone { elementRef = %s.Ids.Ref(elementID) }\n", ind, expr, f.Type.Name, writer)
			g.pf("%s%s.PutLeb(uint64(tableLebBytes(elementRef))); %s.PutLeb(elementRef)\n", ind, writer, writer)
			return
		}
		g.pf("%s%s.PutLeb(%d)\n", ind, writer, ir.TableKindWidth(kind))
	}
	g.emitWireElement(f, expr+"[i]", writer, ind, kind == tkTable)
}

func (g *tableGen) emitWireElement(f *ir.Field, expr, writer, ind string, framed bool) {
	if enumRef(f) != nil {
		g.pf("%s{\n%s\tid, named := %s.TableEnumId(); if !named { return false }\n", ind, ind, expr)
		g.pf("%s\tif %s == %sNone { %s.PutLeb(0) } else { %s.Id(id) }\n%s}\n", ind, expr, f.Type.Name, writer, writer, ind)
		return
	}
	kind := ir.TableWireScalarKind(f)
	if f.Type.Kind == ir.TBytes {
		kind = tkU8
	}
	switch kind {
	case tkTable:
		g.pf("%s{\n%s\tmark := %s.Ids.Count\n", ind, ind, writer)
		g.pf("%s\tn := %sMeasureBody(&%s, %s.Ids); if n < 0 { return false }\n", ind, f.Type.Name, expr, writer)
		if framed {
			g.pf("%s\t%s.PutLeb(uint64(n))\n", ind, writer)
		}
		g.pf("%s\tif %s.Measuring { %s.Advance(n) } else {\n", ind, writer, writer)
		g.pf("%s\t\t%s.Ids.Truncate(mark)\n%s\t\tif !%sSaveBody(%s, &%s) { return false }\n%s\t}\n%s}\n", ind, writer, ind, f.Type.Name, writerPointer(writer), expr, ind, ind)
	case tkBool:
		g.pf("%sif %s { %s.Put8(1) } else { %s.Put8(0) }\n", ind, expr, writer, writer)
	case tkF32, tkF64:
		g.needsMath = true
		bits := 32
		if kind == tkF64 {
			bits = 64
		}
		g.pf("%s%s.Put%d(math.Float%dbits(%s))\n", ind, writer, bits, bits, expr)
	default:
		width := ir.TableKindWidth(kind) * 8
		g.pf("%s%s.Put%d(uint%d(%s))\n", ind, writer, width, width, expr)
	}
}
