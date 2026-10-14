package darttable

import (
	"fmt"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// Map ordering is shared by numbering, wire and text saves (§2.8): sorting
// cannot happen after the first-use node traversal or shared-node ids drift.
func (g *wireGen) emitMapEntryHelpers(st *ir.Struct) {
	key := st.Fields[0]
	kind := ir.TableWireScalarKind(key)
	typ := "int"
	zero := "0"
	if key.Type.Kind == ir.TString {
		typ = "String"
		zero = "''"
	}
	g.pf("  static int compare(%s a, %s b) {\n", typ, typ)
	if key.Type.Kind == ir.TString {
		g.pf("    final x = utf8.encode(a); final y = utf8.encode(b);\n    for (var i = 0; i < x.length && i < y.length; i++) { if (x[i] != y[i]) return x[i].compareTo(y[i]); }\n    return x.length.compareTo(y.length);\n")
	} else if !key.Type.Signed && key.Type.Width == 64 {
		g.pf("    if (a == b) return 0; return _u64Below(a,b) ? -1 : 1;\n")
	} else {
		g.pf("    return a.compareTo(b);\n")
	}
	g.pf("  }\n\n  static List<%s> ordered(List<%s> entries) {\n    final out = List<%s>.from(entries)..sort((a,b)=>compare(a.key,b.key));\n    for (var i=0;i<out.length;i++) {\n", st.Name, st.Name, st.Name)
	if key.Type.Kind == ir.TString {
		g.pf("      if (utf8.encode(out[i].key).length > %d) throw StateError('invalid_value');\n", key.Type.Size)
	} else if key.Type.Width < 64 {
		if key.Type.Signed {
			g.pf("      if(out[i].key < %d || out[i].key > %d) throw StateError('invalid_value');\n", -(int64(1) << (key.Type.Width - 1)), (int64(1)<<(key.Type.Width-1))-1)
		} else {
			g.pf("      if(out[i].key < 0 || out[i].key > %d) throw StateError('invalid_value');\n", (uint64(1)<<key.Type.Width)-1)
		}
	}
	g.pf("      if (i>0 && compare(out[i-1].key,out[i].key)==0) throw StateError('invalid_value');\n    }\n    return out;\n  }\n\n")
	// Scan only primitive key fields. Value decoding here would consume its
	// node/report effects twice and would choose a slot before knowing the key.
	g.pf("  static (%s, int, bool) readKey(TableWireReader r) {\n    var key = %s; var bad = false; var over = false; var wide = false;\n    while (true) {\n      final ref = r.readLeb();\n      if (ref == null) return (key,bad?1:3,wide);\n      if (ref == 0) return (key,bad?1:(r.pos!=r.limit?3:(over?2:0)),wide);\n      final id = r.refToId[ref];\n      if (id == null || !r.has(1)) return (key,bad?1:3,wide);\n      final kind = r.readByte();\n      if (id != %s) { if (!r.skipKind(kind)) return (key,bad?1:3,wide); continue; }\n      if (kind != %d && !_kindWidens(kind,%d)) { bad=true; if (!r.skipKind(kind)) return (key,1,wide); continue; }\n      bad=false; if (kind != %d) wide=true;\n", typ, zero, dartU64Literal(fieldId(key)), kind, kind, kind)
	if key.Type.Kind == ir.TString {
		g.pf("      final length = r.readLeb();\n      if (length == null || !r.has(length) || !_lebValid(r.data,r.pos,length)) return (key,3,wide);\n      key=utf8.decode(r.data.sublist(r.pos,r.pos+length));\n      over=length>%d; r.pos+=length;\n", key.Type.Size)
	} else {
		g.pf("      final width = kind==2||kind==6?1:(kind==3||kind==7?2:(kind==4||kind==8?4:8));\n      if (!r.has(width)) return (key,3,wide);\n      var raw=0; for(var i=0;i<width;i++) {raw|=r.readByte()<<(8*i);}\n")
		if key.Type.Signed {
			g.pf("      key=_signExtend(raw,width);\n")
		} else {
			g.pf("      key=raw;\n")
		}
	}
	g.pf("    }\n  }\n\n")
	g.emitMapJSONKey(st)
}

func (g *wireGen) emitSaveMap(f *ir.Field, expr, ind string, id uint64, ordinal int) {
	entry := f.MapEntry.Name
	g.pf("%sif (%s.isNotEmpty) {\n%s  final entries=%s.ordered(%s);\n%s  final ref=w.refFor(%s);\n%s  final sub=w.sub(); sub.putByte(13); sub.putLeb(entries.length);\n%s  for(var i=0;i<entries.length;i++) {\n%s    final body=sub.sub(); body.path=[...w.path,%d,i];\n%s    %sWireSaveBody(body,entries[i]); sub.putLeb(body.buf.length); sub.putBytes(body.buf);\n%s  }\n%s  w.putLeb(ref); w.putByte(14); w.putLeb(sub.buf.length); w.putBytes(sub.buf);\n%s}\n", ind, expr, ind, entry, expr, ind, dartU64Literal(id), ind, ind, ind, ordinal, ind, dartName(entry), ind, ind, ind)
}

func (g *wireGen) emitLoadMap(f *ir.Field, ind string, ordinal int) {
	n := fieldMember(f)
	entry := f.MapEntry.Name
	g.pf("%s  if (kind!=14) { r.report.kindMismatch++; if(!r.skipKind(kind)){r.report.malformed=true;return;} break; }\n", ind)
	g.pf("%s  {\n%s    final length=r.readLeb();\n%s    if(length==null||!r.has(length)){r.report.malformed=true;return;}\n%s    final end=r.pos+length; final arr=TableWireReader.sub(r,r.pos,end); r.pos=end;\n%s    if(!arr.has(2)){r.report.malformed=true;break;}\n%s    final elemKind=arr.readByte();final count=arr.readLeb();\n%s    if(count==null||count<0||count>0x7fffffff){r.report.malformed=true;break;}\n%s    if(elemKind!=13){r.report.kindMismatch++;break;}\n%s    if(count>(arr.limit-arr.pos)~/2){r.report.malformed=true;break;}\n%s    v.%s=<%s>[];\n%s    var widened=false;\n%s    for(var i=0;i<count;i++) {\n%s      final size=arr.readLeb();if(size==null||!arr.has(size)){r.report.malformed=true;break;}\n%s      final from=arr.pos;final to=from+size;arr.pos=to;\n%s      final key=%s.readKey(TableWireReader.sub(arr,from,to));\n%s      if(key.$3&&!widened){widened=true;r.report.widened++;}\n%s      if(key.$2==1){r.report.kindMismatch++;v.%s=<%s>[];break;}\n%s      if(key.$2==3){r.report.malformed=true;break;}\n%s      if(key.$2==2){r.report.clamped++;continue;}\n%s      final compare=v.%s.isEmpty?-1:%s.compare(v.%s.last.key,key.$1);\n%s      if(compare>0){r.report.malformed=true;break;}\n%s      final value=%s()..key=key.$1;\n%s      final body=TableWireReader.sub(arr,from,to)..path=[...r.path,%d,i];\n%s      %sWireLoadBody(body,value,true);\n%s      if(body.pos!=body.limit){r.report.malformed=true;break;}\n%s      if(compare==0){r.report.duplicate++;v.%s[v.%s.length-1]=value;}else{v.%s.add(value);}\n%s    }\n%s  }\n%s  break;\n", ind, ind, ind, ind, ind, ind, ind, ind, ind, ind, n, entry, ind, ind, ind, ind, ind, entry, ind, ind, n, entry, ind, ind, ind, n, entry, n, ind, ind, entry, ind, ordinal, ind, dartName(entry), ind, ind, n, n, n, ind, ind, ind)
}

func (g *wireGen) emitTextMapValue(st *ir.Struct, f *ir.Field) {
	entry := f.MapEntry.Name
	key := ir.MapKeyField(f)
	text := "entry.key.toString()"
	if key.Type.Kind == ir.TString {
		text = "entry.key"
	} else if !key.Type.Signed && key.Type.Width == 64 {
		text = "(BigInt.from(entry.key).toUnsigned(64)).toString()"
	}
	g.pf("  if(v.%s.isEmpty){out.raw('{}');}else{\n    out.raw('{'); final rows=%s.ordered(v.%s);final parentDepth=depth;var first=true;\n    for(final entry in rows){\n      if(!first)out.raw(',');first=false;out.line(parentDepth+2);out.raw(_jsonString(%s));out.raw(': ');\n      final v=entry;final depth=parentDepth+1;\n", fieldMember(f), entry, fieldMember(f), text)
	g.emitTextToJsonValue(st, ir.MapValueField(f))
	g.pf("    }\n    out.line(depth+1);out.raw('}');\n  }\n")
}

func (g *wireGen) emitTextReadMap(f *ir.Field) {
	entry := f.MapEntry.Name
	n := fieldMember(f)
	k := ir.MapKeyField(f)
	g.pf("        if(val is! Map){r.kindMismatch++;break;}\n        v.%s=<%s>[];\n        for(final pair in r.entries(val)){\n", n, entry)
	if k.Type.Kind == ir.TString {
		g.pf("          if(pair.key is! String){r.kindMismatch++;continue;}\n          final key=pair.key as String;\n          if(utf8.encode(key).length>%d){r.clamped++;continue;}\n", k.Type.Size)
	} else {
		g.pf("          final token=pair.key.toString();\n          if(!RegExp(r'^-?(0|[1-9][0-9]*)(\\.[0-9]+)?([eE][+-]?[0-9]+)?$').hasMatch(token)){r.malformed=true;break;}\n          final key=%s.jsonKey(token);\n          if(key==null){r.kindMismatch++;continue;}\n", entry)
	}
	g.pf("          final value=%s()..key=key;\n          %sFromJsonFields(value,<String,dynamic>{'value':pair.value},r,sh);\n          final prior=v.%s.indexWhere((e)=>%s.compare(e.key,key)==0);\n          if(prior>=0){r.duplicate++;v.%s[prior]=value;}else{v.%s.add(value);}\n        }\n        v.%s=%s.ordered(v.%s);\n        if(r.malformed)return;\n        break;\n", entry, dartName(entry), n, entry, n, n, n, entry, n)
}

// emitMapJSONKey accepts decimal exponent/fraction tokens exactly, with no
// binary float conversion that could merge adjacent uint64 identities (§16.2).
func (g *wireGen) emitMapJSONKey(st *ir.Struct) {
	k := st.Fields[0]
	if k.Type.Kind == ir.TString {
		return
	}
	lo, hi := "BigInt.zero", fmt.Sprintf("(BigInt.one<<%d)-BigInt.one", k.Type.Width)
	if k.Type.Signed {
		lo = fmt.Sprintf("-(BigInt.one<<%d)", k.Type.Width-1)
		hi = fmt.Sprintf("(BigInt.one<<%d)-BigInt.one", k.Type.Width-1)
	}
	g.pf("  static int? jsonKey(String text){\n    final match=RegExp(r'^(-?)(0|[1-9][0-9]*)(?:\\.([0-9]+))?(?:[eE]([+-]?[0-9]+))?$').firstMatch(text);\n    if(match==null)return null;\n    final digits=match[2]!+(match[3]??'');\n    var value=BigInt.parse(digits);\n    final exponent=int.tryParse(match[4]??'0');\n    if(exponent==null)return value==BigInt.zero?0:null;\n    final scale=exponent-(match[3]?.length??0);\n    if(value!=BigInt.zero){\n      if(scale>20||scale < -digits.length)return null;\n      if(scale>=0){value*=BigInt.from(10).pow(scale);}else{final divisor=BigInt.from(10).pow(-scale);if(value%%divisor!=BigInt.zero)return null;value~/=divisor;}\n    }\n    if(match[1]=='-')value=-value;\n    if(value<%s||value>%s)return null;\n    return value.toSigned(64).toInt();\n  }\n\n", lo, hi)
}
