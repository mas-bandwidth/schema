package gotable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// Source offsets describe Go storage. Cook offsets come from the shared ABI
// model, so narrow enum ordinals and fixed-class union overlays need no special
// representation in the application's value.
func (g *tableGen) emitCookFieldColumns(st *ir.Struct, f *ir.Field) {
	offset := int64(0)
	un := g.unit.Unions[st.Name]
	if un == nil {
		un = g.unit.TableUnions[st.Name]
	}
	if un != nil {
		_, _, _, offset = ir.UnionLayout(g.unit, un)
	} else {
		offset = ir.RecordLayout(g.unit, st).FieldByName(f.Name).Offset
	}
	pieces := ir.FieldPieces(g.unit, f, offset)
	element := pieces[0].Size
	count, present := int64(0xffffffff), int64(0xffffffff)
	if f.Type.Optional {
		present = pieces[len(pieces)-1].Offset
	}
	switch {
	case f.IsMap():
		element = ir.RecordLayout(g.unit, f.MapEntry).Size
		count = offset + 8
	case f.IsList():
		element, _ = ir.ListElementLayout(g.unit, f)
		count = offset + 8
	case !f.Type.Pointer && (f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TWString):
		count = pieces[1].Offset
		element = 1
		if f.Type.Kind == ir.TWString {
			element = 2
		}
	case f.Array != ir.ArrayNone || f.KeyEnum != "":
		element /= f.ArrayBound
		if f.Array == ir.ArrayCounted {
			count = pieces[1].Offset
		}
	}
	g.pf("CookOffset:%d,CookElemSize:%d,CookCountOffset:%d,CookPresentOffset:%d,\n", offset, element, count, present)
}

func (g *tableGen) emitCookWriteSurface(st *ir.Struct) {
	if !st.IsTable || st.IsMapEntry() {
		return
	}
	n, typ := st.Name, g.storageName(st.Name)
	// Cook already names the read handle in Go. CookFrom is the claimed
	// name-first writer spelling; the handle remains source compatible.
	if ir.VariableTables(g.unit)[n] {
		g.pf("func %sCookMeasure(value *%s,arena ...*TableArena) int64 {return tableCookRegion(unsafe.Pointer(value),%sTableType(),nil,TableByteOrderLittle,true,tableOptionalArena(arena))}\n", n, typ, n)
		g.pf("func %sCookFrom(value *%s,buffer []byte,order TableByteOrder,arena ...*TableArena) bool {return tableCookRegion(unsafe.Pointer(value),%sTableType(),buffer,order,false,tableOptionalArena(arena))>=0}\n", n, typ, n)
	} else {
		size := ir.RecordLayout(g.unit, st).Size
		align := max(int64(8), ir.RecordLayout(g.unit, st).Align)
		size = (size + align - 1) & -align
		g.pf("func %sCookMeasure(value *%s) int64 {return %d}\n", n, typ, 64+size+16)
		g.pf("func %sCookFrom(value *%s,buffer []byte,order TableByteOrder) bool {return tableCookFixed(unsafe.Pointer(value),%sTableType(),buffer,order)}\n", n, typ, n)
	}
}

func tableCookWriteSource(u *ir.Unit) string {
	s := fmt.Sprintf("const tableCookBuildVersion uint64=0x%016x\n", ir.BuildVersion(u)) + goCookWriteRuntime
	if len(variableTableNames(u)) == 0 {
		return s + `func(w *tableCookWriter) reference(slot *int64,at int64)bool{return *slot==0}
func(w *tableCookWriter) container(p unsafe.Pointer,f *TableFieldInfo,at int64,live bool)bool{return false}
`
	}
	s = strings.Replace(s, "type tableCookWriter struct {", "type tableCookWriter struct { numbering *TableNumbering;", 1)
	s += goCookRegionWriteRuntime
	if !unitHasContainers(u) {
		s += "func(w *tableCookWriter) container(p unsafe.Pointer,f *TableFieldInfo,at int64,live bool)bool{return false}\n"
	} else {
		s += goCookContainerWriteRuntime
	}
	return s
}

