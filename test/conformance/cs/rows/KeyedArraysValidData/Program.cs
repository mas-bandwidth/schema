using System;
using System.Buffers.Binary;
using Tblrt2;

// cell3-cs-keyed-arrays-valid-data — Enum-keyed arrays: valid-data
// write/read acceptance
//
// The law (docs/FIXED-FORM-ALGORITHM.md:1682, proof 1): values set by hand,
// one form-`3` file — "Read a file and save it back; the bytes must be
// identical — a byte a port encodes differently is a byte that does not come
// back." Contract: docs/SPEC-TABLES.md §2.4, §3.4 record, [Enum]T.
//
// The fixture is RT2's `fixed table Parcel` (test/tables/RT2.schema), whose
// `bins [Grade]Bolt` is an ENUM-KEYED array — one slot per named variant of
// Grade {Bronze, Silver}, no count (§2.4) — beside a scalar, a union, a plain
// nested table and a counted array, so a slot that slides is visible. The
// rt2 unit's table wire carries no serialize dependency, so this row test
// compiles standalone against build/tables-generated-cs (the pattern
// test/conformance/cs/rows/W14 set).
//
// The cs tree's only keyed-array assertions live in SKIPPED functions
// (test/cs-tables/src/FixedFormChecks.cs TestFixedVCase, retired by §5.6):
// this file is the active runtime value/byte assertion the roadmap row
// `keyed-arrays/cs/valid-data` owes — exact non-default values in EVERY live
// keyed slot, the keyed lane's own bytes, and the save/read/save identity.
//
// THE VECTOR, one record with every field off its declared default, derived
// from §3.4: fields dense in declaration order; a union is its arm byte then
// the arm's payload; an ENUM-KEYED array is one element per named variant,
// dense in variant order, with NO count byte; a counted array's count
// precedes its payload; slack is the template's zeros. Parcel declares
// grade, fit, bins, core, stack, and Bolt is one int32 (weight, declared
// | min = 0, max = 1000), so the 30 body bytes are:
//
//   0      grade: ordinal Silver = 2                    02
//   1..5   fit: arm byte Tally = 2, int32 777           02 09 03 00 00
//   6..9   bins[Bronze]: weight 321                     41 01 00 00
//  10..13  bins[Silver]: weight 654                     8E 02 00 00
//  14..17  core: weight 999                             E7 03 00 00
//  18..21  stack: count 1                               01 00 00 00
//  22..25  stack[0]: weight 55                          37 00 00 00
//  26..29  stack[1]: unused slot, the template's zeros  00 00 00 00
//
// Every value is inside its declaration, so a clean read moves no counter.

static class Program
{
    // the body vector derived above, keyed lane included
    static readonly byte[] ExpectedBody = new byte[]
    {
        0x02,
        0x02, 0x09, 0x03, 0x00, 0x00,
        0x41, 0x01, 0x00, 0x00,
        0x8E, 0x02, 0x00, 0x00,
        0xE7, 0x03, 0x00, 0x00,
        0x01, 0x00, 0x00, 0x00,
        0x37, 0x00, 0x00, 0x00,
        0x00, 0x00, 0x00, 0x00,
    };

    static int Main()
    {
        bool ok = true;

        ok &= CheckShape();
        ok &= CheckWriteRead();

        if (!ok)
        {
            return 1;
        }

        Console.WriteLine("Enum-keyed arrays: valid-data write/read acceptance");
        return 0;
    }

    static bool Check(bool cond, string what)
    {
        if (!cond)
        {
            Console.Error.WriteLine("FAIL: " + what);
            return false;
        }
        Console.WriteLine("OK: " + what);
        return true;
    }

    // §2.4: one slot per named variant — the extent is the enum's, dense from
    // Bronze = 1, and None keys none.
    static bool CheckShape()
    {
        bool ok = true;

        ok &= Check((int)Grade.Bronze == 1 && (int)Grade.Silver == 2 && (int)Grade.Max == 2,
            "Grade's variants are dense from 1 and Max = 2");
        ok &= Check(new Parcel().Bins.Slots.Length == 2,
            "bins [Grade]Bolt holds one slot per named variant: 2");
        return ok;
    }

