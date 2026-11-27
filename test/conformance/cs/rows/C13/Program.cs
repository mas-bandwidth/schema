using System;
using Tblp3;

// cell-cs-c13 — a present flag byte other than 0 or 1 refuses wire_damaged
//
// The law (docs/FIXED-FORM-ALGORITHM.md §4.5, the scatter table's present
// row, ruling 2026-11-03; SPEC-TABLES §4, §6.5): "a byte of `0` or `1` lands
// as the language's own false or true; any other byte refuses
// `wire_damaged`", and "A present byte of `0` makes the payload under it
// DEAD, and a non-zero dead byte refuses `wire_damaged` in the CHECK pass
// (§5.3 step 11)". REFUSE is total (§5.3): nothing read from the file lands,
// and every row handed in is back at its declared defaults (SPEC-TABLES §6.5). This row once pinned the retired
// rule (0x03 normalized to present, a dead payload riding); go/C13 is its twin.
//
// Vector: fixed table Chain (test/tables/P3.schema, package tblp3), field
// `link ?Link`. ChainFixedWriteBody writes the name length at body 0..3, the
// name bytes at 4..19, the present flag at body offset 20, and the Link payload
// after it (value int32 at 21). The envelope is the writer's own:
// TableFixedWire.HeaderBytes + 4 layout length + ChainFixedLayoutBytes layout
// bytes, then one record of 8 hash bytes + ChainFixedBodyBytes (37).

class Program
{
    const int PresentAt = 20;
    const int LinkValueAt = 21;

    static int BodyStart => Schema.TableFixedWire.HeaderBytes + 4 + (int)Schema.ChainFixedLayoutBytes + 8;

    static int Main()
    {
        bool ok = true;

        ok &= WriterPinsOffset();
        ok &= ReaderLandsZeroAndOne();
        ok &= DeadPayloadRefuses();
        ok &= HostileFlagRefuses();

        if (!ok)
        {
            return 1;
        }

        Console.WriteLine("a present flag byte other than 0 or 1 refuses wire_damaged");
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

    static long Load(byte[] data, Chain held, out Chain landed, out TableReport report)
    {
        report = new TableReport();
        var values = new Chain[] { held };
        var plan = new TableFixedEntry[1024];
        long n = Schema.ChainFixedLoad(values, data, plan, report);
        landed = values[0];
        return n;
    }

    // Storage holding values that are neither the file's nor the declared
    // defaults, so a refusal that leaves anything else is seen.
    static Chain Held()
    {
        var held = new Chain { NameLength = 2, LinkPresent = true };
        held.Link.Value = 99;
        held.Name[0] = (byte)'h'; held.Name[1] = (byte)'i';
        return held;
    }

    static bool AtDefaults(Chain landed)
    {
        var d = new Chain();
        return landed != null && landed.NameLength == d.NameLength && landed.Name.AsSpan().SequenceEqual(d.Name) &&
               landed.LinkPresent == d.LinkPresent && landed.Link.Value == d.Link.Value;
    }

    static bool WriterPinsOffset()
    {
        byte[] wire0 = Envelope(false);
        if (!Check(wire0[BodyStart + PresentAt] == 0 &&
                   System.Buffers.Binary.BinaryPrimitives.ReadInt32LittleEndian(wire0.AsSpan(BodyStart + LinkValueAt)) == 0,
                   "writer: an absent link writes byte 0 at body offset {0} and a zero payload (got 0x{1:x2})", PresentAt,
                   wire0[BodyStart + PresentAt]))
        {
            return false;
        }
        byte[] wire1 = Envelope(true);
        return Check(wire1[BodyStart + PresentAt] == 1 &&
                     System.Buffers.Binary.BinaryPrimitives.ReadInt32LittleEndian(
                         wire1.AsSpan(BodyStart + LinkValueAt)) == 7,
                     "writer: a present link writes byte 1 at body offset {0} and payload 7 at {1}", PresentAt, LinkValueAt);
    }

    static bool ReaderLandsZeroAndOne()
    {
        long n0 = Load(Envelope(false), new Chain { LinkPresent = true }, out Chain back0, out TableReport r0);
        if (!Check(n0 == 1 && back0.LinkPresent == false && back0.Link.Value == 0 && !r0.Damaged && !r0.Refused,
                   "reader: byte 0 over a zero payload lands absent (n={0})", n0)) { return false; }

        long n1 = Load(Envelope(true), new Chain(), out Chain back1, out TableReport r1);
        return Check(n1 == 1 && back1.LinkPresent == true && back1.Link.Value == 7 && !r1.Damaged && !r1.Refused,
                     "reader: byte 1 lands present and the payload behind it rides (n={0}, value={1})", n1, back1.Link.Value);
    }

    static bool DeadPayloadRefuses()
    {
        // byte 0 over the payload 7 a present writer left: the payload is DEAD
        // and not zero, so the CHECK pass refuses the whole load.
        byte[] wire = Envelope(true);
        wire[BodyStart + PresentAt] = 0;
        Chain held = Held();
        long n = Load(wire, held, out Chain landed, out TableReport r);
        return Check(n == -1 && r.Refused && r.Reason == "wire_damaged",
                     "reader: a non-zero payload under present byte 0 refuses (n={0}, reason={1})", n, r.Reason ?? "null")
            && Check(AtDefaults(landed), "reader: after the refused dead-payload load the row holds its declared defaults");
    }

    static bool HostileFlagRefuses()
    {
        bool ok = true;
        foreach (byte hostile in new byte[] { 0x02, 0x03, 0xFF })
        {
            byte[] wire = Envelope(true);
            wire[BodyStart + PresentAt] = hostile;
            Chain held = Held();
            long n = Load(wire, held, out Chain landed, out TableReport r);
            ok &= Check(n == -1 && r.Refused && r.Reason == "wire_damaged",
                        "reader: present byte 0x{0:x2} refuses (n={1}, damaged={2}, refused={3}, reason={4})",
                        hostile, n, r.Damaged, r.Refused, r.Reason ?? "null");
            ok &= Check(AtDefaults(landed), "reader: after the refused load of 0x{0:x2} the row holds its declared defaults", hostile);
        }
        return ok;
    }
}
