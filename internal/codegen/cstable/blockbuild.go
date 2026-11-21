package cstable

import "github.com/mas-bandwidth/schema/v2/ir"

func (g *blockGen) emitBlockBuild(bl *ir.BlockLayout) {
	n := bl.Table.Name
	g.hf("    // Begin settles the layout before workers fill disjoint rows. It touches\n    // the prologue and triples only; use Clear on storage when byte stability matters.\n")
	g.hf("    public static bool Begin(out %sBlock block, global::System.IntPtr pointer, long capacity, in %sCounts counts, out TableBlockRefusal refusal)\n    {\n        block = default; refusal = default;\n", n, n)
	for _, a := range bl.Arrays {
		name := member(a.Field)
		g.hf("        if (counts.%s < 0 || counts.%s > %d) { refusal = new TableBlockRefusal { Array = %q, Count = counts.%s, Maximum = %d }; return false; }\n", name, name, a.Max, a.Field.Name, name, a.Max)
	}
	g.hf("        if (pointer == global::System.IntPtr.Zero || ((ulong)pointer & 63) != 0) { return false; }\n        long at = %d;\n", bl.Projection.Size)
	for _, a := range bl.Arrays {
		name := member(a.Field)
		g.hf("        at = (at + 63) & ~63L; long %sAt = at; at += (long)counts.%s * %d;\n", name, name, a.Stride)
	}
	g.hf("        at = (at + 63) & ~63L; if (at > capacity) { return false; }\n        %sBlockProjection* p = (%sBlockProjection*)pointer;\n", n, n)
	g.hf("        p->Magic = Schema.TableBlockMagic; p->BuildVersion = Schema.BuildVersion; p->ByteOrder = Schema.TableBlockByteOrder;\n")
	for _, a := range bl.Arrays {
		name := member(a.Field)
		g.hf("        p->%s = new TableBlockTriple { OffsetOf = (ulong)%sAt, Count = (uint)counts.%s, Stride = %d };\n", name, name, name, a.Stride)
	}
	g.hf("        block = new %sBlock((byte*)pointer, at); return true;\n    }\n\n", n)
	g.hf("    public static bool Begin(out %sBlock block, %sBlockStorage storage, in %sCounts counts, out TableBlockRefusal refusal) { return Begin(out block, storage.Pointer, storage.Capacity, counts, out refusal); }\n", n, n, n)
	g.hf("    public ref %sBlockProjection WritableProjection { get { return ref *(%sBlockProjection*)basePointer; } }\n", n, n)
	for _, a := range bl.Arrays {
		name := member(a.Field)
		g.hf("    public Span<%sRow> %sWritableSpan { get { var p = (%sBlockProjection*)basePointer; return new Span<%sRow>(basePointer + p->%s.OffsetOf, (int)p->%s.Count); } }\n", a.ElemName, name, n, a.ElemName, name, name)
	}
}

func (g *blockGen) emitBlockStorage(bl *ir.BlockLayout) {
	n := bl.Table.Name
	g.hf("public struct %sCounts\n{\n", n)
	for _, a := range bl.Arrays {
		g.hf("    public int %s;\n", member(a.Field))
	}
	g.hf("}\n\npublic sealed class %sBlockStorage : TableBlockStorage\n{\n    public %sBlockStorage(TableBlockAllocator allocator = null) : base(%sBlock.BlockMaxBytes, allocator) { }\n}\n\n", n, n, n)
}

const tableBlockBuildRuntime = `
public struct TableBlockRefusal
{
    public string Array;
    public long Count, Maximum;
}
public sealed class TableBlockAllocator
{
    public Func<long, global::System.IntPtr> Allocate;
    public Action<global::System.IntPtr> Free;
}
// One allocation at construction. Begin and worker access allocate nothing.
// Borrowed blocks and spans must not outlive this storage or its next Begin.
public unsafe class TableBlockStorage : global::System.IDisposable
{
    global::System.IntPtr allocation;
    readonly TableBlockAllocator allocator;
    public global::System.IntPtr Pointer { get; private set; }
    public long Capacity { get; private set; }
    protected TableBlockStorage(long capacity, TableBlockAllocator allocator)
    {
        if (capacity <= 0 || capacity > long.MaxValue - 63) { throw new global::System.ArgumentOutOfRangeException(nameof(capacity)); }
        if (allocator != null && (allocator.Allocate == null || allocator.Free == null)) { throw new global::System.ArgumentException("allocator requires both callbacks"); }
        this.allocator = allocator;
        allocation = allocator == null ? global::System.Runtime.InteropServices.Marshal.AllocHGlobal(checked((nint)(capacity + 63))) : allocator.Allocate(capacity + 63);
        if (allocation == global::System.IntPtr.Zero) { throw new global::System.OutOfMemoryException(); }
        Pointer = (global::System.IntPtr)(((ulong)allocation + 63) & ~63ul); Capacity = capacity;
    }
    public void Clear()
    {
        if (Pointer == global::System.IntPtr.Zero) { throw new global::System.ObjectDisposedException(nameof(TableBlockStorage)); }
        global::System.Runtime.InteropServices.NativeMemory.Clear((void*)Pointer, checked((nuint)Capacity));
    }
    public void Dispose()
    {
        if (allocation == global::System.IntPtr.Zero) { return; }
        if (allocator == null) { global::System.Runtime.InteropServices.Marshal.FreeHGlobal(allocation); } else { allocator.Free(allocation); }
        allocation = global::System.IntPtr.Zero; Pointer = global::System.IntPtr.Zero; Capacity = 0;
    }
}
`
