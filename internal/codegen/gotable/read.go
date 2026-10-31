package gotable

import (
	"fmt"
	"slices"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func (g *tableGen) emitTableRead(st *ir.Struct) {
	n := st.Name
	g.pf("func %sLoadBody(r *TableReader, value *%s) bool {\n", n, g.storageName(n))
	g.pf("\t%sReset(value)\n", n)
	if g.retain {
		g.pf("r.Retain.discard(r.Path,false,0)\n")
	}
	// `| required` fields (docs/SPEC-TABLES.md §2.3): each gets a seen flag,
	// set when its id rides, and a record that ends without one is refused
	// with missing_required naming the field. A required field inside an `if`
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
	if unitHasRequired(g.unit) {
		// Counted from this body's start, so a report the caller reuses does
		// not refuse a clean record for an earlier read's missing field.
		g.pf("\tmissingBefore := r.Report.MissingRequired\n")
	}
	g.pf("\tfor {\n\t\tref, ok := r.lebPair(); if !ok { ref, ok = r.lebWide() }; if !ok { r.Report.Malformed = true; return false }\n")
	switch {
	case len(req) > 0:
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
		g.pf("\t\t\t\tr.Report.MissingRequired++\n")
		g.pf("\t\t\t\tmissingReq := \"\"\n")
		for i, n := range requiredFieldNames(req) {
			if reqGuard[i] != "" {
				g.pf("\t\t\t\tif !reqSeen%d && (%s) { if missingReq != \"\" { missingReq += \", \" }; missingReq += %q }\n", i, reqGuard[i], n)
			} else {
				g.pf("\t\t\t\tif !reqSeen%d { if missingReq != \"\" { missingReq += \", \" }; missingReq += %q }\n", i, n)
			}
		}
		g.pf("\t\t\t\tr.Report.Reason = \"missing_required: \" + missingReq\n")
		g.pf("\t\t\t\treturn false\n")
		g.pf("\t\t\t}\n")
		g.pf("\t\t\treturn r.Report.MissingRequired == missingBefore\n")
		g.pf("\t\t}\n")
	case unitHasRequired(g.unit):
		// A BODY BELOW THIS ONE that was refused for a missing required field
		// refuses this one too (docs/SPEC-TABLES.md §2.3): the report is shared
		// down the walk, so a nested table, an element, a map entry or a node
		// body that set MissingRequired stops every body above it, and the
		// whole record is refused naming the field — never read as a parent
		// with a defaulted child. A unit with no required field pays nothing.
		g.pf("\t\tif ref == 0 { return r.Report.MissingRequired == missingBefore }\n")
	default:
		g.pf("\t\tif ref == 0 { return true }\n")
	}
	g.pf("\t\tfieldID, ok := r.Resolve(ref); if !ok || !r.Has(1) { r.Report.Malformed = true; return false }\n")
	// THE FIELD HEADER IS READ WHOLE BEFORE A RESERVED ID IS JUDGED, as the
	// oracle reads it (internal/tablewire bodyAt). A reserved id in a body
	// that is not its transport is MALFORMED (docs/SPEC-TABLES.md §3.1, §3.3),
	// and that is framing damage: the level stops and KEEPS WHAT IT DECODED
	// there (§4). Where it stops is what the caller's "terminator is the last
	// byte of L" check reads (§3), so stopping before the kind byte left the
	// cursor one byte short and threw away the decoded prefix the oracle keeps
	// — a union arm read None where the reference kept the arm (wire fuzz
	// seed 24845619678, stream_chain).
	g.pf("\t\tkind := r.Get8()\n")
	g.pf("\t\tif fieldID == 0xfffffffffffffffe || fieldID == 0xfffffffffffffffd || r.Nested && fieldID == 0xffffffffffffffff { r.Report.Malformed = true; return false }\n")
	g.pf("\t\tswitch fieldID {\n")
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
			if idx, ok := reqIdx[f]; ok {
				g.pf("\t\t\t\t\treqSeen%d = true\n", idx)
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
		if idx, ok := reqIdx[f]; ok {
			g.pf("\t\t\treqSeen%d = true\n", idx)
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
		g.pf("default:r.Report.Unknown++;if !r.capture(fieldID,kind,%q){r.Report.Malformed=true;return false}\n}\n}\n}\n", st.DeclaredName())
	} else {
		g.pf("\t\tdefault:\n\t\t\tr.Report.Unknown++; if !r.Skip(kind) { r.Report.Malformed = true; return false }\n\t\t}\n\t}\n}\n\n")
	}
	if g.retain || st.IsMapEntry() || g.regional && ir.VariableTables(g.unit)[st.Name] {
		return
	}
	if g.hasFixedForm(st) {
		// IT CARRIES BOTH FORMS AND EACH ENTRY POINT CARRIES ONE (§3.4, §15).
		// <T>FixedLoad reads form 3 and names a form-1 file `previous_form`;
		// this <T>Load reads form 1 and names 2 `message_form_as_file`, 0
		// `previous_form` (older than form 1) and any other byte `newer_form`. The
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
	// THE TOLERANT READ HAS A NAME OF ITS OWN (docs/SPEC-TABLES.md §6.5): it
	// counts the report and reads past evolution events, and the plain name below
	// is the strict read over it.
	g.pf("// %sLoadTolerant is the TOLERANT read (docs/SPEC-TABLES.md §6.5): it accepts every\n", n)
	g.pf("// wire the framing walk accepts, with the report's counters counted onto the one\n")
	g.pf("// passed. Damage anywhere, a nested body included, refuses the WHOLE load: it\n")
	g.pf("// answers false with the value at its defaults (docs/SPEC-TABLES.md §4), as does\n")
	g.pf("// a form it does not carry. %sLoad is the strict read; call this one by name.\n", n)
	g.pf("func %sLoadTolerant(value *%s, data []byte, report *TableReport) bool {\n", n, g.storageName(n))
	g.pf("\tif report == nil { var ignored TableReport; report = &ignored }\n")
	g.pf("\tr, verdict := tableOpen(data, report); report.Verdict = verdict; report.Reason = \"\"\n")
	g.pf("\tif verdict == TableOpenRefused { report.Reason = \"newer_form\"; if len(data) > 0 { if data[0] == 2 { report.Reason = \"message_form_as_file\" } else if data[0] == 0 { report.Reason = \"previous_form\" } } }\n") // docs/SPEC-TABLES.md §3 (THE FIRST BYTE), §6.5
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
	// DAMAGE DISCARDS THE WHOLE LOAD (docs/SPEC-TABLES.md §4): the walk's own
	// malformed flag is read on its own, apart from any the caller's report
	// already carried, and a read that met damage anywhere, a nested body
	// included, hands back no value.
	g.pf("\treport.Malformed = false\n")
	// A NON-FINITE FLOAT REFUSES THE READ, the tolerant one too, and outranks
	// any damage the walk met after it (docs/SPEC-TABLES.md §3.3): nothing lands.
	g.pf("\tif !%sLoadBody(&r, value) || report.NonFiniteFloat != \"\" {\n", n)
	g.pf("\t\tif tableNonFiniteRefused(report) {\n\t\t\t%sReset(value)\n\t\t\treport.Malformed = report.Malformed || before.Malformed\n\t\t\treturn false\n\t\t}\n", n)
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
	g.pf("\t\tif report.Malformed {\n\t\t\t%sReset(value)\n\t\t}\n", n)
	g.pf("\t\treport.Malformed = report.Malformed || before.Malformed\n")
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
	g.pf("\tif report.Malformed {\n")
	g.pf("\t\t%sReset(value)\n", n)
	g.pf("\t\treport.Verdict = TableOpenDamaged\n")
	g.pf("\t\treturn false\n")
	g.pf("\t}\n")
	g.pf("\treport.Malformed = before.Malformed\n")
	g.pf("\treturn true\n}\n\n")
	// THE PLAIN NAME IS THE STRICT READ (docs/SPEC-TABLES.md §6.5, Go first).
	g.pf("// %sLoad is the plain read, and it is STRICT (docs/SPEC-TABLES.md §6.5): it refuses\n", n)
	g.pf("// whatever %sReadStrict refuses (malformed, clamped, kind_mismatch, unknown form,\n", n)
	g.pf("// wire damage, build version, missing_required), resets value to its defaults and\n")
	g.pf("// answers false. A supplied report receives THIS read's report, and on a refusal\n")
	g.pf("// report.Err() is the typed reason. %sLoadTolerant also reads past an evolution event.\n", n)
	g.pf("func %sLoad(value *%s, data []byte, report *TableReport) bool {\n", n, g.storageName(n))
	g.pf("\tread := report\n\tif read == nil {\n\t\tread = &TableReport{}\n\t} else {\n\t\t*read = TableReport{}\n\t}\n\tok := %sLoadTolerant(value, data, read)\n", n)
	g.pf("\tif !ok || read.Err() != nil { %sReset(value); return false }\n\treturn true\n}\n\n", n)
	// ReadStrict is §6.5's STRICT read for a root whose file load is not the builder.
	g.pf("// %sReadStrict is the STRICT read (docs/SPEC-TABLES.md §6.5): one call, one typed reason.\n", n)
	g.pf("func %sReadStrict(wire []byte)(*%s,error){value:=&%s{};var report TableReport;ok:=%sLoadTolerant(value,wire,&report);if err:=report.Err();err!=nil||!ok{if err==nil{err=TableRefuseWireDamaged};return nil,err};return value,nil}\n\n", n, g.storageName(n), g.storageName(n), n)
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
	case f.Type.TextUnbounded():
		g.emitReadUnboundedText(f, expr, ind)
	case f.Type.Kind == ir.TString:
		g.pf("%ssub, ok := r.Body(); if !ok { r.Report.Malformed = true; return false }\n", ind)
		g.pf("%sif !tableUTF8Valid(sub.Buffer) { r.Report.Malformed = true;\n", ind)
		g.emitTableResetField(f)
		g.pf("break }\n")
		if holder := g.textValueHolder(f); holder != "" {
			g.pf("%skeep := int64(len(sub.Buffer)); if keep > %d { keep = tableUTF8Clamp(sub.Buffer, %d); r.Report.Clamped++; tableTextClamped(&r.Report.ClampedField, %q) }\n", ind, f.Type.Size, f.Type.Size, holder)
		} else {
			g.pf("%skeep := int64(len(sub.Buffer)); if keep > %d { keep = tableUTF8Clamp(sub.Buffer, %d); r.Report.Clamped++ }\n", ind, f.Type.Size, f.Type.Size)
		}
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
		g.pf("%sfor range count {\n", i)
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
		if ir.IsTextListField(f) {
			// a text list's count clamp names the field (docs/SPEC-TABLES.md §2.13)
			g.pf("%skeep := count; if keep > %d { keep = %d; r.Report.Clamped++; tableTextClamped(&r.Report.ClampedField, %q) }\n", i, bound, bound, f.TextListElem().MapEntryOf)
		} else {
			g.pf("%skeep := count; if keep > %d { keep = %d; r.Report.Clamped++ }\n", i, bound, bound)
		}
		counted := f.Array == ir.ArrayCounted || f.Type.Kind == ir.TBytes
		if counted {
			companion := expr + "Count"
			if f.Type.Kind == ir.TBytes {
				companion = expr + "Length"
			}
			g.pf("%sprevious := %s; decoded := int32(0)\n", i, companion)
		}
		if f.TextTotal > 0 {
			g.pf("%sspent := int64(0)\n", i)
		}
		g.pf("%sfor i := range keep {\n", i)
		j := i + "\t"
		switch kind {
		case tkString:
			g.emitReadTextElement(f, expr+"[i]", j)
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
		g.emitTextTotalRead(f, expr+"[i]", j)
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
		g.pf("%sif !tableUTF8Valid(%s.Buffer) { %s }\n", ind, rdr, bad)
		g.pf("%skeep := int64(len(%s.Buffer)); if keep > %d { keep = tableUTF8Clamp(%s.Buffer,%d); r.Report.Clamped++ }; copy(%s[:],%s.Buffer[:keep]); %sLength=int32(keep)\n", ind, rdr, f.Type.Size, rdr, f.Type.Size, expr, rdr, expr)
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
		g.pf("%sif b > 1 { r.Report.Malformed = true; if r.Report.Reason == \"\" { r.Report.Reason = \"malformed: %s\" } }\n", ind, member(f))
		g.pf("%s%s = b != 0\n%s}\n", ind, expr, ind)
	case tkF32, tkF64:
		// A NaN OR AN INFINITY IS REFUSED ON ITS RAW BITS, before the bits
		// become a value and before the field is written (docs/SPEC-TABLES.md
		// §3.3; Glenn, 2026-10-27, "tables go finite-only too, same rule as
		// packets"): the read names the field and the load refuses as
		// non_finite_float, the tolerant one included. A compressed float takes
		// the test before its bounds. Every finite float32 widens exactly, so
		// the float rung (kind 10 into a float64) is the ordinary conversion.
		g.needsMath = true
		owner := g.floatOwner(f)
		refuse := fmt.Sprintf("tableNonFiniteFloat(&r.Report.NonFiniteFloat, %q)", owner)
		switch {
		case kind == tkF32:
			g.pf("%s{\n%s\tb := %s.Get32(); if b&0x7f800000 == 0x7f800000 { %s } else { v := math.Float32frombits(b)\n", ind, ind, rdr, refuse)
		case knownKindWidth(wireKind) > 0:
			g.pf("%s{\n%s\tb := %s.Get64(); if b&0x7ff0000000000000 == 0x7ff0000000000000 { %s } else { v := math.Float64frombits(b)\n", ind, ind, rdr, refuse)
		default:
			// the rung's source width is the wire kind's, known only at run time
			g.pf("%s{\n%s\tfinite, v := true, float64(0); if %s == 10 { b := %s.Get32(); finite, v = b&0x7f800000 != 0x7f800000, float64(math.Float32frombits(b)) } else { b := %s.Get64(); finite, v = b&0x7ff0000000000000 != 0x7ff0000000000000, math.Float64frombits(b) }; if !finite { %s } else {\n", ind, ind, wireKind, rdr, rdr, refuse)
		}
		if f.HasFloatRange {
			lo, hi := formatFloat64(f.FMin), formatFloat64(f.FMax)
			if kind == tkF32 {
				lo, hi = formatFloat32(f.FMin), formatFloat32(f.FMax)
			}
			g.pf("%s\t%s\n", ind, compressedFloatClamp("v", lo, hi, fmt.Sprintf("r.Report.Clamped++; tableTextClamped(&r.Report.ClampedField, %q)", owner)))
		}
		g.pf("%s\t%s = v }\n%s}\n", ind, expr, ind)
	default:
		signed := (f.Type.Kind == ir.TInt || f.Type.Kind == ir.TFixed) && f.Type.Signed
		g.pf("%s{\n%s\tv := %s\n", ind, ind, scalarGetExpr(rdr, wireKind, signed))
		if f.HasIntRange {
			rlo, rhi, _ := ir.TableRawRange(f)
			lo, hi := goIntLit(rlo, signed, 8), goIntLit(rhi, signed, 8)
			g.pf("%s\tif v < %s { v = %s; r.Report.Clamped++; tableTextClamped(&r.Report.ClampedField, %q) } else if v > %s { v = %s; r.Report.Clamped++; tableTextClamped(&r.Report.ClampedField, %q) }\n", ind, lo, lo, g.floatOwner(f), hi, hi, g.floatOwner(f))
		}
		if f.Type.Kind == ir.TBits && f.Type.Width < 64 {
			max := uint64(1)<<f.Type.Width - 1
			g.pf("%s\tif v > %d { v = %d; r.Report.Clamped++; tableTextClamped(&r.Report.ClampedField, %q) }\n", ind, max, max, g.floatOwner(f))
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
