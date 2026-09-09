package cstable

import (
	"sort"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// Region metadata accompanies the managed wire; variable units also carry
// native loading and mutable ownership here.
func generateRegion(u *ir.Unit) []byte {
	g := &tableGen{unit: u}
	g.emitMessageVocabulary()
	var names []string
	variable := ir.VariableTables(u)
	for name := range u.Tables {
		if variable[name] {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	g.tf("%s", tableAllocatorSource)
	g.emitRetainRefusals()
	if len(names) == 0 {
		g.tf("// Retention is refused on every root in this fixed-only unit.\npublic struct TableRetain {}\n")
		return g.assemble()
	}
	for _, name := range names {
		st := u.Tables[name]
		if st.IsMapEntry() {
			continue
		}
		g.pf("public static unsafe %sRow* %sLoad(System.IntPtr region, long capacity, System.ReadOnlySpan<byte> bytes, TableReport report) { return (%sRow*)TableWire.LoadRegion(%sTableType(),region,capacity,bytes,report); }\n", st.Name, st.Name, st.Name, st.Name)
		g.pf("public static %s %sLoadBuilder(System.IntPtr region) { return (%s)TableWire.CopyRegion(%sTableType(),region); }\n", st.Name, st.Name, st.Name, st.Name)
		g.pf("public static unsafe %sRow* %sLoad(System.IntPtr region, long capacity, System.IntPtr attribution, long attributionCapacity, System.ReadOnlySpan<byte> bytes, TableReport report) { return (%sRow*)TableWire.LoadRegion(%sTableType(),region,capacity,attribution,attributionCapacity,bytes,report); }\n", st.Name, st.Name, st.Name, st.Name)
		g.pf("public static long %sLoadMeasure(System.ReadOnlySpan<byte> bytes, out long dataBytes, out long attributionBytes, out TableRefuseReason reason) { long n=TableWire.LoadMeasureParts(%sTableType(),bytes,out dataBytes,out attributionBytes,out string text); reason=text==null?TableRefuseReason.ok:System.Enum.Parse<TableRefuseReason>(text); return n; }\n", st.Name, st.Name)
		g.pf("public static unsafe %sRow* %sLoadRetain(IntPtr region,long capacity,ReadOnlySpan<byte> bytes,ref TableRetain retain,TableReport report) { return (%sRow*)TableWire.LoadRetainRegion(%sTableType(),region,capacity,bytes,ref retain,report); }\n", st.Name, st.Name, st.Name, st.Name)
		g.pf("public static unsafe %sRow* %sLoadRetain(IntPtr region,long capacity,IntPtr attribution,long attributionCapacity,ReadOnlySpan<byte> bytes,ref TableRetain retain,TableReport report) { return (%sRow*)TableWire.LoadRetainRegion(%sTableType(),region,capacity,attribution,attributionCapacity,bytes,ref retain,report); }\n", st.Name, st.Name, st.Name, st.Name)
		g.pf("public static TableWire.Verdict %sLoadRetainMessages(IntPtr region,long capacity,Span<IntPtr> roots,ReadOnlySpan<byte> bytes,TableVocabulary vocabulary,Span<TableRetain> retains,TableReport report,out int count) { return TableWire.LoadMessageRetainRegion(%sTableType(),region,capacity,IntPtr.Zero,0,false,roots,bytes,vocabulary,retains,report,out count); }\n", st.Name, st.Name)
		g.pf("public static TableWire.Verdict %sLoadRetainMessages(IntPtr region,long capacity,IntPtr attribution,long attributionCapacity,Span<IntPtr> roots,ReadOnlySpan<byte> bytes,TableVocabulary vocabulary,Span<TableRetain> retains,TableReport report,out int count) { return TableWire.LoadMessageRetainRegion(%sTableType(),region,capacity,attribution,attributionCapacity,true,roots,bytes,vocabulary,retains,report,out count); }\n", st.Name, st.Name)
		cap := ir.TableWireIdCapacity(u)
		g.pf("public static long %sMeasureRetain(IntPtr region,ref TableRetain retain) { Span<ulong> ids=stackalloc ulong[%d]; Span<int> slots=stackalloc int[%d]; return TableWire.SaveRetainRegion(region,%sTableType(),Span<byte>.Empty,ids,slots,ref retain,null,true); }\n", st.Name, cap, cap, st.Name)
		g.pf("public static long %sMeasureRetain(IntPtr region,ref TableRetain retain,TableAllocator allocator) { Span<ulong> ids=stackalloc ulong[%d]; Span<int> slots=stackalloc int[%d]; return TableWire.SaveRetainRegion(region,%sTableType(),Span<byte>.Empty,ids,slots,ref retain,null,true,allocator); }\n", st.Name, cap, cap, st.Name)
		g.pf("public static long %sSaveRetain(IntPtr region,ref TableRetain retain,Span<byte> bytes,TableReport report) { Span<ulong> ids=stackalloc ulong[%d]; Span<int> slots=stackalloc int[%d]; return TableWire.SaveRetainRegion(region,%sTableType(),bytes,ids,slots,ref retain,report,false); }\n", st.Name, cap, cap, st.Name)
		g.pf("public static long %sSaveRetain(IntPtr region,ref TableRetain retain,Span<byte> bytes,TableReport report,TableAllocator allocator) { Span<ulong> ids=stackalloc ulong[%d]; Span<int> slots=stackalloc int[%d]; return TableWire.SaveRetainRegion(region,%sTableType(),bytes,ids,slots,ref retain,report,false,allocator); }\n", st.Name, cap, cap, st.Name)
		g.pf("public static long %sMeasure(System.IntPtr region) { Span<ulong> ids=stackalloc ulong[%d]; return TableWire.SaveRegion(region,%sTableType(),Span<byte>.Empty,ids,true); }\n", st.Name, cap, st.Name)
		g.pf("public static long %sMeasure(System.IntPtr region,TableAllocator allocator) { Span<ulong> ids=stackalloc ulong[%d]; return TableWire.SaveRegion(region,%sTableType(),Span<byte>.Empty,ids,true,allocator); }\n", st.Name, cap, st.Name)
		g.pf("public static long %sSave(System.IntPtr region,Span<byte> bytes) { Span<ulong> ids=stackalloc ulong[%d]; return TableWire.SaveRegion(region,%sTableType(),bytes,ids,false); }\n", st.Name, cap, st.Name)
		g.pf("public static long %sSave(System.IntPtr region,Span<byte> bytes,TableAllocator allocator) { Span<ulong> ids=stackalloc ulong[%d]; return TableWire.SaveRegion(region,%sTableType(),bytes,ids,false,allocator); }\n", st.Name, cap, st.Name)
		g.pf("public static TableWire.Verdict %sLoadMessages(System.IntPtr region,long capacity,Span<System.IntPtr> roots,ReadOnlySpan<byte> bytes,TableVocabulary vocabulary,TableReport report,out int count) { return TableWire.LoadMessageRegion(%sTableType(),region,capacity,roots,bytes,vocabulary,report,out count); }\n", st.Name, st.Name)
		g.pf("public static TableWire.Verdict %sLoadMessages(System.IntPtr region,long capacity,System.IntPtr attribution,long attributionCapacity,Span<System.IntPtr> roots,ReadOnlySpan<byte> bytes,TableVocabulary vocabulary,TableReport report,out int count) { return TableWire.LoadMessageRegion(%sTableType(),region,capacity,attribution,attributionCapacity,roots,bytes,vocabulary,report,out count); }\n", st.Name, st.Name)
		g.pf("public static long %sMeasureMessages(ReadOnlySpan<System.IntPtr> roots) { return TableWire.SaveMessageRegion(roots,%sTableType(),Span<byte>.Empty,true); }\n", st.Name, st.Name)
		g.pf("public static long %sMeasureMessages(ReadOnlySpan<System.IntPtr> roots,TableAllocator allocator) { return TableWire.SaveMessageRegion(roots,%sTableType(),Span<byte>.Empty,true,allocator); }\n", st.Name, st.Name)
		g.pf("public static long %sSaveMessages(ReadOnlySpan<System.IntPtr> roots,Span<byte> bytes) { return TableWire.SaveMessageRegion(roots,%sTableType(),bytes,false); }\n", st.Name, st.Name)
		g.pf("public static long %sSaveMessages(ReadOnlySpan<System.IntPtr> roots,Span<byte> bytes,TableAllocator allocator) { return TableWire.SaveMessageRegion(roots,%sTableType(),bytes,false,allocator); }\n", st.Name, st.Name)
		g.pf("public static long %sSaveMessages(ReadOnlySpan<System.IntPtr> roots,Span<byte> bytes,TableReport report) { if(roots.Length>256 && report!=null) { report.Refused=true; report.Reason=\"batch_too_large\"; report.Verdict=TableWire.Verdict.Refused; } return %sSaveMessages(roots,bytes); }\n", st.Name, st.Name)
		g.pf("public static long %sLoadMeasure(TableVocabulary vocabulary,ReadOnlySpan<byte> bytes,out long dataBytes,out long attributionBytes) { return TableWire.MessageLoadMeasureParts(%sTableType(),bytes,vocabulary,out dataBytes,out attributionBytes); }\n", st.Name, st.Name)
	}
	g.emitRetain()
	g.emitRegionCookSurface(names)
	g.emitBuilders(names)
	g.pf("public static unsafe partial class TableWire {\n%s\n}\n", tableBuilderLoadSource+tableBuilderCollectionWireSource+tableBuilderWireSource+tableRegionCookSource+tableRegionSource+tableRegionGraphSource+tableRegionWriteSource+tableRegionMessageReadSource+tableRegionMessageLoadSource+tableRegionMessageWriteSource+tableRetainRegionSource()+tableRetainWriteSource()+tableRetainMessageSource())
	return g.assemble()
}

const tableRegionSource = `

    unsafe struct NativeValue
    {
        public byte* Base;
        public long At;
        public bool Mutable { get; set; }
        public NativeValue(byte* data,long at) { Base=data; At=at; Mutable=false; }
        public byte* Slot(TableFieldInfo f,int index=0)
        {
            if(Base==null) { return null; }
            byte* slot=Base+At+f.NativeOffset;
            if(f.Dynamic) { return Mutable?TableListStorage.At(slot,index,f.NativeElementSize):slot+(long)NativeWord(slot,8)+(long)index*f.NativeElementSize; }
            return slot+(long)index*f.NativeElementSize;
        }
        public NativeValue Child(TableFieldInfo f,int index) { return Base==null?default:new NativeValue(Base,Slot(f,index)-Base) { Mutable=Mutable }; }
        public NativeValue Arm(TableFieldInfo f) { return Base==null?default:new NativeValue(Base,At+f.Arms.NativeArmOffset) { Mutable=Mutable }; }
        public ulong Raw(TableFieldInfo f,int index)
        {
            ulong raw=(ulong)NativeWord(Slot(f,index),f.NativeElementSize);
            if((f.Kind>=2 && f.Kind<=5 || f.Kind>=20 && f.Kind<=23) && f.NativeElementSize<8) { int shift=64-f.NativeElementSize*8; raw=unchecked((ulong)((long)(raw<<shift)>>shift)); }
            return raw;
        }
        public long Pointer(TableFieldInfo f,int index) { long delta=(long)Raw(f,index); return delta==0?long.MinValue:Slot(f,index)-Base+delta; }
        public ulong Tag(TableFieldInfo f) { return (ulong)NativeWord(Base+At,f.Arms.NativeTagSize); }
        public bool Present(TableFieldInfo f) { return Base[At+f.NativePresentOffset]!=0; }
        public bool Guard(TableFieldInfo f)
        {
            if(f.NativeGuardOffsets!=null) { for(int i=0;i<f.NativeGuardOffsets.Length;i++) { if((Base[At+f.NativeGuardOffsets[i]]!=0)!=f.NativeGuardValues[i]) { return false; } } }
            return true;
        }
        public int Count(TableFieldInfo f) { return f.NativeCountOffset<0?f.ArrayBound:(int)NativeWord(Base+At+f.NativeCountOffset,4); }
        public UInt128 Wide(TableFieldInfo f,int index) { return NativeWord(Slot(f,index),16); }
        public void SetRaw(TableFieldInfo f,int index,ulong raw) { if(Base!=null) { NativePut(Slot(f,index),raw,f.NativeElementSize); } }
        public void SetWide(TableFieldInfo f,int index,UInt128 raw) { if(Base!=null) { NativePut(Slot(f,index),raw,16); } }
        public Span<byte> Buffer(TableFieldInfo f) { return new Span<byte>(Slot(f),f.ArrayBound+(f.Kind==12?1:0)); }
        public Span<char> Chars(TableFieldInfo f) { return new Span<char>(Slot(f),f.ArrayBound+1); }
        public void SetCount(TableFieldInfo f,int n) { if(Base!=null) { NativePut(Base+At+f.NativeCountOffset,(uint)n,4); } }
        public void SetPresent(TableFieldInfo f,bool present) { if(Base!=null) { Base[At+f.NativePresentOffset]=present?(byte)1:(byte)0; } }
        public void SetTag(TableFieldInfo f,ulong tag) { if(Base!=null) { NativePut(Base+At,tag,f.Arms.NativeTagSize); } }
    }
    unsafe struct NativeState
    {
        public byte* Directory;
        public int Nodes;
        public bool NodesGood;
        public long Next,End;
        public TableTypeInfo Root;
        public TableWorker Worker { get; set; }
        public bool Refused;
    }
    static unsafe UInt128 NativeWord(byte* p,int width)
    {
        UInt128 value=0;
        for(int i=0;i<width;i++) { value |= (UInt128)p[BitConverter.IsLittleEndian?i:width-1-i]<<(i*8); }
        return value;
    }
    static unsafe void NativePut(byte* p,UInt128 value,int width)
    { for(int i=0;i<width;i++) { p[BitConverter.IsLittleEndian?i:width-1-i]=(byte)(value>>(i*8)); } }
    static unsafe void NativeReset(NativeValue value,TableTypeInfo type)
    {
        if(value.Base==null) { return; }
        System.Runtime.InteropServices.NativeMemory.Clear(value.Base+value.At,(nuint)type.StorageSize);
        foreach(TableFieldInfo f in type.Fields) { NativeResetField(value,f,true); }
    }
    static unsafe void NativeResetField(NativeValue value,TableFieldInfo f,bool defaults)
    {
        if(value.Base==null) { return; }
        if(f.Dynamic) { NativePut(value.Base+value.At+f.NativeOffset,0,16); return; }
        if(f.GetBuffer != null)
        {
            value.Buffer(f).Clear(); value.SetCount(f,0);
            if(defaults && f.DefaultBytes != null) { f.DefaultBytes.AsSpan().CopyTo(value.Buffer(f)); value.SetCount(f,f.DefaultBytes.Length); }
        }
        else if(f.GetChars != null) { value.Chars(f).Clear(); value.SetCount(f,0); }
        else
        {
            int n=f.IsArray?f.ArrayBound:1;
            for(int i=0;i<n;i++)
            {
                if(f.Kind==13) { NativeReset(value.Child(f,i),f.Table); }
                else if(f.Kind==15) { System.Runtime.InteropServices.NativeMemory.Clear(value.Slot(f,i),(nuint)f.NativeElementSize); }
                else if(f.GetWide != null) { value.SetWide(f,i,defaults?f.DefaultWide:0); }
                else { value.SetRaw(f,i,defaults?f.DefaultRaw:0); }
            }
            if(f.Counted) { value.SetCount(f,0); }
        }
        if(f.Optional) { value.SetPresent(f,false); }
    }
    static unsafe void NativeResetElement(NativeValue value, TableFieldInfo f, int i)
    {
        if (value.Base == null) { return; }
        if (f.Kind == 13) { NativeReset(value.Child(f, i), f.Table); }
        else if (f.Kind == 15) { System.Runtime.InteropServices.NativeMemory.Clear(value.Slot(f, i), (nuint)f.NativeElementSize); }
        else if (f.GetWide != null) { value.SetWide(f, i, f.DefaultWide); }
        else { value.SetRaw(f, i, f.DefaultRaw); }
    }
    static unsafe void NativeResetCountedTail(NativeValue value, TableFieldInfo f, int previous, int decoded)
    {
        int end = previous;
        if (end > f.ArrayBound) { end = f.ArrayBound; }
        for (int i = decoded; i < end; i++) { NativeResetElement(value, f, i); }
        value.SetCount(f, decoded);
    }
    static unsafe bool NativeReserve(ref NativeState state,NativeValue value,TableFieldInfo f,ulong count,TableReport report)
    {
        if(state.Worker!=null)
        {
            if(count>int.MaxValue) { state.Refused=true; return false; }
            return true; // allocate only the prefix that actually decodes
        }
        long at=Align(state.Next,f.StorageAlign);
        if(count>int.MaxValue || at>state.End || count>(ulong)((state.End-at)/f.StorageSize)) { return Damage(report); }
        long bytes=(long)count*f.StorageSize;
        if(value.Base==null) { state.Next=at+bytes; return true; }
        byte* slot=value.Base+value.At+f.NativeOffset;
        NativePut(slot,count==0?0:unchecked((ulong)(at-(slot-value.Base))),8);
        NativePut(slot+8,0,4); state.Next=at+bytes;
        for(int i=0;i<(int)count;i++)
        {
            if(f.Kind==13) { NativeReset(value.Child(f,i),f.Table); }
            else if(f.Kind==15) { System.Runtime.InteropServices.NativeMemory.Clear(value.Slot(f,i),(nuint)f.NativeElementSize); }
            else if(f.GetWide != null) { value.SetWide(f,i,f.DefaultWide); }
            else { value.SetRaw(f,i,f.DefaultRaw); }
        }
        return true;
    }
    static bool NativeEnsure(ref NativeState state,NativeValue value,TableFieldInfo f,int index)
    {
        if(state.Worker==null) { return true; }
        if(BuilderElement(state.Worker,value,f,index)) { return true; }
        state.Refused=true; return false;
    }
    static unsafe void NativePointer(ref NativeState state,NativeValue value,TableFieldInfo f,int index,ulong node,TableReport report)
    {
        long target=long.MinValue;
        if(state.NodesGood && node!=0)
        {
            if(node>(ulong)state.Nodes+1) { Damage(report); }
            else
            {
                byte* row=state.Directory+(long)(node-1)*16;
                long offset=(long)NativeWord(row,8); ulong id=(ulong)NativeWord(row+8,8);
                if(offset!=-1)
                { if(id!=f.PointerTypeId) { report.KindMismatch++; } else { target=offset; } }
            }
        }
        value.SetRaw(f,index,target==long.MinValue?0:unchecked((ulong)(target-(value.Slot(f,index)-value.Base))));
    }
    ref struct NativeMapKey
    {
        public ReadOnlySpan<byte> Text;
        public ulong Raw;
    }
    static unsafe bool NativeScanKey(Reader body,TableFieldInfo field,out NativeMapKey key,out bool mismatch,out bool widened)
    {
        key=default; mismatch=false; widened=false;
        for(;;)
        {
            if(!body.Ref(out ulong reference,out ulong id)) { return false; }
            if(reference==0) { return body.Offset==body.Buffer.Length; }
            if(!body.Has(1)) { return false; }
            byte kind=body.Byte();
            if(id!=field.Id) { if(!body.Skip(kind)) { return false; } continue; }
            if(kind!=field.Kind && Widen(kind,field.Kind)) { widened=true; }
            else
            {
                mismatch=kind!=field.Kind;
                if(mismatch) { if(!body.Skip(kind)) { return false; } continue; }
            }
            if(kind==12)
            { if(!body.Slice(out Reader text) || !TextValid(text.Buffer)) { return false; } key.Text=text.Buffer; }
            else
            {
                int width=Width(kind); if(!body.Has(width)) { return false; }
                ulong raw=body.Fixed(width);
                if(kind>=2 && kind<=5 && width<8) { raw=unchecked((ulong)((long)(raw<<(64-width*8))>>(64-width*8))); }
                key.Raw=raw;
            }
        }
    }
    static unsafe bool NativeReadMapBody(ref NativeState state,ref Reader a,NativeValue value,TableFieldInfo f,TableReport report,ReadOnlySpan<byte> headerTail=default)
    {
        if(!a.Has(2)) { return true; }
        byte kind=a.Byte();
        Reader header=headerTail.IsEmpty?a:new Reader(headerTail,a.Vocabulary) { Offset=1 };
        if(!header.Var(out ulong count)) { Damage(report); return true; }
        a.Offset=Math.Min(a.Buffer.Length,header.Offset);
        if(kind!=13) { report.KindMismatch++; return true; }
        NativeResetField(value,f,true);
        if(!NativeReserve(ref state,value,f,count,report)) { return false; }
        int landed=0; bool widened=false; NativeMapKey last=default;
        TableFieldInfo keyField=f.Table.Fields[0];
        for(ulong i=0;i<count;i++)
        {
            if(!a.Slice(out Reader body)) { Damage(report); break; }
            bool keyOk=NativeScanKey(body,keyField,out NativeMapKey key,out bool mismatch,out bool entryWidened);
            if(entryWidened && !widened) { widened=true; report.Widened++; }
            if(mismatch) { report.KindMismatch++; landed=0; break; }
            if(!keyOk) { Damage(report); break; }
            if(keyField.Kind==12 && key.Text.Length>keyField.ArrayBound) { report.Clamped++; continue; }
            int order=landed==0?-1:keyField.Kind==12?last.Text.SequenceCompareTo(key.Text):keyField.Kind>=2 && keyField.Kind<=5?unchecked((long)last.Raw).CompareTo(unchecked((long)key.Raw)):last.Raw.CompareTo(key.Raw);
            if(order>0) { Damage(report); break; }
            int slot=order==0?landed-1:landed;
            if(!NativeEnsure(ref state,value,f,slot)) { return false; }
            NativeReadBody(ref state,ref body,value.Child(f,slot),f.Table,report,true);
            if(state.Refused) { return false; }
            if(order==0) { report.Duplicate++; } else { last=key; landed++; }
        }
        value.SetCount(f,landed); return true;
    }
    static bool NativeReadScalar(ref NativeState state, ref Reader r, NativeValue value, TableFieldInfo f, int index, byte kind, TableReport report)
    {
        if (f.GetWide != null)
        {
            int width = Width(kind);
            if (!r.Has(width)) { return Damage(report); }
            UInt128 wide = r.Fixed(Math.Min(width, 8));
            if (width == 16) { wide |= (UInt128)r.Fixed(8) << 64; }
            else if ((kind >= 2 && kind <= 5) || (kind >= 20 && kind <= 23))
            { int shift = 128 - width * 8; wide = unchecked((UInt128)((Int128)(wide << shift) >> shift)); }
            if (f.ClampWide != null) { wide = f.ClampWide(wide, report); }
            value.SetWide(f,index,wide); return true;
        }
        ulong raw;
        if (kind == 30)
        {
            if (!r.Ref(out ulong reference, out ulong id)) { return Damage(report); }
            int variant = reference == 0 ? 0 : FindVariant(f, id, false);
            if (reference != 0 && variant == 0) { report.Unknown++; }
            raw = (ulong)variant;
        }
        else
        {
            int width = Width(kind);
            if (!r.Has(width)) { return Damage(report); }
            raw = r.Fixed(width);
            if (kind >= 2 && kind <= 5 && width < 8)
            { int shift = 64 - width * 8; raw = unchecked((ulong)((long)(raw << shift) >> shift)); }
            if (kind == 1) { raw = raw == 0 ? 0ul : 1ul; }
            if (kind == 10 && f.Kind == 11) { raw = WidenFloat((uint)raw); }
            if (f.ClampRaw != null) { raw = f.ClampRaw(raw, report); }
        }
        if (f.GetBuffer != null) { value.Buffer(f)[index] = (byte)raw; }
        else { value.SetRaw(f,index,raw); }
        return true;
    }
    static bool NativeReadElement(ref NativeState state, ref Reader r, NativeValue value, TableFieldInfo f, int index, byte kind, TableReport report, bool framed)
    {
        if (kind == 17)
        {
            if (!r.Var(out ulong indexValue)) { return Damage(report); }
            NativePointer(ref state,value,f,index,indexValue,report); return true;
        }
        if (kind == 13)
        {
            Reader sub = r;
            if (framed && !r.Slice(out sub)) { return Damage(report); }
            NativeValue child = value.Child(f,index);
            NativeReadBody(ref state, ref sub, child, f.Table, report, true);
            if (state.Refused) { return false; }
            if (sub.Offset != sub.Buffer.Length) { NativeReset(child,f.Table); Damage(report); }
            if (!framed) { r.Offset = r.Buffer.Length; }
            return true;
        }
        if (kind == 15)
        {
            if (!r.Var(out ulong reference)) { return Damage(report); }
            NativeValue union = value.Child(f,index);
            // Array elements establish None as soon as their reference is
            // present, even if its index or the following arm header is bad.
            // A scalar union field retains its old selection until the whole
            // header is bounded, matching the C++ reader under repeated fields.
            if (f.IsArray) { union.SetTag(f,0); }
            if (reference == 0) { union.SetTag(f,0); return true; }
            if (reference > (ulong)r.Vocabulary.Length / 8) { return Damage(report); }
            ulong id = Read64(r.Vocabulary, ((int)reference - 1) * 8);
            if (!r.Has(1)) { return Damage(report); }
            byte armKind = r.Byte();
            if (!r.Slice(out Reader arm)) { return Damage(report); }
            union.SetTag(f,0);
            int tag = FindVariant(f, id, false);
            if (tag == 0) { report.Unknown++; return true; }
            TableUnionArmInfo info = f.Arms.Arms[tag];
            byte expected = info.Field == null ? (byte)32 : Kind(info.Field);
            // A widening arm must first have exactly the source kind's width.
            // Damaged framing never records a successful widening event.
            if (Widen(armKind, expected) && arm.Buffer.Length != Width(armKind))
            { Damage(report); return true; }
            if (!Compatible(armKind, expected, report)) { return true; }
            union.SetTag(f,(ulong)tag);
            if (!NativeReadArm(ref state, ref arm, union.Arm(f), info.Field, armKind, report)) { union.SetTag(f,0); }
            return true;
        }
        return NativeReadScalar(ref state, ref r, value, f, index, kind, report);
    }
    // Selection establishes storage. Table bodies take their declared defaults;
    // general arms are zeroed, including unused tails of arrays of plain types.
    static void NativeResetArm(NativeValue value, TableFieldInfo f)
    {
        if (f.Kind == 13 && !f.IsArray) { NativeReset(value.Child(f,0),f.Table); }
        else { NativeZeroField(value, f); }
    }
    static void NativeZeroField(NativeValue value, TableFieldInfo f)
    {
        if (f.Dynamic) { NativeResetField(value,f,true); return; }
        if (f.GetChars != null) { value.Chars(f).Clear(); }
        else if (f.GetBuffer != null) { value.Buffer(f).Clear(); }
        else
        {
            int n = f.IsArray ? f.ArrayBound : 1;
            for (int i = 0; i < n; i++)
            {
                if (f.Kind == 13)
                {
                    NativeValue child = value.Child(f,i);
                    foreach (TableFieldInfo field in f.Table.Fields) { NativeZeroField(child, field); }
                }
                else if (f.Kind == 15) { value.Child(f,i).SetTag(f,0); }
                else if (f.Kind == 17) { value.SetRaw(f,i,0); }
                else if (f.SetWide != null) { value.SetWide(f,i,0); }
                else { value.SetRaw(f,i,0); }
            }
        }
        if (f.Counted) { value.SetCount(f,0); }
        if (f.Optional) { value.SetPresent(f,false); }
    }
    static bool NativeReadArm(ref NativeState state, ref Reader r, NativeValue value, TableFieldInfo f, byte kind, TableReport report)
    {
        if (f == null) { return r.Buffer.Length == 0 || Damage(report); }
        if (f.Map) { return NativeReadMapBody(ref state, ref r, value, f, report); }
        int width = Width(kind);
        if (width != 0 && r.Buffer.Length != width) { return Damage(report); }
        if (kind == 13)
        {
            NativeReadBody(ref state, ref r, value.Child(f,0), f.Table, report, true);
            if (state.Refused) { return false; }
            return r.Offset == r.Buffer.Length || Damage(report);
        }
        if (kind == 15) { return NativeReadElement(ref state, ref r, value, f, 0, kind, report, true); }
        if (kind == 17)
        {
            if (!r.Var(out ulong indexValue) || r.Offset != r.Buffer.Length) { return Damage(report); }
            NativePointer(ref state, value, f, 0, indexValue, report);
            return true;
        }
        if (kind == 12 || kind == 33)
        {
            NativeResetArm(value, f);
            return kind == 33 ? NativeReadChars(r.Buffer, value, f, report) : NativeReadText(r.Buffer, value, f, report);
        }
        if (kind == 14)
        {
            NativeResetArm(value, f);
            if (!r.Has(2)) { return true; } // an inert short arm still selects
            byte elementKind = r.Byte();
            if (!r.Var(out ulong count)) { return Damage(report); }
            if (!Compatible(elementKind, f.Kind, report)) { return false; }
            int keep = (int)Math.Min(count, (ulong)f.ArrayBound);
            if (!f.Dynamic && count > (ulong)f.ArrayBound) { report.Clamped++; }
            if (f.Dynamic && !NativeReserve(ref state,value,f,count,report)) { return false; }
            int decoded = 0;
            for (int i = 0; i < keep; i++)
            {
                if (f.Dynamic) { if (!r.Has(1)) { Damage(report); break; } if(!NativeEnsure(ref state,value,f,i)) { return false; } }
                if (!NativeReadElement(ref state, ref r, value, f, i, elementKind, report, true)) { break; }
                decoded++;
            }
            if (f.Counted) { value.SetCount(f,decoded); }
            return true; // damaged elements preserve the selected arm's prefix
        }
        if (!NativeReadScalar(ref state, ref r, value, f, 0, kind, report)) { return false; }
        return r.Offset == r.Buffer.Length || Damage(report);
    }
    static bool NativeReadText(ReadOnlySpan<byte> text, NativeValue value, TableFieldInfo f, TableReport report)
    {
        if (!TextValid(text)) { return Damage(report); }
        int keep = text.Length;
        if (keep > f.ArrayBound)
        {
            keep = f.ArrayBound; report.Clamped++;
            while (keep > 0 && (text[keep] & 0xc0) == 0x80) { keep--; }
        }
        if(value.Base!=null) { text.Slice(0,keep).CopyTo(value.Buffer(f)); value.SetCount(f,keep); }
        return true;
    }
    static bool NativeReadChars(ReadOnlySpan<byte> text, NativeValue value, TableFieldInfo f, TableReport report)
    {
        if ((text.Length & 1) != 0) { return Damage(report); }
        for (int i = 0; i < text.Length; i += 2)
        {
            int c = text[i] | text[i + 1] << 8;
            if (c == 0 || (c >= 0xdc00 && c <= 0xdfff)) { return Damage(report); }
            if (c >= 0xd800 && c <= 0xdbff)
            {
                if (i + 3 >= text.Length) { return Damage(report); }
                int low = text[i + 2] | text[i + 3] << 8;
                if (low < 0xdc00 || low > 0xdfff) { return Damage(report); }
                i += 2;
            }
        }
        int keep = text.Length / 2;
        if (keep > f.ArrayBound)
        {
            keep = f.ArrayBound; report.Clamped++;
            if (keep > 0 && (text[keep * 2 - 1] & 0xfc) == 0xd8) { keep--; }
        }
        Span<char> storage = value.Chars(f);
        for (int i = 0; i < keep; i++) { storage[i] = (char)(text[i * 2] | text[i * 2 + 1] << 8); }
        value.SetCount(f,keep); return true;
    }
    static bool NativeReadField(ref NativeState state, ref Reader r, NativeValue value, TableFieldInfo f, byte kind, TableReport report, out bool placed)
    {
        placed = true;
        if (f.Map) { if (!r.Slice(out Reader map)) { return Damage(report); } return NativeReadMapBody(ref state, ref map, value, f, report, r.Buffer.Slice(r.Offset - map.Buffer.Length)); }
        if (kind == 12 || kind == 33)
        {
            if (!r.Slice(out Reader text)) { return Damage(report); }
            if (!(kind == 33 ? NativeReadChars(text.Buffer, value, f, report) : NativeReadText(text.Buffer, value, f, report)))
            { NativeResetField(value,f,true); }
            return true;
        }
        if (kind == 14 || kind == 16)
        {
            if (!r.Slice(out Reader array)) { return Damage(report); }
            if (array.Buffer.Length < 2) { return true; } // inert, including under a repeat
            byte elementKind = array.Byte();
            // Match the reference's header cursor: the count is read at the
            // enclosing cursor, then all elements are bounded by the array's L.
            // A count spilling past L can clamp, but cannot invent an element.
            Reader header = new Reader(r.Buffer.Slice(r.Offset - array.Buffer.Length + 1), r.Vocabulary);
            if (!header.Var(out ulong count)) { Damage(report); return true; }
            array.Offset = Math.Min(array.Buffer.Length, 1 + header.Offset);
            if (!Compatible(elementKind, f.Kind, report)) { placed = false; return true; }
            if (kind == 16)
            {
                for (ulong i = 0; i < count; i++)
                {
                    if (!array.Ref(out ulong reference, out ulong id) || reference == 0 || !array.Slice(out Reader element))
                    { Damage(report); break; }
                    int slot = FindVariant(f, id, true);
                    if (slot == 0) { report.Unknown++; continue; }
                    NativeReadElement(ref state, ref element, value, f, slot - 1, elementKind, report, false);
                }
            }
            else
            {
                int keep = (int)Math.Min(count, (ulong)f.ArrayBound);
                if (!f.Dynamic && count > (ulong)f.ArrayBound) { report.Clamped++; }
                if (f.Dynamic && !NativeReserve(ref state,value,f,count,report)) { return false; }
                int previous = f.Counted ? value.Count(f) : 0;
                int decoded = 0;
                for (int i = 0; i < keep; i++)
                {
                    if (f.Dynamic) { if (!array.Has(1)) { Damage(report); break; } if(!NativeEnsure(ref state,value,f,i)) { return false; } }
                    if (!NativeReadElement(ref state, ref array, value, f, i, elementKind, report, true)) { break; }
                    decoded++;
                }
                if (f.Counted) { NativeResetCountedTail(value, f, previous, decoded); }
            }
            return true;
        }
        return NativeReadElement(ref state, ref r, value, f, 0, kind, report, true);
    }
    static bool NativeReadBody(ref NativeState state, ref Reader r, NativeValue value, TableTypeInfo type, TableReport report, bool nested)
    {
        NativeReset(value,type);
        for (;;)
        {
            if (!r.Ref(out ulong reference, out ulong id)) { return Damage(report); }
            if (reference == 0) { return true; }
            if ((nested && id == ulong.MaxValue) || id == ulong.MaxValue - 1 || id == ulong.MaxValue - 2) { return Damage(report); }
            if (!r.Has(1)) { return Damage(report); }
            byte kind = r.Byte();
            TableFieldInfo field = FindField(type, id);
            if (!nested && id == ulong.MaxValue && type.Variable)
            { if (!r.Skip(kind)) { return Damage(report); } continue; }
            if (field == null)
            {
                report.Unknown++;
                if (!r.Skip(kind)) { return Damage(report); }
                continue;
            }
            int beforeWidened = report.Widened;
            if (Widen(kind,Kind(field)) && !r.Has(Width(kind))) { return Damage(report); }
            if (!Compatible(kind, Kind(field), report))
            {
                if (!r.Skip(kind)) { return Damage(report); }
                continue;
            }
            if (field.MapKey) { report.Widened = beforeWidened; }
            if (!NativeReadField(ref state, ref r, value, field, kind, report, out bool placed)) { return false; }
            if(state.Refused) { return false; }
            if (field.Optional && placed) { value.SetPresent(field,true); }
        }
    }

    // The authoring conversion owns its allocations. Memoizing each node
    // before its fields preserves both sharing and editable cycles.
    public static unsafe object CopyRegion(TableTypeInfo type,IntPtr region,bool mutable=false)
    {
        if(region==IntPtr.Zero) { return null; }
        var nodes=new System.Collections.Generic.Dictionary<long,object>();
        object value=type.Create(); nodes.Add(0,value);
        NativeCopyBody(new NativeValue((byte*)region,0) { Mutable=mutable },value,type,nodes); return value;
    }
    static unsafe void NativeCopyBody(NativeValue source,object target,TableTypeInfo type,System.Collections.Generic.Dictionary<long,object> nodes)
    { foreach(TableFieldInfo f in type.Fields) { NativeCopyField(source,target,f,nodes); } }
    static unsafe void NativeCopyField(NativeValue source,object target,TableFieldInfo f,System.Collections.Generic.Dictionary<long,object> nodes)
    {
        int n=f.IsArray?(f.Counted?source.Count(f):f.ArrayBound):1;
        if(f.Dynamic) { f.EnsureCount(target,n); }
        if(f.NativeCountOffset>=0) { f.SetCount(target,source.Count(f)); }
        if(f.Optional) { f.SetPresent(target,source.Base[source.At+f.NativePresentOffset]!=0); }
        if(f.GetBuffer!=null) { source.Buffer(f).Slice(0,f.GetBuffer(target).Length).CopyTo(f.GetBuffer(target)); return; }
        if(f.GetChars!=null) { source.Chars(f).Slice(0,f.GetChars(target).Length).CopyTo(f.GetChars(target)); return; }
        for(int i=0;i<n;i++)
        {
            if(f.Kind==17)
            {
                long delta=(long)source.Raw(f,i); object child=null;
                if(delta!=0)
                {
                    long at=source.Slot(f,i)-source.Base+delta;
                    if(!nodes.TryGetValue(at,out child))
                    {
                        if(f.BlobKind!=0)
                        {
                            int length=(int)NativeWord(source.Base+at,4);
                            child=new TableBlob(new ReadOnlySpan<byte>(source.Base+at+8,length).ToArray());
                            nodes.Add(at,child);
                        }
                        else { child=f.Table.Create(); nodes.Add(at,child); NativeCopyBody(new NativeValue(source.Base,at) { Mutable=source.Mutable },child,f.Table,nodes); }
                    }
                }
                f.SetChild(target,i,child);
            }
            else if(f.Kind==13) { NativeCopyBody(source.Child(f,i),f.GetChild(target,i),f.Table,nodes); }
            else if(f.Kind==15)
            {
                NativeValue union=source.Child(f,i); ulong tag=(ulong)NativeWord(union.Base+union.At,f.Arms.NativeTagSize);
                object child=f.GetChild(target,i); f.Arms.SetTag(child,tag);
                TableFieldInfo arm=f.Arms.Arms[(int)tag].Field;
                if(arm!=null) { NativeCopyField(union.Arm(f),child,arm,nodes); }
            }
            else if(f.GetWide!=null) { f.SetWide(target,i,source.Wide(f,i)); }
            else { f.SetRaw(target,i,source.Raw(f,i)); }
        }
    }

    public static long LoadMeasureParts(TableTypeInfo type,ReadOnlySpan<byte> bytes,out long dataBytes,out long attributionBytes,out string reason)
    {
        dataBytes=attributionBytes=0;
        long total=LoadMeasure(type,bytes,out reason); if(total<0) { return -1; }
        int idsCount=(int)Read64(bytes,bytes.Length-8),end=bytes.Length-8-idsCount*8;
        Reader root=new Reader(bytes.Slice(1,end-1),bytes.Slice(end,idsCount*8));
        if(!NodeFrames(root,out _,out int nodes)) { nodes=0; }
        attributionBytes=(nodes+1L)*16; dataBytes=total-attributionBytes; return total;
    }
    // Data and attribution may be placed separately; reads after Load need
    // only data. The caller keeps both allocations stable during this call.
    public static unsafe IntPtr LoadRegion(TableTypeInfo type,IntPtr region,long capacity,ReadOnlySpan<byte> bytes,TableReport report)
    {
        long total=LoadMeasureParts(type,bytes,out long dataBytes,out long attributionBytes,out string reason);
        if(total>=0 && capacity<total) { return IntPtr.Zero; }
        return NativeLoadRegion(type,region,dataBytes,(IntPtr)((byte*)region+dataBytes),attributionBytes,bytes,report,total,dataBytes,reason);
    }
    public static unsafe IntPtr LoadRegion(TableTypeInfo type,IntPtr region,long capacity,IntPtr attribution,long attributionCapacity,ReadOnlySpan<byte> bytes,TableReport report)
    {
        long total=LoadMeasureParts(type,bytes,out long dataBytes,out _,out string reason);
        return NativeLoadRegion(type,region,capacity,attribution,attributionCapacity,bytes,report,total,dataBytes,reason);
    }
    static unsafe IntPtr NativeLoadRegion(TableTypeInfo type,IntPtr region,long capacity,IntPtr attribution,long attributionCapacity,ReadOnlySpan<byte> bytes,TableReport report,long total,long dataBytes,string reason)
    {
        if(report==null) { throw new ArgumentNullException(nameof(report)); }
        report.Refused=false; report.Reason=null;
        if(total<0)
        {
            if(reason!=null) { report.Refused=true; report.Reason=reason=="unknown_form"?(bytes[0]==2?"message_form_as_file":"newer_form"):reason; Finish(report,Verdict.Refused); }
            else { Damage(report); Finish(report,Verdict.Damaged); }
            return IntPtr.Zero;
        }
        long attributionBytes=total-dataBytes;
        if(region==IntPtr.Zero || attribution==IntPtr.Zero || capacity<dataBytes || attributionCapacity<attributionBytes || ((ulong)region%(ulong)type.RegionAlign)!=0 || ((ulong)attribution&7)!=0) { return IntPtr.Zero; }
        ulong dataAddress=(ulong)region,attrAddress=(ulong)attribution;
        if(dataAddress<=attrAddress?attrAddress-dataAddress<(ulong)dataBytes:dataAddress-attrAddress<(ulong)attributionBytes) { return IntPtr.Zero; }
        ulong idsCount=Read64(bytes,bytes.Length-8);
        int end=bytes.Length-8-(int)idsCount*8;
        ReadOnlySpan<byte> ids=bytes.Slice(end,(int)idsCount*8);
        Reader root=new Reader(bytes.Slice(1,end-1),ids);
        bool good=NodeFrames(root,out Reader records,out int count);
        if(!good) { count=0; Damage(report); }
        byte* data=(byte*)region; byte* directory=(byte*)attribution;
        System.Runtime.InteropServices.NativeMemory.Clear(data,checked((nuint)dataBytes));
        System.Runtime.InteropServices.NativeMemory.Clear(directory,checked((nuint)attributionBytes));
        NativePut(directory,0,8); NativePut(directory+8,type.Id,8);
        long rootExtent=0; string ignored=null; ExtentBody(root,type,ref rootExtent,ref ignored);
        long at=Align(Align(type.StorageSize,type.RegionAlign)+rootExtent,type.RegionAlign);
        Reader scan=records;
        for(int i=0;i<count;i++)
        {
            scan.Ref(out _,out ulong id); scan.Slice(out Reader body);
            long offset=-1; TableTypeInfo node=type.PointerType(id);
            bool blob=(type.BytesEdge && id==BytesTypeId)||(type.StringEdge && id==StringTypeId);
            if(blob)
            {
                offset=at; at+=Align(8L+body.Buffer.Length+(id==StringTypeId?1:0),type.RegionAlign);
                if(id==StringTypeId && !TextValid(body.Buffer)) { Damage(report); offset=-1; }
                else { NativePut(data+offset,(uint)body.Buffer.Length,4); body.Buffer.CopyTo(new Span<byte>(data+offset+8,body.Buffer.Length)); }
            }
            else if(node!=null)
            {
                offset=at; long extent=0; ExtentBody(body,node,ref extent,ref ignored);
                at+=Align(Align(node.StorageSize,type.RegionAlign)+extent,type.RegionAlign);
            }
            else { report.Unknown++; }
            NativePut(directory+(i+1L)*16,unchecked((ulong)offset),8); NativePut(directory+(i+1L)*16+8,id,8);
        }
        NativeState state=new NativeState { Directory=directory,Nodes=count,NodesGood=good,Root=type,Next=0,End=dataBytes };
        scan=records;
        for(int i=0;i<count;i++)
        {
            scan.Ref(out _,out ulong id); scan.Slice(out Reader body);
            long offset=(long)NativeWord(directory+(i+1L)*16,8); TableTypeInfo node=type.PointerType(id);
            if(offset<0 || node==null) { continue; }
            long extent=0; ExtentBody(body,node,ref extent,ref ignored);
            state.Next=offset+Align(node.StorageSize,type.RegionAlign); state.End=state.Next+extent;
            NativeReadBody(ref state,ref body,new NativeValue(data,offset),node,report,true);
        }
        state.Next=Align(type.StorageSize,type.RegionAlign); state.End=state.Next+rootExtent;
        bool complete=NativeReadBody(ref state,ref root,new NativeValue(data,0),type,report,false);
        Finish(report,complete?Verdict.Ok:Verdict.BodyStopped); return region;
    }
`
