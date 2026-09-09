// FixedFormChecks.cs — the C# twin of test/tables/fixedform_main.cpp
// Held by test: docs/SPEC-TABLES.md §3.4.

using System;
using System.Text;
using FX1 = Tblfx1;
using FX2 = Tblfx2;
using V1 = Tblv1;
using V2 = Tblv2;
using P1 = Tblp1;
using P3 = Tblp3;
using TD = Tabledemo;
using S2 = Scalardemo2;

static partial class Program
{
    static void TestFixedForm()
    {
        TestFixedFxCase();
        TestFixedVCase();
        TestFixedPCase();
        TestFixedWideBlobCase();
        TestFixedWide128Case();
        TestFixedFoldedArraysCase();
        TestFixedNegativeControl();
    }

    static void TestFixedFxCase()
    {
        // an FX1 record, every field off its default so nothing passes by accident
        FX1.FxRoot one = new FX1.FxRoot();
        FX1.Schema.TableReset(one);
        one.Keep = 4242u;
        one.Narrow = 40000;      // a uint16 value the widened read must reproduce
        one.Renamed = 321;
        one.Gone = 654;
        one.Nested.A = 111;
        one.Nested.B = 222;

        byte[] w1 = new byte[FX1.Schema.FxRootFixedMeasure(1)];
        Check(FX1.Schema.FxRootFixedSave(one, w1) == w1.Length, "FX1 save");

        // 1. SAME SCHEMA — the identity plan
        {
            FX1.FxRoot back = new FX1.FxRoot();
            FX1.TableReport r = new FX1.TableReport();
            FX1.TableFixedEntry[] plan = new FX1.TableFixedEntry[1024];
            long n = FX1.Schema.FxRootFixedLoad(back, w1, plan, r);
            Check(n == 1, "same schema: one record");
            Check(back.Keep == 4242u && back.Narrow == 40000 && back.Renamed == 321 && back.Gone == 654, "same schema: the scalars");
            Check(back.Nested.A == 111 && back.Nested.B == 222, "same schema: the nesting");
            Check(r.Unknown == 0 && r.KindMismatch == 0 && r.Widened == 0 && r.Clamped == 0 && !r.Malformed && !r.Refused,
                  "same schema: a clean read moves no counter");
        }

        // 2, 4, 5. FX2 READS FX1 — a plan compiled from FX1's block
        {
            FX2.FxRoot back = new FX2.FxRoot();
            FX2.TableReport r = new FX2.TableReport();
            FX2.TableFixedEntry[] plan = new FX2.TableFixedEntry[1024];
            long n = FX2.Schema.FxRootFixedLoad(back, w1, plan, r);
            Check(n == 1, "older writer: one record");
            Check(back.Keep == 4242u, "older writer: an unmoved field");
            Check(back.Narrow == 40000u, "WIDENED: uint16 into uint32, exactly");
            Check(r.Widened == 1, "WIDENED: one widened counts");
            Check(back.RenamedTo == 321, "RENAMED: `was =` keeps the wire id");
            Check(back.Added == 11, "MISSING: a field the writer does not carry takes its declared default");
            Check(back.Extra.X == 0 && back.Extra.Y == 0, "MISSING: a whole nested type takes its defaults");
            Check(back.Nested.A == 111 && back.Nested.B == 222, "older writer: the nesting");
            Check(r.Unknown == 1, "older writer: `gone` is the one field this reader cannot name");
            Check(r.KindMismatch == 0 && !r.Malformed && !r.Refused, "older writer: nothing else fired");
        }

        // 3. FX1 READS FX2 — an unknown field and an unknown NESTED TYPE
        {
            FX2.FxRoot two = new FX2.FxRoot();
            FX2.Schema.TableReset(two);
            two.Keep = 5150u;
            two.Narrow = 70000u;
            two.RenamedTo = 808;
            two.Added = 909;
            two.Nested.A = 33;
            two.Nested.B = 44;
            two.Extra.X = 55;
            two.Extra.Y = 66;
            byte[] w2 = new byte[FX2.Schema.FxRootFixedMeasure(1)];
            Check(FX2.Schema.FxRootFixedSave(two, w2) == w2.Length, "FX2 save");

            FX1.FxRoot back = new FX1.FxRoot();
            FX1.TableReport r = new FX1.TableReport();
            FX1.TableFixedEntry[] plan = new FX1.TableFixedEntry[1024];
            long n = FX1.Schema.FxRootFixedLoad(back, w2, plan, r);
            Check(n == 1, "newer writer: one record");
            Check(back.Keep == 5150u, "newer writer: an unmoved field lands past the unknowns");
            Check(back.Renamed == 808, "newer writer: `was =` reads the other way too");
            Check(back.Gone == 9, "newer writer: a field the writer dropped takes its declared default");
            Check(back.Nested.A == 33 && back.Nested.B == 44, "newer writer: the nesting lands past the unknown type");
            Check(r.Unknown == 2, "newer writer: two names this reader does not have");
            Check(r.KindMismatch == 1, "newer writer: uint32 into uint16 is a kind that moved, not a widening");
            Check(back.Narrow == 3, "newer writer: a narrowing leaves the declared default");
            Check(!r.Malformed && !r.Refused, "newer writer: no damage and no refusal");
        }
    }

