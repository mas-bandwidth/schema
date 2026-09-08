package gotable

// Graph text uses the same descriptor walk as fixed text. The arena is behind
// an allocation callback so the generic walk remains identical in every unit.
const tableJsonGraphSource = `
type tableJsonGraphNode struct { refs int; label uint64; written bool }
type tableJsonLabel struct { key uint64; ref int64; target uint64; open bool }
type tableJsonGraph struct {
 alloc func(int64)(unsafe.Pointer,int64)
 listAdd func(unsafe.Pointer,int64)unsafe.Pointer
 mapPlace func(unsafe.Pointer,*TableFieldInfo,tableMapKey)(unsafe.Pointer,bool)
 node func(unsafe.Pointer)*tableJsonGraphNode
 allocate func(int64)[]byte
 release func([]byte)
 labels []tableJsonLabel
 memory []byte
 count int
 next uint64
}
func (g *tableJsonGraph) findLabel(key uint64)(int,bool) {if len(g.labels)==0{return 0,false};h:=key*0x9e3779b97f4a7c15;h^=h>>32;mask:=uint64(len(g.labels)-1);for slot:=h&mask;;slot=(slot+1)&mask {entry:=g.labels[slot];if entry.key==0{return int(slot),false};if entry.key==key{return int(slot),true}}}
func (g *tableJsonGraph) getLabel(key uint64)(tableJsonLabel,bool){i,ok:=g.findLabel(key);if !ok{return tableJsonLabel{},false};return g.labels[i],true}
func (g *tableJsonGraph) setLabel(key uint64,entry tableJsonLabel)bool {
 if key==0{return false};i,exists:=g.findLabel(key)
 if !exists&&g.count*2>=len(g.labels){capacity:=len(g.labels)*2;if capacity<16{capacity=16};if g.allocate==nil{return false};memory:=g.allocate(int64(capacity)*int64(unsafe.Sizeof(tableJsonLabel{})));if memory==nil{return false};clear(memory);old,oldMemory:=g.labels,g.memory;g.memory=memory;g.labels=unsafe.Slice((*tableJsonLabel)(unsafe.Pointer(&memory[0])),capacity);for _,e:=range old {if e.key!=0{slot,_:=g.findLabel(e.key);g.labels[slot]=e}};if oldMemory!=nil{g.release(oldMemory)};i,_=g.findLabel(key)}
 if !exists {g.count++};entry.key=key;g.labels[i]=entry;return true
}
func (g *tableJsonGraph) close(){if g.memory!=nil{g.release(g.memory);g.memory=nil;g.labels=nil}}
func tableJsonPointerAt(slot *int64) unsafe.Pointer { if *slot==0{return nil};return unsafe.Add(unsafe.Pointer(slot),*slot) }
func tableJsonWritePointer(out *tableJsonOut,slot *int64,f *TableFieldInfo,depth int32) bool {
 if depth>tableJsonMaxDepth {return false};p:=tableJsonPointerAt(slot);if p==nil{out.text("null");return true};if out.graph==nil {return false}
 if out.graph.node==nil{return false};entry:=out.graph.node(p);if entry==nil{return false}
 if f.Table==nil {if entry.refs>1{return false};size:=*(*uint32)(p);data:=unsafe.Slice((*byte)(unsafe.Add(p,8)),int(size));if f.TypeName=="bytes"{tableJsonWriteBase64(out,data)}else{tableJsonWriteString(out,data)};return true}
 if entry.refs<2{return tableJsonWriteValue(out,p,f.Table(),depth)}
 if entry.label==0 {out.graph.next++;entry.label=out.graph.next}
 if entry.written {out.put('{');out.line(depth+1);out.text("\"&node\": ");tableJsonWriteUnsigned(out,entry.label);out.line(depth);out.put('}');return true}
 entry.written=true
 return tableJsonWriteLabeledValue(out,p,f.Table(),depth,entry.label)
}
func tableJsonReadLabel(in *tableJsonIn) (uint64,bool) {
 in.space();start:=in.pos;if start>=len(in.text)||in.text[start]<'1'||in.text[start]>'9'{in.bad=true;return 0,false};v:=uint64(0)
 for in.pos<len(in.text)&&in.text[in.pos]>='0'&&in.text[in.pos]<='9'{d:=uint64(in.text[in.pos]-'0');if v>(^uint64(0)-d)/10{in.bad=true;return 0,false};v=v*10+d;in.pos++}
 c:=in.peek();if c!=','&&c!='}'{in.bad=true;return 0,false};return v,true
}
func tableJsonReadPointer(in *tableJsonIn,slot *int64,f *TableFieldInfo,depth int32) bool {
 *slot=0
 if depth>tableJsonMaxDepth{in.bad=true;return false}
 if in.peek()=='n'{return in.literal("null")}
 if in.graph==nil||in.graph.alloc==nil{in.bad=true;return false}
 if f.Table==nil{return tableJsonReadBlob(in,slot,f)}
 if in.peek()!='{'{in.bad=true;return false}
 start:=in.pos;probe:=*in;probe.pos++;label:=uint64(0);hasLabel:=false
 if probe.peek()!='}' {var key [tableJsonMaxKey]byte;length,ok:=probe.scanString(key[:]);if !ok {in.bad=true;return false};if length>0&&key[0]=='&'{if string(key[:length])!="&node"||probe.peek()!=':'{in.bad=true;return false};probe.pos++;label,ok=tableJsonReadLabel(&probe);if !ok{in.bad=true;return false};hasLabel=true}}
 if hasLabel {entry,exists:=in.graph.getLabel(label);if exists&&entry.open{in.bad=true;return false};if probe.peek()=='}'{if !exists{in.bad=true;return false};probe.pos++;in.pos=probe.pos;if entry.ref!=0 {if entry.target!=f.TargetId{in.report.KindMismatch++}else{*slot=entry.ref}};return true};if exists {in.bad=true;return false};probe.pos++;if probe.peek()=='}'{in.bad=true;return false}}
 p,ref:=in.graph.alloc(int64(f.Table().Size));if p==nil{in.bad=true;return false};*slot=ref;f.Table().Reset(p)
 if hasLabel {if !in.graph.setLabel(label,tableJsonLabel{ref:ref,target:f.TargetId,open:true}){in.bad=true;return false};in.pos=probe.pos;ok:=tableJsonReadTableFields(in,p,f.Table(),depth,false);if ok {entry,_:=in.graph.getLabel(label);entry.open=false;if !in.graph.setLabel(label,entry){in.bad=true;return false}};return ok}
 in.pos=start;return tableJsonReadTable(in,p,f.Table(),depth)
}
func tableJsonReadBlob(in *tableJsonIn,slot *int64,f *TableFieldInfo) bool {
 if in.peek()!='"'{in.bad=true;return false};probe:=*in;size,ok:=probe.scanString(nil);if !ok{in.bad=true;return false}
 if f.TypeName=="bytes" {
  // Decode using the fixed byte field's exact same base64 policy.
  capacity:=int64(size)*3/4+3;p,ref:=in.graph.alloc(8+capacity);if p==nil{in.bad=true;return false}
  shape:=TableFieldInfo{TypeName:"bytes",Kind:6,IsArray:true,Counted:true,ArrayBound:int32(capacity),Offset:8,CountOffset:0,ElemSize:1}
  before:=in.report.KindMismatch;if !tableJsonReadField(in,p,&shape,0){return false};if in.report.KindMismatch!=before{return true};*(*uint32)(unsafe.Add(p,4))=0;*slot=ref;return true
 }
 p,ref:=in.graph.alloc(8+int64(size)+1);if p==nil{in.bad=true;return false};data:=unsafe.Slice((*byte)(unsafe.Add(p,8)),int(size));length,ok:=in.scanString(data);if !ok{return false};*(*uint32)(p)=uint32(length);*(*uint32)(unsafe.Add(p,4))=0;*slot=ref;return true
}

// A skipped definition keeps its label, without imposing graph syntax on a
// value this reader is skipping. The ordinary JSON scanner still validates it.
func tableJsonDropLabel(in *tableJsonIn) {
 if in.graph==nil||in.peek()!='{'{return};probe:=*in;probe.pos++;var key [16]byte;n,ok:=probe.scanString(key[:]);if !ok||string(key[:n])!="&node"||probe.peek()!=':'{return};probe.pos++;label,ok:=tableJsonReadLabel(&probe);if !ok||probe.peek()!=','{return};if _,exists:=in.graph.getLabel(label);!exists{if !in.graph.setLabel(label,tableJsonLabel{}){in.bad=true}}
}
`
