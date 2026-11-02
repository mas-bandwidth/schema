package darttable

import "github.com/mas-bandwidth/schema/v2/ir"

func (g *cookGen) emitCookTextExtent(f *ir.Field, val, addr, ind string, write bool, base string) {
	g.needConvert = true
	g.pf("%s{\n", ind)
	if f.Type.Optional {
		owner := val[:lastDot(val)]
		g.pf("%s if(!%s.%s&&%s.isNotEmpty)return -1;\n", ind, owner, hasMember(f), val)
	}
	g.pf("%s if(!wire.tableTextValueValid(%s,-1))return -1;\n", ind, val)
	g.pf("%s final text=utf8.encode(%s);if(text.length>0xffffffff)return -1;\n", ind, val)
	g.pf("%s if(text.isNotEmpty){point=(point+7)&-8;\n", ind)
	if write {
		g.pf("%s  final blob=%s+point;\n", ind, base)
		g.pf("%s  if(out!=null){out.setInt64(%s,blob-(%s),order);out.setUint32(blob,text.length,order);out.setUint32(blob+4,0,order);for(var i=0;i<text.length;i++){out.setUint8(blob+8+i,text[i]);}out.setUint8(blob+8+text.length,0);}\n", ind, addr, addr)
	}
	align := ir.TableRegionAlign(g.unit)
	g.pf("%s  point+=(9+text.length+%d-1)&~(%d-1);\n", ind, align, align)
	if write {
		g.pf("%s }else{out?.setInt64(%s,0,order);}\n", ind, addr)
	} else {
		g.pf("%s }\n", ind)
	}
	g.pf("%s}\n", ind)
}