    static void TestFixedVCase()
    {
        V1.Cfg one = new V1.Cfg();
        V1.Schema.TableReset(one);
        one.A = 42;
        byte[] hello = Encoding.UTF8.GetBytes("hello");
        Array.Copy(hello, one.Name, hello.Length);
        one.NameLength = 5;
        one.Grade = V1.Grade.Gold;         // V2 inserts Silver BEFORE Gold
        one.Effect.Type = V1.EffectType.Ward;
        one.Effect.Ward.Charge = 0.75f;    // V2 inserts hex BEFORE ward
        one.Tokens[(int)V1.Slot.Alpha] = 21;
        one.Tokens[(int)V1.Slot.Delta] = 24;    // V2 slides Beta and keeps Delta
        one.TierPresent = true;
        one.Tier = 77;

        byte[] w = new byte[V1.Schema.CfgFixedMeasure(1)];
        Check(V1.Schema.CfgFixedSave(one, w) == w.Length, "V1 save");

        V2.Cfg back = new V2.Cfg();
        V2.TableReport r = new V2.TableReport();
        V2.TableFixedEntry[] plan = new V2.TableFixedEntry[8192];
        long n = V2.Schema.CfgFixedLoad(back, w, plan, r);
        Check(n == 1, "V2 reads V1: one record");
        Check(back.Grade == V2.Grade.Gold, "ENUM: a variant inserted in the middle is remapped by NAME");
        Check(back.Effect.Type == V2.EffectType.Ward, "UNION: an arm inserted in the middle is remapped by NAME");
        Check(back.Effect.Ward.Charge == 0.75f, "UNION: the arm's payload lands");
        Check(Encoding.UTF8.GetString(back.Title, 0, back.TitleLength) == "hello" && back.TitleLength == 5, "RENAMED: title reads name's bytes");
        Check(back.Tokens[(int)V2.Slot.Alpha] == 21, "KEYED: a slot whose key did not move");
        Check(back.Tokens[(int)V2.Slot.Delta] == 24, "KEYED: a slot whose key SLID keeps its value");
        Check(back.Tokens[(int)V2.Slot.Sigma] == 0, "KEYED: a key the writer has no name for takes its default");
        Check(back.TierPresent && back.Tier == 77, "OPTIONAL: the present flag and the payload");
        Check(back.C, "MISSING: V2's own `c` takes its declared default");
        Check(back.A == 5.0f, "KIND MOVED: int32 -> float32 leaves the declared default");
        Check(!r.Malformed && !r.Refused, "V2 reads V1: no damage and no refusal");
    }

    static void TestFixedPCase()
    {
        P1.Chain one = new P1.Chain();
        P1.Schema.TableReset(one);
        byte[] chainBytes = Encoding.UTF8.GetBytes("chain");
        Array.Copy(chainBytes, one.Name, chainBytes.Length);
        one.NameLength = 5;
        one.Link.Value = 500;

        byte[] w = new byte[P1.Schema.ChainFixedMeasure(1)];
        Check(P1.Schema.ChainFixedSave(one, w) == w.Length, "P1 save");

        P3.Chain back = new P3.Chain();
        P3.TableReport r = new P3.TableReport();
        P3.TableFixedEntry[] plan = new P3.TableFixedEntry[1024];
        long n = P3.Schema.ChainFixedLoad(back, w, plan, r);
        Check(n == 1, "P3 reads P1: one record");
        Check(Encoding.UTF8.GetString(back.Name, 0, back.NameLength) == "chain", "P3 reads P1: the plain field lands");
        Check(r.KindMismatch == 1, "OPTIONAL vs VALUE is a reported kind on this form, never a silent reread");
    }

