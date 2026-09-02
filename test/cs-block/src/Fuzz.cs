// The BLOCK FORM's FORGERY FUZZER, C# side (SPEC-TABLES.md §19.2, §19.5) — the
// twin of test/tables/block_fuzz_main.cpp, over the same seed blocks.
//
// The C++ leg writes a valid block per count vector into build/block-fuzz/ with
// its generated builder, because C# has only the READ half of the form: a
// consumer never lays a block out, so the seeds must come from a producer. This
// leg mutates those bytes with the same mutators and holds Open to the same
// oracle:
//
//   REFUSE, or OPEN and be WHOLE. A mutant either makes Open return false and
//   point at nothing, or it opens — and then every row of every array is
//   addressable inside the extent the caller passed, every pitch is this
//   build's own, every count is inside its declared maximum, and the walk reads
//   every byte of every row.
//
// The oracle re-derives its bounds from the DESCRIPTORS and from the triples in
// the instance, never from Open's own arithmetic. Every row range is checked
// BEFORE it is read, so a defect is reported rather than performed: this side
// has no address sanitizer, and a walk that checked nothing would prove nothing.
//
// SEED, N and ONLY come from the environment, so a failing case re-runs alone.

using System;
using System.Collections.Generic;
using System.IO;
using System.Reflection;
using System.Runtime.InteropServices;
using Blockdemo;
using Blockhome;

static class Fuzz
{
    // ---- the descriptors, read out of whichever unit's namespace declared them ----
    //
    // Every unit emits its own TableBlockInfo behind its own namespace, so
    // blockdemo's and blockhome's are structurally identical and distinct types.
    // One reflective converter reads either into the shape below, which is what
    // lets one oracle walk every unit.

    sealed class FuzzField
    {
        public string Name = "";
        public int Offset;
        public int Size;
        public bool OutOfLine;
        public int OffsetOfOffset;
        public int CountOffset;
        public int StrideOffset;
        public int Stride;
        public FuzzInfo? Element;
    }

    sealed class FuzzInfo
    {
        public string Name = "";
        public int Size;
        public int Align;
        public FuzzField[] Fields = Array.Empty<FuzzField>();
    }

    static object? Member(object owner, string name)
    {
        Type type = owner.GetType();
        FieldInfo? field = type.GetField(name);
        if (field != null)
        {
            return field.GetValue(owner);
        }
        PropertyInfo? property = type.GetProperty(name);
        if (property == null)
        {
            throw new Exception("the block descriptors carry no member named " + name);
        }
        return property.GetValue(owner);
    }

    static FuzzInfo Convert(object info, Dictionary<object, FuzzInfo> seen)
    {
        if (seen.TryGetValue(info, out FuzzInfo? already))
        {
            return already;
        }
        FuzzInfo converted = new FuzzInfo
        {
            Name = (string) Member(info, "Name")!,
            Size = (int) Member(info, "Size")!,
            Align = (int) Member(info, "Align")!,
        };
        seen[info] = converted;
        System.Collections.IEnumerable fields = (System.Collections.IEnumerable) Member(info, "Fields")!;
        List<FuzzField> list = new List<FuzzField>();
        foreach (object field in fields)
        {
            object? element = Member(field, "Element");
            list.Add(new FuzzField
            {
                Name = (string) Member(field, "Name")!,
                Offset = (int) Member(field, "Offset")!,
                Size = (int) Member(field, "Size")!,
                OutOfLine = (bool) Member(field, "OutOfLine")!,
                OffsetOfOffset = (int) Member(field, "OffsetOfOffset")!,
                CountOffset = (int) Member(field, "CountOffset")!,
                StrideOffset = (int) Member(field, "StrideOffset")!,
                Stride = (int) Member(field, "Stride")!,
                Element = element == null ? null : Convert(element, seen),
            });
        }
        converted.Fields = list.ToArray();
        return converted;
    }

    // ---- the verdict ----

