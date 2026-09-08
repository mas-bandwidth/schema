package cstable

const tableBuilderCollectionsSource = `
// A mutable collection owns stable elements and a movable index of addresses.
// Its count is the record's Count; Capacity is allocation, never live data.
// Capacity zero denotes an ordinary contiguous loaded/locked region extent.
internal static unsafe class TableListStorage
{
    internal struct Head { public byte** Items; public int Capacity; }
    internal static byte* At(byte* address,int index,long elementBytes)
    {
        TableCookList* slot=(TableCookList*)address;
        if(slot->Capacity==0) { return address+slot->Reference+(long)index*elementBytes; }
        Head* head=(Head*)(address+slot->Reference); return head->Items[index];
    }
    internal static bool Reserve(TableWorker worker,TableCookList* slot,TableFieldInfo field,int wanted)
    {
        if(worker.Arena.Closed || wanted<0 || slot->Count<0) { return false; }
        if(slot->Capacity>=wanted && slot->Capacity!=0) { return true; }
        int old=slot->Capacity==0?slot->Count:slot->Capacity;
        int capacity=(int)Math.Min(int.MaxValue,Math.Max((long)wanted,Math.Max(64L,(long)old*2)));
        if(wanted==0 && old==0) { return true; }
        Head* head=slot->Capacity==0?(Head*)worker.Raw(sizeof(Head)):(Head*)((byte*)slot+slot->Reference);
        byte** items=(byte**)worker.Raw((long)capacity*sizeof(byte*));
        byte* elements=capacity==old?null:worker.Raw((long)(capacity-old)*field.NativeElementSize);
        if(head==null || items==null || capacity!=old && elements==null) { return false; }
        // Adopting an existing loaded extent preserves every old element's address.
        for(int i=0;i<old;i++) { items[i]=At((byte*)slot,i,field.NativeElementSize); }
        for(int i=old;i<capacity;i++) { items[i]=elements+(long)(i-old)*field.NativeElementSize; }
        head->Items=items; head->Capacity=capacity;
        slot->Reference=(byte*)head-(byte*)slot; slot->Capacity=capacity; return true;
    }
    internal static byte* Add(TableWorker worker,TableCookList* slot,TableFieldInfo field)
    {
        if(slot->Count==int.MaxValue || !Reserve(worker,slot,field,slot->Count+1)) { return null; }
        byte* p=At((byte*)slot,slot->Count,field.NativeElementSize);
        Schema.TableWire.ResetNativeElement((IntPtr)p,field); slot->Count++; return p;
    }
    internal static bool Erase(TableWorker worker,TableCookList* slot,TableFieldInfo field,int index)
    {
        if(worker.Arena.Closed || index<0 || index>=slot->Count || !Reserve(worker,slot,field,slot->Count)) { return false; }
        Head* head=(Head*)((byte*)slot+slot->Reference); byte* removed=head->Items[index];
        for(int i=index;i<slot->Count-1;i++) { head->Items[i]=head->Items[i+1]; }
        head->Items[--slot->Count]=removed; return true;
    }
}

public readonly unsafe struct TableList<T> where T:unmanaged
{
    readonly TableWorker worker;
    readonly TableCookList* slot;
    readonly TableFieldInfo field;
    internal TableList(TableWorker worker,TableCookList* slot,TableFieldInfo field)
    { this.worker=worker; this.slot=slot; this.field=field; }
    public int Count { get { return worker.Arena.Closed?0:slot->Count; } }
    public int Capacity { get { return worker.Arena.Closed?0:Math.Max(slot->Capacity,slot->Count); } }
    public T* At(int index) { return index<0 || index>=Count?null:(T*)TableListStorage.At((byte*)slot,index,field.NativeElementSize); }
    public T* Add() { return worker.Arena.Closed?null:(T*)TableListStorage.Add(worker,slot,field); }
    public bool Reserve(int count) { return TableListStorage.Reserve(worker,slot,field,count); }
    public bool Erase(int index) { return TableListStorage.Erase(worker,slot,field,index); }
    public void Clear() { if(!worker.Arena.Closed) { slot->Count=0; } }
}

public readonly unsafe struct TableMap<T> where T:unmanaged
{
    readonly TableWorker worker;
    readonly TableCookList* slot;
    readonly TableFieldInfo field;
    internal TableMap(TableWorker worker,TableCookList* slot,TableFieldInfo field)
    { this.worker=worker; this.slot=slot; this.field=field; }
    public int Count { get { return worker.Arena.Closed?0:slot->Count; } }
    public T* At(int index) { return index<0 || index>=Count?null:(T*)TableListStorage.At((byte*)slot,index,field.NativeElementSize); }
    public T* Find(long key) { return Find(unchecked((ulong)key)); }
    public T* Find(ulong key) { return FindKey(default,key,false); }
    public T* Find(ReadOnlySpan<byte> key) { return FindKey(key,0,true); }
    T* FindKey(ReadOnlySpan<byte> text,ulong raw,bool textual)
    { int at=Search(text,raw,textual,out bool found); return found?At(at):null; }
    public T* Insert(long key) { return Insert(unchecked((ulong)key)); }
    public T* Insert(ulong key) { return InsertKey(default,key,false); }
    public T* Insert(ReadOnlySpan<byte> key) { return InsertKey(key,0,true); }
    int Search(ReadOnlySpan<byte> text,ulong raw,bool textual,out bool found)
    {
        found=false;
        if(worker.Arena.Closed || !Schema.TableWire.BuilderKeyValid(field.Table.Fields[0],text,raw,textual)) { return -1; }
        int lo=0,hi=Count;
        while(lo<hi)
        {
            int mid=lo+(hi-lo)/2; int order=Schema.TableWire.BuilderKeyCompare((IntPtr)At(mid),field.Table.Fields[0],text,raw);
            if(order<0) { lo=mid+1; } else { hi=mid; }
        }
        found=lo<Count && Schema.TableWire.BuilderKeyCompare((IntPtr)At(lo),field.Table.Fields[0],text,raw)==0;
        return lo;
    }
    T* InsertKey(ReadOnlySpan<byte> text,ulong raw,bool textual)
    {
        int at=Search(text,raw,textual,out bool found); if(at<0) { return null; } if(found) { return At(at); }
        byte* entry=TableListStorage.Add(worker,slot,field); if(entry==null) { return null; }
        Schema.TableWire.BuilderKeySet((IntPtr)entry,field.Table.Fields[0],text,raw);
        TableListStorage.Head* head=(TableListStorage.Head*)((byte*)slot+slot->Reference);
        for(int i=slot->Count-1;i>at;i--) { head->Items[i]=head->Items[i-1]; }
        head->Items[at]=entry; return (T*)entry;
    }
    public bool Erase(long key) { return Erase(unchecked((ulong)key)); }
    public bool Erase(ulong key) { return EraseKey(default,key,false); }
    public bool Erase(ReadOnlySpan<byte> key) { return EraseKey(key,0,true); }
    bool EraseKey(ReadOnlySpan<byte> text,ulong raw,bool textual)
    { int at=Search(text,raw,textual,out bool found); return found && TableListStorage.Erase(worker,slot,field,at); }
    public void Clear() { if(!worker.Arena.Closed) { slot->Count=0; } }
}
`

