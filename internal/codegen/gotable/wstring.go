package gotable

import "github.com/mas-bandwidth/schema/v2/ir"

func (g *tableGen) emitReadWString(f *ir.Field, expr, rdr, ind, bad string) {
	g.pf("%sclear(%s[:]); %sLength=0\n", ind, expr, expr)
	g.pf("%sif !tableUtf16Valid(%s.Buffer) { %s }\n", ind, rdr, bad)
	g.pf("%skeep:=len(%s.Buffer)/2;if keep>%d {keep=tableUtf16Clamp(%s.Buffer,%d);r.Report.Clamped++}\n", ind, rdr, f.Type.Size, rdr, f.Type.Size)
	g.pf("%sfor i:=0;i<keep;i++ {%s[i]=uint16(%s.Buffer[2*i])|uint16(%s.Buffer[2*i+1])<<8};%sLength=int32(keep)\n", ind, expr, rdr, rdr, expr)
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
func tableUtf16Clamp(data []byte,keep int) int {
 if keep>0 && keep<len(data)/2 {u:=binary.LittleEndian.Uint16(data[(keep-1)*2:]);if u>=0xd800 && u<=0xdbff {keep--}}
 return keep
}
`

const tableJsonWStringSource = `
func tableJsonWriteWString(out *tableJsonOut,units []uint16) {
 out.put('"')
 for i:=0;i<len(units);i++ {
  code:=uint32(units[i])
  if code>=0xd800 && code<=0xdbff && i+1<len(units) && units[i+1]>=0xdc00 && units[i+1]<=0xdfff {
   code=0x10000+(code-0xd800)<<10+(uint32(units[i+1])-0xdc00);i++
  }
  if code>=0xd800 && code<=0xdfff {code=0xfffd}
  var encoded [4]byte;n:=tableJsonEncodeUtf8(code,encoded[:])
  tableJsonWriteStringBody(out,encoded[:n])
 }
 out.put('"')
}
`
