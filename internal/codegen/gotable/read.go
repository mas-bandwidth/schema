package gotable

import (
	"fmt"
	"slices"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// damaged is the emitted refusal for damage a decoder meets (docs/SPEC-TABLES.md
// §4): the open's wire check has already refused every input the wire alone
// shows to be wrong, so this is the decoder's own guard and never a path to a
// value.
const damaged = `r.Report.Refuse(TableRefuseWireDamaged,"")`

// refuse is the emitted refusal of one declaration condition, naming the
// field, and the stop after it.
func refuse(reason, field, stop string) string {
	return fmt.Sprintf("r.Report.Refuse(%s,%q);%s", reason, field, stop)
}

func (g *tableGen) emitTableRead(st *ir.Struct) {
	n := st.Name
	g.pf("func %sLoadBody(r *TableReader, value *%s) bool {\n", n, g.storageName(n))
	g.pf("\t%sReset(value)\n", n)
	if g.retain {
		g.pf("r.Retain.discard(r.Path,false,0)\n")
	}
	// `| required` fields (docs/SPEC-TABLES.md §2.3): each gets a seen flag,
	// set when its id rides, and a record that ends without one refuses
	// missing_required naming the field. A required field inside an `if`
	// guard is demanded only while its guard holds — with the branch off it is
	// absent and no refusal is owed (§2.3, §3).
	guards := tableGuardExprs(st)
	var req []*ir.Field
	var reqGuard []string
	for _, f := range st.Fields {
		if f.Required {
			req = append(req, f)
			reqGuard = append(reqGuard, guards[f.Name])
		}
	}
	reqIdx := map[*ir.Field]int{}
	for i, f := range req {
		reqIdx[f] = i
		g.pf("\treqSeen%d := false\n", i)
	}
	g.pf("\tfor {\n\t\tref, ok := r.lebPair(); if !ok { ref, ok = r.lebWide() }; if !ok { %s; return false }\n", damaged)
	if len(req) > 0 {
		g.pf("\t\tif ref == 0 {\n")
		parts := make([]string, len(req))
		for i := range req {
			if reqGuard[i] != "" {
				parts[i] = fmt.Sprintf("(!reqSeen%d) && (%s)", i, reqGuard[i])
			} else {
				parts[i] = fmt.Sprintf("!reqSeen%d", i)
			}
		}
		g.pf("\t\t\tif %s {\n", strings.Join(parts, " || "))
		g.pf("\t\t\t\tmissingReq := \"\"\n")
		for i, n := range requiredFieldNames(req) {
			if reqGuard[i] != "" {
				g.pf("\t\t\t\tif !reqSeen%d && (%s) { if missingReq != \"\" { missingReq += \", \" }; missingReq += %q }\n", i, reqGuard[i], n)
			} else {
				g.pf("\t\t\t\tif !reqSeen%d { if missingReq != \"\" { missingReq += \", \" }; missingReq += %q }\n", i, n)
			}
		}
		g.pf("\t\t\t\tr.Report.Refuse(TableRefuseMissingRequired, missingReq)\n")
		g.pf("\t\t\t\treturn false\n")
		g.pf("\t\t\t}\n")
		g.pf("\t\t\treturn true\n")
		g.pf("\t\t}\n")
	} else {
		g.pf("\t\tif ref == 0 { return true }\n")
	}
	g.pf("\t\tfieldID, ok := r.Resolve(ref); if !ok || !r.Has(1) { %s; return false }\n", damaged)
	// A reserved id in a body that is not its transport is damage
	// (docs/SPEC-TABLES.md §3.1, §3.3).
	g.pf("\t\tkind := r.Get8()\n")
	g.pf("\t\tif fieldID == 0xfffffffffffffffe || fieldID == 0xfffffffffffffffd || r.Nested && fieldID == 0xffffffffffffffff { %s; return false }\n", damaged)
	g.pf("\t\tswitch fieldID {\n")
	for ordinal, f := range st.Fields {
		g.retainOrdinal = ordinal
		kind := ir.TableWireFieldKind(f)
		g.pf("\t\tcase 0x%016x: // %s\n", ir.TableFieldWireId(f), f.Name)
		g.pf("\t\t\tif kind != %d {\n", kind)
		if wireWidenable(kind) {
			// Widening lives in the mismatch arm so a matching kind reads a
			// constant width (docs/SPEC-TABLES.md §4).
			g.pf("\t\t\t\tif tableKindWidens(kind, %d) {\n", kind)
			g.emitReadScalar(f, "value."+member(f), "r", "kind", "\t\t\t\t\t", damaged+"; return false")
			if !st.IsMapEntry() || f.Name != ir.MapKeyFieldName {
				g.pf("\t\t\t\t\tr.Report.Widened++\n")
			}
			if f.Type.Optional {
				g.pf("\t\t\t\t\tvalue.%sPresent = true\n", member(f))
			}
			if idx, ok := reqIdx[f]; ok {
				g.pf("\t\t\t\t\treqSeen%d = true\n", idx)
			}
			g.pf("\t\t\t\t\tbreak\n")
			g.pf("\t\t\t\t}\n")
		}
		// A KIND THAT IS NOT THE DECLARED ONE AND NOT A WIDENING REFUSES the
		// load (docs/SPEC-TABLES.md §4); kinds 31 and 32 take the same rule.
		g.pf("\t\t\t\t%s\n", refuse("TableRefuseKindMismatch", g.floatOwner(f), "return false"))
		g.pf("\t\t\t}\n")
		g.emitReadField(f, "\t\t\t")
		if f.Type.Optional {
			g.pf("\t\t\tvalue.%sPresent = true\n", member(f))
		}
		if idx, ok := reqIdx[f]; ok {
			g.pf("\t\t\treqSeen%d = true\n", idx)
		}
	}
	// THE NODE TABLE'S RESERVED ID, where it is this body's own transport
	// (docs/SPEC-TABLES.md §3.1): a VARIABLE root's body carries the numbering
	// under 0xFFFFFFFFFFFFFFFF, read by the graph path before this loop, so the
	// field is stepped over here. A FIXED-CLASS table has no numbering: for it
	// the reserved id is an id it cannot name, skipped and counted unknown.
	if g.regional && ir.VariableTables(g.unit)[st.Name] {
		g.pf("case 0xffffffffffffffff: if !r.Skip(kind) { %s;return false }\n", damaged)
	}
	if g.retain {
		g.pf("default:r.Report.Unknown++;if !r.capture(fieldID,kind,%q){%s;return false}\n}\n}\n}\n", st.DeclaredName(), damaged)
	} else {
		g.pf("\t\tdefault:\n\t\t\tr.Report.Unknown++; if !r.Skip(kind) { %s; return false }\n\t\t}\n\t}\n}\n\n", damaged)
	}
	if g.retain || st.IsMapEntry() || g.regional && ir.VariableTables(g.unit)[st.Name] {
		return
	}
	if g.hasFixedForm(st) {
		g.pf("// %s also carries the FIXED form (form 3): %sFixedLoad reads that one and\n", n, n)
		g.pf("// names a form-1 file `previous_form`. This Load is the VARIABLE form's entry\n")
		g.pf("// point and reads form 1 (docs/SPEC-TABLES.md §3.4, §15).\n")
	}
	// THE PLAIN NAME IS THE READ, AND IT IS THE ONLY READ (docs/SPEC-TABLES.md
	// §6.5): it returns the whole value or refuses the whole load.
	g.pf("// %sLoad is the read (docs/SPEC-TABLES.md §4, §6.5): it returns the whole value, or\n", n)
	g.pf("// refuses with value at its defaults and false. A supplied report receives this\n")
	g.pf("// read's report, and on a refusal report.Err() is the typed reason. The open\n")
	g.pf("// checks every condition the wire alone decides; the body read checks this\n")
	g.pf("// declaration's (tla/ReadOrder.tla, OneVerdict).\n")
	g.pf("func %sLoad(value *%s, data []byte, report *TableReport) bool {\n", n, g.storageName(n))
	g.pf("\tread := report\n\tif read == nil {\n\t\tread = &TableReport{}\n\t} else {\n\t\t*read = TableReport{}\n\t}\n")
	if st.Integrity != "" {
		g.pf("\tvar ok bool\n\tif data, ok = tableIntegrityOpen(data, read); !ok { %sReset(value); return false }\n", n)
	}
	g.pf("\tr, verdict := tableOpenChecked(data, read)\n")
	g.pf("\tif verdict != TableOpenOk || !%sLoadBody(&r, value) || r.Offset != int64(len(r.Buffer)) || read.Refused() {\n", n)
	g.pf("\t\ttableRefused(read)\n\t\t%sReset(value)\n\t\treturn false\n\t}\n\treturn true\n}\n\n", n)
	g.pf("// %sReadStrict is %sLoad under a second spelling (docs/SPEC-TABLES.md §6.5): the value, or the typed reason.\n", n, n)
	g.pf("func %sReadStrict(wire []byte)(*%s,error){value:=&%s{};var report TableReport;if !%sLoad(value,wire,&report){return nil,report.Err()};return value,nil}\n\n", n, g.storageName(n), g.storageName(n), n)
}

func wireWidenable(kind int) bool {
	return kind >= 3 && kind <= 5 || kind >= 7 && kind <= 9 || kind == tkF64 || kind == 18 || kind == 19
}

func (g *tableGen) emitReadField(f *ir.Field, ind string) {
	expr := "value." + member(f)
	kind := ir.TableWireScalarKind(f)
	matched := fmt.Sprintf("%d", kind)
	stop := damaged + ";return false"
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
		g.emitReadScalar(f, expr, "r", matched, ind, stop)
	case f.Type.Kind == ir.TWString:
		g.pf("%ssub,ok:=r.Body();if !ok {%s}\n", ind, stop)
		g.emitReadWString(f, expr, "sub", ind, stop)
	case f.Type.TextUnbounded():
		g.emitReadUnboundedText(f, expr, ind)
	case f.Type.Kind == ir.TString:
		// ill-formed text is damage, and a text past the reader's bound
		// refuses length_over_bound: nothing is cut (docs/SPEC-TABLES.md §4)
		g.pf("%ssub, ok := r.Body(); if !ok || !tableUTF8Valid(sub.Buffer) { %s }\n", ind, stop)
		field := g.textValueHolder(f)
		if field == "" {
			field = g.floatOwner(f)
		}
		g.pf("%sif len(sub.Buffer) > %d { %s }\n", ind, f.Type.Size, refuse("TableRefuseLengthOverBound", field, "return false"))
		g.pf("%sclear(%s[:]); copy(%s[:], sub.Buffer); %sLength = int32(len(sub.Buffer))\n", ind, expr, expr, expr)
	case kind == tkTable:
		g.pf("%ssub, ok := r.Body(); if !ok { %s }\n", ind, stop)
		g.emitReadNested(f.Type.Name, expr, "sub", ind)
	case kind == tkUnion:
		g.retainDiscard("r")
		g.emitReadUnion(f.Type.Ref.(*ir.Union), expr, "r", ind)
	default:
		g.emitReadScalar(f, expr, "r", matched, ind, stop)
	}
}

