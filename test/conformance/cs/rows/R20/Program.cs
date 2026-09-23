using System;
// cell-cs-r20: §21.2's landing rules
// law: docs/FIXED-FORM-ALGORITHM.md:308-312, 335, 637-638
class Program
{
    static int Main()
    {
        var v2 = new Tr2.V2();
        v2.A = 42;
        var report = new Schema.TableReport();
        var bytes = new byte[Tr1.V1.Measure(v2)];
        if (Tr1.V1.TrySave(bytes, v2) != bytes.Length)
        {
            Console.Error.WriteLine("FAIL: save did not fill the buffer");
            return 1;
        }

        var v1 = new Tr1.V1();
        if (!Tr1.V1.TryLoad(v1, bytes, report))
        {
            Console.Error.WriteLine("FAIL: could not load");
            return 1;
        }

        if (report.Unknown != 1)
        {
            Console.Error.WriteLine("FAIL: report.Unknown={0}, want 1", report.Unknown);
            return 1;
        }
        Console.WriteLine("OK: report.Unknown={0}", report.Unknown);

        if (v1.A != 0)
        {
            Console.Error.WriteLine("FAIL: v1.A={0}, want 0", v1.A);
            return 1;
        }
        Console.WriteLine("OK: v1.A={0}", v1.A);


        return 0;
    }
}
