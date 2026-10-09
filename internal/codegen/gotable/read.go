package gotable

import (
	"fmt"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func (g *tableGen) emitTableRead(st *ir.Struct) {
	n := st.Name
	g.pf("func %sLoadBody(r *TableReader, value *%s) bool {\n", n, g.storageName(n))
	g.pf("\t%sReset(value)\n", n)
	if g.retain {
		g.pf("r.Retain.discard(r.Path,false,0)\n")
	}
	g.pf("\tfor {\n\t\tref, ok := r.lebPair(); if !ok { ref, ok = r.lebWide() }; if !ok { r.Report.Malformed = true; return false }\n")
	g.pf("\t\tif ref == 0 { return true }\n")
	g.pf("\t\tfieldID, ok := r.Resolve(ref); if !ok || !r.Has(1) { r.Report.Malformed = true; return false }\n")
	g.pf("\t\tif fieldID == 0xfffffffffffffffe || fieldID == 0xfffffffffffffffd || r.Nested && fieldID == 0xffffffffffffffff { r.Report.Malformed = true; return false }\n")
	g.pf("\t\tkind := r.Get8()\n\t\tswitch fieldID {\n")
	for ordinal, f := range st.Fields {
		g.retainOrdinal = ordinal
		kind := ir.TableWireFieldKind(f)
		g.pf("\t\tcase 0x%016x: // %s\n", ir.TableFieldWireId(f), f.Name)
		g.pf("\t\t\tif kind != %d {\n", kind)
		if wireWidenable(kind) {
			// Widening lives in the mismatch arm so a matching kind reads a constant width (SPEC-TABLES §4).
			g.pf("\t\t\t\tif tableKindWidens(kind, %d) {\n", kind)
			g.emitReadScalar(f, "value."+member(f), "r", "kind", "\t\t\t\t\t", "r.Report.Malformed = true; return false")
			if !st.IsMapEntry() || f.Name != ir.MapKeyFieldName {
				g.pf("\t\t\t\t\tr.Report.Widened++\n")
			}
			if f.Type.Optional {
				g.pf("\t\t\t\t\tvalue.%sPresent = true\n", member(f))
			}
			g.pf("\t\t\t\t\tbreak\n")
			g.pf("\t\t\t\t}\n")
		}
		g.pf("\t\t\t\tr.Report.KindMismatch++; if !r.Skip(kind) { r.Report.Malformed = true; return false }; break\n")
		g.pf("\t\t\t}\n")
		g.emitReadField(f, "\t\t\t")
		if f.Type.Optional {
			g.pf("\t\t\tvalue.%sPresent = true\n", member(f))
		}
	}
	// THE NODE TABLE'S RESERVED ID, AND ONLY WHERE IT IS THIS BODY'S OWN
	// TRANSPORT (docs/SPEC-TABLES.md §3.1). A VARIABLE root's body carries the
	// numbering under `0xFFFFFFFFFFFFFFFF`, and this loop runs over that body
	// after the graph path has already read it, so the field is stepped over in
	// silence — it is not an unknown FIELD, it is this reader's own transport.
	//
	// A FIXED-CLASS table gets NO such arm even in a regional unit, because it
	// has no numbering: for it the reserved id is an id it cannot name, and §3.1
	// says exactly what that costs — "a reader that cannot name the id skips the
	// field by its `L` and counts it unknown, once (§4). No new skip rule." The
	// arm used to ride on every table of a regional unit, which made this leg
	// answer `unknown=0` where the C++ reference and the compiler's own reader
	// both answer `unknown=1`; the wire fuzzer found it the day a fixed root
	// landed in a regional unit (#823).
	if g.regional && ir.VariableTables(g.unit)[st.Name] {
		g.pf("case 0xffffffffffffffff: if !r.Skip(kind) { r.Report.Malformed=true;return false }\n")
	}
	if g.retain {
		g.pf("default:r.Report.Unknown++;if !r.capture(fieldID,kind){r.Report.Malformed=true;return false}\n}\n}\n}\n")
	} else {
		g.pf("\t\tdefault:\n\t\t\tr.Report.Unknown++; if !r.Skip(kind) { r.Report.Malformed = true; return false }\n\t\t}\n\t}\n}\n\n")
	}
	if g.retain || st.IsMapEntry() || g.regional && ir.VariableTables(g.unit)[st.Name] {
		return
	}
	if g.hasFixedForm(st) {
		// IT CARRIES BOTH FORMS AND EACH ENTRY POINT CARRIES ONE (§3.4, §15).
		// <T>FixedLoad reads form 3 and names a form-1 file `previous_form`;
		// this <T>Load reads form 1 and names a form-3 file `newer_form`. The
		// `fixed table` keyword (#823) decides what a WRITER emits, not which
		// bytes a reader of the variable form can still be handed: the shared
		// corpus pins `v1_cfg_as_v2` at `read` over a form-1 file of a DECLARED
		// fixed root, the C++ reference reads it there (cpptable/fixedform.go's
		// header: "a fixed-table type's reader accepts both, by the form byte"),
		// and a leg that refused it alone would be the one leg disagreeing with
		// the reference. The dispatch §15 names as the follow-on is what would
		// let one entry point answer both; it is not this one refusing.
		g.pf("// %s also carries the FIXED form (form 3): %sFixedLoad reads that one and\n", n, n)
		g.pf("// names a form-1 file `previous_form`. This Load is the VARIABLE form's entry\n")
		g.pf("// point and reads form 1, whether or not the table was DECLARED fixed\n")
		g.pf("// (docs/SPEC-TABLES.md §3.4, §15).\n")
	}
	g.pf("func %sLoad(value *%s, data []byte, report *TableReport) bool {\n", n, g.storageName(n))
	g.pf("\tif report == nil { var ignored TableReport; report = &ignored }\n")
	g.pf("\tr, verdict := tableOpen(data, report); report.Verdict = verdict; report.Reason = \"\"\n")
	g.pf("\tif verdict == TableOpenRefused { report.Reason = \"unsupported wire form\"; if len(data) > 0 && data[0] == 2 { report.Reason = \"message form requires an announced vocabulary and a message reader\" } }\n")
	g.pf("\tif verdict != TableOpenOk { %sReset(value); if verdict == TableOpenDamaged { report.Malformed = true }; return false }\n", n)
	g.pf("\t// The root read answers its own early end after the walk, not before it.\n")
	g.pf("\t// A body that returned at its zero reference left the cursor on the byte\n")
	g.pf("\t// after that reference. Every field leaves the cursor where skip would, so\n")
	g.pf("\t// r.Offset != len(r.Buffer) is the old EndsEarly on the true path.\n")
	g.pf("\t// Nothing is decoded on the damaged path: the value goes back to defaults\n")
	g.pf("\t// and the report goes back to what the caller handed in, so an early end\n")
	g.pf("\t// counts no unknown, no kind mismatch and no clamp — as when the framing\n")
	g.pf("\t// walk refused before the reader had seen one byte.\n")
	g.pf("\tbefore := *report\n")
	g.pf("\tif !%sLoadBody(&r, value) {\n", n)
	g.pf("\t\t// The reading walk stops on rules the framing walk has no opinion about\n")
	g.pf("\t\t// — a reserved id in a file body is the one that matters — so a body\n")
	g.pf("\t\t// that stopped is still asked the framing question from the start of\n")
	g.pf("\t\t// the body, and a body whose framing ends early is damage whatever else\n")
	g.pf("\t\t// was wrong with it.\n")
	g.pf("\t\tprobe := r\n")
	g.pf("\t\tprobe.Offset = 0\n")
	g.pf("\t\tif probe.EndsEarly() {\n")
	g.pf("\t\t\t%sReset(value)\n", n)
	g.pf("\t\t\t*report = before\n")
	g.pf("\t\t\treport.Malformed = true\n")
	g.pf("\t\t\treport.Verdict = TableOpenDamaged\n")
	g.pf("\t\t\treturn false\n")
	g.pf("\t\t}\n")
	g.pf("\t\treport.Verdict = TableOpenBodyStopped\n")
	g.pf("\t\treturn false\n")
	g.pf("\t}\n")
	g.pf("\tif r.Offset != int64(len(r.Buffer)) {\n")
	g.pf("\t\t%sReset(value)\n", n)
	g.pf("\t\t*report = before\n")
	g.pf("\t\treport.Malformed = true\n")
	g.pf("\t\treport.Verdict = TableOpenDamaged\n")
	g.pf("\t\treturn false\n")
	g.pf("\t}\n")
	g.pf("\treturn true\n}\n\n")
}

func wireWidenable(kind int) bool {
	return kind >= 3 && kind <= 5 || kind >= 7 && kind <= 9 || kind == tkF64 || kind == 18 || kind == 19
}

func (g *tableGen) emitReadField(f *ir.Field, ind string) {
	expr := "value." + member(f)
	kind := ir.TableWireScalarKind(f)
	matched := fmt.Sprintf("%d", kind)
	switch {
	case f.IsMap():
		g.emitMapRead(f, ind)
	case f.IsList():
		g.emitListRead(f, ind)
	case f.KeyEnum != "":
		g.emitReadArray(f, ind, true)
	case f.Array != ir.ArrayNone || f.Type.Kind == ir.TBytes && !f.Type.Pointer:
		g.emitReadArray(f, ind, false)
	case f.Type.Pointer:
		g.emitReadScalar(f, expr, "r", matched, ind, "r.Report.Malformed=true;return false")
	case f.Type.Kind == ir.TWString:
		g.pf("%ssub,ok:=r.Body();if !ok {r.Report.Malformed=true;return false}\n", ind)
		g.emitReadWString(f, expr, "sub", ind, "r.Report.Malformed=true;break")
	case f.Type.Kind == ir.TString:
		g.pf("%ssub, ok := r.Body(); if !ok { r.Report.Malformed = true; return false }\n", ind)
		g.pf("%sif !tableUtf8Valid(sub.Buffer) { r.Report.Malformed = true;\n", ind)
		g.emitTableResetField(f)
		g.pf("break }\n")
		g.pf("%skeep := int64(len(sub.Buffer)); if keep > %d { keep = tableUtf8Clamp(sub.Buffer, %d); r.Report.Clamped++ }\n", ind, f.Type.Size, f.Type.Size)
		g.pf("%sclear(%s[:]); copy(%s[:keep], sub.Buffer); %sLength = int32(keep)\n", ind, expr, expr, expr)
	case kind == tkTable:
		g.pf("%ssub, ok := r.Body(); if !ok { r.Report.Malformed = true; return false }\n", ind)
		g.emitReadNested(f.Type.Name, expr, "sub", ind)
	case kind == tkUnion:
		g.retainDiscard("r")
		g.emitReadUnion(f.Type.Ref.(*ir.Union), expr, "r", ind, "return false")
	default:
		g.emitReadScalar(f, expr, "r", matched, ind, "r.Report.Malformed = true; return false")
	}
}

func (g *tableGen) emitReadNested(typ, expr, rdr, ind string) {
	g.retainReadPath(rdr, "r", "0")
	g.pf("%s%sLoadBody(&%s, &%s)\n", ind, typ, rdr, expr)
	g.emitCarveReturn("r", rdr, ind)
	g.pf("%sif %s.Offset != int64(len(%s.Buffer)) { r.Report.Malformed = true; %sReset(&%s) }\n", ind, rdr, rdr, typ, expr)
}

func (g *tableGen) emitReadArray(f *ir.Field, ind string, keyed bool) {
	expr := "value." + member(f)
	kind, bound := ir.TableWireScalarKind(f), f.ArrayBound
	if f.Type.Kind == ir.TBytes && !f.Type.Pointer {
		kind, bound = tkU8, f.Type.Size
	}
	g.pf("%ssub, ok := r.Body(); if !ok { r.Report.Malformed = true; return false }\n", ind)
	g.pf("%sif len(sub.Buffer) >= 2 {\n", ind)
	i := ind + "\t"
	// The reference reads the element kind and count from the enclosing
	// reader before bounding the elements by L. Preserve its report events
	// when a damaged count extends beyond L, while Has still bounds every
	// element read to the declared body.
	g.pf("%sheader := sub; header.Buffer = r.Buffer[r.Offset-int64(len(sub.Buffer)):]\n", i)
	g.pf("%selemKind := header.Get8(); count, ok := header.Leb(); sub.Offset = header.Offset\n", i)
	g.pf("%sif !ok { r.Report.Malformed = true } else {\n", i)
	i += "\t"
	g.pf("%sif elemKind != %d {\n", i, kind)
	if wireWidenable(kind) {
		g.pf("%s\tif tableKindWidens(elemKind, %d) { r.Report.Widened++ } else { r.Report.KindMismatch++; break }\n", i, kind)
	} else {
		g.pf("%s\tr.Report.KindMismatch++; break\n", i)
	}
	g.pf("%s}\n", i)
	if !keyed {
		g.retainDiscard("r")
	}
	if keyed {
		g.pf("%sfor i := uint64(0); i < count; i++ {\n", i)
		j := i + "\t"
		g.pf("%skeyRef, ok := sub.Leb(); if !ok { r.Report.Malformed = true; break }\n", j)
		g.pf("%skey, ok := sub.Resolve(keyRef); if !ok { r.Report.Malformed = true; break }\n", j)
		g.pf("%selem, ok := sub.Body(); if !ok { r.Report.Malformed = true; break }\n", j)
		g.pf("%svar slot %s; if !slot.TableEnumValue(key) { r.Report.Unknown++;", j, f.KeyEnum)
		if g.retain {
			g.pf("if r.Retain!=nil{r.Report.RetainLost++};")
		}
		g.pf("continue }\n")
		g.retainReadPath("elem", "r", "int(slot)-1")
		switch kind {
		case tkTable:
			g.pf("%s%sLoadBody(&elem, &%s[int(slot)-1])\n", j, f.Type.Name, expr)
			g.emitCarveReturn("sub", "elem", j)
			g.pf("%sif elem.Offset!=int64(len(elem.Buffer)) { r.Report.Malformed=true;%sReset(&%s[int(slot)-1]) }\n", j, f.Type.Name, expr)
		case tkUnion:
			g.emitReadUnion(f.Type.Ref.(*ir.Union), expr+"[int(slot)-1]", "elem", j, "continue", true)
			g.emitCarveReturn("sub", "elem", j)
		default:
			g.emitReadScalar(f, expr+"[int(slot)-1]", "elem", "elemKind", j, "r.Report.Malformed = true; continue")
		}
		g.pf("%s}\n", i)
	} else {
		g.pf("%skeep := count; if keep > %d { keep = %d; r.Report.Clamped++ }\n", i, bound, bound)
		counted := f.Array == ir.ArrayCounted || f.Type.Kind == ir.TBytes
		if counted {
			companion := expr + "Count"
			if f.Type.Kind == ir.TBytes {
				companion = expr + "Length"
			}
			g.pf("%sprevious := %s; decoded := int32(0)\n", i, companion)
		}
		g.pf("%sfor i := uint64(0); i < keep; i++ {\n", i)
		j := i + "\t"
		switch kind {
		case tkTable:
			g.pf("%selem, ok := sub.Body(); if !ok { r.Report.Malformed = true; break }\n", j)
			g.retainReadPath("elem", "r", "i")
			g.pf("%s%sLoadBody(&elem, &%s[i])\n", j, f.Type.Name, expr)
			g.emitCarveReturn("sub", "elem", j)
			g.pf("%sif elem.Offset!=int64(len(elem.Buffer)) {r.Report.Malformed=true;%sReset(&%s[i])}\n", j, f.Type.Name, expr)
		case tkUnion:
			g.retainReadPath("sub", "r", "i")
			g.emitReadUnion(f.Type.Ref.(*ir.Union), expr+"[i]", "sub", j, "break", true)
		default:
			g.emitReadScalar(f, expr+"[i]", "sub", "elemKind", j, "r.Report.Malformed = true; break")
		}
		if counted {
			g.pf("%sdecoded = int32(i+1)\n", j)
		}
		g.pf("%s}\n", i)
		if counted {
			// Entry reset initializes the unused tail. Only previously live
			// slots need resetting after an accepted shorter replacement.
			switch {
			case !f.Type.Pointer && isStructRef(f.Type):
				g.pf("%sfor i:=decoded;i<previous;i++{%sReset(&%s[i])}\n", i, f.Type.Name, expr)
			case !f.Type.Pointer && isUnionRef(f.Type):
				g.pf("%sfor i:=decoded;i<previous;i++{%s[i].Type=%sTypeNone}\n", i, expr, f.Type.Name)
			default:
				g.pf("%sif decoded<previous{clear(%s[decoded:previous])}\n", i, expr)
			}
		}
		if f.Type.Kind == ir.TBytes && !f.Type.Pointer {
			g.pf("%s%sLength = decoded\n", i, expr)
		} else if counted {
			g.pf("%s%sCount = decoded\n", i, expr)
		}
	}
	g.pf("%s\t}\n%s}\n", ind, ind)
	g.emitCarveReturn("r", "sub", ind)
}

func (g *tableGen) emitReadUnion(un *ir.Union, expr, rdr, ind, stop string, element ...bool) {
	arm := g.nextWireWriter()
	g.pf("%s{\n", ind)
	i := ind + "\t"
	// Capture the destination before nested arm loops introduce their own indices.
	target := g.nextWireWriter()
	g.pf("%s%s := &%s\n", i, target, expr)
	expr = target
	g.pf("%sarmRef, ok := %s.Leb(); if !ok { r.Report.Malformed = true; %s }\n", i, rdr, stop)
	if len(element) > 0 && element[0] {
		g.pf("%s%s.Type = %sTypeNone\n", i, expr, un.Name)
	}
	g.pf("%sif armRef == 0 { %s.Type = %sTypeNone } else {\n", i, expr, un.Name)
	i += "\t"
	g.pf("%sarmID, ok := %s.Resolve(armRef); if !ok || !%s.Has(1) { r.Report.Malformed = true; %s }\n", i, rdr, rdr, stop)
	g.pf("%sarmKind := %s.Get8(); %s, ok := %s.Body(); if !ok { r.Report.Malformed = true; %s }\n", i, rdr, arm, rdr, stop)
	g.pf("%s%s.Type = %sTypeNone\n%sswitch armID {\n", i, expr, un.Name, i)
	for ordinal, v := range un.Variants {
		g.pf("%scase 0x%016x:\n", i, ir.TableWireId(v.WireName()))
		j := i + "\t"
		kind := ir.TableKindNoPayload
		if !v.Void() {
			kind = ir.TableWireFieldKind(v.F)
		}
		g.pf("%sif armKind != %d {\n", j, kind)
		if wireWidenable(kind) {
			g.pf("%s if tableKindWidens(armKind,%d) { if int64(len(%s.Buffer)) != tableKindBytes(armKind) { r.Report.Malformed=true; break }; r.Report.Widened++ } else { r.Report.KindMismatch++; break }\n", j, kind, arm)
		} else {
			g.pf("%s r.Report.KindMismatch++; break\n", j)
		}
		g.pf("%s}\n", j)
		none := fmt.Sprintf("%s.Type = %sTypeNone", expr, un.Name)
		g.pf("%s%s.Type = %sType%s\n", j, expr, un.Name, ir.GoExportName(v.Name))
		g.retainReadPath(arm, rdr, fmt.Sprint(ordinal+1))
		g.emitReadArm(v, g.unionArmExpr(un, v, expr), arm, "armKind", j, none)
	}
	g.pf("%sdefault:r.Report.Unknown++\n", i)
	if g.retain {
		g.pf("if r.Retain!=nil{r.Report.RetainLost++}\n")
	}
	g.pf("%s}\n", i)
	g.emitCarveReturn(rdr, arm, i)
	g.pf("%s}\n%s}\n", ind+"\t", ind)
}

func (g *tableGen) emitReadArm(v ir.UnionVariant, expr, rdr, kind, ind, none string) {
	bad := none + "; r.Report.Malformed = true; break"
	if v.Void() {
		g.pf("%sif len(%s.Buffer) != 0 { %s }\n", ind, rdr, bad)
		return
	}
	f := v.F
	if v.Body() && !v.F.Type.Pointer {
		g.pf("%s%sLoadBody(&%s,&%s)\n", ind, v.Type, rdr, expr)
		g.emitCarveReturn("r", rdr, ind)
		g.pf("%sif %s.Offset != int64(len(%s.Buffer)) { %s }\n", ind, rdr, rdr, bad)
		return
	}
	if f.Array != ir.ArrayNone || f.Type.Kind == ir.TBytes && !f.Type.Pointer {
		g.emitReadArmArray(f, expr, rdr, ind, none)
		return
	}
	if f.Type.Pointer || enumRef(f) != nil {
		// An arm's complete reference framing precedes resolution/reporting.
		g.pf("%s{framing:=%s;_,ok:=framing.Leb();if !ok||framing.Offset!=int64(len(framing.Buffer)){%s}}\n", ind, rdr, bad)
	}
	if f.Type.Pointer {
		g.emitReadScalar(f, expr, rdr, kind, ind, bad)
		g.pf("%sif %s.Offset!=int64(len(%s.Buffer)){%s}\n", ind, rdr, rdr, bad)
		return
	}
	if f.Type.Kind == ir.TWString {
		g.emitReadWString(f, expr, rdr, ind, bad)
		return
	}
	if f.Type.Kind == ir.TString {
		g.pf("%sclear(%s[:]); %sLength = 0\n", ind, expr, expr)
		g.pf("%sif !tableUtf8Valid(%s.Buffer) { %s }\n", ind, rdr, bad)
		g.pf("%skeep := int64(len(%s.Buffer)); if keep > %d { keep = tableUtf8Clamp(%s.Buffer,%d); r.Report.Clamped++ }; copy(%s[:],%s.Buffer[:keep]); %sLength=int32(keep)\n", ind, rdr, f.Type.Size, rdr, f.Type.Size, expr, rdr, expr)
		return
	}
	if isUnionRef(f.Type) {
		g.pf("%s%s.Type = %sTypeNone\n", ind, expr, f.Type.Name)
		g.emitReadUnion(f.Type.Ref.(*ir.Union), expr, rdr, ind, bad)
		return
	}
	if ir.TableKindWidth(ir.TableWireScalarKind(f)) > 0 {
		g.pf("%sif int64(len(%s.Buffer)) != tableKindBytes(%s) { %s }\n", ind, rdr, kind, bad)
	}
	g.emitReadScalar(f, expr, rdr, kind, ind, bad)
	if enumRef(f) != nil {
		g.pf("%sif %s.Offset != int64(len(%s.Buffer)) { %s }\n", ind, rdr, rdr, bad)
	}
}

// Array arms have a bare array body bounded by the arm's L. A short header
// is inert; a damaged count or mismatched element kind deselects the arm.
func (g *tableGen) emitReadArmArray(f *ir.Field, expr, rdr, ind, none string) {
	kind, bound := ir.TableWireScalarKind(f), f.ArrayBound
	counted := f.Array == ir.ArrayCounted || f.Type.Kind == ir.TBytes
	companion := expr + "Count"
	if f.Type.Kind == ir.TBytes && !f.Type.Pointer {
		kind, bound = tkU8, f.Type.Size
		companion = expr + "Length"
	}
	g.pf("%sclear(%s[:])\n", ind, expr)
	if counted {
		g.pf("%s%s=0\n", ind, companion)
	}
	g.pf("%sif !%s.Has(2) { break }\n", ind, rdr)
	g.pf("%selemKind := %s.Get8(); count,ok := %s.Leb(); if !ok { %s; r.Report.Malformed=true; break }\n", ind, rdr, rdr, none)
	g.pf("%sif elemKind != %d {\n", ind, kind)
	if wireWidenable(kind) {
		g.pf("%s if tableKindWidens(elemKind,%d) { r.Report.Widened++ } else { %s; r.Report.KindMismatch++; break }\n", ind, kind, none)
	} else {
		g.pf("%s %s; r.Report.KindMismatch++; break\n", ind, none)
	}
	g.pf("%s}\n%skeep:=count;if keep > %d {keep=%d;r.Report.Clamped++}\n", ind, ind, bound, bound)
	g.pf("%sfor i:=uint64(0); i<keep; i++ {\n", ind)
	j := ind + "\t"
	switch {
	case isStructRef(f.Type) && !f.Type.Pointer:
		elem := g.nextWireWriter()
		g.pf("%s%s,ok:=%s.Body();if !ok {r.Report.Malformed=true;break}\n", j, elem, rdr)
		g.retainReadPath(elem, rdr, "i")
		g.pf("%s%sLoadBody(&%s,&%s[i])\n", j, f.Type.Name, elem, expr)
		g.emitCarveReturn(rdr, elem, j)
	case isUnionRef(f.Type) && !f.Type.Pointer:
		if g.retain {
			g.pf("savedPath:=%s.Path; %s.Path=%s.Path.step(%d,uint32(i))\n", rdr, rdr, rdr, g.retainOrdinal)
		}
		g.emitReadUnion(f.Type.Ref.(*ir.Union), expr+"[i]", rdr, j, "break", true)
		if g.retain {
			g.pf("%s.Path=savedPath\n", rdr)
		}
	default:
		g.emitReadScalar(f, expr+"[i]", rdr, "elemKind", j, "r.Report.Malformed=true;break")
	}
	if counted {
		g.pf("%s%s=int32(i+1)\n", j, companion)
	}
	g.pf("%s}\n", ind)
}

func (g *tableGen) emitReadScalar(f *ir.Field, expr, rdr, wireKind, ind, onBad string) {
	if f.Type.Pointer {
		g.pf("%s{index,ok:=%s.Leb();if !ok {%s};%s.Nodes.Resolve(&%s,index,0x%016x,r.Report)}\n", ind, rdr, onBad, rdr, expr, pointerTargetId(f))
		return
	}
	kind := ir.TableWireScalarKind(f)
	if f.Type.Kind == ir.TBytes && !f.Type.Pointer {
		kind = tkU8
	}
	if enumRef(f) != nil {
		g.pf("%s{\n%s\tvariantRef, ok := %s.Leb(); if !ok { %s }\n", ind, ind, rdr, onBad)
		g.pf("%s\tif variantRef == 0 { %s = %sNone } else {\n", ind, expr, f.Type.Name)
		g.pf("%s\t\tid, ok := %s.Resolve(variantRef); if !ok { %s }\n", ind, rdr, onBad)
		g.pf("%s\t\tif !%s.TableEnumValue(id) { r.Report.Unknown++;", ind, expr)
		if g.retain {
			g.pf("if r.Retain!=nil{r.Report.RetainLost++}")
		}
		g.pf(" }\n%s\t}\n%s}\n", ind, ind)
		return
	}
	if f.Type.Width == 128 && (f.Type.Kind == ir.TInt || f.Type.Kind == ir.TFixed) {
		g.emitReadWide(f, expr, rdr, wireKind, ind, onBad)
		return
	}
	// A compile-time kind is Has(N)+GetN; a runtime kind keeps Unsigned/Signed.
	if n := knownKindWidth(wireKind); n > 0 {
		g.pf("%sif !%s.Has(%d) { %s }\n", ind, rdr, n, onBad)
	} else {
		g.pf("%sif !%s.Has(tableKindBytes(%s)) { %s }\n", ind, rdr, wireKind, onBad)
	}
	switch kind {
	case tkBool:
		g.pf("%s%s = %s.Get8() != 0\n", ind, expr, rdr)
	case tkF32, tkF64:
		g.needsMath = true
		switch {
		case kind == tkF32:
			g.pf("%s{\n%s\tv := math.Float32frombits(%s.Get32())\n", ind, ind, rdr)
		case knownKindWidth(wireKind) > 0:
			g.pf("%s{\n%s\tv := math.Float64frombits(%s.Get64())\n", ind, ind, rdr)
		default:
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
		signed := (f.Type.Kind == ir.TInt || f.Type.Kind == ir.TFixed) && f.Type.Signed
		g.pf("%s{\n%s\tv := %s\n", ind, ind, scalarGetExpr(rdr, wireKind, signed))
		if f.HasIntRange {
			rlo, rhi, _ := ir.TableRawRange(f)
			lo, hi := goIntLit(rlo, signed, 8), goIntLit(rhi, signed, 8)
			g.pf("%s\tif v < %s { v = %s; r.Report.Clamped++ } else if v > %s { v = %s; r.Report.Clamped++ }\n", ind, lo, lo, hi, hi)
		}
		if f.Type.Kind == ir.TBits && f.Type.Width < 64 {
			max := uint64(1)<<f.Type.Width - 1
			g.pf("%s\tif v > %d { v = %d; r.Report.Clamped++ }\n", ind, max, max)
		}
		typ := goFieldType(f.Type)
		if f.Type.Kind == ir.TBytes && !f.Type.Pointer {
			typ = "byte"
		}
		g.pf("%s\t%s = %s(v)\n%s}\n", ind, expr, typ, ind)
	}
}

func knownKindWidth(wireKind string) int {
	if wireKind == "" {
		return 0
	}
	n := 0
	for i := 0; i < len(wireKind); i++ {
		c := wireKind[i]
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return ir.TableKindWidth(n)
}

func scalarGetExpr(rdr, wireKind string, signed bool) string {
	n := knownKindWidth(wireKind)
	if n != 1 && n != 2 && n != 4 && n != 8 {
		method := "Unsigned"
		if signed {
			method = "Signed"
		}
		return fmt.Sprintf("%s.%s(%s)", rdr, method, wireKind)
	}
	get := fmt.Sprintf("%s.Get%d()", rdr, n*8)
	if !signed {
		return get
	}
	switch n {
	case 1:
		return "int8(" + get + ")"
	case 2:
		return "int16(" + get + ")"
	case 4:
		return "int32(" + get + ")"
	default:
		return "int64(" + get + ")"
	}
}

func (g *tableGen) emitCarveReturn(parent, child, ind string) {
	if unitHasContainers(g.unit) {
		g.pf("%s%s.Take(%s);if %s.Nodes.Carve.Refused { ", ind, parent, child, parent)
		if parent != "r" {
			g.pf("r.Nodes.Carve=%s.Nodes.Carve;", parent)
		}
		g.pf("return false }\n")
	}
}