// emitReadNested reads one L-framed nested body: the body must read whole and
// its terminator must be the last byte of its L (docs/SPEC-TABLES.md §3, §4).
func (g *tableGen) emitReadNested(typ, expr, rdr, ind string) {
	g.retainReadPath(rdr, "r", "0")
	g.pf("%sif !%sLoadBody(&%s, &%s) { return false }\n", ind, typ, rdr, expr)
	g.emitCarveReturn("r", rdr, ind)
	g.pf("%sif %s.Offset != int64(len(%s.Buffer)) { %s; return false }\n", ind, rdr, rdr, damaged)
}

// emitReadArray reads an array or keyed body (docs/SPEC-TABLES.md §3, §3.2):
// its header lies inside its own L, a count past the reader's bound refuses
// count_over_bound (length_over_bound for bytes), an element kind that is not
// the declared one and not a widening refuses kind_mismatch, and every byte of
// the L is an element's.
func (g *tableGen) emitReadArray(f *ir.Field, ind string, keyed bool) {
	expr := "value." + member(f)
	kind, bound := ir.TableWireScalarKind(f), f.ArrayBound
	over := "TableRefuseCountOverBound"
	if f.Type.Kind == ir.TBytes && !f.Type.Pointer {
		kind, bound = tkU8, f.Type.Size
		over = "TableRefuseLengthOverBound"
	}
	owner := g.floatOwner(f)
	if ir.IsTextListField(f) {
		owner = f.TextListElem().MapEntryOf
	}
	stop := damaged + ";return false"
	g.pf("%s{\n", ind)
	i := ind + "\t"
	g.pf("%ssub, ok := r.Body(); if !ok || len(sub.Buffer) < 2 { %s }\n", i, stop)
	g.pf("%selemKind := sub.Get8(); count, ok := sub.Leb(); if !ok { %s }\n", i, stop)
	g.pf("%sif elemKind != %d {\n", i, kind)
	if wireWidenable(kind) {
		g.pf("%s\tif !tableKindWidens(elemKind, %d) { %s }; r.Report.Widened++\n", i, kind, refuse("TableRefuseKindMismatch", owner, "return false"))
	} else {
		g.pf("%s\t%s\n", i, refuse("TableRefuseKindMismatch", owner, "return false"))
	}
	g.pf("%s}\n", i)
	if !keyed {
		g.retainDiscard("r")
	}
	if keyed {
		g.pf("%sfor range count {\n", i)
		j := i + "\t"
		g.pf("%skeyRef, ok := sub.Leb(); if !ok { %s }\n", j, stop)
		g.pf("%skey, ok := sub.Resolve(keyRef); if !ok { %s }\n", j, stop)
		g.pf("%selem, ok := sub.Body(); if !ok { %s }\n", j, stop)
		g.pf("%svar slot %s; if !slot.TableEnumValue(key) { r.Report.Unknown++;", j, f.KeyEnum)
		if g.retain {
			g.pf("if r.Retain!=nil{r.Report.RetainLost++};")
		}
		g.pf("continue }\n")
		g.retainReadPath("elem", "r", "int(slot)-1")
		switch kind {
		case tkTable:
			g.pf("%sif !%sLoadBody(&elem, &%s[int(slot)-1]) { return false }\n", j, f.Type.Name, expr)
			g.emitCarveReturn("sub", "elem", j)
		case tkUnion:
			g.emitReadUnion(f.Type.Ref.(*ir.Union), expr+"[int(slot)-1]", "elem", j)
			g.emitCarveReturn("sub", "elem", j)
		default:
			g.emitReadScalar(f, expr+"[int(slot)-1]", "elem", "elemKind", j, stop)
		}
		g.pf("%sif elem.Offset != int64(len(elem.Buffer)) { %s }\n", j, stop)
		g.pf("%s}\n", i)
	} else {
		g.pf("%sif count > %d { %s }\n", i, bound, refuse(over, owner, "return false"))
		if f.TextTotal > 0 {
			g.pf("%sspent := int64(0)\n", i)
		}
		g.pf("%sfor i := range count {\n", i)
		j := i + "\t"
		switch kind {
		case tkString:
			g.emitReadTextElement(f, expr+"[i]", j)
		case tkTable:
			g.pf("%selem, ok := sub.Body(); if !ok { %s }\n", j, stop)
			g.retainReadPath("elem", "r", "i")
			g.pf("%sif !%sLoadBody(&elem, &%s[i]) { return false }\n", j, f.Type.Name, expr)
			g.emitCarveReturn("sub", "elem", j)
			g.pf("%sif elem.Offset != int64(len(elem.Buffer)) { %s }\n", j, stop)
		case tkUnion:
			g.retainReadPath("sub", "r", "i")
			g.emitReadUnion(f.Type.Ref.(*ir.Union), expr+"[i]", "sub", j)
		default:
			g.emitReadScalar(f, expr+"[i]", "sub", "elemKind", j, stop)
		}
		g.emitTextTotalRead(f, expr+"[i]", j)
		g.pf("%s}\n", i)
		if f.Type.Kind == ir.TBytes && !f.Type.Pointer {
			g.pf("%s%sLength = int32(count)\n", i, expr)
		} else if f.Array == ir.ArrayCounted {
			g.pf("%s%sCount = int32(count)\n", i, expr)
		}
	}
	// bytes left inside the L after its count's elements are damage
	// (docs/SPEC-TABLES.md §4)
	g.pf("%sif sub.Offset != int64(len(sub.Buffer)) { %s }\n", i, stop)
	g.emitCarveReturn("r", "sub", i)
	g.pf("%s}\n", ind)
}

