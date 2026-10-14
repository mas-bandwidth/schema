using System;
using System.Buffers.Binary;
using Vold_cfloat_res_refine;

// cell3-cs-compressed-floats-valid-data — Compressed-float declarations stored
// as float32: valid-data write/read acceptance (cs leg).
//
// THE LAW (docs/SPEC-TABLES.md §3.4 THE RECORD, the row's contract):
//
//     | a COMPRESSED float | 4 — it rides as the float, not as a quantized
//     | index; this form is not optimized for bandwidth |
//
// and the proof it owes (docs/FIXED-FORM-ALGORITHM.md:1682, item 1 of §7's
// table): "Read a file and save it back; the bytes must be identical — a byte
// a port encodes differently is a byte that does not come back."
//
// The subject is the tree's own compressed-float declaration,
// test/tables/VOLD_cfloat_res_refine.schema:
//
//     fixed table CfloatResRefine
//     {
//         lead  uint32 = 1
//         aim   float32 | min = -1, max = 1, resolution = 0.1
//         trail uint32 = 2
//     }
//
// `aim` is the compressed-float declaration: a float32 carrying min, max and a
// resolution. In the FIXED form all three are DEFINITIONS — they live in the
// digest the hash stands for and nowhere in the record's bytes — so the value
// rides as the float32's own IEEE-754 bits. The variable form is the contrast
// the row's title refuses: there SPEC.md §4.3 makes a compressed float a
// QUANTIZED INDEX, the integer round((v - min) / res) at its step count.
//
// THE VECTOR, derived from the law. The tree carries no cs-leg corpus file for
// this row (build/fixedform-corpus is the C++ reference's dump, written by no
// gate this leg names), so the smallest file is constructed here, one record,
// aim = 0.123456f — a value INSIDE the declared range but OFF the 0.1 grid, so
// a port that folded the variable form's quantization into this one cannot
// land it:
//
//     form-3 file = 16-byte header + u32 layout length + layout + records
//
//     offset  bytes            content, by the law
//         0   03               the form byte, 3 (§3's registry)
//         1   00 x 7           reserved, zero on write
//         8   (hash u64 LE)    the vocabulary hash, Schema.CfloatResRefineFixedHash
//        16   48 00 00 00      the layout's byte length, u32 LE: 4 + 4 entries x 17
//        20   (72 bytes)       the layout; `aim`'s entry is kind 10 (f32) with
//                              size 4 — THE FLOAT'S OWN WIDTH, never a step count
//        92   (hash u64 LE)    every record is stamped with the hash
//       100   01 00 00 00      lead, u32 LE, declared order, declared width
//       104   80 d6 fc 3d      aim, the IEEE-754 bits of 0.123456f, u32 LE
//       108   02 00 00 00      trail, u32 LE
//
//     total 112 = 16 + 4 + 72 + (8 + 12), one record of CfloatResRefineFixedRecordBytes.
//
// The aim bits, worked: 0.123456 in binary is 1.975296... x 2^-4, so the
// float32 is sign 0, exponent 123 (0x7B, the bias-127 spelling of -4),
// mantissa the fraction 0.975296... x 2^23 = 0x7CD680 — the bits
// 0 01111011 1111100110101101000000, packed 0x3DFCD680, little-endian
// 80 d6 fc 3d. The QUANTIZED INDEX the variable form would put in those four
// bytes instead is round((0.123456 + 1) / 0.1) = 11 = 0x0000000B, bytes
// 0b 00 00 00 — the exact spelling this row exists to refuse.
//
// Run: dotnet run --project test/conformance/cs/rows/CompressedFloatsValidData/CompressedFloatsValidData.csproj
// Exit 0 green, exit 1 red; one printed line per assertion.

static class Program
{
    // The vector's one hand-derived byte constant: the IEEE-754 bits of the
    // off-grid value, and the index the law refuses.
    const int OffGridBits = unchecked((int)0x3DFCD680);
    const int QuantizedIndex = 11;

    static int Main()
    {
        bool ok = true;

        ok &= CheckLawConstants();
        ok &= CheckWriteVector();
        ok &= CheckReadAcceptance();
        ok &= CheckRoundTripIdentity();
        ok &= CheckValidDataSet();

        if (!ok)
        {
            return 1;
        }

        Console.WriteLine("compressed-float declarations stored as float32: valid-data write/read acceptance");
        return 0;
    }

    static void Fail(string what)
    {
        Console.Error.WriteLine("FAIL: " + what);
    }

