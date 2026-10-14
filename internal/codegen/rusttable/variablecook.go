package rusttable

// Typed variable storage is converted to the C ABI region, in the same
// first-visit numbering the wire uses (SPEC-TABLES sections 3.1 and 7.6).
import (
	"fmt"
	"github.com/mas-bandwidth/schema/v2/ir"
	"strings"
)

const variableCookRuntime = `
struct CookRegionWriter<'a> {
 graph: &'a TableNodeGraph, offsets: Vec<usize>, out: &'a mut [u8],
 cursor: usize, order: u64, writing: bool,
}
#[allow(dead_code)]
impl CookRegionWriter<'_> {
 fn align(n:usize,a:usize)->Option<usize>{n.checked_add(a-1).map(|v|v&!(a-1))}
 fn put(&mut self,at:usize,value:u64,width:usize)->bool {
  if !self.writing{return true;}
  let Some(end)=at.checked_add(width) else{return false;};let Some(dst)=self.out.get_mut(at..end)else{return false;};
  for (i,b) in dst.iter_mut().enumerate(){let shift=if self.order==2 {width-1-i}else{i};*b=(value>>(shift*8))as u8;}true
 }
 fn bytes(&mut self,at:usize,value:&[u8])->bool{
  if !self.writing{return true;}
  let Some(end)=at.checked_add(value.len())else{return false;};let Some(dst)=self.out.get_mut(at..end)else{return false;};dst.copy_from_slice(value);true
 }
 fn reference(&mut self,at:usize,index:u64)->bool {
  if index==0 {return self.put(at,0,8);}
  let Some(&offset)=index.checked_sub(1).and_then(|i|self.offsets.get(i as usize))else{return false;};
  self.put(at,(64i64+offset as i64-at as i64)as u64,8)
 }
 fn reserve(&mut self,count:usize,size:usize,align:usize)->Option<usize>{
  let start=Self::align(self.cursor,align)?;self.cursor=start.checked_add(count.checked_mul(size)?)?;Some(start)
 }
}
`

func variableRecordHasExtent(u *ir.Unit, st *ir.Struct) bool {
	for _, f := range st.Fields {
		if f.IsList() {
			return true
		}
		if !f.Type.Pointer && f.Type.Kind == ir.TNamed {
			if ref, ok := f.Type.Ref.(*ir.Struct); ok && variableRecordHasExtent(u, ref) {
				return true
			}
		}
	}
	return false
}
func variableRecordBytes(u *ir.Unit, st *ir.Struct) int64 {
	n := ir.RecordLayout(u, st).Size
	if variableRecordHasExtent(u, st) {
		n = (n + 7) & -8
	}
	return n
}

func (g *gen) emitVariableCook(b *strings.Builder, names []string) {
	b.WriteString(variableCookRuntime)
	for _, n := range names {
		g.emitVariableCookRecord(b, g.unit.Tables[n])
	}
	targets := g.pointerTargets(names)
	fmt.Fprintf(b, "#[allow(unused_variables)]\nfn variable_cook_node_layout(node:&TableNodeRecord)->Option<(usize,usize,u64)> {match node {\n")
	fmt.Fprintf(b, "TableNodeRecord::Blob(id,data)=>Some((8+data.len()+if *id==STRING_TYPE_ID {1}else{0},8,*id)),\nTableNodeRecord::Table(id,_)=>match *id {\n")
	for _, st := range targets {
		l := ir.RecordLayout(g.unit, st)
		fmt.Fprintf(b, "0x%016x=>Some((%d,%d,*id)),\n", ir.TableWireId(st.WireName()), variableRecordBytes(g.unit, st), l.Align)
	}
	fmt.Fprintf(b, "_=>None}}}\n")
	fmt.Fprintf(b, "#[allow(unused_variables)]\nfn variable_cook_node(node:&TableNodeRecord,w:&mut CookRegionWriter<'_>,at:usize)->bool {match node {\nTableNodeRecord::Blob(_,data)=>data.len()<=u32::MAX as usize&&w.put(at,data.len()as u64,4)&&w.bytes(at+8,data),\nTableNodeRecord::Table(id,node)=>match *id {\n")
	for _, st := range targets {
		fmt.Fprintf(b, "0x%016x=>node.downcast_ref::<%s>().is_some_and(|v|%s(v,w,at)),\n", ir.TableWireId(st.WireName()), st.Name, fn(st.Name, "cook_body"))
	}
	fmt.Fprintf(b, "_=>false}}}\n")
	for _, n := range names {
		st := g.unit.Tables[n]
		if g.isVariableRoot(st) {
			g.emitVariableCookRoot(b, st)
		}
	}
}

