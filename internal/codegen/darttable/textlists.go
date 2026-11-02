package darttable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func textListWriteCheck(f *ir.Field, expr, ind, prefix, refusal string) string {
	if !f.IsTextList() {
		return ""
	}
	var b strings.Builder
	if f.Array == ir.ArrayCounted {
		fmt.Fprintf(&b, "%sif (%s.length > %d) %s\n", ind, expr, f.ArrayBound, refusal)
	}
	fmt.Fprintf(&b, "%sfor (final element in %s) {\n", ind, expr)
	if ir.TextListOptional(f) {
		fmt.Fprintf(&b, "%s  if (!element.hasValue) continue;\n", ind)
	}
	fmt.Fprintf(&b, "%s  if (!%stableTextValueValid(element.value, %d)) %s\n%s}\n", ind, prefix, ir.TextListValue(f).Type.Size, refusal, ind)
	return b.String()
}

func (g *wireGen) emitSavePlainTextList(f *ir.Field, expr, ind string, id uint64) {
	g.pf("%sif (%s.isNotEmpty) {\n", ind, expr)
	g.pf("%s  final ref=w.refFor(%s);final s=w.sub();s.putByte(12);s.putLeb(%s.length);\n", ind, dartU64Literal(id), expr)
	g.pf("%s  for(final e in %s){final text=utf8.encode(e.value);s.putLeb(text.length);s.putBytes(text);}\n", ind, expr)
	g.pf("%s  w.putLeb(ref);w.putByte(14);w.putLeb(s.buf.length);w.putBytes(s.buf);\n%s}\n", ind, ind)
}

func (g *wireGen) emitJsonTextList(f *ir.Field, expr string, d int, ind, where string) {
	g.pf("%sif (%s.isEmpty) {out.raw('[]');} else {\n", ind, expr)
	g.pf("%s  out.raw('[');var first%d=true;\n", ind, d)
	g.pf("%s  for(final e%d in %s){if(!first%d)out.raw(',');first%d=false;out.line(%s);\n", ind, d, expr, d, d, depthExpr(d+1))
	if ir.TextListOptional(f) {
		g.pf("%s    if(!e%d.hasValue){out.raw('null');}else{out.raw(_jsonString(e%d.value));}\n", ind, d, d)
	} else {
		g.pf("%s    out.raw(_jsonString(e%d.value));\n", ind, d)
	}
	g.pf("%s  }\n%s  out.line(%s);out.raw(']');\n%s}\n", ind, ind, depthExpr(d), ind)
}

func (g *wireGen) jsonTextListRead(f *ir.Field, val string, d int, fallback string) string {
	bound := int64(-1)
	if f.Array == ir.ArrayCounted {
		bound = f.ArrayBound
	}
	et := f.Type.Name
	read := fmt.Sprintf("if(item is String && !_wideValid(_jsonText(item).codeUnits))throw const FormatException('string_lone_surrogate');e.value=_jsonStringField(item,%d,r);", ir.TextListValue(f).Type.Size)
	if ir.TextListOptional(f) {
		read = fmt.Sprintf("if(item is String){e.hasValue=true;%s}else if(item!=null){r.kindMismatch++;}", read)
	}
	return fmt.Sprintf("_jsonTextList<%s>(%s,r,%d,%q,(item){final e=%s();%s return e;}%s)", et, val, bound, f.TextListElem().MapEntryOf, et, read, fallback)
}

const textListWireRuntime = `
bool tableTextValueValid(String value,int bound) =>
    _wideValid(value.codeUnits) && (bound < 0 || utf8.encode(value).length <= bound);
`

const textListJSONRuntime = `
List<T> _jsonTextList<T>(dynamic val,TableTextReport r,int bound,String field,T Function(dynamic) read,[List<T>? fallback]) {
 final before=r.clamped;
 final out=_jsonList<T>(val,r,bound,read,fallback);
 if(r.clamped!=before&&r.clampedField.isEmpty)r.clampedField=field;
 return out;
}
`
