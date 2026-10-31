package javatable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func variableUnion(f *ir.Field) *ir.Union {
	if f.Type.Kind == ir.TNamed {
		if un, ok := f.Type.Ref.(*ir.Union); ok {
			return un
		}
	}
	return nil
}
func variableUnionStruct(un *ir.Union) *ir.Struct {
	st := &ir.Struct{Name: un.Name}
	for _, arm := range un.Variants {
		if !arm.Void() {
			one := *arm.F
			one.Name = arm.Name
			st.Fields = append(st.Fields, &one)
		}
	}
	mem := unionMembers(un)
	j := 0
	for k, arm := range un.Variants {
		if arm.Void() {
			continue
		}
		if st.Fields[j].Name != mem[k] {
			st.Fields[j].Name = mem[k]
		}
		j++
	}
	return variableStruct(st)
}

// unionMembers returns the Java member name for each variant of un, in variant
// order (empty for void variants). The discriminator reserves the member "tag"
// (retain.go, variablekeyed.go read it); an arm whose own wire name is "tag"
// takes the first free "tagArm<N>" instead, while its wire id and JSON key keep
// the arm's own name (SPEC-TABLES §2.6 union arms).
func unionMembers(un *ir.Union) []string {
	mem := make([]string, len(un.Variants))
	used := map[string]bool{"tag": true}
	for i, arm := range un.Variants {
		if arm.Void() {
			continue
		}
		cand := arm.Name
		if cand == "tag" {
			n := 1
			for {
				c := "tagArm"
				if n > 1 {
					c = fmt.Sprintf("tagArm%d", n)
				}
				if !used[c] {
					cand = c
					break
				}
				n++
			}
		}
		mem[i] = cand
		used[cand] = true
	}
	return mem
}

