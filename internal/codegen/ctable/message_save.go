package ctable

import (
	"strconv"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func (g *tableGen) messageSlot(entry ir.TableVocabularyEntry) int {
	if g.messageSlots == nil {
		g.messageSlots = ir.TableVocabularySlots(g.unit)
	}
	if slot := g.messageSlots[entry.Key()]; slot > 0 {
		return int(slot)
	}
	panic("message entry is absent from the unit vocabulary: " + entry.Key())
}

func (g *tableGen) messageHeader(e ir.TableVocabularyEntry, ind string) {
	g.pf("%stable_bit_put(w,%d,kTableMessageRefBitsHere);\n", ind, g.messageSlot(e))
}

func (g *tableGen) messageEnum(e *ir.Enum, expr, ind string) {
	g.pf("%sswitch(%s) {\n%s case %s: table_bit_put(w,0,kTableMessageRefBitsHere); break;\n", ind, expr, ind, enumNoneConst(e.Name))
	for i, v := range e.Variants {
		entry := ir.TableVocabularyEntry{Id: ir.TableWireId(e.VariantWireName(i))}
		g.pf("%s case %s: table_bit_put(w,%d,kTableMessageRefBitsHere); break;\n", ind, enumConst(e.Name, v), g.messageSlot(entry))
	}
	g.pf("%s default: return 0;\n%s}\n", ind, ind)
}

// Scalar arithmetic is unsigned at the stored width, including 128-bit bases.
func (g *tableGen) messageScalar(f *ir.Field, kind uint8, shape ir.TableMessageShape, expr, ind string) {
	if e := enumRef(f); e != nil {
		g.messageEnum(e, expr, ind)
		return
	}
	bits := ir.TableMessageValueBits(kind, shape)
	switch int(kind) {
	case tkBool:
		g.pf("%stable_bit_put(w,%s ? 1 : 0,1);\n", ind, expr)
	case tkF32:
		if shape.Packing == ir.TableMessageQuantized {
			count, delta, _ := ir.TableMessageQuantization(shape)
			g.pf("%stable_bit_put(w,table_message_quantize(%s,%s,%s,%du),%d);\n", ind, expr, formatFloat(float64(shape.QMin), true), formatFloat(float64(delta), true), count, bits)
		} else {
			g.pf("%stable_bit_put(w,table_float_to_bits(%s),32);\n", ind, expr)
		}
	case tkF64:
		g.pf("%stable_bit_put(w,table_double_to_bits(%s),64);\n", ind, expr)
	default:
		var lo, hi uint64
		if shape.Base != nil {
			low, high := wideLanes(shape.Base)
			lo, hi = low.Uint64(), high.Uint64()
		}
		if f.Type.Width == 128 {
			g.pf("%s{ uint64_t low=(%s).lo-UINT64_C(%d), high=(%s).hi-UINT64_C(%d)-((%s).lo<UINT64_C(%d));\n", ind, expr, lo, expr, hi, expr, lo)
			g.pf("%s (void)high; table_bit_put(w,low,%d);\n", ind, min(bits, 64))
			if bits > 64 {
				g.pf("%s table_bit_put(w,high,%d);\n", ind, bits-64)
			}
			g.pf("%s}\n", ind)
		} else {
			g.pf("%stable_bit_put(w,(uint64_t)(%s)-UINT64_C(%d),%d);\n", ind, expr, lo, bits)
		}
	}
}

func (g *tableGen) messageValue(f *ir.Field, e ir.TableVocabularyEntry, expr, count, ind string) {
	switch {
	case f.IsList() || f.IsMap():
		elem, typ := sequenceElement(f), g.sequenceType(f)
		g.pf("%s{ TableSequenceCursor cursor=%s; int32_t i;\n", ind, g.sequenceCursor(f, "w->nodes", expr))
		g.pf("%s if(!cursor.ok || %s.count<0) return 0; table_bit_put(w,(uint64_t)%s.count,32);\n", ind, expr, expr)
		if ir.TableMessageAligns(e.Kind, e.Shape) {
			g.pf("%stable_bit_align_write(w);\n", ind)
		}
		g.pf("%s for(i=0;i<%s.count;i++) { const %s * element=(const %s *)table_sequence_next(&cursor); if(element==NULL) return 0;\n", ind, expr, typ, typ)
		g.messageElement(elem, e.Shape.Elem, e.Shape.Inner, "(*element)", ind+"  ")
		g.pf("%s } }\n", ind)
	case f.Type.Pointer && f.Array == ir.ArrayNone:
		g.pf("%s{ const void * node=table_ref_at(w->nodes ? w->nodes->ctx : NULL,&%s); uint64_t index=table_number_find(w->nodes,node);\n", ind, expr)
		g.pf("%s if(node!=NULL && index==0) return 0; table_bit_put(w,index,w->index_bits); }\n", ind)
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TWString:
		g.pf("%sif(%s<0 || %s>%d) return 0;\n", ind, count, count, f.Type.Size)
		bits := ir.TableMessageBitsRequired(0, e.Shape.Max)
		if f.Type.Kind == ir.TBytes {
			bits = ir.TableMessageCountBits(e.Shape)
		}
		g.pf("%stable_bit_put(w,(uint64_t)%s,%d);\n", ind, count, bits)
		if f.Type.Kind == ir.TWString {
			g.pf("%s{ int32_t i; for(i=0;i<%s;i++) table_bit_put(w,(uint16_t)%s[i],16); }\n", ind, count, expr)
		} else {
			g.pf("%stable_bit_align_write(w); table_bit_putbytes(w,%s,%s);\n", ind, expr, count)
		}
	case f.KeyEnum != "":
		g.pf("%s{ uint32_t count=0; int32_t i;\n", ind)
		g.pf("%s for(i=0;i<%d;i++) {\n", ind, f.ArrayBound)
		g.messageKeyedRides(f, expr+"[i]", ind+"  ")
		g.pf("%s  count++; }\n%s table_bit_put(w,count,%d);\n", ind, ind, ir.TableMessageCountBits(e.Shape))
		g.pf("%s for(i=0;i<%d;i++) {\n", ind, f.ArrayBound)
		g.messageKeyedRides(f, expr+"[i]", ind+"  ")
		g.messageEnum(f.KeyEnumRef, "i+1", ind+"  ")
		g.messageElement(f, e.Shape.Elem, e.Shape.Inner, expr+"[i]", ind+"  ")
		g.pf("%s } }\n", ind)
	case f.Array != ir.ArrayNone:
		g.pf("%sif(%s<0 || %s>%d) return 0;\n", ind, count, count, f.ArrayBound)
		g.pf("%stable_bit_put(w,(uint64_t)%s-%d,%d);\n", ind, count, e.Shape.Min, ir.TableMessageCountBits(e.Shape))
		if ir.TableMessageAligns(e.Kind, e.Shape) {
			g.pf("%stable_bit_align_write(w);\n", ind)
		}
		g.pf("%s{ int32_t i; for(i=0;i<%s;i++) {\n", ind, count)
		g.messageElement(f, e.Shape.Elem, e.Shape.Inner, expr+"[i]", ind+"  ")
		g.pf("%s} }\n", ind)
	default:
		g.messageElement(f, e.Kind, &e.Shape, expr, ind)
	}
}

func (g *tableGen) messageElement(f *ir.Field, kind uint8, shape *ir.TableMessageShape, expr, ind string) {
	if f.Type.Pointer {
		g.messageValue(&ir.Field{Type: f.Type}, ir.TableVocabularyEntry{}, expr, "", ind)
		return
	}
	switch int(kind) {
	case tkTable:
		g.pf("%sif(!%s(w,&%s)) return 0;\n", ind, g.api(f.Type.Name, "save_message_body"), expr)
	case tkUnion:
		g.pf("%sif(!%s(w,&%s)) return 0;\n", ind, g.unionWireName(f.Type.Ref.(*ir.Union), "message_save"), expr)
	default:
		if shape == nil {
			shape = &ir.TableMessageShape{}
		}
		g.messageScalar(f, kind, *shape, expr, ind)
	}
}

func (g *tableGen) messageKeyedRides(f *ir.Field, expr, ind string) {
	if ir.TableWireScalarKind(f) == tkTable {
		g.pf("%sTableBitWriter probe=*w; probe.buffer=NULL; probe.capacity=INT64_MAX; probe.bits=0; probe.check_default=1;\n", ind)
		g.pf("%sif(!%s(&probe,&%s)) return 0;\n%sif(probe.bits==kTableMessageRefBitsHere) continue;\n", ind, g.api(f.Type.Name, "save_message_body"), expr, ind)
	} else {
		g.pf("%sif(%s) continue;\n", ind, g.wireDefaultEquals(f, expr))
	}
}

func (g *tableGen) emitMessageSaveDeclarations(members []*ir.Struct, unions []*ir.Union) {
	for _, st := range members {
		g.pf("static SCHEMA_UNUSED int %s(TableBitWriter *,const %s *);\n", g.api(st.Name, "save_message_body"), st.Name)
	}
	for _, un := range unions {
		g.pf("static SCHEMA_UNUSED int %s(TableBitWriter *,const %s *);\n", g.unionWireName(un, "message_save"), un.Name)
	}
}

func (g *tableGen) emitMessageUnionSave(un *ir.Union) {
	g.pf("static SCHEMA_UNUSED int %s(TableBitWriter * w,const %s * value)\n{\n switch(value->type) {\n", g.unionWireName(un, "message_save"), un.Name)
	g.pf(" case %s: table_bit_put(w,0,kTableMessageRefBitsHere); break;\n", enumNoneConst(un.Name+"Type"))
	for _, v := range un.Variants {
		g.pf(" case %s: {\n", enumConst(un.Name+"Type", v.Name))
		e := ir.TableArmEntry(v)
		g.messageHeader(e, "  ")
		if !v.Void() {
			count := strconv.FormatInt(v.F.ArrayBound, 10)
			if armCompanioned(v) {
				count = armValue("(*value)", v) + "_count"
				if v.F.Array == ir.ArrayNone {
					count = armValue("(*value)", v) + "_length"
				}
			}
			g.messageValue(v.F, e, armValue("(*value)", v), count, "  ")
		}
		g.pf("  break; }\n")
	}
	g.pf(" default: return 0;\n } return !w->overflow;\n}\n")
}

func (g *tableGen) emitMessageSave(st *ir.Struct) {
	g.pf("static SCHEMA_UNUSED int %s(TableBitWriter * w,const %s * value)\n{\n (void)value;\n", g.api(st.Name, "save_message_body"), st.Name)
	guards := tableGuardExprs(st)
	for _, f := range st.Fields {
		expr, kind := "value->"+f.Name, ir.TableWireScalarKind(f)
		g.pf(" { /* %s */\n", f.Name)
		if guard := guards[f.Name]; guard != "" {
			g.pf(" if(%s) {\n", guard)
		}
		condition := "!(" + g.wireDefaultEquals(f, expr) + ")"
		count := strconv.FormatInt(f.ArrayBound, 10)
		switch {
		case f.IsList() || f.IsMap():
			condition = expr + ".count>0"
			g.pf(" if(%s.count<0) return 0;\n", expr)
		case !f.Type.Blob() && (f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TString || f.Type.Kind == ir.TWString):
			count = expr + "_length"
			g.pf(" if(%s<0 || %s>%d) return 0;\n", count, count, f.Type.Size)
			condition = g.wireBytesDiffer(f, expr)
		case f.Array == ir.ArrayCounted:
			count = expr + "_count"
			g.pf(" if(%s<0 || %s>%d) return 0;\n", count, count, f.ArrayBound)
			condition = count + ">0"
		case f.Type.Optional:
			condition = expr + "_present"
		case f.KeyEnum != "":
			g.pf(" int rides=0; { int32_t i; for(i=0;i<%d;i++) {\n", f.ArrayBound)
			g.messageKeyedRides(f, expr+"[i]", "  ")
			g.pf("  rides=1; break; } }\n")
			condition = "rides"
		case f.Array == ir.ArrayFixed:
			condition = "1"
			if kind != tkTable {
				g.pf(" int rides=0; { int32_t i; for(i=0;i<%d;i++) if(!(%s)) { rides=1; break; } }\n", f.ArrayBound, g.wireDefaultEquals(f, expr+"[i]"))
				condition = "rides"
			}
		case kind == tkTable:
			g.pf(" TableBitWriter probe=*w; probe.buffer=NULL; probe.capacity=INT64_MAX; probe.bits=0; probe.check_default=1;\n if(!%s(&probe,&%s)) return 0;\n", g.api(f.Type.Name, "save_message_body"), expr)
			condition = "probe.bits>kTableMessageRefBitsHere"
		case kind == tkUnion:
			condition = expr + ".type!=" + enumNoneConst(f.Type.Name+"Type")
		}
		if f.Type.Optional {
			condition = expr + "_present"
		}
		g.pf(" if(%s) {\n  if(w->check_default) {w->bits=kTableMessageRefBitsHere+1;return 1;}\n", condition)
		g.messageHeader(ir.TableFieldEntry(f), "  ")
		g.messageValue(f, ir.TableFieldEntry(f), expr, count, "  ")
		g.pf(" }\n")
		if guards[f.Name] != "" {
			g.pf(" }\n")
		}
		g.pf(" }\n")
	}
	g.pf(" table_bit_put(w,0,kTableMessageRefBitsHere); return !w->overflow;\n}\n")
	if !g.isVar(st.Name) && !st.IsMapEntry() {
		g.emitFixedMessageSave(st)
	}
}

func (g *tableGen) emitFixedMessageSave(st *ir.Struct) {
	for _, measure := range []bool{true, false} {
		verb, extra, buf, cap := "save_messages", ",uint8_t * buffer,int64_t capacity,TableReport * report", "buffer", "capacity"
		if measure {
			verb, extra, buf, cap = "measure_messages", ",TableReport * report", "NULL", "INT64_MAX"
		}
		g.pf("static SCHEMA_UNUSED int64_t %s(const %s * values,int64_t count%s)\n{\n", g.api(st.Name, verb), st.Name, extra)
		g.pf(" TableBitWriter w=table_bit_writer(%s,%s); int64_t i;\n if(count<1 || values==NULL) return -1;\n if(count>kTableMessageBatchMax) {if(report!=NULL) {report->refused=1;report->reason=SCHEMA_TABLE_BATCH_TOO_LARGE;}return -1;}\n", buf, cap)
		if !measure {
			g.pf(" if(buffer==NULL || capacity<0) return -1;\n")
		}
		g.pf(" table_bit_put(&w,2,8); table_bit_put(&w,(uint64_t)(count-1),8);\n for(i=0;i<count;i++) if(!%s(&w,values+i)) return -1;\n table_bit_align_write(&w); return w.overflow ? -1 : w.bits/8;\n}\n", g.api(st.Name, "save_message_body"))
	}
}
