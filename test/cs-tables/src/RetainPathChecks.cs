using System;
using System.Runtime.InteropServices;
using R1=Csretain1;
using R2=Csretain2;

static partial class Program
{
    static unsafe void RetainZeroBitExpansion()
    {
        var vocabulary=new R1.TableVocabulary(); R1.Schema.AnnounceRead(vocabulary,R2.Schema.Announce(),new R1.TableReport());
        int slot=0;
        foreach(var field in R2.Schema.RootTableType().Fields) { if(field.Name=="future_list") { slot=field.MessageSlot; } }
        Check(slot>0,"zero-bit future array announcement");
        byte[] wire=new byte[16]; wire[0]=2; int at=16;
        void Put(ulong value,int bits) { for(int i=0;i<bits;i++,at++) { if(((value>>i)&1)!=0) { wire[at/8]|=(byte)(1<<(at%8)); } } }
        Put((ulong)slot,vocabulary.RefBits); Put(uint.MaxValue,32); Put(0,vocabulary.RefBits);
        Array.Resize(ref wire,(at+7)/8);
        long need=R1.Schema.RootLoadMeasure(vocabulary,wire);
        byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)need,64);
        byte* storage=stackalloc byte[64]; R1.TableRetain.Id* ids=stackalloc R1.TableRetain.Id[8];
        Span<R1.TableRetain> stores=stackalloc R1.TableRetain[1];
        stores[0]=new R1.TableRetain { Bytes=storage,Capacity=64,Ids=ids,IdCapacity=8 };
        Span<IntPtr> roots=stackalloc IntPtr[1]; var report=new R1.TableReport();
        try
        {
            var time=System.Diagnostics.Stopwatch.StartNew();
            R1.Schema.RootLoadRetainMessages((IntPtr)region,need,roots,wire,vocabulary,stores,report,out int count);
            Check(count==1 && !report.Malformed && !report.Refused && report.Unknown==1 && report.Retained==0 && report.RetainLost==1,"zero-bit expansion loses one whole record without changing the read");
            Check(time.Elapsed.TotalSeconds<1,"zero-bit expansion is bounded by declared retention capacity");
        }
        finally { NativeMemory.AlignedFree(region); }
    }
    static unsafe void TestRetentionPaths()
    {
        RetainZeroBitExpansion();
        var newer=new R2.Root();
        newer.Choice.Type=R2.ChoiceType.Rows;
        newer.Choice.Rows.Rows[0].Known=1; newer.Choice.Rows.Rows[0].Future=71;
        newer.Choice.Rows.Rows[1].Known=2; newer.Choice.Rows.Rows[1].Future=72;
        newer.Choices[0].Type=R2.ChoiceType.Rows;
        newer.Choices[0].Rows.Rows[0].Future=81; newer.Choices[0].Rows.Rows[1].Future=82;
        newer.Choices[1].Type=R2.ChoiceType.One; newer.Choices[1].One.Future=91;
        byte[] original=new byte[R2.Schema.RootMeasure(newer)]; R2.Schema.RootSave(newer,original);
        foreach(bool message in new[]{false,true})
        {
            byte[] wire=original;
            if(message) { wire=new byte[R2.Schema.RootMeasureMessages(new[]{newer})]; R2.Schema.RootSaveMessages(new[]{newer},wire); }
            var vocabulary=new R1.TableVocabulary(); R1.Schema.AnnounceRead(vocabulary,R2.Schema.Announce(),new R1.TableReport());
            long need=message?R1.Schema.RootLoadMeasure(vocabulary,wire):R1.Schema.RootLoadMeasure(wire);
            byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)need,64);
            byte* storage=(byte*)NativeMemory.Alloc(16384);
            R1.TableRetain.Id* ids=(R1.TableRetain.Id*)NativeMemory.Alloc((nuint)(1024*sizeof(R1.TableRetain.Id)));
            var stores=new R1.TableRetain[]{new R1.TableRetain { Bytes=storage,Capacity=16384,Ids=ids,IdCapacity=1024 }};
            var report=new R1.TableReport();
            try
            {
                if(message)
                { IntPtr[] roots=new IntPtr[1]; R1.Schema.RootLoadRetainMessages((IntPtr)region,need,roots,wire,vocabulary,stores,report,out _); }
                else { R1.Schema.RootLoadRetain((IntPtr)region,need,wire,ref stores[0],report); }
                Check(!report.Malformed && !report.Refused && report.Retained==5 && stores[0].Count==5,"nested union and element retention paths "+message);
                byte[] saved=new byte[R1.Schema.RootMeasureRetain((IntPtr)region,ref stores[0])];
                Check(R1.Schema.RootSaveRetain((IntPtr)region,ref stores[0],saved,new R1.TableReport())==saved.Length && saved.AsSpan().SequenceEqual(original),"nested retention paths reproduce writer values "+message);
                ((R1.RootRow*)region)->Choice.Type=R1.ChoiceType.One;
                byte[] changed=new byte[R1.Schema.RootMeasureRetain((IntPtr)region,ref stores[0])];
                var save=new R1.TableReport(); R1.Schema.RootSaveRetain((IntPtr)region,ref stores[0],changed,save);
                Check(save.RetainLost==2,"switching an arm loses only its descendants "+message);
                var reread=new R2.Root(); var readReport=new R2.TableReport(); R2.Schema.RootLoad(reread,changed,readReport);
                Check(!readReport.Malformed && reread.Choice.One.Future==0 && reread.Choices[0].Rows.Rows[0].Future==81 && reread.Choices[0].Rows.Rows[1].Future==82 && reread.Choices[1].One.Future==91,"retained values never move to a different arm or array element "+message);
            }
            finally { NativeMemory.AlignedFree(region); NativeMemory.Free(storage); NativeMemory.Free(ids); }
        }
    }
}
