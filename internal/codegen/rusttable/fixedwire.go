package rusttable

// Form 1 inputs to the fixed cook writer (SPEC-TABLES sections 3, 4 and 7.6).
// Decode into the existing fixed record image, then use the existing scatter
// and scalar clamps. This preserves the one storage/default implementation.
import (
	"fmt"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const fixedWireRuntime = `
/// A bounded cursor over form-1 payloads (SPEC-TABLES section 3).
pub struct TableFixedWireReader<'a> { pub data: &'a [u8], pub off: usize }
impl<'a> TableFixedWireReader<'a> {
 pub fn take(&mut self, n: usize) -> Option<&'a [u8]> {
  let end=self.off.checked_add(n)?;
  let b=self.data.get(self.off..end)?; self.off=end; Some(b)
 }
 pub fn leb(&mut self) -> Option<u64> {
  let mut v=0u64;
  for i in 0..10 { let b=*self.take(1)?.first()?; if i==9 && b>1 {return None;} v|=((b&127) as u64)<<(7*i); if b&128==0 {if i>0&&b==0{return None;}return Some(v);} }
  None
 }
 pub fn body(&mut self) -> Option<Self> { let n=usize::try_from(self.leb()?).ok()?; Some(Self{data:self.take(n)?,off:0}) }
 pub fn skip(&mut self, kind:u8) -> bool {
  let width=match kind {1|2|6|20|25=>1,3|7|21|26=>2,4|8|10|22|27=>4,5|9|11|23|28=>8,18|19|24|29=>16,_=>0};
  if width>0 {return self.take(width).is_some();}
  match kind {17|30=>self.leb().is_some(),12|13|14|16|31|32|33=>self.body().is_some(),15=>{let Some(arm)=self.leb() else{return false;}; arm==0 || self.take(1).is_some()&&self.body().is_some()},_=>false}
 }
 pub fn scalar(&mut self,kind:u8,declared:u8,width:usize,event:bool,report:&mut TableFixedReport)->Option<Vec<u8>> {
  let old=match kind {1|2|6|20|25=>1,3|7|21|26=>2,4|8|10|22|27=>4,5|9|11|23|28=>8,_=>return None};
  let raw=self.take(old)?;
  let mut out=vec![0u8;width];
  if kind==10&&declared==11 {let bits=u32::from_le_bytes(raw.try_into().ok()?);let wide=if bits&0x7f800000==0x7f800000&&bits&0x007fffff!=0 {((bits as u64>>31)<<63)|0x7ff0000000000000|((bits as u64&0x007fffff)<<29)}else{(f32::from_bits(bits)as f64).to_bits()};out.copy_from_slice(&wide.to_le_bytes());}
  else {if (2..=5).contains(&kind)&&raw[old-1]&128!=0 {out.fill(255);} let n=old.min(width);out[..n].copy_from_slice(&raw[..n]);}
  if event && kind!=declared {report.widened+=1;} Some(out)
 }
 pub fn accepts(kind:u8,declared:u8)->bool {kind==declared || ((2..=5).contains(&kind)&&(2..=5).contains(&declared)||(6..=9).contains(&kind)&&(6..=9).contains(&declared))&&kind<declared || kind==10&&declared==11}
 pub fn open(data:&'a[u8], report:&mut TableFixedReport)->Option<(Self,Vec<u64>)> {
  if data.is_empty(){report.malformed=true;return None;}
  if data[0]!=1 {report.refused=true;report.reason=if data[0]==2 {TableFixedReason::MessageFormAsFile}else{TableFixedReason::NewerForm};return None;}
  let parsed=(|| {
   let tail=data.len().checked_sub(8)?;
   let n=usize::try_from(u64::from_le_bytes(data[tail..].try_into().ok()?)).ok()?;
   let end=tail.checked_sub(n.checked_mul(8)?)?;if end<1{return None;}
   let mut ids=Vec::with_capacity(n);
   for b in data[end..tail].chunks_exact(8) {let id=u64::from_le_bytes(b.try_into().ok()?);if ids.contains(&id){return None;}ids.push(id);}
   let body=&data[1..end];let mut check=Self{data:body,off:0};
   loop {let r=check.leb()?;if r==0 {if check.off!=body.len(){return None;}break;}if r as usize>ids.len(){return None;}let k=check.take(1)?[0];if !check.skip(k){return None;}}
   Some((Self{data:body,off:0},ids))
  })();
  if parsed.is_none(){report.malformed=true;}parsed
 }
}
`

func (g *gen) emitFixedWireBody(st *ir.Struct) {
	if !g.cookable(st.Name) || !g.cookWriteCarried(st) {
		return
	}
	g.pf("#[allow(clippy::identity_op, clippy::needless_range_loop)]\npub fn %s(r: &mut TableFixedWireReader<'_>, ids: &[u64], image: &mut [u8], report: &mut TableFixedReport) -> bool {\n", fn(st.Name, "node_body"))
	g.pf("    image.fill(0);\n")
	for i, v := range fixedDefaultImage(st) {
		if v != 0 {
			g.pf("    image[%d] = %d;\n", i, v)
		}
	}
	g.pf("    while r.off < r.data.len() {\n        let Some(reference) = r.leb() else {report.malformed=true;return false;};\n        if reference == 0 {return r.off == r.data.len();}\n        let Some(&id) = ids.get(reference as usize - 1) else {report.malformed=true;return false;};\n        let Some(k) = r.take(1) else {report.malformed=true;return false;}; let kind=k[0];\n        match id {\n")
	off := int64(0)
	for _, f := range st.Fields {
		size := ir.TableFixedFieldBytes(f)
		base := off
		if f.Type.Optional {
			base++
		}
		g.pf("        0x%016x => {\n", ir.TableFieldWireId(f))
		kind := ir.TableWireFieldKind(f)
		g.pf("            if !TableFixedWireReader::accepts(kind,%d) {report.kind_mismatch+=1;if !r.skip(kind){report.malformed=true;return false;}} else {\n", kind)
		if f.KeyEnum != "" || f.Array != ir.ArrayNone || f.Type.Kind == ir.TBytes {
			g.emitFixedWireArray(f, base, "                ")
		} else if f.Type.Kind == ir.TString {
			g.pf("                let Some(sub)=r.body() else {report.malformed=true;return false;};\n                if let Ok(text)=std::str::from_utf8(sub.data) {let mut used=text.len().min(%d);while !text.is_char_boundary(used){used-=1;}if used!=text.len(){report.clamped+=1;}image[%d..%d].copy_from_slice(&(used as u32).to_le_bytes());image[%d..%d+used].copy_from_slice(&sub.data[..used]);} else {report.malformed=true;}\n", f.Type.Size, base, base+4, base+4, base+4)
		} else {
			g.emitFixedWireElement(f, fmt.Sprintf("%d", base), "r", "                ", true)
		}
		if f.Type.Optional {
			g.pf("                image[%d]=1;\n", off)
		}
		g.pf("            }\n        }\n")
		off += size
	}
	g.pf("        _ => {report.unknown+=1;if !r.skip(kind){report.malformed=true;return false;}}\n        }\n    }\n    report.malformed=true;false\n}\n\n")
}

func (g *gen) emitFixedWireElement(f *ir.Field, dst, r, ind string, wrapped bool) {
	width := ir.TableFixedElementBytes(f)
	if f.Type.Kind == ir.TBytes {
		width = 1
	}
	if f.Type.Kind == ir.TNamed {
		switch ref := f.Type.Ref.(type) {
		case *ir.Struct:
			if wrapped {
				g.pf("%slet Some(mut element)=(%s).body() else {report.malformed=true;return false;};\n", ind, r)
				r = "&mut element"
			}
			g.pf("%sif !%s(%s,ids,&mut image[%s..%s+%d],report){report.malformed=true;}\n", ind, fn(ref.Name, "node_body"), r, dst, dst, width)
			return
		case *ir.Enum:
			g.pf("%slet Some(reference)=(%s).leb() else {report.malformed=true;return false;};\n%slet ordinal:u64=if reference==0 {0}else {let Some(&id)=ids.get(reference as usize-1) else {report.malformed=true;return false;};match id {\n", ind, r, ind)
			for i := range ref.Variants {
				g.pf("%s0x%016x=>%d,\n", ind, ir.TableWireId(ref.VariantWireName(i)), i+1)
			}
			g.pf("%s_=>{report.unknown+=1;0}}}; image[%s..%s+%d].copy_from_slice(&ordinal.to_le_bytes()[..%d]);\n", ind, dst, dst, width, width)
			return
		}
	}
	g.pf("%slet Some(bytes)=(%s).scalar(kind,%d,%d,%v,report) else {report.malformed=true;return false;};image[%s..%s+%d].copy_from_slice(&bytes);\n", ind, r, func() int {
		if f.Type.Kind == ir.TBytes {
			return 6
		}
		return ir.TableWireScalarKind(f)
	}(), width, f.Array == ir.ArrayNone && f.Type.Kind != ir.TBytes, dst, dst, width)
}

func (g *gen) emitFixedWireArray(f *ir.Field, base int64, ind string) {
	bound := f.ArrayBound
	if f.Type.Kind == ir.TBytes {
		bound = f.Type.Size
	}
	stride := ir.TableFixedElementBytes(f)
	if f.Type.Kind == ir.TBytes {
		stride = 1
	}
	start := base
	if f.Array == ir.ArrayCounted || f.Type.Kind == ir.TBytes {
		start += 4
	}
	g.pf("%slet Some(mut sub)=r.body() else {report.malformed=true;return false;};\n%sif sub.data.len()<2 {report.malformed=true;return false;} {\n%slet Some(k)=sub.take(1) else {report.malformed=true;return false;};let kind=k[0];\n%slet Some(count)=sub.leb() else {report.malformed=true;return false;};\n", ind, ind, ind, ind)
	g.pf("%sif !TableFixedWireReader::accepts(kind,%d){report.kind_mismatch+=1;} else {\n", ind, ir.TableWireElemKind(f))
	if f.Array == ir.ArrayCounted || f.Type.Kind == ir.TBytes {
		g.pf("%slet used=count.min(%d);if used!=count {report.clamped+=1;}image[%d..%d].copy_from_slice(&(used as u32).to_le_bytes());\n", ind, bound, base, base+4)
	}
	g.pf("%sif kind != %d {report.widened+=1;}\n", ind, ir.TableWireElemKind(f))
	if f.Array == ir.ArrayFixed {
		g.pf("%sif count > %d {report.clamped+=1;}\n", ind, bound)
	}
	g.pf("%sfor i in 0..count {\n", ind)
	r := "&mut sub"
	if f.KeyEnum != "" {
		g.pf("%slet Some(key)=sub.leb() else {report.malformed=true;return false;};let Some(&id)=key.checked_sub(1).and_then(|v|ids.get(v as usize)) else {report.malformed=true;return false;};\n%slet Some(mut element)=sub.body() else {report.malformed=true;return false;};let slot=match id {\n", ind, ind)
		for i := range f.KeyEnumRef.Variants {
			g.pf("%s0x%016x=>%d,\n", ind, ir.TableWireId(f.KeyEnumRef.VariantWireName(i)), i)
		}
		g.pf("%s_=>{report.unknown+=1;continue;}};\n", ind)
		r = "&mut element"
	} else {
		g.pf("%sif i>=%d {if !sub.skip(kind){report.malformed=true;return false;}continue;}let slot=i as usize;\n", ind, bound)
	}
	g.emitFixedWireElement(f, fmt.Sprintf("%d+slot*%d", start, stride), r, ind+"    ", f.KeyEnum == "")
	g.pf("%s}\n%sif sub.off!=sub.data.len(){report.malformed=true;}\n%s}\n%s}\n", ind, ind, ind, ind)
}

func (g *gen) emitFixedWireRoot(st *ir.Struct) {
	if !g.cookable(st.Name) || !g.cookWriteCarried(st) {
		return
	}
	g.pf("/// Form-1 file reader for a fixed root (SPEC-TABLES sections 3 and 4).\npub fn %s(data:&[u8],report:&mut TableFixedReport)->Option<%sRow> {\n", fn(st.Name, "load"), st.Name)
	g.pf("    let (mut reader,ids)=TableFixedWireReader::open(data,report)?;\n    let mut image=vec![0u8;%d];\n    if !%s(&mut reader,&ids,&mut image,report){report.malformed=true;return None;}\n    let mut value=%sRow::default();\n    %s(&image,&mut value,report);\n", ir.TableFixedTypeBytes(st), fn(st.Name, "node_body"), st.Name, fn(st.Name, "fixed_scatter"))
	if fixedRangeClampNeeded(st) {
		g.pf("    let (mut clamped,mut damaged)=(0,0); %s(&mut value,&mut clamped,&mut damaged);report.clamped+=clamped as u32;if damaged!=0{report.malformed=true;}\n", fn(st.Name, "fixed_clamp_body"))
	}
	g.pf("    Some(value)\n}\n\n")
}