    static void TestFixedNegativeControl()
    {
        // THE WRONG PLAN. FX1's identity plan is correct for an FX1 record and
        // wrong for an FX2 one — FX2 inserts `added` between `renamed_to` and
        // `nested`, so every offset past it has moved. Running FX1's plan over
        // FX2's body is exactly the mistake the hash exists to prevent, and it
        // must come out WRONG.
        FX2.FxRoot two = new FX2.FxRoot();
        FX2.Schema.TableReset(two);
        two.Keep = 5150u;
        two.Narrow = 70000u;
        two.RenamedTo = 808;
        two.Added = 909;
        two.Nested.A = 33;
        two.Nested.B = 44;
        byte[] w2 = new byte[FX2.Schema.FxRootFixedMeasure(1)];
        FX2.Schema.FxRootFixedSave(two, w2);
        ReadOnlySpan<byte> body = w2.AsSpan(FX2.Schema.TableFixedWire.HeaderBytes + 4 + (int)FX2.Schema.FxRootFixedLayoutBytes + 8);

        FX1.FxRoot wrong = new FX1.FxRoot();
        FX1.Schema.TableReset(wrong);
        FX1.TableReport r = new FX1.TableReport();
        FX1.Schema.TableFixedWire.Run(FX1.Schema.FxRootFixedPlan, FX1.Schema.FxRootFixedSlots, body, wrong, r, ReadOnlySpan<byte>.Empty);
        bool intact = wrong.Nested.A == 33 && wrong.Nested.B == 44 && wrong.Renamed == 808;
        Check(!intact, "NEGATIVE CONTROL: the wrong plan must NOT reproduce the record");

        // and the loader never takes that path: the hash is what selects the plan
        FX1.FxRoot right = new FX1.FxRoot();
        FX1.TableReport r2 = new FX1.TableReport();
        FX1.TableFixedEntry[] plan = new FX1.TableFixedEntry[1024];
        long n = FX1.Schema.FxRootFixedLoad(right, w2, plan, r2);
        Check(n == 1 && right.Nested.A == 33 && right.Nested.B == 44,
              "NEGATIVE CONTROL: the loader compiles a plan from the block and gets it right");

        // A BLOCK THAT IS NOT A BLOCK IS REFUSED BY NAME, whole, and never damage.
        {
            byte[] broken = (byte[])w2.Clone();
            broken[FX1.Schema.TableFixedWire.HeaderBytes] ^= 0xFF; // the layout length at 16
            FX1.FxRoot v = new FX1.FxRoot();
            FX1.TableReport r3 = new FX1.TableReport();
            long bad = FX1.Schema.FxRootFixedLoad(v, broken, plan, r3);
            Check(bad < 0 && r3.Refused && r3.Reason == "layout_malformed", "REFUSED BY NAME: layout_malformed");
            Check(r3.Unknown == 0 && r3.KindMismatch == 0 && !r3.Malformed, "REFUSED BY NAME: a refusal moves no counter");
        }

        {
            byte[] unknownKind = (byte[])w2.Clone();
            unknownKind[FX1.Schema.TableFixedWire.HeaderBytes + 4 + 4 + 8] = 99; // byte 32: 20 header+len + 4 count + 8 id = kind
            FX1.FxRoot v = new FX1.FxRoot();
            FX1.TableReport rKind = new FX1.TableReport();
            long bad = FX1.Schema.FxRootFixedLoad(v, unknownKind, plan, rKind);
            Check(bad < 0 && rKind.Refused && rKind.Reason == "layout_malformed", "REFUSED BY NAME: unknown wire kind refuses as layout_malformed");
            Check(rKind.Unknown == 0 && rKind.KindMismatch == 0 && !rKind.Malformed, "REFUSED BY NAME: unknown kind refusal moves no counter");
        }

        // A FORM BYTE THIS READER DOES NOT CARRY IS A REFUSAL AND NEVER DAMAGE, AND
        // THE NAME SAYS WHICH DIRECTION (docs/SPEC-TABLES.md §3, §3.4).
        {
            var rows = new (byte form, string want, string what)[]
            {
                (1, "previous_form", "REFUSED BY NAME: previous_form for the VARIABLE form"),
                (2, "message_form_as_file", "REFUSED BY NAME: message_form_as_file for a batch"),
                (6, "newer_form", "REFUSED BY NAME: newer_form for a byte no form defines"),
            };
            foreach (var row in rows)
            {
                byte[] other = (byte[])w2.Clone();
                other[0] = row.form;
                FX1.FxRoot v = new FX1.FxRoot();
                FX1.TableReport r4 = new FX1.TableReport();
                long bad = FX1.Schema.FxRootFixedLoad(v, other, plan, r4);
                Check(bad < 0 && r4.Refused && r4.Reason == row.want, row.what);
                Check(!r4.Malformed, "REFUSED BY NAME: never damage");
                Check(r4.Unknown == 0 && r4.KindMismatch == 0 && r4.Widened == 0 && r4.Clamped == 0,
                      "REFUSED BY NAME: a form-byte refusal moves no counter");
            }
        }

        // THE HEADER NAMES THE LAYOUT ONCE (docs/SPEC-TABLES.md §3)
        {
            byte[] lying = (byte[])w2.Clone();
            lying[FX1.Schema.TableFixedWire.HashAt] ^= 0xFF;
            FX1.FxRoot v = new FX1.FxRoot();
            FX1.TableReport r6 = new FX1.TableReport();
            long bad = FX1.Schema.FxRootFixedLoad(v, lying, plan, r6);
            Check(bad < 0 && r6.Refused && r6.Reason == "layout_malformed",
                  "REFUSED BY NAME: a header hash that is not the layout's");
            Check(!r6.Malformed, "REFUSED BY NAME: never damage");
        }

        // RECORD HASH MISMATCH: a record stamped with a different layout refuses no_layout
        {
            byte[] corruptRecord = (byte[])w2.Clone();
            int recordHashOffset = FX1.Schema.TableFixedWire.HeaderBytes + 4 + (int)FX2.Schema.FxRootFixedLayoutBytes;
            corruptRecord[recordHashOffset] ^= 0xFF;
            FX1.FxRoot v = new FX1.FxRoot();
            FX1.TableReport rNoLayout = new FX1.TableReport();
            long bad = FX1.Schema.FxRootFixedLoad(v, corruptRecord, plan, rNoLayout);
            Check(bad < 0 && rNoLayout.Refused && rNoLayout.Reason == "no_layout",
                  "REFUSED BY NAME: record hash mismatch refuses no_layout");
            Check(!rNoLayout.Malformed, "REFUSED BY NAME: never damage");
        }

        // FIXEDLOAD WITH NULL REPORT: accepts null report cleanly and allocates nothing
        {
            FX1.FxRoot v = new FX1.FxRoot();
            long loaded = FX1.Schema.FxRootFixedLoad(v, w2, plan, null);
            Check(loaded == 1 && v.Nested.A == 33 && v.Nested.B == 44,
                  "LOAD WITH NULL REPORT: succeeds and populates record");
        }

        // A PLAN THAT DOES NOT FIT THE CALLER'S STORAGE IS A REFUSAL BY NAME, and
        // the codec allocates nothing to get around it.
        {
            FX1.FxRoot v = new FX1.FxRoot();
            FX1.TableReport r5 = new FX1.TableReport();
            FX1.TableFixedEntry[] tiny = new FX1.TableFixedEntry[1];
            long bad = FX1.Schema.FxRootFixedLoad(v, w2, tiny, r5);
            Check(bad < 0 && r5.Refused && r5.Reason == "plan_too_large", "REFUSED BY NAME: plan_too_large");
        }
    }

