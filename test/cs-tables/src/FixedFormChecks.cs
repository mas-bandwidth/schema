// FixedFormChecks.cs — the C# twin of test/tables/fixedform_main.cpp
// Held by test: docs/SPEC-TABLES.md §3.4.

using System;
using System.Buffers.Binary;
using System.Text;
using FX1 = Tblfx1;
using FX2 = Tblfx2;
using V1 = Tblv1;
using V2 = Tblv2;
using P1 = Tblp1;
using P3 = Tblp3;
using TD = Tabledemo;
using S2 = Scalardemo2;
using UT = Tblut;

static partial class Program
{
    // THE SKIP COUNT §5.9 #23 asks a suite with no skip verb to print at the
    // end, beside the printed line at each retired test's call site.
    static int skipped;

    static void TestFixedForm()
    {
        TestFixedFxCase();
        // SKIPPED BY NAME, NOT DELETED (§5.6, §5.9 #23). Both cases are ENTIRELY
        // a forward/compiled read across two generations with no lineage between
        // them — V2 reading a V1 file, P3 reading a P1 file — which §5 retires: the
        // answer is layout_newer, by name, before a plan exists. THE COVERAGE MOVES
        // TO THE LINEAGE HARNESS: every row they assert (a variant inserted mid-list,
        // an arm inserted mid-list, a renamed field, a keyed slot that slid, an
        // optional added) is a row of §5.7 with its own two columns, read against
        // the C++ reference's own bytes rather than against a file this test wrote.
        Console.WriteLine("SKIPPED: TestFixedVCase — §5.6 retires the compiled read of another generation's layout; the coverage moves to the lineage harness");
        Console.WriteLine("SKIPPED: TestFixedPCase — §5.6 retires the compiled read of another generation's layout; the coverage moves to the lineage harness");
        skipped += 2;
        TestFixedWide128Case();
        TestFixedSignedLowEndCase();
        TestFixedFoldedArraysCase();
        TestFixedArmTextCase();
        TestFixedSlackCase();
        TestFixedNegativeControl();
        // SKIPPED BY NAME, NOT DELETED (docs/FIXED-FORM-ALGORITHM.md §5.6, and
        // §5.9 #23 for a suite whose verb is a printed line): TestFixedLayoutValidation
        // asserts the RUN-TIME WALK of a stranger's layout, which §5 retires —
        // a layout arriving on the wire is no longer walked at all, so §1.1's
        // seven rules cannot fire at read time and a malformation under a KNOWN
        // hash is ONE name, layout_malformed. THE COVERAGE IS OWED BY THE LOCK'S
        // VALIDATION of what it records, and by the oracle's validation of the
        // corpus. The function stays in the tree and stays compiling: a deleted
        // test is a coverage claim nobody can audit.
        Console.WriteLine("SKIPPED: TestFixedLayoutValidation — §5.6 retires the run-time walk of a stranger's layout; §1.1's seven rules move to the LOCK's validation of what it records");
        skipped++;
        TestFixedHostileBoolCase();
        TestFixedGuardComparedAtArgW();
        TestFixedAbsentOptionalCase();
        if (skipped > 0)
        {
            Console.WriteLine("cs fixed form: " + skipped + " test(s) skipped by name, §5.6");
        }
    }

    sealed class ArgWProbe { public byte V; }

    // THE GUARD IS COMPARED AT ArgW BYTES, NEVER AS A PREFIX.
    // compiler/fixedguardwidth_test.go holds the IR stamp; this is the run loop.
    // A two-byte tag 0x0101 whose low byte is 1 is not arm 1. ONE PATH: this is
    // TableFixedWire.Run, the same loop identity and compiled both take.
    static void TestFixedGuardComparedAtArgW()
    {
        byte Run(byte argw, uint srcOff, byte[] src)
        {
            FX1.TableFixedSlot<ArgWProbe>[] slots = new FX1.TableFixedSlot<ArgWProbe>[]
            {
                new FX1.TableFixedSlot<ArgWProbe>(setRaw: (t, v) => t.V = (byte)v)
            };
            // COPY of the payload at srcOff, guarded at 0, answering to arm 1
            FX1.TableFixedEntry[] plan = new FX1.TableFixedEntry[]
            {
                new FX1.TableFixedEntry(srcOff, 0u, 1u, 0u, 0u, FX1.Schema.TableFixedWire.Copy, 1, 0, 0, 0, argw)
            };
            ArgWProbe dst = new ArgWProbe();
            byte[] widenScratch = Array.Empty<byte>();
            FX1.Schema.TableFixedWire.Run(plan, slots, src, dst, null, ReadOnlySpan<byte>.Empty, ref widenScratch);
            return dst.V;
        }

        Check(FX1.Schema.TableFixedWire.TagAt(new byte[] { 0x01, 0x01 }, 0, 2) != 1,
              "ArgW: TagAt reads a two-byte 0x0101 as 257, not arm 1");
        Check(FX1.Schema.TableFixedWire.TagAt(new byte[] { 0x01, 0x00 }, 0, 2) == 1,
              "ArgW: TagAt reads a two-byte 0x0001 as arm 1");
        Check(FX1.Schema.TableFixedWire.TagAt(new byte[] { 0x01, 0x01 }, 0, 0) == 1,
              "ArgW: zero is read as one, so a 0x0101 prefix matches arm 1 at width 0");
        Check(FX1.Schema.TableFixedWire.TagAt(new byte[] { 0x01, 0x01, 0x00, 0x00 }, 0, 4) != 1,
              "ArgW: TagAt reads a four-byte 0x00000101 as not arm 1");
        Check(FX1.Schema.TableFixedWire.TagAt(new byte[] { 0x01, 0x00, 0x00, 0x00 }, 0, 4) == 1,
              "ArgW: TagAt reads a four-byte 0x00000001 as arm 1");

        Check(Run(2, 2u, new byte[] { 0x01, 0x01, 0xAA }) == 0,
              "ArgW: a two-byte tag 0x0101 whose low byte is 1 does NOT run arm 1");
        Check(Run(2, 2u, new byte[] { 0x01, 0x00, 0xAA }) == 0xAA,
              "ArgW: a two-byte tag 0x0001 DOES run arm 1");
        Check(Run(0, 2u, new byte[] { 0x01, 0x01, 0xAA }) == 0xAA,
              "ArgW: zero is read as one, so a 0x0101 prefix matches arm 1 at width 0");
        Check(Run(4, 4u, new byte[] { 0x01, 0x01, 0x00, 0x00, 0xBB }) == 0,
              "ArgW: a four-byte tag 0x00000101 whose low byte is 1 does NOT run arm 1");
        Check(Run(4, 4u, new byte[] { 0x01, 0x00, 0x00, 0x00, 0xBB }) == 0xBB,
              "ArgW: a four-byte tag 0x00000001 DOES run arm 1");
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
        one.BlobLength = 4;
        one.Blob[0] = 0xDE; one.Blob[1] = 0xAD; one.Blob[2] = 0xBE; one.Blob[3] = 0xEF;

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
            Check(back.BlobLength == 4 && back.Blob[0] == 0xDE && back.Blob[1] == 0xAD && back.Blob[2] == 0xBE && back.Blob[3] == 0xEF, "same schema: the blob");
            Check(r.Unknown == 0 && r.KindMismatch == 0 && r.Widened == 0 && r.Clamped == 0 && !r.Malformed && !r.Refused,
                  "same schema: a clean read moves no counter");
        }

