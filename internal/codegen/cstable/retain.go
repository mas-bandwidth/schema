package cstable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// All family slices name an exact runtime boundary. A renamed boundary must
// fail generation rather than quietly remove one side of the retaining walk.
func tableSourceIndex(source, marker string) int {
	at := strings.Index(source, marker)
	if at < 0 {
		panic("missing table runtime boundary: " + marker)
	}
	return at
}

func tableReplace(source, old, replacement string) string {
	if !strings.Contains(source, old) {
		panic("missing table retention anchor: " + old)
	}
	return strings.ReplaceAll(source, old, replacement)
}

// Retention is emitted as a parallel family, so the ordinary native walk has
// no retention branches, path storage, or allocation cost.
func (g *tableGen) emitRetain() {
	g.tf(tableRetainStorage)
	depth := retainDepth(g.unit)
	var known []string
	for _, id := range ir.TableWireIds(g.unit) {
		known = append(known, fmt.Sprintf("0x%016xUL", id))
	}
	g.pf("public static unsafe partial class TableWire {\n%s\n}\n", strings.NewReplacer("@DEPTH@", fmt.Sprint(depth), "@KNOWN@", strings.Join(known, ",")).Replace(tableRetainSource))
}

func retainDepth(u *ir.Unit) int {
	memo := map[string]int{}
	var table func(*ir.Struct) int
	var field func(*ir.Field) int
	field = func(f *ir.Field) int {
		if f.Type.Pointer {
			return 0
		}
		if f.IsMap() {
			return 1 + table(f.MapEntry)
		}
		switch ref := f.Type.Ref.(type) {
		case *ir.Struct:
			return 1 + table(ref)
		case *ir.Union:
			n := 1
			for _, v := range ref.Variants {
				if v.F != nil {
					n = max(n, 1+field(v.F))
				}
			}
			return n + 1
		}
		return 0
	}
	table = func(st *ir.Struct) int {
		if n, ok := memo[st.Name]; ok {
			return n
		}
		memo[st.Name] = 0
		n := 0
		for _, f := range st.Fields {
			n = max(n, field(f))
		}
		memo[st.Name] = n
		return n
	}
	depth := 1
	for _, st := range u.Tables {
		depth = max(depth, table(st))
	}
	return depth
}

func (g *tableGen) emitRetainRefusals() {
	variable := ir.VariableTables(g.unit)
	for _, f := range g.unit.Files {
		for _, st := range f.Tables {
			if st.IsMapEntry() {
				continue
			}
			if !variable[st.Name] {
				why := "FIXED-class root: retain-unknown requires a variable region and its node directory (SPEC-TABLES section 6.6)."
				g.pf("[Obsolete(%q,true)] public static IntPtr %sLoadRetain(IntPtr region,long capacity,ReadOnlySpan<byte> bytes,ref TableRetain retain,TableReport report) { throw new NotSupportedException(); }\n", why, st.Name)
				g.pf("[Obsolete(%q,true)] public static long %sMeasureRetain(IntPtr region,ref TableRetain retain) { throw new NotSupportedException(); }\n", why, st.Name)
				g.pf("[Obsolete(%q,true)] public static long %sSaveRetain(IntPtr region,ref TableRetain retain,Span<byte> bytes,TableReport report) { throw new NotSupportedException(); }\n", why, st.Name)
			}
			g.pf("[Obsolete(\"Retention writing the MESSAGE form is refused by name; save each retained region as a file.\",true)] public static long %sSaveRetainMessages(ReadOnlySpan<IntPtr> roots,Span<TableRetain> retains,Span<byte> bytes,TableReport report) { throw new NotSupportedException(); }\n", st.Name)
		}
	}
}

const tableRetainStorage = `
// Caller-owned stores, belonging to one loaded region. The attribution
// directory must stay alive through MeasureRetain and SaveRetain.
public unsafe struct TableRetain
{
    public struct Id { public ulong Value; public int Slot; }
    public byte* Bytes;
    public long Capacity, Used;
    public Id* Ids;
    public int IdCapacity, IdUsed, Count;
    internal byte* Base, Directory;
    internal long DirectoryCount;
    public TableRetain() { this=default; Base=null; Directory=null; DirectoryCount=0; }
}
`

