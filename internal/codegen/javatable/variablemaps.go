package javatable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// On the variable wire a map is an array of its generated key/value table.
// Keep MapEntry on the copied field so ordering and text use map semantics;
// all ordinary table recursion can use the same typed element path.
func variableStruct(st *ir.Struct) *ir.Struct {
	copy := *st
	copy.Fields = make([]*ir.Field, len(st.Fields))
	for i, f := range st.Fields {
		if !f.IsMap() {
			copy.Fields[i] = f
			continue
		}
		one := *f
		one.Type = ir.FieldType{Kind: ir.TNamed, Name: f.MapEntry.Name, Ref: f.MapEntry}
		one.Array = ir.ArrayList
		copy.Fields[i] = &one
	}
	return &copy
}

func writeVariableMapHelpers(b *strings.Builder, u *ir.Unit, st *ir.Struct) {
	for _, f := range st.Fields {
		if !f.IsMap() {
			continue
		}
		m := varMember(f)
		entry := f.MapEntry.Name + "Variable.Value"
		key := ir.MapKeyField(f)
		compare := "Integer.compare(a.key,b.key)"
		switch {
		case key.Type.Kind == ir.TString:
			compare = "java.util.Arrays.compareUnsigned(a.key.getBytes(StandardCharsets.UTF_8),b.key.getBytes(StandardCharsets.UTF_8))"
		case key.Type.Width > 32:
			compare = "Long.compare(a.key,b.key)"
			if !key.Type.Signed {
				compare = "Long.compareUnsigned(a.key,b.key)"
			}
		case !key.Type.Signed:
			compare = "Integer.compareUnsigned(a.key,b.key)"
		}
		fmt.Fprintf(b, "    static int compare_%s(%s a, %s b) { return %s; }\n", m, entry, entry, compare)
		fmt.Fprintf(b, "    static List<%s> order_%s(List<%s> entries) {\n", entry, m, entry)
		fmt.Fprintf(b, "        java.util.TreeMap<%s,%s> ordered = new java.util.TreeMap<>(%sVariable::compare_%s);\n", entry, entry, st.Name, m)
		fmt.Fprintf(b, "        for (%s entry : entries) ordered.put(entry,entry);\n        return new ArrayList<>(ordered.values());\n    }\n", entry)
		fmt.Fprintf(b, "    static void jsonMapValue_%s(StringBuilder b, TableVariable.NodeGraph graph, %s v, int depth) {\n", m, entry)
		writeJsonFieldValue(b, u, f.MapEntry, variableStruct(f.MapEntry).Fields[1])
		b.WriteString("    }\n")
		fmt.Fprintf(b, "    static void readMapValue_%s(TableJson.In in, %s v) {\n", m, entry)
		readJsonFieldValue(b, u, f.MapEntry, variableStruct(f.MapEntry).Fields[1])
		b.WriteString("    }\n")

		rawKey := *key
		rawKey.HasIntRange = false
		if rawKey.Type.Kind == ir.TString {
			rawKey.Type.Size = 2147483647
		}
		fmt.Fprintf(b, "    static void readMapKey_%s(TableVariable.Reader r, %s v, TableVariable.Report report) {\n        while(true) {long ref=r.leb();if(r.fail||ref==0)return;long id=r.resolve(ref);if(r.fail||!r.has(1)){report.malformed=true;return;}int kind=r.get8();if(id==0x%016xL){\n", m, entry, ir.TableFieldWireId(key))
		// The key scan mirrors the oracle's mapKey (docs/SPEC-TABLES.md §2.8):
		// a key under another kind is SKIPPED and marked, never counted, and the
		// LAST occurrence decides the mark. A correct key clears it, so a bad
		// kind followed by a good one reads the good key and lets the body's
		// desynced tail fall out as malformed (the caller checks off!=limit);
		// a body whose last key is bad leaves the mark for one kind_mismatch.
		var keyRead strings.Builder
		writeFieldRead(&keyRead, u, f.MapEntry, &rawKey, false, 0, false)
		kr := keyRead.String()
		// A mis-read key marks report.kindMismatch; the LAST occurrence of the
		// key decides the map reset (§2.8). An exact-kind key clears the mark, a
		// wrong kind sets it, and a WIDENED key leaves it as it was — exactly as
		// the oracle's mapKey leaves kindBad untouched on the widened branch
		// (internal/tablewire/decode.go mapKey). The clearing is placed inside
		// the exact-kind read only, never in a widened branch.
		kr = strings.Replace(kr,
			"                } else { r.skip(kind); report.kindMismatch++; }",
			"                } else { r.skip(kind); report.kindMismatch = 1; }", 1)
		kr = strings.Replace(kr,
			"                else { r.skip(kind); report.kindMismatch++; }",
			"                else { r.skip(kind); report.kindMismatch = 1; }", 1)
		kr = strings.Replace(kr,
			fmt.Sprintf("if (kind == %d) {", variableWireKind(&rawKey)),
			fmt.Sprintf("if (kind == %d) { report.kindMismatch = 0;", variableWireKind(&rawKey)), 1)
		b.WriteString(kr)
		// A KEY THE READ JUDGES MALFORMED ENDS THE SCAN (§2.8; the oracle's
		// mapKey returns at the first key its string read refuses): the map is
		// malformed, a damaged load, and no later occurrence of the key can
		// turn it into a kind mismatch.
		b.WriteString("        }else{r.skip(kind);}if(r.fail||report.malformed)return;}\n    }\n")
	}
}