        // SKIPPED BY NAME, NOT DELETED (§5.6, §5.9 #23). The rest of this case is
        // the PLAN PATH ACROSS TWO GENERATIONS — FX2 reading an FX1 file and FX1
        // reading an FX2 one — which §5 retires: with no lineage between the two
        // builds each file is layout_newer, correctly, and the compiled read those
        // rows assert cannot be reached from a bare pair of generated units at all.
        // THE COVERAGE MOVES TO THE LINEAGE HARNESS, where the newer unit is handed
        // the older unit's locked entry and the row owes both of §5.7's columns.
        Console.WriteLine("SKIPPED: TestFixedFxCrossGeneration — §5.6 retires the run-time walk of a stranger's layout; the coverage moves to the lineage harness");
        skipped++;
    }

    // RETIRED BY §5.6 AND KEPT: the skip at its call site says why.
    static void TestFixedFxCrossGeneration(byte[] w1)
    {
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
            Check(back.BlobLength == 4 && back.Blob[0] == 0xDE && back.Blob[1] == 0xAD && back.Blob[2] == 0xBE && back.Blob[3] == 0xEF, "older writer: the blob");
            Check(r.Unknown == 1, "older writer: `gone` is the one field this reader cannot name");
            Check(r.KindMismatch == 0 && !r.Malformed && !r.Refused, "older writer: nothing else fired");
        }

        // MISSING FIELD ON A DIRTY DEST: the hole list, not a whole TableReset.
        {
            FX2.FxRoot dirty = new FX2.FxRoot();
            dirty.Added = 999;
            dirty.Extra.X = 77;
            dirty.Extra.Y = 88;
            dirty.Keep = 1;
            FX2.TableReport r = new FX2.TableReport();
            FX2.TableFixedEntry[] plan = new FX2.TableFixedEntry[1024];
            long n = FX2.Schema.FxRootFixedLoad(dirty, w1, plan, r);
            Check(n == 1, "dirty dest: one record");
            Check(dirty.Added == 11, "dirty dest: missing Added is the hole's default, not the poison");
            Check(dirty.Extra.X == 0 && dirty.Extra.Y == 0, "dirty dest: missing nested Extra is the hole's default");
            Check(dirty.Keep == 4242u, "dirty dest: landed Keep overwrites poison");
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
            two.BlobLength = 4;
            two.Blob[0] = 0xCA; two.Blob[1] = 0xFE; two.Blob[2] = 0xBA; two.Blob[3] = 0xBE;
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
            Check(back.BlobLength == 4 && back.Blob[0] == 0xCA && back.Blob[1] == 0xFE && back.Blob[2] == 0xBA && back.Blob[3] == 0xBE, "newer writer: the blob");
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
        byte[] wrongScratch = Array.Empty<byte>();
        FX1.Schema.TableFixedWire.Run(FX1.Schema.FxRootFixedPlan, FX1.Schema.FxRootFixedSlots, body, wrong, r, ReadOnlySpan<byte>.Empty, ref wrongScratch);
        bool intact = wrong.Nested.A == 33 && wrong.Nested.B == 44 && wrong.Renamed == 808;
        Check(!intact, "NEGATIVE CONTROL: the wrong plan must NOT reproduce the record");

        FX1.TableFixedEntry[] plan = new FX1.TableFixedEntry[1024];

        // A BLOCK THAT IS NOT A BLOCK IS REFUSED BY NAME, whole, and never damage.
        {
            byte[] broken = (byte[])w2.Clone();
            broken[FX1.Schema.TableFixedWire.HeaderBytes + 3] = 0x7F; // layout length exceeds buffer
            FX1.FxRoot v = new FX1.FxRoot();
            FX1.TableReport r3 = new FX1.TableReport();
            long bad = FX1.Schema.FxRootFixedLoad(v, broken, plan, r3);
            Check(bad < 0 && r3.Refused && r3.Reason == "layout_malformed", "REFUSED BY NAME: layout_malformed");
            Check(r3.Unknown == 0 && r3.KindMismatch == 0 && !r3.Malformed, "REFUSED BY NAME: a refusal moves no counter");
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

        // SKIPPED BY NAME, NOT DELETED (§5.6, and §5.9 #23 for a suite whose skip
        // verb is a printed line). Every row of TestFixedNegativeControlForwardRead
        // asks FX1 to read an FX2 FILE — a FORWARD read, which is the whole of what
        // §5 retires: FX2's hash is in no lineage entry of this build, so the answer
        // is layout_newer BEFORE a plan exists and the conditions those rows assert
        // can no longer be reached from here. THE COVERAGE IS OWED BY THE LINEAGE
        // HARNESS, where the reader is handed FX1's lineage and each of these is a
        // row with its own two columns. The function stays in the tree and stays
        // compiling: a deleted test is a coverage claim nobody can audit.
        Console.WriteLine("SKIPPED: TestFixedNegativeControlForwardRead — §5.6 retires the forward read; the coverage moves to the lineage harness");
        skipped++;
    }

    // RETIRED BY §5.6 AND KEPT: the skip at its call site, above, says why and
    // where the coverage is owed.
    static void TestFixedNegativeControlForwardRead(byte[] w2)
    {
        // and the loader never takes that path: the hash is what selects the plan
        FX1.FxRoot right = new FX1.FxRoot();
        FX1.TableReport r2 = new FX1.TableReport();
        FX1.TableFixedEntry[] plan = new FX1.TableFixedEntry[1024];
        long n = FX1.Schema.FxRootFixedLoad(right, w2, plan, r2);
        Check(n == 1 && right.Nested.A == 33 && right.Nested.B == 44,
              "NEGATIVE CONTROL: the loader compiles a plan from the block and gets it right");

        {
            byte[] unknownKind = (byte[])w2.Clone();
            unknownKind[FX1.Schema.TableFixedWire.HeaderBytes + 4 + 4 + 17 + 8] = 99; // entry 1's kind byte
            FX1.FxRoot v = new FX1.FxRoot();
            FX1.TableReport rKind = new FX1.TableReport();
            long bad = FX1.Schema.FxRootFixedLoad(v, unknownKind, plan, rKind);
            Check(bad < 0 && rKind.Refused && rKind.Reason == "layout_kind_unknown", "REFUSED BY NAME: unknown wire kind refuses as layout_kind_unknown");
            Check(rKind.Unknown == 0 && rKind.KindMismatch == 0 && !rKind.Malformed, "REFUSED BY NAME: unknown kind refusal moves no counter");
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


    static void TestFixedWide128Case()
    {
        S2.SimState s = new S2.SimState();
        S2.Schema.TableReset(s);
        // IN RANGE, BECAUSE THE BOUNDS PASS IS REAL NOW (§4.6). `energy` is
        // declared | min = -5000000000, max = 5000000000, and this case read
        // -1234567890123456789 back exactly only because there was no bounds pass
        // to hold it: the clean-read assertion below was pinning the MISSING
        // pass. The 128-bit path is what the case is about, and a value inside
        // the declaration exercises it just as well.
        s.Energy = -1234567890L;
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
        Check(back.Energy == -1234567890L, "SimState energy int128 matches");
        Check(back.Reach == -42, "SimState reach fixed128 matches");
        Check(back.Mass == 100, "SimState mass ufixed128 matches");
        Check(back.SeedsCount == 1 && back.Seeds[0] == 9999999999999999999UL, "SimState seeds uint128 matches");
    }

    // A SIGNED fixed(I,F) FIELD'S LOW END, FORGED BELOW ITS DECLARED MINIMUM
    // (§4.6, docs/SPEC-TABLES.md §4). `position` is fixed(48, 16) | min = -1000,
    // max = 1000, so the RAW ends the reader clamps at are -1000 << 16 and
    // 1000 << 16, and neither sits on 64-bit storage's own limit: both are
    // comparisons a stored value can lose. The emitter decided WHETHER to write
    // an end by testing the UNSHIFTED bound against an UNSIGNED storage range,
    // which answered "-1000 cannot beat 0" — so every signed fixed field with a
    // declared minimum at or below zero lost its low clamp, and a raw under the
    // minimum rode through unclamped and uncounted. ON BOTH PLANS, because the
    // bounds pass runs over STORAGE and one pass serves both.
    static void TestFixedSignedLowEndCase()
    {
        const long MinRaw = -65536000L;  // -1000 << 16
        const long Forged = -65536001L;  // one raw step below the declaration

        S2.SimState s = new S2.SimState();
        S2.Schema.TableReset(s);
        s.Position = MinRaw;

        byte[] buf = new byte[S2.Schema.SimStateFixedMeasure(1)];
        long saved = S2.Schema.SimStateFixedSave(s, buf);
        Check(saved == buf.Length, "signed fixed low end: save");

        // THE FIELD IS FOUND BY ITS OWN BYTES, not by an offset spelled twice:
        // the minimum's raw appears exactly once in the record, so writing one
        // step below it forges that field and nothing else.
        byte[] needle = new byte[8];
        BinaryPrimitives.WriteInt64LittleEndian(needle, MinRaw);
        int at = -1, hits = 0;
        for (int i = 0; i + 8 <= buf.Length; ++i)
        {
            if (buf.AsSpan(i, 8).SequenceEqual(needle)) { hits++; at = i; }
        }
        Check(hits == 1, "signed fixed low end: the declared minimum's raw sits in the record exactly once");
        BinaryPrimitives.WriteInt64LittleEndian(buf.AsSpan(at), Forged);

        // THE IDENTITY PATH
        S2.SimState back = new S2.SimState();
        S2.TableReport r = new S2.TableReport();
        S2.TableFixedEntry[] plan = new S2.TableFixedEntry[128];
        Check(S2.Schema.SimStateFixedLoad(back, buf, plan, r) == 1,
              "signed fixed low end: the record still reads");
        Check(back.Position == MinRaw,
              "signed fixed low end: a raw below the declared minimum lands the minimum's raw");
        Check(r.Clamped == 1, "signed fixed low end: and counts exactly ONE clamped");
        Check(!r.Malformed && !r.Refused,
              "signed fixed low end: damage in a VALUE is not framing damage");

        // THE COMPILED PATH, with the bounds pass after the run — §5.3 step 11 is
        // the same straight-line code on both plans.
        {
            S2.SimState viaPlan = new S2.SimState();
            S2.Schema.TableReset(viaPlan);
            uint layoutBytes = BinaryPrimitives.ReadUInt32LittleEndian(
                buf.AsSpan(S2.Schema.TableFixedWire.HeaderBytes));
            ReadOnlySpan<byte> layout = buf.AsSpan(S2.Schema.TableFixedWire.HeaderBytes + 4, (int)layoutBytes);
            Check(S2.Schema.TableFixedWire.ParseLayout(layout, out S2.TableFixedLayoutView parsed),
                  "signed fixed low end: the layout parses");

            S2.TableReport r2 = new S2.TableReport();
            Span<S2.TableFixedEntry> compiled = new S2.TableFixedEntry[512];
            int made = S2.Schema.TableFixedWire.Compile(parsed, S2.Schema.SimStateFixedLayout,
                                                       S2.Schema.SimStateFixedDst, compiled, r2);
            Check(made > 0, "signed fixed low end: a plan compiles from my own layout");

            ReadOnlySpan<byte> planBytes = System.Runtime.InteropServices.MemoryMarshal.AsBytes(compiled);
            ReadOnlySpan<byte> body = buf.AsSpan(S2.Schema.TableFixedWire.HeaderBytes + 4 + (int)layoutBytes);
            byte[] scratch = Array.Empty<byte>();
            S2.Schema.TableFixedWire.Run(compiled.Slice(0, made), S2.Schema.SimStateFixedSlots,
                                        body.Slice(8), viaPlan, r2, planBytes, ref scratch);
            S2.Schema.SimStateFixedClamp(viaPlan, r2);
            Check(viaPlan.Position == MinRaw,
                  "signed fixed low end: the COMPILED plan lands the minimum's raw too");
            Check(r2.Clamped == 1,
                  "signed fixed low end: and the compiled read counts ONE clamped as well");
        }
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
            byte[] copyScratch = Array.Empty<byte>();
            TD.Schema.TableFixedWire.Run(copyPlan, TD.Schema.RangedSignedFixedSlots, recordBody, backCopy, null, ReadOnlySpan<byte>.Empty, ref copyScratch);
            Check(backCopy.EdgesCount == 4 && backCopy.Edges[0] == 11 && backCopy.Edges[1] == 22 &&
                  backCopy.Edges[2] == 33 && backCopy.Edges[3] == 44,
                  "RangedSigned: Copy opcode dispatches to SetBytes for folded array");
        }

        // 3. ShipEntry: 16 bytes (Hardpoints [..4]int32)
        {
            TD.ShipEntry se = new TD.ShipEntry();
            TD.Schema.TableReset(se);
            // IN RANGE (§4.6): `hardpoints` is declared | min = 0, max = 8, and
            // 101/202/303/404 read back exactly only because there was no bounds
            // pass. The case is about the 16-byte FOLDED element run, which four
            // in-range values reproduce exactly as well.
            se.HardpointsCount = 4;
            se.Hardpoints[0] = 1;
            se.Hardpoints[1] = 2;
            se.Hardpoints[2] = 3;
            se.Hardpoints[3] = 4;

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
            Check(back.Hardpoints[0] == 1 && back.Hardpoints[1] == 2 && back.Hardpoints[2] == 3 && back.Hardpoints[3] == 4,
                  "ShipEntry 16-byte folded int32 array reproduced");
        }
    }

    static byte[] UtRecord(string label, UT.UtGrade grade, int m, int tail)
    {
        UT.UtRoot root = new UT.UtRoot();
        UT.Schema.TableReset(root);
        root.Pick.Type = UT.UtPickType.B;
        byte[] labelBytes = Encoding.UTF8.GetBytes(label);
        labelBytes.CopyTo(root.Pick.B.Label.AsSpan());
        root.Pick.B.LabelLength = labelBytes.Length;
        root.Pick.B.Grade = grade;
        root.Pick.B.M = m;
        root.Tail = tail;

        byte[] wire = new byte[UT.Schema.UtRootFixedMeasure(1)];
        Check(UT.Schema.UtRootFixedSave(root, wire) == wire.Length, "arm text: save fills what measure says");
        return wire;
    }

    static UT.UtRoot UtCompiled(byte[] wire, UT.TableReport r)
    {
        UT.UtRoot outVal = new UT.UtRoot();
        UT.Schema.TableReset(outVal);

        uint layoutBytes = System.Buffers.Binary.BinaryPrimitives.ReadUInt32LittleEndian(
            wire.AsSpan(UT.Schema.TableFixedWire.HeaderBytes));
        ReadOnlySpan<byte> layout = wire.AsSpan(UT.Schema.TableFixedWire.HeaderBytes + 4, (int)layoutBytes);
        Check(UT.Schema.TableFixedWire.ParseLayout(layout, out UT.TableFixedLayoutView parsed),
              "arm text: the layout parses");

        Span<UT.TableFixedEntry> plan = new UT.TableFixedEntry[512];
        int made = UT.Schema.TableFixedWire.Compile(parsed, UT.Schema.UtRootFixedLayout, UT.Schema.UtRootFixedDst, plan, r);
        Check(made > 0, "arm text: a plan compiles from my own layout");

        ReadOnlySpan<byte> planBytes = System.Runtime.InteropServices.MemoryMarshal.AsBytes(plan);
        ReadOnlySpan<byte> at = wire.AsSpan(UT.Schema.TableFixedWire.HeaderBytes + 4 + (int)layoutBytes);
        byte[] utScratch = Array.Empty<byte>();
        UT.Schema.TableFixedWire.Run(plan.Slice(0, made), UT.Schema.UtRootFixedSlots, at.Slice(8), outVal, r, planBytes, ref utScratch);
        // THE COMPILED PATH IS THE RUN AND THEN THE PLAN'S BOUNDS (§4.6, §5.2),
        // exactly as the generated root spells it: the ordinal op lands the RAW
        // and counts nothing, and this pass clamps it against the WRITER's own
        // variant count out of the plan and counts there (§5.9 #27).
        UT.Schema.TableFixedWire.ClampPlanBounds(plan.Slice(0, made), UT.Schema.UtRootFixedSlots, at.Slice(8), outVal, r, planBytes);
        return outVal;
    }

    static bool UtSame(UT.UtRoot x, UT.UtRoot y)
    {
        byte[] a = new byte[UT.Schema.UtRootFixedMeasure(1)];
        byte[] b = new byte[UT.Schema.UtRootFixedMeasure(1)];
        UT.Schema.UtRootFixedSave(x, a);
        UT.Schema.UtRootFixedSave(y, b);
        return a.AsSpan().SequenceEqual(b);
    }

    static int UtBodyAt(byte[] wire)
    {
        uint layoutBytes = System.Buffers.Binary.BinaryPrimitives.ReadUInt32LittleEndian(
            wire.AsSpan(UT.Schema.TableFixedWire.HeaderBytes));
        return UT.Schema.TableFixedWire.HeaderBytes + 4 + (int)layoutBytes + 8;
    }

    static void TestFixedArmTextCase()
    {
        byte[] wire = UtRecord("hello", UT.UtGrade.gold, 42, 11);

        // THE IDENTITY PATH
        UT.UtRoot id = new UT.UtRoot();
        UT.TableReport r = new UT.TableReport();
        UT.TableFixedEntry[] plan = new UT.TableFixedEntry[512];
        long n = UT.Schema.UtRootFixedLoad(id, wire, plan, r);
        Check(n == 1, "arm text: the identity path reads the record");
        Check(!r.Refused && !r.Malformed && r.Clamped == 0 && r.Unknown == 0 && r.KindMismatch == 0,
              "arm text: a clean read moves no counter");
        Check(id.Pick.Type == UT.UtPickType.B, "arm text: the SECOND arm is the selected one");
        Check(Encoding.UTF8.GetString(id.Pick.B.Label.AsSpan(0, id.Pick.B.LabelLength)) == "hello",
              "arm text: the identity path lands the arm's text");
        Check(id.Pick.B.Grade == UT.UtGrade.gold && id.Pick.B.M == 42 && id.Tail == 11,
              "arm text: and the rest of that arm with it");

        // THE COMPILED PATH, and it must land the same record
        UT.TableReport rc = new UT.TableReport();
        UT.UtRoot viaPlan = UtCompiled(wire, rc);
        Check(Encoding.UTF8.GetString(viaPlan.Pick.B.Label.AsSpan(0, viaPlan.Pick.B.LabelLength)) == "hello",
              "arm text: a COMPILED plan lands a text field under the SECOND arm");
        Check(viaPlan.Pick.B.Grade == UT.UtGrade.gold && viaPlan.Pick.B.M == 42 && viaPlan.Tail == 11,
              "arm text: and the rest of that arm with it");
        Check(rc.Clamped == 0 && !rc.Malformed && !rc.Refused,
              "arm text: the compiled read moves no counter either");
        Check(UtSame(viaPlan, id),
              "arm text: a COMPILED plan lands exactly what the identity plan lands");

        // A TAG PAST THE LAST ARM: None on both paths, and the identity path
        // counts one clamped. THE COMPILED PLAN COUNTS NOTHING HERE and that
        // is not an oversight: a plan says what a SELECTED arm does, so
        // "no arm fired" is not an event any entry of it can see. The tag
        // still lands None, because the prefill put None there and no entry
        // wrote over it.
        {
            byte[] bad = (byte[])wire.Clone();
            int body = UtBodyAt(bad);
            Check(bad[body] == 2, "tag past the last arm: the tag sits where the layout says");
            bad[body] = 7;

            UT.UtRoot v = new UT.UtRoot();
            UT.TableReport r1 = new UT.TableReport();
            Check(UT.Schema.UtRootFixedLoad(v, bad, plan, r1) == 1,
                  "tag past the last arm: the record still reads");
            Check(v.Pick.Type == UT.UtPickType.None,
                  "tag past the last arm: the union lands None");
            Check(r1.Clamped == 1,
                  "tag past the last arm: the identity path counts ONE clamped");
            Check(!r1.Malformed && !r1.Refused,
                  "tag past the last arm: damage in a VALUE is not framing damage");

            UT.TableReport r2 = new UT.TableReport();
            UT.UtRoot bent = UtCompiled(bad, r2);
            Check(bent.Pick.Type == UT.UtPickType.None,
                  "tag past the last arm: the compiled plan lands None too");
            Check(r2.Clamped == 0,
                  "tag past the last arm: no entry of a plan can see an arm that did not fire");
        }

        // AN ORDINAL PAST THE LAST VARIANT: None (0) on both paths, and BOTH
        // count it — the identity scatter on its own read, and the plan's
        // ordinal op on the compiled one.
        {
            byte[] bad = (byte[])wire.Clone();
            int body = UtBodyAt(bad);
            int at = body + 13; // the tag, then the arm's text, then the ordinal
            Check(bad[at] == (byte)UT.UtGrade.gold,
                  "ordinal past the last variant: the ordinal is where the layout says");
            bad[at] = 9;

            UT.UtRoot v = new UT.UtRoot();
            UT.TableReport r1 = new UT.TableReport();
            Check(UT.Schema.UtRootFixedLoad(v, bad, plan, r1) == 1,
                  "ordinal past the last variant: the record still reads");
            Check(v.Pick.B.Grade == UT.UtGrade.None,
                  "ordinal past the last variant: it lands None");
            Check(r1.Clamped == 1,
                  "ordinal past the last variant: the identity path counts ONE clamped");

            UT.TableReport r2 = new UT.TableReport();
            UT.UtRoot bent = UtCompiled(bad, r2);
            Check(bent.Pick.B.Grade == UT.UtGrade.None,
                  "ordinal past the last variant: the compiled plan lands None too");
            Check(r2.Clamped == 1,
                  "ordinal past the last variant: and the plan's ordinal op counts it");
            Check(UtSame(bent, v),
                  "ordinal past the last variant: the two paths land ONE record");
        }
    }

    // THE SLACK IS ZERO (docs/SPEC-TABLES.md §3.4). A string(N) shorter than N
    // and a [..N]T with unused slots are declared bytes carrying no value, and
    // what rides in them is the template's zeros — never the writer's leftovers
    // past the used length or the live count.
    static void TestFixedSlackCase()
    {
        FX1.FxRoot v = new FX1.FxRoot();
        FX1.Schema.TableReset(v);
        v.Keep = 11u;
        v.Narrow = 22;
        v.Renamed = 33;
        v.Gone = 44;
        v.Nested.A = 55;
        v.Nested.B = 66;
        Array.Fill(v.Label, (byte)0xAA);
        v.Label[0] = (byte)'h';
        v.Label[1] = (byte)'i';
        v.LabelLength = 2;
        for (int k = 0; k < 4; ++k) { v.Marks[k] = unchecked((int)0x5A5A5A5A); }
        v.Marks[0] = 7;
        v.MarksCount = 1;

        Check(v.Label[2] == 0xAA, "CONTROL: the text slack really is stained in storage");
        Check(v.Marks[1] == unchecked((int)0x5A5A5A5A), "CONTROL: the array slack really is stained in storage");

        byte[] file = new byte[FX1.Schema.FxRootFixedMeasure(1)];
        Check(FX1.Schema.FxRootFixedSave(v, file) == file.Length, "slack: the record saves");
        int bodyAt = FX1.Schema.TableFixedWire.HeaderBytes + 4 + (int)FX1.Schema.FxRootFixedLayoutBytes + 8;
        ReadOnlySpan<byte> body = file.AsSpan(bodyAt, (int)FX1.Schema.FxRootFixedBodyBytes);
        Check(body.IndexOf((byte)0xAA) < 0, "SLACK IS ZERO: not one stained TEXT byte reached the wire");
        Check(body.IndexOf((byte)0x5A) < 0, "SLACK IS ZERO: not one stained ARRAY byte reached the wire");

        {
            byte[] whole = new byte[v.Label.Length];
            v.Label.AsSpan().CopyTo(whole);
            Check(whole.AsSpan().IndexOf((byte)0xAA) >= 0,
                  "NEGATIVE CONTROL: a whole-span copy WOULD have carried the text stain");
            int[] marks = (int[])v.Marks.Clone();
            Check(marks[3] == unchecked((int)0x5A5A5A5A),
                  "NEGATIVE CONTROL: a whole-span copy WOULD have carried the array stain");
        }

        {
            FX1.FxRoot back = new FX1.FxRoot();
            FX1.TableReport r = new FX1.TableReport();
            FX1.TableFixedEntry[] plan = new FX1.TableFixedEntry[1024];
            Check(FX1.Schema.FxRootFixedLoad(back, file, plan, r) == 1, "slack: the record reads");
            Check(back.LabelLength == 2 && back.Label[0] == (byte)'h' && back.Label[1] == (byte)'i' && back.Label[2] == 0,
                  "slack: the used length reads, and the buffer terminates at it");
            Check(back.MarksCount == 1 && back.Marks[0] == 7, "slack: the live count reads");
            Check(back.Marks[1] == 0 && back.Marks[2] == 0 && back.Marks[3] == 0,
                  "slack: an unused slot lands as the wire's zero and not as some writer's leftover");
            Check(r.Clamped == 0 && !r.Malformed && !r.Refused, "slack: a clean read moves no counter");
        }
    }

    static byte[] LayoutFileOf(byte[] layout)
    {
        byte[] f = new byte[FX1.Schema.TableFixedWire.HeaderBytes + 4 + layout.Length];
        f[0] = 3;
        BinaryPrimitives.WriteUInt64LittleEndian(f.AsSpan(FX1.Schema.TableFixedWire.HashAt), FX1.Schema.TableFixedWire.HashOf(layout));
        BinaryPrimitives.WriteUInt32LittleEndian(f.AsSpan(FX1.Schema.TableFixedWire.HeaderBytes), (uint)layout.Length);
        layout.CopyTo(f, FX1.Schema.TableFixedWire.HeaderBytes + 4);
        return f;
    }

    static void LayoutRefuses(byte[] broken, string want, string what)
    {
        FX1.FxRoot v = new FX1.FxRoot();
        FX1.TableReport r = new FX1.TableReport();
        FX1.TableFixedEntry[] plan = new FX1.TableFixedEntry[1024];
        long n = FX1.Schema.FxRootFixedLoad(v, broken, plan, r);
        Check(n < 0 && r.Refused && r.Reason == want, what);
        Check(r.Unknown == 0 && r.KindMismatch == 0 && r.Widened == 0 && r.Clamped == 0 && !r.Malformed,
              "a layout refusal sets nothing and counts nothing");
    }

    static void PutLayoutEntry(byte[] layout, int at, ulong id, byte kind, uint size, uint children)
    {
        BinaryPrimitives.WriteUInt64LittleEndian(layout.AsSpan(at), id);
        layout[at + 8] = kind;
        BinaryPrimitives.WriteUInt32LittleEndian(layout.AsSpan(at + 9), size);
        BinaryPrimitives.WriteUInt32LittleEndian(layout.AsSpan(at + 13), children);
    }

    static void TestFixedLayoutValidation()
    {
        // the layout this reader ACCEPTS, which every case below breaks once
        FX2.FxRoot two = new FX2.FxRoot();
        FX2.Schema.TableReset(two);
        byte[] good = new byte[FX2.Schema.FxRootFixedMeasure(1)];
        Check(FX2.Schema.FxRootFixedSave(two, good) == good.Length, "layout validation: the unbroken file saves");

        int layoutAt = FX1.Schema.TableFixedWire.HeaderBytes + 4;
        int entry0 = layoutAt + 4;

        // 1. THE ENTRY COUNT FITS THE LAYOUT LENGTH EXACTLY
        {
            byte[] f = (byte[])good.Clone();
            uint count = BinaryPrimitives.ReadUInt32LittleEndian(f.AsSpan(layoutAt));
            BinaryPrimitives.WriteUInt32LittleEndian(f.AsSpan(layoutAt), count + 1);
            LayoutRefuses(f, "layout_count_mismatch", "RULE: the entry count fits the layout length exactly");
        }
        {
            byte[] f = (byte[])good.Clone();
            BinaryPrimitives.WriteUInt32LittleEndian(f.AsSpan(layoutAt), 0);
            LayoutRefuses(f, "layout_count_mismatch", "RULE: an entry count of zero is not a layout");
        }

        // 2. EVERY KIND IS IN THE CLOSED SET
        {
            byte[] f = (byte[])good.Clone();
            f[entry0 + 1 * 17 + 8] = 200; // a kind no form byte defines
            LayoutRefuses(f, "layout_kind_unknown", "RULE: a kind outside the closed set is REFUSED, not skipped");
        }

        // 3. A KIND IS USED AS ITS DEFINITION ALLOWS — here, the ROOT is a table
        {
            byte[] f = (byte[])good.Clone();
            f[entry0 + 0 * 17 + 8] = 14; // an array as the root of a record
            LayoutRefuses(f, "layout_kind_invalid", "RULE: the root entry is a TABLE");
        }

        // 4. A CONSTANT SIZE MATCHES ITS KIND
        {
            byte[] f = (byte[])good.Clone();
            BinaryPrimitives.WriteUInt32LittleEndian(f.AsSpan(entry0 + 1 * 17 + 9), 5); // a uint32 leaf in five bytes
            LayoutRefuses(f, "layout_size_mismatch", "RULE: a constant size its kind does not admit");
        }
        {
            byte[] f = (byte[])good.Clone();
            uint body = BinaryPrimitives.ReadUInt32LittleEndian(f.AsSpan(entry0 + 0 * 17 + 9));
            BinaryPrimitives.WriteUInt32LittleEndian(f.AsSpan(entry0 + 0 * 17 + 9), body + 4);
            LayoutRefuses(f, "layout_size_mismatch", "RULE: a table's size is the sum of its fields'");
        }

        // 5. THE PRE-ORDER CHILD WALK CONSUMES EXACTLY THE ENTRIES
        {
            byte[] f = (byte[])good.Clone();
            uint kids = BinaryPrimitives.ReadUInt32LittleEndian(f.AsSpan(entry0 + 0 * 17 + 13));
            BinaryPrimitives.WriteUInt32LittleEndian(f.AsSpan(entry0 + 0 * 17 + 13), kids + 1);
            LayoutRefuses(f, "layout_tree_unclosed", "RULE: the tree runs out of layout");
        }
        {
            byte[] layout = new byte[4 + 4 * 17];
            BinaryPrimitives.WriteUInt32LittleEndian(layout, 4);
            PutLayoutEntry(layout, 4, 1, 13, 4, 1); // a table of one field
            PutLayoutEntry(layout, 4 + 17, 2, 30, 4, 0); // an enum, its TWO variants unreached
            PutLayoutEntry(layout, 4 + 34, 3, 32, 0, 0);
            PutLayoutEntry(layout, 4 + 51, 4, 32, 0, 0);
            byte[] f = LayoutFileOf(layout);
            LayoutRefuses(f, "layout_tree_unclosed", "RULE: the layout outlasts the tree");
        }

        // 6. THE TOTAL RECORD SIZE IS WITHIN 65536 AND DOES NOT OVERFLOW
        {
            byte[] f = (byte[])good.Clone();
            BinaryPrimitives.WriteUInt32LittleEndian(f.AsSpan(entry0 + 0 * 17 + 9), 65537);
            LayoutRefuses(f, "layout_record_too_large", "RULE: a record size past 65536");
        }
        {
            byte[] f = (byte[])good.Clone();
            BinaryPrimitives.WriteUInt32LittleEndian(f.AsSpan(entry0 + 1 * 17 + 9), 0xFFFFFFFF);
            LayoutRefuses(f, "layout_record_too_large", "RULE: a size that would overflow the sum");
        }

        // 7. NOTHING NESTED PAST THE READER'S WALK BOUND
        {
            uint depth = 64; // MaxDepth is 32
            byte[] layout = new byte[4 + (depth + 1) * 17];
            BinaryPrimitives.WriteUInt32LittleEndian(layout, depth + 1);
            for (uint i = 0; i < depth; ++i)
            {
                PutLayoutEntry(layout, (int)(4 + i * 17), 1, (byte)(i == 0 ? 13 : 35), depth - i, 1);
            }
            PutLayoutEntry(layout, (int)(4 + depth * 17), 2, 1, 1, 0); // a bool at the bottom
            byte[] f = LayoutFileOf(layout);
            LayoutRefuses(f, "layout_too_deep", "RULE: a nesting depth past the walk's own bound");
        }

        // RESIDUE: fewer bytes than a header is layout_malformed
        {
            byte[] f = LayoutFileOf(new byte[2]);
            LayoutRefuses(f, "layout_malformed", "RULE: fewer bytes than a header is layout_malformed");
        }
    }

    static void TestFixedHostileBoolCase()
    {
        TD.WeaponConfig wc = new TD.WeaponConfig();
        TD.Schema.TableReset(wc);
        wc.Homing = false;

        byte[] buf = new byte[TD.Schema.WeaponConfigFixedMeasure(1)];
        long saved = TD.Schema.WeaponConfigFixedSave(wc, buf);
        Check(saved == buf.Length, "hostile bool: initial save");

        int bodyOffset = TD.Schema.TableFixedWire.HeaderBytes + 4 + (int)TD.Schema.WeaponConfigFixedLayoutBytes + 8;
        int homingOffset = bodyOffset + 16;
        buf[homingOffset] = 0x7F; // plant hostile nonzero byte

        TD.WeaponConfig back = new TD.WeaponConfig();
        TD.TableReport r = new TD.TableReport();
        TD.TableFixedEntry[] plan = new TD.TableFixedEntry[64];
        long loaded = TD.Schema.WeaponConfigFixedLoad(back, buf, plan, r);
        Check(loaded == 1, "hostile bool: load one record");
        Check(r.Unknown == 0 && r.KindMismatch == 0 && r.Widened == 0 && r.Clamped == 0 && !r.Malformed && !r.Refused,
              "hostile bool: clean report, no counters moved");
        Check(back.Homing == true, "hostile bool: nonzero byte normalized to true");

        byte[] resaved = new byte[buf.Length];
        long savedAgain = TD.Schema.WeaponConfigFixedSave(back, resaved);
        Check(savedAgain == resaved.Length, "hostile bool: resave");
        Check(resaved[homingOffset] == 1, "hostile bool: resaved byte is strictly 1, not 0x7F");
    }

    static void TestFixedAbsentOptionalCase()
    {
        P3.Chain v = new P3.Chain();
        P3.Schema.TableReset(v);
        byte[] absentBytes = Encoding.UTF8.GetBytes("absent");
        Array.Copy(absentBytes, v.Name, absentBytes.Length);
        v.NameLength = 6;
        v.LinkPresent = false;
        v.Link.Value = 0x5A5A5A;
        Array.Fill(v.Link.Tag, (byte)0x5A);
        v.Link.TagLength = 5;
        Check(v.Link.Tag[0] == 0x5A, "CONTROL: the absent payload really is stained in storage");

        byte[] w = new byte[P3.Schema.ChainFixedMeasure(1)];
        Check(P3.Schema.ChainFixedSave(v, w) == w.Length, "absent optional: the record saves");
        int bodyOffset = P3.Schema.TableFixedWire.HeaderBytes + 4 + (int)P3.Schema.ChainFixedLayoutBytes + 8;
        int bodyBytes = (int)P3.Schema.ChainFixedBodyBytes;
        Span<byte> body = w.AsSpan(bodyOffset, bodyBytes);
        Check(body.IndexOf((byte)0x5A) < 0,
              "ABSENT OPTIONAL: not one byte of the absent payload reached the wire");

        // and the reader reads what the flag says, with the payload at its defaults
        {
            P3.Chain back = new P3.Chain();
            P3.TableReport r = new P3.TableReport();
            P3.TableFixedEntry[] plan = new P3.TableFixedEntry[1024];
            Check(P3.Schema.ChainFixedLoad(back, w, plan, r) == 1,
                  "absent optional: the record reads");
            Check(!back.LinkPresent, "absent optional: the flag");
            Check(back.Link.Value == 0 && back.Link.TagLength == 0,
                  "absent optional: the payload reads as the wire's zeros");
            Check(r.Clamped == 0 && !r.Malformed && !r.Refused,
                  "absent optional: a clean read moves no counter");
        }

        // THE DISCRIMINATING HALF: the same payload, PRESENT. Those bytes do reach
        // the wire, so the check above is about the flag and not about the writer
        // never having written a payload.
        {
            P3.Chain present = new P3.Chain();
            P3.Schema.TableReset(present);
            Array.Copy(absentBytes, present.Name, absentBytes.Length);
            present.NameLength = 6;
            present.LinkPresent = true;
            present.Link.Value = 0x5A5A5A;
            Array.Fill(present.Link.Tag, (byte)0x5A);
            present.Link.TagLength = 4;
            byte[] pw = new byte[P3.Schema.ChainFixedMeasure(1)];
            Check(P3.Schema.ChainFixedSave(present, pw) == pw.Length,
                  "absent optional: the present twin saves");
            Span<byte> pbody = pw.AsSpan(bodyOffset, bodyBytes);
            Check(pbody.IndexOf((byte)0x5A) >= 0,
                  "absent optional: the present twin puts the bytes on the wire");
        }
    }
}