func (g *gen) emitVariableCookRecord(b *strings.Builder, st *ir.Struct) {
	fmt.Fprintf(b, "// %s's region record, field by field (SPEC-TABLES section 7.6).\n#[allow(unused_variables, clippy::identity_op, clippy::unnecessary_cast, clippy::needless_range_loop)]\nfn %s(v:&%s,w:&mut CookRegionWriter<'_>,at:usize)->bool {\n", st.Name, fn(st.Name, "cook_body"), st.Name)
	layout := ir.RecordLayout(g.unit, st)
	for _, fl := range layout.Fields {
		f := fl.Field
		x := "v." + ir.RustSnake(f.Name)
		dst := fmt.Sprintf("at+%d", fl.Offset)
		if f.IsList() {
			size, align := ir.ListElementLayout(g.unit, f)
			fmt.Fprintf(b, "if %s.len()>i32::MAX as usize{return false;}\nlet Some(start)=w.reserve(%s.len(),%d,%d)else{return false;};if !%s.is_empty(){\nif !w.put(%s,(start as i64-(%s)as i64)as u64,8)||!w.put(%s+8,%s.len()as u64,4){return false;}\nfor (i,e) in %s.iter().enumerate(){\n", x, x, size, align, x, dst, dst, dst, x, x)
			g.emitVariableCookElement(b, f, "e", fmt.Sprintf("start+i*%d", size))
			fmt.Fprintf(b, "}}\n")
		} else if f.Array == ir.ArrayCounted {
			pieces := ir.FieldPieces(g.unit, f, fl.Offset)
			size := pieces[0].Size / f.ArrayBound
			fmt.Fprintf(b, "if %s.len()>%d{return false;}\nif !w.put(at+%d,%s.len()as u64,4){return false;}\nfor i in 0..%d {\n", x, f.ArrayBound, pieces[1].Offset, x, f.ArrayBound)
			fmt.Fprintf(b, "let default=%s;let e=%s.get(i).unwrap_or(&default);\n", variableCookDefault(f), x)
			g.emitVariableCookElement(b, f, "e", fmt.Sprintf("%s+i*%d", dst, size))
			fmt.Fprintf(b, "}\n")
		} else if !f.Type.Pointer && (f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes) {
			pieces := ir.FieldPieces(g.unit, f, fl.Offset)
			bytes := x + ".as_bytes()"
			if f.Type.Kind == ir.TBytes {
				bytes = "&" + x + "[..]"
			}
			fmt.Fprintf(b, "if %s.len()>%d{return false;}\nif !w.put(at+%d,%s.len()as u64,4)||!w.bytes(%s,%s){return false;}\n", x, f.Type.Size, pieces[1].Offset, x, dst, bytes)
		} else {
			g.emitVariableCookElement(b, f, "&"+x, dst)
		}
	}
	fmt.Fprintf(b, "true\n}\n")
}
func variableCookDefault(f *ir.Field) string {
	switch classOf(f) {
	case vPointer, vBlob:
		return "None"
	case vTable:
		return f.Type.Name + "::default()"
	case vEnum:
		return enumDefault(f)
	case vString:
		return "String::new()"
	case vBytes:
		return "Vec::<u8>::new()"
	}
	return scalarDefault(f)
}
func (g *gen) emitVariableCookElement(b *strings.Builder, f *ir.Field, x, dst string) {
	switch classOf(f) {
	case vTable:
		fmt.Fprintf(b, "if !%s(%s,w,%s){return false;}\n", fn(f.Type.Name, "cook_body"), x, dst)
	case vPointer:
		fmt.Fprintf(b, "if let Some(node)=%s {if !w.reference(%s,w.graph.node_index(node)){return false;}}\n", x, dst)
	case vBlob:
		fmt.Fprintf(b, "if let Some(node)=%s {if !w.reference(%s,w.graph.blob_index(Rc::as_ptr(node) as *const u8 as usize)){return false;}}\n", x, dst)
	case vEnum:
		ref := f.Type.Ref.(*ir.Enum)
		fmt.Fprintf(b, "let ordinal:u64=match %s { %s::None=>0,", x, ref.Name)
		for i, v := range ref.Variants {
			fmt.Fprintf(b, "%s::%s=>%d,", ref.Name, rustVariant(v), i+1)
		}
		fmt.Fprintf(b, "};if !w.put(%s,ordinal,%d){return false;}\n", dst, ref.StorageBits/8)
	case vString, vBytes:
		// Bounded text arrays are not carried by the ABI row emitter.
		fmt.Fprintf(b, "return false; // text array element has no carried ABI slot\n")
	default:
		value := "*" + x + " as u64"
		width := ir.TableKindWidth(scalarWireKind(f))
		if f.Type.Kind == ir.TFloat32 || f.Type.Kind == ir.TFloat64 {
			value = "(" + x + ").to_bits() as u64"
		}
		if f.Type.Kind == ir.TBool {
			value = "if *" + x + " {1}else{0}"
		}
		fmt.Fprintf(b, "if !w.put(%s,%s,%d){return false;}\n", dst, value, width)
	}
}

