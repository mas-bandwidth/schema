package cstable

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/mas-bandwidth/schema/v3/ir"
)

func (g *blockGen) emitBlockBuild(bl *ir.BlockLayout) {
	n := bl.Table.Name
	g.hf("    // Begin settles the layout before workers fill disjoint rows. It touches\n    // the prologue and triples only; use Clear on storage when byte stability matters.\n")
	g.hf("    public static bool Begin(out %sBlock block, IntPtr pointer, long capacity, in %sCounts counts, out TableBlockRefusal refusal)\n    {\n        block = default; refusal = default;\n", n, n)
	for _, a := range bl.Arrays {
		name := member(a.Field)
		g.hf("        if (counts.%s < 0 || counts.%s > %d) { refusal = new TableBlockRefusal { Array = %q, Count = counts.%s, Maximum = %d }; return false; }\n", name, name, a.Max, a.Field.Name, name, a.Max)
	}
	g.hf("        if (pointer == IntPtr.Zero || ((ulong)pointer & 63) != 0) { return false; }\n        long at = %d;\n", bl.Projection.Size)
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

// generateBlockFill emits <Package>BlockFill.cs, the Begin overloads that take
// a table's managed VALUE and check it before they settle a block's layout.
// They are the one block symbol that names a table wire storage class, and
// that class lives in <Base>Table.cs, which the accelerator pair never
// compiles: <Base>Block.cs and <Base>Cook.cs build together and with no
// table source at all (docs/SPEC-TABLES.md §19.2, §7), and <Base>Table.cs
// carries not one block symbol. So they sit in a file of their own, one per
// unit, beside both, completing the handle as a second part of the partial
// <Table>Block struct. A consumer that blocks only from counts never compiles
// it; a consumer that blocks a managed value compiles it with both.
func generateBlockFill(u *ir.Unit, blocks *ir.BlockUnit) (string, []byte) {
	if !anyBlockForm(u, blocks) {
		return "", nil
	}
	g := &blockGen{unit: u, blocks: blocks}
	for _, f := range u.Files {
		for _, st := range f.Tables {
			if !st.CarriesForm("block") {
				continue
			}
			bl := blocks.Block(st.Name)
			if bl == nil {
				continue
			}
			g.hf("// %s's block, begun from a managed %s: every bound the value must keep is\n", st.Name, st.Name)
			g.hf("// checked first, and a value that breaks one begins no block.\n")
			g.hf("public unsafe readonly partial struct %sBlock\n{\n", st.Name)
			g.emitBlockBuildValue(bl)
			g.hf("}\n\n")
		}
	}
	var h strings.Builder
	h.WriteString(generatedFrom(nil, u))
	h.WriteString("// SPDX-License-Identifier: NONE — this generated output is yours, under terms of\n")
	h.WriteString("// your choice. See the LICENSE exception in the schema compiler; the compiler is\n")
	h.WriteString("// AGPL-3.0, its output is not.\n")
	fmt.Fprintf(&h, "// package %s — the BLOCK FORM begun from a managed value (docs/SPEC-TABLES.md §19).\n", u.Package)
	h.WriteString("//\n")
	h.WriteString("// The BRIDGE between the two halves: these Begin overloads take the table\n")
	h.WriteString("// wire's storage class from <Base>Table.cs and complete the block handle in\n")
	h.WriteString("// <Base>Block.cs. Compile this file only beside both; the block and cook\n")
	h.WriteString("// pair compiles without it, and without any table source.\n\n")
	h.WriteString("using System;\n\n")
	fmt.Fprintf(&h, "namespace %s\n{\n\n", capitalize(u.Package))
	h.WriteString(indent4(g.handles.String()))
	h.WriteString("\n}\n")
	return capitalize(u.Package) + "BlockFill.cs", []byte(h.String())
}

func (g *blockGen) emitBlockBuildValue(bl *ir.BlockLayout) {
	n := bl.Table.Name
	g.hf("    public static bool Begin(out %sBlock block, IntPtr pointer, long capacity, %s value, out TableBlockRefusal refusal)\n    {\n", n, n)
	g.hf("        block = default; refusal = default;\n")
	g.hf("        if (value == null) { return false; }\n")
	var checks strings.Builder
	emitCsBlockValidateStruct(&checks, bl.Table, "value", bl, 8, 0)
	g.hf("%s", checks.String())
	g.hf("        var counts = new %sCounts();\n", n)
	for _, a := range bl.Arrays {
		name := member(a.Field)
		g.hf("        counts.%s = value.%sCount;\n", name, name)
	}
	g.hf("        return Begin(out block, pointer, capacity, in counts, out refusal);\n    }\n\n")
	g.hf("    public static bool Begin(out %sBlock block, %sBlockStorage storage, %s value, out TableBlockRefusal refusal) { return Begin(out block, storage.Pointer, storage.Capacity, value, out refusal); }\n", n, n, n)
}

func emitCsBlockValidateStruct(b *strings.Builder, st *ir.Struct, val string, rootBl *ir.BlockLayout, indent, depth int) {
	for _, f := range st.Fields {
		emitCsBlockValidateField(b, st, f, val, rootBl, indent, depth)
	}
}

func emitCsBlockValidateField(b *strings.Builder, owner *ir.Struct, f *ir.Field, val string, rootBl *ir.BlockLayout, indent, depth int) {
	ind := strings.Repeat(" ", indent)
	prop := member(f)
	if f.Type.Optional {
		fmt.Fprintf(b, "%sif (%s.%sPresent)\n%s{\n", ind, val, prop, ind)
		emitCsBlockValidatePayload(b, owner, f, val, rootBl, indent+4, depth)
		fmt.Fprintf(b, "%s}\n", ind)
		return
	}
	emitCsBlockValidatePayload(b, owner, f, val, rootBl, indent, depth)
}

func emitCsBlockValidatePayload(b *strings.Builder, owner *ir.Struct, f *ir.Field, val string, rootBl *ir.BlockLayout, indent, depth int) {
	ind := strings.Repeat(" ", indent)
	prop := member(f)
	expr := val + "." + prop
	switch {
	case f.Type.Kind == ir.TString:
		fmt.Fprintf(b, "%sif (%s == null || %s.%sLength < 0 || %s.%sLength > %d || %s.%sLength > %s.Length || !TableBlockStorage.TextValid(%s, %s.%sLength)) { return false; }\n",
			ind, expr, val, prop, val, prop, f.Type.Size, val, prop, expr, expr, val, prop)
	case f.Type.Kind == ir.TBytes:
		fmt.Fprintf(b, "%sif (%s == null || %s.%sLength < 0 || %s.%sLength > %d || %s.%sLength > %s.Length) { return false; }\n",
			ind, expr, val, prop, val, prop, f.Type.Size, val, prop, expr)
	case f.KeyEnum != "":
		slots := expr
		if owner != nil && owner.IsTable {
			slots = expr + ".Slots"
		}
		iv := fmt.Sprintf("i%d", depth)
		fmt.Fprintf(b, "%sif (%s == null || %s.Length != %d) { return false; }\n", ind, slots, slots, f.KeyEnumRef.Max)
		fmt.Fprintf(b, "%sfor (int %s = 0; %s < %d; %s++)\n%s{\n", ind, iv, iv, f.KeyEnumRef.Max, iv, ind)
		emitCsBlockValidateElem(b, f, fmt.Sprintf("%s[%s]", slots, iv), indent+4, depth+1)
		fmt.Fprintf(b, "%s}\n", ind)
	case f.Array == ir.ArrayFixed:
		iv := fmt.Sprintf("i%d", depth)
		fmt.Fprintf(b, "%sif (%s == null || %s.Length != %d) { return false; }\n", ind, expr, expr, f.ArrayBound)
		fmt.Fprintf(b, "%sfor (int %s = 0; %s < %d; %s++)\n%s{\n", ind, iv, iv, f.ArrayBound, iv, ind)
		emitCsBlockValidateElem(b, f, fmt.Sprintf("%s[%s]", expr, iv), indent+4, depth+1)
		fmt.Fprintf(b, "%s}\n", ind)
	case f.Array == ir.ArrayCounted:
		iv := fmt.Sprintf("i%d", depth)
		if rootBl != nil && rootBl.ArrayByName(f.Name) != nil {
			fmt.Fprintf(b, "%sif (%s.%sCount < %d || %s.%sCount > %d) { refusal = new TableBlockRefusal { Array = %q, Count = %s.%sCount, Maximum = %d }; return false; }\n",
				ind, val, prop, f.ArrayMin, val, prop, f.ArrayBound, f.Name, val, prop, f.ArrayBound)
		} else {
			fmt.Fprintf(b, "%sif (%s.%sCount < %d || %s.%sCount > %d) { return false; }\n", ind, val, prop, f.ArrayMin, val, prop, f.ArrayBound)
		}
		fmt.Fprintf(b, "%sif (%s == null || %s.%sCount > %s.Length) { return false; }\n", ind, expr, val, prop, expr)
		fmt.Fprintf(b, "%sfor (int %s = 0; %s < %s.%sCount; %s++)\n%s{\n", ind, iv, iv, val, prop, iv, ind)
		emitCsBlockValidateElem(b, f, fmt.Sprintf("%s[%s]", expr, iv), indent+4, depth+1)
		fmt.Fprintf(b, "%s}\n", ind)
	default:
		emitCsBlockValidateElem(b, f, expr, indent, depth)
	}
}

func emitCsBlockValidateElem(b *strings.Builder, f *ir.Field, expr string, indent, depth int) {
	ind := strings.Repeat(" ", indent)
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			fmt.Fprintf(b, "%sif (%s == null) { return false; }\n", ind, expr)
			emitCsBlockValidateStruct(b, r, expr, nil, indent, depth+1)
		case *ir.Enum:
			if !csOrdinalFillsStorage(r.Max, r.StorageBits) {
				fmt.Fprintf(b, "%sif ((ulong)%s > %dUL) { return false; }\n", ind, expr, r.Max)
			}
		case *ir.Flags:
			if len(r.Variants) < 64 {
				mask := (uint64(1) << uint(len(r.Variants))) - 1
				fmt.Fprintf(b, "%sif (((ulong)%s & ~0x%xUL) != 0) { return false; }\n", ind, expr, mask)
			}
		}
		return
	}
	switch f.Type.Kind {
	case ir.TFloat32:
		fmt.Fprintf(b, "%sif (!float.IsFinite(%s)) { return false; }\n", ind, expr)
		if f.HasFloatRange {
			fmt.Fprintf(b, "%sif (%s < %s || %s > %s) { return false; }\n", ind, expr, formatFloat32(f.FMin), expr, formatFloat32(f.FMax))
		}
	case ir.TFloat64:
		fmt.Fprintf(b, "%sif (!double.IsFinite(%s)) { return false; }\n", ind, expr)
		if f.HasFloatRange {
			fmt.Fprintf(b, "%sif (%s < %s || %s > %s) { return false; }\n", ind, expr, formatFloat64(f.FMin), expr, formatFloat64(f.FMax))
		}
	default:
		width := int(fixedStorageBytes(f.Type))
		signed := ir.TableKindSigned(ir.TableScalarKind(f))
		if rlo, rhi, ok := ir.TableRawRange(f); ok {
			low, high := tableClampEnds(f, width)
			lit := func(v *big.Int) string {
				if width == 16 {
					return wideLiteral(v, signed)
				}
				return csIntLit(v, signed, width)
			}
			cmp := csWideCompare(f, width, expr)
			switch {
			case low && high:
				fmt.Fprintf(b, "%sif (%s < %s || %s > %s) { return false; }\n", ind, cmp, lit(rlo), cmp, lit(rhi))
			case low:
				fmt.Fprintf(b, "%sif (%s < %s) { return false; }\n", ind, cmp, lit(rlo))
			case high:
				fmt.Fprintf(b, "%sif (%s > %s) { return false; }\n", ind, cmp, lit(rhi))
			}
		}
		if f.Type.Kind == ir.TBits && int64(f.Type.Width) < 8*int64(width) {
			maxv := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(f.Type.Width)), big.NewInt(1))
			fmt.Fprintf(b, "%sif (%s > %s) { return false; }\n", ind, expr, csIntLit(maxv, false, width))
		}
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
    public Func<long, IntPtr> Allocate;
    public Action<IntPtr> Free;
}
// One allocation at construction. Begin and worker access allocate nothing.
// Borrowed blocks and spans must not outlive this storage or its next Begin.
public unsafe class TableBlockStorage : IDisposable
{
    IntPtr allocation;
    readonly TableBlockAllocator allocator;
    public IntPtr Pointer { get; private set; }
    public long Capacity { get; private set; }
    protected TableBlockStorage(long capacity, TableBlockAllocator allocator)
    {
        if (capacity <= 0 || capacity > long.MaxValue - 63) { throw new ArgumentOutOfRangeException(nameof(capacity)); }
        if (allocator != null && (allocator.Allocate == null || allocator.Free == null)) { throw new ArgumentException("allocator requires both callbacks"); }
        this.allocator = allocator;
        allocation = allocator == null ? Marshal.AllocHGlobal(checked((nint)(capacity + 63))) : allocator.Allocate(capacity + 63);
        if (allocation == IntPtr.Zero) { throw new OutOfMemoryException(); }
        Pointer = (IntPtr)(((ulong)allocation + 63) & ~63ul); Capacity = capacity;
    }
    public void Clear()
    {
        if (Pointer == IntPtr.Zero) { throw new ObjectDisposedException(nameof(TableBlockStorage)); }
        NativeMemory.Clear((void*)Pointer, checked((nuint)Capacity));
    }
    public void Dispose()
    {
        if (allocation == IntPtr.Zero) { return; }
        if (allocator == null) { Marshal.FreeHGlobal(allocation); } else { allocator.Free(allocation); }
        allocation = IntPtr.Zero; Pointer = IntPtr.Zero; Capacity = 0;
    }
    internal static bool TextValid(byte[] bytes, int len)
    {
        if (bytes == null || len < 0 || len > bytes.Length) { return false; }
        int i = 0;
        while (i < len)
        {
            byte c = bytes[i];
            if (c == 0) { return false; }
            if (c < 0x80) { i++; continue; }
            if (c >= 0xC2 && c <= 0xDF)
            {
                if (i + 1 >= len || (bytes[i + 1] & 0xC0) != 0x80) { return false; }
                i += 2; continue;
            }
            if (c >= 0xE0 && c <= 0xEF)
            {
                if (i + 2 >= len || (bytes[i + 1] & 0xC0) != 0x80 || (bytes[i + 2] & 0xC0) != 0x80) { return false; }
                if (c == 0xE0 && bytes[i + 1] < 0xA0) { return false; }
                if (c == 0xED && bytes[i + 1] >= 0xA0) { return false; }
                i += 3; continue;
            }
            if (c >= 0xF0 && c <= 0xF4)
            {
                if (i + 3 >= len || (bytes[i + 1] & 0xC0) != 0x80 || (bytes[i + 2] & 0xC0) != 0x80 || (bytes[i + 3] & 0xC0) != 0x80) { return false; }
                if (c == 0xF0 && bytes[i + 1] < 0x90) { return false; }
                if (c == 0xF4 && bytes[i + 1] >= 0x90) { return false; }
                i += 4; continue;
            }
            return false;
        }
        return true;
    }
}
`