    struct OpenResult
    {
        public bool Opened;
        public long Reported;
        public IntPtr Base;
    }

    sealed class Unit
    {
        public string Name = "";
        public int NumVectors;
        public int ProjectionSize;
        public long[] Maxima = Array.Empty<long>();
        public FuzzInfo Info = new FuzzInfo();
        public Func<IntPtr, long, OpenResult> Open = (p, b) => default;
        public Action<IntPtr, long> TypedWalk = (p, b) => { };
        public Slot[] Slots = Array.Empty<Slot>();
    }

    struct Slot
    {
        public string Name;
        public int Offset;
        public long Maximum; // the semantic maximum for this slot, or -1
    }

    static string siteUnit = "startup";
    static int siteVector;
    static string sitePass = "startup";
    static long siteIndex;
    static string siteDescription = "";
    static ulong runSeed;

    static void Defect(string what)
    {
        Console.WriteLine();
        Console.WriteLine("FAILED: " + what);
        Console.WriteLine("  unit      " + siteUnit);
        Console.WriteLine("  vector    " + siteVector);
        Console.WriteLine("  pass      " + sitePass);
        Console.WriteLine("  index     " + siteIndex);
        Console.WriteLine("  mutation  " + siteDescription);
        Console.WriteLine("  re-run    SEED=" + runSeed + " ONLY=" + siteUnit + ":" + siteVector + ":"
                          + sitePass + ":" + siteIndex + " dotnet run");
        Console.WriteLine();
        Environment.Exit(1);
    }

    // ---- the seeded generator: splitmix64, the C++ leg's own ----

    struct Rng
    {
        public ulong State;

        public ulong Next()
        {
            State += 0x9e3779b97f4a7c15UL;
            ulong z = State;
            z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9UL;
            z = (z ^ (z >> 27)) * 0x94d049bb133111ebUL;
            return z ^ (z >> 31);
        }

        public ulong Below(ulong n) { return n == 0 ? 0 : Next() % n; }
    }

    static ulong Mix(ulong a, ulong b)
    {
        ulong z = a + 0x9e3779b97f4a7c15UL * (b + 1);
        z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9UL;
        z = (z ^ (z >> 27)) * 0x94d049bb133111ebUL;
        return z ^ (z >> 31);
    }

    // ---- the oracle ----

    static ulong sink;
    static byte[] walkScratch = Array.Empty<byte>();

    static unsafe void WalkOpened(Unit unit, byte* basePointer, long bytes, long reported)
    {
        FuzzInfo info = unit.Info;
        if (reported < info.Size || reported > bytes)
        {
            Defect("an opened block reports a used extent outside [ the projection, the bytes the caller passed ]");
        }
        int array = 0;
        foreach (FuzzField field in info.Fields)
        {
            if (!field.OutOfLine)
            {
                continue;
            }
            if (array >= unit.Maxima.Length)
            {
                Defect("the unit's maxima table is shorter than its out-of-line arrays");
            }
            ulong offsetOf = *(ulong*) (basePointer + field.OffsetOfOffset);
            uint count = *(uint*) (basePointer + field.CountOffset);
            uint stride = *(uint*) (basePointer + field.StrideOffset);
            FuzzInfo? element = field.Element;

            if (stride != field.Stride)
            {
                Defect("an opened block carries a pitch that is not this build's own (SPEC-TABLES.md §19.3)");
            }
            if (element == null || element.Size != stride)
            {
                Defect("an opened block's row descriptor disagrees with the pitch it opened at");
            }
            if (count > (ulong) unit.Maxima[array])
            {
                Defect("an opened block carries a count past its DECLARED MAXIMUM");
            }
            if (offsetOf < (ulong) info.Size)
            {
                Defect("an opened block's array starts inside the projection");
            }
            ulong startAlignment = 64;
            if (element != null && (ulong) element.Align > startAlignment)
            {
                startAlignment = (ulong) element.Align;
            }
            if ((offsetOf % startAlignment) != 0)
            {
                Defect("an opened block's array does not start aligned for its element (SPEC-TABLES.md §19.1)");
            }
            if (stride != 0 && count > (ulong.MaxValue - offsetOf) / stride)
            {
                Defect("an opened block's array extent does not fit in 64 bits");
            }
            ulong end = offsetOf + (ulong) count * stride;
            if (end > (ulong) bytes)
            {
                Defect("an opened block's rows leave the extent the caller passed");
            }
            if (end > (ulong) reported)
            {
                Defect("an opened block's rows leave the used extent it reported");
            }

            // the whole walk: every byte of every row, CHECKED above and read
            // here. This side has no sanitizer, so the check is the guarantee
            // and the read is what makes the check about something.
            long span = (long) count * stride;
            if (span > 0)
            {
                if (span > walkScratch.Length)
                {
                    walkScratch = new byte[span];
                }
                Marshal.Copy((IntPtr) (basePointer + offsetOf), walkScratch, 0, (int) span);
                sink += (ulong) walkScratch[0] + walkScratch[span - 1];
            }
            array++;
        }
        if (array != unit.Maxima.Length)
        {
            Defect("the unit's maxima table does not match its out-of-line arrays");
        }
    }

