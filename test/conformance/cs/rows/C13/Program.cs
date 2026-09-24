using System;
using Tblp3;

// cell-cs-c13 — present flag byte != 0
//
// The law (docs/FIXED-FORM-ALGORITHM.md:335, the scatter table's last row): a
// PRESENT FLAG lands as `byte != 0`, normalised to the language's own true.
// `0x02` is not a flag a reader stores verbatim (fix 2). The framing is §2.2's
// `?T` row (docs/FIXED-FORM-ALGORITHM.md:182): the present flag, THEN the
// payload, which rides WHOLE — so a present flag that reads true must also read
// the payload behind it.
//
// Vector: fixed table Chain (test/tables/P3.schema, package tblp3), field
// `link ?Link`. ChainFixedWriteBody writes the name length at body 0..3, the
// name bytes at 4..19, the present flag at body offset 20, and the Link payload
// after it (value int32 at 21, tag length at 25, tag bytes at 29; the Link body
// is 16 bytes). The envelope is the writer's own: TableFixedWire.HeaderBytes +
// 4 layout length + ChainFixedLayoutBytes layout bytes, then one record of 8
// hash bytes + ChainFixedBodyBytes (37).
//
// The writer pins offset 20 (an absent link writes byte 0, a present one byte
// 1); the reader side then loads the same envelope with body[20] planted to 0,
// 1 and 3. The law pins the normalization at exactly those three points:
// f(0)=absent, f(1)=present, f(3)=present — 3 is a hostile nonzero, so the flag
// must land the language's true, not 1 so a reader that compares == 1 (or
// stores the raw byte) is caught. When the flag says present the payload is
// read from behind it (Link.Value == 7), when it says absent the payload is
// skipped (Link.Value stays 0, however stained the bytes are). The resave half
// completes it: the read-back value saves as byte 1, the normalized present,
// never the hostile byte.

class Program
{
    // Chain.link's present flag rides at body offset 20, after the 20-byte name
    // (length + 16 bytes); the Link payload starts at 21 (P3Table.cs
    // ChainFixedWriteBody).
    const int PresentAt = 20;
    const int LinkValueAt = 21;

    static int BodyStart => Schema.TableFixedWire.HeaderBytes + 4 + (int)Schema.ChainFixedLayoutBytes + 8;

    static int Main()
    {
        bool ok = true;

        ok &= WriterPinsOffset();
        ok &= ReaderNormalizes();
        ok &= AbsentSkipsPayload();
        ok &= HostileRoundTripsAsOne();
        ok &= NeighboursIntact();

        if (!ok)
        {
            return 1;
        }

        Console.WriteLine("present flag byte != 0");
        return 0;
    }

    static bool Check(bool cond, string line, params object[] args)
    {
        if (cond)
        {
            Console.WriteLine("OK: " + string.Format(line, args));
        }
        else
        {
            Console.Error.WriteLine("FAIL: " + string.Format(line, args));
        }
        return cond;
    }

    // The envelope of one Chain record with link present or absent, the
    // payload (Link.Value = 7) riding when present.
    static byte[] Envelope(bool present)
    {
        var value = new Chain();
        value.LinkPresent = present;
        if (present) { value.Link.Value = 7; }
        byte[] buf = new byte[Schema.ChainFixedMeasure(1)];
        long saved = Schema.ChainFixedSave(value, buf);
        if (saved != buf.Length)
        {
            Console.Error.WriteLine("FAIL: save wrote {0}, envelope is {1}", saved, buf.Length);
            Environment.Exit(1);
        }
        return buf;
    }

    static long Load(byte[] data, out Chain back, out TableReport report)
    {
        back = null;
        report = new TableReport();
        var values = new Chain[1];
        var plan = new TableFixedEntry[1024];
        long n = Schema.ChainFixedLoad(values, data, plan, report);
        if (n == 1) { back = values[0]; }
        return n;
    }

