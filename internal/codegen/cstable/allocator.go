package cstable

const tableAllocatorSource = `
// Caller context and a zeroed allocation/free pair (§6.5). The default value
// uses NativeMemory. Custom callbacks must return ZEROED, at least 16-aligned storage,
// and must return null on failure. Native builder and region scratch use this pair.
public unsafe struct TableAllocator
{
    public delegate*<global::System.IntPtr, long, global::System.IntPtr> Allocate;
    public delegate*<global::System.IntPtr, global::System.IntPtr, void> Free;
    public global::System.IntPtr Context;
    internal void* Get(long bytes)
    {
        if (bytes <= 0 || (ulong)bytes > (ulong)nuint.MaxValue || (Allocate == null) != (Free == null)) { return null; }
        if (Allocate != null) { return (void*)Allocate(Context, bytes); }
        if (bytes > long.MaxValue - 15) { return null; }
        try
        {
            nuint size = (nuint)((bytes + 15) & ~15L);
            void* p = global::System.Runtime.InteropServices.NativeMemory.AlignedAlloc(size, 16);
            if (p != null) { global::System.Runtime.InteropServices.NativeMemory.Clear(p, size); }
            return p;
        }
        catch (global::System.OutOfMemoryException) { return null; }
    }
    internal void Release(void* pointer)
    {
        if (pointer == null) { return; }
        if (Free != null) { Free(Context, (global::System.IntPtr)pointer); }
        else { global::System.Runtime.InteropServices.NativeMemory.AlignedFree(pointer); }
    }
}
`
