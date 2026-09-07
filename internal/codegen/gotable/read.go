package gotable

import (
	"github.com/mas-bandwidth/schema/v2/ir"
)

func (g *tableGen) emitTableRead(st *ir.Struct) {
	n := st.Name
	g.pf("func %sLoadBody(r *TableReader, value *%s) bool {\n", n, n)
	g.pf("\t%sReset(value)\n", n)
	g.pf("\tfor {\n\t\tref, ok := r.Leb(); if !ok { r.Report.Malformed = true; return false }\n")
	g.pf("\t\tif ref == 0 { return true }\n")
	g.pf("\t\tfieldID, ok := r.Resolve(ref); if !ok || !r.Has(1) { r.Report.Malformed = true; return false }\n")
	g.pf("\t\tif fieldID == 0xfffffffffffffffe || fieldID == 0xfffffffffffffffd || r.Nested && fieldID == 0xffffffffffffffff { r.Report.Malformed = true; return false }\n")
	g.pf("\t\tkind := r.Get8()\n\t\tswitch fieldID {\n")
	for _, f := range st.Fields {
		kind := ir.TableWireFieldKind(f)
		g.pf("\t\tcase 0x%016x: // %s\n", ir.TableFieldWireId(f), f.Name)
		g.pf("\t\t\tif kind != %d {\n", kind)
		if wireWidenable(kind) {
			g.pf("\t\t\t\tif tableKindWidens(kind, %d) { r.Report.Widened++ } else {\n", kind)
		}
		g.pf("\t\t\t\tr.Report.KindMismatch++; if !r.Skip(kind) { r.Report.Malformed = true; return false }; break\n")
		if wireWidenable(kind) {
			g.pf("\t\t\t\t}\n")
		}
		g.pf("\t\t\t}\n")
		g.emitReadField(f, "\t\t\t")
		if f.Type.Optional {
			g.pf("\t\t\tvalue.%sPresent = true\n", member(f))
		}
	}
	g.pf("\t\tdefault:\n\t\t\tr.Report.Unknown++; if !r.Skip(kind) { r.Report.Malformed = true; return false }\n\t\t}\n\t}\n}\n\n")
	g.pf("func %sLoad(value *%s, data []byte, report *TableReport) bool {\n", n, n)
	g.pf("\tif report == nil { var ignored TableReport; report = &ignored }\n")
	g.pf("\tr, verdict := tableOpen(data, report); report.Verdict = verdict\n")
	g.pf("\tif verdict != TableOpenOk { %sReset(value); if verdict == TableOpenDamaged { report.Malformed = true }; return false }\n", n)
	g.pf("\tif !%sLoadBody(&r, value) { report.Verdict = TableOpenBodyStopped; return false }; return true\n}\n\n", n)
}

func wireWidenable(kind int) bool {
	return kind >= 3 && kind <= 5 || kind >= 7 && kind <= 9 || kind == tkF64
}

func (g *tableGen) emitReadField(f *ir.Field, ind string) {
	expr := "value." + member(f)
	kind := ir.TableWireScalarKind(f)
	switch {
	case f.KeyEnum != "":
		g.emitReadArray(f, ind, true)
	case f.Array != ir.ArrayNone || f.Type.Kind == ir.TBytes:
		g.emitReadArray(f, ind, false)
	case f.Type.Kind == ir.TString:
		g.pf("%ssub, ok := r.Body(); if !ok { r.Report.Malformed = true; return false }\n", ind)
		g.pf("%sif !tableUtf8Valid(sub.Buffer) { r.Report.Malformed = true; %sLength = 0; clear(%s[:]); break }\n", ind, expr, expr)
		g.pf("%skeep := int64(len(sub.Buffer)); if keep > %d { keep = tableUtf8Clamp(sub.Buffer, %d); r.Report.Clamped++ }\n", ind, f.Type.Size, f.Type.Size)
		g.pf("%sclear(%s[:]); copy(%s[:keep], sub.Buffer); %sLength = int32(keep)\n", ind, expr, expr, expr)
	case kind == tkTable:
		g.pf("%ssub, ok := r.Body(); if !ok { r.Report.Malformed = true; return false }\n", ind)
		g.emitReadNested(f.Type.Name, expr, "sub", ind)
	case kind == tkUnion:
		g.emitReadUnion(f.Type.Ref.(*ir.Union), expr, "r", ind, "return false")
	default:
		g.emitReadScalar(f, expr, "r", "kind", ind, "r.Report.Malformed = true; return false")
	}
}

