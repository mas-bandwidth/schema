package darttable

import "github.com/mas-bandwidth/schema/v2/ir"

func (g *wireGen) emitSaveWideText(f *ir.Field, expr, ind string) {
	g.pf("%sassert(%s.codeUnits.length<=%d);if(%s.isNotEmpty){w.putLeb(w.refFor(%s));w.putByte(33);w.putLeb(%s.codeUnits.length*2);w.putWide(%s);}\n", ind, expr, f.Type.Size, expr, dartU64Literal(fieldId(f)), expr, expr)
}
func (g *wireGen) emitLoadWideText(f *ir.Field, ind string) {
	g.pf("%sif(kind!=33){r.report.kindMismatch++;if(!r.skipKind(kind)){r.report.malformed=true;return;}break;}\n%s{final length=r.readLeb();if(length==null||!r.has(length)){r.report.malformed=true;return;}v.%s=r.readWide(length,%d)??'';}break;\n", ind, ind, fieldMember(f), f.Type.Size)
}

const textWideMembers = `
  static String wideJSONText(String value){
    final units=value.codeUnits;final out=<int>[];
    for(var i=0;i<units.length;i++){final unit=units[i];
      if(unit>=0xd800&&unit<=0xdbff){if(i+1<units.length&&units[i+1]>=0xdc00&&units[i+1]<=0xdfff){out.add(unit);out.add(units[++i]);}else{out.add(0xfffd);}}
      else if(unit>=0xdc00&&unit<=0xdfff){out.add(0xfffd);}else{out.add(unit);}
    }
    return String.fromCharCodes(out);
  }
  static String wideField(dynamic value,TableTextReport r,int maxUnits,String fallback){
    if(value is! String){r.kindMismatch++;return fallback;}
    final units=wideJSONText(value).codeUnits;var keep=units.length;
    if(keep>maxUnits){keep=maxUnits;if(keep>0&&units[keep-1]>=0xd800&&units[keep-1]<=0xdbff)keep--;r.clamped++;}
    return String.fromCharCodes(units.take(keep));
  }
`