    // The clause "stored as float32" as the wire itself states it: the plan
    // lands aim as ONE 4-BYTE copy at body offset 4 — the compressed float
    // costs the FLOAT'S OWN WIDTH, never ceil((max-min)/res) = 20 steps and
    // never any index width — the three entries tile lead(4)+aim(4)+trail(4),
    // and a second record sits one hash(8)+body(12) stride further on.
    // The constants are read through the runtime (a plan array, Measure) so
    // the assertions are facts the wire answers, not compile-time folds.
    static bool CheckLawConstants()
    {
        bool ok = true;

        TableFixedPlan plan = Schema.CfloatResRefineFixedPlan;
        if (plan.Count != 3)
        {
            Fail("the identity plan does not carry one entry per field: count=" + plan.Count);
            ok = false;
        }
        else
        {
            // The plan tiles the body without gaps: src 0 size 4, src 4 size 4,
            // src 8 size 4 — the compressed float's leg of it is entry 1.
            uint covered = 0;
            for (int i = 0; i < plan.Count; ++i)
            {
                if (plan.Entries[i].Src != covered)
                {
                    Fail("plan[" + i + "] leaves a gap: src=" + plan.Entries[i].Src + " want " + covered);
                    ok = false;
                }
                covered += plan.Entries[i].Size;
            }
            if (covered != 12)
            {
                Fail("the plan covers " + covered + " bytes — the body is lead(4)+aim(4)+trail(4) = 12, the float's own width");
                ok = false;
            }

            TableFixedEntry aim = plan.Entries[1];
            if (aim.Src != 4 || aim.Size != 4 || aim.Dst != 1)
            {
                Fail("the aim entry is not one 4-byte copy at body offset 4 into slot 1: src=" + aim.Src
                    + " size=" + aim.Size + " dst=" + aim.Dst);
                ok = false;
            }
        }

        // The file's own arithmetic, from Measure: 16 + 4 + 72 + 20 = 112 for
        // one record, and the second record one 20-byte stride on.
        if (Schema.CfloatResRefineFixedMeasure(1) != 112)
        {
            Fail("Measure(1)=" + Schema.CfloatResRefineFixedMeasure(1) + " — the law's file is 16 + 4 + 72 + (8 + 12) = 112");
            ok = false;
        }
        if (Schema.CfloatResRefineFixedMeasure(2) - Schema.CfloatResRefineFixedMeasure(1) != 20)
        {
            Fail("the record stride is not hash(8) + body(12) = 20: "
                + (Schema.CfloatResRefineFixedMeasure(2) - Schema.CfloatResRefineFixedMeasure(1)));
            ok = false;
        }

        if (ok)
        {
            Console.WriteLine("OK: the declaration costs its float32 width — the plan's aim entry is 4 bytes at src 4, the record 20");
        }
        return ok;
    }

    // WRITE acceptance: valid data in, the law's bytes out. The aim byte of the
    // record is compared against the hand-derived IEEE-754 bits, and against
    // the quantized index it must never be.
    static bool CheckWriteVector()
    {
        bool ok = true;

        CfloatResRefine v = new CfloatResRefine();
        v.Lead = 1;
        v.Aim = 0.123456f;   // inside [-1, 1], off the 0.1 grid
        v.Trail = 2;

        long want = 112;   // 16 + 4 + 72 + (8 + 12), the derivation above
        if (Schema.CfloatResRefineFixedMeasure(1) != want)
        {
            Fail("Measure(1)=" + Schema.CfloatResRefineFixedMeasure(1) + " want " + want);
            ok = false;
        }

        byte[] file = new byte[(int)want];
        long n = Schema.CfloatResRefineFixedSave(v, file);
        if (n != want)
        {
            Fail("Save wrote " + n + " bytes, want " + want);
            ok = false;
        }

        if (file[0] != 3)
        {
            Fail("the form byte is " + file[0] + ", want 3");
            ok = false;
        }
        for (int i = 1; i < 8; ++i)
        {
            if (file[i] != 0)
            {
                Fail("reserved header byte " + i + " is " + file[i] + ", want 0");
                ok = false;
            }
        }
        if (BinaryPrimitives.ReadUInt64LittleEndian(file.AsSpan(8)) != Schema.CfloatResRefineFixedHash)
        {
            Fail("the header does not carry the vocabulary hash");
            ok = false;
        }
        if (BinaryPrimitives.ReadUInt32LittleEndian(file.AsSpan(16)) != 72u)
        {
            Fail("the layout length is not 72 (4 + 4 entries x 17): " + BinaryPrimitives.ReadUInt32LittleEndian(file.AsSpan(16)));
            ok = false;
        }

        // record 0: the hash again, then the body in declared order — the body
        // starts record+8, past the record's own 8-byte hash.
        int record = 16 + 4 + 72;
        if (BinaryPrimitives.ReadUInt64LittleEndian(file.AsSpan(record)) != Schema.CfloatResRefineFixedHash)
        {
            Fail("the record is not stamped with the hash");
            ok = false;
        }
        if (BinaryPrimitives.ReadUInt32LittleEndian(file.AsSpan(record + 8)) != 1u)
        {
            Fail("lead is not 01 00 00 00: " + BinaryPrimitives.ReadUInt32LittleEndian(file.AsSpan(record + 8)));
            ok = false;
        }
        if (BinaryPrimitives.ReadUInt32LittleEndian(file.AsSpan(record + 16)) != 2u)
        {
            Fail("trail is not 02 00 00 00: " + BinaryPrimitives.ReadUInt32LittleEndian(file.AsSpan(record + 16)));
            ok = false;
        }

        int bits = BitConverter.SingleToInt32Bits(v.Aim);
        int wire = BinaryPrimitives.ReadInt32LittleEndian(file.AsSpan(record + 12));
        if (bits != OffGridBits)
        {
            Fail("the derivation and the runtime disagree on 0.123456f: bits=0x" + bits.ToString("x8"));
            ok = false;
        }
        if (wire != OffGridBits)
        {
            Fail("the aim byte is not the float32's IEEE-754 bits: wire=0x" + wire.ToString("x8")
                + " want 0x" + OffGridBits.ToString("x8") + " — a compressed float rides as the float (SPEC-TABLES §3.4)");
            ok = false;
        }
        if (wire == QuantizedIndex)
        {
            Fail("the aim byte is the quantized index 11 — the VARIABLE form's rule (SPEC.md §4.3), refused here");
            ok = false;
        }

        if (ok)
        {
            Console.WriteLine("OK: written aim byte is 0x" + wire.ToString("x8") + ", the float's own IEEE-754 bits, never the index");
        }
        return ok;
    }

