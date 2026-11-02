using System;
using System.Runtime.InteropServices;
using RT = Tblrt1;

static partial class Program
{
    static unsafe void RetainCase(string name,int kept,int lost,int unknown,int saveLost,string golden=null,int idsCapacity=1024,bool shortStore=false)
    {
        byte[] wire=ReadGolden(name);
        long need=RT.Schema.NodeLoadMeasure(wire);
        Check(need>=0,name+" retention load measure"); if(need<0) { return; }
        byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(need+64),64);
        byte* storage=(byte*)NativeMemory.Alloc(16384);
        RT.TableRetain.Id* ids=(RT.TableRetain.Id*)NativeMemory.Alloc((nuint)(1024*sizeof(RT.TableRetain.Id)));
        try
        {
            RT.TableRetain retain=new RT.TableRetain { Bytes=storage,Capacity=16384,Ids=ids,IdCapacity=idsCapacity };
            if(shortStore)
            {
                var probe=new RT.TableReport();
                Check(RT.Schema.NodeLoadRetain((IntPtr)region,need,wire,ref retain,probe)!=null,name+" full capacity probe");
                retain.Capacity=retain.Used-1;
            }
            new Span<byte>(region,(int)need+64).Fill(0xa5);
            var report=new RT.TableReport();
            Check(RT.Schema.NodeLoadRetain((IntPtr)region,need,wire,ref retain,report)!=null && !report.Malformed && !report.Refused,name+" retained load");
            Check(report.Retained==kept && report.RetainLost==lost && report.Unknown==unknown,name+" retained report "+report.Retained+","+report.RetainLost+","+report.Unknown);
            Check(retain.IdUsed==0,name+" load leaves retained ID store empty");
            for(long i=need;i<need+64;i++) { Check(region[i]==0xa5,name+" retention region overrun guard"); }
            long size=RT.Schema.NodeMeasureRetain((IntPtr)region,ref retain);
            Check(size>=0,name+" retention save measure"); if(size<0) { return; }
            byte[] output=new byte[size]; var save=new RT.TableReport();
            Check(RT.Schema.NodeSaveRetain((IntPtr)region,ref retain,output,save)==size,name+" retention save size");
            Check(save.RetainLost==saveLost,name+" save loss "+save.RetainLost);
            if(golden!=null)
            {
                byte[] expected=ReadGolden(golden);
                Check(output.AsSpan().SequenceEqual(expected),name+" retained C++ golden ("+output.Length+" vs "+expected.Length+")");
            }
            Check(RT.Schema.NodeSaveRetain((IntPtr)region,ref retain,output,null)<0,name+" retention save requires its report");
            long again=RT.Schema.NodeMeasureRetain((IntPtr)region,ref retain); byte[] second=new byte[again];
            Check(RT.Schema.NodeSaveRetain((IntPtr)region,ref retain,second,new RT.TableReport())==size && output.AsSpan().SequenceEqual(second),name+" repeat save is idempotent");
            if(!shortStore && saveLost==0)
            {
                long nextNeed=RT.Schema.NodeLoadMeasure(output); byte* next=(byte*)NativeMemory.AlignedAlloc((nuint)nextNeed,64);
                try
                {
                    Check(RT.Schema.NodeLoadRetain((IntPtr)next,nextNeed,output,ref retain,new RT.TableReport())!=null,name+" second retained load");
                    long thirdSize=RT.Schema.NodeMeasureRetain((IntPtr)next,ref retain); byte[] third=new byte[thirdSize];
                    Check(RT.Schema.NodeSaveRetain((IntPtr)next,ref retain,third,new RT.TableReport())==size && output.AsSpan().SequenceEqual(third),name+" second round trip idempotent");
                }
                finally { NativeMemory.AlignedFree(next); }
            }
        }
        finally { NativeMemory.AlignedFree(region); NativeMemory.Free(storage); NativeMemory.Free(ids); }
    }
    // DAMAGE DISCARDS THE WHOLE LOAD (docs/SPEC-TABLES.md §4): a list element
    // damaged inside its body refuses the retaining load, and the store keeps
    // nothing it captured on the way.
    static unsafe void RetainDamagedListElement()
    {
        byte[] wire=Fixture(new byte[]{1,14,8,13,1,5,2,1,1,0,0,0},"rows","future");
        long need=Listdemo.Schema.SheetLoadMeasure(wire);
        Check(need>=0,"damaged list element can be sized"); if(need<0) { return; }
        byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(need+64),64);
        byte* bytes=stackalloc byte[512]; Listdemo.TableRetain.Id* ids=stackalloc Listdemo.TableRetain.Id[16];
        var store=new Listdemo.TableRetain { Bytes=bytes,Capacity=512,Ids=ids,IdCapacity=16 };
        try
        {
            var report=new Listdemo.TableReport();
            Check(Listdemo.Schema.SheetLoadRetain((IntPtr)region,need,wire,ref store,report)==null && report.Malformed && store.Count==0 && store.Used==0,"body framing damage discards the whole retaining load");
        }
        finally { NativeMemory.AlignedFree(region); }
    }
    static void TestRetention()
    {
        RetainCase("retain_rt2",9,2,11,0,"retain_rt1_save");
        RetainCase("retain_rt2",8,3,11,0,null,1024,true);
        RetainCase("retain_rt2",9,2,11,1,"retain_rt1_save_id_short",2);
        for(int i=2;i<=5;i++) { RetainCase("retain_excluded_"+i,0,1,1,0); }
        RetainCase("retain_rt3",0,1,1,0);
        RetainDamagedListElement();
    }
}