const goCookWriteRuntime = `
// TableByteOrder selects the target order, independently of the writing host.
type TableByteOrder uint8
const (TableByteOrderLittle TableByteOrder=1;TableByteOrderBig TableByteOrder=2)
type tableCookWriter struct { buffer []byte; order TableByteOrder; at int64 }
func(w *tableCookWriter) put(at int64,value uint64,width uint32){if w.buffer==nil{return};for i:=uint32(0);i<width;i++ {shift:=i;if w.order==TableByteOrderBig{shift=width-1-i};w.buffer[at+int64(i)]=byte(value>>(shift*8))}}
func(w *tableCookWriter) header(data,nodes,align int64){w.put(0,0x4b4f4f434d484353,8);w.put(8,tableCookBuildVersion,8);w.put(16,uint64(w.order),8);w.put(24,uint64(data),8);w.put(32,uint64(nodes)*16,8);w.put(40,uint64(align),8)}
func(w *tableCookWriter) record(p unsafe.Pointer,t *TableTypeInfo,at int64,live bool)bool {
 for i:=range t.Fields {f:=&t.Fields[i];rides:=live&&tableJsonGuardHolds(p,t,f.Guard);if f.Optional {rides=rides&&*(*bool)(unsafe.Add(p,f.PresentOffset))};if !w.field(p,f,at,rides){return false}};return true
}
func(w *tableCookWriter) field(base unsafe.Pointer,f *TableFieldInfo,at int64,live bool)bool {
 p:=unsafe.Add(base,f.Offset);dest:=at+int64(f.CookOffset)
 if f.Optional {v:=uint64(0);if *(*bool)(unsafe.Add(base,f.PresentOffset)){v=1};w.put(at+int64(f.CookPresentOffset),v,1)}
 if f.List||f.Map {return w.container(p,f,dest,live)}
 count:=int32(1);used:=int32(1)
 if f.Counted {used=*(*int32)(unsafe.Add(base,f.CountOffset));if used<0||used>f.ArrayBound{return false};w.put(at+int64(f.CookCountOffset),uint64(used),4)}
 if !f.Pointer&&(f.TypeName=="string"||f.TypeName=="bytes"||f.TypeName=="wstring") {
  width:=f.CookElemSize;for i:=int32(0);i<used;i++{v:=tableJsonGetRaw(unsafe.Add(p,int64(i)*int64(width)),width);w.put(dest+int64(i)*int64(width),v,width)};return true
 }
 if f.IsArray {count=f.ArrayBound;if !f.Counted{used=count}}
 for i:=int32(0);i<count;i++ {if !w.value(unsafe.Add(p,int64(i)*int64(f.ElemSize)),f,dest+int64(i)*int64(f.CookElemSize),live&&i<used){return false}};return true
}
func(w *tableCookWriter) value(p unsafe.Pointer,f *TableFieldInfo,at int64,live bool)bool {
 if f.Pointer{return w.reference((*int64)(p),at)}
 if f.Arms!=nil {u:=f.Arms();tag:=tableJsonGetRaw(unsafe.Add(p,u.TagOffset),u.TagSize);if tag>=uint64(len(u.Arms)){return false};w.put(at,tag,u.CookTagSize);if tag==0||u.Arms[tag].Void{return true};return w.field(p,&u.Arms[tag].Field,at,live)}
 if f.Table!=nil{return w.record(p,f.Table(),at,live)}
 width:=f.CookElemSize
 if width==16 {lo:=*(*uint64)(p);hi:=*(*uint64)(unsafe.Add(p,8));if w.order==TableByteOrderBig {lo,hi=hi,lo};w.put(at,lo,8);w.put(at+8,hi,8);return true}
 value:=tableJsonGetRaw(p,f.ElemSize);if f.Kind==1 {value=0;if *(*bool)(p){value=1}};w.put(at,value,width);return true
}
func tableCookFixed(p unsafe.Pointer,t *TableTypeInfo,buffer []byte,order TableByteOrder)bool {
 if p==nil||order!=TableByteOrderLittle&&order!=TableByteOrderBig{return false};align:=max(int64(8),int64(t.CookAlign));data:=(int64(t.CookSize)+align-1)& -align;total:=64+data+16;if int64(len(buffer))<total{return false}
 w:=tableCookWriter{order:order};if !w.record(p,t,64,true){return false};clear(buffer[:total]);w.buffer=buffer;w.header(data,1,align);if !w.record(p,t,64,true){return false};w.put(64+data,0,8);w.put(64+data+8,t.Id,8);return true
}
`

