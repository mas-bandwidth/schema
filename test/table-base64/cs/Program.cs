using System;
using Base64test;

static class Program
{
    static string Hex(ReadOnlySpan<byte> data) => data.Length == 0 ? "-" : Convert.ToHexString(data);
    static void Main()
    {
        string line;
        while ((line = Console.ReadLine()) != null)
        {
            Blob value = new Blob();
            TableReport report = new TableReport();
            Schema.BlobFromJson(value, Convert.FromHexString(line), report);
            if (report.Malformed) { Console.WriteLine("1 0 0 - -"); continue; }
            byte[] output = new byte[Schema.BlobToJsonMeasure(value)];
            if (Schema.BlobToJson(value, output) != output.Length) { throw new Exception("writer size"); }
            Console.WriteLine($"0 {report.Clamped} {report.KindMismatch} {Hex(value.Payload.AsSpan(0, value.PayloadLength))} {Hex(output)}");
        }
    }
}