    // ---- the mutators: the C++ leg's, value for value ----

    static readonly ulong[] Boundaries =
    {
        0UL, 1UL, 2UL,
        0x7fffffffUL, 0x80000000UL, 0xffffffffUL,
        0x100000000UL,
        0x7fffffffffffffffUL, 0x8000000000000000UL, 0xffffffffffffffffUL,
        0xfffffffffffffffeUL, 0xfffffffe00000000UL,
        // and the same extremes rounded DOWN to a block's 64-byte start
        // alignment, because an offset_of that is not 64-aligned is refused
        // before it reaches the arithmetic and never exercises it
        0x4000000000000000UL, 0x7fffffffffffffc0UL, 0x7fffffffffffff80UL,
        0x8000000000000040UL, 0xffffffffffffffc0UL,
    };

    static int NumValues { get { return Boundaries.Length + 3; } }

    static ulong BoundaryValue(Slot slot, int v)
    {
        if (v < Boundaries.Length)
        {
            return Boundaries[v];
        }
        if (slot.Maximum < 0)
        {
            return Boundaries[(v - Boundaries.Length) % Boundaries.Length];
        }
        switch (v - Boundaries.Length)
        {
            case 0: return (ulong) slot.Maximum - 1;
            case 1: return (ulong) slot.Maximum;
            default: return (ulong) slot.Maximum + 1;
        }
    }

    static unsafe void WriteWord(byte* buffer, long bufferBytes, ulong offset, int width, ulong value)
    {
        if ((long) offset + width > bufferBytes)
        {
            return;
        }
        for (int i = 0; i < width; i++)
        {
            buffer[offset + (ulong) i] = (byte) (value >> (8 * i)); // little-endian, the order this build writes
        }
    }

    static readonly int[] Widths = { 1, 2, 4, 8 };