// Text maps use JSON object keys and the entry value's own ordinary codec
// (SPEC-TABLES §2.8, §16.2). A duplicate replaces the complete value.
func writeJsonMapValue(b *strings.Builder, u *ir.Unit, st *ir.Struct, f *ir.Field) {
	m := varMember(f)
	entry := f.MapEntry.Name + "Variable.Value"
	key := ir.MapKeyField(f)
	fmt.Fprintf(b, "        { List<%s> entries = "+st.Name+"Variable.order_%s(v.%s); b.append('{'); for(int i=0;i<entries.size();i++) { if(i>0)b.append(','); TableJson.indent(b,depth+1); %s entry=entries.get(i);\n", entry, m, m, entry)
	keyText := "Integer.toString(entry.key)"
	switch {
	case key.Type.Kind == ir.TString:
		keyText = "entry.key"
	case key.Type.Width > 32:
		keyText = "Long.toString(entry.key)"
		if !key.Type.Signed {
			keyText = "Long.toUnsignedString(entry.key)"
		}
	case !key.Type.Signed:
		keyText = "Integer.toUnsignedString(entry.key)"
	}
	fmt.Fprintf(b, "        TableJson.esc(b,%s); b.append(\": \");\n", keyText)
	fmt.Fprintf(b, "        %sVariable.jsonMapValue_%s(b,graph,entry,depth+1);\n", st.Name, m)
	b.WriteString("        } if(!entries.isEmpty())TableJson.indent(b,depth); b.append('}'); }\n")
}

func readJsonMapValue(b *strings.Builder, u *ir.Unit, st *ir.Struct, f *ir.Field) {
	m := varMember(f)
	entry := f.MapEntry.Name + "Variable.Value"
	key := ir.MapKeyField(f)
	fmt.Fprintf(b, "                if(in.peek()!='{'){in.bad=true;return;} in.p++; v.%s.clear();\n", m)
	fmt.Fprintf(b, "                while(in.peek()!='}') { if(in.peek()==0){in.bad=true;return;} String mapKey=in.scanStored(); if(in.bad)return; if(in.peek()!=':'){in.bad=true;return;} in.p++; %s entry=new %s(); boolean accepted=true;\n", entry, entry)
	if key.Type.Kind == ir.TString {
		fmt.Fprintf(b, "                    entry.key=mapKey; if(mapKey.getBytes(StandardCharsets.UTF_8).length>%d){accepted=false;in.rep.clamped++;}\n", key.Type.Size)
	} else {
		// A key is parsed at arbitrary precision before narrowing: unsigned 64-bit
		// magnitudes must never pass through signed-long range checks (§16.1).
		signed := key.Type.Signed
		min, max := "java.math.BigInteger.ZERO", fmt.Sprintf("java.math.BigInteger.ONE.shiftLeft(%d).subtract(java.math.BigInteger.ONE)", key.Type.Width)
		if signed {
			min = fmt.Sprintf("java.math.BigInteger.ONE.shiftLeft(%d).negate()", key.Type.Width-1)
			max = fmt.Sprintf("java.math.BigInteger.ONE.shiftLeft(%d).subtract(java.math.BigInteger.ONE)", key.Type.Width-1)
		}
		cast := "intValue"
		if key.Type.Width > 32 {
			cast = "longValue"
		}
		fmt.Fprintf(b, "                    TableJson.In number=new TableJson.In(mapKey.getBytes(StandardCharsets.UTF_8),new TableVariable.Report()); String token=number.scanNumber(new boolean[1]); if(number.bad||number.p!=number.t.length||mapKey.isEmpty()||Character.isWhitespace(mapKey.charAt(0))){in.bad=true;return;} try {java.math.BigInteger n=new java.math.BigDecimal(token).toBigIntegerExact(); if(n.compareTo(%s)<0||n.compareTo(%s)>0){accepted=false;in.rep.kindMismatch++;}else entry.key=n.%s();}catch(ArithmeticException|NumberFormatException invalid){accepted=false;in.rep.kindMismatch++;}\n", min, max, cast)
	}
	b.WriteString("                    if(!accepted){in.skipValue();}else {\n")
	fmt.Fprintf(b, "                    %sVariable.readMapValue_%s(in,entry);\n", st.Name, m)
	fmt.Fprintf(b, "                    if(in.bad)return; int found=-1;for(int i=0;i<v.%s.size();i++)if("+st.Name+"Variable.compare_%s(v.%s.get(i),entry)==0){found=i;break;} if(found>=0){v.%s.set(found,entry);in.rep.duplicate++;}else v.%s.add(entry); }\n", m, m, m, m, m)
	b.WriteString("                    int separator=in.peek();if(separator==','){in.p++;continue;}if(separator=='}')break;in.bad=true;return; } in.p++;\n")
}