// An arm header is a field header plus an explicit payload extent. Only the
// selected arm is storage identity and only it participates in node numbering
// (SPEC-TABLES §2.6, §3, §3.1).
func generateVariableUnion(u *ir.Unit, un *ir.Union) string {
	st := variableUnionStruct(un)
	mem := unionMembers(un)
	var b strings.Builder
	b.WriteString("import java.nio.charset.StandardCharsets;\nimport java.util.ArrayList;\nimport java.util.List;\n")
	fmt.Fprintf(&b, "public final class %sVariable {private %sVariable(){}\n", un.Name, un.Name)
	writeVariableMapHelpers(&b, u, st)
	writeVariableKeyedHelpers(&b, u, st)
	b.WriteString("    public static final class Value {public int tag;\n")
	for _, f := range st.Fields {
		fmt.Fprintf(&b, "        public %s %s=%s;\n", variableValueType(f), varMember(f), variableDefault(f))
		if f.Type.Pointer {
			if f.Array != ir.ArrayNone {
				fmt.Fprintf(&b, "        public List<Long> %sIdx=new ArrayList<>();\n", varMember(f))
			} else {
				fmt.Fprintf(&b, "        public long %sIdx;\n", varMember(f))
			}
		}
	}
	b.WriteString("    }\n")
	b.WriteString("    static boolean buildGraph(TableVariable.NodeGraph g,Value v){if(v==null)return false;switch(v.tag){case 0:return true;\n")
	for i, arm := range un.Variants {
		fmt.Fprintf(&b, "        case %d:{\n", i+1)
		if !arm.Void() {
			one := *arm.F
			one.Name = mem[i]
			writeVariableGraphField(&b, &one)
		}
		b.WriteString("        return true;}\n")
	}
	b.WriteString("        default:return false;}}\n")
	b.WriteString("    static void resolveNodes(Value v,TableVariable.NodeTable nt,Object root,long rootTypeId,TableVariable.Report report){if(v==null)return;switch(v.tag){\n")
	for i, arm := range un.Variants {
		if !arm.Void() {
			fmt.Fprintf(&b, "        case %d:{\n", i+1)
			one := *arm.F
			one.Name = mem[i]
			writeVariableResolveField(&b, &one)
			b.WriteString("        break;}\n")
		}
	}
	b.WriteString("        default:break;}}\n")
	for _, retaining := range []bool{false, true} {
		suffix, args := "", ""
		if retaining {
			suffix = "Retain"
			args = ",TableRetain retain,TableVariable.Report report,int[] path,int depth"
		}
		fmt.Fprintf(&b, "    static void writePayload%s(TableVariable.Writer w,TableVariable.NodeGraph graph,Value v%s){switch(v.tag){case 0:w.putLeb(0);return;\n", suffix, args)
		for i, arm := range un.Variants {
			kind := 32
			if !arm.Void() {
				kind = ir.TableWireFieldKind(arm.F)
			}
			fmt.Fprintf(&b, "        case %d:{w.putLeb(w.ref(0x%016xL));w.put8(%d);\n", i+1, ir.TableWireId(arm.WireName()), kind)
			if arm.Void() {
				b.WriteString("        w.putLeb(0);return;}\n")
				continue
			}
			one := *arm.F
			one.Name = mem[i]
			b.WriteString("        int body=w.off;\n")
			if retaining {
				fmt.Fprintf(&b, "path[2*depth]=%d;path[2*depth+1]=0;\n", i)
				writeVariableRetainedPayload(&b, u, un.Name, &one, "v."+varMember(&one), "w", 0, "depth+1")
			} else {
				writeVariablePayload(&b, u, un.Name, &one, "v."+varMember(&one), "w", 0)
			}
			fmt.Fprintf(&b, "        w.frameBody(body,%s);return;}\n", variablePayloadStart(&one, "w", "body"))
		}
		b.WriteString("        default:return;}}\n")
	}
	for _, retaining := range []bool{false, true} {
		suffix, args := "", ""
		if retaining {
			suffix = "Retain"
			args = ",TableRetain retain,int[] path,int depth"
		}
		// A UNION FIELD'S HEADER DAMAGE IS NOT A NEW VALUE (docs/SPEC-TABLES.md
		// §3): the oracle's unionCell leaves the cell an earlier occurrence
		// decoded standing when the id reference, the arm id, the kind byte or
		// the arm's `L` cannot be read, and only the field's own None, unknown
		// or arm damage resets it. null says "leave the previous arm": the
		// field caller keeps it, an array element takes None (§3, §2.8).
		fmt.Fprintf(&b, "    static Value readPayload%s(TableVariable.Reader input,TableVariable.NodeTable nt,TableVariable.Report report%s){Value v=new Value();long ref=input.leb();if(input.fail)return null;if(ref==0)return v;long id=input.resolve(ref);if(input.fail)return null;if(!input.has(1)){report.malformed=true;input.fail=true;return null;}int kind=input.get8();TableVariable.Reader body=input.body();if(body==null)return null;\n", suffix, args)
		for i, arm := range un.Variants {
			fmt.Fprintf(&b, "        if(id==0x%016xL){\n", ir.TableWireId(arm.WireName()))
			if arm.Void() {
				fmt.Fprintf(&b, "        if(kind!=32){report.kindMismatch++;return v;}if(body.off!=body.limit){report.malformed=true;return v;}v.tag=%d;return v;}\n", i+1)
				continue
			}
			one := *arm.F
			one.Name = mem[i]
			expected := ir.TableWireFieldKind(&one)
			var condition strings.Builder
			fmt.Fprintf(&condition, "kind==%d", expected)
			widenKinds := []int{}
			for k := 1; k <= 33; k++ {
				if ir.TableKindWidens(k, expected) {
					fmt.Fprintf(&condition, "||kind==%d", k)
					widenKinds = append(widenKinds, k)
				}
			}
			fmt.Fprintf(&b, "        if(!(%s)){report.kindMismatch++;return v;}\n", condition.String())
			if len(widenKinds) > 0 {
				widthExpr := "0"
				for _, k := range widenKinds {
					widthExpr = fmt.Sprintf("(kind==%d?%d:%s)", k, ir.TableKindWidth(k), widthExpr)
				}
				fmt.Fprintf(&b, "        if(kind!=%d&&((body.limit-body.off)!=%s)){report.malformed=true;return v;}\n", expected, widthExpr)
			}
			b.WriteString("        TableVariable.Reader r=TableVariable.fieldPayload(body,kind);\n")
			directText := one.Array == ir.ArrayNone && (one.Type.Kind == ir.TString) && !one.Type.Pointer
			if directText {
				b.WriteString("TableVariable.Report parentReport=report;report=new TableVariable.Report();TableVariable.Reader textReader=new TableVariable.Reader(r.buf,r.ids,report);textReader.off=r.off;textReader.limit=r.limit;r=textReader;\n")
			}
			// AN ARRAY ARM IS SELECTED ONLY WHEN ITS HEADER READS AS ITS OWN
			// (the engine's arrayBody, §3): an element kind that neither is nor
			// widens to the declared one, or a damaged count, is that arm's own
			// damage, and the union reads None after the read has counted it.
			arrayArm := (one.Array != ir.ArrayNone || one.Type.Kind == ir.TBytes) && !one.Type.Pointer && !one.IsMap() && expected == 14
			if arrayArm {
				fmt.Fprintf(&b, "        boolean armOwn=true;{TableVariable.Reader probe=new TableVariable.Reader(r.buf,r.ids,new TableVariable.Report());probe.off=r.off;probe.limit=r.limit;TableVariable.Reader pa=probe.body();if(pa!=null&&pa.has(2)){int pk=pa.get8();%s;armOwn=(%s)&&!pa.fail;}}\n", countRead(&one, "pa"), variableCompatibleKinds(variableElemWireKind(&one), "pk"))
			}
			writeFieldRead(&b, u, st, &one, retaining, i, true)
			if arrayArm {
				b.WriteString("        if(!armOwn)return new Value();\n")
			}
			if directText {
				b.WriteString("parentReport.clamped+=report.clamped;parentReport.malformed|=report.malformed;if(report.malformed)return new Value();report=parentReport;\n")
			}
			// BYTES LEFT INSIDE A NESTED-UNION ARM'S `L` AFTER A WHOLE INNER
			// UNION ARE NOT DAMAGE (docs/SPEC-TABLES.md §3, the append rule):
			// the arm is stepped over by its `L` and nothing counts, as the Go
			// oracle reads it. Every other arm shape still consumes its `L`.
			_, nestedUnion := one.Type.Ref.(*ir.Union)
			nestedUnion = nestedUnion && one.Type.Kind == ir.TNamed && !one.Type.Pointer && one.Array == ir.ArrayNone
			if nestedUnion {
				fmt.Fprintf(&b, "        if(r.fail){report.malformed=true;return new Value();}v.tag=%d;return v;}\n", i+1)
			} else {
				fmt.Fprintf(&b, "        if(r.fail||r.off!=r.limit){report.malformed=true;return new Value();}v.tag=%d;return v;}\n", i+1)
			}
		}
		b.WriteString("        report.unknown++;")
		if retaining {
			b.WriteString("report.retainLost++;")
		}
		b.WriteString("return v;}\n")
	}
	b.WriteString("    static void jsonFields(StringBuilder b,TableVariable.NodeGraph graph,Value v,int depth){b.append('{');switch(v.tag){case 0:b.append('}');return;\n")
	for i, arm := range un.Variants {
		fmt.Fprintf(&b, "        case %d:{TableJson.indent(b,depth+1);TableJson.esc(b,%q);b.append(\": \");\n", i+1, arm.Name)
		if arm.Void() {
			b.WriteString("        b.append(\"null\");\n")
		} else {
			one := *arm.F
			one.Name = mem[i]
			b.WriteString("        {int nestedDepth=depth+1;\n")
			var child strings.Builder
			writeJsonFieldValue(&child, u, st, &one)
			b.WriteString(strings.ReplaceAll(child.String(), "depth", "nestedDepth"))
			b.WriteString("        }\n")
		}
		b.WriteString("        break;}\n")
	}
	b.WriteString("        default:break;}TableJson.indent(b,depth);b.append('}');}\n")
	b.WriteString("    static Value readJson(TableJson.In in){Value v=new Value();if(in.peek()!='{'){in.bad=true;return v;}in.p++;if(in.peek()=='}'){in.p++;return v;}String armName=in.scanString();if(in.bad)return v;if(in.peek()!=':'){in.bad=true;return v;}in.p++;\n")
	for i, arm := range un.Variants {
		fmt.Fprintf(&b, "        if(%q.equals(armName)){\n", arm.Name)
		if arm.Void() {
			fmt.Fprintf(&b, "        if(in.peek()=='n'){in.lit(\"null\");v.tag=%d;}else{in.skipValue();in.rep.kindMismatch++;}\n", i+1)
		} else {
			one := *arm.F
			one.Name = mem[i]
			shape := variableJsonShape(&one, "in.peek()")
			if one.Type.Pointer {
				shape = "(" + shape + "||in.peek()=='n')"
			}
			fmt.Fprintf(&b, "        if(%s){\n", shape)
			readJsonFieldValue(&b, u, st, &one)
			fmt.Fprintf(&b, "        if(!in.bad)v.tag=%d;}else{in.skipValue();in.rep.kindMismatch++;}\n", i+1)
		}
		b.WriteString("        }else ")
	}
	b.WriteString("{in.rep.unknown++;in.skipValue();}if(in.peek()==','){in.p++;}if(in.peek()!='}'){in.bad=true;return v;}in.p++;return v;}\n")
	// Enum payload helpers use the same ID-to-ordinal rule as table fields.
	for _, f := range st.Fields {
		if en, ok := f.Type.Ref.(*ir.Enum); ok {
			fmt.Fprintf(&b, "    static int %sEnum(long id,TableVariable.Report report){\n", varMember(f))
			for i := range en.Variants {
				fmt.Fprintf(&b, "        if(id==0x%016xL)return %d;\n", ir.TableWireId(en.VariantWireName(i)), i+1)
			}
			b.WriteString("        report.unknown++;return 0;}\n")
		}
	}
	b.WriteString("}\n")
	return b.String()
}

