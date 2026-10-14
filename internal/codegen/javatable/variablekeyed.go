package javatable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func variableElement(f *ir.Field) *ir.Field {
	one := *f
	one.Array = ir.ArrayNone
	one.ArrayBound = 0
	one.KeyEnum = ""
	one.KeyEnumRef = nil
	one.Type.Optional = false
	return &one
}

func variableDefaultTest(f *ir.Field, expr string) string {
	if f.Array != ir.ArrayNone || f.IsMap() {
		return expr + ".isEmpty()"
	}
	if f.Type.Pointer {
		return expr + "==null"
	}
	switch f.Type.Kind {
	case ir.TString:
		return expr + ".equals(" + variableDefault(f) + ")"
	case ir.TWString, ir.TBytes:
		return expr + ".length==0"
	case ir.TNamed:
		switch f.Type.Ref.(type) {
		case *ir.Struct:
			return f.Type.Name + "Variable.isDefault(" + expr + ")"
		case *ir.Union:
			return expr + ".tag==0"
		}
	}
	return expr + "==" + variableDefault(f)
}

// Each keyed slot is addressed by its variant's wire ID, never its position
// in the writer's enum (§2.4, §3.2). The generated slot adapter reuses the
// ordinary scalar/table codec and keeps the holder's typed List API.
func writeVariableKeyedHelpers(b *strings.Builder, u *ir.Unit, st *ir.Struct) {
	for _, f := range st.Fields {
		if f.KeyEnum == "" {
			continue
		}
		m := varMember(f)
		one := variableElement(f)
		one.Name = "value"
		typ := variableValueType(one)
		boxed := variableListElemType(f)
		if isStruct(f) || one.Type.Pointer {
			boxed = typ
		}
		fmt.Fprintf(b, "    static final class Slot_%s { %s value=%s;long valueIdx; }\n", m, typ, variableDefault(one))
		fmt.Fprintf(b, "    static void readSlot_%s(TableVariable.Reader r,Slot_%s v,int kind,TableVariable.NodeTable nt,TableVariable.Report report){\n", m, m)
		writeFieldRead(b, u, st, one, false, 0)
		b.WriteString("    }\n")
		fmt.Fprintf(b, "    static void jsonSlot_%s(StringBuilder b,TableVariable.NodeGraph graph,Slot_%s v,int depth){\n", m, m)
		writeJsonFieldValue(b, u, st, one)
		b.WriteString("    }\n")
		fmt.Fprintf(b, "    static void readJsonSlot_%s(TableJson.In in,Slot_%s v){\n", m, m)
		readJsonFieldValue(b, u, st, one)
		b.WriteString("    }\n")
		fmt.Fprintf(b, "    static int writeKeyedPayload_%s(TableVariable.Writer w,TableVariable.NodeGraph graph,List<%s> slots){TableVariable.Writer entries=w.sub(new byte[1<<20]);int count=0;\n", m, boxed)
		for i := range f.KeyEnumRef.Variants {
			expr := fmt.Sprintf("slots.get(%d)", i)
			fmt.Fprintf(b, "        if(slots.size()>%d&&!(%s)){long key=entries.ref(0x%016xL);TableVariable.Writer body=entries.sub(new byte[1<<20]);\n", i, variableDefaultTest(one, expr), ir.TableWireId(f.KeyEnumRef.VariantWireName(i)))
			writeVariablePayload(b, u, st.Name, one, expr, "body", 0)
			fmt.Fprintf(b, "        int start=%s;entries.putLeb(key);entries.putLeb(body.off-start);entries.putBytes(body.buf,start,body.off-start);count++;}\n", variablePayloadStart(one, "body"))
		}
		fmt.Fprintf(b, "        w.putLeb(1+TableVariable.lebLen(count)+entries.off);w.put8(%d);w.putLeb(count);w.putBytes(entries.buf,0,entries.off);return count;}\n", ir.TableWireElemKind(f))
	}
}

func variablePayloadStart(f *ir.Field, w string) string {
	switch ir.TableWireFieldKind(f) {
	case 12, 13, 14, 16, 33:
		return "(int)TableVariable.lebAt(" + w + ".buf,0," + w + ".off)[1]"
	}
	return "0"
}

