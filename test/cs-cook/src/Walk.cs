// THE WALK: every node the C# side reaches, through its OWN derefs — and the
// DIRECTORY, which is the tool's independent statement of where every node is
// (docs/SPEC-TABLES.md §7.1, §7.5).
//
// Generic over the cook descriptors, which is the whole point of them: a
// pointer slot is eight bytes at `Offset` holding the SIGNED SELF-RELATIVE
// delta of §6.3, so a deref is one add and needs no base pointer, and a delta
// of zero is null. A by-value nesting — a record inside a record, an element of
// a bounded or enum-keyed array — is not a node; it is storage inside one, and
// the walk descends through it to reach the pointer slots inside.

using System;
using System.Collections.Generic;
using System.Text;
using Graphdemo;

static unsafe class Walk
{
    public struct Reached
    {
        public ulong Offset;
        public TableCookInfo Type;
    }

    public static readonly List<Reached> reached = new List<Reached>();

    static byte* region;
    static ulong regionLength;
    static StringBuilder dump;

    public static int Find(ulong offset)
    {
        for (int i = 0; i < reached.Count; i++)
        {
            if (reached[i].Offset == offset)
            {
                return i;
            }
        }
        return -1;
    }

    // Run one walk from the root. `text` is non-null in the DUMP mode, which is
    // the cross-implementation lock on VALUES: the same walk, the same order,
    // the same canonical lines as the C++ leg's.
    public static void Run(TableCookInfo rootType, byte* regionBase, ulong dataLength, StringBuilder text)
    {
        reached.Clear();
        region = regionBase;
        regionLength = dataLength;
        dump = text;
        Node(0, rootType, 0);
    }

    static void Node(ulong offset, TableCookInfo type, int depth)
    {
        if (depth > 4096)
        {
            Fail.Now("the walk nested past any depth a region can hold — a cycle the deref did not close");
        }
        int at = Find(offset);
        if (at >= 0)
        {
            if (!ReferenceEquals(reached[at].Type, type))
            {
                Fail.Now("two references name the node at offset " + offset + " as two different tables: " +
                         reached[at].Type.Name + " and " + type.Name);
            }
            // one node, one visit: sharing and a back-reference are the same
            // fact (§6.3)
            return;
        }
        if (offset > regionLength || (ulong)type.Size > regionLength - offset)
        {
            Fail.Now("the node at offset " + offset + " (" + type.Name + ", sizeof " + type.Size +
                     ") does not fit inside the region's " + regionLength + " bytes");
        }
        Reached r;
        r.Offset = offset;
        r.Type = type;
        int index = reached.Count;
        reached.Add(r);
        if (dump != null)
        {
            dump.Append("node ").Append(index).Append(' ').Append(type.Name).Append(" @").Append(offset).Append('\n');
        }
        Storage(region + offset, type, "", depth);
    }

    static void Storage(byte* storage, TableCookInfo type, string path, int depth)
    {
        for (int i = 0; i < type.NumFields; i++)
        {
            TableCookFieldInfo f = type.Fields[i];
            string name = path.Length == 0 ? f.Name : path + "." + f.Name;

            // every COUNT COMPANION, against its declared bound, and a NEGATIVE
            // one refuses too — an extent is never negative, and a walker
            // handed one indexes backwards out of the region (§7.4's pass two)
            int used = -1;
            if (f.CountOffset >= 0)
            {
                used = *(int*)(storage + f.CountOffset);
                if (used < 0 || used > f.ArrayBound)
                {
                    Fail.Now(type.Name + "." + f.Name + " carries a count companion of " + used +
                             ", outside [ 0, " + f.ArrayBound + " ]");
                }
            }

            if (f.IsPointer)
            {
                long delta = *(long*)(storage + f.Offset);
                if (delta == 0)
                {
                    // NULL IN A REGION IS A DELTA OF ZERO (§6.3)
                    Emit(name, "null");
                    continue;
                }
                byte* slot = storage + f.Offset;
                byte* target = slot + delta;
                if (target < region || target >= region + regionLength)
                {
                    Fail.Now(type.Name + "." + f.Name + " resolves outside the region — a delta of " + delta +
                             " from a slot at " + (long)(slot - region));
                }
                if (f.Record == null)
                {
                    Fail.Now(type.Name + "." + f.Name + " is a pointer whose descriptor names no record");
                }
                ulong targetOffset = (ulong)(target - region);
                Emit(name, "-> @" + targetOffset);
                Node(targetOffset, f.Record, depth + 1);
                continue;
            }

            switch (f.Storage)
            {
                case TableCookStorage.String:
                case TableCookStorage.Bytes:
                    Emit(name, Text(storage + f.Offset, used, f.Storage == TableCookStorage.String));
                    break;
                case TableCookStorage.Record:
                {
                    // a nested record — by value, or every slot of an array of
                    // them. A COUNTED array writes all N slots (§7.2), and a
                    // slot past the live count holds the value-initialized
                    // element, whose pointer slots are zero: walking all of
                    // them is what the check does too.
                    int slots = f.IsArray ? f.ArrayBound : 1;
                    for (int s = 0; s < slots; s++)
                    {
                        string element = f.IsArray ? name + "[" + s + "]" : name;
                        Storage(storage + f.Offset + (long)s * f.ElemSize, f.Record, element, depth);
                    }
                    break;
                }
                default:
                {
                    int slots = f.IsArray ? f.ArrayBound : 1;
                    for (int s = 0; s < slots; s++)
                    {
                        string element = f.IsArray ? name + "[" + s + "]" : name;
                        Emit(element, Scalar(storage + f.Offset + (long)s * f.ElemSize, f.Storage, f.ElemSize));
                    }
                    break;
                }
            }

            if (f.CountOffset >= 0 && f.Storage != TableCookStorage.String && f.Storage != TableCookStorage.Bytes)
            {
                Emit(name + "#count", used.ToString());
            }
            if (f.PresentOffset >= 0)
            {
                Emit(name + "#present", *(storage + f.PresentOffset) != 0 ? "true" : "false");
            }
        }
    }

