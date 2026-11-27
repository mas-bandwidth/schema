using System;
using System.IO;
using System.Runtime.InteropServices;

static partial class Program
{
    // THE RETAIN-REFUSAL SURFACE (test/conformance/README.md): ONE retention
    // store through LoadRetain of the first wire, SaveRetain, LoadRetain of the
    // refused wire into the same store (a region of its own, so the first value
    // stays whole; the first's size where LoadMeasure refuses the wire), and
    // SaveRetain of the first value again. The verdicts and what the second save
    // did go in <case>, the first save in <case>.before, the second in
    // <case>.wire.
    static unsafe int SurfaceRetainRefusal(string outDir)
    {
        foreach (string[] f in lines)
        {
            if (f[0] != "retain-refusal") { continue; }
            byte[] first = File.ReadAllBytes(f[4]), second = File.ReadAllBytes(f[5]);
            byte[]? before = null, after = null;
            bool read = false, secondRefused = false;
            if (f[2] == "tblrt1" && f[3] == "Node")
            {
                long need = Tblrt1.Schema.NodeLoadMeasure(first);
                if (need >= 0)
                {
                    byte* region = (byte*)NativeMemory.AlignedAlloc((nuint)Math.Max(1, need), 64);
                    long otherNeed = Tblrt1.Schema.NodeLoadMeasure(second);
                    if (otherNeed < 0) { otherNeed = need; }
                    byte* other = (byte*)NativeMemory.AlignedAlloc((nuint)Math.Max(1, otherNeed), 64);
                    var store = new Tblrt1.TableRetain { Bytes = (byte*)NativeMemory.Alloc(1 << 20), Capacity = 1 << 20, Ids = (Tblrt1.TableRetain.Id*)NativeMemory.Alloc(1 << 21), IdCapacity = 1 << 17 };
                    try
                    {
                        var report = new Tblrt1.TableReport();
                        IntPtr root = (IntPtr)Tblrt1.Schema.NodeLoadRetain((IntPtr)region, need, first, ref store, report);
                        read = root != IntPtr.Zero && !report.Damaged && !report.Refused;
                        if (read)
                        {
                            long size = Tblrt1.Schema.NodeMeasureRetain(root, ref store);
                            if (size < 0) { return 1; }
                            before = new byte[size];
                            if (Tblrt1.Schema.NodeSaveRetain(root, ref store, before, new Tblrt1.TableReport()) != size) { return 1; }
                            var refused = new Tblrt1.TableReport();
                            secondRefused = Tblrt1.Schema.NodeLoadRetain((IntPtr)other, otherNeed, second, ref store, refused) == null || refused.Damaged || refused.Refused;
                            size = Tblrt1.Schema.NodeMeasureRetain(root, ref store);
                            if (size >= 0)
                            {
                                after = new byte[size];
                                if (Tblrt1.Schema.NodeSaveRetain(root, ref store, after, new Tblrt1.TableReport()) != size) { after = null; }
                            }
                        }
                    }
                    finally
                    {
                        NativeMemory.Free(store.Bytes); NativeMemory.Free(store.Ids);
                        NativeMemory.AlignedFree(region); NativeMemory.AlignedFree(other);
                    }
                }
            }
            else if (f[2] == "tblvarevo1" && f[3] == "EvoDoc")
            {
                long need = Tblvarevo1.Schema.EvoDocLoadMeasure(first);
                if (need >= 0)
                {
                    byte* region = (byte*)NativeMemory.AlignedAlloc((nuint)Math.Max(1, need), 64);
                    long otherNeed = Tblvarevo1.Schema.EvoDocLoadMeasure(second);
                    if (otherNeed < 0) { otherNeed = need; }
                    byte* other = (byte*)NativeMemory.AlignedAlloc((nuint)Math.Max(1, otherNeed), 64);
                    var store = new Tblvarevo1.TableRetain { Bytes = (byte*)NativeMemory.Alloc(1 << 20), Capacity = 1 << 20, Ids = (Tblvarevo1.TableRetain.Id*)NativeMemory.Alloc(1 << 21), IdCapacity = 1 << 17 };
                    try
                    {
                        var report = new Tblvarevo1.TableReport();
                        IntPtr root = (IntPtr)Tblvarevo1.Schema.EvoDocLoadRetain((IntPtr)region, need, first, ref store, report);
                        read = root != IntPtr.Zero && !report.Damaged && !report.Refused;
                        if (read)
                        {
                            long size = Tblvarevo1.Schema.EvoDocMeasureRetain(root, ref store);
                            if (size < 0) { return 1; }
                            before = new byte[size];
                            if (Tblvarevo1.Schema.EvoDocSaveRetain(root, ref store, before, new Tblvarevo1.TableReport()) != size) { return 1; }
                            var refused = new Tblvarevo1.TableReport();
                            secondRefused = Tblvarevo1.Schema.EvoDocLoadRetain((IntPtr)other, otherNeed, second, ref store, refused) == null || refused.Damaged || refused.Refused;
                            size = Tblvarevo1.Schema.EvoDocMeasureRetain(root, ref store);
                            if (size >= 0)
                            {
                                after = new byte[size];
                                if (Tblvarevo1.Schema.EvoDocSaveRetain(root, ref store, after, new Tblvarevo1.TableReport()) != size) { after = null; }
                            }
                        }
                    }
                    finally
                    {
                        NativeMemory.Free(store.Bytes); NativeMemory.Free(store.Ids);
                        NativeMemory.AlignedFree(region); NativeMemory.AlignedFree(other);
                    }
                }
            }
            else { SpillAbsent(outDir, f[1]); continue; }
            if (!read) { File.WriteAllText(Path.Combine(outDir, f[1]), "refused\n"); continue; }
            File.WriteAllBytes(Path.Combine(outDir, f[1] + ".before"), before!);
            string answer = "read\n" + (secondRefused ? "refused\n" : "read\n");
            if (after == null) { answer += "save-refused\n"; }
            else { File.WriteAllBytes(Path.Combine(outDir, f[1] + ".wire"), after); answer += "saved\n"; }
            File.WriteAllText(Path.Combine(outDir, f[1]), answer);
        }
        return 0;
    }
}
