using System;
using System.Text;
using U = Csunions;

static partial class Program
{
    static void TestUnionContracts()
    {
        U.Root value = new U.Root(); U.TableReport report = new U.TableReport();
        // Vocabulary order crosses a collection and an explicitly selected
        // nested empty arm. None itself contributes no identity or payload.
        value.HistoryCount = 3;
        value.History[0].Type = U.ChoiceType.Signal;
        value.History[1].Type = U.ChoiceType.Nested;
        value.History[1].Nested.Type = U.LeafType.Empty;
        byte[] expected = Fixture(new byte[] { 1,14,12,15,3,2,32,0,3,15,3,4,32,0,0,0 },
            "history", "signal", "nested", "empty");
        byte[] actual = new byte[expected.Length];
        Check(U.Schema.RootMeasure(value) == expected.Length && U.Schema.RootSave(value, actual) == actual.Length &&
            actual.AsSpan().SequenceEqual(expected), "union arrays: first-use bytes and selected empty arms");
        U.Root copy = new U.Root();
        Check(U.Schema.RootLoad(copy, expected, report) && copy.HistoryCount == 3 &&
            copy.History[0].Type == U.ChoiceType.Signal && copy.History[1].Nested.Type == U.LeafType.Empty &&
            copy.History[2].Type == U.ChoiceType.None, "union arrays: independent bytes decode");

        // A selected general array zeroes all its storage. A decoded type
        // element then applies its declared defaults, leaving unused tails zero.
        byte[] cells = Fixture(new byte[] { 1,15,2,14,4,13,1,1,0,3,8,42,0,0,0,0 }, "item", "cells", "tail");
        Check(U.Schema.RootLoad(copy, cells, report) && copy.Item.Type == U.ChoiceType.Cells &&
            copy.Item.Cells[0].X == 9 && copy.Item.Cells[1].X == 0 && copy.Item.Cells[2].X == 0 && copy.Tail == 42,
            "general array arm: decoded defaults and zero unused tails");

        byte[] alias = Fixture(new byte[] { 1,15,2,30,1,3,0 }, "item", "grade", "Two");
        Check(U.Schema.RootLoad(copy, alias, report) && copy.Item.Type == U.ChoiceType.Grade &&
            copy.Item.Grade == U.Grade.Second, "enum used only in an arm: identity alias");

        byte[] badVoid = Fixture(new byte[] { 1,15,2,32,1,99,3,8,42,0,0,0,0 }, "item", "signal", "tail");
        report = new U.TableReport();
        Check(U.Schema.RootLoad(copy, badVoid, report) && report.Malformed && copy.Item.Type == U.ChoiceType.None && copy.Tail == 42,
            "void arm: nonzero length clears selection and preserves sibling");

        Messagedemo.ToolMessage message = new Messagedemo.ToolMessage();
        Messagedemo.TableReport armReport = new Messagedemo.TableReport();
        byte[] badWidth = Fixture(new byte[] { 1,15,2,6,2,1,0,0 }, "body", "caps");
        Check(Messagedemo.Schema.ToolMessageLoad(message, badWidth, armReport) && armReport.Malformed &&
            armReport.Widened == 0 && message.Body.Type == Messagedemo.ToolBodyType.None,
            "widening arm: damage precedes the widening event");

        // The later fixed-array element supplies a reference but no complete
        // arm header. It clears the earlier slot and leaves the next field.
        byte[] repeat = Fixture(new byte[] { 1,14,6,15,2,2,32,0,0,1,14,3,15,2,2,3,8,42,0,0,0,0 },
            "pending", "signal", "tail");
        report = new U.TableReport();
        Check(U.Schema.RootLoad(copy, repeat, report) && report.Malformed && copy.Pending[0].Type == U.ChoiceType.None && copy.Tail == 42,
            "repeated union array: damaged later header clears the old selection");

        report = new U.TableReport();
        byte[] foreignArray = Fixture(new byte[] { 1,14,2,8,0,2,8,42,0,0,0,0 }, "optional", "tail");
        Check(U.Schema.RootLoad(copy, foreignArray, report) && report.KindMismatch == 1 && !copy.OptionalPresent && copy.Tail == 42,
            "optional array: foreign element kind does not establish presence");

        report = new U.TableReport();
        Check(U.Schema.RootFromJson(copy, Encoding.UTF8.GetBytes("{\"item\":{\"grade\":7},\"tail\":42}"), report) &&
            report.KindMismatch == 1 && copy.Item.Type == U.ChoiceType.None && copy.Tail == 42,
            "general JSON arm: wrong shape leaves None and preserves sibling");
        report = new U.TableReport();
        Check(U.Schema.RootFromJson(copy, Encoding.UTF8.GetBytes("{\"optional\":[{\"x\":4}],\"optional\":null}"), report) &&
            !copy.OptionalPresent && copy.OptionalCount == 0 && copy.Optional[0].X == 9,
            "optional array: repeated null restores absent storage");

        report = new U.TableReport();
        for (int i = 0; i < 1000; i++) { U.Schema.RootLoad(copy, expected, report); U.Schema.RootSave(copy, actual); }
        long before = GC.GetAllocatedBytesForCurrentThread();
        for (int i = 0; i < 1000; i++) { U.Schema.RootLoad(copy, expected, report); U.Schema.RootSave(copy, actual); }
        Check(GC.GetAllocatedBytesForCurrentThread() == before, "nested union arrays: zero allocations after warmup");
    }
}