func (g *gen) emitVariableCookRoot(b *strings.Builder, st *ir.Struct) {
	n := st.Name
	l := ir.RecordLayout(g.unit, st)
	fmt.Fprintf(b, "// Measure the graph before writing any buffer byte (SPEC-TABLES section 7.6).\nfn %s(v:&%s,buffer:&mut[u8],order:u64,measure:bool)->Option<i64> {\n", fn(n, "cook_layout"), n)
	fmt.Fprintf(b, "if order!=1&&order!=2{return None;}\nlet mut graph=TableNodeGraph::new();%s(v,&mut graph);\nlet mut empty=[];let mut w=CookRegionWriter{graph:&graph,offsets:vec![0;graph.records.len()+1],out:&mut empty,cursor:%d,order,writing:false};\nlet mut alignment=8usize.max(%d);\nif !%s(v,&mut w,64){return None;}\n", fn(n, "number"), 64+variableRecordBytes(g.unit, st), l.Align, fn(n, "cook_body"))
	fmt.Fprintf(b, "for (i,node) in graph.records.iter().enumerate(){let(size,align,_)=variable_cook_node_layout(node)?;alignment=alignment.max(align);let start=CookRegionWriter::align(w.cursor,align)?;w.offsets[i+1]=start-64;w.cursor=start.checked_add(size)?;if !variable_cook_node(node,&mut w,start){return None;}}\nlet data=CookRegionWriter::align(w.cursor.checked_sub(64)?,alignment)?;let total=64usize.checked_add(data)?.checked_add(w.offsets.len().checked_mul(16)?)?;let size=i64::try_from(total).ok()?;if measure{return Some(size);}if buffer.len()<total{return None;}\nbuffer[..total].fill(0);w.out=buffer;w.writing=true;\nif !w.put(0,0x4b4f4f434d484353,8)||!w.put(8,0x%016x,8)||!w.put(16,order,8)||!w.put(24,data as u64,8)||!w.put(32,(w.offsets.len()*16)as u64,8)||!w.put(40,alignment as u64,8){return None;}\nw.cursor=%d;if !%s(v,&mut w,64){return None;}\nif !w.put(64+data,0,8)||!w.put(64+data+8,0x%016x,8){return None;}\n", ir.BuildVersion(g.unit), 64+variableRecordBytes(g.unit, st), fn(n, "cook_body"), ir.TableWireId(st.WireName()))
	fmt.Fprintf(b, "for (i,node) in graph.records.iter().enumerate(){let(record,_,id)=variable_cook_node_layout(node)?;let offset=w.offsets[i+1];let start=64+offset;w.cursor=start+record;if !variable_cook_node(node,&mut w,start)||!w.put(64+data+(i+1)*16,offset as u64,8)||!w.put(64+data+(i+1)*16+8,id,8){return None;}}Some(size)\n}\n")
	fmt.Fprintf(b, "pub fn %s(v:&%s)->i64 {%s(v,&mut[],1,true).unwrap_or(-1)}\n", fn(n, "cook_measure"), n, fn(n, "cook_layout"))
	fmt.Fprintf(b, "pub fn %s(v:&%s)->(i64,TableCookRefuse){let n=%s(v);(n,if n<0{TableCookRefuse::InvalidValue}else{TableCookRefuse::Ok})}\n", fn(n, "cook_measure_reason"), n, fn(n, "cook_measure"))
	fmt.Fprintf(b, "pub fn %s(v:&%s,buffer:&mut[u8],order:u64)->bool{%s(v,buffer,order,false).is_some()}\n", fn(n, "cook_from"), n, fn(n, "cook_layout"))
}