    static void Emit(string path, string value)
    {
        if (dump != null)
        {
            dump.Append("  ").Append(path).Append(" = ").Append(value).Append('\n');
        }
    }

    // A string's or a `bytes`' USED bytes, without the zero tail past them
    // (§7.2) — printed the same way on both legs, non-printable bytes escaped
    // so the comparison stays a byte comparison of text.
    static string Text(byte* at, int used, bool isString)
    {
        if (used < 0)
        {
            used = 0;
        }
        StringBuilder b = new StringBuilder();
        b.Append('"');
        for (int i = 0; i < used; i++)
        {
            byte c = at[i];
            if (c >= 0x20 && c < 0x7f && c != '"' && c != '\\')
            {
                b.Append((char)c);
            }
            else
            {
                b.Append("\\x").Append(c.ToString("x2"));
            }
        }
        b.Append('"');
        b.Append(" len=").Append(used);
        _ = isString;
        return b.ToString();
    }

    static string Scalar(byte* at, TableCookStorage storage, int width)
    {
        switch (storage)
        {
            case TableCookStorage.Bool:
                return *at != 0 ? "true" : "false";
            case TableCookStorage.Float:
                // Nothing in the pointered corpus is a float, and a canonical
                // cross-language spelling of one is a decision this gate should
                // not make in passing. The day a float arrives, the gate says
                // so rather than drifting.
                Fail.Now("the dump met a float, whose canonical cross-language spelling this gate does not fix");
                return "";
            case TableCookStorage.Signed:
                switch (width)
                {
                    case 1: return ((sbyte)*at).ToString();
                    case 2: return (*(short*)at).ToString();
                    case 4: return (*(int*)at).ToString();
                    default: return (*(long*)at).ToString();
                }
            default:
                switch (width)
                {
                    case 1: return (*at).ToString();
                    case 2: return (*(ushort*)at).ToString();
                    case 4: return (*(uint*)at).ToString();
                    default: return (*(ulong*)at).ToString();
                }
        }
    }
}

// The cooked file's shape, as §7.1 states it — read HERE and never by the
// runtime. The header words this harness reads for itself, and the ATTRIBUTION
// part it reads as its ORACLE.
static unsafe class Header
{
    public const long Bytes = 64;
    public const ulong Magic = 0x4b4f4f434d484353UL; // "SCHMCOOK"

    public const int WordMagic = 0;
    public const int WordBuildVersion = 8;
    public const int WordByteOrder = 16;
    public const int WordDataLength = 24;
    public const int WordAttributionLength = 32;
    public const int WordAlignment = 40;
    public const int WordReserved0 = 48;
    public const int WordReserved1 = 56;

    public static ulong Read64(byte* p) { return *(ulong*)p; }
    public static ulong Read64(byte[] b, int at)
    {
        ulong v = 0;
        for (int i = 7; i >= 0; i--)
        {
            v = (v << 8) | b[at + i];
        }
        return v;
    }

    public static void Write64(byte* p, ulong v) { *(ulong*)p = v; }

    // A cooked file's own alignment rule, re-derived here rather than asked of
    // the code under test: the data part begins at align_up( 64, alignment ).
    public static ulong DataOffset(ulong alignment)
    {
        return (ulong)(Bytes + (long)alignment - 1) & ~(alignment - 1);
    }

    // The node type id (docs/SPEC-TABLES.md §3.1, §7.3): fnv1a64 over the TABLE'S
    // NAME. Derived here from the declaration's own name rather than read back
    // out of the file the oracle is checking.
    public static ulong Fnv1a64(string s)
    {
        ulong h = 0xcbf29ce484222325UL;
        foreach (char c in s)
        {
            h ^= (byte)c;
            h *= 0x100000001b3UL;
        }
        return h;
    }
}

// The ATTRIBUTION part: one entry per numbered node, in index order, each
// `offset (u64), type id (u64)`, position 0 being the root at offset 0.
unsafe struct Directory
{
    public byte* Entries;
    public ulong Count;

    public ulong Offset(ulong i) { return Header.Read64(Entries + i * 16); }
    public ulong Type(ulong i) { return Header.Read64(Entries + i * 16 + 8); }

    public static Directory Of(byte* file, ulong length)
    {
        ulong alignment = Header.Read64(file + Header.WordAlignment);
        ulong dataLength = Header.Read64(file + Header.WordDataLength);
        ulong attribution = Header.Read64(file + Header.WordAttributionLength);
        ulong offset = Header.DataOffset(alignment);
        if (attribution == 0)
        {
            Fail.Now("the fixture carries no attribution part, so there is nothing to check the walk against");
        }
        if (attribution % 16 != 0 || offset + dataLength + attribution != length)
        {
            Fail.Now("the fixture's own header does not frame it: " + offset + " + " + dataLength + " + " +
                     attribution + " != " + length);
        }
        Directory dir;
        dir.Entries = file + offset + dataLength;
        dir.Count = attribution / 16;
        return dir;
    }
}