func writeUnionFieldRead(b *strings.Builder, u *ir.Unit, st *ir.Struct, f *ir.Field, retain bool, ordinal int) {
	un := variableUnion(f)
	m := varMember(f)
	method, args := "readPayload", ""
	if retain {
		method = "readPayloadRetain"
		args = ",retain,path,depth+1"
	}
	if f.Array == ir.ArrayNone {
		fmt.Fprintf(b, "if(kind==15){")
		if retain {
			fmt.Fprintf(b, "retain.discardField(path,depth,%d);path[2*depth]=%d;path[2*depth+1]=0;", ordinal, ordinal)
		}
		// A repeated field's last occurrence wins WHOLE, but a damaged occurrence
		// whose header never became an arm leaves the earlier arm standing
		// (docs/SPEC-TABLES.md §3, §4).
		fmt.Fprintf(b, "%sVariable.Value read=%sVariable.%s(r,nt,report%s);if(read!=null)v.%s=read;}else{r.skip(kind);report.kindMismatch++;}\n", un.Name, un.Name, method, args, m)
		return
	}
	// A BOUNDED or FIXED array of unions keeps its first bound elements and
	// counts one clamp, as every other bounded array does (docs/SPEC-TABLES.md
	// §3, §4); only an unbounded count past what a list can hold is refused.
	countCheck := "else if(count<0||count>0x7fffffffL){report.refused=true;report.reason=\"count_refusal\";}else{"
	if f.ArrayBound > 0 {
		countCheck = fmt.Sprintf("else{if(count<0||count>%d){count=%d;report.clamped++;}", f.ArrayBound, f.ArrayBound)
	}
	fmt.Fprintf(b, "if(kind==14){TableVariable.Reader ar=r.body();if(ar!=null&&ar.has(2)){int ek=ar.get8();long count=%s;if(!ar.fail){if(ek!=15){report.kindMismatch++;}%sv.%s.clear();", countRead(f, "ar"), countCheck, m)
	if retain {
		fmt.Fprintf(b, "retain.discardField(path,depth,%d);", ordinal)
	}
	b.WriteString("for(long i=0;i<count;i++){")
	if retain {
		fmt.Fprintf(b, "path[2*depth]=%d;path[2*depth+1]=(int)i;", ordinal)
	}
	// An ARRAY ELEMENT is reset the moment its arm id is read (§3, §4). A null
	// answer is an element whose header never read whole: a positional [N]
	// array keeps the slot at its default, and a counted or unbounded array
	// keeps only the prefix that decoded (the engine's arrayBody, §4).
	if f.Array == ir.ArrayFixed {
		fmt.Fprintf(b, "%sVariable.Value elem=%sVariable.%s(ar,nt,report%s);v.%s.add(elem==null?new %sVariable.Value():elem);if(elem==null||ar.fail)break;}}}}}else{r.skip(kind);report.kindMismatch++;}\n", un.Name, un.Name, method, args, m, un.Name)
		return
	}
	fmt.Fprintf(b, "%sVariable.Value elem=%sVariable.%s(ar,nt,report%s);if(elem==null)break;v.%s.add(elem);if(ar.fail)break;}}}}}else{r.skip(kind);report.kindMismatch++;}\n", un.Name, un.Name, method, args, m)

}

func writeUnionFieldSave(b *strings.Builder, u *ir.Unit, st *ir.Struct, f *ir.Field) {
	un := variableUnion(f)
	m := varMember(f)
	if f.Array == ir.ArrayNone {
		fmt.Fprintf(b, "        if(v.%s.tag!=0){w.putLeb(w.ref(F_%s));w.put8(15);%sVariable.writePayload(w,graph,v.%s);}\n", m, m, un.Name, m)
		return
	}
	fmt.Fprintf(b, "        if(!v.%s.isEmpty()){w.putLeb(w.ref(F_%s));w.put8(14);\n", m, m)
	writeVariablePayload(b, u, st.Name, f, "v."+m, "w", 0)
	b.WriteString("        }\n")
}
