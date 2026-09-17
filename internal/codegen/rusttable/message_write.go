package rusttable

import (
	"fmt"
	"github.com/mas-bandwidth/schema/v2/ir"
	"math/big"
	"strconv"
)

func (g *gen) messageSlot(entry ir.TableVocabularyEntry) uint64 {
	if g.messageSlots == nil {
		g.messageSlots = map[string]uint64{}
		for i, e := range ir.TableVocabulary(g.unit) {
			g.messageSlots[e.Key()] = uint64(i + 1)
		}
	}
	slot := g.messageSlots[entry.Key()]
	if slot == 0 {
		panic("message entry missing from unit vocabulary")
	}
	return slot
}
func (g *gen) messageName(id uint64) uint64 { return g.messageSlot(ir.TableVocabularyEntry{Id: id}) }
func (g *gen) emitMessageSave(st *ir.Struct) {
	g.pf("pub fn %s(w:&mut TableMessageWriter,value:&%s)->bool {let _=&value;\n", fn(st.Name, "save_message_body"), st.Name)
	for _, f := range st.Fields {
		guard := guardExprs(st)[f.Name]
		if guard != "" {
			g.pf("if %s {\n", guard)
		}
		g.emitMessageField(f)
		if guard != "" {
			g.pf("}\n")
		}
	}
	g.pf("w.bits.put(0,TABLE_MESSAGE_REF_BITS);!w.bits.is_overflow()}\n")
	for _, verb := range []string{"measure_messages", "save_messages"} {
		buffer := "None"
		arg := ""
		if verb == "save_messages" {
			buffer = "Some(buffer)"
			arg = ",buffer:&mut[u8]"
		}
		typ := st.Name
		runtime := "table_message_write_fixed"
		if g.variable[st.Name] {
			typ = "TableMessageValue<'_," + typ + ">"
			runtime = "table_message_write_graph"
		}
		g.pf("pub fn %s(values:&[%s]%s,report:&mut TableReport)->core::result::Result<usize,TableMessageError>{%s(values,%s,report)}\n", fn(st.Name, verb), typ, arg, runtime, buffer)
	}
}
func (g *gen) emitMessageField(f *ir.Field) {
	if armCounted(f) {
		n := "value." + f.Name + "_length"
		bound := f.Type.Size
		if f.Array != ir.ArrayNone {
			n = "value." + f.Name + "_count"
			bound = f.ArrayBound
		}
		g.pf("if %s<0 || %s>%d {return false;}\n", n, n, bound)
	}
	cond := g.nonDefaultTest(f)
	switch {
	case f.IsList() || f.IsMap():
		cond = "!value." + f.Name + ".is_empty()"
	case f.Type.Pointer && f.Array == ir.ArrayNone:
		cond = "!value." + f.Name + ".is_null()"
	case f.Type.Optional:
		cond = "value." + f.Name + "_present"
	case !f.Type.Pointer && (f.Type.Kind == ir.TString || f.Type.Kind == ir.TWString || f.Type.Kind == ir.TBytes):
		cond = "value." + f.Name + "_length>0"
		if len(f.DefBytes) > 0 {
			cond = fmt.Sprintf("value.%s_length!=%d || value.%s[..value.%s_length as usize]!=%s", f.Name, len(f.DefBytes), f.Name, f.Name, rustBytes(f.DefBytes))
		}
	case f.KeyEnum != "":
		g.pf("{let mut pairs=0u64;for i in 0..%s {\n", arrayLen(f))
		g.emitMessageKeySkip(f)
		g.pf("pairs+=1;}\n")
		cond = "pairs>0"
	case f.Array == ir.ArrayCounted:
		cond = "value." + f.Name + "_count>0"
	case f.Array == ir.ArrayFixed:
		if isStruct(f) && !f.Type.Pointer {
			cond = "true"
		} else {
			cond = fmt.Sprintf("value.%s.iter().any(|v|*v!=%s)", f.Name, g.arrayElementDefault(f))
		}
	case isUnion(f):
		cond = "value." + f.Name + "!=" + f.Type.Name + "::None"
	case isStruct(f):
		g.pf("let Some(size)=w.body_bits(|w|%s(w,&value.%s)) else{return false;};\n", fn(f.Type.Name, "save_message_body"), f.Name)
		cond = "size>TABLE_MESSAGE_REF_BITS"
	}
	entry := ir.TableFieldEntry(f)
	g.pf("if %s {w.bits.put(%d,TABLE_MESSAGE_REF_BITS);\n", cond, g.messageSlot(entry))
	switch {
	case f.IsList() || f.IsMap():
		g.pf("if !unsafe{table_message_sequence_save(w,&value.%s as *const _ as *const u8,%s())}{return false;}\n", f.Name, sequenceName(g.owner, f))
	case f.KeyEnum != "":
		g.pf("w.bits.put(pairs,%d);for i in 0..%s {\n", ir.TableMessageCountBits(entry.Shape), arrayLen(f))
		g.emitMessageKeySkip(f)
		for i := range f.KeyEnumRef.Variants {
			if i == 0 {
				g.pf("match i {\n")
			}
			g.pf("%d=>w.bits.put(%d,TABLE_MESSAGE_REF_BITS),\n", i, g.messageName(ir.TableWireId(f.KeyEnumRef.VariantWireName(i))))
		}
		g.pf("_=>return false,}\n")
		g.emitMessageElement(f, entry.Shape.Elem, *entry.Shape.Inner, fmt.Sprintf("%s[i]", g.keyedSlots(f)))
		g.pf("}\n")
	default:
		g.emitMessagePayload(f, entry, "value."+f.Name, "value."+f.Name+"_length", "value."+f.Name+"_count")
	}
	g.pf("}\n")
	if f.KeyEnum != "" {
		g.pf("}\n")
	}
}
func (g *gen) emitMessageKeySkip(f *ir.Field) {
	if isStruct(f) {
		g.pf("let Some(size)=w.body_bits(|w|%s(w,&%s[i]))else{return false;};if size==TABLE_MESSAGE_REF_BITS{continue;}\n", fn(f.Type.Name, "save_message_body"), g.keyedSlots(f))
	} else {
		g.pf("if %s[i]==%s {continue;}\n", g.keyedSlots(f), zeroScalar(f))
	}
}
func (g *gen) emitMessagePayload(f *ir.Field, entry ir.TableVocabularyEntry, expr, length, count string) {
	switch {
	case f.Type.Pointer && f.Array == ir.ArrayNone:
		g.emitMessageElement(f, entry.Kind, entry.Shape, expr)
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TWString:
		g.pf("w.bits.put(%s as u64,%d);\n", length, ir.TableMessageBitsRequired(0, entry.Shape.Max))
		if f.Type.Kind == ir.TWString {
			g.pf("for &unit in &(%s)[..%s as usize]{w.bits.put(unit as u64,16);}\n", expr, length)
		} else {
			g.pf("w.bits.align();w.bits.raw(&(%s)[..%s as usize]);\n", expr, length)
		}
	case f.Array != ir.ArrayNone:
		n := strconv.FormatInt(f.ArrayBound, 10)
		if f.Array == ir.ArrayCounted {
			n = count + " as usize"
		}
		g.pf("w.bits.put((%s as u64).wrapping_sub(%d),%d);\n", n, entry.Shape.Min, ir.TableMessageCountBits(entry.Shape))
		if entry.Shape.Elem == 6 {
			g.pf("w.bits.align();\n")
		}
		g.pf("for i in 0..%s {\n", n)
		g.emitMessageElement(f, entry.Shape.Elem, *entry.Shape.Inner, "("+expr+")[i]")
		g.pf("}\n")
	default:
		g.emitMessageElement(f, entry.Kind, entry.Shape, expr)
	}
}
func (g *gen) emitMessageElement(f *ir.Field, kind uint8, shape ir.TableMessageShape, expr string) {
	switch {
	case f.Type.Pointer:
		g.pf("if !w.putref(&(%s)){return false;}\n", expr)
	case isStruct(f):
		g.pf("if !%s(w,&(%s)){return false;}\n", fn(f.Type.Name, "save_message_body"), expr)
	case isUnion(f):
		g.pf("if !%s(w,&(%s)){return false;}\n", fn(f.Type.Name, "save_message_union"), expr)
	case isEnum(f):
		g.pf("match (%s).0 {0=>w.bits.put(0,TABLE_MESSAGE_REF_BITS),\n", expr)
		for i := range f.Type.Ref.(*ir.Enum).Variants {
			g.pf("%d=>w.bits.put(%d,TABLE_MESSAGE_REF_BITS),\n", i+1, g.messageName(ir.TableWireId(f.Type.Ref.(*ir.Enum).VariantWireName(i))))
		}
		g.pf("_=>return false,}\n")
	default:
		width := ir.TableMessageValueBits(kind, shape)
		switch kind {
		case 10:
			if shape.Packing == ir.TableMessageQuantized {
				count, delta, _ := ir.TableMessageQuantization(shape)
				g.pf("w.bits.put(table_message_quantize(%s,%s,%s,%d) as u64,%d);\n", expr, formatFloat(float64(shape.QMin), true), formatFloat(float64(delta), true), count, width)
			} else {
				g.pf("w.bits.put((%s).to_bits() as u64,32);\n", expr)
			}
		case 11:
			g.pf("w.bits.put((%s).to_bits(),64);\n", expr)
		default:
			base := new(big.Int)
			if shape.Packing == ir.TableMessageRanged && shape.Base != nil {
				base.Set(shape.Base)
			}
			base.Mod(base, new(big.Int).Lsh(big.NewInt(1), 128))
			g.pf("w.bits.put128((%s as u128).wrapping_sub(%su128),%d);\n", expr, base.String(), width)
		}
	}
}
func (g *gen) emitMessageUnion(un *ir.Union) {
	g.pf("pub fn %s(w:&mut TableMessageWriter,value:&%s)->bool {match value {\n%s::None=>w.bits.put(0,TABLE_MESSAGE_REF_BITS),\n", fn(un.Name, "save_message_union"), un.Name, un.Name)
	for _, v := range un.Variants {
		entry := ir.TableArmEntry(v)
		binding := "(arm)"
		if v.Void() {
			binding = ""
		}
		g.pf("%s::%s%s=>{w.bits.put(%d,TABLE_MESSAGE_REF_BITS);\n", un.Name, ir.GoExportName(v.Name), binding, g.messageSlot(entry))
		if !v.Void() {
			expr := "*arm"
			if armCounted(v.F) {
				expr = "arm.value"
				bound := v.F.Type.Size
				if v.F.Array != ir.ArrayNone {
					bound = v.F.ArrayBound
				}
				g.pf("if arm.length<0||arm.length>%d{return false;}\n", bound)
			}
			g.emitMessagePayload(v.F, entry, expr, "arm.length", "arm.length")
		}
		g.pf("},\n")
	}
	g.pf("} !w.bits.is_overflow()}\n")
}