    static unsafe void MutateRandom(ref Rng rng, Unit unit, byte* buffer, long copied)
    {
        const int kinds = 7;
        int kind = (int) rng.Below(kinds);
        long projection = unit.ProjectionSize;
        switch (kind)
        {
            case 0:
                siteDescription = "no mutation: the valid block itself";
                return;

            case 1:
            {
                int flips = 1 + (int) rng.Below(8);
                string at = "byte flips at";
                for (int i = 0; i < flips; i++)
                {
                    long limit = (rng.Below(4) != 0 && copied > projection) ? projection : copied;
                    if (limit <= 0)
                    {
                        break;
                    }
                    long where = (long) rng.Below((ulong) limit);
                    buffer[where] ^= (byte) (1u << (int) rng.Below(8));
                    at += " " + where;
                }
                siteDescription = at;
                return;
            }

            case 2:
            {
                int which = (int) rng.Below((ulong) unit.Slots.Length + 4);
                ulong offset;
                int width;
                ulong value;
                string name;
                if (which < unit.Slots.Length)
                {
                    Slot slot = unit.Slots[which];
                    name = slot.Name;
                    offset = (ulong) slot.Offset;
                    width = Widths[rng.Below(4)];
                    if (offset + (ulong) width > (ulong) projection)
                    {
                        width = 4;
                    }
                    value = BoundaryValue(slot, (int) rng.Below((ulong) NumValues));
                }
                else
                {
                    name = "anywhere in the projection";
                    offset = rng.Below((ulong) projection);
                    width = Widths[rng.Below(4)];
                    if (offset + (ulong) width > (ulong) projection)
                    {
                        offset = (ulong) projection - (ulong) width;
                    }
                    value = Boundaries[rng.Below((ulong) Boundaries.Length)];
                }
                WriteWord(buffer, copied, offset, width, value);
                siteDescription = (width * 8) + "-bit overwrite of " + name + " at " + offset
                                + " with 0x" + value.ToString("x");
                return;
            }

            case 3:
            {
                int arrays = unit.Maxima.Length;
                if (arrays < 2 || copied < projection)
                {
                    siteDescription = "no mutation: this unit has fewer than two triples to swap";
                    return;
                }
                int a = (int) rng.Below((ulong) arrays);
                int b = (int) rng.Below((ulong) arrays - 1);
                if (b >= a) { b++; }
                int offsetA = unit.Slots[3 + 3 * a].Offset;
                int offsetB = unit.Slots[3 + 3 * b].Offset;
                for (int i = 0; i < 16; i++)
                {
                    byte swap = buffer[offsetA + i];
                    buffer[offsetA + i] = buffer[offsetB + i];
                    buffer[offsetB + i] = swap;
                }
                siteDescription = "the triples of arrays " + a + " and " + b + " swapped whole";
                return;
            }

            case 4:
            {
                int arrays = unit.Maxima.Length;
                if (arrays < 2 || copied < projection)
                {
                    siteDescription = "no mutation: this unit has fewer than two arrays to overlap";
                    return;
                }
                int a = (int) rng.Below((ulong) arrays);
                int b = (int) rng.Below((ulong) arrays - 1);
                if (b >= a) { b++; }
                int offsetA = unit.Slots[3 + 3 * a].Offset;
                int offsetB = unit.Slots[3 + 3 * b].Offset;
                for (int i = 0; i < 8; i++)
                {
                    buffer[offsetA + i] = buffer[offsetB + i];
                }
                siteDescription = "array " + a + "'s rows moved on top of array " + b + "'s";
                return;
            }

            case 5:
            {
                int a = (int) rng.Below((ulong) unit.Maxima.Length);
                ulong offset = (ulong) unit.Slots[3 + 3 * a].Offset;
                ulong value = rng.Below((ulong) projection);
                WriteWord(buffer, copied, offset, 8, value);
                siteDescription = "array " + a + "'s offset_of moved inside the projection, to " + value;
                return;
            }

            default:
            {
                ulong[] orders = { 1, 2, 0, 0x0100000000000000UL };
                ulong value = orders[rng.Below(4)];
                WriteWord(buffer, copied, 16, 8, value);
                siteDescription = "the byte order word forged to " + value + " with the data unswapped";
                return;
            }
        }
    }

    // ---- the units ----

    static Slot[] BuildSlots(FuzzInfo info, long[] maxima, long maxBytes)
    {
        List<Slot> slots = new List<Slot>
        {
            new Slot { Name = "magic", Offset = 0, Maximum = -1 },
            new Slot { Name = "build_version", Offset = 8, Maximum = -1 },
            new Slot { Name = "byte_order", Offset = 16, Maximum = 2 },
        };
        int array = 0;
        foreach (FuzzField field in info.Fields)
        {
            if (!field.OutOfLine)
            {
                continue;
            }
            slots.Add(new Slot { Name = field.Name, Offset = field.OffsetOfOffset, Maximum = maxBytes });
            slots.Add(new Slot { Name = field.Name, Offset = field.CountOffset, Maximum = maxima[array] });
            slots.Add(new Slot { Name = field.Name, Offset = field.StrideOffset, Maximum = field.Stride });
            array++;
        }
        return slots.ToArray();
    }

