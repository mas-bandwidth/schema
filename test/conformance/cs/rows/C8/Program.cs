using System;
// cell-cs-c8: fixed-point F-shift / bits(N)
// The law (docs/FIXED-FORM-ALGORITHM.md:351-352): a fixed-point field's bounds are in VALUE UNITS and its storage is raw, so both ends are shifted by F first; a bits(N) clamps to 2^N - 1.
unsafe class Program
{
    static int Main()
    {
        var v2 = new Tfx2.V2();
        var रिपोर्ट = new Schema.TableReport();
        var bytes = new byte[Tfx1.V1.Measure(v2)];
        if (Tfx1.V1.TrySave(bytes, v2) != bytes.Length)
        {
            Console.Error.WriteLine("FAIL: save did not fill the buffer");
            return 1;
        }

        var v1 = new Tfx1.V1();
        if (!Tfx1.V1.TryLoad(v1, bytes, रिपोर्ट))
        {
            Console.Error.WriteLine("FAIL: could not load");
            return 1;
        }
        if (v1.Val != 127)
        {
            Console.Error.WriteLine("FAIL: v1.Val={0}, want 127", v1.Val);
            return 1;
        }
        Console.WriteLine("OK: v1.Val={0}", v1.Val);
        return 0;
    }
}
