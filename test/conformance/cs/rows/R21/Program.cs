using System;
// cell-cs-r21: widenf is the bit-exact widening — signalling NaNs kept, the quiet bit carried as the writer wrote it
// law: docs/FIXED-FORM-ALGORITHM.md:335
class Program
{
    static int Main()
    {
        var v1 = new Tw1.V1();
        v1.A = float.NaN;
        var report = new Schema.TableReport();
        var bytes = new byte[Tw2.V2.Measure(v1)];
        if (Tw2.V2.TrySave(bytes, v1) != bytes.Length)
        {
            Console.Error.WriteLine("FAIL: save did not fill the buffer");
            return 1;
        }

        var v2 = new Tw2.V2();
        if (!Tw2.V2.TryLoad(v2, bytes, report))
        {
            Console.Error.WriteLine("FAIL: could not load");
            return 1;
        }

        if (report.Widened != 1)
        {
            Console.Error.WriteLine("FAIL: report.Widened={0}, want 1", report.Widened);
            return 1;
        }

        if (!double.IsNaN(v2.A))
        {
            Console.Error.WriteLine("FAIL: v2.A={0}, want NaN", v2.A);
            return 1;
        }
        Console.WriteLine("OK: v2.A is NaN");
        return 0;
    }
}