    static unsafe Unit[] BuildUnits()
    {
        Dictionary<object, FuzzInfo> seen = new Dictionary<object, FuzzInfo>();

        Unit render = new Unit
        {
            Name = "render",
            NumVectors = 5,
            ProjectionSize = sizeof(RenderFrameBlockProjection),
            Maxima = new long[]
            {
                RenderFrameBlock.CamerasMax, RenderFrameBlock.ShipsMax, RenderFrameBlock.TurretsMax,
                RenderFrameBlock.MissilesMax, RenderFrameBlock.DynamicPropsMax, RenderFrameBlock.StaticPropsMax,
                RenderFrameBlock.CosmeticPropsMax, RenderFrameBlock.LasersMax, RenderFrameBlock.ExplosionsMax,
            },
            Info = Convert(RenderFrameBlock.Type, seen),
            Open = (pointer, bytes) =>
            {
                OpenResult result = default;
                result.Opened = RenderFrameBlock.Open(out RenderFrameBlock block, pointer, bytes);
                if (result.Opened)
                {
                    result.Reported = block.Bytes;
                    result.Base = (IntPtr) block.Base;
                }
                else if (block.Base != null || block.Bytes != 0)
                {
                    Defect("a refused block points at something rather than at nothing (SPEC-TABLES.md §19.2)");
                }
                return result;
            },
            TypedWalk = (pointer, bytes) =>
            {
                if (!RenderFrameBlock.Open(out RenderFrameBlock block, pointer, bytes)) { return; }
                ulong accumulator = 0;
                foreach (ref readonly RenderCameraRow row in block.Cameras) { accumulator += row.CameraId; }
                foreach (ref readonly RenderShipRow row in block.Ships) { accumulator += row.ObjectId; }
                foreach (ref readonly RenderTurretRow row in block.Turrets) { accumulator += row.ObjectId; }
                foreach (ref readonly RenderMissileRow row in block.Missiles) { accumulator += row.ObjectId; }
                foreach (ref readonly RenderDynamicPropRow row in block.DynamicProps) { accumulator += row.ObjectId; }
                foreach (ref readonly RenderStaticPropRow row in block.StaticProps) { accumulator += row.StaticPropId; }
                foreach (ref readonly RenderCosmeticPropRow row in block.CosmeticProps) { accumulator += row.CosmeticPropId; }
                foreach (ref readonly RenderLaserRow row in block.Lasers) { accumulator += row.LaserId; }
                foreach (ref readonly RenderExplosionRow row in block.Explosions) { accumulator += row.ExplosionId; }
                sink += accumulator;
            },
        };
        render.Slots = BuildSlots(render.Info, render.Maxima, RenderFrameBlock.BlockMaxBytes);

        Unit padded = new Unit
        {
            Name = "padded",
            NumVectors = 4,
            ProjectionSize = sizeof(PaddedFrameBlockProjection),
            Maxima = new long[] { PaddedFrameBlock.RowsMax },
            Info = Convert(PaddedFrameBlock.Type, seen),
            Open = (pointer, bytes) =>
            {
                OpenResult result = default;
                result.Opened = PaddedFrameBlock.Open(out PaddedFrameBlock block, pointer, bytes);
                if (result.Opened)
                {
                    result.Reported = block.Bytes;
                    result.Base = (IntPtr) block.Base;
                }
                else if (block.Base != null || block.Bytes != 0)
                {
                    Defect("a refused block points at something rather than at nothing (SPEC-TABLES.md §19.2)");
                }
                return result;
            },
            TypedWalk = (pointer, bytes) =>
            {
                if (!PaddedFrameBlock.Open(out PaddedFrameBlock block, pointer, bytes)) { return; }
                ulong accumulator = 0;
                foreach (ref readonly PaddedRowRow row in block.Rows) { accumulator += row.Id + row.Tag; }
                sink += accumulator;
            },
        };
        padded.Slots = BuildSlots(padded.Info, padded.Maxima, PaddedFrameBlock.BlockMaxBytes);

        Unit part = new Unit
        {
            Name = "part",
            NumVectors = 4,
            ProjectionSize = sizeof(PartFrameBlockProjection),
            Maxima = new long[] { PartFrameBlock.PartsMax },
            Info = Convert(PartFrameBlock.Type, seen),
            Open = (pointer, bytes) =>
            {
                OpenResult result = default;
                result.Opened = PartFrameBlock.Open(out PartFrameBlock block, pointer, bytes);
                if (result.Opened)
                {
                    result.Reported = block.Bytes;
                    result.Base = (IntPtr) block.Base;
                }
                else if (block.Base != null || block.Bytes != 0)
                {
                    Defect("a refused block points at something rather than at nothing (SPEC-TABLES.md §19.2)");
                }
                return result;
            },
            TypedWalk = (pointer, bytes) =>
            {
                if (!PartFrameBlock.Open(out PartFrameBlock block, pointer, bytes)) { return; }
                ulong accumulator = 0;
                foreach (ref readonly PartRowRow row in block.Parts) { accumulator += row.PartId + row.Slot; }
                sink += accumulator;
            },
        };
        part.Slots = BuildSlots(part.Info, part.Maxima, PartFrameBlock.BlockMaxBytes);

        return new Unit[] { render, padded, part };
    }

