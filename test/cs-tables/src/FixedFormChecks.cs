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
        TestFixedValidDataRows();
        TestFixedLittleEndianCopies();
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
        TestFixedFormHeaderProbes();
        TestFixedHashOracleCase();
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
            int forgedOrdinal = 0;
            FX1.Schema.TableFixedWire.Run(plan, slots, src, dst, null, ReadOnlySpan<byte>.Empty, ref widenScratch, ref forgedOrdinal);
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

    // M12 (docs/PORTING.md): the flat element copies read and write the wire's
    // little-endian image. The host here is little-endian, so the bulk copy is
    // checked against explicit little-endian bytes, and the big-endian swap,
    // which no host here runs, is checked on its own.
    static void TestFixedLittleEndianCopies()
    {
        byte[] wire = { 0x01, 0x02, 0x03, 0x04, 0xfe, 0xff, 0xff, 0xff };
        int[] values = new int[2];
        FX1.Schema.TableFixedWire.ReadLittle<int>(wire, values);
        Check(values[0] == 0x04030201 && values[1] == -2, "fixed flat copy: a read takes the little-endian image");
        byte[] back = new byte[8];
        FX1.Schema.TableFixedWire.WriteLittle<int>(values, back);
        Check(back.AsSpan().SequenceEqual(wire), "fixed flat copy: a write gives the little-endian image");
        byte[] swap = { 1, 2, 3, 4, 5, 6, 7, 8 };
        FX1.Schema.TableFixedWire.ReverseEach(swap, 4);
        Check(swap.AsSpan().SequenceEqual(new byte[] { 4, 3, 2, 1, 8, 7, 6, 5 }), "fixed flat copy: the big-endian swap reverses each element");
        FX1.Schema.TableFixedWire.ReverseEach(swap, 1);
        Check(swap[0] == 4 && swap[7] == 5, "fixed flat copy: a one-byte element is never swapped");
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

        byte[] w1 = new byte[(int)FX1.Schema.FxRootFixedMeasure(1)];
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
            Check(r.Unknown == 0 && !r.Refused && r.Widened == 0 && !r.Damaged,
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
            Check(!r.Refused && !r.Damaged, "older writer: nothing else fired");
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
            byte[] w2 = new byte[(int)FX2.Schema.FxRootFixedMeasure(1)];
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
            // uint32 into uint16 is a kind that moved, not a widening: under
            // strict-only it is no lawful entry and refuses (§5.2), never a counter.
            Check(back.Narrow == 3, "newer writer: a narrowing leaves the declared default");
            Check(!r.Damaged && !r.Refused, "newer writer: no damage and no refusal");
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

        byte[] w = new byte[(int)V1.Schema.CfgFixedMeasure(1)];
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
        Check(!r.Damaged && !r.Refused, "V2 reads V1: no damage and no refusal");
    }

    static void TestFixedPCase()
    {
        P1.Chain one = new P1.Chain();
        P1.Schema.TableReset(one);
        byte[] chainBytes = Encoding.UTF8.GetBytes("chain");
        Array.Copy(chainBytes, one.Name, chainBytes.Length);
        one.NameLength = 5;
        one.Link.Value = 500;

        byte[] w = new byte[(int)P1.Schema.ChainFixedMeasure(1)];
        Check(P1.Schema.ChainFixedSave(one, w) == w.Length, "P1 save");

        P3.Chain back = new P3.Chain();
        P3.TableReport r = new P3.TableReport();
        P3.TableFixedEntry[] plan = new P3.TableFixedEntry[1024];
        long n = P3.Schema.ChainFixedLoad(back, w, plan, r);
        // OPTIONAL vs VALUE is a kind that moved: under strict-only the read
        // refuses the whole file, never a silent reread and never a partial value.
        Check(n == -1 && r.Refused && back.NameLength == 0, "OPTIONAL vs VALUE refuses the file, never a silent reread");
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
        byte[] w2 = new byte[(int)FX2.Schema.FxRootFixedMeasure(1)];
        FX2.Schema.FxRootFixedSave(two, w2);
        ReadOnlySpan<byte> body = w2.AsSpan(FX2.Schema.TableFixedWire.HeaderBytes + 4 + (int)FX2.Schema.FxRootFixedLayoutBytes + 8);

        FX1.FxRoot wrong = new FX1.FxRoot();
        FX1.Schema.TableReset(wrong);
        FX1.TableReport r = new FX1.TableReport();
        byte[] wrongScratch = Array.Empty<byte>();
        int wrongForged = 0;
        FX1.Schema.TableFixedWire.Run(FX1.Schema.FxRootFixedPlan, FX1.Schema.FxRootFixedSlots, body, wrong, r, ReadOnlySpan<byte>.Empty, ref wrongScratch, ref wrongForged);
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
            Check(r3.Unknown == 0 && r3.Widened == 0 && !r3.Damaged, "REFUSED BY NAME: a refusal moves no counter");
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
                Check(!r4.Damaged, "REFUSED BY NAME: never damage");
                Check(r4.Unknown == 0 && r4.Widened == 0,
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

    // THE FIVE FORM-HEADER PROBES (card 411411, schema#876; the model is
    // test/elixir-fixedform/main.exs). Each reads the reader's OWN path with the
    // EXACT input docs/FIXED-FORM-ALGORITHM.md §5.3 names and SPEC-TABLES §3.4
    // promises, asserts the report §5.3 fixes (a refusal by name is Refused with
    // its Reason and never Malformed; the residue is Malformed and names
    // nothing), and proves REFUSE is total: the destination is pre-poisoned and
    // compared unchanged, so a refusal that wrote even one byte would show.
    static void TestFixedFormHeaderProbes()
    {
        // A VALID FORM-3 FIXTURE, written by this reader's own writer, so the
        // form-0 and reserved-byte cases carry "the otherwise valid fixture tail".
        FX1.FxRoot fixture = new FX1.FxRoot();
        FX1.Schema.TableReset(fixture);
        fixture.Keep = 4242u;
        fixture.Narrow = 40000;
        fixture.Renamed = 321;
        fixture.Gone = 654;
        fixture.Nested.A = 111;
        fixture.Nested.B = 222;
        byte[] valid = new byte[(int)FX1.Schema.FxRootFixedMeasure(1)];
        Check(FX1.Schema.FxRootFixedSave(fixture, valid) == valid.Length,
              "header probes: a valid form-3 fixture writes");

        // THE POISON: every field a read could land is off its declared default,
        // so a refusal that wrote even one byte would move the fingerprint.
        FX1.FxRoot Poison()
        {
            FX1.FxRoot p = new FX1.FxRoot();
            FX1.Schema.TableReset(p);
            p.Keep = 0xDEADBEEFu;
            p.Narrow = 0xBEEF;
            p.Renamed = -424242;
            p.Gone = -999999;
            p.Nested.A = 12345;
            p.Nested.B = 67890;
            p.MarksCount = 4;
            p.Marks[0] = -7; p.Marks[1] = -8; p.Marks[2] = -9; p.Marks[3] = -10;
            p.BlobLength = 3;
            p.Blob[0] = 0xAA; p.Blob[1] = 0xBB; p.Blob[2] = 0xCC;
            return p;
        }

        string Fp(FX1.FxRoot p) =>
            p.Keep + "|" + p.Narrow + "|" + p.Renamed + "|" + p.Gone + "|" +
            p.Nested.A + "|" + p.Nested.B + "|" + p.MarksCount + "|" +
            string.Join(",", p.Marks) + "|" + p.BlobLength + "|" + Convert.ToHexString(p.Blob);

        string Clean(FX1.TableReport r) =>
            " counters=(" + r.Unknown + "," + r.Widened + ") refused=" + r.Refused + " hash=" + r.LayoutHash;

        FX1.TableFixedEntry[] plan = new FX1.TableFixedEntry[1024];

        // CASE empty-file: ZERO BYTES. There is no first byte, so no name can be
        // earned; the residue is malformed and nothing else claims success.
        {
            FX1.FxRoot v = Poison();
            string before = Fp(v);
            FX1.TableReport r = new FX1.TableReport();
            long n = FX1.Schema.FxRootFixedLoad(v, ReadOnlySpan<byte>.Empty, plan, r);
            Check(n == -1 && r.Damaged && !r.Refused && r.Reason == null &&
                  r.Unknown == 0 && r.Widened == 0 && r.LayoutHash == 0,
                  "empty-file: zero bytes is malformed and claims no success" + Clean(r));
            Check(Fp(v) == before, "empty-file: no destination byte was written");
        }

        // CASE short-form1: THE REAL LENGTH OF A COMMITTED FORM-1 FILE — ten
        // bytes, never a full-length buffer. The form byte is read BEFORE the
        // twenty-byte minimum, so this is previous_form and NOT malformed.
        {
            byte[] form1 = new byte[10];
            form1[0] = 1;
            FX1.FxRoot v = Poison();
            string before = Fp(v);
            FX1.TableReport r = new FX1.TableReport();
            long n = FX1.Schema.FxRootFixedLoad(v, form1, plan, r);
            Check(n == -1 && r.Refused && r.Reason == "previous_form" && !r.Damaged &&
                  r.Unknown == 0 && r.Widened == 0 && r.LayoutHash == 0,
                  "short-form1: a ten-byte form-1 file is previous_form, not malformed" + Clean(r));
            Check(Fp(v) == before, "short-form1: no destination byte was written");
        }

        // CASE short-form2: THE REAL LENGTH OF A FORM-2 BATCH — three bytes,
        // never a full-length buffer. message_form_as_file, not malformed.
        {
            byte[] form2 = new byte[] { 2, 1, 0 };
            FX1.FxRoot v = Poison();
            string before = Fp(v);
            FX1.TableReport r = new FX1.TableReport();
            long n = FX1.Schema.FxRootFixedLoad(v, form2, plan, r);
            Check(n == -1 && r.Refused && r.Reason == "message_form_as_file" && !r.Damaged &&
                  r.Unknown == 0 && r.Widened == 0 && r.LayoutHash == 0,
                  "short-form2: a three-byte form-2 batch is message_form_as_file, not malformed" + Clean(r));
            Check(Fp(v) == before, "short-form2: no destination byte was written");
        }

        // CASE unassigned-form0: FORM BYTE 0 WITH THE OTHERWISE VALID FIXTURE
        // TAIL. No form defines 0 and the registry is ordered, so it is newer_form.
        {
            byte[] form0 = (byte[])valid.Clone();
            form0[0] = 0;
            FX1.FxRoot v = Poison();
            string before = Fp(v);
            FX1.TableReport r = new FX1.TableReport();
            long n = FX1.Schema.FxRootFixedLoad(v, form0, plan, r);
            Check(n == -1 && r.Refused && r.Reason == "newer_form" && !r.Damaged &&
                  r.Unknown == 0 && r.Widened == 0 && r.LayoutHash == 0,
                  "unassigned-form0: form byte 0 is newer_form, not malformed" + Clean(r));
            Check(Fp(v) == before, "unassigned-form0: no destination byte was written");
        }

        // CASE reserved-byte3: A VALID FORM-3 FIXTURE WITH RESERVED BYTE 3 SET
        // NONZERO. The seven reserved bytes are refused, not ignored.
        {
            byte[] reserved = (byte[])valid.Clone();
            reserved[3] = 1;
            FX1.FxRoot v = Poison();
            string before = Fp(v);
            FX1.TableReport r = new FX1.TableReport();
            long n = FX1.Schema.FxRootFixedLoad(v, reserved, plan, r);
            Check(n == -1 && r.Damaged && !r.Refused && r.Reason == null &&
                  r.Unknown == 0 && r.Widened == 0 && r.LayoutHash == 0,
                  "reserved-byte3: a nonzero reserved byte is malformed and claims no success" + Clean(r));
            Check(Fp(v) == before, "reserved-byte3: no destination byte was written");
        }
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
            Check(rKind.Unknown == 0 && !rKind.Refused && !rKind.Damaged, "REFUSED BY NAME: unknown kind refusal moves no counter");
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
            Check(!r6.Damaged, "REFUSED BY NAME: never damage");
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
            Check(!rNoLayout.Damaged, "REFUSED BY NAME: never damage");
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

        byte[] buf = new byte[(int)S2.Schema.SimStateFixedMeasure(1)];
        long saved = S2.Schema.SimStateFixedSave(s, buf);
        Check(saved == buf.Length, "SimState save");

        S2.SimState back = new S2.SimState();
        S2.TableReport r = new S2.TableReport();
        S2.TableFixedEntry[] plan = new S2.TableFixedEntry[128];
        long loaded = S2.Schema.SimStateFixedLoad(back, buf, plan, r);
        Check(loaded == 1, "SimState load one record");
        Check(r.Unknown == 0 && !r.Refused && r.Widened == 0 && !r.Damaged, "SimState clean read");
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

        byte[] buf = new byte[(int)S2.Schema.SimStateFixedMeasure(1)];
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
        {
            // the declared minimum itself is in range and reads exactly
            S2.SimState atMin = new S2.SimState();
            S2.TableReport rm = new S2.TableReport();
            Check(S2.Schema.SimStateFixedLoad(atMin, buf, new S2.TableFixedEntry[128], rm) == 1 && !rm.Refused && atMin.Position == MinRaw,
                  "signed fixed low end: the declared minimum reads exactly");
        }
        BinaryPrimitives.WriteInt64LittleEndian(buf.AsSpan(at), Forged);

        // THE IDENTITY PATH
        S2.SimState back = new S2.SimState();
        S2.TableReport r = new S2.TableReport();
        S2.TableFixedEntry[] plan = new S2.TableFixedEntry[128];
        // A raw below the declared minimum is a value out of range: under
        // strict-only the read refuses the whole load (no read clamps).
        Check(S2.Schema.SimStateFixedLoad(back, buf, plan, r) == -1 && r.Refused,
              "signed fixed low end: a raw below the declared minimum refuses the load");
        Check(back.Position == new S2.SimState().Position,
              "signed fixed low end: the refused load leaves the declared default");
        Check(!r.Damaged,
              "signed fixed low end: a VALUE out of range is not framing damage");

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
            int forged = 0;
            string why = S2.Schema.TableFixedWire.Run(compiled.Slice(0, made), S2.Schema.SimStateFixedSlots,
                                        body.Slice(8), viaPlan, r2, planBytes, ref scratch, ref forged);
            // the run's own refusal, and the forged-ordinal lane, end the read as
            // the public load does (§5.4): an out_of_range refusal, never a count
            if ((why != null || forged != 0) && !r2.Refused) { r2.Refused = true; r2.Reason = why ?? "out_of_range"; }
            S2.Schema.SimStateFixedClamp(viaPlan, r2);
            Check(r2.Refused && !r2.Damaged,
                  "signed fixed low end: the COMPILED plan's bounds pass refuses the out-of-range raw too");
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

            byte[] buf = new byte[(int)TD.Schema.LoadoutConfigFixedMeasure(1)];
            long saved = TD.Schema.LoadoutConfigFixedSave(lc, buf);
            Check(saved == buf.Length, "LoadoutConfig save");

            TD.LoadoutConfig back = new TD.LoadoutConfig();
            TD.TableReport r = new TD.TableReport();
            TD.TableFixedEntry[] plan = new TD.TableFixedEntry[64];
            long loaded = TD.Schema.LoadoutConfigFixedLoad(back, buf, plan, r);
            Check(loaded == 1, "LoadoutConfig load one record");
            Check(r.Unknown == 0 && !r.Refused && r.Widened == 0 && !r.Damaged, "LoadoutConfig clean read");
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

            byte[] buf = new byte[(int)TD.Schema.RangedSignedFixedMeasure(1)];
            long saved = TD.Schema.RangedSignedFixedSave(rs, buf);
            Check(saved == buf.Length, "RangedSigned save");

            TD.RangedSigned back = new TD.RangedSigned();
            TD.TableReport r = new TD.TableReport();
            TD.TableFixedEntry[] plan = new TD.TableFixedEntry[64];
            long loaded = TD.Schema.RangedSignedFixedLoad(back, buf, plan, r);
            Check(loaded == 1, "RangedSigned load one record");
            Check(r.Unknown == 0 && !r.Refused && r.Widened == 0 && !r.Damaged, "RangedSigned clean read");
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
            int copyForged = 0;
            TD.Schema.TableFixedWire.Run(copyPlan, TD.Schema.RangedSignedFixedSlots, recordBody, backCopy, null, ReadOnlySpan<byte>.Empty, ref copyScratch, ref copyForged);
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

            byte[] buf = new byte[(int)TD.Schema.ShipEntryFixedMeasure(1)];
            long saved = TD.Schema.ShipEntryFixedSave(se, buf);
            Check(saved == buf.Length, "ShipEntry save");

            TD.ShipEntry back = new TD.ShipEntry();
            TD.TableReport r = new TD.TableReport();
            TD.TableFixedEntry[] plan = new TD.TableFixedEntry[64];
            long loaded = TD.Schema.ShipEntryFixedLoad(back, buf, plan, r);
            Check(loaded == 1, "ShipEntry load one record");
            Check(r.Unknown == 0 && !r.Refused && r.Widened == 0 && !r.Damaged, "ShipEntry clean read");
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

        byte[] wire = new byte[(int)UT.Schema.UtRootFixedMeasure(1)];
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
        int utForged = 0;
        string why = UT.Schema.TableFixedWire.Run(plan.Slice(0, made), UT.Schema.UtRootFixedSlots, at.Slice(8), outVal, r, planBytes, ref utScratch, ref utForged);
        // the run's own refusal, and the forged-ordinal lane, end the read as the
        // public load does (§5.4): an out_of_range refusal, never a count
        if (r != null && (why != null || utForged != 0) && !r.Refused) { r.Refused = true; r.Reason = why ?? "out_of_range"; }
        return outVal;
    }

    static bool UtSame(UT.UtRoot x, UT.UtRoot y)
    {
        byte[] a = new byte[(int)UT.Schema.UtRootFixedMeasure(1)];
        byte[] b = new byte[(int)UT.Schema.UtRootFixedMeasure(1)];
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
        Check(!r.Refused && !r.Damaged && r.Unknown == 0,
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
        Check(!rc.Refused && !rc.Damaged,
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
            Check(UT.Schema.UtRootFixedLoad(v, bad, plan, r1) == -1 && r1.Refused,
                  "tag past the last arm: the identity path refuses the load");
            Check(v.Pick.Type == UT.UtPickType.None && v.Tail == new UT.UtRoot().Tail,
                  "tag past the last arm: the refused load leaves the declared defaults");
            Check(!r1.Damaged,
                  "tag past the last arm: a VALUE out of range is not framing damage");

            UT.TableReport r2 = new UT.TableReport();
            UT.UtRoot bent = UtCompiled(bad, r2);
            Check(bent.Pick.Type == UT.UtPickType.None,
                  "tag past the last arm: the compiled plan lands None too");
            Check(!r2.Refused,
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
            Check(UT.Schema.UtRootFixedLoad(v, bad, plan, r1) == -1 && r1.Refused && !r1.Damaged,
                  "ordinal past the last variant: the identity path refuses the load");
            Check(v.Pick.Type == UT.UtPickType.None && v.Tail == new UT.UtRoot().Tail,
                  "ordinal past the last variant: the refused load leaves the declared defaults");

            UT.TableReport r2 = new UT.TableReport();
            UtCompiled(bad, r2);
            Check(r2.Refused && !r2.Damaged,
                  "ordinal past the last variant: the plan's ordinal op refuses it too");
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

        byte[] file = new byte[(int)FX1.Schema.FxRootFixedMeasure(1)];
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
            Check(!r.Refused && !r.Damaged, "slack: a clean read moves no counter");
        }
    }

    // HashOf IS THE TEST'S OWN WRITER-SIDE HASH, and the emitted runtime's no
    // longer: cs/R7 (fixedroadmap_t12) retired TableFixedWire.HashOf, because a
    // runtime never computes a hash from layout bytes it holds (§5.3) — the
    // identity lane is an index comparison. A FORGER still has to stamp a
    // header, so the hash lives here, beside the files it forges. It is the
    // Go oracle's walk, ir.TableFixedLayoutHash: fnv1a64 over the layout bytes
    // and then the definitions digest (bill §13). A hostile layout has no
    // schema and so no digest. TestFixedHashOracleCase pins it to the oracle
    // for every fixed root this program compiles.
    static ulong HashOf(ReadOnlySpan<byte> layout, ReadOnlySpan<byte> digest = default)
    {
        ulong h = 0xcbf29ce484222325ul;
        for (int i = 0; i < layout.Length; ++i)
        {
            h ^= layout[i];
            h *= 0x100000001b3ul;
        }
        for (int i = 0; i < digest.Length; ++i)
        {
            h ^= digest[i];
            h *= 0x100000001b3ul;
        }
        return h;
    }

    // THE ORACLE PIN: every fixed root this program compiles, from
    // FixedHashOracle.cs, which TestCSharpHashOfPinsTheGoOracle writes from the
    // Go oracle and holds byte for byte. HashOf over the emitted layout and the
    // carried digest must be the oracle's number AND the compiled constant.
    static void TestFixedHashOracleCase()
    {
        Check(FixedHashOracleRoots.Length > 0, "hash oracle: the fixture carries the fixed roots");
        int bad = 0;
        foreach (var r in FixedHashOracleRoots)
        {
            ulong h = HashOf(r.Layout, r.Digest);
            if (h != r.Oracle || r.Oracle != r.Emitted)
            {
                bad++;
                Console.WriteLine("hash oracle: " + r.Root + " HashOf=0x" + h.ToString("x16") + " oracle=0x" + r.Oracle.ToString("x16") + " emitted=0x" + r.Emitted.ToString("x16"));
            }
        }
        Check(bad == 0, "hash oracle: HashOf(layout ‖ digest) is the Go oracle's hash and the compiled <Root>FixedHash for every fixed root (" + FixedHashOracleRoots.Length + ")");
        // A FLIPPED BYTE MOVES IT: the pin is not vacuous.
        var first = FixedHashOracleRoots[0];
        byte[] flipped = (byte[])first.Layout.Clone();
        flipped[flipped.Length - 1] ^= 0x01;
        Check(HashOf(flipped, first.Digest) != first.Oracle, "hash oracle: a flipped layout byte moves HashOf off the oracle");
    }

    static byte[] LayoutFileOf(byte[] layout)
    {
        byte[] f = new byte[FX1.Schema.TableFixedWire.HeaderBytes + 4 + layout.Length];
        f[0] = 3;
        BinaryPrimitives.WriteUInt64LittleEndian(f.AsSpan(FX1.Schema.TableFixedWire.HashAt), HashOf(layout));
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
        Check(r.Unknown == 0 && !r.Refused && r.Widened == 0 && !r.Damaged,
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
        byte[] good = new byte[(int)FX2.Schema.FxRootFixedMeasure(1)];
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

        byte[] buf = new byte[(int)TD.Schema.WeaponConfigFixedMeasure(1)];
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
        Check(r.Unknown == 0 && !r.Refused && r.Widened == 0 && !r.Damaged,
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

        byte[] w = new byte[(int)P3.Schema.ChainFixedMeasure(1)];
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
            Check(!r.Refused && !r.Damaged,
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
            byte[] pw = new byte[(int)P3.Schema.ChainFixedMeasure(1)];
            Check(P3.Schema.ChainFixedSave(present, pw) == pw.Length,
                  "absent optional: the present twin saves");
            Span<byte> pbody = pw.AsSpan(bodyOffset, bodyBytes);
            Check(pbody.IndexOf((byte)0x5A) >= 0,
                  "absent optional: the present twin puts the bytes on the wire");
        }
    }
    // The sixteen valid-data rows. Each one writes a value and reads it back
    // through the strict public FixedLoad. The names are the evidence the
    // roadmap cells cite.
    static void TestFixedValidDataRows()
    {
        TestFixedValidDataBool();
        TestFixedValidDataSigned128();
        TestFixedValidDataUnsigned128();
        TestFixedValidDataRangedIntegers();
        TestFixedValidDataSignedFixedPoint();
        TestFixedValidDataUnsignedFixedPoint();
        TestFixedValidDataEnums();
        TestFixedValidDataUtf8();
        TestFixedValidDataByteBuffers();
        TestFixedValidDataNested();
        TestFixedValidDataFixedArrays();
        TestFixedValidDataCountedArrays();
        TestFixedValidDataUnions();
        TestFixedValidDataOptionalNesting();
        TestFixedValidDataFileRoundtrip();
        TestFixedValidDataRecordMeasurement();
    }

    static void TestFixedValidDataBool()
    {
        const string id = "bool-values/cs/valid-data";
        const bool validDataBoolExpect = true;

        TD.WeaponConfig one = new TD.WeaponConfig();
        TD.Schema.TableReset(one);
        one.Homing = true;
        byte[] saved = new byte[(int)TD.Schema.WeaponConfigFixedMeasure(1)];
        Check(TD.Schema.WeaponConfigFixedSave(one, saved) == saved.Length, id + ": true saves");

        TD.WeaponConfig back = new TD.WeaponConfig();
        TD.Schema.TableReset(back);
        back.Homing = false;
        TD.TableReport report = new TD.TableReport();
        long n = TD.Schema.WeaponConfigFixedLoad(back, saved, new TD.TableFixedEntry[64], report);
        Check(n == 1 && report.Unknown == 0 && !report.Refused && report.Widened == 0 && !report.Damaged,
              id + ": true loads clean");
        Check(back.Homing == validDataBoolExpect, id + ": true reads back true");

        TD.WeaponConfig zero = new TD.WeaponConfig();
        TD.Schema.TableReset(zero);
        zero.Homing = false;
        byte[] zsaved = new byte[(int)TD.Schema.WeaponConfigFixedMeasure(1)];
        Check(TD.Schema.WeaponConfigFixedSave(zero, zsaved) == zsaved.Length, id + ": false saves");

        TD.WeaponConfig zback = new TD.WeaponConfig();
        TD.Schema.TableReset(zback);
        zback.Homing = true;
        TD.TableReport zreport = new TD.TableReport();
        long zn = TD.Schema.WeaponConfigFixedLoad(zback, zsaved, new TD.TableFixedEntry[64], zreport);
        Check(zn == 1 && zreport.Unknown == 0 && !zreport.Refused && zreport.Widened == 0 && !zreport.Damaged,
              id + ": false loads clean");
        Check(zback.Homing == false, id + ": false reads back false");
    }

    static void TestFixedValidDataSigned128()
    {
        const string id = "signed-128/cs/valid-data";
        S2.SimState one = new S2.SimState();
        S2.Schema.TableReset(one);
        one.Energy = -1234567890L;
        byte[] saved = new byte[(int)S2.Schema.SimStateFixedMeasure(1)];
        Check(S2.Schema.SimStateFixedSave(one, saved) == saved.Length, id + ": saves");

        S2.SimState back = new S2.SimState();
        S2.Schema.TableReset(back);
        back.Energy = 0;
        S2.TableReport report = new S2.TableReport();
        long n = S2.Schema.SimStateFixedLoad(back, saved, new S2.TableFixedEntry[256], report);
        Check(n == 1 && report.Unknown == 0 && !report.Refused && report.Widened == 0 && !report.Damaged,
              id + ": loads clean");
        Check(back.Energy == -1234567890L, id + ": int128 reads back");
    }

    static void TestFixedValidDataUnsigned128()
    {
        const string id = "unsigned-128/cs/valid-data";
        S2.SimState one = new S2.SimState();
        S2.Schema.TableReset(one);
        one.SeedsCount = 2;
        one.Seeds[0] = 1;
        one.Seeds[1] = 9999999999999999999UL;
        byte[] saved = new byte[(int)S2.Schema.SimStateFixedMeasure(1)];
        Check(S2.Schema.SimStateFixedSave(one, saved) == saved.Length, id + ": saves");

        S2.SimState back = new S2.SimState();
        S2.Schema.TableReset(back);
        back.SeedsCount = 0;
        back.Seeds[0] = 0;
        back.Seeds[1] = 0;
        S2.TableReport report = new S2.TableReport();
        long n = S2.Schema.SimStateFixedLoad(back, saved, new S2.TableFixedEntry[256], report);
        Check(n == 1 && report.Unknown == 0 && !report.Refused && report.Widened == 0 && !report.Damaged,
              id + ": loads clean");
        Check(back.SeedsCount == 2 && back.Seeds[0] == 1 && back.Seeds[1] == 9999999999999999999UL,
              id + ": uint128 reads back");
    }

    static void TestFixedValidDataRangedIntegers()
    {
        const string id = "ranged-integers/cs/valid-data";
        TD.RangedSigned one = new TD.RangedSigned();
        TD.Schema.TableReset(one);
        one.I8Inside = 5;
        one.I16Inside = -100;
        one.I32Inside = 42;
        one.I64Inside = -1000L;
        byte[] saved = new byte[(int)TD.Schema.RangedSignedFixedMeasure(1)];
        Check(TD.Schema.RangedSignedFixedSave(one, saved) == saved.Length, id + ": saves");

        TD.RangedSigned back = new TD.RangedSigned();
        TD.Schema.TableReset(back);
        back.I8Inside = 0;
        back.I16Inside = 0;
        back.I32Inside = 0;
        back.I64Inside = 0;
        TD.TableReport report = new TD.TableReport();
        long n = TD.Schema.RangedSignedFixedLoad(back, saved, new TD.TableFixedEntry[256], report);
        Check(n == 1 && report.Unknown == 0 && !report.Refused && report.Widened == 0 && !report.Damaged,
              id + ": loads clean");
        Check(back.I8Inside == 5 && back.I16Inside == -100 && back.I32Inside == 42 && back.I64Inside == -1000L,
              id + ": in-range integers read back");
    }

    static void TestFixedValidDataSignedFixedPoint()
    {
        const string id = "signed-fixed-point/cs/valid-data";
        S2.SimState one = new S2.SimState();
        S2.Schema.TableReset(one);
        one.Tilt = 16;
        one.Position = 1L << 16;
        one.Reach = -42;
        byte[] saved = new byte[(int)S2.Schema.SimStateFixedMeasure(1)];
        Check(S2.Schema.SimStateFixedSave(one, saved) == saved.Length, id + ": saves");

        S2.SimState back = new S2.SimState();
        S2.Schema.TableReset(back);
        back.Tilt = 0;
        back.Position = 0;
        back.Reach = 0;
        S2.TableReport report = new S2.TableReport();
        long n = S2.Schema.SimStateFixedLoad(back, saved, new S2.TableFixedEntry[256], report);
        Check(n == 1 && report.Unknown == 0 && !report.Refused && report.Widened == 0 && !report.Damaged,
              id + ": loads clean");
        Check(back.Tilt == 16 && back.Position == (1L << 16) && back.Reach == -42,
              id + ": raw fixed values read back");
    }

    static void TestFixedValidDataUnsignedFixedPoint()
    {
        const string id = "unsigned-fixed-point/cs/valid-data";
        S2.SimState one = new S2.SimState();
        S2.Schema.TableReset(one);
        one.Ratio = 16;
        one.Speed = 2u << 16;
        one.Mass = 100;
        byte[] saved = new byte[(int)S2.Schema.SimStateFixedMeasure(1)];
        Check(S2.Schema.SimStateFixedSave(one, saved) == saved.Length, id + ": saves");

        S2.SimState back = new S2.SimState();
        S2.Schema.TableReset(back);
        back.Ratio = 0;
        back.Speed = 0;
        back.Mass = 0;
        S2.TableReport report = new S2.TableReport();
        long n = S2.Schema.SimStateFixedLoad(back, saved, new S2.TableFixedEntry[256], report);
        Check(n == 1 && report.Unknown == 0 && !report.Refused && report.Widened == 0 && !report.Damaged,
              id + ": loads clean");
        Check(back.Ratio == 16 && back.Speed == (2u << 16) && back.Mass == 100,
              id + ": raw ufixed values read back");
    }

    static void TestFixedValidDataEnums()
    {
        const string id = "enum-values/cs/valid-data";

        TD.LoadoutConfig none = new TD.LoadoutConfig();
        TD.Schema.TableReset(none);
        none.Grade = TD.Grade.None;
        byte[] noneSaved = new byte[(int)TD.Schema.LoadoutConfigFixedMeasure(1)];
        Check(TD.Schema.LoadoutConfigFixedSave(none, noneSaved) == noneSaved.Length, id + ": None saves");
        TD.LoadoutConfig noneBack = new TD.LoadoutConfig();
        TD.Schema.TableReset(noneBack);
        noneBack.Grade = TD.Grade.Gold;
        TD.TableReport noneReport = new TD.TableReport();
        long nn = TD.Schema.LoadoutConfigFixedLoad(noneBack, noneSaved, new TD.TableFixedEntry[256], noneReport);
        Check(nn == 1 && noneReport.Unknown == 0 && !noneReport.Refused && noneReport.Widened == 0 && !noneReport.Damaged,
              id + ": None loads clean");
        Check(noneBack.Grade == TD.Grade.None, id + ": None reads back");

        TD.LoadoutConfig gold = new TD.LoadoutConfig();
        TD.Schema.TableReset(gold);
        gold.Grade = TD.Grade.Gold;
        byte[] goldSaved = new byte[(int)TD.Schema.LoadoutConfigFixedMeasure(1)];
        Check(TD.Schema.LoadoutConfigFixedSave(gold, goldSaved) == goldSaved.Length, id + ": Gold saves");
        TD.LoadoutConfig goldBack = new TD.LoadoutConfig();
        TD.Schema.TableReset(goldBack);
        goldBack.Grade = TD.Grade.None;
        TD.TableReport goldReport = new TD.TableReport();
        long gn = TD.Schema.LoadoutConfigFixedLoad(goldBack, goldSaved, new TD.TableFixedEntry[256], goldReport);
        Check(gn == 1 && goldReport.Unknown == 0 && !goldReport.Refused && goldReport.Widened == 0 && !goldReport.Damaged,
              id + ": Gold loads clean");
        Check(goldBack.Grade == TD.Grade.Gold, id + ": Gold reads back");
    }

    static void TestFixedValidDataUtf8()
    {
        const string id = "utf8-values/cs/valid-data";
        byte[] label = new byte[] { 0x63, 0x61, 0x66, 0xC3, 0xA9 };
        FX1.FxRoot one = new FX1.FxRoot();
        FX1.Schema.TableReset(one);
        label.CopyTo(one.Label.AsSpan());
        one.LabelLength = label.Length;
        byte[] saved = new byte[(int)FX1.Schema.FxRootFixedMeasure(1)];
        Check(FX1.Schema.FxRootFixedSave(one, saved) == saved.Length, id + ": saves");

        FX1.FxRoot back = new FX1.FxRoot();
        FX1.Schema.TableReset(back);
        Array.Fill(back.Label, (byte)0xFF);
        back.LabelLength = 0;
        FX1.TableReport report = new FX1.TableReport();
        long n = FX1.Schema.FxRootFixedLoad(back, saved, new FX1.TableFixedEntry[1024], report);
        Check(n == 1 && report.Unknown == 0 && !report.Refused && report.Widened == 0 && !report.Damaged,
              id + ": loads clean");
        Check(back.LabelLength == label.Length && back.Label.AsSpan(0, label.Length).SequenceEqual(label),
              id + ": utf-8 bytes read back");
        Check(Encoding.UTF8.GetString(back.Label.AsSpan(0, back.LabelLength)) == "caf\u00e9",
              id + ": utf-8 decodes");
    }

    static void TestFixedValidDataByteBuffers()
    {
        const string id = "byte-buffers/cs/valid-data";
        byte[] blob = new byte[] { 1, 2, 3, 4, 5, 6 };
        FX1.FxRoot one = new FX1.FxRoot();
        FX1.Schema.TableReset(one);
        blob.CopyTo(one.Blob.AsSpan());
        one.BlobLength = blob.Length;
        byte[] saved = new byte[(int)FX1.Schema.FxRootFixedMeasure(1)];
        Check(FX1.Schema.FxRootFixedSave(one, saved) == saved.Length, id + ": saves");

        FX1.FxRoot back = new FX1.FxRoot();
        FX1.Schema.TableReset(back);
        Array.Fill(back.Blob, (byte)0xFF);
        back.BlobLength = 0;
        FX1.TableReport report = new FX1.TableReport();
        long n = FX1.Schema.FxRootFixedLoad(back, saved, new FX1.TableFixedEntry[1024], report);
        Check(n == 1 && report.Unknown == 0 && !report.Refused && report.Widened == 0 && !report.Damaged,
              id + ": loads clean");
        Check(back.BlobLength == 6 && back.Blob.AsSpan(0, 6).SequenceEqual(blob),
              id + ": the six bytes read back");
    }

    static void TestFixedValidDataNested()
    {
        const string id = "nested-fixed-tables/cs/valid-data";
        FX1.FxRoot one = new FX1.FxRoot();
        FX1.Schema.TableReset(one);
        one.Nested.A = 111;
        one.Nested.B = 222;
        byte[] saved = new byte[(int)FX1.Schema.FxRootFixedMeasure(1)];
        Check(FX1.Schema.FxRootFixedSave(one, saved) == saved.Length, id + ": saves");

        FX1.FxRoot back = new FX1.FxRoot();
        FX1.Schema.TableReset(back);
        back.Nested.A = 0;
        back.Nested.B = 0;
        FX1.TableReport report = new FX1.TableReport();
        long n = FX1.Schema.FxRootFixedLoad(back, saved, new FX1.TableFixedEntry[1024], report);
        Check(n == 1 && report.Unknown == 0 && !report.Refused && report.Widened == 0 && !report.Damaged,
              id + ": loads clean");
        Check(back.Nested.A == 111 && back.Nested.B == 222, id + ": the nested fields read back");
    }

    static void TestFixedValidDataFixedArrays()
    {
        const string id = "fixed-arrays/cs/valid-data";
        TD.LoadoutConfig one = new TD.LoadoutConfig();
        TD.Schema.TableReset(one);
        one.Podium[0] = TD.Grade.Gold;
        one.Podium[1] = TD.Grade.Silver;
        one.Podium[2] = TD.Grade.Bronze;
        byte[] saved = new byte[(int)TD.Schema.LoadoutConfigFixedMeasure(1)];
        Check(TD.Schema.LoadoutConfigFixedSave(one, saved) == saved.Length, id + ": saves");

        TD.LoadoutConfig back = new TD.LoadoutConfig();
        TD.Schema.TableReset(back);
        back.Podium[0] = TD.Grade.None;
        back.Podium[1] = TD.Grade.None;
        back.Podium[2] = TD.Grade.None;
        TD.TableReport report = new TD.TableReport();
        long n = TD.Schema.LoadoutConfigFixedLoad(back, saved, new TD.TableFixedEntry[256], report);
        Check(n == 1 && report.Unknown == 0 && !report.Refused && report.Widened == 0 && !report.Damaged,
              id + ": loads clean");
        Check(back.Podium[0] == TD.Grade.Gold && back.Podium[1] == TD.Grade.Silver && back.Podium[2] == TD.Grade.Bronze,
              id + ": the fixed array reads back");
    }

    static void TestFixedValidDataCountedArrays()
    {
        const string id = "counted-arrays/cs/valid-data";
        TD.LoadoutConfig one = new TD.LoadoutConfig();
        TD.Schema.TableReset(one);
        one.GradesCount = 3;
        one.Grades[0] = TD.Grade.Bronze;
        one.Grades[1] = TD.Grade.Gold;
        one.Grades[2] = TD.Grade.Silver;
        byte[] saved = new byte[(int)TD.Schema.LoadoutConfigFixedMeasure(1)];
        Check(TD.Schema.LoadoutConfigFixedSave(one, saved) == saved.Length, id + ": saves");

        TD.LoadoutConfig back = new TD.LoadoutConfig();
        TD.Schema.TableReset(back);
        back.GradesCount = 0;
        back.Grades[0] = TD.Grade.None;
        back.Grades[1] = TD.Grade.None;
        back.Grades[2] = TD.Grade.None;
        TD.TableReport report = new TD.TableReport();
        long n = TD.Schema.LoadoutConfigFixedLoad(back, saved, new TD.TableFixedEntry[256], report);
        Check(n == 1 && report.Unknown == 0 && !report.Refused && report.Widened == 0 && !report.Damaged,
              id + ": loads clean");
        Check(back.GradesCount == 3 && back.Grades[0] == TD.Grade.Bronze && back.Grades[1] == TD.Grade.Gold && back.Grades[2] == TD.Grade.Silver,
              id + ": the counted array reads back");
    }

    static void TestFixedValidDataUnions()
    {
        const string id = "union-values/cs/valid-data";

        UT.UtRoot armB = new UT.UtRoot();
        UT.Schema.TableReset(armB);
        armB.Pick.Type = UT.UtPickType.B;
        byte[] label = Encoding.UTF8.GetBytes("hello");
        label.CopyTo(armB.Pick.B.Label.AsSpan());
        armB.Pick.B.LabelLength = label.Length;
        armB.Pick.B.Grade = UT.UtGrade.gold;
        armB.Pick.B.M = 42;
        armB.Tail = 11;
        byte[] savedB = new byte[(int)UT.Schema.UtRootFixedMeasure(1)];
        Check(UT.Schema.UtRootFixedSave(armB, savedB) == savedB.Length, id + ": arm B saves");
        UT.UtRoot backB = new UT.UtRoot();
        UT.Schema.TableReset(backB);
        backB.Pick.Type = UT.UtPickType.None;
        backB.Tail = 0;
        UT.TableReport reportB = new UT.TableReport();
        long nb = UT.Schema.UtRootFixedLoad(backB, savedB, new UT.TableFixedEntry[512], reportB);
        Check(nb == 1 && reportB.Unknown == 0 && !reportB.Refused && reportB.Widened == 0 && !reportB.Damaged,
              id + ": arm B loads clean");
        Check(backB.Pick.Type == UT.UtPickType.B && backB.Pick.B.Grade == UT.UtGrade.gold && backB.Pick.B.M == 42 && backB.Tail == 11,
              id + ": arm B reads back");
        Check(backB.Pick.B.LabelLength == label.Length && backB.Pick.B.Label.AsSpan(0, label.Length).SequenceEqual(label),
              id + ": arm B text reads back");

        UT.UtRoot armA = new UT.UtRoot();
        UT.Schema.TableReset(armA);
        armA.Pick.Type = UT.UtPickType.A;
        armA.Pick.A.N = 11;
        armA.Tail = 9;
        byte[] savedA = new byte[(int)UT.Schema.UtRootFixedMeasure(1)];
        Check(UT.Schema.UtRootFixedSave(armA, savedA) == savedA.Length, id + ": arm A saves");
        UT.UtRoot backA = new UT.UtRoot();
        UT.Schema.TableReset(backA);
        backA.Pick.Type = UT.UtPickType.B;
        backA.Pick.A.N = 0;
        backA.Tail = 0;
        UT.TableReport reportA = new UT.TableReport();
        long na = UT.Schema.UtRootFixedLoad(backA, savedA, new UT.TableFixedEntry[512], reportA);
        Check(na == 1 && reportA.Unknown == 0 && !reportA.Refused && reportA.Widened == 0 && !reportA.Damaged,
              id + ": arm A loads clean");
        Check(backA.Pick.Type == UT.UtPickType.A && backA.Pick.A.N == 11 && backA.Tail == 9,
              id + ": arm A reads back");
    }

    static void TestFixedValidDataOptionalNesting()
    {
        const string id = "optional-nesting/cs/valid-data";
        byte[] name = Encoding.UTF8.GetBytes("chain");
        byte[] tag = Encoding.UTF8.GetBytes("nest");

        P3.Chain present = new P3.Chain();
        P3.Schema.TableReset(present);
        name.CopyTo(present.Name.AsSpan());
        present.NameLength = name.Length;
        present.LinkPresent = true;
        present.Link.Value = 321;
        tag.CopyTo(present.Link.Tag.AsSpan());
        present.Link.TagLength = tag.Length;
        byte[] saved = new byte[(int)P3.Schema.ChainFixedMeasure(1)];
        Check(P3.Schema.ChainFixedSave(present, saved) == saved.Length, id + ": present saves");

        P3.Chain back = new P3.Chain();
        P3.Schema.TableReset(back);
        back.LinkPresent = false;
        back.Link.Value = 999;
        back.NameLength = 0;
        P3.TableReport report = new P3.TableReport();
        long n = P3.Schema.ChainFixedLoad(back, saved, new P3.TableFixedEntry[256], report);
        Check(n == 1 && report.Unknown == 0 && !report.Refused && report.Widened == 0 && !report.Damaged,
              id + ": present loads clean");
        Check(back.LinkPresent && back.Link.Value == 321 && back.Link.TagLength == tag.Length
              && back.Link.Tag.AsSpan(0, tag.Length).SequenceEqual(tag)
              && back.NameLength == name.Length && back.Name.AsSpan(0, name.Length).SequenceEqual(name),
              id + ": the present link reads back");

        P3.Chain absent = new P3.Chain();
        P3.Schema.TableReset(absent);
        name.CopyTo(absent.Name.AsSpan());
        absent.NameLength = name.Length;
        absent.LinkPresent = false;
        byte[] asaved = new byte[(int)P3.Schema.ChainFixedMeasure(1)];
        Check(P3.Schema.ChainFixedSave(absent, asaved) == asaved.Length, id + ": absent saves");
        P3.Chain aback = new P3.Chain();
        P3.Schema.TableReset(aback);
        aback.LinkPresent = true;
        aback.Link.Value = 7;
        P3.TableReport areport = new P3.TableReport();
        long an = P3.Schema.ChainFixedLoad(aback, asaved, new P3.TableFixedEntry[256], areport);
        Check(an == 1 && areport.Unknown == 0 && !areport.Refused && areport.Widened == 0 && !areport.Damaged,
              id + ": absent loads clean");
        Check(!aback.LinkPresent, id + ": an absent link reads back absent");
    }

    static void TestFixedValidDataFileRoundtrip()
    {
        const string id = "fixed-file-roundtrip/cs/valid-data";
        FX1.FxRoot one = new FX1.FxRoot();
        FX1.Schema.TableReset(one);
        one.Keep = 4242u;
        one.Narrow = 40000;
        one.Renamed = 321;
        one.Gone = 654;
        one.Nested.A = 111;
        one.Nested.B = 222;
        byte[] label = Encoding.UTF8.GetBytes("round");
        label.CopyTo(one.Label.AsSpan());
        one.LabelLength = label.Length;
        one.MarksCount = 2;
        one.Marks[0] = 7;
        one.Marks[1] = 8;
        one.BlobLength = 3;
        one.Blob[0] = 0xDE;
        one.Blob[1] = 0xAD;
        one.Blob[2] = 0xBE;

        byte[] first = new byte[(int)FX1.Schema.FxRootFixedMeasure(1)];
        Check(FX1.Schema.FxRootFixedSave(one, first) == first.Length, id + ": the first save fills the measure");

        FX1.FxRoot back = new FX1.FxRoot();
        FX1.Schema.TableReset(back);
        FX1.TableReport report = new FX1.TableReport();
        long n = FX1.Schema.FxRootFixedLoad(back, first, new FX1.TableFixedEntry[1024], report);
        Check(n == 1 && report.Unknown == 0 && !report.Refused && report.Widened == 0 && !report.Damaged,
              id + ": the load is clean");
        Check(back.Keep == 4242u && back.Narrow == 40000 && back.Renamed == 321 && back.Gone == 654
              && back.Nested.A == 111 && back.Nested.B == 222
              && back.LabelLength == label.Length && back.MarksCount == 2 && back.Marks[0] == 7 && back.Marks[1] == 8
              && back.BlobLength == 3 && back.Blob[0] == 0xDE && back.Blob[1] == 0xAD && back.Blob[2] == 0xBE,
              id + ": the fields read back");

        byte[] second = new byte[first.Length];
        Check(FX1.Schema.FxRootFixedSave(back, second) == second.Length, id + ": the second save fills the measure");
        Check(first.AsSpan().SequenceEqual(second), id + ": save, load, save produces the same bytes");
    }

    static void TestFixedValidDataRecordMeasurement()
    {
        const string id = "record-measurement/cs/valid-data";
        long m0 = FX1.Schema.FxRootFixedMeasure(0);
        long m1 = FX1.Schema.FxRootFixedMeasure(1);
        long m2 = FX1.Schema.FxRootFixedMeasure(2);
        Check(m1 > m0 && m2 > m1, id + ": measure grows with the record count");
        Check(m1 - m0 == m2 - m1, id + ": each record adds the same number of bytes");
        Check(m1 - m0 == FX1.Schema.FxRootFixedRecordBytes, id + ": the step is the record size");
        Check(FX1.Schema.FxRootFixedRecordBytes == 8 + FX1.Schema.FxRootFixedBodyBytes, id + ": the record is the header word plus the body");
        Check(FX1.Schema.FxRootFixedBodyBytes > 0, id + ": the body has a size");

        FX1.FxRoot a = new FX1.FxRoot();
        FX1.Schema.TableReset(a);
        a.Keep = 1u;
        FX1.FxRoot b = new FX1.FxRoot();
        FX1.Schema.TableReset(b);
        b.Keep = 2u;
        byte[] wa = new byte[(int)m1];
        byte[] wb = new byte[(int)m1];
        Check(FX1.Schema.FxRootFixedSave(a, wa) == m1, id + ": one record saves to measure(1)");
        Check(FX1.Schema.FxRootFixedSave(b, wb) == m1, id + ": a different record saves to the same measure(1)");
        Check(!wa.AsSpan().SequenceEqual(wb), id + ": the two records are not the same bytes");
    }

}
