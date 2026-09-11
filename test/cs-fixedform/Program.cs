// TEXT UNDER A UNION ARM (docs/SPEC-TABLES.md §3.4, §15) — test/tables/FU1
// and FU2. FU1 writes a string(8) in the union's SECOND arm. FU2 appends extra
// so a read of those bytes is a compiled plan. One path: FuRootFixedLoad.
// Hash chooses the plan.

using System;
using System.Text;
using FU1 = Tblfu1;
using FU2 = Tblfu2;

static class Program
{
    static bool failed;

    static void Check(bool ok, string what)
    {
        if (!ok)
        {
            Console.WriteLine("FAILED: " + what);
            failed = true;
        }
    }

    static int Main()
    {
        FU1.FuRoot labelled = new FU1.FuRoot();
        FU1.Schema.TableReset(labelled);
        labelled.Flag = true;
        labelled.NotePresent = true;
        labelled.Note = 44;
        labelled.Pick.Type = FU1.PickType.Labelled;
        labelled.Pick.Labelled.Lead = 101;
        byte[] hello = Encoding.UTF8.GetBytes("hello");
        hello.CopyTo(labelled.Pick.Labelled.Label.AsSpan());
        labelled.Pick.Labelled.LabelLength = 5;
        labelled.Pick.Labelled.Trail = 202;
        labelled.Tail = 11;

        FU1.FuRoot plain = new FU1.FuRoot();
        FU1.Schema.TableReset(plain);
        plain.Pick.Type = FU1.PickType.Plain;
        plain.Pick.Plain.N = 303;
        plain.Tail = 12;

        FU1.FuRoot[] older = { labelled, plain };
        byte[] file = new byte[FU1.Schema.FuRootFixedMeasure(2)];
        Check(FU1.Schema.FuRootFixedSave(older, file) == file.Length,
              "C# text under an arm: FU1 writes its two records");
        Check(FU1.Schema.FuRootFixedHash != FU2.Schema.FuRootFixedHash,
              "C# text under an arm: FU2 appends extra, so the layout hash differs");

        FU1.FuRoot[] mine = new FU1.FuRoot[2];
        FU1.TableReport r = new FU1.TableReport();
        FU1.TableFixedEntry[] plan = new FU1.TableFixedEntry[1024];
        Check(FU1.Schema.FuRootFixedLoad(mine, file, plan, r) == 2,
              "C# text under an arm: the identity read takes both records");
        Check(mine[0].Pick.Type == FU1.PickType.Labelled,
              "C# text under an arm: identity, the SECOND arm");
        Check(mine[0].Pick.Labelled.Lead == 101,
              "C# text under an arm: identity, the scalar BEFORE the text");
        Check(mine[0].Pick.Labelled.LabelLength == 5 &&
              Encoding.UTF8.GetString(mine[0].Pick.Labelled.Label.AsSpan(0, 5)) == "hello",
              "C# text under an arm: the IDENTITY read lands the text");
        Check(mine[0].Pick.Labelled.Trail == 202,
              "C# text under an arm: identity, the scalar AFTER the text");
        Check(mine[0].Flag && mine[0].NotePresent && mine[0].Note == 44 && mine[0].Tail == 11,
              "C# text under an arm: identity, the rest of the labelled record");
        Check(mine[1].Pick.Type == FU1.PickType.Plain && mine[1].Pick.Plain.N == 303 && mine[1].Tail == 12,
              "C# text under an arm: identity, the FIRST arm as well");
        Check(r.Clamped == 0 && !r.Malformed && !r.Refused,
              "C# text under an arm: identity, a clean read moves no counter");

        FU2.FuRoot[] theirs = new FU2.FuRoot[2];
        FU2.TableReport r2 = new FU2.TableReport();
        FU2.TableFixedEntry[] plan2 = new FU2.TableFixedEntry[1024];
        Check(FU2.Schema.FuRootFixedLoad(theirs, file, plan2, r2) == 2,
              "C# text under an arm: the compiled read takes both records");
        Check(theirs[0].Pick.Type == FU2.PickType.Labelled,
              "C# text under an arm: compiled, the SECOND arm");
        Check(theirs[0].Pick.Labelled.Lead == 101,
              "C# text under an arm: compiled, the scalar BEFORE the text");
        Check(theirs[0].Pick.Labelled.LabelLength == 5 &&
              Encoding.UTF8.GetString(theirs[0].Pick.Labelled.Label.AsSpan(0, 5)) == "hello",
              "C# text under an arm: the COMPILED read still sees the text");
        Check(theirs[0].Pick.Labelled.Trail == 202,
              "C# text under an arm: compiled, the scalar AFTER the text");
        Check(theirs[0].Flag && theirs[0].NotePresent && theirs[0].Note == 44 && theirs[0].Tail == 11,
              "C# text under an arm: compiled, the rest of the labelled record");
        Check(theirs[0].Extra == 11,
              "C# text under an arm: the field FU1 does not carry took its declared default");
        Check(theirs[1].Pick.Type == FU2.PickType.Plain && theirs[1].Pick.Plain.N == 303 && theirs[1].Tail == 12,
              "C# text under an arm: compiled, the FIRST arm as well");
        Check(theirs[1].Extra == 11,
              "C# text under an arm: compiled, extra defaults on the FIRST arm too");
        Check(r2.Clamped == 0 && !r2.Malformed && !r2.Refused,
              "C# text under an arm: compiled, a clean read moves no counter");

        if (failed)
        {
            Console.WriteLine("cs fixedform FU1/FU2: FAILED");
            return 1;
        }
        Console.WriteLine("cs fixedform FU1/FU2 passed");
        return 0;
    }
}