    // ---- the runner ----

    static string? onlyUnit;
    static int onlyVector;
    static string onlyPass = "";
    static long onlyIndex;
    static long mutantsRun;
    static long mutantsOpened;

    static bool Selected(string unit, int vector, string pass, long index)
    {
        if (onlyUnit == null)
        {
            return true;
        }
        return onlyUnit == unit && onlyVector == vector && onlyPass == pass && onlyIndex == index;
    }

    // One mutant: a region of EXACTLY the bytes the caller claims, at a base
    // offset of `lead`, the seed block copied in, the mutation applied, Open,
    // and the oracle.
    static unsafe void RunOne(Unit unit, int vector, string pass, long index, byte[] seedBlock,
                              long claim, int lead, bool randomMutation, ulong rngSeed, string fixedDescription)
    {
        if (!Selected(unit.Name, vector, pass, index))
        {
            return;
        }
        siteUnit = unit.Name;
        siteVector = vector;
        sitePass = pass;
        siteIndex = index;
        siteDescription = fixedDescription;

        long extent = seedBlock.Length;
        nuint want = (nuint) (claim + lead);
        if (want == 0) { want = 1; }
        byte* allocation = (byte*) NativeMemory.AlignedAlloc(want, 64);
        try
        {
            byte* basePointer = allocation + lead;
            long copied = claim < extent ? claim : extent;
            if (copied > 0)
            {
                Marshal.Copy(seedBlock, 0, (IntPtr) basePointer, (int) copied);
            }
            if (claim > copied)
            {
                // extension with GARBAGE: the bytes past the seed block are not zeros
                Rng garbage = new Rng { State = Mix(rngSeed, 0xda7a) };
                for (long i = copied; i < claim; i++)
                {
                    basePointer[i] = (byte) garbage.Next();
                }
            }

            if (randomMutation)
            {
                Rng rng = new Rng { State = rngSeed };
                MutateRandom(ref rng, unit, basePointer, copied);
            }
            if (claim != extent || lead != 0)
            {
                siteDescription += (siteDescription.Length != 0 ? "; " : "")
                                 + "[ " + claim + " bytes claimed of a " + extent + "-byte block, base + " + lead + " ]";
            }

            mutantsRun++;
            OpenResult result = unit.Open((IntPtr) basePointer, claim);
            if (!result.Opened)
            {
                return;
            }
            if (result.Base != (IntPtr) basePointer)
            {
                Defect("an opened block points somewhere other than at the base the caller passed");
            }
            WalkOpened(unit, basePointer, claim, result.Reported);
            unit.TypedWalk((IntPtr) basePointer, claim);
            mutantsOpened++;
        }
        finally
        {
            NativeMemory.AlignedFree(allocation);
        }
    }