const goCookRegionWriteRuntime = `
func(w *tableCookWriter) reference(slot *int64,at int64)bool {if *slot==0{return true};if w.numbering==nil{return false};i,ok:=w.numbering.Index(slot);if !ok{return false};w.put(at,uint64(64+w.numbering.entries[i-1].offset-at),8);return true}
func tableCookRegion(p unsafe.Pointer,t *TableTypeInfo,buffer []byte,order TableByteOrder,measure bool,a *TableArena)int64 {
 if order!=TableByteOrderLittle&&order!=TableByteOrderBig{return -1};n,ok:=tableNumber(p,t,a);if !ok{return -1};defer n.Release();w:=tableCookWriter{order:order,numbering:&n};data,align:=int64(0),int64(8)
 for i:=range n.entries {e:=&n.entries[i];size,alignment:=int64(0),int64(8);if e.info!=nil {size=int64(e.info.CookSize);alignment=int64(e.info.CookAlign)}else {length:=uint64(*(*uint32)(e.node));if length>0xffffffff{return -1};size=8+int64(length);if e.id==tableStringTypeId{size++}};align=max(align,alignment);data=(data+alignment-1)& -alignment;e.offset=data;w.at=64+data+size;if e.info!=nil&&!w.record(e.node,e.info,64+data,true){return -1};data=w.at-64}
 data=(data+align-1)& -align;total:=64+data+int64(len(n.entries))*16;if measure{return total};if int64(len(buffer))<total{return -1};clear(buffer[:total]);w.buffer=buffer;w.header(data,int64(len(n.entries)),align)
 for i:=range n.entries {e:=&n.entries[i];at:=64+e.offset;if e.info!=nil {w.at=at+int64(e.info.CookSize);if !w.record(e.node,e.info,at,true){return -1}}else {length:=uint64(*(*uint32)(e.node));w.put(at,length,4);copy(buffer[at+8:at+8+int64(length)],unsafe.Slice((*byte)(unsafe.Add(e.node,8)),int(length)))};w.put(64+data+int64(i)*16,uint64(e.offset),8);w.put(64+data+int64(i)*16+8,e.id,8)};return total
}
`

const goCookContainerWriteRuntime = `
func(w *tableCookWriter) container(p unsafe.Pointer,f *TableFieldInfo,at int64,live bool)bool {
 h:=(*tableContainer)(p);if h.Count<0||!live&&h.Count!=0{return false};if h.Count==0{return true};if w.numbering==nil{return false}
 alignment:=int64(f.ElemAlign);w.at=(w.at+alignment-1)& -alignment;start:=w.at;w.at+=int64(h.Count)*int64(f.CookElemSize);if w.at<start{return false};w.put(at,uint64(start-at),8);w.put(at+8,uint64(h.Count),4)
 cursor:=tableMapCursor(h,f,w.numbering.arena);defer cursor.Release();for i:=int32(0);i<h.Count;i++ {elem:=cursor.Next();if elem==nil||!w.value(elem,f,start+int64(i)*int64(f.CookElemSize),true){return false}};return true
}
`
