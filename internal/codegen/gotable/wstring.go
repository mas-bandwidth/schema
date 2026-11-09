package gotable

import "github.com/mas-bandwidth/schema/v2/ir"

func (g *tableGen) emitReadWString(f *ir.Field, expr, rdr, ind, bad string) {
	g.pf("%sclear(%s[:]); %sLength=0\n", ind, expr, expr)
	g.pf("%sif !tableUtf16Valid(%s.Buffer) { %s }\n", ind, rdr, bad)
	g.pf("%skeep:=len(%s.Buffer)/2;if keep>%d {%s}\n", ind, rdr, f.Type.Size, refuse("TableRefuseLengthOverBound", g.floatOwner(f), "return false"))
	g.pf("%sfor i := range keep {%s[i]=uint16(%s.Buffer[2*i])|uint16(%s.Buffer[2*i+1])<<8};%sLength=int32(keep)\n", ind, expr, rdr, rdr, expr)
}

const tableWStringSource = `
func tableUtf16Valid(data []byte) bool {
 if len(data)&1!=0 {return false}
 for i:=0;i<len(data);i+=2 {
  u:=binary.LittleEndian.Uint16(data[i:])
  if u==0 || u>=0xdc00 && u<=0xdfff {return false}
  if u>=0xd800 && u<=0xdbff {
   i+=2;if i>=len(data) {return false}
   v:=binary.LittleEndian.Uint16(data[i:]);if v<0xdc00 || v>0xdfff {return false}
  }
 }
 return true
}
`