    static unsafe void RunUnit(Unit unit, int unitIndex, string directory, ulong seed, long randomMutants)
    {
        for (int vector = 0; vector < unit.NumVectors; vector++)
        {
            string path = Path.Combine(directory, unit.Name + "_v" + vector + ".bin");
            if (!File.Exists(path))
            {
                Console.WriteLine("FAILED: missing block fuzz seed " + path + " (the C++ leg writes it with --dump)");
                Environment.Exit(1);
            }
            byte[] seedBlock = File.ReadAllBytes(path);
            long extent = seedBlock.Length;

            // the unmutated block opens, so a green run is not a fuzzer that
            // refuses everything
            {
                siteUnit = unit.Name; siteVector = vector; sitePass = "valid"; siteIndex = 0;
                RunOne(unit, vector, "valid", 0, seedBlock, extent, 0, false, 0, "the valid block, unmutated");
                if (onlyUnit == null && mutantsOpened == 0)
                {
                    Defect("the VALID block the C++ builder wrote did not open on this side");
                }
            }

            // every length in [ 0, extent + 64 ], exhaustive where the sum of
            // the copies stays sane and sampled beyond
            {
                const long exhaustiveLimit = 8192;
                if (extent <= exhaustiveLimit)
                {
                    for (long claim = 0; claim <= extent + 64; claim++)
                    {
                        RunOne(unit, vector, "trunc", claim, seedBlock, claim, 0, false, 0,
                               "truncated or extended, otherwise untouched");
                    }
                }
                else
                {
                    long index = 0;
                    long[] interesting =
                    {
                        0, 1, 8, unit.ProjectionSize - 1, unit.ProjectionSize, unit.ProjectionSize + 1,
                        extent - 65, extent - 64, extent - 63, extent - 1, extent, extent + 1, extent + 63, extent + 64,
                    };
                    foreach (long claim in interesting)
                    {
                        if (claim >= 0 && claim <= extent + 64)
                        {
                            RunOne(unit, vector, "trunc", index, seedBlock, claim, 0, false, 0,
                                   "truncated or extended, otherwise untouched");
                        }
                        index++;
                    }
                    long samples = extent > (1 << 20) ? 64 : 256;
                    for (long k = 0; k < samples; k++, index++)
                    {
                        Rng rng = new Rng { State = Mix(seed, Mix((ulong) vector, (ulong) k)) };
                        long claim = (long) rng.Below((ulong) extent + 65);
                        RunOne(unit, vector, "trunc", index, seedBlock, claim, 0, false, 0,
                               "truncated or extended, otherwise untouched");
                    }
                }
            }

            // the caller's buffer at base + 1 .. base + 63
            for (int lead = 1; lead < 64; lead++)
            {
                RunOne(unit, vector, "lead", lead, seedBlock, extent, lead, false, 0, "an unaligned base");
            }

            // every named slot x every width x every boundary value
            byte* slotRegion = (byte*) NativeMemory.AlignedAlloc((nuint) extent, 64);
            try
            {
                Marshal.Copy(seedBlock, 0, (IntPtr) slotRegion, (int) extent);
                long index = 0;
                foreach (Slot slot in unit.Slots)
                {
                    foreach (int width in Widths)
                    {
                        if (slot.Offset + width > unit.ProjectionSize)
                        {
                            continue;
                        }
                        for (int v = 0; v < NumValues; v++, index++)
                        {
                            if (!Selected(unit.Name, vector, "slot", index))
                            {
                                continue;
                            }
                            siteUnit = unit.Name; siteVector = vector; sitePass = "slot"; siteIndex = index;
                            ulong value = BoundaryValue(slot, v);
                            siteDescription = (width * 8) + "-bit overwrite of " + slot.Name + " at " + slot.Offset
                                            + " with 0x" + value.ToString("x");
                            // the PROJECTION restored between mutants: a slot
                            // overwrite touches nothing else, and re-copying a
                            // 7.5 MiB block per mutant would spend the whole
                            // gate budget on memcpy without covering one case more
                            Marshal.Copy(seedBlock, 0, (IntPtr) slotRegion, unit.ProjectionSize);
                            WriteWord(slotRegion, extent, (ulong) slot.Offset, width, value);
                            mutantsRun++;
                            OpenResult slotResult = unit.Open((IntPtr) slotRegion, extent);
                            if (slotResult.Opened)
                            {
                                WalkOpened(unit, slotRegion, extent, slotResult.Reported);
                                unit.TypedWalk((IntPtr) slotRegion, extent);
                                mutantsOpened++;
                            }
                        }
                    }
                }
            }
            finally
            {
                NativeMemory.AlignedFree(slotRegion);
            }

            // the seeded mutators, over lengths and leads too
            {
                long budget = randomMutants / unit.NumVectors;
                if (extent > (1 << 20))
                {
                    budget /= 8;
                }
                for (long k = 0; k < budget; k++)
                {
                    ulong rngSeed = Mix(Mix(seed, Mix((ulong) unitIndex, (ulong) vector)), (ulong) k);
                    Rng axes = new Rng { State = Mix(rngSeed, 0x5eed) };
                    long claim = extent;
                    if (axes.Below(4) == 0)
                    {
                        claim = (long) axes.Below((ulong) extent + 65);
                    }
                    int lead = 0;
                    if (axes.Below(8) == 0)
                    {
                        lead = 1 + (int) axes.Below(63);
                    }
                    RunOne(unit, vector, "random", k, seedBlock, claim, lead, true, rngSeed, "");
                }
            }
        }
    }