func (g *tableGen) emitReadNested(typ, expr, rdr, ind string) {
	g.pf("%s%sLoadBody(&%s, &%s)\n%sif %s.Offset != int64(len(%s.Buffer)) { r.Report.Malformed = true; %sReset(&%s) }\n", ind, typ, rdr, expr, ind, rdr, rdr, typ, expr)
}

func (g *tableGen) emitReadArray(f *ir.Field, ind string, keyed bool) {
	expr := "value." + member(f)
	kind, bound := ir.TableWireScalarKind(f), f.ArrayBound
	if f.Type.Kind == ir.TBytes {
		kind, bound = tkU8, f.Type.Size
	}
	g.pf("%ssub, ok := r.Body(); if !ok { r.Report.Malformed = true; return false }\n", ind)
	g.pf("%sif len(sub.Buffer) >= 2 {\n", ind)
	i := ind + "\t"
	g.pf("%selemKind := sub.Get8(); count, ok := sub.Leb()\n", i)
	g.pf("%sif !ok { r.Report.Malformed = true } else {\n", i)
	i += "\t"
	g.pf("%sif elemKind != %d {\n", i, kind)
	if wireWidenable(kind) {
		g.pf("%s\tif tableKindWidens(elemKind, %d) { r.Report.Widened++ } else { r.Report.KindMismatch++; break }\n", i, kind)
	} else {
		g.pf("%s\tr.Report.KindMismatch++; break\n", i)
	}
	g.pf("%s}\n", i)
	if keyed {
		g.pf("%sfor i := uint64(0); i < count; i++ {\n", i)
		j := i + "\t"
		g.pf("%skeyRef, ok := sub.Leb(); if !ok { r.Report.Malformed = true; break }\n", j)
		g.pf("%skey, ok := sub.Resolve(keyRef); if !ok { r.Report.Malformed = true; break }\n", j)
		g.pf("%selem, ok := sub.Body(); if !ok { r.Report.Malformed = true; break }\n", j)
		g.pf("%svar slot %s; if !slot.TableEnumValue(key) { r.Report.Unknown++; continue }\n", j, f.KeyEnum)
		if kind == tkTable {
			g.pf("%s%sLoadBody(&elem, &%s[int(slot)-1])\n", j, f.Type.Name, expr)
		} else {
			g.emitReadScalar(f, expr+"[int(slot)-1]", "elem", "elemKind", j, "r.Report.Malformed = true; continue")
		}
		g.pf("%s}\n", i)
	} else {
		g.pf("%skeep := count; if keep > %d { keep = %d; r.Report.Clamped++ }\n", i, bound, bound)
		counted := f.Array == ir.ArrayCounted || f.Type.Kind == ir.TBytes
		if counted {
			g.pf("%sdecoded := int32(0)\n", i)
		}
		g.pf("%sfor i := uint64(0); i < keep; i++ {\n", i)
		j := i + "\t"
		if kind == tkTable {
			g.pf("%selem, ok := sub.Body(); if !ok { r.Report.Malformed = true; break }\n", j)
			g.pf("%s%sLoadBody(&elem, &%s[i])\n", j, f.Type.Name, expr)
		} else {
			g.emitReadScalar(f, expr+"[i]", "sub", "elemKind", j, "r.Report.Malformed = true; break")
		}
		if counted {
			g.pf("%sdecoded = int32(i+1)\n", j)
		}
		g.pf("%s}\n", i)
		if f.Type.Kind == ir.TBytes {
			g.pf("%s%sLength = decoded\n", i, expr)
		} else if counted {
			g.pf("%s%sCount = decoded\n", i, expr)
		}
	}
	g.pf("%s\t}\n%s}\n", ind, ind)
}

