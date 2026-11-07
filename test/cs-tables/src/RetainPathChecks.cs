using System;
using System.Runtime.InteropServices;
using R1=Csretain1;
using R2=Csretain2;

static partial class Program
{
    static unsafe void TestRetentionPaths()
    {
        var newer=new R2.Root();
        newer.Choice.Type=R2.ChoiceType.Rows;
        newer.Choice.Rows.Rows[0].Known=1; newer.Choice.Rows.Rows[0].Future=71;
        newer.Choice.Rows.Rows[1].Known=2; newer.Choice.Rows.Rows[1].Future=72;
        newer.Choices[0].Type=R2.ChoiceType.Rows;
        newer.Choices[0].Rows.Rows[0].Future=81; newer.Choices[0].Rows.Rows[1].Future=82;
        newer.Choices[1].Type=R2.ChoiceType.One; newer.Choices[1].One.Future=91;
        byte[] original=new byte[R2.Schema.RootMeasure(newer)]; R2.Schema.RootSave(newer,original);
        {
            byte[] wire=original;
            long need=R1.Schema.RootLoadMeasure(wire);
            byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)need,64);
            byte* storage=(byte*)NativeMemory.Alloc(16384);
            R1.TableRetain.Id* ids=(R1.TableRetain.Id*)NativeMemory.Alloc((nuint)(1024*sizeof(R1.TableRetain.Id)));
            var stores=new R1.TableRetain[]{new R1.TableRetain { Bytes=storage,Capacity=16384,Ids=ids,IdCapacity=1024 }};
            var report=new R1.TableReport();
            try
            {
                R1.Schema.RootLoadRetain((IntPtr)region,need,wire,ref stores[0],report);
                Check(!report.Malformed && !report.Refused && report.Retained==5 && stores[0].Count==5,"nested union and element retention paths");
                byte[] saved=new byte[R1.Schema.RootMeasureRetain((IntPtr)region,ref stores[0])];
                Check(R1.Schema.RootSaveRetain((IntPtr)region,ref stores[0],saved,new R1.TableReport())==saved.Length && saved.AsSpan().SequenceEqual(original),"nested retention paths reproduce writer values");
                ((R1.RootRow*)region)->Choice.Type=R1.ChoiceType.One;
                byte[] changed=new byte[R1.Schema.RootMeasureRetain((IntPtr)region,ref stores[0])];
                var save=new R1.TableReport(); R1.Schema.RootSaveRetain((IntPtr)region,ref stores[0],changed,save);
                Check(save.RetainLost==2,"switching an arm loses only its descendants");
                var reread=new R2.Root(); var readReport=new R2.TableReport(); R2.Schema.RootLoad(reread,changed,readReport);
                Check(!readReport.Malformed && reread.Choice.One.Future==0 && reread.Choices[0].Rows.Rows[0].Future==81 && reread.Choices[0].Rows.Rows[1].Future==82 && reread.Choices[1].One.Future==91,"retained values never move to a different arm or array element");
            }
            finally { NativeMemory.AlignedFree(region); NativeMemory.Free(storage); NativeMemory.Free(ids); }
        }
    }
}