    // READ acceptance: the file the writer wrote is a clean read — no refusal,
    // no counter, the off-grid value lands BIT-EXACT. A reader that requantized
    // onto the 0.1 grid would land 0x3DCCCCCD (0.1f) or 0x3E4CCCCD (0.2f);
    // a reader that dequantized an index would land -1 + 11*0.1.
    static bool CheckReadAcceptance()
    {
        bool ok = true;

        CfloatResRefine v = new CfloatResRefine();
        v.Lead = 1;
        v.Aim = 0.123456f;
        v.Trail = 2;

        byte[] file = new byte[(int)Schema.CfloatResRefineFixedMeasure(1)];
        Schema.CfloatResRefineFixedSave(v, file);

        CfloatResRefine[] back = new CfloatResRefine[1];
        TableReport r = new TableReport();
        TableFixedEntry[] plan = new TableFixedEntry[8192];
        long n = Schema.CfloatResRefineFixedLoad(back, file, plan, r);

        if (n != 1)
        {
            Fail("the reader did not accept one record: n=" + n + " reason=" + r.Reason);
            ok = false;
        }
        if (r.Refused || r.Malformed || r.Reason != null)
        {
            Fail("valid data is not a refusal: reason=" + r.Reason + " malformed=" + r.Malformed);
            ok = false;
        }
        if (r.Unknown != 0 || r.KindMismatch != 0 || r.Widened != 0 || r.Clamped != 0 || r.Duplicate != 0)
        {
            Fail("a counter moved on valid data: unknown=" + r.Unknown + " kind=" + r.KindMismatch
                + " widened=" + r.Widened + " clamped=" + r.Clamped + " duplicate=" + r.Duplicate);
            ok = false;
        }
        if (back[0].Lead != 1u || back[0].Trail != 2u)
        {
            Fail("a neighbour moved: lead=" + back[0].Lead + " trail=" + back[0].Trail);
            ok = false;
        }
        if (BitConverter.SingleToInt32Bits(back[0].Aim) != OffGridBits)
        {
            Fail("the off-grid value did not land bit-exact: bits=0x" + BitConverter.SingleToInt32Bits(back[0].Aim).ToString("x8")
                + " want 0x" + OffGridBits.ToString("x8") + " — nothing requantizes, the resolution is a definition");
            ok = false;
        }

        if (ok)
        {
            Console.WriteLine("OK: the read accepted the file, no counter moved, aim landed bit-exact off the grid");
        }
        return ok;
    }

    // The law's proof item (FIXED-FORM-ALGORITHM §7 #1): read a file and save
    // it back; the bytes must be identical.
    static bool CheckRoundTripIdentity()
    {
        CfloatResRefine v = new CfloatResRefine();
        v.Lead = 1;
        v.Aim = 0.123456f;
        v.Trail = 2;

        byte[] first = new byte[(int)Schema.CfloatResRefineFixedMeasure(1)];
        Schema.CfloatResRefineFixedSave(v, first);

        CfloatResRefine[] back = new CfloatResRefine[1];
        Schema.CfloatResRefineFixedLoad(back, first, new TableFixedEntry[8192], new TableReport());

        byte[] again = new byte[first.Length];
        long n = Schema.CfloatResRefineFixedSave(back, again);
        if (n != first.Length || !first.AsSpan().SequenceEqual(again))
        {
            Fail("read-then-save is not byte-identical: wrote " + n + " of " + first.Length);
            return false;
        }

        Console.WriteLine("OK: read a file and save it back — " + first.Length + " bytes identical");
        return true;
    }