const tableRetainSource = `
    const int RetainHeader=26;
    static readonly ulong[] RetainKnown = new ulong[] { @KNOWN@ };
    unsafe struct RetainPath
    {
        public long At;
        public uint Node;
        public int Depth;
        public fixed uint Steps[@DEPTH@*2];
        public RetainPath Step(int ordinal,int index)
        {
            RetainPath next=this;
            if(next.Depth>=@DEPTH@) { throw new InvalidOperationException("Schema retention path depth"); }
            next.Steps[next.Depth*2]=(uint)ordinal;
            next.Steps[next.Depth*2+1]=(uint)index;
            next.Depth++; return next;
        }
    }
    static uint Retain32(byte* p) { return (uint)p[0]|(uint)p[1]<<8|(uint)p[2]<<16|(uint)p[3]<<24; }
    static ulong Retain64(byte* p) { return Retain32(p)|((ulong)Retain32(p+4)<<32); }
    static void RetainPut32(byte* p,uint v) { for(int i=0;i<4;i++) { p[i]=(byte)(v>>(8*i)); } }
    static void RetainPut64(byte* p,ulong v) { RetainPut32(p,(uint)v); RetainPut32(p+4,(uint)(v>>32)); }
    static byte* RetainPayload(byte* record) { return record+RetainHeader+8L*Retain32(record+8); }
    static bool RetainReserved(ulong id) { return id>=ulong.MaxValue-2; }
    static long RetainAt(TableRetain* retain,byte* record)
    {
        uint node=Retain32(record+4);
        return retain->Directory==null || node==0 || node>retain->DirectoryCount?-1:(long)NativeWord(retain->Directory+16L*(node-1),8);
    }
    static bool RetainHere(TableRetain* retain,byte* record,RetainPath path,bool prefix=false,int field=-1)
    {
        if(RetainAt(retain,record)!=path.At) { return false; }
        uint depth=Retain32(record+8);
        if(prefix?depth<path.Depth:depth!=path.Depth) { return false; }
        if(field>=0 && depth<=path.Depth) { return false; }
        byte* steps=record+RetainHeader;
        for(int i=0;i<path.Depth*2;i++) { if(Retain32(steps+4*i)!=path.Steps[i]) { return false; } }
        return field<0 || Retain32(steps+8*path.Depth)==(uint)field;
    }
    static void RetainDiscard(TableRetain* retain,RetainPath path,int field=-1)
    {
        if(retain==null) { return; }
        long from=0,to=0; int kept=0;
        for(int i=0;i<retain->Count;i++)
        {
            byte* record=retain->Bytes+from; int n=checked((int)Retain32(record));
            if(!RetainHere(retain,record,path,true,field))
            { if(from!=to) { new ReadOnlySpan<byte>(record,n).CopyTo(new Span<byte>(retain->Bytes+to,n)); } to+=n; kept++; }
            from+=n;
        }
        retain->Count=kept; retain->Used=to;
    }
    ref struct RetainIn
    {
        public Reader R;
        public byte* Output;
        public long At,Limit;
        public void Raw(ReadOnlySpan<byte> data)
        { if(Output!=null) { data.CopyTo(new Span<byte>(Output+At,data.Length)); } At+=data.Length; }
        public void Fixed(ulong value,int bytes)
        { if(Output!=null) { for(int i=0;i<bytes;i++) { Output[At+i]=(byte)(value>>(8*i)); } } At+=bytes; }
        public void Var(ulong value)
        { while(value>=128) { Fixed((byte)(value|128),1); value>>=7; } Fixed(value,1); }
    }
    static bool RetainInRef(ref RetainIn s,bool zero)
    {
        if(!s.R.Ref(out ulong reference,out ulong id) || reference==0 && !zero || reference!=0 && (id==0 || RetainReserved(id))) { return false; }
        s.Fixed(id,8); return true;
    }
    static bool RetainInFrame(ref RetainIn s,byte kind,int bytes,int depth)
    {
        long slot=s.At; s.Fixed(0,8); long began=s.At;
        if(!RetainInContent(ref s,kind,bytes,depth) || s.At-began>uint.MaxValue) { return false; }
        if(s.Output!=null) { RetainPut32(s.Output+slot,(uint)(s.At-began)); } return true;
    }
    static bool RetainInContent(ref RetainIn s,byte kind,int bytes,int depth)
    {
        if(depth>64 || s.At>s.Limit || !s.R.Has(bytes)) { return false; }
        int end=s.R.Offset+bytes;
        switch(kind)
        {
            case 13:
                for(;;)
                {
                    int mark=s.R.Offset;
                    if(!s.R.Var(out ulong field)) { return false; }
                    if(field==0) { s.Fixed(0,8); break; }
                    s.R.Offset=mark;
                    if(!RetainInRef(ref s,false) || s.R.Offset>=end) { return false; }
                    byte fieldKind=s.R.Byte(); s.Fixed(fieldKind,1);
                    if(!RetainInPayload(ref s,fieldKind,depth) || s.R.Offset>end) { return false; }
                }
                break;
            case 14: case 16:
                if(s.R.Offset>=end) { return false; }
                byte element=s.R.Byte(); s.Fixed(element,1);
                if(!s.R.Var(out ulong count)) { return false; } s.Var(count);
                for(ulong i=0;i<count;i++)
                {
                    if(kind==16)
                    {
                        if(!RetainInRef(ref s,false) || !s.R.Var(out ulong size) || size>(ulong)(end-s.R.Offset) || !RetainInFrame(ref s,element,(int)size,depth+1)) { return false; }
                    }
                    else if(!RetainInPayload(ref s,element,depth)) { return false; }
                    if(s.R.Offset>end) { return false; }
                }
                break;
            case 15: case 30:
                if(!RetainInPayload(ref s,kind,depth)) { return false; } break;
            case 17: return false;
            default:
                s.Raw(s.R.Buffer.Slice(s.R.Offset,bytes)); s.R.Offset+=bytes; break;
        }
        return s.R.Offset==end;
    }
    static bool RetainInPayload(ref RetainIn s,byte kind,int depth)
    {
        if(s.At>s.Limit) { return false; }
        int width=Width(kind);
        if(width!=0)
        { if(!s.R.Has(width)) { return false; } s.Raw(s.R.Buffer.Slice(s.R.Offset,width)); s.R.Offset+=width; return true; }
        switch(kind)
        {
            case 12: case 31: case 32: case 33:
                if(!s.R.Var(out ulong size) || size>(ulong)(s.R.Buffer.Length-s.R.Offset)) { return false; }
                s.Var(size); s.Raw(s.R.Buffer.Slice(s.R.Offset,(int)size)); s.R.Offset+=(int)size; return true;
            case 13: case 14: case 16:
                if(!s.R.Var(out ulong bytes) || bytes>(ulong)(s.R.Buffer.Length-s.R.Offset)) { return false; }
                return RetainInFrame(ref s,kind,(int)bytes,depth+1);
            case 15:
                int mark=s.R.Offset;
                if(!s.R.Var(out ulong arm)) { return false; } s.R.Offset=mark;
                if(!RetainInRef(ref s,true)) { return false; } if(arm==0) { return true; }
                if(!s.R.Has(1)) { return false; }
                byte armKind=s.R.Byte(); s.Fixed(armKind,1);
                if(!s.R.Var(out ulong armBytes) || armBytes>(ulong)(s.R.Buffer.Length-s.R.Offset)) { return false; }
                return RetainInFrame(ref s,armKind,(int)armBytes,depth+1);
            case 30: return RetainInRef(ref s,true);
            default: return false;
        }
    }
    static bool RetainCapture(TableRetain* retain,RetainPath path,ref Reader r,ulong id,byte kind,TableReport report)
    {
        int mark=r.Offset;
        if(!r.Skip(kind)) { return false; }
        if(retain==null) { return true; }
        Reader payload=new Reader(r.Buffer.Slice(mark,r.Offset-mark),r.Vocabulary);
        RetainIn probe=new RetainIn { R=payload,Limit=retain->Capacity-retain->Used-RetainHeader-8L*path.Depth };
        if(!RetainInPayload(ref probe,kind,0) || probe.R.Offset!=payload.Buffer.Length)
        { report.RetainLost++; return true; }
        long need=RetainHeader+8L*path.Depth+probe.At;
        if(need>int.MaxValue || retain->Bytes==null || retain->Used>retain->Capacity-need)
        { report.RetainLost++; return true; }
        byte* record=retain->Bytes+retain->Used;
        RetainPut32(record,(uint)need); RetainPut32(record+4,path.Node); RetainPut32(record+8,(uint)path.Depth);
        RetainPut32(record+12,(uint)probe.At); RetainPut64(record+16,id); record[24]=kind; record[25]=0;
        for(int i=0;i<path.Depth*2;i++) { RetainPut32(record+RetainHeader+4*i,path.Steps[i]); }
        RetainIn write=new RetainIn { R=payload,Output=RetainPayload(record),Limit=probe.At };
        if(!RetainInPayload(ref write,kind,0)) { report.RetainLost++; return true; }
        retain->Used+=need; retain->Count++; report.Retained++; return true;
    }
    ref struct RetainIds
    {
        public Span<ulong> Values;
        public Span<int> Slots;
        public int KnownCount,Count;
        public TableRetain* Store;
        public RegionGraph Graph;
        public TableTypeInfo RootType;
        public bool Overflow,Lost;
        public RetainIds(Span<ulong> values,Span<int> slots,TableRetain* store,RegionGraph graph,TableTypeInfo type)
        { Values=values; Slots=slots; KnownCount=Count=0; Store=store; Graph=graph; RootType=type; Overflow=Lost=false; store->IdUsed=0; }
        public ulong Reference(ulong id)
        {
            for(int i=0;i<KnownCount;i++) { if(Values[i]==id) { return (ulong)Slots[i]; } }
            if(KnownCount==Values.Length) { Overflow=true; return 0; }
            Values[KnownCount]=id; Slots[KnownCount++]=++Count; return (ulong)Count;
        }
        public ulong Record(ulong id)
        {
            if(Array.BinarySearch(RetainKnown,id)>=0) { return Reference(id); }
            for(int i=0;i<Store->IdUsed;i++) { if(Store->Ids[i].Value==id) { return (ulong)Store->Ids[i].Slot; } }
            if(Store->Ids==null || Store->IdUsed>=Store->IdCapacity) { Lost=true; return 0; }
            Store->Ids[Store->IdUsed++]=new TableRetain.Id { Value=id,Slot=++Count }; return (ulong)Count;
        }
        public void Truncate(int mark)
        {
            while(KnownCount>0 && Slots[KnownCount-1]>mark) { KnownCount--; }
            while(Store->IdUsed>0 && Store->Ids[Store->IdUsed-1].Slot>mark) { Store->IdUsed--; }
            Count=mark; Lost=false;
        }
        public void Write(ref Writer w)
        {
            int i=0,j=0;
            while(i<KnownCount || j<Store->IdUsed)
            { if(j==Store->IdUsed || i<KnownCount && Slots[i]<Store->Ids[j].Slot) { w.Fixed(Values[i++],8); } else { w.Fixed(Store->Ids[j++].Value,8); } }
            w.Fixed((ulong)Count,8);
        }
    }
    ref struct RetainOut
    {
        public Reader R;
        public Writer W;
        public byte* Input;
        public bool Measure;
        public long Bytes;
        public void Raw(ReadOnlySpan<byte> data) { if(!Measure) { W.Raw(data); } Bytes+=data.Length; }
        public void Var(ulong value) { if(!Measure) { W.Var(value); } Bytes+=VarSize(value); }
        public void Byte(byte value) { if(!Measure) { W.Byte(value); } Bytes++; }
    }
    static bool RetainOutRef(ref RetainOut s,ref RetainIds ids)
    {
        if(!s.R.Has(8)) { return false; }
        ulong id=s.R.Fixed(8); s.Var(id==0?0:ids.Record(id)); return !ids.Lost && !ids.Overflow;
    }
    static bool RetainOutFrame(ref RetainOut s,ref RetainIds ids,byte kind,int depth)
    {
        if(!s.R.Has(8)) { return false; }
        byte* slot=s.Input+s.R.Offset; uint size=Retain32(slot); s.R.Offset+=8;
        if(size>int.MaxValue) { return false; }
        if(s.Measure)
        {
            long began=s.Bytes;
            if(!RetainOutContent(ref s,ref ids,kind,(int)size,depth) || s.Bytes-began>uint.MaxValue) { return false; }
            uint wire=(uint)(s.Bytes-began); RetainPut32(slot+4,wire); s.Bytes+=VarSize(wire); return true;
        }
        uint cached=Retain32(slot+4); s.Var(cached); long start=s.Bytes;
        return RetainOutContent(ref s,ref ids,kind,(int)size,depth) && s.Bytes-start==cached;
    }
    static bool RetainOutContent(ref RetainOut s,ref RetainIds ids,byte kind,int bytes,int depth)
    {
        if(depth>64 || !s.R.Has(bytes)) { return false; }
        int end=s.R.Offset+bytes;
        switch(kind)
        {
            case 13:
                for(;;)
                {
                    if(!s.R.Has(8)) { return false; }
                    ulong id=s.R.Fixed(8);
                    if(id==0) { s.Var(0); break; }
                    s.R.Offset-=8;
                    if(!RetainOutRef(ref s,ref ids) || !s.R.Has(1)) { return false; }
                    byte field=s.R.Byte(); s.Byte(field);
                    if(!RetainOutPayload(ref s,ref ids,field,depth)) { return false; }
                }
                break;
            case 14: case 16:
                if(!s.R.Has(1)) { return false; }
                byte element=s.R.Byte(); s.Byte(element);
                if(!s.R.Var(out ulong count)) { return false; } s.Var(count);
                for(ulong i=0;i<count;i++)
                {
                    if(kind==16)
                    { if(!RetainOutRef(ref s,ref ids) || !RetainOutFrame(ref s,ref ids,element,depth+1)) { return false; } }
                    else if(!RetainOutPayload(ref s,ref ids,element,depth)) { return false; }
                }
                break;
            case 15: case 30:
                if(!RetainOutPayload(ref s,ref ids,kind,depth)) { return false; } break;
            case 17: return false;
            default: s.Raw(s.R.Buffer.Slice(s.R.Offset,bytes)); s.R.Offset+=bytes; break;
        }
        return s.R.Offset==end;
    }
    static bool RetainOutPayload(ref RetainOut s,ref RetainIds ids,byte kind,int depth)
    {
        int width=Width(kind);
        if(width!=0)
        { if(!s.R.Has(width)) { return false; } s.Raw(s.R.Buffer.Slice(s.R.Offset,width)); s.R.Offset+=width; return true; }
        switch(kind)
        {
            case 12: case 31: case 32: case 33:
                if(!s.R.Var(out ulong size) || size>(ulong)(s.R.Buffer.Length-s.R.Offset)) { return false; }
                s.Var(size); s.Raw(s.R.Buffer.Slice(s.R.Offset,(int)size)); s.R.Offset+=(int)size; return true;
            case 13: case 14: case 16: return RetainOutFrame(ref s,ref ids,kind,depth+1);
            case 15:
                if(!s.R.Has(8)) { return false; }
                ulong arm=s.R.Fixed(8); s.R.Offset-=8;
                if(!RetainOutRef(ref s,ref ids)) { return false; } if(arm==0) { return true; }
                if(!s.R.Has(1)) { return false; }
                byte armKind=s.R.Byte(); s.Byte(armKind); return RetainOutFrame(ref s,ref ids,armKind,depth+1);
            case 30: return RetainOutRef(ref s,ref ids);
            default: return false;
        }
    }
    static long RetainRecordWire(byte* record,ref RetainIds ids,out ulong reference)
    {
        int mark=ids.Count; ids.Lost=false;
        reference=ids.Record(Retain64(record+16));
        RetainOut s=new RetainOut { R=new Reader(new ReadOnlySpan<byte>(RetainPayload(record),(int)Retain32(record+12)),default),Input=RetainPayload(record),Measure=true };
        if(!ids.Lost && !ids.Overflow && RetainOutPayload(ref s,ref ids,record[24],0) && s.R.Offset==s.R.Buffer.Length)
        { return VarSize(reference)+1+s.Bytes; }
        ids.Truncate(mark); return -1;
    }
    static bool RetainHas(TableRetain* retain,RetainPath path)
    {
        if(retain==null) { return false; }
        long at=0;
        for(int i=0;i<retain->Count;i++)
        { byte* record=retain->Bytes+at; at+=Retain32(record); if(RetainHere(retain,record,path)) { return true; } }
        return false;
    }
    static long RetainTailMeasure(ref RetainIds ids,RetainPath path)
    {
        long at=0,bytes=0;
        for(int i=0;i<ids.Store->Count;i++)
        {
            byte* record=ids.Store->Bytes+at; at+=Retain32(record);
            if(!RetainHere(ids.Store,record,path)) { continue; }
            long size=RetainRecordWire(record,ref ids,out _); if(size>=0) { bytes+=size; }
        }
        return bytes;
    }
    static void RetainTailSave(ref Writer w,ref RetainIds ids,RetainPath path)
    {
        long at=0;
        for(int i=0;i<ids.Store->Count;i++)
        {
            byte* record=ids.Store->Bytes+at; at+=Retain32(record);
            if(!RetainHere(ids.Store,record,path) || RetainRecordWire(record,ref ids,out ulong reference)<0) { continue; }
            w.Var(reference); w.Byte(record[24]);
            RetainOut s=new RetainOut { R=new Reader(new ReadOnlySpan<byte>(RetainPayload(record),(int)Retain32(record+12)),default),Input=RetainPayload(record),W=w };
            if(!RetainOutPayload(ref s,ref ids,record[24],0)) { throw new InvalidOperationException("Retention measure/write disagreement"); }
            w=s.W; record[25]=1;
        }
    }
`
