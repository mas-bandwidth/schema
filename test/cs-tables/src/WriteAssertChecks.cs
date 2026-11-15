// C# IS AN ASSERT LEG (docs/SPEC.md §5.1): every write check is a
// System.Diagnostics.Debug.Assert, compiled out of Release, and correct writes
// are the programmer's job. The Debug leg routes assertion failures through a
// listener, as write-refusal-storage-debug-cs does, so a case can observe the
// assert a write of an off-contract value must fire. A Release build carries
// no write check, so there such a write is the caller's error and the leg does
// not drive it. Every read check is untouched and runs in both builds.
using System;
using System.Diagnostics;

sealed class WriteAssertion : Exception
{
    public WriteAssertion(string? message) : base(message) { }
}

sealed class WriteAssertListener : TraceListener
{
    public override void Write(string? message) { }
    public override void WriteLine(string? message) { }
    public override void Fail(string? message) { throw new WriteAssertion(message); }
    public override void Fail(string? message, string? detailMessage) { throw new WriteAssertion(message); }
}

static partial class Program
{
    // Installed once, first thing in Main: an assert no case expects then
    // fails the leg as an unhandled WriteAssertion, as it would by default.
    static void InstallWriteAssertListener()
    {
#if DEBUG
        Trace.Listeners.Clear();
        Trace.Listeners.Add(new WriteAssertListener());
#endif
    }

    // In Debug, `write` must fire a write-side Debug.Assert; in Release the
    // case is not run (the write is off the contract and nothing checks it).
    static void CheckAsserts(Action write, string what)
    {
#if DEBUG
        try { write(); }
        catch (WriteAssertion) { return; }
        Check(false, what + ": no debug assert");
#endif
    }
}
