package cstable

// The region family's ordinary algorithms are emitted a second time with a
// path-bearing value. Shared parsing and storage laws remain one source.
func tableRetainRegionSource() string {
	s := tableRegionSource[tableSourceIndex(tableRegionSource, "    unsafe struct NativeValue"):tableSourceIndex(tableRegionSource, "    // The authoring conversion")]
	s += tableRegionSource[tableSourceIndex(tableRegionSource, "    static unsafe IntPtr NativeLoadRegion"):]
	s = tableReplace(s, "Native", "Retain")
	s = tableReplace(s, "if(BuilderElement(state.Worker,value,f,index)) { return true; }", "")
	s = tableReplace(s, "f.Retain", "f.Native")
	s = tableReplace(s, "f.Arms.Retain", "f.Arms.Native")
	s = tableReplace(s, "public long At;", `public long At;
        public TableRetain* Store;
        public RetainPath Path;
        public int UnionOrdinal, ArmOrdinal, ArmIndex;
        public bool IsUnion, UnionArray, IsArm;`)
	s = tableReplace(s, "public RetainValue(byte* data,long at) { Base=data; At=at; Mutable=false; }", `public RetainValue(byte* data,long at) { this=default; Base=data; At=at; }
        public RetainValue(byte* data,long at,TableRetain* store,uint node)
        { this=default; Base=data; At=at; Store=store; Path=new RetainPath { At=at,Node=node }; }`)
	s = tableReplace(s, "public RetainValue Child(TableFieldInfo f,int index) { return Base==null?default:new RetainValue(Base,Slot(f,index)-Base) { Mutable=Mutable }; }", `public RetainValue Child(TableFieldInfo f,int index)
        {
            if(Base==null) { return default; }
            RetainValue next=new RetainValue(Base,Slot(f,index)-Base) { Store=Store,Path=Path };
            if(f.Kind==13) { next.Path=IsArm && f.IsArray?Path.Step(ArmOrdinal,ArmIndex).Step(f.Ordinal,index):Path.Step(IsArm?ArmOrdinal:f.Ordinal,IsArm?ArmIndex:index); }
            else if(f.Kind==15)
            {
                if(IsArm) { next.Path=next.Path.Step(ArmOrdinal,ArmIndex); }
                if(f.IsArray) { next.Path=next.Path.Step(f.Ordinal,index); }
                next.UnionOrdinal=f.Ordinal; next.IsUnion=true; next.UnionArray=f.IsArray;
            }
            return next;
        }`)
	s = tableReplace(s, "public RetainValue Arm(TableFieldInfo f) { return Base==null?default:new RetainValue(Base,At+f.Arms.NativeArmOffset) { Mutable=Mutable }; }", `public RetainValue Arm(TableFieldInfo f)
        { return Base==null?default:new RetainValue(Base,At+f.Arms.NativeArmOffset) { Store=Store,Path=Path,IsArm=true,ArmOrdinal=f.Ordinal,ArmIndex=(int)Tag(f)-1 }; }`)
	s = tableReplace(s, "public void SetTag(TableFieldInfo f,ulong tag) { if(Base!=null) { RetainPut(Base+At,tag,f.Arms.NativeTagSize); } }", `public void SetTag(TableFieldInfo f,ulong tag)
        {
            if(Base==null) { return; }
            
            RetainPut(Base+At,tag,f.Arms.NativeTagSize);
        }
        public void DiscardUnion() { if(Store!=null && IsUnion) { RetainDiscard(Store,Path,UnionArray?-1:UnionOrdinal); } }
        public void DiscardField(TableFieldInfo f)
        { RetainDiscard(Store,IsArm && f.IsArray?Path.Step(ArmOrdinal,ArmIndex):Path,IsArm && !f.IsArray?ArmOrdinal:f.Ordinal); }`)
	// NativeMemory is a BCL API, not a member of the family.
	s = tableReplace(s, "InteropServices.RetainMemory", "InteropServices.NativeMemory")
	s = tableReplace(s, "        if(f.Dynamic) { RetainPut", `        if(f.Kind==13 || f.Kind==15) { RetainDiscard(value.Store,value.Path,f.Ordinal); }
        if(f.Dynamic) { RetainPut`)
	s = tableReplace(s, "        RetainReset(value,type);\n        for (;;)", "        RetainDiscard(value.Store,value.Path);\n        RetainReset(value,type);\n        for (;;)")
	s = tableReplace(s, "if (sub.Offset != sub.Buffer.Length) { RetainReset(child,f.Table);", "if (sub.Offset != sub.Buffer.Length) { RetainDiscard(child.Store,child.Path); RetainReset(child,f.Table);")
	s = tableReplace(s, "report.Unknown++;\n                if (!r.Skip(kind))", "report.Unknown++;\n                if (!RetainCapture(value.Store,value.Path,ref r,id,kind,report))")
	s = tableReplace(s, "{ report.Unknown++; }", "{ report.Unknown++; if(value.Store!=null) { report.RetainLost++; } }")
	s = tableReplace(s, "{ report.Unknown++; return true; }", "{ report.Unknown++; if(value.Store!=null) { report.RetainLost++; } return true; }")
	s = tableReplace(s, "{ report.Unknown++; continue; }", "{ report.Unknown++; if(value.Store!=null) { report.RetainLost++; } continue; }")
	s = tableReplace(s, "else { report.Unknown++; if(value.Store!=null) { report.RetainLost++; } }", "else { report.Unknown++; report.RetainLost++; }")
	s = tableReplace(s, "long dataBytes,string reason)", "long dataBytes,string reason,TableRetain* retain)")
	s = tableReplace(s, "RetainPut(directory,0,8);", `retain->Used=0; retain->IdUsed=0; retain->Count=0;
        retain->Base=data; retain->Directory=directory; retain->DirectoryCount=count+1L;
        RetainPut(directory,0,8);`)
	s = tableReplace(s, "new RetainValue(data,offset)", "new RetainValue(data,offset,retain,(uint)i+2)")
	s = tableReplace(s, "new RetainValue(data,0)", "new RetainValue(data,0,retain,1)")
	s = tableReplace(s, "                int keep = (int)Math.Min(count, (ulong)f.ArrayBound);", "                RetainDiscard(value.Store,value.Path,f.Ordinal);\n                int keep = (int)Math.Min(count, (ulong)f.ArrayBound);")
	s = tableReplace(s, "if (f.IsArray) { union.SetTag(f,0); }", "if (f.IsArray) { union.DiscardUnion(); union.SetTag(f,0); }")
	s = tableReplace(s, "if (reference == 0) { union.SetTag(f,0); return true; }", "if (reference == 0) { union.DiscardUnion(); union.SetTag(f,0); return true; }")
	s = tableReplace(s, "            union.SetTag(f,0);\n            int tag", "            union.DiscardUnion(); union.SetTag(f,0);\n            int tag")
	s = tableReplace(s, "RetainDiscard(value.Store,value.Path,f.Ordinal)", "value.DiscardField(f)")
	return s
}
