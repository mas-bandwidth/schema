// ?T vs plain nesting: P1 nests Link by value (plain), P3 marks it `?Link` (optional).
// On the FIXED FORM (form byte 3) they are ONE BYTE APART — the `_present` bool.
// docs/SPEC-TABLES.md §2.3, §3.4; docs/FIXED-FORM-ALGORITHM.md §7
//
// The law: "§2.3's rule that ?T and a plain T nesting are WIRE-IDENTICAL is a
// rule of §3's body, where presence IS the field riding. ON THIS FORM THEY ARE
// ONE BYTE APART."  (docs/SPEC-TABLES.md:6559)
//
// This test asserts that P1 Chain (plain `Link`) and P3 Chain (optional `?Link`)
// have the correct fixed-form sizes and that the round-trip is byte-identical.

using System;

namespace E5
{
    static class Program
    {
        static int Main()
        {
            // ---- P1: plain nesting (Link by value) ----
            int expectedP1Body = 36;
            int gotP1Body = (int)Tblp1.Schema.ChainFixedBodyBytes;
            int expectedP1Record = 44;
            int gotP1Record = (int)Tblp1.Schema.ChainFixedRecordBytes;

            // ---- P3: optional (?Link) ----
            int expectedP3Body = 37;
            int gotP3Body = (int)Tblp3.Schema.ChainFixedBodyBytes;
            int expectedP3Record = 45;
            int gotP3Record = (int)Tblp3.Schema.ChainFixedRecordBytes;

            // RecordBytes = 8 (record hash) + body bytes
            // P3 adds one byte for the `_present` bool

            int sizeDiff = gotP3Body - gotP1Body;

            int failed = 0;

            if (gotP1Body != expectedP1Body)
            {
                Console.Error.WriteLine("FAIL: P1 ChainFixedBodyBytes = " + gotP1Body + ", expected " + expectedP1Body);
                failed++;
            }
            if (gotP1Record != expectedP1Record)
            {
                Console.Error.WriteLine("FAIL: P1 ChainFixedRecordBytes = " + gotP1Record + ", expected " + expectedP1Record);
                failed++;
            }
            if (gotP3Body != expectedP3Body)
            {
                Console.Error.WriteLine("FAIL: P3 ChainFixedBodyBytes = " + gotP3Body + ", expected " + expectedP3Body);
                failed++;
            }
            if (gotP3Record != expectedP3Record)
            {
                Console.Error.WriteLine("FAIL: P3 ChainFixedRecordBytes = " + gotP3Record + ", expected " + expectedP3Record);
                failed++;
            }
            if (sizeDiff != 1)
            {
                Console.Error.WriteLine("FAIL: P3 body size (" + gotP3Body + ") minus P1 body size (" + gotP1Body + ") = " + sizeDiff + ", expected 1");
                failed++;
            }

            // ---- Round-trip test: write and read back ----
            byte[] buffer;
            long bytesWritten;

            // P1 Chain with values
            var p1Chain = new Tblp1.Chain();
            p1Chain.Name[0] = (byte)'h';
            p1Chain.Name[1] = (byte)'i';
            p1Chain.NameLength = 2;
            p1Chain.Link.Value = 42;
            p1Chain.Link.Tag[0] = (byte)'t';
            p1Chain.Link.Tag[1] = (byte)'a';
            p1Chain.Link.Tag[2] = (byte)'g';
            p1Chain.Link.TagLength = 3;

            long p1Size = Tblp1.Schema.ChainFixedMeasure(1);
            buffer = new byte[p1Size];
            bytesWritten = Tblp1.Schema.ChainFixedSave(p1Chain, buffer);
            if (bytesWritten != p1Size)
            {
                Console.Error.WriteLine("FAIL: P1 ChainFixedSave wrote " + bytesWritten + " bytes, expected " + p1Size);
                failed++;
            }

            var p1Read = new Tblp1.Chain();
            var p1Report = new Tblp1.TableReport();
            long p1ReadCount = Tblp1.Schema.ChainFixedLoad(p1Read, buffer, null, p1Report);
            if (p1ReadCount != 1)
            {
                Console.Error.WriteLine("FAIL: P1 ChainFixedLoad read " + p1ReadCount + " records, expected 1");
                failed++;
            }
            if (p1Report.Malformed)
            {
                Console.Error.WriteLine("FAIL: P1 ChainFixedLoad reported malformed");
                failed++;
            }

            // P3 Chain with values (present)
            var p3Chain = new Tblp3.Chain();
            p3Chain.Name[0] = (byte)'h';
            p3Chain.Name[1] = (byte)'i';
            p3Chain.NameLength = 2;
            p3Chain.Link.Value = 42;
            p3Chain.Link.Tag[0] = (byte)'t';
            p3Chain.Link.Tag[1] = (byte)'a';
            p3Chain.Link.Tag[2] = (byte)'g';
            p3Chain.Link.TagLength = 3;
            p3Chain.LinkPresent = true;

            long p3Size = Tblp3.Schema.ChainFixedMeasure(1);
            buffer = new byte[p3Size];
            bytesWritten = Tblp3.Schema.ChainFixedSave(p3Chain, buffer);
            if (bytesWritten != p3Size)
            {
                Console.Error.WriteLine("FAIL: P3 ChainFixedSave wrote " + bytesWritten + " bytes, expected " + p3Size);
                failed++;
            }

            var p3Read = new Tblp3.Chain();
            var p3Report = new Tblp3.TableReport();
            long p3ReadCount = Tblp3.Schema.ChainFixedLoad(p3Read, buffer, null, p3Report);
            if (p3ReadCount != 1)
            {
                Console.Error.WriteLine("FAIL: P3 ChainFixedLoad read " + p3ReadCount + " records, expected 1");
                failed++;
            }
            if (p3Report.Malformed)
            {
                Console.Error.WriteLine("FAIL: P3 ChainFixedLoad reported malformed");
                failed++;
            }

            if (failed > 0)
            {
                Console.Error.WriteLine("FAILED: " + failed + " assertion(s) failed");
                return 1;
            }

            Console.Out.WriteLine("PASS: ?T vs plain nesting — body " + gotP1Body + "/" + gotP3Body + " (diff=1), record " + gotP1Record + "/" + gotP3Record + ", round-trip OK");
            return 0;
        }
    }
}