using System;
using System.Buffers.Binary;
using Tblk1;

// cell-cs-f9 — batch_too_large
//
// The law (docs/FIXED-FORM-ALGORITHM.md:147, step 6):
//   rest := bytes - 20 - L; if rest / record_bytes passes the caller's
//   capacity, REFUSE batch_too_large.
//
// This test constructs a valid fixed-form batch with 2 records and loads it
// into a 1-slot array (capacity=1).  The reader must refuse with reason
// "batch_too_large" and return -1 without decoding any record.
//
// The negative control mutates the header hash so the layout is unknown,
// producing "layout_newer" instead — proving the capacity check bites and
// the refusal name is specific.

unsafe class Program
{
    static int Main()
    {
        bool ok = true;

        // GREEN: 2 records into a 1-slot array → batch_too_large
        ok &= CheckBatchTooLarge();

        // GREEN: 2 records into a 2-slot array → loads fine
        ok &= CheckBatchFits();

        // NEGATIVE CONTROL: corrupt the hash → layout_newer, not batch_too_large
        ok &= CheckNegativeControl();

        if (!ok)
        {
            return 1;
        }

        Console.WriteLine("batch_too_large");
        return 0;
    }

    // Build a valid fixed-form byte vector with `count` records.
    // Uses RootFixedSave which writes the canonical header + layout + records.
    static byte[] BuildBatch(int count)
    {
        var values = new Root[count];
        for (int i = 0; i < count; i++)
        {
            values[i] = new Root();
            values[i].Grade = (Tblk1.Grade)(i + 1);
            values[i].Raw = (ushort)(i * 10);
        }
        long need = Schema.RootFixedMeasure(count);
        var buf = new byte[(int)need];
        long wrote = Schema.RootFixedSave(values, buf);
        if (wrote != need)
        {
            throw new Exception(
                string.Format("RootFixedSave wrote {0}, want {1}", wrote, need));
        }
        return buf;
    }

    // 1. batch_too_large: 2 records, capacity 1
    static bool CheckBatchTooLarge()
    {
        byte[] data = BuildBatch(2);
        var values = new Root[1];
        var plan = new TableFixedEntry[8192];
        var report = new TableReport();

        long n = Schema.RootFixedLoad(values, data, plan, report);

        if (n != -1)
        {
            Console.Error.WriteLine(
                "FAIL: CheckBatchTooLarge returned {0}, want -1", n);
            return false;
        }
        if (!report.Refused)
        {
            Console.Error.WriteLine("FAIL: CheckBatchTooLarge report.Refused is false");
            return false;
        }
        if (report.Reason != "batch_too_large")
        {
            Console.Error.WriteLine(
                "FAIL: CheckBatchTooLarge report.Reason=\"{0}\" want \"batch_too_large\"",
                report.Reason);
            return false;
        }
        if (report.Verdict != Schema.TableWire.Verdict.Refused)
        {
            Console.Error.WriteLine(
                "FAIL: CheckBatchTooLarge report.Verdict={0} want Refused",
                report.Verdict);
            return false;
        }
        if (report.Malformed)
        {
            Console.Error.WriteLine("FAIL: CheckBatchTooLarge report.Malformed is true");
            return false;
        }
        Console.WriteLine("OK: 2 records into capacity 1 → batch_too_large, refused={0} reason=\"{1}\" verdict={2}",
            report.Refused, report.Reason, report.Verdict);
        return true;
    }

    // 2. batch fits: 2 records, capacity 2
    static bool CheckBatchFits()
    {
        byte[] data = BuildBatch(2);
        var values = new Root[2];
        values[0] = new Root();
        values[1] = new Root();
        var plan = new TableFixedEntry[8192];
        var report = new TableReport();

        long n = Schema.RootFixedLoad(values, data, plan, report);

        if (n != 2)
        {
            Console.Error.WriteLine(
                "FAIL: CheckBatchFits returned {0}, want 2", n);
            return false;
        }
        if (report.Refused)
        {
            Console.Error.WriteLine(
                "FAIL: CheckBatchFits report.Refused is true, reason=\"{0}\"", report.Reason);
            return false;
        }
        Console.WriteLine("OK: 2 records into capacity 2 → loaded {0}", n);
        return true;
    }

    // 3. Negative control: corrupt the header hash → layout_newer
    static bool CheckNegativeControl()
    {
        byte[] data = BuildBatch(2);
        // Corrupt the hash at offset 8 (the layout hash in the header)
        data[8] ^= 0xFF;
        var values = new Root[1];
        var plan = new TableFixedEntry[8192];
        var report = new TableReport();

        long n = Schema.RootFixedLoad(values, data, plan, report);

        if (n != -1)
        {
            Console.Error.WriteLine(
                "FAIL: CheckNegativeControl returned {0}, want -1", n);
            return false;
        }
        if (!report.Refused)
        {
            Console.Error.WriteLine("FAIL: CheckNegativeControl report.Refused is false");
            return false;
        }
        if (report.Reason == "batch_too_large")
        {
            Console.Error.WriteLine(
                "FAIL: CheckNegativeControl report.Reason is \"batch_too_large\" — the capacity check must not bite before the layout check");
            return false;
        }
        Console.WriteLine("OK: corrupted hash → refusal \"{0}\" (not batch_too_large)", report.Reason);
        return true;
    }
}
