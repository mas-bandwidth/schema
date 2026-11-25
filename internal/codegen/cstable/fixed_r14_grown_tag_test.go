package cstable

import (
	"strings"
	"testing"
)

// TestCSharpFixedR14GrownUnionTag is the cs leg's R14: when a union's tag
// widens between the writer's lock layout and this reader (1 byte to 8), the
// None const reads the writer's raw tag at the WRITER's tag width, so the pair
// compiles WHOLE and a 5-byte writer record is never read past
// (docs/FIXED-FORM-ALGORITHM.md §5.2 EMIT kind 15, ruled 2026-11-25).
//
// The C# plan compiler is in the emitted runtime (fixedruntime.go,
// TableFixedWire.Compile, Compiler.Push and SrcEnd), so the probe below drives
// that compiler on the R14 vector inside a generated unit, RUNS the plan it
// lays through TableFixedWire.Run, and walks the lineage lane
// TableFixedWire.LineagePlans lays at load. The vector is a forged lock layout
// (no real declaration has an 8-byte union tag), so the case cannot ride the
// shared corpus; it is this leg's own test, and its registry row names it.
func TestCSharpFixedR14GrownUnionTag(t *testing.T) {
	t.Parallel()
	files, err := Generate(unitFrom(t, "package r14probe\n\nfixed table C {\n\tlead uint32 = 1\n}\n"))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	out, err := runCSharpProbe(t, files, fixedR14Probe, "r14probe")
	if err != nil || !strings.Contains(out, "R14: a grown union tag reads the writer's tag width and compiles whole") {
		t.Fatalf("R14: the grown union tag probe failed (%v):\n%s", err, out)
	}
}