// emitReadUnion reads one union payload (docs/SPEC-TABLES.md §3): the arm id
// reference, then the arm's kind byte and its L-framed payload. An arm this
// reader cannot name reads None and counts unknown; an arm whose kind is not
// the declared one and not a widening refuses kind_mismatch.
func (g *tableGen) emitReadUnion(un *ir.Union, expr, rdr, ind string, element ...bool) {
	arm := g.nextWireWriter()
	stop := damaged + ";return false"
	g.pf("%s{\n", ind)
	i := ind + "\t"
	target := g.nextWireWriter()
	g.pf("%s%s := &%s\n", i, target, expr)
	expr = target
	g.pf("%sarmRef, ok := %s.Leb(); if !ok { %s }\n", i, rdr, stop)
	g.pf("%s%s.Type = %sTypeNone\n", i, expr, un.Name)
	g.pf("%sif armRef != 0 {\n", i)
	i += "\t"
	g.pf("%sarmID, ok := %s.Resolve(armRef); if !ok || !%s.Has(1) { %s }\n", i, rdr, rdr, stop)
	g.pf("%sarmKind := %s.Get8(); %s, ok := %s.Body(); if !ok { %s }\n", i, rdr, arm, rdr, stop)
	g.pf("%sswitch armID {\n", i)
	for ordinal, v := range un.Variants {
		g.pf("%scase 0x%016x:\n", i, ir.TableWireId(v.WireName()))
		j := i + "\t"
		kind := ir.TableKindNoPayload
		if !v.Void() {
			kind = ir.TableWireFieldKind(v.F)
		}
		owner := un.Name + "." + v.Name
		g.pf("%sif armKind != %d {\n", j, kind)
		if wireWidenable(kind) {
			g.pf("%s if !tableKindWidens(armKind,%d) { %s }; if int64(len(%s.Buffer)) != tableKindBytes(armKind) { %s }; r.Report.Widened++\n", j, kind, refuse("TableRefuseKindMismatch", owner, "return false"), arm, stop)
		} else {
			g.pf("%s %s\n", j, refuse("TableRefuseKindMismatch", owner, "return false"))
		}
		g.pf("%s}\n", j)
		g.pf("%s%s.Type = %sType%s\n", j, expr, un.Name, ir.GoExportName(v.Name))
		g.retainReadPath(arm, rdr, fmt.Sprint(ordinal+1))
		g.emitReadArm(v, g.unionArmExpr(un, v, expr), arm, "armKind", j)
	}
	g.pf("%sdefault:r.Report.Unknown++\n", i)
	if g.retain {
		g.pf("if r.Retain!=nil{r.Report.RetainLost++}\n")
	}
	g.pf("%s}\n", i)
	g.emitCarveReturn(rdr, arm, i)
	g.pf("%s}\n%s}\n", ind+"\t", ind)
}