    static void TestFixedWideBlobCase()
    {
        Check(TD.Schema.WideBlobFixedHash == 0x8202b907d4ac81c4ul, "WideBlob fixed hash");
        Check(TD.Schema.WideBlobFixedPlan.Count == 4, "WideBlob 4 leaves");

        TD.WideBlob wb = new TD.WideBlob();
        byte[] labelBytes = Encoding.UTF8.GetBytes("hello wide blob");
        labelBytes.CopyTo(wb.Label, 0);
        wb.LabelLength = labelBytes.Length;

        byte[] payloadBytes = new byte[] { 1, 2, 3, 4, 5 };
        payloadBytes.CopyTo(wb.Payload, 0);
        wb.PayloadLength = payloadBytes.Length;

        wb.SamplesCount = 3;
        wb.Samples[0] = 100;
        wb.Samples[1] = 200;
        wb.Samples[2] = 300;

        byte[] buf = new byte[TD.Schema.WideBlobFixedMeasure(1)];
        long saved = TD.Schema.WideBlobFixedSave(wb, buf);
        Check(saved == buf.Length, "WideBlob save");

        TD.WideBlob back = new TD.WideBlob();
        TD.TableReport r = new TD.TableReport();
        TD.TableFixedEntry[] plan = new TD.TableFixedEntry[64];
        long loaded = TD.Schema.WideBlobFixedLoad(back, buf, plan, r);
        Check(loaded == 1, "WideBlob load one record");
        Check(r.Unknown == 0 && r.KindMismatch == 0 && r.Widened == 0 && r.Clamped == 0 && !r.Malformed && !r.Refused, "WideBlob clean read");
        Check(back.LabelLength == labelBytes.Length, "WideBlob label length");
        Check(back.PayloadLength == payloadBytes.Length, "WideBlob payload length");
        Check(back.Payload[0] == 1 && back.Payload[4] == 5, "WideBlob payload contents");
        Check(back.SamplesCount == 3, "WideBlob samples count");
        Check(back.Samples[0] == 100 && back.Samples[1] == 200 && back.Samples[2] == 300, "WideBlob samples contents");
    }

