// Storage companions are a programmer contract in C#: Debug.Assert catches
// them before the writer indexes the declared storage (SPEC §5.1).
using System;
using System.Diagnostics;
using Storagerelease;

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
    private static bool Asserted(string name, Action write)
    {
        try { write(); }
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
        Console.WriteLine("FAIL storage debug: " + name + " did not assert");
        return false;
    }

    private static int Main()
    {
        Trace.Listeners.Clear();
        Trace.Listeners.Add(new AssertionListener());
        var count = new Root { ItemsCount = 9 }; // bound 8
        var length = new Root { NameLength = 17 }; // bound 16
        bool ok = Asserted("count", () => Schema.RootMeasureMessages(new[] { count }))
               & Asserted("length", () => Schema.RootMeasureMessages(new[] { length }));
        if (ok) { Console.WriteLine("storage debug gate: count and length assertions fired"); }
        return ok ? 0 : 1;
    }
}