    public static bool Run(string directory)
    {
        ulong seed = 0x5c8ea11deUL;
        long randomMutants = 200000;
        string? environmentSeed = Environment.GetEnvironmentVariable("SEED");
        if (!string.IsNullOrEmpty(environmentSeed))
        {
            seed = System.Convert.ToUInt64(environmentSeed, environmentSeed.StartsWith("0x") ? 16 : 10);
        }
        string? environmentN = Environment.GetEnvironmentVariable("N");
        if (!string.IsNullOrEmpty(environmentN))
        {
            randomMutants = long.Parse(environmentN);
        }
        string? only = Environment.GetEnvironmentVariable("ONLY");
        if (!string.IsNullOrEmpty(only))
        {
            string[] parts = only.Split(':');
            if (parts.Length != 4)
            {
                Console.WriteLine("FAILED: ONLY wants unit:vector:pass:index");
                return false;
            }
            onlyUnit = parts[0];
            onlyVector = int.Parse(parts[1]);
            onlyPass = parts[2];
            onlyIndex = long.Parse(parts[3]);
        }
        runSeed = seed;

        Console.WriteLine("block forgery fuzzer (C#): SEED=" + seed + " N=" + randomMutants
                          + " (per unit, across its count vectors)");

        Unit[] units = BuildUnits();
        for (int i = 0; i < units.Length; i++)
        {
            RunUnit(units[i], i, directory, seed, randomMutants);
        }

        Console.WriteLine("block forgery fuzzer (C#): " + mutantsRun + " mutants over " + units.Length
                          + " units, " + mutantsOpened
                          + " opened and walked whole, none escaped the extent (SPEC-TABLES.md §19.2, §19.5)");
        return true;
    }
}