// emitReadArm reads one arm's payload, which its L frames exactly
// (docs/SPEC-TABLES.md §3, §4).
func (g *tableGen) emitReadArm(v ir.UnionVariant, expr, rdr, kind, ind string) {
	bad := damaged + "; return false"
	if v.Void() {
		g.pf("%sif len(%s.Buffer) != 0 { %s }\n", ind, rdr, bad)
		return
	}
	f := v.F
	if v.Body() && !v.F.Type.Pointer {
		g.pf("%sif !%sLoadBody(&%s,&%s) { return false }\n", ind, v.Type, rdr, expr)
		g.emitCarveReturn("r", rdr, ind)
		g.pf("%sif %s.Offset != int64(len(%s.Buffer)) { %s }\n", ind, rdr, rdr, bad)
		return
	}
	if f.Array != ir.ArrayNone || f.Type.Kind == ir.TBytes && !f.Type.Pointer {
		g.emitReadArmArray(f, expr, rdr, ind)
		return
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
		g.pf("%sif !tableUTF8Valid(%s.Buffer) { %s }\n", ind, rdr, bad)
		g.pf("%sif len(%s.Buffer) > %d { %s }; copy(%s[:],%s.Buffer); %sLength=int32(len(%s.Buffer))\n", ind, rdr, f.Type.Size, refuse("TableRefuseLengthOverBound", g.floatOwner(f), "return false"), expr, rdr, expr, rdr)
		return
	}
	if isUnionRef(f.Type) {
		// bytes after a nested union inside its arm's L are damage (§4)
		g.pf("%s%s.Type = %sTypeNone\n", ind, expr, f.Type.Name)
		g.emitReadUnion(f.Type.Ref.(*ir.Union), expr, rdr, ind)
		g.pf("%sif %s.Offset != int64(len(%s.Buffer)) { %s }\n", ind, rdr, rdr, bad)
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

// emitReadArmArray reads an array arm: a bare array body framed by the arm's
// L, its header inside it and every byte of it an element's
// (docs/SPEC-TABLES.md §2.6, §3, §4).
func (g *tableGen) emitReadArmArray(f *ir.Field, expr, rdr, ind string) {
	kind, bound := ir.TableWireScalarKind(f), f.ArrayBound
	counted := f.Array == ir.ArrayCounted || f.Type.Kind == ir.TBytes
	companion := expr + "Count"
	over := "TableRefuseCountOverBound"
	if f.Type.Kind == ir.TBytes && !f.Type.Pointer {
		kind, bound = tkU8, f.Type.Size
		companion = expr + "Length"
		over = "TableRefuseLengthOverBound"
	}
	owner := g.floatOwner(f)
	bad := damaged + ";return false"
	g.pf("%sclear(%s[:])\n", ind, expr)
	if counted {
		g.pf("%s%s=0\n", ind, companion)
	}
	g.pf("%sif !%s.Has(2) { %s }\n", ind, rdr, bad)
	g.pf("%selemKind := %s.Get8(); count,ok := %s.Leb(); if !ok { %s }\n", ind, rdr, rdr, bad)
	g.pf("%sif elemKind != %d {\n", ind, kind)
	if wireWidenable(kind) {
		g.pf("%s if !tableKindWidens(elemKind,%d) { %s }; r.Report.Widened++\n", ind, kind, refuse("TableRefuseKindMismatch", owner, "return false"))
	} else {
		g.pf("%s %s\n", ind, refuse("TableRefuseKindMismatch", owner, "return false"))
	}
	g.pf("%s}\n%sif count > %d { %s }\n", ind, ind, bound, refuse(over, owner, "return false"))
	g.pf("%sfor i:=uint64(0); i<count; i++ {\n", ind)
	j := ind + "\t"
	switch {
	case isStructRef(f.Type) && !f.Type.Pointer:
		elem := g.nextWireWriter()
		g.pf("%s%s,ok:=%s.Body();if !ok {%s}\n", j, elem, rdr, bad)
		g.retainReadPath(elem, rdr, "i")
		g.pf("%sif !%sLoadBody(&%s,&%s[i]) { return false }\n", j, f.Type.Name, elem, expr)
		g.emitCarveReturn(rdr, elem, j)
		g.pf("%sif %s.Offset != int64(len(%s.Buffer)) { %s }\n", j, elem, elem, bad)
	case isUnionRef(f.Type) && !f.Type.Pointer:
		if g.retain {
			g.pf("savedPath:=%s.Path; %s.Path=%s.Path.step(%d,uint32(i))\n", rdr, rdr, rdr, g.retainOrdinal)
		}
		g.emitReadUnion(f.Type.Ref.(*ir.Union), expr+"[i]", rdr, j)
		if g.retain {
			g.pf("%s.Path=savedPath\n", rdr)
		}
	default:
		g.emitReadScalar(f, expr+"[i]", rdr, "elemKind", j, bad)
	}
	if counted {
		g.pf("%s%s=int32(i+1)\n", j, companion)
	}
	g.pf("%s}\n", ind)
	g.pf("%sif %s.Offset != int64(len(%s.Buffer)) { %s }\n", ind, rdr, rdr, bad)
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
	if e := enumRef(f); e != nil {
		g.pf("%s{\n%s\tvariantRef, ok := %s.Leb(); if !ok { %s }\n", ind, ind, rdr, onBad)
		g.pf("%s\tif variantRef == 0 { %s = %sNone } else {\n", ind, expr, f.Type.Name)
		g.pf("%s\t\tid, ok := %s.Resolve(variantRef); if !ok { %s }\n", ind, rdr, onBad)
		g.pf("%s\t\tif !%s.TableEnumValue(id) { r.Report.Unknown++;", ind, expr)
		if g.retain {
			g.pf("if r.Retain!=nil{r.Report.RetainLost++}")
		}
		g.pf(" }")
		if enumHasDeprecated(e) {
			// A DEPRECATED VARIANT READS AS THE FIELD'S DEFAULT, and as the
			// ELEMENT default inside an array, list, map or keyed array; it is
			// never counted unknown (docs/SPEC-TABLES.md §2.15).
			g.pf(" else if %s.TableEnumDeprecated() { %s }", expr, deprecatedLand(f, expr))
		}
		g.pf("\n%s\t}\n%s}\n", ind, ind)
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
		g.pf("%s{\n%sb := %s.Get8()\n", ind, ind, rdr)
		// a bool byte other than 0 or 1 is damage (docs/SPEC-TABLES.md §4)
		g.pf("%sif b > 1 { %s }\n", ind, onBad)
		g.pf("%s%s = b != 0\n%s}\n", ind, expr, ind)
	case tkF32, tkF64:
		// A NaN OR AN INFINITY IS REFUSED ON ITS RAW BITS, before the bits
		// become a value and before the field is written (docs/SPEC-TABLES.md
		// §3.3; Glenn, 2026-10-27, "tables go finite-only too, same rule as
		// packets"): the read names the field and the load refuses as
		// non_finite_float. A compressed float takes
		// the test before its bounds. Every finite float32 widens exactly, so
		// the float rung (kind 10 into a float64) is the ordinary conversion.
		g.needsMath = true
		owner := g.floatOwner(f)
		nonFinite := fmt.Sprintf("r.Report.Refuse(TableRefuseNonFiniteFloat, %q); %s", owner, onBad)
		switch {
		case kind == tkF32:
			g.pf("%s{\n%s\tb := %s.Get32(); if b&0x7f800000 == 0x7f800000 { %s } else { v := math.Float32frombits(b)\n", ind, ind, rdr, nonFinite)
		case knownKindWidth(wireKind) > 0:
			g.pf("%s{\n%s\tb := %s.Get64(); if b&0x7ff0000000000000 == 0x7ff0000000000000 { %s } else { v := math.Float64frombits(b)\n", ind, ind, rdr, nonFinite)
		default:
			// the rung's source width is the wire kind's, known only at run time
			g.pf("%s{\n%s\tfinite, v := true, float64(0); if %s == 10 { b := %s.Get32(); finite, v = b&0x7f800000 != 0x7f800000, float64(math.Float32frombits(b)) } else { b := %s.Get64(); finite, v = b&0x7ff0000000000000 != 0x7ff0000000000000, math.Float64frombits(b) }; if !finite { %s } else {\n", ind, ind, wireKind, rdr, rdr, nonFinite)
		}
		if f.HasFloatRange {
			lo, hi := formatFloat64(f.FMin), formatFloat64(f.FMax)
			if kind == tkF32 {
				lo, hi = formatFloat32(f.FMin), formatFloat32(f.FMax)
			}
			g.pf("%s\tif v < %s || v > %s { %s } else if v == 0 { v = 0 }\n", ind, lo, hi, refuse("TableRefuseOutOfRange", owner, onBad))
		}
		g.pf("%s\t%s = v }\n%s}\n", ind, expr, ind)
	default:
		signed := (f.Type.Kind == ir.TInt || f.Type.Kind == ir.TFixed) && f.Type.Signed
		g.pf("%s{\n%s\tv := %s\n", ind, ind, scalarGetExpr(rdr, wireKind, signed))
		if f.HasIntRange {
			rlo, rhi, _ := ir.TableRawRange(f)
			lo, hi := goIntLit(rlo, signed, 8), goIntLit(rhi, signed, 8)
			g.pf("%s\tif v < %s || v > %s { %s }\n", ind, lo, hi, refuse("TableRefuseOutOfRange", g.floatOwner(f), onBad))
		}
		if f.Type.Kind == ir.TBits && f.Type.Width < 64 {
			max := uint64(1)<<f.Type.Width - 1
			g.pf("%s\tif v > %d { %s }\n", ind, max, refuse("TableRefuseOutOfRange", g.floatOwner(f), onBad))
		}
		typ := goFieldType(f.Type)
		if f.Type.Kind == ir.TBytes && !f.Type.Pointer {
			typ = "byte"
		}
		g.pf("%s\t%s = %s(v)\n%s}\n", ind, expr, typ, ind)
	}
}

// deprecatedLand is the assignment a reader makes when it meets a deprecated
// variant (docs/SPEC-TABLES.md §2.15): the field's declared default, or None
// for an element of an array, list, map or keyed array.
func deprecatedLand(f *ir.Field, expr string) string {
	return fmt.Sprintf("%s = %s", expr, fieldDefaultExpr(f))
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

// requiredFieldNames returns the wire names of the required fields, for the
// missing_required refusal (docs/SPEC-TABLES.md §2.3).
func requiredFieldNames(req []*ir.Field) []string {
	out := make([]string, len(req))
	for i, f := range req {
		out[i] = f.Name
	}
	return out
}

// unitHasRequired reports whether any table of the unit marks a field
// `| required` (docs/SPEC-TABLES.md §2.3): only then does a body end ask the
// shared report whether a body below it was refused for a missing one.
func unitHasRequired(u *ir.Unit) bool {
	for _, st := range u.Tables {
		for _, f := range st.Fields {
			if f.Required {
				return true
			}
		}
	}
	return false
}

// floatOwner names a float field as <Owner>.<field> for the report's
// non_finite_float refusal (docs/SPEC-TABLES.md §3.3), the owner being the table,
// type or union that declares it. A field the unit does not hold by pointer
// (the message form's element copy, messageElement) is named by the closure
// member whose codec is being emitted when that member declares a field of
// its name, and by its own name only past that.
func (g *tableGen) floatOwner(f *ir.Field) string {
	for _, m := range []map[string]*ir.Struct{g.unit.Tables, g.unit.Structs} {
		for _, st := range m {
			if slices.Contains(st.Fields, f) {
				return st.DeclaredName() + "." + f.Name
			}
		}
	}
	for _, m := range []map[string]*ir.Union{g.unit.Unions, g.unit.TableUnions} {
		for _, u := range m {
			for i := range u.Variants {
				if u.Variants[i].F == f {
					return u.Name + "." + f.Name
				}
			}
		}
	}
	if g.owner != nil {
		for _, x := range g.owner.Fields {
			if x.Name == f.Name {
				return g.owner.DeclaredName() + "." + f.Name
			}
		}
	}
	return f.Name
}
