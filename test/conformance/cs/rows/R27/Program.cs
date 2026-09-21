using System;
using System.IO;

class Program
{
    static int Main(string[] args)
    {
        string[] candidates = new[]
        {
            "testdata/conformance/tables/MANIFEST.txt",
            "../testdata/conformance/tables/MANIFEST.txt",
            "../../../../testdata/conformance/tables/MANIFEST.txt",
            "../../../../../testdata/conformance/tables/MANIFEST.txt",
        };
        string? manifestPath = null;
        foreach (var c in candidates)
        {
            if (File.Exists(c))
            {
                manifestPath = c;
                break;
            }
        }
        if (manifestPath == null)
        {
            Console.Error.WriteLine("MANIFEST.txt not found");
            return 1;
        }
        foreach (string line in File.ReadLines(manifestPath))
        {
            if (line.Contains("cook_lead_27"))
            {
                Console.WriteLine("PASS: cook_lead_27 entry found");
                return 0;
            }
        }
        Console.Error.WriteLine("cook_lead_27 not found");
        return 1;
    }
}
