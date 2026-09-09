using System;
using System.Buffers.Binary;
using System.Collections.Generic;
using V1 = Tblv1;

static partial class Program
{
    static byte[] VocabularyFixture(ulong[] ids)
    {
        byte[] bytes = new byte[10 + ids.Length * 8];
        bytes[0] = 1; // form 1; byte 1 is the empty body's terminator
        for (int i = 0; i < ids.Length; i++)
        {
            BinaryPrimitives.WriteUInt64LittleEndian(bytes.AsSpan(2 + i * 8), ids[i]);
        }
        BinaryPrimitives.WriteUInt64LittleEndian(bytes.AsSpan(bytes.Length - 8), (ulong)ids.Length);
        return bytes;
    }

    static void TestVocabularyDistinctness()
    {
        V1.Cfg target = new V1.Cfg();
        void CheckIds(ulong[] ids, string label)
        {
            // The expected verdict comes from an independent set, including
            // unused and zero identities; no generated hash is the oracle.
            bool distinct = new HashSet<ulong>(ids).Count == ids.Length;
            byte[] wire = VocabularyFixture(ids);
            byte[] padded = new byte[wire.Length + 5];
            wire.CopyTo(padded, 3);
            DirtyResetTarget(target);
            V1.TableReport report = new V1.TableReport();
            var expected = distinct ? V1.Schema.TableWire.Verdict.Ok : V1.Schema.TableWire.Verdict.Damaged;
            var verdict = V1.Schema.CfgLoadVerdict(target, padded.AsSpan(3, wire.Length), report);
            Check(verdict == expected && report.Verdict == expected && report.Malformed == !distinct &&
                !report.Refused && report.Reason == null && report.Unknown == 0 && report.KindMismatch == 0 &&
                report.Clamped == 0 && report.Widened == 0 && report.Duplicate == 0,
                "vocabulary verdict and report: " + label);
            Check(ResetTargetDefaults(target, 5), "vocabulary preserves root defaults: " + label);
            Check(padded.AsSpan(3, wire.Length).SequenceEqual(wire), "vocabulary never mutates input: " + label);
        }

        foreach (int count in new int[] { 0, 1, 2, 8, 15, 16, 17, 127, 128, 255, 256, 257, 512, 1025 })
        {
            ulong[] ids = new ulong[count];
            for (int i = 0; i < count; i++) { ids[i] = (ulong)i; }
            if (count > 1) { ids[1] = ulong.MaxValue; }
            CheckIds(ids, "distinct count " + count);
            if (count > 1)
            {
                ids[count - 1] = ids[0];
                CheckIds(ids, "duplicate zero at end count " + count);
                ids[0] = ulong.MaxValue;
                ids[count - 1] = ulong.MaxValue;
                CheckIds(ids, "duplicate maximum identity count " + count);
            }
        }

        // Force the maximum fast-path probe chain, crossing slot 511 to zero.
        // The arithmetic only selects stress inputs; HashSet still supplies
        // the verdict. A collision must never be mistaken for a duplicate.
        ulong[] collisions = new ulong[256];
        for (ulong id = 0, found = 0; found < (ulong)collisions.Length; id++)
        {
            if ((unchecked(id * 0x9e3779b97f4a7c15ul) >> 55) == 511)
            {
                collisions[(int)found++] = id;
            }
        }
        CheckIds(collisions, "256 distinct identities in one bucket with wraparound");
        collisions[255] = collisions[254];
        CheckIds(collisions, "duplicate after longest probe chain");

        Random random = new Random(731049);
        byte[] randomBytes = new byte[8];
        for (int sample = 0; sample < 300; sample++)
        {
            ulong[] ids = new ulong[random.Next(0, 400)];
            for (int i = 0; i < ids.Length; i++)
            {
                random.NextBytes(randomBytes);
                ids[i] = BinaryPrimitives.ReadUInt64LittleEndian(randomBytes);
            }
            if (ids.Length > 1 && sample % 2 == 0)
            {
                int first = random.Next(ids.Length - 1);
                ids[random.Next(first + 1, ids.Length)] = ids[first];
            }
            CheckIds(ids, "independent randomized set " + sample);
        }

        byte[] valid = VocabularyFixture(new ulong[16]);
        for (int i = 0; i < 16; i++)
        {
            BinaryPrimitives.WriteUInt64LittleEndian(valid.AsSpan(2 + i * 8), (ulong)i);
        }
        byte[] forgedCount = (byte[])valid.Clone();
        BinaryPrimitives.WriteUInt64LittleEndian(forgedCount.AsSpan(forgedCount.Length - 8), ulong.MaxValue);
        DirtyResetTarget(target);
        V1.TableReport forgedReport = new V1.TableReport();
        Check(V1.Schema.CfgLoadVerdict(target, forgedCount, forgedReport) == V1.Schema.TableWire.Verdict.Damaged &&
            forgedReport.Malformed && !forgedReport.Refused && ResetTargetDefaults(target, 5),
            "forged maximum vocabulary count is rejected before indexing or hashing");
        V1.TableReport allocationReport = new V1.TableReport();
        for (int i = 0; i < 1000; i++) { V1.Schema.CfgLoadVerdict(target, valid, allocationReport); }
        long before = GC.GetAllocatedBytesForCurrentThread();
        bool allValid = true;
        for (int i = 0; i < 1000; i++)
        {
            allValid &= V1.Schema.CfgLoadVerdict(target, valid, allocationReport) == V1.Schema.TableWire.Verdict.Ok;
        }
        long allocated = GC.GetAllocatedBytesForCurrentThread() - before;
        Check(allValid && allocated == 0, "vocabulary validation allocates zero bytes after warmup: " + allocated);
    }
}
