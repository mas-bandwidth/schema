// THE BLOCK HOME gate (SPEC-TABLES.md §19.2's C# surface).
//
// Two defects the dogfood found, both from one root cause — a C# backend that
// emitted per DECLARING FILE and skipped a file with no `table` in it:
//
//   * the unit's shared block runtime went to `ir.ProtocolIdHome`, which in a
//     real unit is the constants file and declares no table, so the file was
//     never written and every reference to `Schema.TableBlockMagic`,
//     `Schema.BuildVersion`, `TableBlockTriple`, `TableBlockRows<T>`,
//     `TableBlockInfo` and the layout check was undefined;
//   * a blittable record declared in a file of `type`s alone went to that
//     file's Block.cs, which is never written either, so a consumer
//     referenced a struct nothing emitted.
//
// `tables/blockhome` is a unit with exactly that shape: `Constants.schema`
// sorts first and declares no table, `Data.schema` declares only `type`s and
// one of them is a block row's field, and `Frame.schema` declares the table.
// COMPILING this file is the gate — neither defect can survive a build — and
// the touches below make sure the surface is reachable and not merely present.

using System;
using System.Runtime.CompilerServices;
using Blockhome;
using HomeRow = Blockhome.Block;

static class BlockHomeGate
{
    internal static bool Run()
    {
        bool ok = true;

        // the shared RUNTIME, which the protocol id's home would have hidden.
        // Read through locals, because these are compile-time constants and a
        // comparison against one folds to an unreachable branch.
        ulong magic = Schema.TableBlockMagic;
        ulong version = Schema.BuildVersion;
        ulong order = Schema.TableBlockByteOrder;
        if (magic != 0x4b4c42414d484353UL)
        {
            Console.WriteLine("FAILED: the block home's magic");
            ok = false;
        }
        if (version == 0UL || order == 0UL)
        {
            Console.WriteLine("FAILED: the block home's runtime constants");
            ok = false;
        }

        // the BLITTABLE RECORD from the type-only file, and the layout the
        // block form asserts for it
        if (Unsafe.SizeOf<HomeRow.ArmorConfig>() == 0 || Unsafe.SizeOf<HomeRow.ArmorPlate>() == 0)
        {
            Console.WriteLine("FAILED: the block home's records from the type-only file");
            ok = false;
        }
        long stride = PartFrameBlock.PartsStride;
        if (Unsafe.SizeOf<HomeRow.PartRow>() != stride)
        {
            Console.WriteLine("FAILED: the row's size is not the pitch the block declares");
            ok = false;
        }

        // and the handle, its descriptors and its Open, all reachable
        if (PartFrameBlock.Type.Name != "PartFrame" || PartFrameBlock.Type.NumFields != 2)
        {
            Console.WriteLine("FAILED: the block home's descriptors");
            ok = false;
        }
        PartFrameBlock block;
        if (PartFrameBlock.Open(out block, IntPtr.Zero, 0))
        {
            Console.WriteLine("FAILED: Open accepted a null pointer");
            ok = false;
        }
        return ok;
    }
}