const tableBuilderCollectionWireSource = `
    public static void ResetNativeElement(IntPtr pointer,TableFieldInfo field)
    {
        if(field.Kind==13) { ResetNative(pointer,field.Table); }
        else if(field.Kind==15) { System.Runtime.InteropServices.NativeMemory.Clear((void*)pointer,(nuint)field.NativeElementSize); }
        else { NativePut((byte*)pointer,field.GetWide!=null?field.DefaultWide:field.DefaultRaw,field.NativeElementSize); }
    }
    public static bool BuilderKeyValid(TableFieldInfo key,ReadOnlySpan<byte> text,ulong raw,bool textual)
    {
        if(key.Kind==12) { return textual && text.Length<=key.ArrayBound && TextValid(text); }
        if(textual) { return false; }
        int bits=key.NativeElementSize*8;
        if(bits==64) { return true; }
        if(key.Kind>=2 && key.Kind<=5)
        { long value=unchecked((long)raw),bound=1L<<(bits-1); return value>=-bound && value<bound; }
        return raw<(1UL<<bits);
    }
    public static int BuilderKeyCompare(IntPtr pointer,TableFieldInfo key,ReadOnlySpan<byte> text,ulong raw)
    {
        NativeValue value=new NativeValue((byte*)pointer,0);
        if(key.Kind==12) { return value.Buffer(key).Slice(0,value.Count(key)).SequenceCompareTo(text); }
        ulong stored=value.Raw(key,0);
        return key.Kind>=2 && key.Kind<=5?unchecked((long)stored).CompareTo(unchecked((long)raw)):stored.CompareTo(raw);
    }
    public static void BuilderKeySet(IntPtr pointer,TableFieldInfo key,ReadOnlySpan<byte> text,ulong raw)
    {
        NativeValue value=new NativeValue((byte*)pointer,0);
        if(key.Kind==12) { value.Buffer(key).Clear(); text.CopyTo(value.Buffer(key)); value.SetCount(key,text.Length); }
        else { value.SetRaw(key,0,raw); }
    }
`
