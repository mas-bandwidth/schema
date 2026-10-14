package darttable

import (
	"sort"
	"strconv"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func unionRef(f *ir.Field) *ir.Union { u, _ := f.Type.Ref.(*ir.Union); return u }

// This first arm slice carries table bodies and payload-free selections (§2.6).
// Scalar and array arm payloads remain refused until their own bare codecs land.
func cardWireUnion(u *ir.Union, seen map[string]bool) bool {
	if seen["union:"+u.Name] {
		return true
	}
	seen["union:"+u.Name] = true
	defer delete(seen, "union:"+u.Name)
	for _, a := range u.Variants {
		if !a.Void() && (!a.Body() || !a.Ref.IsTable || !cardWireTableSeen(a.Ref, seen)) {
			return false
		}
	}
	return true
}
func (g *wireGen) wireUnions() []*ir.Union {
	found := map[string]*ir.Union{}
	for _, st := range g.closure {
		for _, f := range st.Fields {
			if u := unionRef(f); u != nil {
				found[u.Name] = u
			}
		}
	}
	names := make([]string, 0, len(found))
	for n := range found {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]*ir.Union, 0, len(names))
	for _, n := range names {
		out = append(out, found[n])
	}
	return out
}
func (g *wireGen) emitUnionStorage(u *ir.Union) {
	g.pf("abstract final class %sType {\n static const int none=0;\n", u.Name)
	for i, a := range u.Variants {
		g.pf(" static const int %s=%d;\n", fieldMember(&ir.Field{Name: a.Name}), i+1)
	}
	g.pf(" static const int count=%d;\n static const int max=%d;\n}\n", len(u.Variants), len(u.Variants))
	g.pf("final class %s {\n  int type=%sType.none;\n", u.Name, u.Name)
	for _, a := range u.Variants {
		if !a.Void() {
			g.pf("  %s %s=%s();\n", a.Ref.Name, fieldMember(a.F), a.Ref.Name)
		}
	}
	g.pf("}\n")
}
func (g *wireGen) emitSaveUnion(f *ir.Field, ind string) {
	u := unionRef(f)
	expr := "v." + fieldMember(f)
	g.pf("%sif(%s.type<0||%s.type>%d)throw StateError('invalid_value');\n", ind, expr, expr, len(u.Variants))
	g.pf("%sif(%s.type!=0){w.putLeb(w.refFor(%s));w.putByte(15);switch(%s.type){\n", ind, expr, dartU64Literal(fieldId(f)), expr)
	for i, a := range u.Variants {
		g.pf("%scase %d:{final ref=w.refFor(%s);\n", ind, i+1, dartU64Literal(ir.TableWireId(a.WireName())))
		if a.Void() {
			g.pf("%sw.putLeb(ref);w.putByte(32);w.putLeb(0);break;}\n", ind)
		} else {
			g.pf("%sfinal sub=w.sub();%sWireSaveBody(sub,%s.%s);w.putLeb(ref);w.putByte(13);w.putLeb(sub.buf.length);w.putBytes(sub.buf);break;}\n", ind, dartName(a.Ref.Name), expr, fieldMember(a.F))
		}
	}
	g.pf("%s}}\n", ind)
}
func (g *wireGen) emitLoadUnion(f *ir.Field, ind string) {
	u := unionRef(f)
	expr := "v." + fieldMember(f)
	g.pf("%sif(kind!=15){r.report.kindMismatch++;if(!r.skipKind(kind)){r.report.malformed=true;return;}break;}\n", ind)
	g.pf("%s{final armRef=r.readLeb();if(armRef==null){r.report.malformed=true;return;}if(armRef==0){%s=%s();break;}final armId=r.refToId[armRef];if(armId==null||!r.has(1)){r.report.malformed=true;return;}final armKind=r.readByte();final length=r.readLeb();if(length==null||!r.has(length)){r.report.malformed=true;return;}final end=r.pos+length;final next=%s();switch(armId){\n", ind, expr, u.Name, u.Name)
	for i, a := range u.Variants {
		g.pf("%scase %s:\n", ind, dartU64Literal(ir.TableWireId(a.WireName())))
		k := 13
		if a.Void() {
			k = 32
		}
		g.pf("%sif(armKind!=%d){r.report.kindMismatch++;break;}\n", ind, k)
		if a.Void() {
			g.pf("%sif(length!=0){r.report.malformed=true;break;}next.type=%d;break;\n", ind, i+1)
		} else {
			g.pf("%s{final sub=TableWireReader.sub(r,r.pos,end);%sWireLoadBody(sub,next.%s,true);if(sub.pos!=end){r.report.malformed=true;}else{next.type=%d;}break;}\n", ind, dartName(a.Ref.Name), fieldMember(a.F), i+1)
		}
	}
	g.pf("%sdefault:r.report.unknown++;if(r.retain!=null)r.report.retainLost++;}\n%s%s=next;r.pos=end;}break;\n", ind, ind, expr)
}
func (g *wireGen) emitUnionWalk(f *ir.Field, ind string, text bool) {
	expr := "v." + fieldMember(f)
	g.pf("%sswitch(%s.type){\n", ind, expr)
	for i, a := range unionRef(f).Variants {
		if !a.Void() {
			if text {
				g.pf("%scase %d:%sCount(sh,%s.%s);break;\n", ind, i+1, dartName(a.Ref.Name), expr, fieldMember(a.F))
			} else {
				g.pf("%scase %d:%sNumber(s,%s.%s,open);break;\n", ind, i+1, dartName(a.Ref.Name), expr, fieldMember(a.F))
			}
		}
	}
	g.pf("%s}\n", ind)
}
func (g *wireGen) emitTextUnionValue(f *ir.Field) {
	expr := "v." + fieldMember(f)
	u := unionRef(f)
	g.pf("if(%s.type<0||%s.type>%d)throw StateError('invalid_value');switch(%s.type){case 0:out.raw('{}');break;\n", expr, expr, len(u.Variants), expr)
	for i, a := range u.Variants {
		g.pf("case %d:out.raw('{');out.line(depth+2);out.raw(_jsonString(%s));out.raw(': ');\n", i+1, strconv.Quote(a.Name))
		if a.Void() {
			g.pf("out.raw('null');\n")
		} else {
			g.pf("%sToJsonBody(out,%s.%s,depth+2,sh);\n", dartName(a.Ref.Name), expr, fieldMember(a.F))
		}
		g.pf("out.line(depth+1);out.raw('}');break;\n")
	}
	g.pf("}\n")
}
func (g *wireGen) emitTextReadUnion(f *ir.Field) {
	expr := "v." + fieldMember(f)
	u := unionRef(f)
	g.pf("if(val is! Map<String,dynamic>){r.kindMismatch++;break;}final next=%s();if(val.isEmpty){%s=next;break;}final entries=r.entries(val).toList();final arm=entries.first;switch(arm.key){\n", u.Name, expr)
	for i, a := range u.Variants {
		g.pf("case %s:\n", strconv.Quote(a.Name))
		if a.Void() {
			g.pf("if(arm.value!=null){r.kindMismatch++;}else{next.type=%d;}break;\n", i+1)
		} else {
			g.pf("if(arm.value is! Map<String,dynamic>){r.kindMismatch++;}else{%sFromJsonFields(next.%s,arm.value as Map<String,dynamic>,r,sh);if(!r.malformed)next.type=%d;}break;\n", dartName(a.Ref.Name), fieldMember(a.F), i+1)
		}
	}
	g.pf("default:r.unknown++;}%s=next;if(entries.length>1)r.malformed=true;break;\n", expr)
}
