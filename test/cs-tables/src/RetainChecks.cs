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
    static unsafe void RetainMessages()
    {
        var vocabulary=new RT.TableVocabulary();
        var report=new RT.TableReport();
        Check(RT.Schema.AnnounceRead(vocabulary,ReadGolden("retain_conn"),report)==RT.Schema.TableWire.Verdict.Ok,"retention announcement");
        byte[] wire=ReadGolden("retain_message");
        long need=RT.Schema.NodeLoadMeasure(vocabulary,wire);
        byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(need+64),64);
        Span<RT.TableRetain> stores=stackalloc RT.TableRetain[2];
        Span<IntPtr> roots=stackalloc IntPtr[2];
        for(int i=0;i<2;i++)
        { stores[i]=new RT.TableRetain { Bytes=(byte*)NativeMemory.Alloc(16384),Capacity=16384,Ids=(RT.TableRetain.Id*)NativeMemory.Alloc((nuint)(1024*sizeof(RT.TableRetain.Id))),IdCapacity=1024 }; }
        try
        {
            new Span<byte>(region,(int)need+64).Fill(0xa5);
            var result=RT.Schema.NodeLoadRetainMessages((IntPtr)region,need,roots,wire,vocabulary,stores,report,out int count);
            Check(result==RT.Schema.TableWire.Verdict.Ok && !report.Malformed && !report.Refused && count==2,"retention message load");
            Check(report.Retained==10 && report.RetainLost==6 && report.Unknown==17,"message retention reports "+report.Retained+","+report.RetainLost+","+report.Unknown);
            for(long i=need;i<need+64;i++) { Check(region[i]==0xa5,"message retention overrun guard"); }
            for(int i=0;i<2;i++)
            {
                long size=RT.Schema.NodeMeasureRetain(roots[i],ref stores[i]);
                byte[] saved=new byte[size]; var save=new RT.TableReport();
                Check(RT.Schema.NodeSaveRetain(roots[i],ref stores[i],saved,save)==size && save.RetainLost==0,"retained message saves as file");
                Check(saved.AsSpan().SequenceEqual(ReadGolden("retain_message_save_"+i)),"retained message C++ file golden "+i);
            }
        }
        finally
        {
            NativeMemory.AlignedFree(region);
            for(int i=0;i<2;i++) { NativeMemory.Free(stores[i].Bytes); NativeMemory.Free(stores[i].Ids); }
        }
    }
    static unsafe void RetainDamagedListElement()
    {
        byte[] wire=Fixture(new byte[]{1,14,8,13,1,5,2,1,1,0,0,0},"rows","future");
        byte[] expected=Fixture(new byte[]{1,14,7,13,1,4,2,1,1,0,0},"rows","future");
        long need=Listdemo.Schema.SheetLoadMeasure(wire);
        Check(need>=0,"damaged list element can be sized"); if(need<0) { return; }
        byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(need+64),64);
        byte* bytes=stackalloc byte[512]; Listdemo.TableRetain.Id* ids=stackalloc Listdemo.TableRetain.Id[16];
        var store=new Listdemo.TableRetain { Bytes=bytes,Capacity=512,Ids=ids,IdCapacity=16 };
        try
        {
            var report=new Listdemo.TableReport();
            Check(Listdemo.Schema.SheetLoadRetain((IntPtr)region,need,wire,ref store,report)!=null && report.Malformed && report.Retained==1 && store.Count==1,"body framing damage preserves its own captured unknown field");
            byte[] saved=new byte[Listdemo.Schema.SheetMeasureRetain((IntPtr)region,ref store)];
            Check(Listdemo.Schema.SheetSaveRetain((IntPtr)region,ref store,saved,new Listdemo.TableReport())==saved.Length && saved.AsSpan().SequenceEqual(expected),"damaged list element saves its retained field with repaired framing");
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
        RetainMessages();
        RetainDamagedListElement();
    }
}