// A skipped repeat preserves its earlier map; an accepted header replaces it.
// Key order is checked before decoding the value, retaining the ascending
// prefix on damage and resuming the parent at the map body's end (§2.8, §4).
// A header whose ELEMENT KIND is not a table's is the ordinary array kind
// mismatch (§4), and its count is read against the ENCLOSING body as the
// engine's mapField reads it, so a count spelled past the map's own L still
// leaves the mismatch counted. A count whose entries cannot fit the body (each
// entry is at least two bytes) is refused, never read as a short map: the oracle's LoadMeasure
// answers count_over_length for it (§2.9, §6.5).
// An entry whose VALUE body is damaged still stands with what decoded before
// the damage: a malformed value is the entry's own field event, never a map
// reset (§2.8: "the value reads its default, the entry stays"), so the entry
// is kept whole rather than reset to a fresh default.
func writeMapFieldRead(b *strings.Builder, u *ir.Unit, st *ir.Struct, f *ir.Field, retain bool, ordinal int) {
	m := varMember(f)
	entry := f.MapEntry.Name + "Variable.Value"
	fmt.Fprintf(b, "                if(kind==14){TableVariable.Reader ar=r.body();if(ar!=null&&ar.has(2)){int ek=ar.get8();long n=ek!=13?ar.count(r.limit):ar.leb();if(!ar.fail){if(ek!=13){report.kindMismatch++;}else if(n<0||n>0x7fffffffL){report.refused=true;report.reason=\"count_refusal\";}else if(n>(long)(ar.limit-ar.off)/2){report.refused=true;}else {\n")
	if retain {
		fmt.Fprintf(b, "                    retain.discardField(path,depth,%d);\n", ordinal)
	}
	fmt.Fprintf(b, "                    v.%s.clear(); boolean keyWidened=false; for(long i=0;i<n;i++){TableVariable.Reader er=ar.body();if(er==null)break;%s keyValue=new %s();TableVariable.Report keyReport=new TableVariable.Report();TableVariable.Reader kr=new TableVariable.Reader(er.buf,er.ids,keyReport);kr.off=er.off;kr.limit=er.limit;"+st.Name+"Variable.readMapKey_%s(kr,keyValue,keyReport);\n", m, entry, entry, m)
	// A KEY KIND the declaration WIDENS counts for the map at the first key
	// that carries it (§2.8, §4: "a widened counts for the map"), BEFORE the
	// scan's framing or a later key's mismatch is judged. The oracle counts it
	// in mapField as soon as mapKey reports the widened kind, even where the
	// same key scan then flags malformed: java-fix-map-widened-count.
	// A KEY KIND the reader does not declare EMPTIES the map for one
	// kind_mismatch (§2.8), and that is not one of the five occurrences that
	// discard retained records (§6.6, record lifetime): the records held under
	// the entries read before it keep their paths, and the save counts each one
	// retain_lost when its path names no body.
	fmt.Fprintf(b, "                    if(keyReport.widened>0&&!keyWidened){report.widened++;keyWidened=true;}if(keyReport.kindMismatch>0){report.kindMismatch++;v.%s.clear();break;}if(keyReport.malformed||kr.fail||kr.off!=kr.limit){report.malformed=true;break;}\n", m)
	key := ir.MapKeyField(f)
	if key.Type.Kind == ir.TString {
		fmt.Fprintf(b, "                    if(keyValue.key.getBytes(StandardCharsets.UTF_8).length>%d){report.clamped++;continue;}\n", key.Type.Size)
	}
	fmt.Fprintf(b, "                    int order=v.%s.isEmpty()?-1:"+st.Name+"Variable.compare_%s(v.%s.get(v.%s.size()-1),keyValue);if(order>0){report.malformed=true;break;} %s decoded=new %s();int priorWidened=report.widened;\n", m, m, m, m, entry, entry)
	if retain {
		fmt.Fprintf(b, "                    path[2*depth]=%d;path[2*depth+1]=v.%s.size()-(order==0?1:0);if(order==0)retain.discardPath(path,depth+1);%sRetain.readFieldsRetain(er,decoded,nt,retain,report,path,depth+1);\n", ordinal, m, f.MapEntry.Name)
	} else {
		fmt.Fprintf(b, "                    %sVariable.readFields(er,decoded,nt,report);\n", f.MapEntry.Name)
	}
	fmt.Fprintf(b, "                    report.widened=priorWidened+Math.max(0,report.widened-priorWidened-keyReport.widened);if(er.off!=er.limit){report.malformed=true;}if(order==0){v.%s.set(v.%s.size()-1,decoded);report.duplicate++;}else v.%s.add(decoded);\n", m, m, m)
	b.WriteString("                    } } } }}else{r.skip(kind);report.kindMismatch++;}\n")
}