    static void TestFixedWide128Case()
    {
        S2.SimState s = new S2.SimState();
        S2.Schema.TableReset(s);
        s.Energy = -1234567890123456789L;
        s.Reach = -42;
        s.Mass = 100;
        s.SeedsCount = 1;
        s.Seeds[0] = 9999999999999999999UL;

        byte[] buf = new byte[S2.Schema.SimStateFixedMeasure(1)];
        long saved = S2.Schema.SimStateFixedSave(s, buf);
        Check(saved == buf.Length, "SimState save");

        S2.SimState back = new S2.SimState();
        S2.TableReport r = new S2.TableReport();
        S2.TableFixedEntry[] plan = new S2.TableFixedEntry[128];
        long loaded = S2.Schema.SimStateFixedLoad(back, buf, plan, r);
        Check(loaded == 1, "SimState load one record");
        Check(r.Unknown == 0 && r.KindMismatch == 0 && r.Widened == 0 && r.Clamped == 0 && !r.Malformed && !r.Refused, "SimState clean read");
        Check(back.Energy == -1234567890123456789L, "SimState energy int128 matches");
        Check(back.Reach == -42, "SimState reach fixed128 matches");
        Check(back.Mass == 100, "SimState mass ufixed128 matches");
        Check(back.SeedsCount == 1 && back.Seeds[0] == 9999999999999999999UL, "SimState seeds uint128 matches");
    }