func writeKeyedFieldRead(b *strings.Builder, u *ir.Unit, st *ir.Struct, f *ir.Field) {
	m := varMember(f)
	one := variableElement(f)
	kind := ir.TableWireElemKind(f)
	fmt.Fprintf(b, "                if(kind==16){TableVariable.Reader ar=r.body();if(ar!=null&&ar.has(2)){int ek=ar.get8();long count=ar.leb();if(!ar.fail){if(ek!=%d){report.kindMismatch++;}else{for(long i=0;i<count;i++){long keyRef=ar.leb();if(ar.fail||keyRef==0){report.malformed=true;break;}long key=ar.resolve(keyRef);TableVariable.Reader body=ar.body();if(body==null)break;int slot=-1;\n", kind)
	for i := range f.KeyEnumRef.Variants {
		fmt.Fprintf(b, "                    if(key==0x%016xL)slot=%d;\n", ir.TableWireId(f.KeyEnumRef.VariantWireName(i)), i)
	}
	fmt.Fprintf(b, "                    if(slot<0){report.unknown++;continue;}%sVariable.Slot_%s value=new %sVariable.Slot_%s();TableVariable.Reader payload=TableVariable.fieldPayload(body,ek);%sVariable.readSlot_%s(payload,value,ek,nt,report);if(payload.off!=payload.limit){report.malformed=true;value=new %sVariable.Slot_%s();}v.%s.set(slot,value.value);\n", st.Name, m, st.Name, m, st.Name, m, st.Name, m, m)
	if one.Type.Pointer {
		fmt.Fprintf(b, "                    v.%sIdx.set(slot,value.valueIdx);\n", m)
	}
	b.WriteString("                    }}}}}else{r.skip(kind);report.kindMismatch++;}\n")
}

func writeJsonKeyedValue(b *strings.Builder, u *ir.Unit, st *ir.Struct, f *ir.Field) {
	m := varMember(f)
	b.WriteString("        b.append('{');\n")
	for i, name := range f.KeyEnumRef.Variants {
		if i > 0 {
			b.WriteString("        b.append(',');\n")
		}
		fmt.Fprintf(b, "        TableJson.indent(b,depth+1);TableJson.esc(b,%q);b.append(\": \");{"+st.Name+"Variable.Slot_%s slot=new "+st.Name+"Variable.Slot_%s();slot.value=v.%s.get(%d);"+st.Name+"Variable.jsonSlot_%s(b,graph,slot,depth+1);}\n", name, m, m, m, i, m)
	}
	b.WriteString("        TableJson.indent(b,depth);b.append('}');\n")
}

func readJsonKeyedValue(b *strings.Builder, u *ir.Unit, st *ir.Struct, f *ir.Field) {
	m := varMember(f)
	one := variableElement(f)
	fmt.Fprintf(b, "                if(in.peek()!='{'){in.bad=true;return;}in.p++;for(int i=0;i<%d;i++)v.%s.set(i,%s);boolean[] seen=new boolean[%d];while(in.peek()!='}'){String slotName=in.scanString();if(in.bad)return;if(in.peek()!=':'){in.bad=true;return;}in.p++;int slot=-1;\n", f.ArrayBound, m, variableDefault(one), f.ArrayBound)
	for i, name := range f.KeyEnumRef.Variants {
		fmt.Fprintf(b, "                if(%q.equals(slotName))slot=%d;\n", name, i)
	}
	fmt.Fprintf(b, "                if(slot<0){in.rep.unknown++;in.skipValue();}else{if(seen[slot])in.rep.duplicate++;seen[slot]=true;"+st.Name+"Variable.Slot_%s value=new "+st.Name+"Variable.Slot_%s();value.value=v.%s.get(slot);"+st.Name+"Variable.readJsonSlot_%s(in,value);v.%s.set(slot,value.value);}int separator=in.peek();if(separator==','){in.p++;continue;}if(separator=='}')break;in.bad=true;return;}in.p++;\n", m, m, m, m, m)
}
