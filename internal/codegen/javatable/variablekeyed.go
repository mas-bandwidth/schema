package javatable

import (
	"fmt"
	"math"
	"strconv"
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
	if variableWide(f) && f.Type.Width > 64 {
		return expr + ".equals(" + variableWideDefault(f) + ")"
	}
	if f.Array != ir.ArrayNone || f.IsMap() {
		return expr + ".isEmpty()"
	}
	if f.Type.Pointer {
		return expr + "==null"
	}
	switch f.Type.Kind {
	case ir.TString:
		return expr + ".equals(" + variableDefault(f) + ")"
	case ir.TWString:
		return expr + ".length==0"
	case ir.TBytes:
		return "java.util.Arrays.equals(" + expr + "," + variableDefault(f) + ")"
	case ir.TFloat32:
		// A FLOAT IS ITS DEFAULT BY ITS BITS (docs/SPEC-TABLES.md §3.3): a -0
		// under a +0 default rides. The default's bits are a constant, parsed
		// from the same literal javac reads, so the test stays one short line.
		def := variableDefault(f)
		if v, err := strconv.ParseFloat(strings.TrimSuffix(def, "f"), 32); err == nil {
			return fmt.Sprintf("Float.floatToRawIntBits(%s)==0x%08x", expr, math.Float32bits(float32(v)))
		}
		return "Float.floatToRawIntBits(" + expr + ")==Float.floatToRawIntBits(" + def + ")"
	case ir.TFloat64:
		def := variableDefault(f)
		if v, err := strconv.ParseFloat(strings.TrimSuffix(def, "d"), 64); err == nil {
			return fmt.Sprintf("Double.doubleToRawLongBits(%s)==0x%016xL", expr, math.Float64bits(v))
		}
		return "Double.doubleToRawLongBits(" + expr + ")==Double.doubleToRawLongBits(" + def + ")"
	case ir.TNamed:
		switch f.Type.Ref.(type) {
		case *ir.Struct:
			return f.Type.Name + "Variable.isDefault(" + expr + ",w,graph)"
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
		typ := variableValueType(one)
		boxed := variableListElemType(f)
		if isStruct(f) || one.Type.Pointer {
			boxed = typ
		}
		fmt.Fprintf(b, "    static final class Slot_%s { %s %s=%s;long %sIdx; }\n", m, typ, m, variableDefault(one), m)
		fmt.Fprintf(b, "    static void readSlot_%s(TableVariable.Reader r,Slot_%s v,int kind,TableVariable.NodeTable nt,TableVariable.Report report){\n", m, m)
		writeFieldRead(b, u, st, one, false, 0, false)
		b.WriteString("    }\n")
		fmt.Fprintf(b, "    static void readSlotRetain_%s(TableVariable.Reader r,Slot_%s v,int kind,TableVariable.NodeTable nt,TableVariable.Report report,TableRetain retain,int[] path,int depth){\n", m, m)
		writeFieldRead(b, u, st, one, true, 0, false)
		b.WriteString("    }\n")
		fmt.Fprintf(b, "    static void jsonSlot_%s(StringBuilder b,TableVariable.NodeGraph graph,Slot_%s v,int depth){\n", m, m)
		writeJsonFieldValue(b, u, st, one)
		b.WriteString("    }\n")
		fmt.Fprintf(b, "    static void readJsonSlot_%s(TableJson.In in,Slot_%s v){\n", m, m)
		readJsonFieldValue(b, u, st, one)
		b.WriteString("    }\n")
		fmt.Fprintf(b, "    static void writeKeyedField_%s(TableVariable.Writer w,TableVariable.NodeGraph graph,List<%s> slots){int at=w.off;int mark=w.ids.count;long ref=w.ref(F_%s);w.putLeb(ref);w.put8(16);int count=writeKeyedPayload_%s(w,graph,slots);if(count==0){w.off=at;w.ids.count=mark;}}\n", m, boxed, m, m)
		for _, retaining := range []bool{false, true} {
			suffix, args := "", ""
			if retaining {
				suffix = "Retain"
				args = ",TableRetain retain,TableVariable.Report report,int[] path,int depth"
			}
			fmt.Fprintf(b, "    static int writeKeyedPayload%s_%s(TableVariable.Writer w,TableVariable.NodeGraph graph,List<%s> slots%s){int entries=w.off;int count=0;\n", suffix, m, boxed, args)
			for i := range f.KeyEnumRef.Variants {
				expr := fmt.Sprintf("slots.get(%d)", i)
				defaultTest := variableDefaultTest(one, expr)
				retainedBody := retaining && isStruct(one) && !one.Type.Pointer
				if retainedBody {
					defaultTest = "false"
				}
				fmt.Fprintf(b, "        if(slots.size()>%d&&!(%s)){int idsMark=w.ids.count;long key=w.ref(0x%016xL);int body=w.off;\n", i, defaultTest, ir.TableWireId(f.KeyEnumRef.VariantWireName(i)))
				if retaining {
					fmt.Fprintf(b, "path[2*depth]=0;path[2*depth+1]=0;\n")
					// The caller assigns the holder field and slot at depth-1.
					fmt.Fprintf(b, "path[2*(depth-1)+1]=%d;\n", i)
					writeVariableRetainedPayload(b, u, st.Name, one, expr, "w", 0, "depth+1")
				} else {
					writeVariablePayload(b, u, st.Name, one, expr, "w", 0)
				}
				if retainedBody {
					// the slot's payload is leb(length) then the body, and an
					// EMPTY body — a default element with no retained tail — is
					// the one terminator byte, two bytes framed: elided, as the
					// plain write elides a default slot (§2.4, §6.6)
					b.WriteString("if(w.off>body+2){")
				}
				fmt.Fprintf(b, "        w.frameEntry(body,key,%s);count++;", variablePayloadStart(one, "w", "body"))
				if retainedBody {
					b.WriteString("}else{w.off=body;w.ids.count=idsMark;}")
				}
				b.WriteString("}\n")
			}
			fmt.Fprintf(b, "        w.frameKeyed(entries,%d,count);return count;}\n", ir.TableWireElemKind(f))
		}
	}
}

func variablePayloadStart(f *ir.Field, w, start string) string {
	switch ir.TableWireFieldKind(f) {
	case 12, 13, 14, 16, 33:
		return "TableVariable.lebEnd(" + w + ".buf," + start + "," + w + ".off)-" + start
	}
	return "0"
}

func writeKeyedFieldRead(b *strings.Builder, u *ir.Unit, st *ir.Struct, f *ir.Field, retain bool, ordinal int) {
	m := varMember(f)
	one := variableElement(f)
	kind := ir.TableWireElemKind(f)
	fmt.Fprintf(b, "                if(kind==16){TableVariable.Reader ar=r.body();if(ar!=null&&ar.has(2)){int ek=ar.get8();long count=ar.leb();if(!ar.fail){if(!(%s)){report.kindMismatch++;}else{if(ek!=%d)report.widened++;for(long i=0;Long.compareUnsigned(i,count)<0;i++){long keyRef=ar.leb();if(ar.fail||keyRef==0){report.malformed=true;break;}long key=ar.resolve(keyRef);TableVariable.Reader body=ar.body();if(body==null)break;int slot=-1;\n", variableCompatibleKinds(kind, "ek"), kind)
	for i := range f.KeyEnumRef.Variants {
		fmt.Fprintf(b, "                    if(key==0x%016xL)slot=%d;\n", ir.TableWireId(f.KeyEnumRef.VariantWireName(i)), i)
	}
	lost := ""
	method := "readSlot_"
	args := ""
	if retain {
		lost = "report.retainLost++;"
		method = "readSlotRetain_"
		args = ",retain,path,depth+1"
	}
	fmt.Fprintf(b, "                    if(slot<0){report.unknown++;%s continue;}\n", lost)
	if retain {
		fmt.Fprintf(b, "path[2*depth]=%d;path[2*depth+1]=slot;retain.discardPath(path,depth+1);\n", ordinal)
	}
	// In internal/tablewire/decode.go (keyed), the Go oracle counts ONE widened
	// for the field, decodes each scalar or enum slot at the wire kind's width
	// and skips the slot by its L; only a table slot whose body stops short of
	// L is malformed (docs/SPEC-TABLES.md §3.2, §4).
	slack := ""
	if isStruct(one) && !one.Type.Pointer {
		slack = fmt.Sprintf("if(payload.off!=payload.limit){report.malformed=true;value=new %sVariable.Slot_%s();}", st.Name, m)
	}
	fmt.Fprintf(b, "%sVariable.Slot_%s value=new %sVariable.Slot_%s();TableVariable.Reader payload=TableVariable.fieldPayload(body,ek);int beforeWidened=report.widened;%sVariable.%s%s(payload,value,ek,nt,report%s);if(ek!=%d)report.widened=beforeWidened;%sv.%s.set(slot,value.%s);\n", st.Name, m, st.Name, m, st.Name, method, m, args, kind, slack, m, m)

	if one.Type.Pointer {
		fmt.Fprintf(b, "                    v.%sIdx.set(slot,value.%sIdx);\n", m, m)
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
		fmt.Fprintf(b, "        TableJson.indent(b,depth+1);TableJson.esc(b,%q);b.append(\": \");{"+st.Name+"Variable.Slot_%s slot=new "+st.Name+"Variable.Slot_%s();slot.%s=v.%s.get(%d);"+st.Name+"Variable.jsonSlot_%s(b,graph,slot,depth+1);}\n", name, m, m, m, m, i, m)
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
	fmt.Fprintf(b, "                if(slot<0){in.rep.unknown++;in.skipValue();}else{if(seen[slot])in.rep.duplicate++;seen[slot]=true;"+st.Name+"Variable.Slot_%s value=new "+st.Name+"Variable.Slot_%s();value.%s=v.%s.get(slot);"+st.Name+"Variable.readJsonSlot_%s(in,value);v.%s.set(slot,value.%s);}int separator=in.peek();if(separator==','){in.p++;continue;}if(separator=='}')break;in.bad=true;return;}in.p++;\n", m, m, m, m, m, m, m)
}