func (g *tableGen) emitReadUnion(un *ir.Union, expr, rdr, ind, stop string) {
	g.pf("%sarmRef, ok := %s.Leb(); if !ok { r.Report.Malformed = true; %s }\n", ind, rdr, stop)
	g.pf("%s%s.Type = %sTypeNone\n", ind, expr, un.Name)
	g.pf("%sif armRef != 0 {\n", ind)
	i := ind + "\t"
	g.pf("%sarmID, ok := %s.Resolve(armRef); if !ok || !%s.Has(1) { r.Report.Malformed = true; %s }\n", i, rdr, rdr, stop)
	g.pf("%sarmKind := %s.Get8(); arm, ok := %s.Body(); if !ok { r.Report.Malformed = true; %s }\n", i, rdr, rdr, stop)
	g.pf("%sswitch armID {\n", i)
	for _, v := range un.Variants {
		armExpr := expr + "." + ir.GoExportName(v.Name)
		g.pf("%scase 0x%016x:\n", i, ir.TableWireId(v.WireName()))
		g.pf("%s\tif armKind != 13 { r.Report.KindMismatch++; break }\n", i)
		
		g.pf("%s\t%s.Type = %sType%s\n", i, expr, un.Name, ir.GoExportName(v.Name))
		g.pf("%s\t%sLoadBody(&arm, &%s)\n", i, v.Type, armExpr)
		g.pf("%s\tif arm.Offset != int64(len(arm.Buffer)) { r.Report.Malformed = true; %s.Type = %sTypeNone }\n", i, expr, un.Name)
	}
	g.pf("%sdefault: r.Report.Unknown++\n%s}\n%s}\n", i, i, ind)
}

func (g *tableGen) emitReadScalar(f *ir.Field, expr, rdr, wireKind, ind, onBad string) {
	kind := ir.TableWireScalarKind(f)
	if f.Type.Kind == ir.TBytes {
		kind = tkU8
	}
	if enumRef(f) != nil {
		g.pf("%s{\n%s\tvariantRef, ok := %s.Leb(); if !ok { %s }\n", ind, ind, rdr, onBad)
		g.pf("%s\tif variantRef == 0 { %s = %sNone } else {\n", ind, expr, f.Type.Name)
		g.pf("%s\t\tid, ok := %s.Resolve(variantRef); if !ok { %s }\n", ind, rdr, onBad)
		g.pf("%s\t\tif !%s.TableEnumValue(id) { r.Report.Unknown++ }\n%s\t}\n%s}\n", ind, expr, ind, ind)
		return
	}
	g.pf("%sif !%s.Has(tableKindBytes(%s)) { %s }\n", ind, rdr, wireKind, onBad)
	switch kind {
	case tkBool:
		g.pf("%s%s = %s.Get8() != 0\n", ind, expr, rdr)
	case tkF32, tkF64:
		g.needsMath = true
		if kind == tkF32 {
			g.pf("%s{\n%s\tv := math.Float32frombits(%s.Get32())\n", ind, ind, rdr)
		} else {
			g.pf("%s{\n%s\tvar v float64\n%s\tif %s == 10 { v = math.Float64frombits(tableWidenFloat(%s.Get32())) } else { v = math.Float64frombits(%s.Get64()) }\n", ind, ind, ind, wireKind, rdr, rdr)
		}
		if f.HasFloatRange {
			lo, hi := formatFloat64(f.FMin), formatFloat64(f.FMax)
			if kind == tkF32 {
				lo, hi = formatFloat32(f.FMin), formatFloat32(f.FMax)
			}
			g.pf("%s\tif v < %s { v = %s; r.Report.Clamped++ } else if v > %s { v = %s; r.Report.Clamped++ }\n", ind, lo, lo, hi, hi)
		}
		g.pf("%s\t%s = v\n%s}\n", ind, expr, ind)
	default:
		signed := f.Type.Kind == ir.TInt && f.Type.Signed
		method := "Unsigned"
		if signed {
			method = "Signed"
		}
		g.pf("%s{\n%s\tv := %s.%s(%s)\n", ind, ind, rdr, method, wireKind)
		if f.HasIntRange {
			lo, hi := goIntLit(f.IntMin, signed, 8), goIntLit(f.IntMax, signed, 8)
			g.pf("%s\tif v < %s { v = %s; r.Report.Clamped++ } else if v > %s { v = %s; r.Report.Clamped++ }\n", ind, lo, lo, hi, hi)
		}
		if f.Type.Kind == ir.TBits && f.Type.Width < 64 {
			max := uint64(1)<<f.Type.Width - 1
			g.pf("%s\tif v > %d { v = %d; r.Report.Clamped++ }\n", ind, max, max)
		}
		typ := goFieldType(f.Type)
		if f.Type.Kind == ir.TBytes {
			typ = "byte"
		}
		g.pf("%s\t%s = %s(v)\n%s}\n", ind, expr, typ, ind)
	}
}
