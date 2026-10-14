package darttable

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func (g *wireGen) keyedEnums() []*ir.Enum {
	byName := map[string]*ir.Enum{}
	for _, st := range g.closure {
		for _, f := range st.Fields {
			if f.KeyEnum != "" {
				byName[f.KeyEnum] = f.KeyEnumRef
			}
		}
	}
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]*ir.Enum, 0, len(names))
	for _, name := range names {
		out = append(out, byName[name])
	}
	return out
}
func keyedFactory(f *ir.Field) string { return "for" + ir.GoExportName(f.KeyEnum) }
func (g *wireGen) keyedDefault(f *ir.Field) string {
	switch {
	case tableRef(f) != nil:
		return f.Type.Name + "()"
	case enumRef(f) != nil:
		return g.enumDefault(f)
	case f.Type.Kind == ir.TBool:
		if f.HasDefault && f.DefBool {
			return "true"
		}
		return "false"
	case f.Type.Kind == ir.TFloat32 || f.Type.Kind == ir.TFloat64:
		return g.floatDefault(f)
	default:
		return g.intDefault(f)
	}
}
func (g *wireGen) keyedConstruct(f *ir.Field) string {
	return fmt.Sprintf("TableKeyed<%s>.%s(()=>%s)", dartListElemType(f), keyedFactory(f), g.keyedDefault(f))
}
func (g *wireGen) keyedRuntime() []byte {
	var b strings.Builder
	b.WriteString("// Enum-keyed authoring storage (docs/SPEC-TABLES.md §2.4).\nfinal class TableKeyed<T> {\n  final List<T> _slots;\n")
	for _, e := range g.keyedEnums() {
		fmt.Fprintf(&b, "  TableKeyed.for%s(T Function() make):_slots=List<T>.generate(%d,(_)=>make());\n", ir.GoExportName(e.Name), e.Max)
	}
	b.WriteString(`  T operator [](int key){
    if(key<1||key>_slots.length)throw RangeError('unknown keyed-array variant $key');
    return _slots[key-1];
  }
  void operator []=(int key,T value){
    if(key<1||key>_slots.length)throw RangeError('unknown keyed-array variant $key');
    _slots[key-1]=value;
  }
  Iterable<(int,T)> get entries sync* {
    for(var i=0;i<_slots.length;i++){yield(i+1,_slots[i]);}
  }
}
`)
	return []byte(b.String())
}
func (g *wireGen) emitKeyedBareWrite(f *ir.Field, expr, writer, ind string) {
	kind := listElementKind(f)
	switch kind {
	case wireKindTable:
		g.pf("%s%sWireSaveBody(%s,%s);\n", ind, dartName(f.Type.Name), writer, expr)
	case wireKindBool:
		g.pf("%s%s.putByte(%s?1:0);\n", ind, writer, expr)
	case wireKindF32:
		g.pf("%s%s.putU32(_f32Bits(%s));\n", ind, writer, expr)
	case wireKindF64:
		g.pf("%s%s.putU64(_f64Bits(%s));\n", ind, writer, expr)
	case wireKindEnum:
		e := enumRef(f)
		g.pf("%svar variant=0;switch(%s){case 0:break;\n", ind, expr)
		for i := range e.Variants {
			g.pf("%scase %d:variant=%s;break;\n", ind, i+1, dartU64Literal(ir.TableWireId(e.VariantWireName(i))))
		}
		g.pf("%sdefault:throw StateError('invalid_value');}\n%s%s.putLeb(variant==0?0:%s.refFor(variant));\n", ind, ind, writer, writer)
	default:
		if wireWide(f) {
			g.emitWideBareWrite(f, expr, writer, ind)
			break
		}
		put := "putU64"
		switch ir.TableKindWidth(kind) {
		case 1:
			put = "putByte"
		case 2:
			put = "putU16"
		case 4:
			put = "putU32"
		}
		g.pf("%s%s.%s(%s);\n", ind, writer, put, expr)
	}
}
func (g *wireGen) emitSaveKeyed(f *ir.Field, ind string, ordinal int) {
	n := fieldMember(f)
	g.pf("%s{final fieldMark=w.state.mark();final ref=w.refFor(%s);final rows=w.sub();var count=0;\n", ind, dartU64Literal(fieldId(f)))
	g.pf("%s for(final slot in v.%s.entries){\n%s  final mark=w.state.mark();var keyId=0;switch(slot.$1){\n", ind, n, ind)
	for i := range f.KeyEnumRef.Variants {
		g.pf("%s case %d:keyId=%s;break;\n", ind, i+1, dartU64Literal(ir.TableWireId(f.KeyEnumRef.VariantWireName(i))))
	}
	g.pf("%s default:throw StateError('invalid_value');}\n%s final key=rows.refFor(keyId);final body=rows.sub()..path=[...w.path,%d,slot.$1-1];\n", ind, ind, ordinal)
	g.emitKeyedBareWrite(f, "slot.$2", "body", ind+" ")
	cond := "slot.$2 == " + g.keyedDefault(f)
	if tableRef(f) != nil {
		cond = "body.buf.length<=1"
	}
	g.pf("%s if(%s){w.state.rollback(mark);continue;}\n%s rows.putLeb(key);rows.putLeb(body.buf.length);rows.putBytes(body.buf);count++;\n%s }\n", ind, cond, ind, ind)
	g.pf("%s if(count==0){w.state.rollback(fieldMark);}else{final body=w.sub()..putByte(%d)..putLeb(count)..putBytes(rows.buf);w.putLeb(ref);w.putByte(16);w.putLeb(body.buf.length);w.putBytes(body.buf);}\n%s}\n", ind, listElementKind(f), ind)
}
func (g *wireGen) emitLoadKeyed(f *ir.Field, ind string, ordinal int) {
	n := fieldMember(f)
	kind := listElementKind(f)
	g.pf("%s if(kind!=16){r.report.kindMismatch++;if(!r.skipKind(kind)){r.report.malformed=true;return;}break;}\n", ind)
	g.pf("%s {final length=r.readLeb();if(length==null||!r.has(length)){r.report.malformed=true;return;}final end=r.pos+length;final arr=TableWireReader.sub(r,r.pos,end);r.pos=end;\n%s if(!arr.has(2))break;final elemKind=arr.readByte();final count=arr.readLeb();if(count==null){r.report.malformed=true;break;}\n%s if(elemKind!=%d){if(!_kindWidens(elemKind,%d)){r.report.kindMismatch++;break;}r.report.widened++;}\n%s for(var i=0;count<0||i<count;i++){\n%s final ref=arr.readLeb();if(ref==null||ref==0){r.report.malformed=true;break;}final id=arr.refToId[ref];if(id==null){r.report.malformed=true;break;}\n%s final size=arr.readLeb();if(size==null||!arr.has(size)){r.report.malformed=true;break;}final body=TableWireReader.sub(arr,arr.pos,arr.pos+size);arr.pos+=size;var key=0;switch(id){\n", ind, ind, ind, kind, kind, ind, ind, ind)
	for i := range f.KeyEnumRef.Variants {
		g.pf("%scase %s:key=%d;break;\n", ind, dartU64Literal(ir.TableWireId(f.KeyEnumRef.VariantWireName(i))), i+1)
	}
	g.pf("%s default:r.report.unknown++;if(r.retain!=null)r.report.retainLost++;continue;}\n", ind)
	if ref := tableRef(f); ref != nil {
		g.pf("%s var value=%s();body.path=[...r.path,%d,key-1];%sWireLoadBody(body,value,true);if(body.pos!=body.limit){r.report.malformed=true;value=%s();}v.%s[key]=value;\n", ind, ref.Name, ordinal, dartName(ref.Name), ref.Name, n)
	} else {
		g.pf("%s final values=<%s>[];\n", ind, dartListElemType(f))
		bare := *f
		bare.Array = ir.ArrayList
		bare.KeyEnum = ""
		g.emitLoadScalarListElem(&bare, "values", "body", ind)
		g.pf("%s if(values.isNotEmpty)v.%s[key]=values.single;\n", ind, n)
	}
	g.pf("%s }\n%s }\n%s break;\n", ind, ind, ind)
}
func (g *wireGen) emitTextKeyedValue(st *ir.Struct, f *ir.Field) {
	g.pf("if(v.%s.entries.isEmpty){out.raw('{}');}else{out.raw('{');final parentDepth=depth;var first=true;for(final slot in v.%s.entries){if(!first)out.raw(',');first=false;out.line(parentDepth+2);out.raw(_jsonString(%sName(slot.$1)));out.raw(': ');final depth=parentDepth+1;final v=(slot:slot.$2);\n", fieldMember(f), fieldMember(f), dartName(f.KeyEnum))
	bare := *f
	bare.Name = "slot"
	bare.Array = ir.ArrayNone
	bare.KeyEnum = ""
	g.emitTextToJsonValue(st, &bare)
	g.pf("}out.line(depth+1);out.raw('}');}\n")
}
func (g *wireGen) emitTextReadKeyed(f *ir.Field) {
	n := fieldMember(f)
	g.pf("if(val is! Map){r.kindMismatch++;break;}v.%s=%s;final seen=<int>{};for(final pair in r.entries(val)){var key=0;switch(pair.key){\n", n, g.keyedConstruct(f))
	for i, name := range f.KeyEnumRef.Variants {
		g.pf("case %q:key=%d;break;\n", name, i+1)
	}
	g.pf("default:r.unknown++;continue;}if(!seen.add(key))r.duplicate++;final item=pair.value;\n")
	switch {
	case tableRef(f) != nil:
		ref := tableRef(f)
		g.pf("if(item is Map<String,dynamic>){v.%s[key]=%s();%sFromJsonFields(v.%s[key],item,r,sh);}else{r.kindMismatch++;}\n", n, ref.Name, dartName(ref.Name), n)
	case wireWide(f):
		g.pf("v.%s[key]=%s;\n", n, g.wideJSONRead(f, "item", "v."+n+"[key]"))
	case enumRef(f) != nil:
		g.pf("if(item is String){v.%s[key]=%sFromName(item,r);}else{r.kindMismatch++;}\n", n, dartName(enumRef(f).Name))
	case f.Type.Kind == ir.TBool:
		g.pf("if(item is bool){v.%s[key]=item;}else{r.kindMismatch++;}\n", n)
	case f.Type.Kind == ir.TFloat32 || f.Type.Kind == ir.TFloat64:
		lo, hi := jsonFloatBounds(f)
		g.pf("if(TableTextReport.isNumber(item)){v.%s[key]=_jsonFloatField(item,r,%v,v.%s[key],%s,%s);}else{r.kindMismatch++;}\n", n, f.Type.Kind == ir.TFloat32, n, lo, hi)
	default:
		lo, hi := jsonIntBounds(f)
		g.pf("if(TableTextReport.isNumber(item)){v.%s[key]=_jsonIntField(item,r,%s,%s,%s,v.%s[key]);}else{r.kindMismatch++;}\n", n, lo, hi, jsonUnsigned(f), n)
	}
	g.pf("}break;\n")
}
