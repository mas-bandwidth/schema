using System;
using Tblm1;

// cell-cs-c12 — bool byte != 0
//
// The law (docs/FIXED-FORM-ALGORITHM.md:335, the scatter table's last row): a
// `bool` lands as `byte != 0`, normalised to the language's own true. `0x02` is
// not a bool a reader stores verbatim (fix 2).
//
// Vector: fixed table Save (test/tables/M1.schema, package tblm1), field `force
// bool`, the body's last byte after `path string(16)`. SaveFixedWriteBody writes
// the path length at body 0..3, the path bytes at 4..19, and the bool at body
// offset 20: `b.Slice(20)[0] = (byte)(Force ? 1 : 0)` (M1Table.cs). The body is
// a positional image, so the byte is there whether Force is true or false. The
// envelope is the writer's own: TableFixedWire.HeaderBytes + 4 layout length +
// SaveFixedLayoutBytes layout bytes, then one record of 8 hash bytes +
// SaveFixedBodyBytes (21).
//
// The writer pins offset 20 (defaults Force=false writes byte 0, Force=true
// writes byte 1); the reader side then loads the same envelope with body[20]
// planted to 0, 1 and 2. The law pins the normalization at exactly those three
// points: f(0)=false, f(1)=true, f(2)=true — 2 is the byte fix 2 names, nonzero
// so it must land the language's true, not 1 so a reader that compares == 1 (or
// stores the raw byte) is caught. The resave half completes it: the read-back
// value saves as byte 1, the normalized true, never the hostile byte. The path
// length before the bool is the trail that proves the planted byte moved
// nothing else.

class Program
{
    // Save.force rides at the end of the body (M1Table.cs SaveFixedWriteBody).
    const int BoolAt = 20;

    static int BodyStart => Schema.TableFixedWire.HeaderBytes + 4 + (int)Schema.SaveFixedLayoutBytes + 8;

    static int Main()
    {
        bool ok = true;

        ok &= WriterPinsOffset();
        ok &= ReaderNormalizes();
        ok &= HostileRoundTripsAsOne();
        ok &= NeighboursIntact();

        if (!ok)
        {
            return 1;
        }

        Console.WriteLine("bool byte != 0");
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

    // The envelope of one Save record with `force` set, everything else default.
    static byte[] Envelope(bool force)
    {
        var value = new Save { Force = force };
        byte[] buf = new byte[Schema.SaveFixedMeasure(1)];
        long saved = Schema.SaveFixedSave(value, buf);
        if (saved != buf.Length)
        {
            Console.Error.WriteLine("FAIL: save wrote {0}, envelope is {1}", saved, buf.Length);
            Environment.Exit(1);
        }
        return buf;
    }

    static long Load(byte[] data, out Save back, out TableReport report)
    {
        back = null;
        report = new TableReport();
        var values = new Save[1];
        var plan = new TableFixedEntry[1024];
        long n = Schema.SaveFixedLoad(values, data, plan, report);
        if (n == 1) { back = values[0]; }
        return n;
    }

    static bool WriterPinsOffset()
    {
        // defaults: Force = false, so the writer must write byte 0 at body 20 ...
        byte[] wire0 = Envelope(false);
        if (!Check(wire0[BodyStart + BoolAt] == 0,
                   "writer: Force=false writes byte 0 at body offset {0} (got 0x{1:x2})", BoolAt,
                   wire0[BodyStart + BoolAt]))
        {
            return false;
        }

        // ... and Force = true writes byte 1. Both pins together name the byte
        // the reader normalization below plants into.
        byte[] wire1 = Envelope(true);
        return Check(wire1[BodyStart + BoolAt] == 1,
                     "writer: Force=true writes byte 1 at body offset {0} (got 0x{1:x2})", BoolAt,
                     wire1[BodyStart + BoolAt]);
    }

    static bool ReaderNormalizes()
    {
        byte[] wire = Envelope(false);

        // f(0) = false: the zero byte is the language's false.
        wire[BodyStart + BoolAt] = 0;
        if (Load(wire, out Save back0, out TableReport r0) != 1) { Console.Error.WriteLine("FAIL: byte 0 load"); return false; }
        if (!Check(back0.Force == false, "reader: byte 0 lands false")) { return false; }

        // f(1) = true.
        wire[BodyStart + BoolAt] = 1;
        if (Load(wire, out Save back1, out TableReport r1) != 1) { Console.Error.WriteLine("FAIL: byte 1 load"); return false; }
        if (!Check(back1.Force == true, "reader: byte 1 lands true")) { return false; }

        // f(2) = true, the hostile byte of fix 2: nonzero, so the language's
        // true; NOT stored verbatim, and not a `== 1` compare either.
        wire[BodyStart + BoolAt] = 2;
        if (Load(wire, out Save back2, out TableReport r2) != 1) { Console.Error.WriteLine("FAIL: byte 2 load"); return false; }
        if (!Check(back2.Force == true, "reader: hostile 0x02 lands true, not verbatim")) { return false; }

        // normalization is silent: the hostile load moves no counter (the
        // counting half of the sentence is cs/R17, another card's item).
        return Check(r0.Unknown == 0 && r0.Clamped == 0 && !r0.Malformed && !r0.Refused &&
                     r2.Unknown == 0 && r2.Clamped == 0 && !r2.Malformed && !r2.Refused,
                     "reader: hostile loads move no counter");
    }

    static bool HostileRoundTripsAsOne()
    {
        byte[] wire = Envelope(false);
        wire[BodyStart + BoolAt] = 2;
        if (Load(wire, out Save back, out _) != 1) { Console.Error.WriteLine("FAIL: hostile load"); return false; }

        byte[] resave = new byte[Schema.SaveFixedMeasure(1)];
        long saved = Schema.SaveFixedSave(back, resave);
        if (saved != resave.Length) { Console.Error.WriteLine("FAIL: resave wrote {0}", saved); return false; }
        return Check(resave[BodyStart + BoolAt] == 1,
                     "round-trip: resave of the hostile-2 read writes byte 1 (got 0x{0:x2})",
                     resave[BodyStart + BoolAt]);
    }

    static bool NeighboursIntact()
    {
        // the planted byte is force's and only force's: the path length and
        // bytes before it read at their own defaults through the hostile load.
        byte[] wire = Envelope(false);
        wire[BodyStart + BoolAt] = 2;
        if (Load(wire, out Save back, out _) != 1) { Console.Error.WriteLine("FAIL: hostile load"); return false; }
        return Check(back.PathLength == 0 && back.Path[0] == 0 && back.Path[15] == 0,
                     "neighbours: PathLength == 0 and the path bytes unchanged through the hostile load");
    }
}