    static bool CheckWriteRead()
    {
        bool ok = true;

        // the writer: every field off its declared default, the keyed slots
        // set through the enum-keyed indexer so the KEY names the slot
        Parcel p = new Parcel();
        p.Grade = Grade.Silver;
        p.Fit.Type = FittingType.Tally;
        p.Fit.Tally = 777;
        p.Bins[(int)Grade.Bronze].Weight = 321;
        p.Bins[(int)Grade.Silver].Weight = 654;
        p.Core.Weight = 999;
        p.StackCount = 1;
        p.Stack[0].Weight = 55;

        ok &= Check(p.Bins.Slots[0].Weight == 321 && p.Bins.Slots[1].Weight == 654,
            "the indexer keys the slots by variant: Bronze lands 0, Silver lands 1");

        byte[] file = new byte[Schema.ParcelFixedMeasure(1)];
        ok &= Check(Schema.ParcelFixedSave(p, file) == file.Length,
            "the record saves: the writer fills exactly what measure says");

        // the file half of the assertion: the form, the hash, the layout the
        // header names, and the keyed lane's own bytes
        ok &= Check(file[0] == Schema.TableFixedWire.Form,
            "the form byte is 3, the fixed form");
        bool reserved = true;
        for (int i = 1; i < Schema.TableFixedWire.HashAt; ++i)
        {
            reserved &= file[i] == 0;
        }
        ok &= Check(reserved, "the reserved header bytes ride as zeros");
        ok &= Check(BinaryPrimitives.ReadUInt64LittleEndian(file.AsSpan(Schema.TableFixedWire.HashAt))
            == Schema.ParcelFixedHash,
            "the header carries the layout's hash");
        uint layoutLen = BinaryPrimitives.ReadUInt32LittleEndian(file.AsSpan(Schema.TableFixedWire.HeaderBytes));
        ok &= Check(layoutLen == (uint)Schema.ParcelFixedLayoutBytes,
            "the header names the layout's length");
        int recordAt = Schema.TableFixedWire.HeaderBytes + 4 + (int)layoutLen;
        ok &= Check(BinaryPrimitives.ReadUInt64LittleEndian(file.AsSpan(recordAt))
            == Schema.ParcelFixedHash,
            "the record is stamped with its layout's hash");
        ReadOnlySpan<byte> body = file.AsSpan(recordAt + 8, (int)Schema.ParcelFixedBodyBytes);
        ok &= Check(body.SequenceEqual(ExpectedBody),
            "the 30 body bytes are the vector derived from \u00a73.4, keyed lane included: "
            + Convert.ToHexString(body));

        // the reader: every field a read could land is poisoned first, so a
        // byte that does not land keeps its poison and shows
        Parcel back = new Parcel();
        back.Grade = Grade.Bronze;
        back.Fit.Type = FittingType.Bolt;
        back.Fit.Bolt.Weight = -111;
        back.Bins.Slots[0].Weight = -777;
        back.Bins.Slots[1].Weight = -888;
        back.Core.Weight = -999;
        back.StackCount = 2;
        back.Stack[0].Weight = -1;
        back.Stack[1].Weight = -2;
        ok &= Check(back.Bins.Slots[0].Weight == -777 && back.Bins.Slots[1].Weight == -888,
            "CONTROL: the keyed slots really are poisoned in storage");

        TableReport r = new TableReport();
        TableFixedEntry[] plan = new TableFixedEntry[1024];
        long n = Schema.ParcelFixedLoad(back, file, plan, r);
        ok &= Check(n == 1, "one record reads");

        // THE ROW: the exact non-default value lands in EVERY live keyed slot
        ok &= Check(back.Bins[(int)Grade.Bronze].Weight == 321,
            "KEYED [Bronze]: the exact non-default value lands over the poison");
        ok &= Check(back.Bins[(int)Grade.Silver].Weight == 654,
            "KEYED [Silver]: the exact non-default value lands over the poison");
        ok &= Check(back.Grade == Grade.Silver,
            "the scalar before the keyed array lands");
        ok &= Check(back.Fit.Type == FittingType.Tally && back.Fit.Tally == 777,
            "the union before the keyed array lands");
        ok &= Check(back.Core.Weight == 999,
            "the nested table after the keyed array lands: no slide");
        ok &= Check(back.StackCount == 1 && back.Stack[0].Weight == 55,
            "the counted array after the keyed array lands: no slide");
        ok &= Check(r.Unknown == 0 && r.KindMismatch == 0 && r.Widened == 0 && r.Clamped == 0
            && !r.Malformed && !r.Refused,
            "a clean read moves no counter");

        // the law's identity: read a file and save it back; the bytes must be
        // identical
        byte[] again = new byte[Schema.ParcelFixedMeasure(1)];
        ok &= Check(Schema.ParcelFixedSave(back, again) == again.Length,
            "the read-back record saves");
        ok &= Check(again.AsSpan().SequenceEqual(file),
            "read a file and save it back: the bytes are identical");

        return ok;
    }
}