    static void TestFixedFoldedArraysCase()
    {
        // 1. LoadoutConfig: 4 bytes (Grades [..4]Grade) and 3 bytes (Podium [3]Grade)
        {
            TD.LoadoutConfig lc = new TD.LoadoutConfig();
            TD.Schema.TableReset(lc);
            lc.GradesCount = 4;
            lc.Grades[0] = TD.Grade.Bronze;
            lc.Grades[1] = TD.Grade.Silver;
            lc.Grades[2] = TD.Grade.Gold;
            lc.Grades[3] = TD.Grade.Bronze;
            lc.Podium[0] = TD.Grade.Gold;
            lc.Podium[1] = TD.Grade.Silver;
            lc.Podium[2] = TD.Grade.Bronze;

            byte[] buf = new byte[TD.Schema.LoadoutConfigFixedMeasure(1)];
            long saved = TD.Schema.LoadoutConfigFixedSave(lc, buf);
            Check(saved == buf.Length, "LoadoutConfig save");

            TD.LoadoutConfig back = new TD.LoadoutConfig();
            TD.TableReport r = new TD.TableReport();
            TD.TableFixedEntry[] plan = new TD.TableFixedEntry[64];
            long loaded = TD.Schema.LoadoutConfigFixedLoad(back, buf, plan, r);
            Check(loaded == 1, "LoadoutConfig load one record");
            Check(r.Unknown == 0 && r.KindMismatch == 0 && r.Widened == 0 && r.Clamped == 0 && !r.Malformed && !r.Refused, "LoadoutConfig clean read");
            Check(back.GradesCount == 4, "LoadoutConfig grades count");
            Check(back.Grades[0] == TD.Grade.Bronze && back.Grades[1] == TD.Grade.Silver &&
                  back.Grades[2] == TD.Grade.Gold && back.Grades[3] == TD.Grade.Bronze,
                  "LoadoutConfig 4-byte folded enum array reproduced");
            Check(back.Podium[0] == TD.Grade.Gold && back.Podium[1] == TD.Grade.Silver && back.Podium[2] == TD.Grade.Bronze,
                  "LoadoutConfig 3-byte fixed enum array reproduced");
        }

        // 2. RangedSigned: 8 bytes (Edges [..4]int16)
        {
            TD.RangedSigned rs = new TD.RangedSigned();
            TD.Schema.TableReset(rs);
            rs.EdgesCount = 4;
            rs.Edges[0] = 11;
            rs.Edges[1] = 22;
            rs.Edges[2] = 33;
            rs.Edges[3] = 44;

            byte[] buf = new byte[TD.Schema.RangedSignedFixedMeasure(1)];
            long saved = TD.Schema.RangedSignedFixedSave(rs, buf);
            Check(saved == buf.Length, "RangedSigned save");

            TD.RangedSigned back = new TD.RangedSigned();
            TD.TableReport r = new TD.TableReport();
            TD.TableFixedEntry[] plan = new TD.TableFixedEntry[64];
            long loaded = TD.Schema.RangedSignedFixedLoad(back, buf, plan, r);
            Check(loaded == 1, "RangedSigned load one record");
            Check(r.Unknown == 0 && r.KindMismatch == 0 && r.Widened == 0 && r.Clamped == 0 && !r.Malformed && !r.Refused, "RangedSigned clean read");
            Check(back.EdgesCount == 4, "RangedSigned edges count");
            Check(back.Edges[0] == 11 && back.Edges[1] == 22 && back.Edges[2] == 33 && back.Edges[3] == 44,
                  "RangedSigned 8-byte folded int16 array reproduced");

            // Also test TableFixedWire.Copy dispatch fallback with SetBytes != null
            TD.RangedSigned backCopy = new TD.RangedSigned();
            TD.Schema.TableReset(backCopy);
            TD.TableFixedEntry[] copyPlan = (TD.TableFixedEntry[])TD.Schema.RangedSignedFixedPlan.Entries.Clone();
            for (int i = 0; i < copyPlan.Length; ++i)
            {
                if (copyPlan[i].Op == TD.Schema.TableFixedWire.Flat)
                {
                    copyPlan[i] = new TD.TableFixedEntry(
                        copyPlan[i].Src, copyPlan[i].Dst, copyPlan[i].Size,
                        copyPlan[i].Aux, copyPlan[i].Guard, TD.Schema.TableFixedWire.Copy,
                        copyPlan[i].Arg, 0);
                }
            }
            ReadOnlySpan<byte> recordBody = buf.AsSpan(TD.Schema.TableFixedWire.HeaderBytes + 4 + (int)TD.Schema.RangedSignedFixedLayoutBytes + 8);
            TD.Schema.TableFixedWire.Run(copyPlan, TD.Schema.RangedSignedFixedSlots, recordBody, backCopy, null, ReadOnlySpan<byte>.Empty);
            Check(backCopy.EdgesCount == 4 && backCopy.Edges[0] == 11 && backCopy.Edges[1] == 22 &&
                  backCopy.Edges[2] == 33 && backCopy.Edges[3] == 44,
                  "RangedSigned: Copy opcode dispatches to SetBytes for folded array");
        }

        // 3. ShipEntry: 16 bytes (Hardpoints [..4]int32)
        {
            TD.ShipEntry se = new TD.ShipEntry();
            TD.Schema.TableReset(se);
            se.HardpointsCount = 4;
            se.Hardpoints[0] = 101;
            se.Hardpoints[1] = 202;
            se.Hardpoints[2] = 303;
            se.Hardpoints[3] = 404;

            byte[] buf = new byte[TD.Schema.ShipEntryFixedMeasure(1)];
            long saved = TD.Schema.ShipEntryFixedSave(se, buf);
            Check(saved == buf.Length, "ShipEntry save");

            TD.ShipEntry back = new TD.ShipEntry();
            TD.TableReport r = new TD.TableReport();
            TD.TableFixedEntry[] plan = new TD.TableFixedEntry[64];
            long loaded = TD.Schema.ShipEntryFixedLoad(back, buf, plan, r);
            Check(loaded == 1, "ShipEntry load one record");
            Check(r.Unknown == 0 && r.KindMismatch == 0 && r.Widened == 0 && r.Clamped == 0 && !r.Malformed && !r.Refused, "ShipEntry clean read");
            Check(back.HardpointsCount == 4, "ShipEntry hardpoints count");
            Check(back.Hardpoints[0] == 101 && back.Hardpoints[1] == 202 && back.Hardpoints[2] == 303 && back.Hardpoints[3] == 404,
                  "ShipEntry 16-byte folded int32 array reproduced");
        }
    }
}