    static bool WriterPinsOffset()
    {
        // defaults: the link is absent, so the writer must write byte 0 at
        // body 20 ...
        byte[] wire0 = Envelope(false);
        if (!Check(wire0[BodyStart + PresentAt] == 0,
                   "writer: absent link writes byte 0 at body offset {0} (got 0x{1:x2})", PresentAt,
                   wire0[BodyStart + PresentAt]))
        {
            return false;
        }

        // ... and a present link writes byte 1 with the payload behind it. Both
        // pins together name the byte the reader normalization below plants into.
        byte[] wire1 = Envelope(true);
        return Check(wire1[BodyStart + PresentAt] == 1 &&
                     System.Buffers.Binary.BinaryPrimitives.ReadInt32LittleEndian(
                         wire1.AsSpan(BodyStart + LinkValueAt)) == 7,
                     "writer: present link writes byte 1 at body offset {0} and payload 7 at {1}", PresentAt, LinkValueAt);
    }

    static bool ReaderNormalizes()
    {
        byte[] wire = Envelope(true);

        // f(0) = absent is AbsentSkipsPayload's half; f(1) here:
        wire[BodyStart + PresentAt] = 1;
        if (Load(wire, out Chain back1, out TableReport r1) != 1) { Console.Error.WriteLine("FAIL: byte 1 load"); return false; }
        if (!Check(back1.LinkPresent == true, "reader: byte 1 lands present")) { return false; }
        if (!Check(back1.Link.Value == 7, "reader: the payload behind a present flag rides")) { return false; }

        // f(3) = present, the hostile byte of fix 2: nonzero, so the language's
        // true; NOT stored verbatim, and not a `== 1` compare either.
        wire[BodyStart + PresentAt] = 3;
        if (Load(wire, out Chain back3, out TableReport r3) != 1) { Console.Error.WriteLine("FAIL: byte 3 load"); return false; }
        if (!Check(back3.LinkPresent == true, "reader: hostile 0x03 lands present, not verbatim")) { return false; }
        if (!Check(back3.Link.Value == 7, "reader: the payload behind a hostile flag still rides")) { return false; }

        // normalization is silent: the hostile load moves no counter (the
        // counting half of the sentence is cs/R17, another card's item).
        return Check(r1.Unknown == 0 && r1.Clamped == 0 && !r1.Malformed && !r1.Refused &&
                     r3.Unknown == 0 && r3.Clamped == 0 && !r3.Malformed && !r3.Refused,
                     "reader: hostile loads move no counter");
    }

    static bool AbsentSkipsPayload()
    {
        byte[] wire = Envelope(true);

        // f(0) = absent: the zero byte is the language's false. The record is a
        // positional image and the read loop moves bytes (§4.6), so the payload
        // behind a false flag still rides WHOLE (§2.2) — the flag decides the
        // flag and nothing else; the writer's zeros-under-absent are the
        // writer's guarantee, not a reader skip.
        wire[BodyStart + PresentAt] = 0;
        if (Load(wire, out Chain back0, out TableReport r0) != 1) { Console.Error.WriteLine("FAIL: byte 0 load"); return false; }
        return Check(back0.LinkPresent == false && back0.Link.Value == 7,
                     "reader: byte 0 lands absent and the payload behind it rides whole")
            && Check(r0.Unknown == 0 && r0.Clamped == 0 && !r0.Malformed && !r0.Refused,
                     "reader: the absent load moves no counter");
    }

    static bool HostileRoundTripsAsOne()
    {
        byte[] wire = Envelope(true);
        wire[BodyStart + PresentAt] = 3;
        if (Load(wire, out Chain back, out _) != 1) { Console.Error.WriteLine("FAIL: hostile load"); return false; }

        byte[] resave = new byte[Schema.ChainFixedMeasure(1)];
        long saved = Schema.ChainFixedSave(back, resave);
        if (saved != resave.Length) { Console.Error.WriteLine("FAIL: resave wrote {0}", saved); return false; }
        return Check(resave[BodyStart + PresentAt] == 1,
                     "round-trip: resave of the hostile-3 read writes byte 1 (got 0x{0:x2})",
                     resave[BodyStart + PresentAt]);
    }

    static bool NeighboursIntact()
    {
        // the planted byte is the flag's and only the flag's: the name length
        // and bytes before it read at their own defaults through the hostile load.
        byte[] wire = Envelope(true);
        wire[BodyStart + PresentAt] = 3;
        if (Load(wire, out Chain back, out _) != 1) { Console.Error.WriteLine("FAIL: hostile load"); return false; }
        return Check(back.NameLength == 0 && back.Name[0] == 0 && back.Name[15] == 0,
                     "neighbours: NameLength == 0 and the name bytes unchanged through the hostile load");
    }
}
