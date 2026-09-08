package cstable

import (
	"sort"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func (g *tableGen) emitBuilders(names []string) {
	g.tf("%s", tableBuilderSource+tableBuilderCollectionsSource)
	g.pf("internal static TableTypeInfo TableBuilderType<T>() where T:unmanaged\n{\n")
	closure := ir.TableClosure(g.unit)
	var rows []string
	for name := range closure {
		if g.unit.Tables[name] != nil || g.unit.Structs[name] != nil {
			rows = append(rows, name)
		}
	}
	sort.Strings(rows)
	for _, n := range rows {
		g.pf("    if(typeof(T)==typeof(%sRow)) { return %sTableType(); }\n", n, n)
	}
	g.pf("    throw new ArgumentException(\"T must be a generated native table or type row in this unit\");\n}\n")
	for _, n := range names {
		if g.unit.Tables[n].IsMapEntry() {
			continue
		}
		g.tf("public sealed unsafe class %sBuilder : TableBuilder\n{\n", n)
		g.tf("    public %sBuilder(TableAllocator allocator=default):base(Schema.%sTableType(),allocator) {}\n", n, n)
		g.pf("public static bool %sLoadBuilder(%sBuilder builder,ReadOnlySpan<byte> bytes,TableReport report) { return builder.Load(bytes,report); }\n", n, n)
		g.tf("    public %sRow* GetRoot() { return (%sRow*)MutableRoot; }\n", n, n)
		g.tf("    public %sRow* AsConst() { return (%sRow*)LockedRoot; }\n}\n\n", n, n)
	}
}

const tableBuilderSource = `
// Stable native slabs. Create one worker per filling thread and join workers
// before saving, locking or disposing. Only acquiring a slab synchronizes.
public sealed unsafe class TableArena : IDisposable
{
    internal const int SlabBytes=64*1024;
    const int SegmentBytes=4*1024*1024;
    struct Segment { public Segment* Next; public long NextByte,Bytes; }
    Segment* segments;
    Segment* current;
    readonly object gate=new object();
    internal readonly TableAllocator Allocator;
    internal bool Closed;
    public TableArena(TableAllocator allocator=default) { Allocator=allocator; }
    internal byte* Grab(long bytes)
    {
        if(bytes<=0 || bytes>long.MaxValue-64) { return null; }
        bytes=(bytes+15)&~15L;
        lock(gate)
        {
            if(Closed) { return null; }
            Segment* segment=current;
            if(bytes>SlabBytes || segment==null || bytes>segment->Bytes-segment->NextByte)
            {
                long capacity=bytes>SlabBytes?bytes+64:SegmentBytes;
                segment=(Segment*)Allocator.Get(capacity);
                if(segment==null) { return null; }
                segment->Bytes=capacity; segment->NextByte=64; segment->Next=segments; segments=segment;
                if(bytes<=SlabBytes) { current=segment; }
            }
            byte* result=(byte*)segment+segment->NextByte; segment->NextByte+=bytes; return result;
        }
    }
    public TableWorker CreateWorker() { return new TableWorker(this); }
    public void Dispose()
    {
        lock(gate)
        {
            if(Closed) { return; } Closed=true;
            while(segments!=null) { Segment* next=segments->Next; Allocator.Release(segments); segments=next; }
            current=null;
        }
    }
    // Arena references use the same self-relative slot as regions. Slabs never
    // move, so the reference remains valid even across separately allocated slabs.
    public static void SetReference(ref long slot,void* target)
    { fixed(long* p=&slot) { slot=target==null?0:(byte*)target-(byte*)p; } }
    public static T* At<T>(ref long slot) where T:unmanaged
    { fixed(long* p=&slot) { return slot==0?null:(T*)((byte*)p+slot); } }
}

public unsafe struct TableBlobSlot
{
    public IntPtr Pointer;
    public long Length;
    public long ByteLength;
    public byte* Data { get { return Pointer==IntPtr.Zero?null:(byte*)Pointer+8; } }
    public Span<byte> Bytes { get { return new Span<byte>(Data,checked((int)ByteLength)); } }
}

public sealed unsafe class TableWorker
{
    internal readonly TableArena Arena;
    byte* next;
    byte* end;
    internal TableWorker(TableArena arena) { Arena=arena; }
    internal byte* Raw(long bytes)
    {
        if(Arena.Closed || bytes<=0 || bytes>long.MaxValue-15) { return null; }
        bytes=(bytes+15)&~15L;
        if(bytes>TableArena.SlabBytes) { return Arena.Grab(bytes); }
        if(next==null || end-next<bytes)
        {
            next=Arena.Grab(TableArena.SlabBytes); if(next==null) { return null; }
            end=next+TableArena.SlabBytes;
        }
        byte* result=next; next+=bytes; return result;
    }
    public TableList<T> List<T>(ref TableCookList slot,TableFieldInfo field) where T:unmanaged
    {
        if(!field.Dynamic || field.Map || sizeof(T)!=field.NativeElementSize) { throw new ArgumentException("list element must match its descriptor"); }
        fixed(TableCookList* p=&slot) { return new TableList<T>(this,p,field); }
    }
    public TableMap<T> Map<T>(ref TableCookList slot,TableFieldInfo field) where T:unmanaged
    {
        if(!field.Map || sizeof(T)!=field.NativeElementSize) { throw new ArgumentException("map entry must match its descriptor"); }
        fixed(TableCookList* p=&slot) { return new TableMap<T>(this,p,field); }
    }
    static class Row<T> where T:unmanaged { internal static readonly TableTypeInfo Type=Schema.TableBuilderType<T>(); }
    public T* Alloc<T>() where T:unmanaged
    {
        TableTypeInfo type=Row<T>.Type;
        byte* p=Raw(type.StorageSize);
        if(p!=null) { Schema.TableWire.ResetNative((IntPtr)p,type); }
        return (T*)p;
    }
    public TableBlobSlot AllocBytes(long length) { return Blob(length,0); }
    public TableBlobSlot AllocString(long length) { return Blob(length,1); }
    public TableBlobSlot AllocWString(long units)
    { if(units<0 || units>int.MaxValue/2) { return default; } TableBlobSlot slot=Blob(units*2,2); if(slot.Pointer!=IntPtr.Zero) { slot.Length=units; } return slot; }
    TableBlobSlot Blob(long length,int terminator)
    {
        if(length<0 || length>int.MaxValue) { return default; }
        byte* p=Raw(8+length+terminator);
        if(p==null) { return default; }
        *(ulong*)p=(ulong)length;
        return new TableBlobSlot { Pointer=(IntPtr)p,Length=length,ByteLength=length };
    }
}

public abstract unsafe class TableBuilder : IDisposable
{
    readonly TableTypeInfo type;
    readonly TableAllocator allocator;
    readonly TableArena arena;
    readonly TableWorker worker;
    IntPtr root,packed;
    bool disposed,locked;
    public long DataBytes { get; private set; }
    public long AttributionBytes { get; private set; }
    public IntPtr Region { get { return LockedRoot; } }
    public long RegionBytes { get { return DataBytes+AttributionBytes; } }
    public bool IsLocked { get { return locked && !disposed; } }
    protected IntPtr MutableRoot { get { return disposed || locked?IntPtr.Zero:root; } }
    protected IntPtr LockedRoot { get { return disposed || !locked?IntPtr.Zero:packed; } }
    protected TableBuilder(TableTypeInfo type,TableAllocator allocator)
    {
        this.type=type; this.allocator=allocator;
        arena=new TableArena(allocator); worker=arena.CreateWorker();
        root=(IntPtr)worker.Raw(type.StorageSize);
        if(root!=IntPtr.Zero) { Schema.TableWire.ResetNative(root,type); }
    }
    public TableWorker CreateWorker() { return arena.CreateWorker(); }
    public T* Alloc<T>() where T:unmanaged { return worker.Alloc<T>(); }
    public TableBlobSlot AllocBytes(long bytes) { return worker.AllocBytes(bytes); }
    public TableBlobSlot AllocString(long bytes) { return worker.AllocString(bytes); }
    public TableBlobSlot AllocWString(long units) { return worker.AllocWString(units); }
    public bool Lock()
    {
        if(disposed) { return false; } if(locked) { return true; }
        if(root==IntPtr.Zero) { return false; }
        IntPtr result=Schema.TableWire.PackOwned(root,type,allocator,out long data,out long attribution,true);
        if(result==IntPtr.Zero) { return false; }
        packed=result; DataBytes=data; AttributionBytes=attribution; locked=true; root=IntPtr.Zero;
        arena.Dispose(); return true;
    }
    public bool Load(ReadOnlySpan<byte> bytes,TableReport report)
    { return !disposed && !locked && Schema.TableWire.LoadBuilder(root,type,worker,bytes,report); }
    public bool CopyFrom(IntPtr region)
    {
        if(disposed || locked || region==IntPtr.Zero) { return false; }
        long n=Schema.TableWire.PackSize(region,type,allocator,out _,out _);
        if(n<0) { return false; }
        byte* target=worker.Raw(n); if(target==null) { return false; }
        if(!Schema.TableWire.PackInto(region,type,(IntPtr)target,n,allocator,out _,out _)) { return false; }
        root=(IntPtr)target; return true;
    }
    public long Measure() { return Schema.TableWire.MeasureBuilder(disposed?IntPtr.Zero:locked?packed:root,type,allocator,!locked); }
    public long Save(Span<byte> bytes) { return Schema.TableWire.SaveBuilder(disposed?IntPtr.Zero:locked?packed:root,type,bytes,allocator,!locked); }
    public long CookMeasure() { return Schema.TableWire.CookRegion(disposed?IntPtr.Zero:locked?packed:root,type,IntPtr.Zero,0,TableByteOrder.Little,true,allocator,!locked); }
    public bool Cook(Span<byte> bytes,TableByteOrder order=TableByteOrder.Little)
    { fixed(byte* p=bytes) { return Schema.TableWire.CookRegion(disposed?IntPtr.Zero:locked?packed:root,type,(IntPtr)p,bytes.Length,order,false,allocator,!locked)>=0; } }
    public void Dispose()
    {
        if(disposed) { return; } disposed=true; arena.Dispose(); allocator.Release((void*)packed); packed=root=IntPtr.Zero;
        DataBytes=AttributionBytes=0;
    }
}
`

const tableBuilderWireSource = `
    public static void ResetNative(IntPtr pointer,TableTypeInfo type) { NativeReset(new NativeValue((byte*)pointer,0),type); }
    public static long MeasureBuilder(IntPtr root,TableTypeInfo type,TableAllocator allocator,bool mutable=false)
    { Span<ulong> ids=stackalloc ulong[tableOwnVocabulary.Count]; return SaveRegion(root,type,Span<byte>.Empty,ids,true,allocator,mutable); }
    public static long SaveBuilder(IntPtr root,TableTypeInfo type,Span<byte> bytes,TableAllocator allocator,bool mutable=false)
    { Span<ulong> ids=stackalloc ulong[tableOwnVocabulary.Count]; return SaveRegion(root,type,bytes,ids,false,allocator,mutable); }
    public static long PackSize(IntPtr pointer,TableTypeInfo type,TableAllocator allocator,out long data,out long attribution,bool mutable=false)
    {
        data=attribution=0; if(pointer==IntPtr.Zero) { return -1; }
        NativeValue root=new NativeValue((byte*)pointer,0) { Mutable=mutable }; RegionGraph graph=RegionNumber(root,type,allocator);
        if(!graph.Valid) { return -1; }
        try
        {
            data=RegionPlace(root,type,ref graph,out _); if(data<0) { return -1; }
            attribution=((long)graph.Count+1)*16; return checked(data+attribution);
        }
        catch(OverflowException) { return -1; }
        finally { graph.Dispose(); }
    }
    public static bool PackInto(IntPtr pointer,TableTypeInfo type,IntPtr target,long capacity,TableAllocator allocator,out long data,out long attribution,bool mutable=false)
    {
        data=attribution=0; if(pointer==IntPtr.Zero || target==IntPtr.Zero) { return false; }
        NativeValue root=new NativeValue((byte*)pointer,0) { Mutable=mutable }; RegionGraph graph=RegionNumber(root,type,allocator);
        if(!graph.Valid) { return false; }
        try
        {
            data=RegionPlace(root,type,ref graph,out _); if(data<0) { return false; }
            attribution=((long)graph.Count+1)*16; long size=checked(data+attribution);
            if(size>capacity || (ulong)size>(ulong)nuint.MaxValue) { return false; }
            System.Runtime.InteropServices.NativeMemory.Clear((void*)target,(nuint)size);
            RegionPackWriter writer=new RegionPackWriter { Bytes=(byte*)target,Big=!BitConverter.IsLittleEndian,Graph=graph };
            RegionPack(ref writer,root,type,data,true); return true;
        }
        catch(OverflowException) { return false; }
        finally { graph.Dispose(); }
    }
    public static IntPtr PackOwned(IntPtr pointer,TableTypeInfo type,TableAllocator allocator,out long data,out long attribution,bool mutable=false)
    {
        long size=PackSize(pointer,type,allocator,out data,out attribution,mutable);
        if(size<0) { return IntPtr.Zero; }
        void* target=allocator.Get(size); if(target==null) { return IntPtr.Zero; }
        if(!PackInto(pointer,type,(IntPtr)target,size,allocator,out long writtenData,out long writtenAttribution,mutable) || writtenData!=data || writtenAttribution!=attribution)
        { allocator.Release(target); return IntPtr.Zero; }
        return (IntPtr)target;
    }
`
