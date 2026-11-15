// Write checks are a programmer contract in C#: Debug.Assert catches
// them (SPEC §5.1): the storage companions before the writer indexes the
// declared storage, and each value check the table writers carry.
using System;
using System.Diagnostics;
using Storagerelease;
using W = Writechecks;

internal sealed class StorageAssertion : Exception { }

internal sealed class AssertionListener : TraceListener
{
    public override void Write(string? message) { }
    public override void WriteLine(string? message) { }
    public override void Fail(string? message) { throw new StorageAssertion(); }
    public override void Fail(string? message, string? detailMessage) { throw new StorageAssertion(); }
}

internal static class StorageDebug
{
    private static bool Asserted(string name, Func<long> write)
    {
        long answer;
        try { answer = write(); }
        catch (StorageAssertion)
        {
            Console.WriteLine("asserted: " + name);
            return true;
        }
        catch (Exception e)
        {
            Console.WriteLine("FAIL storage debug: " + name + " raised " + e.GetType().Name + " instead of an assert");
            return false;
        }
        Console.WriteLine("FAIL storage debug: " + name + " did not assert (the writer answered " + answer + ")");
        return false;
    }

    // The region Save's graph walk: a count past its bound (2) in a builder's
    // native row.
    private static unsafe long RegionCount()
    {
        using (var builder = new W.GraphBuilder())
        {
            builder.GetRoot()->ItemsCount = 3;
            return builder.Save(new byte[256]);
        }
    }

    // A Probe every check admits: x is required on [0.25, 1].
    private static W.Probe Valid() { return new W.Probe { X = 0.5f }; }

    private static int Main()
    {
        Trace.Listeners.Clear();
        Trace.Listeners.Add(new AssertionListener());
        var count = new Root { ItemsCount = 9 }; // bound 8
        var length = new Root { NameLength = 17 }; // bound 16
        bool ok = Asserted("count", () => Schema.RootMeasureMessages(new[] { count }))
               & Asserted("length", () => Schema.RootMeasureMessages(new[] { length }));

        // The value checks (docs/SPEC.md §5.1 rows 5-8 and 10), each a
        // Debug.Assert on this leg and nothing in a Release build.
        byte[] buffer = new byte[1024];
        W.Probe clean = Valid();
        if (W.Schema.ProbeMeasure(clean) <= 0) { Console.WriteLine("FAIL storage debug: the valid probe does not write"); ok = false; }
        W.Probe nan = Valid(); nan.V = float.NaN;
        ok &= Asserted("finite", () => W.Schema.ProbeMeasure(nan));
        ok &= Asserted("cook finite", () => W.Schema.ProbeCookMeasure(nan));
        ok &= Asserted("fixed finite", () => W.Schema.FlatFixedSave(new W.Flat { V = float.PositiveInfinity }, buffer));
        W.Probe grade = Valid(); grade.Grade = (W.Grade)9;
        ok &= Asserted("enum", () => W.Schema.ProbeSave(grade, buffer));
        W.Probe tag = Valid(); tag.Effect.Type = (W.EffectType)9;
        ok &= Asserted("union tag", () => W.Schema.ProbeSave(tag, buffer));
        W.Probe text = Valid(); text.Name[0] = 0xff; text.NameLength = 1;
        ok &= Asserted("text", () => W.Schema.ProbeSave(text, buffer));
        W.Probe element = Valid(); element.LabelsCount = 1; element.Labels[0].ValueLength = -1;
        ok &= Asserted("text-list length", () => W.Schema.ProbeSave(element, buffer));
        W.Probe total = Valid(); total.LabelsCount = 2; total.Labels[0].ValueLength = 3; total.Labels[1].ValueLength = 3; // total=5
        ok &= Asserted("text-list total", () => W.Schema.ProbeSave(total, buffer));
        W.Probe range = Valid(); range.X = 2.0f;
        ok &= Asserted("required range", () => W.Schema.ProbeSave(range, buffer));
        ok &= Asserted("region count", RegionCount);
        if (ok) { Console.WriteLine("storage debug gate: every storage and value write assertion fired"); }
        return ok ? 0 : 1;
    }
}