// fixedR14Probe is the probe: every assertion prints ok or FAIL by name.
const fixedR14Probe = `using System;
using System.Buffers.Binary;

using R14probe;
using Wire = R14probe.Schema.TableFixedWire;

// cs/R14 - EVERY COMPILED ENTRY'S REACH IS IN THE WRITER'S COORDINATES:
// a union tag grown 1 -> 8 reads the writer's tag at the writer's width and
// compiles whole.
//
// THE LAW (docs/FIXED-FORM-ALGORITHM.md §5.2 EMIT kind 15): the union's None
// const LANDS my_tag bytes into the reader's tag, and a leg that reads the
// writer's raw tag beside it (to hold the writer's arm set, §4.6) reads it at
// THE WRITER'S TAG WIDTH, their_tag bytes, never my_tag: its source reach is
// src + their_tag, inside the writer's record. A reach at the reader's width
// would read the arm's payload into the tag and refuse a lawful file
// layout_record_too_large. A const that reads no record byte (an arm's tag
// const, a present byte) has no source reach.
//
// THE C# COMPILER (internal/codegen/cstable/fixedruntime.go): the None const
// is pushed with Size = my_tag (the width it lands), DstSize = the writer's
// arm count and Meta = the writer's tag width; TableFixedWire.SrcEnd takes a
// const's reach as Src + Meta for the None const and 0 for every other const,
// and Compiler.Push holds that reach to the writer's declared record. The run
// reads the writer's tag through TagAt(src, Src, Meta), the writer's width.
//
// THE VECTOR: a layout block is a u32LE entry count, then 17-byte entries:
// id u64LE, kind u8, size u32LE, children u32LE. Kind 13 a table, 15 a union
// (size = tag + widest arm), 8 a u32.
//   THEIRS (the writer): [1,13,5,1], [7,15,5,1], [9,8,4,0] — a record of 5,
//   one union whose tag is 1 byte over one u32 arm.
//   MINE(tag) (the reader): [1,13,tag+4,1], [7,15,tag+4,1], [9,8,4,0].
// The reader's storage here is two slots: the union's tag (a ulong, so an
// 8-byte tag lands whole) and the u32 arm.
//
// exit: 0 = every assertion passed, 1 = one or more failed

sealed class Image
{
    public ulong Tag = 0xDEADBEEFDEADBEEFUL; // poisoned: every landing is seen
    public uint U = 0xDEADBEEFu;
}

static class Program
{
    static int failed;

    static void Check(bool ok, string what)
    {
        Console.WriteLine((ok ? "ok   " : "FAIL ") + what);
        if (!ok) { failed++; }
    }

    static void Entry(byte[] b, int i, ulong id, byte kind, uint size, uint children)
    {
        int at = 4 + i * 17;
        BinaryPrimitives.WriteUInt64LittleEndian(b.AsSpan(at), id);
        b[at + 8] = kind;
        BinaryPrimitives.WriteUInt32LittleEndian(b.AsSpan(at + 9), size);
        BinaryPrimitives.WriteUInt32LittleEndian(b.AsSpan(at + 13), children);
    }

    static byte[] Block(uint tag)
    {
        byte[] b = new byte[4 + 3 * 17];
        BinaryPrimitives.WriteUInt32LittleEndian(b, 3);
        Entry(b, 0, 1, 13, tag + 4, 1);
        Entry(b, 1, 7, 15, tag + 4, 1);
        Entry(b, 2, 9, 8, 4, 0);
        return b;
    }

    static readonly byte[] Theirs = Block(1);

    // THE READER'S DESTINATIONS, one per MINE entry: the union's tag is slot 0
    // (its Aux lane), the arm is slot 1 (the union's Dst 0 plus the arm's 1).
    static readonly TableFixedDst[] Dst =
    {
        new TableFixedDst(0, 0, 0, 0, 0),
        new TableFixedDst(0, 0, 0, 0, 0),
        new TableFixedDst(1, 0, 0, 0, 0),
    };

    static readonly TableFixedSlot<Image>[] Slots =
    {
        new TableFixedSlot<Image>(setRaw: (img, v) => img.Tag = v),
        new TableFixedSlot<Image>(setRaw: (img, v) => img.U = (uint)v),
    };

    static int Compile(uint tag, out TableFixedEntry[] plan)
    {
        plan = new TableFixedEntry[64];
        if (!Wire.ParseLayout(Theirs, out TableFixedLayoutView theirs))
        {
            Check(false, "the writer's layout parses");
            return int.MinValue;
        }
        return Wire.Compile(theirs, Block(tag), Dst, plan, new TableReport());
    }

    static string Run(TableFixedEntry[] plan, int count, byte[] record, Image img)
    {
        byte[] scratch = null;
        int forged = 0;
        // A read past the writer's record throws in C#: the row names it as
        // that read's answer rather than dying on it.
        try
        {
            return Wire.Run<Image>(plan.AsSpan(0, count), Slots, record, img, new TableReport(),
                ReadOnlySpan<byte>.Empty, ref scratch, ref forged);
        }
        catch (ArgumentOutOfRangeException e)
        {
            return "read past the writer's record: " + e.GetType().Name;
        }
    }

    static int Main()
    {
        // ---- 0. every vector is a lawful layout -----------------------------
        Check(Wire.ParseLayout(Theirs, out _), "THEIRS parses");
        foreach (uint tag in new uint[] { 1, 2, 8 })
        {
            Check(Wire.ParseLayout(Block(tag), out _), $"MINE({tag}) parses");
        }

        // ---- 1. THE CELL: a tag grown 1 -> 8 compiles WHOLE ------------------
        int made = Compile(8, out TableFixedEntry[] plan);
        Check(made == 3,
            $"the grown tag compiles whole: None const, the arm's tag const, the u32's copy (got {made}; -3 is layout_record_too_large)");
        if (made > 0)
        {
            int none = -1;
            for (int i = 0; i < made; ++i)
            {
                if (plan[i].Op == Wire.Const && plan[i].Aux == 0 && plan[i].DstSize != 0) { none = i; }
            }
            Check(none >= 0, "the union's None const is in the plan");
            if (none >= 0)
            {
                TableFixedEntry e = plan[none];
                Check(e.Src == 0, $"the None const reads the writer's tag at its offset 0, got {e.Src}");
                Check(e.Size == 8, $"the None const LANDS my 8-byte tag, got {e.Size}");
                Check(e.Meta == 1, $"the None const reads the WRITER's 1-byte tag, never 8: got {e.Meta}");
                Check(e.DstSize == 1, $"it carries the writer's arm count, 1: got {e.DstSize}");
                Check(e.Guard == Wire.NoGuard, "the None const is unguarded");
                uint reach = Wire.SrcEnd(e);
                Check(reach == 1 && reach <= 5, $"its reach, 0 + 1, is inside the writer's record of 5: got {reach}");
            }
            bool armCopy = false;
            for (int i = 0; i < made; ++i)
            {
                if (plan[i].Op == Wire.Copy && plan[i].Src == 1 && plan[i].Size == 4 && plan[i].Dst == 1) { armCopy = true; }
            }
            Check(armCopy, "the u32 arm is a copy of 4 bytes from src 1, past the WRITER's 1-byte tag");

            // ---- 2. THE READ IT LAYS ------------------------------------------
            Image a = new Image();
            string why = Run(plan, made, new byte[] { 1, 0x44, 0x33, 0x22, 0x11 }, a);
            Check(why == null, $"arm 1 reads with no refusal, got {why ?? "none"}");
            Check(a.Tag == 1UL, $"my 8-byte tag lands exactly 1, high bytes zero: got 0x{a.Tag:x}");
            Check(a.U == 0x11223344u, $"the u32 lands whole past my tag: got 0x{a.U:x}");

            Image n = new Image();
            why = Run(plan, made, new byte[] { 0, 0, 0, 0, 0 }, n);
            Check(why == null && n.Tag == 0UL, $"tag 0 reads as None in my 8-byte tag: why={why ?? "none"} tag=0x{n.Tag:x}");

            Image f = new Image();
            why = Run(plan, made, new byte[] { 2, 0, 0, 0, 0 }, f);
            Check(why == "out_of_range", $"a forged writer tag 2 refuses out_of_range (§4.6), got {why ?? "none"}");
        }

        // ---- 3. THE TWINS: the tag grown to 2 and kept at 1 compile whole -----
        foreach (uint tag in new uint[] { 1, 2 })
        {
            int m = Compile(tag, out _);
            Check(m == 3, $"THEIRS vs MINE({tag}) compiles whole: got {m}");
        }

        // ---- 4. THE LINEAGE LANE the generated unit lays at load -------------
        foreach (uint tag in new uint[] { 8, 2 })
        {
            TableFixedKnownLayout[] known = { new TableFixedKnownLayout(0x3333333344444444UL, Theirs, 8 + 5) };
            TableFixedLineagePlan[] lanes = Wire.LineagePlans(known, Block(tag), Dst, 0x1111111122222222UL);
            TableFixedLineagePlan lane = lanes[0];
            Check(lane.Why == null && lane.Count == 3,
                $"the grown tag (1 -> {tag}) lays a ready lineage lane: why={lane.Why ?? "none"} count={lane.Count}");
        }

        if (failed != 0)
        {
            Console.WriteLine($"R14 FAIL: {failed} assertion(s) failed");
            return 1;
        }
        Console.WriteLine("R14: a grown union tag reads the writer's tag width and compiles whole");
        return 0;
    }
}
`
