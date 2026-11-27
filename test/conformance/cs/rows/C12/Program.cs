using System;
using Tblm1;

// cell-cs-c12 — a bool byte other than 0 or 1 refuses wire_damaged
//
// The law (docs/FIXED-FORM-ALGORITHM.md §4.5, the scatter table's bool row,
// ruling 2026-11-03; SPEC-TABLES §4, §6.5): "a byte of `0` or `1` lands as
// the language's own false or true; any other byte refuses `wire_damaged`: no
// writer emits one, and `0x02` is neither stored verbatim nor normalized."
// The refusal is the CHECK pass's (§5.3 step 11), and REFUSE is total (§5.3):
// nothing read from the file lands, and every row handed in is back at its
// declared defaults (SPEC-TABLES §6.5, the caller's values after a refusal). This row once
// pinned the retired rule (0x02 normalized to true); go/C12 is its twin.
//
// Vector: fixed table Save (test/tables/M1.schema, package tblm1), field `force
// bool`, the body's last byte after `path string(16)`. SaveFixedWriteBody writes
// the path length at body 0..3, the path bytes at 4..19, and the bool at body
// offset 20. The envelope is the writer's own: TableFixedWire.HeaderBytes + 4
// layout length + SaveFixedLayoutBytes layout bytes, then one record of 8 hash
// bytes + SaveFixedBodyBytes (21).
//
// The writer pins offset 20 (Force=false writes byte 0, Force=true byte 1); the
// reader then loads the same envelope with body[20] planted to 0, 1 and 2:
// f(0)=false, f(1)=true, and 2 refuses, n == -1 with the report saying so and
// the row handed in back at its declared defaults.

class Program
{
    // Save.force rides at the end of the body (M1Table.cs SaveFixedWriteBody).
    const int BoolAt = 20;

    static int BodyStart => Schema.TableFixedWire.HeaderBytes + 4 + (int)Schema.SaveFixedLayoutBytes + 8;

    static int Main()
    {
        bool ok = true;

        ok &= WriterPinsOffset();
        ok &= ReaderLandsZeroAndOne();
        ok &= HostileByteRefuses();

        if (!ok)
        {
            return 1;
        }

        Console.WriteLine("a bool byte other than 0 or 1 refuses wire_damaged");
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

    // One load into storage that already holds `held`, so a refusal that leaves
    // anything but the declared defaults is seen.
    static long Load(byte[] data, Save held, out Save landed, out TableReport report)
    {
        report = new TableReport();
        var values = new Save[] { held };
        var plan = new TableFixedEntry[1024];
        long n = Schema.SaveFixedLoad(values, data, plan, report);
        landed = values[0];
        return n;
    }

    static bool WriterPinsOffset()
    {
        byte[] wire0 = Envelope(false);
        if (!Check(wire0[BodyStart + BoolAt] == 0,
                   "writer: Force=false writes byte 0 at body offset {0} (got 0x{1:x2})", BoolAt,
                   wire0[BodyStart + BoolAt]))
        {
            return false;
        }
        byte[] wire1 = Envelope(true);
        return Check(wire1[BodyStart + BoolAt] == 1,
                     "writer: Force=true writes byte 1 at body offset {0} (got 0x{1:x2})", BoolAt,
                     wire1[BodyStart + BoolAt]);
    }

    static bool ReaderLandsZeroAndOne()
    {
        byte[] wire = Envelope(false);

        wire[BodyStart + BoolAt] = 0;
        long n0 = Load(wire, new Save { Force = true }, out Save back0, out TableReport r0);
        if (!Check(n0 == 1 && back0.Force == false && !r0.Damaged && !r0.Refused,
                   "reader: byte 0 lands false (n={0}, force={1})", n0, back0.Force)) { return false; }

        wire[BodyStart + BoolAt] = 1;
        long n1 = Load(wire, new Save(), out Save back1, out TableReport r1);
        return Check(n1 == 1 && back1.Force == true && !r1.Damaged && !r1.Refused,
                     "reader: byte 1 lands true (n={0}, force={1})", n1, back1.Force);
    }

    static bool HostileByteRefuses()
    {
        bool ok = true;
        foreach (byte hostile in new byte[] { 0x02, 0xFF })
        {
            byte[] wire = Envelope(true);
            wire[BodyStart + BoolAt] = hostile;
            // the storage holds values that are neither the file's nor the
            // declared defaults: path "abc", force true. After the refusal the
            // row is back at its declared defaults.
            var held = new Save { Force = true, PathLength = 3 };
            held.Path[0] = (byte)'a'; held.Path[1] = (byte)'b'; held.Path[2] = (byte)'c';
            long n = Load(wire, held, out Save landed, out TableReport r);
            ok &= Check(n == -1 && r.Refused && r.Reason == "wire_damaged",
                        "reader: bool byte 0x{0:x2} refuses (n={1}, damaged={2}, refused={3}, reason={4})",
                        hostile, n, r.Damaged, r.Refused, r.Reason ?? "null");
            var defaults = new Save();
            ok &= Check(landed != null && landed.Force == defaults.Force && landed.PathLength == defaults.PathLength &&
                        landed.Path.AsSpan().SequenceEqual(defaults.Path),
                        "reader: after the refused load of 0x{0:x2} the row holds its declared defaults", hostile);
        }
        return ok;
    }
}