    // The valid-data set: the range's two ends, the declared default, a value
    // ON the 0.1 grid, and the off-grid probe — five records in one file, every
    // aim byte its own IEEE-754 bits, every read bit-exact, no clamp at either
    // end (the bounds pass is exact: -1 and 1 are IN range), byte identity over
    // the whole file.
    static bool CheckValidDataSet()
    {
        bool ok = true;

        float[] values = new float[] { -1.0f, 0.0f, 1.0f, 0.1f, 0.123456f };
        int[] wantBits = new int[]
        {
            unchecked((int)0xBF800000), unchecked((int)0x00000000), unchecked((int)0x3F800000),
            unchecked((int)0x3DCCCCCD), OffGridBits,
        };

        CfloatResRefine[] vs = new CfloatResRefine[values.Length];
        for (int k = 0; k < values.Length; ++k)
        {
            vs[k] = new CfloatResRefine();
            vs[k].Lead = 1;
            vs[k].Aim = values[k];
            vs[k].Trail = 2;
        }

        int record = 16 + 4 + 72;              // the derivation: past header, length and layout
        int stride = 20;                       // hash(8) + body(12), per record

        byte[] file = new byte[(int)Schema.CfloatResRefineFixedMeasure(values.Length)];
        if (Schema.CfloatResRefineFixedMeasure(values.Length) != 192)
        {
            Fail("Measure(5)=" + Schema.CfloatResRefineFixedMeasure(5) + " — five records are 16 + 4 + 72 + 5*20 = 192");
            ok = false;
        }
        long n = Schema.CfloatResRefineFixedSave(vs, file);
        if (n != file.Length)
        {
            Fail("Save wrote " + n + " bytes, want " + file.Length);
            ok = false;
        }

        for (int k = 0; k < values.Length; ++k)
        {
            int at = record + k * stride + 8;   // past the record's hash
            if (BinaryPrimitives.ReadUInt32LittleEndian(file.AsSpan(at)) != 1u
                || BinaryPrimitives.ReadUInt32LittleEndian(file.AsSpan(at + 8)) != 2u)
            {
                Fail("record " + k + ": a neighbour moved");
                ok = false;
            }
            int wire = BinaryPrimitives.ReadInt32LittleEndian(file.AsSpan(at + 4));
            if (wire != wantBits[k])
            {
                Fail("record " + k + " (" + values[k] + "): the aim byte is 0x" + wire.ToString("x8")
                    + ", want the IEEE-754 bits 0x" + wantBits[k].ToString("x8"));
                ok = false;
            }
        }

        CfloatResRefine[] back = new CfloatResRefine[values.Length];
        TableReport r = new TableReport();
        long rn = Schema.CfloatResRefineFixedLoad(back, file, new TableFixedEntry[8192], r);
        if (rn != values.Length)
        {
            Fail("the reader took " + rn + " records, want " + values.Length + " (reason=" + r.Reason + ")");
            ok = false;
        }
        if (r.Refused || r.Malformed || r.Reason != null)
        {
            Fail("valid data is not a refusal: reason=" + r.Reason + " malformed=" + r.Malformed);
            ok = false;
        }
        if (r.Unknown != 0 || r.KindMismatch != 0 || r.Widened != 0 || r.Clamped != 0 || r.Duplicate != 0)
        {
            Fail("a counter moved on valid data: unknown=" + r.Unknown + " kind=" + r.KindMismatch
                + " widened=" + r.Widened + " clamped=" + r.Clamped + " duplicate=" + r.Duplicate);
            ok = false;
        }
        for (int k = 0; k < values.Length; ++k)
        {
            if (back[k] == null || BitConverter.SingleToInt32Bits(back[k].Aim) != wantBits[k])
            {
                Fail("record " + k + " (" + values[k] + ") did not land bit-exact: bits="
                    + (back[k] == null ? "null" : "0x" + BitConverter.SingleToInt32Bits(back[k].Aim).ToString("x8")));
                ok = false;
            }
        }

        byte[] again = new byte[file.Length];
        Schema.CfloatResRefineFixedSave(back, again);
        if (!file.AsSpan().SequenceEqual(again))
        {
            Fail("read-then-save is not byte-identical over the " + values.Length + "-record file");
            ok = false;
        }

        if (ok)
        {
            Console.WriteLine("OK: min, default, max, a grid point and the off-grid probe — all five land bit-exact, no clamp, bytes identical");
        }
        return ok;
    }
}
